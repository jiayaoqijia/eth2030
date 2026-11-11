package bls

import (
	"math/big"
	"testing"
)

func TestBLSAggCheckG1Subgroup(t *testing.T) {
	ba := NewBLSAgg()

	// Valid pubkey.
	secret := big.NewInt(42)
	pk := BLSPubkeyFromSecret(secret)
	if err := ba.CheckG1Subgroup(pk); err != nil {
		t.Fatalf("valid pubkey failed subgroup check: %v", err)
	}

	// Invalid: all zeros (not a valid compressed point).
	var zeroPK [BLSPubkeySize]byte
	if err := ba.CheckG1Subgroup(zeroPK); err == nil {
		t.Fatal("zero pubkey should fail subgroup check")
	}
}

func TestBLSAggCheckG2Subgroup(t *testing.T) {
	ba := NewBLSAgg()

	secret := big.NewInt(123)
	sig := BLSSign(secret, []byte("test"))
	if err := ba.CheckG2Subgroup(sig); err != nil {
		t.Fatalf("valid signature failed subgroup check: %v", err)
	}

	var zeroSig [BLSSignatureSize]byte
	if err := ba.CheckG2Subgroup(zeroSig); err == nil {
		t.Fatal("zero signature should fail subgroup check")
	}
}

func TestBLSAggDecompressG1(t *testing.T) {
	ba := NewBLSAgg()

	secret := big.NewInt(77)
	pk := BLSPubkeyFromSecret(secret)

	p, err := ba.DecompressG1(pk)
	if err != nil {
		t.Fatalf("DecompressG1: %v", err)
	}
	if p == nil {
		t.Fatal("DecompressG1 returned nil")
	}

	// Re-serialize should match.
	serialized := SerializeG1(p)
	if serialized != pk {
		t.Fatal("re-serialized G1 does not match original")
	}
}

func TestBLSAggDecompressG2(t *testing.T) {
	ba := NewBLSAgg()

	secret := big.NewInt(88)
	sig := BLSSign(secret, []byte("decompress test"))

	p, err := ba.DecompressG2(sig)
	if err != nil {
		t.Fatalf("DecompressG2: %v", err)
	}
	if p == nil {
		t.Fatal("DecompressG2 returned nil")
	}

	serialized := SerializeG2(p)
	if serialized != sig {
		t.Fatal("re-serialized G2 does not match original")
	}
}

func TestBLSAggGenerateAndVerifyPoP(t *testing.T) {
	ba := NewBLSAgg()
	secret := big.NewInt(12345)
	pk := BLSPubkeyFromSecret(secret)

	pop := ba.GeneratePoP(secret)

	// Valid PoP.
	if !ba.VerifyPoP(pk, pop) {
		t.Fatal("valid PoP should verify")
	}

	// Wrong key.
	wrongPK := BLSPubkeyFromSecret(big.NewInt(54321))
	if ba.VerifyPoP(wrongPK, pop) {
		t.Fatal("PoP should not verify with wrong key")
	}
}

func TestBLSAggPoPInvalidPubkey(t *testing.T) {
	ba := NewBLSAgg()
	var zeroPK [BLSPubkeySize]byte
	var pop ProofOfPossession
	if ba.VerifyPoP(zeroPK, pop) {
		t.Fatal("PoP should fail with zero pubkey")
	}
}

func TestBLSAggAggregatePublicKeysValidated(t *testing.T) {
	ba := NewBLSAgg()

	pk1 := BLSPubkeyFromSecret(big.NewInt(10))
	pk2 := BLSPubkeyFromSecret(big.NewInt(20))

	agg, err := ba.AggregatePublicKeysValidated([][BLSPubkeySize]byte{pk1, pk2})
	if err != nil {
		t.Fatalf("AggregatePublicKeysValidated: %v", err)
	}

	// Should be non-zero.
	allZero := true
	for _, b := range agg {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("aggregated pubkey is zero")
	}

	// Commutative.
	agg2, err := ba.AggregatePublicKeysValidated([][BLSPubkeySize]byte{pk2, pk1})
	if err != nil {
		t.Fatal(err)
	}
	if agg != agg2 {
		t.Fatal("aggregate pubkeys should be commutative")
	}
}

func TestBLSAggAggregatePublicKeysValidatedEmpty(t *testing.T) {
	ba := NewBLSAgg()
	_, err := ba.AggregatePublicKeysValidated(nil)
	if err != ErrBLSAggNoPubkeys {
		t.Fatalf("expected ErrBLSAggNoPubkeys, got %v", err)
	}
}

