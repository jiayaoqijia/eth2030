// epbs_state.go implements the Gloas (EIP-7732) beacon state accessors:
// staked-builder registry helpers, PTC election (get_ptc), the
// balance-weighted selection helpers, and the builder withdrawal plumbing
// used by process_withdrawals.
//
// Shapes and formulas follow refs/consensus-specs/specs/gloas/beacon-chain.md.
package consensus

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// ExpectedWithdrawal is a withdrawal scheduled by the consensus layer that
// the next execution payload must honor (payload_expected_withdrawals).
type ExpectedWithdrawal struct {
	Index          uint64
	ValidatorIndex uint64
	Address        [20]byte
	Amount         uint64
}

// ensureEPBSState lazily sizes the ePBS state lists. Safe to call on any
// state, including zero-value states created outside NewBeaconStateV2.
func (s *BeaconStateV2) ensureEPBSState() {
	spe := s.SlotsPerEpoch
	if spe == 0 {
		spe = 32
	}
	if s.ExecutionPayloadAvailability == nil {
		s.ExecutionPayloadAvailability = make([]bool, SlotsPerHistoricalRoot)
	}
	if s.BuilderPendingPayments == nil {
		s.BuilderPendingPayments = make([]BuilderPendingPayment, 2*spe)
	}
}

// GetBuilder returns the builder at the given registry index, or nil.
func (s *BeaconStateV2) GetBuilder(builderIndex uint64) *Builder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if builderIndex >= uint64(len(s.Builders)) {
		return nil
	}
	return s.Builders[builderIndex]
}

// AddBuilder appends (or reuses) a registry slot for a new builder and
// returns its index. Reuses the first exited builder with zero balance,
// matching get_index_for_new_builder.
func (s *BeaconStateV2) AddBuilder(builder *Builder) uint64 {
	currentEpoch := s.GetCurrentEpoch()
	for index, existing := range s.Builders {
		if existing.WithdrawableEpoch <= currentEpoch && existing.Balance == 0 {
			s.Builders[index] = builder
			return uint64(index)
		}
	}
	s.Builders = append(s.Builders, builder)
	return uint64(len(s.Builders) - 1)
}

// IsActiveBuilder reports whether the builder is registered and active
// (not yet withdrawable) per is_active_builder.
func (s *BeaconStateV2) IsActiveBuilder(builderIndex uint64) bool {
	builder := s.GetBuilder(builderIndex)
	if builder == nil {
		return false
	}
	return builder.Balance > 0 && builder.WithdrawableEpoch > s.GetCurrentEpoch()
}

// CanBuilderCoverBid reports whether the builder balance covers the bid
// amount per can_builder_cover_bid.
func (s *BeaconStateV2) CanBuilderCoverBid(builderIndex, amount uint64) bool {
	builder := s.GetBuilder(builderIndex)
	if builder == nil {
		return false
	}
	return builder.Balance >= amount
}

// GetBuilderPaymentQuorumThreshold returns the PTC weight a payment must
// gather before it is flushed to the withdrawal queue at the epoch boundary,
// per get_builder_payment_quorum_threshold. Thread-safe.
func (s *BeaconStateV2) GetBuilderPaymentQuorumThreshold() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.builderPaymentQuorumThresholdLocked()
}

// builderPaymentQuorumThresholdLocked is GetBuilderPaymentQuorumThreshold
// for callers already holding the state lock.
func (s *BeaconStateV2) builderPaymentQuorumThresholdLocked() uint64 {
	totalBalance := s.totalBal(s.GetCurrentEpoch())
	spe := s.SlotsPerEpoch
	if spe == 0 {
		spe = 32
	}
	perSlotBalance := totalBalance / spe
	return perSlotBalance * BuilderPaymentThresholdNumerator / BuilderPaymentThresholdDenominator
}

// GetPTC returns the payload timeliness committee for the given slot per
// get_ptc: all slot committees concatenated, then balance-weighted
// selection of PTC_SIZE members without shuffling the candidate list.
func GetPTC(state *BeaconStateV2, slot Slot) []uint64 {
	if state == nil {
		return nil
	}
	epoch := Epoch(uint64(slot) / stateOrFallbackSPE(state))
	seed, err := GetSeed(state, epoch, DomainPTCAttesterUint32())
	if err != nil {
		return nil
	}
	seeded := sha256.Sum256(append(seed[:], epbsUint64Bytes(uint64(slot))...))

	committeesPerSlot, err := GetCommitteeCountPerSlot(state, epoch)
	if err != nil {
		return nil
	}
	var indices []ValidatorIndex
	for i := uint64(0); i < committeesPerSlot; i++ {
		committee, err := ComputeBeaconCommittee(state, slot, i, beaconAttesterSeed(state, epoch))
		if err != nil {
			return nil
		}
		indices = append(indices, committee...)
	}
	if len(indices) == 0 {
		return nil
	}
	return computeBalanceWeightedSelection(state, indices, seeded, PTCSize, false)
}

