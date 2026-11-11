package pqc

import (
	"testing"
)

func TestGenerateDilithiumKey(t *testing.T) {
	kp, err := GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey failed: %v", err)
	}
	if kp == nil {
		t.Fatal("GenerateDilithiumKey returned nil")
	}
	if len(kp.SecretKey) != MLDSAPrivateKeySize {
		t.Errorf("SecretKey length = %d, want %d", len(kp.SecretKey), MLDSAPrivateKeySize)
	}
	if len(kp.PublicKey) != MLDSAPublicKeySize {
		t.Errorf("PublicKey length = %d, want %d", len(kp.PublicKey), MLDSAPublicKeySize)
	}
	if kp.Params == nil {
		t.Fatal("Params is nil")
	}
	if kp.Params.SecurityLevel != DilithiumSecurityLevel3 {
		t.Errorf("SecurityLevel = %d, want %d", kp.Params.SecurityLevel, DilithiumSecurityLevel3)
	}
}

func TestDilithiumKeyPairDistinct(t *testing.T) {
	kp1, err := GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey 1: %v", err)
	}
	kp2, err := GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey 2: %v", err)
	}

	// Different random seeds should produce different public keys.
	same := true
	for i := range kp1.PublicKey {
		if kp1.PublicKey[i] != kp2.PublicKey[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("two independently generated keys produced identical public keys")
	}
}

func TestDilithiumSign(t *testing.T) {
	kp, err := GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey: %v", err)
	}

	msg := []byte("test message for dilithium signing")
	sig, err := kp.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) == 0 {
		t.Error("Sign returned empty signature")
	}
	if len(sig) != DilithiumSignatureSize() {
		t.Errorf("signature length = %d, want %d", len(sig), DilithiumSignatureSize())
	}
}

func TestDilithiumVerify(t *testing.T) {
	kp, err := GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey: %v", err)
	}

	msg := []byte("verify this message")
	sig, err := kp.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if !kp.Verify(msg, sig) {
		t.Error("Verify returned false for valid signature")
	}
}

func TestDilithiumVerifyWrongMessage(t *testing.T) {
	kp, err := GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey: %v", err)
	}

	msg := []byte("original message")
	sig, err := kp.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	wrongMsg := []byte("different message")
	if kp.Verify(wrongMsg, sig) {
		t.Error("Verify accepted signature for wrong message")
	}
}

func TestDilithiumVerifyWrongSignature(t *testing.T) {
	kp, err := GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey: %v", err)
	}

	msg := []byte("tamper test message")
	sig, err := kp.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Tamper with the signature.
	tampered := make([]byte, len(sig))
	copy(tampered, sig)
	tampered[0] ^= 0xff

	if kp.Verify(msg, tampered) {
		t.Error("Verify accepted tampered signature")
	}
}

func TestDilithiumSignatureSize(t *testing.T) {
	size := DilithiumSignatureSize()
	if size != MLDSASignatureSize {
		t.Errorf("DilithiumSignatureSize() = %d, want %d", size, MLDSASignatureSize)
	}
}

func TestDefaultDilithiumParams(t *testing.T) {
	p := DefaultDilithiumParams()
	if p.N != DilithiumN {
		t.Errorf("N = %d, want %d", p.N, DilithiumN)
	}
	if p.Q != DilithiumQ {
		t.Errorf("Q = %d, want %d", p.Q, DilithiumQ)
	}
	if p.D != DilithiumD {
		t.Errorf("D = %d, want %d", p.D, DilithiumD)
	}
	if p.SecurityLevel != DilithiumSecurityLevel3 {
		t.Errorf("SecurityLevel = %d, want %d", p.SecurityLevel, DilithiumSecurityLevel3)
	}
}

func TestVerifyDilithiumStandalone(t *testing.T) {
	kp, err := GenerateDilithiumKey()
	if err != nil {
		t.Fatalf("GenerateDilithiumKey: %v", err)
	}

	msg := []byte("standalone verify test")
	sig, err := kp.Sign(msg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Standalone verification (checks well-formedness).
	if !VerifyDilithium(kp.PublicKey, msg, sig) {
		t.Error("VerifyDilithium rejected valid signature")
	}

	// All-zero signature should be rejected.
	zeroSig := make([]byte, DilithiumSignatureSize())
	if VerifyDilithium(kp.PublicKey, msg, zeroSig) {
		t.Error("VerifyDilithium accepted all-zero signature")
	}

	// Wrong-length signature should be rejected.
	if VerifyDilithium(kp.PublicKey, msg, []byte("short")) {
		t.Error("VerifyDilithium accepted wrong-length signature")
	}
}

func TestDilithiumSignNilKey(t *testing.T) {
	var kp *DilithiumKeyPair
	_, err := kp.Sign([]byte("test"))
	if err != ErrDilithiumNilKey {
		t.Errorf("Sign on nil key: got %v, want %v", err, ErrDilithiumNilKey)
	}
}

func TestDilithiumVerifyNilKey(t *testing.T) {
	var kp *DilithiumKeyPair
	if kp.Verify([]byte("test"), make([]byte, 64)) {
		t.Error("Verify on nil key returned true")
	}
}
