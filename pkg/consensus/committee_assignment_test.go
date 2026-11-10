package consensus

import (
	"crypto/sha256"
	"testing"
)

func testSeed() [32]byte {
	return sha256.Sum256([]byte("test_seed_for_committee_assignment"))
}

func makeActiveIndices(n int) []ValidatorIndex {
	indices := make([]ValidatorIndex, n)
	for i := 0; i < n; i++ {
		indices[i] = ValidatorIndex(i)
	}
	return indices
}

func makeEffBalances(indices []ValidatorIndex, balance uint64) map[ValidatorIndex]uint64 {
	m := make(map[ValidatorIndex]uint64)
	for _, idx := range indices {
		m[idx] = balance
	}
	return m
}

func TestSwapOrNotShuffleZeroCount(t *testing.T) {
	seed := testSeed()
	_, err := SwapOrNotShuffle(0, 0, seed)
	if err != ErrCAZeroCount {
		t.Fatalf("expected ErrCAZeroCount, got %v", err)
	}
}

func TestSwapOrNotShuffleOutOfRange(t *testing.T) {
	seed := testSeed()
	_, err := SwapOrNotShuffle(10, 5, seed)
	if err != ErrCAInvalidIndex {
		t.Fatalf("expected ErrCAInvalidIndex, got %v", err)
	}
}

func TestSwapOrNotShuffleSingleElement(t *testing.T) {
	seed := testSeed()
	result, err := SwapOrNotShuffle(0, 1, seed)
	if err != nil {
		t.Fatalf("SwapOrNotShuffle: %v", err)
	}
	if result != 0 {
		t.Errorf("result=%d, want 0", result)
	}
}

func TestSwapOrNotShuffleDeterministic(t *testing.T) {
	seed := testSeed()
	n := uint64(100)

	result1, err := SwapOrNotShuffle(42, n, seed)
	if err != nil {
		t.Fatalf("SwapOrNotShuffle: %v", err)
	}

	result2, err := SwapOrNotShuffle(42, n, seed)
	if err != nil {
		t.Fatalf("SwapOrNotShuffle: %v", err)
	}

	if result1 != result2 {
		t.Errorf("shuffle not deterministic: %d != %d", result1, result2)
	}
}

func TestSwapOrNotShufflePermutation(t *testing.T) {
	seed := testSeed()
	n := uint64(20)

	seen := make(map[uint64]bool)
	for i := uint64(0); i < n; i++ {
		result, err := SwapOrNotShuffle(i, n, seed)
		if err != nil {
			t.Fatalf("SwapOrNotShuffle(%d): %v", i, err)
		}
		if result >= n {
			t.Fatalf("shuffled index %d out of range [0, %d)", result, n)
		}
		if seen[result] {
			t.Fatalf("duplicate shuffled index %d", result)
		}
		seen[result] = true
	}

	if uint64(len(seen)) != n {
		t.Errorf("expected %d unique results, got %d", n, len(seen))
	}
}

func TestSwapOrNotShuffleDifferentSeeds(t *testing.T) {
	n := uint64(50)
	seed1 := sha256.Sum256([]byte("seed_a"))
	seed2 := sha256.Sum256([]byte("seed_b"))

	r1, _ := SwapOrNotShuffle(0, n, seed1)
	r2, _ := SwapOrNotShuffle(0, n, seed2)

	// Different seeds should (almost always) produce different results.
	// Not a guarantee, but very unlikely to be equal for any reasonable
	// test case.
	if r1 == r2 {
		t.Error("fixed seeds produced identical shuffled indices")
	}
}

func TestShuffleIndicesCA(t *testing.T) {
	seed := testSeed()
	indices := makeActiveIndices(16)
	shuffled, err := ShuffleIndices(indices, seed)
	if err != nil {
		t.Fatalf("ShuffleIndices: %v", err)
	}

	if len(shuffled) != 16 {
		t.Fatalf("len=%d, want 16", len(shuffled))
	}

	// Verify it's a permutation.
	seen := make(map[ValidatorIndex]bool)
	for _, idx := range shuffled {
		if seen[idx] {
			t.Fatalf("duplicate index %d", idx)
		}
		seen[idx] = true
	}

	// Verify it's different from the original (extremely likely).
	same := true
	for i, idx := range shuffled {
		if idx != indices[i] {
			same = false
			break
		}
	}
	if same {
		t.Error("shuffled indices identical to original (extremely unlikely)")
	}
}

func TestShuffleIndicesEmpty(t *testing.T) {
	seed := testSeed()
	_, err := ShuffleIndices(nil, seed)
	if err != ErrCANoValidators {
		t.Fatalf("expected ErrCANoValidators, got %v", err)
	}
}

