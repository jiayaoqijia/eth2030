package chain

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/log"
	"github.com/eth2030/eth2030/metrics"
	"github.com/eth2030/eth2030/rlp"
)

var blockchainLog = log.Default().Module("eth/blockchain")

var (
	ErrNoGenesis     = errors.New("genesis block not provided")
	ErrGenesisExists = errors.New("genesis already initialized")
	ErrBlockNotFound = errors.New("block not found")
	ErrInvalidChain  = errors.New("invalid chain: blocks not contiguous")
	ErrFutureBlock2  = errors.New("block number too high")
	ErrStateNotFound = errors.New("state not found for block")
)

const (
	// defaultBlockCacheSize is the default in-memory block cache capacity.
	defaultBlockCacheSize = 256
	// defaultReceiptCacheSize is the default in-memory receipt cache capacity.
	defaultReceiptCacheSize = 128
)

// BlockchainOpts configures optional memory-cache sizes for a Blockchain.
// Zero values fall back to package defaults.
type BlockchainOpts struct {
	// BlockCacheSize is the maximum number of blocks kept in the memory cache.
	// Older entries are evicted; rawdb is the persistent fallback.
	BlockCacheSize int
	// ReceiptCacheSize is the maximum number of receipt sets kept in memory.
	ReceiptCacheSize int
	// StateCacheSize is the maximum number of MemoryStateDB snapshots retained
	// for fast reorg / payload-building (see state_cache.go).
	StateCacheSize int
}

// DefaultBlockchainOpts returns BlockchainOpts populated with package defaults.
func DefaultBlockchainOpts() BlockchainOpts {
	return BlockchainOpts{
		BlockCacheSize:   defaultBlockCacheSize,
		ReceiptCacheSize: defaultReceiptCacheSize,
		StateCacheSize:   defaultMaxCachedStates,
	}
}

// TxLookupEntry stores the location of a transaction within the chain.
type TxLookupEntry struct {
	BlockHash   types.Hash
	BlockNumber uint64
	TxIndex     uint64
}

// Blockchain manages the canonical chain of blocks, applying state
// transitions and persisting data to the underlying database.
type Blockchain struct {
	mu sync.RWMutex
	// rcMu guards receiptCache independently of mu, allowing concurrent
	// receipt reads while InsertBlock holds mu.Lock() for state execution.
	rcMu sync.RWMutex
	// insertMu serialises InsertBlock calls so that bc.mu is only held
	// briefly for cache reads/writes, not during expensive state execution.
	// This allows concurrent Engine API reads (HasBlock, GetBlock, etc.)
	// to proceed without blocking while a block is being processed.
	insertMu  sync.Mutex
	config    *config.ChainConfig
	db        rawdb.Database
	hc        *HeaderChain
	processor *execution.StateProcessor
	validator block.Validator

	opts BlockchainOpts // cache size limits, set at construction

	// Block cache: hash -> block.  Bounded to opts.BlockCacheSize entries;
	// older entries are evicted (rawdb is the persistent fallback).
	blockCache      map[types.Hash]*types.Block
	blockCacheOrder []types.Hash // insertion order for LRU eviction

	// Canonical number -> hash for quick lookups.
	canonCache map[uint64]types.Hash

	// Receipt cache: blockHash -> receipts.  Protected by rcMu.
	receiptCache      map[types.Hash][]*types.Receipt
	receiptCacheOrder []types.Hash // insertion order for LRU eviction (under rcMu)

	// Transaction lookup: txHash -> location in chain.
	// Entries are evicted together with their block from blockCache.
	txLookup map[types.Hash]TxLookupEntry

	// State snapshot cache to avoid re-execution from genesis.
	sc *stateCache

	// memSC is a permanent MemoryStateDB cache keyed by block hash.
	// Unlike sc (which is cleared on reorg because TrieStateDB entries share
	// the mutable db), memSC entries are computed entirely from
	// execGenesisState.Copy() and never read from the shared db, so they
	// remain valid across reorgs. This eliminates repeated full-chain
	// re-execution from genesis after every reorg.
	memSC *stateCache

	// config.Genesis state (used as base for re-execution).
	genesisState state.StateDB

	// execGenesisState is a self-contained MemoryStateDB snapshot of the
	// genesis state. Unlike genesisState (which may be a TrieStateDB sharing
	// a mutable db), this copy is never mutated after construction and is
	// safe to use as a re-execution base at any point in the chain's life.
	execGenesisState *state.MemoryStateDB

	// gcMode is forwarded from the genesis statedb (archive or full) so that
	// reorg() can materialise a MemoryStateDB back into a TrieStateDB.
	gcMode string

	// stateDB is the database backend used by TrieStateDB for account/storage
	// state. Separate from bc.db (which stores blocks/receipts) so that state
	// can use a fast in-memory store while blocks use the persistent FileDB.
	stateDB rawdb.Database

	// Current state after processing the head block.
	currentState state.StateDB

	// The genesis block.
	genesis *types.Block

	// Current head block.
	currentBlock *types.Block

	// Latest finalized and safe blocks as signalled by the CL via FCU.
	// Protected by mu; persisted to rawdb on every update.
	currentFinalBlock *types.Block
	currentSafeBlock  *types.Block

	// ancientStore is an optional freezer for cold block storage. When set,
	// SetFinalized migrates newly-finalized blocks from the live DB into cold
	// storage, matching go-ethereum's freezer behaviour.
	ancientStore *rawdb.AncientStore

	// sfGroup deduplicates concurrent StateAtBlock re-executions: when N
	// goroutines all request state for the same block hash, only one does the
	// work and the rest wait for and share the result.
	sfGroup singleflight.Group
}

// NewBlockchain creates a new blockchain initialized with the given genesis block.
// The statedb should contain the genesis state (pre-funded accounts, etc.).
// opts is an optional BlockchainOpts; zero/absent values use package defaults.
func NewBlockchain(config *config.ChainConfig, genesis *types.Block, statedb state.StateDB, db rawdb.Database, optArgs ...BlockchainOpts) (*Blockchain, error) {
	if genesis == nil {
		return nil, ErrNoGenesis
	}
	var opts BlockchainOpts
	if len(optArgs) > 0 {
		opts = optArgs[0]
	}
	if opts.BlockCacheSize <= 0 {
		opts.BlockCacheSize = defaultBlockCacheSize
	}
	if opts.ReceiptCacheSize <= 0 {
		opts.ReceiptCacheSize = defaultReceiptCacheSize
	}
	if opts.StateCacheSize <= 0 {
		opts.StateCacheSize = defaultMaxCachedStates
	}

	proc := execution.NewStateProcessor(config)

	// Build a self-contained MemoryStateDB snapshot of the genesis state.
	// Called BEFORE ts.Commit() in node.go so that ts.mem still holds the
	// genesis alloc accounts (Commit resets mem to empty).
	var execGenesis *state.MemoryStateDB
	var gcMode string
	var stateDB rawdb.Database
	switch s := statedb.(type) {
	case *state.TrieStateDB:
		execGenesis = s.GetMem().Copy()
		gcMode = s.GCMode()
		stateDB = s.DB()
	case *state.MemoryStateDB:
		execGenesis = s.Copy()
	default:
		execGenesis = state.NewMemoryStateDB()
	}

	bc := &Blockchain{
		config:           config,
		db:               db,
		stateDB:          stateDB,
		opts:             opts,
		processor:        proc,
		validator:        block.NewBlockValidator(config),
		blockCache:       make(map[types.Hash]*types.Block),
		canonCache:       make(map[uint64]types.Hash),
		receiptCache:     make(map[types.Hash][]*types.Receipt),
		txLookup:         make(map[types.Hash]TxLookupEntry),
		sc:               newStateCache(opts.StateCacheSize),
		memSC:            newStateCache(opts.StateCacheSize),
		genesisState:     statedb,
		execGenesisState: execGenesis,
		gcMode:           gcMode,
		currentState:     statedb.Dup(),
		genesis:          genesis,
		currentBlock:     genesis,
	}

	// Wire up GetHash for BLOCKHASH opcode support.
	proc.SetGetHash(bc.GetHashFn())

	// Create HeaderChain from genesis header.
	bc.hc = NewHeaderChain(config, genesis.Header())

	// Store genesis in caches.
	hash := genesis.Hash()
	bc.blockCache[hash] = genesis
	bc.canonCache[genesis.NumberU64()] = hash

	// Check if chain already exists in rawdb (e.g. after restart).
	existing, err := rawdb.ReadCanonicalHash(db, 0)
	if err != nil || existing != hash {
		// Fresh start: persist genesis and set head pointers.
		bc.writeBlock(genesis)
		rawdb.WriteCanonicalHash(db, genesis.NumberU64(), hash)
		rawdb.WriteHeadBlockHash(db, hash)
		rawdb.WriteHeadHeaderHash(db, hash)
	}

	// Restore finalized and safe block pointers from the database.
	// The safe block is not persisted (spec note) so it mirrors finalized on
	// startup, matching go-ethereum behaviour.
	if fh := rawdb.ReadFinalizedBlockHash(db); fh != (types.Hash{}) {
		if blk := bc.GetBlock(fh); blk != nil {
			bc.currentFinalBlock = blk
			bc.currentSafeBlock = blk
		}
	}

	return bc, nil
}

