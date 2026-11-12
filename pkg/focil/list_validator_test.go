package focil

import (
	"errors"
	"math/big"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- Test helpers ---

// mockHeadState implements HeadState for testing.
type mockHeadState struct {
	slot       uint64
	authorized map[uint64]bool // proposerIndex -> authorized
}

func (m *mockHeadState) Slot() uint64 { return m.slot }

func (m *mockHeadState) IsILCommitteeMember(proposerIndex uint64, slot uint64) bool {
	if m.authorized == nil {
		return true
	}
	return m.authorized[proposerIndex]
}

func newMockHeadState(slot uint64, authorizedProposers ...uint64) *mockHeadState {
	auth := make(map[uint64]bool)
	for _, p := range authorizedProposers {
		auth[p] = true
	}
	return &mockHeadState{slot: slot, authorized: auth}
}

func makeValidatorTx(nonce uint64, gas uint64) (*types.Transaction, []byte) {
	to := types.HexToAddress("0xdeaddeaddeaddeaddeaddeaddeaddeaddeaddead")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1000),
		Gas:      gas,
		To:       &to,
		Value:    big.NewInt(0),
		V:        big.NewInt(27),
		R:        big.NewInt(1),
		S:        big.NewInt(1),
	})
	data, _ := tx.EncodeRLP()
	return tx, data
}

func makeIL(slot uint64, proposer uint64, txCount int, gasPerTx uint64) *InclusionList {
	entries := make([]InclusionListEntry, txCount)
	for i := 0; i < txCount; i++ {
		_, data := makeValidatorTx(uint64(i), gasPerTx)
		entries[i] = InclusionListEntry{
			Transaction: data,
			Index:       uint64(i),
		}
	}
	return &InclusionList{
		Slot:          slot,
		ProposerIndex: proposer,
		Entries:       entries,
	}
}

// --- DefaultListValidatorConfig ---

func TestDefaultListValidatorConfig(t *testing.T) {
	cfg := DefaultListValidatorConfig()
	if cfg.MaxListSize != MAX_TRANSACTIONS_PER_INCLUSION_LIST {
		t.Errorf("MaxListSize = %d, want %d", cfg.MaxListSize, MAX_TRANSACTIONS_PER_INCLUSION_LIST)
	}
	if cfg.MinInclusionRate != 0.75 {
		t.Errorf("MinInclusionRate = %f, want 0.75", cfg.MinInclusionRate)
	}
	if cfg.MaxGasPerItem != MAX_GAS_PER_INCLUSION_LIST {
		t.Errorf("MaxGasPerItem = %d, want %d", cfg.MaxGasPerItem, MAX_GAS_PER_INCLUSION_LIST)
	}
}

// --- NewInclusionListValidator ---

func TestNewInclusionListValidator(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	if v == nil {
		t.Fatal("NewInclusionListValidator returned nil")
	}
}

func TestNewInclusionListValidatorDefaults(t *testing.T) {
	v := NewInclusionListValidator(ListValidatorConfig{})
	cfg := v.Config()
	if cfg.MaxListSize != MAX_TRANSACTIONS_PER_INCLUSION_LIST {
		t.Errorf("MaxListSize defaulted to %d, want %d", cfg.MaxListSize, MAX_TRANSACTIONS_PER_INCLUSION_LIST)
	}
	if cfg.MinInclusionRate != 0.75 {
		t.Errorf("MinInclusionRate defaulted to %f, want 0.75", cfg.MinInclusionRate)
	}
	if cfg.MaxGasPerItem != MAX_GAS_PER_INCLUSION_LIST {
		t.Errorf("MaxGasPerItem defaulted to %d", cfg.MaxGasPerItem)
	}
}

func TestNewInclusionListValidatorInvalidRate(t *testing.T) {
	v := NewInclusionListValidator(ListValidatorConfig{MinInclusionRate: 1.5})
	cfg := v.Config()
	if cfg.MinInclusionRate != 0.75 {
		t.Errorf("out-of-range rate should default to 0.75, got %f", cfg.MinInclusionRate)
	}
}

