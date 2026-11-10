package eiptest_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

type loggingPool struct {
	mu   sync.Mutex
	txs  []*types.Transaction
	calls int
}

func (p *loggingPool) Pending() []*types.Transaction {
	p.mu.Lock(); p.calls++; n := len(p.txs); p.mu.Unlock()
	fmt.Printf("PL pool.Pending() called #%d returning %d txs\n", p.calls, n)
	return p.txs
}

func TestProbePoolWiring(t *testing.T) {
	s := eiptest.New(t)
	ep := s.Engine()
	// swap in a logging pool
	lp := &loggingPool{}
	tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), 0,
		eiptest.ToPtr(s.Bob().Address), nil, nil, 100_000, bigFeeCap, bigTipCap)
	lp.txs = append(lp.txs, tx)
	// reach into backend via exported setter
	ep.Backend().SetTxPool(lp)
	id, _ := ep.FCU()
	resp, err := ep.GetPayload(id)
	if err != nil { t.Fatal(err) }
	fmt.Printf("PL payload txs=%d\n", len(resp.ExecutionPayload.Transactions))
	_ = block.TxPoolReader(lp)
}
