// Package state provides state management for Ethereum accounts and storage.
package state

import (
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/trie"
)

// DB key prefixes — chosen to avoid collision with block/receipt/tx rawdb keys.
var (
	dbPrefixAccount = []byte("sa") // "sa" + addr[20]   → RLP-encoded account
	dbPrefixStorage = []byte("ss") // "ss" + addr[20] + slot[32] → val[32]
	dbPrefixCode    = []byte("sc") // "sc" + codeHash[32] → code bytes
)

func accountDBKey(addr types.Address) []byte {
	key := make([]byte, 2+20)
	copy(key, dbPrefixAccount)
	copy(key[2:], addr[:])
	return key
}

func storageDBKey(addr types.Address, slot types.Hash) []byte {
	key := make([]byte, 2+20+32)
	copy(key, dbPrefixStorage)
	copy(key[2:], addr[:])
	copy(key[22:], slot[:])
	return key
}

func storageDBPrefix(addr types.Address) []byte {
	key := make([]byte, 2+20)
	copy(key, dbPrefixStorage)
	copy(key[2:], addr[:])
	return key
}

func codeDBKey(codeHash []byte) []byte {
	key := make([]byte, 2+32)
	copy(key, dbPrefixCode)
	copy(key[2:], codeHash)
	return key
}

// GCModeArchive and GCModeFull are the two supported garbage-collection modes.
// GCModeArchive (default) retains all historical state — no state keys are
// ever deleted beyond explicit zeroing or self-destruct. GCModeFull will in
// future prune overwritten state keys after each Commit to save disk space.
const (
	GCModeArchive = "archive"
	GCModeFull    = "full"
)

// TrieStateDB is a disk-backed StateDB implementation. It keeps a
// MemoryStateDB as a dirty write buffer for the current block and persists
// committed state to a rawdb.Database.
//
// Memory model: after each Commit() the dirty buffer is reset. Subsequent
// reads populate the buffer on demand from the DB. Memory usage is therefore
// proportional to the working set of a single block rather than growing with
// chain history.
type TrieStateDB struct {
	mem       *MemoryStateDB             // dirty write buffer for the current block
	db        rawdb.Database             // persistent backing store
	gcMode    string                     // GCModeArchive or GCModeFull
	recreated map[types.Address]struct{} // accounts reset by CreateAccount this block

	// frozenAccounts is a read-only snapshot of every account RLP in the DB
	// as it was at the end of the most recent Commit(). buildStateTrie() uses
	// this map instead of iterating the live DB so that concurrent commits from
	// other goroutines (e.g. a newPayload running in parallel with a block
	// builder) cannot corrupt the state-root computation.
	// The map is shared across Dup() copies; callers must not mutate it.
	frozenAccounts map[types.Address][]byte

	// persistedStorage is a per-address index of all storage slots currently
	// in the DB.  It is updated in Commit() with copy-on-write (COW) semantics
	// so that Dup() copies share the inner maps cheaply.
	//
	// This replaces the O(N_total) MemoryDB prefix scan in computeStorageRootFromDB
	// with an O(K_addr) per-address map lookup, eliminating the O(N²) CPU growth
	// seen with storagespam workloads.
	//
	// The map is nil until the first Commit(). A nil map means "no persisted
	// storage yet"; computeStorageRootFromDB treats it as an empty set and falls
	// back to the DB iterator for correctness on instances that adopt a
	// pre-existing DB (e.g. NewTrieStateDBFromMemory before genesis Commit).
	persistedStorage map[types.Address]map[types.Hash]types.Hash

	// commitRoot caches the state root computed by the most recent Commit().
	// GetRoot() returns this value directly when the dirty buffer is empty,
	// avoiding a redundant full-trie rebuild.  It is invalidated (set to zero)
	// whenever any state-modifying call is made after the last Commit.
	commitRoot types.Hash

	// cachedRoot is a one-shot cache: set by GetRoot() after building the trie,
	// consumed by the next Commit() call so it does not rebuild the trie again.
	// Cleared after Commit() or when any mutation happens after GetRoot().
	cachedRoot types.Hash

	// committedTrie is the fully-built MPT over all persisted accounts as of the
	// most recent Commit(). It is updated incrementally on each Commit() — only
	// the dirty accounts from that block are applied — so that buildStateTrie()
	// can Clone it and apply the current dirty layer in O(N_dirty) rather than
	// rebuilding the entire trie from frozenAccounts in O(N_total).
	//
	// nil until the first Commit(); buildStateTrie() falls back to the
	// frozenAccounts iteration path when it is nil.
	committedTrie *trie.Trie
}

// GetMem returns a reference to the internal MemoryStateDB dirty buffer.
// Callers must not modify the returned value; use it only for snapshotting
// the in-memory state (e.g. at genesis, before any Commit has been called).
func (t *TrieStateDB) GetMem() *MemoryStateDB {
	return t.mem
}

