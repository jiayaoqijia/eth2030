package state

import (
	"math/big"
	"sort"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/trie"
)

// stateObject represents an Ethereum account with its associated state.
type stateObject struct {
	account          types.Account
	code             []byte
	dirtyStorage     map[types.Hash]types.Hash
	committedStorage map[types.Hash]types.Hash
	selfDestructed   bool
	deleted          bool // finalized deletion; retain a tombstone until durable commit

	// dbStorageRoot caches the storage Merkle root as stored in the DB at the
	// time this account was loaded by TrieStateDB.loadFromDB.  It is the zero
	// hash for freshly-created accounts.  TrieStateDB uses it as a fast path
	// to skip recomputing the storage root when no slots were modified.
	dbStorageRoot types.Hash
}

func newStateObject() *stateObject {
	return &stateObject{
		account:          types.NewAccount(),
		dirtyStorage:     make(map[types.Hash]types.Hash),
		committedStorage: make(map[types.Hash]types.Hash),
	}
}

// MemoryStateDB is an in-memory implementation of the StateDB interface.
type MemoryStateDB struct {
	stateObjects     map[types.Address]*stateObject
	journal          *journal
	logs             map[types.Hash][]*types.Log
	refund           uint64
	accessList       *accessList
	transientStorage map[types.Address]map[types.Hash]types.Hash

	// Current transaction context for log attribution.
	txHash  types.Hash
	txIndex int
}

// NewMemoryStateDB creates a new in-memory state database.
func NewMemoryStateDB() *MemoryStateDB {
	return &MemoryStateDB{
		stateObjects:     make(map[types.Address]*stateObject),
		journal:          newJournal(),
		logs:             make(map[types.Hash][]*types.Log),
		accessList:       newAccessList(),
		transientStorage: make(map[types.Address]map[types.Hash]types.Hash),
	}
}

func (s *MemoryStateDB) getStateObject(addr types.Address) *stateObject {
	return s.stateObjects[addr]
}

func (s *MemoryStateDB) getLiveStateObject(addr types.Address) *stateObject {
	obj := s.stateObjects[addr]
	if obj != nil && obj.deleted {
		return nil
	}
	return obj
}

func (s *MemoryStateDB) getOrNewStateObject(addr types.Address) *stateObject {
	if obj := s.stateObjects[addr]; obj != nil && !obj.deleted {
		return obj
	}
	s.journal.append(createAccountChange{addr: addr, prev: s.stateObjects[addr]})
	obj := newStateObject()
	s.stateObjects[addr] = obj
	return obj
}

// --- Account operations ---

func (s *MemoryStateDB) CreateAccount(addr types.Address) {
	prev := s.stateObjects[addr] // may be nil
	s.journal.append(createAccountChange{addr: addr, prev: prev})
	s.stateObjects[addr] = newStateObject()
}

func (s *MemoryStateDB) SubBalance(addr types.Address, amount *big.Int) {
	obj := s.getOrNewStateObject(addr)
	s.journal.append(balanceChange{addr: addr, prev: new(big.Int).Set(obj.account.Balance)})
	obj.account.Balance = new(big.Int).Sub(obj.account.Balance, amount)
}

func (s *MemoryStateDB) AddBalance(addr types.Address, amount *big.Int) {
	obj := s.getOrNewStateObject(addr)
	s.journal.append(balanceChange{addr: addr, prev: new(big.Int).Set(obj.account.Balance)})
	obj.account.Balance = new(big.Int).Add(obj.account.Balance, amount)
}

func (s *MemoryStateDB) GetBalance(addr types.Address) *big.Int {
	if obj := s.getLiveStateObject(addr); obj != nil {
		return new(big.Int).Set(obj.account.Balance)
	}
	return new(big.Int)
}

func (s *MemoryStateDB) GetNonce(addr types.Address) uint64 {
	if obj := s.getLiveStateObject(addr); obj != nil {
		return obj.account.Nonce
	}
	return 0
}

