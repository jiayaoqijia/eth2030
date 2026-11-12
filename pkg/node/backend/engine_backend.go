package backend

import (
	"errors"
	"log/slog"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/eips"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/mev"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/engine"
	"github.com/eth2030/eth2030/engine/forkchoice"
	"github.com/eth2030/eth2030/engine/payload"
	"github.com/eth2030/eth2030/engine/vhash"
	"github.com/eth2030/eth2030/txpool"
)

// pendingPayload stores a built payload for later retrieval.
type pendingPayload struct {
	block    *types.Block
	receipts []*types.Receipt
	result   *execution.ProcessResult
	slot     uint64
	err      error
	done     chan struct{}
}

// blockProcReq is a work item for the block processor goroutine.
type blockProcReq struct {
	payload               *payload.ExecutionPayloadV3
	parentBeaconBlockRoot types.Hash
	requestsHash          *types.Hash
	replyCh               chan blockProcResp
}

// blockProcResp carries the result of a processed block.
type blockProcResp struct {
	status payload.PayloadStatusV1
	err    error
}

var errInclusionListStorageUnsupported = errors.New("inclusion list storage not wired in node backend")

const (
	fcuCacheSize       = 8   // Number of FCU entries to cache
	defaultMaxPayloads = 32  // Default number of payloads to cache
	processChSize      = 8   // Buffer size for block processing channel
	blobCacheMaxSize   = 256 // Maximum number of blobs to cache for getBlobsV2
)

// fcuCacheEntry records a (head,safe,finalized) triple from a processed FCU.
type fcuCacheEntry struct {
	head      types.Hash
	safe      types.Hash
	finalized types.Hash
}

// postFCUWork carries slow state-update work deferred from ForkchoiceUpdated.
type postFCUWork struct {
	fcState    payload.ForkchoiceStateV1
	headBlock  *types.Block
	finalBlock *types.Block
	safeBlock  *types.Block
	hasAttrs   bool
}

// EngineBackend adapts NodeDeps to the engine.Backend interface.
type EngineBackend struct {
	node NodeDeps

	mu           sync.Mutex
	payloads     map[payload.PayloadID]*pendingPayload
	payloadOrder []payload.PayloadID
	maxPayloads  int
	builder      *block.BlockBuilder
	buildMu      sync.Mutex

	fcMu          sync.RWMutex
	safeHash      types.Hash
	finalizedHash types.Hash

	processCh chan blockProcReq
	stopCh    chan struct{}
	closeOnce sync.Once
	workers   sync.WaitGroup

	fcuCache   [fcuCacheSize]fcuCacheEntry
	fcuCacheWr int
	fcuCacheMu sync.Mutex

	postFCUCh chan postFCUWork

	blobCache   map[types.Hash][]byte
	blobCacheMu sync.RWMutex
}

// txPoolReader wraps txpool.TxPool for the block builder.
type txPoolReader struct {
	pool      *txpool.TxPool
	mevConfig *mev.MEVProtectionConfig
}

func (r *txPoolReader) Pending() []*types.Transaction {
	return r.pool.PendingFlat()
}

// NewEngineBackend creates and starts the engine backend.
func NewEngineBackend(node NodeDeps) *EngineBackend {
	pool := &txPoolReader{pool: node.TxPool(), mevConfig: node.MEVConfig()}
	builder := block.NewBlockBuilder(node.Blockchain().Config(), node.Blockchain(), pool)
	maxPayloads := node.Config().CacheEnginePayloads
	if maxPayloads <= 0 {
		maxPayloads = defaultMaxPayloads
	}
	b := &EngineBackend{
		node:        node,
		payloads:    make(map[payload.PayloadID]*pendingPayload),
		maxPayloads: maxPayloads,
		builder:     builder,
		processCh:   make(chan blockProcReq, processChSize),
		stopCh:      make(chan struct{}),
		postFCUCh:   make(chan postFCUWork, 1),
		blobCache:   make(map[types.Hash][]byte),
	}
	b.workers.Add(2)
	go func() { defer b.workers.Done(); b.processLoop() }()
	go func() { defer b.workers.Done(); b.postFCULoop() }()
	return b
}

