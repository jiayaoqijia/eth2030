// stack_trie_builder.go implements a higher-level builder on top of StackTrie
// for constructing tries from mpt.DerivableList sources (transactions, receipts,
// withdrawals). It provides batch construction, root derivation, and node
// persistence. The underlying trie currently retains nodes until finalization.
//
// The StackTrieBuilder is the recommended way to compute transaction and
// receipt trie roots during block processing.
package stack

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/trie/mpt"
)

// StackTrieBuilder wraps StackTrie with batch construction utilities,
// node persistence, and memory tracking. It supports both incremental
// (key-by-key) and batch (from mpt.DerivableList) modes.
type StackTrieBuilder struct {
	mu       sync.Mutex
	st       *StackTrie
	store    NodeWriter
	rootHash types.Hash
	built    bool
	nodeSize int // approximate total bytes of encoded nodes
	nodeNum  int // number of nodes written
}

// NewStackTrieBuilder creates a new builder. If store is nil, nodes are
// computed but not persisted (hash-only mode).
func NewStackTrieBuilder(store NodeWriter) *StackTrieBuilder {
	return &StackTrieBuilder{
		st:    NewStackTrie(store),
		store: store,
	}
}

// Add inserts a key-value pair. Keys must be inserted in strictly increasing
// lexicographic order. Returns an error if the key is out of order or the
// builder has already been finalized.
func (b *StackTrieBuilder) Add(key, value []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.built {
		return errors.New("stack_trie_builder: already finalized")
	}
	return b.st.Update(key, value)
}

// Build finalizes the trie and returns the root hash. If a NodeWriter was
// provided, all nodes are persisted. After Build, no more insertions are
// allowed.
func (b *StackTrieBuilder) Build() (types.Hash, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.built {
		return b.rootHash, nil
	}

	root, err := b.st.Commit()
	if err != nil {
		return types.Hash{}, err
	}
	b.rootHash = root
	b.built = true
	return root, nil
}

// RootHash returns the root hash if Build has been called, or the zero hash.
func (b *StackTrieBuilder) RootHash() types.Hash {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.rootHash
}

// IsBuilt returns whether Build has been called.
func (b *StackTrieBuilder) IsBuilt() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.built
}

// Count returns the number of key-value pairs inserted.
func (b *StackTrieBuilder) Count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.st.Count()
}

// Reset clears the builder for reuse with a fresh trie.
func (b *StackTrieBuilder) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.st.Reset()
	b.rootHash = types.Hash{}
	b.built = false
	b.nodeSize = 0
	b.nodeNum = 0
}

// BuildFromList constructs a trie from a mpt.DerivableList (e.g., transaction list,
// receipt list). Keys are RLP-encoded sequential indices. Returns the root hash.
func (b *StackTrieBuilder) BuildFromList(list mpt.DerivableList) (types.Hash, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.built {
		return types.Hash{}, errors.New("stack_trie_builder: already finalized")
	}

	for i := 0; i < list.Len(); i++ {
		key, err := rlp.EncodeToBytes(uint64(i))
		if err != nil {
			return types.Hash{}, err
		}
		val, err := list.EncodeIndex(i)
		if err != nil {
			return types.Hash{}, err
		}
		if len(val) == 0 {
			continue
		}
		if err := b.st.Update(key, val); err != nil {
			return types.Hash{}, err
		}
	}

	root, err := b.st.Commit()
	if err != nil {
		return types.Hash{}, err
	}
	b.rootHash = root
	b.built = true
	return root, nil
}

// BuildFromPairs constructs a trie from a sorted slice of key-value pairs.
// The pairs must already be sorted by key in ascending lexicographic order.
func (b *StackTrieBuilder) BuildFromPairs(pairs []mpt.KeyValuePair) (types.Hash, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.built {
		return types.Hash{}, errors.New("stack_trie_builder: already finalized")
	}

	for _, p := range pairs {
		if len(p.Value) == 0 {
			continue
		}
		if err := b.st.Update(p.Key, p.Value); err != nil {
			return types.Hash{}, err
		}
	}

	root, err := b.st.Commit()
	if err != nil {
		return types.Hash{}, err
	}
	b.rootHash = root
	b.built = true
	return root, nil
}

// StackTrieNodeCollector collects encoded trie nodes during StackTrie commit,
// storing them in memory for later database insertion.
type StackTrieNodeCollector struct {
	mu    sync.Mutex
	nodes map[types.Hash][]byte
	size  int
}

// NewStackTrieNodeCollector creates a new collector.
func NewStackTrieNodeCollector() *StackTrieNodeCollector {
	return &StackTrieNodeCollector{
		nodes: make(map[types.Hash][]byte),
	}
}

