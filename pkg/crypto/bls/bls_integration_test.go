package bls

import (
	"bytes"
	"math/big"
	"sync"
	"testing"
)

func TestBLSIntegrationPureGoVerify(t *testing.T) {
	backend := &PureGoBLSBackend{}
	vectors := GetBLSTestVectors()
	for _, tv := range vectors {
		ok := backend.Verify(tv.Pubkey[:], tv.Message, tv.Signature[:])
		if !ok {
			t.Errorf("PureGo Verify failed for %q", tv.Name)
		}
	}
}

func TestBLSIntegrationPureGoVerifyWrongMessage(t *testing.T) {
	// Even without correct pairing, verifying a wrong message against a
	// correctly-formatted pubkey+sig should return false (point mismatch).
	backend := &PureGoBLSBackend{}
	vectors := GetBLSTestVectors()
	tv := vectors[0]
	ok := backend.Verify(tv.Pubkey[:], []byte("wrong message"), tv.Signature[:])
	if ok {
		t.Error("should reject wrong message")
	}
}

func TestBLSIntegrationPureGoVerifyWrongPubkey(t *testing.T) {
	backend := &PureGoBLSBackend{}
	vectors := GetBLSTestVectors()
	tv := vectors[0]
	otherPK := BLSPubkeyFromSecret(big.NewInt(9999))
	ok := backend.Verify(otherPK[:], tv.Message, tv.Signature[:])
	if ok {
		t.Error("should reject wrong pubkey")
	}
}

func TestBLSIntegrationAggregateVerify(t *testing.T) {
	backend := &PureGoBLSBackend{}

	secrets := []*big.Int{big.NewInt(11), big.NewInt(22), big.NewInt(33)}
	msgs := [][]byte{[]byte("msg1"), []byte("msg2"), []byte("msg3")}

	pubkeys := make([][]byte, 3)
	sigs := make([][BLSSignatureSize]byte, 3)
	for i, sk := range secrets {
		pk := BLSPubkeyFromSecret(sk)
		pubkeys[i] = pk[:]
		sigs[i] = BLSSign(sk, msgs[i])
	}

	aggSig := AggregateSignatures(sigs[:])
	ok := backend.AggregateVerify(pubkeys, msgs, aggSig[:])
	if !ok {
		t.Error("AggregateVerify should succeed with valid inputs")
	}
}

func TestBLSIntegrationAggregateVerifyInputValidation(t *testing.T) {
	backend := &PureGoBLSBackend{}

	// Mismatched lengths.
	ok := backend.AggregateVerify(
		[][]byte{make([]byte, BLSPubkeySize)},
		[][]byte{[]byte("msg1"), []byte("msg2")},
		make([]byte, BLSSignatureSize),
	)
	if ok {
		t.Error("AggregateVerify should reject mismatched pubkeys/msgs lengths")
	}

	// Wrong pubkey length.
	ok = backend.AggregateVerify(
		[][]byte{make([]byte, 10)},
		[][]byte{[]byte("msg1")},
		make([]byte, BLSSignatureSize),
	)
	if ok {
		t.Error("AggregateVerify should reject wrong pubkey length")
	}
}

func TestBLSIntegrationFastAggregateVerify(t *testing.T) {
	backend := &PureGoBLSBackend{}

	msg := []byte("common message")
	secrets := []*big.Int{big.NewInt(100), big.NewInt(200), big.NewInt(300)}
	pubkeys := make([][]byte, 3)
	sigs := make([][BLSSignatureSize]byte, 3)
	for i, sk := range secrets {
		pk := BLSPubkeyFromSecret(sk)
		pubkeys[i] = pk[:]
		sigs[i] = BLSSign(sk, msg)
	}

	aggSig := AggregateSignatures(sigs[:])
	ok := backend.FastAggregateVerify(pubkeys, msg, aggSig[:])
	if !ok {
		t.Error("FastAggregateVerify should succeed with valid inputs")
	}
}

func TestBLSIntegrationFastAggregateVerifyInputValidation(t *testing.T) {
	backend := &PureGoBLSBackend{}

	// Wrong pubkey size in slice.
	ok := backend.FastAggregateVerify(
		[][]byte{make([]byte, 5)},
		[]byte("msg"),
		make([]byte, BLSSignatureSize),
	)
	if ok {
		t.Error("FastAggregateVerify should reject wrong pubkey length")
	}

	// Wrong sig size.
	ok = backend.FastAggregateVerify(
		[][]byte{make([]byte, BLSPubkeySize)},
		[]byte("msg"),
		make([]byte, 10),
	)
	if ok {
		t.Error("FastAggregateVerify should reject wrong sig length")
	}
}

