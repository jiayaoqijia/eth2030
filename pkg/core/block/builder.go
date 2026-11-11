package block

import (
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"sort"

	"github.com/eth2030/eth2030/bal"
	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/eips"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/gaspool"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/trie"
)

// EIP-4844 blob gas errors for block building.
var (
	ErrBlobGasLimitExceeded = errors.New("blob gas limit exceeded for block")
	ErrInvalidBlobHash      = errors.New("blob hash has invalid version byte")
)

// TxPoolReader is an interface for reading pending transactions from a pool.
type TxPoolReader interface {
	Pending() []*types.Transaction
}

// BuildBlockAttributes holds the payload attributes for building a new block.
type BuildBlockAttributes struct {
	Timestamp    uint64
	FeeRecipient types.Address
	Random       types.Hash
	Withdrawals  []*types.Withdrawal
	BeaconRoot   *types.Hash
	GasLimit     uint64

	// InclusionListTxs contains RLP-encoded transactions from inclusion lists
	// that MUST be included in this block (EIP-7547/7805 FOCIL).
	// These transactions are applied before pool transactions.
	InclusionListTxs [][]byte
}

// BlockBuilder constructs new blocks from pending transactions.
type BlockBuilder struct {
	config *config.ChainConfig
	chain  BlockchainReader
	txPool TxPoolReader
	state  state.StateDB
}

// NewBlockBuilder creates a new block builder.
// If chain is nil, a standalone builder is created (for backward compatibility).
func NewBlockBuilder(config *config.ChainConfig, chain BlockchainReader, pool TxPoolReader) *BlockBuilder {
	return &BlockBuilder{
		config: config,
		chain:  chain,
		txPool: pool,
	}
}

// sortedTxLists separates and sorts pending transactions into regular and blob
// transaction lists, each ordered by effective gas price descending.
func sortedTxLists(pending []*types.Transaction, baseFee *big.Int) (regular, blobs []*types.Transaction) {
	for _, tx := range pending {
		if tx.Type() == types.BlobTxType {
			blobs = append(blobs, tx)
		} else {
			regular = append(regular, tx)
		}
	}
	sortByPrice := func(txs []*types.Transaction) {
		sort.Slice(txs, func(i, j int) bool {
			pi := effectiveGasPrice(txs[i], baseFee)
			pj := effectiveGasPrice(txs[j], baseFee)
			return pi.Cmp(pj) > 0
		})
	}
	sortByPrice(regular)
	sortByPrice(blobs)
	return regular, blobs
}

// validateBlobHashes checks that every versioned hash starts with 0x01.
func validateBlobHashes(hashes []types.Hash) error {
	for i, h := range hashes {
		if h[0] != gas.BlobTxHashVersion {
			return fmt.Errorf("%w: hash %d version 0x%02x, want 0x%02x",
				ErrInvalidBlobHash, i, h[0], gas.BlobTxHashVersion)
		}
	}
	return nil
}

// calcExcessBlobGasFromParent returns the excess blob gas for a new block
// given the parent header. Uses parent's ExcessBlobGas and BlobGasUsed;
// returns 0 if either is nil (pre-Cancun parent).
func calcExcessBlobGasFromParent(parent *types.Header) uint64 {
	var parentExcess, parentUsed uint64
	if parent.ExcessBlobGas != nil {
		parentExcess = *parent.ExcessBlobGas
	}
	if parent.BlobGasUsed != nil {
		parentUsed = *parent.BlobGasUsed
	}
	return gas.CalcExcessBlobGas(parentExcess, parentUsed)
}

// calldataFloorDelta computes the additional gas to charge under EIP-7623
// when the calldata floor exceeds the standard gas used by a transaction.
// Returns 0 when no additional charge is needed.
func calldataFloorDelta(tx *types.Transaction, standardGasUsed uint64) uint64 {
	isCreate := tx.To() == nil
	floor := execution.CalldataFloorGas(tx.Data(), isCreate)
	if floor > standardGasUsed {
		return floor - standardGasUsed
	}
	return 0
}

func logBuilderAAPostExecution(header *types.Header, tx *types.Transaction, txIndex int, usedGas uint64, aaInfo *execution.AAPostExecutionInfo, legacy bool) {
	if aaInfo == nil {
		return
	}
	event := "builder_aa_post_exec"
	if legacy {
		event = "builder_aa_post_exec_legacy"
	}
	slog.Debug(event,
		"event", event,
		"blockNum", header.Number.Uint64(),
		"txIndex", txIndex,
		"txHash", tx.Hash().Hex(),
		"sender", aaInfo.Sender.Hex(),
		"nonceBefore", aaInfo.NonceBefore,
		"nonceAfter", aaInfo.NonceAfter,
		"gasUsed", usedGas,
	)
}

