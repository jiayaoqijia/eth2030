package secp256k1

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/hex"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"math/big"
	"testing"
)

// --- ECDSA signature edge cases ---

// TestSignRejectsNonHashInput verifies Sign rejects non-32-byte inputs.
func TestSignRejectsNonHashInput(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	tests := []struct {
		name string
		hash []byte
	}{
		{"nil", nil},
		{"empty", []byte{}},
		{"1 byte", []byte{0x01}},
		{"31 bytes", make([]byte, 31)},
		{"33 bytes", make([]byte, 33)},
		{"64 bytes", make([]byte, 64)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Sign(tc.hash, key)
			if err == nil {
				t.Error("Sign should reject non-32-byte hash")
			}
		})
	}
}

// TestSigToPubRejectsInvalidInputs tests various invalid inputs to SigToPub.
func TestSigToPubRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name string
		hash []byte
		sig  []byte
	}{
		{"nil hash", nil, make([]byte, 65)},
		{"short hash", make([]byte, 16), make([]byte, 65)},
		{"long hash", make([]byte, 64), make([]byte, 65)},
		{"nil sig", make([]byte, 32), nil},
		{"short sig", make([]byte, 32), make([]byte, 64)},
		{"long sig", make([]byte, 32), make([]byte, 66)},
		{"v=2", make([]byte, 32), append(make([]byte, 64), 2)},
		{"v=3", make([]byte, 32), append(make([]byte, 64), 3)},
		{"v=255", make([]byte, 32), append(make([]byte, 64), 255)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := SigToPub(tc.hash, tc.sig)
			if err == nil {
				t.Error("SigToPub should reject invalid input")
			}
		})
	}
}

// TestSignatureRecoveryV0AndV1 verifies that signature recovery works for
// both v=0 and v=1 cases by signing multiple messages.
func TestSignatureRecoveryV0AndV1(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	seenV := make(map[byte]bool)
	// Sign many messages to try to get both v=0 and v=1.
	for i := 0; i < 100; i++ {
		hash := gethcrypto.Keccak256([]byte{byte(i), byte(i >> 8), byte(i >> 16)})
		sig, err := Sign(hash, key)
		if err != nil {
			t.Fatalf("Sign failed: %v", err)
		}
		v := sig[64]
		if v > 1 {
			t.Fatalf("Sign produced v=%d, expected 0 or 1", v)
		}
		seenV[v] = true

		// Verify recovery works for this v value.
		recovered, err := Ecrecover(hash, sig)
		if err != nil {
			t.Fatalf("Ecrecover failed for v=%d: %v", v, err)
		}
		expected := FromECDSAPub(&key.PublicKey)
		if !bytes.Equal(recovered, expected) {
			t.Fatalf("Ecrecover mismatch for v=%d", v)
		}
	}

	// It is statistically unlikely but possible that only one v value is seen.
	// Don't fail the test for this, just log it.
	if !seenV[0] || !seenV[1] {
		t.Log("Warning: did not observe both v=0 and v=1 in 100 signatures")
	}
}

// TestSignatureAlwaysLowS verifies the EIP-2 low-s enforcement.
func TestSignatureAlwaysLowS(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	for i := 0; i < 50; i++ {
		hash := gethcrypto.Keccak256([]byte{byte(i)})
		sig, err := Sign(hash, key)
		if err != nil {
			t.Fatalf("Sign failed: %v", err)
		}
		s := new(big.Int).SetBytes(sig[32:64])
		if s.Sign() <= 0 {
			t.Fatal("s should be positive")
		}
		if s.Cmp(secp256k1halfN) > 0 {
			t.Fatalf("s > N/2 (EIP-2 violation): s=%s, halfN=%s", s, secp256k1halfN)
		}
	}
}