func TestBLSIntegrationInvalidSigRejection(t *testing.T) {
	backend := &PureGoBLSBackend{}
	vectors := GetBLSTestVectors()
	tv := vectors[0]

	// Zero signature (no compression flag).
	zeroSig := make([]byte, BLSSignatureSize)
	ok := backend.Verify(tv.Pubkey[:], tv.Message, zeroSig)
	if ok {
		t.Error("should reject zero signature")
	}
}

func TestBLSIntegrationInvalidPubkeyFormat(t *testing.T) {
	backend := &PureGoBLSBackend{}
	vectors := GetBLSTestVectors()
	tv := vectors[0]

	// Short pubkey.
	ok := backend.Verify([]byte{0x01, 0x02}, tv.Message, tv.Signature[:])
	if ok {
		t.Error("should reject short pubkey")
	}

	// Wrong-length signature.
	ok = backend.Verify(tv.Pubkey[:], tv.Message, []byte{0x80})
	if ok {
		t.Error("should reject short signature")
	}
}

func TestBLSIntegrationBackendSwitching(t *testing.T) {
	original := DefaultBLSBackend()
	t.Cleanup(func() { SetBLSBackend(original) })

	// Switch to blst.
	SetBLSBackend(&BlstBLSBackend{})
	if BLSIntegrationStatus() != "blst" {
		t.Errorf("status should be blst, got %q", BLSIntegrationStatus())
	}

	// Switch back.
	SetBLSBackend(nil)
	if BLSIntegrationStatus() != "pure-go" {
		t.Errorf("status should be pure-go after nil reset, got %q", BLSIntegrationStatus())
	}
}

func TestBLSIntegrationBlstBackendPlaceholder(t *testing.T) {
	blst := &BlstBLSBackend{}
	if blst.Name() != "blst" {
		t.Errorf("blst backend Name = %q", blst.Name())
	}
	// Placeholder should always return false.
	if blst.Verify(nil, nil, nil) {
		t.Error("blst placeholder should return false")
	}
	if blst.AggregateVerify(nil, nil, nil) {
		t.Error("blst placeholder AggregateVerify should return false")
	}
	if blst.FastAggregateVerify(nil, nil, nil) {
		t.Error("blst placeholder FastAggregateVerify should return false")
	}
}

func TestBLSIntegrationG1GeneratorValidation(t *testing.T) {
	gen := BLSG1GeneratorCompressed
	if gen[0]&0x80 == 0 {
		t.Error("G1 generator should have compression flag set")
	}
	if gen[0]&0x40 != 0 {
		t.Error("G1 generator should not be infinity")
	}
	if err := ValidateBLSPubkey(gen[:]); err != nil {
		t.Errorf("G1 generator should be a valid pubkey: %v", err)
	}
}

func TestBLSIntegrationG2GeneratorValidation(t *testing.T) {
	gen := BLSG2GeneratorCompressed
	if gen[0]&0x80 == 0 {
		t.Error("G2 generator should have compression flag set")
	}
	if gen[0]&0x40 != 0 {
		t.Error("G2 generator should not be infinity")
	}
	if err := ValidateBLSSignature(gen[:]); err != nil {
		t.Errorf("G2 generator should pass signature format validation: %v", err)
	}
}

func TestBLSIntegrationDomainSeparationTag(t *testing.T) {
	expected := "BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_"
	if string(BLSSignatureDST) != expected {
		t.Errorf("DST = %q, want %q", string(BLSSignatureDST), expected)
	}
	if len(BLSSignatureDST) != 43 {
		t.Errorf("DST length = %d, want 43", len(BLSSignatureDST))
	}
}

func TestBLSIntegrationNilInputs(t *testing.T) {
	backend := &PureGoBLSBackend{}

	if backend.Verify(nil, nil, nil) {
		t.Error("Verify(nil,nil,nil) should return false")
	}
	if backend.AggregateVerify(nil, nil, nil) {
		t.Error("AggregateVerify(nil,nil,nil) should return false")
	}
	if backend.FastAggregateVerify(nil, nil, nil) {
		t.Error("FastAggregateVerify(nil,nil,nil) should return false")
	}

	// Empty slices.
	if backend.AggregateVerify([][]byte{}, [][]byte{}, make([]byte, BLSSignatureSize)) {
		t.Error("AggregateVerify with empty pubkeys should return false")
	}
	if backend.FastAggregateVerify([][]byte{}, []byte("msg"), make([]byte, BLSSignatureSize)) {
		t.Error("FastAggregateVerify with empty pubkeys should return false")
	}
}

