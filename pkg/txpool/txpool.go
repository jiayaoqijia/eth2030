package txpool

import (
	"errors"
	"fmt"
	"math/big"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/eips"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/log"
	"github.com/eth2030/eth2030/metrics"
	"github.com/eth2030/eth2030/txpool/frametx"
	"github.com/eth2030/eth2030/txpool/pricing"
)

var txpoolLog = log.Default().Module("eth/txpool")

// Type aliases re-exported from sub-packages for backward compatibility.
type (
	PaymasterApprover = frametx.PaymasterApprover
	FrameStateReader  = frametx.FrameStateReader
)

// Pool constants.
const (
	// PriceBump is the minimum gas price bump percentage for replace-by-fee.
	PriceBump = 10

	// MaxPoolSize is the maximum number of transactions the pool holds.
	MaxPoolSize = 4096

	// MaxPerSender is the maximum number of transactions per sender.
	MaxPerSender = 64

	// MaxTxSize is the maximum allowed encoded transaction size (128KB).
	MaxTxSize = 128 * 1024

	// MaxNonceGap is the maximum allowed gap between a transaction's nonce
	// and the sender's current state nonce. Transactions with nonces too far
	// ahead are rejected to prevent memory exhaustion from nonce-gap attacks.
	MaxNonceGap = 64

	// EIP-2930 access list gas costs (pre-Glamsterdam).
	AccessListAddressCost = 2400 // per address in access list
	AccessListStorageCost = 1900 // per storage key in access list

	// Glamsterdam (EIP-8038/7981) access list gas costs.
	AccessListAddressCostGlamst = 3200 // per address (EIP-8038)
	AccessListStorageCostGlamst = 2500 // per storage key (EIP-8038)
	// TotalCostFloorPerTokenGlamst is the data-token cost multiplier for
	// access list entries under EIP-7981 (must match execution/processor.go).
	TotalCostFloorPerTokenGlamst = 16

	// TxCreateGas is the extra gas for contract creation (added to TxBase).
	TxCreateGas = 32000

	// EIP-7702 SetCode authorization gas costs (must match execution/processor.go).
	PerAuthBaseCost     = 12500 // base cost per authorization entry
	PerEmptyAccountCost = 25000 // additional cost when auth target account is empty

	// priceBumpPercent is kept for internal use (same as PriceBump).
	priceBumpPercent = PriceBump
)

// Error codes for transaction validation.
var (
	ErrAlreadyKnown           = errors.New("already known")
	ErrNonceTooLow            = errors.New("nonce too low")
	ErrNonceTooHigh           = errors.New("nonce too high")
	ErrGasLimit               = errors.New("exceeds block gas limit")
	ErrInsufficientFunds      = errors.New("insufficient funds for gas * price + value")
	ErrIntrinsicGas           = errors.New("intrinsic gas too low")
	ErrTxPoolFull             = errors.New("transaction pool is full")
	ErrNegativeValue          = errors.New("negative value")
	ErrOversizedData          = errors.New("oversized data")
	ErrUnderpriced            = errors.New("transaction underpriced")
	ErrReplacementUnderpriced = errors.New("replacement transaction underpriced")
	ErrSenderLimitExceeded    = errors.New("per-sender transaction limit exceeded")
	ErrFeeCapBelowTip         = errors.New("max fee per gas less than max priority fee per gas")
	ErrFeeCapBelowBaseFee     = errors.New("max fee per gas less than block base fee")
	ErrBlobTxMissingHashes    = errors.New("blob transaction missing versioned hashes")
	ErrBlobFeeCapBelowBaseFee = errors.New("blob fee cap less than blob base fee")
	ErrNegativeGasPrice       = errors.New("negative gas price or fee cap")
	ErrOversizedRLP           = errors.New("encoded transaction too large")
	ErrUnstakedPaymaster      = errors.New("frame tx: VERIFY target is not a staked paymaster")
)

// Config holds TxPool configuration.
type Config struct {
	MaxSize           int               // Maximum number of transactions in pool
	MaxPerSender      int               // Maximum pending per sender
	MinGasPrice       *big.Int          // Minimum gas price to accept
	BlockGasLimit     uint64            // Current block gas limit
	PaymasterRegistry PaymasterApprover // optional: nil disables paymaster check (AA-1.2)
	PaymasterStrict   bool              // true = strict (default), false = off (AA-1.2)
	// AllowLocalTx enables acceptance of type-0x08 LocalTx (BB-2.2, --experimental-local-tx).
	// When false (default), LocalTx are rejected at pool entry.
	AllowLocalTx bool
	// AllowAATx enables acceptance of EIP-7701 type-0x05 AA transactions (--txpool.allow-aa).
	// Defaults to true; set false to disable AA tx acceptance on networks without Glamsterdam.
	AllowAATx bool
	// IsPrague indicates the chain is at or past the Prague fork.
	// When true, the EIP-7623 calldata floor is applied during validation.
	IsPrague bool
	// IsGlamsterdan indicates the chain is at or past the Glamsterdam fork (EIP-2780).
	// When true, the base tx gas cost is 4500 instead of 21000 for intrinsic validation.
	IsGlamsterdan bool
	// PriceHistoryBlocks is the number of recent blocks the gas price suggestor
	// uses to compute fee recommendations (--txpool.price-history, default 20).
	PriceHistoryBlocks int
}

// DefaultConfig returns sensible defaults for the pool.
func DefaultConfig() Config {
	return Config{
		MaxSize:            MaxPoolSize,
		MaxPerSender:       MaxPerSender,
		MinGasPrice:        big.NewInt(1), // 1 wei minimum
		BlockGasLimit:      30_000_000,
		PaymasterStrict:    true,
		AllowAATx:          true, // EIP-7701 AA is enabled by default (Glamsterdam+)
		PriceHistoryBlocks: pricing.DefaultFeeHistoryDepth,
	}
}

// StateReader provides account state for validation.
type StateReader interface {
	GetNonce(addr types.Address) uint64
	GetBalance(addr types.Address) *big.Int
}

// txLookup tracks transactions by hash for fast duplicate detection.
type txLookup struct {
	all map[types.Hash]*types.Transaction
}

func newTxLookup() *txLookup {
	return &txLookup{all: make(map[types.Hash]*types.Transaction)}
}

func (l *txLookup) Get(hash types.Hash) *types.Transaction {
	return l.all[hash]
}

func (l *txLookup) Add(tx *types.Transaction) {
	l.all[tx.Hash()] = tx
}

func (l *txLookup) Remove(hash types.Hash) {
	delete(l.all, hash)
}

func (l *txLookup) Count() int {
	return len(l.all)
}

// txSortedList maintains a sorted list of transactions by nonce for a single sender.
type txSortedList struct {
	items []*types.Transaction
}

