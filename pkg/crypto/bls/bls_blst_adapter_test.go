//go:build blst

package bls

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"math/big"
	"testing"
)

// makeIKM generates deterministic IKM of the required 32-byte minimum length.
func makeIKM(seed byte) []byte {
	ikm := make([]byte, 32)
	for i := range ikm {
		ikm[i] = seed ^ byte(i*17+3)
	}
	return ikm
}

// makeRandomIKM generates cryptographically random 32-byte IKM.
func makeRandomIKM() []byte {
	ikm := make([]byte, 32)
	if _, err := rand.Read(ikm); err != nil {
		panic("failed to generate random IKM")
	}
	return ikm
}

func TestBlstRealBackendName(t *testing.T) {
	backend := &BlstRealBackend{}
	name := backend.Name()
	if name != "blst-real" {
		t.Errorf("expected name 'blst-real', got %q", name)
	}
}

func TestBlstKeyGen(t *testing.T) {
	ikm := makeIKM(0x42)
	pk, sk, err := BlstKeyGen(ikm)
	if err != nil {
		t.Fatalf("BlstKeyGen failed: %v", err)
	}
	if len(pk) != 48 {
		t.Errorf("expected pubkey length 48, got %d", len(pk))
	}
	if len(sk) != 32 {
		t.Errorf("expected secret key length 32, got %d", len(sk))
	}
	// Pubkey should have compression bit set.
	if pk[0]&0x80 == 0 {
		t.Error("pubkey compression bit not set")
	}
}

func TestBlstSignVerifyRoundtrip(t *testing.T) {
	ikm := makeIKM(0x01)
	pk, sk, err := BlstKeyGen(ikm)
	if err != nil {
		t.Fatalf("BlstKeyGen failed: %v", err)
	}

	msg := []byte("hello eth2030 consensus layer")
	sig, err := BlstSign(sk, msg)
	if err != nil {
		t.Fatalf("BlstSign failed: %v", err)
	}
	if len(sig) != 96 {
		t.Errorf("expected sig length 96, got %d", len(sig))
	}

	backend := &BlstRealBackend{}
	if !backend.Verify(pk, msg, sig) {
		t.Error("Verify returned false for valid signature")
	}
}

func TestBlstVerifyWrongMessage(t *testing.T) {
	ikm := makeIKM(0x02)
	pk, sk, err := BlstKeyGen(ikm)
	if err != nil {
		t.Fatalf("BlstKeyGen failed: %v", err)
	}

	msg1 := []byte("correct message")
	msg2 := []byte("wrong message")
	sig, err := BlstSign(sk, msg1)
	if err != nil {
		t.Fatalf("BlstSign failed: %v", err)
	}

	backend := &BlstRealBackend{}
	if backend.Verify(pk, msg2, sig) {
		t.Error("Verify should return false for wrong message")
	}
}

func TestBlstVerifyInvalidSig(t *testing.T) {
	ikm := makeIKM(0x03)
	pk, _, err := BlstKeyGen(ikm)
	if err != nil {
		t.Fatalf("BlstKeyGen failed: %v", err)
	}

	msg := []byte("test message")
	// Random bytes are not a valid compressed G2 point.
	badSig := make([]byte, 96)
	rand.Read(badSig)

	backend := &BlstRealBackend{}
	if backend.Verify(pk, msg, badSig) {
		t.Error("Verify should return false for invalid signature bytes")
	}
}

func TestBlstVerifyInvalidPubkey(t *testing.T) {
	ikm := makeIKM(0x04)
	_, sk, err := BlstKeyGen(ikm)
	if err != nil {
		t.Fatalf("BlstKeyGen failed: %v", err)
	}

	msg := []byte("test message")
	sig, err := BlstSign(sk, msg)
	if err != nil {
		t.Fatalf("BlstSign failed: %v", err)
	}

	// Random bytes are not a valid compressed G1 point.
	badPK := make([]byte, 48)
	rand.Read(badPK)

	backend := &BlstRealBackend{}
	if backend.Verify(badPK, msg, sig) {
		t.Error("Verify should return false for invalid pubkey bytes")
	}
}

