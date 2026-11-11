package engine

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/bal"
	"github.com/eth2030/eth2030/core/block"
	coreconfig "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/eips"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/bls"
	"github.com/eth2030/eth2030/engine/actor"
	enginepayload "github.com/eth2030/eth2030/engine/payload"
	"github.com/eth2030/eth2030/focil"
	"github.com/eth2030/eth2030/log"
	"github.com/eth2030/eth2030/metrics"
)

var backendLog = log.Default().Module("engine/backend")

const (
	// defaultMaxPayloads is the default cap for pending built payloads.
	defaultMaxPayloads = 32

	// defaultMaxILs is the default cap for inclusion lists stored per slot.
	defaultMaxILs = 256
)

// pendingPayload holds a payload being built by the block builder.
type pendingPayload struct {
	block        *types.Block
	receipts     []*types.Receipt
	requests     types.Requests
	bal          *bal.BlockAccessList // EIP-7928
	blockValue   *big.Int
	parentHash   types.Hash
	timestamp    uint64
	feeRecipient types.Address
	prevRandao   types.Hash
	withdrawals  []*Withdrawal
	// slotNumber is the beacon slot from payload attributes (Gloas payloads
	// expose it as a payload field for CL-side envelope bookkeeping).
	slotNumber uint64
}

// EngineBackendConfig holds tunable memory limits for EngineBackend.
type EngineBackendConfig struct {
	// MaxPayloads caps the number of pending built payloads kept in memory.
	// Defaults to defaultMaxPayloads when zero.
	MaxPayloads int

	// MaxILs caps the number of inclusion lists stored per slot.
	// Defaults to defaultMaxILs when zero.
	MaxILs int
}

// EngineBackend is the execution-layer backend that connects the Engine API
// to the block builder and state processor.
// P1: Refactored to use fine-grained locks for better concurrency.
type EngineBackend struct {
	// P1: Fine-grained locks for better concurrency.
	// stateMu protects forkchoice state (headHash, safeHash, finalHash).
	stateMu sync.RWMutex
	// blocksMu protects blocks, bals, numberIndex.
	blocksMu sync.RWMutex
	// payloadMu protects payloads, payloadOrder.
	payloadMu sync.RWMutex
	// ilMu protects inclusion lists.
	ilMu sync.RWMutex

	config    *coreconfig.ChainConfig
	statedb   state.StateDB
	processor *execution.StateProcessor
	// parallelProcessor executes transactions in parallel using BAL (EIP-7928).
	parallelProcessor *execution.ParallelProcessor
	// parallelEnabled controls whether parallel execution is used.
	parallelEnabled bool

	// Forkchoice state - protected by stateMu.
	headHash  types.Hash
	safeHash  types.Hash
	finalHash types.Hash

	// Block storage - protected by blocksMu.
	blocks      map[types.Hash]*types.Block
	bals        map[types.Hash]*bal.BlockAccessList // stored BALs for getPayloadBodiesV2
	numberIndex map[uint64]types.Hash               // reverse index: block number -> hash

	// Payload storage - protected by payloadMu.
	payloads     map[PayloadID]*pendingPayload
	payloadOrder []PayloadID // insertion order for payload LRU eviction
	maxPayloads  int         // configurable cap; set from EngineBackendConfig

	// Inclusion lists - protected by ilMu.
	ils    []*types.InclusionList // received via engine_newInclusionListV1
	maxILs int                    // configurable cap; set from EngineBackendConfig

	nextPayloadID atomic.Uint64

	// Actor-based state management (Phase 6 of engine-channel-refactor).
	// Actors run concurrently and handle state without locks.
	// The mutex-protected fields above are kept for gradual migration.
	actors   *actor.EngineActors
	actorCtx context.Context
	actorMu  sync.RWMutex // protects actors field during Close()

	// actorTimeout is the timeout for actor operations.
	actorTimeout time.Duration

	// asyncBuilder handles asynchronous payload building.
	// This allows FCU to return quickly without waiting for payload completion.
	asyncBuilder *enginepayload.AsyncBuilder

	// asyncPayloads tracks payloads being built asynchronously.
	asyncPayloadsMu   sync.RWMutex
	asyncPayloads     map[PayloadID]*enginepayload.PendingPayload
	asyncPayloadOrder []PayloadID // insertion order for LRU eviction

	// cleanupStopCh signals the cleanup goroutine to stop.
	cleanupStopCh chan struct{}
	cleanupWg     sync.WaitGroup

	// txPool provides access to pending transactions for inclusion list generation.
	txPoolMu sync.RWMutex
	txPool   block.TxPoolReader
}

// NewEngineBackend creates a new Engine API backend with default memory limits.
func NewEngineBackend(config *coreconfig.ChainConfig, statedb state.StateDB, genesis *types.Block) *EngineBackend {
	return NewEngineBackendWithConfig(config, statedb, genesis, EngineBackendConfig{})
}

// NewEngineBackendWithConfig creates a new Engine API backend with explicit
// memory limits. Zero values in cfg fall back to package defaults.
func NewEngineBackendWithConfig(config *coreconfig.ChainConfig, statedb state.StateDB, genesis *types.Block, cfg EngineBackendConfig) *EngineBackend {
	if cfg.MaxPayloads <= 0 {
		cfg.MaxPayloads = defaultMaxPayloads
	}
	if cfg.MaxILs <= 0 {
		cfg.MaxILs = defaultMaxILs
	}
	b := &EngineBackend{
		config:            config,
		statedb:           statedb,
		processor:         execution.NewStateProcessor(config),
		parallelProcessor: execution.NewParallelProcessor(config),
		parallelEnabled:   true, // Enable by default for EIP-7928 blocks
		blocks:            make(map[types.Hash]*types.Block),
		bals:              make(map[types.Hash]*bal.BlockAccessList),
		payloads:          make(map[PayloadID]*pendingPayload),
		maxPayloads:       cfg.MaxPayloads,
		maxILs:            cfg.MaxILs,
		numberIndex:       make(map[uint64]types.Hash),
		actorCtx:          context.Background(),
		actorTimeout:      actor.DefaultTimeout,
		asyncPayloads:     make(map[PayloadID]*enginepayload.PendingPayload),
	}
	if genesis != nil {
		h := genesis.Hash()
		b.blocks[h] = genesis
		b.numberIndex[genesis.NumberU64()] = h
		b.headHash = h
		b.safeHash = h
		b.finalHash = h
	}
	// Initialize actors for concurrent state management.
	b.actors = actor.NewEngineActors(b.actorCtx, b.maxPayloads, b.maxILs)

	// Initialize async payload builder (P0 improvement: async payload building).
	b.asyncBuilder = enginepayload.NewAsyncBuilder(config, nil, enginepayload.AsyncBuilderConfig{
		Workers:    2,
		Timeout:    30 * time.Second,
		MaxPending: cfg.MaxPayloads,
	})
	// Set parent validation to prevent building on stale parents during reorgs.
	// This is critical to avoid ParentBlockHashMismatch errors when the CL
	// expects a block built on a different parent than what was queued.
	b.asyncBuilder.SetValidateParent(func(parentHash types.Hash) bool {
		headHash := b.getHeadHash()
		return headHash == parentHash
	})
	b.asyncBuilder.Start()

	// Start cleanup goroutine for failed async payloads.
	b.cleanupStopCh = make(chan struct{})
	b.cleanupWg.Add(1)
	go b.asyncPayloadsCleanupLoop()

	return b
}

// Close gracefully stops all actors and releases resources.
func (b *EngineBackend) Close() {
	// Stop cleanup goroutine first.
	if b.cleanupStopCh != nil {
		close(b.cleanupStopCh)
		b.cleanupWg.Wait()
	}

	b.actorMu.Lock()
	defer b.actorMu.Unlock()
	if b.asyncBuilder != nil {
		b.asyncBuilder.Stop()
	}
	if b.actors != nil {
		b.actors.Stop()
		b.actors = nil
	}
}

// asyncPayloadsCleanupLoop periodically removes failed async payloads
// to prevent memory leaks from accumulated failed builds.
func (b *EngineBackend) asyncPayloadsCleanupLoop() {
	defer b.cleanupWg.Done()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-b.cleanupStopCh:
			return
		case <-ticker.C:
			b.cleanupFailedAsyncPayloads()
		}
	}
}

