package state

import (
	"bytes"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/trie/mpt"
)

// StateSnapshot uses secure trie keys. Snap peers do not supply account or
// storage preimages; execution resolves them by hashing the address/slot.
type StateSnapshot struct {
	Accounts map[types.Hash][]byte
	Storage  map[types.Hash]map[types.Hash][]byte
	Codes    map[types.Hash][]byte
}

func NewStateSnapshot() *StateSnapshot {
	return &StateSnapshot{make(map[types.Hash][]byte), make(map[types.Hash]map[types.Hash][]byte), make(map[types.Hash][]byte)}
}

func snapshotTrie(entries map[types.Hash][]byte) *mpt.Trie {
	tr := mpt.New()
	for key, value := range entries {
		_ = tr.Put(key[:], value)
	}
	return tr
}

func (s *StateSnapshot) Root() types.Hash { return snapshotTrie(s.Accounts).Hash() }

// Validate verifies completeness, not just inclusion of individual leaves.
func (s *StateSnapshot) Validate(root types.Hash) error {
	if s == nil {
		return fmt.Errorf("snapshot: nil snapshot")
	}
	if s.Root() != root {
		return fmt.Errorf("snapshot: account root mismatch")
	}
	for hash, body := range s.Accounts {
		var acc rlpAccount
		if err := rlp.DecodeBytes(body, &acc); err != nil {
			return err
		}
		if len(acc.Root) != 32 || len(acc.CodeHash) != 32 || acc.Balance == nil || acc.Balance.Sign() < 0 || acc.Balance.BitLen() > 256 {
			return fmt.Errorf("snapshot: invalid account %s", hash)
		}
		if snapshotTrie(s.Storage[hash]).Hash() != types.BytesToHash(acc.Root) {
			return fmt.Errorf("snapshot: storage root mismatch for %s", hash)
		}
		for _, value := range s.Storage[hash] {
			var decoded []byte
			if err := rlp.DecodeBytes(value, &decoded); err != nil {
				return err
			}
			if len(decoded) == 0 || len(decoded) > 32 || decoded[0] == 0 {
				return fmt.Errorf("snapshot: noncanonical storage value")
			}
		}
		codeHash := types.BytesToHash(acc.CodeHash)
		if codeHash != types.EmptyCodeHash && crypto.Keccak256Hash(s.Codes[codeHash]) != codeHash {
			return fmt.Errorf("snapshot: missing or invalid code %s", codeHash)
		}
	}
	for account, slots := range s.Storage {
		if _, ok := s.Accounts[account]; !ok && len(slots) > 0 {
			return fmt.Errorf("snapshot: storage without account")
		}
	}
	for hash, code := range s.Codes {
		if crypto.Keccak256Hash(code) != hash {
			return fmt.Errorf("snapshot: invalid bytecode entry")
		}
	}
	return nil
}

func (s *StateSnapshot) copy() *StateSnapshot {
	cp := NewStateSnapshot()
	for k, v := range s.Accounts {
		cp.Accounts[k] = bytes.Clone(v)
	}
	for a, slots := range s.Storage {
		cp.Storage[a] = make(map[types.Hash][]byte, len(slots))
		for k, v := range slots {
			cp.Storage[a][k] = bytes.Clone(v)
		}
	}
	for k, v := range s.Codes {
		cp.Codes[k] = bytes.Clone(v)
	}
	return cp
}

func snapshotPrefix(root types.Hash) []byte { return append([]byte("snap-state/"), root[:]...) }

// StoreStateSnapshot publishes a root only after all leaves, storage and code
// validate. An interrupted write has no completion marker and is not adoptable.
func StoreStateSnapshot(db rawdb.Database, root types.Hash, s *StateSnapshot) error {
	if err := s.Validate(root); err != nil {
		return err
	}
	prefix := snapshotPrefix(root)
	batch := db.NewBatch()
	for k, v := range s.Accounts {
		key := append(append(bytes.Clone(prefix), 'a'), k[:]...)
		if err := batch.Put(key, v); err != nil {
			return err
		}
	}
	for a, slots := range s.Storage {
		for k, v := range slots {
			key := append(append(append(bytes.Clone(prefix), 's'), a[:]...), k[:]...)
			if err := batch.Put(key, v); err != nil {
				return err
			}
		}
	}
	for k, v := range s.Codes {
		if err := batch.Put(append([]byte("snap-code/"), k[:]...), v); err != nil {
			return err
		}
	}
	if err := batch.Write(); err != nil {
		return err
	}
	return db.Put(append(prefix, 'v'), []byte{1})
}