func TestBlstVerifyEmptyInputs(t *testing.T) {
	backend := &BlstRealBackend{}

	// All nil/empty combinations should return false without panicking.
	cases := []struct {
		name string
		pk   []byte
		msg  []byte
		sig  []byte
	}{
		{"nil pubkey", nil, []byte("msg"), make([]byte, 96)},
		{"empty pubkey", []byte{}, []byte("msg"), make([]byte, 96)},
		{"nil sig", make([]byte, 48), []byte("msg"), nil},
		{"empty sig", make([]byte, 48), []byte("msg"), []byte{}},
		{"nil msg (valid length pk/sig)", make([]byte, 48), nil, make([]byte, 96)},
		{"all nil", nil, nil, nil},
		{"all empty", []byte{}, []byte{}, []byte{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Should not panic.
			result := backend.Verify(tc.pk, tc.msg, tc.sig)
			if result {
				t.Error("Verify should return false for empty/nil inputs")
			}
		})
	}
}

func TestBlstAggregateVerify(t *testing.T) {
	// Three signers, each signs a different message.
	backend := &BlstRealBackend{}
	numSigners := 3
	pks := make([][]byte, numSigners)
	msgs := make([][]byte, numSigners)
	sigs := make([][]byte, numSigners)

	for i := 0; i < numSigners; i++ {
		ikm := makeIKM(byte(0x10 + i))
		pk, sk, err := BlstKeyGen(ikm)
		if err != nil {
			t.Fatalf("BlstKeyGen[%d] failed: %v", i, err)
		}
		pks[i] = pk
		msgs[i] = []byte(fmt.Sprintf("message number %d", i))
		sig, err := BlstSign(sk, msgs[i])
		if err != nil {
			t.Fatalf("BlstSign[%d] failed: %v", i, err)
		}
		sigs[i] = sig
	}

	aggSig, err := BlstAggregateSigs(sigs)
	if err != nil {
		t.Fatalf("BlstAggregateSigs failed: %v", err)
	}

	if !backend.AggregateVerify(pks, msgs, aggSig) {
		t.Error("AggregateVerify returned false for valid aggregate")
	}
}

func TestBlstFastAggregateVerify(t *testing.T) {
	// Three signers, all sign the same message.
	backend := &BlstRealBackend{}
	numSigners := 3
	pks := make([][]byte, numSigners)
	sigs := make([][]byte, numSigners)
	msg := []byte("common attestation message")

	for i := 0; i < numSigners; i++ {
		ikm := makeIKM(byte(0x20 + i))
		pk, sk, err := BlstKeyGen(ikm)
		if err != nil {
			t.Fatalf("BlstKeyGen[%d] failed: %v", i, err)
		}
		pks[i] = pk
		sig, err := BlstSign(sk, msg)
		if err != nil {
			t.Fatalf("BlstSign[%d] failed: %v", i, err)
		}
		sigs[i] = sig
	}

	aggSig, err := BlstAggregateSigs(sigs)
	if err != nil {
		t.Fatalf("BlstAggregateSigs failed: %v", err)
	}

	if !backend.FastAggregateVerify(pks, msg, aggSig) {
		t.Error("FastAggregateVerify returned false for valid aggregate")
	}
}

func TestBlstAggregateVerifyWrongMsg(t *testing.T) {
	backend := &BlstRealBackend{}
	numSigners := 3
	pks := make([][]byte, numSigners)
	msgs := make([][]byte, numSigners)
	sigs := make([][]byte, numSigners)

	for i := 0; i < numSigners; i++ {
		ikm := makeIKM(byte(0x30 + i))
		pk, sk, err := BlstKeyGen(ikm)
		if err != nil {
			t.Fatalf("BlstKeyGen[%d] failed: %v", i, err)
		}
		pks[i] = pk
		msgs[i] = []byte(fmt.Sprintf("message %d", i))
		sig, err := BlstSign(sk, msgs[i])
		if err != nil {
			t.Fatalf("BlstSign[%d] failed: %v", i, err)
		}
		sigs[i] = sig
	}

	aggSig, err := BlstAggregateSigs(sigs)
	if err != nil {
		t.Fatalf("BlstAggregateSigs failed: %v", err)
	}

	// Tamper with one message.
	msgs[1] = []byte("tampered message")
	if backend.AggregateVerify(pks, msgs, aggSig) {
		t.Error("AggregateVerify should return false with tampered message")
	}
}