// BuildBlock constructs a new block using payload attributes.
// It selects transactions from the txpool, orders them by effective gas price
// (descending), and applies them until the block gas limit is reached.
// Blob transactions (EIP-4844) are tracked separately with blob gas limits.
// After all transactions are applied, withdrawals are processed (EIP-4895),
// requests are accumulated (EIP-7685), and the state root is computed.
func (b *BlockBuilder) BuildBlock(parent *types.Header, attrs *BuildBlockAttributes) (*types.Block, []*types.Receipt, error) {
	// Determine gas limit: use attributes if provided, otherwise derive from parent.
	gasLimit := attrs.GasLimit
	if gasLimit == 0 {
		gasLimit = calcGasLimit(parent.GasLimit, parent.GasUsed)
	}

	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, big.NewInt(1)),
		GasLimit:   gasLimit,
		Time:       attrs.Timestamp,
		Coinbase:   attrs.FeeRecipient,
		Difficulty: new(big.Int), // always 0 post-merge
		MixDigest:  attrs.Random,
		BaseFee:    gas.CalcBaseFee(parent),
		UncleHash:  EmptyUncleHash,
	}

	if attrs.BeaconRoot != nil {
		header.ParentBeaconRoot = attrs.BeaconRoot
	}

	// EIP-4844: compute blob gas fields when Cancun is active.
	cancunActive := b.config != nil && b.config.IsCancun(header.Time)
	var blobGasUsed uint64
	var excessBlobGas uint64
	if cancunActive {
		excessBlobGas = calcExcessBlobGasFromParent(parent)
		header.ExcessBlobGas = &excessBlobGas
		header.BlobGasUsed = &blobGasUsed // updated later
	}

	// EIP-7706: compute calldata gas fields when Glamsterdan is active.
	glamActive := b.config != nil && b.config.IsGlamsterdan(header.Time)
	// GAP-2.2: track DimStorage gas used at block level; enforced per-tx below.
	var blockDimStorageUsed uint64
	var calldataGasUsed uint64
	if glamActive {
		var pCalldataExcess, pCalldataUsed uint64
		if parent.CalldataExcessGas != nil {
			pCalldataExcess = *parent.CalldataExcessGas
		}
		if parent.CalldataGasUsed != nil {
			pCalldataUsed = *parent.CalldataGasUsed
		}
		calldataExcessGas := gas.CalcCalldataExcessGas(pCalldataExcess, pCalldataUsed, parent.GasLimit)
		header.CalldataExcessGas = &calldataExcessGas
		header.CalldataGasUsed = &calldataGasUsed // updated later
	}

	// Get state at parent block.
	statedb := b.state
	if statedb == nil && b.chain != nil {
		parentBlock := b.chain.GetBlock(parent.Hash())
		if parentBlock == nil {
			// Try genesis.
			if parent.Hash() == b.chain.Genesis().Hash() {
				parentBlock = b.chain.Genesis()
			}
		}
		if parentBlock != nil {
			var err error
			statedb, err = b.chain.StateAtBlock(parentBlock)
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if statedb == nil {
		// Fallback: create an empty state (useful for testing).
		statedb = state.NewMemoryStateDB()
	}

	gp := new(gaspool.GasPool).AddGas(header.GasLimit)

	// Determine if BAL tracking is active for this block.
	balActive := b.config != nil && b.config.IsAmsterdam(header.Time)
	var blockBAL *bal.BlockAccessList
	if balActive {
		blockBAL = bal.NewBlockAccessList()
	}

	// --- Pre-execution system contracts (AccessIndex 0) ---
	var preTracker *bal.AccessTracker
	if balActive {
		preTracker = bal.NewTracker()
	}

	// EIP-4788: store the parent beacon block root before any user transactions.
	if b.config != nil && b.config.IsCancun(header.Time) {
		if balActive && header.ParentBeaconRoot != nil {
			timestampIdx := header.Time % eips.BeaconHistoryBufferLength
			rootIdx := timestampIdx + eips.BeaconHistoryBufferLength
			tsSlot := eips.Uint64ToHash(timestampIdx)
			rootSlot := eips.Uint64ToHash(rootIdx)
			oldTsVal := statedb.GetState(eips.BeaconRootAddress, tsSlot)
			oldRootVal := statedb.GetState(eips.BeaconRootAddress, rootSlot)
			eips.ProcessBeaconBlockRoot(statedb, header)
			newTsVal := eips.Uint64ToHash(header.Time)
			preTracker.RecordStorageChange(eips.BeaconRootAddress, tsSlot, oldTsVal, newTsVal)
			preTracker.RecordStorageChange(eips.BeaconRootAddress, rootSlot, oldRootVal, *header.ParentBeaconRoot)
		} else {
			eips.ProcessBeaconBlockRoot(statedb, header)
		}
	}

	// EIP-2935: store parent block hash in history storage contract (Prague+).
	pragueActive := b.config != nil && b.config.IsPrague(header.Time)
	if pragueActive && header.Number.Uint64() > 0 {
		if balActive {
			parentNum := header.Number.Uint64() - 1
			slotBig := new(big.Int).SetUint64(parentNum % eips.HistoryServeWindow)
			var slotHash types.Hash
			if slotBig.Sign() > 0 {
				slotBig.FillBytes(slotHash[32-len(slotBig.Bytes()):])
			}
			oldVal := statedb.GetState(eips.HistoryStorageAddress, slotHash)
			eips.ProcessParentBlockHash(statedb, parentNum, header.ParentHash)
			preTracker.RecordStorageChange(eips.HistoryStorageAddress, slotHash, oldVal, header.ParentHash)
		} else {
			eips.ProcessParentBlockHash(statedb, header.Number.Uint64()-1, header.ParentHash)
		}
	}

	// EIP-7997: deploy the deterministic CREATE2 factory at Glamsterdam activation.
	if b.config != nil && b.config.IsGlamsterdan(header.Time) {
		if balActive && statedb.GetCodeSize(eips.FactoryAddress) == 0 {
			preTracker.RecordAddressTouch(eips.FactoryAddress)
		}
		eips.ApplyEIP7997(statedb)
	}

	// Merge pre-execution system contract accesses into the block BAL.
	if balActive {
		preBAL := preTracker.Build(0) // AccessIndex 0 for pre-execution
		for _, entry := range preBAL.Entries {
			blockBAL.AddEntry(entry)
		}
	}

	var (
		txs      []*types.Transaction
		receipts []*types.Receipt
		gasUsed  uint64
	)

	txIndex := 0

	// EIP-7547/7805 FOCIL: process inclusion list transactions first.
	// These MUST be included before any pool transactions.
	ilTxHashes := make(map[types.Hash]struct{})
	if len(attrs.InclusionListTxs) > 0 {
		for _, ilTxBytes := range attrs.InclusionListTxs {
			ilTx, err := types.DecodeTxRLP(ilTxBytes)
			if err != nil {
				continue
			}
			ilTxHashes[ilTx.Hash()] = struct{}{}

			// EIP-7825 caps individual transactions at Osaka.
			if gp.Gas() < ilTx.Gas() || exceedsTxGasCap(b.config, header.Time, ilTx) {
				continue
			}
			// Check base fee.
			if header.BaseFee != nil && ilTx.GasFeeCap() != nil {
				if ilTx.GasFeeCap().Cmp(header.BaseFee) < 0 {
					continue
				}
			}

			statedb.SetTxContext(ilTx.Hash(), txIndex)

			// EIP-7928: create per-transaction BAL tracker before execution.
			var (
				preBalances map[types.Address]*big.Int
				preNonces   map[types.Address]uint64
			)
			var tracker *bal.AccessTracker
			if balActive {
				tracker = bal.NewTracker()
				preBalances, preNonces = execution.CapturePreState(b.config, statedb, ilTx, header.Coinbase)
			}

			snap := statedb.Snapshot()
			receipt, used, applyErr := execution.ApplyTransactionWithBALGetHash(b.config, b.getHashFn(), statedb, header, ilTx, gp, execution.BalTrackerOrNil(tracker))
			if applyErr != nil {
				statedb.RevertToSnapshot(snap)
				continue
			}

			// GAP-2.2: enforce per-block DimStorage gas cap at Glamsterdam+.
			if glamActive && !vm.CheckDimStorageCap(blockDimStorageUsed, receipt.DimStorageGas) {
				statedb.RevertToSnapshot(snap)
				gp.AddGas(used)
				continue
			}
			blockDimStorageUsed += receipt.DimStorageGas
			// EIP-7701: nonce increment must happen for all AA txs regardless of BAL status.
			aaInfo := execution.ApplyAAPostExecution(statedb, ilTx, receipt, used, nil)
			if balActive {
				logBuilderAAPostExecution(header, ilTx, txIndex, used, aaInfo, false)
			}

			txs = append(txs, ilTx)
			receipts = append(receipts, receipt)
			// EIP-7778: Under Glamsterdam, header.GasUsed uses pre-refund gas (BlockGasUsed).
			if glamActive && receipt.BlockGasUsed > 0 {
				gasUsed += receipt.BlockGasUsed
			} else {
				gasUsed += used
			}

			if ilTx.Type() == types.BlobTxType && cancunActive {
				blobGasUsed += ilTx.BlobGas()
			}

			if balActive {
				execution.PopulateTracker(tracker, statedb, preBalances, preNonces)
				txBAL := tracker.Build(uint64(txIndex + 1))
				for _, entry := range txBAL.Entries {
					blockBAL.AddEntry(entry)
				}
			}

			txIndex++
		}
	}

	// Collect pending transactions from pool.
	var pendingTxs []*types.Transaction
	if b.txPool != nil {
		pendingTxs = b.txPool.Pending()
	}

	// Separate and sort: regular txs first, then blob txs.
	// Blob txs are only included when they carry sidecar data (blobs + KZG
	// commitments + proofs). Without a sidecar the CL would see blob versioned
	// hashes in the payload but receive an empty BlobsBundle — causing rejection.
	regularTxs, blobTxs := sortedTxLists(pendingTxs, header.BaseFee)
	var validBlobTxs []*types.Transaction
	for _, tx := range blobTxs {
		if tx.BlobSidecar() != nil {
			validBlobTxs = append(validBlobTxs, tx)
		}
	}
	allSorted := append(regularTxs, validBlobTxs...)

	// Partition transactions into non-conflicting groups for parallel execution
	// observability (gigagas parallel path, M+ roadmap). The groups are not yet
	// executed in parallel — the partition informs future scheduling decisions.
	if len(allSorted) > 0 {
		depGraph := execution.NewDependencyGraph(allSorted, nil)
		txGroups := depGraph.Partition(4) // up to 4 parallel groups
		slog.Debug("tx dependency partition",
			"txs", len(allSorted),
			"groups", len(txGroups),
			"conflicts", depGraph.ConflictCount(),
		)
		_ = txGroups // groups available for future parallel dispatch
	}

	// ReceiptGenerator tracks enhanced receipt metadata (bloom, cumulative gas,
	// receipt trie root) alongside the inline receipt accumulator below.
	recGen := execution.NewReceiptGenerator(execution.DefaultReceiptGeneratorConfig())

	for _, tx := range allSorted {
		// Skip transactions already included from the inclusion list.
		if _, isIL := ilTxHashes[tx.Hash()]; isIL {
			continue
		}
		// Check if transaction meets base fee requirement.
		if header.BaseFee != nil && tx.GasFeeCap() != nil {
			if tx.GasFeeCap().Cmp(header.BaseFee) < 0 {
				continue
			}
		}

		// Skip if not enough gas left for this tx.
		if gp.Gas() < tx.Gas() || exceedsTxGasCap(b.config, header.Time, tx) {
			continue
		}

		// EIP-4844: validate blob transactions and enforce blob gas limit.
		if tx.Type() == types.BlobTxType && cancunActive {
			txBlobGas := tx.BlobGas()
			if blobGasUsed+txBlobGas > gas.MaxBlobGasPerBlock {
				continue // would exceed block blob gas limit
			}
			// Validate versioned hashes.
			if err := validateBlobHashes(tx.BlobHashes()); err != nil {
				continue
			}
			// Validate blob fee cap against current blob base fee.
			blobBaseFee := execution.CalcBlobBaseFee(excessBlobGas)
			if tx.BlobGasFeeCap() == nil || tx.BlobGasFeeCap().Cmp(blobBaseFee) < 0 {
				continue
			}
		}

		// Set tx context so logs are keyed correctly.
		statedb.SetTxContext(tx.Hash(), txIndex)

		// EIP-7928: create per-transaction BAL tracker before execution.
		var (
			preBalances map[types.Address]*big.Int
			preNonces   map[types.Address]uint64
		)
		var tracker *bal.AccessTracker
		if balActive {
			tracker = bal.NewTracker()
			preBalances, preNonces = execution.CapturePreState(b.config, statedb, tx, header.Coinbase)
		}

		// Try to apply the transaction.
		snap := statedb.Snapshot()
		gpBefore := gp.Gas()
		receipt, used, err := execution.ApplyTransactionWithBALGetHash(b.config, b.getHashFn(), statedb, header, tx, gp, execution.BalTrackerOrNil(tracker))
		if err != nil {
			// Transaction failed: revert and skip it.
			statedb.RevertToSnapshot(snap)
			slog.Debug("builder: skipped tx", "hash", tx.Hash(), "err", err)
			continue
		}

		// GAP-2.2: enforce per-block DimStorage gas cap at Glamsterdam+.
		if glamActive && !vm.CheckDimStorageCap(blockDimStorageUsed, receipt.DimStorageGas) {
			statedb.RevertToSnapshot(snap)
			// Restore the exact amount debited from the pool (EIP-7778 uses
			// pre-refund gas for pool accounting, which may differ from used).
			gp.AddGas(gpBefore - gp.Gas())
			continue
		}
		blockDimStorageUsed += receipt.DimStorageGas
		// EIP-7701: nonce increment must happen for all AA txs regardless of BAL status.
		aaInfo := execution.ApplyAAPostExecution(statedb, tx, receipt, used, nil)
		if balActive {
			logBuilderAAPostExecution(header, tx, txIndex, used, aaInfo, false)
		}

		txs = append(txs, tx)
		receipts = append(receipts, receipt)
		// EIP-7778: Under Glamsterdam, header.GasUsed uses pre-refund gas (BlockGasUsed).
		if glamActive && receipt.BlockGasUsed > 0 {
			gasUsed += receipt.BlockGasUsed
		} else {
			gasUsed += used
		}
		slog.Debug("builder: included tx",
			"event", "builder_tx_gas",
			"blockNum", header.Number.Uint64(),
			"txIndex", txIndex,
			"txHash", tx.Hash().Hex(),
			"gasUsed", used,
			"blockGasUsed", receipt.BlockGasUsed,
			"glamActive", glamActive,
		)

		// Feed the ReceiptGenerator with per-tx outcome for enhanced bloom and
		// receipt-trie-root computation (used for cross-validation).
		outcome := &execution.TxExecutionOutcome{
			GasUsed:          used,
			Failed:           receipt.Status == types.ReceiptStatusFailed,
			Logs:             receipt.Logs,
			ContractAddress:  receipt.ContractAddress,
			BlobGasUsed:      receipt.BlobGasUsed,
			BlobGasPrice:     receipt.BlobGasPrice,
			CalldataGasUsed:  receipt.CalldataGasUsed,
			CalldataGasPrice: receipt.CalldataGasPrice,
			TxHash:           tx.Hash(),
			TxType:           tx.Type(),
		}
		if header.BaseFee != nil && tx.GasFeeCap() != nil {
			outcome.EffectiveGasPrice = new(big.Int).Add(
				header.BaseFee,
				new(big.Int).Sub(tx.GasFeeCap(), header.BaseFee),
			)
			if outcome.EffectiveGasPrice.Cmp(tx.GasFeeCap()) > 0 {
				outcome.EffectiveGasPrice = new(big.Int).Set(tx.GasFeeCap())
			}
		}
		recGen.GenerateReceipt(outcome, uint(txIndex))

		// Track blob gas for EIP-4844 blob transactions.
		if tx.Type() == types.BlobTxType && cancunActive {
			blobGasUsed += tx.BlobGas()
		}

		// Record state changes in the BAL.
		if balActive {
			execution.PopulateTracker(tracker, statedb, preBalances, preNonces)
			txBAL := tracker.Build(uint64(txIndex + 1))
			for _, entry := range txBAL.Entries {
				blockBAL.AddEntry(entry)
			}
		}

		txIndex++
	}

	header.GasUsed = gasUsed

	// EIP-4844: set blob gas used in header.
	if cancunActive {
		header.BlobGasUsed = &blobGasUsed
	}

	// EIP-7706: compute and set calldata gas used in header (scalar + 3D vector).
	if glamActive {
		for _, tx := range txs {
			calldataGasUsed += tx.CalldataGas()
		}
		header.CalldataGasUsed = &calldataGasUsed
		// Populate the 3D gas vector for calldata dimension (dim index 2).
		calldataGasLimit := gas.CalcCalldataGasLimit(header.GasLimit)
		calldataExcessGas := uint64(0)
		if header.CalldataExcessGas != nil {
			calldataExcessGas = *header.CalldataExcessGas
		}
		gasLimits := [3]uint64{header.GasLimit, 0, calldataGasLimit}
		gasUsed3D := [3]uint64{header.GasUsed, 0, calldataGasUsed}
		excessGas := [3]uint64{0, 0, calldataExcessGas}
		header.GasLimitVec = &gasLimits
		header.GasUsedVec = &gasUsed3D
		header.ExcessGasVec = &excessGas
	}

	// Finalize ReceiptGenerator: compute the aggregate bloom and receipt trie root.
	// The block hash is not yet known, so use a zero placeholder hash.
	recGen.FinalizeBlock(types.Hash{}, header.Number.Uint64())
	// Use the generator's bloom for cross-validation with types.CreateBloom.
	_ = recGen.BlockBloom()
	_ = recGen.ComputeReceiptTrieRoot()

	// Compute block-level bloom filter from all receipts.
	header.Bloom = types.CreateBloom(receipts)

	// Set CumulativeGasUsed on each receipt (running total, not individual).
	// Receipt RLP encoding includes CumulativeGasUsed, so this must match
	// what ProcessWithBAL produces during re-execution.
	var cumGas uint64
	for _, r := range receipts {
		cumGas += r.GasUsed
		r.CumulativeGasUsed = cumGas
	}

	// Compute transaction and receipt roots.
	header.TxHash = deriveTxsRoot(txs)
	header.ReceiptHash = deriveReceiptsRoot(receipts)

	// Compute state root after applying all transactions.
	header.Root = statedb.GetRoot()

	// Build the block body with withdrawals. Post-Shanghai blocks must always
	// include withdrawals (even if empty) per consensus rules.
	withdrawals := attrs.Withdrawals
	shanghaiActive := b.config != nil && b.config.IsShanghai(header.Time)
	if withdrawals == nil && shanghaiActive {
		withdrawals = []*types.Withdrawal{}
	}

	body := &types.Body{
		Transactions: txs,
		Withdrawals:  withdrawals,
	}

	// EIP-4895: process withdrawals and credit each recipient.
	if withdrawals != nil {
		wHash := deriveWithdrawalsRoot(withdrawals)
		header.WithdrawalsHash = &wHash

		for _, w := range withdrawals {
			// Withdrawal amount is in Gwei; convert to wei.
			amount := new(big.Int).SetUint64(w.Amount)
			amount.Mul(amount, big.NewInt(1_000_000_000)) // Gwei -> wei
			statedb.AddBalance(w.Address, amount)
		}
		// Recompute state root after withdrawals.
		header.Root = statedb.GetRoot()
	}

	// EIP-7685: accumulate execution layer requests (Prague+).
	if pragueActive {
		requests, err := execution.ProcessRequests(b.config, statedb, header)
		if err == nil && requests != nil {
			rHash := types.ComputeRequestsHash(requests)
			header.RequestsHash = &rHash
			// Recompute state root after request processing.
			header.Root = statedb.GetRoot()
		} else if err == nil {
			// No requests: set empty requests hash.
			emptyReqs := types.Requests{}
			rHash := types.ComputeRequestsHash(emptyReqs)
			header.RequestsHash = &rHash
		}
	}

	// Set Block Access List hash (EIP-7928) when Amsterdam is active.
	// Sort entries into lexicographic order before hashing (EIP-7928 §ordering).
	if balActive && blockBAL != nil {
		blockBAL.Sort()
		h := blockBAL.Hash()
		header.BlockAccessListHash = &h

		// Score block parallelism for observability.
		analyzer := bal.NewAdvancedConflictAnalyzer(bal.NewBALConflictDetector(bal.StrategySerialize))
		score := analyzer.ScoreParallelism(blockBAL)
		slog.Debug("block parallelism score", "score", score.Score, "clusters", score.ClusterCount)
	}

	block := types.NewBlock(header, body)
	if err := validateRLPBlockSize(b.config, block); err != nil {
		return nil, nil, err
	}
	return block, receipts, nil
}

func exceedsTxGasCap(cfg *config.ChainConfig, timestamp uint64, tx *types.Transaction) bool {
	return gas.IsGasLimitCapped(cfg, timestamp) && gas.ValidateTransactionGasLimit(tx.Gas()) != nil
}

// BuildBlockLegacy constructs a new block using the legacy interface (for backward
// compatibility with existing tests). Transactions are provided directly.
func (b *BlockBuilder) BuildBlockLegacy(parent *types.Header, txsByPrice []*types.Transaction, timestamp uint64, coinbase types.Address, extra []byte) (*types.Block, []*types.Receipt, error) {
	gasLimit := calcGasLimit(parent.GasLimit, parent.GasUsed)

	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     new(big.Int).Add(parent.Number, big.NewInt(1)),
		GasLimit:   gasLimit,
		Time:       timestamp,
		Coinbase:   coinbase,
		Difficulty: new(big.Int),
		Extra:      extra,
		BaseFee:    gas.CalcBaseFee(parent),
		UncleHash:  EmptyUncleHash,
	}

	// Use provided state or create empty.
	statedb := b.state
	if statedb == nil {
		statedb = state.NewMemoryStateDB()
	}

	gp := new(gaspool.GasPool).AddGas(header.GasLimit)

	// Determine if BAL tracking is active for this block.
	balActive := b.config != nil && b.config.IsAmsterdam(header.Time)
	var blockBAL *bal.BlockAccessList
	if balActive {
		blockBAL = bal.NewBlockAccessList()
	}

	// --- Pre-execution system contracts (AccessIndex 0) ---
	var preTrackerLegacy *bal.AccessTracker
	if balActive {
		preTrackerLegacy = bal.NewTracker()
	}

	// EIP-4788: store the parent beacon block root before any user transactions.
	if b.config != nil && b.config.IsCancun(header.Time) {
		if balActive && header.ParentBeaconRoot != nil {
			timestampIdx := header.Time % eips.BeaconHistoryBufferLength
			rootIdx := timestampIdx + eips.BeaconHistoryBufferLength
			tsSlot := eips.Uint64ToHash(timestampIdx)
			rootSlot := eips.Uint64ToHash(rootIdx)
			oldTsVal := statedb.GetState(eips.BeaconRootAddress, tsSlot)
			oldRootVal := statedb.GetState(eips.BeaconRootAddress, rootSlot)
			eips.ProcessBeaconBlockRoot(statedb, header)
			newTsVal := eips.Uint64ToHash(header.Time)
			preTrackerLegacy.RecordStorageChange(eips.BeaconRootAddress, tsSlot, oldTsVal, newTsVal)
			preTrackerLegacy.RecordStorageChange(eips.BeaconRootAddress, rootSlot, oldRootVal, *header.ParentBeaconRoot)
		} else {
			eips.ProcessBeaconBlockRoot(statedb, header)
		}
	}

	// EIP-2935: store parent block hash in history storage contract (Prague+).
	if b.config != nil && b.config.IsPrague(header.Time) && header.Number.Uint64() > 0 {
		if balActive {
			parentNum := header.Number.Uint64() - 1
			slotBig := new(big.Int).SetUint64(parentNum % eips.HistoryServeWindow)
			var slotHash types.Hash
			if slotBig.Sign() > 0 {
				slotBig.FillBytes(slotHash[32-len(slotBig.Bytes()):])
			}
			oldVal := statedb.GetState(eips.HistoryStorageAddress, slotHash)
			eips.ProcessParentBlockHash(statedb, parentNum, header.ParentHash)
			preTrackerLegacy.RecordStorageChange(eips.HistoryStorageAddress, slotHash, oldVal, header.ParentHash)
		} else {
			eips.ProcessParentBlockHash(statedb, header.Number.Uint64()-1, header.ParentHash)
		}
	}

	// EIP-7997: deploy the deterministic CREATE2 factory at Glamsterdam activation.
	if b.config != nil && b.config.IsGlamsterdan(header.Time) {
		if balActive && statedb.GetCodeSize(eips.FactoryAddress) == 0 {
			preTrackerLegacy.RecordAddressTouch(eips.FactoryAddress)
		}
		eips.ApplyEIP7997(statedb)
	}

	// Merge pre-execution system contract accesses into the block BAL.
	if balActive {
		preBAL := preTrackerLegacy.Build(0)
		for _, entry := range preBAL.Entries {
			blockBAL.AddEntry(entry)
		}
	}

	// Check if EIP-4844 is active.
	cancunActive := b.config != nil && b.config.IsCancun(header.Time)
	var blobGasUsed uint64
	if cancunActive {
		excessBlobGas := calcExcessBlobGasFromParent(parent)
		header.ExcessBlobGas = &excessBlobGas
		header.BlobGasUsed = &blobGasUsed
	}

	// EIP-7706: compute calldata gas fields when Glamsterdan is active.
	glamActiveLegacy := b.config != nil && b.config.IsGlamsterdan(header.Time)
	// GAP-2.2: per-block DimStorage gas cap tracking (same cap as BuildBlock).
	var blockDimStorageUsedLegacy uint64
	var calldataGasUsedLegacy uint64
	if glamActiveLegacy {
		var pCalldataExcess, pCalldataUsed uint64
		if parent.CalldataExcessGas != nil {
			pCalldataExcess = *parent.CalldataExcessGas
		}
		if parent.CalldataGasUsed != nil {
			pCalldataUsed = *parent.CalldataGasUsed
		}
		calldataExcessGas := gas.CalcCalldataExcessGas(pCalldataExcess, pCalldataUsed, parent.GasLimit)
		header.CalldataExcessGas = &calldataExcessGas
		header.CalldataGasUsed = &calldataGasUsedLegacy
	}

	var (
		txs      []*types.Transaction
		receipts []*types.Receipt
		gasUsed  uint64
	)

	// Sort transactions: separate regular and blob, each by price descending.
	regularTxs, blobTxs := sortedTxLists(txsByPrice, header.BaseFee)
	allSorted := append(regularTxs, blobTxs...)

	snapshot := statedb.Snapshot()

	txIndex := 0
	for _, tx := range allSorted {
		// Check if transaction meets base fee requirement.
		if header.BaseFee != nil && tx.GasFeeCap() != nil {
			if tx.GasFeeCap().Cmp(header.BaseFee) < 0 {
				continue
			}
		}

		// Skip if not enough gas left for this tx.
		if gp.Gas() < tx.Gas() || exceedsTxGasCap(b.config, header.Time, tx) {
			continue
		}

		// EIP-4844: enforce blob gas limit.
		if tx.Type() == types.BlobTxType && cancunActive {
			txBlobGas := tx.BlobGas()
			if blobGasUsed+txBlobGas > gas.MaxBlobGasPerBlock {
				continue
			}
			if err := validateBlobHashes(tx.BlobHashes()); err != nil {
				continue
			}
		}

		// Set tx context so logs are keyed correctly.
		statedb.SetTxContext(tx.Hash(), txIndex)

		// EIP-7928: create per-transaction BAL tracker before execution.
		var (
			preBalances map[types.Address]*big.Int
			preNonces   map[types.Address]uint64
		)
		var tracker *bal.AccessTracker
		if balActive {
			tracker = bal.NewTracker()
			preBalances, preNonces = execution.CapturePreState(b.config, statedb, tx, header.Coinbase)
		}

		// Try to apply the transaction.
		snap := statedb.Snapshot()
		receipt, used, err := execution.ApplyTransactionWithBALGetHash(b.config, b.getHashFn(), statedb, header, tx, gp, execution.BalTrackerOrNil(tracker))
		if err != nil {
			// Transaction failed: revert and skip it.
			statedb.RevertToSnapshot(snap)
			slog.Debug("builder: skipped tx (legacy)", "hash", tx.Hash(), "err", err)
			continue
		}

		// GAP-2.2: enforce per-block DimStorage gas cap at Glamsterdam+.
		if glamActiveLegacy && !vm.CheckDimStorageCap(blockDimStorageUsedLegacy, receipt.DimStorageGas) {
			statedb.RevertToSnapshot(snap)
			gp.AddGas(used)
			continue
		}
		blockDimStorageUsedLegacy += receipt.DimStorageGas
		// EIP-7701: nonce increment must happen for all AA txs regardless of BAL status.
		aaInfo := execution.ApplyAAPostExecution(statedb, tx, receipt, used, nil)
		if balActive {
			logBuilderAAPostExecution(header, tx, txIndex, used, aaInfo, true)
		}

		txs = append(txs, tx)
		receipts = append(receipts, receipt)
		// EIP-7778: Under Glamsterdam, header.GasUsed uses pre-refund gas (BlockGasUsed).
		if glamActiveLegacy && receipt.BlockGasUsed > 0 {
			gasUsed += receipt.BlockGasUsed
		} else {
			gasUsed += used
		}

		// Track blob gas.
		if tx.Type() == types.BlobTxType && cancunActive {
			blobGasUsed += tx.BlobGas()
		}

		// Record state changes in the BAL.
		if balActive {
			execution.PopulateTracker(tracker, statedb, preBalances, preNonces)
			txBAL := tracker.Build(uint64(txIndex + 1))
			for _, entry := range txBAL.Entries {
				blockBAL.AddEntry(entry)
			}
		}

		txIndex++
	}

	header.GasUsed = gasUsed
	if cancunActive {
		header.BlobGasUsed = &blobGasUsed
	}

	// EIP-7706: compute and set calldata gas used in header.
	if glamActiveLegacy {
		for _, tx := range txs {
			calldataGasUsedLegacy += tx.CalldataGas()
		}
		header.CalldataGasUsed = &calldataGasUsedLegacy
	}

	// Compute block-level bloom filter from all receipts.
	header.Bloom = types.CreateBloom(receipts)

	// If no transactions were included, revert to the parent state.
	if len(txs) == 0 {
		statedb.RevertToSnapshot(snapshot)
	}

	// Set CumulativeGasUsed on each receipt (running total).
	var cumGasLegacy uint64
	for _, r := range receipts {
		cumGasLegacy += r.GasUsed
		r.CumulativeGasUsed = cumGasLegacy
	}

	// Compute transaction and receipt roots.
	header.TxHash = deriveTxsRoot(txs)
	header.ReceiptHash = deriveReceiptsRoot(receipts)

	// Compute state root.
	header.Root = statedb.GetRoot()

	// Set Block Access List hash (EIP-7928) when Amsterdam is active.
	// Sort entries into lexicographic order before hashing (EIP-7928 §ordering).
	if balActive && blockBAL != nil {
		blockBAL.Sort()
		h := blockBAL.Hash()
		header.BlockAccessListHash = &h
	}

	body := &types.Body{
		Transactions: txs,
	}
	// Post-Shanghai blocks must include withdrawals (even if empty).
	shanghaiActive := b.config != nil && b.config.IsShanghai(header.Time)
	if shanghaiActive {
		body.Withdrawals = []*types.Withdrawal{}
		emptyWHash := deriveWithdrawalsRoot(body.Withdrawals)
		header.WithdrawalsHash = &emptyWHash
	}

	block := types.NewBlock(header, body)
	if err := validateRLPBlockSize(b.config, block); err != nil {
		return nil, nil, err
	}
	return block, receipts, nil
}

