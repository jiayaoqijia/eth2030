package proofs

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Mandatory proof system errors.
var (
	ErrMandatoryNilSubmission    = errors.New("mandatory: nil proof submission")
	ErrMandatoryEmptyProofData   = errors.New("mandatory: empty proof data")
	ErrMandatoryEmptyProofType   = errors.New("mandatory: empty proof type")
	ErrMandatoryZeroBlockHash    = errors.New("mandatory: zero block hash")
	ErrMandatoryZeroProverID     = errors.New("mandatory: zero prover ID")
	ErrMandatoryProverExists     = errors.New("mandatory: prover already registered")
	ErrMandatoryProverNotFound   = errors.New("mandatory: prover not registered")
	ErrMandatoryNoProvers        = errors.New("mandatory: no registered provers")
	ErrMandatoryNotEnoughProvers = errors.New("mandatory: not enough registered provers")
	ErrMandatoryNoProofTypes     = errors.New("mandatory: prover has no proof types")
	ErrMandatoryBlockNotAssigned = errors.New("mandatory: block has no assigned provers")
)

// MandatoryProofConfig configures the mandatory proof system parameters.
type MandatoryProofConfig struct {
	// RequiredProofs is the minimum number of valid proofs needed (default 3).
	RequiredProofs int
	// TotalProvers is the number of provers assigned per block (default 5).
	TotalProvers int
	// ProofDeadlineSlots is the number of slots allowed for proof submission.
	ProofDeadlineSlots uint64
	// PenaltyAmount is the base penalty (in Gwei) for late or missing proofs.
	PenaltyAmount uint64
}

// DefaultMandatoryProofConfig returns configuration with default values.
func DefaultMandatoryProofConfig() MandatoryProofConfig {
	return MandatoryProofConfig{
		RequiredProofs:     3,
		TotalProvers:       5,
		ProofDeadlineSlots: 32,
		PenaltyAmount:      1000,
	}
}

// ProofSubmission represents a proof submitted by a prover for a block.
type ProofSubmission struct {
	ProverID  types.Hash
	ProofType string
	ProofData []byte
	BlockHash types.Hash
	Timestamp uint64
}

// ProofRequirementStatus describes the proof requirement state for a block.
type ProofRequirementStatus struct {
	BlockHash   types.Hash
	Required    int
	Submitted   int
	Verified    int
	IsSatisfied bool
	ProverIDs   []types.Hash
}

// proverInfo holds registration data for a prover.
type proverInfo struct {
	ID         types.Hash
	ProofTypes []string
}

// blockProofState tracks proof submissions for a single block.
type blockProofState struct {
	assignedProvers []types.Hash
	submissions     []*ProofSubmission
	verified        map[types.Hash]bool // proverID -> verified
	deadline        uint64
}

// MandatoryProofSystem enforces the mandatory 3-of-5 proof requirement.
// Thread-safe.
type MandatoryProofSystem struct {
	mu        sync.RWMutex
	config    MandatoryProofConfig
	provers   map[types.Hash]*proverInfo      // registered provers
	blocks    map[types.Hash]*blockProofState // per-block proof state
	verifiers map[string]func(*ProofSubmission) bool
}

// NewMandatoryProofSystem creates a new mandatory proof system.
func NewMandatoryProofSystem(config MandatoryProofConfig) *MandatoryProofSystem {
	if config.RequiredProofs <= 0 {
		config.RequiredProofs = 3
	}
	if config.TotalProvers <= 0 {
		config.TotalProvers = 5
	}
	if config.RequiredProofs > config.TotalProvers {
		config.RequiredProofs = config.TotalProvers
	}
	if config.ProofDeadlineSlots == 0 {
		config.ProofDeadlineSlots = 32
	}
	return &MandatoryProofSystem{
		config:    config,
		provers:   make(map[types.Hash]*proverInfo),
		blocks:    make(map[types.Hash]*blockProofState),
		verifiers: make(map[string]func(*ProofSubmission) bool),
	}
}

