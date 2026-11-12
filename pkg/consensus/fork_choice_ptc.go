// fork_choice_ptc.go extends ForkChoiceV2 with the Gloas (EIP-7732)
// payload-status layer: PTC vote tracking per block root, payload
// presence/availability states, parent payload classification (FULL /
// EMPTY / PENDING), and head selection that filters children by payload
// status compatibility and breaks FULL/EMPTY ties with
// should_extend_payload.
//
// Scope note: LMD vote weights still follow the pre-Gloas root-chain
// propagation (is_supporting_vote payload_present matching is applied as
// the eligibility filter and tiebreaker here, not as per-node weight zeroing).
package consensus

import (
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// ptcVoteVector records one boolean vote per PTC member slot.
type ptcVoteVector = [PTCSize]bool

// EPBSForkChoice wraps ForkChoiceV2 with the ePBS fork-choice extensions.
type EPBSForkChoice struct {
	fc *ForkChoiceV2
	mu sync.RWMutex

	// Bid commitments per block root: bid.block_hash and
	// bid.parent_block_hash, for parent payload classification.
	bidBlockHash    map[types.Hash]types.Hash
	bidParentHash   map[types.Hash]types.Hash
	bidSlot         map[types.Hash]uint64
	parentBlockRoot map[types.Hash]types.Hash

	// PTC votes per block root.
	timelinessVotes map[types.Hash]*ptcVoteVector
	availabilityVo  map[types.Hash]*ptcVoteVector

	// Locally revealed and processed payloads (payload_states).
	payloadStates map[types.Hash]bool

	// Proposer boost root for should_extend_payload.
	proposerBoostRoot types.Hash

	// Resolver for the PTC membership of a slot and the current slot.
	ptcResolver func(slot uint64) []uint64
	currentSlot func() uint64
}

// NewEPBSForkChoice wraps an existing ForkChoiceV2.
func NewEPBSForkChoice(fc *ForkChoiceV2) *EPBSForkChoice {
	return &EPBSForkChoice{
		fc:              fc,
		bidBlockHash:    make(map[types.Hash]types.Hash),
		bidParentHash:   make(map[types.Hash]types.Hash),
		bidSlot:         make(map[types.Hash]uint64),
		parentBlockRoot: make(map[types.Hash]types.Hash),
		timelinessVotes: make(map[types.Hash]*ptcVoteVector),
		availabilityVo:  make(map[types.Hash]*ptcVoteVector),
		payloadStates:   make(map[types.Hash]bool),
	}
}

// SetPTCResolver installs the PTC membership resolver (GetPTC over the
// beacon state).
func (e *EPBSForkChoice) SetPTCResolver(fn func(slot uint64) []uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ptcResolver = fn
}

// SetCurrentSlotFn installs the current-slot clock used by wire-message
// validation and tiebreakers.
func (e *EPBSForkChoice) SetCurrentSlotFn(fn func() uint64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.currentSlot = fn
}

// SetProposerBoostRoot records the boosted proposer block root.
func (e *EPBSForkChoice) SetProposerBoostRoot(root types.Hash) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.proposerBoostRoot = root
}

// OnBlockWithBid registers a beacon block carrying a signed execution
// payload bid, mirroring on_block: votes initialize to false (the anchor
// block may be seeded full via SeedPayloadVotes).
func (e *EPBSForkChoice) OnBlockWithBid(root types.Hash, bid *ExecutionPayloadBid) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if bid != nil {
		e.bidBlockHash[root] = bid.BlockHash
		e.bidParentHash[root] = bid.ParentBlockHash
		e.bidSlot[root] = bid.Slot
	}
	if _, ok := e.timelinessVotes[root]; !ok {
		e.timelinessVotes[root] = &ptcVoteVector{}
		e.availabilityVo[root] = &ptcVoteVector{}
	}
}

// SeedPayloadVotes marks the anchor block's payload as present and
// available, mirroring get_forkchoice_store.
func (e *EPBSForkChoice) SeedPayloadVotes(root types.Hash) {
	e.mu.Lock()
	defer e.mu.Unlock()
	full := &ptcVoteVector{}
	for i := range full {
		full[i] = true
	}
	e.timelinessVotes[root] = full
	e.availabilityVo[root] = full
	e.payloadStates[root] = true
}

// MarkPayloadRevealed records that the payload for the block root was
// revealed and processed locally (payload_states).
func (e *EPBSForkChoice) MarkPayloadRevealed(root types.Hash) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.payloadStates[root] = true
}