// GCMode returns the garbage-collection mode string ("archive" or "full").
func (t *TrieStateDB) GCMode() string {
	return t.gcMode
}

// DB returns the underlying database backing this TrieStateDB.
func (t *TrieStateDB) DB() rawdb.Database {
	return t.db
}

// NewTrieStateDB creates a TrieStateDB backed by db using archive GC mode.
func NewTrieStateDB(db rawdb.Database) *TrieStateDB {
	return NewTrieStateDBWithGCMode(db, GCModeArchive)
}

// NewTrieStateDBWithGCMode creates a TrieStateDB with the specified GC mode.
// Use GCModeArchive to retain all history or GCModeFull to prune overwritten
// state after each Commit (pruning is a no-op in the current implementation;
// the field is plumbed for future use).
func NewTrieStateDBWithGCMode(db rawdb.Database, gcMode string) *TrieStateDB {
	if gcMode != GCModeFull {
		gcMode = GCModeArchive
	}
	return &TrieStateDB{
		mem:    NewMemoryStateDB(),
		db:     db,
		gcMode: gcMode,
	}
}

// NewTrieStateDBFromMemory creates a TrieStateDB that adopts an existing
// MemoryStateDB as its initial dirty layer. Call Commit() immediately after
// to persist the in-memory state to the DB and free the dirty layer. This
// is typically used to convert a genesis MemoryStateDB into a TrieStateDB.
func NewTrieStateDBFromMemory(db rawdb.Database, mem *MemoryStateDB) *TrieStateDB {
	return NewTrieStateDBFromMemoryWithGCMode(db, mem, GCModeArchive)
}

// NewTrieStateDBFromMemoryWithGCMode is like NewTrieStateDBFromMemory but
// accepts an explicit GC mode.
func NewTrieStateDBFromMemoryWithGCMode(db rawdb.Database, mem *MemoryStateDB, gcMode string) *TrieStateDB {
	if gcMode != GCModeFull {
		gcMode = GCModeArchive
	}
	// The adopted state may already have been committed in memory. Its
	// committed slots still need their first write to this backing store.
	for _, obj := range mem.stateObjects {
		for slot, value := range obj.committedStorage {
			if _, dirty := obj.dirtyStorage[slot]; !dirty {
				obj.dirtyStorage[slot] = value
			}
		}
	}
	return &TrieStateDB{
		mem:    mem,
		db:     db,
		gcMode: gcMode,
	}
}

// loadFromDB loads account state from the persistent store into the dirty
// buffer. It is a no-op if the address is already buffered.
//
// When frozenAccounts is available (after the first Commit), it is used as
// the authoritative source instead of the live DB. This prevents a race
// where a concurrent InsertBlock commit updates the DB while the builder is
// reading account state, causing non-deterministic gas computation.
func (t *TrieStateDB) loadFromDB(addr types.Address) {
	if t.mem.stateObjects[addr] != nil {
		return
	}
	var data []byte
	if t.frozenAccounts != nil {
		// Use the frozen snapshot: it is a complete view of all committed
		// accounts and is never mutated after the Commit that created it.
		frozen, ok := t.frozenAccounts[addr]
		if !ok {
			return // account does not exist in the committed state
		}
		data = frozen
	} else {
		// Pre-first-commit: no snapshot yet; read directly from DB.
		var err error
		data, err = t.db.Get(accountDBKey(addr))
		if err != nil {
			return // not in DB → account does not exist
		}
	}
	var acc rlpAccount
	if err := rlp.DecodeBytes(data, &acc); err != nil {
		return
	}
	if acc.Balance == nil {
		acc.Balance = new(big.Int)
	}
	obj := &stateObject{
		account: types.Account{
			Nonce:    acc.Nonce,
			Balance:  new(big.Int).Set(acc.Balance),
			CodeHash: make([]byte, len(acc.CodeHash)),
		},
		dirtyStorage:     make(map[types.Hash]types.Hash),
		committedStorage: make(map[types.Hash]types.Hash),
		dbStorageRoot:    types.BytesToHash(acc.Root),
	}
	copy(obj.account.CodeHash, acc.CodeHash)

	// Storage slots are NOT loaded here — they are loaded lazily on demand
	// by GetState / GetCommittedState / SetState.  Eager bulk loading was the
	// root cause of O(N²) memory growth with storagespam workloads.

	// Load contract code if non-empty.
	if len(acc.CodeHash) > 0 && types.BytesToHash(acc.CodeHash) != types.EmptyCodeHash {
		if code, err := t.db.Get(codeDBKey(acc.CodeHash)); err == nil {
			obj.code = code
		}
	}

	t.mem.stateObjects[addr] = obj
}

