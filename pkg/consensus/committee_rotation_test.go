package consensus

import (
	"crypto/sha256"
	"sync"
	"testing"
)

func crTestSeed() [32]byte {
	return sha256.Sum256([]byte("committee_rotation_test_seed"))
}

func crMakeIndices(n int) []ValidatorIndex {
	indices := make([]ValidatorIndex, n)
	for i := 0; i < n; i++ {
		indices[i] = ValidatorIndex(i)
	}
	return indices
}

// TestCRShuffleListDeterministic verifies the shuffle produces the same
// output for the same input.
func TestCRShuffleListDeterministic(t *testing.T) {
	seed := crTestSeed()
	indices := crMakeIndices(64)

	shuffled1, err := crShuffleList(indices, seed)
	if err != nil {
		t.Fatalf("crShuffleList: %v", err)
	}
	shuffled2, err := crShuffleList(indices, seed)
	if err != nil {
		t.Fatalf("crShuffleList: %v", err)
	}

	if len(shuffled1) != len(shuffled2) {
		t.Fatalf("length mismatch: %d vs %d", len(shuffled1), len(shuffled2))
	}
	for i := range shuffled1 {
		if shuffled1[i] != shuffled2[i] {
			t.Fatalf("index %d differs: %d vs %d", i, shuffled1[i], shuffled2[i])
		}
	}
}

// TestCRShuffleListPermutation verifies the shuffle is a valid permutation.
func TestCRShuffleListPermutation(t *testing.T) {
	seed := crTestSeed()
	n := 100
	indices := crMakeIndices(n)

	shuffled, err := crShuffleList(indices, seed)
	if err != nil {
		t.Fatalf("crShuffleList: %v", err)
	}

	if len(shuffled) != n {
		t.Fatalf("len=%d, want %d", len(shuffled), n)
	}

	seen := make(map[ValidatorIndex]bool)
	for _, idx := range shuffled {
		if idx >= ValidatorIndex(n) {
			t.Fatalf("index %d out of range [0, %d)", idx, n)
		}
		if seen[idx] {
			t.Fatalf("duplicate index %d in shuffle result", idx)
		}
		seen[idx] = true
	}
}

// TestCRShuffleListEdgeCases verifies different seeds, empty, and single-element.
func TestCRShuffleListEdgeCases(t *testing.T) {
	// Different seeds produce different output.
	seed1 := sha256.Sum256([]byte("seed_alpha"))
	seed2 := sha256.Sum256([]byte("seed_beta"))
	s1, _ := crShuffleList(crMakeIndices(50), seed1)
	s2, _ := crShuffleList(crMakeIndices(50), seed2)
	different := false
	for i := range s1 {
		if s1[i] != s2[i] {
			different = true
			break
		}
	}
	if !different {
		t.Error("different seeds produced identical shuffle")
	}

	// Empty returns error.
	if _, err := crShuffleList(nil, crTestSeed()); err != ErrCRNoValidators {
		t.Fatalf("expected ErrCRNoValidators, got %v", err)
	}

	// Single element.
	shuffled, err := crShuffleList(crMakeIndices(1), crTestSeed())
	if err != nil {
		t.Fatalf("single: %v", err)
	}
	if len(shuffled) != 1 || shuffled[0] != 0 {
		t.Fatalf("expected [0], got %v", shuffled)
	}
}

// TestCRComputeCommitteeCount verifies committee count calculation.
func TestCRComputeCommitteeCount(t *testing.T) {
	tests := []struct {
		active uint64
		spe    uint64
		want   uint64
	}{
		{128, 32, 1},      // 128/32/128 = 0 -> 1
		{4096, 32, 1},     // 4096/32/128 = 1
		{262144, 32, 64},  // 262144/32/128 = 64
		{1000000, 32, 64}, // capped at 64
		{512, 4, 1},       // SSF mode: 512/4/128 = 1
		{65536, 4, 64},    // SSF mode: 65536/4/128 = 128 -> capped 64
	}
	for _, tc := range tests {
		got := crComputeCommitteeCount(tc.active, tc.spe, CRTargetCommitteeSize, CRMaxCommitteesPerSlot)
		if got != tc.want {
			t.Errorf("crComputeCommitteeCount(%d, %d) = %d, want %d",
				tc.active, tc.spe, got, tc.want)
		}
	}
}

