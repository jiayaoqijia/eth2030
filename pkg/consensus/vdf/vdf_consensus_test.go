package vdf

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

// computeExpectedOutput computes the VDF output a validator would produce
// for a given epoch and seed, using the same domain separation as VDFConsensus.
func computeExpectedOutput(t *testing.T, seed []byte, epochNum uint64, difficulty uint64) []byte {
	t.Helper()
	vdf := crypto.NewVDFv2(crypto.DefaultVDFv2Config())
	domainInput := crypto.Keccak256(seed, epochUint64Bytes(epochNum))
	result, err := vdf.Evaluate(domainInput, difficulty)
	if err != nil {
		t.Fatalf("failed to compute expected VDF output: %v", err)
	}
	return result.EncodeSolution()
}

func TestNewVDFConsensus(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	if vc == nil {
		t.Fatal("NewVDFConsensus returned nil")
	}
	if vc.CurrentEpoch() != 0 {
		t.Errorf("expected initial epoch 0, got %d", vc.CurrentEpoch())
	}
}

func TestNewVDFConsensusDefaultsClamp(t *testing.T) {
	cfg := VDFConsensusConfig{} // all zeros
	vc := NewVDFConsensus(cfg)
	c := vc.Config()
	if c.VDFDifficulty == 0 {
		t.Error("VDFDifficulty should be clamped to non-zero")
	}
	if c.EpochLength == 0 {
		t.Error("EpochLength should be clamped to non-zero")
	}
	if c.MinParticipation <= 0 {
		t.Error("MinParticipation should be clamped to positive")
	}
}

func TestBeginEpoch(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	seed := []byte("epoch-seed-data-1234")

	if err := vc.BeginEpoch(1, seed); err != nil {
		t.Fatalf("BeginEpoch(1) failed: %v", err)
	}
	if vc.CurrentEpoch() != 1 {
		t.Errorf("expected current epoch 1, got %d", vc.CurrentEpoch())
	}
}

func TestBeginEpochZero(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	err := vc.BeginEpoch(0, []byte("seed"))
	if err != errVDFZeroEpoch {
		t.Errorf("expected errVDFZeroEpoch, got %v", err)
	}
}

func TestBeginEpochEmptySeed(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	err := vc.BeginEpoch(1, []byte{})
	if err != errVDFEmptySeed {
		t.Errorf("expected errVDFEmptySeed, got %v", err)
	}
}

func TestBeginEpochDuplicate(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	seed := []byte("seed")

	_ = vc.BeginEpoch(1, seed)
	err := vc.BeginEpoch(1, seed)
	if err != errVDFEpochAlreadyStarted {
		t.Errorf("expected errVDFEpochAlreadyStarted, got %v", err)
	}
}

func TestRevealOutput(t *testing.T) {
	cfg := DefaultVDFConsensusConfig()
	vc := NewVDFConsensus(cfg)
	seed := []byte("reveal-test-seed")

	_ = vc.BeginEpoch(1, seed)

	output := computeExpectedOutput(t, seed, 1, cfg.VDFDifficulty)

	err := vc.RevealOutput(1, "validator-1", output)
	if err != nil {
		t.Fatalf("RevealOutput failed: %v", err)
	}

	if vc.RevealCount(1) != 1 {
		t.Errorf("expected 1 reveal, got %d", vc.RevealCount(1))
	}
}

func TestRevealOutputInvalidVDF(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	seed := []byte("invalid-test-seed")

	_ = vc.BeginEpoch(1, seed)

	err := vc.RevealOutput(1, "validator-1", []byte("wrong-output"))
	if err != ErrVDFInvalidOutput {
		t.Errorf("expected ErrVDFInvalidOutput, got %v", err)
	}
}

func TestRevealOutputEpochNotStarted(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())

	err := vc.RevealOutput(99, "validator-1", []byte("output"))
	if err != ErrVDFEpochNotStarted {
		t.Errorf("expected ErrVDFEpochNotStarted, got %v", err)
	}
}

