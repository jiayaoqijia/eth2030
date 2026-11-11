package ssz

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

func sha256Hash(data []byte) [32]byte {
	return sha256.Sum256(data)
}

// --- Pack tests ---

func TestPackNilReturnsZeroChunk(t *testing.T) {
	chunks := Pack(nil)
	if len(chunks) != 1 {
		t.Fatalf("Pack(nil) should return 1 chunk, got %d", len(chunks))
	}
	if chunks[0] != [32]byte{} {
		t.Error("Pack(nil) chunk should be zero")
	}
}

func TestPackEmptyReturnsZeroChunk(t *testing.T) {
	chunks := Pack([]byte{})
	if len(chunks) != 1 {
		t.Fatalf("Pack(empty) should return 1 chunk, got %d", len(chunks))
	}
}

func TestPackExactChunk(t *testing.T) {
	data := make([]byte, 32)
	data[0] = 0xaa
	chunks := Pack(data)
	if len(chunks) != 1 {
		t.Fatalf("Pack(32 bytes) should return 1 chunk, got %d", len(chunks))
	}
	if chunks[0][0] != 0xaa {
		t.Error("first byte mismatch")
	}
}

func TestPackMultipleChunks(t *testing.T) {
	data := make([]byte, 64)
	data[0] = 1
	data[32] = 2
	chunks := Pack(data)
	if len(chunks) != 2 {
		t.Fatalf("expected 2 chunks, got %d", len(chunks))
	}
	if chunks[0][0] != 1 {
		t.Error("chunk 0 first byte should be 1")
	}
	if chunks[1][0] != 2 {
		t.Error("chunk 1 first byte should be 2")
	}
}

func TestPackPartialChunk(t *testing.T) {
	data := []byte{0xab, 0xcd}
	chunks := Pack(data)
	if len(chunks) != 1 {
		t.Fatalf("Pack(2 bytes) should return 1 chunk, got %d", len(chunks))
	}
	if chunks[0][0] != 0xab || chunks[0][1] != 0xcd {
		t.Error("data mismatch in partial chunk")
	}
	// Rest should be zero-padded.
	for i := 2; i < 32; i++ {
		if chunks[0][i] != 0 {
			t.Errorf("byte %d should be zero, got %d", i, chunks[0][i])
		}
	}
}

// --- nextPowerOfTwo tests ---