func (s *MemoryStateDB) SetNonce(addr types.Address, nonce uint64) {
	obj := s.getOrNewStateObject(addr)
	s.journal.append(nonceChange{addr: addr, prev: obj.account.Nonce})
	obj.account.Nonce = nonce
}

func (s *MemoryStateDB) GetCode(addr types.Address) []byte {
	if obj := s.getLiveStateObject(addr); obj != nil {
		return obj.code
	}
	return nil
}

func (s *MemoryStateDB) SetCode(addr types.Address, code []byte) {
	obj := s.getOrNewStateObject(addr)
	prevCode := obj.code
	prevHash := make([]byte, len(obj.account.CodeHash))
	copy(prevHash, obj.account.CodeHash)
	s.journal.append(codeChange{addr: addr, prevCode: prevCode, prevHash: prevHash})
	obj.code = code
	obj.account.CodeHash = crypto.Keccak256(code)
}

func (s *MemoryStateDB) GetCodeHash(addr types.Address) types.Hash {
	if obj := s.getLiveStateObject(addr); obj != nil {
		return types.BytesToHash(obj.account.CodeHash)
	}
	return types.Hash{}
}

func (s *MemoryStateDB) GetCodeSize(addr types.Address) int {
	if obj := s.getLiveStateObject(addr); obj != nil {
		return len(obj.code)
	}
	return 0
}

// --- Self-destruct ---

func (s *MemoryStateDB) SelfDestruct(addr types.Address) {
	obj := s.getLiveStateObject(addr)
	if obj == nil {
		return
	}
	s.journal.append(selfDestructChange{
		addr:           addr,
		prevDestructed: obj.selfDestructed,
		prevDeleted:    obj.deleted,
		prevBalance:    new(big.Int).Set(obj.account.Balance),
	})
	obj.selfDestructed = true
	obj.account.Balance = new(big.Int)
}

func (s *MemoryStateDB) HasSelfDestructed(addr types.Address) bool {
	if obj := s.getLiveStateObject(addr); obj != nil {
		return obj.selfDestructed
	}
	return false
}

// --- Storage operations ---

func (s *MemoryStateDB) GetState(addr types.Address, key types.Hash) types.Hash {
	if obj := s.getLiveStateObject(addr); obj != nil {
		if val, ok := obj.dirtyStorage[key]; ok {
			return val
		}
		return obj.committedStorage[key]
	}
	return types.Hash{}
}

// StorageSnapshot returns the account's current nonzero storage slots.
func (s *MemoryStateDB) StorageSnapshot(addr types.Address) map[types.Hash]types.Hash {
	result := make(map[types.Hash]types.Hash)
	if obj := s.getLiveStateObject(addr); obj != nil {
		for slot, value := range obj.committedStorage {
			if !value.IsZero() {
				result[slot] = value
			}
		}
		for slot, value := range obj.dirtyStorage {
			if value.IsZero() {
				delete(result, slot)
			} else {
				result[slot] = value
			}
		}
	}
	return result
}

func (s *MemoryStateDB) SetState(addr types.Address, key types.Hash, value types.Hash) {
	obj := s.getOrNewStateObject(addr)
	prevDirty, prevExists := obj.dirtyStorage[key]
	var prev types.Hash
	if prevExists {
		prev = prevDirty
	} else {
		prev = obj.committedStorage[key]
	}
	s.journal.append(storageChange{addr: addr, key: key, prev: prev, prevExists: prevExists})
	obj.dirtyStorage[key] = value
}

func (s *MemoryStateDB) GetCommittedState(addr types.Address, key types.Hash) types.Hash {
	if obj := s.getLiveStateObject(addr); obj != nil {
		return obj.committedStorage[key]
	}
	return types.Hash{}
}

// --- Account existence ---

func (s *MemoryStateDB) Exist(addr types.Address) bool {
	return s.getLiveStateObject(addr) != nil
}

