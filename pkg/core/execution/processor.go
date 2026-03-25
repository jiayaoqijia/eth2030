package execution

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/bal"
	corconfig "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/eips"
	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/gaspool"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
	"github.com/eth2030/eth2030/log"
	"github.com/eth2030/eth2030/metrics"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/trie"
)

var execLog = log.Default().Module("eth/execution")

// balTrackerOrNil converts a typed *bal.AccessTracker to the vm.BALTracker
// interface. When t is nil it returns a true nil interface, preventing the
// classic Go typed-nil-interface pitfall that would make a nil check pass
// while the underlying pointer is nil.
func balTrackerOrNil(t *bal.AccessTracker) vm.BALTracker {
	if t == nil {
		return nil
	}
	return t
}

type AAPostExecutionInfo struct {
	Sender      types.Address
	NonceBefore uint64
	NonceAfter  uint64
}

func txSenderHex(tx *types.Transaction) string {
	if sender := tx.Sender(); sender != nil {
		return sender.Hex()
	}
	return ""
}

// ApplyAAPostExecution mirrors the successful AA post-execution state mutation
// used during block replay so block building and verification compute the same root.
// Per EIP-7701, the SCA validates its own nonce during the validation phase.
// After successful execution, the protocol increments the nonce (same as legacy txs).
func ApplyAAPostExecution(statedb state.StateDB, tx *types.Transaction, receipt *types.Receipt, usedGas uint64, paymasterValidator eips.PaymasterValidator) *AAPostExecutionInfo {
	if tx.Type() != types.AATxType || receipt.Status != types.ReceiptStatusSuccessful {
		return nil
	}
	aatx, ok := tx.Inner().(*types.AATx)
	if !ok {
		return nil
	}
	nonceBefore := statedb.GetNonce(aatx.Sender)
	// Increment nonce after successful AA transaction execution.
	// The SCA validates nonce during validation phase, but the protocol
	// increments it post-execution (same semantics as legacy transactions).
	statedb.SetNonce(aatx.Sender, nonceBefore+1)
	info := &AAPostExecutionInfo{
		Sender:      aatx.Sender,
		NonceBefore: nonceBefore,
		NonceAfter:  statedb.GetNonce(aatx.Sender),
	}
	if paymasterValidator != nil {
		uo := buildAAUserOp(aatx)
		_ = paymasterValidator.PostOp(uo, nil, usedGas, statedb)
	}
	return info
}

// logAccountStateChanges logs balance and nonce changes for key accounts involved in a transaction.
// This is used for debugging state root mismatches and consensus issues.
func logAccountStateChanges(statedb state.StateDB, tx *types.Transaction, preBalances map[types.Address]*big.Int, preNonces map[types.Address]uint64, blockNum uint64, txIndex int) {
	if len(preBalances) == 0 && len(preNonces) == 0 {
		return
	}
	for addr, preBal := range preBalances {
		postBal := statedb.GetBalance(addr)
		preNonce := preNonces[addr]
		postNonce := statedb.GetNonce(addr)
		if preBal.Cmp(postBal) != 0 || preNonce != postNonce {
			execLog.Debug("tx_account_state_change",
				"event", "tx_account_state_change",
				"blockNum", blockNum,
				"txIndex", txIndex,
				"txHash", tx.Hash().Hex(),
				"address", addr.Hex(),
				"preBalance", preBal.String(),
				"postBalance", postBal.String(),
				"balanceDelta", new(big.Int).Sub(postBal, preBal).String(),
				"preNonce", preNonce,
				"postNonce", postNonce,
				"nonceDelta", int64(postNonce)-int64(preNonce),
			)
		}
	}
}

const (
	// TxGas is the base gas cost of a transaction (21000).
	TxGas uint64 = 21000
	// TxDataZeroGas is the gas cost per zero byte of transaction data.
	TxDataZeroGas uint64 = 4
	// TxDataNonZeroGas is the gas cost per non-zero byte of transaction data.
	TxDataNonZeroGas uint64 = 16
	// TxCreateGas is the extra gas for contract creation transactions.
	TxCreateGas uint64 = 32000

	// EIP-7702: per-authorization base gas cost charged for every entry
	// in the authorization list, regardless of whether the target account
	// is empty or not.
	PerAuthBaseCost uint64 = 12500

	// EIP-7702: additional gas charged per authorization entry that targets
	// an account that does not yet exist in the state trie (empty account).
	PerEmptyAccountCost uint64 = 25000
)

var (
	ErrNonceTooLow         = errors.New("nonce too low")
	ErrNonceTooHigh        = errors.New("nonce too high")
	ErrInsufficientBalance = errors.New("insufficient balance for transfer")
	ErrGasLimitExceeded    = errors.New("gas limit exceeded")
	ErrIntrinsicGasTooLow  = errors.New("intrinsic gas too low")
	ErrContractCreation    = errors.New("contract creation failed")
	ErrContractCall        = errors.New("contract call failed")
	// ErrBALFeasibilityViolated is returned when a block generates too many BAL
	// items relative to gas consumed (EIP-7928 §early-rejection, every 8 txs).
	ErrBALFeasibilityViolated = errors.New("BAL feasibility check failed: items × ITEM_COST > gasUsed")
)

// StateProcessor processes blocks by applying transactions sequentially.
type StateProcessor struct {
	config             *corconfig.ChainConfig
	getHash            vm.GetHashFunc
	slasher            corconfig.PaymasterSlasher // optional: slashes paymasters on bad settlement (AA-1.3)
	paymasterValidator eips.PaymasterValidator    // optional: validates AA paymaster willingness (EIP-7701)
}

// NewStateProcessor creates a new state processor.
func NewStateProcessor(config *corconfig.ChainConfig) *StateProcessor {
	return &StateProcessor{config: config}
}

// SetGetHash sets the block hash lookup function for the BLOCKHASH opcode.
func (p *StateProcessor) SetGetHash(fn vm.GetHashFunc) {
	p.getHash = fn
}

// SetSlasher wires a corconfig.PaymasterSlasher into the processor. When set, the
// processor will call SlashOnBadSettlement whenever a frame tx paymaster
// fails to cover gas after execution (AA-1.3).
func (p *StateProcessor) SetSlasher(s corconfig.PaymasterSlasher) {
	p.slasher = s
}

// SetPaymasterValidator wires an eips.PaymasterValidator into the processor.
// When set, PostOp is called on the validator after each successful AA tx (EIP-7701).
func (p *StateProcessor) SetPaymasterValidator(v eips.PaymasterValidator) {
	p.paymasterValidator = v
}

// Process executes all transactions in a block sequentially and returns the receipts.
func (p *StateProcessor) Process(block *types.Block, statedb state.StateDB) ([]*types.Receipt, error) {
	result, err := p.ProcessWithBAL(block, statedb)
	if err != nil {
		return nil, err
	}
	return result.Receipts, nil
}

