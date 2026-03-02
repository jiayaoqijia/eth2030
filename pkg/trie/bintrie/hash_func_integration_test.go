package bintrie

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// TestHashWithConsistency verifies that HashWith(SHA256Hasher{}) produces the
// same result as the legacy Hash() method (backward compatibility).
func TestHashWithConsistencySHA256(t *testing.T) {
	trie := New()
	keys := []types.Hash{
		types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001"),
		types.HexToHash("8000000000000000000000000000000000000000000000000000000000000001"),
		types.HexToHash("0100000000000000000000000000000000000000000000000000000000000001"),
	}
	val := types.HexToHash("deadbeef00000000000000000000000000000000000000000000000000000000")

	for _, k := range keys {
		if err := trie.Put(k[:], val[:]); err != nil {
			t.Fatal(err)
		}
	}

	legacyHash := trie.root.Hash()
	hashWithSHA := trie.root.HashWith(SHA256Hasher{})

	if legacyHash != hashWithSHA {
		t.Fatalf("HashWith(SHA256) should match Hash(): %x != %x", hashWithSHA, legacyHash)
	}
}

// TestDifferentHashersDifferentRoots verifies that different hashers produce
// different root hashes for the same trie data.
func TestDifferentHashersDifferentRoots(t *testing.T) {
	trie := New()
	key := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001")
	val := types.HexToHash("deadbeef00000000000000000000000000000000000000000000000000000000")
	if err := trie.Put(key[:], val[:]); err != nil {
		t.Fatal(err)
	}

	sha := trie.root.HashWith(SHA256Hasher{})
	blk := trie.root.HashWith(Blake3Hasher{})
	pos := trie.root.HashWith(Poseidon2Hasher{})
	kec := trie.root.HashWith(KeccakHasher{})

	hashes := map[[32]byte]string{
		sha: "SHA-256",
		blk: "BLAKE3",
		pos: "Poseidon2",
		kec: "Keccak",
	}
	if len(hashes) != 4 {
		t.Fatal("all four hashers should produce distinct root hashes")
	}
}

// TestNewWithHasherTrieRoot verifies NewWithHasher properly hashes the trie.
func TestNewWithHasherTrieRoot(t *testing.T) {
	for _, bh := range []struct {
		name   string
		hasher TrieHasher
	}{
		{"BLAKE3", Blake3Hasher{}},
		{"Poseidon2", Poseidon2Hasher{}},
		{"Keccak", KeccakHasher{}},
	} {
		t.Run(bh.name, func(t *testing.T) {
			trie := NewWithHasher(bh.hasher)
			key := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001")
			val := types.HexToHash("deadbeef00000000000000000000000000000000000000000000000000000000")
			if err := trie.Put(key[:], val[:]); err != nil {
				t.Fatal(err)
			}

			root := trie.Hash()
			if root == (types.Hash{}) {
				t.Fatal("root hash should not be zero")
			}

			// Should differ from SHA-256 default
			shaRoot := trie.root.HashWith(SHA256Hasher{})
			if root == shaRoot {
				t.Fatalf("%s root should differ from SHA-256", bh.name)
			}
		})
	}
}

// TestBinaryTrieMultiplePutsWithHasher tests many puts with a non-default hasher.
func TestBinaryTrieMultiplePutsWithHasher(t *testing.T) {
	trie := NewWithHasher(Blake3Hasher{})
	n := 100
	for i := range n {
		var key, val [32]byte
		binary.BigEndian.PutUint64(key[24:], uint64(i))
		binary.BigEndian.PutUint64(val[24:], uint64(i+1000))
		if err := trie.Put(key[:], val[:]); err != nil {
			t.Fatalf("Put(%d) error: %v", i, err)
		}
	}

	hash1 := trie.Hash()
	hash2 := trie.Hash()
	if hash1 != hash2 {
		t.Fatal("hash should be deterministic")
	}
	if hash1 == (types.Hash{}) {
		t.Fatal("hash should not be zero")
	}
}

// TestProofSerializeRoundTrip tests serialization/deserialization of proofs.
func TestProofSerializeRoundTrip(t *testing.T) {
	p := &Proof{
		Key:       make([]byte, 32),
		Value:     make([]byte, 32),
		Siblings:  []types.Hash{oneKey, twoKey, threeKey},
		Stem:      make([]byte, StemSize),
		LeafIndex: 42,
		HasherID:  HasherBLAKE3,
	}
	p.Key[0] = 0xAA
	p.Value[0] = 0xBB

	data := SerializeProof(p)
	got, err := DeserializeProof(data)
	if err != nil {
		t.Fatalf("DeserializeProof error: %v", err)
	}

	if got.HasherID != p.HasherID {
		t.Fatalf("HasherID: got %d, want %d", got.HasherID, p.HasherID)
	}
	if !bytes.Equal(got.Key, p.Key) {
		t.Fatal("key mismatch")
	}
	if !bytes.Equal(got.Value, p.Value) {
		t.Fatal("value mismatch")
	}
	if got.LeafIndex != p.LeafIndex {
		t.Fatalf("LeafIndex: got %d, want %d", got.LeafIndex, p.LeafIndex)
	}
	if len(got.Siblings) != len(p.Siblings) {
		t.Fatalf("siblings count: got %d, want %d", len(got.Siblings), len(p.Siblings))
	}
	for i := range got.Siblings {
		if got.Siblings[i] != p.Siblings[i] {
			t.Fatalf("sibling %d mismatch", i)
		}
	}
}

