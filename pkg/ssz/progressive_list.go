package ssz

import (
	"errors"
	"fmt"
)

// EIP-7916: SSZ ProgressiveList
//
// A progressive list uses a recursive Merkle tree structure that grows
// progressively with the actual leaf count. The tree is composed of a
// sequence of binary subtrees with increasing capacity:
//   depth 1: 1 chunk  (subtree of 1 leaf)
//   depth 2: 4 chunks (subtree of 4 leaves)
//   depth 3: 16 chunks
//   depth 4: 64 chunks
//   ...
// At each level, the capacity is 4^(level-1) for level >= 1.
// The total capacity after d subtrees is sum_{i=0}^{d-1} 4^i = (4^d - 1)/3.

var (
	ErrProgressiveListEmpty = errors.New("ssz: progressive list index out of range")
)

// ProgressiveList is an SSZ list type with a progressive Merkle tree shape
// per EIP-7916. Elements are stored as their 32-byte hash tree roots (chunks).
type ProgressiveList struct {
	chunks [][32]byte
}

// NewProgressiveList creates a new ProgressiveList from element hash tree roots.
func NewProgressiveList(chunks [][32]byte) *ProgressiveList {
	cp := make([][32]byte, len(chunks))
	copy(cp, chunks)
	return &ProgressiveList{chunks: cp}
}

// NewProgressiveListEmpty creates an empty ProgressiveList.
func NewProgressiveListEmpty() *ProgressiveList {
	return &ProgressiveList{}
}

// Len returns the number of elements.
func (pl *ProgressiveList) Len() int {
	return len(pl.chunks)
}

// Get returns the chunk at the given index.
func (pl *ProgressiveList) Get(index int) ([32]byte, error) {
	if index < 0 || index >= len(pl.chunks) {
		return [32]byte{}, fmt.Errorf("%w: index %d, len %d", ErrProgressiveListEmpty, index, len(pl.chunks))
	}
	return pl.chunks[index], nil
}

// Append adds an element (as its hash tree root) to the list.
func (pl *ProgressiveList) Append(chunk [32]byte) {
	pl.chunks = append(pl.chunks, chunk)
}

// HashTreeRoot computes the progressive list hash tree root per EIP-7916.
// The result is mix_in_length(merkleize_progressive(chunks), len(chunks)).
func (pl *ProgressiveList) HashTreeRoot() [32]byte {
	root := merkleizeProgressive(pl.chunks, 1)
	return MixInLength(root, uint64(len(pl.chunks)))
}

// merkleizeProgressive implements the recursive progressive Merkle tree.
//
// merkleize_progressive(chunks, num_leaves):
//   - If len(chunks) == 0: return Bytes32() (zero hash)
//   - Otherwise: hash(
//     merkleize(chunks[:num_leaves], num_leaves),
//     merkleize_progressive(chunks[num_leaves:], num_leaves * 4)
//     )
func merkleizeProgressive(chunks [][32]byte, numLeaves int) [32]byte {
	if len(chunks) == 0 {
		return zeroHash()
	}

	// Split: first subtree has up to numLeaves chunks.
	splitAt := numLeaves
	if splitAt > len(chunks) {
		splitAt = len(chunks)
	}

	// Left: standard SSZ Merkleize of the first subtree.
	left := Merkleize(chunks[:splitAt], numLeaves)

	// Right: recursive progressive Merkleize of the remainder.
	right := merkleizeProgressive(chunks[splitAt:], numLeaves*4)

	return hash(left, right)
}

// ProgressiveListProof generates a Merkle proof for a specific element in the
// progressive list. The proof includes sibling hashes needed to verify the
// element's inclusion, plus the length mix-in.
//
// Returns the proof hashes (bottom to top) and the generalized index.
func (pl *ProgressiveList) GenerateProof(index int) ([][32]byte, uint64, error) {
	if index < 0 || index >= len(pl.chunks) {
		return nil, 0, fmt.Errorf("%w: index %d, len %d", ErrProgressiveListEmpty, index, len(pl.chunks))
	}

	// Determine which subtree the index falls into.
	// Subtree 0: chunks[0..1), capacity 1
	// Subtree 1: chunks[1..5), capacity 4
	// Subtree 2: chunks[5..21), capacity 16
	// etc.

	proof, gindex := progressiveProof(pl.chunks, 1, index, 1)

	// Mix in the length: the root is hash(merkle_root, length_chunk), so
	// append the length chunk as the final sibling (for the length mix-in).
	var lengthChunk [32]byte
	lengthChunk[0] = byte(len(pl.chunks))
	lengthChunk[1] = byte(len(pl.chunks) >> 8)
	lengthChunk[2] = byte(len(pl.chunks) >> 16)
	lengthChunk[3] = byte(len(pl.chunks) >> 24)
	proof = append(proof, lengthChunk)

	// The gindex for the mix-in level: multiply by 2 (left child of the
	// mix-in node, since length is the right child).
	gindex = gindex * 2

	return proof, gindex, nil
}