// cleanupFailedAsyncPayloads removes failed payloads from asyncPayloads.
// It also removes completed payloads that are not in use.
func (b *EngineBackend) cleanupFailedAsyncPayloads() {
	b.asyncPayloadsMu.Lock()
	defer b.asyncPayloadsMu.Unlock()

	var toRemove []PayloadID
	for id, pending := range b.asyncPayloads {
		status := pending.Status()
		// Remove failed or completed payloads that are not in use
		if status == enginepayload.BuildStatusFailed ||
			(status == enginepayload.BuildStatusCompleted && !pending.InUse()) {
			toRemove = append(toRemove, id)
		}
	}

	for _, id := range toRemove {
		delete(b.asyncPayloads, id)
		// Remove from order slice
		for i, oid := range b.asyncPayloadOrder {
			if oid == id {
				b.asyncPayloadOrder = append(b.asyncPayloadOrder[:i], b.asyncPayloadOrder[i+1:]...)
				break
			}
		}
	}

	if len(toRemove) > 0 {
		backendLog.Debug("async_payloads_cleanup",
			"event", "async_payloads_cleanup",
			"removed", len(toRemove),
			"remaining", len(b.asyncPayloads),
		)
	}
}

func (b *EngineBackend) getHeadHash() types.Hash {
	b.stateMu.RLock()
	defer b.stateMu.RUnlock()
	return b.headHash
}

// GetBlockByHash returns a block stored by the backend, or nil if unknown.
func (b *EngineBackend) GetBlockByHash(hash types.Hash) *types.Block {
	if blk, ok := b.getBlock(hash); ok {
		return blk
	}
	return nil
}

func (b *EngineBackend) getForkchoiceState() (headHash, safeHash, finalHash types.Hash) {
	b.stateMu.RLock()
	defer b.stateMu.RUnlock()
	return b.headHash, b.safeHash, b.finalHash
}

func (b *EngineBackend) setForkchoiceState(headHash, safeHash, finalHash types.Hash) {
	b.stateMu.Lock()
	defer b.stateMu.Unlock()
	b.headHash = headHash
	b.safeHash = safeHash
	b.finalHash = finalHash
}

func (b *EngineBackend) hasBlock(hash types.Hash) bool {
	b.blocksMu.RLock()
	defer b.blocksMu.RUnlock()
	_, ok := b.blocks[hash]
	return ok
}

func (b *EngineBackend) getBlock(hash types.Hash) (*types.Block, bool) {
	b.blocksMu.RLock()
	defer b.blocksMu.RUnlock()
	blk, ok := b.blocks[hash]
	return blk, ok
}

func (b *EngineBackend) storeBlock(blk *types.Block, blockBAL *bal.BlockAccessList) {
	b.blocksMu.Lock()
	defer b.blocksMu.Unlock()

	blockHash := blk.Hash()
	if prevHash, ok := b.numberIndex[blk.NumberU64()]; ok && prevHash != blockHash {
		backendLog.Debug("block_number_collision",
			"event", "block_number_collision",
			"blockNum", blk.NumberU64(),
			"newHash", blockHash.Hex(),
			"prevHash", prevHash.Hex(),
			"parentHash", blk.ParentHash().Hex(),
		)
	}
	b.blocks[blockHash] = blk
	b.numberIndex[blk.NumberU64()] = blockHash
	if blockBAL != nil {
		b.bals[blockHash] = blockBAL
	}
	b.evictOldBlocks()
}

func (b *EngineBackend) getPayload(id PayloadID) (*pendingPayload, bool) {
	b.payloadMu.RLock()
	defer b.payloadMu.RUnlock()
	pending, ok := b.payloads[id]
	return pending, ok
}

func (b *EngineBackend) getStoredPayload(id PayloadID) (*pendingPayload, error) {
	pending, ok := b.getPayload(id)
	if !ok {
		return nil, ErrUnknownPayload
	}
	return pending, nil
}

func (b *EngineBackend) storePayload(id PayloadID, pending *pendingPayload) {
	b.payloadMu.Lock()
	defer b.payloadMu.Unlock()
	b.payloads[id] = pending
	b.payloadOrder = append(b.payloadOrder, id)
	b.evictOldestPayload()
}

func (b *EngineBackend) getInclusionListCount() int {
	b.ilMu.RLock()
	defer b.ilMu.RUnlock()
	return len(b.ils)
}

func (b *EngineBackend) addInclusionList(il *types.InclusionList) {
	b.ilMu.Lock()
	defer b.ilMu.Unlock()
	b.ils = append(b.ils, il)
	if len(b.ils) > b.maxILs {
		b.ils = b.ils[len(b.ils)-b.maxILs:]
	}
}

func (b *EngineBackend) clearInclusionLists() {
	b.ilMu.Lock()
	defer b.ilMu.Unlock()
	b.ils = b.ils[:0]
}

func (b *EngineBackend) waitAsyncPayload(id PayloadID, pin bool) (*enginepayload.PendingPayload, *enginepayload.BuildResult, func(), error) {
	b.asyncPayloadsMu.RLock()
	asyncPending, asyncOk := b.asyncPayloads[id]
	release := func() {}
	if pin && asyncOk && asyncPending != nil {
		if !asyncPending.Acquire() {
			b.asyncPayloadsMu.RUnlock()
			asyncOk = false
		} else {
			release = asyncPending.Release
		}
	}
	b.asyncPayloadsMu.RUnlock()

	if !asyncOk || asyncPending == nil {
		return nil, nil, nil, nil
	}

	result, err := asyncPending.Wait(8 * time.Second)
	if err != nil {
		backendLog.Warn("payload_build_timeout",
			"event", "payload_build_timeout",
			"payloadID", fmt.Sprintf("%x", id),
			"error", err,
		)
		return nil, nil, release, ErrUnknownPayload
	}

	if result.Status == enginepayload.BuildStatusFailed {
		backendLog.Error("payload_build_failed",
			"event", "payload_build_failed",
			"payloadID", fmt.Sprintf("%x", id),
			"error", result.Error,
		)
		return nil, nil, release, ErrUnknownPayload
	}

	if result.Status != enginepayload.BuildStatusCompleted || result.Block == nil {
		return nil, nil, release, nil
	}

	return asyncPending, result, release, nil
}

func (b *EngineBackend) loadPayloadBodiesByHash(hashes []types.Hash) []*enginepayload.ExecutionPayloadBodyV2 {
	headHash := b.getHeadHash()

	b.blocksMu.RLock()
	defer b.blocksMu.RUnlock()

	headNum := uint64(0)
	if head, ok := b.blocks[headHash]; ok {
		headNum = head.NumberU64()
	}

	results := make([]*enginepayload.ExecutionPayloadBodyV2, len(hashes))
	for i, h := range hashes {
		block, found := b.blocks[h]
		if !found || !rawdb.IsBALRetained(headNum, block.NumberU64()) {
			continue
		}
		body := enginepayload.BlockToPayloadBodyV2(block)
		if blockBAL, ok := b.bals[h]; ok {
			balBytes, _ := json.Marshal(blockBAL)
			body.BlockAccessList = balBytes
		}
		results[i] = body
	}
	return results
}

func (b *EngineBackend) loadPayloadBodiesByRange(start, count uint64) []*enginepayload.ExecutionPayloadBodyV2 {
	headHash := b.getHeadHash()

	b.blocksMu.RLock()
	defer b.blocksMu.RUnlock()

	headNum := uint64(0)
	if head, ok := b.blocks[headHash]; ok {
		headNum = head.NumberU64()
	}

	canonical := make(map[uint64]types.Hash, count)
	if head, ok := b.blocks[headHash]; ok {
		minNum := start
		for cur := head; cur != nil && cur.NumberU64() >= minNum; {
			num := cur.NumberU64()
			if num >= start && num < start+count {
				canonical[num] = cur.Hash()
				if len(canonical) == int(count) {
					break
				}
			}
			if num == 0 {
				break
			}
			parent, ok := b.blocks[cur.ParentHash()]
			if !ok {
				backendLog.Debug("payload_bodies_range_missing_parent",
					"event", "payload_bodies_range_missing_parent",
					"headHash", headHash.Hex(),
					"blockNum", num,
					"blockHash", cur.Hash().Hex(),
					"missingParentHash", cur.ParentHash().Hex(),
					"start", start,
					"count", count,
				)
				break
			}
			cur = parent
		}
	}

	results := make([]*enginepayload.ExecutionPayloadBodyV2, count)
	for i := uint64(0); i < count; i++ {
		num := start + i
		hash, ok := canonical[num]
		if !ok {
			continue
		}
		if indexedHash, indexed := b.numberIndex[num]; indexed && indexedHash != hash {
			backendLog.Debug("payload_bodies_range_canonical_mismatch",
				"event", "payload_bodies_range_canonical_mismatch",
				"blockNum", num,
				"headHash", headHash.Hex(),
				"canonicalHash", hash.Hex(),
				"indexedHash", indexedHash.Hex(),
			)
		}
		block, ok := b.blocks[hash]
		if !ok || !rawdb.IsBALRetained(headNum, num) {
			continue
		}
		body := enginepayload.BlockToPayloadBodyV2(block)
		if blockBAL, ok := b.bals[hash]; ok {
			balBytes, _ := json.Marshal(blockBAL)
			body.BlockAccessList = balBytes
		}
		results[i] = body
	}
	if len(canonical) != int(count) {
		backendLog.Debug("payload_bodies_range_incomplete",
			"event", "payload_bodies_range_incomplete",
			"headHash", headHash.Hex(),
			"headNum", headNum,
			"start", start,
			"count", count,
			"resolved", len(canonical),
		)
	}
	return results
}

