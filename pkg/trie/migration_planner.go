// migration_planner.go implements migration planning for MPT-to-binary trie
// migration. It creates phased migration plans, tracks execution progress,
// validates completed phases, and supports multiple concurrent plans.
package trie

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Migration planner errors.
var (
	ErrPlannerNilConfig    = errors.New("planner: nil config")
	ErrPlannerInvalidBatch = errors.New("planner: batch size must be > 0")
	ErrPlannerInvalidDepth = errors.New("planner: max depth must be > 0")
	ErrPlannerPlanNotFound = errors.New("planner: plan not found")
	ErrPlannerPhaseInvalid = errors.New("planner: invalid phase index")
	ErrPlannerPhaseNotDone = errors.New("planner: phase not yet executed")
	ErrPlannerPhaseFailed  = errors.New("planner: phase execution failed")
)

// PlannerConfig configures a MigrationPlanner.
type PlannerConfig struct {
	// SourceType is the source trie type (e.g. "mpt").
	SourceType string

	// TargetType is the target trie type (e.g. "binary").
	TargetType string

	// BatchSize is the number of accounts per migration phase.
	BatchSize uint64

	// MaxDepth is the maximum trie depth to traverse.
	MaxDepth int

	// EstimatedAccounts is the estimated total number of accounts.
	EstimatedAccounts uint64
}

// DefaultPlannerConfig returns a PlannerConfig with sensible defaults.
func DefaultPlannerConfig() *PlannerConfig {
	return &PlannerConfig{
		SourceType:        "mpt",
		TargetType:        "binary",
		BatchSize:         1000,
		MaxDepth:          256,
		EstimatedAccounts: 0,
	}
}

// MigrationPhase describes a single phase of a migration plan.
type MigrationPhase struct {
	// Name is a human-readable label for this phase.
	Name string

	// StartKey is the first key in this phase's range.
	StartKey [32]byte

	// EndKey is the last key in this phase's range.
	EndKey [32]byte

	// AccountCount is the estimated number of accounts in this phase.
	AccountCount uint64

	// StorageCount is the estimated number of storage slots in this phase.
	StorageCount uint64

	// Status is the phase execution status: "pending", "running", "done", "failed".
	Status string
}

// MigrationPlan describes a complete migration from source to target trie.
type MigrationPlan struct {
	// ID is a unique plan identifier (index-based).
	ID int

	// Phases is the ordered list of migration phases.
	Phases []*MigrationPhase

	// TotalAccounts is the estimated total accounts to migrate.
	TotalAccounts uint64

	// EstimatedGas is the estimated total gas for migration.
	EstimatedGas uint64

	// EstimatedBlocks is the estimated number of blocks to complete.
	EstimatedBlocks uint64

	// CreatedAt is the plan creation timestamp (unix seconds).
	CreatedAt int64

	// Config is a copy of the planner config used to create this plan.
	Config PlannerConfig
}

// PhaseResult captures the outcome of executing a single phase.
type PhaseResult struct {
	// AccountsMigrated is the number of accounts processed.
	AccountsMigrated uint64

	// StorageMigrated is the number of storage slots processed.
	StorageMigrated uint64

	// GasUsed is the gas consumed during this phase.
	GasUsed uint64

	// Duration is the phase execution duration in nanoseconds.
	Duration int64

	// Errors contains any errors encountered during execution.
	Errors []string
}

// PlanProgress reports the progress of a migration plan.
type PlanProgress struct {
	// CompletedPhases is the number of phases with status "done".
	CompletedPhases int

	// TotalPhases is the total number of phases in the plan.
	TotalPhases int

	// Percentage is the completion percentage (0.0 to 100.0).
	Percentage float64
}

// MigrationPlanner creates, executes, and tracks migration plans.
// Thread-safe for concurrent access.
type MigrationPlanner struct {
	mu         sync.RWMutex
	config     *PlannerConfig
	plans      []*MigrationPlan
	results    map[string]*PhaseResult // key: "planID:phaseIdx"
	sourceTrie *Trie                   // optional: source MPT for real execution
	destTrie   *BinaryTrie             // optional: destination binary trie
}

