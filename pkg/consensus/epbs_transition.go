// epbs_transition.go implements the Gloas (EIP-7732) state-transition
// functions: process_execution_payload_bid, process_payload_attestation,
// process_builder_pending_payments, and the consensus side of
// process_execution_payload, plus the attestation-weight and slashing
// helpers that feed the builder pending-payment window.
//
// Formulas follow refs/consensus-specs/specs/gloas/beacon-chain.md.
package consensus

// G2PointAtInfinity is the compressed BLS12-381 G2 infinity signature,
// the required self-build bid signature (bls.G2_POINT_AT_INFINITY).
var G2PointAtInfinity = [96]byte{0xc0}

// paymentWindowIndex maps an attested slot to its index in the
// builder_pending_payments window: the second epoch half holds payments
// for the current epoch, the first half for the previous epoch.
func paymentWindowIndex(state *BeaconStateV2, slot uint64, currentEpochTarget bool) int {
	spe := stateOrFallbackSPE(state)
	offset := uint64(0)
	if currentEpochTarget {
		offset = spe
	}
	return int(offset + slot%spe)
}

// ProcessExecutionPayloadBid validates the signed bid carried in a beacon
// block body and records the builder pending payment, per
// process_execution_payload_bid. maxBlobsPerBlock comes from the fork's
// blob schedule (get_blob_parameters).
func ProcessExecutionPayloadBid(
	state *BeaconStateV2,
	block *BeaconBlockV2,
	forkVersion [4]byte,
	genesisRoot [32]byte,
	maxBlobsPerBlock int,
) error {
	if state == nil {
		return ErrEPBSNilState
	}
	if block == nil || block.Body == nil {
		return ErrEPBSNilBody
	}
	signedBid := block.Body.SignedExecutionPayloadBid
	if signedBid == nil {
		return ErrEPBSNilBid
	}
	bid := signedBid.Message
	builderIndex := bid.BuilderIndex
	amount := bid.Value

	// For self-builds, amount must be zero regardless of withdrawal
	// credential prefix, and the signature must be the point at infinity.
	if builderIndex == BuilderIndexSelfBuild {
		if amount != 0 {
			return ErrEPBSSelfBuildAmount
		}
		if signedBid.Signature != G2PointAtInfinity {
			return ErrEPBSSelfBuildSig
		}
	} else {
		// Verify that the builder is active.
		if !state.IsActiveBuilder(builderIndex) {
			return ErrEPBSInactiveBuilder
		}
		// Verify that the builder has funds to cover the bid.
		if !state.CanBuilderCoverBid(builderIndex, amount) {
			return ErrEPBSInsufficientFunds
		}
		// Verify that the bid signature is valid.
		if !VerifyExecutionPayloadBidSignature(state, signedBid, forkVersion, genesisRoot) {
			return ErrEPBSInvalidBidSig
		}
	}

	// Verify commitments are under limit.
	if len(bid.BlobKZGCommitments) > maxBlobsPerBlock {
		return ErrEPBSBlobLimit
	}

	// Verify that the bid is for the current slot.
	if bid.Slot != uint64(block.Slot) {
		return ErrEPBSBidSlotMismatch
	}
	// Verify that the bid is for the right parent block.
	if bid.ParentBlockHash != state.LatestBlockHash {
		return ErrEPBSBidParentHash
	}
	if bid.ParentBlockRoot != block.ParentRoot {
		return ErrEPBSBidParentRoot
	}
	if bid.PrevRandao != state.RandaoMixes[uint64(state.GetCurrentEpoch())%EpochsPerHistoricalVector] {
		return ErrEPBSBidPrevRandao
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	state.ensureEPBSState()

	// Record the pending payment if there is some payment.
	if amount > 0 {
		state.BuilderPendingPayments[paymentWindowIndex(state, uint64(block.Slot), true)] = BuilderPendingPayment{
			Withdrawal: BuilderPendingWithdrawal{
				FeeRecipient: [20]byte(bid.FeeRecipient),
				Amount:       amount,
				BuilderIndex: builderIndex,
			},
		}
	}

	// Cache the signed execution payload bid.
	state.LatestExecutionPayloadBid = &bid
	return nil
}

// ProcessPayloadAttestation validates one aggregated PTC attestation
// included in a beacon block, per process_payload_attestation.
func ProcessPayloadAttestation(
	state *BeaconStateV2,
	attestation *PayloadAttestation,
	forkVersion [4]byte,
	genesisRoot [32]byte,
) error {
	if state == nil {
		return ErrEPBSNilState
	}
	if attestation == nil {
		return ErrEPBSNilAttestation
	}
	data := attestation.Data

	// Check that the attestation is for the parent beacon block.
	if data.BeaconBlockRoot != state.LatestBlockHeader.ParentRoot {
		return ErrEPBSAttestationRoot
	}
	// Check that the attestation is for the previous slot.
	if data.Slot+1 != state.Slot {
		return ErrEPBSAttestationSlot
	}
	// Verify signature over the resolved PTC indices.
	indexed, err := GetIndexedPayloadAttestation(state, attestation)
	if err != nil {
		return err
	}
	if !VerifyIndexedPayloadAttestation(state, indexed, forkVersion, genesisRoot) {
		return ErrEPBSAttestationSig
	}
	return nil
}

// ProcessBuilderPendingPayments flushes quorum-weighted payments from the
// previous-epoch half of the window into the withdrawal queue and slides
// the window, per process_builder_pending_payments. Called during epoch
// processing, before effective balance updates.
func ProcessBuilderPendingPayments(state *BeaconStateV2) {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.ensureEPBSState()

	spe := int(stateOrFallbackSPE(state))
	quorum := state.builderPaymentQuorumThresholdLocked()
	for i := 0; i < spe && i < len(state.BuilderPendingPayments); i++ {
		payment := state.BuilderPendingPayments[i]
		if payment.Weight >= quorum {
			state.BuilderPendingWithdrawals = append(state.BuilderPendingWithdrawals, payment.Withdrawal)
		}
	}
	if len(state.BuilderPendingWithdrawals) > int(BuilderPendingWithdrawalsLimit) {
		state.BuilderPendingWithdrawals = state.BuilderPendingWithdrawals[len(state.BuilderPendingWithdrawals)-int(BuilderPendingWithdrawalsLimit):]
	}
	oldPayments := append([]BuilderPendingPayment(nil), state.BuilderPendingPayments[spe:]...)
	newPayments := make([]BuilderPendingPayment, spe)
	state.BuilderPendingPayments = append(oldPayments, newPayments...)
}

// AddBuilderPaymentWeight records the effective balance of a validator's
// same-slot attestation toward the builder payment quorum, per the Gloas
// process_attestation modification. currentEpochTarget selects the window
// half; the caller must only count validators setting a new participation
// flag this slot.
func AddBuilderPaymentWeight(state *BeaconStateV2, slot uint64, currentEpochTarget bool, validatorIndex uint64) {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.ensureEPBSState()

	idx := paymentWindowIndex(state, slot, currentEpochTarget)
	if idx < 0 || idx >= len(state.BuilderPendingPayments) {
		return
	}
	payment := &state.BuilderPendingPayments[idx]
	if payment.Withdrawal.Amount > 0 && validatorIndex < uint64(len(state.Validators)) {
		payment.Weight += state.Validators[validatorIndex].EffectiveBalance
	}
}

// RemoveBuilderPaymentForSlashedProposal clears the pending payment of a
// slashed proposer's slot, per the Gloas process_proposer_slashing
// modification.
func RemoveBuilderPaymentForSlashedProposal(state *BeaconStateV2, slot uint64) {
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.ensureEPBSState()

	spe := stateOrFallbackSPE(state)
	proposalEpoch := Epoch(slot / spe)
	var idx int
	switch proposalEpoch {
	case state.GetCurrentEpoch():
		idx = paymentWindowIndex(state, slot, true)
	case state.GetPreviousEpoch():
		idx = paymentWindowIndex(state, slot, false)
	default:
		return
	}
	if idx >= 0 && idx < len(state.BuilderPendingPayments) {
		state.BuilderPendingPayments[idx] = BuilderPendingPayment{}
	}
}

// ProcessExecutionPayload applies the consensus side of a revealed payload
// envelope, per process_execution_payload: signature and consistency checks
// against the committed bid, builder payment queueing, and availability
// bookkeeping. The execution-engine call itself stays in the caller (the
// engine already validates and imports payloads).
func ProcessExecutionPayload(
	state *BeaconStateV2,
	signedEnvelope *SignedExecutionPayloadEnvelope,
	proposerIndex uint64,
	verifySignature bool,
	forkVersion [4]byte,
	genesisRoot [32]byte,
) error {
	if state == nil {
		return ErrEPBSNilState
	}
	if signedEnvelope == nil {
		return ErrEPBSNilEnvelope
	}
	envelope := signedEnvelope.Message

	if verifySignature &&
		!VerifyExecutionPayloadEnvelopeSignature(state, signedEnvelope, proposerIndex, forkVersion, genesisRoot) {
		return ErrEPBSAttestationSig
	}

	// Cache latest block header state root, per process_slot /
	// process_execution_payload: the header root used for the envelope
	// commitment includes the cached state root.
	previousStateRoot := state.HashTreeRootV2()
	if state.LatestBlockHeader.StateRoot == ([32]byte{}) {
		state.LatestBlockHeader.StateRoot = previousStateRoot
	}
	headerRoot := HashBeaconBlockHeader(&BeaconBlockHeader{
		Slot:          state.LatestBlockHeader.Slot,
		ProposerIndex: state.LatestBlockHeader.ProposerIndex,
		ParentRoot:    state.LatestBlockHeader.ParentRoot,
		StateRoot:     state.LatestBlockHeader.StateRoot,
		BodyRoot:      state.LatestBlockHeader.BodyRoot,
	})

	// Verify consistency with the beacon block.
	if envelope.BeaconBlockRoot != headerRoot {
		return ErrEPBSEnvelopeBlockRoot
	}
	if envelope.Slot != state.Slot {
		return ErrEPBSEnvelopeSlot
	}

	// Verify consistency with the committed bid.
	committedBid := state.LatestExecutionPayloadBid
	if committedBid == nil {
		return ErrEPBSNilBid
	}
	if envelope.BuilderIndex != committedBid.BuilderIndex {
		return ErrEPBSEnvelopeBuilder
	}
	if envelope.PrevRandao != committedBid.PrevRandao {
		return ErrEPBSEnvelopePrevRandao
	}
	// Verify the gas limit and the block hash.
	if envelope.GasLimit != committedBid.GasLimit {
		return ErrEPBSEnvelopeGasLimit
	}
	if envelope.BlockHash != committedBid.BlockHash {
		return ErrEPBSEnvelopeBlockHash
	}
	// Verify consistency of the parent hash with the previous payload.
	if envelope.ParentHash != state.LatestBlockHash {
		return ErrEPBSEnvelopeParent
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	state.ensureEPBSState()

	// Queue the builder payment.
	idx := paymentWindowIndex(state, state.Slot, true)
	payment := state.BuilderPendingPayments[idx]
	if payment.Withdrawal.Amount > 0 {
		state.BuilderPendingWithdrawals = append(state.BuilderPendingWithdrawals, payment.Withdrawal)
	}
	state.BuilderPendingPayments[idx] = BuilderPendingPayment{}

	// Cache the execution payload hash.
	state.ExecutionPayloadAvailability[state.Slot%SlotsPerHistoricalRoot] = true
	state.LatestBlockHash = envelope.BlockHash
	return nil
}