func (s *MemoryStateDB) Empty(addr types.Address) bool {
	obj := s.getLiveStateObject(addr)
	if obj == nil {
		return true
	}
	return obj.account.Nonce == 0 &&
		obj.account.Balance.Sign() == 0 &&
		types.BytesToHash(obj.account.CodeHash) == types.EmptyCodeHash
}

// --- Snapshot and revert ---

func (s *MemoryStateDB) Snapshot() int {
	return s.journal.snapshot()
}

func (s *MemoryStateDB) RevertToSnapshot(id int) {
	s.journal.revertToSnapshot(id, s)
}

// --- Logs ---

func (s *MemoryStateDB) AddLog(log *types.Log) {
	// Use the current tx context hash so logs are keyed correctly.
	txHash := s.txHash
	log.TxHash = txHash
	log.TxIndex = uint(s.txIndex)
	s.journal.append(logChange{txHash: txHash, prevLen: len(s.logs[txHash])})
	s.logs[txHash] = append(s.logs[txHash], log)
}

func (s *MemoryStateDB) GetLogs(txHash types.Hash) []*types.Log {
	return s.logs[txHash]
}

// SetTxContext sets the current transaction hash and index for log attribution.
func (s *MemoryStateDB) SetTxContext(txHash types.Hash, txIndex int) {
	s.txHash = txHash
	s.txIndex = txIndex
}

// --- Refund counter ---

func (s *MemoryStateDB) AddRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	s.refund += gas
}

func (s *MemoryStateDB) SubRefund(gas uint64) {
	s.journal.append(refundChange{prev: s.refund})
	s.refund -= gas
}

func (s *MemoryStateDB) GetRefund() uint64 {
	return s.refund
}

// --- Access list (EIP-2929) ---

func (s *MemoryStateDB) AddAddressToAccessList(addr types.Address) {
	if !s.accessList.AddAddress(addr) {
		s.journal.append(accessListAddAccountChange{addr: addr})
	}
}

func (s *MemoryStateDB) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	addrPresent, slotPresent := s.accessList.AddSlot(addr, slot)
	if !addrPresent {
		s.journal.append(accessListAddAccountChange{addr: addr})
	}
	if !slotPresent {
		s.journal.append(accessListAddSlotChange{addr: addr, slot: slot})
	}
}

func (s *MemoryStateDB) AddressInAccessList(addr types.Address) bool {
	return s.accessList.ContainsAddress(addr)
}

func (s *MemoryStateDB) SlotInAccessList(addr types.Address, slot types.Hash) (addressOk bool, slotOk bool) {
	return s.accessList.ContainsSlot(addr, slot)
}

func (s *MemoryStateDB) ClearAccessList() {
	s.accessList = newAccessList()
}

// --- Transient storage (EIP-1153) ---

func (s *MemoryStateDB) GetTransientState(addr types.Address, key types.Hash) types.Hash {
	if slots, ok := s.transientStorage[addr]; ok {
		return slots[key]
	}
	return types.Hash{}
}

func (s *MemoryStateDB) SetTransientState(addr types.Address, key types.Hash, value types.Hash) {
	prev := s.GetTransientState(addr, key)
	s.journal.append(transientStorageChange{addr: addr, key: key, prev: prev})
	if _, ok := s.transientStorage[addr]; !ok {
		s.transientStorage[addr] = make(map[types.Hash]types.Hash)
	}
	s.transientStorage[addr][key] = value
}

// ClearTransientStorage resets all transient storage. Per EIP-1153, transient
// storage is cleared at the end of each transaction.
func (s *MemoryStateDB) ClearTransientStorage() {
	s.transientStorage = make(map[types.Address]map[types.Hash]types.Hash)
}

// --- Commit ---

// rlpAccount is the RLP-serializable form of an Ethereum account (Yellow Paper).
type rlpAccount struct {
	Nonce    uint64
	Balance  *big.Int
	Root     []byte // storage trie root hash (32 bytes)
	CodeHash []byte // keccak256 of code (32 bytes)
}