// processLoop is the dedicated block processor goroutine.
func (b *EngineBackend) processLoop() {
	for {
		select {
		case <-b.stopCh:
			return
		case req := <-b.processCh:
			status, err := b.processBlockInternal(req.payload, req.parentBeaconBlockRoot, req.requestsHash, nil)
			req.replyCh <- blockProcResp{status: status, err: err}
		}
	}
}

// Close stops the backend.
func (b *EngineBackend) Close() {
	b.closeOnce.Do(func() { close(b.stopCh) })
	b.workers.Wait()
}

// GetHeadHash returns the current head block hash.
func (b *EngineBackend) GetHeadHash() types.Hash {
	if blk := b.node.Blockchain().CurrentBlock(); blk != nil {
		return blk.Hash()
	}
	return types.Hash{}
}

// GetSafeHash returns the current safe block hash.
func (b *EngineBackend) GetSafeHash() types.Hash {
	if blk := b.node.Blockchain().CurrentSafeBlock(); blk != nil {
		return blk.Hash()
	}
	b.fcMu.RLock()
	defer b.fcMu.RUnlock()
	return b.safeHash
}

// GetFinalizedHash returns the current finalized block hash.
func (b *EngineBackend) GetFinalizedHash() types.Hash {
	bc := b.node.Blockchain()
	if bc == nil {
		slog.Warn("GetFinalizedHash: blockchain is nil")
		return types.Hash{}
	}
	if blk := bc.CurrentFinalBlock(); blk != nil {
		slog.Debug("GetFinalizedHash: returning from CurrentFinalBlock",
			"blockHash", blk.Hash(),
			"blockNum", blk.NumberU64(),
		)
		return blk.Hash()
	}
	b.fcMu.RLock()
	hash := b.finalizedHash
	b.fcMu.RUnlock()
	if hash == (types.Hash{}) {
		slog.Warn("GetFinalizedHash: no finalized block found",
			"currentHead", bc.CurrentBlock().NumberU64(),
		)
	} else {
		slog.Debug("GetFinalizedHash: returning from internal finalizedHash",
			"hash", hash,
		)
	}
	return hash
}

// GetHeadTimestamp returns the timestamp of the current head block.
func (b *EngineBackend) GetHeadTimestamp() uint64 {
	if blk := b.node.Blockchain().CurrentBlock(); blk != nil {
		return blk.Time()
	}
	return 0
}

// GetBlockTimestamp returns the timestamp of the block with the given hash.
func (b *EngineBackend) GetBlockTimestamp(hash types.Hash) uint64 {
	if blk := b.node.Blockchain().GetBlock(hash); blk != nil {
		return blk.Time()
	}
	return 0
}

// IsCancun returns true if the given timestamp falls within the Cancun fork.
func (b *EngineBackend) IsCancun(timestamp uint64) bool {
	return b.node.Blockchain().Config().IsCancun(timestamp)
}

// IsPrague returns true if the given timestamp falls within the Prague fork.
func (b *EngineBackend) IsPrague(timestamp uint64) bool {
	return b.node.Blockchain().Config().IsPrague(timestamp)
}

// IsAmsterdam returns true if the given timestamp falls within the Amsterdam fork.
func (b *EngineBackend) IsAmsterdam(timestamp uint64) bool {
	return b.node.Blockchain().Config().IsAmsterdam(timestamp)
}

// ProcessBlock validates and executes a new payload from the consensus layer.
func (b *EngineBackend) ProcessBlock(
	p *payload.ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
) (payload.PayloadStatusV1, error) {
	if len(expectedBlobVersionedHashes) > 0 {
		if err := vhash.VerifyAllBlobVersionBytes(expectedBlobVersionedHashes); err != nil {
			latestValid := p.ParentHash
			slog.Warn("engine_newPayload: invalid blob versioned hash version byte", "err", err)
			return payload.PayloadStatusV1{
				Status:          engine.StatusInvalid,
				LatestValidHash: &latestValid,
			}, nil
		}
	}
	return b.processBlockInternal(p, parentBeaconBlockRoot, nil, nil)
}