// TestCRComputeEpochCommitteesBasic verifies basic epoch committee computation.
func TestCRComputeEpochCommitteesBasic(t *testing.T) {
	mgr := NewCommitteeRotationManager(DefaultCommitteeRotationConfig())
	seed := crTestSeed()
	indices := crMakeIndices(256)

	ec, err := mgr.ComputeEpochCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("ComputeEpochCommittees: %v", err)
	}

	if ec.Epoch != 0 {
		t.Errorf("epoch=%d, want 0", ec.Epoch)
	}
	if ec.TotalValidators != 256 {
		t.Errorf("total=%d, want 256", ec.TotalValidators)
	}
	if ec.SlotsPerEpoch != CRDefaultSlotsPerEpoch {
		t.Errorf("slotsPerEpoch=%d, want %d", ec.SlotsPerEpoch, CRDefaultSlotsPerEpoch)
	}

	// All validators should be assigned exactly once.
	total := 0
	for s := uint64(0); s < ec.SlotsPerEpoch; s++ {
		for c := uint64(0); c < ec.CommitteesPerSlot; c++ {
			members, err := ec.GetCommittee(s, c)
			if err != nil {
				t.Fatalf("GetCommittee(%d, %d): %v", s, c, err)
			}
			total += len(members)
		}
	}
	if total != 256 {
		t.Errorf("total assigned=%d, want 256", total)
	}
}

// TestCRComputeEpochCommitteesNoValidators verifies error on empty input.
func TestCRComputeEpochCommitteesNoValidators(t *testing.T) {
	mgr := NewCommitteeRotationManager(DefaultCommitteeRotationConfig())
	seed := crTestSeed()

	_, err := mgr.ComputeEpochCommittees(0, nil, seed)
	if err != ErrCRNoValidators {
		t.Fatalf("expected ErrCRNoValidators, got %v", err)
	}
}

// TestCR128KCap verifies the 128K attester cap is enforced.
func TestCR128KCap(t *testing.T) {
	mgr := NewCommitteeRotationManager(CommitteeRotationConfig{
		SlotsPerEpoch:        4,
		TargetCommitteeSize:  CRTargetCommitteeSize,
		MaxCommitteesPerSlot: CRMaxCommitteesPerSlot,
		MaxAttesters:         1000, // small cap for test speed
	})
	seed := crTestSeed()
	indices := crMakeIndices(2000) // exceeds cap

	ec, err := mgr.ComputeEpochCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("ComputeEpochCommittees: %v", err)
	}

	if ec.TotalValidators != 1000 {
		t.Errorf("total=%d, want 1000 (capped)", ec.TotalValidators)
	}

	// Count all assigned.
	total := 0
	for s := uint64(0); s < ec.SlotsPerEpoch; s++ {
		for c := uint64(0); c < ec.CommitteesPerSlot; c++ {
			members, err := ec.GetCommittee(s, c)
			if err != nil {
				t.Fatalf("GetCommittee: %v", err)
			}
			total += len(members)
		}
	}
	if total != 1000 {
		t.Errorf("assigned=%d, want 1000", total)
	}
}

// TestCRAssignmentConsistency verifies that each validator's assignment
// matches its actual position in the committee.
func TestCRAssignmentConsistency(t *testing.T) {
	mgr := NewCommitteeRotationManager(DefaultCommitteeRotationConfig())
	seed := crTestSeed()
	indices := crMakeIndices(512)

	ec, err := mgr.ComputeEpochCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("ComputeEpochCommittees: %v", err)
	}

	for _, idx := range indices {
		a, found := ec.GetAssignment(idx)
		if !found {
			t.Fatalf("validator %d not found in assignments", idx)
		}
		if a.ValidatorIndex != idx {
			t.Errorf("assignment.ValidatorIndex=%d, want %d", a.ValidatorIndex, idx)
		}

		slotOffset := uint64(a.Slot) % ec.SlotsPerEpoch
		members, err := ec.GetCommittee(slotOffset, a.CommitteeIndex)
		if err != nil {
			t.Fatalf("GetCommittee: %v", err)
		}
		if a.Position >= len(members) {
			t.Errorf("position %d >= committee size %d", a.Position, len(members))
			continue
		}
		if members[a.Position] != idx {
			t.Errorf("committee[%d][%d][%d]=%d, want %d",
				slotOffset, a.CommitteeIndex, a.Position, members[a.Position], idx)
		}
	}
}

// TestCRGetCommitteeBySlot verifies slot-based committee retrieval.
func TestCRGetCommitteeBySlot(t *testing.T) {
	mgr := NewCommitteeRotationManager(DefaultCommitteeRotationConfig())
	seed := crTestSeed()
	indices := crMakeIndices(256)

	_, err := mgr.ComputeEpochCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("ComputeEpochCommittees: %v", err)
	}

	// Slot 0 should be valid.
	members, err := mgr.GetCommitteeBySlot(0, 0)
	if err != nil {
		t.Fatalf("GetCommitteeBySlot: %v", err)
	}
	if len(members) == 0 {
		t.Fatal("expected non-empty committee at slot 0")
	}

	// Slot in a non-computed epoch should fail.
	_, err = mgr.GetCommitteeBySlot(Slot(CRDefaultSlotsPerEpoch), 0)
	if err != ErrCREpochNotComputed {
		t.Fatalf("expected ErrCREpochNotComputed, got %v", err)
	}
}

