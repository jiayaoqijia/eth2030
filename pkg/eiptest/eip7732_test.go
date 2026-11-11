package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/consensus"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/engine"
)

func TestEIP7732_PayloadAuctionLifecycle(t *testing.T) {
	s := eiptest.New(t)
	probe := s.Engine()
	builder, err := probe.API().RegisterBuilderV1(engine.SignedBuilderRegistrationV1{
		Message: engine.BuilderRegistrationV1{Pubkey: engine.BLSPubkey{1}, FeeRecipient: s.Alice().Address, GasLimit: s.Genesis().GasLimit()},
	})
	if err != nil {
		t.Fatal(err)
	}

	const slot uint64 = 1
	bid := engine.SignedExecutionPayloadBid{Message: engine.ExecutionPayloadBid{
		ParentBlockHash: s.Genesis().Hash(), BlockHash: types.Hash{1},
		Slot: slot, BuilderIndex: builder.Index, GasLimit: builder.GasLimit, FeeRecipient: builder.FeeRecipient,
	}}
	var accepted bool
	if err := probe.Call("engine_submitBuilderBidV1", &accepted, bid); err == nil {
		t.Fatal("accepted a zero-value bid")
	}
	for i, price := range []uint64{7, 12} {
		bid.Message.BlockHash = types.Hash{byte(i + 1)}
		bid.Message.Value = price
		if err := probe.Call("engine_submitBuilderBidV1", &accepted, bid); err != nil || !accepted {
			t.Fatalf("submit bid %d: accepted=%t: %v", price, accepted, err)
		}
	}
	var bids []engine.SignedExecutionPayloadBid
	if err := probe.Call("engine_getBuilderBidsV1", &bids, slot); err != nil {
		t.Fatal(err)
	}
	if len(bids) != 2 || bids[0].Message.Value != 12 || bids[1].Message.Value != 7 {
		t.Fatalf("incorrect bid ordering: %+v", bids)
	}
}

// epbsIntegrationState builds a consensus state with n validators backed by
// deterministic BLS keys and returns the state plus its secrets.
func epbsIntegrationState(t *testing.T, n int) (*consensus.BeaconStateV2, []*big.Int) {
	t.Helper()
	state := consensus.NewBeaconStateV2(32)
	secrets := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		secrets[i] = big.NewInt(int64(100 + i))
		pubkey := crypto.BLSPubkeyFromSecret(secrets[i])
		v := &consensus.ValidatorV2{
			Pubkey:           pubkey,
			EffectiveBalance: consensus.MaxEffectiveBalanceV2,
			ActivationEpoch:  0,
			ExitEpoch:        consensus.FarFutureEpoch,
		}
		state.Validators = append(state.Validators, v)
		state.Balances = append(state.Balances, consensus.MaxEffectiveBalanceV2)
	}
	return state, secrets
}

// epbsRegisterBuilder registers a funded builder on the state with a
// deterministic key and returns its index, secret, and pubkey.
func epbsRegisterBuilder(t *testing.T, state *consensus.BeaconStateV2, seq int, balance uint64) (uint64, *big.Int, [48]byte) {
	t.Helper()
	secret := big.NewInt(int64(5000 + seq))
	pubkey := crypto.BLSPubkeyFromSecret(secret)
	builder := &consensus.Builder{
		Pubkey:            pubkey,
		ExecutionAddress:  types.Address{0x77},
		Balance:           balance,
		DepositEpoch:      0,
		WithdrawableEpoch: consensus.FarFutureEpoch,
	}
	return state.AddBuilder(builder), secret, pubkey
}

// epbsSignBid signs a bid with the beacon builder domain.
func epbsSignBid(secret *big.Int, bid *consensus.ExecutionPayloadBid) [96]byte {
	root := bid.HashTreeRoot()
	signingRoot := consensus.ComputeSigningRoot(root, consensus.DomainSeparation(consensus.DomainBeaconBuilder, [4]byte{}, [32]byte{}))
	return crypto.BLSSign(secret, signingRoot[:])
}