func TestRevealOutputDuplicate(t *testing.T) {
	cfg := DefaultVDFConsensusConfig()
	vc := NewVDFConsensus(cfg)
	seed := []byte("dup-test-seed")

	_ = vc.BeginEpoch(1, seed)
	output := computeExpectedOutput(t, seed, 1, cfg.VDFDifficulty)

	_ = vc.RevealOutput(1, "validator-1", output)
	err := vc.RevealOutput(1, "validator-1", output)
	if err != errVDFDuplicateReveal {
		t.Errorf("expected errVDFDuplicateReveal, got %v", err)
	}
}

func TestRevealOutputEmptyValidator(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	_ = vc.BeginEpoch(1, []byte("seed"))

	err := vc.RevealOutput(1, "", []byte("output"))
	if err != errVDFEmptyValidatorID {
		t.Errorf("expected errVDFEmptyValidatorID, got %v", err)
	}
}

func TestRevealOutputEmptyOutput(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	_ = vc.BeginEpoch(1, []byte("seed"))

	err := vc.RevealOutput(1, "val1", []byte{})
	if err != errVDFEmptyOutput {
		t.Errorf("expected errVDFEmptyOutput, got %v", err)
	}
}

func TestFinalizeEpoch(t *testing.T) {
	cfg := VDFConsensusConfig{
		VDFDifficulty:    5,
		EpochLength:      32,
		RevealWindow:     4,
		MinParticipation: 0.5,
	}
	vc := NewVDFConsensus(cfg)
	seed := []byte("finalize-test-seed")

	_ = vc.BeginEpoch(1, seed)

	output := computeExpectedOutput(t, seed, 1, cfg.VDFDifficulty)

	// Reveal from enough validators (min = 0.5 * 4 = 2).
	for i := 0; i < 3; i++ {
		id := "validator-" + string(rune('A'+i))
		err := vc.RevealOutput(1, id, output)
		if err != nil {
			t.Fatalf("RevealOutput(%s) failed: %v", id, err)
		}
	}

	er, err := vc.FinalizeEpoch(1)
	if err != nil {
		t.Fatalf("FinalizeEpoch failed: %v", err)
	}

	if !er.Finalized {
		t.Error("expected Finalized=true")
	}
	if er.EpochNum != 1 {
		t.Errorf("expected EpochNum=1, got %d", er.EpochNum)
	}
	if er.RevealedCount != 3 {
		t.Errorf("expected 3 reveals, got %d", er.RevealedCount)
	}
	if len(er.VDFOutput) != 32 {
		t.Errorf("expected 32-byte VDF output, got %d", len(er.VDFOutput))
	}
}

func TestFinalizeEpochInsufficientReveals(t *testing.T) {
	cfg := VDFConsensusConfig{
		VDFDifficulty:    5,
		EpochLength:      32,
		RevealWindow:     10,
		MinParticipation: 0.5,
	}
	vc := NewVDFConsensus(cfg)
	seed := []byte("insuff-test-seed")

	_ = vc.BeginEpoch(1, seed)

	output := computeExpectedOutput(t, seed, 1, cfg.VDFDifficulty)

	// Only 1 reveal, but need at least 5 (0.5 * 10).
	_ = vc.RevealOutput(1, "validator-1", output)

	_, err := vc.FinalizeEpoch(1)
	if err != ErrVDFInsufficientReveals {
		t.Errorf("expected ErrVDFInsufficientReveals, got %v", err)
	}
}

func TestFinalizeEpochNotStarted(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	_, err := vc.FinalizeEpoch(99)
	if err != ErrVDFEpochNotStarted {
		t.Errorf("expected ErrVDFEpochNotStarted, got %v", err)
	}
}

