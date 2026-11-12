// stack_trie.go implements a stack-based trie for efficient sequential insertion.
// It is used for computing transaction and receipt trie roots where keys are
// processed in sorted (RLP-encoded index) order. The implementation retains
// inserted nodes until finalization; it does not provide bounded-memory streaming.
package stack

import (
	"errors"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

var (
	// ErrStackTrieOutOfOrder is returned when keys are inserted out of order.
	ErrStackTrieOutOfOrder = errors.New("stack trie: keys must be inserted in sorted order")

	// ErrStackTrieFinalized is returned when Update is called after Hash or Commit.
	ErrStackTrieFinalized = errors.New("stack trie: already finalized")
)

// stackTrieNodeType distinguishes the node states in the StackTrie.
type stackTrieNodeType byte

const (
	stEmpty  stackTrieNodeType = iota // empty / unused slot
	stLeaf                            // leaf node (key suffix + value)
	stExt                             // extension node (shared prefix + child)
	stBranch                          // branch node (16 children + value)
	stHash                            // already hashed/committed subtree
)

// stackTrieNode is a node in the StackTrie's working stack. Nodes transition
// through types as new keys are inserted: empty -> leaf -> branch (via split).
type stackTrieNode struct {
	typ      stackTrieNodeType
	key      []byte             // nibble key (for leaf: full remaining key; for ext: shared prefix)
	val      []byte             // value bytes (for leaf nodes)
	children [16]*stackTrieNode // branch children (only for stBranch)
	hash     []byte             // cached hash (only for stHash)
}

// StackTrie builds a Merkle Patricia Trie from key-value pairs inserted in
// strictly sorted order. It is optimized for the use case of computing
// transaction trie roots and receipt trie roots.
type StackTrie struct {
	root      *stackTrieNode
	lastKey   []byte // last inserted key in nibble form, for order checking
	finalized bool
	kvCount   int

	// Optional writer to persist nodes during commit.
	writer   NodeWriter
	writeErr error
}

// NewStackTrie creates a new empty StackTrie. An optional NodeWriter can be
// provided for persisting nodes during Commit.
func NewStackTrie(writer NodeWriter) *StackTrie {
	return &StackTrie{
		root:   newStackTrieNode(),
		writer: writer,
	}
}

func newStackTrieNode() *stackTrieNode {
	return &stackTrieNode{typ: stEmpty}
}

// Update inserts a key-value pair into the stack trie. Keys must be inserted
// in strictly ascending sorted order (lexicographic on the raw bytes).
func (st *StackTrie) Update(key, value []byte) error {
	if st.finalized {
		return ErrStackTrieFinalized
	}
	if len(value) == 0 {
		return nil // skip empty values
	}

	// Enforce sorted order on raw byte keys (not nibble keys, since the
	// terminator byte in nibble form breaks raw-byte ordering).
	if st.lastKey != nil {
		if compareBytesLess(key, st.lastKey) || keysEqual(key, st.lastKey) {
			return ErrStackTrieOutOfOrder
		}
	}
	st.lastKey = make([]byte, len(key))
	copy(st.lastKey, key)

	// Convert to nibbles and strip the terminator. The terminator (0x10) is
	// only needed during encoding to distinguish leaves from extensions;
	// internally, leaf type is tracked by stackTrieNodeType.
	nibbles := keybytesToHex(key)
	nibbles = nibbles[:len(nibbles)-1] // strip terminator
	st.kvCount++
	st.insert(st.root, nibbles, value)
	return nil
}

// insert recursively inserts a nibble key (without terminator) and value into
// the node tree. Keys that terminate at a branch point store their value in
// the branch's val field (the 17th element in RLP encoding).
func (st *StackTrie) insert(n *stackTrieNode, key, value []byte) {
	switch n.typ {
	case stEmpty:
		// Turn empty node into a leaf.
		n.typ = stLeaf
		n.key = make([]byte, len(key))
		copy(n.key, key)
		n.val = make([]byte, len(value))
		copy(n.val, value)

	case stLeaf:
		// Find common prefix between existing leaf key and new key.
		match := prefixLen(n.key, key)

		if match == len(n.key) && match == len(key) {
			// Same key: update value.
			n.val = make([]byte, len(value))
			copy(n.val, value)
			return
		}

		// Split the leaf into a branch (possibly with an extension).
		existingKey := n.key
		existingVal := n.val

		branch := &stackTrieNode{typ: stBranch}

		// Place the existing leaf's value into the branch.
		if match == len(existingKey) {
			// Existing key terminates at this branch point; value
			// goes into the branch's value slot.
			branch.val = existingVal
		} else {
			// Existing key continues; create a child leaf.
			oldChild := newStackTrieNode()
			oldChild.typ = stLeaf
			oldChild.key = stCopyBytes(existingKey[match+1:])
			oldChild.val = existingVal
			branch.children[existingKey[match]] = oldChild
		}

		// Place the new key's value into the branch.
		if match == len(key) {
			// New key terminates at this branch point.
			branch.val = make([]byte, len(value))
			copy(branch.val, value)
		} else {
			newChild := newStackTrieNode()
			newChild.typ = stLeaf
			newChild.key = stCopyBytes(key[match+1:])
			newChild.val = make([]byte, len(value))
			copy(newChild.val, value)
			branch.children[key[match]] = newChild
		}

		// Wrap in extension if there's a common prefix, else promote branch.
		if match > 0 {
			n.typ = stExt
			n.key = stCopyBytes(existingKey[:match])
			n.val = nil
			for i := range n.children {
				n.children[i] = nil
			}
			n.children[0] = branch
		} else {
			*n = *branch
		}

	case stExt:
		// Match the extension prefix.
		match := prefixLen(n.key, key)
		if match == len(n.key) {
			// Key continues past extension; recurse into child.
			st.insert(n.children[0], key[match:], value)
			return
		}
		// Need to split the extension.
		oldExt := n.key
		child := n.children[0]

		branch := &stackTrieNode{typ: stBranch}

		// Existing subtree goes under a new extension or directly.
		remaining := len(oldExt) - match - 1
		if remaining > 0 {
			ext := newStackTrieNode()
			ext.typ = stExt
			ext.key = stCopyBytes(oldExt[match+1:])
			ext.children[0] = child
			branch.children[oldExt[match]] = ext
		} else {
			branch.children[oldExt[match]] = child
		}

		// Place new key.
		if match == len(key) {
			// New key terminates at this branch point.
			branch.val = make([]byte, len(value))
			copy(branch.val, value)
		} else {
			newChild := newStackTrieNode()
			newChild.typ = stLeaf
			newChild.key = stCopyBytes(key[match+1:])
			newChild.val = make([]byte, len(value))
			copy(newChild.val, value)
			branch.children[key[match]] = newChild
		}

		if match > 0 {
			n.key = stCopyBytes(oldExt[:match])
			n.children[0] = branch
		} else {
			// No prefix left: promote branch to this node.
			*n = *branch
		}

	case stBranch:
		if len(key) == 0 {
			// Key terminates at this branch; store value.
			n.val = make([]byte, len(value))
			copy(n.val, value)
			return
		}
		idx := key[0]
		if n.children[idx] == nil {
			n.children[idx] = newStackTrieNode()
		}
		st.insert(n.children[idx], key[1:], value)

	case stHash:
		// Should not happen; already finalized subtree.
		return
	}
}

// stCopyBytes returns a copy of a byte slice (nil-safe, returns nil for empty/nil).
func stCopyBytes(b []byte) []byte {
	if len(b) == 0 {
		return nil
	}
	cp := make([]byte, len(b))
	copy(cp, b)
	return cp
}

// Hash computes and returns the root hash of the stack trie. After calling
// Hash, no more updates can be performed.
func (st *StackTrie) Hash() types.Hash {
	st.finalized = true
	if st.kvCount == 0 {
		return emptyRoot
	}
	return st.hashNode(st.root)
}

// hashNode recursively computes the hash of a stack trie node.
func (st *StackTrie) hashNode(n *stackTrieNode) types.Hash {
	enc := st.encodeStackNode(n)
	return crypto.Keccak256Hash(enc)
}

// encodeStackNode RLP-encodes a stack trie node.
func (st *StackTrie) encodeStackNode(n *stackTrieNode) []byte {
	switch n.typ {
	case stEmpty:
		return []byte{0x80} // RLP empty string

	case stLeaf:
		// Encode as [compact_key, value]. Add the terminator back to mark
		// this as a leaf in compact (hex-prefix) encoding.
		leafKey := make([]byte, len(n.key)+1)
		copy(leafKey, n.key)
		leafKey[len(leafKey)-1] = terminatorByte
		compactKey := hexToCompact(leafKey)
		return st.encodeShortRLP(compactKey, n.val)

	case stExt:
		// Encode as [compact_key, child_hash_or_inline].
		compactKey := hexToCompact(n.key)
		childEnc := st.encodeStackNode(n.children[0])
		keyEnc := encodeRLPBytes(compactKey)
		var valEnc []byte
		if len(childEnc) >= 32 {
			h := crypto.Keccak256(childEnc)
			st.persistNode(h, childEnc)
			valEnc = encodeRLPBytes(h) // hash reference: wrap as RLP string
		} else {
			valEnc = childEnc // inline node: already RLP-encoded, use directly
		}
		payload := append(keyEnc, valEnc...)
		return wrapListPayload(payload)

	case stBranch:
		return st.encodeBranchRLP(n)

	case stHash:
		return n.hash
	}
	return []byte{0x80}
}

// encodeShortRLP encodes a 2-element RLP list [key, val].
func (st *StackTrie) encodeShortRLP(key, val []byte) []byte {
	keyEnc := encodeRLPBytes(key)
	valEnc := encodeRLPBytes(val)
	payload := append(keyEnc, valEnc...)
	return wrapListPayload(payload)
}

// encodeBranchRLP encodes a 17-element branch node.
func (st *StackTrie) encodeBranchRLP(n *stackTrieNode) []byte {
	var payload []byte
	for i := 0; i < 16; i++ {
		child := n.children[i]
		if child == nil {
			payload = append(payload, 0x80) // empty
			continue
		}
		childEnc := st.encodeStackNode(child)
		if len(childEnc) >= 32 {
			h := crypto.Keccak256(childEnc)
			st.persistNode(h, childEnc)
			payload = append(payload, encodeRLPBytes(h)...)
		} else {
			payload = append(payload, childEnc...)
		}
	}
	// Element 17: branch value (non-empty when a key terminates at this branch).
	if n.val != nil {
		payload = append(payload, encodeRLPBytes(n.val)...)
	} else {
		payload = append(payload, 0x80)
	}
	return wrapListPayload(payload)
}

// persistNode writes a node to the optional writer, used during Commit.
func (st *StackTrie) persistNode(hash, data []byte) {
	if st.writer == nil || len(hash) != 32 || st.writeErr != nil {
		return
	}
	var h types.Hash
	copy(h[:], hash)
	st.writeErr = st.writer.Put(h, data)
}

// Commit finalizes the trie, computing the root hash and persisting all nodes
// to the configured writer. Returns the root hash.
func (st *StackTrie) Commit() (types.Hash, error) {
	st.finalized = true
	if st.kvCount == 0 {
		return emptyRoot, nil
	}
	rootEnc := st.encodeStackNode(st.root)
	rootHash := crypto.Keccak256Hash(rootEnc)

	// Persist root node itself.
	if st.writer != nil && st.writeErr == nil {
		st.writeErr = st.writer.Put(rootHash, rootEnc)
	}
	if st.writeErr != nil {
		return types.Hash{}, st.writeErr
	}
	return rootHash, nil
}

// Count returns the number of key-value pairs inserted.
func (st *StackTrie) Count() int {
	return st.kvCount
}

// Reset clears the stack trie for reuse.
func (st *StackTrie) Reset() {
	st.root = newStackTrieNode()
	st.lastKey = nil
	st.finalized = false
	st.kvCount = 0
	st.writeErr = nil
}

// encodeRLPBytes encodes a byte slice as an RLP string.
func encodeRLPBytes(b []byte) []byte {
	if len(b) == 0 {
		return []byte{0x80}
	}
	if len(b) == 1 && b[0] < 0x80 {
		return []byte{b[0]}
	}
	if len(b) <= 55 {
		result := make([]byte, 1+len(b))
		result[0] = 0x80 + byte(len(b))
		copy(result[1:], b)
		return result
	}
	lenBytes := putUintBigEndian(uint64(len(b)))
	result := make([]byte, 1+len(lenBytes)+len(b))
	result[0] = 0xb7 + byte(len(lenBytes))
	copy(result[1:], lenBytes)
	copy(result[1+len(lenBytes):], b)
	return result
}