// TestEIP7732_BuilderPaymentLifecycle walks the EIP-7732 payment state
// machine over the consensus state with real signatures: a funded builder
// bids, the bid records a pending payment, the revealed payload moves the
// payment into the withdrawal queue, and an attest-backed quorum flushes a
// second payment at the epoch boundary.
func TestEIP7732_BuilderPaymentLifecycle(t *testing.T) {
	state, _ := epbsIntegrationState(t, 128)
	state.Slot = 10

	builderIdx, secret, _ := epbsRegisterBuilder(t, state, 1, 10_000_000_000)

	// The block commits to the builder's bid.
	bid := &consensus.ExecutionPayloadBid{
		ParentBlockHash: types.Hash{0x11},
		ParentBlockRoot: types.Hash{0x12},
		BlockHash:       types.Hash{0x22},
		PrevRandao:      types.Hash{0x33},
		FeeRecipient:    types.Address{0x44},
		GasLimit:        30_000_000,
		BuilderIndex:    builderIdx,
		Slot:            10,
		Value:           1_000_000_000,
	}
	signed := &consensus.SignedExecutionPayloadBid{Message: *bid, Signature: epbsSignBid(secret, bid)}
	block := &consensus.BeaconBlockV2{
		Slot:       10,
		ParentRoot: types.Hash{0x12},
		Body:       &consensus.BeaconBlockBody{SignedExecutionPayloadBid: signed},
	}
	state.LatestBlockHash = types.Hash{0x11}
	state.RandaoMixes[0] = types.Hash{0x33}

	if err := consensus.ProcessExecutionPayloadBid(state, block, [4]byte{}, [32]byte{}, 9); err != nil {
		t.Fatalf("bid rejected: %v", err)
	}
	payment := state.BuilderPendingPayments[32+10%32] // current-epoch half, slot%32
	if payment.Withdrawal.Amount != 1_000_000_000 || payment.Withdrawal.BuilderIndex != builderIdx {
		t.Fatalf("pending payment not recorded: %+v", payment)
	}

	// The builder reveals a consistent payload envelope; the payment moves
	// to the withdrawal queue. The envelope commits to the latest header
	// root with the cached state root, exactly as the transition computes it.
	expectedHeaderRoot := consensus.HashBeaconBlockHeader(&consensus.BeaconBlockHeader{
		Slot:          state.LatestBlockHeader.Slot,
		ProposerIndex: state.LatestBlockHeader.ProposerIndex,
		ParentRoot:    state.LatestBlockHeader.ParentRoot,
		StateRoot:     state.HashTreeRootV2(),
		BodyRoot:      state.LatestBlockHeader.BodyRoot,
	})
	envelope := &consensus.SignedExecutionPayloadEnvelope{
		Message: consensus.ExecutionPayloadEnvelope{
			BuilderIndex:    builderIdx,
			BeaconBlockRoot: expectedHeaderRoot,
			Slot:            10,
			BlockHash:       types.Hash{0x22},
			ParentHash:      types.Hash{0x11},
			PrevRandao:      types.Hash{0x33},
			GasLimit:        30_000_000,
		},
	}
	if err := consensus.ProcessExecutionPayload(state, envelope, 0, false, [4]byte{}, [32]byte{}); err != nil {
		t.Fatalf("envelope rejected: %v", err)
	}
	if len(state.BuilderPendingWithdrawals) != 1 || state.BuilderPendingWithdrawals[0].Amount != 1_000_000_000 {
		t.Fatalf("builder payment not queued for the execution layer: %+v", state.BuilderPendingWithdrawals)
	}
	if state.BuilderPendingPayments[32+10%32].Withdrawal.Amount != 0 {
		t.Fatal("pending payment not cleared after the payload was processed")
	}

	// A second payment reaches the builder-payment quorum through PTC
	// attestation weight and is flushed at the epoch boundary.
	bid2 := &consensus.ExecutionPayloadBid{
		ParentBlockHash: types.Hash{0x22},
		ParentBlockRoot: types.Hash{0x23},
		BlockHash:       types.Hash{0x25},
		PrevRandao:      types.Hash{0x33},
		GasLimit:        30_000_000,
		BuilderIndex:    builderIdx,
		Slot:            42,
		Value:           2_000_000_000,
	}
	state.Slot = 42
	state.LatestBlockHash = types.Hash{0x22}
	state.RandaoMixes[1] = types.Hash{0x33} // epoch 1 randao mix
	signed2 := &consensus.SignedExecutionPayloadBid{Message: *bid2, Signature: epbsSignBid(secret, bid2)}
	block2 := &consensus.BeaconBlockV2{Slot: 42, ParentRoot: types.Hash{0x23}, Body: &consensus.BeaconBlockBody{SignedExecutionPayloadBid: signed2}}
	if err := consensus.ProcessExecutionPayloadBid(state, block2, [4]byte{}, [32]byte{}, 9); err != nil {
		t.Fatalf("second bid rejected: %v", err)
	}
	quorum := state.GetBuilderPaymentQuorumThreshold()
	state.Validators[3].EffectiveBalance = quorum
	consensus.AddBuilderPaymentWeight(state, 42, true, 3)
	// A payment recorded in epoch E sits in the window's current-epoch half;
	// the first boundary slides it into the previous-epoch half, the second
	// flushes it if the quorum weight arrived.
	consensus.ProcessBuilderPendingPayments(state)
	consensus.AddBuilderPaymentWeight(state, 42, false, 3)
	consensus.ProcessBuilderPendingPayments(state)
	if len(state.BuilderPendingWithdrawals) != 2 || state.BuilderPendingWithdrawals[1].Amount != 2_000_000_000 {
		t.Fatalf("quorum payment not flushed at the epoch boundary: %+v", state.BuilderPendingWithdrawals)
	}
}