// RegisterProver registers a prover with the given ID and supported proof types.
func (m *MandatoryProofSystem) RegisterProver(proverID types.Hash, proofTypes []string) error {
	if proverID == (types.Hash{}) {
		return ErrMandatoryZeroProverID
	}
	if len(proofTypes) == 0 {
		return ErrMandatoryNoProofTypes
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.provers[proverID]; exists {
		return ErrMandatoryProverExists
	}

	pts := make([]string, len(proofTypes))
	copy(pts, proofTypes)
	m.provers[proverID] = &proverInfo{
		ID:         proverID,
		ProofTypes: pts,
	}
	return nil
}

// AssignProvers deterministically assigns TotalProvers provers to a block.
// Selection is based on hashing the block hash with each prover's ID.
func (m *MandatoryProofSystem) AssignProvers(blockHash types.Hash) ([]types.Hash, error) {
	if blockHash == (types.Hash{}) {
		return nil, ErrMandatoryZeroBlockHash
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.provers) == 0 {
		return nil, ErrMandatoryNoProvers
	}
	if len(m.provers) < m.config.TotalProvers {
		return nil, fmt.Errorf("%w: have %d, need %d",
			ErrMandatoryNotEnoughProvers, len(m.provers), m.config.TotalProvers)
	}

	// Already assigned? Return existing assignment.
	if state, ok := m.blocks[blockHash]; ok {
		result := make([]types.Hash, len(state.assignedProvers))
		copy(result, state.assignedProvers)
		return result, nil
	}

	// Collect all prover IDs and score them deterministically.
	type scored struct {
		id    types.Hash
		score types.Hash
	}
	candidates := make([]scored, 0, len(m.provers))
	for id := range m.provers {
		// Score = Keccak256(blockHash || proverID).
		s := crypto.Keccak256Hash(blockHash[:], id[:])
		candidates = append(candidates, scored{id: id, score: s})
	}

	// Selection sort to pick top TotalProvers by score (lowest hash wins).
	for i := 0; i < m.config.TotalProvers && i < len(candidates); i++ {
		minIdx := i
		for j := i + 1; j < len(candidates); j++ {
			if hashLess(candidates[j].score, candidates[minIdx].score) {
				minIdx = j
			}
		}
		candidates[i], candidates[minIdx] = candidates[minIdx], candidates[i]
	}

	assigned := make([]types.Hash, m.config.TotalProvers)
	for i := 0; i < m.config.TotalProvers; i++ {
		assigned[i] = candidates[i].id
	}

	m.blocks[blockHash] = &blockProofState{
		assignedProvers: assigned,
		submissions:     make([]*ProofSubmission, 0),
		verified:        make(map[types.Hash]bool),
		deadline:        0, // set externally via GetProofDeadline
	}

	result := make([]types.Hash, len(assigned))
	copy(result, assigned)
	return result, nil
}

// hashLess returns true if a < b (lexicographic byte comparison).
func hashLess(a, b types.Hash) bool {
	for i := 0; i < types.HashLength; i++ {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

// SubmitProof submits a proof for a block. The prover must be assigned.
func (m *MandatoryProofSystem) SubmitProof(submission *ProofSubmission) error {
	if submission == nil {
		return ErrMandatoryNilSubmission
	}
	if submission.BlockHash == (types.Hash{}) {
		return ErrMandatoryZeroBlockHash
	}
	if submission.ProverID == (types.Hash{}) {
		return ErrMandatoryZeroProverID
	}
	if len(submission.ProofData) == 0 {
		return ErrMandatoryEmptyProofData
	}
	if submission.ProofType == "" {
		return ErrMandatoryEmptyProofType
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.blocks[submission.BlockHash]
	if !ok {
		return ErrMandatoryBlockNotAssigned
	}

	// Verify the prover is assigned to this block.
	assigned := false
	for _, id := range state.assignedProvers {
		if id == submission.ProverID {
			assigned = true
			break
		}
	}
	if !assigned {
		return fmt.Errorf("mandatory: prover %s not assigned to block %s",
			submission.ProverID.Hex(), submission.BlockHash.Hex())
	}

	// Store the submission.
	sub := &ProofSubmission{
		ProverID:  submission.ProverID,
		ProofType: submission.ProofType,
		BlockHash: submission.BlockHash,
		Timestamp: submission.Timestamp,
		ProofData: make([]byte, len(submission.ProofData)),
	}
	copy(sub.ProofData, submission.ProofData)
	state.submissions = append(state.submissions, sub)

	return nil
}

// RegisterVerifier installs the verifier for a proof type. The verifier
// supplies trusted circuit keys and the expected statement for the block.
func (m *MandatoryProofSystem) RegisterVerifier(proofType string, verify func(*ProofSubmission) bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if verify == nil {
		delete(m.verifiers, proofType)
		return
	}
	m.verifiers[proofType] = verify
}

// VerifyProof verifies the stored submission with the registered provider.
func (m *MandatoryProofSystem) VerifyProof(submission *ProofSubmission) bool {
	if submission == nil || len(submission.ProofData) == 0 {
		return false
	}
	m.mu.RLock()
	state := m.blocks[submission.BlockHash]
	provider := m.verifiers[submission.ProofType]
	if state == nil || provider == nil {
		m.mu.RUnlock()
		return false
	}
	idx := slices.IndexFunc(state.submissions, func(stored *ProofSubmission) bool {
		return stored.ProverID == submission.ProverID && stored.ProofType == submission.ProofType &&
			bytes.Equal(stored.ProofData, submission.ProofData)
	})
	if idx < 0 {
		m.mu.RUnlock()
		return false
	}
	stored := *state.submissions[idx]
	stored.ProofData = bytes.Clone(stored.ProofData)
	m.mu.RUnlock()

	blockHash, proverID := stored.BlockHash, stored.ProverID
	if !provider(&stored) {
		return false
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.blocks[blockHash] != state {
		return false
	}
	state.verified[proverID] = true
	return true
}

// CheckRequirement returns the current proof requirement status for a block.
func (m *MandatoryProofSystem) CheckRequirement(blockHash types.Hash) *ProofRequirementStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	status := &ProofRequirementStatus{
		BlockHash: blockHash,
		Required:  m.config.RequiredProofs,
	}

	state, ok := m.blocks[blockHash]
	if !ok {
		return status
	}

	status.Submitted = len(state.submissions)
	status.Verified = 0
	for _, v := range state.verified {
		if v {
			status.Verified++
		}
	}
	status.IsSatisfied = status.Verified >= m.config.RequiredProofs

	proverIDs := make([]types.Hash, len(state.assignedProvers))
	copy(proverIDs, state.assignedProvers)
	status.ProverIDs = proverIDs

	return status
}

// GetProofDeadline returns the deadline slot for proof submission for a block.
// The deadline is the block's implied slot plus ProofDeadlineSlots.
func (m *MandatoryProofSystem) GetProofDeadline(blockHash types.Hash) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, ok := m.blocks[blockHash]
	if !ok {
		return m.config.ProofDeadlineSlots
	}

	// Use the earliest submission timestamp as a proxy for the block slot,
	// or return default if no submissions yet.
	if len(state.submissions) > 0 {
		earliest := state.submissions[0].Timestamp
		for _, s := range state.submissions[1:] {
			if s.Timestamp < earliest {
				earliest = s.Timestamp
			}
		}
		return earliest + m.config.ProofDeadlineSlots
	}
	return m.config.ProofDeadlineSlots
}

// PenalizeLatePoof calculates the penalty for a late or missing proof.
// Returns the penalty amount in Gwei. Penalty increases if the prover was
// assigned but never submitted or verified.
func (m *MandatoryProofSystem) PenalizeLatePoof(proverID types.Hash, blockHash types.Hash) uint64 {
	m.mu.RLock()
	defer m.mu.RUnlock()

	state, ok := m.blocks[blockHash]
	if !ok {
		return 0
	}

	// Check if prover was assigned.
	assigned := false
	for _, id := range state.assignedProvers {
		if id == proverID {
			assigned = true
			break
		}
	}
	if !assigned {
		return 0
	}

	// Check if prover submitted and verified.
	submitted := false
	for _, s := range state.submissions {
		if s.ProverID == proverID {
			submitted = true
			break
		}
	}

	verified := state.verified[proverID]

	// No submission at all: full penalty.
	if !submitted {
		return m.config.PenaltyAmount
	}

	// Submitted but not verified: half penalty.
	if !verified {
		return m.config.PenaltyAmount / 2
	}

	// Verified: no penalty.
	return 0
}
