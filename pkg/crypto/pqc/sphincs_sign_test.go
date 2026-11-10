package pqc

import (
	"testing"
)

func TestSPHINCSKeypairGeneration(t *testing.T) {
	pk, sk := SPHINCSKeypair()
	if len(pk) != SPHINCSPubKeySize {
		t.Fatalf("public key length = %d, want %d", len(pk), SPHINCSPubKeySize)
	}
	if len(sk) != SPHINCSSecKeySize {
		t.Fatalf("secret key length = %d, want %d", len(sk), SPHINCSSecKeySize)
	}
}

func TestSPHINCSKeypairUniqueness(t *testing.T) {
	pk1, sk1 := SPHINCSKeypair()
	pk2, sk2 := SPHINCSKeypair()

	if sphincsBytesEqual(pk1, pk2) {
		t.Fatal("two keypairs should produce different public keys")
	}
	if sphincsBytesEqual(sk1, sk2) {
		t.Fatal("two keypairs should produce different secret keys")
	}
}

func TestSPHINCSSignVerify(t *testing.T) {
	pk, sk := SPHINCSKeypair()
	msg := []byte("hash-based post-quantum signatures")

	sig := SPHINCSSign(sk, msg)
	if sig == nil {
		t.Fatal("signing returned nil")
	}
	if len(sig) == 0 {
		t.Fatal("signature is empty")
	}

	if !SPHINCSVerify(pk, msg, sig) {
		t.Fatal("valid signature rejected")
	}
}

func TestSPHINCSSignDifferentMessages(t *testing.T) {
	pk, sk := SPHINCSKeypair()

	messages := []string{
		"message alpha",
		"message beta",
		"a longer message to exercise the SPHINCS+ signing codepath",
	}

	for _, m := range messages {
		msg := []byte(m)
		sig := SPHINCSSign(sk, msg)
		if sig == nil {
			t.Fatalf("signing %q returned nil", m)
		}
		if !SPHINCSVerify(pk, msg, sig) {
			t.Fatalf("valid signature rejected for %q", m)
		}
	}
}

func TestSPHINCSVerifyWrongMessage(t *testing.T) {
	pk, sk := SPHINCSKeypair()
	sig := SPHINCSSign(sk, []byte("correct"))
	if sig == nil {
		t.Fatal("signing returned nil")
	}

	if SPHINCSVerify(pk, []byte("incorrect"), sig) {
		t.Fatal("signature verified for wrong message")
	}
}

func TestSPHINCSVerifyWrongKey(t *testing.T) {
	_, sk1 := SPHINCSKeypair()
	pk2, _ := SPHINCSKeypair()

	msg := []byte("test message")
	sig := SPHINCSSign(sk1, msg)
	if sig == nil {
		t.Fatal("signing returned nil")
	}

	if SPHINCSVerify(pk2, msg, sig) {
		t.Fatal("signature verified with wrong public key")
	}
}

func TestSPHINCSSignEmptyMessage(t *testing.T) {
	_, sk := SPHINCSKeypair()
	sig := SPHINCSSign(sk, []byte{})
	if sig != nil {
		t.Fatal("signing empty message should return nil")
	}
}

func TestSPHINCSSignNilMessage(t *testing.T) {
	_, sk := SPHINCSKeypair()
	sig := SPHINCSSign(sk, nil)
	if sig != nil {
		t.Fatal("signing nil message should return nil")
	}
}

func TestSPHINCSSignShortKey(t *testing.T) {
	sig := SPHINCSSign(SPHINCSPrivateKey([]byte{1, 2}), []byte("test"))
	if sig != nil {
		t.Fatal("signing with short key should return nil")
	}
}

func TestSPHINCSVerifyShortPK(t *testing.T) {
	if SPHINCSVerify(SPHINCSPublicKey([]byte{1}), []byte("msg"), SPHINCSSignature([]byte{1, 2, 3})) {
		t.Fatal("verify with short PK should fail")
	}
}

func TestSPHINCSVerifyEmptyMessage(t *testing.T) {
	pk, _ := SPHINCSKeypair()
	if SPHINCSVerify(pk, []byte{}, SPHINCSSignature([]byte{1})) {
		t.Fatal("verify with empty message should fail")
	}
}

func TestSPHINCSVerifyShortSig(t *testing.T) {
	pk, _ := SPHINCSKeypair()
	if SPHINCSVerify(pk, []byte("msg"), SPHINCSSignature([]byte{1, 2})) {
		t.Fatal("verify with short signature should fail")
	}
}

func sphincsBytesEqual(a, b []byte) bool {
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