func TestFinalizeEpochAlreadyFinalized(t *testing.T) {
	cfg := VDFConsensusConfig{
		VDFDifficulty:    5,
		EpochLength:      32,
		RevealWindow:     2,
		MinParticipation: 0.5,
	}
	vc := NewVDFConsensus(cfg)
	seed := []byte("double-final-seed")

	_ = vc.BeginEpoch(1, seed)
	output := computeExpectedOutput(t, seed, 1, cfg.VDFDifficulty)
	_ = vc.RevealOutput(1, "val1", output)

	_, _ = vc.FinalizeEpoch(1)
	_, err := vc.FinalizeEpoch(1)
	if err != ErrVDFEpochAlreadyFinalized {
		t.Errorf("expected ErrVDFEpochAlreadyFinalized, got %v", err)
	}
}

func TestRevealAfterFinalize(t *testing.T) {
	cfg := VDFConsensusConfig{
		VDFDifficulty:    5,
		EpochLength:      32,
		RevealWindow:     2,
		MinParticipation: 0.5,
	}
	vc := NewVDFConsensus(cfg)
	seed := []byte("reveal-after-final-seed")

	_ = vc.BeginEpoch(1, seed)
	output := computeExpectedOutput(t, seed, 1, cfg.VDFDifficulty)
	_ = vc.RevealOutput(1, "val1", output)
	_, _ = vc.FinalizeEpoch(1)

	err := vc.RevealOutput(1, "val2", output)
	if err != ErrVDFEpochAlreadyFinalized {
		t.Errorf("expected ErrVDFEpochAlreadyFinalized, got %v", err)
	}
}

func TestGetRandomness(t *testing.T) {
	cfg := VDFConsensusConfig{
		VDFDifficulty:    5,
		EpochLength:      32,
		RevealWindow:     2,
		MinParticipation: 0.5,
	}
	vc := NewVDFConsensus(cfg)
	seed := []byte("get-rand-seed")

	_ = vc.BeginEpoch(1, seed)
	output := computeExpectedOutput(t, seed, 1, cfg.VDFDifficulty)
	_ = vc.RevealOutput(1, "val1", output)
	_, _ = vc.FinalizeEpoch(1)

	randomness, err := vc.GetRandomness(1)
	if err != nil {
		t.Fatalf("GetRandomness failed: %v", err)
	}
	if len(randomness) != 32 {
		t.Errorf("expected 32-byte randomness, got %d", len(randomness))
	}
}

func TestGetRandomnessNotFinalized(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	_ = vc.BeginEpoch(1, []byte("seed"))

	_, err := vc.GetRandomness(1)
	if err != ErrVDFEpochNotStarted {
		t.Errorf("expected ErrVDFEpochNotStarted for non-finalized, got %v", err)
	}
}

func TestGetRandomnessNotStarted(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())

	_, err := vc.GetRandomness(99)
	if err != ErrVDFEpochNotStarted {
		t.Errorf("expected ErrVDFEpochNotStarted, got %v", err)
	}
}

func TestIsEpochFinalized(t *testing.T) {
	cfg := VDFConsensusConfig{
		VDFDifficulty:    5,
		EpochLength:      32,
		RevealWindow:     2,
		MinParticipation: 0.5,
	}
	vc := NewVDFConsensus(cfg)
	seed := []byte("is-final-seed")

	if vc.IsEpochFinalized(1) {
		t.Error("epoch 1 should not be finalized before starting")
	}

	_ = vc.BeginEpoch(1, seed)
	if vc.IsEpochFinalized(1) {
		t.Error("epoch 1 should not be finalized right after starting")
	}

	output := computeExpectedOutput(t, seed, 1, cfg.VDFDifficulty)
	_ = vc.RevealOutput(1, "val1", output)
	_, _ = vc.FinalizeEpoch(1)

	if !vc.IsEpochFinalized(1) {
		t.Error("epoch 1 should be finalized after FinalizeEpoch")
	}
}

