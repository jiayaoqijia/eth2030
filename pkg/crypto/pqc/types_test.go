package pqc

import (
	"testing"
)

func TestPQAlgorithmConstants(t *testing.T) {
	// Verify algorithm enum values.
	if DILITHIUM3 != 0 {
		t.Errorf("DILITHIUM3 = %d, want 0", DILITHIUM3)
	}
	if FALCON512 != 1 {
		t.Errorf("FALCON512 = %d, want 1", FALCON512)
	}
	if SPHINCSSHA256 != 2 {
		t.Errorf("SPHINCSSHA256 = %d, want 2", SPHINCSSHA256)
	}
}

func TestDilithium3SizeConstants(t *testing.T) {
	if Dilithium3PubKeySize != 1952 {
		t.Errorf("Dilithium3PubKeySize = %d, want 1952", Dilithium3PubKeySize)
	}
	if Dilithium3SecKeySize != 4032 {
		t.Errorf("Dilithium3SecKeySize = %d, want 4032", Dilithium3SecKeySize)
	}
	if Dilithium3SigSize != 3309 {
		t.Errorf("Dilithium3SigSize = %d, want 3309", Dilithium3SigSize)
	}
}

func TestFalcon512SizeConstants(t *testing.T) {
	if Falcon512PubKeySize != 897 {
		t.Errorf("Falcon512PubKeySize = %d, want 897", Falcon512PubKeySize)
	}
	if Falcon512SecKeySize != 1345 {
		t.Errorf("Falcon512SecKeySize = %d, want 1345", Falcon512SecKeySize)
	}
	if Falcon512SigSize != 666 {
		t.Errorf("Falcon512SigSize = %d, want 666", Falcon512SigSize)
	}
}

func TestSPHINCSsha256SizeConstants(t *testing.T) {
	if SPHINCSSha256PubKeySize != 32 {
		t.Errorf("SPHINCSSha256PubKeySize = %d, want 32", SPHINCSSha256PubKeySize)
	}
	if SPHINCSSha256SecKeySize != 64 {
		t.Errorf("SPHINCSSha256SecKeySize = %d, want 64", SPHINCSSha256SecKeySize)
	}
	if SPHINCSSha256SigSize != 17088 {
		t.Errorf("SPHINCSSha256SigSize = %d, want 17088", SPHINCSSha256SigSize)
	}
}

func TestPubKeySizeAllAlgorithms(t *testing.T) {
	tests := []struct {
		alg  PQAlgorithm
		want int
	}{
		{DILITHIUM3, DSign3PubKeyBytes},
		{FALCON512, 897},
		{SPHINCSSHA256, 32},
	}
	for _, tt := range tests {
		got := PubKeySize(tt.alg)
		if got != tt.want {
			t.Errorf("PubKeySize(%d) = %d, want %d", tt.alg, got, tt.want)
		}
	}
}

func TestPubKeySizeUnknown(t *testing.T) {
	for _, alg := range []PQAlgorithm{PQAlgorithm(3), PQAlgorithm(100), PQAlgorithm(255)} {
		if got := PubKeySize(alg); got != 0 {
			t.Errorf("PubKeySize(%d) = %d, want 0", alg, got)
		}
	}
}

func TestSecKeySizeAllAlgorithms(t *testing.T) {
	tests := []struct {
		alg  PQAlgorithm
		want int
	}{
		{DILITHIUM3, DSign3SecKeyBytes},
		{FALCON512, 1345},
		{SPHINCSSHA256, 64},
	}
	for _, tt := range tests {
		got := SecKeySize(tt.alg)
		if got != tt.want {
			t.Errorf("SecKeySize(%d) = %d, want %d", tt.alg, got, tt.want)
		}
	}
}

func TestSecKeySizeUnknown(t *testing.T) {
	for _, alg := range []PQAlgorithm{PQAlgorithm(3), PQAlgorithm(100), PQAlgorithm(255)} {
		if got := SecKeySize(alg); got != 0 {
			t.Errorf("SecKeySize(%d) = %d, want 0", alg, got)
		}
	}
}