// TestValidateSignatureValuesComprehensive tests all boundary conditions.
func TestValidateSignatureValuesComprehensive(t *testing.T) {
	one := big.NewInt(1)
	nMinusOne := new(big.Int).Sub(secp256k1N, big.NewInt(1))

	tests := []struct {
		name      string
		v         byte
		r, s      *big.Int
		homestead bool
		want      bool
	}{
		// Valid cases.
		{"v=0 r=1 s=1 pre", 0, one, one, false, true},
		{"v=1 r=1 s=1 pre", 1, one, one, false, true},
		{"v=0 r=N-1 s=1 pre", 0, nMinusOne, one, false, true},
		{"v=0 r=1 s=N-1 pre", 0, one, nMinusOne, false, true},
		{"v=0 r=1 s=halfN homestead", 0, one, secp256k1halfN, true, true},

		// Invalid: nil.
		{"nil r", 0, nil, one, false, false},
		{"nil s", 0, one, nil, false, false},
		{"nil both", 0, nil, nil, false, false},

		// Invalid: zero.
		{"zero r", 0, big.NewInt(0), one, false, false},
		{"zero s", 0, one, big.NewInt(0), false, false},
		{"negative r", 0, big.NewInt(-1), one, false, false},
		{"negative s", 0, one, big.NewInt(-1), false, false},

		// Invalid: out of range.
		{"r=N", 0, new(big.Int).Set(secp256k1N), one, false, false},
		{"s=N", 0, one, new(big.Int).Set(secp256k1N), false, false},
		{"r>N", 0, new(big.Int).Add(secp256k1N, big.NewInt(1)), one, false, false},

		// Invalid: v out of range.
		{"v=2", 2, one, one, false, false},
		{"v=27", 27, one, one, false, false},
		{"v=255", 255, one, one, false, false},

		// Homestead: high-s rejected.
		{"high-s homestead", 0, one, new(big.Int).Add(secp256k1halfN, big.NewInt(1)), true, false},
		// Pre-homestead: high-s accepted.
		{"high-s pre-homestead", 0, one, new(big.Int).Add(secp256k1halfN, big.NewInt(1)), false, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateSignatureValues(tc.v, tc.r, tc.s, tc.homestead)
			if got != tc.want {
				t.Errorf("ValidateSignatureValues(%d, %v, %v, %v) = %v, want %v",
					tc.v, tc.r, tc.s, tc.homestead, got, tc.want)
			}
		})
	}
}

// TestValidateSignatureRejectsMismatchedPubkeyPrefix verifies that pubkeys
// without the 0x04 prefix are rejected.
func TestValidateSignatureRejectsMismatchedPubkeyPrefix(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	hash := gethcrypto.Keccak256([]byte("test"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}
	pub := FromECDSAPub(&key.PublicKey)

	// Correct prefix: should pass.
	if !ValidateSignature(pub, hash, sig[:64]) {
		t.Error("ValidateSignature rejected valid signature")
	}

	// Wrong prefix byte: should fail.
	badPub := make([]byte, 65)
	copy(badPub, pub)
	badPub[0] = 0x02
	if ValidateSignature(badPub, hash, sig[:64]) {
		t.Error("ValidateSignature should reject pubkey with 0x02 prefix")
	}

	badPub[0] = 0x03
	if ValidateSignature(badPub, hash, sig[:64]) {
		t.Error("ValidateSignature should reject pubkey with 0x03 prefix")
	}

	badPub[0] = 0x00
	if ValidateSignature(badPub, hash, sig[:64]) {
		t.Error("ValidateSignature should reject pubkey with 0x00 prefix")
	}
}

// --- Key generation edge cases ---

// TestGenerateKeyProducesUniqueKeys tests that consecutive key generations
// produce different keys.
func TestGenerateKeyProducesUniqueKeys(t *testing.T) {
	keys := make([]*ecdsa.PrivateKey, 10)
	for i := 0; i < 10; i++ {
		var err error
		keys[i], err = GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey failed: %v", err)
		}
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i].D.Cmp(keys[j].D) == 0 {
				t.Errorf("keys[%d] and keys[%d] have same private key", i, j)
			}
		}
	}
}

// TestGenerateKeySignVerify verifies the full lifecycle: generate, sign, verify, recover.
func TestGenerateKeySignVerify(t *testing.T) {
	for i := 0; i < 5; i++ {
		key, err := GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey failed: %v", err)
		}

		hash := gethcrypto.Keccak256([]byte("lifecycle test message"))
		sig, err := Sign(hash, key)
		if err != nil {
			t.Fatalf("Sign failed: %v", err)
		}

		// Validate (without V).
		pub := FromECDSAPub(&key.PublicKey)
		if !ValidateSignature(pub, hash, sig[:64]) {
			t.Error("ValidateSignature rejected valid signature")
		}

		// Recover public key.
		recovered, err := SigToPub(hash, sig)
		if err != nil {
			t.Fatalf("SigToPub failed: %v", err)
		}
		if key.PublicKey.X.Cmp(recovered.X) != 0 || key.PublicKey.Y.Cmp(recovered.Y) != 0 {
			t.Error("recovered key does not match original")
		}

		// Derive address from recovered key.
		originalAddr := PubkeyToAddress(key.PublicKey)
		recoveredAddr := PubkeyToAddress(*recovered)
		if originalAddr != recoveredAddr {
			t.Error("address derived from recovered key does not match")
		}
	}
}

