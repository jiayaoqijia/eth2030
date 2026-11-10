package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP7002_WithdrawalRequests verifies EIP-7002 at the request-queue
// level: the withdrawal request system contract's storage queue drains into
// typed 0x01 requests through the shared requests processor.
func TestEIP7002_WithdrawalRequests(t *testing.T) {
	s := eiptest.New(t)
	st := s.State()

	wd := types.WithdrawalRequestAddress
	st.SetCode(wd, []byte{0x00})
	st.SetState(wd, types.Hash{}, eiptest.Uint64ToHash(1))
	// 48-byte pubkey + 8-byte amount is the request payload; the queue packs
	// one word per request in this layout.
	var word types.Hash
	word[0] = types.WithdrawalRequestType
	word[31] = 42 // amount
	st.SetState(wd, types.BytesToHash([]byte{0x01}), word)

	header := s.CurrentBlock().Header()
	requests, err := execution.ProcessRequests(s.Config(), st, header)
	if err != nil {
		t.Fatalf("ProcessRequests: %v", err)
	}
	if len(requests) != 1 {
		t.Fatalf("withdrawal requests = %d, want 1", len(requests))
	}
	if requests[0].Type != types.WithdrawalRequestType {
		t.Fatalf("request type = %d, want 0x01 (withdrawal)", requests[0].Type)
	}
}