func (l *txSortedList) Add(tx *types.Transaction) {
	// Insert maintaining nonce order.
	idx := sort.Search(len(l.items), func(i int) bool {
		return l.items[i].Nonce() >= tx.Nonce()
	})
	if idx < len(l.items) && l.items[idx].Nonce() == tx.Nonce() {
		// Replace existing tx with same nonce (if higher gas price).
		l.items[idx] = tx
		return
	}
	l.items = append(l.items, nil)
	copy(l.items[idx+1:], l.items[idx:])
	l.items[idx] = tx
}

func (l *txSortedList) Remove(nonce uint64) bool {
	for i, tx := range l.items {
		if tx.Nonce() == nonce {
			l.items = append(l.items[:i], l.items[i+1:]...)
			return true
		}
	}
	return false
}

func (l *txSortedList) Get(nonce uint64) *types.Transaction {
	idx := sort.Search(len(l.items), func(i int) bool {
		return l.items[i].Nonce() >= nonce
	})
	if idx < len(l.items) && l.items[idx].Nonce() == nonce {
		return l.items[idx]
	}
	return nil
}

func (l *txSortedList) Len() int {
	return len(l.items)
}

// Ready returns transactions that are ready to execute (sequential from baseNonce).
func (l *txSortedList) Ready(baseNonce uint64) []*types.Transaction {
	var ready []*types.Transaction
	expectedNonce := baseNonce
	for _, tx := range l.items {
		if tx.Nonce() != expectedNonce {
			break
		}
		ready = append(ready, tx)
		expectedNonce++
	}
	return ready
}

// TxPool implements a transaction pool for pending and queued transactions.
type TxPool struct {
	config      Config
	state       StateReader
	codeReader  FrameStateReader     // optional: enables VERIFY frame code check (AA-3.1)
	baseFee     *big.Int             // current base fee, nil if unknown
	blobBaseFee *big.Int             // current blob base fee (EIP-4844), nil if unknown
	suggestor   *pricing.PriceBumper // gas price suggestion engine

	mu      sync.RWMutex
	pending map[types.Address]*txSortedList // processable transactions
	queue   map[types.Address]*txSortedList // future transactions
	lookup  *txLookup                       // hash -> tx
}

// New creates a new transaction pool.
func New(config Config, state StateReader) *TxPool {
	bumperCfg := pricing.DefaultBumperConfig()
	if config.PriceHistoryBlocks > 0 {
		bumperCfg.HistoryDepth = config.PriceHistoryBlocks
	}
	if config.MinGasPrice != nil {
		bumperCfg.IgnorePrice = new(big.Int).Set(config.MinGasPrice)
	}
	return &TxPool{
		config:    config,
		state:     state,
		suggestor: pricing.NewPriceBumper(bumperCfg),
		pending:   make(map[types.Address]*txSortedList),
		queue:     make(map[types.Address]*txSortedList),
		lookup:    newTxLookup(),
	}
}

// RecordBlock feeds fee data from a newly processed block into the gas price
// suggestion engine. Call this once per block so SuggestGasPrice returns
// up-to-date recommendations.
func (pool *TxPool) RecordBlock(header *types.Header, txs []*types.Transaction) {
	pool.suggestor.RecordBlockFromHeader(header, txs)
}

// SuggestGasPrice returns a fee suggestion for the desired speed tier.
// Valid tiers: pricing.TierUrgent, TierFast, TierStandard, TierSlow.
// Falls back to TierStandard for unknown tier names.
func (pool *TxPool) SuggestGasPrice(tier string) pricing.FeeSuggestion {
	return pool.suggestor.SuggestFee(tier)
}

// SuggestAllTiers returns fee suggestions for all four speed tiers at once.
func (pool *TxPool) SuggestAllTiers() pricing.TieredSuggestion {
	return pool.suggestor.SuggestAllTiers()
}

// SetCodeReader wires a FrameStateReader into the pool, enabling lightweight
// VERIFY frame pre-flight validation (code-existence check) per AA-3.1.
// Must be called before the pool starts accepting frame transactions.
func (pool *TxPool) SetCodeReader(r FrameStateReader) {
	pool.mu.Lock()
	pool.codeReader = r
	pool.mu.Unlock()
}

// AddLocal adds a locally-submitted transaction to the pool.
func (pool *TxPool) AddLocal(tx *types.Transaction) error {
	return pool.add(tx)
}

// AddRemote adds a remotely-received transaction to the pool.
func (pool *TxPool) AddRemote(tx *types.Transaction) error {
	return pool.add(tx)
}

func (pool *TxPool) add(tx *types.Transaction) error {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	hash := tx.Hash()

	txpoolLog.Debug("tx_received",
		"event", "tx_received",
		"hash", hash.Hex(),
		"txType", tx.Type(),
		"nonce", tx.Nonce(),
	)

	// Check for duplicates.
	if pool.lookup.Get(hash) != nil {
		txpoolLog.Debug("tx_duplicate",
			"event", "tx_duplicate",
			"hash", hash.Hex(),
		)
		return ErrAlreadyKnown
	}

	// Validate the transaction.
	if err := pool.validateTx(tx); err != nil {
		txpoolLog.Debug("tx_rejected",
			"event", "tx_rejected",
			"hash", hash.Hex(),
			"txType", tx.Type(),
			"nonce", tx.Nonce(),
			"reason", err.Error(),
		)
		return err
	}

	// Determine sender (simplified - would normally recover from signature).
	from := pool.senderOf(tx)

	// Check nonce relative to state to decide pending vs queue.
	stateNonce := pool.state.GetNonce(from)

	if tx.Nonce() < stateNonce {
		txpoolLog.Debug("tx_rejected",
			"event", "tx_rejected",
			"hash", hash.Hex(),
			"reason", "nonce too low",
			"txNonce", tx.Nonce(),
			"stateNonce", stateNonce,
		)
		return ErrNonceTooLow
	}

	// Nonce gap detection: reject transactions with nonces too far ahead
	// of the current state nonce to prevent memory exhaustion attacks.
	if tx.Nonce() > stateNonce+MaxNonceGap {
		txpoolLog.Debug("tx_rejected",
			"event", "tx_rejected",
			"hash", hash.Hex(),
			"reason", "nonce too high (gap)",
			"txNonce", tx.Nonce(),
			"stateNonce", stateNonce,
			"maxGap", MaxNonceGap,
		)
		return ErrNonceTooHigh
	}

	// Check for replace-by-fee: existing tx from same sender with same nonce.
	replaced, err := pool.checkReplacement(from, tx)
	if err != nil {
		txpoolLog.Debug("tx_rejected",
			"event", "tx_rejected",
			"hash", hash.Hex(),
			"reason", err.Error(),
			"nonce", tx.Nonce(),
		)
		return err
	}
	if replaced {
		txpoolLog.Debug("tx_replaced",
			"event", "tx_replaced",
			"hash", hash.Hex(),
			"nonce", tx.Nonce(),
		)
	}

	// Per-sender limit: count all txs from this sender across pending + queue.
	// Replacements don't increase the count, so skip the check if replacing.
	if !replaced {
		senderCount := pool.senderTxCount(from)
		if senderCount >= pool.config.MaxPerSender {
			txpoolLog.Debug("tx_rejected",
				"event", "tx_rejected",
				"hash", hash.Hex(),
				"reason", "per-sender limit",
				"senderCount", senderCount,
				"limit", pool.config.MaxPerSender,
			)
			return ErrSenderLimitExceeded
		}
	}

	// If pool is full and this isn't a replacement, try eviction.
	if !replaced && pool.lookup.Count() >= pool.config.MaxSize {
		evicted := pool.evictLowest(pool.baseFee)
		if evicted == 0 {
			txpoolLog.Debug("tx_rejected",
				"event", "tx_rejected",
				"hash", hash.Hex(),
				"reason", "pool full",
				"poolSize", pool.lookup.Count(),
			)
			return ErrTxPoolFull
		}
	}

	// Add to lookup.
	pool.lookup.Add(tx)

	if tx.Nonce() == stateNonce {
		// This tx is immediately processable.
		pool.addPending(from, tx)
		txpoolLog.Debug("tx_pending",
			"event", "tx_pending",
			"hash", hash.Hex(),
			"nonce", tx.Nonce(),
			"poolSize", pool.lookup.Count(),
		)
	} else {
		// Future tx, add to queue.
		pool.addQueue(from, tx)
		txpoolLog.Debug("tx_queued",
			"event", "tx_queued",
			"hash", hash.Hex(),
			"nonce", tx.Nonce(),
			"stateNonce", stateNonce,
		)
	}

	// Promote queued txs that are now processable.
	pool.promoteQueue(from)

	metrics.TxPoolAdded.Inc()
	metrics.TxPoolPending.Set(int64(len(pool.pending)))
	metrics.TxPoolQueued.Set(int64(len(pool.queue)))
	return nil
}

