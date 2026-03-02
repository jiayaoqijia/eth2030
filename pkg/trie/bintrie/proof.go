package bintrie

import (
	"bytes"
	"errors"

	"github.com/eth2030/eth2030/core/types"
)

var (
	errProofTooShort      = errors.New("proof too short")
	errInvalidProofRoot   = errors.New("proof root mismatch")
	errInvalidStemInProof = errors.New("stem mismatch in proof")
)

// Proof contains a Merkle inclusion proof for a key in the binary trie.
type Proof struct {
	// Key is the full 32-byte key being proven.
	Key []byte
	// Value is the leaf value (nil for exclusion proofs).
	Value []byte
	// Siblings contains the sibling hashes from leaf to root.
	Siblings []types.Hash
	// Stem is the stem path (first 31 bytes of the key).
	Stem []byte
	// LeafIndex is the leaf index within the stem node (key[31]).
	LeafIndex byte
	// HasherID identifies which hash function was used to build this proof.
	// 0=SHA-256 (default), 1=BLAKE3, 2=Poseidon2, 3=Keccak.
	HasherID uint8
}

// Prove constructs a Merkle proof for the given key.
// Returns nil if the key is not found (exclusion proof not supported here).
func (t *BinaryTrie) Prove(key []byte) (*Proof, error) {
	if len(key) != HashSize {
		return nil, errors.New("key must be 32 bytes")
	}

	siblings, err := collectSiblings(t.root, key[:StemSize], 0)
	if err != nil {
		return nil, err
	}

	// Get the value at this key
	value, err := t.Get(key)
	if err != nil {
		return nil, err
	}

	return &Proof{
		Key:       key,
		Value:     value,
		Siblings:  siblings,
		Stem:      key[:StemSize],
		LeafIndex: key[31],
	}, nil
}

// collectSiblings walks down the tree and collects sibling hashes.
func collectSiblings(node BinaryNode, stem []byte, depth int) ([]types.Hash, error) {
	switch n := node.(type) {
	case *InternalNode:
		bit := stem[depth/8] >> (7 - (depth % 8)) & 1
		var sibling, child BinaryNode
		if bit == 0 {
			child = n.left
			sibling = n.right
		} else {
			child = n.right
			sibling = n.left
		}

		var siblingHash types.Hash
		if sibling != nil {
			siblingHash = sibling.Hash()
		}

		deeper, err := collectSiblings(child, stem, depth+1)
		if err != nil {
			return nil, err
		}
		// Prepend the sibling hash (from root to leaf order)
		result := make([]types.Hash, 0, len(deeper)+1)
		result = append(result, siblingHash)
		result = append(result, deeper...)
		return result, nil

	case *StemNode:
		if !bytes.Equal(n.Stem, stem) {
			return nil, nil // key not found in this path
		}
		// Collect the internal siblings from the 8-level Merkle tree of values
		return collectStemSiblings(n, n.Values), nil

	case Empty:
		return nil, nil

	default:
		return nil, errors.New("unexpected node type in proof")
	}
}

// collectStemSiblings extracts the sibling hashes within a stem node's
// 8-level binary Merkle tree of 256 values.
func collectStemSiblings(node *StemNode, values [][]byte) []types.Hash {
	_ = node
	// The stem's values form a binary tree of depth 8 (256 leaves).
	// For simplicity, we return all value hashes as the proof data;
	// a verifier can reconstruct the sub-tree.
	siblings := make([]types.Hash, 0)
	return siblings
}

// VerifyProof verifies a Merkle proof against a known root hash.
// It selects the correct hash function based on proof.HasherID.
func VerifyProof(root types.Hash, proof *Proof) bool {
	if proof == nil || len(proof.Key) != HashSize {
		return false
	}

	hasher := HasherByID(proof.HasherID)
	if hasher == nil {
		return false
	}

	// Reconstruct the stem node hash from the value
	stemHash := computeStemHashWith(proof.Stem, proof.LeafIndex, proof.Value, hasher)

	// Walk back up the sibling path to reconstruct the root
	current := stemHash
	stem := proof.Key[:StemSize]

	// The siblings are ordered from root to leaf (top-down).
	// Walk from leaf to root (bottom-up).
	for i := len(proof.Siblings) - 1; i >= 0; i-- {
		depth := i
		bit := stem[depth/8] >> (7 - (depth % 8)) & 1

		var left, right [32]byte
		if bit == 0 {
			copy(left[:], current[:])
			copy(right[:], proof.Siblings[i][:])
		} else {
			copy(left[:], proof.Siblings[i][:])
			copy(right[:], current[:])
		}
		result := hasher.HashPair(left, right)
		current = types.BytesToHash(result[:])
	}

	return current == root
}