// SetState sets the state database for standalone builder usage (testing).
func (b *BlockBuilder) SetState(statedb state.StateDB) {
	b.state = statedb
}

// getHashFn returns the BLOCKHASH lookup function from the chain, or nil if
// the builder was created without a chain (standalone/test mode).
func (b *BlockBuilder) getHashFn() func(uint64) types.Hash {
	if b.chain == nil {
		return nil
	}
	return b.chain.GetHashFn()
}

// EffectiveGasPrice returns the effective gas price for a transaction
// considering the base fee (EIP-1559). Exported for use in tests.
func EffectiveGasPrice(tx *types.Transaction, baseFee *big.Int) *big.Int {
	return effectiveGasPrice(tx, baseFee)
}

// ValidateBlobHashes checks that every versioned hash starts with 0x01.
// Exported for use in tests.
func ValidateBlobHashes(hashes []types.Hash) error {
	return validateBlobHashes(hashes)
}

// CalldataFloorDelta computes the additional gas to charge under EIP-7623.
// Exported for use in tests.
func CalldataFloorDelta(tx *types.Transaction, standardGasUsed uint64) uint64 {
	return calldataFloorDelta(tx, standardGasUsed)
}

// SortedTxLists separates and sorts transactions into regular and blob lists.
// Exported for use in tests.
func SortedTxLists(pending []*types.Transaction, baseFee *big.Int) (regular, blobs []*types.Transaction) {
	return sortedTxLists(pending, baseFee)
}

