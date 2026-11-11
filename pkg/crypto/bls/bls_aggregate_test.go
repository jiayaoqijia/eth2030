package bls

import (
	"math/big"
	"testing"
)

// testKeyPair generates a deterministic key pair for testing.
func testKeyPair(seed int64) (*big.Int, [BLSPubkeySize]byte) {
	secret := new(big.Int).SetInt64(seed + 1) // avoid zero
	pk := BLSPubkeyFromSecret(secret)
	return secret, pk
}

func TestBLSPubkeyFromSecret(t *testing.T) {
	secret := big.NewInt(42)
	pk := BLSPubkeyFromSecret(secret)

	// Public key should not be all zeros.
	allZero := true
	for _, b := range pk {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("pubkey is all zeros")
	}

	// Same secret should produce same pubkey.
	pk2 := BLSPubkeyFromSecret(secret)
	if pk != pk2 {
		t.Fatal("same secret produced different pubkeys")
	}

	// Different secret should produce different pubkey.
	pk3 := BLSPubkeyFromSecret(big.NewInt(43))
	if pk == pk3 {
		t.Fatal("different secrets produced same pubkey")
	}
}

func TestSerializeDeserializeG1(t *testing.T) {
	// Test generator round-trip.
	gen := BlsG1Generator()
	serialized := SerializeG1(gen)
	deserialized := DeserializeG1(serialized)
	if deserialized == nil {
		t.Fatal("failed to deserialize G1 generator")
	}
	// Verify the deserialized point matches.
	x1, y1 := gen.blsG1ToAffine()
	x2, y2 := deserialized.blsG1ToAffine()
	if x1.Cmp(x2) != 0 || y1.Cmp(y2) != 0 {
		t.Fatal("G1 round-trip mismatch")
	}
}

func TestSerializeDeserializeG1Infinity(t *testing.T) {
	inf := BlsG1Infinity()
	serialized := SerializeG1(inf)
	deserialized := DeserializeG1(serialized)
	if deserialized == nil {
		t.Fatal("failed to deserialize G1 infinity")
	}
	if !deserialized.blsG1IsInfinity() {
		t.Fatal("deserialized G1 is not infinity")
	}
}

func TestSerializeDeserializeG2(t *testing.T) {
	gen := BlsG2Generator()
	serialized := SerializeG2(gen)
	deserialized := DeserializeG2(serialized)
	if deserialized == nil {
		t.Fatal("failed to deserialize G2 generator")
	}
	x1, y1 := gen.blsG2ToAffine()
	x2, y2 := deserialized.blsG2ToAffine()
	if !x1.equal(x2) || !y1.equal(y2) {
		t.Fatal("G2 round-trip mismatch")
	}
}

func TestSerializeDeserializeG2Infinity(t *testing.T) {
	inf := BlsG2Infinity()
	serialized := SerializeG2(inf)
	deserialized := DeserializeG2(serialized)
	if deserialized == nil {
		t.Fatal("failed to deserialize G2 infinity")
	}
	if !deserialized.blsG2IsInfinity() {
		t.Fatal("deserialized G2 is not infinity")
	}
}

func TestBLSSignAndVerify(t *testing.T) {
	secret := big.NewInt(12345)
	pk := BLSPubkeyFromSecret(secret)
	msg := []byte("hello world")

	sig := BLSSign(secret, msg)

	if !BLSVerify(pk, msg, sig) {
		t.Fatal("valid signature did not verify")
	}
}

func TestBLSVerifyWrongMessage(t *testing.T) {
	secret := big.NewInt(12345)
	pk := BLSPubkeyFromSecret(secret)

	sig := BLSSign(secret, []byte("hello"))

	if BLSVerify(pk, []byte("world"), sig) {
		t.Fatal("signature verified with wrong message")
	}
}

func TestBLSVerifyWrongKey(t *testing.T) {
	secret1 := big.NewInt(12345)
	secret2 := big.NewInt(54321)
	pk2 := BLSPubkeyFromSecret(secret2)
	msg := []byte("test message")

	sig := BLSSign(secret1, msg)

	if BLSVerify(pk2, msg, sig) {
		t.Fatal("signature verified with wrong public key")
	}
}

func TestAggregatePublicKeys(t *testing.T) {
	_, pk1 := testKeyPair(1)
	_, pk2 := testKeyPair(2)

	agg := AggregatePublicKeys([][48]byte{pk1, pk2})

	// Should not be zero.
	allZero := true
	for _, b := range agg {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("aggregated pubkey is all zeros")
	}

	// Order should not matter.
	agg2 := AggregatePublicKeys([][48]byte{pk2, pk1})
	if agg != agg2 {
		t.Fatal("aggregate public keys are not commutative")
	}
}