// beaconAttesterSeed returns the epoch seed for beacon attester committees.
func beaconAttesterSeed(state *BeaconStateV2, epoch Epoch) [32]byte {
	seed, err := GetSeed(state, epoch, CADomainBeaconAttester)
	if err != nil {
		return [32]byte{}
	}
	return seed
}

// stateOrFallbackSPE returns the state's slots-per-epoch with a safe default.
func stateOrFallbackSPE(state *BeaconStateV2) uint64 {
	if state.SlotsPerEpoch == 0 {
		return 32
	}
	return state.SlotsPerEpoch
}

// DomainPTCAttesterUint32 returns the PTC attester domain as the uint32
// form used by GetSeed.
func DomainPTCAttesterUint32() uint32 {
	return binary.LittleEndian.Uint32([]byte(DomainPTCAttester[:]))
}

// computeBalanceWeightedSelection samples `size` indices by effective
// balance, per compute_balance_weighted_selection. When shuffle is true the
// candidate indices are themselves shuffled with the seed; otherwise the
// list is traversed in order.
func computeBalanceWeightedSelection(
	state *BeaconStateV2,
	indices []ValidatorIndex,
	seed [32]byte,
	size uint64,
	shuffle bool,
) []uint64 {
	total := uint64(len(indices))
	if total == 0 || size == 0 {
		return nil
	}
	state.mu.RLock()
	defer state.mu.RUnlock()

	selected := make([]uint64, 0, size)
	var i uint64
	for uint64(len(selected)) < size {
		nextIndex := i % total
		if shuffle {
			nextIndex = uint64(computeShuffledIndex(nextIndex, total, seed))
		}
		candidate := indices[nextIndex]
		if computeBalanceWeightedAcceptance(state, candidate, seed, i) {
			selected = append(selected, uint64(candidate))
		}
		i++
		if i == 0 {
			// Overflow guard: reject pathological termination.
			return nil
		}
	}
	return selected
}

// computeBalanceWeightedAcceptance accepts a candidate with probability
// proportional to its effective balance, per
// compute_balance_weighted_acceptance. Caller must hold the state read lock.
func computeBalanceWeightedAcceptance(state *BeaconStateV2, index ValidatorIndex, seed [32]byte, i uint64) bool {
	const maxRandomValue = uint64(1)<<16 - 1
	randomBytes := sha256.Sum256(append(seed[:], epbsUint64Bytes(i/16)...))
	offset := (i % 16) * 2
	randomValue := uint64(randomBytes[offset]) | uint64(randomBytes[offset+1])<<8
	if uint64(index) >= uint64(len(state.Validators)) {
		return false
	}
	effectiveBalance := state.Validators[index].EffectiveBalance
	return effectiveBalance*maxRandomValue >= MaxEffectiveBalanceV2*randomValue
}

// GetIndexedPayloadAttestation resolves the aggregation bits against the
// PTC of the attested slot, per get_indexed_payload_attestation.
func GetIndexedPayloadAttestation(
	state *BeaconStateV2,
	attestation *PayloadAttestation,
) (*IndexedPayloadAttestation, error) {
	if state == nil || attestation == nil {
		return nil, ErrEPBSNilAttestation
	}
	ptc := GetPTC(state, Slot(attestation.Data.Slot))
	if ptc == nil {
		return nil, ErrEPBSNilState
	}
	indexed := &IndexedPayloadAttestation{
		Data:      attestation.Data,
		Signature: attestation.Signature,
	}
	for i, member := range ptc {
		byteIdx := i / 8
		bitIdx := i % 8
		if byteIdx < len(attestation.AggregationBits) &&
			(attestation.AggregationBits[byteIdx]>>bitIdx)&1 != 0 {
			indexed.AttestingIndices = append(indexed.AttestingIndices, member)
		}
	}
	return indexed, nil
}

// VerifyIndexedPayloadAttestation verifies the aggregate BLS signature over
// the payload attestation data with the PTC domain, per
// is_valid_indexed_payload_attestation.
func VerifyIndexedPayloadAttestation(
	state *BeaconStateV2,
	indexed *IndexedPayloadAttestation,
	forkVersion [4]byte,
	genesisRoot [32]byte,
) bool {
	if state == nil || indexed == nil || len(indexed.AttestingIndices) == 0 {
		return false
	}
	pubkeys := make([][48]byte, 0, len(indexed.AttestingIndices))
	for _, idx := range indexed.AttestingIndices {
		if idx >= uint64(len(state.Validators)) {
			return false
		}
		pubkeys = append(pubkeys, state.Validators[idx].Pubkey)
	}
	domain := DomainSeparation(DomainPTCAttester, forkVersion, genesisRoot)
	signingRoot := ComputeSigningRoot(indexed.Data.HashTreeRoot(), domain)
	return crypto.FastAggregateVerify(pubkeys, signingRoot[:], indexed.Signature)
}