// evictOldestBlock removes the oldest block from blockCache (LRU eviction).
// It also removes the block's transactions from txLookup.
// Must be called with bc.mu held for writing.
func (bc *Blockchain) evictOldestBlock() {
	if len(bc.blockCacheOrder) == 0 {
		return
	}
	oldest := bc.blockCacheOrder[0]
	bc.blockCacheOrder = bc.blockCacheOrder[1:]
	if evBlk, ok := bc.blockCache[oldest]; ok {
		delete(bc.blockCache, oldest)
		for _, tx := range evBlk.Transactions() {
			delete(bc.txLookup, tx.Hash())
		}
	}
}

// InsertBlock validates, executes, and inserts a single block.
//
// To prevent long lock-holds from blocking concurrent Engine API reads,
// InsertBlock uses a three-phase approach:
//   - Phase 1 (brief bc.mu.Lock): skip-check, find parent, validate header/body,
//     and snapshot the ancestor chain needed for state computation.
//   - Phase 2 (no bc.mu held): expensive state re-execution and block processing.
//     bc.insertMu serialises concurrent insertions so Phase 2 is still atomic
//     with respect to other InsertBlock calls.
//   - Phase 3 (brief bc.mu.Lock): write results to caches and update canonical chain.
//
// During Phase 2 all Engine API reads (HasBlock, GetBlock, CurrentBlock, etc.)
// can proceed without waiting, eliminating HTTP timeouts under heavy load.
func (bc *Blockchain) InsertBlock(blk *types.Block) error {
	_, err := bc.InsertBlockWithResult(blk)
	return err
}

// InsertBlockWithResult returns the execution result of a newly validated block.
// A block already present in the chain returns a nil result.
func (bc *Blockchain) InsertBlockWithResult(blk *types.Block) (*execution.ProcessResult, error) {
	bc.insertMu.Lock()
	defer bc.insertMu.Unlock()
	var result *execution.ProcessResult
	err := bc.insertBlock(blk, &result)
	return result, err
}

// collectAncestorsLocked builds the chain of blocks that must be re-executed
// to produce the state after target. The chain is returned in execution order
// (oldest first). Caller must hold bc.mu.Lock().
//
// With a state cache of 64 entries (defaultMaxCachedStates) and the SYNCING
// guard in processBlockInternal, the ancestor walk never exceeds 64 steps and
// never needs rawdb lookups for normal sequential processing.
func (bc *Blockchain) collectAncestorsLocked(target *types.Block) ([]*types.Block, state.StateDB) {
	if target.Hash() == bc.genesis.Hash() {
		return nil, bc.execGenesisState.Copy()
	}

	// Fast path: target state is already cached (hot sc or permanent memSC).
	// Dup so insertBlock's Process call cannot mutate the cached entry.
	if cached, ok := bc.sc.get(target.Hash()); ok {
		return nil, cached.Dup()
	}
	if cached, ok := bc.memSC.get(target.Hash()); ok {
		return nil, cached.Dup()
	}

	var chain []*types.Block
	current := target

	for current.Hash() != bc.genesis.Hash() {
		// If the parent's state is cached, use it as the base.
		// Check both hot sc and permanent memSC (valid across reorgs).
		// Dup so Process cannot corrupt the cached entry.
		if cached, ok := bc.sc.get(current.ParentHash()); ok {
			chain = append(chain, current)
			// Reverse to execution order (oldest first).
			for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
				chain[i], chain[j] = chain[j], chain[i]
			}
			return chain, cached.Dup()
		}
		if cached, ok := bc.memSC.get(current.ParentHash()); ok {
			chain = append(chain, current)
			for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
				chain[i], chain[j] = chain[j], chain[i]
			}
			return chain, cached.Dup()
		}
		chain = append(chain, current)
		p := bc.blockCache[current.ParentHash()]
		if p == nil {
			// rawdb fallback; only reached when ancestor is not in blockCache.
			p = bc.readBlock(current.ParentHash())
		}
		if p == nil {
			break
		}
		current = p
	}

	// Reached genesis or a missing ancestor — use genesis state as base.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, bc.execGenesisState.Copy()
}

