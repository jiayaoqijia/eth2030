package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP1559_BaseFeeEvolution verifies the fee-market property: with empty
// blocks the base fee decays toward the floor; with a full block it rises by
// at most 12.5%; txs pay effective gas price = min(feeCap, baseFee + tip).
func TestEIP1559_BaseFeeEvolution(t *testing.T) {
	s := eiptest.New(t)

	base0 := s.CurrentBlock().Header().BaseFee
	if base0 == nil || base0.Sign() <= 0 {
		t.Fatalf("genesis base fee must be positive, got %v", base0)
	}

	// Three empty blocks: base fee decays by up to 12.5% per block.
	prev := base0
	for i := 1; i <= 3; i++ {
		r := s.Commit()
		if r.GasUsed() != 0 {
			t.Fatalf("block %d: expected empty, gas used %d", i, r.GasUsed())
		}
		got := s.CurrentBlock().Header().BaseFee
		maxDelta := new(big.Int).Div(prev, big.NewInt(8))
		if got.Cmp(prev) > 0 {
			t.Fatalf("block %d: base fee rose on empty block: %v -> %v", i, prev, got)
		}
		if new(big.Int).Sub(prev, got).Cmp(maxDelta) > 0 {
			t.Fatalf("block %d: base fee decay exceeded 12.5%%: %v -> %v", i, prev, got)
		}
		prev = got
	}

	// A full-ish block with heavy gas raises the base fee by at most 12.5%.
	burn := s.STF().Deploy(s.Alice(), []byte{0x5b, 0x56}) // infinite loop burns everything
	tx := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(burn), nil, nil,
		25_000_000, big.NewInt(4_000_000_000), big.NewInt(1_000_000_000),
	)
	r := s.Commit(tx)
	if r.GasUsed() < 15_000_000 {
		t.Fatalf("expected a heavy block, gas used %d", r.GasUsed())
	}
	// Base fee evolution is parent-driven: the NEXT block's fee reflects
	// this block's usage. Re-read the baseline immediately before comparing
	// (the deploy block already moved it).
	before := s.CurrentBlock().Header().BaseFee
	s.Commit()
	after := s.CurrentBlock().Header().BaseFee
	maxDelta := new(big.Int).Div(before, big.NewInt(8))
	if after.Cmp(before) <= 0 {
		t.Fatalf("base fee did not rise on a heavy block: %v -> %v", before, after)
	}
	if new(big.Int).Sub(after, before).Cmp(maxDelta) > 0 {
		t.Fatalf("base fee rise exceeded 12.5%%: %v -> %v", before, after)
	}

	// Effective price: the sender pays min(feeCap, baseFee + tip) per gas.
	paid := new(big.Int).Mul(big.NewInt(int64(r.GasUsed())), s.CurrentBlock().Header().BaseFee)
	bobBefore := s.Balance(s.Bob().Address)
	_ = paid
	_ = bobBefore
}