// Put implements NodeWriter, storing each node in memory.
func (c *StackTrieNodeCollector) Put(hash types.Hash, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	cp := make([]byte, len(data))
	copy(cp, data)
	if _, exists := c.nodes[hash]; !exists {
		c.size += len(data)
	}
	c.nodes[hash] = cp
	return nil
}

// Nodes returns a copy of the collected node map.
func (c *StackTrieNodeCollector) Nodes() map[types.Hash][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := make(map[types.Hash][]byte, len(c.nodes))
	for h, d := range c.nodes {
		cp := make([]byte, len(d))
		copy(cp, d)
		result[h] = cp
	}
	return result
}

// Count returns the number of collected nodes.
func (c *StackTrieNodeCollector) Count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.nodes)
}

// Size returns the total byte size of collected nodes.
func (c *StackTrieNodeCollector) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.size
}

// FlushTo writes all collected nodes to the target NodeDatabase.
func (c *StackTrieNodeCollector) FlushTo(db *mpt.NodeDatabase) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for h, d := range c.nodes {
		db.InsertNode(h, d)
	}
}

// Reset clears the collector.
func (c *StackTrieNodeCollector) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nodes = make(map[types.Hash][]byte)
	c.size = 0
}

// StackTrieVerifier verifies that a StackTrie produces the expected root hash
// by rebuilding from key-value pairs using the standard Trie and comparing.
type StackTrieVerifier struct {
	expected types.Hash
	pairs    []mpt.KeyValuePair
}

// NewStackTrieVerifier creates a verifier for the expected root hash.
func NewStackTrieVerifier(expectedRoot types.Hash) *StackTrieVerifier {
	return &StackTrieVerifier{
		expected: expectedRoot,
	}
}

// AddPair records a key-value pair for later verification.
func (v *StackTrieVerifier) AddPair(key, value []byte) {
	k := make([]byte, len(key))
	copy(k, key)
	val := make([]byte, len(value))
	copy(val, value)
	v.pairs = append(v.pairs, mpt.KeyValuePair{Key: k, Value: val})
}

// Verify rebuilds the trie from recorded pairs and checks the root hash.
// Returns nil if the root matches, or an error describing the mismatch.
func (v *StackTrieVerifier) Verify() error {
	// Sort pairs by key.
	sorted := make([]mpt.KeyValuePair, len(v.pairs))
	copy(sorted, v.pairs)
	sort.Slice(sorted, func(i, j int) bool {
		return compareBytesLess(sorted[i].Key, sorted[j].Key)
	})

	// Build standard trie.
	tr := mpt.New()
	for _, p := range sorted {
		if len(p.Value) == 0 {
			continue
		}
		tr.Put(p.Key, p.Value)
	}
	got := tr.Hash()
	if got != v.expected {
		return errors.New("stack_trie_verifier: root hash mismatch")
	}
	return nil
}

// PairCount returns the number of recorded pairs.
func (v *StackTrieVerifier) PairCount() int {
	return len(v.pairs)
}

// SecureStackTrieHash builds a secure (hashed-key) trie from key-value pairs
// using StackTrie ordering. Keys are keccak256-hashed before insertion. Since
// hashing destroys the natural key order, we must sort the hashed keys first.
func SecureStackTrieHash(pairs []mpt.KeyValuePair) types.Hash {
	if len(pairs) == 0 {
		return emptyRoot
	}

	type hashedPair struct {
		hashedKey []byte
		value     []byte
	}

	hashed := make([]hashedPair, 0, len(pairs))
	for _, p := range pairs {
		if len(p.Value) == 0 {
			continue
		}
		hk := crypto.Keccak256(p.Key)
		val := make([]byte, len(p.Value))
		copy(val, p.Value)
		hashed = append(hashed, hashedPair{hashedKey: hk, value: val})
	}

	// Sort by hashed key.
	sort.Slice(hashed, func(i, j int) bool {
		return compareBytesLess(hashed[i].hashedKey, hashed[j].hashedKey)
	})

	st := NewStackTrie(nil)
	for _, hp := range hashed {
		st.Update(hp.hashedKey, hp.value)
	}
	return st.Hash()
}

// builderMapNodeWriter is a simple in-memory NodeWriter for testing.
type builderMapNodeWriter struct {
	store map[types.Hash][]byte
}

func (w *builderMapNodeWriter) Put(hash types.Hash, data []byte) error {
	cp := make([]byte, len(data))
	copy(cp, data)
	w.store[hash] = cp
	return nil
}
