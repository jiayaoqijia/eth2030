package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
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