// evictOldBlocks removes block entries that are more than 64 blocks behind the
// current head from b.blocks and b.bals. Must be called with blocksMu held.
func (b *EngineBackend) evictOldBlocks() {
	headHash := b.getHeadHash()
	head, ok := b.blocks[headHash]
	if !ok || head.NumberU64() < 64 {
		return
	}
	cutoff := head.NumberU64() - 64

	for hash, blk := range b.blocks {
		if blk.NumberU64() < cutoff {
			delete(b.numberIndex, blk.NumberU64())
			delete(b.blocks, hash)
			delete(b.bals, hash)
		}
	}
}

// evictOldestPayload removes the oldest pending payload when the map exceeds
// b.maxPayloads. Must be called with payloadMu held.
func (b *EngineBackend) evictOldestPayload() {
	for len(b.payloads) > b.maxPayloads {
		if len(b.payloadOrder) == 0 {
			break
		}
		oldest := b.payloadOrder[0]
		b.payloadOrder = b.payloadOrder[1:]
		delete(b.payloads, oldest)
	}
}

// ProcessBlock validates and executes a new payload from the consensus layer.
func (b *EngineBackend) ProcessBlock(
	payload *ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
) (PayloadStatusV1, error) {
	metrics.EngineNewPayload.Inc()
	backendLog.Debug("payload_received",
		"event", "payload_received",
		"blockHash", payload.BlockHash.Hex(),
		"blockNum", payload.BlockNumber,
		"parentHash", payload.ParentHash.Hex(),
		"txCount", len(payload.Transactions),
		"gasUsed", payload.GasUsed,
		"gasLimit", payload.GasLimit,
		"timestamp", payload.Timestamp,
	)

	blk, err := payloadToBlock(payload, parentBeaconBlockRoot)
	if err != nil {
		errMsg := err.Error()
		backendLog.Warn("payload_invalid",
			"event", "payload_invalid",
			"blockHash", payload.BlockHash.Hex(),
			"blockNum", payload.BlockNumber,
			"reason", "payload decode failed",
			"error", errMsg,
		)
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	// Validate block hash: the hash computed from the header fields must match
	// the blockHash provided in the payload. This validation ensures that the
	// CL's block hash calculation (which includes TxHash, WithdrawalsHash,
	// ParentBeaconRoot, etc.) matches what we compute from the payload.
	computedHash := blk.Hash()
	if payload.BlockHash != (types.Hash{}) && computedHash != payload.BlockHash {
		errMsg := fmt.Sprintf("block hash mismatch: computed %s, payload %s", computedHash, payload.BlockHash)
		backendLog.Warn("payload_invalid",
			"event", "payload_invalid",
			"blockHash", payload.BlockHash.Hex(),
			"blockNum", payload.BlockNumber,
			"reason", "block hash mismatch",
			"computedHash", computedHash.Hex(),
		)
		return PayloadStatusV1{
			Status:          StatusInvalidBlockHash,
			LatestValidHash: &computedHash,
			ValidationError: &errMsg,
		}, nil
	}

	// P1: Use fine-grained locks instead of single b.mu.
	// Check that the parent exists.
	parentHash := blk.ParentHash()
	parentBlock, parentOk := b.getBlock(parentHash)

	if !parentOk {
		backendLog.Debug("payload_syncing",
			"event", "payload_syncing",
			"blockHash", payload.BlockHash.Hex(),
			"blockNum", payload.BlockNumber,
			"parentHash", parentHash.Hex(),
		)
		return PayloadStatusV1{Status: StatusSyncing}, nil
	}

	// Validate timestamp progression: block timestamp must be > parent timestamp.
	if parentBlock != nil && payload.Timestamp <= parentBlock.Header().Time {
		errMsg := fmt.Sprintf("invalid timestamp: block %d <= parent %d", payload.Timestamp, parentBlock.Header().Time)
		backendLog.Warn("payload_invalid",
			"event", "payload_invalid",
			"blockHash", payload.BlockHash.Hex(),
			"blockNum", payload.BlockNumber,
			"reason", "timestamp not advancing",
			"blockTs", payload.Timestamp,
			"parentTs", parentBlock.Header().Time,
		)
		return PayloadStatusV1{
			Status:          StatusInvalid,
			LatestValidHash: &parentHash,
			ValidationError: &errMsg,
		}, nil
	}

	// Run through the state processor.
	stateCopy := b.statedb.Dup()
	_, err = b.processor.Process(blk, stateCopy)
	if err != nil {
		errMsg := fmt.Sprintf("state processing failed: %v", err)
		backendLog.Error("payload_exec_fail",
			"event", "payload_exec_fail",
			"blockHash", payload.BlockHash.Hex(),
			"blockNum", payload.BlockNumber,
			"txCount", len(payload.Transactions),
			"error", err,
		)
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	// Store the block and update state (evict old blocks to bound memory).
	blockHash := blk.Hash()
	b.storeBlock(blk, nil)
	b.statedb = stateCopy

	// Dual-write to actors for migration.
	if err := b.storeActorBlock(blockHash, blk, nil); err != nil {
		backendLog.Debug("actor_block_store_failed", "error", err)
	}

	backendLog.Info("payload_valid",
		"event", "payload_valid",
		"blockHash", blockHash.Hex(),
		"blockNum", payload.BlockNumber,
		"txCount", len(payload.Transactions),
		"gasUsed", payload.GasUsed,
	)

	return PayloadStatusV1{
		Status:          StatusValid,
		LatestValidHash: &blockHash,
	}, nil
}

// ProcessBlockV4 validates and executes a Prague payload with execution requests.
func (b *EngineBackend) ProcessBlockV4(
	payload *ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (PayloadStatusV1, error) {
	var extended ExecutionPayloadV5
	extended.ExecutionPayloadV3 = *payload
	return b.processRequestPayload(&extended, parentBeaconBlockRoot, executionRequests, false)
}

// GetHeadTimestamp returns the timestamp of the current head block.
func (b *EngineBackend) GetHeadTimestamp() uint64 {
	headHash := b.getHeadHash()
	headBlock, ok := b.getBlock(headHash)
	if !ok {
		return 0
	}
	return headBlock.Header().Time
}

// GetBlockTimestamp returns the timestamp of the block with the given hash,
// or 0 if the block is not known.
func (b *EngineBackend) GetBlockTimestamp(hash types.Hash) uint64 {
	blk, ok := b.getBlock(hash)
	if !ok {
		return 0
	}
	return blk.Header().Time
}

// IsCancun returns true if the given timestamp falls within the Cancun fork.
func (b *EngineBackend) IsCancun(timestamp uint64) bool {
	return b.config.IsCancun(timestamp)
}

// ForkchoiceUpdated processes a forkchoice state update from the CL.
// P1: Uses fine-grained locks for better concurrency.
func (b *EngineBackend) ForkchoiceUpdated(
	fcState ForkchoiceStateV1,
	attrs *PayloadAttributesV3,
) (ForkchoiceUpdatedResult, error) {
	metrics.EngineFCU.Inc()
	backendLog.Debug("fcu_received",
		"event", "fcu_received",
		"head", fcState.HeadBlockHash.Hex(),
		"safe", fcState.SafeBlockHash.Hex(),
		"finalized", fcState.FinalizedBlockHash.Hex(),
		"hasAttrs", attrs != nil,
	)

	// P1: Use fine-grained locks instead of single b.mu.
	// Validate head block exists.
	if fcState.HeadBlockHash != (types.Hash{}) && !b.hasBlock(fcState.HeadBlockHash) {
		backendLog.Debug("fcu_syncing",
			"event", "fcu_syncing",
			"head", fcState.HeadBlockHash.Hex(),
		)
		return ForkchoiceUpdatedResult{
			PayloadStatus: PayloadStatusV1{Status: StatusSyncing},
		}, nil
	}

	// Update forkchoice pointers with stateMu.
	b.setForkchoiceState(fcState.HeadBlockHash, fcState.SafeBlockHash, fcState.FinalizedBlockHash)

	// Dual-write to actors for migration.
	if err := b.setActorHeadHash(fcState.HeadBlockHash); err != nil {
		backendLog.Debug("actor_head_update_failed", "error", err)
	}
	if err := b.setActorSafeHash(fcState.SafeBlockHash); err != nil {
		backendLog.Debug("actor_safe_update_failed", "error", err)
	}
	if err := b.setActorFinalHash(fcState.FinalizedBlockHash); err != nil {
		backendLog.Debug("actor_final_update_failed", "error", err)
	}

	headHash, safeHash, finalHash := b.getForkchoiceState()
	headBlock, headOk := b.getBlock(headHash)

	headNum := uint64(0)
	if headOk {
		headNum = headBlock.NumberU64()
	}
	backendLog.Info("fcu_updated",
		"event", "fcu_updated",
		"head", headHash.Hex(),
		"headNum", headNum,
		"safe", safeHash.Hex(),
		"finalized", finalHash.Hex(),
		"hasAttrs", attrs != nil,
	)

	status := PayloadStatusV1{
		Status:          StatusValid,
		LatestValidHash: &headHash,
	}

	result := ForkchoiceUpdatedResult{PayloadStatus: status}

	// If payload attributes provided, start building a new payload asynchronously.
	if attrs != nil {
		if attrs.Timestamp == 0 {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}

		// P2: Get parentBlock under lock protection to avoid race condition
		parentBlock, _ := b.getBlock(fcState.HeadBlockHash)
		if parentBlock == nil {
			return ForkchoiceUpdatedResult{}, ErrInvalidForkchoiceState
		}

		// Validate timestamp progression: must be greater than parent block.
		if attrs.Timestamp <= parentBlock.Header().Time {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}

		id := b.generatePayloadID(fcState.HeadBlockHash, attrs.Timestamp)

		backendLog.Debug("payload_build_queued",
			"event", "payload_build_queued",
			"payloadID", fmt.Sprintf("%x", id),
			"parentHash", fcState.HeadBlockHash.Hex(),
			"parentNum", parentBlock.NumberU64(),
			"timestamp", attrs.Timestamp,
			"feeRecipient", attrs.SuggestedFeeRecipient.Hex(),
		)

		// P0: Async payload building - queue the build and return immediately.
		// This allows FCU to return quickly without waiting for payload completion.
		if b.asyncBuilder == nil {
			// Fallback: synchronous build if async builder not initialized.
			backendLog.Warn("async_builder_nil", "event", "async_builder_nil")
			return result, nil
		}

		buildAttrs := &enginepayload.BuildAttributes{
			Timestamp:    attrs.Timestamp,
			FeeRecipient: attrs.SuggestedFeeRecipient,
			PrevRandao:   attrs.PrevRandao,
			GasLimit:     parentBlock.Header().GasLimit,
			Withdrawals:  WithdrawalsToCore(attrs.Withdrawals),
			BeaconRoot:   &attrs.ParentBeaconBlockRoot,
		}

		pending, queueErr := b.asyncBuilder.QueueBuild(
			id,
			fcState.HeadBlockHash,
			parentBlock.Header(),
			b.statedb,
			buildAttrs,
		)

		// Log queue error but continue - the pending is still valid with failed status
		if queueErr != nil {
			backendLog.Warn("payload_queue_failed",
				"event", "payload_queue_failed",
				"payloadID", fmt.Sprintf("%x", id),
				"error", queueErr,
			)
			// Don't set PayloadID since build failed to queue
			return result, nil
		}

		// Store the pending payload for later retrieval.
		b.asyncPayloadsMu.Lock()
		b.asyncPayloads[id] = pending
		b.asyncPayloadOrder = append(b.asyncPayloadOrder, id)
		// Evict oldest async payloads if over limit (FIFO eviction).
		// Skip payloads that are currently in use to prevent premature eviction.
		for len(b.asyncPayloads) > b.maxPayloads && len(b.asyncPayloadOrder) > 0 {
			oldest := b.asyncPayloadOrder[0]
			b.asyncPayloadOrder = b.asyncPayloadOrder[1:]
			// Check if the payload is in use before evicting
			if p, ok := b.asyncPayloads[oldest]; ok && p.InUse() {
				// Put it back at the end of the order, try next one
				b.asyncPayloadOrder = append(b.asyncPayloadOrder, oldest)
				continue
			}
			delete(b.asyncPayloads, oldest)
			break
		}
		b.asyncPayloadsMu.Unlock()

		result.PayloadID = &id
	}

	return result, nil
}

// GetPayloadByID retrieves a previously built payload by its ID.
// P0: Supports async payload building - waits for completion if still building.
// P2: Uses reference counting to prevent premature eviction during Wait.
func (b *EngineBackend) GetPayloadByID(id PayloadID) (*GetPayloadResponse, error) {
	asyncPending, result, release, err := b.waitAsyncPayload(id, true)
	if release != nil {
		defer release()
	}
	if err != nil {
		return nil, err
	}
	if asyncPending != nil && result != nil {
		engineWithdrawals := enginepayload.WithdrawalsToEngine(asyncPending.Withdrawals)
		ep := enginepayload.BlockToPayload(result.Block, asyncPending.PrevRandao, engineWithdrawals)

		backendLog.Debug("payload_get",
			"event", "payload_get",
			"payloadID", fmt.Sprintf("%x", id),
			"blockHash", result.Block.Hash().Hex(),
			"blockNum", result.Block.NumberU64(),
			"txCount", len(result.Block.Transactions()),
			"source", "async",
		)

		return &GetPayloadResponse{
			ExecutionPayload: ep,
			BlockValue:       result.BlockValue,
			BlobsBundle:      collectBlobsBundleV1(result.Block.Transactions()),
		}, nil
	}

	// Try actor next.
	actorPayload, err := b.getActorPayload(id)
	if err == nil && actorPayload != nil {
		// Convert actor.PendingPayload to response.
		withdrawals := make([]*Withdrawal, len(actorPayload.Withdrawals))
		for i, w := range actorPayload.Withdrawals {
			withdrawals[i] = &Withdrawal{
				Index:          w.Index,
				ValidatorIndex: w.ValidatorIndex,
				Address:        w.Address,
				Amount:         w.Amount,
			}
		}
		ep := enginepayload.BlockToPayload(actorPayload.Block, actorPayload.PrevRandao, withdrawals)

		backendLog.Debug("payload_get",
			"event", "payload_get",
			"payloadID", fmt.Sprintf("%x", id),
			"blockHash", actorPayload.Block.Hash().Hex(),
			"blockNum", actorPayload.Block.NumberU64(),
			"txCount", len(actorPayload.Block.Transactions()),
			"source", "actor",
		)

		return &GetPayloadResponse{
			ExecutionPayload: ep,
			BlockValue:       new(big.Int).Set(actorPayload.BlockValue),
			BlobsBundle:      collectBlobsBundleV1(actorPayload.Block.Transactions()),
		}, nil
	}

	// Fallback to mutex-protected payloads.
	pending, err := b.getStoredPayload(id)
	if err != nil {
		backendLog.Warn("payload_get_miss",
			"event", "payload_get_miss",
			"payloadID", fmt.Sprintf("%x", id),
		)
		return nil, err
	}

	ep := enginepayload.BlockToPayload(pending.block, pending.prevRandao, pending.withdrawals)

	backendLog.Debug("payload_get",
		"event", "payload_get",
		"payloadID", fmt.Sprintf("%x", id),
		"blockHash", pending.block.Hash().Hex(),
		"blockNum", pending.block.NumberU64(),
		"txCount", len(pending.block.Transactions()),
		"blockValue", pending.blockValue.String(),
		"source", "mutex",
	)

	return &GetPayloadResponse{
		ExecutionPayload: ep,
		BlockValue:       new(big.Int).Set(pending.blockValue),
		BlobsBundle:      collectBlobsBundleV1(pending.block.Transactions()),
	}, nil
}

// generatePayloadID creates a unique payload ID from parent hash and timestamp.
func (b *EngineBackend) generatePayloadID(parentHash types.Hash, timestamp uint64) PayloadID {
	newID := b.nextPayloadID.Add(1)
	var id PayloadID
	binary.BigEndian.PutUint64(id[:], newID)
	return id
}

// payloadToBlock converts an ExecutionPayloadV3 to a types.Block.
// IMPORTANT: This function computes the correct TxHash, WithdrawalsHash, and
// sets ParentBeaconRoot from the provided parameter. These fields are required
// for the block hash to match what the CL computes.
func payloadToBlock(payload *ExecutionPayloadV3, parentBeaconBlockRoot types.Hash) (*types.Block, error) {
	// Decode transactions.
	txs := make([]*types.Transaction, len(payload.Transactions))
	for i, enc := range payload.Transactions {
		tx, err := types.DecodeTxRLP(enc)
		if err != nil {
			return nil, fmt.Errorf("invalid transaction %d: %w", i, err)
		}
		txs = append(txs[:i], tx)
	}

	// Compute transactions root from decoded transactions.
	// This matches the CL's ordered_trie_root computation.
	txsRoot := block.DeriveTxsRoot(txs)

	// Convert withdrawals.
	var withdrawals []*types.Withdrawal
	if payload.Withdrawals != nil {
		withdrawals = WithdrawalsToCore(payload.Withdrawals)
	}

	// Compute withdrawals root.
	// This matches the CL's ordered_trie_root computation for withdrawals.
	var withdrawalsHash *types.Hash
	if payload.Withdrawals != nil {
		wr := block.DeriveWithdrawalsRoot(withdrawals)
		withdrawalsHash = &wr
	}

	// Build header with all required fields for correct block hash.
	blobGasUsed := payload.BlobGasUsed
	excessBlobGas := payload.ExcessBlobGas
	header := &types.Header{
		ParentHash:    payload.ParentHash,
		UncleHash:     types.EmptyUncleHash,
		Coinbase:      payload.FeeRecipient,
		Root:          payload.StateRoot,
		TxHash:        txsRoot, // Computed from transactions
		ReceiptHash:   payload.ReceiptsRoot,
		Bloom:         payload.LogsBloom,
		Difficulty:    new(big.Int),
		Number:        new(big.Int).SetUint64(payload.BlockNumber),
		GasLimit:      payload.GasLimit,
		GasUsed:       payload.GasUsed,
		Time:          payload.Timestamp,
		Extra:         payload.ExtraData,
		MixDigest:     payload.PrevRandao,
		BaseFee:       payload.BaseFeePerGas,
		BlobGasUsed:   &blobGasUsed,
		ExcessBlobGas: &excessBlobGas,
		// Post-Shanghai fields
		WithdrawalsHash: withdrawalsHash,
	}

	// Post-Cancun fields (EIP-4788): only set if non-zero.
	// Per consensus spec, parent_beacon_block_root is optional in the header
	// and should only be included when present (non-zero).
	if parentBeaconBlockRoot != (types.Hash{}) {
		header.ParentBeaconRoot = &parentBeaconBlockRoot
	}

	return types.NewBlock(header, &types.Body{
		Transactions: txs,
		Withdrawals:  withdrawals,
	}), nil
}

// restoreCalldataGasFields recomputes CalldataGasUsed and CalldataExcessGas
// for a block received via the engine API. These fields are part of the
// EIP-7706 header but are not transmitted in ExecutionPayloadV5. By deriving
// them from block transactions and parent state we restore the original block
// hash so the parent chain tracks calldata excess gas correctly (SPEC-6.4).
// If the recomputed block hash matches payloadBlockHash the augmented block is
// returned; otherwise the original block is returned unchanged.
func restoreCalldataGasFields(block *types.Block, parent *types.Block, payloadBlockHash types.Hash) *types.Block {
	// Sum calldata gas across all transactions.
	calldataGasUsed := uint64(0)
	for _, tx := range block.Transactions() {
		calldataGasUsed += tx.CalldataGas()
	}
	// Derive excess gas from parent state (defaults to 0 if parent lacks fields).
	parentExcess, parentUsed := uint64(0), uint64(0)
	if parent != nil {
		if parent.Header().CalldataExcessGas != nil {
			parentExcess = *parent.Header().CalldataExcessGas
		}
		if parent.Header().CalldataGasUsed != nil {
			parentUsed = *parent.Header().CalldataGasUsed
		}
	}
	calldataExcessGas := gas.CalcCalldataExcessGas(parentExcess, parentUsed, block.Header().GasLimit)

	// Rebuild the block with the calldata gas fields injected into the header.
	hdr := block.Header()
	hdr.CalldataGasUsed = &calldataGasUsed
	hdr.CalldataExcessGas = &calldataExcessGas
	augmented := types.NewBlock(hdr, block.Body())

	// Only use the augmented block if its hash matches what the builder produced.
	// This protects against incorrect recomputation.
	if payloadBlockHash == (types.Hash{}) || augmented.Hash() == payloadBlockHash {
		return augmented
	}
	return block
}

// ProcessBlockV5 validates and executes an Amsterdam payload with BAL validation.
// NOTE: EIP-8141 FrameTx receipts are included in the standard receipt array.
// The FrameTxReceipt type (core/types/frame_receipt.go) provides per-frame
// results; however, gas accounting flows through the standard Receipt structure
// so no special handling is needed here.
func (b *EngineBackend) ProcessBlockV5(
	payload *ExecutionPayloadV5,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (PayloadStatusV1, error) {
	return b.processRequestPayload(payload, parentBeaconBlockRoot, executionRequests, true)
}

func (b *EngineBackend) processRequestPayload(
	payload *ExecutionPayloadV5, parentBeaconBlockRoot types.Hash, executionRequests [][]byte, withBAL bool,
) (PayloadStatusV1, error) {
	if err := types.ValidateExecutionRequestsWire(executionRequests); err != nil {
		return PayloadStatusV1{}, ErrInvalidParams
	}
	version := "V4"
	if withBAL {
		version = "V5"
	}
	metrics.EngineNewPayload.Inc()
	backendLog.Debug("payload_received",
		"event", "payload_received",
		"version", version,
		"blockHash", payload.BlockHash.Hex(),
		"blockNum", payload.BlockNumber,
		"parentHash", payload.ParentHash.Hex(),
		"txCount", len(payload.Transactions),
		"gasUsed", payload.GasUsed,
		"timestamp", payload.Timestamp,
	)

	// First, process the block through the standard path.
	blk, err := payloadToBlock(&payload.ExecutionPayloadV3, parentBeaconBlockRoot)
	if err != nil {
		errMsg := err.Error()
		backendLog.Warn("payload_invalid",
			"event", "payload_invalid",
			"version", version,
			"blockHash", payload.BlockHash.Hex(),
			"reason", "payload decode failed",
			"error", errMsg,
		)
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	// P1: Use fine-grained locks instead of single b.mu.
	// Check that the parent exists.
	parentHash := blk.ParentHash()
	parentBlock, parentOk := b.getBlock(parentHash)

	if !parentOk {
		backendLog.Debug("payload_syncing",
			"event", "payload_syncing",
			"version", version,
			"blockHash", payload.BlockHash.Hex(),
			"parentHash", parentHash.Hex(),
		)
		return PayloadStatusV1{Status: StatusSyncing}, nil
	}

	header := blk.Header()
	if b.config.IsCancun(header.Time) {
		header.ParentBeaconRoot = &parentBeaconBlockRoot
	}
	if b.config.IsPrague(header.Time) {
		hash := types.ComputeWireRequestsHash(executionRequests)
		header.RequestsHash = &hash
	}
	if withBAL && b.config.IsAmsterdam(header.Time) {
		hash, err := enginepayload.DecodeBlockAccessList(payload.BlockAccessList)
		if err != nil {
			message := err.Error()
			return PayloadStatusV1{Status: StatusInvalid, ValidationError: &message}, nil
		}
		header.BlockAccessListHash = &hash
	}
	blk = types.NewBlock(header, blk.Body())

	// SPEC-6.4: restore CalldataGasUsed/CalldataExcessGas stripped by the
	// engine API wire format. These fields are part of the header hash
	// (EIP-7706) but not included in ExecutionPayloadV5. We recompute them
	// from the block's transactions and the parent's calldata gas state.
	if b.config != nil && b.config.IsGlamsterdan(blk.Header().Time) {
		blk = restoreCalldataGasFields(blk, parentBlock, payload.BlockHash)
	}

	if blk.Hash() != payload.BlockHash {
		message := "payload block hash does not match reconstructed header"
		return PayloadStatusV1{Status: StatusInvalid, ValidationError: &message}, nil
	}
	if _, known := b.getBlock(blk.Hash()); known {
		hash := blk.Hash()
		return PayloadStatusV1{Status: StatusValid, LatestValidHash: &hash}, nil
	}

	if payload.Timestamp <= parentBlock.Time() {
		message := fmt.Sprintf("invalid timestamp: block %d <= parent %d", payload.Timestamp, parentBlock.Time())
		return PayloadStatusV1{Status: StatusInvalid, LatestValidHash: &parentHash, ValidationError: &message}, nil
	}

	// Run through the state processor with BAL computation.
	stateCopy := b.statedb.Dup()
	result, err := b.processor.ProcessWithBAL(blk, stateCopy)
	if err != nil {
		errMsg := fmt.Sprintf("state processing failed: %v", err)
		backendLog.Error("payload_exec_fail",
			"event", "payload_exec_fail",
			"version", version,
			"blockHash", payload.BlockHash.Hex(),
			"blockNum", payload.BlockNumber,
			"error", err,
		)
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	if expected := blk.Header().BlockAccessListHash; expected != nil {
		if result.BlockAccessList == nil || result.BlockAccessList.Hash() != *expected {
			message := "block access list does not match execution"
			return PayloadStatusV1{Status: StatusInvalid, ValidationError: &message}, nil
		}
	}
	if err := types.ValidateRequestsHash(blk.Header(), result.Requests); err != nil {
		message := err.Error()
		return PayloadStatusV1{Status: StatusInvalid, LatestValidHash: &parentHash, ValidationError: &message}, nil
	}

	// EIP-7805: check IL satisfaction against block and stored ILs.
	ilsLen := b.getInclusionListCount()
	if ilsLen > 0 {
		ils := b.ilsAsFocil()
		gasRemaining := blk.GasLimit() - blk.GasUsed()
		if result := focilCheckILSatisfaction(blk, ils, gasRemaining); !result {
			errMsg := focil.InclusionListUnsatisfied
			backendLog.Warn("payload_il_unsatisfied",
				"event", "payload_il_unsatisfied",
				"blockHash", payload.BlockHash.Hex(),
				"blockNum", payload.BlockNumber,
			)
			return PayloadStatusV1{
				Status:          StatusInclusionListUnsatisfied,
				ValidationError: &errMsg,
			}, nil
		}
	}

	// Store the block and update state (evict old blocks to bound memory).
	blockHash := blk.Hash()
	b.storeBlock(blk, result.BlockAccessList)
	b.statedb = stateCopy

	// Dual-write to actors for migration.
	if err := b.storeActorBlock(blockHash, blk, result.BlockAccessList); err != nil {
		backendLog.Debug("actor_block_store_failed", "error", err)
	}

	// ILs are slot-scoped: once a block is accepted, the ILs for that slot
	// are consumed. Clear them to prevent unbounded growth across slots.
	b.clearInclusionLists()

	// Clear actor ILs as well.
	if err := b.clearActorInclusionLists(); err != nil {
		backendLog.Debug("actor_il_clear_failed", "error", err)
	}

	backendLog.Info("payload_valid",
		"event", "payload_valid",
		"version", version,
		"blockHash", blockHash.Hex(),
		"blockNum", payload.BlockNumber,
		"txCount", len(payload.Transactions),
		"gasUsed", payload.GasUsed,
	)

	return PayloadStatusV1{
		Status:          StatusValid,
		LatestValidHash: &blockHash,
	}, nil
}

// ForkchoiceUpdatedV4 processes a forkchoice update with V4 payload attributes (Amsterdam).
// P1: Uses fine-grained locks for better concurrency.
func (b *EngineBackend) ForkchoiceUpdatedV4(
	fcState ForkchoiceStateV1,
	attrs *PayloadAttributesV4,
) (ForkchoiceUpdatedResult, error) {
	// Validate head block exists.
	if fcState.HeadBlockHash != (types.Hash{}) && !b.hasBlock(fcState.HeadBlockHash) {
		return ForkchoiceUpdatedResult{
			PayloadStatus: PayloadStatusV1{Status: StatusSyncing},
		}, nil
	}

	// Update forkchoice pointers (per spec: must NOT be rolled back on attribute errors).
	b.setForkchoiceState(fcState.HeadBlockHash, fcState.SafeBlockHash, fcState.FinalizedBlockHash)
	headHash := b.getHeadHash()

	// Dual-write to actors for migration.
	if err := b.setActorHeadHash(fcState.HeadBlockHash); err != nil {
		backendLog.Debug("actor_head_update_failed", "error", err)
	}
	if err := b.setActorSafeHash(fcState.SafeBlockHash); err != nil {
		backendLog.Debug("actor_safe_update_failed", "error", err)
	}
	if err := b.setActorFinalHash(fcState.FinalizedBlockHash); err != nil {
		backendLog.Debug("actor_final_update_failed", "error", err)
	}

	status := PayloadStatusV1{
		Status:          StatusValid,
		LatestValidHash: &headHash,
	}

	result := ForkchoiceUpdatedResult{PayloadStatus: status}

	if attrs != nil {
		if attrs.Timestamp == 0 {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}

		id := b.generatePayloadID(fcState.HeadBlockHash, attrs.Timestamp)

		parentBlock, _ := b.getBlock(fcState.HeadBlockHash)
		if parentBlock == nil {
			return ForkchoiceUpdatedResult{}, ErrInvalidForkchoiceState
		}

		// Validate that timestamp is greater than parent.
		if attrs.Timestamp <= parentBlock.Header().Time {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}

		builder := block.NewBlockBuilder(b.config, nil, b.payloadTxPool())
		builder.SetState(b.statedb.Dup())
		parentHeader := parentBlock.Header()

		blk, built, err := builder.BuildBlockWithResult(parentHeader, &block.BuildBlockAttributes{
			Timestamp:        attrs.Timestamp,
			FeeRecipient:     attrs.SuggestedFeeRecipient,
			Random:           attrs.PrevRandao,
			GasLimit:         parentHeader.GasLimit,
			Withdrawals:      WithdrawalsToCore(attrs.Withdrawals),
			BeaconRoot:       &attrs.ParentBeaconBlockRoot,
			InclusionListTxs: attrs.InclusionListTransactions,
		})
		if err != nil {
			return ForkchoiceUpdatedResult{}, fmt.Errorf("payload build failed: %w", err)
		}

		receipts, blockBAL := built.Receipts, built.BlockAccessList

		b.storePayload(id, &pendingPayload{
			block:        blk,
			receipts:     receipts,
			requests:     built.Requests,
			bal:          blockBAL,
			blockValue:   new(big.Int),
			parentHash:   fcState.HeadBlockHash,
			timestamp:    attrs.Timestamp,
			feeRecipient: attrs.SuggestedFeeRecipient,
			prevRandao:   attrs.PrevRandao,
			withdrawals:  attrs.Withdrawals,
			slotNumber:   attrs.SlotNumber,
		})

		// Dual-write to actors for migration.
		actorPayload := &actor.PendingPayload{
			Block:        blk,
			Receipts:     receipts,
			Bal:          blockBAL,
			BlockValue:   new(big.Int),
			ParentHash:   fcState.HeadBlockHash,
			Timestamp:    attrs.Timestamp,
			FeeRecipient: attrs.SuggestedFeeRecipient,
			PrevRandao:   attrs.PrevRandao,
			Withdrawals:  withdrawalsToActor(attrs.Withdrawals),
		}
		if err := b.storeActorPayload(id, actorPayload); err != nil {
			backendLog.Debug("actor_payload_store_failed", "error", err)
		}

		result.PayloadID = &id
	}

	return result, nil
}

// GetPayloadV4ByID retrieves a previously built payload for getPayloadV4 (Prague).
func (b *EngineBackend) GetPayloadV4ByID(id PayloadID) (*GetPayloadV4Response, error) {
	asyncPending, result, _, err := b.waitAsyncPayload(id, false)
	if err != nil {
		return nil, err
	}
	if asyncPending != nil && result != nil {
		engineWithdrawals := enginepayload.WithdrawalsToEngine(asyncPending.Withdrawals)
		ep4 := enginepayload.BlockToPayload(result.Block, asyncPending.PrevRandao, engineWithdrawals)
		return &GetPayloadV4Response{
			ExecutionPayload:  &ep4.ExecutionPayloadV3,
			BlockValue:        result.BlockValue,
			BlobsBundle:       collectBlobsBundleV1(result.Block.Transactions()),
			ExecutionRequests: result.Requests.Encode(),
		}, nil
	}

	pending, err := b.getStoredPayload(id)
	if err != nil {
		return nil, err
	}

	ep4 := enginepayload.BlockToPayload(pending.block, pending.prevRandao, pending.withdrawals)

	return &GetPayloadV4Response{
		ExecutionPayload:  &ep4.ExecutionPayloadV3,
		BlockValue:        new(big.Int).Set(pending.blockValue),
		BlobsBundle:       collectBlobsBundleV1(pending.block.Transactions()),
		ExecutionRequests: pending.requests.Encode(),
	}, nil
}

// GetPayloadV5 retrieves a previously built payload for getPayloadV5 (Gloas/Heze).
// Gloas payloads carry BAL + slotNumber fields (lighthouse >= 8.4 requires them).
// Implements GlamsterdamBackend interface.
func (b *EngineBackend) GetPayloadV5(id PayloadID) (*GetPayloadV5Response, error) {
	asyncPending, result, _, err := b.waitAsyncPayload(id, false)
	if err != nil {
		return nil, err
	}
	if asyncPending != nil && result != nil {
		engineWithdrawals := enginepayload.WithdrawalsToEngine(asyncPending.Withdrawals)
		ep5 := enginepayload.BlockToPayloadV5(result.Block, asyncPending.PrevRandao, engineWithdrawals, result.BAL, asyncPending.SlotNumber)
		return &GetPayloadV5Response{
			ExecutionPayload:  ep5,
			BlockValue:        result.BlockValue,
			BlobsBundle:       collectBlobsBundleV2(result.Block.Transactions()),
			Override:          false,
			ExecutionRequests: result.Requests.Encode(),
		}, nil
	}

	pending, err := b.getStoredPayload(id)
	if err != nil {
		return nil, err
	}

	ep5 := enginepayload.BlockToPayloadV5(pending.block, pending.prevRandao, pending.withdrawals, pending.bal, pending.slotNumber)

	return &GetPayloadV5Response{
		ExecutionPayload:  ep5,
		BlockValue:        pending.blockValue,
		BlobsBundle:       collectBlobsBundleV2(pending.block.Transactions()),
		Override:          false,
		ExecutionRequests: pending.requests.Encode(),
	}, nil
}

// GetPayloadV6ByID retrieves a previously built payload for getPayloadV6 (Amsterdam).
// NOTE: EIP-8141 FrameTx receipts use the standard Receipt structure for gas
// accounting. Per-frame results are available via FrameTxReceipt if needed by
// downstream consumers (e.g., block explorers).
func (b *EngineBackend) GetPayloadV6ByID(id PayloadID) (*GetPayloadV6Response, error) {
	asyncPending, result, _, err := b.waitAsyncPayload(id, false)
	if err != nil {
		return nil, err
	}
	if asyncPending != nil && result != nil {
		engineWithdrawals := enginepayload.WithdrawalsToEngine(asyncPending.Withdrawals)
		ep5 := enginepayload.BlockToPayloadV5(result.Block, asyncPending.PrevRandao, engineWithdrawals, result.BAL, asyncPending.SlotNumber)
		return &GetPayloadV6Response{
			ExecutionPayload:  ep5,
			BlockValue:        result.BlockValue,
			BlobsBundle:       collectBlobsBundle(result.Block.Transactions()),
			ExecutionRequests: result.Requests.Encode(),
		}, nil
	}

	pending, err := b.getStoredPayload(id)
	if err != nil {
		return nil, err
	}

	ep5 := enginepayload.BlockToPayloadV5(pending.block, pending.prevRandao, pending.withdrawals, pending.bal, pending.slotNumber)

	return &GetPayloadV6Response{
		ExecutionPayload:  ep5,
		BlockValue:        new(big.Int).Set(pending.blockValue),
		BlobsBundle:       collectBlobsBundle(pending.block.Transactions()),
		ExecutionRequests: pending.requests.Encode(),
	}, nil
}

// GetPayloadV7ByID retrieves a previously built payload for getPayloadV7.
// When EIP-7807 SSZ blocks are active at the payload timestamp, the response
// additionally carries the SSZ payload encoding and its hash tree root — the
// SSZ execution block hash the CL commits to.
func (b *EngineBackend) GetPayloadV7ByID(id PayloadID) (*GetPayloadV6Response, error) {
	resp, err := b.GetPayloadV6ByID(id)
	if err != nil {
		return nil, err
	}
	ep5 := resp.ExecutionPayload
	if ep5 == nil || !b.config.IsSSZBlock(ep5.Timestamp) {
		return resp, nil
	}
	asyncPending, result, release, err := b.waitAsyncPayload(id, false)
	if err != nil {
		return resp, nil // hash unavailable without the built block; hash fields stay unset
	}
	if release != nil {
		defer release()
	}
	var block *types.Block
	var receipts []*types.Receipt
	var requests types.Requests
	var balRLP []byte
	var slot uint64
	if asyncPending != nil && result != nil {
		block, receipts, requests, slot = result.Block, result.Receipts, result.Requests, asyncPending.SlotNumber
		if result.BAL != nil {
			balRLP, _ = result.BAL.EncodeRLP()
		}
	} else if pending, perr := b.getStoredPayload(id); perr == nil {
		block, receipts, requests, slot = pending.block, pending.receipts, pending.requests, pending.slotNumber
		if pending.bal != nil {
			balRLP, _ = pending.bal.EncodeRLP()
		}
	}
	if block == nil || block.Header().BaseFee == nil {
		return resp, nil
	}
	excessRegular := uint64(0)
	if block.Header().CalldataExcessGas != nil {
		excessRegular = *block.Header().CalldataExcessGas
	}
	ctx := types.ExecutionSSZContext{
		BlobGasLimit:     gas.FusakaMaxBlobGasPerBlock,
		BlobBaseFee:      gas.CalcBlobBaseFeeV2(*block.Header().ExcessBlobGas, block.Header().BaseFee),
		ExcessRegularGas: excessRegular,
		Requests:         requests,
		BlockAccessList:  balRLP,
		SlotNumber:       slot,
	}
	payloadSSZ, err := types.NewExecutionPayloadSSZ(block, receipts, ctx)
	if err != nil {
		return resp, nil
	}
	enc, err := payloadSSZ.MarshalSSZ()
	if err != nil {
		return resp, nil
	}
	root, err := payloadSSZ.HashTreeRoot()
	if err != nil {
		return resp, nil
	}
	resp.SSZPayload = json.RawMessage(`"` + "0x" + hex.EncodeToString(enc) + `"`)
	resp.SSZBlockHash = &root
	return resp, nil
}

func collectBlobSidecars(txs []*types.Transaction) (blobs, commitments [][]byte, sidecarBlobs [][]byte) {
	for _, tx := range txs {
		sc := tx.BlobSidecar()
		if sc == nil {
			continue
		}
		blobs = append(blobs, sc.Blobs...)
		commitments = append(commitments, sc.Commitments...)
		sidecarBlobs = append(sidecarBlobs, sc.Blobs...)
	}
	return blobs, commitments, sidecarBlobs
}

func expandBlobCellProofs(blobs [][]byte, warnLabel string) ([][]byte, int) {
	if len(blobs) == 0 {
		return nil, 0
	}

	kzg := bls.DefaultKZGBackend()
	proofs := make([][]byte, 0, len(blobs)*bls.KZGCellsPerExtBlob)
	totalProofs := 0
	for _, blob := range blobs {
		_, cellProofs, err := kzg.ComputeCellsAndProofs(blob)
		if err != nil {
			backendLog.Warn(warnLabel,
				"err", err)
			for range bls.KZGCellsPerExtBlob {
				proofs = append(proofs, make([]byte, bls.KZGBytesPerProof))
				totalProofs++
			}
			continue
		}
		for _, p := range cellProofs {
			cp := p
			proofs = append(proofs, cp[:])
			totalProofs++
		}
	}
	return proofs, totalProofs
}

// collectBlobsBundle builds a BlobsBundleV2 from the blob sidecar data attached
// to the given transactions. Each blob's 48-byte KZG proof is expanded to 128
// per-cell KZG proofs using ComputeCellsAndProofs, as required by the Fulu/PeerDAS
// spec (engine_getPayloadV6 blobsBundle.proofs must have 128*N entries).
func collectBlobsBundle(txs []*types.Transaction) *BlobsBundleV2 {
	bundle := &BlobsBundleV2{}
	blobTxCount := 0
	totalProofs := 0
	for _, tx := range txs {
		if tx.BlobSidecar() != nil {
			blobTxCount++
		}
	}
	blobs, commitments, sidecarBlobs := collectBlobSidecars(txs)
	bundle.Blobs = blobs
	bundle.Commitments = commitments
	totalBlobs := len(sidecarBlobs)
	bundle.Proofs, totalProofs = expandBlobCellProofs(sidecarBlobs, "collectBlobsBundle: ComputeCellsAndProofs failed, using zero proofs")
	backendLog.Debug("collectBlobsBundle: result",
		"blobTxCount", blobTxCount,
		"totalBlobs", totalBlobs,
		"totalProofs", totalProofs,
		"expectedProofs", totalBlobs*bls.KZGCellsPerExtBlob,
	)
	return bundle
}

// collectBlobsBundleV1 collects blob sidecars with one proof per blob (V1 format).
// Used for engine_getPayloadV3 and engine_getPayloadV4 responses.
func collectBlobsBundleV1(txs []*types.Transaction) *BlobsBundleV1 {
	bundle := &BlobsBundleV1{}
	for _, tx := range txs {
		sc := tx.BlobSidecar()
		if sc == nil {
			continue
		}
		bundle.Blobs = append(bundle.Blobs, sc.Blobs...)
		bundle.Commitments = append(bundle.Commitments, sc.Commitments...)
		bundle.Proofs = append(bundle.Proofs, sc.Proofs...)
	}
	return bundle
}

// collectBlobsBundleV2 collects blob sidecars with cell proofs (V2 format).
// Used for engine_getPayloadV5 responses (Gloas/Heze fork).
// Each blob's 48-byte KZG proof is expanded to 128 per-cell KZG proofs
// using ComputeCellsAndProofs, as required by the Fulu/PeerDAS spec.
func collectBlobsBundleV2(txs []*types.Transaction) *enginepayload.BlobsBundleV2 {
	bundle := &enginepayload.BlobsBundleV2{}
	blobs, commitments, sidecarBlobs := collectBlobSidecars(txs)
	bundle.Blobs = blobs
	bundle.Commitments = commitments
	bundle.Proofs, _ = expandBlobCellProofs(sidecarBlobs, "collectBlobsBundleV2: ComputeCellsAndProofs failed, using zero proofs")
	backendLog.Debug("collectBlobsBundleV2: result",
		"blobCount", len(blobs),
		"commitmentCount", len(commitments),
		"proofCount", len(bundle.Proofs),
		"expectedProofs", len(sidecarBlobs)*bls.KZGCellsPerExtBlob,
	)
	return bundle
}

// IsPrague returns true if the given timestamp falls within the Prague fork.
func (b *EngineBackend) IsPrague(timestamp uint64) bool {
	return b.config.IsPrague(timestamp)
}

// IsAmsterdam returns true if the given timestamp falls within the Amsterdam fork.
func (b *EngineBackend) IsAmsterdam(timestamp uint64) bool {
	return b.config.IsAmsterdam(timestamp)
}

// ProcessInclusionList validates and stores a new inclusion list from the CL.
// Implements InclusionListBackend.
// P1: Uses fine-grained lock for IL storage.
func (b *EngineBackend) ProcessInclusionList(il *types.InclusionList) error {
	b.addInclusionList(il)

	// Dual-write to actors for migration.
	if err := b.storeActorInclusionList(il); err != nil {
		backendLog.Debug("actor_il_store_failed", "error", err)
	}

	return nil
}

// GetInclusionList generates an inclusion list from the mempool.
// Implements InclusionListBackend.
// EIP-7805: Select transactions up to MAX_BYTES_PER_INCLUSION_LIST (8 KiB).
func (b *EngineBackend) GetInclusionList() *types.InclusionList {
	// If no txpool is wired, return empty IL.
	pool := b.payloadTxPool()
	if pool == nil {
		return &types.InclusionList{Transactions: [][]byte{}}
	}

	selected, totalSize := eips.SelectTransactionsForInclusionList(pool.Pending(), eips.MaxBytesPerInclusionList)

	backendLog.Debug("getInclusionList", "tx_count", len(selected), "total_bytes", totalSize)

	return &types.InclusionList{Transactions: selected}
}

// SetTxPool wires a transaction pool reader for inclusion list generation
// AND payload building (the async builder consumes pending txs per build).
func (b *EngineBackend) SetTxPool(pool block.TxPoolReader) {
	b.txPoolMu.Lock()
	defer b.txPoolMu.Unlock()
	b.txPool = pool
	if b.asyncBuilder != nil {
		b.asyncBuilder.SetTxPool(pool)
	}
}

func (b *EngineBackend) payloadTxPool() block.TxPoolReader {
	b.txPoolMu.RLock()
	defer b.txPoolMu.RUnlock()
	return b.txPool
}

// ilsAsFocil converts stored types.InclusionList entries to focil.InclusionList format.
// P1: Caller must hold ilMu or ensure thread-safe access.
func (b *EngineBackend) ilsAsFocil() []*focil.InclusionList {
	b.ilMu.RLock()
	defer b.ilMu.RUnlock()

	result := make([]*focil.InclusionList, len(b.ils))
	for i, il := range b.ils {
		entries := make([]focil.InclusionListEntry, len(il.Transactions))
		for j, tx := range il.Transactions {
			entries[j] = focil.InclusionListEntry{Transaction: tx, Index: uint64(j)}
		}
		result[i] = &focil.InclusionList{
			Slot:          il.Slot,
			ProposerIndex: il.ValidatorIndex,
			CommitteeRoot: il.CommitteeRoot,
			Entries:       entries,
		}
	}
	return result
}

// SetSlasher wires a PaymasterSlasher into the state processor so the
// processor can slash paymasters on bad settlement (AA EIP-7701).
func (b *EngineBackend) SetSlasher(s coreconfig.PaymasterSlasher) {
	b.processor.SetSlasher(s)
}

// focilCheckILSatisfaction wraps focil.CheckILSatisfaction for use in engine_newPayload.
func focilCheckILSatisfaction(block *types.Block, ils []*focil.InclusionList, gasRemaining uint64) bool {
	return focil.CheckILSatisfaction(block, ils, nil, gasRemaining) == focil.ILSatisfied
}

// --- Actor-based accessor methods (Phase 6 of engine-channel-refactor) ---
// These methods use actors for state access, providing lock-free concurrent access.
// They can be used alongside mutex-protected methods during migration.

// getActorBlock retrieves a block using the actor backend.
func (b *EngineBackend) getActorBlock(hash types.Hash) (*types.Block, error) {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return nil, fmt.Errorf("actors not initialized")
	}
	return actors.GetBlockByHash(hash, b.actorTimeout)
}

// getActorBlockByNumber retrieves a block by number using the actor backend.
func (b *EngineBackend) getActorBlockByNumber(num uint64) (*types.Block, error) {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return nil, fmt.Errorf("actors not initialized")
	}
	return actors.GetBlockByNumber(num, b.actorTimeout)
}

// getActorHeadHash retrieves the head hash using the actor backend.
func (b *EngineBackend) getActorHeadHash() (types.Hash, error) {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return types.Hash{}, fmt.Errorf("actors not initialized")
	}
	return actors.GetHeadHash(b.actorTimeout)
}

// setActorHeadHash sets the head hash using the actor backend.
func (b *EngineBackend) setActorHeadHash(hash types.Hash) error {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return fmt.Errorf("actors not initialized")
	}
	return actors.SetHeadHash(hash, b.actorTimeout)
}

// getActorSafeHash retrieves the safe hash using the actor backend.
func (b *EngineBackend) getActorSafeHash() (types.Hash, error) {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return types.Hash{}, fmt.Errorf("actors not initialized")
	}
	return actors.GetSafeHash(b.actorTimeout)
}

// setActorSafeHash sets the safe hash using the actor backend.
func (b *EngineBackend) setActorSafeHash(hash types.Hash) error {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return fmt.Errorf("actors not initialized")
	}
	return actors.SetSafeHash(hash, b.actorTimeout)
}