// progressiveProof recursively builds a proof for the element at `target`
// within the progressive tree structure.
func progressiveProof(chunks [][32]byte, numLeaves int, target int, gindex uint64) ([][32]byte, uint64) {
	if len(chunks) == 0 {
		return nil, gindex
	}

	splitAt := numLeaves
	if splitAt > len(chunks) {
		splitAt = len(chunks)
	}

	if target < splitAt {
		// Target is in the left (standard Merkle) subtree.
		// Build proof within this subtree, then add the right subtree root.
		leftProof, leftGindex := standardMerkleProof(chunks[:splitAt], numLeaves, target, gindex*2)
		rightRoot := merkleizeProgressive(chunks[splitAt:], numLeaves*4)
		leftProof = append(leftProof, rightRoot)
		return leftProof, leftGindex
	}

	// Target is in the right (progressive) subtree.
	leftRoot := Merkleize(chunks[:splitAt], numLeaves)
	rightProof, rightGindex := progressiveProof(chunks[splitAt:], numLeaves*4, target-splitAt, gindex*2+1)
	rightProof = append(rightProof, leftRoot)
	return rightProof, rightGindex
}

// standardMerkleProof builds a proof for an element within a standard
// (power-of-two padded) Merkle tree.
func standardMerkleProof(chunks [][32]byte, limit int, target int, gindex uint64) ([][32]byte, uint64) {
	limit = nextPowerOfTwo(limit)

	// Pad chunks to limit.
	padded := make([][32]byte, limit)
	copy(padded, chunks)

	// Compute tree depth.
	depth := 0
	for (1 << uint(depth)) < limit {
		depth++
	}

	if depth == 0 {
		// Single leaf: no sibling hashes needed at this level.
		return nil, gindex
	}

	// Build the full tree bottom-up and collect siblings.
	layers := make([][][32]byte, depth+1)
	layers[0] = padded

	for d := 0; d < depth; d++ {
		layerSize := len(layers[d]) / 2
		layers[d+1] = make([][32]byte, layerSize)
		for i := 0; i < layerSize; i++ {
			layers[d+1][i] = hash(layers[d][2*i], layers[d][2*i+1])
		}
	}

	// Walk from the leaf to the root, collecting siblings.
	var proof [][32]byte
	idx := target
	currentGindex := gindex
	for d := 0; d < depth; d++ {
		siblingIdx := idx ^ 1
		proof = append(proof, layers[d][siblingIdx])
		currentGindex = currentGindex * 2
		if idx%2 == 1 {
			currentGindex++
		}
		idx /= 2
	}

	// The final gindex is computed by tracking position from root.
	// Recompute properly: start from the subtree gindex and navigate down.
	leafGindex := gindex
	pos := target
	for d := 0; d < depth; d++ {
		leafGindex = leafGindex << 1
		if pos>>(uint(depth-1-d))&1 == 1 {
			leafGindex |= 1
		}
	}

	return proof, leafGindex
}

// --- Convenience functions for typed progressive lists ---

// HashTreeRootProgressiveContainer implements EIP-7495. Field roots are
// positioned by active-field index; inactive positions are zero chunks.
func HashTreeRootProgressiveContainer(fieldRoots [][32]byte, activeFields [32]byte) [32]byte {
	return hash(merkleizeProgressive(fieldRoots, 1), activeFields)
}

// HashTreeRootCompatibleUnion mixes the EIP-8016 selector into its value root.
func HashTreeRootCompatibleUnion(valueRoot [32]byte, selector uint8) [32]byte {
	return hash(valueRoot, HashTreeRootUint8(selector))
}

// HashTreeRootProgressiveList computes the hash tree root of a progressive
// list where each element is provided as its 32-byte hash tree root.
func HashTreeRootProgressiveList(elementRoots [][32]byte) [32]byte {
	root := merkleizeProgressive(elementRoots, 1)
	return MixInLength(root, uint64(len(elementRoots)))
}

// HashTreeRootProgressiveBasicList computes the hash tree root of a
// progressive list of basic type elements. The serialized data is packed
// into chunks, then progressive-Merkleized and mixed with length.
func HashTreeRootProgressiveBasicList(serialized []byte, count int) [32]byte {
	var chunks [][32]byte
	if len(serialized) != 0 {
		chunks = Pack(serialized)
	}
	root := merkleizeProgressive(chunks, 1)
	return MixInLength(root, uint64(count))
}

// HashTreeRootProgressiveBitlist computes the hash tree root of a
// ProgressiveBitlist per EIP-7916.
func HashTreeRootProgressiveBitlist(bits []bool) [32]byte {
	if len(bits) == 0 {
		root := merkleizeProgressive(nil, 1)
		return MixInLength(root, 0)
	}
	packed := MarshalBitvector(bits)
	chunks := Pack(packed)
	root := merkleizeProgressive(chunks, 1)
	return MixInLength(root, uint64(len(bits)))
}