func TestBlstFastAggregateVerifyWrongMsg(t *testing.T) {
	backend := &BlstRealBackend{}
	numSigners := 3
	pks := make([][]byte, numSigners)
	sigs := make([][]byte, numSigners)
	msg := []byte("original message")

	for i := 0; i < numSigners; i++ {
		ikm := makeIKM(byte(0x40 + i))
		pk, sk, err := BlstKeyGen(ikm)
		if err != nil {
			t.Fatalf("BlstKeyGen[%d] failed: %v", i, err)
		}
		pks[i] = pk
		sig, err := BlstSign(sk, msg)
		if err != nil {
			t.Fatalf("BlstSign[%d] failed: %v", i, err)
		}
		sigs[i] = sig
	}

	aggSig, err := BlstAggregateSigs(sigs)
	if err != nil {
		t.Fatalf("BlstAggregateSigs failed: %v", err)
	}

	wrongMsg := []byte("wrong message")
	if backend.FastAggregateVerify(pks, wrongMsg, aggSig) {
		t.Error("FastAggregateVerify should return false with wrong message")
	}
}

func TestBlstKeyGenDeterminism(t *testing.T) {
	ikm := makeIKM(0x50)

	pk1, sk1, err := BlstKeyGen(ikm)
	if err != nil {
		t.Fatalf("BlstKeyGen(1) failed: %v", err)
	}
	pk2, sk2, err := BlstKeyGen(ikm)
	if err != nil {
		t.Fatalf("BlstKeyGen(2) failed: %v", err)
	}

	if !bytes.Equal(pk1, pk2) {
		t.Error("same IKM produced different public keys")
	}
	if !bytes.Equal(sk1, sk2) {
		t.Error("same IKM produced different secret keys")
	}
}

func TestBlstKeyGenDifferentIKM(t *testing.T) {
	ikm1 := makeIKM(0x60)
	ikm2 := makeIKM(0x61)

	pk1, sk1, err := BlstKeyGen(ikm1)
	if err != nil {
		t.Fatalf("BlstKeyGen(1) failed: %v", err)
	}
	pk2, sk2, err := BlstKeyGen(ikm2)
	if err != nil {
		t.Fatalf("BlstKeyGen(2) failed: %v", err)
	}

	if bytes.Equal(pk1, pk2) {
		t.Error("different IKM produced same public keys")
	}
	if bytes.Equal(sk1, sk2) {
		t.Error("different IKM produced same secret keys")
	}
}

func TestBlstAggregateSigs(t *testing.T) {
	backend := &BlstRealBackend{}
	numSigners := 5
	pks := make([][]byte, numSigners)
	sigs := make([][]byte, numSigners)
	msg := []byte("aggregate test message")

	for i := 0; i < numSigners; i++ {
		ikm := makeIKM(byte(0x70 + i))
		pk, sk, err := BlstKeyGen(ikm)
		if err != nil {
			t.Fatalf("BlstKeyGen[%d] failed: %v", i, err)
		}
		pks[i] = pk
		sig, err := BlstSign(sk, msg)
		if err != nil {
			t.Fatalf("BlstSign[%d] failed: %v", i, err)
		}
		sigs[i] = sig
	}

	aggSig, err := BlstAggregateSigs(sigs)
	if err != nil {
		t.Fatalf("BlstAggregateSigs failed: %v", err)
	}
	if len(aggSig) != 96 {
		t.Errorf("expected aggregate sig length 96, got %d", len(aggSig))
	}

	// Verify the aggregate.
	if !backend.FastAggregateVerify(pks, msg, aggSig) {
		t.Error("FastAggregateVerify failed for 5-signer aggregate")
	}

	// Verify that aggregating empty sigs fails.
	_, err = BlstAggregateSigs(nil)
	if err != ErrBlstNoSignatures {
		t.Errorf("expected ErrBlstNoSignatures, got %v", err)
	}
}

func TestBlstMultipleSignVerify(t *testing.T) {
	backend := &BlstRealBackend{}
	ikm := makeIKM(0x80)
	pk, sk, err := BlstKeyGen(ikm)
	if err != nil {
		t.Fatalf("BlstKeyGen failed: %v", err)
	}

	for i := 0; i < 100; i++ {
		msg := []byte(fmt.Sprintf("message iteration %d with extra data to sign", i))
		sig, err := BlstSign(sk, msg)
		if err != nil {
			t.Fatalf("BlstSign[%d] failed: %v", i, err)
		}
		if !backend.Verify(pk, msg, sig) {
			t.Errorf("Verify failed for message %d", i)
		}
	}
}

func TestBlstBLSBackendInterface(t *testing.T) {
	// Compile-time check that BlstRealBackend satisfies BLSBackend.
	var _ BLSBackend = (*BlstRealBackend)(nil)

	// Runtime check via the interface.
	var backend BLSBackend = &BlstRealBackend{}
	if backend.Name() != "blst-real" {
		t.Errorf("interface Name() returned %q, expected 'blst-real'", backend.Name())
	}
}

