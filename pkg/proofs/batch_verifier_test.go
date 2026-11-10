package proofs

import (
	"errors"
	"testing"

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
