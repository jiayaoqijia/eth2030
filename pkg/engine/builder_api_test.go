package engine

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// newBuilderTestAPI creates an EngineAPI with a mock backend for builder API tests.
func newBuilderTestAPI() *EngineAPI {
	return NewEngineAPI(&builderMockBackend{})
}

// builderMockBackend implements engine.Backend for testing builder API methods.
type builderMockBackend struct{}

func (m *builderMockBackend) ProcessBlock(payload *ExecutionPayloadV3, _ []types.Hash, _ types.Hash) (PayloadStatusV1, error) {
	return PayloadStatusV1{Status: StatusValid}, nil
}
func (m *builderMockBackend) ProcessBlockV4(payload *ExecutionPayloadV3, _ []types.Hash, _ types.Hash, _ [][]byte) (PayloadStatusV1, error) {
	return PayloadStatusV1{Status: StatusValid}, nil
}
func (m *builderMockBackend) ProcessBlockV5(payload *ExecutionPayloadV5, _ []types.Hash, _ types.Hash, _ [][]byte) (PayloadStatusV1, error) {
	return PayloadStatusV1{Status: StatusValid}, nil
}
func (m *builderMockBackend) ForkchoiceUpdated(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
	return ForkchoiceUpdatedResult{PayloadStatus: PayloadStatusV1{Status: StatusValid}}, nil
}
func (m *builderMockBackend) ForkchoiceUpdatedV4(state ForkchoiceStateV1, attrs *PayloadAttributesV4) (ForkchoiceUpdatedResult, error) {
	return ForkchoiceUpdatedResult{PayloadStatus: PayloadStatusV1{Status: StatusValid}}, nil
}
func (m *builderMockBackend) GetPayloadByID(id PayloadID) (*GetPayloadResponse, error) {
	return nil, ErrUnknownPayload
}
func (m *builderMockBackend) GetPayloadV4ByID(id PayloadID) (*GetPayloadV4Response, error) {
	return nil, ErrUnknownPayload
}
func (m *builderMockBackend) GetPayloadV6ByID(id PayloadID) (*GetPayloadV6Response, error) {
	return nil, ErrUnknownPayload
}
func (m *builderMockBackend) GetHeadTimestamp() uint64              { return 1000 }
func (m *builderMockBackend) GetBlockTimestamp(_ types.Hash) uint64 { return 0 }
func (m *builderMockBackend) IsCancun(ts uint64) bool               { return true }
func (m *builderMockBackend) IsPrague(ts uint64) bool               { return true }
func (m *builderMockBackend) IsAmsterdam(ts uint64) bool            { return true }
func (m *builderMockBackend) GetHeadHash() types.Hash               { return types.Hash{} }
func (m *builderMockBackend) GetSafeHash() types.Hash               { return types.Hash{} }
func (m *builderMockBackend) GetFinalizedHash() types.Hash          { return types.Hash{} }

// registerBuilderForAPI registers a builder on an EngineAPI and returns its index.
func registerBuilderForAPI(t *testing.T, api *EngineAPI) BuilderIndex {
	t.Helper()
	var pubkey BLSPubkey
	pubkey[0] = 0x01
	reg := &BuilderRegistrationV1{
		FeeRecipient: types.HexToAddress("0xaaaa"),
		GasLimit:     30000000,
		Timestamp:    1000,
		Pubkey:       pubkey,
	}
	builder, err := api.builderRegistry.RegisterBuilder(reg, MinBuilderStake)
	if err != nil {
		t.Fatalf("register builder: %v", err)
	}
	return builder.Index
}

// TestSubmitBuilderBidV1_API tests submitting a builder bid via the API.
func TestSubmitBuilderBidV1_API(t *testing.T) {
	api := newBuilderTestAPI()
	idx := registerBuilderForAPI(t, api)

	bid := SignedExecutionPayloadBid{
		Message: ExecutionPayloadBid{
			Slot:            10,
			BuilderIndex:    idx,
			Value:           100,
			BlockHash:       types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			ParentBlockHash: types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			GasLimit:        30000000,
			FeeRecipient:    types.HexToAddress("0xaaaa"),
		},
	}

	err := api.SubmitBuilderBidV1(bid)
	if err != nil {
		t.Fatalf("SubmitBuilderBidV1 error: %v", err)
	}
}

