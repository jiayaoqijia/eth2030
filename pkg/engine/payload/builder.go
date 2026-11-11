package payload

import (
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"github.com/eth2030/eth2030/bal"
	"github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/chain"
	coreconfig "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	corevm "github.com/eth2030/eth2030/core/vm"
	engerrors "github.com/eth2030/eth2030/engine/errors"
	"github.com/eth2030/eth2030/proofs"
)

// ErrUnknownPayload is returned when a payload ID is not found.
var ErrUnknownPayload = engerrors.ErrUnknownPayload

// BuiltPayload holds the result of a payload build process.
type BuiltPayload struct {
	Block             *types.Block
	Receipts          []*types.Receipt
	BlockValue        *big.Int
	BlobsBundle       *BlobsBundleV1
	Override          bool
	ExecutionRequests [][]byte
	BAL               *bal.BlockAccessList
}

// PayloadBuilder manages async payload construction.
type PayloadBuilder struct {
	mu       sync.RWMutex
	config   *coreconfig.ChainConfig
	statedb  state.StateDB
	txPool   block.TxPoolReader
	payloads map[PayloadID]*BuiltPayload
	// prover is an optional STARK prover for VERIFY frame transactions (US-PQ-5b).
	prover proofs.ValidationFrameProver
}

// NewPayloadBuilder creates a new PayloadBuilder.
func NewPayloadBuilder(config *coreconfig.ChainConfig, statedb state.StateDB, txPool block.TxPoolReader) *PayloadBuilder {
	return &PayloadBuilder{
		config:   config,
		statedb:  statedb,
		txPool:   txPool,
		payloads: make(map[PayloadID]*BuiltPayload),
	}
}

// SetValidationFrameProver wires an optional STARK prover for VERIFY frame
// transactions. When set, StartBuild will call ReplaceValidationFrames after
// each block is built (US-PQ-5b).
func (pb *PayloadBuilder) SetValidationFrameProver(p proofs.ValidationFrameProver) {
	pb.mu.Lock()
	defer pb.mu.Unlock()
	pb.prover = p
}

// StartBuild begins building a payload with the given attributes.
func (pb *PayloadBuilder) StartBuild(
	id PayloadID,
	parentBlock *types.Block,
	attrs *PayloadAttributesV4,
) error {
	pb.mu.Lock()
	defer pb.mu.Unlock()

	builder := block.NewBlockBuilder(pb.config, nil, pb.txPool)
	builder.SetState(pb.statedb.Dup())
	parentHeader := parentBlock.Header()

	var beaconRoot *types.Hash
	if (attrs.ParentBeaconBlockRoot != types.Hash{}) {
		br := attrs.ParentBeaconBlockRoot
		beaconRoot = &br
	}

	// Validate gas limit is within allowed range from parent.
	minGas, maxGas := chain.CalcGasLimitRange(parentHeader.GasLimit)
	if parentHeader.GasLimit < minGas || parentHeader.GasLimit > maxGas {
		return fmt.Errorf("payload gas limit %d out of range [%d, %d]", parentHeader.GasLimit, minGas, maxGas)
	}

	// Validate timestamp is not more than 15 seconds in the future.
	syntheticHeader := &types.Header{Time: attrs.Timestamp}
	if err := chain.VerifyTimestampWindow(syntheticHeader, uint64(time.Now().Unix()), 15); err != nil {
		return fmt.Errorf("payload timestamp: %w", err)
	}

	blk, result, err := builder.BuildBlockWithResult(parentHeader, &block.BuildBlockAttributes{
		Timestamp:    attrs.Timestamp,
		FeeRecipient: attrs.SuggestedFeeRecipient,
		Random:       attrs.PrevRandao,
		GasLimit:     parentHeader.GasLimit,
		Withdrawals:  WithdrawalsToCore(attrs.Withdrawals),
		BeaconRoot:   beaconRoot,
	})
	if err != nil {
		return err
	}

	// EP-3 US-PQ-5b: replace VERIFY frame calldata with STARK proof when enabled.
	if pb.prover != nil {
		sealed, _, err := corevm.ReplaceValidationFrames(blk, pb.prover)
		if err != nil {
			slog.Warn("frame stark replacement failed", "err", err)
		} else {
			blk = sealed
		}
	}

	// Calculate block value as the sum of effective tips paid by transactions.
	blockValue := calcBlockValue(blk, result.Receipts, parentHeader.BaseFee)

	pb.payloads[id] = &BuiltPayload{
		Block:             blk,
		Receipts:          result.Receipts,
		BlockValue:        blockValue,
		BlobsBundle:       &BlobsBundleV1{},
		ExecutionRequests: result.Requests.Encode(),
		BAL:               result.BlockAccessList,
	}

	return nil
}

// GetPayload retrieves a completed payload by its ID.
func (pb *PayloadBuilder) GetPayload(id PayloadID) (*BuiltPayload, error) {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	built, ok := pb.payloads[id]
	if !ok {
		return nil, ErrUnknownPayload
	}
	return built, nil
}

// calcBlockValue computes the total tips (block value) from receipts.
// Block value = sum over txs of (effectiveGasPrice - baseFee) * gasUsed.
func calcBlockValue(block *types.Block, receipts []*types.Receipt, baseFee *big.Int) *big.Int {
	total := new(big.Int)
	if baseFee == nil {
		return total
	}

	txs := block.Transactions()
	for i, receipt := range receipts {
		if i >= len(txs) {
			break
		}
		tx := txs[i]

		// Compute effective gas price.
		effectivePrice := effectiveTipPerGas(tx, baseFee)
		if effectivePrice.Sign() <= 0 {
			continue
		}

		// tip = effectiveTip * gasUsed
		tip := new(big.Int).Mul(effectivePrice, new(big.Int).SetUint64(receipt.GasUsed))
		total.Add(total, tip)
	}
	return total
}

// effectiveTipPerGas computes (effectiveGasPrice - baseFee) for a transaction.
func effectiveTipPerGas(tx *types.Transaction, baseFee *big.Int) *big.Int {
	if tx.GasFeeCap() == nil || tx.GasTipCap() == nil {
		// Legacy transaction: tip = gasPrice - baseFee.
		gp := tx.GasPrice()
		if gp == nil {
			return new(big.Int)
		}
		return new(big.Int).Sub(gp, baseFee)
	}

	// EIP-1559: effectiveTip = min(gasTipCap, gasFeeCap - baseFee)
	maxTip := new(big.Int).Sub(tx.GasFeeCap(), baseFee)
	if maxTip.Cmp(tx.GasTipCap()) > 0 {
		return new(big.Int).Set(tx.GasTipCap())
	}
	return maxTip
}