// TestBLSTBackend verifies all five BLS operations of BlstRealBackend (GAP-7.1).
func TestBLSTBackend(t *testing.T) {
	backend := &BlstRealBackend{}

	// Key generation.
	ikm := make([]byte, 32)
	for i := range ikm {
		ikm[i] = byte(i + 1)
	}
	pk, sk, err := BlstKeyGen(ikm)
	if err != nil {
		t.Fatalf("BlstKeyGen: %v", err)
	}
	if len(pk) != blstPubkeySize {
		t.Errorf("pubkey len = %d, want %d", len(pk), blstPubkeySize)
	}

	msg := []byte("test-bls-message")
	sig, err := BlstSign(sk, msg)
	if err != nil {
		t.Fatalf("BlstSign: %v", err)
	}
	if len(sig) != blstSigSize {
		t.Errorf("sig len = %d, want %d", len(sig), blstSigSize)
	}

	// Verify (single).
	if !backend.Verify(pk, msg, sig) {
		t.Error("Verify failed for valid signature")
	}
	pure := &PureGoBLSBackend{}
	if !pure.Verify(pk, msg, sig) {
		t.Error("PureGo rejected a blst signature from an independently generated key")
	}
	fromPure := BLSSign(new(big.Int).SetBytes(sk), msg)
	if !bytes.Equal(fromPure[:], sig) || !backend.Verify(pk, msg, fromPure[:]) {
		t.Error("PureGo and blst produced different signatures for the same key and message")
	}

	// FastAggregateVerify with single key.
	if !backend.FastAggregateVerify([][]byte{pk}, msg, sig) {
		t.Error("FastAggregateVerify failed for single key")
	}

	// AggregateVerify with single key/message.
	if !backend.AggregateVerify([][]byte{pk}, [][]byte{msg}, sig) {
		t.Error("AggregateVerify failed for single key/message")
	}

	// Wrong message must fail.
	if backend.Verify(pk, []byte("wrong-msg"), sig) {
		t.Error("Verify should fail for wrong message")
	}
}

// TestBLSTBackendDefault verifies that after init(), blst is the default
// backend when built with the blst tag (GAP-7.2).
func TestBLSTBackendDefault(t *testing.T) {
	backend := DefaultBLSBackend()
	if backend.Name() != "blst-real" {
		t.Errorf("default backend = %q, want \"blst-real\"", backend.Name())
	}
}

// TestBLSAttestation verifies the BLS backend can verify a consensus-style
// attestation aggregate with the blst library (GAP-7.2).
func TestBLSAttestation(t *testing.T) {
	backend := &BlstRealBackend{}
	const numAttesters = 10

	// Generate keys and signatures for numAttesters.
	pks := make([][]byte, numAttesters)
	sigs := make([][]byte, numAttesters)
	msg := []byte("beacon-block-root-attestation")

	for i := 0; i < numAttesters; i++ {
		ikm := make([]byte, 32)
		ikm[0] = byte(i + 1)
		pk, sk, err := BlstKeyGen(ikm)
		if err != nil {
			t.Fatalf("BlstKeyGen[%d]: %v", i, err)
		}
		pks[i] = pk
		sig, err := BlstSign(sk, msg)
		if err != nil {
			t.Fatalf("BlstSign[%d]: %v", i, err)
		}
		sigs[i] = sig
	}

	// Aggregate signatures.
	aggSig, err := BlstAggregateSigs(sigs)
	if err != nil {
		t.Fatalf("BlstAggregateSigs: %v", err)
	}

	// FastAggregateVerify (all attesters signed the same message).
	if !backend.FastAggregateVerify(pks, msg, aggSig) {
		t.Error("FastAggregateVerify failed for attestation aggregate")
	}
}

func TestBlstKeyGenShortIKM(t *testing.T) {
	// IKM shorter than 32 bytes should fail.
	shortIKM := make([]byte, 16)
	_, _, err := BlstKeyGen(shortIKM)
	if err != ErrBlstInvalidIKM {
		t.Errorf("expected ErrBlstInvalidIKM for short IKM, got %v", err)
	}
}

func TestBlstSignInvalidSecretKey(t *testing.T) {
	// Invalid (too short) secret key should fail.
	_, err := BlstSign([]byte{1, 2, 3}, []byte("msg"))
	if err != ErrBlstInvalidSecretKey {
		t.Errorf("expected ErrBlstInvalidSecretKey, got %v", err)
	}
}

