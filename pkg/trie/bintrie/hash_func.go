package bintrie

import (
	"crypto/sha256"

	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/zkvm"
	"lukechampine.com/blake3"
)

// TrieHasher abstracts the hash function used for the binary trie,
// allowing pluggable hash backends (SHA-256, BLAKE3, Poseidon2, Keccak).
type TrieHasher interface {
	// Hash computes a 32-byte hash of arbitrary data.
	Hash(data []byte) [32]byte
	// HashPair computes a 32-byte hash of two concatenated 32-byte hashes.
	HashPair(left, right [32]byte) [32]byte
	// Name returns the hasher's human-readable name.
	Name() string
	// ProvingOverhead returns the relative ZK proving cost. Lower is better
	// for circuit-friendly hashes (Poseidon2 ~10, BLAKE3 ~100, SHA-256 ~300,
	// Keccak ~1000).
	ProvingOverhead() float64
}

// Hasher ID constants for wire-format identification.
const (
	HasherSHA256    uint8 = 0
	HasherBLAKE3    uint8 = 1
	HasherPoseidon2 uint8 = 2
	HasherKeccak    uint8 = 3
)

// HasherByID returns a TrieHasher for the given identifier.
// Returns nil for unknown IDs.
func HasherByID(id uint8) TrieHasher {
	switch id {
	case HasherSHA256:
		return SHA256Hasher{}
	case HasherBLAKE3:
		return Blake3Hasher{}
	case HasherPoseidon2:
		return Poseidon2Hasher{}
	case HasherKeccak:
		return KeccakHasher{}
	default:
		return nil
	}
}

// HasherIDFor returns the wire-format ID for the given hasher.
// Defaults to HasherSHA256 for unknown hashers.
func HasherIDFor(h TrieHasher) uint8 {
	switch h.(type) {
	case SHA256Hasher:
		return HasherSHA256
	case Blake3Hasher:
		return HasherBLAKE3
	case Poseidon2Hasher:
		return HasherPoseidon2
	case KeccakHasher:
		return HasherKeccak
	default:
		return HasherSHA256
	}
}

// SHA256Hasher implements TrieHasher using crypto/sha256.
type SHA256Hasher struct{}

func (SHA256Hasher) Hash(data []byte) [32]byte {
	return sha256.Sum256(data)
}

func (SHA256Hasher) HashPair(left, right [32]byte) [32]byte {
	h := sha256.New()
	h.Write(left[:])
	h.Write(right[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func (SHA256Hasher) Name() string        { return "SHA-256" }
func (SHA256Hasher) ProvingOverhead() float64 { return 300 }

// Blake3Hasher implements TrieHasher using BLAKE3.
type Blake3Hasher struct{}

func (Blake3Hasher) Hash(data []byte) [32]byte {
	return blake3.Sum256(data)
}

func (Blake3Hasher) HashPair(left, right [32]byte) [32]byte {
	h := blake3.New(32, nil)
	h.Write(left[:])
	h.Write(right[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func (Blake3Hasher) Name() string        { return "BLAKE3" }
func (Blake3Hasher) ProvingOverhead() float64 { return 100 }

// Poseidon2Hasher implements TrieHasher using the Poseidon2 hash from
// the zkvm package, optimized for ZK proving circuits.
type Poseidon2Hasher struct{}

func (Poseidon2Hasher) Hash(data []byte) [32]byte {
	return zkvm.Poseidon2HashBytes(data)
}

func (Poseidon2Hasher) HashPair(left, right [32]byte) [32]byte {
	var buf [64]byte
	copy(buf[:32], left[:])
	copy(buf[32:], right[:])
	return zkvm.Poseidon2HashBytes(buf[:])
}

func (Poseidon2Hasher) Name() string        { return "Poseidon2" }
func (Poseidon2Hasher) ProvingOverhead() float64 { return 10 }

// KeccakHasher implements TrieHasher using Keccak-256.
type KeccakHasher struct{}

func (KeccakHasher) Hash(data []byte) [32]byte {
	var out [32]byte
	copy(out[:], crypto.Keccak256(data))
	return out
}

func (KeccakHasher) HashPair(left, right [32]byte) [32]byte {
	var out [32]byte
	copy(out[:], crypto.Keccak256(left[:], right[:]))
	return out
}

func (KeccakHasher) Name() string        { return "Keccak-256" }
func (KeccakHasher) ProvingOverhead() float64 { return 1000 }