func LoadStateSnapshot(db rawdb.Database, root types.Hash) (*StateSnapshot, error) {
	prefix := snapshotPrefix(root)
	marker, err := db.Get(append(bytes.Clone(prefix), 'v'))
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(marker, []byte{1}) {
		return nil, fmt.Errorf("snapshot: invalid completion marker")
	}
	s := NewStateSnapshot()
	iter := db.NewIterator(prefix)
	defer iter.Release()
	for iter.Next() {
		key := iter.Key()[len(prefix):]
		switch {
		case len(key) == 33 && key[0] == 'a':
			s.Accounts[types.BytesToHash(key[1:])] = bytes.Clone(iter.Value())
		case len(key) == 65 && key[0] == 's':
			a, k := types.BytesToHash(key[1:33]), types.BytesToHash(key[33:])
			if s.Storage[a] == nil {
				s.Storage[a] = make(map[types.Hash][]byte)
			}
			s.Storage[a][k] = bytes.Clone(iter.Value())
		}
	}
	for _, body := range s.Accounts {
		var acc rlpAccount
		if err := rlp.DecodeBytes(body, &acc); err != nil {
			return nil, err
		}
		hash := types.BytesToHash(acc.CodeHash)
		if hash != types.EmptyCodeHash {
			code, err := db.Get(append([]byte("snap-code/"), hash[:]...))
			if err != nil {
				return nil, err
			}
			s.Codes[hash] = code
		}
	}
	if err := s.Validate(root); err != nil {
		return nil, err
	}
	return s, nil
}

// SnapStateDB executes on a verified hashed snapshot with a journalled native
// dirty overlay. Snapshot data is immutable and shared across Dup copies.
type SnapStateDB struct {
	memory *MemoryStateDB
	base   *StateSnapshot
	db     rawdb.Database
}

func OpenSnapState(db rawdb.Database, root types.Hash) (*SnapStateDB, error) {
	snap, err := LoadStateSnapshot(db, root)
	if err != nil {
		return nil, err
	}
	return &SnapStateDB{NewMemoryStateDB(), snap, db}, nil
}

func (s *SnapStateDB) load(addr types.Address) {
	if s.memory.stateObjects[addr] != nil {
		return
	}
	body := s.base.Accounts[crypto.Keccak256Hash(addr[:])]
	if body == nil {
		return
	}
	var acc rlpAccount
	if err := rlp.DecodeBytes(body, &acc); err != nil {
		panic("verified snapshot account became invalid")
	}
	obj := newStateObject()
	obj.account.Nonce, obj.account.Balance, obj.account.CodeHash = acc.Nonce, new(big.Int).Set(acc.Balance), bytes.Clone(acc.CodeHash)
	obj.dbStorageRoot = types.BytesToHash(acc.Root)
	obj.code = bytes.Clone(s.base.Codes[types.BytesToHash(acc.CodeHash)])
	s.memory.stateObjects[addr] = obj
}

func (s *SnapStateDB) loadSlot(addr types.Address, key types.Hash) {
	s.load(addr)
	obj := s.memory.stateObjects[addr]
	if obj == nil {
		return
	}
	if _, ok := obj.committedStorage[key]; ok {
		return
	}
	// A newly created account must not inherit the old account's storage.
	if obj.dbStorageRoot == (types.Hash{}) {
		return
	}
	enc := s.base.Storage[crypto.Keccak256Hash(addr[:])][crypto.Keccak256Hash(key[:])]
	var value []byte
	if len(enc) > 0 {
		_ = rlp.DecodeBytes(enc, &value)
	}
	obj.committedStorage[key] = types.BytesToHash(value)
}

func (s *SnapStateDB) GetState(a types.Address, k types.Hash) types.Hash {
	s.loadSlot(a, k)
	return s.memory.GetState(a, k)
}
func (s *SnapStateDB) GetCommittedState(a types.Address, k types.Hash) types.Hash {
	s.loadSlot(a, k)
	return s.memory.GetCommittedState(a, k)
}
func (s *SnapStateDB) SetState(a types.Address, k, v types.Hash) {
	s.loadSlot(a, k)
	s.memory.SetState(a, k, v)
}

func (s *SnapStateDB) snapshot() *StateSnapshot {
	out := s.base.copy()
	applySnapshotOverlay(out, s.memory)
	return out
}