// NotifyPTCAttestations extracts per-member votes from aggregated payload
// attestations included in a beacon block, per notify_ptc_messages.
// Signature verification is not needed for block-included attestations.
func (e *EPBSForkChoice) NotifyPTCAttestations(state *BeaconStateV2, attestations []*PayloadAttestation) {
	if state == nil || state.Slot == 0 {
		return
	}
	for _, att := range attestations {
		if att == nil {
			continue
		}
		// All signers of an aggregated attestation share the same data, so
		// each set bit votes the data booleans at its PTC position.
		for i := range e.ptcForSlot(state, att.Data.Slot) {
			byteIdx := i / 8
			bitIdx := i % 8
			if byteIdx < len(att.AggregationBits) &&
				(att.AggregationBits[byteIdx]>>bitIdx)&1 != 0 {
				e.setPTCVote(att.Data.BeaconBlockRoot, i, att.Data.PayloadPresent, att.Data.BlobDataAvailable)
			}
		}
	}
}

// OnPayloadAttestationMessage applies a wire PTC message, per
// on_payload_attestation_message. The state must be the block state of
// data.BeaconBlockRoot. Wire messages must be for the current slot and are
// signature-verified; block-included messages skip both.
func (e *EPBSForkChoice) OnPayloadAttestationMessage(
	state *BeaconStateV2,
	msg *PayloadAttestationMessage,
	isFromBlock bool,
	forkVersion [4]byte,
	genesisRoot [32]byte,
) error {
	if state == nil || msg == nil {
		return ErrEPBSNilState
	}
	// PTC votes can only change the vote for their assigned beacon block.
	if msg.Data.Slot != state.Slot {
		return ErrEPBSAttestationSlot
	}
	// Check that the attester is from the PTC.
	ptcIndex := -1
	for i, m := range e.ptcForSlot(state, msg.Data.Slot) {
		if m == msg.ValidatorIndex {
			ptcIndex = i
			break
		}
	}
	if ptcIndex < 0 {
		return ErrEPBSInactiveBuilder // not a PTC member
	}

	if !isFromBlock {
		// Wire messages must be for the current slot.
		if e.currentSlot != nil && msg.Data.Slot != e.currentSlot() {
			return ErrEPBSBidSlotMismatch
		}
		// Verify the signature.
		indexed := &IndexedPayloadAttestation{
			AttestingIndices: []uint64{msg.ValidatorIndex},
			Data:             msg.Data,
			Signature:        msg.Signature,
		}
		if !VerifyIndexedPayloadAttestation(state, indexed, forkVersion, genesisRoot) {
			return ErrEPBSAttestationSig
		}
	}

	e.setPTCVote(msg.Data.BeaconBlockRoot, ptcIndex, msg.Data.PayloadPresent, msg.Data.BlobDataAvailable)
	return nil
}

// setPTCVote sets the vote bits at the given PTC index.
func (e *EPBSForkChoice) setPTCVote(root types.Hash, ptcIndex int, payloadPresent, blobDataAvailable bool) {
	if ptcIndex < 0 || ptcIndex >= PTCSize {
		return
	}
	timeliness, ok := e.timelinessVotes[root]
	if !ok {
		timeliness = &ptcVoteVector{}
		e.timelinessVotes[root] = timeliness
		e.availabilityVo[root] = &ptcVoteVector{}
	}
	timeliness[ptcIndex] = payloadPresent
	e.availabilityVo[root][ptcIndex] = blobDataAvailable
}

// ptcForSlot resolves the PTC for a slot via the installed resolver.
func (e *EPBSForkChoice) ptcForSlot(state *BeaconStateV2, slot uint64) []uint64 {
	e.mu.RLock()
	resolver := e.ptcResolver
	e.mu.RUnlock()
	if resolver != nil {
		return resolver(slot)
	}
	return GetPTC(state, Slot(slot))
}

// IsPayloadTimely reports whether the PTC voted the payload present above
// the threshold and the payload is locally available, per is_payload_timely.
func (e *EPBSForkChoice) IsPayloadTimely(root types.Hash) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	votes, ok := e.timelinessVotes[root]
	if !ok || !e.payloadStates[root] {
		return false
	}
	return e.sumVotes(votes) > PayloadTimelyThreshold
}

// IsPayloadDataAvailable reports whether the PTC voted the blob data
// available above the threshold and the payload is locally available, per
// is_payload_data_available.
func (e *EPBSForkChoice) IsPayloadDataAvailable(root types.Hash) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	votes, ok := e.availabilityVo[root]
	if !ok || !e.payloadStates[root] {
		return false
	}
	return e.sumVotes(votes) > DataAvailabilityTimelyThreshold
}