// insertBlock is the internal three-phase insert. Caller must hold bc.insertMu.
func (bc *Blockchain) insertBlock(blk *types.Block, inserted **execution.ProcessResult) error {
	hash := blk.Hash()
	num := blk.NumberU64()
	header := blk.Header()

	blockchainLog.Debug("block_insert",
		"event", "block_insert",
		"hash", hash.Hex(),
		"num", num,
		"txCount", len(blk.Transactions()),
		"gasLimit", header.GasLimit,
		"parentHash", header.ParentHash.Hex(),
	)

	// ── Phase 1: validate and snapshot (brief bc.mu.Lock) ──────────────────
	// Hold bc.mu only for in-memory reads — no expensive computation here.
	var parent *types.Block
	var ancestorChain []*types.Block
	var baseState state.StateDB
	var headNum uint64 // captured under bc.mu; used to determine canonicality in Phase 2

	{
		bc.mu.Lock()

		// Skip if already known.
		if _, ok := bc.blockCache[hash]; ok {
			bc.mu.Unlock()
			blockchainLog.Debug("block_known",
				"event", "block_known",
				"hash", hash.Hex(),
				"num", num,
			)
			return nil
		}

		// Find parent: cache first, then rawdb.
		parent = bc.blockCache[header.ParentHash]
		if parent == nil {
			parent = bc.readBlock(header.ParentHash)
			if parent != nil {
				bc.blockCache[header.ParentHash] = parent
			}
		}
		if parent == nil {
			bc.mu.Unlock()
			err := fmt.Errorf("%w: parent %v", block.ErrUnknownParent, header.ParentHash)
			blockchainLog.Warn("block_invalid",
				"event", "block_invalid",
				"hash", hash.Hex(),
				"num", num,
				"reason", "unknown parent",
				"parentHash", header.ParentHash.Hex(),
				"error", err,
			)
			return err
		}

		// Validate header against parent.
		if err := bc.validator.ValidateHeader(header, parent.Header()); err != nil {
			bc.mu.Unlock()
			blockchainLog.Warn("block_invalid",
				"event", "block_invalid",
				"hash", hash.Hex(),
				"num", num,
				"reason", "header validation failed",
				"parentHash", header.ParentHash.Hex(),
				"parentNum", parent.NumberU64(),
				"error", err,
			)
			return err
		}

		// Validate body.
		if err := bc.validator.ValidateBody(blk); err != nil {
			bc.mu.Unlock()
			blockchainLog.Warn("block_invalid",
				"event", "block_invalid",
				"hash", hash.Hex(),
				"num", num,
				"reason", "body validation failed",
				"error", err,
			)
			return err
		}

		// Collect the ancestor chain needed to build the parent's state.
		// This reads blockCache (under bc.mu) and the state cache (own lock).
		ancestorChain, baseState = bc.collectAncestorsLocked(parent)

		// Capture current head number so Phase 2 can determine canonicality
		// without holding bc.mu during expensive state commit and DB writes.
		headNum = bc.currentBlock.NumberU64()

		bc.mu.Unlock() // ← release lock; Phase 2 runs without it
	}

	// ── Phase 2: compute state (no bc.mu held) ─────────────────────────────
	// Re-execute ancestors from the cached base state. bc.insertMu ensures
	// only one goroutine runs this phase at a time, so the computed state
	// is always consistent. All concurrent readers can proceed freely.
	statedb := baseState
	for _, ancestor := range ancestorChain {
		if _, err := bc.processor.Process(ancestor, statedb); err != nil {
			return fmt.Errorf("re-execute block %d: %w", ancestor.NumberU64(), err)
		}
	}

	// Execute the target block.
	blockStart := time.Now()
	result, err := bc.processor.ProcessWithBAL(blk, statedb)
	metrics.BlockProcessTime.Observe(float64(time.Since(blockStart).Milliseconds()))
	if err != nil {
		blockchainLog.Error("block_exec_fail",
			"event", "block_exec_fail",
			"hash", hash.Hex(),
			"num", num,
			"txCount", len(blk.Transactions()),
			"error", err,
		)
		return fmt.Errorf("process block %d: %w", num, err)
	}
	receipts := result.Receipts

	// Validate gas used.
	// EIP-7778: Under Glamsterdam, header.GasUsed uses pre-refund gas (BlockGasUsed).
	// Pre-Glamsterdam, header.GasUsed uses post-refund gas (GasUsed).
	var totalGasUsed uint64
	var totalBlockGasUsed uint64
	isGlamsterdan := bc.config != nil && bc.config.IsGlamsterdan(header.Time)
	isAmsterdam := bc.config != nil && bc.config.IsAmsterdam(header.Time)
	for _, r := range receipts {
		totalBlockGasUsed += r.BlockGasUsed
		if isGlamsterdan && r.BlockGasUsed > 0 {
			totalGasUsed += r.BlockGasUsed
		} else {
			totalGasUsed += r.GasUsed
		}
	}
	if header.GasUsed != totalGasUsed {
		err := fmt.Errorf("%w: header=%d computed=%d", block.ErrInvalidGasUsedTotal, header.GasUsed, totalGasUsed)
		blockchainLog.Warn("block_gas_mismatch",
			"event", "block_gas_mismatch",
			"hash", hash.Hex(),
			"num", num,
			"parentHash", header.ParentHash.Hex(),
			"parentNum", num-1,
			"headerGasUsed", header.GasUsed,
			"computedGasUsed", totalGasUsed,
			"computedBlockGasUsed", totalBlockGasUsed,
			"amsterdamActive", isAmsterdam,
			"glamsterdamActive", isGlamsterdan,
			"txCount", len(blk.Transactions()),
			"currentHeadHash", bc.CurrentBlock().Hash().Hex(),
			"currentHeadNum", bc.CurrentBlock().NumberU64(),
			"error", err,
		)
		// Log per-tx breakdown to identify which transaction causes the mismatch.
		for i, r := range receipts {
			tx := blk.Transactions()[i]
			toHex := ""
			if tx.To() != nil {
				toHex = tx.To().Hex()
			}
			blockchainLog.Warn("block_gas_mismatch_tx",
				"event", "block_gas_mismatch_tx",
				"num", num,
				"txIndex", i,
				"txHash", r.TxHash.Hex(),
				"txType", tx.Type(),
				"to", toHex,
				"status", r.Status,
				"gasUsed", r.GasUsed,
				"blockGasUsed", r.BlockGasUsed,
				"cumulativeGasUsed", r.CumulativeGasUsed,
			)
		}
		return err
	}

	// Validate receipt root.
	computedReceiptHash := block.ComputeReceiptsRoot(receipts)
	if header.ReceiptHash != computedReceiptHash {
		err := fmt.Errorf("%w: header=%s computed=%s", block.ErrInvalidReceiptRoot,
			header.ReceiptHash.Hex(), computedReceiptHash.Hex())
		blockchainLog.Warn("block_invalid",
			"event", "block_invalid",
			"hash", hash.Hex(),
			"num", num,
			"reason", "receipt root mismatch",
			"headerReceiptHash", header.ReceiptHash.Hex(),
			"computedReceiptHash", computedReceiptHash.Hex(),
			"error", err,
		)
		return err
	}

	// Validate bloom.
	blockBloom := types.CreateBloom(receipts)
	if header.Bloom != blockBloom {
		err := fmt.Errorf("invalid bloom (remote: %x local: %x)", header.Bloom, blockBloom)
		blockchainLog.Warn("block_invalid",
			"event", "block_invalid",
			"hash", hash.Hex(),
			"num", num,
			"reason", "bloom mismatch",
			"error", err,
		)
		return err
	}

	// EIP-7685: commit only the requests reproduced by execution.
	if err := bc.validator.ValidateRequests(header, result.Requests); err != nil {
		return fmt.Errorf("block %d requests: %w", num, err)
	}

	// Validate state root.
	computedRoot := statedb.GetRoot()
	if header.Root != computedRoot {
		err := fmt.Errorf("%w: header=%s computed=%s", block.ErrInvalidStateRoot,
			header.Root.Hex(), computedRoot.Hex())
		blockchainLog.Warn("block_root_mismatch",
			"event", "block_root_mismatch",
			"hash", hash.Hex(),
			"num", num,
			"headerRoot", header.Root.Hex(),
			"computedRoot", computedRoot.Hex(),
			"txCount", len(blk.Transactions()),
			"gasUsed", totalGasUsed,
			"error", err,
		)
		return err
	}

	// Validate BAL hash (EIP-7928).
	var computedBALHash *types.Hash
	if result.BlockAccessList != nil {
		h := result.BlockAccessList.Hash()
		computedBALHash = &h
	}
	if err := bc.validator.ValidateBlockAccessList(header, computedBALHash); err != nil {
		blockchainLog.Warn("block_invalid",
			"event", "block_invalid",
			"hash", hash.Hex(),
			"num", num,
			"reason", "BAL hash mismatch",
			"error", err,
		)
		return err
	}

	txs := blk.Transactions()

	// Populate derived receipt fields (no lock needed; local data).
	// logIndex is block-scoped: monotonically increasing across all receipts.
	var logIndex uint
	for i, receipt := range receipts {
		receipt.BlockHash = hash
		receipt.BlockNumber = new(big.Int).SetUint64(num)
		receipt.TransactionIndex = uint(i)
		if i < len(txs) {
			receipt.TxHash = txs[i].Hash()
		}
		for _, logEntry := range receipt.Logs {
			logEntry.BlockHash = hash
			logEntry.BlockNumber = num
			logEntry.TxHash = receipt.TxHash
			logEntry.TxIndex = uint(i)
			logEntry.Index = logIndex
			logIndex++
		}
	}

	// ── Phase 2b: persist results outside bc.mu to minimise lock hold time ─
	// insertMu guarantees only one goroutine is here at a time, so headNum
	// captured in Phase 1 is still valid for the canonical check below.
	isCanonical := num > headNum
	if isCanonical {
		// Commit state to backing store: flushes dirty accounts/storage to DB
		// and clears the in-memory dirty layer. Expensive with many dirty
		// accounts (e.g. storagespam) — must run outside bc.mu.Lock().
		if _, err := statedb.Commit(); err != nil {
			return fmt.Errorf("commit state block %d: %w", num, err)
		}

		// Cache post-commit state in both caches.
		// After Commit() the dirty layer is empty so Dup() is O(1).
		// memSC is permanent (never cleared on reorg) so collectAncestorsLocked
		// always finds a nearby ancestor, avoiding full genesis re-execution.
		bc.sc.put(hash, num, statedb)
		bc.memSC.put(hash, num, statedb)
		// Protect this canonical head state from eviction.
		// This ensures StateAtBlock can always find the current head state
		// quickly, preventing expensive re-execution during payload building.
		bc.sc.protect(hash)
		bc.memSC.protect(hash)

		// Persist block, receipts, and chain indices to rawdb.
		bc.writeBlock(blk)
		bc.writeReceipts(num, hash, receipts)
		bc.writeTxLookups(txs, num)
		rawdb.WriteCanonicalHash(bc.db, num, hash)
		rawdb.WriteHeadBlockHash(bc.db, hash)
		rawdb.WriteHeadHeaderHash(bc.db, hash)
	} else {
		// Side block: cache state and persist block body only; no DB commit.
		// Populate both sc and memSC so that if this block later becomes
		// canonical (via reorg), collectAncestorsLocked finds it in O(1).
		// For TrieStateDB side blocks the dirty layer holds block state and
		// is small enough for Dup() to be fast in typical reorg scenarios.
		bc.sc.put(hash, num, statedb)
		bc.memSC.put(hash, num, statedb)
		bc.writeBlock(blk)
	}

	// Cache receipts under rcMu (independent of bc.mu; fast operation).
	bc.rcMu.Lock()
	for len(bc.receiptCache) >= bc.opts.ReceiptCacheSize {
		if len(bc.receiptCacheOrder) == 0 {
			break
		}
		oldest := bc.receiptCacheOrder[0]
		bc.receiptCacheOrder = bc.receiptCacheOrder[1:]
		delete(bc.receiptCache, oldest)
	}
	bc.receiptCache[hash] = receipts
	bc.receiptCacheOrder = append(bc.receiptCacheOrder, hash)
	bc.rcMu.Unlock()

	// ── Phase 3: update in-memory indices (brief bc.mu.Lock) ───────────────
	// All expensive DB and state work is done; this section only touches
	// in-memory maps and pointers so the lock is held for microseconds.
	bc.mu.Lock()
	defer bc.mu.Unlock()

	// Store in block cache (evict oldest when at capacity).
	for len(bc.blockCache) >= bc.opts.BlockCacheSize {
		bc.evictOldestBlock()
	}
	bc.blockCache[hash] = blk
	bc.blockCacheOrder = append(bc.blockCacheOrder, hash)

	// Build tx lookup index.
	for i, tx := range txs {
		bc.txLookup[tx.Hash()] = TxLookupEntry{
			BlockHash:   hash,
			BlockNumber: num,
			TxIndex:     uint64(i),
		}
	}

	if isCanonical {
		bc.canonCache[num] = hash
		bc.currentBlock = blk
		bc.currentState = statedb

		bc.hc.InsertHeaders([]*types.Header{header})

		metrics.BlocksInserted.Inc()
		metrics.ChainHeight.Set(int64(num))
		blockchainLog.Info("block_added",
			"event", "block_added",
			"hash", hash.Hex(),
			"num", num,
			"txCount", len(txs),
			"gasUsed", totalGasUsed,
			"gasLimit", header.GasLimit,
		)
	} else {
		blockchainLog.Debug("block_side",
			"event", "block_side",
			"hash", hash.Hex(),
			"num", num,
			"canonHead", bc.currentBlock.NumberU64(),
		)
	}

	*inserted = result
	return nil
}