// VerifyExecutionPayloadBidSignature verifies the builder's signature over
// the bid message with the beacon builder domain, per
// verify_execution_payload_bid_signature.
func VerifyExecutionPayloadBidSignature(
	state *BeaconStateV2,
	signedBid *SignedExecutionPayloadBid,
	forkVersion [4]byte,
	genesisRoot [32]byte,
) bool {
	if state == nil || signedBid == nil {
		return false
	}
	builder := state.GetBuilder(signedBid.Message.BuilderIndex)
	if builder == nil {
		return false
	}
	domain := DomainSeparation(DomainBeaconBuilder, forkVersion, genesisRoot)
	signingRoot := ComputeSigningRoot(signedBid.Message.HashTreeRoot(), domain)
	return crypto.BLSVerify(builder.Pubkey, signingRoot[:], signedBid.Signature)
}

// VerifyExecutionPayloadEnvelopeSignature verifies the builder signature on
// a payload envelope per verify_execution_payload_envelope_signature. For
// self-builds the envelope is signed with the beacon proposer's key.
func VerifyExecutionPayloadEnvelopeSignature(
	state *BeaconStateV2,
	signedEnvelope *SignedExecutionPayloadEnvelope,
	proposerIndex uint64,
	forkVersion [4]byte,
	genesisRoot [32]byte,
) bool {
	if state == nil || signedEnvelope == nil {
		return false
	}
	var pubkey [48]byte
	if signedEnvelope.Message.BuilderIndex == BuilderIndexSelfBuild {
		if proposerIndex >= uint64(len(state.Validators)) {
			return false
		}
		pubkey = state.Validators[proposerIndex].Pubkey
	} else {
		builder := state.GetBuilder(signedEnvelope.Message.BuilderIndex)
		if builder == nil {
			return false
		}
		pubkey = builder.Pubkey
	}
	domain := DomainSeparation(DomainBeaconBuilder, forkVersion, genesisRoot)
	signingRoot := ComputeSigningRoot(envelopeHashRoot(&signedEnvelope.Message), domain)
	return crypto.BLSVerify(pubkey, signingRoot[:], signedEnvelope.Signature)
}

// envelopeHashRoot computes the signing root input of a payload envelope.
func envelopeHashRoot(env *ExecutionPayloadEnvelope) [32]byte {
	var data []byte
	var builderBytes [8]byte
	binary.LittleEndian.PutUint64(builderBytes[:], env.BuilderIndex)
	data = append(data, builderBytes[:]...)
	data = append(data, env.BeaconBlockRoot[:]...)
	data = append(data, epbsUint64Bytes(env.Slot)...)
	data = append(data, env.StateRoot[:]...)
	data = append(data, env.PrevRandao[:]...)
	data = append(data, env.BlockHash[:]...)
	data = append(data, env.ParentHash[:]...)
	data = append(data, epbsUint64Bytes(env.GasLimit)...)
	return sha256.Sum256(data)
}

// GetBuilderWithdrawals drains the builder pending-withdrawal queue into
// execution-layer withdrawals, per get_builder_withdrawals. The queue is
// not mutated; callers update it with the returned processed count.
func GetBuilderWithdrawals(
	state *BeaconStateV2,
	withdrawalIndex uint64,
	priorCount int,
) ([]ExpectedWithdrawal, uint64, uint64) {
	spe := stateOrFallbackSPE(state)
	maxPerPayload := int(spe) // MAX_WITHDRAWALS_PER_PAYLOAD proxy: one per slot
	if maxPerPayload < 16 {
		maxPerPayload = 16
	}
	withdrawalsLimit := maxPerPayload - 1
	if priorCount >= withdrawalsLimit {
		return nil, withdrawalIndex, 0
	}

	var withdrawals []ExpectedWithdrawal
	processed := uint64(0)
	for _, pending := range state.BuilderPendingWithdrawals {
		if priorCount+len(withdrawals) >= withdrawalsLimit {
			break
		}
		withdrawals = append(withdrawals, ExpectedWithdrawal{
			Index:          withdrawalIndex,
			ValidatorIndex: ConvertBuilderIndexToValidatorIndex(pending.BuilderIndex),
			Address:        pending.FeeRecipient,
			Amount:         pending.Amount,
		})
		withdrawalIndex++
		processed++
	}
	return withdrawals, withdrawalIndex, processed
}

