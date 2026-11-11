package encrypted

import (
	"testing"
)

func TestVDFDecryptionTimer_GenerateSlotPuzzle(t *testing.T) {
	timer := NewVDFDecryptionTimer(2)
	puzzle := timer.GenerateSlotPuzzle(42)

	if puzzle.Slot != 42 {
		t.Errorf("expected slot 42, got %d", puzzle.Slot)
	}
	if len(puzzle.Input) == 0 {
		t.Error("puzzle input should not be empty")
	}
	if puzzle.Difficulty != 2 {
		t.Errorf("expected difficulty 2, got %d", puzzle.Difficulty)
	}
	if puzzle.Solved {
		t.Error("new puzzle should not be solved")
	}
}

func TestVDFDecryptionTimer_GenerateSlotPuzzle_Idempotent(t *testing.T) {
	timer := NewVDFDecryptionTimer(2)
	p1 := timer.GenerateSlotPuzzle(10)
	p2 := timer.GenerateSlotPuzzle(10)

	if p1 != p2 {
		t.Error("same slot should return same puzzle pointer")
	}
	if timer.PuzzleCount() != 1 {
		t.Errorf("expected 1 puzzle, got %d", timer.PuzzleCount())
	}
}

func TestVDFDecryptionTimer_SubmitSolution(t *testing.T) {
	timer := NewVDFDecryptionTimer(2) // low difficulty for fast solving
	puzzle := timer.GenerateSlotPuzzle(1)

	solution := SolveVDFPuzzle(puzzle)
	if solution == nil {
		t.Fatal("failed to solve puzzle")
	}

	ok, err := timer.SubmitSolution(1, solution)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("valid solution should return true")
	}
	if !puzzle.Solved {
		t.Error("puzzle should be marked solved")
	}
}

func TestVDFDecryptionTimer_SubmitSolution_Invalid(t *testing.T) {
	// Use high difficulty so random bytes almost certainly fail.
	timer := NewVDFDecryptionTimer(1 << 20)
	timer.GenerateSlotPuzzle(1)

	bad := []byte{0xFF, 0xFF, 0xFF, 0xFF}
	ok, err := timer.SubmitSolution(1, bad)
	if err != ErrVDFTimerInvalidSolution {
		t.Fatalf("expected ErrVDFTimerInvalidSolution, got ok=%v err=%v", ok, err)
	}
	if ok {
		t.Error("invalid solution should return false")
	}
}

func TestVDFDecryptionTimer_SubmitSolution_NoPuzzle(t *testing.T) {
	timer := NewVDFDecryptionTimer(2)
	_, err := timer.SubmitSolution(999, []byte{0x01})
	if err != ErrVDFTimerNilPuzzle {
		t.Fatalf("expected ErrVDFTimerNilPuzzle, got %v", err)
	}
}

func TestVDFDecryptionTimer_IsSlotReady(t *testing.T) {
	timer := NewVDFDecryptionTimer(2)
	puzzle := timer.GenerateSlotPuzzle(5)

	if timer.IsSlotReady(5) {
		t.Error("slot should not be ready before solving")
	}

	solution := SolveVDFPuzzle(puzzle)
	if solution == nil {
		t.Fatal("failed to solve puzzle")
	}
	timer.SubmitSolution(5, solution)

	if !timer.IsSlotReady(5) {
		t.Error("slot should be ready after solving")
	}

	// Unknown slot returns false.
	if timer.IsSlotReady(999) {
		t.Error("unknown slot should not be ready")
	}
}

func TestVDFDecryptionTimer_GetDecryptionKey(t *testing.T) {
	timer := NewVDFDecryptionTimer(2)
	puzzle := timer.GenerateSlotPuzzle(7)

	solution := SolveVDFPuzzle(puzzle)
	if solution == nil {
		t.Fatal("failed to solve puzzle")
	}
	timer.SubmitSolution(7, solution)

	key, err := timer.GetDecryptionKey(7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key) != 32 {
		t.Errorf("expected 32-byte key, got %d bytes", len(key))
	}

	// Same slot should produce same key.
	key2, _ := timer.GetDecryptionKey(7)
	for i := range key {
		if key[i] != key2[i] {
			t.Error("key should be deterministic")
			break
		}
	}
}

func TestVDFDecryptionTimer_GetDecryptionKey_NotReady(t *testing.T) {
	timer := NewVDFDecryptionTimer(2)
	timer.GenerateSlotPuzzle(3)

	_, err := timer.GetDecryptionKey(3)
	if err != ErrVDFTimerNotReady {
		t.Fatalf("expected ErrVDFTimerNotReady, got %v", err)
	}
}

func TestVDFDecryptionTimer_PruneBefore(t *testing.T) {
	timer := NewVDFDecryptionTimer(2)
	timer.GenerateSlotPuzzle(1)
	timer.GenerateSlotPuzzle(5)
	timer.GenerateSlotPuzzle(10)
	timer.GenerateSlotPuzzle(15)

	pruned := timer.PruneBefore(10)
	if pruned != 2 {
		t.Errorf("expected 2 pruned, got %d", pruned)
	}
	if timer.PuzzleCount() != 2 {
		t.Errorf("expected 2 remaining, got %d", timer.PuzzleCount())
	}
}

func TestVDFTimedDecryptor_TryDecrypt_NotReady(t *testing.T) {
	timer := NewVDFDecryptionTimer(2)
	timer.GenerateSlotPuzzle(1)

	dec, _ := NewThresholdDecryptor(1, 1)
	vtd := NewVDFTimedDecryptor(timer, dec, 1)

	_, err := vtd.TryDecrypt()
	if err != ErrVDFTimerNotReady {
		t.Fatalf("expected ErrVDFTimerNotReady, got %v", err)
	}

	if vtd.Slot() != 1 {
		t.Errorf("expected slot 1, got %d", vtd.Slot())
	}
}

func TestSolveVDFPuzzle(t *testing.T) {
	timer := NewVDFDecryptionTimer(2) // very low difficulty
	puzzle := timer.GenerateSlotPuzzle(100)

	solution := SolveVDFPuzzle(puzzle)
	if solution == nil {
		t.Fatal("SolveVDFPuzzle should find a solution with low difficulty")
	}
	if ok, err := timer.SubmitSolution(puzzle.Slot, solution); err != nil || !ok {
		t.Errorf("solution verification = %t, %v", ok, err)
	}
}

func TestVDFDecryptionTimer_DefaultDifficulty(t *testing.T) {
	timer := NewVDFDecryptionTimer(0)
	puzzle := timer.GenerateSlotPuzzle(1)
	if puzzle.Difficulty != 1<<16 {
		t.Errorf("expected default difficulty %d, got %d", 1<<16, puzzle.Difficulty)
	}
}

func TestVDFDecryptionTimer_SubmitSolution_AlreadySolved(t *testing.T) {
	timer := NewVDFDecryptionTimer(2)
	puzzle := timer.GenerateSlotPuzzle(1)
	solution := SolveVDFPuzzle(puzzle)
	if solution == nil {
		t.Fatal("failed to solve")
	}

	timer.SubmitSolution(1, solution)
	// Submit again -- should return true without error.
	ok, err := timer.SubmitSolution(1, []byte{0x00})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ok {
		t.Error("already-solved slot should return true")
	}
}