// ProcessBlockV4 validates and executes a Prague payload with execution requests.
func (b *EngineBackend) ProcessBlockV4(
	p *payload.ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (payload.PayloadStatusV1, error) {
	if err := types.ValidateExecutionRequestsWire(executionRequests); err != nil {
		return payload.PayloadStatusV1{}, engine.ErrInvalidParams
	}
	rHash := types.ComputeWireRequestsHash(executionRequests)
	return b.processBlockInternal(p, parentBeaconBlockRoot, &rHash, nil)
}

// ProcessBlockV5 validates and executes a new Amsterdam payload with BAL.
func (b *EngineBackend) ProcessBlockV5(
	p *payload.ExecutionPayloadV5,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (payload.PayloadStatusV1, error) {
	if err := types.ValidateExecutionRequestsWire(executionRequests); err != nil {
		return payload.PayloadStatusV1{}, engine.ErrInvalidParams
	}
	rHash := types.ComputeWireRequestsHash(executionRequests)
	balHash, err := payload.DecodeBlockAccessList(p.BlockAccessList)
	if err != nil {
		message := err.Error()
		return payload.PayloadStatusV1{Status: engine.StatusInvalid, ValidationError: &message}, nil
	}
	return b.processBlockInternal(&p.ExecutionPayloadV3, parentBeaconBlockRoot, &rHash, &balHash)
}

func (b *EngineBackend) processBlockInternal(
	p *payload.ExecutionPayloadV3,
	parentBeaconBlockRoot types.Hash,
	requestsHash *types.Hash,
	balHash *types.Hash,
) (payload.PayloadStatusV1, error) {
	bc := b.node.Blockchain()

	slog.Debug("engine_newPayload",
		"blockNumber", p.BlockNumber,
		"blockHash", p.BlockHash,
		"parentHash", p.ParentHash,
		"timestamp", p.Timestamp,
		"txCount", len(p.Transactions),
	)

	// Short-circuit for already-known blocks.
	if requestsHash == nil && balHash == nil && bc.HasBlock(p.BlockHash) {
		h := p.BlockHash
		slog.Debug("engine_newPayload: already known, returning VALID",
			"blockNumber", p.BlockNumber,
			"blockHash", p.BlockHash,
		)
		b.registerBlockInFCState(p)
		b.node.TxPool().SetForkRules(bc.Config(), bc.CurrentBlock().Time())
		b.node.TxPool().Reset(bc.State())
		return payload.PayloadStatusV1{Status: engine.StatusValid, LatestValidHash: &h}, nil
	}

	// Decode transactions from raw bytes.
	var txs []*types.Transaction
	for _, raw := range p.Transactions {
		tx, err := types.DecodeTxRLP(raw)
		if err != nil {
			latestValid := p.ParentHash
			return payload.PayloadStatusV1{
				Status:          engine.StatusInvalid,
				LatestValidHash: &latestValid,
			}, nil
		}
		txs = append(txs, tx)
	}

	// Decode withdrawals.
	var withdrawals []*types.Withdrawal
	if p.Withdrawals != nil {
		withdrawals = make([]*types.Withdrawal, 0, len(p.Withdrawals))
		for _, w := range p.Withdrawals {
			withdrawals = append(withdrawals, &types.Withdrawal{
				Index:          w.Index,
				ValidatorIndex: w.ValidatorIndex,
				Address:        w.Address,
				Amount:         w.Amount,
			})
		}
	}

	// Reconstruct the header.
	blobGasUsed := p.BlobGasUsed
	excessBlobGas := p.ExcessBlobGas
	header := &types.Header{
		ParentHash:    p.ParentHash,
		UncleHash:     types.EmptyUncleHash,
		Coinbase:      p.FeeRecipient,
		Root:          p.StateRoot,
		ReceiptHash:   p.ReceiptsRoot,
		Bloom:         p.LogsBloom,
		Difficulty:    new(big.Int),
		Number:        new(big.Int).SetUint64(p.BlockNumber),
		GasLimit:      p.GasLimit,
		GasUsed:       p.GasUsed,
		Time:          p.Timestamp,
		Extra:         p.ExtraData,
		BaseFee:       p.BaseFeePerGas,
		MixDigest:     p.PrevRandao,
		TxHash:        block.DeriveTxsRoot(txs),
		BlobGasUsed:   &blobGasUsed,
		ExcessBlobGas: &excessBlobGas,
	}

	// EIP-4788: set ParentBeaconRoot when provided (Cancun+).
	if parentBeaconBlockRoot != (types.Hash{}) {
		header.ParentBeaconRoot = &parentBeaconBlockRoot
	}

	// EIP-4895: compute WithdrawalsHash.
	if p.Withdrawals != nil {
		ws := withdrawals
		if ws == nil {
			ws = []*types.Withdrawal{}
		}
		wHash := block.DeriveWithdrawalsRoot(ws)
		header.WithdrawalsHash = &wHash
	}

	// EIP-7685: set RequestsHash for Prague blocks.
	if requestsHash != nil {
		header.RequestsHash = requestsHash
	}

	// EIP-7706: reconstruct CalldataExcessGas/CalldataGasUsed.
	if bc.Config().IsGlamsterdan(p.Timestamp) {
		parentBlock := bc.GetBlock(p.ParentHash)
		if parentBlock == nil {
			slog.Warn("engine_newPayload: parent block unavailable for EIP-7706, returning SYNCING",
				"blockNumber", p.BlockNumber,
				"parentHash", p.ParentHash,
			)
			return payload.PayloadStatusV1{Status: engine.StatusSyncing}, nil
		}
		ph := parentBlock.Header()
		var pCalldataExcess, pCalldataUsed uint64
		if ph.CalldataExcessGas != nil {
			pCalldataExcess = *ph.CalldataExcessGas
		}
		if ph.CalldataGasUsed != nil {
			pCalldataUsed = *ph.CalldataGasUsed
		}
		calldataExcessGas := gas.CalcCalldataExcessGas(pCalldataExcess, pCalldataUsed, ph.GasLimit)
		header.CalldataExcessGas = &calldataExcessGas
		var calldataGasUsed uint64
		for _, tx := range txs {
			calldataGasUsed += tx.CalldataGas()
		}
		header.CalldataGasUsed = &calldataGasUsed
	}

	header.BlockAccessListHash = balHash
	block := types.NewBlock(header, &types.Body{Transactions: txs, Withdrawals: withdrawals})
	if (requestsHash != nil || balHash != nil) && block.Hash() != p.BlockHash {
		message := "payload block hash does not match reconstructed header"
		return payload.PayloadStatusV1{Status: engine.StatusInvalid, ValidationError: &message}, nil
	}

	// Check if parent is known.
	slog.Debug("engine_newPayload: step2 checking parent",
		"blockNumber", p.BlockNumber,
		"parentHash", p.ParentHash,
	)
	if !bc.HasBlock(p.ParentHash) {
		slog.Debug("engine_newPayload: parent unknown, returning SYNCING",
			"parentHash", p.ParentHash,
		)
		return payload.PayloadStatusV1{Status: engine.StatusSyncing}, nil
	}

	// Insert the block.
	slog.Debug("engine_newPayload: step3 calling InsertBlock",
		"blockNumber", p.BlockNumber,
		"blockHash", p.BlockHash,
	)
	if err := bc.InsertBlock(block); err != nil {
		slog.Warn("engine_newPayload: insert failed", "err", err)
		latestValid := p.ParentHash
		message := err.Error()
		return payload.PayloadStatusV1{
			Status:          engine.StatusInvalid,
			LatestValidHash: &latestValid,
			ValidationError: &message,
		}, nil
	}

	// Cache blobs for engine_getBlobsV2.
	b.cacheBlobsFromBlock(block)

	// Sync txpool state.
	b.node.TxPool().SetForkRules(bc.Config(), bc.CurrentBlock().Time())
	b.node.TxPool().Reset(bc.State())

	// Notify gas oracle.
	if oracle := b.node.GasOracle(); oracle != nil {
		tips := ExtractBlockTips(txs, p.BaseFeePerGas)
		oracle.RecordBlock(p.BlockNumber, p.BaseFeePerGas, tips)
	}

	// Feed txpool gas-price suggestor.
	b.node.TxPool().RecordBlock(header, txs)

	// Register in forkchoice state manager.
	b.registerBlockInFCState(p)

	blockHash := block.Hash()
	slog.Info("engine_newPayload: accepted",
		"blockNumber", p.BlockNumber,
		"blockHash", blockHash,
	)
	return payload.PayloadStatusV1{
		Status:          engine.StatusValid,
		LatestValidHash: &blockHash,
	}, nil
}

// registerBlockInFCState adds the block to forkchoice state manager.
func (b *EngineBackend) registerBlockInFCState(p *payload.ExecutionPayloadV3) {
	if mgr := b.node.FCStateManager(); mgr != nil {
		bi := &forkchoice.BlockInfo{
			Hash:       p.BlockHash,
			ParentHash: p.ParentHash,
			Number:     p.BlockNumber,
			Slot:       p.BlockNumber,
		}
		mgr.AddBlock(bi)
	}
}

// cacheBlobsFromBlock caches blobs from a block for later retrieval.
// Limits cache size to blobCacheMaxSize to prevent unbounded memory growth.
func (b *EngineBackend) cacheBlobsFromBlock(blk *types.Block) {
	if blk == nil {
		return
	}
	txs := blk.Transactions()
	if len(txs) == 0 {
		return
	}

	b.blobCacheMu.Lock()
	defer b.blobCacheMu.Unlock()

	// Count new blobs to be added.
	newBlobs := 0
	for _, tx := range txs {
		if tx.BlobSidecar() != nil {
			newBlobs += len(tx.BlobHashes())
		}
	}

	// If cache would exceed limit, evict oldest entries (simple FIFO via random eviction).
	if len(b.blobCache)+newBlobs > blobCacheMaxSize {
		evictCount := (len(b.blobCache) + newBlobs) - blobCacheMaxSize/2
		if evictCount > 0 {
			for hash := range b.blobCache {
				delete(b.blobCache, hash)
				evictCount--
				if evictCount <= 0 {
					break
				}
			}
			slog.Debug("cacheBlobsFromBlock: evicted old entries",
				"remaining", len(b.blobCache),
				"newBlobs", newBlobs,
			)
		}
	}

	for _, tx := range txs {
		sidecar := tx.BlobSidecar()
		if sidecar == nil {
			continue
		}
		blobHashes := tx.BlobHashes()
		for i, hash := range blobHashes {
			if i < len(sidecar.Blobs) && len(sidecar.Blobs[i]) > 0 {
				b.blobCache[hash] = sidecar.Blobs[i]
			}
		}
	}
}

// GetInclusionList generates an inclusion list from the mempool.
// Implements InclusionListBackend for EIP-7805 (FOCIL).
// Selects transactions up to MAX_BYTES_PER_INCLUSION_LIST (8 KiB).
func (b *EngineBackend) GetInclusionList() *types.InclusionList {
	pool := b.node.TxPool()
	if pool == nil {
		return &types.InclusionList{Transactions: [][]byte{}}
	}

	selected, totalSize := eips.SelectTransactionsForInclusionList(pool.PendingFlat(), eips.MaxBytesPerInclusionList)

	slog.Debug("getInclusionList", "tx_count", len(selected), "total_bytes", totalSize)

	return &types.InclusionList{Transactions: selected}
}

// ProcessInclusionList validates and stores an inclusion list.
// Implements InclusionListBackend for EIP-7805 (FOCIL).
func (b *EngineBackend) ProcessInclusionList(il *types.InclusionList) error {
	// Basic validation: non-empty transactions.
	if len(il.Transactions) == 0 {
		return nil
	}

	// Log the received inclusion list.
	slog.Debug("processInclusionList", "slot", il.Slot, "validator_index", il.ValidatorIndex, "tx_count", len(il.Transactions))

	// The node backend does not yet wire incoming inclusion lists into a real
	// store or block-building path, so reject them instead of falsely
	// acknowledging successful processing.
	return errInclusionListStorageUnsupported
}
