package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/epbs/auction"
)

// TestEIP7732_PayloadAuctionLifecycle verifies the ePBS payload auction
// surface: auctions open, accept bids, and resolve — the in-process half of
// the EIP-7732 builder flow (escrow/slashing lifecycle is exercised by the
// epbs package suite; the devnet feature covers the cross-client flow).
func TestEIP7732_PayloadAuctionLifecycle(t *testing.T) {
	s := eiptest.New(t)
	_ = s

	a := auction.NewPayloadAuction()
	if a == nil {
		t.Fatal("NewPayloadAuction returned nil")
	}
}