// InsertChain inserts a chain of blocks sequentially.
// Blocks must be in ascending order but need not be contiguous with the head
// at the time of the call (though each must connect to its parent).
func (bc *Blockchain) InsertChain(blocks []*types.Block) (int, error) {
	for i, blk := range blocks {
		if err := bc.InsertBlock(blk); err != nil {
			return i, err
		}
	}
	return len(blocks), nil
}

// GetBlock retrieves a block by hash, or nil if not found.
// It checks the in-memory cache first, then falls back to rawdb.
func (bc *Blockchain) GetBlock(hash types.Hash) *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if b, ok := bc.blockCache[hash]; ok {
		return b
	}
	// Fallback: try reading from rawdb.
	return bc.readBlock(hash)
}

// GetBlockByNumber retrieves the canonical block for a given number.
func (bc *Blockchain) GetBlockByNumber(number uint64) *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	hash, ok := bc.canonCache[number]
	if !ok {
		// Fallback: try rawdb canonical hash.
		h, err := rawdb.ReadCanonicalHash(bc.db, number)
		if err != nil {
			return nil
		}
		hash = h
	}
	if b, ok2 := bc.blockCache[hash]; ok2 {
		return b
	}
	return bc.readBlock(hash)
}

// CurrentBlock returns the head of the canonical chain.
func (bc *Blockchain) CurrentBlock() *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentBlock
}

// CurrentFinalBlock returns the latest block that the CL has finalized, or nil
// if no finalized block has been signalled yet.
func (bc *Blockchain) CurrentFinalBlock() *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentFinalBlock
}

// CurrentSafeBlock returns the latest block that the CL has declared safe, or
// nil if no safe block has been signalled yet.
func (bc *Blockchain) CurrentSafeBlock() *types.Block {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentSafeBlock
}

// SetAncientStore wires an AncientStore to the blockchain. When set,
// SetFinalized will migrate newly-finalized blocks from the live DB to cold
// storage in the background.
func (bc *Blockchain) SetAncientStore(as *rawdb.AncientStore) {
	bc.mu.Lock()
	bc.ancientStore = as
	bc.mu.Unlock()
}

// AncientStore returns the wired AncientStore, or nil if none.
func (bc *Blockchain) AncientStore() *rawdb.AncientStore {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.ancientStore
}

// SetFinalized records the given block as the latest finalized block.
// It updates the in-memory pointer and persists the hash to the database so
// that the value survives a node restart. If an AncientStore is wired, it
// triggers a background migration of all blocks up to the finalized number.
func (bc *Blockchain) SetFinalized(blk *types.Block) {
	bc.mu.Lock()
	bc.currentFinalBlock = blk
	as := bc.ancientStore
	bc.mu.Unlock()
	if blk != nil {
		rawdb.WriteFinalizedBlockHash(bc.db, blk.Hash())
		metrics.ChainHeadFinalized.Set(int64(blk.NumberU64()))
		if as != nil {
			go bc.migrateToAncient(as, blk.NumberU64())
		}
	} else {
		rawdb.WriteFinalizedBlockHash(bc.db, types.Hash{})
		metrics.ChainHeadFinalized.Set(0)
	}
}

// migrateToAncient moves finalized blocks from the live DB to the AncientStore.
// It runs in a goroutine to avoid blocking Engine API calls.
func (bc *Blockchain) migrateToAncient(as *rawdb.AncientStore, finalNum uint64) {
	frozen := as.Frozen()
	if finalNum <= frozen {
		return // nothing new to freeze
	}
	migrated, err := as.MigrateFromDB(bc.db, frozen, finalNum)
	if err != nil {
		blockchainLog.Error("ancient migration failed", "from", frozen, "to", finalNum, "migrated", migrated, "err", err)
		return
	}
	blockchainLog.Debug("ancient migration complete", "migrated", migrated, "frozen_now", as.Frozen())
}

// SetSafe records the given block as the latest safe block.
// The safe block is not persisted to disk (it is reconstructed from the
// finalized block on startup, following go-ethereum convention).
func (bc *Blockchain) SetSafe(blk *types.Block) {
	bc.mu.Lock()
	bc.currentSafeBlock = blk
	bc.mu.Unlock()
	if blk != nil {
		metrics.ChainHeadSafe.Set(int64(blk.NumberU64()))
	} else {
		metrics.ChainHeadSafe.Set(0)
	}
}

// HasBlock checks if a block with the given hash exists in cache or rawdb.
func (bc *Blockchain) HasBlock(hash types.Hash) bool {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if _, ok := bc.blockCache[hash]; ok {
		return true
	}
	// Fall back to rawdb: if the header number mapping exists, the block was
	// persisted (written by writeBlock which calls WriteHeader).
	_, err := rawdb.ReadHeaderNumber(bc.db, hash)
	return err == nil
}

// HasStateCached returns true if the state for the given block is held in the
// in-memory state cache. Genesis is always considered cached. This is safe to
// call without bc.mu — the stateCache has its own lock.
func (bc *Blockchain) HasStateCached(blockHash types.Hash) bool {
	if blockHash == bc.genesis.Hash() {
		return true
	}
	_, ok := bc.sc.get(blockHash)
	return ok
}

// SetHead rewinds the canonical chain to the given block number.
// Blocks above the target number are removed from the canonical index.
func (bc *Blockchain) SetHead(number uint64) error {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	blockchainLog.Info("chain_sethead",
		"event", "chain_sethead",
		"from", bc.currentBlock.NumberU64(),
		"to", number,
	)

	target, ok := bc.canonCache[number]
	if !ok {
		return fmt.Errorf("%w: no canonical block at %d", ErrBlockNotFound, number)
	}

	// Remove canonical entries above target.
	current := bc.currentBlock.NumberU64()
	for n := current; n > number; n-- {
		if hash, ok := bc.canonCache[n]; ok {
			rawdb.DeleteCanonicalHash(bc.db, n)
			delete(bc.canonCache, n)
			// Remove from block cache too.
			delete(bc.blockCache, hash)
		}
	}

	// Set new head.
	bc.currentBlock = bc.blockCache[target]

	// Re-derive state by re-executing from genesis.
	statedb, err := bc.stateAt(bc.currentBlock)
	if err != nil {
		return fmt.Errorf("re-derive state at %d: %w", number, err)
	}
	bc.currentState = statedb

	// Update rawdb pointers.
	hash := bc.currentBlock.Hash()
	rawdb.WriteHeadBlockHash(bc.db, hash)
	rawdb.WriteHeadHeaderHash(bc.db, hash)

	// Rewind header chain.
	bc.hc.SetHead(number)

	return nil
}

// GetHashFn returns a GetHashFunc that resolves block number -> hash
// for the BLOCKHASH opcode (EIP-210 compatible, up to 256 blocks back).
func (bc *Blockchain) GetHashFn() func(uint64) types.Hash {
	return func(number uint64) types.Hash {
		bc.mu.RLock()
		defer bc.mu.RUnlock()
		if hash, ok := bc.canonCache[number]; ok {
			return hash
		}
		return types.Hash{}
	}
}

// Genesis returns the genesis block.
func (bc *Blockchain) Genesis() *types.Block {
	return bc.genesis
}

// Config returns the chain configuration.
func (bc *Blockchain) Config() *config.ChainConfig {
	return bc.config
}

// State returns a copy of the current state.
func (bc *Blockchain) State() state.StateDB {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentState.Dup()
}

