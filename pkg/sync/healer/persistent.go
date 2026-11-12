package healer

import (
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/trie/mpt"
)

// TrieStore authenticates healed nodes against parent references reachable
// from the pivot root. Peer bytes cannot choose their own storage key.
type TrieStore struct {
	db       rawdb.Database
	mu       sync.Mutex
	expected map[string]types.Hash
	err      error
}

func NewTrieStore(db rawdb.Database) *TrieStore {
	return &TrieStore{db: db, expected: make(map[string]types.Hash)}
}
func (s *TrieStore) MissingTrieNodes(root types.Hash, limit int) [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	expected, err := mpt.MissingNodes(root, mpt.NewRawDBNodeReader(s.db.Get), limit)
	s.err = err
	if err != nil {
		return [][]byte{{0xff}}
	}
	paths := make([][]byte, 0, len(expected))
	for path, hash := range expected {
		s.expected[path] = hash
		paths = append(paths, []byte(path))
	}
	return paths
}
func (s *TrieStore) WriteTrieNode(path, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	hash, ok := s.expected[string(path)]
	if !ok || crypto.Keccak256Hash(data) != hash {
		return fmt.Errorf("state healer: node does not match authenticated path")
	}
	return s.db.Put(append([]byte("t"), hash[:]...), data)
}
func (s *TrieStore) HasTrieNode(path []byte) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	hash, ok := s.expected[string(path)]
	if !ok {
		return false
	}
	exists, _ := s.db.Has(append([]byte("t"), hash[:]...))
	return exists
}
func (s *TrieStore) HasBytecode(hash types.Hash) bool {
	exists, _ := s.db.Has(append([]byte("snap-code/"), hash[:]...))
	return exists
}
func (s *TrieStore) WriteBytecode(hash types.Hash, code []byte) error {
	if crypto.Keccak256Hash(code) != hash {
		return fmt.Errorf("state healer: code hash mismatch")
	}
	return s.db.Put(append([]byte("snap-code/"), hash[:]...), code)
}
func (*TrieStore) WriteAccount(types.Hash, AccountData) error {
	return fmt.Errorf("state healer: account ranges belong to the snapshot downloader")
}
func (*TrieStore) WriteStorage(types.Hash, types.Hash, []byte) error {
	return fmt.Errorf("state healer: storage ranges belong to the snapshot downloader")
}
