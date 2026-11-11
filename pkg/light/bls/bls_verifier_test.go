package bls

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

// smallTestSize is a small committee size for fast tests.
// BLS operations on BLS12-381 are computationally expensive with big.Int,
// so we keep test committees small.
const smallTestSize = 4

func makeTestCommitteeAndSecrets(size int) ([][48]byte, []*[32]byte) {
	return MakeBLSTestCommittee(size)
}

func TestNewSyncCommitteeBLSVerifier(t *testing.T) {
	v := NewSyncCommitteeBLSVerifier()
	if v.CommitteeSize() != SyncCommitteeSize {
		t.Fatalf("expected committee size %d, got %d", SyncCommitteeSize, v.CommitteeSize())
	}
	if v.ParticipationRate() != 0 {
		t.Fatal("initial participation rate should be 0")
	}
	if v.TotalVerified() != 0 {
		t.Fatal("initial verified count should be 0")
	}
	if v.TotalFailed() != 0 {
		t.Fatal("initial failed count should be 0")
	}
}

func TestNewSyncCommitteeBLSVerifierWithSize(t *testing.T) {
	v := NewSyncCommitteeBLSVerifierWithSize(16)
	if v.CommitteeSize() != 16 {
		t.Fatalf("expected committee size 16, got %d", v.CommitteeSize())
	}
}

func TestExtractParticipants(t *testing.T) {
	committee := make([][48]byte, 8)
	for i := range committee {
		committee[i][0] = byte(i + 1) // distinct keys
	}

	// All participate.
	bits := []byte{0xFF}
	participants := extractParticipants(committee, bits)
	if len(participants) != 8 {
		t.Fatalf("expected 8 participants, got %d", len(participants))
	}

	// None participate.
	bits = []byte{0x00}
	participants = extractParticipants(committee, bits)
	if len(participants) != 0 {
		t.Fatalf("expected 0 participants, got %d", len(participants))
	}

	// First 4 participate.
	bits = []byte{0x0F}
	participants = extractParticipants(committee, bits)
	if len(participants) != 4 {
		t.Fatalf("expected 4 participants, got %d", len(participants))
	}
}

func TestMeetsQuorum(t *testing.T) {
	tests := []struct {
		participants int
		total        int
		want         bool
	}{
		{0, 0, false},
		{0, 3, false},
		{1, 3, false},
		{2, 3, true},
		{3, 3, true},
		{340, 512, false},
		{341, 512, false},
		{342, 512, true},
		{512, 512, true},
	}
	for _, tc := range tests {
		got := meetsQuorum(tc.participants, tc.total)
		if got != tc.want {
			t.Errorf("meetsQuorum(%d, %d) = %v, want %v", tc.participants, tc.total, got, tc.want)
		}
	}
}

func TestCountParticipants(t *testing.T) {
	bits := MakeParticipationBits(16, 10)
	count := CountParticipants(bits, 16)
	if count != 10 {
		t.Fatalf("expected 10 participants, got %d", count)
	}

	bits = MakeParticipationBits(16, 0)
	count = CountParticipants(bits, 16)
	if count != 0 {
		t.Fatalf("expected 0 participants, got %d", count)
	}

	bits = MakeParticipationBits(16, 16)
	count = CountParticipants(bits, 16)
	if count != 16 {
		t.Fatalf("expected 16 participants, got %d", count)
	}
}

func TestMakeParticipationBits(t *testing.T) {
	bits := MakeParticipationBits(8, 5)
	// First 5 bits should be set: 0b00011111 = 0x1F.
	if bits[0] != 0x1F {
		t.Fatalf("expected 0x1F, got 0x%02X", bits[0])
	}
}

func TestMakeBLSTestCommittee(t *testing.T) {
	pubkeys, secrets := MakeBLSTestCommittee(smallTestSize)
	if len(pubkeys) != smallTestSize {
		t.Fatalf("expected %d pubkeys, got %d", smallTestSize, len(pubkeys))
	}
	if len(secrets) != smallTestSize {
		t.Fatalf("expected %d secrets, got %d", smallTestSize, len(secrets))
	}

	// Verify each key pair is valid.
	for i := 0; i < smallTestSize; i++ {
		sk := new(big.Int).SetBytes(secrets[i][:])
		expectedPK := crypto.BLSPubkeyFromSecret(sk)
		if pubkeys[i] != expectedPK {
			t.Fatalf("key pair %d: pubkey mismatch", i)
		}
	}

	// All pubkeys should be distinct.
	seen := make(map[[48]byte]bool)
	for i, pk := range pubkeys {
		if seen[pk] {
			t.Fatalf("duplicate pubkey at index %d", i)
		}
		seen[pk] = true
	}
}