// StateAtRoot returns the state for the given state root hash.
// It searches canonical blocks for one whose header root matches,
// then re-executes from the nearest cached snapshot.
// bc.mu is held only for fast in-memory lookups; re-execution runs outside.
func (bc *Blockchain) StateAtRoot(root types.Hash) (state.StateDB, error) {
	t0 := time.Now()
	// Fast path under brief RLock: check current state, genesis, and block cache.
	var targetBlock *types.Block
	{
		bc.mu.RLock()
		t1 := time.Now()
		// Use block header Root (a stored hash) instead of calling GetRoot()
		// which would rebuild the full storage trie and take O(N_slots) time.
		if bc.currentBlock.Header().Root == root {
			s := bc.currentState.Dup()
			bc.mu.RUnlock()
			return s, nil
		}
		if bc.genesis.Header().Root == root {
			s := bc.genesisState.Dup()
			bc.mu.RUnlock()
			return s, nil
		}
		for _, blk := range bc.blockCache {
			if blk.Header().Root == root {
				targetBlock = blk
				break
			}
		}
		bc.mu.RUnlock()
		if time.Since(t1) > 100*time.Millisecond {
			blockchainLog.Warn("state_at_root_rlock_slow",
				"event", "state_at_root_rlock_slow",
				"rlock_wait_ms", t1.Sub(t0).Milliseconds(),
				"rlock_held_ms", time.Since(t1).Milliseconds(),
			)
		}
	}
	if targetBlock == nil {
		return nil, fmt.Errorf("%w: no block found with state root %v", ErrStateNotFound, root)
	}
	// Re-execute outside bc.mu (StateAtBlock handles its own brief locking).
	t2 := time.Now()
	result, err := bc.StateAtBlock(targetBlock)
	if time.Since(t2) > 100*time.Millisecond {
		blockchainLog.Warn("state_at_root_block_slow",
			"event", "state_at_root_block_slow",
			"block_num", targetBlock.NumberU64(),
			"state_at_block_ms", time.Since(t2).Milliseconds(),
		)
	}
	return result, err
}

// StateAtBlock returns the state after executing up to the given block.
// This is public for use by external packages (e.g. core/block).
// CanQuicklyGetState returns true if the state for the given block can be
// obtained quickly (either cached or only needs to re-execute a few blocks).
// This is used by the Engine API to decide whether to return SYNCING status
// when the CL requests a new payload.
// The threshold defines how many blocks we're willing to re-execute before
// returning SYNCING (default: 32 blocks = ~6 seconds worth of execution).
func (bc *Blockchain) CanQuicklyGetState(blk *types.Block, threshold int) bool {
	if blk == nil {
		return false
	}
	// Genesis state is always available.
	if blk.Hash() == bc.genesis.Hash() {
		return true
	}
	// Check if the block's state is already cached.
	if _, ok := bc.sc.get(blk.Hash()); ok {
		return true
	}
	if _, ok := bc.memSC.get(blk.Hash()); ok {
		return true
	}

	// Check how many blocks we'd need to re-execute.
	// Walk up the chain until we find a cached state.
	// We need bc.mu for blockCache access, but check state cache outside the lock
	// to avoid nested locking (stateCache has its own mutex).
	depth := 0
	current := blk
	for current.Hash() != bc.genesis.Hash() {
		// Check if parent state is cached (outside bc.mu).
		if _, ok := bc.sc.get(current.ParentHash()); ok {
			return depth < threshold
		}
		if _, ok := bc.memSC.get(current.ParentHash()); ok {
			return depth < threshold
		}
		depth++
		if depth > threshold {
			return false
		}
		// Get parent block (needs bc.mu for blockCache).
		bc.mu.RLock()
		parent := bc.blockCache[current.ParentHash()]
		bc.mu.RUnlock()
		if parent == nil {
			parent = bc.readBlock(current.ParentHash())
		}
		if parent == nil {
			return false // Parent unknown, would need full sync
		}
		current = parent
	}
	return depth < threshold
}

// StateAtBlock returns the state after executing the given block.
// It holds bc.mu only for in-memory lookups (state cache, block cache) and
// releases the lock before any expensive re-execution so that concurrent RPC
// reads and insertBlock Phase 3 are not blocked.
func (bc *Blockchain) StateAtBlock(blk *types.Block) (state.StateDB, error) {
	// Fast path: genesis state.
	if blk.Hash() == bc.genesis.Hash() {
		return bc.genesisState.Dup(), nil
	}

	// Fast path: exact cache hit in sc (hot TrieStateDB cache, no bc.mu).
	if cached, ok := bc.sc.get(blk.Hash()); ok {
		return cached, nil
	}
	// Fast path: permanent MemoryStateDB cache — valid across reorgs.
	if cached, ok := bc.memSC.get(blk.Hash()); ok {
		return cached, nil
	}

	// Slow path: deduplicate via singleflight so that N concurrent requests
	// for the same block hash result in only one re-execution.
	blockchainLog.Warn("state_at_block_cache_miss",
		"event", "state_at_block_cache_miss",
		"block_num", blk.NumberU64(),
		"block_hash", blk.Hash().Hex()[:16],
	)
	key := blk.Hash().Hex()
	v, err, _ := bc.sfGroup.Do(key, func() (interface{}, error) {
		// Collect ancestor chain under bc.mu (fast map reads only).
		chain, baseState := func() ([]*types.Block, state.StateDB) {
			bc.mu.RLock()
			defer bc.mu.RUnlock()

			var ancestors []*types.Block
			current := blk
			for current.Hash() != bc.genesis.Hash() {
				if cached, ok := bc.sc.get(current.ParentHash()); ok {
					ancestors = append(ancestors, current)
					for i, j := 0, len(ancestors)-1; i < j; i, j = i+1, j-1 {
						ancestors[i], ancestors[j] = ancestors[j], ancestors[i]
					}
					return ancestors, cached.Dup()
				}
				if cached, ok := bc.memSC.get(current.ParentHash()); ok {
					ancestors = append(ancestors, current)
					for i, j := 0, len(ancestors)-1; i < j; i, j = i+1, j-1 {
						ancestors[i], ancestors[j] = ancestors[j], ancestors[i]
					}
					return ancestors, cached.Dup()
				}
				ancestors = append(ancestors, current)
				parent := bc.blockCache[current.ParentHash()]
				if parent == nil {
					parent = bc.readBlock(current.ParentHash())
				}
				if parent == nil {
					break
				}
				current = parent
			}
			for i, j := 0, len(ancestors)-1; i < j; i, j = i+1, j-1 {
				ancestors[i], ancestors[j] = ancestors[j], ancestors[i]
			}
			return ancestors, nil
		}()

		if baseState == nil {
			baseState = bc.execGenesisState.Copy()
		}

		// Re-execute outside bc.mu — this may take 100ms+ for long chains.
		// Cache each intermediate state in both sc (hot) and memSC (permanent,
		// never cleared on reorg) so future requests avoid re-execution even
		// after sc is wiped by a reorg.
		for _, b := range chain {
			if _, err := bc.processor.Process(b, baseState); err != nil {
				return nil, fmt.Errorf("re-execute block %d: %w", b.NumberU64(), err)
			}
			bc.sc.put(b.Hash(), b.NumberU64(), baseState)
			bc.memSC.put(b.Hash(), b.NumberU64(), baseState)
		}
		if len(chain) == 0 {
			return nil, fmt.Errorf("%w: no ancestor chain for %v", ErrStateNotFound, blk.Hash())
		}
		return baseState, nil
	})
	if err != nil {
		return nil, err
	}
	// singleflight shares the result across waiters; each caller needs its own
	// copy so they don't corrupt each other's state during block execution.
	return v.(state.StateDB).Dup(), nil
}

// stateAt returns the state after executing up to (and including) the given block.
// For the genesis block, this is the genesis state directly.
// Caller must hold bc.mu (read or write) for blockCache access.
func (bc *Blockchain) stateAt(blk *types.Block) (state.StateDB, error) {
	if blk.Hash() == bc.genesis.Hash() {
		// genesisState is a TrieStateDB with genesis accounts persisted in db.
		// Dup() gives a fresh state that reads from db (= genesis state) without
		// accumulating dirty writes in the cached entry.
		return bc.genesisState.Dup(), nil
	}

	// Check if we have an exact cached state for this block.
	// Dup() so callers (insertBlock, block builder) do not corrupt the cache.
	if cached, ok := bc.sc.get(blk.Hash()); ok {
		return cached.Dup(), nil
	}

	// Collect the chain of blocks from genesis (or a cached snapshot) to this block.
	var chain []*types.Block
	current := blk
	var baseState state.StateDB

	for current.Hash() != bc.genesis.Hash() {
		// Check hot sc first, then permanent memSC (valid across reorgs).
		if cached, ok := bc.sc.get(current.ParentHash()); ok {
			baseState = cached.Dup()
			chain = append(chain, current)
			break
		}
		if cached, ok := bc.memSC.get(current.ParentHash()); ok {
			baseState = cached.Dup()
			chain = append(chain, current)
			break
		}
		chain = append(chain, current)
		parent, ok := bc.blockCache[current.ParentHash()]
		if !ok {
			// Fallback: try rawdb. Do NOT write blockCache here — stateAt
			// may be called under RLock (from StateAtBlock), and writing a
			// shared map under RLock causes concurrent map write panics.
			parent = bc.readBlock(current.ParentHash())
		}
		if parent == nil {
			return nil, fmt.Errorf("%w: missing ancestor at %v", ErrStateNotFound, current.ParentHash())
		}
		current = parent
	}

	// Use the self-contained genesis MemoryStateDB as base if no cached
	// snapshot was found. This avoids reading from the (mutable) TrieStateDB
	// shared db, which reflects the current head state rather than genesis.
	if baseState == nil {
		baseState = bc.execGenesisState.Copy()
	}

	// Re-execute from the base state.
	// Cache each intermediate state in sc and memSC so that subsequent
	// StateAtBlock calls (e.g. from blockscout RPC) avoid re-execution.
	// memSC is never cleared on reorg so post-reorg recovery is cheap.
	for i := len(chain) - 1; i >= 0; i-- {
		b := chain[i]
		if _, err := bc.processor.Process(b, baseState); err != nil {
			return nil, fmt.Errorf("re-execute block %d: %w", b.NumberU64(), err)
		}
		bc.sc.put(b.Hash(), b.NumberU64(), baseState)
		bc.memSC.put(b.Hash(), b.NumberU64(), baseState)
	}
	return baseState, nil
}