// loadSlotFromDB loads a single storage slot from the persistent store into
// obj.committedStorage.  Returns the slot value (zero hash if not in DB).
//
// When persistedStorage is available (after the first Commit), the in-memory
// slot index is used instead of the live DB to prevent contamination from
// concurrent InsertBlock commits.
func (t *TrieStateDB) loadSlotFromDB(addr types.Address, obj *stateObject, slot types.Hash) types.Hash {
	if t.persistedStorage != nil {
		slots, ok := t.persistedStorage[addr]
		if !ok {
			// Address has no persisted storage in the snapshot.
			return types.Hash{}
		}
		val := slots[slot]
		if val != (types.Hash{}) {
			obj.committedStorage[slot] = val
		}
		return val
	}
	// Pre-first-commit or address not yet tracked: read directly from DB.
	data, err := t.db.Get(storageDBKey(addr, slot))
	if err != nil {
		return types.Hash{} // slot not in DB
	}
	var val types.Hash
	copy(val[:], data)
	if val != (types.Hash{}) {
		obj.committedStorage[slot] = val
	}
	return val
}

// computeStorageRootFromDB computes the storage Merkle root for addr by
// merging all persisted slots (from t.persistedStorage) with the in-memory
// committed and dirty overlays (dirty > committed > persisted).
//
// Uses the O(K_addr) per-address index instead of an O(N_total) DB prefix
// scan, eliminating the O(N²) CPU growth with storagespam workloads.
func (t *TrieStateDB) computeStorageRootFromDB(addr types.Address, obj *stateObject) types.Hash {
	// Build overlay: committed (sparse cache + recently-flushed dirty) then dirty.
	overlay := make(map[types.Hash]types.Hash, len(obj.committedStorage)+len(obj.dirtyStorage))
	for slot, val := range obj.committedStorage {
		overlay[slot] = val
	}
	for slot, val := range obj.dirtyStorage {
		overlay[slot] = val
	}

	storageTrie := trie.New()
	seen := make(map[types.Hash]bool, len(overlay))

	// Walk persisted slots for this address (O(K_addr), not O(N_total)).
	// t.persistedStorage[addr] is nil for brand-new accounts — safe to range over nil map.
	persisted := t.persistedStorage[addr]
	if obj.dbStorageRoot == (types.Hash{}) {
		persisted = nil
	}
	for slot, dbVal := range persisted {
		seen[slot] = true
		val, overridden := overlay[slot]
		if !overridden {
			val = dbVal
		}
		if val != (types.Hash{}) {
			hashedSlot := crypto.Keccak256(slot[:])
			trimmed := trimLeadingZeros(val[:])
			encoded, err := rlp.EncodeToBytes(trimmed)
			if err == nil {
				storageTrie.Put(hashedSlot, encoded)
			}
		}
	}

	// Add overlay slots not yet in persistedStorage (new this block).
	for slot, val := range overlay {
		if seen[slot] {
			continue
		}
		if val != (types.Hash{}) {
			hashedSlot := crypto.Keccak256(slot[:])
			trimmed := trimLeadingZeros(val[:])
			encoded, err := rlp.EncodeToBytes(trimmed)
			if err == nil {
				storageTrie.Put(hashedSlot, encoded)
			}
		}
	}

	return storageTrie.Hash()
}

// --- StateDB interface: account operations ---

func (t *TrieStateDB) CreateAccount(addr types.Address) {
	t.cachedRoot = types.Hash{}
	t.commitRoot = types.Hash{}
	t.loadFromDB(addr)
	t.mem.CreateAccount(addr)
}

func (t *TrieStateDB) SubBalance(addr types.Address, amount *big.Int) {
	t.cachedRoot = types.Hash{}
	t.commitRoot = types.Hash{}
	t.loadFromDB(addr)
	t.mem.SubBalance(addr, amount)
}

func (t *TrieStateDB) AddBalance(addr types.Address, amount *big.Int) {
	t.cachedRoot = types.Hash{}
	t.commitRoot = types.Hash{}
	t.loadFromDB(addr)
	t.mem.AddBalance(addr, amount)
}

func (t *TrieStateDB) GetBalance(addr types.Address) *big.Int {
	t.loadFromDB(addr)
	return t.mem.GetBalance(addr)
}

func (t *TrieStateDB) GetNonce(addr types.Address) uint64 {
	t.loadFromDB(addr)
	return t.mem.GetNonce(addr)
}

func (t *TrieStateDB) SetNonce(addr types.Address, nonce uint64) {
	t.cachedRoot = types.Hash{}
	t.commitRoot = types.Hash{}
	t.loadFromDB(addr)
	t.mem.SetNonce(addr, nonce)
}