// TestSubmitBuilderBidV1_NilRegistry tests that submitting a bid with no
// registry returns an error.
func TestSubmitBuilderBidV1_NilRegistry(t *testing.T) {
	api := newBuilderTestAPI()
	api.builderRegistry = nil

	bid := SignedExecutionPayloadBid{}
	err := api.SubmitBuilderBidV1(bid)
	if err != ErrBuilderNotFound {
		t.Fatalf("want ErrBuilderNotFound, got %v", err)
	}
}

// TestGetBuilderBidsV1_API tests retrieving bids for a slot.
func TestGetBuilderBidsV1_API(t *testing.T) {
	api := newBuilderTestAPI()
	idx := registerBuilderForAPI(t, api)

	for i := uint64(1); i <= 2; i++ {
		bid := SignedExecutionPayloadBid{
			Message: ExecutionPayloadBid{
				Slot:            10,
				BuilderIndex:    idx,
				Value:           i * 100,
				BlockHash:       types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
				ParentBlockHash: types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
				GasLimit:        30000000,
				FeeRecipient:    types.HexToAddress("0xaaaa"),
			},
		}
		if err := api.SubmitBuilderBidV1(bid); err != nil {
			t.Fatalf("submit bid %d: %v", i, err)
		}
	}

	bids := api.GetBuilderBidsV1(10)
	if len(bids) != 2 {
		t.Fatalf("want 2 bids, got %d", len(bids))
	}
	if bids[0].Message.Value != 200 {
		t.Fatalf("want first bid value 200, got %d", bids[0].Message.Value)
	}
}

// TestGetBuilderBidsV1_EmptySlot tests retrieving bids for a slot with no bids.
func TestGetBuilderBidsV1_EmptySlot(t *testing.T) {
	api := newBuilderTestAPI()
	bids := api.GetBuilderBidsV1(999)
	if len(bids) != 0 {
		t.Fatalf("want 0 bids, got %d", len(bids))
	}
}

// TestGetBuilderBidsV1_NilRegistry tests with no builder registry.
func TestGetBuilderBidsV1_NilRegistry(t *testing.T) {
	api := newBuilderTestAPI()
	api.builderRegistry = nil
	bids := api.GetBuilderBidsV1(10)
	if bids != nil {
		t.Fatalf("want nil, got %v", bids)
	}
}

// TestGetBestBuilderBidV1_API tests retrieving the best bid for a slot.
func TestGetBestBuilderBidV1_API(t *testing.T) {
	api := newBuilderTestAPI()
	idx := registerBuilderForAPI(t, api)

	for _, v := range []uint64{50, 200, 100} {
		bid := SignedExecutionPayloadBid{
			Message: ExecutionPayloadBid{
				Slot:            10,
				BuilderIndex:    idx,
				Value:           v,
				BlockHash:       types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
				ParentBlockHash: types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
				GasLimit:        30000000,
				FeeRecipient:    types.HexToAddress("0xaaaa"),
			},
		}
		if err := api.SubmitBuilderBidV1(bid); err != nil {
			t.Fatalf("submit bid: %v", err)
		}
	}

	best, err := api.GetBestBuilderBidV1(10)
	if err != nil {
		t.Fatalf("GetBestBuilderBidV1 error: %v", err)
	}
	if best.Message.Value != 200 {
		t.Fatalf("want best bid value 200, got %d", best.Message.Value)
	}
}

// TestGetBestBuilderBidV1_NoBids tests when no bids exist for a slot.
func TestGetBestBuilderBidV1_NoBids(t *testing.T) {
	api := newBuilderTestAPI()
	_, err := api.GetBestBuilderBidV1(999)
	if err != ErrNoBidsAvailable {
		t.Fatalf("want ErrNoBidsAvailable, got %v", err)
	}
}

// TestRegisterBuilderV1_API tests registering a builder via the API.
func TestRegisterBuilderV1_API(t *testing.T) {
	api := newBuilderTestAPI()
	var pubkey BLSPubkey
	pubkey[0] = 0x02

	signed := SignedBuilderRegistrationV1{
		Message: BuilderRegistrationV1{
			FeeRecipient: types.HexToAddress("0xbbbb"),
			GasLimit:     30000000,
			Timestamp:    1000,
			Pubkey:       pubkey,
		},
	}

	builder, err := api.RegisterBuilderV1(signed)
	if err != nil {
		t.Fatalf("RegisterBuilderV1 error: %v", err)
	}
	if builder.Status != BuilderStatusActive {
		t.Fatalf("want active status, got %d", builder.Status)
	}
}

