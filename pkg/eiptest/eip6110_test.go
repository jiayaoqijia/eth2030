package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP6110_DepositRequests verifies EIP-6110 at the request-queue level:
// the deposit contract's storage queue (slot 0 = count, slots 1..N = data)
// is drained by the requests processor into typed 0x00 requests.
func TestEIP6110_DepositRequests(t *testing.T) {
	s := eiptest.New(t)
	st := s.State()

	// The queue convention: the system contract account must exist, slot 0
	// holds the count, slots 1..N hold one request word each.
	dep := types.DepositContractAddress
	st.SetCode(dep, []byte{0x00}) // mark the contract as existing
	st.SetState(dep, types.Hash{}, eiptest.Uint64ToHash(2))
	st.SetState(dep, types.BytesToHash([]byte{0x01}), depositWord(1))
	st.SetState(dep, types.BytesToHash([]byte{0x02}), depositWord(2))

	header := s.CurrentBlock().Header()
	requests, err := execution.ProcessRequests(s.Config(), st, header)
	if err != nil {
		t.Fatalf("ProcessRequests: %v", err)
	}
	if len(requests) != 2 {
		t.Fatalf("deposit requests = %d, want 2", len(requests))
	}
	for i, req := range requests {
		if req.Type != types.DepositRequestType {
			t.Fatalf("request %d type = %d, want 0x00 (deposit)", i, req.Type)
		}
	}
	// The queue count is consumed after processing.
	if got := st.GetState(dep, types.Hash{}); got != (types.Hash{}) {
		t.Fatalf("queue count not cleared after processing: %s", got.Hex())
	}
}

// depositWord builds a 32-byte deposit request data word.
func depositWord(i byte) types.Hash {
	var h types.Hash
	h[0] = types.DepositRequestType
	h[31] = i
	return h
}

var _ = state.NewMemoryStateDB