// --- ValidateList ---

func TestValidateListValid(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := makeIL(100, 5, 3, 21000)
	state := newMockHeadState(100, 5)

	if err := v.ValidateList(il, state); err != nil {
		t.Errorf("valid list: %v", err)
	}
}

func TestValidateListValidPreviousSlot(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := makeIL(99, 5, 3, 21000)
	state := newMockHeadState(100, 5)

	// Per EIP-7805 the IL slot matches the current or previous slot: a
	// list for head-1 is accepted, head+1 is not.
	if err := v.ValidateList(il, state); err != nil {
		t.Errorf("valid previous-slot list: %v", err)
	}
	next := makeIL(101, 5, 3, 21000)
	if err := v.ValidateList(next, state); err == nil {
		t.Error("next-slot list must be rejected")
	}
}

func TestValidateListNilList(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	err := v.ValidateList(nil, nil)
	if !errors.Is(err, ErrValidatorNilList) {
		t.Errorf("expected ErrValidatorNilList, got %v", err)
	}
}

func TestValidateListEmptyEntries(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := &InclusionList{Slot: 100, Entries: []InclusionListEntry{}}
	err := v.ValidateList(il, nil)
	if !errors.Is(err, ErrValidatorEmptyList) {
		t.Errorf("expected ErrValidatorEmptyList, got %v", err)
	}
}

func TestValidateListTooLarge(t *testing.T) {
	v := NewInclusionListValidator(ListValidatorConfig{MaxListSize: 3})
	il := makeIL(100, 0, 5, 21000)
	err := v.ValidateList(il, nil)
	if !errors.Is(err, ErrValidatorListTooLarge) {
		t.Errorf("expected ErrValidatorListTooLarge, got %v", err)
	}
}

func TestValidateListZeroSlot(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := makeIL(0, 0, 1, 21000)
	il.Slot = 0
	err := v.ValidateList(il, nil)
	if !errors.Is(err, ErrValidatorZeroSlot) {
		t.Errorf("expected ErrValidatorZeroSlot, got %v", err)
	}
}

func TestValidateListDuplicateTx(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	_, data := makeValidatorTx(0, 21000)
	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: data, Index: 0},
			{Transaction: data, Index: 1}, // same tx
		},
	}
	err := v.ValidateList(il, nil)
	if !errors.Is(err, ErrValidatorDuplicateTx) {
		t.Errorf("expected ErrValidatorDuplicateTx, got %v", err)
	}
}

func TestValidateListInvalidTx(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: []byte{0xff, 0xfe, 0xfd}, Index: 0},
		},
	}
	err := v.ValidateList(il, nil)
	if !errors.Is(err, ErrValidatorInvalidTx) {
		t.Errorf("expected ErrValidatorInvalidTx, got %v", err)
	}
}

func TestValidateListEmptyTxEntry(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: []byte{}}},
	}
	err := v.ValidateList(il, nil)
	if !errors.Is(err, ErrValidatorInvalidTx) {
		t.Errorf("expected ErrValidatorInvalidTx, got %v", err)
	}
}

func TestValidateListGasExceeded(t *testing.T) {
	v := NewInclusionListValidator(ListValidatorConfig{MaxGasPerItem: 10000})
	il := makeIL(100, 0, 1, 21000) // gas 21000 > 10000
	err := v.ValidateList(il, nil)
	if !errors.Is(err, ErrValidatorGasExceeded) {
		t.Errorf("expected ErrValidatorGasExceeded, got %v", err)
	}
}

func TestValidateListSlotMismatch(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := makeIL(200, 5, 1, 21000)
	state := newMockHeadState(100, 5) // head is at 100, list targets 200
	err := v.ValidateList(il, state)
	if !errors.Is(err, ErrValidatorSlotMismatch) {
		t.Errorf("expected ErrValidatorSlotMismatch, got %v", err)
	}
}