// ProcessWithBAL executes all transactions in a block and returns the receipts
// along with the computed Block Access List (EIP-7928). The BAL is populated
// only when the Amsterdam fork is active; otherwise it is nil.
func (p *StateProcessor) ProcessWithBAL(block *types.Block, statedb state.StateDB) (*ProcessResult, error) {
	var (
		receipts []*types.Receipt
		gasPool  = new(gaspool.GasPool).AddGas(block.GasLimit())
		header   = block.Header()
	)

	execLog.Debug("block_process_start",
		"event", "block_process_start",
		"hash", block.Hash().Hex(),
		"num", block.NumberU64(),
		"txCount", len(block.Transactions()),
		"gasLimit", header.GasLimit,
		"baseFee", header.BaseFee,
		"timestamp", header.Time,
	)

	// Determine if BAL tracking is active for this block.
	balActive := p.config != nil && p.config.IsAmsterdam(header.Time)

	var blockBAL *bal.BlockAccessList
	if balActive {
		blockBAL = bal.NewBlockAccessList()
	}

	// --- Pre-execution system contracts (AccessIndex 0) ---
	// Capture old slot values BEFORE system calls modify state, then record
	// the changes into a pre-execution tracker.
	var preTracker *bal.AccessTracker
	if balActive {
		preTracker = bal.NewTracker()
	}

	// EIP-4788: store the parent beacon block root in the beacon root contract.
	if p.config != nil && p.config.IsCancun(header.Time) {
		if balActive && header.ParentBeaconRoot != nil {
			// Capture old values before the system call writes.
			timestampIdx := header.Time % eips.BeaconHistoryBufferLength
			rootIdx := timestampIdx + eips.BeaconHistoryBufferLength
			tsSlot := eips.Uint64ToHash(timestampIdx)
			rootSlot := eips.Uint64ToHash(rootIdx)
			oldTsVal := statedb.GetState(eips.BeaconRootAddress, tsSlot)
			oldRootVal := statedb.GetState(eips.BeaconRootAddress, rootSlot)

			eips.ProcessBeaconBlockRoot(statedb, header)

			// Record the storage changes.
			newTsVal := eips.Uint64ToHash(header.Time)
			preTracker.RecordStorageChange(eips.BeaconRootAddress, tsSlot, oldTsVal, newTsVal)
			preTracker.RecordStorageChange(eips.BeaconRootAddress, rootSlot, oldRootVal, *header.ParentBeaconRoot)
		} else {
			eips.ProcessBeaconBlockRoot(statedb, header)
		}
	}

	// EIP-2935: store parent block hash in history storage contract (Prague+).
	if p.config != nil && p.config.IsPrague(header.Time) && header.Number.Uint64() > 0 {
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
	if p.config != nil && p.config.IsGlamsterdan(header.Time) {
		if balActive && statedb.GetCodeSize(eips.FactoryAddress) == 0 {
			// Factory is about to be deployed — record the code touch.
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

	var cumulativeGasUsed uint64
	var cumulativeBlockGasUsed uint64
	var cumulativeCalldataGasUsed uint64
	amsterdamActive := p.config != nil && p.config.IsAmsterdam(header.Time)
	glamsterdamActive := p.config != nil && p.config.IsGlamsterdan(header.Time)

	// EIP-7706: compute calldata gas limit for this block.
	calldataGasActive := glamsterdamActive && header.CalldataExcessGas != nil
	var calldataGasLimit uint64
	if calldataGasActive {
		calldataGasLimit = gas.CalcCalldataGasLimit(header.GasLimit)
	}

	for i, tx := range block.Transactions() {
		statedb.SetTxContext(tx.Hash(), i)

		// Log TX execution start with key fields for traceability.
		txFrom := tx.Sender()
		fromHex := ""
		if txFrom != nil {
			fromHex = txFrom.Hex()
		}
		toHex := ""
		if tx.To() != nil {
			toHex = tx.To().Hex()
		}
		execLog.Debug("tx_exec_start",
			"event", "tx_exec_start",
			"blockNum", block.NumberU64(),
			"txIndex", i,
			"txHash", tx.Hash().Hex(),
			"txType", tx.Type(),
			"from", fromHex,
			"to", toHex,
			"nonce", tx.Nonce(),
			"gas", tx.Gas(),
			"value", tx.Value(),
		)

		// EIP-7928: create per-transaction BAL tracker and inject into EVM.
		// The tracker is created BEFORE execution so opcodes record state
		// accesses (storage reads, storage changes, address touches) during
		// EVM interpretation.
		var (
			preBalances map[types.Address]*big.Int
			preNonces   map[types.Address]uint64
		)
		var tracker *bal.AccessTracker
		if balActive {
			tracker = bal.NewTracker()
			preBalances, preNonces = capturePreState(statedb, tx)
		}

		// EIP-7701: pre-execution AA validation and gas estimation for AA txs.
		// Native AA is activated at the Amsterdam fork (part of Glamsterdam).
		if balActive && tx.Type() == types.AATxType {
			if aatx, ok := tx.Inner().(*types.AATx); ok {
				uo := buildAAUserOp(aatx)
				// ValidateUserOpState checks balance, nonce, and paymaster allowance.
				if err := eips.ValidateUserOpState(uo, statedb, header.BaseFee); err != nil {
					execLog.Warn("tx_aa_state_invalid",
						"event", "tx_aa_state_invalid",
						"blockNum", block.NumberU64(),
						"txIndex", i,
						"txHash", tx.Hash().Hex(),
						"error", err,
					)
					return nil, fmt.Errorf("aa: state validation failed tx %d: %w", i, err)
				}
				// EstimateUserOpGas provides recommended gas limits for observability.
				_, _, _ = eips.EstimateUserOpGas(uo, statedb, header.BaseFee)
			}
		}

		receipt, usedGas, err := applyTransactionFull(p.config, p.getHash, statedb, header, tx, gasPool, balTrackerOrNil(tracker), p.slasher)
		if err != nil {
			fromHex = txSenderHex(tx)
			execLog.Error("tx_evm_error",
				"event", "tx_evm_error",
				"blockNum", block.NumberU64(),
				"txIndex", i,
				"txHash", tx.Hash().Hex(),
				"from", fromHex,
				"nonce", tx.Nonce(),
				"error", err,
			)
			return nil, fmt.Errorf("could not apply tx %d [%v]: %w", i, tx, err)
		}

		// Log execution outcome: TX_EXECUTED (success) or TX_REVERTED (EVM revert).
		fromHex = txSenderHex(tx)
		txType := tx.Type()
		if receipt.Status == types.ReceiptStatusSuccessful {
			contractHex := ""
			if receipt.ContractAddress != (types.Address{}) {
				contractHex = receipt.ContractAddress.Hex()
			}
			execLog.Debug("tx_executed",
				"event", "tx_executed",
				"blockNum", block.NumberU64(),
				"txIndex", i,
				"txHash", tx.Hash().Hex(),
				"txType", txType,
				"from", fromHex,
				"to", toHex,
				"nonce", tx.Nonce(),
				"gasUsed", usedGas,
				"blockGasUsed", receipt.BlockGasUsed,
				"logCount", len(receipt.Logs),
				"contractCreated", contractHex,
			)
		} else {
			execLog.Debug("tx_reverted",
				"event", "tx_reverted",
				"blockNum", block.NumberU64(),
				"txIndex", i,
				"txHash", tx.Hash().Hex(),
				"txType", txType,
				"from", fromHex,
				"to", toHex,
				"nonce", tx.Nonce(),
				"gasUsed", usedGas,
				"blockGasUsed", receipt.BlockGasUsed,
			)
		}

		// EIP-7701: post-execution AA lifecycle hooks for successful AA txs.
		// The nonce increment MUST happen for all AA transactions, regardless of
		// Amsterdam/BAL status. Paymaster PostOp is only called when Amsterdam is active.
		aaInfo := ApplyAAPostExecution(statedb, tx, receipt, usedGas, p.paymasterValidator)
		if balActive && aaInfo != nil {
			execLog.Debug("tx_aa_post_exec",
				"event", "tx_aa_post_exec",
				"blockNum", block.NumberU64(),
				"txIndex", i,
				"txHash", tx.Hash().Hex(),
				"sender", aaInfo.Sender.Hex(),
				"nonceBefore", aaInfo.NonceBefore,
				"nonceAfter", aaInfo.NonceAfter,
				"gasUsed", usedGas,
			)
		}

		metrics.EVMExecutions.Inc()
		metrics.EVMGasUsed.Add(int64(usedGas))
		cumulativeBlockGasUsed += receipt.BlockGasUsed
		if receipt.BlockGasUsed > receipt.GasUsed {
			execLog.Debug("tx_gas_accounting_split",
				"event", "tx_gas_accounting_split",
				"blockNum", block.NumberU64(),
				"txIndex", i,
				"txHash", tx.Hash().Hex(),
				"txType", tx.Type(),
				"gasUsed", receipt.GasUsed,
				"blockGasUsed", receipt.BlockGasUsed,
				"delta", receipt.BlockGasUsed-receipt.GasUsed,
				"amsterdamActive", amsterdamActive,
				"glamsterdamActive", glamsterdamActive,
			)
		}

		// Debug log for account state changes (helps debug consensus issues).
		if balActive {
			logAccountStateChanges(statedb, tx, preBalances, preNonces, block.NumberU64(), i)
		}

		// Track cumulative gas across all transactions in the block.
		cumulativeGasUsed += usedGas
		receipt.CumulativeGasUsed = cumulativeGasUsed

		// EIP-7706: track calldata gas and enforce the per-block limit.
		if calldataGasActive {
			txCalldataGas := tx.CalldataGas()
			if cumulativeCalldataGasUsed+txCalldataGas > calldataGasLimit {
				execLog.Warn("calldata_gas_exceeded",
					"event", "calldata_gas_exceeded",
					"blockNum", block.NumberU64(),
					"txIndex", i,
					"txHash", tx.Hash().Hex(),
					"cumulative", cumulativeCalldataGasUsed,
					"txCalldataGas", txCalldataGas,
					"limit", calldataGasLimit,
				)
				return nil, fmt.Errorf("calldata gas limit exceeded: used %d + tx %d > limit %d",
					cumulativeCalldataGasUsed, txCalldataGas, calldataGasLimit)
			}
			cumulativeCalldataGasUsed += txCalldataGas
		}
		receipt.TransactionIndex = uint(i)
		receipt.BlockHash = block.Hash()
		receipt.BlockNumber = new(big.Int).Set(header.Number)

		// Set log context fields (BlockNumber, BlockHash, Index).
		setLogContext(receipt, header, block.Hash())

		receipts = append(receipts, receipt)

		// After successful tx, merge opcode-level accesses + balance/nonce diffs.
		if balActive {
			populateTracker(tracker, statedb, preBalances, preNonces)
			txBAL := tracker.Build(uint64(i + 1)) // AccessIndex 1..n for transactions
			for _, entry := range txBAL.Entries {
				blockBAL.AddEntry(entry)
			}

			// EIP-7928 §early-rejection: every 8 txs verify that BAL items
			// generated so far do not exceed gas consumed / ITEM_COST.
			// This catches adversarial blocks that produce dense BAL cheaply.
			if (i+1)%8 == 0 && cumulativeGasUsed > 0 {
				if uint64(blockBAL.Len())*bal.BALItemCost > cumulativeGasUsed {
					return nil, fmt.Errorf("%w: items=%d × 2000=%d > gasUsed=%d (after tx %d)",
						ErrBALFeasibilityViolated, blockBAL.Len(),
						uint64(blockBAL.Len())*bal.BALItemCost, cumulativeGasUsed, i)
				}
			}
		}
	}

	// Assign global log indices across all receipts so that each log
	// in the block has a unique, sequential Index value.
	var logIndex uint
	for _, receipt := range receipts {
		for _, log := range receipt.Logs {
			log.Index = logIndex
			logIndex++
		}
	}

	// EIP-4895: process beacon chain withdrawals after all transactions.
	// Withdrawals are applied post-Shanghai (activated with the merge).
	if p.config != nil && p.config.IsShanghai(header.Time) {
		if balActive {
			postTracker := bal.NewTracker()

			// Capture pre-withdrawal balances.
			preWithdrawalBals := make(map[types.Address]*big.Int)
			for _, w := range block.Withdrawals() {
				if w == nil {
					continue
				}
				if _, seen := preWithdrawalBals[w.Address]; !seen {
					preWithdrawalBals[w.Address] = new(big.Int).Set(statedb.GetBalance(w.Address))
				}
			}

			ProcessWithdrawals(statedb, block.Withdrawals())

			// Record balance changes from withdrawals.
			for addr, preBal := range preWithdrawalBals {
				postBal := statedb.GetBalance(addr)
				if preBal.Cmp(postBal) != 0 {
					postTracker.RecordBalanceChange(addr, preBal, postBal)
				}
			}

			// AccessIndex = n+1 for post-execution (after all transactions).
			postBAL := postTracker.Build(uint64(len(block.Transactions()) + 1))
			for _, entry := range postBAL.Entries {
				blockBAL.AddEntry(entry)
			}
		} else {
			ProcessWithdrawals(statedb, block.Withdrawals())
		}
	}

	// Sort BAL entries into strict lexicographic order by (Address, AccessIndex)
	// per EIP-7928 §ordering before returning.
	if blockBAL != nil {
		blockBAL.Sort()
	}

	execLog.Debug("block_process_done",
		"event", "block_process_done",
		"hash", block.Hash().Hex(),
		"num", block.NumberU64(),
		"txCount", len(block.Transactions()),
		"gasUsed", cumulativeGasUsed,
		"blockGasUsed", cumulativeBlockGasUsed,
		"amsterdamActive", amsterdamActive,
		"glamsterdamActive", glamsterdamActive,
		"receiptCount", len(receipts),
		"balEntries", func() int {
			if blockBAL != nil {
				return blockBAL.Len()
			}
			return 0
		}(),
	)

	return &ProcessResult{
		Receipts:        receipts,
		BlockAccessList: blockBAL,
	}, nil
}

// capturePreState captures balance and nonce values for addresses involved
// in a transaction before it is applied. This allows computing the delta
// for the BAL after the transaction completes.
func capturePreState(statedb state.StateDB, tx *types.Transaction) (map[types.Address]*big.Int, map[types.Address]uint64) {
	balances := make(map[types.Address]*big.Int)
	nonces := make(map[types.Address]uint64)

	// Sender (from cached sender on the tx).
	if sender := tx.Sender(); sender != nil {
		balances[*sender] = new(big.Int).Set(statedb.GetBalance(*sender))
		nonces[*sender] = statedb.GetNonce(*sender)
	}

	// Recipient.
	if to := tx.To(); to != nil {
		balances[*to] = new(big.Int).Set(statedb.GetBalance(*to))
		nonces[*to] = statedb.GetNonce(*to)
	}

	return balances, nonces
}

// populateTracker records balance and nonce changes into the BAL tracker
// by comparing pre-tx state snapshots with post-tx state.
func populateTracker(tracker *bal.AccessTracker, statedb state.StateDB, preBalances map[types.Address]*big.Int, preNonces map[types.Address]uint64) {
	for addr, preBal := range preBalances {
		postBal := statedb.GetBalance(addr)
		if preBal.Cmp(postBal) != 0 {
			tracker.RecordBalanceChange(addr, preBal, postBal)
		}
	}
	for addr, preNonce := range preNonces {
		postNonce := statedb.GetNonce(addr)
		if preNonce != postNonce {
			tracker.RecordNonceChange(addr, preNonce, postNonce)
		}
	}
}

// ProcessResult holds the output of block processing: receipts, EIP-7685 requests,
// and the Block Access List (EIP-7928) when the Amsterdam fork is active.
type ProcessResult struct {
	Receipts        []*types.Receipt
	Requests        types.Requests
	BlockAccessList *bal.BlockAccessList
}

// ProcessWithdrawals applies EIP-4895 beacon chain withdrawals to the state.
// Each withdrawal credits the specified address with the withdrawal amount.
// The amount field is denominated in Gwei and is converted to Wei (1 Gwei = 1e9 Wei).
// Withdrawals do not consume gas and are applied after all transactions.
// A nil or empty withdrawals slice is a no-op.
func ProcessWithdrawals(statedb state.StateDB, withdrawals []*types.Withdrawal) {
	for _, w := range withdrawals {
		if w == nil {
			continue
		}
		// Convert Gwei to Wei: amount_wei = amount_gwei * 1e9.
		amount := new(big.Int).SetUint64(w.Amount)
		amount.Mul(amount, big.NewInt(1_000_000_000))
		statedb.AddBalance(w.Address, amount)
	}
}

// CalcWithdrawalsHash computes the withdrawals root hash from a slice of
// withdrawals. Each withdrawal is RLP-encoded as [index, validatorIndex,
// address, amount] and inserted into a Merkle Patricia Trie keyed by its
// position index. Returns EmptyRootHash for nil or empty withdrawals.
func CalcWithdrawalsHash(withdrawals []*types.Withdrawal) types.Hash {
	return calcWithdrawalsRoot(withdrawals)
}

// calcWithdrawalsRoot computes the withdrawals root using a Merkle Patricia Trie.
func calcWithdrawalsRoot(ws []*types.Withdrawal) types.Hash {
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

// ProcessWithRequests executes all transactions in a block and then collects
// EIP-7685 execution layer requests from system contracts. Use this for
// post-Prague blocks that include the requests_hash field.
func (p *StateProcessor) ProcessWithRequests(block *types.Block, statedb state.StateDB) (*ProcessResult, error) {
	receipts, err := p.Process(block, statedb)
	if err != nil {
		return nil, err
	}

	requests, err := ProcessRequests(p.config, statedb, block.Header())
	if err != nil {
		return nil, fmt.Errorf("processing execution requests: %w", err)
	}

	return &ProcessResult{
		Receipts: receipts,
		Requests: requests,
	}, nil
}

// ProcessRequests collects execution layer requests from system contracts
// after all transactions are processed. This implements EIP-7685.
//
// Per the EIP, requests are generated by calling specific system contracts:
//   - Deposit requests (0x00): read from the deposit contract
//   - Withdrawal requests (0x01): call the withdrawal request contract
//   - Consolidation requests (0x02): call the consolidation request contract
//
// System calls use a special system address as the caller, with a large gas
// allowance. The calls are not user-initiated transactions and do not
// consume block gas.
func ProcessRequests(config *corconfig.ChainConfig, statedb state.StateDB, header *types.Header) (types.Requests, error) {
	if config == nil || !config.IsPrague(header.Time) {
		return nil, nil
	}

	var requests types.Requests

	// Collect deposit requests (type 0x00).
	depositRequests, err := processDepositRequests(statedb)
	if err != nil {
		return nil, fmt.Errorf("deposit requests: %w", err)
	}
	requests = append(requests, depositRequests...)

	// Collect withdrawal requests (type 0x01).
	withdrawalRequests, err := processWithdrawalRequests(statedb)
	if err != nil {
		return nil, fmt.Errorf("withdrawal requests: %w", err)
	}
	requests = append(requests, withdrawalRequests...)

	// Collect consolidation requests (type 0x02).
	consolidationRequests, err := processConsolidationRequests(statedb)
	if err != nil {
		return nil, fmt.Errorf("consolidation requests: %w", err)
	}
	requests = append(requests, consolidationRequests...)

	return requests, nil
}

// processDepositRequests reads deposit request data from the deposit contract.
func processDepositRequests(statedb state.StateDB) (types.Requests, error) {
	addr := types.DepositContractAddress
	if !statedb.Exist(addr) {
		return nil, nil
	}
	return readRequestsFromStorage(statedb, addr, types.DepositRequestType)
}

// processWithdrawalRequests calls the withdrawal request system contract
// and collects the resulting requests.
func processWithdrawalRequests(statedb state.StateDB) (types.Requests, error) {
	addr := types.WithdrawalRequestAddress
	if !statedb.Exist(addr) {
		return nil, nil
	}
	return readRequestsFromStorage(statedb, addr, types.WithdrawalRequestType)
}

// processConsolidationRequests calls the consolidation request system contract
// and collects the resulting requests.
func processConsolidationRequests(statedb state.StateDB) (types.Requests, error) {
	addr := types.ConsolidationRequestAddress
	if !statedb.Exist(addr) {
		return nil, nil
	}
	return readRequestsFromStorage(statedb, addr, types.ConsolidationRequestType)
}

// requestCountSlot is the well-known storage slot (slot 0) where system
// contracts store the count of pending requests.
var requestCountSlot = types.Hash{}

// requestDataSlotBase is the base storage slot (slot 1) where system
// contracts store request data sequentially.
var requestDataSlotBase = types.BytesToHash([]byte{0x01})

// readRequestsFromStorage reads requests from a system contract's storage.
//
// Convention: slot 0 holds the request count (as a uint256). Slots 1..N each
// hold one request's data as a raw 32-byte word. The contract is expected to
// pack request data into consecutive slots starting at slot 1.
//
// After reading, the count slot is cleared to zero (requests are consumed).
func readRequestsFromStorage(statedb state.StateDB, addr types.Address, reqType byte) (types.Requests, error) {
	countVal := statedb.GetState(addr, requestCountSlot)
	count := countToUint64(countVal)
	if count == 0 {
		return nil, nil
	}

	var requests types.Requests
	for i := uint64(0); i < count; i++ {
		slot := incrementSlot(requestDataSlotBase, i)
		data := statedb.GetState(addr, slot)

		if data == (types.Hash{}) {
			continue
		}

		trimmed := trimTrailingZeros(data[:])
		if len(trimmed) > 0 {
			requests = append(requests, types.NewRequest(reqType, trimmed))
		}
	}

	// Clear the request count after consumption.
	statedb.SetState(addr, requestCountSlot, types.Hash{})

	return requests, nil
}

// countToUint64 extracts a uint64 count from a 32-byte storage value.
// The value is stored as big-endian uint256; we take the low 8 bytes.
func countToUint64(val types.Hash) uint64 {
	var count uint64
	for i := 24; i < 32; i++ {
		count = (count << 8) | uint64(val[i])
	}
	return count
}

// incrementSlot adds an offset to a storage slot hash. Used to compute
// sequential slot addresses: slot = base + offset.
func incrementSlot(base types.Hash, offset uint64) types.Hash {
	var result types.Hash
	copy(result[:], base[:])
	carry := offset
	for i := 31; i >= 0 && carry > 0; i-- {
		sum := uint64(result[i]) + (carry & 0xFF)
		result[i] = byte(sum & 0xFF)
		carry = (carry >> 8) + (sum >> 8)
	}
	return result
}

// trimTrailingZeros removes trailing zero bytes from a slice.
func trimTrailingZeros(b []byte) []byte {
	end := len(b)
	for end > 0 && b[end-1] == 0 {
		end--
	}
	if end == 0 {
		return nil
	}
	out := make([]byte, end)
	copy(out, b[:end])
	return out
}

// ApplyTransaction applies a single transaction to the state and returns a receipt.
// It is a convenience wrapper that calls applyTransaction with no GetHash function.
func ApplyTransaction(config *corconfig.ChainConfig, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *gaspool.GasPool) (*types.Receipt, uint64, error) {
	return applyTransaction(config, nil, statedb, header, tx, gp)
}

// ApplyTransactionWithBAL applies a single transaction to the state with
// EIP-7928 BAL tracking enabled. The provided tracker is injected into the
// EVM so opcodes record state accesses during execution.
func ApplyTransactionWithBAL(config *corconfig.ChainConfig, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *gaspool.GasPool, tracker vm.BALTracker) (*types.Receipt, uint64, error) {
	return applyTransactionWithBAL(config, nil, statedb, header, tx, gp, tracker)
}

// ApplyTransactionWithBALGetHash is like ApplyTransactionWithBAL but also
// accepts a GetHash function for the BLOCKHASH opcode. The block builder
// should use this variant so that BLOCKHASH returns correct values, matching
// the verifier.
func ApplyTransactionWithBALGetHash(config *corconfig.ChainConfig, getHash vm.GetHashFunc, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *gaspool.GasPool, tracker vm.BALTracker) (*types.Receipt, uint64, error) {
	return applyTransactionWithBAL(config, getHash, statedb, header, tx, gp, tracker)
}

// applyTransaction is the internal implementation that accepts an optional GetHash function.
func applyTransaction(config *corconfig.ChainConfig, getHash vm.GetHashFunc, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *gaspool.GasPool) (*types.Receipt, uint64, error) {
	return applyTransactionInternal(config, getHash, statedb, header, tx, gp, nil, nil)
}

// applyTransactionWithBAL is like applyTransaction but injects the BAL tracker
// into the EVM so that opcodes record state accesses for EIP-7928.
func applyTransactionWithBAL(config *corconfig.ChainConfig, getHash vm.GetHashFunc, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *gaspool.GasPool, tracker vm.BALTracker) (*types.Receipt, uint64, error) {
	return applyTransactionInternal(config, getHash, statedb, header, tx, gp, tracker, nil)
}

// applyTransactionFull is like applyTransactionWithBAL but also accepts a
// corconfig.PaymasterSlasher for EIP-8141 paymaster slashing (AA-1.3).
func applyTransactionFull(config *corconfig.ChainConfig, getHash vm.GetHashFunc, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *gaspool.GasPool, tracker vm.BALTracker, slasher corconfig.PaymasterSlasher) (*types.Receipt, uint64, error) {
	return applyTransactionInternal(config, getHash, statedb, header, tx, gp, tracker, slasher)
}

// applyTransactionInternal is the shared implementation for all applyTransaction
// variants. tracker enables EIP-7928 BAL recording; slasher enables EIP-8141
// paymaster slashing on bad gas settlement.
func applyTransactionInternal(config *corconfig.ChainConfig, getHash vm.GetHashFunc, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *gaspool.GasPool, tracker vm.BALTracker, slasher corconfig.PaymasterSlasher) (*types.Receipt, uint64, error) {
	// Enforce I+ fork guard for PQ transactions.
	rules := config.Rules(header.Number, config.IsMerge(), header.Time)
	if tx.Type() == types.PQTransactionType && !rules.IsIPlus {
		return nil, 0, fmt.Errorf("PQ transactions require I+ fork")
	}

	// Recover sender if not already cached.
	if tx.Sender() == nil {
		if aatx, ok := tx.Inner().(*types.AATx); ok {
			tx.SetSender(aatx.Sender)
		} else if tx.Type() == types.PQTransactionType {
			if pk := tx.PQPublicKey(); len(pk) > 0 {
				tx.SetSender(types.PQPubKeyToAddress(pk))
			}
		} else if config.ChainID != nil {
			signer := types.NewLondonSigner(config.ChainID.Uint64())
			if addr, err := signer.Sender(tx); err == nil {
				tx.SetSender(addr)
			}
		}
	}
	msg := corconfig.TransactionToMessage(tx)
	// AA-1.3: wire slasher so applyMessage can slash paymasters on bad settlement.
	msg.Slasher = slasher

	preexistingFromWarm := statedb.AddressInAccessList(msg.From)
	preexistingToWarm := false
	if msg.To != nil {
		preexistingToWarm = statedb.AddressInAccessList(*msg.To)
	}
	preexistingCoinbaseWarm := statedb.AddressInAccessList(header.Coinbase)
	preexistingWarmSlots := 0
	for _, tuple := range msg.AccessList {
		for _, key := range tuple.StorageKeys {
			if _, warm := statedb.SlotInAccessList(tuple.Address, key); warm {
				preexistingWarmSlots++
			}
		}
	}
	if preexistingFromWarm || preexistingToWarm || preexistingCoinbaseWarm || preexistingWarmSlots > 0 {
		toHex := ""
		if msg.To != nil {
			toHex = msg.To.Hex()
		}
		execLog.Debug("tx_access_list_reset",
			"event", "tx_access_list_reset",
			"blockNum", header.Number.Uint64(),
			"txHash", tx.Hash().Hex(),
			"txType", tx.Type(),
			"from", msg.From.Hex(),
			"to", toHex,
			"fromWarm", preexistingFromWarm,
			"toWarm", preexistingToWarm,
			"coinbaseWarm", preexistingCoinbaseWarm,
			"warmSlots", preexistingWarmSlots,
		)
	}
	statedb.ClearAccessList()

	snapshot := statedb.Snapshot()

	result, err := applyMessage(config, getHash, statedb, header, &msg, gp, tracker)
	if err != nil {
		statedb.RevertToSnapshot(snapshot)
		return nil, 0, err
	}

	// Create receipt. CumulativeGasUsed is set to this transaction's gas
	// usage as a placeholder; the caller (Process/ProcessWithBAL) is
	// responsible for accumulating it across all transactions in the block.
	var receiptStatus uint64
	if result.Failed() {
		receiptStatus = types.ReceiptStatusFailed
	} else {
		receiptStatus = types.ReceiptStatusSuccessful
	}

	receipt := types.NewReceipt(receiptStatus, result.UsedGas)
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = result.UsedGas
	receipt.EffectiveGasPrice = msgEffectiveGasPrice(&msg, header.BaseFee)
	receipt.Type = tx.Type()

	// Set contract address for contract creation transactions.
	if msg.To == nil {
		receipt.ContractAddress = result.ContractAddress
	}

	// Set EIP-4844 blob gas fields.
	if blobGas := tx.BlobGas(); blobGas > 0 {
		receipt.BlobGasUsed = blobGas
		if header.ExcessBlobGas != nil {
			receipt.BlobGasPrice = calcBlobBaseFee(*header.ExcessBlobGas)
		}
	}

	// Set EIP-7706 calldata gas fields.
	if calldataGas := tx.CalldataGas(); calldataGas > 0 && header.CalldataExcessGas != nil {
		receipt.CalldataGasUsed = calldataGas
		receipt.CalldataGasPrice = gas.CalcCalldataBaseFeeFromHeader(header)
	}

	// GAP-2.2: propagate DimStorage gas to receipt for block-level cap enforcement.
	receipt.DimStorageGas = result.DimStorageGas

	// EIP-7778: propagate BlockGasUsed for block gas limit validation.
	// This is the pre-refund gas used, which should match header.GasUsed.
	receipt.BlockGasUsed = result.BlockGasUsed

	// Collect logs from state and compute bloom filter.
	receipt.Logs = statedb.GetLogs(tx.Hash())
	receipt.Bloom = types.LogsBloom(receipt.Logs)

	return receipt, result.UsedGas, nil
}

// setLogContext populates block-level context fields on each log in the
// receipt: BlockNumber, BlockHash, and the global Index (log position within
// the block). The TxHash and TxIndex are already set by StateDB.AddLog.
func setLogContext(receipt *types.Receipt, header *types.Header, blockHash types.Hash) {
	for _, log := range receipt.Logs {
		log.BlockNumber = header.Number.Uint64()
		log.BlockHash = blockHash
	}
}

// intrinsicGas computes the base gas cost of a transaction before EVM execution.
// For EIP-7702 SetCode transactions, authCount is the number of authorization
// entries. Per EIP-7702, each authorization is charged PER_EMPTY_ACCOUNT_COST
// (25000) upfront; refunds are handled during authorization processing.
func intrinsicGas(data []byte, isCreate, isShanghai bool, authCount uint64) uint64 {
	g := TxGas
	if isCreate {
		g += TxCreateGas
	}
	for _, b := range data {
		if b == 0 {
			g += TxDataZeroGas
		} else {
			g += TxDataNonZeroGas
		}
	}
	// EIP-3860: init code word gas for contract creations (Shanghai+).
	if isCreate && isShanghai {
		words := (uint64(len(data)) + 31) / 32
		g += words * vm.InitCodeWordGas
	}
	// EIP-7702: per-authorization gas cost.
	// Per the spec, charge PER_EMPTY_ACCOUNT_COST (25000) for each authorization
	// upfront. Refunds are added during authorization processing if the
	// authority account already exists.
	g += authCount * PerEmptyAccountCost
	return g
}

// EIP-7623: calldata gas cost floor constants.
// These define a higher floor cost for calldata to incentivize blob usage.
const (
	// TotalCostFloorPerToken is the floor gas cost per non-zero calldata byte
	// under EIP-7623. The actual gas charged is max(standard_cost, floor_cost).
	TotalCostFloorPerToken uint64 = 10

	// StandardTokenCost is the standard EIP-2028 calldata cost for non-zero bytes.
	StandardTokenCost uint64 = 16

	// FloorTokenCost is the EIP-7623 floor cost applied after execution.
	// floorDataGas = tokens * TOTAL_COST_FLOOR_PER_TOKEN
	// where tokens = zero_bytes * 1 + nonzero_bytes * 4
	FloorTokenCost uint64 = 10
)

// EIP-7976: Glamsterdam calldata floor cost increase.
// STANDARD_TOKEN_COST stays at 4 (unchanged per non-zero byte in standard path).
// TOTAL_COST_FLOOR_PER_TOKEN increases from 10 to 16.
// floor_tokens = (zero_bytes + nonzero_bytes) * 4 (all bytes weighted equally).
const (
	TotalCostFloorPerTokenGlamst uint64 = 16
)

// calldataFloorGas computes the EIP-7623 calldata floor gas cost.
// tokens = zero_bytes * 1 + nonzero_bytes * 4
// floor_gas = 21000 + tokens * TOTAL_COST_FLOOR_PER_TOKEN
func calldataFloorGas(data []byte, isCreate bool) uint64 {
	var tokens uint64
	for _, b := range data {
		if b == 0 {
			tokens += 1
		} else {
			tokens += 4
		}
	}
	floor := TxGas + tokens*TotalCostFloorPerToken
	if isCreate {
		floor += TxCreateGas
	}
	return floor
}

// calldataFloorGasGlamst computes the EIP-7976 calldata floor gas cost for Glamsterdam.
// Per EIP-7976: floor_tokens = (zero_bytes + nonzero_bytes) * 4
// floor_gas = TX_BASE_COST + floor_tokens * TOTAL_COST_FLOOR_PER_TOKEN
// The TX_BASE_COST is the Glamsterdam value from EIP-2780.
func calldataFloorGasGlamst(data []byte, accessList types.AccessList, isCreate bool) uint64 {
	// EIP-7976: floor tokens = (zero + nonzero) * 4 = total_bytes * 4
	calldataFloorTokens := uint64(len(data)) * 4

	// EIP-7981: include access list tokens in the floor calculation.
	accessListTokens := accessListDataTokens(accessList)

	totalTokens := calldataFloorTokens + accessListTokens
	floor := vm.TxBaseGlamsterdam + totalTokens*TotalCostFloorPerTokenGlamst
	if isCreate {
		floor += TxCreateGas
	}
	return floor
}

// calldataTokens computes calldata tokens for the standard path.
// tokens = zero_bytes * 1 + nonzero_bytes * 4
func calldataTokens(data []byte) uint64 {
	var tokens uint64
	for _, b := range data {
		if b == 0 {
			tokens++
		} else {
			tokens += 4
		}
	}
	return tokens
}

// accessListDataTokens computes data tokens for access list entries per EIP-7981.
// tokens = zero_bytes + nonzero_bytes * 4 for all addresses and storage keys.
func accessListDataTokens(accessList types.AccessList) uint64 {
	var zero, nonzero uint64
	for _, tuple := range accessList {
		// Count bytes in address (20 bytes).
		for _, b := range tuple.Address {
			if b == 0 {
				zero++
			} else {
				nonzero++
			}
		}
		// Count bytes in each storage key (32 bytes).
		for _, key := range tuple.StorageKeys {
			for _, b := range key {
				if b == 0 {
					zero++
				} else {
					nonzero++
				}
			}
		}
	}
	return zero + nonzero*4
}

// accessListGas computes the gas cost for an EIP-2930 access list.
// Per EIP-2930: 2400 gas per address, 1900 gas per storage key.
func accessListGas(accessList types.AccessList) uint64 {
	var g uint64
	for _, tuple := range accessList {
		g += 2400                                  // TxAccessListAddressGas
		g += uint64(len(tuple.StorageKeys)) * 1900 // TxAccessListStorageKeyGas
	}
	return g
}

// accessListGasGlamst computes gas cost for access lists under Glamsterdam.
// EIP-8038: increased per-entry costs.
// EIP-7981: adds data token cost (TOTAL_COST_FLOOR_PER_TOKEN * tokens).
func accessListGasGlamst(accessList types.AccessList) uint64 {
	var g uint64
	for _, tuple := range accessList {
		g += vm.AccessListAddressGlamst
		g += uint64(len(tuple.StorageKeys)) * vm.AccessListStorageGlamst
	}
	// EIP-7981: charge data cost on access list.
	tokens := accessListDataTokens(accessList)
	g += tokens * TotalCostFloorPerTokenGlamst
	return g
}

// intrinsicGasGlamst computes intrinsic gas for Glamsterdam per EIP-2780.
// TX_BASE_COST = 4500. Calldata pricing unchanged. Access list uses Glamsterdam costs.
// GAS_NEW_ACCOUNT surcharge when value > 0 to non-existent non-precompile non-create.
func intrinsicGasGlamst(data []byte, isCreate bool, hasValue bool, toExists bool, authCount uint64) uint64 {
	g := vm.TxBaseGlamsterdam
	if isCreate {
		g += TxCreateGas
	}
	// Standard calldata pricing (unchanged by EIP-2780).
	for _, b := range data {
		if b == 0 {
			g += TxDataZeroGas
		} else {
			g += TxDataNonZeroGas
		}
	}
	// EIP-2780: new-account surcharge for value transfers to non-existent accounts.
	if !isCreate && hasValue && !toExists {
		g += vm.GasNewAccount
	}
	// EIP-7702: per-authorization gas cost.
	// Per the spec, charge PER_EMPTY_ACCOUNT_COST (25000) for each authorization
	// upfront. Refunds are added during authorization processing if the
	// authority account already exists.
	g += authCount * PerEmptyAccountCost
	return g
}

// applyMessage executes a transaction message against the state.
// An optional BALTracker can be provided for EIP-7928 state access tracking;
// when non-nil, the tracker is injected into the EVM so opcodes record
// storage reads, storage changes, and address touches during execution.
func applyMessage(config *corconfig.ChainConfig, getHash vm.GetHashFunc, statedb state.StateDB, header *types.Header, msg *corconfig.Message, gp *gaspool.GasPool, balTracker ...vm.BALTracker) (*ExecutionResult, error) {
	// Validate and consume gas from the pool
	if err := gp.SubGas(msg.GasLimit); err != nil {
		return nil, err
	}

	// Validate nonce
	// EIP-8141: FrameTx uses its own nonce validation in ExecuteFrameTx with msg.FrameSender.
	// Skip the standard nonce check here to avoid checking against the wrong address (msg.From).
	if msg.TxType != types.FrameTxType {
		stateNonce := statedb.GetNonce(msg.From)
		if msg.Nonce < stateNonce {
			gp.AddGas(msg.GasLimit)
			return nil, fmt.Errorf("%w: address %v, tx nonce: %d, state nonce: %d", ErrNonceTooLow, msg.From, msg.Nonce, stateNonce)
		}
		if msg.Nonce > stateNonce {
			gp.AddGas(msg.GasLimit)
			return nil, fmt.Errorf("%w: address %v, tx nonce: %d, state nonce: %d", ErrNonceTooHigh, msg.From, msg.Nonce, stateNonce)
		}
	}

	// EIP-3607: Reject transactions from senders with deployed code.
	// Only EOAs (externally owned accounts) can originate transactions.
	// Exception: accounts with EIP-7702 delegation designators (0xef0100 prefix)
	// are allowed to send transactions since they are still EOAs that have
	// delegated their code execution.
	// EIP-8141: For FrameTx, check msg.FrameSender instead of msg.From.
	senderAddr := msg.From
	if msg.TxType == types.FrameTxType && msg.FrameSender != (types.Address{}) {
		senderAddr = msg.FrameSender
	}
	if codeHash := statedb.GetCodeHash(senderAddr); codeHash != (types.Hash{}) && codeHash != types.EmptyCodeHash {
		// Check if the sender has EIP-7702 delegated code, which is allowed.
		if code := statedb.GetCode(senderAddr); !types.HasDelegationPrefix(code) {
			gp.AddGas(msg.GasLimit)
			return nil, fmt.Errorf("sender not an EOA: address %v, codehash: %v", senderAddr, codeHash)
		}
	}

	// EIP-1559 (London+): validate gas fee caps for ALL transaction types.
	// For legacy/access-list txs, GasFeeCap and GasTipCap both equal GasPrice,
	// so the tip > cap check always passes, but the baseFee check still applies.
	isEIP1559Tx := msg.TxType >= types.DynamicFeeTxType
	if header.BaseFee != nil && header.BaseFee.Sign() > 0 {
		if msg.GasFeeCap != nil && msg.GasTipCap != nil {
			// Reject if MaxPriorityFeePerGas > MaxFeePerGas.
			if msg.GasFeeCap.Cmp(msg.GasTipCap) < 0 {
				gp.AddGas(msg.GasLimit)
				return nil, fmt.Errorf("max priority fee per gas higher than max fee per gas: tip %s, cap %s", msg.GasTipCap, msg.GasFeeCap)
			}
			// Reject if MaxFeePerGas < BaseFee (applies to all tx types under London).
			if msg.GasFeeCap.Cmp(header.BaseFee) < 0 {
				gp.AddGas(msg.GasLimit)
				return nil, fmt.Errorf("max fee per gas less than block base fee: fee %s, baseFee %s", msg.GasFeeCap, header.BaseFee)
			}
		}
	}

	// Calculate effective gas price per EIP-1559.
	gasPrice := msgEffectiveGasPrice(msg, header.BaseFee)
	gasCost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(msg.GasLimit))

	// EIP-7706: compute calldata gas cost separately.
	var calldataGasCost *big.Int
	if config != nil && config.IsGlamsterdan(header.Time) && header.CalldataExcessGas != nil {
		calldataBaseFee := gas.CalcCalldataBaseFeeFromHeader(header)
		calldataGas := types.CalldataTokenGas(msg.Data)
		calldataGasCost = gas.CalldataGasCost(calldataGas, calldataBaseFee)
	} else {
		calldataGasCost = new(big.Int)
	}

	// Balance check: use GasFeeCap (max possible cost) for EIP-1559 txs,
	// effectiveGasPrice for legacy txs. This matches go-ethereum's buyGas.
	balanceGasCost := gasCost
	if isEIP1559Tx && msg.GasFeeCap != nil {
		balanceGasCost = new(big.Int).Mul(msg.GasFeeCap, new(big.Int).SetUint64(msg.GasLimit))
	}
	totalCost := new(big.Int).Add(msg.Value, balanceGasCost)
	totalCost.Add(totalCost, calldataGasCost)
	// EIP-8141: For FrameTx, use FrameSender for balance check and deduction.
	payerAddr := msg.From
	if msg.TxType == types.FrameTxType && msg.FrameSender != (types.Address{}) {
		payerAddr = msg.FrameSender
	}
	balance := statedb.GetBalance(payerAddr)
	if balance.Cmp(totalCost) < 0 {
		gp.AddGas(msg.GasLimit)
		return nil, fmt.Errorf("%w: address %v have %v want %v", ErrInsufficientBalance, payerAddr, balance, totalCost)
	}

	// Deduct gas cost + calldata gas cost from sender
	deduction := new(big.Int).Add(gasCost, calldataGasCost)
	statedb.SubBalance(payerAddr, deduction)

	isCreate := msg.To == nil

	// Increment nonce (for contract creation, EVM.Create handles it).
	// EIP-8141: FrameTx nonce is incremented after frame execution, not before.
	// EIP-7701: AA transactions manage their own nonce through the SCA's validation
	// and execution phases. The protocol does not increment nonce for AA transactions.
	// EIP-7702: SetCodeTx nonce is incremented inside ProcessAuthorizations, after
	// the authorization nonce validation. We must NOT increment it here or the
	// authorization nonce check will fail.
	if !isCreate && msg.TxType != types.FrameTxType && msg.TxType != types.AATxType && msg.TxType != types.SetCodeTxType {
		statedb.SetNonce(msg.From, msg.Nonce+1)
	}

	// EIP-7702: count authorizations for intrinsic gas calculation.
	// Per the spec, we charge PER_EMPTY_ACCOUNT_COST (25000) for each authorization
	// upfront, then refund PER_EMPTY_ACCOUNT_COST - PER_AUTH_BASE_COST (12500)
	// for each authorization that targets an existing account.
	var authCount uint64
	var existingAuths []types.Address // track existing authorities for refund
	if msg.TxType == types.SetCodeTxType && len(msg.AuthList) > 0 {
		authCount = uint64(len(msg.AuthList))
		for i := range msg.AuthList {
			authority, err := eips.RecoverAuthority(&msg.AuthList[i])
			if err != nil {
				continue
			}
			// Track if authority already exists (for refund during authorization processing)
			if statedb.Exist(authority) && !statedb.Empty(authority) {
				existingAuths = append(existingAuths, authority)
			}
		}
	}

	// Compute intrinsic gas (includes access list costs per EIP-2930
	// and EIP-7702 authorization costs).
	isGlamsterdan := config != nil && config.IsGlamsterdan(header.Time)
	var igas uint64
	if isGlamsterdan {
		// EIP-2780: reduced intrinsic gas (4500 base).
		hasValue := msg.Value != nil && msg.Value.Sign() > 0
		toExists := msg.To != nil && statedb.Exist(*msg.To)
		igas = intrinsicGasGlamst(msg.Data, isCreate, hasValue, toExists, authCount)
		// EIP-7981/8038: Glamsterdam access list gas.
		igas += accessListGasGlamst(msg.AccessList)
	} else {
		isShanghaiForIgas := config != nil && config.IsMerge() && config.IsShanghai(header.Time)
		igas = intrinsicGas(msg.Data, isCreate, isShanghaiForIgas, authCount)
		igas += accessListGas(msg.AccessList)
	}

	// EIP-7623/7976: the gas limit must also cover the calldata floor (Prague+).
	// This prevents post-execution floor adjustment from exceeding gas limit.
	if config != nil && config.IsPrague(header.Time) {
		var floor uint64
		if isGlamsterdan {
			// EIP-7976: increased calldata floor + EIP-7981: access list floor
			floor = calldataFloorGasGlamst(msg.Data, msg.AccessList, isCreate)
		} else {
			floor = calldataFloorGas(msg.Data, isCreate)
		}
		if floor > igas {
			igas = floor
		}
	}

	if igas > msg.GasLimit {
		// Intrinsic gas exceeds gas limit — return as error (matching go-ethereum).
		gp.AddGas(msg.GasLimit)
		return nil, fmt.Errorf("intrinsic gas too low: have %d, want %d", msg.GasLimit, igas)
	}

	gasLeft := msg.GasLimit - igas

	// Create EVM
	blockCtx := vm.BlockContext{
		GetHash:     getHash,
		BlockNumber: header.Number,
		Time:        header.Time,
		Coinbase:    header.Coinbase,
		GasLimit:    header.GasLimit,
		BaseFee:     header.BaseFee,
		PrevRandao:  header.MixDigest,
	}
	txCtx := vm.TxContext{
		Origin:     msg.From,
		GasPrice:   gasPrice,
		BlobHashes: msg.BlobHashes,
	}
	evm := vm.NewEVMWithState(blockCtx, txCtx, vm.Config{}, statedb)

	// EIP-7928: inject BAL tracker so opcodes record state accesses.
	if len(balTracker) > 0 && balTracker[0] != nil {
		evm.SetBALTracker(balTracker[0], 0)
	}

	// GAP-2.1: attach per-dimension gas tracker under Glamsterdam so that
	// SSTORE state-creation premiums are routed to DimStorage.
	var dimUsage *vm.TxDimGasUsage
	if isGlamsterdan {
		dimUsage = &vm.TxDimGasUsage{}
		evm.SetDimGasUsage(dimUsage)
	}

	// GAP-1.2/1.3: seed the state-creation gas reservoir from execution gas.
	// Under Glamsterdam, a fraction of gasLeft is reserved for SSTORE/CREATE
	// state-creation ops; the reservoir is forwarded to sub-calls intact.
	if isGlamsterdan {
		_, reservoir := vm.InitReservoir(gasLeft, vm.DefaultReservoirConfig())
		evm.SetInitialReservoir(reservoir)
	}

	// Select the correct jump table based on fork rules.
	var precompileAddrs map[types.Address]vm.PrecompiledContract
	if config != nil {
		rules := config.Rules(header.Number, config.IsMerge(), header.Time)
		forkRules := vm.ForkRules{
			IsGlamsterdan:    rules.IsGlamsterdan,
			IsPrague:         rules.IsPrague,
			IsCancun:         rules.IsCancun,
			IsShanghai:       rules.IsShanghai,
			IsMerge:          rules.IsMerge,
			IsLondon:         rules.IsLondon,
			IsBerlin:         rules.IsBerlin,
			IsIstanbul:       rules.IsIstanbul,
			IsConstantinople: rules.IsConstantinople,
			IsByzantium:      rules.IsByzantium,
			IsHomestead:      rules.IsHomestead,
			IsEIP158:         rules.IsEIP158,
			IsEIP7708:        rules.IsEIP7708,
			IsEIP7954:        rules.IsEIP7954,
			IsIPlus:          rules.IsIPlus,
		}
		evm.SetJumpTable(vm.SelectJumpTable(forkRules))
		precompileAddrs = vm.SelectPrecompiles(forkRules)
		evm.SetPrecompiles(precompileAddrs)
		evm.SetForkRules(forkRules)
	}

	// Pre-warm EIP-2930 access list: mark sender, destination, coinbase, and precompiles as warm.
	statedb.AddAddressToAccessList(msg.From)
	if msg.To != nil {
		statedb.AddAddressToAccessList(*msg.To)
	}
	statedb.AddAddressToAccessList(header.Coinbase)
	// Warm all active precompile addresses per EIP-2929.
	for addr := range precompileAddrs {
		statedb.AddAddressToAccessList(addr)
	}
	for _, tuple := range msg.AccessList {
		statedb.AddAddressToAccessList(tuple.Address)
		for _, key := range tuple.StorageKeys {
			statedb.AddSlotToAccessList(tuple.Address, key)
		}
	}

	// EIP-7702: process authorization list for SetCode (type 0x04) transactions.
	// Per the spec, authorizations are processed before main EVM execution.
	// The authorization list sets delegation code on signer accounts.
	if msg.TxType == types.SetCodeTxType && len(msg.AuthList) > 0 {
		var chainID *big.Int
		if config != nil && config.ChainID != nil {
			chainID = config.ChainID
		}
		if err := eips.ProcessAuthorizations(statedb, msg.AuthList, chainID); err != nil {
			return nil, fmt.Errorf("processing EIP-7702 authorizations: %w", err)
		}
	}

	var (
		execErr      error
		returnData   []byte
		gasRemaining uint64
		contractAddr types.Address
	)

	// EIP-8141: frame transaction execution context (set when executing a FrameTx).
	var frameCtx *eips.FrameExecutionContext

	if msg.TxType == types.FrameTxType && len(msg.Frames) > 0 {
		// EIP-8141: execute as frame transaction.
		frameTx := &types.FrameTx{
			Nonce:  msg.Nonce,
			Sender: msg.FrameSender,
			Frames: msg.Frames,
		}
		if msg.GasFeeCap != nil {
			frameTx.MaxFeePerGas = new(big.Int).Set(msg.GasFeeCap)
		}
		if msg.GasTipCap != nil {
			frameTx.MaxPriorityFeePerGas = new(big.Int).Set(msg.GasTipCap)
		}

		// Set up the EVM FrameContext for TXPARAM* and APPROVE opcodes.
		evm.FrameCtx = &vm.FrameContext{
			Sender:         msg.FrameSender,
			Nonce:          new(big.Int).SetUint64(msg.Nonce),
			TxType:         uint64(types.FrameTxType),
			MaxPriorityFee: msg.GasTipCap,
			MaxFee:         msg.GasFeeCap,
			MaxCost:        eips.MaxFrameTxCost(frameTx),
			BlobCount:      uint64(len(msg.BlobHashes)),
			SigHash:        types.ComputeFrameSigHash(frameTx),
		}
		// Populate vm.Frame entries for TXPARAM introspection.
		vmFrames := make([]vm.Frame, len(msg.Frames))
		for i, f := range msg.Frames {
			var target types.Address
			if f.Target != nil {
				target = *f.Target
			} else {
				target = msg.FrameSender
			}
			vmFrames[i] = vm.Frame{
				Mode:     uint64(f.Mode),
				Target:   target,
				GasLimit: f.GasLimit,
				Data:     f.Data,
			}
		}
		evm.FrameCtx.Frames = vmFrames

		stateNonceForFrame := statedb.GetNonce(msg.FrameSender)

		// EIP-8141: validate nonce BEFORE executing frames.
		// Per spec: "Ensure tx.nonce == state[tx.sender].nonce"
		// This is a transaction-level validation that should reject invalid transactions.
		if msg.Nonce != stateNonceForFrame {
			return nil, fmt.Errorf("frame tx: nonce mismatch: got %d, want %d", msg.Nonce, stateNonceForFrame)
		}

		callFn := func(caller, target types.Address, frameGasLimit uint64, data []byte, mode uint8, frameIndex int) (uint64, uint64, []*types.Log, bool, uint8, error) {
			execLog.Debug("FrameTx callFn", "frameIndex", frameIndex, "caller", caller, "target", target, "mode", mode, "gasLimit", frameGasLimit)
			// EIP-8141: clear transient storage between frames for isolation.
			if frameIndex > 0 {
				statedb.ClearTransientStorage()
			}

			// Update the current frame index in the EVM context.
			evm.FrameCtx.CurrentFrameIndex = uint64(frameIndex)

			// EIP-8141: ORIGIN returns frame caller throughout all call depths.
			// Set the current frame caller for ORIGIN opcode.
			evm.FrameCtx.CurrentFrameCaller = caller

			// Reset per-frame APPROVE tracking before each frame call.
			if evm.FrameCtx != nil {
				evm.FrameCtx.ApproveCalledThisFrame = false
			}

			// Cap frame gas to remaining gas.
			availGas := gasLeft
			for j := 0; j < frameIndex; j++ {
				if j < len(msg.Frames) {
					availGas -= msg.Frames[j].GasLimit
				}
			}
			if frameGasLimit > availGas {
				frameGasLimit = availGas
			}

			// EIP-8141: VERIFY frames use StaticCall to prevent state modifications (GAP-2).
			var remainGas uint64
			var callErr error
			if mode == types.ModeVerify {
				_, remainGas, callErr = evm.StaticCall(caller, target, data, frameGasLimit)
			} else {
				_, remainGas, callErr = evm.Call(caller, target, data, frameGasLimit, new(big.Int))
			}
			status := uint64(types.ReceiptStatusSuccessful)
			if callErr != nil {
				status = uint64(types.ReceiptStatusFailed)
			}

			// Check if APPROVE was called during this frame using explicit tracking
			// fields set by opApprove. This correctly distinguishes APPROVE(2) from
			// separate APPROVE(0)+APPROVE(1) calls.
			approved := false
			var approveScope uint8
			if evm.FrameCtx != nil && evm.FrameCtx.ApproveCalledThisFrame {
				approved = true
				approveScope = evm.FrameCtx.LastApproveScope
			}
			execLog.Debug("FrameTx frame completed", "frameIndex", frameIndex, "approved", approved, "approveScope", approveScope, "status", status, "callErr", callErr)

			// Update frame status in vm context for TXPARAM(0x15) introspection.
			if frameIndex < len(evm.FrameCtx.Frames) {
				evm.FrameCtx.Frames[frameIndex].Status = status
			}

			gasUsed := frameGasLimit - remainGas
			// Collect logs emitted during this frame using the actual tx hash
			// so they match the key set by SetTxContext.
			logs := statedb.GetLogs(msg.TxHash)
			return status, gasUsed, logs, approved, approveScope, callErr
		}

		var frameErr error
		frameCtx, frameErr = eips.ExecuteFrameTx(frameTx, stateNonceForFrame, callFn)
		if frameErr != nil {
			execLog.Error("FrameTx execution failed", "error", frameErr, "sender", msg.FrameSender, "nonce", msg.Nonce)
			execErr = frameErr
			gasRemaining = 0
		} else {
			execLog.Debug("FrameTx execution succeeded", "sender", msg.FrameSender, "senderApproved", frameCtx.SenderApproved, "payerApproved", frameCtx.PayerApproved, "payer", frameCtx.Payer)
			// Calculate remaining gas after all frame execution.
			var totalFrameGasUsed uint64
			for _, fr := range frameCtx.FrameResults {
				totalFrameGasUsed += fr.GasUsed
			}
			if totalFrameGasUsed >= gasLeft {
				gasRemaining = 0
			} else {
				gasRemaining = gasLeft - totalFrameGasUsed
			}

			// EIP-8141: increment nonce after successful frame execution with sender approval.
			if frameCtx.SenderApproved {
				statedb.SetNonce(msg.FrameSender, msg.Nonce+1)
			}

			// EIP-8141: transfer gas cost from sender to payer for sponsored tx (GAP-1).
			// The sender was pre-charged above. If a different payer was
			// approved, refund sender and charge payer instead.
			if frameCtx.Payer != (types.Address{}) && frameCtx.Payer != msg.FrameSender {
				statedb.AddBalance(msg.FrameSender, deduction)
				statedb.SubBalance(frameCtx.Payer, deduction)
				// AA-1.3: if the payer's balance went negative, they failed to cover
				// gas — slash the paymaster.
				if msg.Slasher != nil {
					if b := statedb.GetBalance(frameCtx.Payer); b.Sign() < 0 {
						_ = msg.Slasher.SlashOnBadSettlement(frameCtx.Payer)
					}
				}
			}
		}
	} else if isCreate {
		// Contract creation: run EVM Create
		var ret []byte
		ret, contractAddr, gasRemaining, execErr = evm.Create(msg.From, msg.Data, gasLeft, msg.Value)
		returnData = ret
	} else {
		// Call (handles precompiles, contracts, and simple value transfers).
		// evm.Call performs value transfer, precompile dispatch, and code
		// execution internally, matching go-ethereum's behavior.
		returnData, gasRemaining, execErr = evm.Call(msg.From, *msg.To, msg.Data, gasLeft, msg.Value)
	}

	// Calculate gas used = intrinsic + (gasLeft - gasRemaining)
	gasUsed := igas + (gasLeft - gasRemaining)

	// EIP-7778: block gas accounting uses pre-refund gas.
	gasUsedBeforeRefund := gasUsed

	// Apply refund (EIP-3529: max refund = gasUsed / 5)
	// Under Glamsterdam, SSTORE no longer issues refunds (EIP-7778, handled
	// by opSstoreGlamst), but other refund sources still apply to user gas.
	rawRefund := statedb.GetRefund()
	refund := rawRefund
	maxRefund := gasUsed / 5
	if refund > maxRefund {
		refund = maxRefund
	}
	gasUsed -= refund

	// EIP-7623/7976: apply calldata floor gas (Prague+).
	// The floor cost ensures a minimum gas charge for transactions with
	// significant calldata, incentivizing blob usage over calldata.
	if config != nil && config.IsPrague(header.Time) {
		var floor uint64
		if isGlamsterdan {
			// EIP-7976/7981: Glamsterdam calldata/access-list floor.
			floor = calldataFloorGasGlamst(msg.Data, msg.AccessList, isCreate)
		} else {
			floor = calldataFloorGas(msg.Data, isCreate)
		}
		if floor > gasUsed {
			gasUsed = floor
		}
		// EIP-7778: block accounting also uses the floor if higher.
		if floor > gasUsedBeforeRefund {
			gasUsedBeforeRefund = floor
		}
	}

	// Refund remaining gas — to payer if a sponsored frame tx, otherwise to sender (GAP-1).
	remainingGas := msg.GasLimit - gasUsed
	if remainingGas > 0 {
		refundAmount := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(remainingGas))
		refundRecipient := msg.From
		if msg.TxType == types.FrameTxType && msg.FrameSender != (types.Address{}) {
			refundRecipient = msg.FrameSender
		}
		if frameCtx != nil && frameCtx.Payer != (types.Address{}) && frameCtx.Payer != refundRecipient {
			refundRecipient = frameCtx.Payer
		}
		statedb.AddBalance(refundRecipient, refundAmount)
	}

	// Return unused gas to the pool.
	// EIP-7778: under Glamsterdam, block gas pool uses pre-refund gas
	// to prevent block gas limit circumvention via refund exploitation.
	if isGlamsterdan {
		blockRemainingGas := msg.GasLimit - gasUsedBeforeRefund
		gp.AddGas(blockRemainingGas)
	} else {
		gp.AddGas(remainingGas)
	}

	// Pay tip to coinbase (EIP-1559: effective_tip * gasUsed goes to block producer).
	if header.BaseFee != nil && header.BaseFee.Sign() > 0 {
		tip := new(big.Int).Sub(gasPrice, header.BaseFee)
		if tip.Sign() > 0 {
			tipPayment := new(big.Int).Mul(tip, new(big.Int).SetUint64(gasUsed))
			statedb.AddBalance(header.Coinbase, tipPayment)
		}

		// EIP-7708: emit burn log for base fee portion (baseFee * gasUsed).
		if evm.GetForkRules().IsEIP7708 {
			burnAmount := new(big.Int).Mul(header.BaseFee, new(big.Int).SetUint64(gasUsed))
			vm.EmitBurnLog(statedb, msg.From, burnAmount)
		}
	} else {
		// Pre-EIP-1559: all gas payment goes to coinbase.
		coinbasePayment := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(gasUsed))
		statedb.AddBalance(header.Coinbase, coinbasePayment)
	}

	// GAP-2.2: capture DimStorage gas used by this transaction.
	var dimStorageGas uint64
	if dimUsage != nil {
		dimStorageGas = dimUsage.DimStorage
	}

	return &ExecutionResult{
		UsedGas:         gasUsed,
		BlockGasUsed:    gasUsedBeforeRefund,
		DimStorageGas:   dimStorageGas,
		Err:             execErr,
		ReturnData:      returnData,
		ContractAddress: contractAddr,
	}, nil
}

// msgEffectiveGasPrice computes the actual gas price paid per EIP-1559.
// For legacy txs, it returns GasPrice directly.
// For EIP-1559 txs, it returns min(GasFeeCap, BaseFee + GasTipCap).
func msgEffectiveGasPrice(msg *corconfig.Message, baseFee *big.Int) *big.Int {
	if msg.GasFeeCap != nil && baseFee != nil && baseFee.Sign() > 0 {
		// EIP-1559 transaction
		tip := msg.GasTipCap
		if tip == nil {
			tip = new(big.Int)
		}
		effectivePrice := new(big.Int).Add(baseFee, tip)
		if effectivePrice.Cmp(msg.GasFeeCap) > 0 {
			effectivePrice = new(big.Int).Set(msg.GasFeeCap)
		}
		return effectivePrice
	}
	// Legacy transaction
	if msg.GasPrice != nil {
		return new(big.Int).Set(msg.GasPrice)
	}
	return new(big.Int)
}

// calcBlobBaseFee computes the blob base fee from the excess blob gas.
// Per EIP-4844: blob_base_fee = MIN_BLOB_BASE_FEE * e^(excess_blob_gas / BLOB_BASE_FEE_UPDATE_FRACTION)
// We use the fake exponential approximation from the EIP.
func calcBlobBaseFee(excessBlobGas uint64) *big.Int {
	return fakeExponential(big.NewInt(1), new(big.Int).SetUint64(excessBlobGas), big.NewInt(3338477))
}

// fakeExponential approximates factor * e^(numerator / denominator) using Taylor expansion.
func fakeExponential(factor, numerator, denominator *big.Int) *big.Int {
	i := big.NewInt(1)
	output := new(big.Int)
	accum := new(big.Int).Mul(factor, denominator)
	for accum.Sign() > 0 {
		output.Add(output, accum)
		accum.Mul(accum, numerator)
		accum.Div(accum, new(big.Int).Mul(denominator, i))
		i.Add(i, big.NewInt(1))
	}
	return output.Div(output, denominator)
}

// --- Exported helpers for callers inside core/ ---

// CalldataFloorGas computes the EIP-7623 calldata floor gas (pre-Glamsterdam).
func CalldataFloorGas(data []byte, isCreate bool) uint64 {
	return calldataFloorGas(data, isCreate)
}

// CalldataFloorGasGlamst computes the EIP-7976 calldata floor gas (Glamsterdam+).
func CalldataFloorGasGlamst(data []byte, accessList types.AccessList, isCreate bool) uint64 {
	return calldataFloorGasGlamst(data, accessList, isCreate)
}

// CapturePreState captures balance and nonce values for a transaction's addresses.
func CapturePreState(statedb state.StateDB, tx *types.Transaction) (map[types.Address]*big.Int, map[types.Address]uint64) {
	return capturePreState(statedb, tx)
}

// BalTrackerOrNil converts a *bal.AccessTracker to vm.BALTracker safely.
func BalTrackerOrNil(t *bal.AccessTracker) vm.BALTracker {
	return balTrackerOrNil(t)
}

// PopulateTracker records balance and nonce changes into the BAL tracker.
func PopulateTracker(tracker *bal.AccessTracker, statedb state.StateDB, preBalances map[types.Address]*big.Int, preNonces map[types.Address]uint64) {
	populateTracker(tracker, statedb, preBalances, preNonces)
}

// CalcBlobBaseFee computes the blob base fee from the excess blob gas.
func CalcBlobBaseFee(excessBlobGas uint64) *big.Int {
	return calcBlobBaseFee(excessBlobGas)
}

// ApplyTransactionInternal is an exported version of applyTransaction for
// callers within the core/ package that need the internal signature.
func ApplyTransactionInternal(cfg *corconfig.ChainConfig, getHash vm.GetHashFunc, statedb state.StateDB, header *types.Header, tx *types.Transaction, gp *gaspool.GasPool) (*types.Receipt, uint64, error) {
	return applyTransaction(cfg, getHash, statedb, header, tx, gp)
}

// IntrinsicGasGlamst is an exported version of intrinsicGasGlamst for tests.
func IntrinsicGasGlamst(data []byte, isCreate bool, hasValue bool, toExists bool, authCount uint64) uint64 {
	return intrinsicGasGlamst(data, isCreate, hasValue, toExists, authCount)
}

// IntrinsicGas is an exported version of intrinsicGas for tests.
func IntrinsicGas(data []byte, isCreate, isShanghai bool, authCount uint64) uint64 {
	return intrinsicGas(data, isCreate, isShanghai, authCount)
}

// CalldataTokens is an exported version of calldataTokens for tests.
func CalldataTokens(data []byte) uint64 {
	return calldataTokens(data)
}

// AccessListDataTokens is an exported version of accessListDataTokens for tests.
func AccessListDataTokens(accessList types.AccessList) uint64 {
	return accessListDataTokens(accessList)
}

// AccessListGasGlamst is an exported version of accessListGasGlamst for tests.
func AccessListGasGlamst(accessList types.AccessList) uint64 {
	return accessListGasGlamst(accessList)
}

// ApplyMessage is an exported version of applyMessage for tests.
func ApplyMessage(cfg *corconfig.ChainConfig, getHash vm.GetHashFunc, statedb state.StateDB, header *types.Header, msg *corconfig.Message, gp *gaspool.GasPool, balTrackers ...vm.BALTracker) (*ExecutionResult, error) {
	return applyMessage(cfg, getHash, statedb, header, msg, gp, balTrackers...)
}

// ValidateBAL is an exported version of validateBAL for tests.
func ValidateBAL(header *types.Header, accessList *bal.BlockAccessList) error {
	return validateBAL(header, accessList)
}

// RequestCountSlot exposes the request count storage slot for tests.
var RequestCountSlot = requestCountSlot

// RequestDataSlotBase exposes the request data base storage slot for tests.
var RequestDataSlotBase = requestDataSlotBase

// IncrementSlot is an exported version of incrementSlot for tests.
func IncrementSlot(base types.Hash, offset uint64) types.Hash {
	return incrementSlot(base, offset)
}

// CountToUint64 is an exported version of countToUint64 for tests.
func CountToUint64(val types.Hash) uint64 {
	return countToUint64(val)
}

// TrimTrailingZeros is an exported version of trimTrailingZeros for tests.
func TrimTrailingZeros(b []byte) []byte {
	return trimTrailingZeros(b)
}

// buildAAUserOp constructs an eips.UserOperation from the on-chain AATx fields.
// Used for pre/post-execution EIP-7701 validation hooks.
func buildAAUserOp(aatx *types.AATx) *eips.UserOperation {
	return &eips.UserOperation{
		Sender:                        aatx.Sender,
		Nonce:                         new(big.Int).SetUint64(aatx.Nonce),
		CallData:                      aatx.SenderExecutionData,
		CallGasLimit:                  aatx.SenderExecutionGas,
		VerificationGasLimit:          aatx.SenderValidationGas,
		PaymasterVerificationGasLimit: aatx.PaymasterValidationGas,
		PaymasterPostOpGasLimit:       aatx.PaymasterPostOpGas,
		Paymaster:                     aatx.Paymaster,
		MaxFeePerGas:                  aatx.MaxFeePerGas,
		MaxPriorityFeePerGas:          aatx.MaxPriorityFeePerGas,
	}
}
