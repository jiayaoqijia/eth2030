package proofs

import (
	"errors"
	"testing"
	"time"

	"github.com/eth2030/eth2030/crypto/bls"
	"github.com/eth2030/eth2030/crypto/ipa"
)

func TestBatchVerifierUsesOpeningProofs(t *testing.T) {
	var values [ipa.VectorSize]ipa.Scalar
	values[8].SetUint64(17)
	var point ipa.Scalar
	point.SetUint64(8)
	commitment, proof, err := ipa.Prove(&values, point)
	if err != nil {
		t.Fatal(err)
	}
	z, y := point.BytesLE(), values[8].BytesLE()
	input := append(append(commitment[:], z[:]...), y[:]...)
	kzg := make([]byte, 48)
	kzg[0] = 0xc0
	kzgInput := make([]byte, 112)
	copy(kzgInput, kzg)
	if _, err := bls.NewGoEthKZGBackend(); err != nil {
		t.Fatal(err)
	}
	batch := NewBatchVerifier(DefaultBatchVerifierConfig())
	defer batch.Close()
	results, err := batch.VerifyBatch([]VerifiableProof{
		{ID: "ipa", Type: IPA, Data: proof, PublicIn: input},
		{ID: "kzg", Type: KZG, Data: kzg, PublicIn: kzgInput},
		{ID: "snark", Type: ZKSNARK, Data: []byte("unverified")},
		{ID: "stark", Type: ZKSTARK, Data: []byte("unverified")},
	})
	if err != nil || results.TotalValid != 2 {
		t.Fatalf("verification results = %+v, %v", results, err)
	}
	for _, failure := range results.FailedProofs() {
		if !errors.Is(failure.Err, ErrBatchVerifyNoProvider) {
			t.Errorf("%s: %v", failure.ProofID, failure.Err)
		}
	}
	input[64] ^= 1
	kzgInput[111] ^= 1
	results, err = batch.VerifyBatch([]VerifiableProof{
		{ID: "ipa", Type: IPA, Data: proof, PublicIn: input},
		{ID: "kzg", Type: KZG, Data: kzg, PublicIn: kzgInput},
	})
	if err != nil || results.TotalValid != 0 || len(results.FailedProofs()) != 2 {
		t.Fatalf("tampered inputs = %+v, %v", results, err)
	}
}

func TestBatchVerifierDeadlineAndEmptyID(t *testing.T) {
	bv := NewBatchVerifier(BatchVerifierConfig{Workers: 1, Timeout: 20 * time.Millisecond, PerProofTimeout: time.Second})
	release := make(chan struct{})
	defer close(release)
	bv.RegisterVerifier(ZKSNARK, func([]byte, []byte) (bool, error) {
		<-release
		return true, nil
	})
	result, err := bv.VerifyBatch([]VerifiableProof{{Type: ZKSNARK}, {ID: "queued", Type: ZKSNARK}})
	if err != nil || result.TotalErrors != 2 || result.TotalValid != 0 || result.AllValid {
		t.Fatalf("deadline results = %+v, %v", result, err)
	}
	for _, proof := range result.Results {
		if !errors.Is(proof.Err, ErrBatchVerifyTimeout) {
			t.Fatalf("result = %+v", proof)
		}
	}
}

func TestBatchVerifierCountsEmptyIDOnce(t *testing.T) {
	bv := NewBatchVerifier(DefaultBatchVerifierConfig())
	bv.RegisterVerifier(ZKSNARK, func([]byte, []byte) (bool, error) { return true, nil })
	result, err := bv.VerifyBatch([]VerifiableProof{{Type: ZKSNARK}})
	if err != nil || result.TotalValid != 1 || result.TotalErrors != 0 || !result.AllValid {
		t.Fatalf("empty ID result = %+v, %v", result, err)
	}
}
