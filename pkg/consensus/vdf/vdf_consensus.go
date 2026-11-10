// Package consensus implements Ethereum consensus-layer primitives.
//
// vdf_consensus.go wires VDF beacon randomness into the consensus layer,
// providing epoch-level unbiasable randomness via VDF computation and
// multi-validator reveal. Part of the K+/M+ roadmap: "VDF, secure prequorum".
package vdf

import (
	"errors"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/crypto"
	cryptovdf "github.com/eth2030/eth2030/crypto/vdf"
)

// Errors for VDF consensus operations.
var (
	ErrVDFEpochNotStarted       = errors.New("vdf-consensus: epoch not started")
	ErrVDFEpochAlreadyFinalized = errors.New("vdf-consensus: epoch already finalized")
	ErrVDFInsufficientReveals   = errors.New("vdf-consensus: insufficient reveals for finalization")
	ErrVDFInvalidOutput         = errors.New("vdf-consensus: invalid VDF output")
	errVDFEpochAlreadyStarted   = errors.New("vdf-consensus: epoch already started")
	errVDFDuplicateReveal       = errors.New("vdf-consensus: validator already revealed")
	errVDFEmptySeed             = errors.New("vdf-consensus: empty seed")
	errVDFEmptyValidatorID      = errors.New("vdf-consensus: empty validator ID")
	errVDFEmptyOutput           = errors.New("vdf-consensus: empty output")
	errVDFZeroEpoch             = errors.New("vdf-consensus: zero epoch number")
)

// VDFConsensusConfig holds parameters for VDF-based consensus randomness.
type VDFConsensusConfig struct {
	VDFDifficulty    uint64  // number of VDF iterations per epoch
	EpochLength      uint64  // number of slots per epoch
	RevealWindow     uint64  // number of slots in the reveal window
	MinParticipation float64 // minimum fraction of validators that must reveal (0.0-1.0)
}

// DefaultVDFConsensusConfig returns a sensible default configuration.
func DefaultVDFConsensusConfig() VDFConsensusConfig {
	return VDFConsensusConfig{
		VDFDifficulty:    10,
		EpochLength:      32,
		RevealWindow:     8,
		MinParticipation: 0.5,
	}
}

// EpochRandomness holds the VDF randomness output for one epoch.
type EpochRandomness struct {
	EpochNum      uint64 // epoch number
	VDFOutput     []byte // finalized VDF output (keccak of combined reveals)
	Seed          []byte // seed used for VDF computation
	RevealedCount int    // number of validators who revealed
	Finalized     bool   // whether this epoch's randomness is finalized
}

// epochState tracks internal mutable state for an in-progress epoch.
type epochState struct {
	seed       []byte
	reveals    map[string][]byte // validatorID -> VDF output
	finalized  bool
	randomness *EpochRandomness
}

// VDFConsensus integrates VDF beacon randomness into the consensus layer.
// It manages epoch lifecycle (begin, reveal, finalize) and caches finalized
// randomness. All methods are safe for concurrent use.
type VDFConsensus struct {
	mu           sync.RWMutex
	config       VDFConsensusConfig
	epochs       map[uint64]*epochState
	currentEpoch uint64
}

// NewVDFConsensus creates a new VDF consensus engine with the given config.
func NewVDFConsensus(config VDFConsensusConfig) *VDFConsensus {
	if config.VDFDifficulty == 0 {
		config.VDFDifficulty = 10
	}
	if config.EpochLength == 0 {
		config.EpochLength = 32
	}
	if config.RevealWindow == 0 {
		config.RevealWindow = 8
	}
	if config.MinParticipation <= 0 || config.MinParticipation > 1.0 {
		config.MinParticipation = 0.5
	}

	return &VDFConsensus{
		config: config,
		epochs: make(map[uint64]*epochState),
	}
}

// BeginEpoch starts VDF computation for the given epoch with the provided seed.
// The seed is typically derived from the RANDAO mix of the previous epoch.
func (vc *VDFConsensus) BeginEpoch(epochNum uint64, seed []byte) error {
	if epochNum == 0 {
		return errVDFZeroEpoch
	}
	if len(seed) == 0 {
		return errVDFEmptySeed
	}

	vc.mu.Lock()
	defer vc.mu.Unlock()

	if _, exists := vc.epochs[epochNum]; exists {
		return errVDFEpochAlreadyStarted
	}

	seedCopy := make([]byte, len(seed))
	copy(seedCopy, seed)

	vc.epochs[epochNum] = &epochState{
		seed:    seedCopy,
		reveals: make(map[string][]byte),
	}

	if epochNum > vc.currentEpoch {
		vc.currentEpoch = epochNum
	}

	return nil
}

