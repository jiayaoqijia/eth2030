package kzg

import (
	"encoding/binary"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestKZGVerifierPointEvaluation(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())

	eval := MakeTestPointEvaluation(0)
	valid, err := verifier.VerifyPointEvaluation(eval)
	if err != nil {
		t.Fatalf("VerifyPointEvaluation failed: %v", err)
	}
	if !valid {
		t.Error("expected valid point evaluation")
	}
}

func TestKZGVerifierPointEvaluationNil(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())
	_, err := verifier.VerifyPointEvaluation(nil)
	if err != ErrKZGNilProof {
		t.Errorf("expected ErrKZGNilProof, got %v", err)
	}
}

func TestKZGVerifierPointEvaluationZeroCommitment(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())
	eval := &PointEvaluation{}
	_, err := verifier.VerifyPointEvaluation(eval)
	if err != ErrKZGNilCommitment {
		t.Errorf("expected ErrKZGNilCommitment, got %v", err)
	}
}

func TestKZGVerifierClosed(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())
	verifier.Close()

	eval := MakeTestPointEvaluation(0)
	_, err := verifier.VerifyPointEvaluation(eval)
	if err != ErrKZGVerifierClosed {
		t.Errorf("expected ErrKZGVerifierClosed, got %v", err)
	}
}

func TestKZGVerifierBlobCommitment(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())

	pair := MakeTestBlobCommitmentPair(0)
	valid, err := verifier.VerifyBlobCommitment(pair)
	if err != nil {
		t.Fatalf("VerifyBlobCommitment failed: %v", err)
	}
	if !valid {
		t.Error("expected valid blob commitment")
	}
}

func TestKZGVerifierBlobCommitmentMismatch(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())

	pair := MakeTestBlobCommitmentPair(0)
	pair.BlobHash = types.HexToHash("0xdeadbeef") // wrong hash

	valid, err := verifier.VerifyBlobCommitment(pair)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if valid {
		t.Error("expected invalid for mismatched blob hash")
	}
}

func TestKZGVerifierBlobCommitmentNil(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())
	_, err := verifier.VerifyBlobCommitment(nil)
	if err != ErrKZGNilCommitment {
		t.Errorf("expected ErrKZGNilCommitment, got %v", err)
	}
}

func TestKZGVerifierBlobCommitmentZero(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())
	pair := &BlobCommitmentPair{}
	_, err := verifier.VerifyBlobCommitment(pair)
	if err != ErrKZGNilCommitment {
		t.Errorf("expected ErrKZGNilCommitment, got %v", err)
	}
}

func TestKZGVerifierBatchVerify(t *testing.T) {
	verifier := NewKZGVerifier(KZGVerifierConfig{
		MaxBatchSize:   128,
		ParallelVerify: true,
	})

	evals := make([]PointEvaluation, 5)
	for i := 0; i < 5; i++ {
		eval := MakeTestPointEvaluation(uint64(i))
		evals[i] = *eval
	}

	result, err := verifier.BatchVerify(evals)
	if err != nil {
		t.Fatalf("BatchVerify failed: %v", err)
	}

	if result.ValidCount != 5 {
		t.Errorf("valid count = %d, want 5", result.ValidCount)
	}
	if !result.AllValid {
		t.Error("expected all valid")
	}
}

func TestKZGVerifierBatchVerifySequential(t *testing.T) {
	verifier := NewKZGVerifier(KZGVerifierConfig{
		MaxBatchSize:   128,
		ParallelVerify: false,
	})

	evals := make([]PointEvaluation, 3)
	for i := 0; i < 3; i++ {
		eval := MakeTestPointEvaluation(uint64(i))
		evals[i] = *eval
	}

	result, err := verifier.BatchVerify(evals)
	if err != nil {
		t.Fatalf("BatchVerify (sequential) failed: %v", err)
	}
	if result.ValidCount != 3 {
		t.Errorf("valid count = %d, want 3", result.ValidCount)
	}
}

func TestKZGVerifierBatchVerifyEmpty(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())
	_, err := verifier.BatchVerify(nil)
	if err != ErrKZGBatchEmpty {
		t.Errorf("expected ErrKZGBatchEmpty, got %v", err)
	}
}

func TestKZGVerifierBatchVerifyClosed(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())
	verifier.Close()

	evals := []PointEvaluation{{}}
	_, err := verifier.BatchVerify(evals)
	if err != ErrKZGVerifierClosed {
		t.Errorf("expected ErrKZGVerifierClosed, got %v", err)
	}
}

func TestKZGVerifierStats(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())

	eval := MakeTestPointEvaluation(0)
	_, _ = verifier.VerifyPointEvaluation(eval)

	verified, failed, batches := verifier.Stats()
	if verified+failed != 1 {
		t.Errorf("expected total 1, got verified=%d failed=%d", verified, failed)
	}
	if batches != 0 {
		t.Errorf("batches = %d, want 0", batches)
	}

	evals := []PointEvaluation{*eval}
	_, _ = verifier.BatchVerify(evals)

	_, _, batches = verifier.Stats()
	if batches != 1 {
		t.Errorf("batches = %d, want 1", batches)
	}
}