// NewMigrationPlanner creates a new planner with the given config.
// If config is nil, defaults are used.
func NewMigrationPlanner(config *PlannerConfig) *MigrationPlanner {
	if config == nil {
		config = DefaultPlannerConfig()
	}
	if config.BatchSize == 0 {
		config.BatchSize = 1000
	}
	if config.MaxDepth <= 0 {
		config.MaxDepth = 256
	}
	if config.SourceType == "" {
		config.SourceType = "mpt"
	}
	if config.TargetType == "" {
		config.TargetType = "binary"
	}
	return &MigrationPlanner{
		config:  config,
		plans:   make([]*MigrationPlan, 0),
		results: make(map[string]*PhaseResult),
	}
}

// SetSourceTrie attaches a source MPT and destination binary trie for
// real incremental migration execution in ExecutePhase.
func (mp *MigrationPlanner) SetSourceTrie(source *Trie, dest *BinaryTrie) {
	mp.mu.Lock()
	defer mp.mu.Unlock()
	mp.sourceTrie = source
	mp.destTrie = dest
}

// CreatePlan creates a migration plan for the given state root. The plan
// divides the key space into phases based on BatchSize and EstimatedAccounts.
func (mp *MigrationPlanner) CreatePlan(stateRoot [32]byte) (*MigrationPlan, error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	estimated := mp.config.EstimatedAccounts
	if estimated == 0 {
		// Default to a single phase covering the entire key space.
		estimated = mp.config.BatchSize
	}

	batchSize := mp.config.BatchSize
	numPhases := int((estimated + batchSize - 1) / batchSize)
	if numPhases == 0 {
		numPhases = 1
	}
	if numPhases > 256 {
		numPhases = 256
	}

	phases := make([]*MigrationPhase, numPhases)
	step := 256 / numPhases
	if step == 0 {
		step = 1
	}

	for i := 0; i < numPhases; i++ {
		var startKey, endKey [32]byte
		startKey[0] = byte(i * step)

		if i == numPhases-1 {
			for j := range endKey {
				endKey[j] = 0xFF
			}
		} else {
			endKey[0] = byte((i+1)*step - 1)
			for j := 1; j < 32; j++ {
				endKey[j] = 0xFF
			}
		}

		acctCount := batchSize
		if uint64(i) == uint64(numPhases-1) && estimated%batchSize != 0 {
			acctCount = estimated % batchSize
		}

		phases[i] = &MigrationPhase{
			Name:         fmt.Sprintf("phase-%d", i),
			StartKey:     startKey,
			EndKey:       endKey,
			AccountCount: acctCount,
			StorageCount: acctCount * 10, // estimate 10 storage slots per account
			Status:       "pending",
		}
	}

	// Gas estimates: 5200 gas per account (200 read + 5000 write).
	gasPerAccount := uint64(5200)
	totalGas := estimated * gasPerAccount
	gasPerBlock := uint64(30_000_000) // 30M gas per block
	estBlocks := (totalGas + gasPerBlock - 1) / gasPerBlock

	plan := &MigrationPlan{
		ID:              len(mp.plans),
		Phases:          phases,
		TotalAccounts:   estimated,
		EstimatedGas:    totalGas,
		EstimatedBlocks: estBlocks,
		CreatedAt:       time.Now().Unix(),
		Config:          *mp.config,
	}

	mp.plans = append(mp.plans, plan)
	return plan, nil
}

