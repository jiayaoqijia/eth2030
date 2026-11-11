package secp256k1

import (
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"math/big"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	if key.D == nil || key.D.Sign() == 0 {
		t.Error("GenerateKey produced nil or zero private key")
	}
	if key.PublicKey.X == nil || key.PublicKey.Y == nil {
		t.Error("GenerateKey produced nil public key coordinates")
	}
}

func TestPubkeyToAddressDeterministic(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	addr1 := PubkeyToAddress(key.PublicKey)
	addr2 := PubkeyToAddress(key.PublicKey)
	if addr1 != addr2 {
		t.Errorf("PubkeyToAddress not deterministic: %s != %s", addr1, addr2)
	}
}

func TestPubkeyToAddressNotZero(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	addr := PubkeyToAddress(key.PublicKey)
	if addr.IsZero() {
		t.Error("PubkeyToAddress returned zero address for valid key")
	}
}

func TestSignRequires32ByteHash(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	_, err = Sign([]byte("short"), key)
	if err == nil {
		t.Error("Sign should reject non-32-byte hash")
	}
}

func TestSignProduces65Bytes(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	hash := gethcrypto.Keccak256([]byte("test message"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	if len(sig) != 65 {
		t.Errorf("Sign produced %d bytes, want 65", len(sig))
	}
}

func TestCompressDecompressRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	compressed := CompressPubkey(&key.PublicKey)
	if len(compressed) != 33 {
		t.Fatalf("CompressPubkey produced %d bytes, want 33", len(compressed))
	}
	recovered, err := DecompressPubkey(compressed)
	if err != nil {
		t.Fatalf("DecompressPubkey failed: %v", err)
	}
	if key.PublicKey.X.Cmp(recovered.X) != 0 || key.PublicKey.Y.Cmp(recovered.Y) != 0 {
		t.Error("CompressPubkey/DecompressPubkey round-trip failed")
	}
}

func TestDecompressInvalidLength(t *testing.T) {
	_, err := DecompressPubkey([]byte{1, 2, 3})
	if err == nil {
		t.Error("DecompressPubkey should reject invalid length")
	}
}

func TestFromECDSAPubLength(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	pub := FromECDSAPub(&key.PublicKey)
	if len(pub) != 65 {
		t.Errorf("FromECDSAPub produced %d bytes, want 65", len(pub))
	}
	if pub[0] != 0x04 {
		t.Errorf("FromECDSAPub first byte = 0x%02x, want 0x04", pub[0])
	}
}

func TestFromECDSAPubNil(t *testing.T) {
	if FromECDSAPub(nil) != nil {
		t.Error("FromECDSAPub(nil) should return nil")
	}
}

func TestValidateSignatureValuesRejectsZeroR(t *testing.T) {
	if ValidateSignatureValues(0, big.NewInt(0), big.NewInt(1), false) {
		t.Error("ValidateSignatureValues should reject r=0")
	}
}

func TestValidateSignatureValuesRejectsZeroS(t *testing.T) {
	if ValidateSignatureValues(0, big.NewInt(1), big.NewInt(0), false) {
		t.Error("ValidateSignatureValues should reject s=0")
	}
}

func TestValidateSignatureValuesRejectsNilR(t *testing.T) {
	if ValidateSignatureValues(0, nil, big.NewInt(1), false) {
		t.Error("ValidateSignatureValues should reject nil r")
	}
}

func TestValidateSignatureValuesRejectsNilS(t *testing.T) {
	if ValidateSignatureValues(0, big.NewInt(1), nil, false) {
		t.Error("ValidateSignatureValues should reject nil s")
	}
}

func TestValidateSignatureValuesAcceptsValid(t *testing.T) {
	r := big.NewInt(1)
	s := big.NewInt(1)
	if !ValidateSignatureValues(0, r, s, false) {
		t.Error("ValidateSignatureValues should accept valid r=1, s=1")
	}
}

func TestValidateSignatureValuesHomesteadLowS(t *testing.T) {
	r := big.NewInt(1)
	// s greater than secp256k1halfN should be rejected in homestead mode
	highS := new(big.Int).Add(secp256k1halfN, big.NewInt(1))
	if ValidateSignatureValues(0, r, highS, true) {
		t.Error("ValidateSignatureValues should reject high S in homestead mode")
	}
	// s at halfN should be accepted
	if !ValidateSignatureValues(0, r, secp256k1halfN, true) {
		t.Error("ValidateSignatureValues should accept s == halfN in homestead mode")
	}
}

func TestValidateSignatureValuesRejectsInvalidV(t *testing.T) {
	if ValidateSignatureValues(2, big.NewInt(1), big.NewInt(1), false) {
		t.Error("ValidateSignatureValues should reject v > 1")
	}
}

func TestValidateSignatureValuesRejectsRGeN(t *testing.T) {
	if ValidateSignatureValues(0, secp256k1N, big.NewInt(1), false) {
		t.Error("ValidateSignatureValues should reject r >= N")
	}
}

func TestCompressPubkeyNil(t *testing.T) {
	if CompressPubkey(nil) != nil {
		t.Error("CompressPubkey(nil) should return nil")
	}
}

func TestValidateSignatureRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	hash := gethcrypto.Keccak256([]byte("test message"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	pubBytes := FromECDSAPub(&key.PublicKey)
	// ValidateSignature takes 64-byte sig (R||S without V)
	if !ValidateSignature(pubBytes, hash, sig[:64]) {
		t.Error("ValidateSignature should accept valid signature from Sign")
	}
}

func TestValidateSignatureRejectsWrongKey(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	hash := gethcrypto.Keccak256([]byte("test message"))
	sig, err := Sign(hash, key1)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	wrongPub := FromECDSAPub(&key2.PublicKey)
	if ValidateSignature(wrongPub, hash, sig[:64]) {
		t.Error("ValidateSignature should reject signature verified with wrong key")
	}
}

func TestValidateSignatureRejectsWrongHash(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	hash := gethcrypto.Keccak256([]byte("test message"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	wrongHash := gethcrypto.Keccak256([]byte("different message"))
	pubBytes := FromECDSAPub(&key.PublicKey)
	if ValidateSignature(pubBytes, wrongHash, sig[:64]) {
		t.Error("ValidateSignature should reject signature with wrong hash")
	}
}

func TestValidateSignatureRejectsInvalidInputs(t *testing.T) {
	if ValidateSignature([]byte{1, 2}, make([]byte, 32), make([]byte, 64)) {
		t.Error("ValidateSignature should reject invalid pubkey length")
	}
	if ValidateSignature(make([]byte, 65), make([]byte, 16), make([]byte, 64)) {
		t.Error("ValidateSignature should reject invalid hash length")
	}
	if ValidateSignature(make([]byte, 65), make([]byte, 32), make([]byte, 32)) {
		t.Error("ValidateSignature should reject invalid sig length")
	}
}

func TestSecp256k1CurveParams(t *testing.T) {
	curve := S256()
	params := curve.Params()

	if params.BitSize != 256 {
		t.Errorf("bit size = %d, want 256", params.BitSize)
	}
	// Generator must be on the curve.
	if !curve.IsOnCurve(params.Gx, params.Gy) {
		t.Error("generator point is not on curve")
	}
	if want := "fffffffffffffffffffffffffffffffebaaedce6af48a03bbfd25e8cd0364141"; params.N.Text(16) != want {
		t.Fatalf("curve order = %x, want %s", params.N, want)
	}
}

func TestEcrecoverRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	hash := gethcrypto.Keccak256([]byte("test message for ecrecover"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	recovered, err := Ecrecover(hash, sig)
	if err != nil {
		t.Fatalf("Ecrecover failed: %v", err)
	}

	expected := FromECDSAPub(&key.PublicKey)
	if len(recovered) != len(expected) {
		t.Fatalf("recovered pubkey length = %d, want %d", len(recovered), len(expected))
	}
	for i := range recovered {
		if recovered[i] != expected[i] {
			t.Fatalf("recovered pubkey mismatch at byte %d", i)
		}
	}
}

func TestSigToPubRoundTrip(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	hash := gethcrypto.Keccak256([]byte("another test"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	pub, err := SigToPub(hash, sig)
	if err != nil {
		t.Fatalf("SigToPub failed: %v", err)
	}

	if key.PublicKey.X.Cmp(pub.X) != 0 || key.PublicKey.Y.Cmp(pub.Y) != 0 {
		t.Error("SigToPub did not recover the correct public key")
	}
}

func TestEcrecoverDeriveAddress(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	expectedAddr := PubkeyToAddress(key.PublicKey)

	hash := gethcrypto.Keccak256([]byte("address derivation test"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	recovered, err := SigToPub(hash, sig)
	if err != nil {
		t.Fatalf("SigToPub failed: %v", err)
	}

	recoveredAddr := PubkeyToAddress(*recovered)
	if expectedAddr != recoveredAddr {
		t.Errorf("recovered address %s != expected %s", recoveredAddr, expectedAddr)
	}
}

func TestEcrecoverMultipleMessages(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	expectedPub := FromECDSAPub(&key.PublicKey)

	messages := []string{"msg1", "msg2", "msg3", "hello world", "ethereum"}
	for _, msg := range messages {
		hash := gethcrypto.Keccak256([]byte(msg))
		sig, err := Sign(hash, key)
		if err != nil {
			t.Fatalf("Sign(%q) failed: %v", msg, err)
		}
		recovered, err := Ecrecover(hash, sig)
		if err != nil {
			t.Fatalf("Ecrecover(%q) failed: %v", msg, err)
		}
		for i := range recovered {
			if recovered[i] != expectedPub[i] {
				t.Fatalf("Ecrecover(%q): mismatch at byte %d", msg, i)
			}
		}
	}
}

func TestEcrecoverRejectsInvalidSig(t *testing.T) {
	hash := gethcrypto.Keccak256([]byte("test"))

	// Wrong length.
	_, err := Ecrecover(hash, make([]byte, 64))
	if err == nil {
		t.Error("Ecrecover should reject 64-byte sig")
	}

	// Invalid V.
	sig := make([]byte, 65)
	sig[64] = 4
	_, err = Ecrecover(hash, sig)
	if err == nil {
		t.Error("Ecrecover should reject V > 1")
	}
}

func TestEcrecoverRejectsWrongHash(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	hash := gethcrypto.Keccak256([]byte("original"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	wrongHash := gethcrypto.Keccak256([]byte("different"))
	recovered, err := Ecrecover(wrongHash, sig)
	if err != nil {
		// May still succeed but recover a different key.
		return
	}
	expected := FromECDSAPub(&key.PublicKey)
	match := true
	for i := range recovered {
		if recovered[i] != expected[i] {
			match = false
			break
		}
	}
	if match {
		t.Error("Ecrecover with wrong hash should recover different pubkey")
	}
}

func TestSignLowS(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	// Sign multiple times and verify S is always in lower half.
	for i := 0; i < 20; i++ {
		hash := gethcrypto.Keccak256([]byte{byte(i), byte(i >> 8)})
		sig, err := Sign(hash, key)
		if err != nil {
			t.Fatalf("Sign failed: %v", err)
		}
		s := new(big.Int).SetBytes(sig[32:64])
		if s.Cmp(secp256k1halfN) > 0 {
			t.Error("Sign should produce low-S signatures (EIP-2)")
		}
	}
}

func TestDifferentKeysProduceDifferentAddresses(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	addr1 := PubkeyToAddress(key1.PublicKey)
	addr2 := PubkeyToAddress(key2.PublicKey)
	if addr1 == addr2 {
		t.Error("Different keys should produce different addresses")
	}
}
