package vrf

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestVRFProposer_GenerateKeyPair(t *testing.T) {
	kp := GenerateVRFKeyPair([]byte("test-seed"))
	if kp == nil {
		t.Fatal("expected non-nil key pair")
	}

	// Keys should not be all zeros.
	var zeroKey [VRFKeySize]byte
	if kp.SecretKey == zeroKey {
		t.Error("expected non-zero secret key")
	}
	if kp.PublicKey == zeroKey {
		t.Error("expected non-zero public key")
	}

	// Different seeds produce different keys.
	kp2 := GenerateVRFKeyPair([]byte("other-seed"))
	if kp.SecretKey == kp2.SecretKey {
		t.Error("expected different secret keys for different seeds")
	}
}

func TestVRFProposer_ProveVerify(t *testing.T) {
	kp := GenerateVRFKeyPair([]byte("prove-verify-seed"))
	input := ComputeVRFElectionInput(10, 100)

	output, proof := VRFProve(kp.SecretKey, input)

	// Output should not be zero.
	var zeroOutput VRFOutput
	if output == zeroOutput {
		t.Error("expected non-zero VRF output")
	}

	// Verification should succeed.
	if !VRFVerify(kp.PublicKey, input, output, proof) {
		t.Error("expected VRF verification to succeed")
	}
}

func TestVRFProposer_ProveIsDeterministic(t *testing.T) {
	kp := GenerateVRFKeyPair([]byte("deterministic-seed"))
	input := ComputeVRFElectionInput(5, 50)

	output1, proof1 := VRFProve(kp.SecretKey, input)
	output2, proof2 := VRFProve(kp.SecretKey, input)

	if output1 != output2 {
		t.Error("expected deterministic output")
	}
	if proof1 != proof2 {
		t.Error("expected deterministic proof")
	}
}

func TestVRFProposer_VerifyWrongInput(t *testing.T) {
	kp := GenerateVRFKeyPair([]byte("wrong-input-seed"))
	input := ComputeVRFElectionInput(10, 100)
	output, proof := VRFProve(kp.SecretKey, input)

	// Verification with different input should fail.
	wrongInput := ComputeVRFElectionInput(10, 101)
	if VRFVerify(kp.PublicKey, wrongInput, output, proof) {
		t.Error("expected verification to fail with wrong input")
	}
}

func TestVRFProposer_VerifyWrongPublicKey(t *testing.T) {
	key := GenerateVRFKeyPair([]byte("owner"))
	other := GenerateVRFKeyPair([]byte("another validator"))
	input := ComputeVRFElectionInput(10, 100)
	output, proof := VRFProve(key.SecretKey, input)
	if VRFVerify(other.PublicKey, input, output, proof) {
		t.Fatal("proof verified under a different validator key")
	}
}

func TestVRFProposer_VerifyWrongOutput(t *testing.T) {
	kp := GenerateVRFKeyPair([]byte("wrong-output-seed"))
	input := ComputeVRFElectionInput(10, 100)
	_, proof := VRFProve(kp.SecretKey, input)

	// Verification with wrong output should fail.
	var wrongOutput VRFOutput
	wrongOutput[0] = 0xFF
	if VRFVerify(kp.PublicKey, input, wrongOutput, proof) {
		t.Error("expected verification to fail with wrong output")
	}
}

func TestVRFProposer_VerifyWrongProof(t *testing.T) {
	kp := GenerateVRFKeyPair([]byte("wrong-proof-seed"))
	input := ComputeVRFElectionInput(10, 100)
	output, wrongProof := VRFProve(kp.SecretKey, input)

	// Verification with tampered proof should fail.
	wrongProof[0] ^= 1
	if VRFVerify(kp.PublicKey, input, output, wrongProof) {
		t.Error("expected verification to fail with wrong proof")
	}
}

func TestVRFProposer_DifferentKeysGiveDifferentOutputs(t *testing.T) {
	kp1 := GenerateVRFKeyPair([]byte("key1"))
	kp2 := GenerateVRFKeyPair([]byte("key2"))
	input := ComputeVRFElectionInput(10, 100)

	output1, _ := VRFProve(kp1.SecretKey, input)
	output2, _ := VRFProve(kp2.SecretKey, input)

	if output1 == output2 {
		t.Error("expected different outputs for different keys")
	}
}

