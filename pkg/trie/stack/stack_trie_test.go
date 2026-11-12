package stack

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/trie/mpt"
)

// mapNodeWriter is a simple in-memory NodeWriter for tests.
type mapNodeWriter struct {
	store map[types.Hash][]byte
}

type failingNodeWriter struct{ err error }

func (w *failingNodeWriter) Put(types.Hash, []byte) error { return w.err }

func TestStackTrieCommitPropagatesNodeWriteFailure(t *testing.T) {
	writeErr := errors.New("disk unavailable")
	writer := &failingNodeWriter{err: writeErr}
	st := NewStackTrie(writer)
	for _, key := range []byte{1, 2} {
		if err := st.Update([]byte{key}, bytes.Repeat([]byte{key}, 80)); err != nil {
			t.Fatal(err)
		}
	}
	if root, err := st.Commit(); !errors.Is(err, writeErr) || root != (types.Hash{}) {
		t.Fatalf("failed write reported success: root=%s err=%v", root, err)
	}
	st.Reset()
	writer.err = nil
	if err := st.Update([]byte{1}, []byte{2}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Commit(); err != nil {
		t.Fatalf("reset retained previous write failure: %v", err)
	}
}

func (w *mapNodeWriter) Put(hash types.Hash, data []byte) error {
	cp := make([]byte, len(data))
	copy(cp, data)
	w.store[hash] = cp
	return nil
}

func TestStackTrie_Empty(t *testing.T) {
	st := NewStackTrie(nil)
	hash := st.Hash()
	if hash != emptyRoot {
		t.Fatalf("empty stack trie hash = %s, want %s", hash.Hex(), emptyRoot.Hex())
	}
}

func TestStackTrie_SingleEntry(t *testing.T) {
	// Compare StackTrie result with standard Trie.
	st := NewStackTrie(nil)
	if err := st.Update([]byte("hello"), []byte("world")); err != nil {
		t.Fatal(err)
	}

	tr := mpt.New()
	tr.Put([]byte("hello"), []byte("world"))

	stHash := st.Hash()
	trHash := tr.Hash()
	if stHash != trHash {
		t.Fatalf("stack trie hash = %s, standard trie hash = %s", stHash.Hex(), trHash.Hex())
	}
}

func TestStackTrie_MultipleEntries(t *testing.T) {
	// Keys must be inserted in sorted order.
	keys := []string{"abc", "abcdef", "do", "doe", "dog", "doge", "horse"}
	vals := []string{"def", "ghij", "verb", "reindeer", "puppy", "coin", "stallion"}

	st := NewStackTrie(nil)
	for i, k := range keys {
		if err := st.Update([]byte(k), []byte(vals[i])); err != nil {
			t.Fatalf("Update(%q) error: %v", k, err)
		}
	}

	tr := mpt.New()
	for i, k := range keys {
		tr.Put([]byte(k), []byte(vals[i]))
	}

	stHash := st.Hash()
	trHash := tr.Hash()
	if stHash != trHash {
		t.Fatalf("stack trie hash = %s, standard trie hash = %s", stHash.Hex(), trHash.Hex())
	}
}

func TestStackTrie_OutOfOrderError(t *testing.T) {
	st := NewStackTrie(nil)
	if err := st.Update([]byte("b"), []byte("2")); err != nil {
		t.Fatal(err)
	}
	err := st.Update([]byte("a"), []byte("1"))
	if err != ErrStackTrieOutOfOrder {
		t.Fatalf("expected ErrStackTrieOutOfOrder, got %v", err)
	}
}

func TestStackTrie_DuplicateKeyError(t *testing.T) {
	st := NewStackTrie(nil)
	if err := st.Update([]byte("a"), []byte("1")); err != nil {
		t.Fatal(err)
	}
	err := st.Update([]byte("a"), []byte("2"))
	if err != ErrStackTrieOutOfOrder {
		t.Fatalf("expected ErrStackTrieOutOfOrder for duplicate key, got %v", err)
	}
}

func TestStackTrie_FinalizedError(t *testing.T) {
	st := NewStackTrie(nil)
	st.Update([]byte("a"), []byte("1"))
	st.Hash()

	err := st.Update([]byte("b"), []byte("2"))
	if err != ErrStackTrieFinalized {
		t.Fatalf("expected ErrStackTrieFinalized, got %v", err)
	}
}

func TestStackTrie_SkipEmptyValue(t *testing.T) {
	st := NewStackTrie(nil)
	if err := st.Update([]byte("a"), nil); err != nil {
		t.Fatalf("unexpected error for nil value: %v", err)
	}
	if err := st.Update([]byte("a"), []byte{}); err != nil {
		t.Fatalf("unexpected error for empty value: %v", err)
	}
	if st.Count() != 0 {
		t.Fatalf("expected 0 entries, got %d", st.Count())
	}
}

func TestStackTrie_Count(t *testing.T) {
	st := NewStackTrie(nil)
	st.Update([]byte("a"), []byte("1"))
	st.Update([]byte("b"), []byte("2"))
	st.Update([]byte("c"), []byte("3"))
	if st.Count() != 3 {
		t.Fatalf("Count() = %d, want 3", st.Count())
	}
}

func TestStackTrie_Reset(t *testing.T) {
	st := NewStackTrie(nil)
	st.Update([]byte("a"), []byte("1"))
	st.Hash()

	st.Reset()
	if st.Count() != 0 {
		t.Fatalf("Count after reset = %d, want 0", st.Count())
	}
	if st.finalized {
		t.Fatal("expected not finalized after reset")
	}

	// Should be usable again.
	if err := st.Update([]byte("b"), []byte("2")); err != nil {
		t.Fatalf("Update after reset: %v", err)
	}
}

func TestStackTrie_Commit(t *testing.T) {
	store := make(map[types.Hash][]byte)
	writer := &mapNodeWriter{store: store}

	st := NewStackTrie(writer)
	st.Update([]byte("abc"), []byte("def"))
	st.Update([]byte("abcdef"), []byte("ghij"))

	root, err := st.Commit()
	if err != nil {
		t.Fatalf("Commit error: %v", err)
	}
	if root == emptyRoot || root == (types.Hash{}) {
		t.Fatal("expected non-empty root after commit")
	}

	// The writer should have received at least the root node.
	if len(store) == 0 {
		t.Fatal("expected at least one node written to store")
	}
	// The root hash should be in the store.
	if _, ok := store[root]; !ok {
		t.Fatal("root node not found in store")
	}
}

func TestStackTrie_TransactionTrieRoot(t *testing.T) {
	// Simulate a transaction trie: keys are RLP-encoded indices.
	type txEntry struct {
		key []byte
		val []byte
	}
	var entries []txEntry
	for i := 0; i < 10; i++ {
		key, _ := rlp.EncodeToBytes(uint64(i))
		val := []byte(fmt.Sprintf("tx-data-%d", i))
		entries = append(entries, txEntry{key: key, val: val})
	}

	// Sort entries by key since RLP(0)=0x80 sorts after RLP(1..9)=0x01..0x09.
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].key, entries[j].key) < 0
	})

	// StackTrie requires keys in sorted order.
	st := NewStackTrie(nil)
	for _, e := range entries {
		if err := st.Update(e.key, e.val); err != nil {
			t.Fatalf("Update: %v", err)
		}
	}

	// Standard Trie (accepts any order).
	tr := mpt.New()
	for _, e := range entries {
		tr.Put(e.key, e.val)
	}

	stHash := st.Hash()
	trHash := tr.Hash()
	if stHash != trHash {
		t.Fatalf("tx trie: stack=%s standard=%s", stHash.Hex(), trHash.Hex())
	}
}

