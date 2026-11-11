package consensus

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// forkChoicePTCTestSetup builds a state, a wrapped fork choice, and blocks
// parent -> child with opposing payload commitments (childA builds on the
// parent's payload; childB commits to a different parent block hash).
func forkChoicePTCTestSetup(t *testing.T) (*BeaconStateV2, *ForkChoiceV2, *EPBSForkChoice, types.Hash, types.Hash, types.Hash) {
	t.Helper()
	state := epbsTestState(t, 512)
	fc := NewForkChoiceV2(Checkpoint{}, Checkpoint{})
	efc := NewEPBSForkChoice(fc)
	efc.SetPTCResolver(func(slot uint64) []uint64 { return GetPTC(state, Slot(slot)) })
	efc.SetCurrentSlotFn(func() uint64 { return state.Slot })

	parentRoot := types.Hash{0x01}
	if err := fc.OnBlock(10, parentRoot, types.Hash{}, 0, 0); err != nil {
		t.Fatalf("OnBlock(parent): %v", err)
	}
	// Bid registered; votes initialize to false and the payload is not yet
	// revealed, so the parent stays PENDING until tests mark it.
	efc.OnBlockWithBid(parentRoot, &ExecutionPayloadBid{
		Slot:      10,
		BlockHash: types.Hash{0xAA},
	})

	childARoot := types.Hash{0x02} // builds on parent's payload (FULL)
	childBRoot := types.Hash{0x03} // builds on empty parent (EMPTY)
	if err := fc.OnBlock(11, childARoot, parentRoot, 0, 0); err != nil {
		t.Fatalf("OnBlock(childA): %v", err)
	}
	if err := fc.OnBlock(11, childBRoot, parentRoot, 0, 0); err != nil {
		t.Fatalf("OnBlock(childB): %v", err)
	}
	efc.OnBlockWithBid(childARoot, &ExecutionPayloadBid{
		Slot:            11,
		ParentBlockHash: types.Hash{0xAA}, // matches parent bid block hash
	})
	efc.OnBlockWithBid(childBRoot, &ExecutionPayloadBid{
		Slot:            11,
		ParentBlockHash: types.Hash{0xBB}, // diverges from parent payload
	})
	return state, fc, efc, parentRoot, childARoot, childBRoot
}

func TestEPBSForkChoiceParentPayloadStatus(t *testing.T) {
	_, _, efc, _, childA, childB := forkChoicePTCTestSetup(t)

	if status := efc.GetParentPayloadStatus(childA); status != PayloadStatusFull {
		t.Fatalf("childA should classify the parent FULL, got %d", status)
	}
	if status := efc.GetParentPayloadStatus(childB); status != PayloadStatusEmpty {
		t.Fatalf("childB should classify the parent EMPTY, got %d", status)
	}
	if !efc.IsParentNodeFull(childA) {
		t.Fatal("IsParentNodeFull(childA) should hold")
	}
}

func TestEPBSForkChoicePTCVotesAndTimeliness(t *testing.T) {
	state, _, efc, parentRoot, _, _ := forkChoicePTCTestSetup(t)
	// The containing beacon block is one slot after the attested slot.
	state.Slot = 11

	// Block-included aggregated attestation: a majority votes present.
	ptc := GetPTC(state, 10)
	if len(ptc) != PTCSize {
		t.Fatalf("PTC size %d", len(ptc))
	}
	att := &PayloadAttestation{
		Data: PayloadAttestationData{
			BeaconBlockRoot:   parentRoot,
			Slot:              10,
			PayloadPresent:    true,
			BlobDataAvailable: true,
		},
	}
	for i := 0; i < PayloadTimelyThreshold+1; i++ {
		byteIdx, bitIdx := i/8, i%8
		att.AggregationBits[byteIdx] |= 1 << bitIdx
	}
	efc.NotifyPTCAttestations(state, []*PayloadAttestation{att})

	// Not timely yet: the payload was not revealed locally.
	if efc.IsPayloadTimely(parentRoot) {
		t.Fatal("timeliness must require local payload availability")
	}
	efc.MarkPayloadRevealed(parentRoot)
	if !efc.IsPayloadTimely(parentRoot) {
		t.Fatal("majority PTC vote + local payload should be timely")
	}
	if !efc.IsPayloadDataAvailable(parentRoot) {
		t.Fatal("blob data availability vote should clear the threshold")
	}

	// One vote below the threshold is not timely.
	other := types.Hash{0x44}
	efc.OnBlockWithBid(other, &ExecutionPayloadBid{Slot: 10})
	efc.MarkPayloadRevealed(other)
	efc.setPTCVote(other, 0, true, true)
	if efc.IsPayloadTimely(other) {
		t.Fatal("single vote must not reach the timeliness threshold")
	}
}

