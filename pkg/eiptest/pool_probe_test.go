package eiptest_test

import (
	"errors"
	"testing"

	"github.com/eth2030/eth2030/core/chain"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/txpool"
)

func TestPoolProbe_AdmissionReplacementEvictionAndReorg(t *testing.T) {
	cfg := txpool.DefaultConfig()
	cfg.MaxSize = 2
	s := eiptest.New(t, eiptest.WithPoolConfig(cfg))
	p := s.Pool()
	tx := func(account *eiptest.Account, nonce uint64, fee, tip int64) *types.Transaction {
		return eiptest.DynamicFee(s.Config().ChainID, account, nonce, eiptest.ToPtr(s.Charlie().Address),
			eiptest.Wei(1), nil, 21_000, eiptest.GweiWei(fee), eiptest.GweiWei(tip))
	}
	future := tx(s.Alice(), 1, 2, 1)
	p.ExpectAccepted(t, future)
	p.ExpectPendingCount(t, 0)
	first := tx(s.Alice(), 0, 2, 1)
	p.ExpectAccepted(t, first)
	p.ExpectPendingCount(t, 2)
	p.ExpectNonceGapFree(t)
	p.ExpectRejected(t, tx(s.Alice(), 0, 3, 1), "replacement transaction underpriced")
	replacement := tx(s.Alice(), 0, 4, 2)
	p.ExpectAccepted(t, replacement)
	p.ExpectPresent(t, first, false)

	high := tx(s.Bob(), 0, 6, 3)
	p.ExpectAccepted(t, high)
	p.ExpectPresent(t, future, false)
	p.ExpectNonceGapFree(t)
	if r := s.Commit(); r.TxCount() != 0 {
		t.Fatal("ordinary empty commit consumed pool transactions")
	}
	parent := s.CurrentBlock()
	built := p.CommitPending()
	if built.TxCount() != 2 {
		t.Fatalf("built %d transactions, want 2", built.TxCount())
	}
	p.ExpectPendingCount(t, 0)
	side, err := s.BuildSideBlock(parent, built.Block.Time()+1)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Reorg(side); err != nil {
		t.Fatal(err)
	}
	eiptest.ExpectNonce(t, s, s.Alice().Address, 0)
	p.ExpectPresent(t, replacement, true)
	p.ExpectPresent(t, high, true)
	p.ExpectNonceGapFree(t)
	if err := s.Finalize(side); err != nil {
		t.Fatal(err)
	}
	if got := s.Chain().CurrentFinalBlock(); got == nil || got.Hash() != side.Hash() {
		t.Fatal("finalized head did not advance")
	}
	if err := s.Reorg(built.Block); !errors.Is(err, chain.ErrReorgPastFinalized) {
		t.Fatalf("reorg below finality: %v", err)
	}
}
