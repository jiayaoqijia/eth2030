package payload

import (
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/bal"
	"github.com/eth2030/eth2030/core/block"
	coreconfig "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/log"
	"github.com/eth2030/eth2030/metrics"
)

var asyncBuilderLog = log.Default().Module("engine/payload/async")

// BuildStatus represents the status of a payload build operation.
type BuildStatus int

const (
	// BuildStatusPending means the build is queued but not started.
	BuildStatusPending BuildStatus = iota
	// BuildStatusBuilding means the build is in progress.
	BuildStatusBuilding
	// BuildStatusCompleted means the build finished successfully.
	BuildStatusCompleted
	// BuildStatusFailed means the build failed.
	BuildStatusFailed
)

// String returns a human-readable status.
func (s BuildStatus) String() string {
	switch s {
	case BuildStatusPending:
		return "pending"
	case BuildStatusBuilding:
		return "building"
	case BuildStatusCompleted:
		return "completed"
	case BuildStatusFailed:
		return "failed"
	default:
		return "unknown"
	}
}

// BuildRequest represents a request to build a payload.
type BuildRequest struct {
	ID           PayloadID
	ParentHash   types.Hash
	ParentHeader *types.Header
	State        state.StateDB
	Attrs        *BuildAttributes
	ResultChan   chan *BuildResult
}

// BuildAttributes contains the attributes for building a payload.
type BuildAttributes struct {
	Timestamp        uint64
	FeeRecipient     types.Address
	PrevRandao       types.Hash
	GasLimit         uint64
	Withdrawals      []*types.Withdrawal
	BeaconRoot       *types.Hash // ParentBeaconBlockRoot from CL (EIP-4788)
	InclusionListTxs [][]byte
}

// BuildResult contains the result of a payload build operation.
type BuildResult struct {
	ID         PayloadID
	Status     BuildStatus
	Block      *types.Block
	Receipts   []*types.Receipt
	BAL        *bal.BlockAccessList
	BlockValue *big.Int
	Error      error
}

// PendingPayload holds a payload that is being built or has been built.
type PendingPayload struct {
	mu sync.RWMutex

	// Immutable fields set at creation
	ID           PayloadID
	ParentHash   types.Hash
	Timestamp    uint64
	FeeRecipient types.Address
	PrevRandao   types.Hash
	Withdrawals  []*types.Withdrawal
	// SlotNumber is the beacon slot from payload attributes (Gloas payloads
	// expose it in the getPayload response). Set right after construction.
	SlotNumber uint64

	// Mutable fields set when build completes
	status     BuildStatus
	block      *types.Block
	receipts   []*types.Receipt
	bal        *bal.BlockAccessList
	blockValue *big.Int
	err        error

	// For waiting on build completion
	readyChan chan struct{}

	// refCount tracks active users of this payload to prevent premature eviction.
	refCount atomic.Int32
}

// NewPendingPayload creates a new pending payload in building state.
func NewPendingPayload(id PayloadID, parentHash types.Hash, timestamp uint64,
	feeRecipient types.Address, prevRandao types.Hash, withdrawals []*types.Withdrawal) *PendingPayload {
	p := &PendingPayload{
		ID:           id,
		ParentHash:   parentHash,
		Timestamp:    timestamp,
		FeeRecipient: feeRecipient,
		PrevRandao:   prevRandao,
		Withdrawals:  withdrawals,
		status:       BuildStatusPending,
		readyChan:    make(chan struct{}),
	}
	p.refCount.Store(1) // Initial reference for creator
	return p
}

// Acquire increments the reference count. Returns false if already released.
func (p *PendingPayload) Acquire() bool {
	for {
		current := p.refCount.Load()
		if current <= 0 {
			return false // Already released
		}
		if p.refCount.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

// Release decrements the reference count.
func (p *PendingPayload) Release() {
	p.refCount.Add(-1)
}

// InUse returns true if the payload is currently being used.
func (p *PendingPayload) InUse() bool {
	return p.refCount.Load() > 0
}

// Status returns the current build status.
func (p *PendingPayload) Status() BuildStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status
}

// SetBuilding marks the payload as being built.
func (p *PendingPayload) SetBuilding() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = BuildStatusBuilding
}