// TestCRGetValidatorAssignment verifies per-validator assignment lookup.
func TestCRGetValidatorAssignment(t *testing.T) {
	mgr := NewCommitteeRotationManager(DefaultCommitteeRotationConfig())
	seed := crTestSeed()
	indices := crMakeIndices(128)

	_, err := mgr.ComputeEpochCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("ComputeEpochCommittees: %v", err)
	}

	a, err := mgr.GetValidatorAssignment(0, ValidatorIndex(5))
	if err != nil {
		t.Fatalf("GetValidatorAssignment: %v", err)
	}
	if a.ValidatorIndex != 5 {
		t.Errorf("assignment index=%d, want 5", a.ValidatorIndex)
	}

	// Non-computed epoch.
	_, err = mgr.GetValidatorAssignment(99, ValidatorIndex(5))
	if err != ErrCREpochNotComputed {
		t.Fatalf("expected ErrCREpochNotComputed, got %v", err)
	}
}

// TestCRCache verifies caching behavior.
func TestCRCache(t *testing.T) {
	mgr := NewCommitteeRotationManager(DefaultCommitteeRotationConfig())
	seed := crTestSeed()
	indices := crMakeIndices(128)

	ec1, err := mgr.ComputeEpochCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("ComputeEpochCommittees: %v", err)
	}

	ec2, err := mgr.ComputeEpochCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("cached call: %v", err)
	}

	// Should return the exact same pointer (cached).
	if ec1 != ec2 {
		t.Error("expected cached result to return same pointer")
	}

	if mgr.CachedEpochCount() != 1 {
		t.Errorf("cached count=%d, want 1", mgr.CachedEpochCount())
	}
}

// TestCRRotateEpoch verifies epoch rotation.
func TestCRRotateEpoch(t *testing.T) {
	mgr := NewCommitteeRotationManager(DefaultCommitteeRotationConfig())
	seed := crTestSeed()
	indices := crMakeIndices(128)

	ec0, err := mgr.RotateEpoch(indices, seed)
	if err != nil {
		t.Fatalf("RotateEpoch: %v", err)
	}
	if ec0.Epoch != 0 {
		t.Errorf("first rotation epoch=%d, want 0", ec0.Epoch)
	}

	seed2 := sha256.Sum256([]byte("next_epoch_seed"))
	ec1, err := mgr.RotateEpoch(indices, seed2)
	if err != nil {
		t.Fatalf("RotateEpoch: %v", err)
	}
	if ec1.Epoch != 1 {
		t.Errorf("second rotation epoch=%d, want 1", ec1.Epoch)
	}

	if mgr.CachedEpochCount() != 2 {
		t.Errorf("cached=%d, want 2", mgr.CachedEpochCount())
	}
}

// TestCRPruneBeforeEpoch verifies cache pruning.
func TestCRPruneBeforeEpoch(t *testing.T) {
	mgr := NewCommitteeRotationManager(DefaultCommitteeRotationConfig())
	seed := crTestSeed()
	indices := crMakeIndices(128)

	for ep := Epoch(0); ep < 5; ep++ {
		_, err := mgr.ComputeEpochCommittees(ep, indices, seed)
		if err != nil {
			t.Fatalf("ComputeEpochCommittees(%d): %v", ep, err)
		}
	}

	if mgr.CachedEpochCount() != 5 {
		t.Fatalf("cached=%d, want 5", mgr.CachedEpochCount())
	}

	pruned := mgr.PruneBeforeEpoch(3)
	if pruned != 3 {
		t.Errorf("pruned=%d, want 3", pruned)
	}
	if mgr.CachedEpochCount() != 2 {
		t.Errorf("after prune cached=%d, want 2", mgr.CachedEpochCount())
	}
}

