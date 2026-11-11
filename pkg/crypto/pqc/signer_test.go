package pqc

import (
	"testing"
)

func TestGetSignerDilithium(t *testing.T) {
	s := GetSigner(DILITHIUM3)
	if s == nil {
		t.Fatal("GetSigner(DILITHIUM3) returned nil")
	}
	if s.Algorithm() != DILITHIUM3 {
		t.Errorf("Algorithm() = %d, want %d", s.Algorithm(), DILITHIUM3)
	}
	if _, ok := s.(*DilithiumSigner); !ok {
		t.Error("expected *DilithiumSigner type")
	}
}

func TestGetSignerFalcon(t *testing.T) {
	s := GetSigner(FALCON512)
	if s == nil {
		t.Fatal("GetSigner(FALCON512) returned nil")
	}
	if s.Algorithm() != FALCON512 {
		t.Errorf("Algorithm() = %d, want %d", s.Algorithm(), FALCON512)
	}
	if _, ok := s.(*FalconSigner); !ok {
		t.Error("expected *FalconSigner type")
	}
}

func TestGetSignerSPHINCS(t *testing.T) {
	s := GetSigner(SPHINCSSHA256)
	if s == nil {
		t.Error("GetSigner(SPHINCSSHA256) returned nil")
	}
}

func TestGetSignerUnknownAlgorithm(t *testing.T) {
	for _, alg := range []PQAlgorithm{PQAlgorithm(3), PQAlgorithm(99), PQAlgorithm(255)} {
		s := GetSigner(alg)
		if s != nil {
			t.Errorf("GetSigner(%d) should return nil for unknown algorithm", alg)
		}
	}
}

func TestPQSignerInterfaceCompliance(t *testing.T) {
	// Verify both concrete signers implement the PQSigner interface.
	var _ PQSigner = (*DilithiumSigner)(nil)
	var _ PQSigner = (*FalconSigner)(nil)
}

func TestGetSignerSignVerifyRoundTrip(t *testing.T) {
	algorithms := []PQAlgorithm{DILITHIUM3, FALCON512}

	for _, alg := range algorithms {
		signer := GetSigner(alg)
		if signer == nil {
			t.Fatalf("GetSigner(%d) returned nil", alg)
		}

		kp, err := signer.GenerateKey()
		if err != nil {
			t.Fatalf("alg %d: GenerateKey: %v", alg, err)
		}

		msg := []byte("round-trip test via GetSigner")
		sig, err := signer.Sign(kp.SecretKey, msg)
		if err != nil {
			t.Fatalf("alg %d: Sign: %v", alg, err)
		}

		if !signer.Verify(kp.PublicKey, msg, sig) {
			t.Errorf("alg %d: valid signature rejected", alg)
		}
	}
}

func TestGetSignerKeyPairSizeConsistency(t *testing.T) {
	algorithms := []PQAlgorithm{DILITHIUM3, FALCON512}

	for _, alg := range algorithms {
		signer := GetSigner(alg)
		if signer == nil {
			t.Fatalf("GetSigner(%d) returned nil", alg)
		}

		kp, err := signer.GenerateKey()
		if err != nil {
			t.Fatalf("alg %d: GenerateKey: %v", alg, err)
		}

		if len(kp.PublicKey) != PubKeySize(alg) {
			t.Errorf("alg %d: pubkey size = %d, want %d", alg, len(kp.PublicKey), PubKeySize(alg))
		}
		if len(kp.SecretKey) != SecKeySize(alg) {
			t.Errorf("alg %d: seckey size = %d, want %d", alg, len(kp.SecretKey), SecKeySize(alg))
		}
	}
}

func TestGetSignerSignatureSizeConsistency(t *testing.T) {
	algorithms := []PQAlgorithm{DILITHIUM3, FALCON512}

	for _, alg := range algorithms {
		signer := GetSigner(alg)
		if signer == nil {
			t.Fatalf("GetSigner(%d) returned nil", alg)
		}

		kp, _ := signer.GenerateKey()
		sig, err := signer.Sign(kp.SecretKey, []byte("sig size test"))
		if err != nil {
			t.Fatalf("alg %d: Sign: %v", alg, err)
		}

		if len(sig) != SigSize(alg) {
			t.Errorf("alg %d: sig size = %d, want %d", alg, len(sig), SigSize(alg))
		}
	}
}

func TestGetSignerReturnsCorrectTypes(t *testing.T) {
	s1 := GetSigner(DILITHIUM3)
	s2 := GetSigner(FALCON512)

	if s1 == nil || s2 == nil {
		t.Fatal("GetSigner returned nil")
	}

	if _, ok := s1.(*DilithiumSigner); !ok {
		t.Error("DILITHIUM3 signer should be *DilithiumSigner")
	}
	if _, ok := s2.(*FalconSigner); !ok {
		t.Error("FALCON512 signer should be *FalconSigner")
	}
}