// mergeStorage builds a merged view of committed+dirty storage for an account,
// deleting any zero-valued entries (which represent slot deletions).
func mergeStorage(obj *stateObject) map[types.Hash]types.Hash {
	merged := make(map[types.Hash]types.Hash, len(obj.committedStorage)+len(obj.dirtyStorage))
	for k, v := range obj.committedStorage {
		merged[k] = v
	}
	for k, v := range obj.dirtyStorage {
		if v == (types.Hash{}) {
			delete(merged, k)
		} else {
			merged[k] = v
		}
	}
	return merged
}

// computeStorageRoot builds a storage trie from the account's committed+dirty
// storage and returns its Merkle root hash, using proper Ethereum encoding:
//
//	key   = Keccak256(slot)
//	value = RLP(trimmedValue)
func computeStorageRoot(obj *stateObject) types.Hash {
	merged := mergeStorage(obj)
	if len(merged) == 0 {
		return types.EmptyRootHash
	}

	storageTrie := trie.New()
	for slot, val := range merged {
		// Key is the Keccak-256 hash of the storage slot.
		hashedSlot := crypto.Keccak256(slot[:])

		// Value is RLP-encoded with leading zeros stripped.
		trimmed := trimLeadingZeros(val[:])
		encoded, err := rlp.EncodeToBytes(trimmed)
		if err != nil {
			continue
		}
		storageTrie.Put(hashedSlot, encoded)
	}
	return storageTrie.Hash()
}

// trimLeadingZeros removes leading zero bytes from a byte slice.
// Returns an empty slice if the input is all zeros.
func trimLeadingZeros(b []byte) []byte {
	for i, v := range b {
		if v != 0 {
			return b[i:]
		}
	}
	return []byte{}
}

func (s *MemoryStateDB) Commit() (types.Hash, error) {
	for addr, obj := range s.stateObjects {
		if obj.selfDestructed {
			delete(s.stateObjects, addr)
		}
	}
	s.journal = newJournal()
	// Flush dirty storage to committed storage.
	for _, obj := range s.stateObjects {
		for key, val := range obj.dirtyStorage {
			if val == (types.Hash{}) {
				delete(obj.committedStorage, key)
			} else {
				obj.committedStorage[key] = val
			}
		}
		obj.dirtyStorage = make(map[types.Hash]types.Hash)
	}

	if len(s.stateObjects) == 0 {
		return types.EmptyRootHash, nil
	}

	// Build the state trie: key = keccak(address), value = rlp(account)
	stateTrie := trie.New()

	// Sort addresses for deterministic ordering (trie is deterministic
	// regardless, but this makes debugging easier).
	addrs := make([]types.Address, 0, len(s.stateObjects))
	for addr := range s.stateObjects {
		addrs = append(addrs, addr)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return addrs[i].Hex() < addrs[j].Hex()
	})

	for _, addr := range addrs {
		obj := s.stateObjects[addr]
		if obj.selfDestructed {
			continue
		}

		// Compute storage root for this account.
		storageRoot := computeStorageRoot(obj)
		obj.account.Root = storageRoot

		// Ensure CodeHash is set.
		codeHash := obj.account.CodeHash
		if len(codeHash) == 0 {
			codeHash = types.EmptyCodeHash.Bytes()
		}

		// RLP-encode the account.
		acc := rlpAccount{
			Nonce:    obj.account.Nonce,
			Balance:  obj.account.Balance,
			Root:     storageRoot[:],
			CodeHash: codeHash,
		}
		encoded, err := rlp.EncodeToBytes(acc)
		if err != nil {
			return types.Hash{}, err
		}

		// Key is the Keccak-256 hash of the address.
		hashedAddr := crypto.Keccak256(addr[:])
		stateTrie.Put(hashedAddr, encoded)
	}

	return stateTrie.Hash(), nil
}