// senderTxCount returns the total number of transactions from a sender
// across both pending and queued lists.
func (pool *TxPool) senderTxCount(from types.Address) int {
	count := 0
	if list, ok := pool.pending[from]; ok {
		count += list.Len()
	}
	if list, ok := pool.queue[from]; ok {
		count += list.Len()
	}
	return count
}

// checkReplacement handles replace-by-fee logic. If an existing tx with the same
// nonce from the same sender exists, the new tx must have >= 10% higher gas price.
// Returns (true, nil) if replaced, (false, nil) if no existing tx, or
// (false, ErrReplacementUnderpriced) if the bump is insufficient.
func (pool *TxPool) checkReplacement(from types.Address, tx *types.Transaction) (bool, error) {
	// Check pending list first.
	if list, ok := pool.pending[from]; ok {
		if old := list.Get(tx.Nonce()); old != nil {
			if !pool.hasSufficientBump(old, tx) {
				return false, ErrReplacementUnderpriced
			}
			pool.lookup.Remove(old.Hash())
			return true, nil
		}
	}
	// Check queue.
	if list, ok := pool.queue[from]; ok {
		if old := list.Get(tx.Nonce()); old != nil {
			if !pool.hasSufficientBump(old, tx) {
				return false, ErrReplacementUnderpriced
			}
			pool.lookup.Remove(old.Hash())
			return true, nil
		}
	}
	return false, nil
}

// hasSufficientBump checks if newTx has >= priceBumpPercent higher
// effective gas price than oldTx. For EIP-1559 style transactions, both the
// fee cap and tip cap must individually meet the bump threshold.
func (pool *TxPool) hasSufficientBump(oldTx, newTx *types.Transaction) bool {
	oldPrice := EffectiveGasPrice(oldTx, pool.baseFee)
	newPrice := EffectiveGasPrice(newTx, pool.baseFee)

	// New price must be >= old price * (100 + priceBumpPercent) / 100.
	threshold := new(big.Int).Mul(oldPrice, big.NewInt(100+priceBumpPercent))
	threshold.Div(threshold, big.NewInt(100))
	if newPrice.Cmp(threshold) < 0 {
		return false
	}

	// For EIP-1559 style transactions, also require the tip cap to meet the bump.
	// This prevents gaming where a tx with a high fee cap but low tip replaces
	// a tx with a lower fee cap but higher tip.
	if isDynamic(oldTx) && isDynamic(newTx) {
		oldTip := oldTx.GasTipCap()
		newTip := newTx.GasTipCap()
		if oldTip != nil && newTip != nil {
			tipThreshold := new(big.Int).Mul(oldTip, big.NewInt(100+priceBumpPercent))
			tipThreshold.Div(tipThreshold, big.NewInt(100))
			if newTip.Cmp(tipThreshold) < 0 {
				return false
			}
		}
	}

	return true
}

// isDynamic returns true if the transaction is an EIP-1559 style transaction
// (DynamicFeeTx, BlobTx, or SetCodeTx).
func isDynamic(tx *types.Transaction) bool {
	return tx.Type() == types.DynamicFeeTxType ||
		tx.Type() == types.BlobTxType ||
		tx.Type() == types.SetCodeTxType
}

