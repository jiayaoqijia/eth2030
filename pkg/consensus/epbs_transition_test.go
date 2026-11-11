package consensus

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// epbsTestState builds a state with n active validators at 32 ETH effective
// balance, so balance-weighted PTC selection accepts every candidate.
func epbsTestState(t *testing.T, n int) *BeaconStateV2 {
	t.Helper()
	state := NewBeaconStateV2(32)
	for i := 0; i < n; i++ {
		v := &ValidatorV2{
			EffectiveBalance: MaxEffectiveBalanceV2,
			ActivationEpoch:  0,
			ExitEpoch:        FarFutureEpoch,
		}
		state.Validators = append(state.Validators, v)
		state.Balances = append(state.Balances, MaxEffectiveBalanceV2)
	}
	return state
}

// epbsTestBuilder registers a builder backed by a deterministic BLS key and
// returns the builder index, the secret, and the pubkey.
func epbsTestBuilder(t *testing.T, state *BeaconStateV2, balance uint64) (uint64, *big.Int, [48]byte) {
	t.Helper()
	epbsBuilderKeySeq++
	secret := big.NewInt(int64(1000 + epbsBuilderKeySeq))
	pubkey := crypto.BLSPubkeyFromSecret(secret)
	builder := &Builder{
		Pubkey:            pubkey,
		ExecutionAddress:  [20]byte{0x01, 0x02},
		Balance:           balance,
		DepositEpoch:      0,
		WithdrawableEpoch: FarFutureEpoch,
	}
	state.mu.Lock()
	index := state.AddBuilder(builder)
	state.mu.Unlock()
	return index, secret, pubkey
}

// epbsBuilderKeySeq gives each test builder a distinct BLS secret.
var epbsBuilderKeySeq int

func TestGetPTCDeterministicAndSized(t *testing.T) {
	state := epbsTestState(t, 16384) // 4 committees per slot: 512 candidates
	ptc1 := GetPTC(state, 7)
	ptc2 := GetPTC(state, 7)
	if len(ptc1) != PTCSize {
		t.Fatalf("expected PTC size %d, got %d", PTCSize, len(ptc1))
	}
	for i := range ptc1 {
		if ptc1[i] != ptc2[i] {
			t.Fatal("GetPTC not deterministic for the same slot")
		}
		if ptc1[i] >= 16384 {
			t.Fatalf("PTC member %d out of validator range", ptc1[i])
		}
	}
	// With at least PTCSize candidates (512 slot-committee members at 32 ETH
	// each), the balance-weighted traversal must yield distinct members.
	seen := map[uint64]bool{}
	for _, m := range ptc1 {
		seen[m] = true
	}
	if len(seen) != PTCSize {
		t.Fatalf("expected %d distinct PTC members, got %d", PTCSize, len(seen))
	}
}

func TestProcessExecutionPayloadBidSelfBuild(t *testing.T) {
	state := epbsTestState(t, 128)
	state.Slot = 10

	block := &BeaconBlockV2{
		Slot:       10,
		ParentRoot: testParentRoot(),
		Body: &BeaconBlockBody{
			SignedExecutionPayloadBid: &SignedExecutionPayloadBid{
				Message: ExecutionPayloadBid{
					BuilderIndex: BuilderIndexSelfBuild,
					Slot:         10,
					ParentBlockRoot: testParentRoot(),
				},
			},
		},
	}
	// Self-build with a non-infinity signature must be rejected.
	block.Body.SignedExecutionPayloadBid.Signature = [96]byte{0x01}
	if err := ProcessExecutionPayloadBid(state, block, [4]byte{}, [32]byte{}, 9); err != ErrEPBSSelfBuildSig {
		t.Fatalf("expected ErrEPBSSelfBuildSig, got %v", err)
	}

	// Correct self-build: infinity signature, zero value.
	block.Body.SignedExecutionPayloadBid.Signature = G2PointAtInfinity
	if err := ProcessExecutionPayloadBid(state, block, [4]byte{}, [32]byte{}, 9); err != nil {
		t.Fatalf("self-build bid rejected: %v", err)
	}
	if state.LatestExecutionPayloadBid == nil ||
		state.LatestExecutionPayloadBid.BuilderIndex != BuilderIndexSelfBuild {
		t.Fatal("self-build bid not cached")
	}
}