func TestValidateListUnauthorized(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := makeIL(100, 99, 1, 21000)   // proposer 99 not authorized
	state := newMockHeadState(100, 5) // only proposer 5 authorized
	err := v.ValidateList(il, state)
	if !errors.Is(err, ErrValidatorUnauthorized) {
		t.Errorf("expected ErrValidatorUnauthorized, got %v", err)
	}
}

func TestValidateListNoHeadState(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := makeIL(100, 0, 3, 21000)
	// nil headState skips slot/proposer checks.
	if err := v.ValidateList(il, nil); err != nil {
		t.Errorf("nil headState should skip context checks: %v", err)
	}
}

// --- ValidateInclusion ---

func TestValidateInclusionAllIncluded(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())

	tx1, tx1Bytes := makeValidatorTx(0, 21000)
	tx2, tx2Bytes := makeValidatorTx(1, 21000)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes},
			{Transaction: tx2Bytes},
		},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1, tx2}}
	block := types.NewBlock(header, body)

	result, err := v.ValidateInclusion(block, il)
	if err != nil {
		t.Fatalf("ValidateInclusion: %v", err)
	}
	if !result.Satisfied {
		t.Error("all txs included, should be satisfied")
	}
	if result.Rate != 1.0 {
		t.Errorf("Rate = %f, want 1.0", result.Rate)
	}
	if len(result.Missing) != 0 {
		t.Errorf("Missing = %d, want 0", len(result.Missing))
	}
	if result.IncludedCount != 2 {
		t.Errorf("IncludedCount = %d, want 2", result.IncludedCount)
	}
}

func TestValidateInclusionPartial(t *testing.T) {
	v := NewInclusionListValidator(ListValidatorConfig{MinInclusionRate: 0.5})

	tx1, tx1Bytes := makeValidatorTx(0, 21000)
	_, tx2Bytes := makeValidatorTx(1, 21000)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes},
			{Transaction: tx2Bytes},
		},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1}} // only tx1
	block := types.NewBlock(header, body)

	result, err := v.ValidateInclusion(block, il)
	if err != nil {
		t.Fatalf("ValidateInclusion: %v", err)
	}
	if !result.Satisfied {
		t.Error("50% included meets 50% threshold, should be satisfied")
	}
	if result.Rate != 0.5 {
		t.Errorf("Rate = %f, want 0.5", result.Rate)
	}
	if len(result.Missing) != 1 {
		t.Errorf("Missing = %d, want 1", len(result.Missing))
	}
}

func TestValidateInclusionBelowThreshold(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig()) // 75%

	tx1, tx1Bytes := makeValidatorTx(0, 21000)
	_, tx2Bytes := makeValidatorTx(1, 21000)
	_, tx3Bytes := makeValidatorTx(2, 21000)
	_, tx4Bytes := makeValidatorTx(3, 21000)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes},
			{Transaction: tx2Bytes},
			{Transaction: tx3Bytes},
			{Transaction: tx4Bytes},
		},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1}} // 1/4 = 25%
	block := types.NewBlock(header, body)

	result, err := v.ValidateInclusion(block, il)
	if err != nil {
		t.Fatalf("ValidateInclusion: %v", err)
	}
	if result.Satisfied {
		t.Error("25% below 75% threshold, should not be satisfied")
	}
	if result.Rate != 0.25 {
		t.Errorf("Rate = %f, want 0.25", result.Rate)
	}
	if len(result.Missing) != 3 {
		t.Errorf("Missing = %d, want 3", len(result.Missing))
	}
}

func TestValidateInclusionNilBlock(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := makeIL(100, 0, 1, 21000)
	_, err := v.ValidateInclusion(nil, il)
	if !errors.Is(err, ErrValidatorNilBlock) {
		t.Errorf("expected ErrValidatorNilBlock, got %v", err)
	}
}

func TestValidateInclusionNilList(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)
	_, err := v.ValidateInclusion(block, nil)
	if !errors.Is(err, ErrValidatorNilList) {
		t.Errorf("expected ErrValidatorNilList, got %v", err)
	}
}