// validateTx performs comprehensive validation of a transaction.
func (pool *TxPool) validateTx(tx *types.Transaction) error {
	// BB-2.2: gate type-0x08 LocalTx behind --experimental-local-tx flag.
	if tx.Type() == types.LocalTxType && !pool.config.AllowLocalTx {
		return errors.New("local tx (type 0x08) not enabled; set --experimental-local-tx")
	}

	// Reject negative values.
	if tx.Value() != nil && tx.Value().Sign() < 0 {
		return ErrNegativeValue
	}

	// Gas price / fee cap must be non-negative.
	if gp := tx.GasPrice(); gp != nil && gp.Sign() < 0 {
		return ErrNegativeGasPrice
	}
	if fc := tx.GasFeeCap(); fc != nil && fc.Sign() < 0 {
		return ErrNegativeGasPrice
	}

	// Gas limit check.
	if tx.Gas() > pool.config.BlockGasLimit {
		return ErrGasLimit
	}

	// RLP size limit enforcement: reject transactions exceeding 128KB encoded size.
	if rlpBytes, err := tx.EncodeRLP(); err == nil {
		if len(rlpBytes) > MaxTxSize {
			return ErrOversizedRLP
		}
	}

	// Data size check (max 128KB).
	if len(tx.Data()) > MaxTxSize {
		return ErrOversizedData
	}

	// EIP-8141: FrameTx validation — check frame structure and minimum gas.
	if tx.Type() == types.FrameTxType {
		if tx.Gas() < types.FrameTxIntrinsicCost {
			return ErrIntrinsicGas
		}
		// Validate frame structure (count, modes, targets, blob consistency).
		frames := tx.Frames()
		if len(frames) == 0 {
			return errors.New("frame tx: must have at least one frame")
		}
		if len(frames) > types.MaxFrames {
			return errors.New("frame tx: too many frames")
		}
		for i, f := range frames {
			if f.Mode > types.ModeSender {
				return fmt.Errorf("frame tx: invalid mode %d in frame %d", f.Mode, i)
			}
		}
		// EIP-8141: VERIFY frame structural validation (PARTIAL-5).
		// Codeless VERIFY target rejection requires StateDB.GetCodeSize, which
		// is not available via the txpool's StateReader. Full VERIFY simulation
		// (checking APPROVE is called) is deferred to block processing in
		// processor.go where the EVM is available. Here we validate structure only.
		hasVerify := false
		for _, f := range frames {
			if f.Mode == types.ModeVerify {
				hasVerify = true
				break
			}
		}
		// A FrameTx with SENDER frames but no VERIFY will fail at execution time
		// (ErrFrameSenderNotApproved), but we can reject it early.
		hasSender := false
		for _, f := range frames {
			if f.Mode == types.ModeSender {
				hasSender = true
				break
			}
		}
		if hasSender && !hasVerify {
			return errors.New("frame tx: SENDER frames require at least one VERIFY frame for approval")
		}

		// AA-1.2: Paymaster registry check — reject frame txs whose VERIFY
		// target is an external address not in the staked paymaster registry.
		// Only active when PaymasterRegistry is non-nil and PaymasterStrict is set.
		if pool.config.PaymasterStrict && pool.config.PaymasterRegistry != nil {
			sender := tx.FrameSender()
			for _, f := range frames {
				if f.Mode != types.ModeVerify || f.Target == nil {
					continue
				}
				// External VERIFY target (different from sender) acts as paymaster.
				if *f.Target != sender {
					if !pool.config.PaymasterRegistry.IsApprovedPaymaster(*f.Target) {
						return ErrUnstakedPaymaster
					}
				}
			}
		}

		// AA-3.1: VERIFY frame code check — reject frame txs whose VERIFY
		// target is an EOA (has no deployed code). Requires codeReader to be set.
		if pool.codeReader != nil {
			ftx := &types.FrameTx{
				Sender: tx.FrameSender(),
				Frames: frames,
			}
			if err := frametx.SimulateVerifyFrame(ftx, pool.codeReader); err != nil {
				return err
			}
		}
	}

	// EIP-7701: AA transaction — reject if AA is not enabled on this network.
	if tx.Type() == types.AATxType {
		if !pool.config.AllowAATx {
			return errors.New("AA transactions not enabled; set --txpool.allow-aa")
		}
		if aatx, ok := tx.Inner().(*types.AATx); ok {
			uo := &eips.UserOperation{
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
			if err := eips.ValidateUserOp(uo); err != nil {
				return fmt.Errorf("aa: invalid user op: %w", err)
			}
			// Compute canonical UserOp hash for indexing and debug logging (EIP-7701 §3.1).
			_ = eips.UserOpHash(uo, tx.ChainId())
			// Reject if worst-case gas cost exceeds sender's current balance.
			if pool.baseFee != nil {
				maxCost := eips.MaxUserOpGasCost(uo, pool.baseFee)
				senderBal := pool.state.GetBalance(aatx.Sender)
				if senderBal.Cmp(maxCost) < 0 {
					txpoolLog.Debug("aa_tx_balance_check_failed",
						"event", "aa_tx_balance_check_failed",
						"hash", tx.Hash().Hex(),
						"sender", aatx.Sender.Hex(),
						"paymaster", addressHex(aatx.Paymaster),
						"nonce", aatx.Nonce,
						"baseFee", bigIntString(pool.baseFee),
						"senderBalance", bigIntString(senderBal),
						"requiredBalance", bigIntString(maxCost),
						"callGasLimit", aatx.SenderExecutionGas,
						"verificationGasLimit", aatx.SenderValidationGas,
						"paymasterVerificationGasLimit", aatx.PaymasterValidationGas,
						"paymasterPostOpGasLimit", aatx.PaymasterPostOpGas,
						"maxFeePerGas", bigIntString(aatx.MaxFeePerGas),
						"maxPriorityFeePerGas", bigIntString(aatx.MaxPriorityFeePerGas),
					)
					return fmt.Errorf("aa: insufficient balance for gas: have %s need %s",
						senderBal, maxCost)
				}
			}
		}
	}

	// EIP-2930 access list gas accounting: include access list cost in intrinsic gas.
	// Use Glamsterdam costs when active to match the builder's intrinsic gas check.
	intrinsicGas := pool.intrinsicGas(tx)
	// EIP-7702: charge both base cost and empty-account cost per authorization.
	// The pool cannot recover each authority from state, so it conservatively
	// charges PerEmptyAccountCost for every entry. This matches the worst case
	// in the builder (all authorities absent from state) and prevents txs that
	// can never be mined from entering the pool.
	if tx.Type() == types.SetCodeTxType {
		authList := tx.AuthorizationList()
		intrinsicGas += uint64(len(authList)) * (PerAuthBaseCost + PerEmptyAccountCost)
	}
	if tx.Gas() < intrinsicGas {
		return ErrIntrinsicGas
	}

	// EIP-7623 / EIP-7976: enforce calldata floor gas (Prague+).
	// The block processor enforces this floor; txs that fail it are
	// silently skipped by the builder, wasting pool space. Reject them here.
	if pool.config.IsPrague && tx.Type() != types.AATxType {
		floor := CalldataFloorGas(tx.Data(), tx.To() == nil, pool.config.IsGlamsterdan, tx.AccessList())
		if tx.Gas() < floor {
			return ErrIntrinsicGas
		}
	}

	// Minimum gas price check.
	if pool.config.MinGasPrice != nil {
		effectivePrice := tx.GasPrice()
		if effectivePrice != nil && effectivePrice.Cmp(pool.config.MinGasPrice) < 0 {
			return ErrUnderpriced
		}
	}

	// EIP-1559 (type 2): maxFeePerGas must be >= maxPriorityFeePerGas.
	if tx.Type() == types.DynamicFeeTxType || tx.Type() == types.BlobTxType || tx.Type() == types.SetCodeTxType {
		feeCap := tx.GasFeeCap()
		tipCap := tx.GasTipCap()
		if feeCap != nil && tipCap != nil && feeCap.Cmp(tipCap) < 0 {
			return ErrFeeCapBelowTip
		}
	}

	// EIP-1559 base fee validation: reject txs with GasFeeCap below the current base fee.
	if pool.baseFee != nil {
		feeCap := tx.GasFeeCap()
		if feeCap == nil {
			feeCap = tx.GasPrice()
		}
		if feeCap != nil && feeCap.Cmp(pool.baseFee) < 0 {
			return ErrFeeCapBelowBaseFee
		}
	}

	// Blob transaction (type 3) validation.
	if tx.Type() == types.BlobTxType {
		if len(tx.BlobHashes()) == 0 {
			return ErrBlobTxMissingHashes
		}
		// EIP-4844: validate blob gas fee cap against the current blob base fee.
		if pool.blobBaseFee != nil {
			blobFeeCap := tx.BlobGasFeeCap()
			if blobFeeCap == nil || blobFeeCap.Cmp(pool.blobBaseFee) < 0 {
				return ErrBlobFeeCapBelowBaseFee
			}
		}
	}

	// Balance check: sender must have enough for value + gas * gasPrice.
	payer := pool.payerOf(tx)
	balance := pool.state.GetBalance(payer)
	if balance != nil {
		cost := pool.txCost(tx)
		// Debug log for FrameTx
		if tx.Type() == types.FrameTxType {
			txpoolLog.Debug("frame_tx_balance_check",
				"event", "frame_tx_balance_check",
				"hash", tx.Hash().Hex(),
				"payer", payer.Hex(),
				"payerBalance", bigIntString(balance),
				"requiredCost", bigIntString(cost),
				"gas", tx.Gas(),
				"gasPrice", bigIntString(tx.GasPrice()),
				"gasFeeCap", bigIntString(tx.GasFeeCap()),
				"gasTipCap", bigIntString(tx.GasTipCap()),
				"value", bigIntString(tx.Value()),
			)
		}
		if balance.Cmp(cost) < 0 {
			if aatx, ok := tx.Inner().(*types.AATx); ok {
				txpoolLog.Debug("aa_tx_payer_cost_check_failed",
					"event", "aa_tx_payer_cost_check_failed",
					"hash", tx.Hash().Hex(),
					"sender", aatx.Sender.Hex(),
					"payer", payer.Hex(),
					"nonce", aatx.Nonce,
					"payerBalance", bigIntString(balance),
					"requiredCost", bigIntString(cost),
					"gas", tx.Gas(),
					"gasPrice", bigIntString(tx.GasPrice()),
					"maxFeePerGas", bigIntString(aatx.MaxFeePerGas),
					"maxPriorityFeePerGas", bigIntString(aatx.MaxPriorityFeePerGas),
					"senderValidationGas", aatx.SenderValidationGas,
					"paymasterValidationGas", aatx.PaymasterValidationGas,
					"senderExecutionGas", aatx.SenderExecutionGas,
					"paymasterPostOpGas", aatx.PaymasterPostOpGas,
				)
			}
			return ErrInsufficientFunds
		}
	}

	return nil
}

func (pool *TxPool) intrinsicGas(tx *types.Transaction) uint64 {
	if tx.Type() == types.AATxType {
		gas := types.AABaseCost
		if aatx, ok := tx.Inner().(*types.AATx); ok {
			if aatx.Deployer != nil && *aatx.Deployer != aatx.Sender {
				gas += AccessListAddressCost
			}
			if aatx.Paymaster != nil && *aatx.Paymaster != aatx.Sender {
				gas += AccessListAddressCost
			}
		}
		if pool.config.IsGlamsterdan {
			gas += AccessListGasGlamst(tx.AccessList())
		} else {
			gas += AccessListGas(tx.AccessList())
		}
		return gas
	}

	gas := IntrinsicGas(tx.Data(), tx.To() == nil, pool.config.IsGlamsterdan)
	if pool.config.IsGlamsterdan {
		gas += AccessListGasGlamst(tx.AccessList())
	} else {
		gas += AccessListGas(tx.AccessList())
	}
	return gas
}

// txCost returns the maximum cost a transaction could incur:
// gas * gasPrice + value (+ blobGas * blobFeeCap for blob txs).
func (pool *TxPool) txCost(tx *types.Transaction) *big.Int {
	gasPrice := tx.GasPrice()
	if gasPrice == nil {
		gasPrice = new(big.Int)
	}
	cost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(tx.Gas()))
	if v := tx.Value(); v != nil {
		cost.Add(cost, v)
	}
	// For blob txs, add blob gas cost.
	if tx.Type() == types.BlobTxType {
		blobFeeCap := tx.BlobGasFeeCap()
		if blobFeeCap != nil {
			blobCost := new(big.Int).Mul(blobFeeCap, new(big.Int).SetUint64(tx.BlobGas()))
			cost.Add(cost, blobCost)
		}
	}
	return cost
}