// StorageRoot computes and returns the storage trie root for the given address.
// Returns EmptyRootHash if the account does not exist or has no storage.
func (s *MemoryStateDB) StorageRoot(addr types.Address) types.Hash {
	obj := s.getLiveStateObject(addr)
	if obj == nil {
		return types.EmptyRootHash
	}
	return computeStorageRoot(obj)
}

// GetRoot returns the current state root without flushing dirty storage.
// Useful for computing the root mid-block.
func (s *MemoryStateDB) GetRoot() types.Hash {
	if len(s.stateObjects) == 0 {
		return types.EmptyRootHash
	}

	stateTrie := trie.New()
	for addr, obj := range s.stateObjects {
		if obj.selfDestructed {
			continue
		}

		storageRoot := computeStorageRoot(obj)
		codeHash := obj.account.CodeHash
		if len(codeHash) == 0 {
			codeHash = types.EmptyCodeHash.Bytes()
		}

		acc := rlpAccount{
			Nonce:    obj.account.Nonce,
			Balance:  obj.account.Balance,
			Root:     storageRoot[:],
			CodeHash: codeHash,
		}
		encoded, err := rlp.EncodeToBytes(acc)
		if err != nil {
			continue
		}

		hashedAddr := crypto.Keccak256(addr[:])
		stateTrie.Put(hashedAddr, encoded)
	}

	return stateTrie.Hash()
}

// BuildStateTrie constructs a Merkle Patricia Trie containing all accounts in
// the state. This is used for generating Merkle proofs (e.g., eth_getProof).
// Each account is keyed by keccak256(address) and the value is the RLP-encoded
// account [nonce, balance, storageRoot, codeHash].
func (s *MemoryStateDB) BuildStateTrie() *trie.Trie {
	stateTrie := trie.New()
	for addr, obj := range s.stateObjects {
		if obj.selfDestructed {
			continue
		}
		storageRoot := computeStorageRoot(obj)
		codeHash := obj.account.CodeHash
		if len(codeHash) == 0 {
			codeHash = types.EmptyCodeHash.Bytes()
		}
		acc := rlpAccount{
			Nonce:    obj.account.Nonce,
			Balance:  obj.account.Balance,
			Root:     storageRoot[:],
			CodeHash: codeHash,
		}
		encoded, err := rlp.EncodeToBytes(acc)
		if err != nil {
			continue
		}
		hashedAddr := crypto.Keccak256(addr[:])
		stateTrie.Put(hashedAddr, encoded)
	}
	return stateTrie
}

// BuildStorageTrie constructs a Merkle Patricia Trie for the given account's
// storage. Returns nil if the account does not exist or has no storage. Each
// storage slot is keyed by keccak256(slot) and the value is RLP-encoded.
func (s *MemoryStateDB) BuildStorageTrie(addr types.Address) *trie.Trie {
	obj := s.getLiveStateObject(addr)
	if obj == nil {
		return nil
	}
	merged := mergeStorage(obj)
	if len(merged) == 0 {
		return nil
	}
	storageTrie := trie.New()
	for slot, val := range merged {
		hashedSlot := crypto.Keccak256(slot[:])
		trimmed := trimLeadingZeros(val[:])
		encoded, err := rlp.EncodeToBytes(trimmed)
		if err != nil {
			continue
		}
		storageTrie.Put(hashedSlot, encoded)
	}
	return storageTrie
}

