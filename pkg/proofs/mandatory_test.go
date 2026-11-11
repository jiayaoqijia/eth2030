package proofs

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func proverHash(id byte) types.Hash {
	var h types.Hash
	h[0] = id
	return h
}

func blockHash(id byte) types.Hash {
	var h types.Hash
	h[31] = id
	return h
}

func TestDefaultMandatoryProofConfig(t *testing.T) {
	cfg := DefaultMandatoryProofConfig()
	if cfg.RequiredProofs != 3 {
		t.Errorf("RequiredProofs: got %d, want 3", cfg.RequiredProofs)
	}
	if cfg.TotalProvers != 5 {
		t.Errorf("TotalProvers: got %d, want 5", cfg.TotalProvers)
	}
	if cfg.ProofDeadlineSlots != 32 {
		t.Errorf("ProofDeadlineSlots: got %d, want 32", cfg.ProofDeadlineSlots)
	}
	if cfg.PenaltyAmount != 1000 {
		t.Errorf("PenaltyAmount: got %d, want 1000", cfg.PenaltyAmount)
	}
}

func TestNewMandatoryProofSystem_Defaults(t *testing.T) {
	sys := NewMandatoryProofSystem(MandatoryProofConfig{})
	if sys.config.RequiredProofs != 3 {
		t.Errorf("default RequiredProofs: got %d, want 3", sys.config.RequiredProofs)
	}
	if sys.config.TotalProvers != 5 {
		t.Errorf("default TotalProvers: got %d, want 5", sys.config.TotalProvers)
	}
}

func TestNewMandatoryProofSystem_RequiredCannotExceedTotal(t *testing.T) {
	sys := NewMandatoryProofSystem(MandatoryProofConfig{
		RequiredProofs: 10,
		TotalProvers:   3,
	})
	if sys.config.RequiredProofs != 3 {
		t.Errorf("RequiredProofs should be clamped to TotalProvers: got %d", sys.config.RequiredProofs)
	}
}