func applySnapshotOverlay(out *StateSnapshot, mem *MemoryStateDB) {
	for addr, obj := range mem.stateObjects {
		hash := crypto.Keccak256Hash(addr[:])
		if obj.selfDestructed {
			delete(out.Accounts, hash)
			delete(out.Storage, hash)
			continue
		}
		slots := out.Storage[hash]
		if slots == nil || obj.dbStorageRoot == (types.Hash{}) {
			slots = make(map[types.Hash][]byte)
			out.Storage[hash] = slots
		}
		for _, overlay := range []map[types.Hash]types.Hash{obj.committedStorage, obj.dirtyStorage} {
			for key, val := range overlay {
				hashed := crypto.Keccak256Hash(key[:])
				if val == (types.Hash{}) {
					delete(slots, hashed)
				} else {
					slots[hashed], _ = rlp.EncodeToBytes(bytes.TrimLeft(val[:], "\x00"))
				}
			}
		}
		root := snapshotTrie(slots).Hash()
		codeHash := types.BytesToHash(obj.account.CodeHash)
		if len(obj.account.CodeHash) == 0 {
			codeHash = types.EmptyCodeHash
		}
		if codeHash != types.EmptyCodeHash {
			out.Codes[codeHash] = bytes.Clone(obj.code)
		}
		out.Accounts[hash], _ = rlp.EncodeToBytes(rlpAccount{Nonce: obj.account.Nonce, Balance: obj.account.Balance, Root: root[:], CodeHash: codeHash[:]})
	}
}

func (s *SnapStateDB) GetRoot() types.Hash { return s.snapshot().Root() }
func (s *SnapStateDB) StorageRoot(addr types.Address) types.Hash {
	return snapshotTrie(s.snapshot().Storage[crypto.Keccak256Hash(addr[:])]).Hash()
}
func (s *SnapStateDB) Commit() (types.Hash, error) {
	snap := s.snapshot()
	root := snap.Root()
	if err := StoreStateSnapshot(s.db, root, snap); err != nil {
		return types.Hash{}, err
	}
	s.base = snap
	s.memory = NewMemoryStateDB()
	return root, nil
}
func (s *SnapStateDB) Dup() StateDB { return &SnapStateDB{s.memory.Copy(), s.base, s.db} }

func (s *SnapStateDB) CreateAccount(addr types.Address) {
	s.load(addr)
	s.memory.CreateAccount(addr)
}

func (s *SnapStateDB) SubBalance(addr types.Address, amount *big.Int) {
	s.load(addr)
	s.memory.SubBalance(addr, amount)
}

func (s *SnapStateDB) AddBalance(addr types.Address, amount *big.Int) {
	s.load(addr)
	s.memory.AddBalance(addr, amount)
}

func (s *SnapStateDB) GetBalance(addr types.Address) *big.Int {
	s.load(addr)
	return s.memory.GetBalance(addr)
}

func (s *SnapStateDB) GetNonce(addr types.Address) uint64 {
	s.load(addr)
	return s.memory.GetNonce(addr)
}

func (s *SnapStateDB) SetNonce(addr types.Address, nonce uint64) {
	s.load(addr)
	s.memory.SetNonce(addr, nonce)
}

func (s *SnapStateDB) GetCode(addr types.Address) []byte {
	s.load(addr)
	return bytes.Clone(s.memory.GetCode(addr))
}

func (s *SnapStateDB) SetCode(addr types.Address, code []byte) {
	s.load(addr)
	s.memory.SetCode(addr, code)
}

func (s *SnapStateDB) GetCodeHash(addr types.Address) types.Hash {
	s.load(addr)
	return s.memory.GetCodeHash(addr)
}

func (s *SnapStateDB) GetCodeSize(addr types.Address) int {
	s.load(addr)
	return s.memory.GetCodeSize(addr)
}

func (s *SnapStateDB) SelfDestruct(addr types.Address) {
	s.load(addr)
	s.memory.SelfDestruct(addr)
}

func (s *SnapStateDB) HasSelfDestructed(addr types.Address) bool {
	s.load(addr)
	return s.memory.HasSelfDestructed(addr)
}

func (s *SnapStateDB) Exist(addr types.Address) bool {
	s.load(addr)
	return s.memory.Exist(addr)
}

func (s *SnapStateDB) Empty(addr types.Address) bool {
	s.load(addr)
	return s.memory.Empty(addr)
}

var _ StateDB = (*SnapStateDB)(nil)

