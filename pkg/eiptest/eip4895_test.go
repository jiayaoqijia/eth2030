package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP4895_Withdrawals verifies beacon withdrawals: withdrawals attached
// to a block credit the target accounts in gwei without consuming gas and
// without any transaction.
func TestEIP4895_Withdrawals(t *testing.T) {
	s := eiptest.New(t)

	// Use a scratch account (not genesis funded) to observe a clean credit.
	scratch := s.Account(9)
	before := s.Balance(scratch.Address)

	wd := &types.Withdrawal{
		Index:     1,
		ValidatorIndex: 7,
		Address:   scratch.Address,
		Amount:    1_000, // gwei
	}
	// Sim needs withdrawal support on commit: drive via the builder attrs
	// through a custom commit. WirePool keeps the builder; use CommitWithdrawals.
	r := s.CommitWithWithdrawals(wd)
	if r.TxCount() != 0 {
		t.Fatalf("withdrawal block should carry no user txs, got %d", r.TxCount())
	}

	want := new(big.Int).Add(before, new(big.Int).Mul(big.NewInt(1_000), big.NewInt(1_000_000_000)))
	got := s.Balance(scratch.Address)
	if got.Cmp(want) != 0 {
		t.Fatalf("withdrawal credit = %s wei, want %s wei (1000 gwei)", got, want)
	}
}