func TestEPBSForkChoiceWireMessageValidation(t *testing.T) {
	state, _, efc, parentRoot, _, _ := forkChoicePTCTestSetup(t)
	state.Slot = 10 // the attested block state carries the attested slot
	ptc := GetPTC(state, 10)

	secret := big.NewInt(4242)
	pubkey := crypto.BLSPubkeyFromSecret(secret)
	// Install the PTC member key into the validator set.
	memberSlot := int(ptc[0])
	state.Validators[memberSlot].Pubkey = pubkey

	data := PayloadAttestationData{
		BeaconBlockRoot: parentRoot,
		Slot:            10,
		PayloadPresent:  true,
	}
	signingRoot := ComputeSigningRoot(data.HashTreeRoot(), DomainSeparation(DomainPTCAttester, [4]byte{}, [32]byte{}))
	msg := &PayloadAttestationMessage{
		ValidatorIndex: ptc[0],
		Data:           data,
		Signature:      crypto.BLSSign(secret, signingRoot[:]),
	}
	if err := efc.OnPayloadAttestationMessage(state, msg, false, [4]byte{}, [32]byte{}); err != nil {
		t.Fatalf("valid wire PTC message rejected: %v", err)
	}

	// A non-PTC validator index must be rejected.
	notMember := uint64(0)
	for _, m := range ptc {
		if notMember != m {
			break
		}
		notMember++
	}
	bad := &PayloadAttestationMessage{ValidatorIndex: notMember, Data: data}
	if err := efc.OnPayloadAttestationMessage(state, bad, false, [4]byte{}, [32]byte{}); err == nil {
		t.Fatal("non-PTC member message must be rejected")
	}

	// A message for the wrong slot must be rejected.
	stale := &PayloadAttestationMessage{ValidatorIndex: ptc[0], Data: data}
	stale.Data.Slot = 9
	if err := efc.OnPayloadAttestationMessage(state, stale, true, [4]byte{}, [32]byte{}); err == nil {
		t.Fatal("stale-slot message must be rejected")
	}
}

func TestEPBSForkChoiceHeadChildFilter(t *testing.T) {
	_, _, efc, parentRoot, childA, childB := forkChoicePTCTestSetup(t)

	// Parent pending: both children eligible.
	if !efc.HeadChildFilter(parentRoot, childA) || !efc.HeadChildFilter(parentRoot, childB) {
		t.Fatal("pending parent must accept both children")
	}

	// Payload revealed and PTC-timely: parent is FULL, only childA remains.
	// The resolver needs live state, so votes are recorded directly.
	efc.MarkPayloadRevealed(parentRoot)
	for i := 0; i < PayloadTimelyThreshold+1; i++ {
		efc.setPTCVote(parentRoot, i, true, true)
	}

	if status := efc.GetParentPayloadStatusOfRoot(parentRoot); status != PayloadStatusFull {
		t.Fatalf("parent should be FULL after timely reveal, got %d", status)
	}
	if !efc.HeadChildFilter(parentRoot, childA) {
		t.Fatal("FULL parent must keep the payload-building child")
	}
	if efc.HeadChildFilter(parentRoot, childB) {
		t.Fatal("FULL parent must drop the empty-building child")
	}
}
