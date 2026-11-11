package pqc

import (
	"testing"
)

func TestSPHINCSSignerKeyGen(t *testing.T) {
	signer := NewSPHINCSSigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("key generation failed: %v", err)
	}
	if kp == nil {
		t.Fatal("key pair is nil")
	}
	if len(kp.PublicKey) != SPHINCSPubKeySize {
		t.Fatalf("public key size = %d, want %d", len(kp.PublicKey), SPHINCSPubKeySize)
	}
	if len(kp.SecretKey) != SPHINCSSecKeySize {
		t.Fatalf("secret key size = %d, want %d", len(kp.SecretKey), SPHINCSSecKeySize)
	}
	if kp.Algorithm != SPHINCSSHA256 {
		t.Fatalf("algorithm = %d, want %d", kp.Algorithm, SPHINCSSHA256)
	}
}

func TestSPHINCSSignerKeyGenUniqueness(t *testing.T) {
	signer := NewSPHINCSSigner()
	kp1, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("first key gen failed: %v", err)
	}
	kp2, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("second key gen failed: %v", err)
	}

	// Random key generation should produce different keys.
	if sphincsSignerBytesEqual(kp1.PublicKey, kp2.PublicKey) {
		t.Fatal("two random key generations produced identical public keys")
	}
	if sphincsSignerBytesEqual(kp1.SecretKey, kp2.SecretKey) {
		t.Fatal("two random key generations produced identical secret keys")
	}
}

func TestSPHINCSSignerSignAndVerify(t *testing.T) {
	signer := NewSPHINCSSigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("key generation failed: %v", err)
	}

	msg := []byte("SPHINCS+ post-quantum signing test")
	sig, err := signer.Sign(kp.SecretKey, msg)
	if err != nil {
		t.Fatalf("signing failed: %v", err)
	}
	if len(sig) != SPHINCSSignerSigSize {
		t.Fatalf("signature size = %d, want %d", len(sig), SPHINCSSignerSigSize)
	}

	if !signer.Verify(kp.PublicKey, msg, sig) {
		t.Fatal("valid signature rejected")
	}
}

func TestSPHINCSSignerVerifyWrongMessage(t *testing.T) {
	signer := NewSPHINCSSigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("key generation failed: %v", err)
	}

	msg := []byte("correct message")
	sig, err := signer.Sign(kp.SecretKey, msg)
	if err != nil {
		t.Fatalf("signing failed: %v", err)
	}

	// Verify with wrong message.
	if signer.Verify(kp.PublicKey, []byte("wrong message"), sig) {
		t.Fatal("signature verified for wrong message")
	}
}

func TestSPHINCSSignerNilInputs(t *testing.T) {
	signer := NewSPHINCSSigner()

	// Sign with nil key.
	_, err := signer.Sign(nil, []byte("msg"))
	if err == nil {
		t.Fatal("expected error for nil secret key")
	}

	// Sign with empty message.
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("key gen failed: %v", err)
	}
	_, err = signer.Sign(kp.SecretKey, nil)
	if err == nil {
		t.Fatal("expected error for nil message")
	}
	_, err = signer.Sign(kp.SecretKey, []byte{})
	if err == nil {
		t.Fatal("expected error for empty message")
	}

	// Verify with invalid inputs.
	if signer.Verify(nil, []byte("msg"), make([]byte, SPHINCSSignerSigSize)) {
		t.Fatal("verify with nil pk should fail")
	}
	if signer.Verify(kp.PublicKey, nil, make([]byte, SPHINCSSignerSigSize)) {
		t.Fatal("verify with nil msg should fail")
	}
	if signer.Verify(kp.PublicKey, []byte("msg"), nil) {
		t.Fatal("verify with nil sig should fail")
	}
}

func TestSPHINCSSignerAlgorithm(t *testing.T) {
	signer := NewSPHINCSSigner()
	if signer.Algorithm() != SPHINCSSHA256 {
		t.Fatalf("algorithm = %d, want %d", signer.Algorithm(), SPHINCSSHA256)
	}
}

func TestSPHINCSSignerSignShortKey(t *testing.T) {
	signer := NewSPHINCSSigner()
	_, err := signer.Sign([]byte{1, 2, 3}, []byte("msg"))
	if err == nil {
		t.Fatal("expected error for short secret key")
	}
}

func TestSPHINCSVerifyDetailed(t *testing.T) {
	// Bad public key.
	_, err := SPHINCSVerifyDetailed(SPHINCSPublicKey([]byte{1}), []byte("msg"), SPHINCSSignature([]byte{1}))
	if err == nil {
		t.Fatal("expected error for short pk")
	}

	// Empty message.
	pk := make([]byte, SPHINCSPubKeySize)
	_, err = SPHINCSVerifyDetailed(SPHINCSPublicKey(pk), nil, SPHINCSSignature([]byte{1}))
	if err == nil {
		t.Fatal("expected error for nil msg")
	}
}

func sphincsSignerBytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