// TestRegisterBuilderV1_NilRegistry tests with no builder registry.
func TestRegisterBuilderV1_NilRegistry(t *testing.T) {
	api := newBuilderTestAPI()
	api.builderRegistry = nil

	signed := SignedBuilderRegistrationV1{}
	_, err := api.RegisterBuilderV1(signed)
	if err != ErrBuilderNotFound {
		t.Fatalf("want ErrBuilderNotFound, got %v", err)
	}
}

// TestHandleSubmitBuilderBidV1_RPC tests the JSON-RPC handler for builder bid submission.
func TestHandleSubmitBuilderBidV1_RPC(t *testing.T) {
	api := newBuilderTestAPI()
	idx := registerBuilderForAPI(t, api)

	bid := SignedExecutionPayloadBid{
		Message: ExecutionPayloadBid{
			Slot:            10,
			BuilderIndex:    idx,
			Value:           100,
			BlockHash:       types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			ParentBlockHash: types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			GasLimit:        30000000,
			FeeRecipient:    types.HexToAddress("0xaaaa"),
		},
	}

	bidJSON, _ := json.Marshal(bid)
	params := []json.RawMessage{bidJSON}
	result, rpcErr := api.handleSubmitBuilderBidV1(params)
	if rpcErr != nil {
		t.Fatalf("handleSubmitBuilderBidV1 error: code=%d msg=%s", rpcErr.Code, rpcErr.Message)
	}
	if result != true {
		t.Fatalf("want true, got %v", result)
	}
}

// TestHandleSubmitBuilderBidV1_WrongParamCount tests wrong number of params.
func TestHandleSubmitBuilderBidV1_WrongParamCount(t *testing.T) {
	api := newBuilderTestAPI()
	_, rpcErr := api.handleSubmitBuilderBidV1(nil)
	if rpcErr == nil {
		t.Fatal("expected error for nil params")
	}
	if rpcErr.Code != InvalidParamsCode {
		t.Fatalf("want code %d, got %d", InvalidParamsCode, rpcErr.Code)
	}
}

// TestHandleGetBuilderBidsV1_RPC tests the JSON-RPC handler for getting bids.
func TestHandleGetBuilderBidsV1_RPC(t *testing.T) {
	api := newBuilderTestAPI()

	slotJSON, _ := json.Marshal(uint64(10))
	params := []json.RawMessage{slotJSON}
	result, rpcErr := api.handleGetBuilderBidsV1(params)
	if rpcErr != nil {
		t.Fatalf("error: code=%d msg=%s", rpcErr.Code, rpcErr.Message)
	}
	bids, ok := result.([]*SignedExecutionPayloadBid)
	if !ok {
		t.Fatalf("result type: %T", result)
	}
	if len(bids) != 0 {
		t.Fatalf("want 0 bids, got %d", len(bids))
	}
}

// TestBidHash_Deterministic tests that ExecutionPayloadBid.BidHash is deterministic.
func TestBidHash_Deterministic(t *testing.T) {
	bid := &ExecutionPayloadBid{
		ParentBlockHash: types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		BlockHash:       types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		Slot:            10,
		Value:           100,
		BuilderIndex:    0,
		GasLimit:        30000000,
		FeeRecipient:    types.HexToAddress("0xaaaa"),
	}

	h1 := bid.BidHash()
	h2 := bid.BidHash()
	if h1 != h2 {
		t.Fatal("BidHash should be deterministic")
	}
	if h1 == (types.Hash{}) {
		t.Fatal("BidHash should not be zero")
	}

	bid.Value = 200
	h3 := bid.BidHash()
	if h3 == h1 {
		t.Fatal("different bid should produce different hash")
	}
}