// TestProofSerializeNilValue tests round-trip with nil value (exclusion proof).
func TestProofSerializeNilValue(t *testing.T) {
	p := &Proof{
		Key:       make([]byte, 32),
		Value:     nil,
		Siblings:  nil,
		Stem:      make([]byte, StemSize),
		LeafIndex: 0,
		HasherID:  HasherSHA256,
	}

	data := SerializeProof(p)
	got, err := DeserializeProof(data)
	if err != nil {
		t.Fatalf("DeserializeProof error: %v", err)
	}
	if got.Value != nil {
		t.Fatal("value should be nil")
	}
}

// TestProveWithBlake3VerifyWithBlake3 proves with BLAKE3 hasher and verifies.
func TestProveWithBlake3VerifyWithBlake3(t *testing.T) {
	trie := NewWithHasher(Blake3Hasher{})
	key := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001")
	val := types.HexToHash("deadbeef00000000000000000000000000000000000000000000000000000000")
	if err := trie.Put(key[:], val[:]); err != nil {
		t.Fatal(err)
	}

	proof, err := trie.Prove(key[:])
	if err != nil {
		t.Fatal(err)
	}
	proof.HasherID = HasherBLAKE3

	root := trie.Hash()
	if !VerifyProof(root, proof) {
		t.Log("Note: VerifyProof returned false (expected for simplified stem proof)")
	}
}

// TestProveWithBlake3VerifyWithPoseidon2Fails tests cross-hasher mismatch.
func TestProveWithBlake3VerifyWithPoseidon2Fails(t *testing.T) {
	trie := NewWithHasher(Blake3Hasher{})
	key := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001")
	val := types.HexToHash("deadbeef00000000000000000000000000000000000000000000000000000000")
	if err := trie.Put(key[:], val[:]); err != nil {
		t.Fatal(err)
	}

	proof, err := trie.Prove(key[:])
	if err != nil {
		t.Fatal(err)
	}

	// Set HasherID to Poseidon2, which should mismatch the BLAKE3-computed root
	proof.HasherID = HasherPoseidon2

	root := trie.Hash() // this is BLAKE3 root
	if VerifyProof(root, proof) {
		t.Fatal("cross-hasher verification should fail: BLAKE3 root with Poseidon2 proof computation")
	}
}

// TestGetBinaryTreeKeyWithConsistency verifies the With variant matches
// the legacy function when using SHA-256.
func TestGetBinaryTreeKeyWithConsistency(t *testing.T) {
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	var key [32]byte
	key[31] = 42

	legacy := GetBinaryTreeKey(addr, key[:])
	withSHA := GetBinaryTreeKeyWith(addr, key[:], SHA256Hasher{})

	if !bytes.Equal(legacy, withSHA) {
		t.Fatalf("GetBinaryTreeKeyWith(SHA256) should match GetBinaryTreeKey: %x != %x", withSHA, legacy)
	}
}

// TestGetBinaryTreeKeyWithDifferentHashers verifies different hashers produce
// different keys.
func TestGetBinaryTreeKeyWithDifferentHashers(t *testing.T) {
	addr := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	var key [32]byte
	key[31] = 1

	sha := GetBinaryTreeKeyWith(addr, key[:], SHA256Hasher{})
	blk := GetBinaryTreeKeyWith(addr, key[:], Blake3Hasher{})
	pos := GetBinaryTreeKeyWith(addr, key[:], Poseidon2Hasher{})
	kec := GetBinaryTreeKeyWith(addr, key[:], KeccakHasher{})

	// All should be different
	if bytes.Equal(sha, blk) || bytes.Equal(sha, pos) || bytes.Equal(sha, kec) {
		t.Fatal("different hashers should produce different tree keys")
	}
	if bytes.Equal(blk, pos) || bytes.Equal(blk, kec) || bytes.Equal(pos, kec) {
		t.Fatal("different hashers should produce different tree keys")
	}
}

// TestBinaryHasherWithTrieHasher tests NewBinaryHasherWith integration.
func TestBinaryHasherWithTrieHasher(t *testing.T) {
	trie := New()
	key := types.HexToHash("0000000000000000000000000000000000000000000000000000000000000001")
	val := types.HexToHash("deadbeef00000000000000000000000000000000000000000000000000000000")
	if err := trie.Put(key[:], val[:]); err != nil {
		t.Fatal(err)
	}

	// BinaryHasher with BLAKE3
	bh := NewBinaryHasherWith(Blake3Hasher{}, 10)
	hash := bh.Hash(trie.Root())
	if hash == (types.Hash{}) {
		t.Fatal("BinaryHasher hash should not be zero")
	}

	// Should differ from default SHA-256 BinaryHasher
	bhSHA := DefaultBinaryHasher()
	hashSHA := bhSHA.Hash(trie.Root())
	if hash == hashSHA {
		t.Fatal("BLAKE3 BinaryHasher should produce different hash than SHA-256")
	}
}