func TestProcessExecutionPayloadBidBuilderFlow(t *testing.T) {
	state := epbsTestState(t, 128)
	state.Slot = 10

	builderIdx, secret, _ := epbsTestBuilder(t, state, 5_000_000_000)

	bid := ExecutionPayloadBid{
		ParentBlockHash: types.Hash{0x11},
		ParentBlockRoot: testParentRoot(),
		BlockHash:       types.Hash{0x22},
		PrevRandao:      types.Hash{0x33},
		FeeRecipient:    types.Address{0x44},
		GasLimit:        30_000_000,
		BuilderIndex:    builderIdx,
		Slot:            10,
		Value:           1_000_000_000,
	}
	signed := &SignedExecutionPayloadBid{Message: bid}
	root := signed.Message.HashTreeRoot()
	signingRoot := ComputeSigningRoot(root, DomainSeparation(DomainBeaconBuilder, [4]byte{}, [32]byte{}))
	signed.Signature = crypto.BLSSign(secret, signingRoot[:])

	block := &BeaconBlockV2{
		Slot:       10,
		ParentRoot: testParentRoot(),
		Body:       &BeaconBlockBody{SignedExecutionPayloadBid: signed},
	}
	state.LatestBlockHash = types.Hash{0x11}
	state.RandaoMixes[0] = types.Hash{0x33}

	if err := ProcessExecutionPayloadBid(state, block, [4]byte{}, [32]byte{}, 9); err != nil {
		t.Fatalf("builder bid rejected: %v", err)
	}

	// The pending payment is recorded in the current-epoch window half.
	payment := state.BuilderPendingPayments[paymentWindowIndex(state, 10, true)]
	if payment.Withdrawal.Amount != 1_000_000_000 || payment.Withdrawal.BuilderIndex != builderIdx {
		t.Fatalf("pending payment not recorded: %+v", payment)
	}
	if payment.Weight != 0 {
		t.Fatalf("pending payment weight must start at zero, got %d", payment.Weight)
	}

	// A bid the builder cannot cover must be rejected.
	expensive := signed
	expensive.Message.Value = 6_000_000_000
	if err := ProcessExecutionPayloadBid(state, block, [4]byte{}, [32]byte{}, 9); err != ErrEPBSInsufficientFunds {
		t.Fatalf("expected ErrEPBSInsufficientFunds, got %v", err)
	}

	// A bid for the wrong slot must be rejected (re-signed so the
	// signature check passes and the slot check is what fires).
	wrongSlot := &SignedExecutionPayloadBid{Message: bid}
	wrongSlot.Message.Slot = 11
	wrongRoot := wrongSlot.Message.HashTreeRoot()
	wrongSigningRoot := ComputeSigningRoot(wrongRoot, DomainSeparation(DomainBeaconBuilder, [4]byte{}, [32]byte{}))
	wrongSlot.Signature = crypto.BLSSign(secret, wrongSigningRoot[:])
	block.Body.SignedExecutionPayloadBid = wrongSlot
	if err := ProcessExecutionPayloadBid(state, block, [4]byte{}, [32]byte{}, 9); err != ErrEPBSBidSlotMismatch {
		t.Fatalf("expected ErrEPBSBidSlotMismatch, got %v", err)
	}
}

func TestProcessPayloadAttestationValidation(t *testing.T) {
	state := epbsTestState(t, 512)
	state.Slot = 20
	state.LatestBlockHeader = BeaconBlockHeaderV2{
		Slot:       19,
		ParentRoot: types.Hash{0xAA},
	}

	att := &PayloadAttestation{
		Data: PayloadAttestationData{
			BeaconBlockRoot:   types.Hash{0xBB}, // wrong root
			Slot:              19,
			PayloadPresent:    true,
			BlobDataAvailable: true,
		},
	}
	if err := ProcessPayloadAttestation(state, att, [4]byte{}, [32]byte{}); err != ErrEPBSAttestationRoot {
		t.Fatalf("expected ErrEPBSAttestationRoot, got %v", err)
	}

	att.Data.BeaconBlockRoot = types.Hash{0xAA}
	att.Data.Slot = 17 // not the previous slot
	if err := ProcessPayloadAttestation(state, att, [4]byte{}, [32]byte{}); err != ErrEPBSAttestationSlot {
		t.Fatalf("expected ErrEPBSAttestationSlot, got %v", err)
	}
}