func TestComputeVersionedHash(t *testing.T) {
	var commitment KZGCommitment
	commitment[0] = 0xab
	commitment[1] = 0xcd

	hash := computeVersionedHash(commitment)
	// Version byte should be 0x01.
	if hash[0] != 0x01 {
		t.Errorf("version byte = %x, want 0x01", hash[0])
	}
	if hash.IsZero() {
		t.Error("versioned hash should not be zero")
	}

	// Deterministic.
	hash2 := computeVersionedHash(commitment)
	if hash != hash2 {
		t.Error("versioned hash should be deterministic")
	}
}

func TestIsZeroKZG(t *testing.T) {
	if !isZeroKZG(make([]byte, 48)) {
		t.Error("all-zero should return true")
	}
	if isZeroKZG([]byte{0x01}) {
		t.Error("non-zero should return false")
	}
	if !isZeroKZG(nil) {
		t.Error("nil should return true")
	}
}

func TestMakeTestPointEvaluation(t *testing.T) {
	eval := MakeTestPointEvaluation(42)
	if isZeroKZG(eval.Commitment[:]) {
		t.Error("commitment should not be zero")
	}
	if isZeroKZG(eval.Proof[:]) {
		t.Error("proof should not be zero")
	}

	// Should pass verification.
	valid := verifyPointEval(eval.Commitment, eval.Proof, eval.Point, eval.Value)
	if !valid {
		t.Error("test point evaluation should be valid")
	}
}

func TestMakeTestBlobCommitmentPair(t *testing.T) {
	pair := MakeTestBlobCommitmentPair(7)
	if pair.BlobIndex != 7 {
		t.Errorf("blob index = %d, want 7", pair.BlobIndex)
	}
	if pair.BlobHash[0] != 0x01 {
		t.Errorf("blob hash version byte = %x, want 0x01", pair.BlobHash[0])
	}
	if isZeroKZG(pair.Commitment[:]) {
		t.Error("commitment should not be zero")
	}
}

func TestDefaultKZGVerifierConfig(t *testing.T) {
	cfg := DefaultKZGVerifierConfig()
	if cfg.MaxBatchSize != 128 {
		t.Errorf("MaxBatchSize = %d, want 128", cfg.MaxBatchSize)
	}
	if !cfg.ParallelVerify {
		t.Error("ParallelVerify should default to true")
	}
}

func TestKZGConstants(t *testing.T) {
	if KZGCommitmentSize != 48 {
		t.Errorf("KZGCommitmentSize = %d, want 48", KZGCommitmentSize)
	}
	if KZGProofPointSize != 48 {
		t.Errorf("KZGProofPointSize = %d, want 48", KZGProofPointSize)
	}
	if BlobFieldElementCount != 4096 {
		t.Errorf("BlobFieldElementCount = %d, want 4096", BlobFieldElementCount)
	}
	if FieldElementSize != 32 {
		t.Errorf("FieldElementSize = %d, want 32", FieldElementSize)
	}
	if BlobSize != 131072 {
		t.Errorf("BlobSize = %d, want 131072", BlobSize)
	}
}

func TestKZGVerifierMultipleVerifications(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())

	for i := uint64(0); i < 10; i++ {
		eval := MakeTestPointEvaluation(i)
		valid, err := verifier.VerifyPointEvaluation(eval)
		if err != nil {
			t.Fatalf("iteration %d: error: %v", i, err)
		}
		if !valid {
			t.Errorf("iteration %d: expected valid", i)
		}
	}

	verified, _, _ := verifier.Stats()
	if verified != 10 {
		t.Errorf("verified = %d, want 10", verified)
	}
}

func TestKZGBatchResultFields(t *testing.T) {
	verifier := NewKZGVerifier(DefaultKZGVerifierConfig())

	// Create one valid eval.
	eval := MakeTestPointEvaluation(0)
	result, err := verifier.BatchVerify([]PointEvaluation{*eval})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(result.Items))
	}

	item := result.Items[0]
	if item.Commitment != eval.Commitment {
		t.Error("item commitment mismatch")
	}
	if item.Proof != eval.Proof {
		t.Error("item proof mismatch")
	}
}

// The zero polynomial uses the point at infinity for both commitment and proof.
func MakeTestPointEvaluation(index uint64) *PointEvaluation {
	eval := &PointEvaluation{Commitment: KZGCommitment{0xc0}, Proof: KZGProofPoint{0xc0}}
	binary.BigEndian.PutUint64(eval.Point[24:], index)
	return eval
}

func MakeTestBlobCommitmentPair(index uint64) *BlobCommitmentPair {
	commitment := KZGCommitment{0xc0}
	return &BlobCommitmentPair{
		BlobHash:   computeVersionedHash(commitment),
		Commitment: commitment, Proof: KZGProofPoint{0xc0}, BlobIndex: index,
	}
}