func TestBLSAggAggregatePublicKeysValidatedInvalid(t *testing.T) {
	ba := NewBLSAgg()
	var bad [BLSPubkeySize]byte
	_, err := ba.AggregatePublicKeysValidated([][BLSPubkeySize]byte{bad})
	if err != ErrBLSAggInvalidPubkey {
		t.Fatalf("expected ErrBLSAggInvalidPubkey, got %v", err)
	}
}

func TestBLSAggAggregateSignaturesValidated(t *testing.T) {
	ba := NewBLSAgg()

	sig1 := BLSSign(big.NewInt(100), []byte("msg1"))
	sig2 := BLSSign(big.NewInt(200), []byte("msg2"))

	agg, err := ba.AggregateSignaturesValidated([][BLSSignatureSize]byte{sig1, sig2})
	if err != nil {
		t.Fatalf("AggregateSignaturesValidated: %v", err)
	}

	allZero := true
	for _, b := range agg {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Fatal("aggregated signature is zero")
	}
}

func TestBLSAggAggregateSignaturesValidatedEmpty(t *testing.T) {
	ba := NewBLSAgg()
	_, err := ba.AggregateSignaturesValidated(nil)
	if err != ErrBLSAggNoSignatures {
		t.Fatalf("expected ErrBLSAggNoSignatures, got %v", err)
	}
}

func TestBLSAggSignAndVerifyWithDST(t *testing.T) {
	ba := NewBLSAgg()
	secret := big.NewInt(7890)
	pk := BLSPubkeyFromSecret(secret)
	msg := []byte("custom domain")
	dst := DSTBeaconAttestation

	sig := ba.SignWithDST(secret, msg, dst)

	// Should verify with correct DST.
	if !ba.VerifyWithDST(pk, msg, sig, dst) {
		t.Fatal("signature should verify with correct DST")
	}

	// Should NOT verify with different DST.
	if ba.VerifyWithDST(pk, msg, sig, DSTBeaconProposal) {
		t.Fatal("signature should not verify with wrong DST")
	}
}

func TestBLSAggVerifyWithDSTInvalidPubkey(t *testing.T) {
	ba := NewBLSAgg()
	var zeroPK [BLSPubkeySize]byte
	var sig [BLSSignatureSize]byte
	if ba.VerifyWithDST(zeroPK, []byte("msg"), sig, DSTBeaconAttestation) {
		t.Fatal("should not verify with zero pubkey")
	}
}

func TestBLSSignatureSetEmpty(t *testing.T) {
	ss := NewBLSSignatureSet()
	if ss.Len() != 0 {
		t.Fatalf("Len() = %d, want 0", ss.Len())
	}
	if ss.Verify() {
		t.Fatal("empty set should not verify")
	}
}

func TestBLSSignatureSetSingle(t *testing.T) {
	secret := big.NewInt(999)
	pk := BLSPubkeyFromSecret(secret)
	msg := []byte("single entry")
	sig := BLSSign(secret, msg)

	ss := NewBLSSignatureSet()
	ss.Add(pk, msg, sig)

	if ss.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", ss.Len())
	}
	if !ss.Verify() {
		t.Fatal("single valid entry should verify")
	}
}

func TestBLSSignatureSetMultiple(t *testing.T) {
	ss := NewBLSSignatureSet()

	for i := int64(1); i <= 5; i++ {
		secret := big.NewInt(i * 1000)
		pk := BLSPubkeyFromSecret(secret)
		msg := []byte{byte(i)}
		sig := BLSSign(secret, msg)
		ss.Add(pk, msg, sig)
	}

	if ss.Len() != 5 {
		t.Fatalf("Len() = %d, want 5", ss.Len())
	}
	if !ss.Verify() {
		t.Fatal("batch of valid signatures should verify")
	}
}

func TestBLSSignatureSetInvalidEntry(t *testing.T) {
	ss := NewBLSSignatureSet()

	// Add invalid entry (zero pubkey).
	var zeroPK [BLSPubkeySize]byte
	var zeroSig [BLSSignatureSize]byte
	ss.Add(zeroPK, []byte("bad"), zeroSig)

	if ss.Verify() {
		t.Fatal("set with invalid entry should not verify")
	}
}

func TestComputeSigningRoot(t *testing.T) {
	var domain [32]byte
	domain[0] = 0x07
	var msgRoot [32]byte
	msgRoot[0] = 0xAB

	root1 := ComputeSigningRoot(domain, msgRoot)
	root2 := ComputeSigningRoot(domain, msgRoot)
	if root1 != root2 {
		t.Fatal("ComputeSigningRoot should be deterministic")
	}

	// Different domain -> different root.
	var domain2 [32]byte
	domain2[0] = 0x08
	root3 := ComputeSigningRoot(domain2, msgRoot)
	if root1 == root3 {
		t.Fatal("different domain should produce different root")
	}
}