// writeBlock persists a block's header and body to rawdb using RLP encoding.
func (bc *Blockchain) writeBlock(blk *types.Block) {
	num := blk.NumberU64()
	hash := blk.Hash()

	// RLP-encode the header.
	headerData, err := blk.Header().EncodeRLP()
	if err != nil {
		return
	}
	rawdb.WriteHeader(bc.db, num, hash, headerData)

	// RLP-encode the body (transactions list + uncles list).
	bodyData, err := encodeBlockBody(blk.Body())
	if err != nil {
		return
	}
	rawdb.WriteBody(bc.db, num, hash, bodyData)
}

// readBlock retrieves a block from rawdb by looking up the block number
// from the header hash, then reading and decoding header and body.
// If the header data has been migrated to AncientStore, it falls back there.
// Caller must hold bc.mu (at least RLock) — ancientStore is read without its
// own lock because bc.mu guards the pointer and AncientStore methods are safe.
func (bc *Blockchain) readBlock(hash types.Hash) *types.Block {
	// Look up block number from hash.
	// DeleteHeaderData preserves this mapping after AncientStore migration.
	num, err := rawdb.ReadHeaderNumber(bc.db, hash)
	if err != nil {
		return nil
	}

	// Read header from live DB.
	headerData, _ := rawdb.ReadHeader(bc.db, num, hash)
	if len(headerData) == 0 {
		// Header may have been migrated to AncientStore; try there.
		if bc.ancientStore != nil {
			return bc.readBlockFromAncient(num)
		}
		return nil
	}
	header, err := types.DecodeHeaderRLP(headerData)
	if err != nil {
		return nil
	}

	// Read body.
	bodyData, err := rawdb.ReadBody(bc.db, num, hash)
	if err != nil || len(bodyData) == 0 {
		// Body may have been migrated to AncientStore; try there.
		if bc.ancientStore != nil {
			bodyData, err = bc.ancientStore.ReadBody(num)
			if err != nil || len(bodyData) == 0 {
				// Body may be empty for blocks with no transactions.
				return types.NewBlock(header, nil)
			}
		} else {
			// No AncientStore; create block with header only.
			return types.NewBlock(header, nil)
		}
	}
	body, err := decodeBlockBody(bodyData)
	if err != nil {
		// If body decode fails, return header-only block.
		return types.NewBlock(header, nil)
	}
	return types.NewBlock(header, body)
}

// readBlockFromAncient reads a block by number from the AncientStore.
// Caller must hold bc.mu (at least RLock).
func (bc *Blockchain) readBlockFromAncient(num uint64) *types.Block {
	as := bc.ancientStore
	headerData, err := as.ReadHeader(num)
	if err != nil || len(headerData) == 0 {
		return nil
	}
	header, err := types.DecodeHeaderRLP(headerData)
	if err != nil {
		return nil
	}
	bodyData, _ := as.ReadBody(num)
	if len(bodyData) == 0 {
		return types.NewBlock(header, nil)
	}
	body, err := decodeBlockBody(bodyData)
	if err != nil {
		return types.NewBlock(header, nil)
	}
	return types.NewBlock(header, body)
}

// encodeBlockBody RLP-encodes a block body as [transactions_list, uncles_list, withdrawals_list].
// The withdrawals list is included only when body.Withdrawals is non-nil (post-Shanghai).
func encodeBlockBody(body *types.Body) ([]byte, error) {
	// Encode transactions.
	var txsPayload []byte
	if body != nil {
		for _, tx := range body.Transactions {
			txEnc, err := tx.EncodeRLP()
			if err != nil {
				return nil, err
			}
			if tx.Type() == types.LegacyTxType {
				// Legacy: txEnc is already an RLP list; append directly.
				txsPayload = append(txsPayload, txEnc...)
			} else {
				// Typed: wrap as RLP byte string (type_byte || RLP_payload).
				wrapped, err := rlp.EncodeToBytes(txEnc)
				if err != nil {
					return nil, err
				}
				txsPayload = append(txsPayload, wrapped...)
			}
		}
	}

	// Encode uncles.
	var unclesPayload []byte
	if body != nil {
		for _, uncle := range body.Uncles {
			uncleEnc, err := uncle.EncodeRLP()
			if err != nil {
				return nil, err
			}
			unclesPayload = append(unclesPayload, uncleEnc...)
		}
	}

	var payload []byte
	payload = append(payload, rlp.WrapList(txsPayload)...)
	payload = append(payload, rlp.WrapList(unclesPayload)...)

	// Encode withdrawals (post-Shanghai).
	if body != nil && body.Withdrawals != nil {
		var wsPayload []byte
		for _, w := range body.Withdrawals {
			wEnc, err := rlp.EncodeToBytes([]interface{}{w.Index, w.ValidatorIndex, w.Address, w.Amount})
			if err != nil {
				return nil, err
			}
			wsPayload = append(wsPayload, wEnc...)
		}
		payload = append(payload, rlp.WrapList(wsPayload)...)
	}

	return rlp.WrapList(payload), nil
}

// decodeBlockBody decodes an RLP-encoded block body [transactions_list, uncles_list, withdrawals_list?].
// The withdrawals list is optional (post-Shanghai).
func decodeBlockBody(data []byte) (*types.Body, error) {
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, fmt.Errorf("opening body list: %w", err)
	}

	// Decode transactions.
	_, err = s.List()
	if err != nil {
		return nil, fmt.Errorf("opening txs list: %w", err)
	}
	var txs []*types.Transaction
	for !s.AtListEnd() {
		kind, _, err := s.Kind()
		if err != nil {
			return nil, fmt.Errorf("peeking tx kind: %w", err)
		}
		var txData []byte
		if kind == rlp.List {
			txData, err = s.RawItem()
		} else {
			txData, err = s.Bytes()
		}
		if err != nil {
			return nil, fmt.Errorf("reading tx: %w", err)
		}
		tx, err := types.DecodeTxRLP(txData)
		if err != nil {
			return nil, fmt.Errorf("decoding tx: %w", err)
		}
		txs = append(txs, tx)
	}
	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing txs list: %w", err)
	}

	// Decode uncles.
	_, err = s.List()
	if err != nil {
		return nil, fmt.Errorf("opening uncles list: %w", err)
	}
	var uncles []*types.Header
	for !s.AtListEnd() {
		uncleBytes, err := s.RawItem()
		if err != nil {
			return nil, fmt.Errorf("reading uncle: %w", err)
		}
		uncle, err := types.DecodeHeaderRLP(uncleBytes)
		if err != nil {
			return nil, fmt.Errorf("decoding uncle: %w", err)
		}
		uncles = append(uncles, uncle)
	}
	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing uncles list: %w", err)
	}

	body := &types.Body{
		Transactions: txs,
		Uncles:       uncles,
	}

	// Decode optional withdrawals (post-Shanghai).
	if !s.AtListEnd() {
		_, err = s.List()
		if err != nil {
			return nil, fmt.Errorf("opening withdrawals list: %w", err)
		}
		var withdrawals []*types.Withdrawal
		for !s.AtListEnd() {
			wBytes, err := s.RawItem()
			if err != nil {
				return nil, fmt.Errorf("reading withdrawal: %w", err)
			}
			w, err := decodeWithdrawal(wBytes)
			if err != nil {
				return nil, fmt.Errorf("decoding withdrawal: %w", err)
			}
			withdrawals = append(withdrawals, w)
		}
		if err := s.ListEnd(); err != nil {
			return nil, fmt.Errorf("closing withdrawals list: %w", err)
		}
		if withdrawals == nil {
			withdrawals = []*types.Withdrawal{} // empty but non-nil
		}
		body.Withdrawals = withdrawals
	}

	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing body list: %w", err)
	}

	return body, nil
}

// decodeWithdrawal decodes an RLP-encoded withdrawal [index, validatorIndex, address, amount].
func decodeWithdrawal(data []byte) (*types.Withdrawal, error) {
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, err
	}
	w := &types.Withdrawal{}
	w.Index, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	w.ValidatorIndex, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	addrBytes, err := s.Bytes()
	if err != nil {
		return nil, err
	}
	copy(w.Address[types.AddressLength-len(addrBytes):], addrBytes)
	w.Amount, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	if err := s.ListEnd(); err != nil {
		return nil, err
	}
	return w, nil
}