func TestProcessBuilderPendingPaymentsQuorumAndSlide(t *testing.T) {
	state := epbsTestState(t, 512)
	// Quorum = per-slot active balance * 6 / 10. With 512 validators at
	// 32 ETH: 512*32e9/32*6/10 = 3.072e10.
	builderIdx, _, _ := epbsTestBuilder(t, state, 10_000_000_000)

	// Weight below quorum: dropped at the boundary.
	state.BuilderPendingPayments[3] = BuilderPendingPayment{
		Weight: 1,
		Withdrawal: BuilderPendingWithdrawal{
			Amount: 1_000_000, BuilderIndex: builderIdx,
		},
	}
	// Weight at quorum: queued.
	quorum := state.GetBuilderPaymentQuorumThreshold()
	state.BuilderPendingPayments[5] = BuilderPendingPayment{
		Weight: quorum,
		Withdrawal: BuilderPendingWithdrawal{
			Amount: 2_000_000, BuilderIndex: builderIdx,
		},
	}

	ProcessBuilderPendingPayments(state)

	if len(state.BuilderPendingWithdrawals) != 1 ||
		state.BuilderPendingWithdrawals[0].Amount != 2_000_000 {
		t.Fatalf("expected one quorum payment queued, got %+v", state.BuilderPendingWithdrawals)
	}
	// The previous-epoch half (first SLOTS_PER_EPOCH slots) was flushed and
	// reset; the current-epoch half slid into its place.
	if got := state.BuilderPendingPayments[3]; got.Weight != 0 {
		t.Fatalf("sub-quorum payment should be dropped, got %+v", got)
	}
	if got := state.BuilderPendingPayments[5]; got.Withdrawal.Amount != 0 {
		t.Fatalf("flushed payment slot should be reset, got %+v", got)
	}
	for i := 0; i < 32; i++ {
		if state.BuilderPendingPayments[32+i].Withdrawal.Amount != 0 {
			t.Fatalf("new window half should be empty at slot %d", i)
		}
	}
}

func TestProcessExecutionPayloadEnvelope(t *testing.T) {
	state := epbsTestState(t, 128)
	state.Slot = 10

	bid := &ExecutionPayloadBid{
		BuilderIndex: BuilderIndexSelfBuild,
		Slot:         10,
		BlockHash:    types.Hash{0x22},
		PrevRandao:   types.Hash{0x33},
		GasLimit:     30_000_000,
	}
	state.LatestExecutionPayloadBid = bid
	state.LatestBlockHash = types.Hash{0x11}
	state.BuilderPendingPayments[paymentWindowIndex(state, 10, true)] = BuilderPendingPayment{
		Withdrawal: BuilderPendingWithdrawal{Amount: 500, BuilderIndex: BuilderIndexSelfBuild},
	}

	// Mismatched block hash must be rejected.
	envelope := &SignedExecutionPayloadEnvelope{
		Message: ExecutionPayloadEnvelope{
			BuilderIndex:    BuilderIndexSelfBuild,
			BeaconBlockRoot: types.Hash{0xCC},
			Slot:            10,
			BlockHash:       types.Hash{0x99},
			ParentHash:      types.Hash{0x11},
			GasLimit:        30_000_000,
			PrevRandao:      types.Hash{0x33},
		},
	}
	if err := ProcessExecutionPayload(state, envelope, 0, false, [4]byte{}, [32]byte{}); err != ErrEPBSEnvelopeBlockRoot {
		t.Fatalf("expected ErrEPBSEnvelopeBlockRoot, got %v", err)
	}

	// Consistent envelope: payment queued, availability marked.
	envelope.Message.BlockHash = types.Hash{0x22}
	envelope.Message.BeaconBlockRoot = HashBeaconBlockHeader(&BeaconBlockHeader{
		Slot:      state.LatestBlockHeader.Slot,
		StateRoot: state.LatestBlockHeader.StateRoot,
		BodyRoot:  state.LatestBlockHeader.BodyRoot,
		ParentRoot: state.LatestBlockHeader.ParentRoot,
		ProposerIndex: state.LatestBlockHeader.ProposerIndex,
	})
	if err := ProcessExecutionPayload(state, envelope, 0, false, [4]byte{}, [32]byte{}); err != nil {
		t.Fatalf("envelope rejected: %v", err)
	}
	if !state.ExecutionPayloadAvailability[state.Slot%SlotsPerHistoricalRoot] {
		t.Fatal("payload availability not marked for the current slot")
	}
	// From the next slot's perspective the parent block is full.
	state.Slot = 11
	if !state.IsParentBlockFull() {
		t.Fatal("parent block should be full after payload processing")
	}
	if state.LatestBlockHash != (types.Hash{0x22}) {
		t.Fatal("latest block hash not updated")
	}
	if len(state.BuilderPendingWithdrawals) != 1 || state.BuilderPendingWithdrawals[0].Amount != 500 {
		t.Fatal("builder payment not queued on payload processing")
	}
	if got := state.BuilderPendingPayments[paymentWindowIndex(state, 10, true)]; got.Withdrawal.Amount != 0 {
		t.Fatal("pending payment not cleared after payload processing")
	}
}

