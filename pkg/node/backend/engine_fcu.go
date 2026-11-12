package backend

import (
	"errors"
	"fmt"
	"github.com/eth2030/eth2030/core/chain"
	"log/slog"

	"github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
	"github.com/eth2030/eth2030/engine"
	"github.com/eth2030/eth2030/engine/payload"
)

// ForkchoiceUpdated processes a forkchoice state update from the consensus layer.
func (b *EngineBackend) ForkchoiceUpdated(
	fcState payload.ForkchoiceStateV1,
	attrs *payload.PayloadAttributesV3,
) (payload.ForkchoiceUpdatedResult, error) {
	return b.forkchoiceUpdated(fcState, attrs, nil, 0)
}

func (b *EngineBackend) forkchoiceUpdated(
	fcState payload.ForkchoiceStateV1,
	attrs *payload.PayloadAttributesV3,
	inclusionListTxs [][]byte,
	slot uint64,
) (payload.ForkchoiceUpdatedResult, error) {
	bc := b.node.Blockchain()

	slog.Debug("engine_forkchoiceUpdated",
		"headBlockHash", fcState.HeadBlockHash,
		"safeBlockHash", fcState.SafeBlockHash,
		"finalizedBlockHash", fcState.FinalizedBlockHash,
		"hasPayloadAttrs", attrs != nil,
		"genesisHash", bc.Genesis().Hash(),
	)

	if err := b.beginWork(); err != nil {
		return payload.ForkchoiceUpdatedResult{}, err
	}
	defer b.workers.Done()
	b.fcuMu.Lock()
	defer b.fcuMu.Unlock()
	headBlock := bc.GetBlock(fcState.HeadBlockHash)
	syncing := func() (payload.ForkchoiceUpdatedResult, error) {
		if syncer, ok := b.node.(interface {
			RequestExecutionSync(payload.ForkchoiceStateV1)
		}); ok {
			syncer.RequestExecutionSync(fcState)
		}
		return payload.ForkchoiceUpdatedResult{PayloadStatus: payload.PayloadStatusV1{Status: engine.StatusSyncing}}, nil
	}
	if headBlock == nil || !bc.HasValidatedBlock(fcState.HeadBlockHash) {
		return syncing()
	}
	var applyErr error
	if coordinator, ok := b.node.(interface {
		ApplyExecutionForkchoice(payload.ForkchoiceStateV1) error
	}); ok {
		applyErr = coordinator.ApplyExecutionForkchoice(fcState)
	} else {
		applyErr = bc.ApplyForkchoice(fcState.HeadBlockHash, fcState.SafeBlockHash, fcState.FinalizedBlockHash)
	}
	if errors.Is(applyErr, chain.ErrStateNotFound) {
		return syncing()
	}
	if errors.Is(applyErr, chain.ErrStorageFailure) {
		return payload.ForkchoiceUpdatedResult{}, applyErr
	}
	if applyErr != nil {
		return payload.ForkchoiceUpdatedResult{}, fmt.Errorf("%w: %v", engine.ErrInvalidForkchoiceState, applyErr)
	}
	b.fcMu.Lock()
	b.safeHash = fcState.SafeBlockHash
	b.finalizedHash = fcState.FinalizedBlockHash
	b.fcMu.Unlock()
	b.node.TxPool().SetForkRules(bc.Config(), bc.CurrentBlock().Time())
	b.node.TxPool().Reset(bc.State())
	b.node.TxPool().RecordBlock(headBlock.Header(), headBlock.Transactions())
	if oracle := b.node.GasOracle(); oracle != nil {
		oracle.RecordBlock(headBlock.NumberU64(), headBlock.BaseFee(), ExtractBlockTips(headBlock.Transactions(), headBlock.BaseFee()))
	}
	headHash := headBlock.Hash()
	payloadStatus := payload.PayloadStatusV1{Status: engine.StatusValid, LatestValidHash: &headHash}
	if attrs == nil {
		return payload.ForkchoiceUpdatedResult{PayloadStatus: payloadStatus}, nil
	}
	if attrs.Timestamp <= headBlock.Time() {
		return payload.ForkchoiceUpdatedResult{}, engine.ErrInvalidPayloadAttributes
	}

	// Step 7b: payload attributes provided — start async block build.
	slog.Debug("engine_forkchoiceUpdated: building payload",
		"parentNum", headBlock.NumberU64(),
		"parentHash", headBlock.Hash(),
		"timestamp", attrs.Timestamp,
		"feeRecipient", attrs.SuggestedFeeRecipient,
	)
	parentHeader := headBlock.Header()

	// Convert engine withdrawals to core types.
	var withdrawals []*types.Withdrawal
	for _, w := range attrs.Withdrawals {
		withdrawals = append(withdrawals, &types.Withdrawal{
			Index:          w.Index,
			ValidatorIndex: w.ValidatorIndex,
			Address:        w.Address,
			Amount:         w.Amount,
		})
	}

	beaconRoot := attrs.ParentBeaconBlockRoot
	buildAttrs := &block.BuildBlockAttributes{
		Timestamp:        attrs.Timestamp,
		FeeRecipient:     attrs.SuggestedFeeRecipient,
		Random:           attrs.PrevRandao,
		Withdrawals:      withdrawals,
		BeaconRoot:       &beaconRoot,
		GasLimit:         parentHeader.GasLimit,
		InclusionListTxs: inclusionListTxs,
	}

	// Generate payload ID.
	payloadID := generatePayloadID(parentHeader.Hash(), buildAttrs)

	// Idempotency check.
	b.mu.Lock()
	if existing, ok := b.payloads[payloadID]; ok && existing != nil {
		b.mu.Unlock()
		slog.Debug("engine_forkchoiceUpdated: payload already building, reusing slot",
			"payloadID", payloadID,
		)
		return payload.ForkchoiceUpdatedResult{
			PayloadStatus: payloadStatus,
			PayloadID:     &payloadID,
		}, nil
	}

	// Register in-progress slot.
	pp := &pendingPayload{done: make(chan struct{}), slot: slot}
	b.payloads[payloadID] = pp
	b.payloadOrder = append(b.payloadOrder, payloadID)
	for len(b.payloads) > b.maxPayloads && len(b.payloadOrder) > 0 {
		oldest := b.payloadOrder[0]
		b.payloadOrder = b.payloadOrder[1:]
		delete(b.payloads, oldest)
	}
	b.mu.Unlock()

	// Build the block in the background.
	if !b.launch(func() {
		defer close(pp.done)
		parentBlock := b.node.Blockchain().GetBlock(parentHeader.Hash())
		var statedb state.StateDB
		if parentBlock != nil {
			var err error
			statedb, err = b.node.Blockchain().StateAtBlock(parentBlock)
			if err != nil {
				slog.Warn("engine_forkchoiceUpdated: state fetch failed",
					"parentNum", parentHeader.Number,
					"parentHash", parentHeader.Hash(),
					"err", err,
				)
				pp.err = fmt.Errorf("state fetch: %w", err)
				return
			}
		}
		slog.Debug("engine_forkchoiceUpdated: state ready",
			"parentNum", parentHeader.Number,
			"parentHash", parentHeader.Hash(),
		)

		b.buildMu.Lock()
		defer b.buildMu.Unlock()

		if statedb != nil {
			b.builder.SetState(statedb)
		}

		slog.Debug("engine_forkchoiceUpdated: calling BuildBlock",
			"parentNum", parentHeader.Number,
			"parentHash", parentHeader.Hash(),
		)
		builtBlock, receipts, err := b.builder.BuildBlock(parentHeader, buildAttrs)
		if err != nil {
			slog.Warn("engine_forkchoiceUpdated: build block failed",
				"parentNum", parentHeader.Number,
				"err", err,
			)
			pp.err = fmt.Errorf("build block: %w", err)
			return
		}

		// Replace VERIFY frame calldata with STARK proof when enabled.
		if prover := b.node.StarkFrameProver(); prover != nil {
			if sealed, _, err := vm.ReplaceValidationFrames(builtBlock, prover); err == nil {
				builtBlock = sealed
			}
		}

		slog.Debug("engine_forkchoiceUpdated: BuildBlock done",
			"blockNum", builtBlock.NumberU64(),
			"blockHash", builtBlock.Hash(),
			"txCount", len(builtBlock.Transactions()),
		)

		pp.block = builtBlock
		pp.receipts = receipts

		b.cacheBlobsFromBlock(builtBlock)

		result, insertErr := b.node.Blockchain().InsertSyncedBlockWithResult(builtBlock)
		pp.result = result
		if insertErr != nil {
			slog.Warn("engine_forkchoiceUpdated: pre-insert built block failed",
				"blockNum", builtBlock.NumberU64(),
				"blockHash", builtBlock.Hash(),
				"err", insertErr,
			)
			pp.err = fmt.Errorf("validate built block: %w", insertErr)
			return
		}

		slog.Info("engine_forkchoiceUpdated: built payload",
			"payloadID", payloadID,
			"blockNumber", builtBlock.NumberU64(),
			"blockHash", builtBlock.Hash(),
			"txCount", len(builtBlock.Transactions()),
		)
	}) {
		pp.err = fmt.Errorf("engine backend closed")
		close(pp.done)
		return payload.ForkchoiceUpdatedResult{}, pp.err
	}

	return payload.ForkchoiceUpdatedResult{
		PayloadStatus: payloadStatus,
		PayloadID:     &payloadID,
	}, nil
}

// ForkchoiceUpdatedV4 processes a forkchoice update with V4 payload attributes.
func (b *EngineBackend) ForkchoiceUpdatedV4(
	fcState payload.ForkchoiceStateV1,
	attrs *payload.PayloadAttributesV4,
) (payload.ForkchoiceUpdatedResult, error) {
	var v3Attrs *payload.PayloadAttributesV3
	if attrs != nil {
		v3Attrs = &attrs.PayloadAttributesV3
	}
	if attrs == nil {
		return b.ForkchoiceUpdated(fcState, nil)
	}
	return b.forkchoiceUpdated(fcState, v3Attrs, attrs.InclusionListTransactions, attrs.SlotNumber)
}