func TestRegisterProver_Valid(t *testing.T) {
	sys := NewMandatoryProofSystem(DefaultMandatoryProofConfig())
	err := sys.RegisterProver(proverHash(1), []string{"ZK-SNARK", "ZK-STARK"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRegisterProver_ZeroID(t *testing.T) {
	sys := NewMandatoryProofSystem(DefaultMandatoryProofConfig())
	err := sys.RegisterProver(types.Hash{}, []string{"ZK-SNARK"})
	if err != ErrMandatoryZeroProverID {
		t.Errorf("expected ErrMandatoryZeroProverID, got %v", err)
	}
}

func TestRegisterProver_NoTypes(t *testing.T) {
	sys := NewMandatoryProofSystem(DefaultMandatoryProofConfig())
	err := sys.RegisterProver(proverHash(1), nil)
	if err != ErrMandatoryNoProofTypes {
		t.Errorf("expected ErrMandatoryNoProofTypes, got %v", err)
	}
}

func TestRegisterProver_Duplicate(t *testing.T) {
	sys := NewMandatoryProofSystem(DefaultMandatoryProofConfig())
	sys.RegisterProver(proverHash(1), []string{"ZK-SNARK"})
	err := sys.RegisterProver(proverHash(1), []string{"ZK-STARK"})
	if err != ErrMandatoryProverExists {
		t.Errorf("expected ErrMandatoryProverExists, got %v", err)
	}
}

func setupSystemWithProvers(t *testing.T, n int) *MandatoryProofSystem {
	t.Helper()
	sys := NewMandatoryProofSystem(DefaultMandatoryProofConfig())
	sys.RegisterVerifier("ZK-SNARK", func(*ProofSubmission) bool { return true })
	for i := 1; i <= n; i++ {
		err := sys.RegisterProver(proverHash(byte(i)), []string{"ZK-SNARK"})
		if err != nil {
			t.Fatalf("failed to register prover %d: %v", i, err)
		}
	}
	return sys
}

func TestAssignProvers_Valid(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)

	assigned, err := sys.AssignProvers(bh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(assigned) != 5 {
		t.Fatalf("expected 5 assigned provers, got %d", len(assigned))
	}

	// All assigned provers should be unique.
	seen := make(map[types.Hash]bool)
	for _, id := range assigned {
		if seen[id] {
			t.Errorf("duplicate prover assignment: %s", id.Hex())
		}
		seen[id] = true
	}
}

func TestAssignProvers_Deterministic(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)

	assigned1, _ := sys.AssignProvers(bh)
	assigned2, _ := sys.AssignProvers(bh)

	for i := range assigned1 {
		if assigned1[i] != assigned2[i] {
			t.Errorf("assignment not deterministic at index %d", i)
		}
	}
}

func TestAssignProvers_ZeroBlockHash(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	_, err := sys.AssignProvers(types.Hash{})
	if err != ErrMandatoryZeroBlockHash {
		t.Errorf("expected ErrMandatoryZeroBlockHash, got %v", err)
	}
}

func TestAssignProvers_NoProvers(t *testing.T) {
	sys := NewMandatoryProofSystem(DefaultMandatoryProofConfig())
	_, err := sys.AssignProvers(blockHash(1))
	if err != ErrMandatoryNoProvers {
		t.Errorf("expected ErrMandatoryNoProvers, got %v", err)
	}
}

func TestAssignProvers_NotEnoughProvers(t *testing.T) {
	sys := setupSystemWithProvers(t, 3) // need 5
	_, err := sys.AssignProvers(blockHash(1))
	if err == nil {
		t.Fatal("expected error for not enough provers")
	}
}

func TestAssignProvers_DifferentBlocks(t *testing.T) {
	sys := setupSystemWithProvers(t, 10)

	a1, _ := sys.AssignProvers(blockHash(1))
	a2, _ := sys.AssignProvers(blockHash(2))

	// With 10 provers and different block hashes, assignment order must differ.
	same := true
	for i := range a1 {
		if a1[i] != a2[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("assignments for different block hashes should differ (deterministic shuffle by block hash)")
	}
}

func TestSubmitProof_Valid(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)
	assigned, _ := sys.AssignProvers(bh)

	err := sys.SubmitProof(&ProofSubmission{
		ProverID:  assigned[0],
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01, 0x02, 0x03},
		BlockHash: bh,
		Timestamp: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSubmitProof_NilSubmission(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	if err := sys.SubmitProof(nil); err != ErrMandatoryNilSubmission {
		t.Errorf("expected ErrMandatoryNilSubmission, got %v", err)
	}
}

func TestSubmitProof_ZeroBlockHash(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	err := sys.SubmitProof(&ProofSubmission{
		ProverID:  proverHash(1),
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
	})
	if err != ErrMandatoryZeroBlockHash {
		t.Errorf("expected ErrMandatoryZeroBlockHash, got %v", err)
	}
}

func TestSubmitProof_ZeroProverID(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	err := sys.SubmitProof(&ProofSubmission{
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
		BlockHash: blockHash(1),
	})
	if err != ErrMandatoryZeroProverID {
		t.Errorf("expected ErrMandatoryZeroProverID, got %v", err)
	}
}

func TestSubmitProof_EmptyData(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	err := sys.SubmitProof(&ProofSubmission{
		ProverID:  proverHash(1),
		ProofType: "ZK-SNARK",
		BlockHash: blockHash(1),
	})
	if err != ErrMandatoryEmptyProofData {
		t.Errorf("expected ErrMandatoryEmptyProofData, got %v", err)
	}
}

func TestSubmitProof_EmptyType(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	err := sys.SubmitProof(&ProofSubmission{
		ProverID:  proverHash(1),
		ProofData: []byte{0x01},
		BlockHash: blockHash(1),
	})
	if err != ErrMandatoryEmptyProofType {
		t.Errorf("expected ErrMandatoryEmptyProofType, got %v", err)
	}
}

func TestSubmitProof_BlockNotAssigned(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	err := sys.SubmitProof(&ProofSubmission{
		ProverID:  proverHash(1),
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
		BlockHash: blockHash(99), // not assigned
	})
	if err != ErrMandatoryBlockNotAssigned {
		t.Errorf("expected ErrMandatoryBlockNotAssigned, got %v", err)
	}
}

func TestSubmitProof_UnassignedProver(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)
	sys.AssignProvers(bh)

	// Use a prover that was not assigned to this block.
	err := sys.SubmitProof(&ProofSubmission{
		ProverID:  types.HexToHash("0xdead"),
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
		BlockHash: bh,
	})
	if err == nil {
		t.Fatal("expected error for unassigned prover")
	}
}

func TestVerifyProof_Valid(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)
	assigned, _ := sys.AssignProvers(bh)

	sub := &ProofSubmission{
		ProverID:  assigned[0],
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01, 0x02, 0x03},
		BlockHash: bh,
		Timestamp: 100,
	}
	sys.SubmitProof(sub)

	sys.RegisterVerifier("ZK-SNARK", nil)
	if sys.VerifyProof(sub) {
		t.Fatal("proof accepted without a verifier")
	}
	sys.RegisterVerifier("ZK-SNARK", func(candidate *ProofSubmission) bool {
		return candidate.BlockHash == bh && candidate.ProofData[0] == 1
	})
	changed := *sub
	changed.ProofData = []byte{1, 2, 4}
	if sys.VerifyProof(&changed) {
		t.Fatal("proof accepted without a matching submission")
	}
	if !sys.VerifyProof(sub) {
		t.Error("expected proof to verify successfully")
	}
}

func TestVerifyProof_NilSubmission(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	if sys.VerifyProof(nil) {
		t.Error("nil submission should not verify")
	}
}

func TestVerifyProof_EmptyData(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	if sys.VerifyProof(&ProofSubmission{ProofData: nil}) {
		t.Error("empty proof data should not verify")
	}
}

func TestVerifyProof_BlockNotTracked(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	sub := &ProofSubmission{
		ProverID:  proverHash(1),
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
		BlockHash: blockHash(99),
	}
	if sys.VerifyProof(sub) {
		t.Error("proof for untracked block should not verify")
	}
}

func TestCheckRequirement_NoBlock(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	status := sys.CheckRequirement(blockHash(1))
	if status.Required != 3 {
		t.Errorf("Required: got %d, want 3", status.Required)
	}
	if status.Submitted != 0 {
		t.Errorf("Submitted: got %d, want 0", status.Submitted)
	}
	if status.IsSatisfied {
		t.Error("should not be satisfied with no submissions")
	}
}

func TestCheckRequirement_Satisfied(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)
	assigned, _ := sys.AssignProvers(bh)

	// Submit and verify 3 proofs (the minimum required).
	for i := 0; i < 3; i++ {
		sub := &ProofSubmission{
			ProverID:  assigned[i],
			ProofType: "ZK-SNARK",
			ProofData: []byte{byte(i + 1)},
			BlockHash: bh,
			Timestamp: uint64(100 + i),
		}
		if err := sys.SubmitProof(sub); err != nil {
			t.Fatalf("submit %d: %v", i, err)
		}
		if !sys.VerifyProof(sub) {
			t.Fatalf("verify %d failed", i)
		}
	}

	status := sys.CheckRequirement(bh)
	if status.Submitted != 3 {
		t.Errorf("Submitted: got %d, want 3", status.Submitted)
	}
	if status.Verified != 3 {
		t.Errorf("Verified: got %d, want 3", status.Verified)
	}
	if !status.IsSatisfied {
		t.Error("should be satisfied with 3 verified proofs")
	}
	if len(status.ProverIDs) != 5 {
		t.Errorf("ProverIDs length: got %d, want 5", len(status.ProverIDs))
	}
}