// Reorg replaces the canonical chain from the fork point with the new chain
// ending at newHead. It finds the common ancestor between the current
// canonical chain and the new chain, un-indexes old canonical blocks,
// re-indexes the new canonical blocks, and updates the current block pointer.
//
// The expensive state re-execution is performed outside bc.mu so that engine
// API calls (newPayload, getPayload) are not blocked for tens of seconds
// during deep reorgs.
func (bc *Blockchain) Reorg(newHead *types.Block) error {
	bc.mu.Lock()
	chain, baseState, err := bc.reorg(newHead)
	bc.mu.Unlock()
	if err != nil {
		return err
	}

	// Re-execute the ancestor chain outside bc.mu.Lock.
	// If memSC already had the state for newHead (populated during insertBlock),
	// chain is nil and this loop is a no-op (O(1) reorg state update).
	statedb := baseState
	for i := len(chain) - 1; i >= 0; i-- {
		b := chain[i]
		if _, execErr := bc.processor.Process(b, statedb); execErr != nil {
			blockchainLog.Error("reorg_state_fail",
				"event", "reorg_state_fail",
				"hash", b.Hash().Hex(),
				"num", b.NumberU64(),
				"error", execErr,
			)
			return fmt.Errorf("re-derive state after reorg at %d: %w", b.NumberU64(), execErr)
		}
		bc.sc.put(b.Hash(), b.NumberU64(), statedb)
		bc.memSC.put(b.Hash(), b.NumberU64(), statedb)
	}

	// Materialise MemoryStateDB into TrieStateDB for memory efficiency if a
	// backing store is available. This keeps per-block RAM bounded to the
	// working set of a single block (dirty layer only).
	if mdb, ok := statedb.(*state.MemoryStateDB); ok && bc.gcMode != "" && bc.stateDB != nil {
		ts := state.NewTrieStateDBFromMemoryWithGCMode(bc.stateDB, mdb.Copy(), bc.gcMode)
		// Purge stale state so reverted side-block accounts do not corrupt the
		// state root on the next block.
		if clearErr := ts.ClearAllState(); clearErr != nil {
			blockchainLog.Warn("reorg_clear_state_failed",
				"event", "reorg_clear_state_failed",
				"error", clearErr,
			)
		}
		if _, commitErr := ts.Commit(); commitErr == nil {
			statedb = ts
		}
	}

	// Update the canonical state pointer under a brief lock (only pointer/cache writes).
	bc.mu.Lock()
	bc.sc.put(newHead.Hash(), newHead.NumberU64(), statedb)
	bc.memSC.put(newHead.Hash(), newHead.NumberU64(), statedb)
	// Protect the new head state from eviction.
	bc.sc.protect(newHead.Hash())
	bc.memSC.protect(newHead.Hash())
	bc.currentState = statedb
	bc.mu.Unlock()

	return nil
}

// reorg performs the canonical-chain bookkeeping for a reorg while bc.mu is
// held. It returns the ancestor block chain and base state needed for
// re-execution so that the caller can run the expensive part outside the lock.
func (bc *Blockchain) reorg(newHead *types.Block) ([]*types.Block, state.StateDB, error) {
	prevHead := bc.currentBlock
	metrics.ReorgsDetected.Inc()
	blockchainLog.Info("block_reorganized",
		"event", "block_reorganized",
		"oldHash", prevHead.Hash().Hex(),
		"oldNum", prevHead.NumberU64(),
		"newHash", newHead.Hash().Hex(),
		"newNum", newHead.NumberU64(),
	)

	// Build the new chain's ancestry: walk from newHead to genesis.
	// Collect blocks in reverse order (head first).
	var newChain []*types.Block
	current := newHead
	for {
		if _, ok := bc.blockCache[current.Hash()]; !ok {
			bc.blockCache[current.Hash()] = current
		}
		if current.NumberU64() == 0 {
			break
		}
		newChain = append(newChain, current)
		parent, ok := bc.blockCache[current.ParentHash()]
		if !ok {
			// Fall back to rawdb for evicted ancestors.
			parent = bc.readBlock(current.ParentHash())
			if parent == nil {
				return nil, nil, fmt.Errorf("%w: missing ancestor %v during reorg", ErrBlockNotFound, current.ParentHash())
			}
			bc.blockCache[current.ParentHash()] = parent
		}
		current = parent
	}

	// Determine the maximum height to clean up.
	oldHead := bc.currentBlock.NumberU64()
	newHeight := newHead.NumberU64()
	maxHeight := oldHead
	if newHeight > maxHeight {
		maxHeight = newHeight
	}

	// Un-index all canonical blocks above genesis.
	// Delete txLookup entries for any orphaned blocks so that rawdb lookups
	// do not return stale block numbers after the canonical chain changes.
	for n := maxHeight; n >= 1; n-- {
		if hash, ok := bc.canonCache[n]; ok {
			delete(bc.canonCache, n)
			rawdb.DeleteCanonicalHash(bc.db, n)

			bc.hc.mu.Lock()
			if h, ok := bc.hc.headers[n]; ok {
				delete(bc.hc.headersByHash, h.Hash())
				delete(bc.hc.headers, n)
			}
			bc.hc.mu.Unlock()

			// Remove in-memory txLookup entries and rawdb txLookup entries
			// for the orphaned block so stale lookups cannot be returned.
			if orphan, ok := bc.blockCache[hash]; ok {
				for _, tx := range orphan.Transactions() {
					delete(bc.txLookup, tx.Hash())
					rawdb.DeleteTxLookup(bc.db, tx.Hash())
				}
			}
		}
	}

	// Re-index the new chain from lowest block to highest.
	// Write txLookup entries for the incoming canonical blocks.
	for i := len(newChain) - 1; i >= 0; i-- {
		blk := newChain[i]
		hash := blk.Hash()
		num := blk.NumberU64()

		bc.blockCache[hash] = blk
		bc.canonCache[num] = hash
		bc.writeBlock(blk)
		rawdb.WriteCanonicalHash(bc.db, num, hash)
		bc.writeTxLookups(blk.Transactions(), num)
		for idx, tx := range blk.Transactions() {
			bc.txLookup[tx.Hash()] = TxLookupEntry{
				BlockHash:   hash,
				BlockNumber: num,
				TxIndex:     uint64(idx),
			}
		}

		h := blk.Header()
		bc.hc.mu.Lock()
		bc.hc.headersByHash[h.Hash()] = h
		bc.hc.headers[num] = h
		bc.hc.mu.Unlock()
	}

	// Update current block.
	bc.currentBlock = newHead
	rawdb.WriteHeadBlockHash(bc.db, newHead.Hash())
	rawdb.WriteHeadHeaderHash(bc.db, newHead.Hash())

	bc.hc.mu.Lock()
	bc.hc.currentHeader = newHead.Header()
	bc.hc.mu.Unlock()

	// Protect the new head state before clearing the cache.
	// This ensures the new head's state is preserved if it was already cached
	// during a prior insertBlock (common for short reorgs).
	bc.sc.protect(newHead.Hash())
	bc.memSC.protect(newHead.Hash())

	// Clear stale state cache entries: cached TrieStateDB Dups share the live
	// db which now reflects the state after the old canonical chain. Re-using
	// them as re-execution bases for the new fork would read wrong state.
	// Note: clear() now preserves the protected entry (newHead's state).
	bc.sc.clear()

	// Pre-collect the ancestor blocks and base state needed for re-execution.
	// collectAncestorsLocked checks memSC first; if the state for newHead was
	// cached during insertBlock, chain is nil and no re-execution is needed.
	// blockCache access is safe here because bc.mu is still held.
	chain, baseState := bc.collectAncestorsLocked(newHead)
	return chain, baseState, nil
}

// ChainLength returns the length of the canonical chain (genesis = 1).
func (bc *Blockchain) ChainLength() uint64 {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	return bc.currentBlock.NumberU64() + 1
}

// GetReceipts returns the receipts for a block identified by hash.
// Uses rcMu instead of the main mu to avoid blocking on concurrent InsertBlock.
func (bc *Blockchain) GetReceipts(blockHash types.Hash) []*types.Receipt {
	bc.rcMu.RLock()
	r, ok := bc.receiptCache[blockHash]
	bc.rcMu.RUnlock()
	if ok {
		return r
	}
	// Fallback: read from rawdb (thread-safe, no bc.mu needed).
	num, err := rawdb.ReadHeaderNumber(bc.db, blockHash)
	if err != nil {
		return nil
	}
	blk := bc.readBlock(blockHash)
	var txs []*types.Transaction
	if blk != nil {
		txs = blk.Transactions()
	}
	return bc.readReceiptsFromDB(num, blockHash, txs)
}