func TestCurrentEpochMultiple(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())

	_ = vc.BeginEpoch(3, []byte("seed3"))
	if vc.CurrentEpoch() != 3 {
		t.Errorf("expected epoch 3, got %d", vc.CurrentEpoch())
	}

	_ = vc.BeginEpoch(5, []byte("seed5"))
	if vc.CurrentEpoch() != 5 {
		t.Errorf("expected epoch 5, got %d", vc.CurrentEpoch())
	}

	// Starting a lower epoch should not change current.
	_ = vc.BeginEpoch(2, []byte("seed2"))
	if vc.CurrentEpoch() != 5 {
		t.Errorf("expected epoch still 5, got %d", vc.CurrentEpoch())
	}
}

func TestConcurrentAccess(t *testing.T) {
	cfg := VDFConsensusConfig{
		VDFDifficulty:    5,
		EpochLength:      32,
		RevealWindow:     4,
		MinParticipation: 0.25,
	}
	vc := NewVDFConsensus(cfg)
	seed := []byte("concurrent-seed")
	_ = vc.BeginEpoch(1, seed)

	output := computeExpectedOutput(t, seed, 1, cfg.VDFDifficulty)

	var wg sync.WaitGroup
	// Multiple goroutines trying to reveal and read state.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			vc.CurrentEpoch()
			vc.IsEpochFinalized(1)
			vc.RevealCount(1)
			// Only one will succeed per validator ID; others get duplicate error.
			id := "val-" + string(rune('A'+n%26))
			vc.RevealOutput(1, id, output)
		}(i)
	}
	wg.Wait()

	// Should be able to finalize.
	vc.FinalizeEpoch(1)
}

func TestFinalizeEpochZero(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	_, err := vc.FinalizeEpoch(0)
	if err != errVDFZeroEpoch {
		t.Errorf("expected errVDFZeroEpoch, got %v", err)
	}
}

func TestFinalizeNoReveals(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	_ = vc.BeginEpoch(1, []byte("seed"))

	_, err := vc.FinalizeEpoch(1)
	if err != ErrVDFInsufficientReveals {
		t.Errorf("expected ErrVDFInsufficientReveals, got %v", err)
	}
}

func TestDeterministicRandomness(t *testing.T) {
	// Two separate VDFConsensus instances with the same config, seed, and
	// reveals should produce the same finalized randomness.
	cfg := VDFConsensusConfig{
		VDFDifficulty:    5,
		EpochLength:      32,
		RevealWindow:     2,
		MinParticipation: 0.5,
	}
	seed := []byte("deterministic-seed")

	output := computeExpectedOutput(t, seed, 1, cfg.VDFDifficulty)

	vc1 := NewVDFConsensus(cfg)
	_ = vc1.BeginEpoch(1, seed)
	_ = vc1.RevealOutput(1, "valA", output)
	er1, _ := vc1.FinalizeEpoch(1)

	vc2 := NewVDFConsensus(cfg)
	_ = vc2.BeginEpoch(1, seed)
	_ = vc2.RevealOutput(1, "valA", output)
	er2, _ := vc2.FinalizeEpoch(1)

	if !sliceEqual(er1.VDFOutput, er2.VDFOutput) {
		t.Error("randomness should be deterministic for same inputs")
	}
}

func TestRevealCountForUnstartedEpoch(t *testing.T) {
	vc := NewVDFConsensus(DefaultVDFConsensusConfig())
	if vc.RevealCount(42) != 0 {
		t.Error("expected 0 reveals for unstarted epoch")
	}
}

func TestEpochUint64Bytes(t *testing.T) {
	b := epochUint64Bytes(0x0102030405060708)
	expected := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if !sliceEqual(b, expected) {
		t.Errorf("expected %x, got %x", expected, b)
	}
}

func TestSliceEqual(t *testing.T) {
	if !sliceEqual([]byte{1, 2, 3}, []byte{1, 2, 3}) {
		t.Error("equal slices should be equal")
	}
	if sliceEqual([]byte{1, 2, 3}, []byte{1, 2, 4}) {
		t.Error("different slices should not be equal")
	}
	if sliceEqual([]byte{1, 2}, []byte{1, 2, 3}) {
		t.Error("different length slices should not be equal")
	}
}