// ExportStateSnapshot projects a stable native state copy into secure trie
// leaves for snap serving. Callers obtain the copy through StateAtBlock.
func ExportStateSnapshot(db StateDB) (*StateSnapshot, error) {
	if s, ok := db.(*SnapStateDB); ok {
		return s.snapshot(), nil
	}
	out := NewStateSnapshot()
	switch s := db.(type) {
	case *MemoryStateDB:
		applySnapshotOverlay(out, s)
	case *TrieStateDB:
		frozen := s.frozenAccounts
		if frozen == nil {
			frozen = make(map[types.Address][]byte)
			it := s.db.NewIterator(dbPrefixAccount)
			for it.Next() {
				if len(it.Key()) == 22 {
					frozen[types.BytesToAddress(it.Key()[2:])] = bytes.Clone(it.Value())
				}
			}
			it.Release()
		}
		for addr, body := range frozen {
			ah := crypto.Keccak256Hash(addr[:])
			out.Accounts[ah] = bytes.Clone(body)
			slots := make(map[types.Hash][]byte)
			out.Storage[ah] = slots
			if s.persistedStorage != nil {
				for key, val := range s.persistedStorage[addr] {
					if val != (types.Hash{}) {
						slots[crypto.Keccak256Hash(key[:])], _ = rlp.EncodeToBytes(bytes.TrimLeft(val[:], "\x00"))
					}
				}
			} else {
				it := s.db.NewIterator(storageDBPrefix(addr))
				for it.Next() {
					if len(it.Key()) == 54 {
						slots[crypto.Keccak256Hash(it.Key()[22:])], _ = rlp.EncodeToBytes(bytes.TrimLeft(it.Value(), "\x00"))
					}
				}
				it.Release()
			}
			var acc rlpAccount
			if err := rlp.DecodeBytes(body, &acc); err != nil {
				return nil, err
			}
			hash := types.BytesToHash(acc.CodeHash)
			if hash != types.EmptyCodeHash {
				code, err := s.db.Get(codeDBKey(acc.CodeHash))
				if err != nil {
					return nil, err
				}
				out.Codes[hash] = code
			}
		}
		applySnapshotOverlay(out, s.mem)
	default:
		return nil, fmt.Errorf("snapshot: unsupported state %T", db)
	}
	if err := out.Validate(db.GetRoot()); err != nil {
		return nil, err
	}
	return out, nil
}

// AccountProof includes absence proofs for range origins without an account.
func (s *StateSnapshot) AccountProof(key types.Hash) ([][]byte, error) {
	return snapshotTrie(s.Accounts).ProveAbsence(key[:])
}
func (s *StateSnapshot) StorageProof(account, key types.Hash) ([][]byte, error) {
	return snapshotTrie(s.Storage[account]).ProveAbsence(key[:])
}

func (s *SnapStateDB) Snapshot() int { return s.memory.Snapshot() }

func (s *SnapStateDB) RevertToSnapshot(id int) { s.memory.RevertToSnapshot(id) }

func (s *SnapStateDB) AddLog(log *types.Log) { s.memory.AddLog(log) }

func (s *SnapStateDB) GetLogs(hash types.Hash) []*types.Log { return s.memory.GetLogs(hash) }

func (s *SnapStateDB) SetTxContext(hash types.Hash, index int) { s.memory.SetTxContext(hash, index) }

func (s *SnapStateDB) AddRefund(gas uint64) { s.memory.AddRefund(gas) }

func (s *SnapStateDB) SubRefund(gas uint64) { s.memory.SubRefund(gas) }

func (s *SnapStateDB) GetRefund() uint64 { return s.memory.GetRefund() }

func (s *SnapStateDB) AddAddressToAccessList(addr types.Address) {
	s.memory.AddAddressToAccessList(addr)
}

func (s *SnapStateDB) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	s.memory.AddSlotToAccessList(addr, slot)
}

func (s *SnapStateDB) AddressInAccessList(addr types.Address) bool {
	return s.memory.AddressInAccessList(addr)
}

func (s *SnapStateDB) SlotInAccessList(addr types.Address, slot types.Hash) (bool, bool) {
	return s.memory.SlotInAccessList(addr, slot)
}

func (s *SnapStateDB) ClearAccessList() { s.memory.ClearAccessList() }

func (s *SnapStateDB) GetTransientState(addr types.Address, key types.Hash) types.Hash {
	return s.memory.GetTransientState(addr, key)
}

func (s *SnapStateDB) SetTransientState(addr types.Address, key, value types.Hash) {
	s.memory.SetTransientState(addr, key, value)
}

func (s *SnapStateDB) ClearTransientStorage() { s.memory.ClearTransientStorage() }

func (s *SnapStateDB) FinalizePreState() { s.memory.FinalizePreState() }

func (s *SnapStateDB) FinalizeTransaction() { s.memory.FinalizeTransaction() }
