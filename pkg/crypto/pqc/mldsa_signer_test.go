package pqc

import (
	"testing"
)

func TestMLDSASignerKeyGen(t *testing.T) {
	signer := NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	if len(kp.PublicKey) != MLDSAPublicKeySize {
		t.Errorf("public key size = %d, want %d", len(kp.PublicKey), MLDSAPublicKeySize)
	}
	if len(kp.SecretKey) != MLDSAPrivateKeySize {
		t.Errorf("secret key size = %d, want %d", len(kp.SecretKey), MLDSAPrivateKeySize)
	}
}

func TestMLDSASignerKeyGenUniqueness(t *testing.T) {
	signer := NewMLDSASigner()
	kp1, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey 1 failed: %v", err)
	}
	kp2, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey 2 failed: %v", err)
	}
	// Keys should be different (random seed).
	same := true
	for i := range kp1.PublicKey {
		if kp1.PublicKey[i] != kp2.PublicKey[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("two generated key pairs should not be identical")
	}
}

func TestMLDSASignerSignAndVerify(t *testing.T) {
	signer := NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	msg := []byte("test message for ML-DSA-65 signing")
	sig, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	if len(sig) != MLDSASignatureSize {
		t.Errorf("signature size = %d, want %d", len(sig), MLDSASignatureSize)
	}

	// Verify the signature.
	if !signer.Verify(kp.PublicKey, msg, sig) {
		t.Error("Verify returned false for valid signature")
	}
}

func TestMLDSASignerVerifyWrongMessage(t *testing.T) {
	signer := NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	msg := []byte("original message")
	sig, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	wrongMsg := []byte("different message")
	if signer.Verify(kp.PublicKey, wrongMsg, sig) {
		t.Error("Verify should return false for wrong message")
	}
}

func TestMLDSASignerVerifyWrongKey(t *testing.T) {
	signer := NewMLDSASigner()
	kp1, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey 1 failed: %v", err)
	}
	kp2, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey 2 failed: %v", err)
	}

	msg := []byte("test message")
	sig, err := signer.Sign(kp1, msg)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Verify with wrong public key should fail.
	if signer.Verify(kp2.PublicKey, msg, sig) {
		t.Error("Verify should return false for wrong public key")
	}
}

func TestMLDSASignerNilKeyErrors(t *testing.T) {
	signer := NewMLDSASigner()

	_, err := signer.Sign(nil, []byte("msg"))
	if err != ErrMLDSANilKey {
		t.Errorf("Sign(nil key) error = %v, want ErrMLDSANilKey", err)
	}

	kp, _ := signer.GenerateKey()
	_, err = signer.Sign(kp, nil)
	if err != ErrMLDSAEmptyMsg {
		t.Errorf("Sign(nil msg) error = %v, want ErrMLDSAEmptyMsg", err)
	}

	_, err = signer.Sign(kp, []byte{})
	if err != ErrMLDSAEmptyMsg {
		t.Errorf("Sign(empty msg) error = %v, want ErrMLDSAEmptyMsg", err)
	}
}

func TestMLDSASignerVerifyBadInputs(t *testing.T) {
	signer := NewMLDSASigner()

	// Empty inputs.
	if signer.Verify(nil, []byte("msg"), make([]byte, MLDSASignatureSize)) {
		t.Error("Verify should reject nil pubkey")
	}
	if signer.Verify(make([]byte, MLDSAPublicKeySize), nil, make([]byte, MLDSASignatureSize)) {
		t.Error("Verify should reject nil message")
	}
	if signer.Verify(make([]byte, MLDSAPublicKeySize), []byte("msg"), nil) {
		t.Error("Verify should reject nil signature")
	}

	// Wrong sizes.
	if signer.Verify(make([]byte, 10), []byte("msg"), make([]byte, MLDSASignatureSize)) {
		t.Error("Verify should reject short pubkey")
	}
	if signer.Verify(make([]byte, MLDSAPublicKeySize), []byte("msg"), make([]byte, 10)) {
		t.Error("Verify should reject short signature")
	}
}

func TestMLDSASignerDeterministicSigning(t *testing.T) {
	signer := NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	msg := []byte("deterministic test")
	sig1, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign 1 failed: %v", err)
	}
	sig2, err := signer.Sign(kp, msg)
	if err != nil {
		t.Fatalf("Sign 2 failed: %v", err)
	}

	// Both signatures should verify.
	if !signer.Verify(kp.PublicKey, msg, sig1) {
		t.Error("sig1 verification failed")
	}
	if !signer.Verify(kp.PublicKey, msg, sig2) {
		t.Error("sig2 verification failed")
	}
}

func TestMLDSASignerAlgorithm(t *testing.T) {
	signer := NewMLDSASigner()
	if signer.Algorithm() != DILITHIUM3 {
		t.Errorf("Algorithm() = %d, want DILITHIUM3 (%d)", signer.Algorithm(), DILITHIUM3)
	}
}

func TestMLDSASignerConstants(t *testing.T) {
	if MLDSAPublicKeySize != 1952 || MLDSAPrivateKeySize != 4032 || MLDSASignatureSize != 3309 {
		t.Fatalf("unexpected ML-DSA-65 sizes: %d %d %d", MLDSAPublicKeySize, MLDSAPrivateKeySize, MLDSASignatureSize)
	}
}