func TestCheckRequirement_NotSatisfied(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)
	assigned, _ := sys.AssignProvers(bh)

	// Submit and verify only 2 proofs (below threshold).
	for i := 0; i < 2; i++ {
		sub := &ProofSubmission{
			ProverID:  assigned[i],
			ProofType: "ZK-SNARK",
			ProofData: []byte{byte(i + 1)},
			BlockHash: bh,
			Timestamp: uint64(100 + i),
		}
		sys.SubmitProof(sub)
		sys.VerifyProof(sub)
	}

	status := sys.CheckRequirement(bh)
	if status.IsSatisfied {
		t.Error("should not be satisfied with only 2 verified proofs")
	}
}

func TestGetProofDeadline_NoBlock(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	dl := sys.GetProofDeadline(blockHash(99))
	if dl != 32 {
		t.Errorf("expected default deadline 32, got %d", dl)
	}
}

func TestGetProofDeadline_WithSubmissions(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)
	assigned, _ := sys.AssignProvers(bh)

	sys.SubmitProof(&ProofSubmission{
		ProverID:  assigned[0],
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
		BlockHash: bh,
		Timestamp: 200,
	})
	sys.SubmitProof(&ProofSubmission{
		ProverID:  assigned[1],
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x02},
		BlockHash: bh,
		Timestamp: 150, // earlier
	})

	dl := sys.GetProofDeadline(bh)
	// Earliest timestamp (150) + deadline slots (32) = 182.
	if dl != 182 {
		t.Errorf("expected deadline 182, got %d", dl)
	}
}

func TestGetProofDeadline_NoSubmissions(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)
	sys.AssignProvers(bh)

	dl := sys.GetProofDeadline(bh)
	if dl != 32 {
		t.Errorf("expected default deadline 32, got %d", dl)
	}
}

func TestPenalizeLatePoof_NoSubmission(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)
	assigned, _ := sys.AssignProvers(bh)

	// Prover assigned but never submitted: full penalty.
	penalty := sys.PenalizeLatePoof(assigned[0], bh)
	if penalty != 1000 {
		t.Errorf("expected full penalty 1000, got %d", penalty)
	}
}

func TestPenalizeLatePoof_SubmittedNotVerified(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)
	assigned, _ := sys.AssignProvers(bh)

	sys.SubmitProof(&ProofSubmission{
		ProverID:  assigned[0],
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
		BlockHash: bh,
		Timestamp: 100,
	})
	// Submitted but NOT verified: half penalty.
	penalty := sys.PenalizeLatePoof(assigned[0], bh)
	if penalty != 500 {
		t.Errorf("expected half penalty 500, got %d", penalty)
	}
}