// SetCompleted marks the payload as completed with the given result.
func (p *PendingPayload) SetCompleted(block *types.Block, receipts []*types.Receipt, bal *bal.BlockAccessList, blockValue *big.Int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = BuildStatusCompleted
	p.block = block
	p.receipts = receipts
	p.bal = bal
	p.blockValue = blockValue
	close(p.readyChan)
}

// SetFailed marks the payload as failed with the given error.
func (p *PendingPayload) SetFailed(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.status = BuildStatusFailed
	p.err = err
	close(p.readyChan)
}

// Wait blocks until the build completes or fails, with a timeout.
// Returns the build result or an error if timeout exceeded.
func (p *PendingPayload) Wait(timeout time.Duration) (*BuildResult, error) {
	select {
	case <-p.readyChan:
		p.mu.RLock()
		defer p.mu.RUnlock()
		return &BuildResult{
			ID:         p.ID,
			Status:     p.status,
			Block:      p.block,
			Receipts:   p.receipts,
			BAL:        p.bal,
			BlockValue: p.blockValue,
			Error:      p.err,
		}, nil
	case <-time.After(timeout):
		return nil, fmt.Errorf("payload build timeout after %v", timeout)
	}
}

// GetResult returns the build result if completed, or an error if not ready.
func (p *PendingPayload) GetResult() (*BuildResult, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.status == BuildStatusPending || p.status == BuildStatusBuilding {
		return nil, fmt.Errorf("payload %x not ready: status=%s", p.ID, p.status)
	}

	return &BuildResult{
		ID:         p.ID,
		Status:     p.status,
		Block:      p.block,
		Receipts:   p.receipts,
		BAL:        p.bal,
		BlockValue: p.blockValue,
		Error:      p.err,
	}, nil
}

// ValidateParentFunc is a function that validates if a parent block is still
// the current head. It returns true if the parent is valid for building.
// This is used to prevent building on stale parents during chain reorgs.
type ValidateParentFunc func(parentHash types.Hash) bool

// AsyncBuilder manages asynchronous payload building with a worker pool.
type AsyncBuilder struct {
	config         *coreconfig.ChainConfig
	txPool         block.TxPoolReader
	workers        int
	timeout        time.Duration
	validateParent ValidateParentFunc // Optional validation for parent blocks

	mu           sync.RWMutex
	pending      map[PayloadID]*PendingPayload
	pendingOrder []PayloadID
	maxPending   int

	queue   chan *BuildRequest
	stopCh  chan struct{}
	running atomic.Bool
	wg      sync.WaitGroup
}

// AsyncBuilderConfig holds configuration for AsyncBuilder.
type AsyncBuilderConfig struct {
	// Workers is the number of concurrent build workers.
	Workers int

	// Timeout is the maximum time to wait for a build.
	Timeout time.Duration

	// MaxPending is the maximum number of pending payloads.
	MaxPending int
}

// Default configuration values for AsyncBuilder.
const (
	defaultWorkers    = 2
	defaultTimeout    = 30 * time.Second
	defaultMaxPending = 32
	// queueSizeMultiplier determines the build queue size relative to worker count.
	// A larger queue helps absorb bursts but increases memory usage.
	queueSizeMultiplier = 2
)

// NewAsyncBuilder creates a new async payload builder.
func NewAsyncBuilder(config *coreconfig.ChainConfig, txPool block.TxPoolReader, cfg AsyncBuilderConfig) *AsyncBuilder {
	if cfg.Workers <= 0 {
		cfg.Workers = defaultWorkers
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultTimeout
	}
	if cfg.MaxPending <= 0 {
		cfg.MaxPending = defaultMaxPending
	}

	queueSize := cfg.Workers * queueSizeMultiplier
	return &AsyncBuilder{
		config:     config,
		txPool:     txPool,
		workers:    cfg.Workers,
		timeout:    cfg.Timeout,
		pending:    make(map[PayloadID]*PendingPayload),
		maxPending: cfg.MaxPending,
		queue:      make(chan *BuildRequest, queueSize),
		stopCh:     make(chan struct{}),
	}
}