func TestSigSizeAllAlgorithms(t *testing.T) {
	tests := []struct {
		alg  PQAlgorithm
		want int
	}{
		{DILITHIUM3, DSign3SigBytes},
		{FALCON512, 666},
		{SPHINCSSHA256, 17088},
	}
	for _, tt := range tests {
		got := SigSize(tt.alg)
		if got != tt.want {
			t.Errorf("SigSize(%d) = %d, want %d", tt.alg, got, tt.want)
		}
	}
}

func TestSigSizeUnknown(t *testing.T) {
	for _, alg := range []PQAlgorithm{PQAlgorithm(3), PQAlgorithm(100), PQAlgorithm(255)} {
		if got := SigSize(alg); got != 0 {
			t.Errorf("SigSize(%d) = %d, want 0", alg, got)
		}
	}
}

func TestPQKeyPairStruct(t *testing.T) {
	kp := PQKeyPair{
		Algorithm: DILITHIUM3,
		PublicKey: []byte{0x01, 0x02, 0x03},
		SecretKey: []byte{0x04, 0x05, 0x06},
	}

	if kp.Algorithm != DILITHIUM3 {
		t.Errorf("Algorithm = %d, want %d", kp.Algorithm, DILITHIUM3)
	}
	if len(kp.PublicKey) != 3 {
		t.Errorf("PublicKey len = %d, want 3", len(kp.PublicKey))
	}
	if len(kp.SecretKey) != 3 {
		t.Errorf("SecretKey len = %d, want 3", len(kp.SecretKey))
	}
}

func TestPQSignatureStruct(t *testing.T) {
	sig := PQSignature{
		Algorithm: FALCON512,
		PublicKey: []byte{0x10},
		Signature: []byte{0x20, 0x30},
	}

	if sig.Algorithm != FALCON512 {
		t.Errorf("Algorithm = %d, want %d", sig.Algorithm, FALCON512)
	}
	if len(sig.PublicKey) != 1 {
		t.Errorf("PublicKey len = %d, want 1", len(sig.PublicKey))
	}
	if len(sig.Signature) != 2 {
		t.Errorf("Signature len = %d, want 2", len(sig.Signature))
	}
}

func TestErrorVariables(t *testing.T) {
	// Verify errors are non-nil and have distinct messages.
	errors := []error{
		ErrUnknownAlgorithm,
		ErrInvalidKeySize,
		ErrInvalidSigSize,
		ErrVerifyFailed,
	}

	for _, e := range errors {
		if e == nil {
			t.Fatal("error variable is nil")
		}
		if e.Error() == "" {
			t.Error("error message is empty")
		}
	}

	// Check that error messages have the "pqc:" prefix.
	msgs := map[string]error{
		"pqc: unknown algorithm":      ErrUnknownAlgorithm,
		"pqc: invalid key size":       ErrInvalidKeySize,
		"pqc: invalid signature size": ErrInvalidSigSize,
		"pqc: verification failed":    ErrVerifyFailed,
	}
	for expected, e := range msgs {
		if e.Error() != expected {
			t.Errorf("error message = %q, want %q", e.Error(), expected)
		}
	}
}

func TestPQAlgorithmDistinct(t *testing.T) {
	// Verify all algorithm values are distinct.
	algs := []PQAlgorithm{DILITHIUM3, FALCON512, SPHINCSSHA256}
	seen := make(map[PQAlgorithm]bool)
	for _, a := range algs {
		if seen[a] {
			t.Errorf("duplicate algorithm value: %d", a)
		}
		seen[a] = true
	}
}

func TestSizeFunctionsConsistency(t *testing.T) {
	// All known algorithms should return non-zero sizes for all three functions.
	for _, alg := range []PQAlgorithm{DILITHIUM3, FALCON512, SPHINCSSHA256} {
		if PubKeySize(alg) == 0 {
			t.Errorf("PubKeySize(%d) = 0", alg)
		}
		if SecKeySize(alg) == 0 {
			t.Errorf("SecKeySize(%d) = 0", alg)
		}
		if SigSize(alg) == 0 {
			t.Errorf("SigSize(%d) = 0", alg)
		}
	}
}