func (t *TrieStateDB) GetCode(addr types.Address) []byte {
	t.loadFromDB(addr)
	return t.mem.GetCode(addr)
}

func (t *TrieStateDB) SetCode(addr types.Address, code []byte) {
	t.cachedRoot = types.Hash{}
	t.commitRoot = types.Hash{}
	t.loadFromDB(addr)
	t.mem.SetCode(addr, code)
}

func (t *TrieStateDB) GetCodeHash(addr types.Address) types.Hash {
	t.loadFromDB(addr)
	return t.mem.GetCodeHash(addr)
}

func (t *TrieStateDB) GetCodeSize(addr types.Address) int {
	t.loadFromDB(addr)
	return t.mem.GetCodeSize(addr)
}

// --- StateDB interface: self-destruct ---

func (t *TrieStateDB) SelfDestruct(addr types.Address) {
	t.cachedRoot = types.Hash{}
	t.commitRoot = types.Hash{}
	t.loadFromDB(addr)
	t.mem.SelfDestruct(addr)
}

func (t *TrieStateDB) HasSelfDestructed(addr types.Address) bool {
	t.loadFromDB(addr)
	return t.mem.HasSelfDestructed(addr)
}

// --- StateDB interface: storage (lazy per-slot loading) ---

func (t *TrieStateDB) GetState(addr types.Address, key types.Hash) types.Hash {
	t.loadFromDB(addr)
	obj := t.mem.stateObjects[addr]
	if obj == nil || obj.deleted {
		return types.Hash{}
	}
	if val, ok := obj.dirtyStorage[key]; ok {
		return val
	}
	if val, ok := obj.committedStorage[key]; ok {
		return val
	}
	return t.loadSlotFromDB(addr, obj, key)
}

func (t *TrieStateDB) GetCommittedState(addr types.Address, key types.Hash) types.Hash {
	t.loadFromDB(addr)
	obj := t.mem.stateObjects[addr]
	if obj == nil || obj.deleted {
		return types.Hash{}
	}
	if val, ok := obj.committedStorage[key]; ok {
		return val
	}
	return t.loadSlotFromDB(addr, obj, key)
}

func (t *TrieStateDB) SetState(addr types.Address, key types.Hash, value types.Hash) {
	t.cachedRoot = types.Hash{}
	t.commitRoot = types.Hash{}
	t.loadFromDB(addr)
	obj := t.mem.stateObjects[addr]
	if obj != nil {
		// Ensure committed value is populated before MemoryStateDB.SetState reads
		// obj.committedStorage[key] to record the journal prev value.
		if _, ok := obj.committedStorage[key]; !ok {
			if _, ok2 := obj.dirtyStorage[key]; !ok2 {
				t.loadSlotFromDB(addr, obj, key)
			}
		}
	}
	t.mem.SetState(addr, key, value)
}

// --- StateDB interface: account existence ---

func (t *TrieStateDB) Exist(addr types.Address) bool {
	t.loadFromDB(addr)
	return t.mem.Exist(addr)
}

func (t *TrieStateDB) Empty(addr types.Address) bool {
	t.loadFromDB(addr)
	return t.mem.Empty(addr)
}

// --- StateDB interface: snapshot/revert ---

func (t *TrieStateDB) Snapshot() int {
	return t.mem.Snapshot()
}

func (t *TrieStateDB) RevertToSnapshot(id int) {
	t.cachedRoot = types.Hash{}
	t.commitRoot = types.Hash{}
	t.mem.RevertToSnapshot(id)
}

// --- StateDB interface: logs ---

func (t *TrieStateDB) AddLog(log *types.Log) {
	t.mem.AddLog(log)
}

func (t *TrieStateDB) GetLogs(txHash types.Hash) []*types.Log {
	return t.mem.GetLogs(txHash)
}

func (t *TrieStateDB) SetTxContext(txHash types.Hash, txIndex int) {
	t.mem.SetTxContext(txHash, txIndex)
}

func (t *TrieStateDB) FinalizePreState() { t.mem.FinalizePreState() }

// --- StateDB interface: refund counter ---

func (t *TrieStateDB) AddRefund(gas uint64) {
	t.mem.AddRefund(gas)
}

func (t *TrieStateDB) SubRefund(gas uint64) {
	t.mem.SubRefund(gas)
}

func (t *TrieStateDB) GetRefund() uint64 {
	return t.mem.GetRefund()
}

// --- StateDB interface: access list (EIP-2929) ---

func (t *TrieStateDB) AddAddressToAccessList(addr types.Address) {
	t.mem.AddAddressToAccessList(addr)
}

func (t *TrieStateDB) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	t.mem.AddSlotToAccessList(addr, slot)
}

func (t *TrieStateDB) AddressInAccessList(addr types.Address) bool {
	return t.mem.AddressInAccessList(addr)
}