// SetValidateParent sets the parent validation function.
// This should be called before starting the builder to enable
// validation that prevents building on stale parents during reorgs.
func (b *AsyncBuilder) SetValidateParent(fn ValidateParentFunc) {
	b.validateParent = fn
}

// Start starts the builder workers.
func (b *AsyncBuilder) Start() {
	if b.running.Swap(true) {
		return // Already running
	}

	for i := 0; i < b.workers; i++ {
		b.wg.Add(1)
		go b.worker(i)
	}

	asyncBuilderLog.Info("async_builder_started", "workers", b.workers)
}

// Stop stops the builder workers.
func (b *AsyncBuilder) Stop() {
	if !b.running.Swap(false) {
		return // Not running
	}

	close(b.stopCh)
	b.wg.Wait()

	asyncBuilderLog.Info("async_builder_stopped")
}

// QueueBuild queues a payload build request and returns immediately.
// The returned PendingPayload can be used to check status or wait for completion.
// Returns a non-nil error if the queue is full (the pending is still returned
// with failed status).
func (b *AsyncBuilder) QueueBuild(
	id PayloadID,
	parentHash types.Hash,
	parentHeader *types.Header,
	statedb state.StateDB,
	attrs *BuildAttributes,
) (*PendingPayload, error) {
	pending := NewPendingPayload(id, parentHash, attrs.Timestamp,
		attrs.FeeRecipient, attrs.PrevRandao, attrs.Withdrawals)

	// Store in pending map
	b.mu.Lock()

	// Check if this ID already exists - fail the old one if so
	if old, exists := b.pending[id]; exists {
		old.SetFailed(fmt.Errorf("replaced by new build request with same ID"))
	}

	b.pending[id] = pending
	b.pendingOrder = append(b.pendingOrder, id)

	// Evict old payloads if over limit
	for len(b.pending) > b.maxPending && len(b.pendingOrder) > 0 {
		oldest := b.pendingOrder[0]
		b.pendingOrder = b.pendingOrder[1:]
		if old, exists := b.pending[oldest]; exists {
			old.SetFailed(fmt.Errorf("evicted to make room for new build"))
		}
		delete(b.pending, oldest)
	}
	queueSize := len(b.queue)
	activeBuilds := len(b.pending)
	b.mu.Unlock()

	// Update metrics
	metrics.EnginePayloadBuildTotal.Inc()
	metrics.EnginePayloadBuildQueueSize.Set(int64(queueSize))
	metrics.EnginePayloadBuildActive.Set(int64(activeBuilds))

	// Queue the build request
	req := &BuildRequest{
		ID:           id,
		ParentHash:   parentHash,
		ParentHeader: parentHeader,
		State:        statedb,
		Attrs:        attrs,
	}

	select {
	case b.queue <- req:
		asyncBuilderLog.Debug("build_queued", "payloadID", fmt.Sprintf("%x", id))
		return pending, nil
	default:
		// Queue full, mark as failed
		err := fmt.Errorf("build queue full")
		pending.SetFailed(err)
		metrics.EnginePayloadBuildFailed.Inc()
		asyncBuilderLog.Warn("build_queue_full", "payloadID", fmt.Sprintf("%x", id))
		return pending, err
	}
}

// GetPayload retrieves a pending payload by ID.
func (b *AsyncBuilder) GetPayload(id PayloadID) (*PendingPayload, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	p, ok := b.pending[id]
	return p, ok
}