func TestNextPowerOfTwo(t *testing.T) {
	tests := []struct {
		n    int
		want int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{4, 4},
		{5, 8},
		{7, 8},
		{8, 8},
		{9, 16},
		{16, 16},
		{17, 32},
	}
	for _, tt := range tests {
		got := nextPowerOfTwo(tt.n)
		if got != tt.want {
			t.Errorf("nextPowerOfTwo(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

// --- Merkleize tests ---

func TestMerkleizeSingleChunk(t *testing.T) {
	var chunk [32]byte
	chunk[0] = 0xab
	root := Merkleize([][32]byte{chunk}, 0)
	if root != chunk {
		t.Error("Merkleize of single chunk should return the chunk itself")
	}
}

func TestMerkleizeTwoChunksDetailed(t *testing.T) {
	var a, b [32]byte
	a[0] = 1
	b[0] = 2
	root := Merkleize([][32]byte{a, b}, 0)
	expected := sha256Hash(append(a[:], b[:]...))
	if root != expected {
		t.Fatalf("Merkleize(2 chunks) = %x, want %x", root, expected)
	}
}

func TestMerkleizeFourChunks(t *testing.T) {
	chunks := make([][32]byte, 4)
	for i := range chunks {
		chunks[i][0] = byte(i + 1)
	}
	root := Merkleize(chunks, 0)

	// Build expected tree manually.
	left := sha256Hash(append(chunks[0][:], chunks[1][:]...))
	right := sha256Hash(append(chunks[2][:], chunks[3][:]...))
	expected := sha256Hash(append(left[:], right[:]...))
	if root != expected {
		t.Fatalf("Merkleize(4 chunks) mismatch")
	}
}

func TestMerkleizeWithLimit(t *testing.T) {
	var chunk [32]byte
	chunk[0] = 0xff
	root := Merkleize([][32]byte{chunk}, 4)

	z := [32]byte{}
	left := sha256Hash(append(chunk[:], z[:]...))
	right := sha256Hash(append(z[:], z[:]...))
	expected := sha256Hash(append(left[:], right[:]...))
	if root != expected {
		t.Fatalf("Merkleize with limit=4 mismatch")
	}
}

func TestMerkleizeEmptyChunks(t *testing.T) {
	root := Merkleize(nil, 0)
	// Empty input gets a zero chunk and is Merkleized.
	if root != [32]byte{} {
		t.Fatalf("empty Merkle root = %x, want zero", root)
	}
}

// --- MixInLength tests ---

func TestMixInLengthValue(t *testing.T) {
	var root [32]byte
	root[0] = 0xaa
	result := MixInLength(root, 42)

	var lenChunk [32]byte
	binary.LittleEndian.PutUint64(lenChunk[:8], 42)
	expected := sha256Hash(append(root[:], lenChunk[:]...))
	if result != expected {
		t.Fatalf("MixInLength mismatch")
	}
}

func TestMixInLengthZero(t *testing.T) {
	var root [32]byte
	root[0] = 0xbb
	result := MixInLength(root, 0)

	var lenChunk [32]byte // length 0
	expected := sha256Hash(append(root[:], lenChunk[:]...))
	if result != expected {
		t.Fatalf("MixInLength(0) mismatch")
	}
}

// --- HashTreeRoot basic type tests ---

func TestHashTreeRootBoolFalse(t *testing.T) {
	root := HashTreeRootBool(false)
	if root != [32]byte{} {
		t.Error("hash_tree_root(false) should be zero chunk")
	}
}

func TestHashTreeRootBoolTrue(t *testing.T) {
	root := HashTreeRootBool(true)
	var expected [32]byte
	expected[0] = 1
	if root != expected {
		t.Error("hash_tree_root(true) mismatch")
	}
}

func TestHashTreeRootUint8(t *testing.T) {
	root := HashTreeRootUint8(0xff)
	var expected [32]byte
	expected[0] = 0xff
	if root != expected {
		t.Errorf("hash_tree_root(uint8(0xff)) = %x, want %x", root, expected)
	}
}

func TestHashTreeRootUint16(t *testing.T) {
	root := HashTreeRootUint16(0x0102)
	var expected [32]byte
	binary.LittleEndian.PutUint16(expected[:2], 0x0102)
	if root != expected {
		t.Errorf("hash_tree_root(uint16) mismatch")
	}
}

func TestHashTreeRootUint32(t *testing.T) {
	root := HashTreeRootUint32(0xaabbccdd)
	var expected [32]byte
	binary.LittleEndian.PutUint32(expected[:4], 0xaabbccdd)
	if root != expected {
		t.Errorf("hash_tree_root(uint32) mismatch")
	}
}

func TestHashTreeRootUint64(t *testing.T) {
	root := HashTreeRootUint64(0xdeadbeef)
	var expected [32]byte
	binary.LittleEndian.PutUint64(expected[:8], 0xdeadbeef)
	if root != expected {
		t.Errorf("hash_tree_root(uint64) mismatch")
	}
}

func TestHashTreeRootBytes32(t *testing.T) {
	var b [32]byte
	b[0] = 0xab
	b[31] = 0xcd
	root := HashTreeRootBytes32(b)
	if root != b {
		t.Error("hash_tree_root(bytes32) should return the value itself")
	}
}

// --- HashTreeRoot composite type tests ---

func TestHashTreeRootVectorTwoElements(t *testing.T) {
	rootA := HashTreeRootUint64(10)
	rootB := HashTreeRootUint64(20)
	vecRoot := HashTreeRootVector([][32]byte{rootA, rootB})
	expected := sha256Hash(append(rootA[:], rootB[:]...))
	if vecRoot != expected {
		t.Error("vector hash tree root mismatch")
	}
}

func TestHashTreeRootVectorSingleElement(t *testing.T) {
	root := HashTreeRootUint64(42)
	vecRoot := HashTreeRootVector([][32]byte{root})
	if vecRoot != root {
		t.Error("single-element vector root should equal the element root")
	}
}

func TestHashTreeRootListWithLength(t *testing.T) {
	rootA := HashTreeRootUint64(10)
	rootB := HashTreeRootUint64(20)
	listRoot := HashTreeRootList([][32]byte{rootA, rootB}, 4)

	merkleRoot := Merkleize([][32]byte{rootA, rootB}, 4)
	expected := MixInLength(merkleRoot, 2)
	if listRoot != expected {
		t.Error("list hash tree root mismatch")
	}
}

func TestHashTreeRootListEmpty(t *testing.T) {
	listRoot := HashTreeRootList(nil, 8)
	// Empty list: Merkleize([], 8) then MixInLength(root, 0).
	merkleRoot := Merkleize(nil, 8)
	expected := MixInLength(merkleRoot, 0)
	if listRoot != expected {
		t.Error("empty list hash tree root mismatch")
	}
}

func TestHashTreeRootContainerTwoFields(t *testing.T) {
	rootA := HashTreeRootUint64(100)
	rootB := HashTreeRootUint64(200)
	containerRoot := HashTreeRootContainer([][32]byte{rootA, rootB})
	expected := sha256Hash(append(rootA[:], rootB[:]...))
	if containerRoot != expected {
		t.Error("container hash tree root mismatch")
	}
}

func TestHashTreeRootByteListDeterministic(t *testing.T) {
	data := []byte("hello world")
	root := HashTreeRootByteList(data, 64)
	if root == [32]byte{} {
		t.Error("byte list root should not be zero for non-empty data")
	}

	// Same data, same max -> same root.
	root2 := HashTreeRootByteList(data, 64)
	if root != root2 {
		t.Error("byte list root should be deterministic")
	}

	// Different data -> different root.
	root3 := HashTreeRootByteList([]byte("goodbye"), 64)
	if root == root3 {
		t.Error("different data should produce different root")
	}
}

func TestHashTreeRootByteListEmpty(t *testing.T) {
	root := HashTreeRootByteList(nil, 32)
	var zeroChunkAndLength [64]byte
	if want := sha256.Sum256(zeroChunkAndLength[:]); root != want {
		t.Fatalf("empty byte list root = %x, want %x", root, want)
	}
}

func TestHashTreeRootBitvectorPacked(t *testing.T) {
	bits := []bool{true, false, true, true, false, false, true, false}
	root := HashTreeRootBitvector(bits)
	// Packed: 0x4d, padded to 32 bytes.
	var expected [32]byte
	expected[0] = 0x4d
	if root != expected {
		t.Errorf("bitvector root = %x, want %x", root, expected)
	}
}

func TestHashTreeRootBitlistDeterministic(t *testing.T) {
	bits := []bool{true, false, true}
	root := HashTreeRootBitlist(bits, 8)
	if root == [32]byte{} {
		t.Error("bitlist root should not be zero")
	}

	// Same bits -> same root.
	root2 := HashTreeRootBitlist(bits, 8)
	if root != root2 {
		t.Error("bitlist root should be deterministic")
	}
}

func TestHashTreeRootBitlistEmpty(t *testing.T) {
	root := HashTreeRootBitlist(nil, 8)
	var zeroChunkAndLength [64]byte
	if want := sha256.Sum256(zeroChunkAndLength[:]); root != want {
		t.Fatalf("empty bitlist root = %x, want %x", root, want)
	}
}

func TestHashTreeRootBasicVectorSingleChunk(t *testing.T) {
	// 4 uint64s = 32 bytes = 1 chunk.
	data := make([]byte, 0, 32)
	for i := uint64(1); i <= 4; i++ {
		data = append(data, MarshalUint64(i)...)
	}
	root := HashTreeRootBasicVector(data)
	var expected [32]byte
	copy(expected[:], data)
	if root != expected {
		t.Error("basic vector root mismatch")
	}
}

func TestHashTreeRootBasicVectorMultiChunk(t *testing.T) {
	// 8 uint64s = 64 bytes = 2 chunks.
	data := make([]byte, 0, 64)
	for i := uint64(1); i <= 8; i++ {
		data = append(data, MarshalUint64(i)...)
	}
	root := HashTreeRootBasicVector(data)
	// Build expected: hash(chunk0, chunk1).
	var c0, c1 [32]byte
	copy(c0[:], data[0:32])
	copy(c1[:], data[32:64])
	expected := sha256Hash(append(c0[:], c1[:]...))
	if root != expected {
		t.Error("multi-chunk basic vector root mismatch")
	}
}

func TestHashTreeRootBasicListWithMaxLen(t *testing.T) {
	// 2 uint64s with maxLen=8.
	data := append(MarshalUint64(100), MarshalUint64(200)...)
	root := HashTreeRootBasicList(data, 2, 8, 8)
	if root == [32]byte{} {
		t.Error("basic list root should not be zero")
	}
}

// --- zeroHashes tests ---

func TestZeroHashes(t *testing.T) {
	zh := zeroHashes(3)
	if len(zh) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(zh))
	}
	// zh[0] should be all zeros.
	if zh[0] != [32]byte{} {
		t.Error("zh[0] should be zero")
	}
	// zh[1] = hash(zero, zero)
	expected := sha256Hash(append(zh[0][:], zh[0][:]...))
	if zh[1] != expected {
		t.Error("zh[1] mismatch")
	}
	// zh[2] = hash(zh[1], zh[1])
	expected2 := sha256Hash(append(zh[1][:], zh[1][:]...))
	if zh[2] != expected2 {
		t.Error("zh[2] mismatch")
	}
}