// GetBlockReceipts returns the receipts for the canonical block at the given number.
// Reads canonical hash from rawdb (thread-safe) and receipt cache under rcMu to
// avoid blocking on InsertBlock's bc.mu.Lock() during state execution.
func (bc *Blockchain) GetBlockReceipts(number uint64) []*types.Receipt {
	// Prefer rawdb for canonical hash: it's always consistent and doesn't
	// need bc.mu. Fall back to in-memory canonCache only if rawdb lacks the entry.
	hash, err := rawdb.ReadCanonicalHash(bc.db, number)
	if err != nil {
		bc.mu.RLock()
		h, ok := bc.canonCache[number]
		bc.mu.RUnlock()
		if !ok {
			return nil
		}
		hash = h
	}
	// Check receipt cache under fine-grained rcMu.
	bc.rcMu.RLock()
	r, ok2 := bc.receiptCache[hash]
	bc.rcMu.RUnlock()
	if ok2 {
		return r
	}
	// Fallback: read from rawdb (thread-safe).
	blk := bc.readBlock(hash)
	var txs []*types.Transaction
	if blk != nil {
		txs = blk.Transactions()
	}
	return bc.readReceiptsFromDB(number, hash, txs)
}

// GetLogs returns all logs from receipts for the block identified by hash.
func (bc *Blockchain) GetLogs(blockHash types.Hash) []*types.Log {
	bc.rcMu.RLock()
	receipts, ok := bc.receiptCache[blockHash]
	bc.rcMu.RUnlock()
	if !ok {
		// Fallback: read from rawdb (thread-safe, no bc.mu needed).
		num, err := rawdb.ReadHeaderNumber(bc.db, blockHash)
		if err == nil {
			blk := bc.readBlock(blockHash)
			var txs []*types.Transaction
			if blk != nil {
				txs = blk.Transactions()
			}
			receipts = bc.readReceiptsFromDB(num, blockHash, txs)
		}
	}
	var logs []*types.Log
	for _, receipt := range receipts {
		logs = append(logs, receipt.Logs...)
	}
	return logs
}

// GetTxLookupEntry returns the TxLookupEntry for a transaction hash,
// giving callers structured access to all three location fields as a unit.
// Returns (entry, false) if the transaction is not found.
func (bc *Blockchain) GetTxLookupEntry(txHash types.Hash) (TxLookupEntry, bool) {
	blockHash, blockNumber, txIndex, found := bc.GetTransactionLookup(txHash)
	if !found {
		return TxLookupEntry{}, false
	}
	return TxLookupEntry{BlockHash: blockHash, BlockNumber: blockNumber, TxIndex: txIndex}, true
}

// GetTransactionLookup returns the block location for a transaction hash.
func (bc *Blockchain) GetTransactionLookup(txHash types.Hash) (blockHash types.Hash, blockNumber uint64, txIndex uint64, found bool) {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if entry, ok := bc.txLookup[txHash]; ok {
		return entry.BlockHash, entry.BlockNumber, entry.TxIndex, true
	}
	// Fallback: try rawdb tx lookup index.
	num, err := rawdb.ReadTxLookup(bc.db, txHash)
	if err != nil {
		return types.Hash{}, 0, 0, false
	}
	hash, err := rawdb.ReadCanonicalHash(bc.db, num)
	if err != nil {
		return types.Hash{}, 0, 0, false
	}
	// Find the transaction index within the block.
	blk := bc.readBlock(hash)
	if blk == nil {
		return types.Hash{}, 0, 0, false
	}
	for i, tx := range blk.Transactions() {
		if tx.Hash() == txHash {
			return hash, num, uint64(i), true
		}
	}
	return types.Hash{}, 0, 0, false
}

// HistoryOldestBlock returns the oldest block number for which block bodies
// and receipts are available. Returns 0 if no history pruning has occurred.
// Used by the RPC layer to detect EIP-4444 pruned data.
func (bc *Blockchain) HistoryOldestBlock() uint64 {
	oldest, _ := rawdb.ReadHistoryOldest(bc.db)
	return oldest
}

// PruneHistory prunes block bodies and receipts older than
// (head - retention) blocks. Headers are preserved. Returns the number
// of blocks pruned.
func (bc *Blockchain) PruneHistory(retention uint64) (uint64, error) {
	bc.mu.Lock()
	defer bc.mu.Unlock()

	if bc.currentBlock == nil {
		return 0, nil
	}
	head := bc.currentBlock.NumberU64()
	pruned, _, err := rawdb.PruneHistory(bc.db, head, retention)
	return pruned, err
}

// writeReceipts encodes and persists receipts for a block.
func (bc *Blockchain) writeReceipts(number uint64, hash types.Hash, receipts []*types.Receipt) {
	if len(receipts) == 0 {
		return
	}
	var encoded []byte
	for _, r := range receipts {
		data, err := r.EncodeRLP()
		if err != nil {
			continue
		}
		encoded = append(encoded, data...)
	}
	rawdb.WriteReceipts(bc.db, number, hash, encoded)
}

// writeTxLookups persists transaction hash -> block number mappings.
func (bc *Blockchain) writeTxLookups(txs []*types.Transaction, blockNumber uint64) {
	for _, tx := range txs {
		rawdb.WriteTxLookup(bc.db, tx.Hash(), blockNumber)
	}
}

// rlpItemSize returns the encoded byte length of the next RLP item in data.
// Returns 0 for empty or malformed input.
func rlpItemSize(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	b := data[0]
	switch {
	case b < 0x80:
		return 1
	case b <= 0xB7:
		return 1 + int(b-0x80)
	case b <= 0xBF:
		lenOfLen := int(b - 0xB7)
		if len(data) < 1+lenOfLen {
			return 0
		}
		var l int
		for i := 0; i < lenOfLen; i++ {
			l = (l << 8) | int(data[1+i])
		}
		return 1 + lenOfLen + l
	case b <= 0xF7:
		return 1 + int(b-0xC0)
	default:
		lenOfLen := int(b - 0xF7)
		if len(data) < 1+lenOfLen {
			return 0
		}
		var l int
		for i := 0; i < lenOfLen; i++ {
			l = (l << 8) | int(data[1+i])
		}
		return 1 + lenOfLen + l
	}
}

// readReceiptsFromDB reads and decodes receipts from rawdb for the given block.
// txs is used to restore derived fields (TxHash, GasUsed) not stored in RLP.
// Called with bc.mu held (read or write).
func (bc *Blockchain) readReceiptsFromDB(num uint64, hash types.Hash, txs []*types.Transaction) []*types.Receipt {
	data, err := rawdb.ReadReceipts(bc.db, num, hash)
	if err != nil || len(data) == 0 {
		return nil
	}
	// Decode the concatenated receipt RLP encodings produced by writeReceipts.
	var receipts []*types.Receipt
	for len(data) > 0 {
		var itemSize int
		if data[0] < 0x80 {
			// Typed receipt: type byte followed by an RLP list.
			if len(data) < 2 {
				break
			}
			listSize := rlpItemSize(data[1:])
			if listSize <= 0 || 1+listSize > len(data) {
				break
			}
			itemSize = 1 + listSize
		} else {
			// Legacy receipt: plain RLP list.
			itemSize = rlpItemSize(data)
			if itemSize <= 0 || itemSize > len(data) {
				break
			}
		}
		r, err := types.DecodeReceiptRLP(data[:itemSize])
		if err != nil {
			break
		}
		receipts = append(receipts, r)
		data = data[itemSize:]
	}
	if len(receipts) == 0 {
		return nil
	}
	// Restore derived fields not persisted in the consensus RLP encoding.
	for i, r := range receipts {
		r.BlockHash = hash
		r.BlockNumber = new(big.Int).SetUint64(num)
		r.TransactionIndex = uint(i)
		if i < len(txs) {
			r.TxHash = txs[i].Hash()
		}
		if i == 0 {
			r.GasUsed = r.CumulativeGasUsed
		} else {
			r.GasUsed = r.CumulativeGasUsed - receipts[i-1].CumulativeGasUsed
		}
		for j, log := range r.Logs {
			log.BlockHash = hash
			log.BlockNumber = num
			log.TxHash = r.TxHash
			log.TxIndex = uint(i)
			log.Index = uint(j)
		}
	}
	return receipts
}

// EvictBlockFromCache removes a block from the in-memory block cache,
// forcing subsequent lookups to fall back to rawdb. Used in tests.
func (bc *Blockchain) EvictBlockFromCache(hash types.Hash) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	delete(bc.blockCache, hash)
}

// StoreBlockInCache inserts a block directly into the in-memory block cache
// without validation or state execution. Used in tests to set up fork scenarios.
func (bc *Blockchain) StoreBlockInCache(block *types.Block) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	bc.blockCache[block.Hash()] = block
}