func TestComputeCommitteeCount(t *testing.T) {
	ca := NewCommitteeAssigner(DefaultCommitteeAssignerConfig())

	// 128 validators: 128 / 32 / 128 = 0 => 1.
	if c := ca.ComputeCommitteeCount(128); c != 1 {
		t.Errorf("count=%d, want 1", c)
	}

	// 4096 validators: 4096 / 32 / 128 = 1.
	if c := ca.ComputeCommitteeCount(4096); c != 1 {
		t.Errorf("count=%d, want 1", c)
	}

	// 4097 validators: 4097 / 32 / 128 = 1.
	if c := ca.ComputeCommitteeCount(4097); c != 1 {
		t.Errorf("count=%d, want 1", c)
	}

	// 262144 validators: 262144 / 32 / 128 = 64.
	if c := ca.ComputeCommitteeCount(262144); c != 64 {
		t.Errorf("count=%d, want 64", c)
	}

	// Very large: should be capped at 64.
	if c := ca.ComputeCommitteeCount(1000000); c != 64 {
		t.Errorf("count=%d, want 64 (capped)", c)
	}
}

func TestComputeBeaconCommittees(t *testing.T) {
	ca := NewCommitteeAssigner(DefaultCommitteeAssignerConfig())
	seed := testSeed()
	indices := makeActiveIndices(256)

	results, err := ca.ComputeBeaconCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("ComputeBeaconCommittees: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected non-empty results")
	}

	// Verify all indices are covered.
	totalMembers := 0
	for _, r := range results {
		totalMembers += len(r.Members)
	}
	if totalMembers != 256 {
		t.Errorf("total members=%d, want 256", totalMembers)
	}

	// Verify cache works.
	results2, err := ca.ComputeBeaconCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("cached call: %v", err)
	}
	if len(results2) != len(results) {
		t.Error("cached results differ in length")
	}
}

func TestComputeBeaconCommitteesNoValidators(t *testing.T) {
	ca := NewCommitteeAssigner(DefaultCommitteeAssignerConfig())
	seed := testSeed()
	_, err := ca.ComputeBeaconCommittees(0, nil, seed)
	if err != ErrCANoValidators {
		t.Fatalf("expected ErrCANoValidators, got %v", err)
	}
}

func TestComputeProposerAssignments(t *testing.T) {
	ca := NewCommitteeAssigner(DefaultCommitteeAssignerConfig())
	seed := testSeed()
	indices := makeActiveIndices(128)
	balances := makeEffBalances(indices, 32*GweiPerETH)

	assignments, err := ca.ComputeProposerAssignments(0, indices, balances, seed)
	if err != nil {
		t.Fatalf("ComputeProposerAssignments: %v", err)
	}

	if len(assignments) != 32 {
		t.Fatalf("len=%d, want 32 (slots per epoch)", len(assignments))
	}

	// Each slot should have a valid proposer index.
	for _, a := range assignments {
		if int(a.Proposer) >= len(indices) {
			t.Errorf("proposer index %d out of range", a.Proposer)
		}
		if a.Epoch != 0 {
			t.Errorf("epoch=%d, want 0", a.Epoch)
		}
	}

	// Verify deterministic (cache).
	assignments2, err := ca.ComputeProposerAssignments(0, indices, balances, seed)
	if err != nil {
		t.Fatalf("cached call: %v", err)
	}
	for i, a := range assignments2 {
		if a.Proposer != assignments[i].Proposer {
			t.Errorf("slot %d: cached proposer differs", i)
		}
	}
}

func TestComputeProposerAssignmentsNoValidators(t *testing.T) {
	ca := NewCommitteeAssigner(DefaultCommitteeAssignerConfig())
	seed := testSeed()
	_, err := ca.ComputeProposerAssignments(0, nil, nil, seed)
	if err != ErrCANoValidators {
		t.Fatalf("expected ErrCANoValidators, got %v", err)
	}
}

func TestProposerWeightedSelection(t *testing.T) {
	config := DefaultCommitteeAssignerConfig()
	config.MaxEffectiveBalanceCA = 2048 * GweiPerETH
	ca := NewCommitteeAssigner(config)
	seed := testSeed()

	// Create 8 validators, one with much higher balance.
	indices := makeActiveIndices(8)
	balances := makeEffBalances(indices, 32*GweiPerETH)
	uniform, err := ca.ComputeProposerAssignments(0, indices, balances, seed)
	if err != nil {
		t.Fatal(err)
	}
	balances[0] = 2048 * GweiPerETH // max effective balance validator

	assignments, err := NewCommitteeAssigner(config).ComputeProposerAssignments(0, indices, balances, seed)
	if err != nil {
		t.Fatalf("ComputeProposerAssignments: %v", err)
	}

	countFirst := func(assignments []ProposerAssignment) int {
		count := 0
		for _, a := range assignments {
			if a.Proposer == 0 {
				count++
			}
		}
		return count
	}
	if got, baseline := countFirst(assignments), countFirst(uniform); got <= baseline {
		t.Fatalf("validator 0 selected %d times with higher balance, want more than %d", got, baseline)
	}
}

