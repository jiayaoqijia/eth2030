package pqc

import (
	"bytes"
	"testing"
)

func TestFalconSignerImplementsPQSigner(t *testing.T) {
	var _ PQSigner = (*FalconSigner)(nil)
}

func TestFalconGenerateDistinctKeys(t *testing.T) {
	signer := &FalconSigner{}
	kp1, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("first GenerateKey: %v", err)
	}
	kp2, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("second GenerateKey: %v", err)
	}

	if bytes.Equal(kp1.PublicKey, kp2.PublicKey) {
		t.Error("key generation repeated a public key")
	}
	if bytes.Equal(kp1.SecretKey, kp2.SecretKey) {
		t.Error("key generation repeated a secret key")
	}
}

func TestFalconGenerateKeyAlgorithmField(t *testing.T) {
	signer := &FalconSigner{}
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	if kp.Algorithm != FALCON512 {
		t.Errorf("Algorithm = %d, want %d", kp.Algorithm, FALCON512)
	}
}

func TestFalconGenerateKeyNonZero(t *testing.T) {
	signer := &FalconSigner{}
	kp, _ := signer.GenerateKey()

	allZero := true
	for _, b := range kp.PublicKey {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("public key is all zeros")
	}

	allZero = true
	for _, b := range kp.SecretKey {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("secret key is all zeros")
	}
}

func TestFalconSignProducesCorrectSize(t *testing.T) {
	signer := &FalconSigner{}
	kp, _ := signer.GenerateKey()

	sig, err := signer.Sign(kp.SecretKey, []byte("test"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if len(sig) != Falcon512SigSize {
		t.Errorf("signature length = %d, want %d", len(sig), Falcon512SigSize)
	}
}

func TestFalconRepeatedSignaturesVerify(t *testing.T) {
	signer := &FalconSigner{}
	kp, _ := signer.GenerateKey()

	msg := []byte("determinism test for falcon")
	sig1, _ := signer.Sign(kp.SecretKey, msg)
	sig2, _ := signer.Sign(kp.SecretKey, msg)

	if !signer.Verify(kp.PublicKey, msg, sig1) || !signer.Verify(kp.PublicKey, msg, sig2) {
		t.Error("both signatures must verify")
	}
}

func TestFalconSignDifferentMessagesProduceDifferentSigs(t *testing.T) {
	signer := &FalconSigner{}
	kp, _ := signer.GenerateKey()

	sig1, _ := signer.Sign(kp.SecretKey, []byte("message one"))
	sig2, _ := signer.Sign(kp.SecretKey, []byte("message two"))

	if bytes.Equal(sig1, sig2) {
		t.Error("different messages should produce different signatures")
	}
}

func TestFalconSignEmptyMessage(t *testing.T) {
	signer := &FalconSigner{}
	kp, _ := signer.GenerateKey()

	// Empty message should still produce a valid signature.
	sig, err := signer.Sign(kp.SecretKey, []byte{})
	if err != nil {
		t.Fatalf("Sign with empty message: %v", err)
	}
	if len(sig) != Falcon512SigSize {
		t.Errorf("signature length = %d, want %d", len(sig), Falcon512SigSize)
	}
}

func TestFalconSignNilMessage(t *testing.T) {
	signer := &FalconSigner{}
	kp, _ := signer.GenerateKey()

	sig, err := signer.Sign(kp.SecretKey, nil)
	if err != nil {
		t.Fatalf("Sign with nil message: %v", err)
	}
	if len(sig) != Falcon512SigSize {
		t.Errorf("signature length = %d, want %d", len(sig), Falcon512SigSize)
	}
}

func TestFalconSignTooShortKey(t *testing.T) {
	signer := &FalconSigner{}
	_, err := signer.Sign([]byte("too-short"), []byte("msg"))
	if err != ErrInvalidKeySize {
		t.Errorf("expected ErrInvalidKeySize, got %v", err)
	}
}

func TestFalconSignTooLongKey(t *testing.T) {
	signer := &FalconSigner{}
	longKey := make([]byte, Falcon512SecKeySize+100)
	_, err := signer.Sign(longKey, []byte("msg"))
	if err != ErrInvalidKeySize {
		t.Errorf("expected ErrInvalidKeySize for oversize key, got %v", err)
	}
}

func TestFalconSignNilKey(t *testing.T) {
	signer := &FalconSigner{}
	_, err := signer.Sign(nil, []byte("msg"))
	if err != ErrInvalidKeySize {
		t.Errorf("expected ErrInvalidKeySize for nil key, got %v", err)
	}
}

func TestFalconVerifyValid(t *testing.T) {
	signer := &FalconSigner{}
	kp, _ := signer.GenerateKey()
	msg := []byte("falcon verify test")
	sig, _ := signer.Sign(kp.SecretKey, msg)

	if !signer.Verify(kp.PublicKey, msg, sig) {
		t.Error("valid signature should verify")
	}
}

func TestFalconVerifyWrongPubKeySize(t *testing.T) {
	signer := &FalconSigner{}
	sig := make([]byte, Falcon512SigSize)
	// Fill with non-zero to pass stubVerify's zero check.
	for i := range sig[:32] {
		sig[i] = 0xAA
	}

	if signer.Verify([]byte("short-pk"), []byte("msg"), sig) {
		t.Error("should reject wrong pubkey size")
	}
}

func TestFalconVerifyWrongSigSize(t *testing.T) {
	signer := &FalconSigner{}
	pk := make([]byte, Falcon512PubKeySize)

	if signer.Verify(pk, []byte("msg"), []byte("short-sig")) {
		t.Error("should reject wrong signature size")
	}
}

func TestFalconVerifyAllZeroSig(t *testing.T) {
	signer := &FalconSigner{}
	pk := make([]byte, Falcon512PubKeySize)
	sig := make([]byte, Falcon512SigSize) // all zeros

	if signer.Verify(pk, []byte("msg"), sig) {
		t.Error("should reject all-zero signature")
	}
}

func TestFalconVerifyNilInputs(t *testing.T) {
	signer := &FalconSigner{}

	if signer.Verify(nil, []byte("msg"), make([]byte, Falcon512SigSize)) {
		t.Error("should reject nil public key")
	}
	if signer.Verify(make([]byte, Falcon512PubKeySize), []byte("msg"), nil) {
		t.Error("should reject nil signature")
	}
}

func TestFalconAlgorithmReturnsCorrectValue(t *testing.T) {
	signer := &FalconSigner{}
	if signer.Algorithm() != FALCON512 {
		t.Errorf("Algorithm() = %d, want %d", signer.Algorithm(), FALCON512)
	}
}

func TestFalconSignVerifyRoundTrip(t *testing.T) {
	signer := &FalconSigner{}
	kp, _ := signer.GenerateKey()

	messages := [][]byte{
		[]byte("short"),
		[]byte("a medium-length test message for falcon-512"),
		make([]byte, 1024), // large message
	}
	// Fill the large message with non-zero data.
	for i := range messages[2] {
		messages[2][i] = byte(i % 256)
	}

	for i, msg := range messages {
		sig, err := signer.Sign(kp.SecretKey, msg)
		if err != nil {
			t.Fatalf("message %d: Sign: %v", i, err)
		}
		if !signer.Verify(kp.PublicKey, msg, sig) {
			t.Errorf("message %d: valid signature rejected", i)
		}
	}
}