func TestPenalizeLatePoof_Verified(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)
	assigned, _ := sys.AssignProvers(bh)

	sub := &ProofSubmission{
		ProverID:  assigned[0],
		ProofType: "ZK-SNARK",
		ProofData: []byte{0x01},
		BlockHash: bh,
		Timestamp: 100,
	}
	sys.SubmitProof(sub)
	sys.VerifyProof(sub)

	// Verified: no penalty.
	penalty := sys.PenalizeLatePoof(assigned[0], bh)
	if penalty != 0 {
		t.Errorf("expected no penalty, got %d", penalty)
	}
}

func TestPenalizeLatePoof_UnassignedProver(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)
	sys.AssignProvers(bh)

	penalty := sys.PenalizeLatePoof(types.HexToHash("0xdead"), bh)
	if penalty != 0 {
		t.Errorf("expected no penalty for unassigned prover, got %d", penalty)
	}
}

func TestPenalizeLatePoof_NoBlock(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	penalty := sys.PenalizeLatePoof(proverHash(1), blockHash(99))
	if penalty != 0 {
		t.Errorf("expected no penalty for untracked block, got %d", penalty)
	}
}

func TestFullWorkflow_3of5(t *testing.T) {
	sys := setupSystemWithProvers(t, 5)
	bh := blockHash(1)

	// Step 1: Assign provers.
	assigned, err := sys.AssignProvers(bh)
	if err != nil {
		t.Fatalf("AssignProvers: %v", err)
	}
	if len(assigned) != 5 {
		t.Fatalf("expected 5 assigned, got %d", len(assigned))
	}

	// Step 2: 3 provers submit and get verified.
	for i := 0; i < 3; i++ {
		sub := &ProofSubmission{
			ProverID:  assigned[i],
			ProofType: "ZK-SNARK",
			ProofData: []byte{byte(i + 1), 0xaa, 0xbb},
			BlockHash: bh,
			Timestamp: uint64(100 + i),
		}
		if err := sys.SubmitProof(sub); err != nil {
			t.Fatalf("SubmitProof %d: %v", i, err)
		}
		if !sys.VerifyProof(sub) {
			t.Fatalf("VerifyProof %d: failed", i)
		}
	}

	// Step 3: Check requirement is satisfied.
	status := sys.CheckRequirement(bh)
	if !status.IsSatisfied {
		t.Fatalf("expected requirement satisfied, got Verified=%d", status.Verified)
	}

	// Step 4: Provers 3 and 4 (index 3,4) did not submit.
	for i := 3; i < 5; i++ {
		penalty := sys.PenalizeLatePoof(assigned[i], bh)
		if penalty != 1000 {
			t.Errorf("prover %d: expected penalty 1000, got %d", i, penalty)
		}
	}

	// Step 5: Provers 0-2 submitted and verified, no penalty.
	for i := 0; i < 3; i++ {
		penalty := sys.PenalizeLatePoof(assigned[i], bh)
		if penalty != 0 {
			t.Errorf("prover %d: expected no penalty, got %d", i, penalty)
		}
	}
}

func TestConcurrentSubmitAndVerify(t *testing.T) {
	bh := blockHash(1)

	// Use a custom config with TotalProvers=10.
	sys2 := NewMandatoryProofSystem(MandatoryProofConfig{
		RequiredProofs:     3,
		TotalProvers:       10,
		ProofDeadlineSlots: 32,
		PenaltyAmount:      1000,
	})
	sys2.RegisterVerifier("ZK-SNARK", func(*ProofSubmission) bool { return true })
	for i := 1; i <= 10; i++ {
		sys2.RegisterProver(proverHash(byte(i)), []string{"ZK-SNARK"})
	}
	assigned, err := sys2.AssignProvers(bh)
	if err != nil {
		t.Fatalf("AssignProvers: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < len(assigned); i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sub := &ProofSubmission{
				ProverID:  assigned[idx],
				ProofType: "ZK-SNARK",
				ProofData: []byte{byte(idx + 1)},
				BlockHash: bh,
				Timestamp: uint64(100 + idx),
			}
			sys2.SubmitProof(sub)
			sys2.VerifyProof(sub)
		}(i)
	}
	wg.Wait()

	status := sys2.CheckRequirement(bh)
	if !status.IsSatisfied {
		t.Errorf("expected satisfied after concurrent submissions, Verified=%d", status.Verified)
	}
}

func TestHashLess(t *testing.T) {
	a := types.HexToHash("0x01")
	b := types.HexToHash("0x02")
	if !hashLess(a, b) {
		t.Error("0x01 should be less than 0x02")
	}
	if hashLess(b, a) {
		t.Error("0x02 should not be less than 0x01")
	}
	if hashLess(a, a) {
		t.Error("equal hashes should not be less")
	}
}
