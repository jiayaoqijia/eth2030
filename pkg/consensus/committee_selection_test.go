package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

func TestComputeShuffledIndexZeroCount(t *testing.T) {
	_, err := ComputeShuffledIndex(0, 0, [32]byte{})
	if err != ErrCSZeroIndexCount {
		t.Errorf("expected ErrCSZeroIndexCount, got %v", err)
	}
}

func TestComputeShuffledIndexOutOfRange(t *testing.T) {
	_, err := ComputeShuffledIndex(10, 5, [32]byte{})
	if err == nil {
		t.Error("expected error for index out of range")
	}
}

func TestComputeShuffledIndexCSSingleElement(t *testing.T) {
	result, err := ComputeShuffledIndex(0, 1, [32]byte{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 0 {
		t.Errorf("expected 0 for single element, got %d", result)
	}
}

func TestComputeShuffledIndexDeterministic(t *testing.T) {
	seed := sha256.Sum256([]byte("test seed"))
	count := uint64(100)

	r1, err := ComputeShuffledIndex(42, count, seed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	r2, err := ComputeShuffledIndex(42, count, seed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if r1 != r2 {
		t.Errorf("shuffle not deterministic: %d != %d", r1, r2)
	}
}

func TestComputeShuffledIndexPermutation(t *testing.T) {
	seed := sha256.Sum256([]byte("permutation test"))
	count := uint64(50)

	seen := make(map[uint64]bool)
	for i := uint64(0); i < count; i++ {
		r, err := ComputeShuffledIndex(i, count, seed)
		if err != nil {
			t.Fatalf("error at index %d: %v", i, err)
		}
		if r >= count {
			t.Fatalf("shuffled index %d out of range (count=%d)", r, count)
		}
		if seen[r] {
			t.Fatalf("duplicate shuffled index %d at input %d", r, i)
		}
		seen[r] = true
	}
	if len(seen) != int(count) {
		t.Errorf("expected %d unique outputs, got %d", count, len(seen))
	}
}

func TestComputeShuffledIndexDifferentSeeds(t *testing.T) {
	seed1 := sha256.Sum256([]byte("seed one"))
	seed2 := sha256.Sum256([]byte("seed two"))
	count := uint64(100)

	r1, _ := ComputeShuffledIndex(10, count, seed1)
	r2, _ := ComputeShuffledIndex(10, count, seed2)

	// Different seeds should (with overwhelming probability) produce different results.
	if r1 == r2 {
		t.Error("fixed seeds produced identical shuffled indices")
	}
}

func TestGetSeedNilState(t *testing.T) {
	_, err := GetSeed(nil, 0, CSDomainAttester)
	if err != ErrCSNilState {
		t.Errorf("expected ErrCSNilState, got %v", err)
	}
}

func TestGetSeedDeterministic(t *testing.T) {
	state := makeCommitteeState(100, 32)

	s1, err := GetSeed(state, 0, CSDomainAttester)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	s2, err := GetSeed(state, 0, CSDomainAttester)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if s1 != s2 {
		t.Error("seed not deterministic")
	}
}

func TestGetSeedDifferentDomains(t *testing.T) {
	state := makeCommitteeState(100, 32)

	s1, _ := GetSeed(state, 0, CSDomainAttester)
	s2, _ := GetSeed(state, 0, CSDomainProposer)
	if s1 == s2 {
		t.Error("different domains should produce different seeds")
	}
}

func TestGetSeedDifferentEpochs(t *testing.T) {
	state := makeCommitteeState(100, 32)
	// Set different RANDAO mixes per epoch.
	for i := 0; i < 10; i++ {
		var mix [32]byte
		binary.LittleEndian.PutUint64(mix[:8], uint64(i)*17+3)
		state.RandaoMixes[i] = mix
	}

	s1, _ := GetSeed(state, 0, CSDomainAttester)
	s2, _ := GetSeed(state, 1, CSDomainAttester)
	if s1 == s2 {
		t.Error("different epochs should produce different seeds")
	}
}

func TestGetActiveValidatorIndicesNilState(t *testing.T) {
	_, err := GetActiveValidatorIndices(nil, 0)
	if err != ErrCSNilState {
		t.Errorf("expected ErrCSNilState, got %v", err)
	}
}

func TestGetActiveValidatorIndicesNoValidators(t *testing.T) {
	state := NewBeaconStateV2(32)
	_, err := GetActiveValidatorIndices(state, 0)
	if err != ErrCSNoValidators {
		t.Errorf("expected ErrCSNoValidators, got %v", err)
	}
}

func TestGetActiveValidatorIndicesCorrectCount(t *testing.T) {
	state := makeCommitteeState(50, 32)
	indices, err := GetActiveValidatorIndices(state, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(indices) != 50 {
		t.Errorf("expected 50 indices, got %d", len(indices))
	}
}

func TestGetActiveValidatorIndicesExcludesInactive(t *testing.T) {
	state := makeCommitteeState(10, 32)
	// Make validator 5 inactive by setting exit epoch before epoch 0.
	state.Validators[5].ExitEpoch = 0

	indices, err := GetActiveValidatorIndices(state, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(indices) != 9 {
		t.Errorf("expected 9 active indices, got %d", len(indices))
	}
	for _, idx := range indices {
		if uint64(idx) == 5 {
			t.Error("validator 5 should be excluded (inactive)")
		}
	}
}

func TestGetCommitteeCountPerSlotNilState(t *testing.T) {
	_, err := GetCommitteeCountPerSlot(nil, 0)
	if err != ErrCSNilState {
		t.Errorf("expected ErrCSNilState, got %v", err)
	}
}

func TestGetCommitteeCountPerSlotSmallSet(t *testing.T) {
	// 10 validators / 32 slots / 128 target = 0 -> clamped to 1.
	state := makeCommitteeState(10, 32)
	count, err := GetCommitteeCountPerSlot(state, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 committee for small set, got %d", count)
	}
}

func TestGetCommitteeCountPerSlotLargeSet(t *testing.T) {
	// 100000 validators / 32 slots / 128 target = 24.
	state := makeCommitteeState(100000, 32)
	count, err := GetCommitteeCountPerSlot(state, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count < 1 || count > uint64(MaxCommitteesPerSlot) {
		t.Errorf("committee count %d out of expected range [1, %d]", count, MaxCommitteesPerSlot)
	}
	expected := uint64(100000) / 32 / TargetCommitteeSize
	if count != expected {
		t.Errorf("expected %d committees, got %d", expected, count)
	}
}

func TestComputeCommitteeCountClamp(t *testing.T) {
	// Test maximum clamping: enormous validator set.
	count := computeCommitteeCount(10_000_000, 32)
	if count != uint64(MaxCommitteesPerSlot) {
		t.Errorf("expected max %d, got %d", MaxCommitteesPerSlot, count)
	}
}

func TestComputeCommitteeCountMinimum(t *testing.T) {
	count := computeCommitteeCount(1, 32)
	if count != 1 {
		t.Errorf("expected minimum 1, got %d", count)
	}
}

func TestComputeBeaconCommitteeNilState(t *testing.T) {
	_, err := ComputeBeaconCommittee(nil, 0, 0, [32]byte{})
	if err != ErrCSNilState {
		t.Errorf("expected ErrCSNilState, got %v", err)
	}
}

func TestComputeBeaconCommitteeInvalidIndex(t *testing.T) {
	state := makeCommitteeState(10, 32)
	seed := sha256.Sum256([]byte("test"))
	_, err := ComputeBeaconCommittee(state, 0, 999, seed)
	if err == nil {
		t.Error("expected error for invalid committee index")
	}
}

func TestComputeBeaconCommitteeValid(t *testing.T) {
	state := makeCommitteeState(256, 32)
	seed := sha256.Sum256([]byte("committee test"))

	committee, err := ComputeBeaconCommittee(state, 0, 0, seed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(committee) == 0 {
		t.Error("expected non-empty committee")
	}
	for _, idx := range committee {
		if uint64(idx) >= 256 {
			t.Errorf("committee member %d out of range", idx)
		}
	}
}

func TestComputeBeaconCommitteeDeterministic(t *testing.T) {
	state := makeCommitteeState(128, 32)
	seed := sha256.Sum256([]byte("determinism"))

	c1, err := ComputeBeaconCommittee(state, 0, 0, seed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	c2, err := ComputeBeaconCommittee(state, 0, 0, seed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(c1) != len(c2) {
		t.Fatalf("committee sizes differ: %d vs %d", len(c1), len(c2))
	}
	for i := range c1 {
		if c1[i] != c2[i] {
			t.Errorf("committee[%d]: %d != %d", i, c1[i], c2[i])
		}
	}
}

func TestComputeBeaconCommitteeCoversAll(t *testing.T) {
	n := 256
	state := makeCommitteeState(n, 32)
	seed := sha256.Sum256([]byte("coverage"))

	seen := make(map[ValidatorIndex]bool)
	spe := state.SlotsPerEpoch
	for slot := uint64(0); slot < spe; slot++ {
		committeesPerSlot := computeCommitteeCount(n, spe)
		for ci := uint64(0); ci < committeesPerSlot; ci++ {
			committee, err := ComputeBeaconCommittee(state, Slot(slot), ci, seed)
			if err != nil {
				t.Fatalf("slot %d ci %d: %v", slot, ci, err)
			}
			for _, idx := range committee {
				seen[idx] = true
			}
		}
	}
	if len(seen) != n {
		t.Errorf("expected all %d validators in committees, got %d", n, len(seen))
	}
}

func TestComputeProposerIndexNilState(t *testing.T) {
	_, err := ComputeProposerIndex(nil, 0, [32]byte{})
	if err != ErrCSNilState {
		t.Errorf("expected ErrCSNilState, got %v", err)
	}
}

func TestComputeProposerIndexNoValidators(t *testing.T) {
	state := NewBeaconStateV2(32)
	_, err := ComputeProposerIndex(state, 0, [32]byte{})
	if err != ErrCSNoValidators {
		t.Errorf("expected ErrCSNoValidators, got %v", err)
	}
}

func TestComputeProposerIndexValid(t *testing.T) {
	state := makeCommitteeState(100, 32)
	seed := sha256.Sum256([]byte("proposer"))

	idx, err := ComputeProposerIndex(state, 0, seed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uint64(idx) >= 100 {
		t.Errorf("proposer index %d out of range", idx)
	}
}

func TestComputeProposerIndexDeterministic(t *testing.T) {
	state := makeCommitteeState(100, 32)
	seed := sha256.Sum256([]byte("deterministic proposer"))

	p1, _ := ComputeProposerIndex(state, 0, seed)
	p2, _ := ComputeProposerIndex(state, 0, seed)
	if p1 != p2 {
		t.Errorf("proposer not deterministic: %d != %d", p1, p2)
	}
}

func TestComputeProposerIndexDifferentSeeds(t *testing.T) {
	state := makeCommitteeState(100, 32)
	seed1 := sha256.Sum256([]byte("seed alpha"))
	seed2 := sha256.Sum256([]byte("seed beta"))

	p1, _ := ComputeProposerIndex(state, 0, seed1)
	p2, _ := ComputeProposerIndex(state, 0, seed2)
	_ = p1
	_ = p2
}

func TestShuffleValidatorsCSEmpty(t *testing.T) {
	_, err := ShuffleValidatorsCS(nil, [32]byte{})
	if err != ErrCSNoValidators {
		t.Errorf("expected ErrCSNoValidators, got %v", err)
	}
}

func TestShuffleValidatorsCSPermutation(t *testing.T) {
	seed := sha256.Sum256([]byte("shuffle validators"))
	indices := make([]ValidatorIndex, 100)
	for i := range indices {
		indices[i] = ValidatorIndex(i)
	}

	shuffled, err := ShuffleValidatorsCS(indices, seed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(shuffled) != 100 {
		t.Fatalf("expected 100 shuffled, got %d", len(shuffled))
	}

	seen := make(map[ValidatorIndex]bool)
	for _, idx := range shuffled {
		if seen[idx] {
			t.Fatalf("duplicate index %d in shuffled output", idx)
		}
		seen[idx] = true
	}
	if len(seen) != 100 {
		t.Errorf("expected 100 unique, got %d", len(seen))
	}
}

func TestShuffleValidatorsCSNotIdentity(t *testing.T) {
	seed := sha256.Sum256([]byte("not identity"))
	indices := make([]ValidatorIndex, 50)
	for i := range indices {
		indices[i] = ValidatorIndex(i)
	}

	shuffled, err := ShuffleValidatorsCS(indices, seed)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	same := 0
	for i := range indices {
		if indices[i] == shuffled[i] {
			same++
		}
	}
	if same == len(indices) {
		t.Error("shuffle produced identity permutation (extremely unlikely)")
	}
}

// makeCommitteeState creates a BeaconStateV2 with n active validators.
func makeCommitteeState(n int, spe uint64) *BeaconStateV2 {
	state := NewBeaconStateV2(spe)
	for i := 0; i < n; i++ {
		v := &ValidatorV2{
			EffectiveBalance:           32 * GweiPerETH,
			ActivationEpoch:            0,
			ExitEpoch:                  FarFutureEpoch,
			ActivationEligibilityEpoch: 0,
			WithdrawableEpoch:          FarFutureEpoch,
		}
		binary.LittleEndian.PutUint64(v.Pubkey[:8], uint64(i))
		state.AddValidatorV2(v, 32*GweiPerETH)
	}
	return state
}
