package bintrie

import (
	"bytes"
	"errors"

	"github.com/eth2030/eth2030/core/types"
)

// State expiry constants.
const (
	// ExpiryEpochDivisor is the number of blocks per expiry epoch.
	ExpiryEpochDivisor = 8192

	// revivalGasCost is the gas required to revive an expired stem value.
	revivalGasCost = 25000
)

// Revival proof errors.
var (
	ErrRevivalInvalidProof = errors.New("revival: invalid Merkle proof")
	ErrRevivalValueMismatch = errors.New("revival: proven value does not match provided value")
	ErrRevivalKeyNotFound   = errors.New("revival: key not found in trie")
	ErrRevivalNotExpired    = errors.New("revival: value is not expired")
)

// RevivalGasCost returns the gas cost to revive an expired stem value.
func RevivalGasCost() uint64 {
	return revivalGasCost
}

// BlockToEpoch converts a block number to an expiry epoch.
func BlockToEpoch(blockNumber uint64) uint64 {
	return blockNumber / ExpiryEpochDivisor
}

// ReviveStemValue revives an expired value in the trie using a historical
// Merkle proof. The proof must verify against the provided historical root
// (not the current trie root). After successful revival, the value is
// restored and the expiry bit is cleared.
//
// Returns nil if the value is not expired (no-op).
func (t *BinaryTrie) ReviveStemValue(key, value []byte, proof *InclusionProof) error {
	if len(key) != HashSize {
		return ErrInvalidKeyLength
	}

	// Find the stem node for this key.
	stemNode := findStemNode(t.root, key[:StemSize], 0)
	if stemNode == nil {
		return ErrRevivalKeyNotFound
	}

	leafIdx := key[StemSize]

	// If the value is not expired, this is a no-op.
	if !stemNode.IsValueExpired(leafIdx) {
		return nil
	}

	// Verify the Merkle proof against the proof's own reconstructed root.
	// The proof root is a historical root, not the current trie root.
	proofRoot := reconstructProofRoot(proof)
	if proofRoot == (types.Hash{}) {
		return ErrRevivalInvalidProof
	}

	// Verify the proof reconstructs to a valid root.
	pv := NewProofVerifier(proofRoot[:])
	if !pv.VerifyInclusion(proof) {
		return ErrRevivalInvalidProof
	}

	// Verify the proven value matches the provided value.
	if !bytes.Equal(proof.Value[:], value) {
		return ErrRevivalValueMismatch
	}

	// Revival: clear the expiry bit and restore the value.
	stemNode.UnexpireValue(leafIdx)
	stemNode.Values[leafIdx] = make([]byte, len(value))
	copy(stemNode.Values[leafIdx], value)

	return nil
}

// findStemNode locates the StemNode for the given stem in the trie.
func findStemNode(node BinaryNode, stem []byte, depth int) *StemNode {
	switch n := node.(type) {
	case *InternalNode:
		bit := stem[depth/8] >> (7 - (depth % 8)) & 1
		if bit == 0 {
			return findStemNode(n.left, stem, depth+1)
		}
		return findStemNode(n.right, stem, depth+1)
	case *StemNode:
		if bytes.Equal(n.Stem, stem) {
			return n
		}
		return nil
	default:
		return nil
	}
}

// reconstructProofRoot reconstructs the root hash from an inclusion proof
// by walking from the stem hash up through the siblings.
func reconstructProofRoot(proof *InclusionProof) types.Hash {
	if proof == nil {
		return types.Hash{}
	}

	stem := proof.Key[:StemSize]
	leafIdx := proof.Key[StemSize]
	stemHash := computeStemLeafHash(stem, leafIdx, proof.Value[:])

	current := stemHash
	for i, sib := range proof.Path.Siblings {
		depth := len(proof.Path.Siblings) - 1 - i
		bit := getBit(stem, depth)
		current = hashPair(current, sib, bit)
	}

	return current
}