func (pool *TxPool) payerOf(tx *types.Transaction) types.Address {
	if aatx, ok := tx.Inner().(*types.AATx); ok {
		if aatx.Paymaster != nil {
			return *aatx.Paymaster
		}
		return aatx.Sender
	}
	return pool.senderOf(tx)
}

func bigIntString(v *big.Int) string {
	if v == nil {
		return "0"
	}
	return v.String()
}

func addressHex(addr *types.Address) string {
	if addr == nil {
		return ""
	}
	return addr.Hex()
}

func (pool *TxPool) addPending(from types.Address, tx *types.Transaction) {
	list, ok := pool.pending[from]
	if !ok {
		list = &txSortedList{}
		pool.pending[from] = list
	}
	list.Add(tx)
}

func (pool *TxPool) addQueue(from types.Address, tx *types.Transaction) {
	list, ok := pool.queue[from]
	if !ok {
		list = &txSortedList{}
		pool.queue[from] = list
	}
	list.Add(tx)
}

// promoteQueue moves transactions from queue to pending when their nonce becomes
// sequential with the current pending nonce.
func (pool *TxPool) promoteQueue(from types.Address) {
	queueList, ok := pool.queue[from]
	if !ok || queueList.Len() == 0 {
		return
	}

	pendingList := pool.pending[from]
	var nextNonce uint64
	if pendingList != nil && pendingList.Len() > 0 {
		last := pendingList.items[pendingList.Len()-1]
		nextNonce = last.Nonce() + 1
	} else {
		nextNonce = pool.state.GetNonce(from)
	}

	// Move sequential txs from queue to pending.
	promoted := queueList.Ready(nextNonce)
	for _, tx := range promoted {
		pool.addPending(from, tx)
		queueList.Remove(tx.Nonce())
		txpoolLog.Debug("tx_promoted",
			"event", "tx_promoted",
			"hash", tx.Hash().Hex(),
			"nonce", tx.Nonce(),
		)
	}

	if queueList.Len() == 0 {
		delete(pool.queue, from)
	}
}

