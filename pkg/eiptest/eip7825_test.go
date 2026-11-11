package eiptest_test

import (
	"errors"
	"testing"

	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

func TestEIP7825_TransactionGasCap(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkOsaka, 100))
	tx := func(limit uint64) *types.Transaction {
		return eiptest.DynamicFee(s.Config().ChainID, s.Alice(), s.Nonce(s.Alice().Address),
			eiptest.ToPtr(s.Bob().Address), nil, nil, limit, bigFeeCap, bigTipCap)
	}
	pool := s.Pool()
	before := s.Fork(eiptest.ForkOsaka).Before()
	before.ExpectInactive(t)
	pool.ExpectAccepted(t, tx(gas.MaxTransactionGas+1))

	s.Fork(eiptest.ForkOsaka).AtBoundary().ExpectActive(t)
	oversized := tx(gas.MaxTransactionGas + 2)
	pool.ExpectRejected(t, oversized, "EIP-7825")
	if err := s.CommitInvalid(oversized); !errors.Is(err, gas.ErrTxGasLimitExceeded) {
		t.Fatalf("block validation: %v", err)
	}
	if got := s.Commit(oversized).TxCount(); got != 0 {
		t.Fatalf("builder included %d over-cap transactions", got)
	}
	atCap := tx(gas.MaxTransactionGas)
	eiptest.ExpectSuccess(t, s.Commit(atCap), 0)
}
