package bls

import (
	"math/big"
	"testing"
)

// testKZGSecret is the trusted setup secret used in tests (s = 42).
var testKZGSecret = big.NewInt(42)

// TestKZGVerifyProofSimple tests KZG proof verification with a simple polynomial.
// Polynomial: p(X) = 3X + 7
// p(s) = 3*42 + 7 = 133
// Evaluate at z = 5: p(5) = 3*5 + 7 = 22
func TestKZGCompressDecompressG1Generator(t *testing.T) {
	gen := BlsG1Generator()
	compressed := KZGCompressG1(gen)
	if len(compressed) != 48 {
		t.Fatalf("compressed length = %d, want 48", len(compressed))
	}

	// Compression flag must be set.
	if compressed[0]&0x80 == 0 {
		t.Fatal("compression flag not set")
	}

	// Infinity flag must not be set.
	if compressed[0]&0x40 != 0 {
		t.Fatal("infinity flag should not be set for generator")
	}

	recovered, err := KZGDecompressG1(compressed)
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}

	// Check recovered point matches generator.
	genX, genY := gen.blsG1ToAffine()
	recX, recY := recovered.blsG1ToAffine()
	if genX.Cmp(recX) != 0 || genY.Cmp(recY) != 0 {
		t.Fatal("round-trip compression/decompression failed for generator")
	}
}

// TestKZGCompressDecompressG1Infinity tests round-trip for infinity.
func TestKZGCompressDecompressG1Infinity(t *testing.T) {
	inf := BlsG1Infinity()
	compressed := KZGCompressG1(inf)
	if len(compressed) != 48 {
		t.Fatalf("compressed length = %d, want 48", len(compressed))
	}

	// Check flags.
	if compressed[0] != 0xc0 {
		t.Fatalf("infinity compressed byte[0] = 0x%02x, want 0xc0", compressed[0])
	}

	recovered, err := KZGDecompressG1(compressed)
	if err != nil {
		t.Fatalf("decompression failed: %v", err)
	}
	if !recovered.blsG1IsInfinity() {
		t.Fatal("decompressed point should be infinity")
	}
}

// TestKZGCompressDecompressG1ScalarMul tests round-trip for [k]G1.
func TestKZGCompressDecompressG1ScalarMul(t *testing.T) {
	scalars := []*big.Int{
		big.NewInt(1),
		big.NewInt(2),
		big.NewInt(42),
		big.NewInt(12345),
	}
	for _, k := range scalars {
		p := blsG1ScalarMul(BlsG1Generator(), k)
		compressed := KZGCompressG1(p)

		recovered, err := KZGDecompressG1(compressed)
		if err != nil {
			t.Fatalf("decompression failed for scalar %s: %v", k, err)
		}

		origX, origY := p.blsG1ToAffine()
		recX, recY := recovered.blsG1ToAffine()
		if origX.Cmp(recX) != 0 || origY.Cmp(recY) != 0 {
			t.Fatalf("round-trip failed for scalar %s", k)
		}
	}
}

// TestKZGDecompressG1Invalid tests invalid compressed inputs.
func TestKZGDecompressG1Invalid(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"wrong length", make([]byte, 47)},
		{"wrong length long", make([]byte, 49)},
		{"uncompressed flag", func() []byte {
			b := make([]byte, 48)
			b[0] = 0x00 // no compression flag
			return b
		}()},
		{"infinity with sort flag", func() []byte {
			b := make([]byte, 48)
			b[0] = 0xe0 // compression + infinity + sort
			return b
		}()},
		{"infinity with nonzero data", func() []byte {
			b := make([]byte, 48)
			b[0] = 0xc0 // compression + infinity
			b[47] = 0x01
			return b
		}()},
		{"x >= p", func() []byte {
			// Set x to p (which is too large).
			b := make([]byte, 48)
			b[0] = 0x80 // compression flag only
			pBytes := blsP.Bytes()
			copy(b[48-len(pBytes):], pBytes)
			// Re-set compression flag (might have been overwritten).
			b[0] |= 0x80
			return b
		}()},
	}

	for _, tt := range tests {
		_, err := KZGDecompressG1(tt.data)
		if err == nil {
			t.Errorf("%s: expected error, got nil", tt.name)
		}
	}
}

// TestKZGVerifyFromBytes tests the byte-level verification interface.
func TestKZGVerifyFromBytesInvalid(t *testing.T) {
	// Invalid commitment bytes.
	err := KZGVerifyFromBytes(make([]byte, 48), big.NewInt(0), big.NewInt(0), make([]byte, 48))
	if err == nil {
		t.Fatal("expected error for all-zero commitment (no compression flag)")
	}
}

// TestKZGVerifyFromBytesWrongProof tests that wrong proof fails byte verification.