// Pending returns all processable transactions, grouped by sender and sorted by nonce.
func (pool *TxPool) Pending() map[types.Address][]*types.Transaction {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	result := make(map[types.Address][]*types.Transaction)
	for addr, list := range pool.pending {
		txs := make([]*types.Transaction, len(list.items))
		copy(txs, list.items)
		result[addr] = txs
	}
	return result
}

// senderGroup holds a sender's nonce-ordered pending txs and the effective
// gas price of the head (lowest-nonce) tx, used for cross-sender ordering.
type senderGroup struct {
	txs       []*types.Transaction
	headPrice *big.Int
}

// PendingFlat returns all pending transactions as a flat slice.
// Senders are ordered by the effective gas price of their lowest-nonce tx
// (descending), and each sender's txs are kept in ascending nonce order.
// This preserves per-sender nonce continuity so the block builder can
// include a full nonce sequence without gaps.
func (pool *TxPool) PendingFlat() []*types.Transaction {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	groups := make([]senderGroup, 0, len(pool.pending))
	for _, list := range pool.pending {
		if list.Len() == 0 {
			continue
		}
		txs := make([]*types.Transaction, len(list.items))
		copy(txs, list.items) // list.items is already nonce-sorted
		groups = append(groups, senderGroup{
			txs:       txs,
			headPrice: EffectiveGasPrice(txs[0], pool.baseFee),
		})
	}

	// Sort senders by head-tx effective gas price, highest first.
	sort.SliceStable(groups, func(i, j int) bool {
		pi := groups[i].headPrice
		pj := groups[j].headPrice
		if pi == nil {
			return false
		}
		if pj == nil {
			return true
		}
		return pi.Cmp(pj) > 0
	})

	var all []*types.Transaction
	for _, g := range groups {
		all = append(all, g.txs...)
	}
	return all
}

// Get retrieves a transaction by hash.
func (pool *TxPool) Get(hash types.Hash) *types.Transaction {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return pool.lookup.Get(hash)
}

// Remove removes a transaction from the pool (e.g., after inclusion in a block).
// After removal, queued transactions are promoted if their nonces become sequential.
func (pool *TxPool) Remove(hash types.Hash) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	tx := pool.lookup.Get(hash)
	if tx == nil {
		return
	}
	pool.lookup.Remove(hash)

	from := pool.senderOf(tx)
	wasPending := false

	if list, ok := pool.pending[from]; ok {
		if list.Remove(tx.Nonce()) {
			wasPending = true
		}
		if list.Len() == 0 {
			delete(pool.pending, from)
		}
	}
	if list, ok := pool.queue[from]; ok {
		list.Remove(tx.Nonce())
		if list.Len() == 0 {
			delete(pool.queue, from)
		}
	}

	// If a pending tx was removed, try to promote queued txs to fill the gap.
	if wasPending {
		pool.promoteQueue(from)
	}
}

// Count returns the total number of transactions in the pool.
func (pool *TxPool) Count() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	return pool.lookup.Count()
}

// PendingCount returns the number of pending (processable) transactions.
func (pool *TxPool) PendingCount() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	count := 0
	for _, list := range pool.pending {
		count += list.Len()
	}
	return count
}

// QueuedCount returns the number of queued (future) transactions.
func (pool *TxPool) QueuedCount() int {
	pool.mu.RLock()
	defer pool.mu.RUnlock()
	count := 0
	for _, list := range pool.queue {
		count += list.Len()
	}
	return count
}

// Reset removes all transactions with nonces below the current state nonces.
// Called after a new block is processed.
func (pool *TxPool) Reset(stateReader StateReader) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	pool.state = stateReader

	var totalConfirmed, totalDemoted int
	for addr, list := range pool.pending {
		stateNonce := pool.state.GetNonce(addr)
		var toRemove []uint64
		var toQueue []*types.Transaction
		for _, tx := range list.items {
			if tx.Nonce() < stateNonce {
				// Confirmed: evict from pool entirely.
				toRemove = append(toRemove, tx.Nonce())
				pool.lookup.Remove(tx.Hash())
			} else if tx.Nonce() > stateNonce {
				// Future nonce (happens after a reorg that lowered stateNonce):
				// demote back to queue so promoteQueue can re-order them correctly.
				toQueue = append(toQueue, tx)
			}
			// tx.Nonce() == stateNonce: correctly at the head of pending, keep it.
		}
		for _, n := range toRemove {
			list.Remove(n)
		}
		totalConfirmed += len(toRemove)
		for _, tx := range toQueue {
			list.Remove(tx.Nonce())
			pool.addQueue(addr, tx)
		}
		totalDemoted += len(toQueue)
		if list.Len() == 0 {
			delete(pool.pending, addr)
		}
	}

	txpoolLog.Debug("pool_reset",
		"event", "pool_reset",
		"confirmed", totalConfirmed,
		"reorgDemoted", totalDemoted,
		"poolSize", pool.lookup.Count(),
		"pending", len(pool.pending),
		"queued", len(pool.queue),
	)

	// Re-promote queued txs (including those just demoted from pending).
	for addr := range pool.queue {
		pool.promoteQueue(addr)
	}
}

// senderOf extracts the sender address from a transaction.
// If the sender has been cached via SetSender, returns that.
// Otherwise recovers the sender from the ECDSA signature via ecrecover.
func (pool *TxPool) senderOf(tx *types.Transaction) types.Address {
	if from := tx.Sender(); from != nil {
		return *from
	}
	if aatx, ok := tx.Inner().(*types.AATx); ok {
		tx.SetSender(aatx.Sender)
		return aatx.Sender
	}
	// EIP-8141: FrameTx has explicit Sender field (no signature).
	if ftx, ok := tx.Inner().(*types.FrameTx); ok {
		tx.SetSender(ftx.Sender)
		return ftx.Sender
	}
	// Recover sender from signature using ecrecover.
	sigHash := tx.SigningHash()
	v, r, s := tx.RawSignatureValues()
	if r == nil || s == nil {
		return types.Address{}
	}
	// Build 65-byte signature [R || S || V].
	sig := make([]byte, 65)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)
	// Compute recovery ID from V.
	if v != nil {
		vVal := v.Uint64()
		switch tx.Type() {
		case types.LegacyTxType:
			// EIP-155: V = chainID*2 + 35 + recovery_id
			// Pre-EIP-155: V = 27 + recovery_id
			if vVal >= 35 {
				chainID := tx.ChainId()
				if chainID != nil && chainID.Sign() > 0 {
					vVal -= chainID.Uint64()*2 + 35
				}
			} else if vVal >= 27 {
				vVal -= 27
			}
		default:
			// Typed transactions: V is 0 or 1 directly.
		}
		sig[64] = byte(vVal)
	}
	pub, err := crypto.SigToPub(sigHash[:], sig)
	if err != nil {
		return types.Address{}
	}
	addr := crypto.PubkeyToAddress(*pub)
	tx.SetSender(addr)
	return addr
}