func (t *TrieStateDB) SlotInAccessList(addr types.Address, slot types.Hash) (bool, bool) {
	return t.mem.SlotInAccessList(addr, slot)
}

func (t *TrieStateDB) ClearAccessList() {
	t.mem.ClearAccessList()
}

// --- StateDB interface: transient storage (EIP-1153) ---

func (t *TrieStateDB) GetTransientState(addr types.Address, key types.Hash) types.Hash {
	return t.mem.GetTransientState(addr, key)
}

func (t *TrieStateDB) SetTransientState(addr types.Address, key types.Hash, value types.Hash) {
	t.mem.SetTransientState(addr, key, value)
}

func (t *TrieStateDB) ClearTransientStorage() {
	t.mem.ClearTransientStorage()
}

// --- StateDB interface: root computation ---

func (t *TrieStateDB) StorageRoot(addr types.Address) types.Hash {
	t.loadFromDB(addr)
	obj := t.mem.stateObjects[addr]
	if obj == nil || obj.deleted {
		return types.EmptyRootHash
	}
	return t.computeStorageRootFromDB(addr, obj)
}

// GetRoot computes the full state root by merging DB-persisted accounts with
// the dirty mem buffer.  When the dirty buffer is empty (i.e. right after
// Commit), the cached commitRoot is returned directly without rebuilding the
// trie — this is the common case for read-only callers like eth_call.
func (t *TrieStateDB) GetRoot() types.Hash {
	if len(t.mem.stateObjects) == 0 && t.commitRoot != (types.Hash{}) {
		return t.commitRoot
	}
	root := t.buildStateTrie().Hash()
	// Cache the result so the next Commit() call can skip rebuilding the trie.
	t.cachedRoot = root
	return root
}

// buildStateTrie constructs the full MPT over all accounts: dirty mem accounts
// override (or delete) DB accounts. DB accounts not in mem are included as-is
// using their stored RLP (which embeds the storage root from last Commit).
//
// Fast path: when committedTrie is available (after the first Commit), clones
// it and applies only the dirty accounts — O(N_nodes + N_dirty) instead of
// O(N_total) iteration over frozenAccounts.
func (t *TrieStateDB) buildStateTrie() *trie.Trie {
	// --- Fast path: incremental update on committed trie -------------------
	if t.committedTrie != nil {
		stateTrie := t.committedTrie.Clone()
		for addr, obj := range t.mem.stateObjects {
			key := crypto.Keccak256(addr[:])
			if obj.selfDestructed {
				stateTrie.Delete(key) //nolint:errcheck
				continue
			}
			var storageRoot types.Hash
			if len(obj.committedStorage) == 0 && len(obj.dirtyStorage) == 0 && obj.dbStorageRoot != (types.Hash{}) {
				storageRoot = obj.dbStorageRoot
			} else {
				storageRoot = t.computeStorageRootFromDB(addr, obj)
			}
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
			stateTrie.Put(key, encoded) //nolint:errcheck
		}
		return stateTrie
	}

	// --- Slow path: full rebuild from frozenAccounts / DB ------------------
	stateTrie := trie.New()

	// Build a set of dirty addresses to detect overrides.
	memAddrs := make(map[types.Address]bool, len(t.mem.stateObjects))
	for addr := range t.mem.stateObjects {
		memAddrs[addr] = true
	}

	// Insert dirty mem accounts (may override or logically delete DB entries).
	for addr, obj := range t.mem.stateObjects {
		if obj.selfDestructed {
			continue // logically deleted; skip
		}
		// Fast path: if no storage was modified this block AND the account was
		// loaded from DB (dbStorageRoot != zero), the storage root is unchanged.
		// New accounts (dbStorageRoot == zero) must go through computeStorageRootFromDB
		// so that an empty trie returns EmptyRootHash, not the zero hash.
		var storageRoot types.Hash
		if len(obj.committedStorage) == 0 && len(obj.dirtyStorage) == 0 && obj.dbStorageRoot != (types.Hash{}) {
			storageRoot = obj.dbStorageRoot
		} else {
			storageRoot = t.computeStorageRootFromDB(addr, obj)
		}
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
		stateTrie.Put(crypto.Keccak256(addr[:]), encoded) //nolint:errcheck
	}

	// Insert committed accounts not present in the dirty buffer.
	//
	// Prefer the frozen snapshot (captured at the last Commit) over a live DB
	// scan. The frozen snapshot is a stable, read-only copy that cannot be
	// corrupted by concurrent Commit calls from other goroutines (e.g. a
	// newPayload handler committing while a block builder is computing a root).
	//
	// Fall back to a live DB scan for TrieStateDB instances that have never
	// been committed (frozenAccounts == nil), such as a freshly-constructed
	// reader or the genesis-initialisation path.
	if t.frozenAccounts != nil {
		for addr, rlpBytes := range t.frozenAccounts {
			if memAddrs[addr] {
				continue // dirty mem overrides frozen snapshot
			}
			stateTrie.Put(crypto.Keccak256(addr[:]), rlpBytes) //nolint:errcheck
		}
	} else {
		iter := t.db.NewIterator(dbPrefixAccount)
		for iter.Next() {
			key := iter.Key()
			if len(key) != 2+20 {
				continue
			}
			var addr types.Address
			copy(addr[:], key[2:])
			if memAddrs[addr] {
				continue
			}
			stateTrie.Put(crypto.Keccak256(addr[:]), iter.Value())
		}
		iter.Release()
	}

	return stateTrie
}

