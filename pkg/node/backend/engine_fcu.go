package backend

import (
	"fmt"
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
	if mode, ok := b.node.(interface{ ExternalForkchoiceAllowed() bool }); ok && !mode.ExternalForkchoiceAllowed() {
		return payload.ForkchoiceUpdatedResult{}, fmt.Errorf("%w: native consensus mode", engine.ErrInvalidForkchoiceState)
	}
	bc := b.node.Blockchain()

	slog.Debug("engine_forkchoiceUpdated",
		"headBlockHash", fcState.HeadBlockHash,
		"safeBlockHash", fcState.SafeBlockHash,
		"finalizedBlockHash", fcState.FinalizedBlockHash,
		"hasPayloadAttrs", attrs != nil,
		"genesisHash", bc.Genesis().Hash(),
	)

	// Step 1: look up the forkchoice head block.
	if syncer, ok := b.node.(interface {
		RequestExecutionSync(payload.ForkchoiceStateV1)
	}); ok {
		syncer.RequestExecutionSync(fcState)
	}
	headBlock := bc.GetBlock(fcState.HeadBlockHash)
	if headBlock == nil {
		slog.Warn("engine_forkchoiceUpdated: unknown head block, returning SYNCING",
			"headBlockHash", fcState.HeadBlockHash,
			"genesisHash", bc.Genesis().Hash(),
			"currentHead", bc.CurrentBlock().Hash(),
		)
		return payload.ForkchoiceUpdatedResult{
			PayloadStatus: payload.PayloadStatusV1{Status: engine.StatusSyncing},
		}, nil
	}

	headHash := headBlock.Hash()
	payloadStatus := payload.PayloadStatusV1{
		Status:          engine.StatusValid,
		LatestValidHash: &headHash,
	}

	// Step 2: update in-memory safe/finalized hashes.
	b.fcMu.Lock()
	finalHashChanged := fcState.FinalizedBlockHash != (types.Hash{}) &&
		fcState.FinalizedBlockHash != b.finalizedHash
	safeHashChanged := fcState.SafeBlockHash != (types.Hash{}) &&
		fcState.SafeBlockHash != b.safeHash
	if fcState.FinalizedBlockHash != (types.Hash{}) {
		b.finalizedHash = fcState.FinalizedBlockHash
	}
	if fcState.SafeBlockHash != (types.Hash{}) {
		b.safeHash = fcState.SafeBlockHash
	}
	b.fcMu.Unlock()

	if finalHashChanged {
		slog.Info("engine_forkchoiceUpdated: finalized block advanced",
			"finalizedHash", fcState.FinalizedBlockHash,
			"headNum", headBlock.NumberU64(),
		)
	}

	// Step 3: FCU cache check.
	triple := fcuCacheEntry{
		head:      headHash,
		safe:      fcState.SafeBlockHash,
		finalized: fcState.FinalizedBlockHash,
	}
	if attrs == nil && !finalHashChanged && !safeHashChanged {
		b.fcuCacheMu.Lock()
		hit := b.fcuCacheContains(triple)
		b.fcuCacheMu.Unlock()
		if hit {
			slog.Debug("engine_forkchoiceUpdated: cache hit, returning immediately",
				"headBlockHash", headHash,
			)
			return payload.ForkchoiceUpdatedResult{PayloadStatus: payloadStatus}, nil
		}
	}

	// Step 4: resolve finalized/safe blocks.
	var finalBlock, safeBlock *types.Block
	if finalHashChanged {
		if finalBlock = bc.GetBlock(fcState.FinalizedBlockHash); finalBlock == nil {
			slog.Warn("engine_forkchoiceUpdated: finalized block unknown, skipping",
				"hash", fcState.FinalizedBlockHash,
			)
		}
	}
	if safeHashChanged {
		if safeBlock = bc.GetBlock(fcState.SafeBlockHash); safeBlock == nil {
			slog.Warn("engine_forkchoiceUpdated: safe block unknown, skipping",
				"hash", fcState.SafeBlockHash,
			)
		}
	}

	// Step 5: push slow work to background goroutine.
	b.sendPostFCUWork(postFCUWork{
		fcState:    fcState,
		headBlock:  headBlock,
		finalBlock: finalBlock,
		safeBlock:  safeBlock,
		hasAttrs:   attrs != nil,
	})

	// Step 6: store triple in FCU cache.
	b.fcuCacheMu.Lock()
	b.fcuCache[b.fcuCacheWr] = triple
	b.fcuCacheWr = (b.fcuCacheWr + 1) % fcuCacheSize
	b.fcuCacheMu.Unlock()

	// Step 7: no payload attributes — return immediately.
	if attrs == nil {
		slog.Debug("engine_forkchoiceUpdated: no attrs, done",
			"headNum", headBlock.NumberU64(),
		)
		return payload.ForkchoiceUpdatedResult{PayloadStatus: payloadStatus}, nil
	}

	// Step 7a: check if we can quickly obtain the parent state.
	const stateThreshold = 32
	if !bc.CanQuicklyGetState(headBlock, stateThreshold) {
		slog.Warn("engine_forkchoiceUpdated: parent state needs deep re-execution, warming and returning SYNCING",
			"headNum", headBlock.NumberU64(),
			"headHash", headBlock.Hash(),
		)
		go func() {
			_, err := bc.StateAtBlock(headBlock)
			if err != nil {
				slog.Warn("engine_forkchoiceUpdated: background state warming failed",
					"headNum", headBlock.NumberU64(),
					"err", err,
				)
			} else {
				slog.Info("engine_forkchoiceUpdated: background state warming completed",
					"headNum", headBlock.NumberU64(),
				)
			}
		}()
		return payload.ForkchoiceUpdatedResult{
			PayloadStatus: payload.PayloadStatusV1{Status: engine.StatusSyncing},
		}, nil
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
	go func() {
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

		result, insertErr := b.node.Blockchain().InsertBlockWithResult(builtBlock)
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
	}()

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

// fcuCacheContains reports whether e is present in the FCU cache.
func (b *EngineBackend) fcuCacheContains(e fcuCacheEntry) bool {
	for _, c := range b.fcuCache {
		if c == e {
			return true
		}
	}
	return false
}

// sendPostFCUWork dispatches work to the background goroutine non-blocking.
// If the channel is full, the previous work is discarded and replaced with new work.
func (b *EngineBackend) sendPostFCUWork(work postFCUWork) {
	select {
	case b.postFCUCh <- work:
	default:
		// Channel full, drain and retry
		select {
		case <-b.postFCUCh:
		default:
		}
		b.postFCUCh <- work
	}
}

// postFCULoop handles slow post-FCU state updates.
func (b *EngineBackend) postFCULoop() {
	for {
		select {
		case <-b.stopCh:
			return
		case work := <-b.postFCUCh:
			b.doPostFCUWork(work)
		}
	}
}

// doPostFCUWork performs state updates after ForkchoiceUpdated.
func (b *EngineBackend) doPostFCUWork(work postFCUWork) {
	bc := b.node.Blockchain()
	if bc == nil {
		slog.Warn("doPostFCUWork: blockchain is nil")
		return
	}

	if work.finalBlock != nil {
		slog.Info("doPostFCUWork: setting finalized block",
			"blockHash", work.finalBlock.Hash(),
			"blockNum", work.finalBlock.NumberU64(),
			"finalizedBlockHash", work.fcState.FinalizedBlockHash,
		)
		bc.SetFinalized(work.finalBlock)
	} else if work.fcState.FinalizedBlockHash != (types.Hash{}) {
		slog.Warn("doPostFCUWork: finalBlock is nil but finalizedBlockHash is set",
			"finalizedBlockHash", work.fcState.FinalizedBlockHash,
		)
	}
	if work.safeBlock != nil {
		slog.Debug("doPostFCUWork: setting safe block",
			"blockHash", work.safeBlock.Hash(),
			"blockNum", work.safeBlock.NumberU64(),
		)
		bc.SetSafe(work.safeBlock)
	}

	b.fcuCacheMu.Lock()
	b.fcuCache[b.fcuCacheWr] = fcuCacheEntry{
		head:      work.fcState.HeadBlockHash,
		safe:      work.fcState.SafeBlockHash,
		finalized: work.fcState.FinalizedBlockHash,
	}
	b.fcuCacheWr = (b.fcuCacheWr + 1) % fcuCacheSize
	b.fcuCacheMu.Unlock()
}