func TestAggregateSignatures(t *testing.T) {
	secret1 := big.NewInt(100)
	secret2 := big.NewInt(200)

	msg := []byte("same message")
	sig1 := BLSSign(secret1, msg)
	sig2 := BLSSign(secret2, msg)

	agg := AggregateSignatures([][96]byte{sig1, sig2})

	// Should not be zero.
	allZero := true
	for _, b := range agg {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("aggregated signature is all zeros")
	}
}

func TestFastAggregateVerify(t *testing.T) {
	secret1 := big.NewInt(100)
	secret2 := big.NewInt(200)
	pk1 := BLSPubkeyFromSecret(secret1)
	pk2 := BLSPubkeyFromSecret(secret2)

	msg := []byte("common message")
	sig1 := BLSSign(secret1, msg)
	sig2 := BLSSign(secret2, msg)

	aggSig := AggregateSignatures([][96]byte{sig1, sig2})

	if !FastAggregateVerify([][48]byte{pk1, pk2}, msg, aggSig) {
		t.Fatal("fast aggregate verify failed for valid signatures")
	}
}

func TestFastAggregateVerifyWrongMessage(t *testing.T) {
	secret1 := big.NewInt(100)
	secret2 := big.NewInt(200)
	pk1 := BLSPubkeyFromSecret(secret1)
	pk2 := BLSPubkeyFromSecret(secret2)

	sig1 := BLSSign(secret1, []byte("msg"))
	sig2 := BLSSign(secret2, []byte("msg"))
	aggSig := AggregateSignatures([][96]byte{sig1, sig2})

	if FastAggregateVerify([][48]byte{pk1, pk2}, []byte("wrong"), aggSig) {
		t.Fatal("fast aggregate verify should fail with wrong message")
	}
}

func TestFastAggregateVerifyEmptyPubkeys(t *testing.T) {
	var sig [96]byte
	if FastAggregateVerify([][48]byte{}, []byte("msg"), sig) {
		t.Fatal("should reject empty pubkey list")
	}
}

func TestVerifyAggregate(t *testing.T) {
	secret1 := big.NewInt(300)
	secret2 := big.NewInt(400)
	pk1 := BLSPubkeyFromSecret(secret1)
	pk2 := BLSPubkeyFromSecret(secret2)

	msg1 := []byte("message one")
	msg2 := []byte("message two")
	sig1 := BLSSign(secret1, msg1)
	sig2 := BLSSign(secret2, msg2)

	aggSig := AggregateSignatures([][96]byte{sig1, sig2})

	if !VerifyAggregate(
		[][48]byte{pk1, pk2},
		[][]byte{msg1, msg2},
		aggSig,
	) {
		t.Fatal("aggregate verify failed for valid distinct-message signatures")
	}
}

func TestVerifyAggregateMismatchedLengths(t *testing.T) {
	_, pk1 := testKeyPair(1)
	var sig [96]byte

	if VerifyAggregate([][48]byte{pk1}, [][]byte{}, sig) {
		t.Fatal("should reject mismatched pubkey/msg lengths")
	}
}

func TestHashToG2Deterministic(t *testing.T) {
	msg := []byte("test")
	dst := []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")

	p1 := HashToG2(msg, dst)
	p2 := HashToG2(msg, dst)

	s1 := SerializeG2(p1)
	s2 := SerializeG2(p2)
	if s1 != s2 {
		t.Fatal("HashToG2 is not deterministic")
	}
}

func TestHashToG2DifferentMessages(t *testing.T) {
	dst := []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")
	p1 := HashToG2([]byte("a"), dst)
	p2 := HashToG2([]byte("b"), dst)

	s1 := SerializeG2(p1)
	s2 := SerializeG2(p2)
	if s1 == s2 {
		t.Fatal("different messages should hash to different G2 points")
	}
}

func TestSingleSignerFastAggregateVerify(t *testing.T) {
	// Fast aggregate verify with a single signer should behave like normal verify.
	secret := big.NewInt(7777)
	pk := BLSPubkeyFromSecret(secret)
	msg := []byte("single signer test")
	sig := BLSSign(secret, msg)

	if !FastAggregateVerify([][48]byte{pk}, msg, sig) {
		t.Fatal("single-signer fast aggregate verify failed")
	}
}