// sumVotes counts true bits. Caller must hold the lock.
func (e *EPBSForkChoice) sumVotes(votes *ptcVoteVector) int {
	count := 0
	for _, v := range votes {
		if v {
			count++
		}
	}
	return count
}

// GetParentPayloadStatus classifies the payload status of a block's parent,
// per get_parent_payload_status: FULL when the child's bid commits to the
// parent's bid block hash, EMPTY otherwise.
func (e *EPBSForkChoice) GetParentPayloadStatus(childRoot types.Hash) uint8 {
	e.mu.RLock()
	defer e.mu.RUnlock()
	childBidParent, ok := e.bidParentHash[childRoot]
	if !ok {
		return PayloadStatusPending
	}
	parentRoot := e.parentRootOf(childRoot)
	if parentRoot == (types.Hash{}) {
		return PayloadStatusPending
	}
	parentBidBlock, ok := e.bidBlockHash[parentRoot]
	if !ok {
		return PayloadStatusPending
	}
	if childBidParent == parentBidBlock {
		return PayloadStatusFull
	}
	return PayloadStatusEmpty
}

// parentRootOf looks up the proto-array parent of a block root.
// Caller must hold the lock.
func (e *EPBSForkChoice) parentRootOf(root types.Hash) types.Hash {
	idx, ok := e.fc.protoArray.indexMap[root]
	if !ok {
		return types.Hash{}
	}
	node := e.fc.nodeAt(idx)
	if node == nil || node.Parent == invalidIndex {
		return types.Hash{}
	}
	parent := e.fc.nodeAt(node.Parent)
	if parent == nil {
		return types.Hash{}
	}
	return parent.Root
}

// IsParentNodeFull reports whether a block builds on a full parent payload.
func (e *EPBSForkChoice) IsParentNodeFull(childRoot types.Hash) bool {
	return e.GetParentPayloadStatus(childRoot) == PayloadStatusFull
}

// ShouldExtendPayload decides whether an available payload from the
// previous slot should be extended, per should_extend_payload.
func (e *EPBSForkChoice) ShouldExtendPayload(root types.Hash) bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	boost := e.proposerBoostRoot
	if e.IsPayloadTimely(root) && e.IsPayloadDataAvailable(root) {
		return true
	}
	if boost == (types.Hash{}) {
		return true
	}
	boostParent := e.parentRootOf(boost)
	if boostParent != root {
		return true
	}
	boostBidParent, ok := e.bidParentHash[boost]
	if ok && e.bidBlockHash[root] != (types.Hash{}) && boostBidParent == e.bidBlockHash[root] {
		return true
	}
	return false
}

// PayloadStatusTiebreaker returns the ordering key for a payload-status
// decision, per get_payload_status_tiebreaker.
func (e *EPBSForkChoice) PayloadStatusTiebreaker(root types.Hash, slot uint64) uint8 {
	status := e.GetParentPayloadStatusOfRoot(root)
	if status == PayloadStatusPending {
		return status
	}
	if e.currentSlot != nil && slot+1 != e.currentSlot() {
		return status
	}
	if status == PayloadStatusEmpty {
		return 1
	}
	if e.ShouldExtendPayload(root) {
		return 2
	}
	return 0
}

// GetParentPayloadStatusOfRoot classifies a root's own payload status by
// how its children commit to it, per the ForkChoiceNode model: a root is
// FULL when a locally revealed payload exists and the PTC vote is timely,
// EMPTY when the slot elapsed without a payload, PENDING otherwise.
// Caller-facing helper used by head selection.
func (e *EPBSForkChoice) GetParentPayloadStatusOfRoot(root types.Hash) uint8 {
	e.mu.RLock()
	_, known := e.timelinessVotes[root]
	_, revealed := e.payloadStates[root]
	slot := e.bidSlot[root]
	e.mu.RUnlock()

	if !known {
		return PayloadStatusPending
	}
	if revealed && e.IsPayloadTimely(root) {
		return PayloadStatusFull
	}
	if e.currentSlot != nil && e.currentSlot() > slot {
		return PayloadStatusEmpty
	}
	return PayloadStatusPending
}

// HeadChildFilter reports whether a child block is eligible from a parent
// with the given payload status, per get_node_children: children must have
// been built on the parent's committed payload (FULL) or on the empty
// parent (EMPTY); PENDING parents accept any child.
func (e *EPBSForkChoice) HeadChildFilter(parentRoot, childRoot types.Hash) bool {
	parentStatus := e.GetParentPayloadStatusOfRoot(parentRoot)
	if parentStatus == PayloadStatusPending {
		return true
	}
	return e.GetParentPayloadStatus(childRoot) == parentStatus
}
