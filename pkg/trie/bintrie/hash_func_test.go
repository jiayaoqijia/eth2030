package bintrie

import (
	"testing"
)

func TestAllHashersProducce32ByteOutput(t *testing.T) {
	hashers := []TrieHasher{
		SHA256Hasher{},
		Blake3Hasher{},
		Poseidon2Hasher{},
		KeccakHasher{},
	}
	data := []byte("binary trie test data")

	for _, h := range hashers {
		out := h.Hash(data)
		if len(out) != 32 {
			t.Fatalf("%s: Hash output length = %d, want 32", h.Name(), len(out))
		}
	}
}

func TestHashPairNonCommutative(t *testing.T) {
	hashers := []TrieHasher{
		SHA256Hasher{},
		Blake3Hasher{},
		Poseidon2Hasher{},
		KeccakHasher{},
	}

	var a, b [32]byte
	for i := range a {
		a[i] = byte(i)
	}
	for i := range b {
		b[i] = byte(255 - i)
	}

	for _, h := range hashers {
		ab := h.HashPair(a, b)
		ba := h.HashPair(b, a)
		if ab == ba {
			t.Fatalf("%s: HashPair(a,b) == HashPair(b,a) — should not be commutative", h.Name())
		}
	}
}

func TestHashDeterministicAllHashers(t *testing.T) {
	hashers := []TrieHasher{
		SHA256Hasher{},
		Blake3Hasher{},
		Poseidon2Hasher{},
		KeccakHasher{},
	}
	data := []byte("deterministic test input")

	for _, h := range hashers {
		h1 := h.Hash(data)
		h2 := h.Hash(data)
		if h1 != h2 {
			t.Fatalf("%s: Hash not deterministic", h.Name())
		}
	}
}

func TestHashPairDeterministic(t *testing.T) {
	hashers := []TrieHasher{
		SHA256Hasher{},
		Blake3Hasher{},
		Poseidon2Hasher{},
		KeccakHasher{},
	}

	var a, b [32]byte
	a[0] = 0xAA
	b[0] = 0xBB

	for _, h := range hashers {
		r1 := h.HashPair(a, b)
		r2 := h.HashPair(a, b)
		if r1 != r2 {
			t.Fatalf("%s: HashPair not deterministic", h.Name())
		}
	}
}

func TestProvingOverheadValues(t *testing.T) {
	tests := []struct {
		hasher   TrieHasher
		expected float64
	}{
		{SHA256Hasher{}, 300},
		{Blake3Hasher{}, 100},
		{Poseidon2Hasher{}, 10},
		{KeccakHasher{}, 1000},
	}
	for _, tt := range tests {
		if got := tt.hasher.ProvingOverhead(); got != tt.expected {
			t.Fatalf("%s: ProvingOverhead() = %v, want %v", tt.hasher.Name(), got, tt.expected)
		}
	}
}

func TestHasherByIDRoundTrip(t *testing.T) {
	ids := []uint8{HasherSHA256, HasherBLAKE3, HasherPoseidon2, HasherKeccak}
	for _, id := range ids {
		h := HasherByID(id)
		if h == nil {
			t.Fatalf("HasherByID(%d) returned nil", id)
		}
		gotID := HasherIDFor(h)
		if gotID != id {
			t.Fatalf("HasherIDFor(HasherByID(%d)) = %d", id, gotID)
		}
	}
}

func TestHasherByIDUnknown(t *testing.T) {
	if HasherByID(99) != nil {
		t.Fatal("HasherByID(99) should return nil")
	}
}

func TestDifferentHashersProduceDifferentOutputs(t *testing.T) {
	hashers := []TrieHasher{
		SHA256Hasher{},
		Blake3Hasher{},
		Poseidon2Hasher{},
		KeccakHasher{},
	}
	data := []byte("cross-hasher distinction test")

	results := make(map[[32]byte]string)
	for _, h := range hashers {
		out := h.Hash(data)
		if prev, ok := results[out]; ok {
			t.Fatalf("%s and %s produced identical hash", prev, h.Name())
		}
		results[out] = h.Name()
	}
}