// TestCRSSFMode verifies SSF 4-slot epoch mode.
func TestCRSSFMode(t *testing.T) {
	mgr := NewCommitteeRotationManager(SSFCommitteeRotationConfig())
	seed := crTestSeed()
	indices := crMakeIndices(512)

	ec, err := mgr.ComputeEpochCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("ComputeEpochCommittees SSF: %v", err)
	}

	if ec.SlotsPerEpoch != CRSSFSlotsPerEpoch {
		t.Errorf("SSF slotsPerEpoch=%d, want %d", ec.SlotsPerEpoch, CRSSFSlotsPerEpoch)
	}

	if mgr.EffectiveSlotsPerEpoch() != CRSSFSlotsPerEpoch {
		t.Errorf("EffectiveSlotsPerEpoch=%d, want %d",
			mgr.EffectiveSlotsPerEpoch(), CRSSFSlotsPerEpoch)
	}

	// All validators assigned.
	total := 0
	for s := uint64(0); s < ec.SlotsPerEpoch; s++ {
		for c := uint64(0); c < ec.CommitteesPerSlot; c++ {
			members, err := ec.GetCommittee(s, c)
			if err != nil {
				t.Fatalf("GetCommittee: %v", err)
			}
			total += len(members)
		}
	}
	if total != 512 {
		t.Errorf("total assigned=%d, want 512", total)
	}
}

// TestCREpochTransitionRotation verifies assignment changes across epochs.
func TestCREpochTransitionRotation(t *testing.T) {
	mgr := NewCommitteeRotationManager(DefaultCommitteeRotationConfig())
	indices := crMakeIndices(256)
	seed1 := sha256.Sum256([]byte("epoch0_seed"))
	seed2 := sha256.Sum256([]byte("epoch1_seed"))

	ec0, err := mgr.ComputeEpochCommittees(0, indices, seed1)
	if err != nil {
		t.Fatal(err)
	}
	ec1, err := mgr.ComputeEpochCommittees(1, indices, seed2)
	if err != nil {
		t.Fatal(err)
	}
	a0, ok0 := ec0.GetAssignment(ValidatorIndex(42))
	a1, ok1 := ec1.GetAssignment(ValidatorIndex(42))
	if !ok0 || !ok1 {
		t.Fatal("missing validator assignment")
	}
	if uint64(a0.Slot)%ec0.SlotsPerEpoch == uint64(a1.Slot)%ec1.SlotsPerEpoch &&
		a0.CommitteeIndex == a1.CommitteeIndex && a0.Position == a1.Position {
		t.Fatal("fixed epoch seeds produced identical relative assignments")
	}
}

// TestCRLastComputedEpochAndSeed verifies epoch tracker and seed derivation.
func TestCRLastComputedEpochAndSeed(t *testing.T) {
	mgr := NewCommitteeRotationManager(DefaultCommitteeRotationConfig())
	if _, ok := mgr.LastComputedEpoch(); ok {
		t.Error("expected no computed epoch initially")
	}
	mgr.ComputeEpochCommittees(5, crMakeIndices(64), crTestSeed())
	ep, ok := mgr.LastComputedEpoch()
	if !ok || ep != 5 {
		t.Errorf("LastComputedEpoch=%d (ok=%v), want 5 (true)", ep, ok)
	}

	// Seed derivation.
	mix := sha256.Sum256([]byte("randao_mix_test"))
	s1 := ComputeEpochSeed(0, mix)
	s2 := ComputeEpochSeed(0, mix)
	if s1 != s2 {
		t.Fatal("ComputeEpochSeed not deterministic")
	}
	if s1 == ComputeEpochSeed(1, mix) {
		t.Fatal("different epochs should produce different seeds")
	}
}

// TestCRGetCommitteeInvalidSlot verifies error on invalid slot.
func TestCRGetCommitteeInvalidSlot(t *testing.T) {
	ec := &CREpochCommittees{
		SlotsPerEpoch:     4,
		CommitteesPerSlot: 1,
		committees:        make([][][]ValidatorIndex, 4),
	}
	for i := range ec.committees {
		ec.committees[i] = [][]ValidatorIndex{{}}
	}

	_, err := ec.GetCommittee(10, 0) // slot offset out of range
	if err != ErrCRInvalidSlot {
		t.Fatalf("expected ErrCRInvalidSlot, got %v", err)
	}

	_, err = ec.GetCommittee(0, 5) // committee index out of range
	if err != ErrCRInvalidCommittee {
		t.Fatalf("expected ErrCRInvalidCommittee, got %v", err)
	}
}

// TestCRConcurrentAccess verifies thread safety of the rotation manager.
func TestCRConcurrentAccess(t *testing.T) {
	mgr := NewCommitteeRotationManager(DefaultCommitteeRotationConfig())
	seed := crTestSeed()
	indices := crMakeIndices(256)

	// Pre-compute epoch 0.
	_, err := mgr.ComputeEpochCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("ComputeEpochCommittees: %v", err)
	}

	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrent reads.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := mgr.GetCommitteeBySlot(Slot(idx%32), 0)
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	// Concurrent compute for a different epoch.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(ep int) {
			defer wg.Done()
			seed := sha256.Sum256([]byte{byte(ep)})
			_, err := mgr.ComputeEpochCommittees(Epoch(ep+1), indices, seed)
			if err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}
}