func TestBLSIntegrationValidatePubkey(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr error
	}{
		{"nil", nil, ErrBLSInvalidPubkeyLen},
		{"empty", []byte{}, ErrBLSInvalidPubkeyLen},
		{"too_short", make([]byte, 47), ErrBLSInvalidPubkeyLen},
		{"too_long", make([]byte, 49), ErrBLSInvalidPubkeyLen},
		{"no_compress_flag", make([]byte, 48), ErrBLSInvalidPubkeyFormat},
		{"infinity", BLSPointAtInfinityG1[:], ErrBLSInvalidPubkeyInf},
		{"valid_generator", BLSG1GeneratorCompressed[:], nil},
	}
	for _, tt := range tests {
		err := ValidateBLSPubkey(tt.input)
		if err != tt.wantErr {
			t.Errorf("%s: got err=%v, want %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestBLSIntegrationValidateSignature(t *testing.T) {
	tests := []struct {
		name    string
		input   []byte
		wantErr error
	}{
		{"nil", nil, ErrBLSInvalidSigLen},
		{"too_short", make([]byte, 95), ErrBLSInvalidSigLen},
		{"too_long", make([]byte, 97), ErrBLSInvalidSigLen},
		{"no_compress_flag", make([]byte, 96), ErrBLSInvalidSigFormat},
		{"valid_infinity", BLSPointAtInfinityG2[:], nil},
		{"valid_generator", BLSG2GeneratorCompressed[:], nil},
	}
	for _, tt := range tests {
		err := ValidateBLSSignature(tt.input)
		if err != tt.wantErr {
			t.Errorf("%s: got err=%v, want %v", tt.name, err, tt.wantErr)
		}
	}
}

func TestBLSIntegrationConcurrentVerify(t *testing.T) {
	// Test concurrent access to the backend (format validation and
	// serialization, not pairing) to verify thread safety.
	var wg sync.WaitGroup
	errCh := make(chan string, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := ValidateBLSPubkey(BLSG1GeneratorCompressed[:])
			if err != nil {
				errCh <- "concurrent ValidateBLSPubkey failed"
			}
			err = ValidateBLSSignature(BLSG2GeneratorCompressed[:])
			if err != nil {
				errCh <- "concurrent ValidateBLSSignature failed"
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for e := range errCh {
		t.Error(e)
	}
}

func TestBLSIntegrationConcurrentBackendSwitch(t *testing.T) {
	// Verify SetBLSBackend/DefaultBLSBackend are safe under concurrency.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			SetBLSBackend(&PureGoBLSBackend{})
		}()
		go func() {
			defer wg.Done()
			_ = DefaultBLSBackend().Name()
		}()
	}
	wg.Wait()
	// Reset to default.
	SetBLSBackend(nil)
	if BLSIntegrationStatus() != "pure-go" {
		t.Errorf("after concurrent ops, status should be pure-go, got %q", BLSIntegrationStatus())
	}
}

func TestBLSIntegrationVerifyWithBackendNil(t *testing.T) {
	ok := BLSVerifyWithBackend(nil, nil, nil, nil)
	if ok {
		t.Error("BLSVerifyWithBackend(nil, ...) should return false")
	}
}

func TestBLSIntegrationVerifyWithBackendPureGo(t *testing.T) {
	backend := &PureGoBLSBackend{}
	vectors := GetBLSTestVectors()
	tv := vectors[1]
	ok := BLSVerifyWithBackend(backend, tv.Pubkey[:], tv.Message, tv.Signature[:])
	if !ok {
		t.Error("BLSVerifyWithBackend should succeed with valid inputs")
	}
}

func TestBLSIntegrationSubgroupOrder(t *testing.T) {
	if BLSSubgroupOrder.Cmp(blsR) != 0 {
		t.Errorf("BLSSubgroupOrder does not match blsR")
	}
	expected := "73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001"
	if BLSSubgroupOrder.Text(16) != expected {
		t.Errorf("BLSSubgroupOrder hex mismatch: %s", BLSSubgroupOrder.Text(16))
	}
}

func TestBLSIntegrationPointAtInfinity(t *testing.T) {
	if BLSPointAtInfinityG1[0] != 0xC0 {
		t.Errorf("G1 infinity first byte = 0x%x, want 0xC0", BLSPointAtInfinityG1[0])
	}
	for i := 1; i < 48; i++ {
		if BLSPointAtInfinityG1[i] != 0 {
			t.Errorf("G1 infinity byte %d = 0x%x, want 0", i, BLSPointAtInfinityG1[i])
		}
	}
	if BLSPointAtInfinityG2[0] != 0xC0 {
		t.Errorf("G2 infinity first byte = 0x%x, want 0xC0", BLSPointAtInfinityG2[0])
	}
	for i := 1; i < 96; i++ {
		if BLSPointAtInfinityG2[i] != 0 {
			t.Errorf("G2 infinity byte %d = 0x%x, want 0", i, BLSPointAtInfinityG2[i])
		}
	}
}

func TestBLSIntegrationG1GeneratorRoundTrip(t *testing.T) {
	gen := BlsG1Generator()
	serialized := SerializeG1(gen)
	if !bytes.Equal(serialized[:], BLSG1GeneratorCompressed[:]) {
		t.Errorf("G1 generator serialization mismatch:\n  got:  %x\n  want: %x",
			serialized[:], BLSG1GeneratorCompressed[:])
	}
	deser := DeserializeG1(serialized)
	if deser == nil {
		t.Fatal("failed to deserialize G1 generator")
	}
	if deser.blsG1IsInfinity() {
		t.Error("deserialized G1 generator should not be infinity")
	}
}

func TestBLSIntegrationG2GeneratorRoundTrip(t *testing.T) {
	gen := BlsG2Generator()
	serialized := SerializeG2(gen)
	if !bytes.Equal(serialized[:], BLSG2GeneratorCompressed[:]) {
		t.Errorf("G2 generator serialization mismatch:\n  got:  %x\n  want: %x",
			serialized[:], BLSG2GeneratorCompressed[:])
	}
	deser := DeserializeG2(serialized)
	if deser == nil {
		t.Fatal("failed to deserialize G2 generator")
	}
	if deser.blsG2IsInfinity() {
		t.Error("deserialized G2 generator should not be infinity")
	}
}

func TestBLSIntegrationTestVectorCount(t *testing.T) {
	vectors := GetBLSTestVectors()
	if len(vectors) < 3 {
		t.Errorf("expected at least 3 test vectors, got %d", len(vectors))
	}
	for _, tv := range vectors {
		if tv.Name == "" {
			t.Error("test vector name is empty")
		}
		if len(tv.Message) == 0 {
			t.Error("test vector message is empty")
		}
		if tv.SecretKey == nil || tv.SecretKey.Sign() == 0 {
			t.Error("test vector secret key is zero")
		}
	}
}

func TestBLSIntegrationTestVectorSignatureFormat(t *testing.T) {
	vectors := GetBLSTestVectors()
	for _, tv := range vectors {
		// Each signature should have the compression flag set.
		if tv.Signature[0]&0x80 == 0 {
			t.Errorf("%s: signature missing compression flag", tv.Name)
		}
		// Each pubkey should be valid.
		if err := ValidateBLSPubkey(tv.Pubkey[:]); err != nil {
			t.Errorf("%s: invalid pubkey: %v", tv.Name, err)
		}
		// Each signature should pass format validation.
		if err := ValidateBLSSignature(tv.Signature[:]); err != nil {
			t.Errorf("%s: invalid signature format: %v", tv.Name, err)
		}
	}
}

func TestBLSIntegrationTestVectorDeterminism(t *testing.T) {
	// Signing the same message with the same key should produce the same sig.
	sk := big.NewInt(42)
	msg := []byte("hello")
	sig1 := BLSSign(sk, msg)
	sig2 := BLSSign(sk, msg)
	if sig1 != sig2 {
		t.Error("BLSSign should be deterministic")
	}
}

func TestBLSIntegrationPubkeyDeterminism(t *testing.T) {
	sk := big.NewInt(12345)
	pk1 := BLSPubkeyFromSecret(sk)
	pk2 := BLSPubkeyFromSecret(sk)
	if pk1 != pk2 {
		t.Error("BLSPubkeyFromSecret should be deterministic")
	}
}

func TestBLSIntegrationValidatePubkeyXCoordRange(t *testing.T) {
	// Create a pubkey where x >= p (should fail).
	buf := make([]byte, 48)
	buf[0] = 0x80 | 0x1F // compression flag + max remaining bits
	for i := 1; i < 48; i++ {
		buf[i] = 0xFF
	}
	if err := ValidateBLSPubkey(buf); err != ErrBLSInvalidPubkeyFormat {
		t.Errorf("expected ErrBLSInvalidPubkeyFormat for x >= p, got %v", err)
	}
}