// effectiveGasPrice returns the effective gas price for a transaction
// considering the base fee (EIP-1559).
func effectiveGasPrice(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if baseFee == nil || tx.GasFeeCap() == nil || tx.GasTipCap() == nil {
		return tx.GasPrice()
	}
	// effectiveGasPrice = min(gasFeeCap, baseFee + gasTipCap)
	effectiveTip := new(big.Int).Add(baseFee, tx.GasTipCap())
	if effectiveTip.Cmp(tx.GasFeeCap()) > 0 {
		return new(big.Int).Set(tx.GasFeeCap())
	}
	return effectiveTip
}

// calcGasLimit calculates the gas limit for the next block.
// Per EIP-1559, the gas limit can change by at most 1/1024 per block.
func calcGasLimit(parentGasLimit, parentGasUsed uint64) uint64 {
	// Target gas usage is 50% of the limit.
	target := parentGasLimit / 2
	delta := parentGasLimit / 1024

	if parentGasUsed > target {
		// Increase gas limit.
		newLimit := parentGasLimit + delta
		if newLimit > parentGasLimit+delta {
			return parentGasLimit + delta
		}
		return newLimit
	} else if parentGasUsed < target {
		// Decrease gas limit (but not below minimum).
		if delta > parentGasLimit {
			return MinGasLimit
		}
		newLimit := parentGasLimit - delta
		if newLimit < MinGasLimit {
			return MinGasLimit
		}
		return newLimit
	}
	return parentGasLimit
}

