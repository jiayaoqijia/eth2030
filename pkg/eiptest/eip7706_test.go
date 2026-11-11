package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP7706_CalldataGasHeader verifies EIP-7706: once Glamsterdan is
// active, block headers carry the calldata gas vector (used + excess) and
// the fields are absent before activation.
func TestEIP7706_CalldataGasHeader(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkGlamsterdan, 100))

	fp := s.Fork(eiptest.ForkGlamsterdan)
	before := fp.Before()
	before.ExpectInactive(t)

	h := s.CurrentBlock().Header()
	if h.CalldataGasUsed != nil {
		t.Fatal("CalldataGasUsed set before Glamsterdan (fork gating violation)")
	}

	at := fp.AtBoundary()
	at.ExpectActive(t)

	h2 := s.CurrentBlock().Header()
	if h2.CalldataGasUsed == nil || h2.CalldataExcessGas == nil {
		t.Fatal("CalldataGasUsed/ExcessGas missing after Glamsterdan activation")
	}

	// At 64 floor gas per byte, 100,000 gas cannot cover 1,500 bytes.
	for _, size := range []int{100, 1_500, 2_000} {
		floor := uint64(4_500 + 64*size)
		candidate := func(limit uint64) *types.Transaction {
			return eiptest.UnsignedDynamicFee(s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
				eiptest.ToPtr(s.Bob().Address), nil, make([]byte, size), limit, bigFeeCap, bigTipCap)
		}
		if r := s.Commit(candidate(floor - 1)); r.TxCount() != 0 {
			t.Fatalf("included %d bytes below the calldata floor", size)
		}
		r := s.Commit(candidate(floor))
		eiptest.ExpectGasUsed(t, r, 0, floor)
		if used := *r.Block.Header().CalldataGasUsed; used != uint64(size*4) {
			t.Fatalf("calldata gas for %d zero bytes = %d, want %d", size, used, size*4)
		}
	}
}
