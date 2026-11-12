package focil

import (
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Validator-level errors for inclusion list validation.
var (
	ErrValidatorNilList      = errors.New("focil-validator: inclusion list is nil")
	ErrValidatorEmptyList    = errors.New("focil-validator: inclusion list has no entries")
	ErrValidatorListTooLarge = errors.New("focil-validator: inclusion list exceeds max size")
	ErrValidatorDuplicateTx  = errors.New("focil-validator: duplicate transaction hash in list")
	ErrValidatorInvalidTx    = errors.New("focil-validator: invalid transaction encoding")
	ErrValidatorGasExceeded  = errors.New("focil-validator: transaction gas exceeds per-item limit")
	ErrValidatorTotalGas     = errors.New("focil-validator: total gas exceeds list limit")
	ErrValidatorSlotMismatch = errors.New("focil-validator: list slot does not match head slot")
	ErrValidatorZeroSlot     = errors.New("focil-validator: slot must be > 0")
	ErrValidatorUnauthorized = errors.New("focil-validator: proposer not authorized")
	ErrValidatorNilBlock     = errors.New("focil-validator: block is nil")
	ErrValidatorBelowMinRate = errors.New("focil-validator: block below minimum inclusion rate")
)

// ListValidatorConfig configures the InclusionListValidator.
type ListValidatorConfig struct {
	// MaxListSize is the maximum number of entries in an inclusion list.
	// Default: MAX_TRANSACTIONS_PER_INCLUSION_LIST (16).
	MaxListSize int

	// MinInclusionRate is the minimum fraction of IL transactions that a
	// block must include, expressed as a value between 0.0 and 1.0.
	// Default: 0.75 (75%).
	MinInclusionRate float64

	// MaxGasPerItem is the maximum gas allowed for a single IL entry.
	// Default: MAX_GAS_PER_INCLUSION_LIST (entire list budget, no per-item cap).
	MaxGasPerItem uint64
}

// DefaultListValidatorConfig returns production defaults.
func DefaultListValidatorConfig() ListValidatorConfig {
	return ListValidatorConfig{
		MaxListSize:      MAX_TRANSACTIONS_PER_INCLUSION_LIST,
		MinInclusionRate: 0.75,
		MaxGasPerItem:    MAX_GAS_PER_INCLUSION_LIST,
	}
}

// HeadState provides the minimal state interface for IL validation.
// Implementations should return the current slot and whether a proposer
// index is authorized as an IL committee member.
type HeadState interface {
	// Slot returns the current head slot.
	Slot() uint64

	// IsILCommitteeMember returns true if the proposer index is an
	// authorized inclusion list committee member for the given slot.
	IsILCommitteeMember(proposerIndex uint64, slot uint64) bool
}

// InclusionListValidator validates inclusion lists and checks block
// compliance with configurable thresholds.
//
// Thread-safe: all public methods are safe for concurrent use.
type InclusionListValidator struct {
	mu     sync.RWMutex
	config ListValidatorConfig
}

// NewInclusionListValidator creates a new validator with the given config.
func NewInclusionListValidator(config ListValidatorConfig) *InclusionListValidator {
	if config.MaxListSize <= 0 {
		config.MaxListSize = MAX_TRANSACTIONS_PER_INCLUSION_LIST
	}
	if config.MinInclusionRate <= 0 || config.MinInclusionRate > 1.0 {
		config.MinInclusionRate = 0.75
	}
	if config.MaxGasPerItem == 0 {
		config.MaxGasPerItem = MAX_GAS_PER_INCLUSION_LIST
	}
	return &InclusionListValidator{config: config}
}

// Config returns a copy of the current configuration.
func (v *InclusionListValidator) Config() ListValidatorConfig {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.config
}

// ValidateList checks an inclusion list for structural and contextual
// correctness against the current head state.
//
// Checks performed:
//  1. List is not nil
//  2. List has at least one entry
//  3. List does not exceed MaxListSize
//  4. No duplicate transaction hashes
//  5. Each transaction decodes correctly (valid RLP format)
//  6. Each transaction's gas is within MaxGasPerItem
//  7. Slot is > 0 and matches head state (if headState is non-nil)
//  8. Proposer is authorized (if headState is non-nil)
func (v *InclusionListValidator) ValidateList(list *InclusionList, headState HeadState) error {
	v.mu.RLock()
	cfg := v.config
	v.mu.RUnlock()

	if list == nil {
		return ErrValidatorNilList
	}
	if len(list.Entries) == 0 {
		return ErrValidatorEmptyList
	}
	if len(list.Entries) > cfg.MaxListSize {
		return fmt.Errorf("%w: got %d, max %d",
			ErrValidatorListTooLarge, len(list.Entries), cfg.MaxListSize)
	}
	if list.Slot == 0 {
		return ErrValidatorZeroSlot
	}
	if list.TotalBytes() > MAX_BYTES_PER_INCLUSION_LIST {
		return fmt.Errorf("%w: %d bytes, max %d",
			ErrValidatorListTooLarge, list.TotalBytes(), MAX_BYTES_PER_INCLUSION_LIST)
	}

	// Validate head state context if provided. Per EIP-7805 the IL slot
	// must match the current or previous slot.
	if headState != nil {
		headSlot := headState.Slot()
		if list.Slot != headSlot && list.Slot+1 != headSlot {
			return fmt.Errorf("%w: list slot %d, head slot %d",
				ErrValidatorSlotMismatch, list.Slot, headSlot)
		}
		if !headState.IsILCommitteeMember(list.ProposerIndex, list.Slot) {
			return fmt.Errorf("%w: proposer %d at slot %d",
				ErrValidatorUnauthorized, list.ProposerIndex, list.Slot)
		}
	}

	// Track seen tx hashes for duplicate detection.
	seen := make(map[types.Hash]struct{}, len(list.Entries))
	var totalGas uint64

	for i, entry := range list.Entries {
		if len(entry.Transaction) == 0 {
			return fmt.Errorf("%w: entry %d is empty", ErrValidatorInvalidTx, i)
		}

		tx, err := types.DecodeTxRLP(entry.Transaction)
		if err != nil {
			return fmt.Errorf("%w: entry %d: %v", ErrValidatorInvalidTx, i, err)
		}

		txHash := tx.Hash()
		if _, dup := seen[txHash]; dup {
			return fmt.Errorf("%w: %s at entry %d",
				ErrValidatorDuplicateTx, txHash.Hex(), i)
		}
		seen[txHash] = struct{}{}

		gas := tx.Gas()
		if gas > cfg.MaxGasPerItem {
			return fmt.Errorf("%w: entry %d uses %d gas, max %d",
				ErrValidatorGasExceeded, i, gas, cfg.MaxGasPerItem)
		}

		totalGas += gas
	}

	if totalGas > MAX_GAS_PER_INCLUSION_LIST {
		return fmt.Errorf("%w: total %d, max %d",
			ErrValidatorTotalGas, totalGas, MAX_GAS_PER_INCLUSION_LIST)
	}

	return nil
}

// InclusionResult holds the result of checking a block against an IL.
type InclusionResult struct {
	// Satisfied is true if the block meets the minimum inclusion rate.
	Satisfied bool

	// Missing contains the hashes of IL transactions not found in the block.
	Missing []types.Hash

	// IncludedCount is the number of IL transactions found in the block.
	IncludedCount int

	// TotalCount is the total number of valid transactions in the IL.
	TotalCount int

	// Rate is the fraction of IL transactions included (0.0 to 1.0).
	Rate float64
}

// ValidateInclusion checks whether a block includes enough transactions
// from the inclusion list to meet the minimum inclusion rate.
//
// Returns the inclusion result with the satisfied flag, missing hashes,
// and the inclusion rate. A block with no valid IL transactions is
// considered vacuously satisfied.
func (v *InclusionListValidator) ValidateInclusion(
	block *types.Block,
	list *InclusionList,
) (*InclusionResult, error) {
	v.mu.RLock()
	cfg := v.config
	v.mu.RUnlock()

	if block == nil {
		return nil, ErrValidatorNilBlock
	}
	if list == nil {
		return nil, ErrValidatorNilList
	}

	// Build set of block transaction hashes.
	blockTxHashes := make(map[types.Hash]struct{}, len(block.Transactions()))
	for _, tx := range block.Transactions() {
		blockTxHashes[tx.Hash()] = struct{}{}
	}

	// Decode IL entries and check presence in block.
	var missing []types.Hash
	included := 0
	total := 0

	for _, entry := range list.Entries {
		tx, err := types.DecodeTxRLP(entry.Transaction)
		if err != nil {
			// Skip invalid entries per spec.
			continue
		}
		total++
		txHash := tx.Hash()
		if _, found := blockTxHashes[txHash]; found {
			included++
		} else {
			missing = append(missing, txHash)
		}
	}

	// Vacuously satisfied if no valid entries.
	if total == 0 {
		return &InclusionResult{
			Satisfied:     true,
			IncludedCount: 0,
			TotalCount:    0,
			Rate:          1.0,
		}, nil
	}

	rate := float64(included) / float64(total)
	satisfied := rate >= cfg.MinInclusionRate

	return &InclusionResult{
		Satisfied:     satisfied,
		Missing:       missing,
		IncludedCount: included,
		TotalCount:    total,
		Rate:          rate,
	}, nil
}

// ScoreInclusion rates how well a block satisfies an inclusion list on a
// scale of 0.0 to 1.0. A score of 1.0 means all valid IL transactions
// are included. Returns 0.0 if the block or list is nil.
func (v *InclusionListValidator) ScoreInclusion(
	block *types.Block,
	list *InclusionList,
) float64 {
	if block == nil || list == nil {
		return 0.0
	}

	blockTxHashes := make(map[types.Hash]struct{}, len(block.Transactions()))
	for _, tx := range block.Transactions() {
		blockTxHashes[tx.Hash()] = struct{}{}
	}

	total := 0
	included := 0

	for _, entry := range list.Entries {
		tx, err := types.DecodeTxRLP(entry.Transaction)
		if err != nil {
			continue
		}
		total++
		if _, found := blockTxHashes[tx.Hash()]; found {
			included++
		}
	}

	if total == 0 {
		return 1.0
	}
	return float64(included) / float64(total)
}