// getActorFinalHash retrieves the finalized hash using the actor backend.
func (b *EngineBackend) getActorFinalHash() (types.Hash, error) {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return types.Hash{}, fmt.Errorf("actors not initialized")
	}
	return actors.GetFinalHash(b.actorTimeout)
}

// setActorFinalHash sets the finalized hash using the actor backend.
func (b *EngineBackend) setActorFinalHash(hash types.Hash) error {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return fmt.Errorf("actors not initialized")
	}
	return actors.SetFinalHash(hash, b.actorTimeout)
}

// storeActorBlock stores a block using the actor backend.
func (b *EngineBackend) storeActorBlock(hash types.Hash, blk *types.Block, bal *bal.BlockAccessList) error {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return fmt.Errorf("actors not initialized")
	}
	return actors.StoreBlock(hash, blk, bal, b.actorTimeout)
}

// getActorBodiesByRange retrieves block bodies by range using the actor backend.
func (b *EngineBackend) getActorBodiesByRange(start, count uint64) ([]*actor.BlockBody, error) {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return nil, fmt.Errorf("actors not initialized")
	}
	return actors.GetBodiesByRange(start, count, b.actorTimeout)
}

// getActorPayload retrieves a pending payload using the actor backend.
func (b *EngineBackend) getActorPayload(id PayloadID) (*actor.PendingPayload, error) {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return nil, fmt.Errorf("actors not initialized")
	}
	return actors.GetPayload(id, b.actorTimeout)
}