// --- StateDB interface: commit ---

// Commit flushes the dirty buffer to the DB, resets the buffer, and returns
// the new state root. After a successful Commit, Dup() is O(1).
func (t *TrieStateDB) Commit() (types.Hash, error) {
	// Recompute recreation from the current overlay, so a reverted CREATE
	// cannot leave a stale purge flag that erases the restored storage.
	t.recreated = nil
	for addr, obj := range t.mem.stateObjects {
		if obj.dbStorageRoot == (types.Hash{}) && !obj.selfDestructed {
			exists, err := t.db.Has(accountDBKey(addr))
			if err != nil {
				return types.Hash{}, err
			}
			if exists {
				if t.recreated == nil {
					t.recreated = make(map[types.Address]struct{})
				}
				t.recreated[addr] = struct{}{}
			}
		}
	}

	// Capture ALL dirty storage slots BEFORE the dirty→committed flush so we
	// know exactly which slots to write/delete in the DB.  We only write dirty
	// slots — never the full committedStorage — to avoid the O(N_total) write
	// amplification that caused 700%+ CPU with storagespam workloads.
	type dirtySlot struct {
		addr types.Address
		slot types.Hash
		val  types.Hash
	}
	var dirtySlots []dirtySlot
	for addr, obj := range t.mem.stateObjects {
		if obj.selfDestructed {
			continue // entire storage will be deleted via prefix scan below
		}
		for slot, val := range obj.dirtyStorage {
			dirtySlots = append(dirtySlots, dirtySlot{addr, slot, val})
		}
	}

	// Flush dirty → committed storage.
	// Unlike MemoryStateDB we keep zero values in committedStorage as explicit
	// "slot was zeroed this block" markers so computeStorageRootFromDB can
	// override the DB value with zero (i.e. exclude the slot from the trie).
	for _, obj := range t.mem.stateObjects {
		for key, val := range obj.dirtyStorage {
			obj.committedStorage[key] = val // keep zeros as explicit overrides
		}
		obj.dirtyStorage = make(map[types.Hash]types.Hash)
	}

	// Compute state root over the full merged state (DB + in-memory overlay).
	// Reuse the root cached by GetRoot() if available (avoids a redundant full
	// trie rebuild when insertBlock calls GetRoot() then Commit() in sequence).
	var root types.Hash
	if t.cachedRoot != (types.Hash{}) {
		root = t.cachedRoot
		t.cachedRoot = types.Hash{} // consume the cache
	} else {
		root = t.buildStateTrie().Hash()
	}

	// Persist dirty buffer to DB atomically.
	// Also collect encoded account bytes for the incremental frozenAccounts update
	// below, avoiding a full DB scan.
	type encodedAccount struct {
		addr    types.Address
		encoded []byte
	}
	var encodedAccounts []encodedAccount
	var destroyedAddrs []types.Address

	batch := t.db.NewBatch()
	for addr, obj := range t.mem.stateObjects {
		if obj.selfDestructed {
			// Remove account and all its storage from the DB.
			if err := batch.Delete(accountDBKey(addr)); err != nil {
				return types.Hash{}, fmt.Errorf("delete account %s: %w", addr.Hex(), err)
			}
			iter := t.db.NewIterator(storageDBPrefix(addr))
			for iter.Next() {
				if err := batch.Delete(iter.Key()); err != nil {
					iter.Release()
					return types.Hash{}, fmt.Errorf("delete storage %s: %w", addr.Hex(), err)
				}
			}
			iter.Release()
			destroyedAddrs = append(destroyedAddrs, addr)
			continue
		}

		// Write account record (includes baked-in storage root for fast reload).
		// Use the fast path when no storage was modified this block AND the account
		// was loaded from DB (dbStorageRoot != zero).  dirtyStorage is always empty
		// here (flushed to committedStorage above).
		var storageRoot types.Hash
		if len(obj.committedStorage) == 0 && obj.dbStorageRoot != (types.Hash{}) {
			storageRoot = obj.dbStorageRoot
		} else {
			storageRoot = t.computeStorageRootFromDB(addr, obj)
		}
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
			return types.Hash{}, fmt.Errorf("encode account %s: %w", addr.Hex(), err)
		}
		if err := batch.Put(accountDBKey(addr), encoded); err != nil {
			return types.Hash{}, fmt.Errorf("put account %s: %w", addr.Hex(), err)
		}
		encodedAccounts = append(encodedAccounts, encodedAccount{addr, encoded})

		// Write contract code (idempotent: code is immutable once deployed).
		if len(obj.code) > 0 {
			if err := batch.Put(codeDBKey(obj.account.CodeHash), obj.code); err != nil {
				return types.Hash{}, fmt.Errorf("put code %s: %w", addr.Hex(), err)
			}
		}
	}

	// Write only the dirty storage slots captured before the flush.
	// Zero-value slots are deleted from DB; non-zero are upserted.
	for _, ds := range dirtySlots {
		if ds.val == (types.Hash{}) {
			if err := batch.Delete(storageDBKey(ds.addr, ds.slot)); err != nil {
				return types.Hash{}, fmt.Errorf("delete slot %s[%s]: %w", ds.addr.Hex(), ds.slot.Hex(), err)
			}
		} else {
			if err := batch.Put(storageDBKey(ds.addr, ds.slot), ds.val[:]); err != nil {
				return types.Hash{}, fmt.Errorf("put slot %s[%s]: %w", ds.addr.Hex(), ds.slot.Hex(), err)
			}
		}
	}

	// For accounts reset by CreateAccount, purge ALL stale DB storage.
	// The new account's dirty slots were captured above and will be written;
	// any remaining DB slots belong to the old incarnation and must be removed.
	for addr := range t.recreated {
		obj, ok := t.mem.stateObjects[addr]
		if !ok || obj.selfDestructed {
			continue // self-destructed: already handled via prefix delete above
		}
		iter := t.db.NewIterator(storageDBPrefix(addr))
		for iter.Next() {
			if err := batch.Delete(iter.Key()); err != nil {
				iter.Release()
				return types.Hash{}, fmt.Errorf("delete stale slot %s: %w", addr.Hex(), err)
			}
		}
		iter.Release()
	}

	if err := batch.Write(); err != nil {
		return types.Hash{}, fmt.Errorf("flush state to db: %w", err)
	}

	// Update persistedStorage with COW semantics so Dup() copies sharing the
	// old map are unaffected.  The inner maps for dirty addresses are deep-copied
	// before modification; all other inner maps are shared read-only.
	//
	// Ordering: this runs AFTER buildStateTrie() so computeStorageRootFromDB
	// (called from buildStateTrie) sees the pre-commit persistedStorage (slots
	// from previous blocks only).  The updated map is used starting next block.
	{
		// Collect which addresses have dirty slots or need purging.
		dirtyAddrs := make(map[types.Address]bool, len(t.mem.stateObjects))
		for _, ds := range dirtySlots {
			dirtyAddrs[ds.addr] = true
		}

		// Build new top-level map; share inner maps for unmodified addresses.
		newPS := make(map[types.Address]map[types.Hash]types.Hash, len(t.persistedStorage)+len(dirtyAddrs))
		for addr, slots := range t.persistedStorage {
			newPS[addr] = slots // shared inner map (read-only from parent Dup)
		}

		// For dirty addresses: create an exclusive inner map (copy old, apply updates).
		for addr := range dirtyAddrs {
			_, isRecreated := t.recreated[addr]
			old := t.persistedStorage[addr]
			var fresh map[types.Hash]types.Hash
			if isRecreated {
				// Discard stale slots from the old incarnation; start fresh.
				fresh = make(map[types.Hash]types.Hash)
			} else {
				fresh = make(map[types.Hash]types.Hash, len(old)+4)
				for k, v := range old {
					fresh[k] = v
				}
			}
			newPS[addr] = fresh
		}

		// Apply dirty slot updates.
		for _, ds := range dirtySlots {
			if ds.val == (types.Hash{}) {
				delete(newPS[ds.addr], ds.slot)
			} else {
				newPS[ds.addr][ds.slot] = ds.val
			}
		}

		// Remove self-destructed accounts entirely.
		for addr, obj := range t.mem.stateObjects {
			if obj.selfDestructed {
				delete(newPS, addr)
			}
		}

		// Recreated accounts with NO dirty slots: clear stale persisted storage.
		for addr := range t.recreated {
			if !dirtyAddrs[addr] {
				delete(newPS, addr)
			}
		}

		t.persistedStorage = newPS
	}

	// Update the frozen account snapshot incrementally.
	//
	// Previously this did a full DB scan (O(N_total_entries)) after every Commit,
	// which was O(N²) with storagespam workloads because storage entries dominate.
	//
	// Now we build the new snapshot from the previous frozen map plus only the
	// accounts touched this block — O(N_total_accounts + N_dirty), both tiny.
	//
	// First Commit (frozenAccounts == nil): must scan the DB because there is no
	// prior snapshot.  After genesis the DB has very few entries so this is cheap.
	if t.frozenAccounts == nil {
		frozen := make(map[types.Address][]byte)
		frozenIter := t.db.NewIterator(dbPrefixAccount)
		for frozenIter.Next() {
			key := frozenIter.Key()
			if len(key) != 2+20 {
				continue
			}
			var addr types.Address
			copy(addr[:], key[2:])
			val := make([]byte, len(frozenIter.Value()))
			copy(val, frozenIter.Value())
			frozen[addr] = val
		}
		frozenIter.Release()
		t.frozenAccounts = frozen
	} else {
		// Incremental COW update: copy the top-level map (shallow) then apply
		// this block's account additions and deletions.
		frozen := make(map[types.Address][]byte, len(t.frozenAccounts)+len(encodedAccounts))
		for addr, v := range t.frozenAccounts {
			frozen[addr] = v // byte slices are immutable; sharing is safe
		}
		for _, ea := range encodedAccounts {
			frozen[ea.addr] = ea.encoded
		}
		for _, addr := range destroyedAddrs {
			delete(frozen, addr)
		}
		t.frozenAccounts = frozen
	}

	// Maintain the committedTrie incrementally so future buildStateTrie() calls
	// can Clone it + apply dirty accounts rather than iterating all frozenAccounts.
	if t.committedTrie == nil {
		// First Commit: rebuild from frozenAccounts (genesis, small).
		ct := trie.New()
		for addr, rlpBytes := range t.frozenAccounts {
			ct.Put(crypto.Keccak256(addr[:]), rlpBytes) //nolint:errcheck
		}
		t.committedTrie = ct
	} else {
		// Subsequent commits: apply only the dirty accounts to the existing trie.
		for _, ea := range encodedAccounts {
			t.committedTrie.Put(crypto.Keccak256(ea.addr[:]), ea.encoded) //nolint:errcheck
		}
		for _, addr := range destroyedAddrs {
			t.committedTrie.Delete(crypto.Keccak256(addr[:])) //nolint:errcheck
		}
	}

	// Reset dirty buffer and recreated tracking: memory drops to near-zero.
	t.mem = NewMemoryStateDB()
	t.recreated = nil

	// Cache the root so repeated GetRoot() calls after this Commit are O(1).
	t.commitRoot = root

	return root, nil
}