// Copy returns a deep copy of the MemoryStateDB. The copy shares no mutable
// state with the original, making it safe to use in parallel goroutines.
func (s *MemoryStateDB) Copy() *MemoryStateDB {
	cp := &MemoryStateDB{
		stateObjects:     make(map[types.Address]*stateObject, len(s.stateObjects)),
		journal:          newJournal(),
		logs:             make(map[types.Hash][]*types.Log, len(s.logs)),
		refund:           s.refund,
		accessList:       s.accessList.Copy(),
		transientStorage: make(map[types.Address]map[types.Hash]types.Hash, len(s.transientStorage)),
		txHash:           s.txHash,
		txIndex:          s.txIndex,
	}

	for addr, obj := range s.stateObjects {
		newObj := &stateObject{
			account: types.Account{
				Nonce:    obj.account.Nonce,
				Balance:  new(big.Int).Set(obj.account.Balance),
				Root:     obj.account.Root,
				CodeHash: make([]byte, len(obj.account.CodeHash)),
			},
			code:             make([]byte, len(obj.code)),
			dirtyStorage:     make(map[types.Hash]types.Hash, len(obj.dirtyStorage)),
			committedStorage: make(map[types.Hash]types.Hash, len(obj.committedStorage)),
			selfDestructed:   obj.selfDestructed,
			deleted:          obj.deleted,
			dbStorageRoot:    obj.dbStorageRoot,
		}
		copy(newObj.account.CodeHash, obj.account.CodeHash)
		copy(newObj.code, obj.code)
		for k, v := range obj.dirtyStorage {
			newObj.dirtyStorage[k] = v
		}
		for k, v := range obj.committedStorage {
			newObj.committedStorage[k] = v
		}
		cp.stateObjects[addr] = newObj
	}

	for txHash, logs := range s.logs {
		cpLogs := make([]*types.Log, len(logs))
		for i, log := range logs {
			cpLog := *log
			cpLog.Data = append([]byte(nil), log.Data...)
			cpLog.Topics = append([]types.Hash(nil), log.Topics...)
			cpLogs[i] = &cpLog
		}
		cp.logs[txHash] = cpLogs
	}

	for addr, slots := range s.transientStorage {
		cpSlots := make(map[types.Hash]types.Hash, len(slots))
		for k, v := range slots {
			cpSlots[k] = v
		}
		cp.transientStorage[addr] = cpSlots
	}

	return cp
}

// Merge applies all state changes from src into this MemoryStateDB.
// This is used to merge results from parallel execution back into the main state.
func (s *MemoryStateDB) Merge(src *MemoryStateDB) {
	for addr, srcObj := range src.stateObjects {
		dstObj := s.getOrNewStateObject(addr)
		dstObj.account.Balance = new(big.Int).Set(srcObj.account.Balance)
		dstObj.account.Nonce = srcObj.account.Nonce
		dstObj.account.CodeHash = make([]byte, len(srcObj.account.CodeHash))
		copy(dstObj.account.CodeHash, srcObj.account.CodeHash)
		dstObj.code = make([]byte, len(srcObj.code))
		copy(dstObj.code, srcObj.code)
		dstObj.selfDestructed = srcObj.selfDestructed
		for k, v := range srcObj.dirtyStorage {
			dstObj.dirtyStorage[k] = v
		}
	}
}

// Prefetch pre-loads state for the given addresses into the state cache.
// This is a no-op for addresses already loaded. For MemoryStateDB (which
// keeps everything in memory), this simply ensures the state objects exist,
// making subsequent reads faster by avoiding nil checks. In a disk-backed
// implementation, this would trigger async reads from the database.
func (s *MemoryStateDB) Prefetch(addrs []types.Address) {
	for _, addr := range addrs {
		// Trigger creation of the state object if it doesn't exist.
		// This pre-warms the cache so parallel transaction processing
		// can avoid contention on lazy initialization.
		if s.stateObjects[addr] == nil {
			// For prefetch, we only ensure the entry exists in the map
			// without creating a journal entry (this is a read-side hint).
			s.stateObjects[addr] = newStateObject()
		}
	}
}

// PrefetchStorage pre-loads storage slots for the given address into cache.
// For MemoryStateDB this is a no-op since all storage is already in memory,
// but it establishes the interface contract for disk-backed implementations.
func (s *MemoryStateDB) PrefetchStorage(addr types.Address, keys []types.Hash) {
	// Ensure the state object exists.
	if s.stateObjects[addr] == nil {
		s.stateObjects[addr] = newStateObject()
	}
	// In a disk-backed implementation, this would trigger async reads
	// of the specified storage keys from the backing store.
}