func TestComputeSyncCommitteeCA(t *testing.T) {
	ca := NewCommitteeAssigner(DefaultCommitteeAssignerConfig())
	seed := testSeed()
	indices := makeActiveIndices(1024)
	balances := makeEffBalances(indices, 32*GweiPerETH)

	result, err := ca.ComputeSyncCommitteeCA(0, indices, balances, seed)
	if err != nil {
		t.Fatalf("ComputeSyncCommitteeCA: %v", err)
	}

	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result.Members) != CASyncCommitteeSize {
		t.Errorf("sync committee size=%d, want %d", len(result.Members), CASyncCommitteeSize)
	}
	if result.Period != 0 {
		t.Errorf("period=%d, want 0", result.Period)
	}

	// Verify cache.
	result2, err := ca.ComputeSyncCommitteeCA(0, indices, balances, seed)
	if err != nil {
		t.Fatalf("cached call: %v", err)
	}
	if len(result2.Members) != len(result.Members) {
		t.Error("cached result differs in size")
	}
}

func TestComputeSyncCommitteeCANoValidators(t *testing.T) {
	ca := NewCommitteeAssigner(DefaultCommitteeAssignerConfig())
	seed := testSeed()
	_, err := ca.ComputeSyncCommitteeCA(0, nil, nil, seed)
	if err != ErrCANoValidators {
		t.Fatalf("expected ErrCANoValidators, got %v", err)
	}
}

func TestGetCommitteeForSlot(t *testing.T) {
	ca := NewCommitteeAssigner(DefaultCommitteeAssignerConfig())
	seed := testSeed()
	indices := makeActiveIndices(256)

	_, err := ca.ComputeBeaconCommittees(0, indices, seed)
	if err != nil {
		t.Fatalf("ComputeBeaconCommittees: %v", err)
	}

	// Get committee for slot 0, index 0.
	members, err := ca.GetCommitteeForSlot(0, 0, 0)
	if err != nil {
		t.Fatalf("GetCommitteeForSlot: %v", err)
	}
	if len(members) == 0 {
		t.Fatal("expected non-empty committee")
	}

	// Invalid slot (no committees computed for epoch 5).
	_, err = ca.GetCommitteeForSlot(5, 160, 0)
	if err != ErrCAInvalidSlot {
		t.Fatalf("expected ErrCAInvalidSlot, got %v", err)
	}
}

func TestClearCaches(t *testing.T) {
	ca := NewCommitteeAssigner(DefaultCommitteeAssignerConfig())
	seed := testSeed()
	indices := makeActiveIndices(128)

	ca.ComputeBeaconCommittees(0, indices, seed)

	ca.ClearCaches()

	// After clearing, should get error for lookup.
	_, err := ca.GetCommitteeForSlot(0, 0, 0)
	if err != ErrCAInvalidSlot {
		t.Fatalf("expected ErrCAInvalidSlot after cache clear, got %v", err)
	}
}

func TestComputeAttesterSeed(t *testing.T) {
	mix := sha256.Sum256([]byte("randao_mix"))
	seed1 := ComputeAttesterSeed(0, mix)
	seed2 := ComputeAttesterSeed(0, mix)
	if seed1 != seed2 {
		t.Fatal("seed computation not deterministic")
	}

	seed3 := ComputeAttesterSeed(1, mix)
	if seed1 == seed3 {
		t.Fatal("different epochs should produce different seeds")
	}
}

func TestComputeProposerSeed(t *testing.T) {
	mix := sha256.Sum256([]byte("randao_mix"))
	seed1 := ComputeProposerSeed(0, mix)
	seed2 := ComputeProposerSeed(1, mix)
	if seed1 == seed2 {
		t.Fatal("different epochs should produce different seeds")
	}
}

func TestComputeSyncSeedFunc(t *testing.T) {
	mix := sha256.Sum256([]byte("randao_mix"))
	seed1 := ComputeSyncSeed(0, mix)
	seed2 := ComputeSyncSeed(256, mix)
	if seed1 == seed2 {
		t.Fatal("different epochs should produce different sync seeds")
	}
}

func TestSortedActiveIndicesFunc(t *testing.T) {
	validators := []*ValidatorRecordV2{
		{Index: 0, ActivationEpoch: 0, ExitEpoch: FarFutureEpoch},
		{Index: 1, ActivationEpoch: 100, ExitEpoch: FarFutureEpoch}, // not active at epoch 5
		{Index: 2, ActivationEpoch: 0, ExitEpoch: FarFutureEpoch},
		{Index: 3, ActivationEpoch: 0, ExitEpoch: 3}, // exited at epoch 3
	}

	indices := SortedActiveIndices(validators, 5)
	if len(indices) != 2 {
		t.Fatalf("active count=%d, want 2", len(indices))
	}
	if indices[0] != 0 || indices[1] != 2 {
		t.Errorf("indices=%v, want [0 2]", indices)
	}
}