// RevealOutput records a validator's VDF output for the given epoch. The output
// is verified against the epoch seed before being accepted.
func (vc *VDFConsensus) RevealOutput(epochNum uint64, validatorID string, solution []byte) error {
	if epochNum == 0 {
		return errVDFZeroEpoch
	}
	if validatorID == "" {
		return errVDFEmptyValidatorID
	}
	if len(solution) == 0 {
		return errVDFEmptyOutput
	}

	vc.mu.Lock()
	defer vc.mu.Unlock()

	es, exists := vc.epochs[epochNum]
	if !exists {
		return ErrVDFEpochNotStarted
	}
	if es.finalized {
		return ErrVDFEpochAlreadyFinalized
	}
	if _, dup := es.reveals[validatorID]; dup {
		return errVDFDuplicateReveal
	}

	// Verify the VDF output against the epoch seed.
	// Build the expected input by domain-separating with the epoch.
	domainInput := crypto.Keccak256(es.seed, epochUint64Bytes(epochNum))

	output, valid := cryptovdf.VerifySolution(domainInput, vc.config.VDFDifficulty, solution)
	if !valid {
		return ErrVDFInvalidOutput
	}
	es.reveals[validatorID] = output

	return nil
}

// FinalizeEpoch finalizes the epoch's randomness if enough validators have
// revealed their VDF outputs. The finalized randomness is the keccak256 hash
// of all reveals sorted by validator ID.
func (vc *VDFConsensus) FinalizeEpoch(epochNum uint64) (*EpochRandomness, error) {
	if epochNum == 0 {
		return nil, errVDFZeroEpoch
	}

	vc.mu.Lock()
	defer vc.mu.Unlock()

	es, exists := vc.epochs[epochNum]
	if !exists {
		return nil, ErrVDFEpochNotStarted
	}
	if es.finalized {
		return nil, ErrVDFEpochAlreadyFinalized
	}

	// Check minimum participation. We use reveal count vs a target count
	// derived from the reveal window (as a proxy for expected validators).
	revealCount := len(es.reveals)
	if revealCount == 0 {
		return nil, ErrVDFInsufficientReveals
	}

	// If min participation > 0 and we have at least one reveal window slot
	// that expects a reveal, check the threshold.
	minReveals := int(vc.config.MinParticipation * float64(vc.config.RevealWindow))
	if minReveals < 1 {
		minReveals = 1
	}
	if revealCount < minReveals {
		return nil, ErrVDFInsufficientReveals
	}

	// Combine all reveals in deterministic order (sorted by validator ID).
	ids := make([]string, 0, len(es.reveals))
	for id := range es.reveals {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	combined := make([]byte, 0, len(ids)*32)
	for _, id := range ids {
		combined = append(combined, es.reveals[id]...)
	}

	randomness := crypto.Keccak256(combined)

	er := &EpochRandomness{
		EpochNum:      epochNum,
		VDFOutput:     randomness,
		Seed:          make([]byte, len(es.seed)),
		RevealedCount: revealCount,
		Finalized:     true,
	}
	copy(er.Seed, es.seed)

	es.finalized = true
	es.randomness = er

	return er, nil
}

// GetRandomness returns the finalized randomness for an epoch. Returns an error
// if the epoch has not been started or has not yet been finalized.
func (vc *VDFConsensus) GetRandomness(epochNum uint64) ([]byte, error) {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	es, exists := vc.epochs[epochNum]
	if !exists {
		return nil, ErrVDFEpochNotStarted
	}
	if !es.finalized || es.randomness == nil {
		return nil, ErrVDFEpochNotStarted
	}

	result := make([]byte, len(es.randomness.VDFOutput))
	copy(result, es.randomness.VDFOutput)
	return result, nil
}

// CurrentEpoch returns the highest epoch number that has been started.
func (vc *VDFConsensus) CurrentEpoch() uint64 {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return vc.currentEpoch
}

// IsEpochFinalized returns true if the given epoch's randomness has been finalized.
func (vc *VDFConsensus) IsEpochFinalized(epochNum uint64) bool {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	es, exists := vc.epochs[epochNum]
	if !exists {
		return false
	}
	return es.finalized
}

// RevealCount returns the number of reveals received for the given epoch.
func (vc *VDFConsensus) RevealCount(epochNum uint64) int {
	vc.mu.RLock()
	defer vc.mu.RUnlock()

	es, exists := vc.epochs[epochNum]
	if !exists {
		return 0
	}
	return len(es.reveals)
}

// Config returns the current VDF consensus configuration.
func (vc *VDFConsensus) Config() VDFConsensusConfig {
	vc.mu.RLock()
	defer vc.mu.RUnlock()
	return vc.config
}

// epochUint64Bytes converts a uint64 to big-endian bytes.
func epochUint64Bytes(n uint64) []byte {
	b := make([]byte, 8)
	b[0] = byte(n >> 56)
	b[1] = byte(n >> 48)
	b[2] = byte(n >> 40)
	b[3] = byte(n >> 32)
	b[4] = byte(n >> 24)
	b[5] = byte(n >> 16)
	b[6] = byte(n >> 8)
	b[7] = byte(n)
	return b
}

// sliceEqual returns true if two byte slices are identical.
func sliceEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