// GetBuildersSweepWithdrawals sweeps withdrawable builders with remaining
// balances, per get_builders_sweep_withdrawals.
func GetBuildersSweepWithdrawals(
	state *BeaconStateV2,
	withdrawalIndex uint64,
	priorCount int,
) ([]ExpectedWithdrawal, uint64, uint64) {
	spe := stateOrFallbackSPE(state)
	maxPerPayload := int(spe)
	if maxPerPayload < 16 {
		maxPerPayload = 16
	}
	withdrawalsLimit := maxPerPayload - 1
	if priorCount >= withdrawalsLimit || len(state.Builders) == 0 {
		return nil, withdrawalIndex, 0
	}

	buildersLimit := len(state.Builders)
	if buildersLimit > int(MaxBuildersPerWithdrawalsSweep) {
		buildersLimit = int(MaxBuildersPerWithdrawalsSweep)
	}

	epoch := state.GetCurrentEpoch()
	var withdrawals []ExpectedWithdrawal
	processed := uint64(0)
	builderIndex := state.NextWithdrawalBuilderIndex % uint64(len(state.Builders))
	for i := 0; i < buildersLimit; i++ {
		if priorCount+len(withdrawals) >= withdrawalsLimit {
			break
		}
		builder := state.Builders[builderIndex]
		if builder.WithdrawableEpoch <= epoch && builder.Balance > 0 {
			withdrawals = append(withdrawals, ExpectedWithdrawal{
				Index:          withdrawalIndex,
				ValidatorIndex: ConvertBuilderIndexToValidatorIndex(builderIndex),
				Address:        builder.ExecutionAddress,
				Amount:         builder.Balance,
			})
			withdrawalIndex++
		}
		builderIndex = (builderIndex + 1) % uint64(len(state.Builders))
		processed++
	}
	return withdrawals, withdrawalIndex, processed
}

// ApplyExpectedWithdrawalToBuilders deducts builder withdrawals from the
// builder registry (validator withdrawals are deducted by the caller),
// per the builder branch of apply_withdrawals.
func (s *BeaconStateV2) ApplyExpectedWithdrawalToBuilders(withdrawal ExpectedWithdrawal) {
	if !IsBuilderIndex(withdrawal.ValidatorIndex) {
		return
	}
	builderIndex := ConvertValidatorIndexToBuilderIndex(withdrawal.ValidatorIndex)
	builder := s.GetBuilder(builderIndex)
	if builder == nil {
		return
	}
	deduct := withdrawal.Amount
	if builder.Balance < deduct {
		deduct = builder.Balance
	}
	builder.Balance -= deduct
}

// UpdatePayloadExpectedWithdrawals stores the withdrawals the next payload
// must honor, per update_payload_expected_withdrawals.
func (s *BeaconStateV2) UpdatePayloadExpectedWithdrawals(withdrawals []ExpectedWithdrawal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.PayloadExpectedWithdrawals = append([]ExpectedWithdrawal(nil), withdrawals...)
}

// UpdateBuilderPendingWithdrawals drops the processed head of the pending
// queue, per update_builder_pending_withdrawals.
func (s *BeaconStateV2) UpdateBuilderPendingWithdrawals(processedCount uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if processedCount >= uint64(len(s.BuilderPendingWithdrawals)) {
		s.BuilderPendingWithdrawals = s.BuilderPendingWithdrawals[:0]
		return
	}
	s.BuilderPendingWithdrawals = s.BuilderPendingWithdrawals[processedCount:]
}

// UpdateNextWithdrawalBuilderIndex advances the builder sweep cursor, per
// update_next_withdrawal_builder_index.
func (s *BeaconStateV2) UpdateNextWithdrawalBuilderIndex(processedCount uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.Builders) == 0 {
		return
	}
	s.NextWithdrawalBuilderIndex = (s.NextWithdrawalBuilderIndex + processedCount) % uint64(len(s.Builders))
}

// IsParentBlockFull reports whether the previous slot's payload was
// included, per is_parent_block_full: the availability bit of the previous
// slot is set.
func (s *BeaconStateV2) IsParentBlockFull() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.Slot == 0 || s.ExecutionPayloadAvailability == nil {
		return false
	}
	prev := (s.Slot - 1) % SlotsPerHistoricalRoot
	return s.ExecutionPayloadAvailability[prev]
}

// MarkPayloadAvailable records the current slot's payload as revealed and
// processed, updating execution_payload_availability and latest_block_hash,
// per the tail of process_execution_payload.
func (s *BeaconStateV2) MarkPayloadAvailable(blockHash types.Hash) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureEPBSState()
	s.ExecutionPayloadAvailability[s.Slot%SlotsPerHistoricalRoot] = true
	s.LatestBlockHash = blockHash
}