// TestBuilderRegistry_Lifecycle tests register, bid, best bid, unregister.
func TestBuilderRegistry_Lifecycle(t *testing.T) {
	reg := NewBuilderRegistry()

	var pubkey BLSPubkey
	pubkey[0] = 0x01
	builder, err := reg.RegisterBuilder(&BuilderRegistrationV1{
		Pubkey:       pubkey,
		FeeRecipient: types.HexToAddress("0xaaaa"),
		GasLimit:     30000000,
		Timestamp:    1000,
	}, MinBuilderStake)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	err = reg.SubmitBid(&SignedExecutionPayloadBid{
		Message: ExecutionPayloadBid{
			Slot:            1,
			BuilderIndex:    builder.Index,
			Value:           100,
			BlockHash:       types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			ParentBlockHash: types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			GasLimit:        30000000,
			FeeRecipient:    types.HexToAddress("0xaaaa"),
		},
	})
	if err != nil {
		t.Fatalf("submit bid: %v", err)
	}

	best, err := reg.GetBestBid(1)
	if err != nil {
		t.Fatalf("get best bid: %v", err)
	}
	if best.Message.Value != 100 {
		t.Fatalf("want value 100, got %d", best.Message.Value)
	}

	if err := reg.UnregisterBuilder(pubkey); err != nil {
		t.Fatalf("unregister: %v", err)
	}

	err = reg.SubmitBid(&SignedExecutionPayloadBid{
		Message: ExecutionPayloadBid{
			Slot:            2,
			BuilderIndex:    builder.Index,
			Value:           200,
			BlockHash:       types.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333"),
			ParentBlockHash: types.HexToHash("0x4444444444444444444444444444444444444444444444444444444444444444"),
			GasLimit:        30000000,
			FeeRecipient:    types.HexToAddress("0xaaaa"),
		},
	})
	if err == nil {
		t.Fatal("expected error for bid from exiting builder")
	}
}

// TestBuilderRegistry_InsufficientStakeAPI tests registration with insufficient stake.
func TestBuilderRegistry_InsufficientStakeAPI(t *testing.T) {
	reg := NewBuilderRegistry()
	var pubkey BLSPubkey
	pubkey[0] = 0x01

	_, err := reg.RegisterBuilder(&BuilderRegistrationV1{
		Pubkey: pubkey,
	}, big.NewInt(1)) // 1 wei, well below minimum
	if err == nil {
		t.Fatal("expected error for insufficient stake")
	}
}

// TestBuilderRegistry_PruneSlotAPI tests pruning bids for a slot.
func TestBuilderRegistry_PruneSlotAPI(t *testing.T) {
	api := newBuilderTestAPI()
	idx := registerBuilderForAPI(t, api)

	bid := SignedExecutionPayloadBid{
		Message: ExecutionPayloadBid{
			Slot:            10,
			BuilderIndex:    idx,
			Value:           100,
			BlockHash:       types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			ParentBlockHash: types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			GasLimit:        30000000,
			FeeRecipient:    types.HexToAddress("0xaaaa"),
		},
	}
	if err := api.SubmitBuilderBidV1(bid); err != nil {
		t.Fatalf("submit bid: %v", err)
	}

	api.builderRegistry.PruneSlot(10)

	bids := api.GetBuilderBidsV1(10)
	if len(bids) != 0 {
		t.Fatalf("want 0 bids after prune, got %d", len(bids))
	}
}

// TestSubmitBuilderBidV1_BidObserver tests that an installed bid observer
// receives every accepted builder bid and that observer errors propagate.
func TestSubmitBuilderBidV1_BidObserver(t *testing.T) {
	api := newBuilderTestAPI()
	idx := registerBuilderForAPI(t, api)

	var observed []*SignedExecutionPayloadBid
	api.SetBidObserver(func(signed *SignedExecutionPayloadBid) error {
		observed = append(observed, signed)
		return nil
	})

	bid := SignedExecutionPayloadBid{
		Message: ExecutionPayloadBid{
			Slot:            10,
			BuilderIndex:    idx,
			Value:           100,
			BlockHash:       types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
			ParentBlockHash: types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
			GasLimit:        30000000,
			FeeRecipient:    types.HexToAddress("0xaaaa"),
		},
	}
	if err := api.SubmitBuilderBidV1(bid); err != nil {
		t.Fatalf("SubmitBuilderBidV1 error: %v", err)
	}
	if len(observed) != 1 || observed[0].Message.Slot != 10 {
		t.Fatalf("observer did not receive the accepted bid: %d", len(observed))
	}

	// Observer errors surface to the caller after registry acceptance.
	api.SetBidObserver(func(*SignedExecutionPayloadBid) error {
		return ErrNoBidsAvailable
	})
	if err := api.SubmitBuilderBidV1(bid); err == nil {
		t.Fatal("expected observer error to propagate")
	}
}