// TestEIP7732_PTCAttestationBlockFlow produces a beacon block carrying
// aggregated PTC payload attestations, validates them through the state
// transition, and feeds them into the fork choice: a majority payload-
// present vote plus the revealed payload makes the block timely.
func TestEIP7732_PTCAttestationBlockFlow(t *testing.T) {
	// 16384 active validators give one 512-member committee per slot, so
	// the PTC has 512 distinct members and the aggregate signature has no
	// duplicate public keys.
	state, secrets := epbsIntegrationState(t, 16384)
	state.Slot = 20
	state.LatestBlockHeader = consensus.BeaconBlockHeaderV2{Slot: 19, ParentRoot: types.Hash{0xAA}}

	// The PTC for slot 19 attests to the parent block with the payload present.
	ptc := consensus.GetPTC(state, 19)
	if len(ptc) != consensus.PTCSize {
		t.Fatalf("PTC size = %d", len(ptc))
	}
	data := consensus.PayloadAttestationData{
		BeaconBlockRoot: types.Hash{0xAA},
		Slot:            19,
		PayloadPresent:  true,
	}
	dataRoot := data.HashTreeRoot()
	signingRoot := consensus.ComputeSigningRoot(dataRoot, consensus.DomainSeparation(consensus.DomainPTCAttester, [4]byte{}, [32]byte{}))

	att := &consensus.PayloadAttestation{Data: data}
	for i := 0; i < consensus.PayloadTimelyThreshold+1; i++ {
		att.AggregationBits[i/8] |= 1 << (i % 8)
	}
	// Aggregate one signature per set PTC position, in PTC order.
	sigs := make([][96]byte, 0, consensus.PayloadTimelyThreshold+1)
	for i := 0; i < consensus.PayloadTimelyThreshold+1; i++ {
		sigs = append(sigs, crypto.BLSSign(secrets[ptc[i]], signingRoot[:]))
	}
	att.Signature = crypto.AggregateSignatures(sigs)

	// The state transition validates the attestation.
	if err := consensus.ProcessPayloadAttestation(state, att, [4]byte{}, [32]byte{}); err != nil {
		t.Fatalf("aggregated PTC attestation rejected: %v", err)
	}

	// The block producer includes it (cap MAX_PAYLOAD_ATTESTATIONS).
	pools := &consensus.OperationPools{PayloadAttestations: []*consensus.PayloadAttestation{att, att, att, att, att}}
	producer := consensus.NewBlockProducer(nil, pools)
	block, err := producer.ProduceBlock(20, 0, types.Hash{0x50}, types.Hash{0x51}, [96]byte{0x60})
	if err != nil {
		t.Fatalf("ProduceBlock: %v", err)
	}
	if len(block.Body.PayloadAttestations) != consensus.MaxPayloadAttestations {
		t.Fatalf("block carries %d payload attestations, want %d",
			len(block.Body.PayloadAttestations), consensus.MaxPayloadAttestations)
	}

	// The fork choice records the votes; a revealed payload with a majority
	// vote is timely.
	fc := consensus.NewForkChoiceV2(consensus.Checkpoint{}, consensus.Checkpoint{})
	efc := consensus.NewEPBSForkChoice(fc)
	parentRoot := types.Hash{0xAA}
	if err := fc.OnBlock(19, parentRoot, types.Hash{}, 0, 0); err != nil {
		t.Fatalf("OnBlock: %v", err)
	}
	efc.OnBlockWithBid(parentRoot, &consensus.ExecutionPayloadBid{Slot: 19, BlockHash: types.Hash{0x77}})
	efc.NotifyPTCAttestations(state, []*consensus.PayloadAttestation{att})
	if efc.IsPayloadTimely(parentRoot) {
		t.Fatal("timeliness must require the payload to be revealed locally")
	}
	efc.MarkPayloadRevealed(parentRoot)
	if !efc.IsPayloadTimely(parentRoot) {
		t.Fatal("majority PTC vote plus revealed payload must be timely")
	}
}