func TestStackTrie_BinaryKeys(t *testing.T) {
	// Test with single-byte keys covering different nibble patterns.
	st := NewStackTrie(nil)
	tr := mpt.New()

	for i := 0; i < 16; i++ {
		key := []byte{byte(i * 16)} // 0x00, 0x10, 0x20, ...
		val := []byte{byte(i)}
		st.Update(key, val)
		tr.Put(key, val)
	}

	stHash := st.Hash()
	trHash := tr.Hash()
	if stHash != trHash {
		t.Fatalf("binary keys: stack=%s standard=%s", stHash.Hex(), trHash.Hex())
	}
}

func TestStackTrie_LargeValues(t *testing.T) {
	st := NewStackTrie(nil)
	tr := mpt.New()

	largeVal := make([]byte, 1024)
	for i := range largeVal {
		largeVal[i] = byte(i % 256)
	}

	st.Update([]byte("big"), largeVal)
	st.Update([]byte("small"), []byte("tiny"))

	tr.Put([]byte("big"), largeVal)
	tr.Put([]byte("small"), []byte("tiny"))

	stHash := st.Hash()
	trHash := tr.Hash()
	if stHash != trHash {
		t.Fatalf("large values: stack=%s standard=%s", stHash.Hex(), trHash.Hex())
	}
}

func TestStackTrie_PrefixSharing(t *testing.T) {
	// Keys that share long common prefixes.
	st := NewStackTrie(nil)
	tr := mpt.New()

	keys := []string{
		"ethereum",
		"ethereum2028",
		"ethereum2028client",
	}
	for _, k := range keys {
		st.Update([]byte(k), []byte("v"))
		tr.Put([]byte(k), []byte("v"))
	}

	stHash := st.Hash()
	trHash := tr.Hash()
	if stHash != trHash {
		t.Fatalf("prefix sharing: stack=%s standard=%s", stHash.Hex(), trHash.Hex())
	}
}

func TestEncodeRLPBytes(t *testing.T) {
	tests := []struct {
		input []byte
		want  byte // first byte of encoding
	}{
		{nil, 0x80},
		{[]byte{}, 0x80},
		{[]byte{0x01}, 0x01},    // single byte < 0x80
		{[]byte{0x80}, 0x81},    // single byte >= 0x80
		{[]byte("hello"), 0x85}, // short string (len=5)
	}
	for _, tc := range tests {
		enc := encodeRLPBytes(tc.input)
		if enc[0] != tc.want {
			t.Errorf("encodeRLPBytes(%x): first byte = 0x%02x, want 0x%02x", tc.input, enc[0], tc.want)
		}
	}
}