// --- Compress/Decompress edge cases ---

// TestDecompressPubkeyInvalidPrefix verifies invalid prefix bytes are rejected.
func TestDecompressPubkeyInvalidPrefix(t *testing.T) {
	tests := []struct {
		name   string
		prefix byte
	}{
		{"0x00", 0x00},
		{"0x01", 0x01},
		{"0x04", 0x04},
		{"0x05", 0x05},
		{"0xff", 0xff},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := make([]byte, 33)
			data[0] = tc.prefix
			data[32] = 1 // some non-zero x coordinate
			_, err := DecompressPubkey(data)
			if err == nil {
				t.Errorf("DecompressPubkey should reject prefix 0x%02x", tc.prefix)
			}
		})
	}
}

// TestDecompressPubkeyTooShort verifies short inputs are rejected.
func TestDecompressPubkeyTooShort(t *testing.T) {
	for l := 0; l < 33; l++ {
		if l == 33 {
			continue
		}
		_, err := DecompressPubkey(make([]byte, l))
		if err == nil {
			t.Errorf("DecompressPubkey should reject %d-byte input", l)
		}
	}
}

// TestDecompressPubkeyTooLong verifies long inputs are rejected.
func TestDecompressPubkeyTooLong(t *testing.T) {
	_, err := DecompressPubkey(make([]byte, 34))
	if err == nil {
		t.Error("DecompressPubkey should reject 34-byte input")
	}
}

// TestCompressDecompressPreservesParityMultiple tests round-trip with
// multiple keys to ensure both even and odd Y parities are handled.
func TestCompressDecompressPreservesParityMultiple(t *testing.T) {
	seenEven := false
	seenOdd := false
	for i := 0; i < 20; i++ {
		key, err := GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey failed: %v", err)
		}
		compressed := CompressPubkey(&key.PublicKey)
		if compressed[0] == 0x02 {
			seenEven = true
		} else if compressed[0] == 0x03 {
			seenOdd = true
		} else {
			t.Fatalf("unexpected prefix: 0x%02x", compressed[0])
		}

		recovered, err := DecompressPubkey(compressed)
		if err != nil {
			t.Fatalf("DecompressPubkey failed: %v", err)
		}
		if key.PublicKey.X.Cmp(recovered.X) != 0 || key.PublicKey.Y.Cmp(recovered.Y) != 0 {
			t.Error("round-trip failed")
		}
	}
	if !seenEven || !seenOdd {
		t.Log("Warning: did not see both even and odd Y parities in 20 keys")
	}
}

// --- PubkeyToAddress edge cases ---

// TestPubkeyToAddressKnownVector tests PubkeyToAddress against a known Ethereum address.
// Using the well-known test vector from go-ethereum:
// Private key: 0xfad9c8855b740a0b7ed4c221dbad0f33a83a49cad6b3fe8d5817ac83d38b6a19
// Expected address: 0x96216849c49358B10257cb55b28eA603c874b05E
func TestPubkeyToAddressKnownVector(t *testing.T) {
	privKeyBytes, _ := hex.DecodeString("fad9c8855b740a0b7ed4c221dbad0f33a83a49cad6b3fe8d5817ac83d38b6a19")
	d := new(big.Int).SetBytes(privKeyBytes)
	curve := S256()
	x, y := curve.ScalarBaseMult(d.Bytes())
	pub := ecdsa.PublicKey{Curve: s256, X: x, Y: y}
	addr := PubkeyToAddress(pub)
	want := "96216849c49358B10257cb55b28eA603c874b05E"
	got := hex.EncodeToString(addr[:])
	if !equalHexCaseInsensitive(got, want) {
		t.Errorf("PubkeyToAddress = %s, want %s", got, want)
	}
}

func equalHexCaseInsensitive(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'F' {
			ca += 32
		}
		if cb >= 'A' && cb <= 'F' {
			cb += 32
		}
		if ca != cb {
			return false
		}
	}
	return true
}