// storeActorPayload stores a pending payload using the actor backend.
func (b *EngineBackend) storeActorPayload(id PayloadID, p *actor.PendingPayload) error {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return fmt.Errorf("actors not initialized")
	}
	return actors.StorePayload(id, p, b.actorTimeout)
}

// storeActorInclusionList stores an inclusion list using the actor backend.
func (b *EngineBackend) storeActorInclusionList(il *types.InclusionList) error {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return fmt.Errorf("actors not initialized")
	}
	return actors.StoreInclusionList(il, b.actorTimeout)
}

// getActorInclusionLists retrieves all inclusion lists using the actor backend.
func (b *EngineBackend) getActorInclusionLists() ([]*types.InclusionList, error) {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return nil, fmt.Errorf("actors not initialized")
	}
	return actors.GetAllInclusionLists(b.actorTimeout)
}

// clearActorInclusionLists clears all inclusion lists using the actor backend.
func (b *EngineBackend) clearActorInclusionLists() error {
	b.actorMu.RLock()
	actors := b.actors
	b.actorMu.RUnlock()
	if actors == nil {
		return fmt.Errorf("actors not initialized")
	}
	return actors.ClearInclusionLists(b.actorTimeout)
}

// withdrawalsToActor converts engine Withdrawals to actor Withdrawals.
func withdrawalsToActor(ws []*Withdrawal) []*actor.Withdrawal {
	if ws == nil {
		return nil
	}
	result := make([]*actor.Withdrawal, len(ws))
	for i, w := range ws {
		if w != nil {
			result[i] = &actor.Withdrawal{
				Index:          w.Index,
				ValidatorIndex: w.ValidatorIndex,
				Address:        w.Address,
				Amount:         w.Amount,
			}
		}
	}
	return result
}

// Verify interface compliance at compile time.
var _ Backend = (*EngineBackend)(nil)
