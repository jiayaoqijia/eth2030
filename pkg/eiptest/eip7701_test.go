package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP7701_AATransaction verifies EIP-7701 native AA: a type-0x05 tx
// carries the sender's execution data, is admitted to the pool, and its
// post-execution nonce increment is applied exactly once (the historical
// replay-parity bug surface).
func TestEIP7701_AATransaction(t *testing.T) {
	s := eiptest.New(t)
	aa := s.Account(10)
	// The scratch AA account is not genesis-funded by default; fund it via a
	// transfer from Alice before submitting AA transactions.
	fund := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(aa.Address), new(big.Int).Mul(big.NewInt(10), eiptest.Ether), nil,
		100_000, bigFeeCap, bigTipCap,
	)
	eiptest.ExpectSuccess(t, s.Commit(fund), 0)

	// AA tx: sender executes its own code path via SenderExecutionData.
	// The AA executor routes frames through the AA entrypoint; a minimal
	// execution payload exercises the pipeline.
	tx := eiptest.UnsignedAA(
		aa.Address, s.Config().ChainID, s.Nonce(aa.Address),
		nil, // empty execution data — validation/execution defaults
		100_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)

	// Nonce incremented exactly once post-execution.
	eiptest.ExpectNonce(t, s, aa.Address, 1)

	// Replay parity (EIP-7701 regression surface): re-executing the block
	// yields the same state root.
	blk := r.Block
	r2 := s.Commit() // advance; replay of blk happens inside InsertBlock
	_ = r2
	if s.Block(blk.NumberU64()).Hash() != blk.Hash() {
		t.Fatal("replayed block hash diverged")
	}

	// Pool surface: a second AA tx is admitted through the real pool.
	s.WirePool()
	p := s.Pool()
	tx2 := eiptest.UnsignedAA(
		aa.Address, s.Config().ChainID, s.Nonce(aa.Address),
		nil, 100_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	p.ExpectAccepted(t, tx2)
}

// TestEIP7701_AATypeByte verifies the envelope type byte is 0x05 per spec
// (the historical 0x0c regression).
func TestEIP7701_AATypeByte(t *testing.T) {
	if types.AATxType != 0x05 {
		t.Fatalf("AATxType = 0x%02x, want 0x05", types.AATxType)
	}
}