// EvictOldest removes the oldest pending payload.
func (b *AsyncBuilder) EvictOldest() {
	b.mu.Lock()
	defer b.mu.Unlock()

	for len(b.pending) > b.maxPending && len(b.pendingOrder) > 0 {
		oldest := b.pendingOrder[0]
		b.pendingOrder = b.pendingOrder[1:]
		delete(b.pending, oldest)
	}
}

// worker processes build requests from the queue.
func (b *AsyncBuilder) worker(id int) {
	defer b.wg.Done()

	asyncBuilderLog.Debug("worker_started", "workerID", id)

	for {
		select {
		case <-b.stopCh:
			asyncBuilderLog.Debug("worker_stopped", "workerID", id)
			return
		case req := <-b.queue:
			b.processRequest(req, id)
		}
	}
}

// processRequest handles a single build request.
func (b *AsyncBuilder) processRequest(req *BuildRequest, workerID int) {
	start := time.Now()

	// Update active builds gauge
	b.mu.RLock()
	activeBuilds := len(b.pending)
	b.mu.RUnlock()
	metrics.EnginePayloadBuildActive.Set(int64(activeBuilds))

	// Get the pending payload
	b.mu.RLock()
	pending, ok := b.pending[req.ID]
	b.mu.RUnlock()

	if !ok {
		asyncBuilderLog.Warn("payload_not_found", "payloadID", fmt.Sprintf("%x", req.ID))
		return
	}

	// CRITICAL: Validate that the parent is still valid before building.
	// This prevents building on stale parents during chain reorgs which causes
	// ParentBlockHashMismatch errors when the CL expects a different parent.
	if b.validateParent != nil && !b.validateParent(req.ParentHash) {
		err := fmt.Errorf("parent block %s is no longer valid (chain reorg)", req.ParentHash.Hex())
		pending.SetFailed(err)
		metrics.EnginePayloadBuildFailed.Inc()
		asyncBuilderLog.Warn("build_parent_invalid",
			"workerID", workerID,
			"payloadID", fmt.Sprintf("%x", req.ID),
			"parentHash", req.ParentHash.Hex(),
			"reason", "parent no longer current head",
		)
		return
	}

	pending.SetBuilding()
	asyncBuilderLog.Debug("build_started",
		"workerID", workerID,
		"payloadID", fmt.Sprintf("%x", req.ID),
		"parentHash", req.ParentHash.Hex(),
	)

	// Build the block
	builder := block.NewBlockBuilder(b.config, nil, b.txPool)
	builder.SetState(req.State.Dup())

	blk, receipts, err := builder.BuildBlock(req.ParentHeader, &block.BuildBlockAttributes{
		Timestamp:        req.Attrs.Timestamp,
		FeeRecipient:     req.Attrs.FeeRecipient,
		Random:           req.Attrs.PrevRandao,
		GasLimit:         req.Attrs.GasLimit,
		Withdrawals:      req.Attrs.Withdrawals,
		BeaconRoot:       req.Attrs.BeaconRoot,
		InclusionListTxs: req.Attrs.InclusionListTxs,
	})

	if err != nil {
		pending.SetFailed(err)
		metrics.EnginePayloadBuildFailed.Inc()
		asyncBuilderLog.Error("build_failed",
			"workerID", workerID,
			"payloadID", fmt.Sprintf("%x", req.ID),
			"error", err,
			"duration", time.Since(start),
		)
		return
	}

	// Calculate block value
	blockValue := calcBlockValue(blk, receipts, req.ParentHeader.BaseFee)

	pending.SetCompleted(blk, receipts, nil, blockValue)
	metrics.EnginePayloadBuildSuccess.Inc()

	asyncBuilderLog.Info("build_completed",
		"workerID", workerID,
		"payloadID", fmt.Sprintf("%x", req.ID),
		"blockHash", blk.Hash().Hex(),
		"blockNum", blk.NumberU64(),
		"txCount", len(blk.Transactions()),
		"gasUsed", blk.GasUsed(),
		"duration", time.Since(start),
	)
}
