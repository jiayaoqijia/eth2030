package state

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/trie/bintrie"
)

// BinaryTrieBackedStateDB wraps a MemoryStateDB and adds binary trie-backed
// state root computation via EIP-7864. It delegates all state operations to
// the embedded MemoryStateDB and overrides root computation and commit to
// build a real binary Merkle trie from account and storage data.
type BinaryTrieBackedStateDB struct {
	*MemoryStateDB
	trie    *bintrie.BinaryTrie
	nodeDB  map[types.Hash][]byte // in-memory node store for persistence
	lastRoot types.Hash           // root after last Commit
}

// NewBinaryTrieBackedStateDB creates a new BinaryTrieBackedStateDB with a
// fresh empty binary trie and in-memory state.
func NewBinaryTrieBackedStateDB() *BinaryTrieBackedStateDB {
	return &BinaryTrieBackedStateDB{
		MemoryStateDB: NewMemoryStateDB(),
		trie:          bintrie.New(),
		nodeDB:        make(map[types.Hash][]byte),
	}
}

// IntermediateRoot computes the current state root by flushing all accounts
// and storage into the binary trie and returning its hash. The trie is
// rebuilt from scratch each time to ensure correctness.
func (s *BinaryTrieBackedStateDB) IntermediateRoot(deleteEmpty bool) types.Hash {
	if deleteEmpty {
		s.deleteEmptyBinaryAccounts()
	}

	hasLive := false
	for _, obj := range s.stateObjects {
		if !obj.selfDestructed {
			hasLive = true
			break
		}
	}
	if !hasLive {
		return s.trie.Hash()
	}

	t := bintrie.New()
	for addr, obj := range s.stateObjects {
		if obj.selfDestructed {
			continue
		}
		s.flushAccountToTrie(t, addr, obj)
	}
	s.trie = t
	return t.Hash()
}

// GetRoot returns the current state root using the binary trie, equivalent
// to IntermediateRoot(false).
func (s *BinaryTrieBackedStateDB) GetRoot() types.Hash {
	return s.IntermediateRoot(false)
}

// Commit flushes dirty storage to committed storage, rebuilds the binary
// trie with all account and storage data, persists trie nodes to the
// in-memory node store, and returns the post-commit root hash.
func (s *BinaryTrieBackedStateDB) Commit() (types.Hash, error) {
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

	// Rebuild trie from committed state.
	t := bintrie.New()
	for addr, obj := range s.stateObjects {
		if obj.selfDestructed {
			continue
		}
		s.flushAccountToTrie(t, addr, obj)
	}
	s.trie = t

	// Collect and persist trie nodes.
	root := t.Hash()
	t.Root().CollectNodes(nil, func(path []byte, node bintrie.BinaryNode) {
		serialized := bintrie.SerializeNode(node)
		h := node.Hash()
		s.nodeDB[h] = make([]byte, len(serialized))
		copy(s.nodeDB[h], serialized)
	})
	s.lastRoot = root

	return root, nil
}

// StorageRoot computes the storage trie root for a given address using the
// binary trie. Returns the empty trie hash if the account doesn't exist.
func (s *BinaryTrieBackedStateDB) StorageRoot(addr types.Address) types.Hash {
	obj := s.stateObjects[addr]
	if obj == nil {
		return s.emptyBinaryTrieHash()
	}

	// Build a temporary trie with just this account's storage.
	t := bintrie.New()
	merged := mergeStorage(obj)
	for slot, val := range merged {
		if val == (types.Hash{}) {
			continue
		}
		_ = t.UpdateStorage(addr, slot[:], val[:])
	}
	return t.Hash()
}

// Copy returns a deep copy of the BinaryTrieBackedStateDB.
func (s *BinaryTrieBackedStateDB) Copy() *BinaryTrieBackedStateDB {
	cp := &BinaryTrieBackedStateDB{
		MemoryStateDB: s.MemoryStateDB.Copy(),
		trie:          s.trie.Copy(),
		nodeDB:        make(map[types.Hash][]byte, len(s.nodeDB)),
		lastRoot:      s.lastRoot,
	}
	for k, v := range s.nodeDB {
		buf := make([]byte, len(v))
		copy(buf, v)
		cp.nodeDB[k] = buf
	}
	return cp
}

// LastCommittedRoot returns the root hash from the most recent Commit call.
func (s *BinaryTrieBackedStateDB) LastCommittedRoot() types.Hash {
	return s.lastRoot
}

// NodeDB returns the in-memory node store for inspection/persistence.
func (s *BinaryTrieBackedStateDB) NodeDB() map[types.Hash][]byte {
	return s.nodeDB
}

// flushAccountToTrie writes an account's data and storage into the binary trie.
func (s *BinaryTrieBackedStateDB) flushAccountToTrie(t *bintrie.BinaryTrie, addr types.Address, obj *stateObject) {
	bal := obj.account.Balance
	if bal == nil {
		bal = new(big.Int)
	}

	codeHash := obj.account.CodeHash
	if len(codeHash) == 0 {
		codeHash = types.EmptyCodeHash.Bytes()
	}

	acc := &types.Account{
		Nonce:    obj.account.Nonce,
		Balance:  bal,
		CodeHash: codeHash,
	}

	_ = t.UpdateAccount(addr, acc, len(obj.code))

	// Write storage slots.
	merged := mergeStorage(obj)
	for slot, val := range merged {
		if val == (types.Hash{}) {
			continue
		}
		_ = t.UpdateStorage(addr, slot[:], val[:])
	}

	// Write code if present.
	if len(obj.code) > 0 {
		_ = t.UpdateContractCode(addr, obj.code)
	}
}

// deleteEmptyBinaryAccounts removes EIP-161 empty accounts.
func (s *BinaryTrieBackedStateDB) deleteEmptyBinaryAccounts() {
	for addr, obj := range s.stateObjects {
		if obj.selfDestructed {
			continue
		}
		if isEmptyAccount(obj) {
			delete(s.stateObjects, addr)
		}
	}
}

// emptyBinaryTrieHash returns the hash of an empty binary trie.
func (s *BinaryTrieBackedStateDB) emptyBinaryTrieHash() types.Hash {
	return bintrie.New().Hash()
}

// Verify interface compliance at compile time.
var _ StateDB = (*BinaryTrieBackedStateDB)(nil)