// ClearAllState deletes every account, storage-slot, and code entry from the
// underlying DB. Call this before committing a full canonical snapshot (e.g.
// during a reorg) so that stale entries from reverted blocks are removed.
func (t *TrieStateDB) ClearAllState() error {
	prefixes := [][]byte{dbPrefixAccount, dbPrefixStorage, dbPrefixCode}
	for _, pfx := range prefixes {
		iter := t.db.NewIterator(pfx)
		var keys [][]byte
		for iter.Next() {
			k := make([]byte, len(iter.Key()))
			copy(k, iter.Key())
			keys = append(keys, k)
		}
		iter.Release()
		for _, k := range keys {
			if err := t.db.Delete(k); err != nil {
				return fmt.Errorf("clear state key: %w", err)
			}
		}
	}
	return nil
}

// --- StateDB interface: copy ---

// Dup returns an independent copy of this TrieStateDB. Both copies share the
// same DB reference (reads/writes are serialized through the DB's own lock).
// After Commit(), mem is nearly empty so this is cheap.
func (t *TrieStateDB) Dup() StateDB {
	var recreatedCopy map[types.Address]struct{}
	if len(t.recreated) > 0 {
		recreatedCopy = make(map[types.Address]struct{}, len(t.recreated))
		for k := range t.recreated {
			recreatedCopy[k] = struct{}{}
		}
	}
	return &TrieStateDB{
		mem:              t.mem.Copy(),
		db:               t.db,
		gcMode:           t.gcMode,
		recreated:        recreatedCopy,
		frozenAccounts:   t.frozenAccounts,   // shared read-only; never mutated by callers
		persistedStorage: t.persistedStorage, // shared read-only; COW in Commit()
		commitRoot:       t.commitRoot,       // inherited; cleared when dirty buffer grows
		committedTrie:    t.committedTrie,    // shared read-only; Clone()d before any mutation
	}
}

// Verify interface compliance at compile time.
var _ StateDB = (*TrieStateDB)(nil)

func (t *TrieStateDB) FinalizeTransaction() { t.mem.FinalizeTransaction() }