func TestBlstAggregateVerifyEmptyInputs(t *testing.T) {
	backend := &BlstRealBackend{}

	// Empty pubkeys/msgs.
	if backend.AggregateVerify(nil, nil, make([]byte, 96)) {
		t.Error("AggregateVerify should return false for nil pubkeys")
	}
	if backend.AggregateVerify([][]byte{}, [][]byte{}, make([]byte, 96)) {
		t.Error("AggregateVerify should return false for empty pubkeys")
	}

	// Mismatched lengths.
	if backend.AggregateVerify([][]byte{{1}}, [][]byte{{1}, {2}}, make([]byte, 96)) {
		t.Error("AggregateVerify should return false for mismatched lengths")
	}
}

func TestBlstFastAggregateVerifyEmptyInputs(t *testing.T) {
	backend := &BlstRealBackend{}

	if backend.FastAggregateVerify(nil, []byte("msg"), make([]byte, 96)) {
		t.Error("FastAggregateVerify should return false for nil pubkeys")
	}
	if backend.FastAggregateVerify([][]byte{}, []byte("msg"), make([]byte, 96)) {
		t.Error("FastAggregateVerify should return false for empty pubkeys")
	}
}

func TestBlstSetAsActiveBackend(t *testing.T) {
	// Verify that BlstRealBackend can be set as the active BLS backend.
	original := DefaultBLSBackend()
	defer SetBLSBackend(original)

	SetBLSBackend(&BlstRealBackend{})
	active := DefaultBLSBackend()
	if active.Name() != "blst-real" {
		t.Errorf("expected active backend 'blst-real', got %q", active.Name())
	}

	// Verify a signature through the active backend.
	ikm := makeIKM(0x90)
	pk, sk, err := BlstKeyGen(ikm)
	if err != nil {
		t.Fatalf("BlstKeyGen failed: %v", err)
	}
	msg := []byte("test via active backend")
	sig, err := BlstSign(sk, msg)
	if err != nil {
		t.Fatalf("BlstSign failed: %v", err)
	}
	if !BLSVerifyWithBackend(active, pk, msg, sig) {
		t.Error("BLSVerifyWithBackend failed through active blst backend")
	}
}

func TestBlstCrossSignerVerifyFails(t *testing.T) {
	// Signature from one key should not verify with a different key.
	backend := &BlstRealBackend{}
	ikm1 := makeIKM(0xA0)
	ikm2 := makeIKM(0xA1)

	pk1, sk1, _ := BlstKeyGen(ikm1)
	pk2, _, _ := BlstKeyGen(ikm2)

	msg := []byte("cross signer test")
	sig, _ := BlstSign(sk1, msg)

	if !backend.Verify(pk1, msg, sig) {
		t.Error("Verify should succeed with correct key")
	}
	if backend.Verify(pk2, msg, sig) {
		t.Error("Verify should fail with wrong key")
	}
}

// --- Benchmarks ---

func BenchmarkBlstVerify(b *testing.B) {
	ikm := makeRandomIKM()
	pk, sk, err := BlstKeyGen(ikm)
	if err != nil {
		b.Fatalf("BlstKeyGen failed: %v", err)
	}
	msg := []byte("benchmark verify message for BLS12-381")
	sig, err := BlstSign(sk, msg)
	if err != nil {
		b.Fatalf("BlstSign failed: %v", err)
	}
	backend := &BlstRealBackend{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !backend.Verify(pk, msg, sig) {
			b.Fatal("Verify failed during benchmark")
		}
	}
}

func BenchmarkBlstFastAggregateVerify(b *testing.B) {
	numSigners := 100
	pks := make([][]byte, numSigners)
	sigs := make([][]byte, numSigners)
	msg := []byte("benchmark fast aggregate verify")

	for i := 0; i < numSigners; i++ {
		ikm := makeRandomIKM()
		pk, sk, err := BlstKeyGen(ikm)
		if err != nil {
			b.Fatalf("BlstKeyGen[%d] failed: %v", i, err)
		}
		pks[i] = pk
		sig, err := BlstSign(sk, msg)
		if err != nil {
			b.Fatalf("BlstSign[%d] failed: %v", i, err)
		}
		sigs[i] = sig
	}

	aggSig, err := BlstAggregateSigs(sigs)
	if err != nil {
		b.Fatalf("BlstAggregateSigs failed: %v", err)
	}
	backend := &BlstRealBackend{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !backend.FastAggregateVerify(pks, msg, aggSig) {
			b.Fatal("FastAggregateVerify failed during benchmark")
		}
	}
}