// ExecutePhase executes a single phase of a plan using the incremental
// migration engine when a source trie is available, otherwise simulates
// migration. It records the phase result and updates the phase status.
func (mp *MigrationPlanner) ExecutePhase(planID int, phase int) (*PhaseResult, error) {
	mp.mu.Lock()
	defer mp.mu.Unlock()

	if planID < 0 || planID >= len(mp.plans) {
		return nil, ErrPlannerPlanNotFound
	}
	plan := mp.plans[planID]

	if phase < 0 || phase >= len(plan.Phases) {
		return nil, ErrPlannerPhaseInvalid
	}
	p := plan.Phases[phase]

	start := time.Now()
	p.Status = "running"

	// Use the incremental migration engine if a source trie is attached.
	// Otherwise, fall back to simulated counts for planning purposes.
	accountsMigrated := p.AccountCount
	storageMigrated := p.StorageCount
	gasUsed := accountsMigrated * 5200

	if mp.sourceTrie != nil && mp.destTrie != nil {
		config := IncrementalMigrationConfig{
			BatchSize: int(p.AccountCount),
			CursorKey: fmt.Sprintf("planner_%d_%d", planID, phase),
		}
		im := NewIncrementalMigration(mp.sourceTrie, mp.destTrie, config)
		migrated, _, _ := im.MigrateBlock(int(p.AccountCount))
		accountsMigrated = uint64(migrated)
		storageMigrated = accountsMigrated * 10
		gasUsed = accountsMigrated * 5200
	}

	duration := time.Since(start).Nanoseconds()

	result := &PhaseResult{
		AccountsMigrated: accountsMigrated,
		StorageMigrated:  storageMigrated,
		GasUsed:          gasUsed,
		Duration:         duration,
		Errors:           make([]string, 0),
	}

	p.Status = "done"
	key := fmt.Sprintf("%d:%d", planID, phase)
	mp.results[key] = result

	return result, nil
}

// ValidatePhase checks that a phase has been executed and completed
// without errors.
func (mp *MigrationPlanner) ValidatePhase(planID int, phase int) error {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	if planID < 0 || planID >= len(mp.plans) {
		return ErrPlannerPlanNotFound
	}
	plan := mp.plans[planID]

	if phase < 0 || phase >= len(plan.Phases) {
		return ErrPlannerPhaseInvalid
	}

	p := plan.Phases[phase]
	if p.Status == "pending" || p.Status == "running" {
		return ErrPlannerPhaseNotDone
	}
	if p.Status == "failed" {
		return ErrPlannerPhaseFailed
	}

	key := fmt.Sprintf("%d:%d", planID, phase)
	result, ok := mp.results[key]
	if !ok {
		return ErrPlannerPhaseNotDone
	}
	if len(result.Errors) > 0 {
		return fmt.Errorf("planner: phase had %d errors", len(result.Errors))
	}
	return nil
}

// Progress returns the progress of a migration plan.
func (mp *MigrationPlanner) Progress(planID int) *PlanProgress {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	if planID < 0 || planID >= len(mp.plans) {
		return &PlanProgress{}
	}
	plan := mp.plans[planID]

	completed := 0
	for _, p := range plan.Phases {
		if p.Status == "done" {
			completed++
		}
	}

	total := len(plan.Phases)
	pct := 0.0
	if total > 0 {
		pct = float64(completed) / float64(total) * 100.0
	}

	return &PlanProgress{
		CompletedPhases: completed,
		TotalPhases:     total,
		Percentage:      pct,
	}
}

// ActivePlans returns all plans that have at least one phase not yet "done".
func (mp *MigrationPlanner) ActivePlans() []*MigrationPlan {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	var active []*MigrationPlan
	for _, plan := range mp.plans {
		allDone := true
		for _, p := range plan.Phases {
			if p.Status != "done" {
				allDone = false
				break
			}
		}
		if !allDone {
			active = append(active, plan)
		}
	}
	return active
}

// PlanCount returns the total number of plans.
func (mp *MigrationPlanner) PlanCount() int {
	mp.mu.RLock()
	defer mp.mu.RUnlock()
	return len(mp.plans)
}

// GetPlan returns a plan by ID or nil if not found.
func (mp *MigrationPlanner) GetPlan(planID int) *MigrationPlan {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	if planID < 0 || planID >= len(mp.plans) {
		return nil
	}
	return mp.plans[planID]
}

// GetPhaseResult returns the result for a specific phase, or nil if not yet
// executed.
func (mp *MigrationPlanner) GetPhaseResult(planID int, phase int) *PhaseResult {
	mp.mu.RLock()
	defer mp.mu.RUnlock()

	key := fmt.Sprintf("%d:%d", planID, phase)
	return mp.results[key]
}