// FinalizePreState copies current dirty storage into committed storage for all accounts.
// Call this after loading pre-state but before executing transactions, so that
// GetCommittedState returns correct "original" values for SSTORE gas calculations.
func (s *MemoryStateDB) FinalizePreState() {
	for _, obj := range s.stateObjects {
		for key, value := range obj.dirtyStorage {
			obj.committedStorage[key] = value
		}
	}
}

func (s *MemoryStateDB) FinalizeTransaction() {
	for _, obj := range s.stateObjects {
		if obj.selfDestructed {
			obj.deleted = true
		}
	}
}

// SnapshotDiff returns the current state serialized as hash-keyed maps
// suitable for snapshot.Tree.Update. The caller receives ownership.
//
//   - accounts: keccak256(address) → RLP-encoded slim account (nil = deleted)
//   - storage:  keccak256(address) → (keccak256(slot) → slot bytes) (nil = deleted)
func (s *MemoryStateDB) SnapshotDiff() (map[types.Hash][]byte, map[types.Hash]map[types.Hash][]byte) {
	accounts := make(map[types.Hash][]byte, len(s.stateObjects))
	storage := make(map[types.Hash]map[types.Hash][]byte)

	for addr, obj := range s.stateObjects {
		var addrHash types.Hash
		copy(addrHash[:], crypto.Keccak256(addr[:]))

		if obj.selfDestructed {
			accounts[addrHash] = nil // mark as deleted
			continue
		}

		storageRoot := computeStorageRoot(obj)
		codeHash := obj.account.CodeHash
		if len(codeHash) == 0 {
			codeHash = types.EmptyCodeHash.Bytes()
		}
		acc := rlpAccount{
			Nonce:    obj.account.Nonce,
			Balance:  obj.account.Balance,
			Root:     storageRoot[:],
			CodeHash: codeHash,
		}
		encoded, err := rlp.EncodeToBytes(acc)
		if err != nil {
			continue
		}
		accounts[addrHash] = encoded

		// Merge committed + dirty storage into snapshot slots.
		totalSlots := len(obj.committedStorage) + len(obj.dirtyStorage)
		if totalSlots == 0 {
			continue
		}
		slots := make(map[types.Hash][]byte, totalSlots)
		for slot, val := range obj.committedStorage {
			var slotHash types.Hash
			copy(slotHash[:], crypto.Keccak256(slot[:]))
			if val == (types.Hash{}) {
				slots[slotHash] = nil
			} else {
				// Snap protocol stores values as RLP(trimmed big-endian bytes).
				trimmed := new(big.Int).SetBytes(val[:]).Bytes()
				enc, _ := rlp.EncodeToBytes(trimmed)
				slots[slotHash] = enc
			}
		}
		for slot, val := range obj.dirtyStorage {
			var slotHash types.Hash
			copy(slotHash[:], crypto.Keccak256(slot[:]))
			if val == (types.Hash{}) {
				slots[slotHash] = nil
			} else {
				// Snap protocol stores values as RLP(trimmed big-endian bytes).
				trimmed := new(big.Int).SetBytes(val[:]).Bytes()
				enc, _ := rlp.EncodeToBytes(trimmed)
				slots[slotHash] = enc
			}
		}
		storage[addrHash] = slots
	}
	return accounts, storage
}

// Dup returns a deep copy of the MemoryStateDB as a StateDB interface.
// This implements the StateDB.Dup method for use by state caches and
// callers that work with the StateDB interface.
func (s *MemoryStateDB) Dup() StateDB {
	return s.Copy()
}

// Verify interface compliance at compile time.
var _ StateDB = (*MemoryStateDB)(nil)