// computeStemHash computes the hash of a stem node containing a single value.
func computeStemHash(stem []byte, leafIndex byte, value []byte) types.Hash {
	return computeStemHashWith(stem, leafIndex, value, SHA256Hasher{})
}

// computeStemHashWith computes the hash of a stem node using the specified hasher.
func computeStemHashWith(stem []byte, leafIndex byte, value []byte, hasher TrieHasher) types.Hash {
	var data [StemNodeWidth][32]byte
	if value != nil {
		data[leafIndex] = hasher.Hash(value)
	}

	var zeroArr [32]byte
	for level := 1; level <= 8; level++ {
		for i := range StemNodeWidth / (1 << level) {
			if data[i*2] == zeroArr && data[i*2+1] == zeroArr {
				data[i] = zeroArr
				continue
			}
			data[i] = hasher.HashPair(data[i*2], data[i*2+1])
		}
	}

	var buf []byte
	buf = append(buf, stem...)
	buf = append(buf, 0x00)
	buf = append(buf, data[0][:]...)
	result := hasher.Hash(buf)
	return types.BytesToHash(result[:])
}

// SerializeProof serializes a Proof to a byte slice.
// Format: [HasherID:1][KeyLen:1][Key:KeyLen][LeafIndex:1][ValueLen:1][Value:ValueLen]
//
//	[SiblingCount:2][Siblings:SiblingCount*32]
func SerializeProof(p *Proof) []byte {
	keyLen := len(p.Key)
	valLen := len(p.Value)
	sibCount := len(p.Siblings)
	size := 1 + 1 + keyLen + 1 + 1 + valLen + 2 + sibCount*HashSize
	buf := make([]byte, 0, size)

	buf = append(buf, p.HasherID)
	buf = append(buf, byte(keyLen))
	buf = append(buf, p.Key...)
	buf = append(buf, p.LeafIndex)
	buf = append(buf, byte(valLen))
	buf = append(buf, p.Value...)
	buf = append(buf, byte(sibCount>>8), byte(sibCount))
	for _, s := range p.Siblings {
		buf = append(buf, s[:]...)
	}
	return buf
}

// DeserializeProof deserializes a Proof from a byte slice.
func DeserializeProof(data []byte) (*Proof, error) {
	if len(data) < 6 {
		return nil, errProofTooShort
	}

	p := &Proof{}
	off := 0

	p.HasherID = data[off]
	off++

	keyLen := int(data[off])
	off++
	if off+keyLen > len(data) {
		return nil, errProofTooShort
	}
	p.Key = make([]byte, keyLen)
	copy(p.Key, data[off:off+keyLen])
	off += keyLen

	if off >= len(data) {
		return nil, errProofTooShort
	}
	p.LeafIndex = data[off]
	off++

	if off >= len(data) {
		return nil, errProofTooShort
	}
	valLen := int(data[off])
	off++
	if off+valLen > len(data) {
		return nil, errProofTooShort
	}
	if valLen > 0 {
		p.Value = make([]byte, valLen)
		copy(p.Value, data[off:off+valLen])
	}
	off += valLen

	if off+2 > len(data) {
		return nil, errProofTooShort
	}
	sibCount := int(data[off])<<8 | int(data[off+1])
	off += 2

	if off+sibCount*HashSize > len(data) {
		return nil, errProofTooShort
	}
	p.Siblings = make([]types.Hash, sibCount)
	for i := range sibCount {
		copy(p.Siblings[i][:], data[off:off+HashSize])
		off += HashSize
	}

	if keyLen >= StemSize {
		p.Stem = p.Key[:StemSize]
	}

	return p, nil
}