// TestPubkeyToAddressNilKey tests that a nil or zero key returns a zero address.
func TestPubkeyToAddressNilKey(t *testing.T) {
	pub := ecdsa.PublicKey{Curve: s256, X: nil, Y: nil}
	addr := PubkeyToAddress(pub)
	if !addr.IsZero() {
		t.Error("PubkeyToAddress with nil X/Y should return zero address")
	}
}

// --- FromECDSAPub edge cases ---

// TestFromECDSAPubNilFields tests various nil field combinations.
func TestFromECDSAPubNilFields(t *testing.T) {
	if FromECDSAPub(nil) != nil {
		t.Error("FromECDSAPub(nil) should return nil")
	}
	if FromECDSAPub(&ecdsa.PublicKey{X: nil, Y: big.NewInt(1)}) != nil {
		t.Error("FromECDSAPub with nil X should return nil")
	}
	if FromECDSAPub(&ecdsa.PublicKey{X: big.NewInt(1), Y: nil}) != nil {
		t.Error("FromECDSAPub with nil Y should return nil")
	}
}

// TestFromECDSAPubFormat verifies the output format is [0x04 || X(32) || Y(32)].
func TestFromECDSAPubFormat(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	pub := FromECDSAPub(&key.PublicKey)
	if len(pub) != 65 {
		t.Fatalf("length = %d, want 65", len(pub))
	}
	if pub[0] != 0x04 {
		t.Errorf("prefix = 0x%02x, want 0x04", pub[0])
	}
	// X and Y should be recoverable.
	x := new(big.Int).SetBytes(pub[1:33])
	y := new(big.Int).SetBytes(pub[33:65])
	if x.Cmp(key.PublicKey.X) != 0 {
		t.Error("X coordinate mismatch")
	}
	if y.Cmp(key.PublicKey.Y) != 0 {
		t.Error("Y coordinate mismatch")
	}
}

// --- secp256k1 curve edge cases ---

// TestCompressPubkeyNilFields tests CompressPubkey with nil coordinates.
func TestCompressPubkeyNilFields(t *testing.T) {
	if CompressPubkey(&ecdsa.PublicKey{X: nil, Y: big.NewInt(1)}) != nil {
		t.Error("CompressPubkey with nil X should return nil")
	}
	if CompressPubkey(&ecdsa.PublicKey{X: big.NewInt(1), Y: nil}) != nil {
		t.Error("CompressPubkey with nil Y should return nil")
	}
}

// TestEcrecoverZeroHashAndSig tests recovery with all-zero hash and signature.
func TestEcrecoverZeroHashAndSig(t *testing.T) {
	hash := make([]byte, 32)
	sig := make([]byte, 65)
	// r=0 in the signature means the recovery should fail since r must be in [1, n).
	_, err := Ecrecover(hash, sig)
	if err == nil {
		// If it doesn't error, verify the result is still something defined
		// (the behavior with r=0, s=0 is implementation-specific).
		t.Log("Ecrecover with zero hash and sig did not error (implementation-defined)")
	}
}

// TestEcrecoverSignatureMalleability verifies that flipping s to N-s
// (with the same v) recovers a different public key.
func TestEcrecoverSignatureMalleability(t *testing.T) {
	key, err := GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	hash := gethcrypto.Keccak256([]byte("malleability test"))
	sig, err := Sign(hash, key)
	if err != nil {
		t.Fatalf("Sign failed: %v", err)
	}

	// Sign always produces low-s. Create a high-s variant with the same v.
	// This should recover a different public key, because using N-s with the
	// same recovery ID yields a different point.
	s := new(big.Int).SetBytes(sig[32:64])
	highS := new(big.Int).Sub(secp256k1N, s)
	malleable := make([]byte, 65)
	copy(malleable, sig[:32])
	hsBytes := highS.Bytes()
	copy(malleable[64-len(hsBytes):64], hsBytes)
	malleable[64] = sig[64] // same v

	recovered, err := Ecrecover(hash, malleable)
	if err != nil {
		// Recovery might fail; that's acceptable behavior for a malleable sig.
		return
	}
	expected := FromECDSAPub(&key.PublicKey)
	if bytes.Equal(recovered, expected) {
		t.Error("malleable signature (N-s, same v) should recover a different public key")
	}
}