func TestValidateInclusionEmptyIL(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := &InclusionList{Slot: 100, Entries: []InclusionListEntry{}}
	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)

	result, err := v.ValidateInclusion(block, il)
	if err != nil {
		t.Fatalf("ValidateInclusion: %v", err)
	}
	// Vacuously satisfied with no entries.
	if !result.Satisfied {
		t.Error("empty IL should be vacuously satisfied")
	}
	if result.Rate != 1.0 {
		t.Errorf("Rate = %f, want 1.0 for empty IL", result.Rate)
	}
}

// --- ScoreInclusion ---

func TestScoreInclusionFull(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())

	tx1, tx1Bytes := makeValidatorTx(0, 21000)
	tx2, tx2Bytes := makeValidatorTx(1, 21000)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes},
			{Transaction: tx2Bytes},
		},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1, tx2}}
	block := types.NewBlock(header, body)

	score := v.ScoreInclusion(block, il)
	if score != 1.0 {
		t.Errorf("score = %f, want 1.0", score)
	}
}

func TestScoreInclusionPartial(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())

	tx1, tx1Bytes := makeValidatorTx(0, 21000)
	_, tx2Bytes := makeValidatorTx(1, 21000)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes},
			{Transaction: tx2Bytes},
		},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1}}
	block := types.NewBlock(header, body)

	score := v.ScoreInclusion(block, il)
	if score != 0.5 {
		t.Errorf("score = %f, want 0.5", score)
	}
}

func TestScoreInclusionNone(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())

	_, tx1Bytes := makeValidatorTx(0, 21000)
	il := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: tx1Bytes}},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)

	score := v.ScoreInclusion(block, il)
	if score != 0.0 {
		t.Errorf("score = %f, want 0.0", score)
	}
}

func TestScoreInclusionNilBlock(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := makeIL(100, 0, 1, 21000)
	score := v.ScoreInclusion(nil, il)
	if score != 0.0 {
		t.Errorf("score(nil block) = %f, want 0.0", score)
	}
}

func TestScoreInclusionNilList(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	block := types.NewBlock(header, nil)
	score := v.ScoreInclusion(block, nil)
	if score != 0.0 {
		t.Errorf("score(nil list) = %f, want 0.0", score)
	}
}

// --- Concurrency ---

func TestInclusionListValidatorConcurrentValidate(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())
	il := makeIL(100, 5, 3, 21000)
	state := newMockHeadState(100, 5)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := v.ValidateList(il, state); err != nil {
				t.Errorf("concurrent ValidateList: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestInclusionListValidatorConcurrentScore(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())

	tx1, tx1Bytes := makeValidatorTx(0, 21000)
	il := &InclusionList{
		Slot:    100,
		Entries: []InclusionListEntry{{Transaction: tx1Bytes}},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1}}
	block := types.NewBlock(header, body)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			score := v.ScoreInclusion(block, il)
			if score != 1.0 {
				t.Errorf("concurrent score = %f, want 1.0", score)
			}
		}()
	}
	wg.Wait()
}

func TestInclusionListValidatorConcurrentValidateInclusion(t *testing.T) {
	v := NewInclusionListValidator(DefaultListValidatorConfig())

	tx1, tx1Bytes := makeValidatorTx(0, 21000)
	tx2, tx2Bytes := makeValidatorTx(1, 21000)

	il := &InclusionList{
		Slot: 100,
		Entries: []InclusionListEntry{
			{Transaction: tx1Bytes},
			{Transaction: tx2Bytes},
		},
	}

	header := &types.Header{Number: big.NewInt(1), GasLimit: 1_000_000}
	body := &types.Body{Transactions: []*types.Transaction{tx1, tx2}}
	block := types.NewBlock(header, body)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := v.ValidateInclusion(block, il)
			if err != nil {
				t.Errorf("concurrent ValidateInclusion: %v", err)
				return
			}
			if !result.Satisfied {
				t.Error("concurrent: should be satisfied")
			}
		}()
	}
	wg.Wait()
}