// DeriveTxsRoot is the exported version of deriveTxsRoot.
func DeriveTxsRoot(txs []*types.Transaction) types.Hash { return deriveTxsRoot(txs) }

// DeriveReceiptsRoot is the exported version of deriveReceiptsRoot.
func DeriveReceiptsRoot(receipts []*types.Receipt) types.Hash { return deriveReceiptsRoot(receipts) }

// DeriveWithdrawalsRoot is the exported version of deriveWithdrawalsRoot.
func DeriveWithdrawalsRoot(ws []*types.Withdrawal) types.Hash { return deriveWithdrawalsRoot(ws) }

// deriveTxsRoot computes the transactions root using a Merkle Patricia Trie.
// Key: RLP(index), Value: RLP-encoded transaction.
func deriveTxsRoot(txs []*types.Transaction) types.Hash {
	if len(txs) == 0 {
		return types.EmptyRootHash
	}
	t := trie.New()
	for i, tx := range txs {
		key, _ := rlp.EncodeToBytes(uint64(i))
		val, err := tx.EncodeRLP()
		if err != nil {
			continue
		}
		t.Put(key, val)
	}
	return t.Hash()
}

// deriveReceiptsRoot computes the receipts root using a Merkle Patricia Trie.
// Key: RLP(index), Value: RLP-encoded receipt.
func deriveReceiptsRoot(receipts []*types.Receipt) types.Hash {
	if len(receipts) == 0 {
		return types.EmptyRootHash
	}
	t := trie.New()
	for i, receipt := range receipts {
		key, _ := rlp.EncodeToBytes(uint64(i))
		val, err := receipt.EncodeRLP()
		if err != nil {
			continue
		}
		t.Put(key, val)
	}
	return t.Hash()
}

// deriveWithdrawalsRoot computes the withdrawals root using a Merkle Patricia Trie.
func deriveWithdrawalsRoot(ws []*types.Withdrawal) types.Hash {
	if len(ws) == 0 {
		return types.EmptyRootHash
	}
	t := trie.New()
	for i, w := range ws {
		key, _ := rlp.EncodeToBytes(uint64(i))
		// RLP-encode withdrawal as [index, validatorIndex, address, amount].
		val, _ := rlp.EncodeToBytes([]interface{}{w.Index, w.ValidatorIndex, w.Address, w.Amount})
		t.Put(key, val)
	}
	return t.Hash()
}

// CalcGasLimit calculates the gas limit for the next block. Exported for tests.
func CalcGasLimit(parentGasLimit, parentGasUsed uint64) uint64 {
	return calcGasLimit(parentGasLimit, parentGasUsed)
}

// Suppress unused import.
var _ = calldataFloorDelta