func TestVerifySyncCommitteeSignature_Valid(t *testing.T) {
	pubkeys, secrets := makeTestCommitteeAndSecrets(smallTestSize)
	v := NewSyncCommitteeBLSVerifierWithSize(smallTestSize)

	// All members participate.
	bits := MakeParticipationBits(smallTestSize, smallTestSize)
	msg := []byte("test block root for signing")

	sig := SignSyncCommitteeBLS(secrets, bits, msg)

	if !v.VerifySyncCommitteeSignature(pubkeys, bits, msg, sig) {
		t.Fatal("valid BLS sync committee signature failed verification")
	}

	if v.TotalVerified() != 1 {
		t.Fatalf("expected 1 verified, got %d", v.TotalVerified())
	}
	if v.ParticipationRate() != 1.0 {
		t.Fatalf("expected participation rate 1.0, got %f", v.ParticipationRate())
	}
}

func TestVerifySyncCommitteeSignature_WrongMessage(t *testing.T) {
	pubkeys, secrets := makeTestCommitteeAndSecrets(smallTestSize)
	v := NewSyncCommitteeBLSVerifierWithSize(smallTestSize)

	bits := MakeParticipationBits(smallTestSize, smallTestSize)
	sig := SignSyncCommitteeBLS(secrets, bits, []byte("correct message"))

	if v.VerifySyncCommitteeSignature(pubkeys, bits, []byte("wrong message"), sig) {
		t.Fatal("should reject signature for wrong message")
	}
	if v.TotalFailed() != 1 {
		t.Fatalf("expected 1 failed, got %d", v.TotalFailed())
	}
}

func TestVerifySyncCommitteeSignature_InsufficientQuorum(t *testing.T) {
	pubkeys, secrets := makeTestCommitteeAndSecrets(smallTestSize)
	v := NewSyncCommitteeBLSVerifierWithSize(smallTestSize)

	// Only 1 out of 4 participates: 25% < 66.7%.
	bits := MakeParticipationBits(smallTestSize, 1)
	msg := []byte("test")
	sig := SignSyncCommitteeBLS(secrets, bits, msg)

	if v.VerifySyncCommitteeSignature(pubkeys, bits, msg, sig) {
		t.Fatal("should reject insufficient quorum")
	}
	if v.TotalFailed() != 1 {
		t.Fatalf("expected 1 failed, got %d", v.TotalFailed())
	}
}

func TestVerifySyncCommitteeSignature_WrongCommitteeSize(t *testing.T) {
	pubkeys, _ := makeTestCommitteeAndSecrets(smallTestSize)
	v := NewSyncCommitteeBLSVerifierWithSize(smallTestSize + 1) // expects different size

	bits := MakeParticipationBits(smallTestSize, smallTestSize)
	var sig [96]byte

	if v.VerifySyncCommitteeSignature(pubkeys, bits, []byte("msg"), sig) {
		t.Fatal("should reject wrong committee size")
	}
}

func TestVerifySyncCommitteeSignature_NoParticipants(t *testing.T) {
	pubkeys, _ := makeTestCommitteeAndSecrets(smallTestSize)
	v := NewSyncCommitteeBLSVerifierWithSize(smallTestSize)

	bits := MakeParticipationBits(smallTestSize, 0) // nobody signed
	var sig [96]byte

	if v.VerifySyncCommitteeSignature(pubkeys, bits, []byte("msg"), sig) {
		t.Fatal("should reject zero participants")
	}
}

func TestVerifySyncCommitteeSignature_SupermajoritySubset(t *testing.T) {
	// Use 3 out of 4 (75% > 66.7%).
	pubkeys, secrets := makeTestCommitteeAndSecrets(smallTestSize)
	v := NewSyncCommitteeBLSVerifierWithSize(smallTestSize)

	bits := MakeParticipationBits(smallTestSize, 3) // 3 of 4
	msg := []byte("supermajority test")
	sig := SignSyncCommitteeBLS(secrets, bits, msg)

	if !v.VerifySyncCommitteeSignature(pubkeys, bits, msg, sig) {
		t.Fatal("should accept 3/4 supermajority")
	}

	expectedRate := 3.0 / 4.0
	if v.ParticipationRate() != expectedRate {
		t.Fatalf("expected participation rate %f, got %f", expectedRate, v.ParticipationRate())
	}
}

func TestVerifierCounters(t *testing.T) {
	pubkeys, secrets := makeTestCommitteeAndSecrets(smallTestSize)
	v := NewSyncCommitteeBLSVerifierWithSize(smallTestSize)

	bits := MakeParticipationBits(smallTestSize, smallTestSize)
	msg := []byte("counter test")
	sig := SignSyncCommitteeBLS(secrets, bits, msg)

	// Successful verification.
	v.VerifySyncCommitteeSignature(pubkeys, bits, msg, sig)
	if v.TotalVerified() != 1 || v.TotalFailed() != 0 {
		t.Fatal("counters wrong after success")
	}

	// Failed verification (wrong message).
	v.VerifySyncCommitteeSignature(pubkeys, bits, []byte("wrong"), sig)
	if v.TotalVerified() != 1 || v.TotalFailed() != 1 {
		t.Fatal("counters wrong after failure")
	}
}