func TestComputeDomain(t *testing.T) {
	domainType := [4]byte{0x07, 0x00, 0x00, 0x00}
	forkVersion := [4]byte{0x01, 0x00, 0x00, 0x00}
	var genesisRoot [32]byte

	d1 := ComputeDomain(domainType, forkVersion, genesisRoot)
	d2 := ComputeDomain(domainType, forkVersion, genesisRoot)
	if d1 != d2 {
		t.Fatal("ComputeDomain should be deterministic")
	}

	// Domain type should be in the first 4 bytes.
	if d1[0] != 0x07 || d1[1] != 0x00 || d1[2] != 0x00 || d1[3] != 0x00 {
		t.Fatal("domain type not preserved in output")
	}
}

func TestDeduplicatePubkeys(t *testing.T) {
	ba := NewBLSAgg()

	pk1 := BLSPubkeyFromSecret(big.NewInt(1))
	pk2 := BLSPubkeyFromSecret(big.NewInt(2))

	// With duplicates.
	unique, indices := ba.DeduplicatePubkeys([][BLSPubkeySize]byte{pk1, pk2, pk1, pk2, pk1})
	if len(unique) != 2 {
		t.Fatalf("expected 2 unique pubkeys, got %d", len(unique))
	}
	if len(indices) != 2 {
		t.Fatalf("expected 2 indices, got %d", len(indices))
	}
	if indices[0] != 0 || indices[1] != 1 {
		t.Fatalf("unexpected indices: %v", indices)
	}
}

func TestHasDuplicatePubkeys(t *testing.T) {
	ba := NewBLSAgg()

	pk1 := BLSPubkeyFromSecret(big.NewInt(1))
	pk2 := BLSPubkeyFromSecret(big.NewInt(2))

	if ba.HasDuplicatePubkeys([][BLSPubkeySize]byte{pk1, pk2}) {
		t.Fatal("should not detect duplicates in unique set")
	}
	if !ba.HasDuplicatePubkeys([][BLSPubkeySize]byte{pk1, pk2, pk1}) {
		t.Fatal("should detect duplicate")
	}
}

func TestRandomScalar(t *testing.T) {
	s1 := randomScalar()
	s2 := randomScalar()
	if s1.Sign() <= 0 {
		t.Fatal("random scalar should be positive")
	}
	if s2.Sign() <= 0 {
		t.Fatal("random scalar should be positive")
	}
	// Extremely unlikely to be equal.
	if s1.Cmp(s2) == 0 {
		t.Fatal("two random scalars should not be equal")
	}
}

func TestAggregateVerifyDistinct(t *testing.T) {
	ba := NewBLSAgg()

	pk1 := BLSPubkeyFromSecret(big.NewInt(300))
	pk2 := BLSPubkeyFromSecret(big.NewInt(400))

	msg1 := []byte("message one")
	msg2 := []byte("message two")
	sig1 := BLSSign(big.NewInt(300), msg1)
	sig2 := BLSSign(big.NewInt(400), msg2)

	aggSig := AggregateSignatures([][BLSSignatureSize]byte{sig1, sig2})

	ok, err := ba.AggregateVerifyDistinct(
		[][BLSPubkeySize]byte{pk1, pk2},
		[][]byte{msg1, msg2},
		aggSig,
	)
	if err != nil {
		t.Fatalf("AggregateVerifyDistinct: %v", err)
	}
	if !ok {
		t.Fatal("valid aggregate should verify")
	}
}

func TestAggregateVerifyDistinctMismatch(t *testing.T) {
	ba := NewBLSAgg()
	pk := BLSPubkeyFromSecret(big.NewInt(1))
	var sig [BLSSignatureSize]byte

	_, err := ba.AggregateVerifyDistinct(
		[][BLSPubkeySize]byte{pk},
		[][]byte{{1}, {2}},
		sig,
	)
	if err != ErrBLSAggMismatchedLengths {
		t.Fatalf("expected ErrBLSAggMismatchedLengths, got %v", err)
	}
}

func TestFastAggregateVerifyWithPoPMismatch(t *testing.T) {
	ba := NewBLSAgg()
	pk := BLSPubkeyFromSecret(big.NewInt(1))
	var sig [BLSSignatureSize]byte

	// Mismatched lengths.
	ok := ba.FastAggregateVerifyWithPoP(
		[][BLSPubkeySize]byte{pk},
		[]ProofOfPossession{},
		[]byte("msg"),
		sig,
	)
	if ok {
		t.Fatal("should fail with mismatched lengths")
	}

	// Empty.
	ok = ba.FastAggregateVerifyWithPoP(nil, nil, []byte("msg"), sig)
	if ok {
		t.Fatal("should fail with empty pubkeys")
	}
}