func TestVRFProposer_ComputeProposerScore(t *testing.T) {
	kp := GenerateVRFKeyPair([]byte("score-seed"))
	input := ComputeVRFElectionInput(1, 1)
	output, _ := VRFProve(kp.SecretKey, input)

	score := ComputeProposerScore(output)
	if score == nil {
		t.Fatal("expected non-nil score")
	}
	if score.Sign() < 0 {
		t.Error("expected non-negative score")
	}
}

func TestVRFProposer_ElectProposer(t *testing.T) {
	se := NewSecretElection()

	entries := make([]*VRFElectionEntry, 5)
	for i := 0; i < 5; i++ {
		kp := GenerateVRFKeyPair([]byte{byte(i)})
		input := ComputeVRFElectionInput(1, 10)
		output, proof := VRFProve(kp.SecretKey, input)

		entries[i] = &VRFElectionEntry{
			ValidatorIndex: uint64(i),
			Epoch:          1,
			Slot:           10,
			Output:         output,
			Proof:          proof,
			Score:          ComputeProposerScore(output),
		}
	}

	winner, err := se.ElectProposer(entries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if winner == nil {
		t.Fatal("expected non-nil winner")
	}

	// Winner should have the lowest score.
	winnerScore := ComputeProposerScore(winner.Output)
	for _, entry := range entries {
		entryScore := ComputeProposerScore(entry.Output)
		if entryScore.Cmp(winnerScore) < 0 {
			t.Errorf("validator %d has lower score than winner %d",
				entry.ValidatorIndex, winner.ValidatorIndex)
		}
	}
}

func TestVRFProposer_ElectProposerEmpty(t *testing.T) {
	se := NewSecretElection()
	_, err := se.ElectProposer(nil)
	if err != ErrVRFNoValidators {
		t.Errorf("expected ErrVRFNoValidators, got %v", err)
	}
}

func TestVRFProposer_SubmitReveal(t *testing.T) {
	se := NewSecretElection()

	reveal := &VRFReveal{
		ValidatorIndex: 42,
		Slot:           100,
		BlockHash:      types.Hash{0x01},
	}

	err := se.SubmitReveal(reveal)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reveals := se.GetReveals(100)
	if len(reveals) != 1 {
		t.Errorf("expected 1 reveal, got %d", len(reveals))
	}
}

func TestVRFProposer_SubmitRevealNil(t *testing.T) {
	se := NewSecretElection()
	err := se.SubmitReveal(nil)
	if err != ErrVRFNoReveal {
		t.Errorf("expected ErrVRFNoReveal, got %v", err)
	}
}

func TestVRFProposer_DoubleReveal(t *testing.T) {
	se := NewSecretElection()

	reveal1 := &VRFReveal{
		ValidatorIndex: 42,
		Slot:           100,
		BlockHash:      types.Hash{0x01},
	}
	reveal2 := &VRFReveal{
		ValidatorIndex: 42,
		Slot:           100,
		BlockHash:      types.Hash{0x02}, // different block hash
	}

	err := se.SubmitReveal(reveal1)
	if err != nil {
		t.Fatalf("unexpected error on first reveal: %v", err)
	}

	err = se.SubmitReveal(reveal2)
	if err != ErrVRFDoubleReveal {
		t.Errorf("expected ErrVRFDoubleReveal, got %v", err)
	}

	// Slashing evidence should be recorded.
	evidence := se.GetSlashingEvidence()
	if len(evidence) != 1 {
		t.Fatalf("expected 1 slashing evidence, got %d", len(evidence))
	}
	if evidence[0].ValidatorIndex != 42 {
		t.Errorf("expected validator 42, got %d", evidence[0].ValidatorIndex)
	}
	if evidence[0].Reveal1.BlockHash != (types.Hash{0x01}) {
		t.Error("expected reveal1 block hash 0x01")
	}
	if evidence[0].Reveal2.BlockHash != (types.Hash{0x02}) {
		t.Error("expected reveal2 block hash 0x02")
	}
}

func TestVRFProposer_DuplicateRevealSameBlock(t *testing.T) {
	se := NewSecretElection()

	reveal := &VRFReveal{
		ValidatorIndex: 42,
		Slot:           100,
		BlockHash:      types.Hash{0x01},
	}

	_ = se.SubmitReveal(reveal)
	err := se.SubmitReveal(reveal)
	if err != ErrVRFAlreadyRevealed {
		t.Errorf("expected ErrVRFAlreadyRevealed, got %v", err)
	}
}

func TestVRFProposer_PurgeSlot(t *testing.T) {
	se := NewSecretElection()

	reveal := &VRFReveal{
		ValidatorIndex: 1,
		Slot:           50,
		BlockHash:      types.Hash{0xAA},
	}
	_ = se.SubmitReveal(reveal)

	se.PurgeSlot(50)

	reveals := se.GetReveals(50)
	if len(reveals) != 0 {
		t.Errorf("expected 0 reveals after purge, got %d", len(reveals))
	}
}

func TestVRFProposer_VerifyReveal(t *testing.T) {
	kp := GenerateVRFKeyPair([]byte("reveal-verify-seed"))
	epoch := uint64(5)
	slot := uint64(50)
	input := ComputeVRFElectionInput(epoch, slot)
	output, proof := VRFProve(kp.SecretKey, input)

	reveal := &VRFReveal{
		ValidatorIndex: 1,
		Slot:           slot,
		BlockHash:      types.Hash{0x01},
		Output:         output,
		Proof:          proof,
	}

	if !VerifyReveal(kp.PublicKey, reveal, epoch) {
		t.Error("expected reveal verification to succeed")
	}

	// Wrong epoch should fail.
	if VerifyReveal(kp.PublicKey, reveal, epoch+1) {
		t.Error("expected reveal verification to fail with wrong epoch")
	}
}

func TestVRFProposer_VerifyRevealNil(t *testing.T) {
	var pk [VRFKeySize]byte
	if VerifyReveal(pk, nil, 0) {
		t.Error("expected false for nil reveal")
	}
}

func TestVRFProposer_BlockBindingHash(t *testing.T) {
	var output1 VRFOutput
	output1[0] = 0xAA
	hash1 := types.Hash{0x01}
	hash2 := types.Hash{0x02}

	binding1 := BlockBindingHash(output1, hash1)
	binding2 := BlockBindingHash(output1, hash2)

	if binding1 == binding2 {
		t.Error("expected different bindings for different block hashes")
	}

	// Same inputs should produce same binding.
	binding3 := BlockBindingHash(output1, hash1)
	if binding1 != binding3 {
		t.Error("expected same binding for same inputs")
	}
}

func TestVRFProposer_MultipleValidatorsMultipleSlots(t *testing.T) {
	se := NewSecretElection()

	// Submit reveals from multiple validators across multiple slots.
	for slot := uint64(1); slot <= 3; slot++ {
		for val := uint64(0); val < 5; val++ {
			reveal := &VRFReveal{
				ValidatorIndex: val,
				Slot:           slot,
				BlockHash:      types.Hash{byte(slot)},
			}
			err := se.SubmitReveal(reveal)
			if err != nil {
				t.Fatalf("unexpected error for slot=%d val=%d: %v", slot, val, err)
			}
		}
	}

	for slot := uint64(1); slot <= 3; slot++ {
		reveals := se.GetReveals(slot)
		if len(reveals) != 5 {
			t.Errorf("slot %d: expected 5 reveals, got %d", slot, len(reveals))
		}
	}
}

func TestVRFProposer_ComputeElectionInput(t *testing.T) {
	input := ComputeVRFElectionInput(10, 100)
	if len(input) != 16 {
		t.Errorf("expected 16 byte input, got %d", len(input))
	}

	// Different epoch/slot should give different input.
	input2 := ComputeVRFElectionInput(10, 101)
	same := true
	for i := range input {
		if input[i] != input2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("expected different inputs for different slots")
	}
}