// TestEIP7732_ForkChoiceHeadRespectsPayloadStatus drives head selection
// between a payload-building child and an empty-building child: once the
// parent's payload is revealed and PTC-timely, only the child committed to
// that payload remains eligible.
func TestEIP7732_ForkChoiceHeadRespectsPayloadStatus(t *testing.T) {
	state, _ := epbsIntegrationState(t, 512)
	fc := consensus.NewForkChoiceV2(consensus.Checkpoint{}, consensus.Checkpoint{})
	efc := consensus.NewEPBSForkChoice(fc)
	efc.SetCurrentSlotFn(func() uint64 { return 10 }) // the parent's slot is still current

	parentRoot := types.Hash{0x01}
	if err := fc.OnBlock(10, parentRoot, types.Hash{}, 0, 0); err != nil {
		t.Fatalf("OnBlock(parent): %v", err)
	}
	efc.OnBlockWithBid(parentRoot, &consensus.ExecutionPayloadBid{Slot: 10, BlockHash: types.Hash{0xAA}})

	// Two children produced with opposing parent payload commitments.
	childARoot := types.Hash{0x02}
	childBRoot := types.Hash{0x03}
	if err := fc.OnBlock(11, childARoot, parentRoot, 0, 0); err != nil {
		t.Fatalf("OnBlock(childA): %v", err)
	}
	if err := fc.OnBlock(11, childBRoot, parentRoot, 0, 0); err != nil {
		t.Fatalf("OnBlock(childB): %v", err)
	}
	efc.OnBlockWithBid(childARoot, &consensus.ExecutionPayloadBid{
		Slot: 11, ParentBlockHash: types.Hash{0xAA}, // builds on the revealed payload
	})
	efc.OnBlockWithBid(childBRoot, &consensus.ExecutionPayloadBid{
		Slot: 11, ParentBlockHash: types.Hash{0xBB}, // builds on the empty parent
	})

	// While the parent slot is current and the payload is unrevealed, the
	// parent is pending and both children are eligible.
	if status := efc.GetParentPayloadStatusOfRoot(parentRoot); status != consensus.PayloadStatusPending {
		t.Fatalf("unrevealed current-slot parent status = %d, want PENDING", status)
	}
	if !efc.HeadChildFilter(parentRoot, childARoot) || !efc.HeadChildFilter(parentRoot, childBRoot) {
		t.Fatal("pending parent must accept both children")
	}

	// The payload is revealed and the PTC votes present: parent FULL.
	// Votes arrive through the block-attestation path, as they would on
	// chain.
	state.Slot = 11
	att := &consensus.PayloadAttestation{
		Data: consensus.PayloadAttestationData{
			BeaconBlockRoot:   parentRoot,
			Slot:              10,
			PayloadPresent:    true,
			BlobDataAvailable: true,
		},
	}
	for i := 0; i < consensus.PayloadTimelyThreshold+1; i++ {
		att.AggregationBits[i/8] |= 1 << (i % 8)
	}
	efc.NotifyPTCAttestations(state, []*consensus.PayloadAttestation{att})
	efc.MarkPayloadRevealed(parentRoot)
	if status := efc.GetParentPayloadStatusOfRoot(parentRoot); status != consensus.PayloadStatusFull {
		t.Fatalf("parent status = %d, want FULL", status)
	}
	if !efc.HeadChildFilter(parentRoot, childARoot) {
		t.Fatal("FULL parent must keep the payload-building child")
	}
	if efc.HeadChildFilter(parentRoot, childBRoot) {
		t.Fatal("FULL parent must drop the empty-building child")
	}
	if status := efc.GetParentPayloadStatus(childBRoot); status != consensus.PayloadStatusEmpty {
		t.Fatalf("childB parent status = %d, want EMPTY", status)
	}
}