func TestBuilderIndexFlagRoundTrip(t *testing.T) {
	const idx = 42
	v := ConvertBuilderIndexToValidatorIndex(idx)
	if !IsBuilderIndex(v) {
		t.Fatal("builder index flag not detected")
	}
	if got := ConvertValidatorIndexToBuilderIndex(v); got != idx {
		t.Fatalf("round trip mismatch: %d", got)
	}
	if IsBuilderIndex(uint64(idx)) {
		t.Fatal("plain validator index misclassified as builder")
	}
}

func TestBuildersSweepWithdrawals(t *testing.T) {
	state := epbsTestState(t, 64)
	// Builder 0: withdrawable with balance. Builder 1: not withdrawable.
	b0, _, _ := epbsTestBuilder(t, state, 3_000_000_000)
	b1, _, _ := epbsTestBuilder(t, state, 4_000_000_000)
	state.Builders[b0].WithdrawableEpoch = 0
	state.Builders[b1].WithdrawableEpoch = FarFutureEpoch
	state.NextWithdrawalBuilderIndex = b0

	withdrawals, nextIndex, processed := GetBuildersSweepWithdrawals(state, 100, 0)
	if len(withdrawals) != 1 || withdrawals[0].Amount != 3_000_000_000 {
		t.Fatalf("expected only the withdrawable builder swept, got %+v", withdrawals)
	}
	if withdrawals[0].ValidatorIndex != ConvertBuilderIndexToValidatorIndex(b0) {
		t.Fatal("sweep withdrawal must use the builder index space")
	}
	if nextIndex != 101 || processed != 2 {
		t.Fatalf("unexpected sweep cursor: next=%d processed=%d", nextIndex, processed)
	}

	// Applying the withdrawal drains the builder balance.
	state.ApplyExpectedWithdrawalToBuilders(withdrawals[0])
	if state.Builders[b0].Balance != 0 {
		t.Fatalf("builder balance not drained: %d", state.Builders[b0].Balance)
	}
	state.UpdateNextWithdrawalBuilderIndex(processed)
	if state.NextWithdrawalBuilderIndex != b0 {
		t.Fatalf("sweep cursor not wrapped: %d", state.NextWithdrawalBuilderIndex)
	}
}

func TestRemoveBuilderPaymentForSlashedProposal(t *testing.T) {
	state := epbsTestState(t, 64)
	state.Slot = 64 // epoch 2
	state.BuilderPendingPayments[paymentWindowIndex(state, 63, false)] = BuilderPendingPayment{
		Withdrawal: BuilderPendingWithdrawal{Amount: 777},
	}
	RemoveBuilderPaymentForSlashedProposal(state, 63)
	if got := state.BuilderPendingPayments[paymentWindowIndex(state, 63, false)]; got.Withdrawal.Amount != 0 {
		t.Fatalf("slashed proposal payment not cleared: %+v", got)
	}
}

func TestAddBuilderPaymentWeight(t *testing.T) {
	state := epbsTestState(t, 64)
	state.BuilderPendingPayments[paymentWindowIndex(state, 5, true)] = BuilderPendingPayment{
		Withdrawal: BuilderPendingWithdrawal{Amount: 100, BuilderIndex: 0},
	}
	AddBuilderPaymentWeight(state, 5, true, 1)
	if got := state.BuilderPendingPayments[paymentWindowIndex(state, 5, true)]; got.Weight != MaxEffectiveBalanceV2 {
		t.Fatalf("weight not accumulated: %d", got.Weight)
	}
}