// EffectiveGasPrice calculates the effective gas price for a transaction
// given a base fee. For legacy transactions, this is simply GasPrice.
// For EIP-1559 transactions: min(MaxFeePerGas, BaseFee + MaxPriorityFeePerGas).
// If baseFee is nil, returns GasFeeCap (MaxFeePerGas) as the effective price.
func EffectiveGasPrice(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if baseFee == nil || tx.Type() == types.LegacyTxType || tx.Type() == types.AccessListTxType {
		gp := tx.GasPrice()
		if gp == nil {
			return new(big.Int)
		}
		return new(big.Int).Set(gp)
	}
	// EIP-1559 style: effective = min(feeCap, baseFee + tipCap)
	feeCap := tx.GasFeeCap()
	tipCap := tx.GasTipCap()
	if feeCap == nil {
		return new(big.Int)
	}
	if tipCap == nil {
		tipCap = new(big.Int)
	}
	// baseFee + tipCap
	effectiveTip := new(big.Int).Add(baseFee, tipCap)
	// min(feeCap, baseFee + tipCap)
	if effectiveTip.Cmp(feeCap) > 0 {
		return new(big.Int).Set(feeCap)
	}
	return effectiveTip
}

// PendingSorted returns all pending transactions sorted by effective gas price
// (descending). Higher-priced transactions come first for block building.
func (pool *TxPool) PendingSorted() []*types.Transaction {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	baseFee := pool.baseFee

	var all []*types.Transaction
	for _, list := range pool.pending {
		all = append(all, list.items...)
	}

	sort.Slice(all, func(i, j int) bool {
		pi := EffectiveGasPrice(all[i], baseFee)
		pj := EffectiveGasPrice(all[j], baseFee)
		return pi.Cmp(pj) > 0
	})
	return all
}

// GetBlobsByVersionedHashes returns blob sidecar data for each requested versioned
// hash. For each hash the corresponding entry is non-nil only when a pending blob
// transaction carries that versioned hash and its sidecar is still attached.
// Returns a slice parallel to hashes (nil entry = not found).
func (pool *TxPool) GetBlobsByVersionedHashes(hashes []types.Hash) []*BlobAndProof {
	pool.mu.RLock()
	defer pool.mu.RUnlock()

	// Build an index: versioned_hash -> (commitment, proof, blob).
	type entry struct {
		blob       []byte
		commitment []byte
		proof      []byte
	}
	index := make(map[types.Hash]*entry)

	for _, list := range pool.pending {
		for _, tx := range list.items {
			if tx.Type() != types.BlobTxType {
				continue
			}
			sc := tx.BlobSidecar()
			if sc == nil {
				continue
			}
			for i, h := range tx.BlobHashes() {
				if i >= len(sc.Blobs) || i >= len(sc.Commitments) || i >= len(sc.Proofs) {
					break
				}
				index[h] = &entry{
					blob:       sc.Blobs[i],
					commitment: sc.Commitments[i],
					proof:      sc.Proofs[i],
				}
			}
		}
	}
	// Also check the queue.
	for _, list := range pool.queue {
		for _, tx := range list.items {
			if tx.Type() != types.BlobTxType {
				continue
			}
			sc := tx.BlobSidecar()
			if sc == nil {
				continue
			}
			for i, h := range tx.BlobHashes() {
				if i >= len(sc.Blobs) || i >= len(sc.Commitments) || i >= len(sc.Proofs) {
					break
				}
				if _, found := index[h]; !found {
					index[h] = &entry{
						blob:       sc.Blobs[i],
						commitment: sc.Commitments[i],
						proof:      sc.Proofs[i],
					}
				}
			}
		}
	}

	result := make([]*BlobAndProof, len(hashes))
	for i, h := range hashes {
		if e, ok := index[h]; ok {
			result[i] = &BlobAndProof{
				Blob:       e.blob,
				Commitment: e.commitment,
				Proof:      e.proof,
			}
		}
	}
	return result
}

// BlobAndProof is a single blob with its KZG commitment and proof.
type BlobAndProof struct {
	Blob       []byte
	Commitment []byte
	Proof      []byte
}

// evictLowest removes the transaction with the lowest effective gas price from
// the pool. It protects the highest-nonce pending tx for each sender (so every
// sender keeps at least one tx). Returns the number of evicted transactions.
func (pool *TxPool) evictLowest(baseFee *big.Int) int {
	// Collect all transactions with their effective prices, excluding
	// protected txs (highest-nonce pending tx per sender).
	type candidate struct {
		tx    *types.Transaction
		from  types.Address
		price *big.Int
		queue bool // whether the tx is in the queue (not pending)
	}

	var candidates []candidate

	// Gather pending txs. Only the highest-nonce (tail) tx per sender is
	// evictable: removing the tail leaves the remaining sequence intact with
	// no nonce gap, so no cascade is needed. Evicting a lower-nonce tx would
	// orphan all higher-nonce txs, stranding them in queue permanently.
	// Single-tx senders are protected (they are the sole pool anchor).
	for addr, list := range pool.pending {
		if list.Len() <= 1 {
			continue
		}
		last := list.items[list.Len()-1]
		candidates = append(candidates, candidate{
			tx:    last,
			from:  addr,
			price: EffectiveGasPrice(last, baseFee),
			queue: false,
		})
	}

	// All queued txs are eviction candidates.
	for addr, list := range pool.queue {
		for _, tx := range list.items {
			candidates = append(candidates, candidate{
				tx:    tx,
				from:  addr,
				price: EffectiveGasPrice(tx, baseFee),
				queue: true,
			})
		}
	}

	if len(candidates) == 0 {
		return 0
	}

	// Sort by price ascending so the cheapest is first.
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].price.Cmp(candidates[j].price) < 0
	})

	// Evict the cheapest.
	c := candidates[0]
	txpoolLog.Debug("tx_evicted",
		"event", "tx_evicted",
		"hash", c.tx.Hash().Hex(),
		"nonce", c.tx.Nonce(),
		"effectivePrice", c.price.String(),
		"wasQueued", c.queue,
	)
	pool.lookup.Remove(c.tx.Hash())
	if c.queue {
		if list, ok := pool.queue[c.from]; ok {
			list.Remove(c.tx.Nonce())
			if list.Len() == 0 {
				delete(pool.queue, c.from)
			}
		}
	} else {
		// Evicting the tail pending tx: just remove it. The remaining lower-nonce
		// txs form a valid contiguous sequence — no cascade needed.
		if list, ok := pool.pending[c.from]; ok {
			list.Remove(c.tx.Nonce())
			if list.Len() == 0 {
				delete(pool.pending, c.from)
			}
		}
	}
	metrics.TxPoolDropped.Inc()
	metrics.TxPoolPending.Set(int64(len(pool.pending)))
	metrics.TxPoolQueued.Set(int64(len(pool.queue)))
	return 1
}

// SetBaseFee updates the pool's base fee and demotes pending transactions
// that can no longer afford the new base fee to the queue.
func (pool *TxPool) SetBaseFee(baseFee *big.Int) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	txpoolLog.Debug("basefee_update",
		"event", "basefee_update",
		"newBaseFee", baseFee.String(),
	)

	pool.baseFee = new(big.Int).Set(baseFee)

	// Demote pending txs whose max fee is below the new base fee.
	var totalDemoted int
	for addr, list := range pool.pending {
		var demote []*types.Transaction
		for _, tx := range list.items {
			feeCap := tx.GasFeeCap()
			if feeCap == nil {
				feeCap = tx.GasPrice()
			}
			if feeCap != nil && feeCap.Cmp(baseFee) < 0 {
				demote = append(demote, tx)
			}
		}
		for _, tx := range demote {
			list.Remove(tx.Nonce())
			pool.addQueue(addr, tx)
			txpoolLog.Debug("tx_demoted",
				"event", "tx_demoted",
				"hash", tx.Hash().Hex(),
				"nonce", tx.Nonce(),
				"feeCap", tx.GasFeeCap().String(),
				"newBaseFee", baseFee.String(),
			)
		}
		totalDemoted += len(demote)
		if list.Len() == 0 {
			delete(pool.pending, addr)
		}
	}
	if totalDemoted > 0 {
		txpoolLog.Debug("basefee_demoted",
			"event", "basefee_demoted",
			"count", totalDemoted,
			"newBaseFee", baseFee.String(),
		)
	}
}

// IntrinsicGas computes the intrinsic gas for a transaction (excluding access list).
// On Glamsterdam (EIP-2780) the base tx cost dropped from 21000 to 4500; for
// contract creation the cost is base + TxCreateGas. The new-account surcharge is
// NOT included here because the pool cannot inspect destination account existence.
func IntrinsicGas(data []byte, isContractCreation bool, isGlamsterdan bool) uint64 {
	var gas uint64
	if isGlamsterdan {
		gas = 4500
	} else {
		gas = 21000
	}
	if isContractCreation {
		gas += TxCreateGas // 32000 — added to base, not overriding it
	}

	if len(data) > 0 {
		var nz uint64
		for _, b := range data {
			if b != 0 {
				nz++
			}
		}
		z := uint64(len(data)) - nz
		gas += nz * 16 // non-zero byte cost
		gas += z * 4   // zero byte cost
	}
	return gas
}

// AccessListGas computes the gas cost of an EIP-2930 access list (pre-Glamsterdam).
// Each address costs 2400 gas and each storage key costs 1900 gas.
func AccessListGas(al types.AccessList) uint64 {
	if len(al) == 0 {
		return 0
	}
	var gas uint64
	for _, tuple := range al {
		gas += AccessListAddressCost
		gas += uint64(len(tuple.StorageKeys)) * AccessListStorageCost
	}
	return gas
}

// AccessListGasGlamst computes the gas cost of an access list under Glamsterdam.
// Per EIP-8038: address cost 3200, storage key cost 2500.
// Per EIP-7981: also charges TotalCostFloorPerTokenGlamst per data token
// (zero_bytes*1 + nonzero_bytes*4) for each address (20 bytes) and storage key (32 bytes).
func AccessListGasGlamst(al types.AccessList) uint64 {
	if len(al) == 0 {
		return 0
	}
	var gas, tokens uint64
	for _, tuple := range al {
		gas += AccessListAddressCostGlamst
		gas += uint64(len(tuple.StorageKeys)) * AccessListStorageCostGlamst
		// EIP-7981: data token cost for address bytes (20 bytes).
		for _, b := range tuple.Address {
			if b == 0 {
				tokens++
			} else {
				tokens += 4
			}
		}
		// EIP-7981: data token cost for each storage key (32 bytes).
		for _, key := range tuple.StorageKeys {
			for _, b := range key {
				if b == 0 {
					tokens++
				} else {
					tokens += 4
				}
			}
		}
	}
	gas += tokens * TotalCostFloorPerTokenGlamst
	return gas
}

// CalldataFloorGas computes the EIP-7623/EIP-7976 calldata floor gas for a
// transaction. The block processor enforces this floor; txs that fail it are
// silently skipped, so we reject them at pool entry.
//
// Prague/EIP-7623: floor = TxGas(21000) + tokens*10 + (TxCreateGas if create)
//
//	tokens = zero_bytes*1 + nonzero_bytes*4
//
// Glamsterdam/EIP-7976: floor = TxBase(4500) + total_bytes*4*16 + accessListFloor + (TxCreateGas if create)
//
//	access list floor counts each address (20B) and key (32B) at token cost.
func CalldataFloorGas(data []byte, isCreate bool, isGlamsterdan bool, al types.AccessList) uint64 {
	const (
		pragueTxBase      = 21000 // pre-Glamsterdam base
		pragueFloorPerTok = 10    // EIP-7623
		glamstTxBase      = 4500  // Glamsterdam base
		glamstFloorPerTok = 16    // EIP-7976 / TotalCostFloorPerTokenGlamst
	)

	if isGlamsterdan {
		// EIP-7976: all bytes are worth 4 tokens each.
		tokens := uint64(len(data)) * 4
		// Access list data tokens (addr + keys, 4 tokens per byte regardless of value).
		for _, tuple := range al {
			tokens += 20 * 4 // address: 20 bytes × 4 tokens
			tokens += uint64(len(tuple.StorageKeys)) * 32 * 4
		}
		floor := glamstTxBase + tokens*glamstFloorPerTok
		if isCreate {
			floor += TxCreateGas
		}
		return floor
	}

	// EIP-7623 Prague: tokens = zero_bytes + nonzero_bytes*4.
	var tokens uint64
	for _, b := range data {
		if b == 0 {
			tokens++
		} else {
			tokens += 4
		}
	}
	floor := pragueTxBase + tokens*pragueFloorPerTok
	if isCreate {
		floor += TxCreateGas
	}
	return floor
}

// SetBlobBaseFee updates the pool's blob base fee (EIP-4844). Blob transactions
// with a blob fee cap below this value will be rejected during validation.
func (pool *TxPool) SetBlobBaseFee(blobBaseFee *big.Int) {
	pool.mu.Lock()
	defer pool.mu.Unlock()
	pool.blobBaseFee = new(big.Int).Set(blobBaseFee)
}
