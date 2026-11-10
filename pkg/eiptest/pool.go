package eiptest

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/txpool"
)

// PoolProbe drives the real txpool against the simulated chain: submission,
// admission rules, pending selection, and chain-event feedback (reset on
// block insert).
type PoolProbe struct {
	s    *Sim
	pool *txpool.TxPool
}

// Pool returns the txpool probe, wiring the pool on first use.
func (s *Sim) Pool() *PoolProbe {
	s.t.Helper()
	return &PoolProbe{s: s, pool: s.WirePool()}
}

// PoolHandle exposes the underlying pool.
func (p *PoolProbe) PoolHandle() *txpool.TxPool { return p.pool }

// Submit submits a transaction as a remote peer would, returning the pool's
// error (nil when accepted).
func (p *PoolProbe) Submit(tx *types.Transaction) error {
	return p.pool.AddRemote(tx)
}

// ExpectAccepted asserts the tx is admitted to the pool.
func (p *PoolProbe) ExpectAccepted(t *testing.T, tx *types.Transaction) {
	t.Helper()
	if err := p.Submit(tx); err != nil {
		t.Fatalf("pool rejected tx (type %d, nonce %d): %v", tx.Type(), tx.Nonce(), err)
	}
}

// ExpectRejected asserts the tx is rejected with an error containing substr.
func (p *PoolProbe) ExpectRejected(t *testing.T, tx *types.Transaction, substr string) {
	t.Helper()
	err := p.Submit(tx)
	if err == nil {
		t.Fatalf("pool accepted tx (type %d, nonce %d), want rejection containing %q", tx.Type(), tx.Nonce(), substr)
	}
	if substr != "" && !contains(err.Error(), substr) {
		t.Fatalf("pool rejection = %q, want containing %q", err.Error(), substr)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// Pending returns the pool's current pending (executable) transactions.
func (p *PoolProbe) Pending() []*types.Transaction { return p.pool.PendingFlat() }

// ExpectPendingCount asserts the number of pending transactions.
func (p *PoolProbe) ExpectPendingCount(t *testing.T, n int) {
	t.Helper()
	if got := len(p.Pending()); got != n {
		t.Fatalf("pending count = %d, want %d", got, n)
	}
}

// CommitPending builds a block from the pool and inserts it, then verifies
// the pool was reset (accepted txs removed). Returns the block result.
func (p *PoolProbe) CommitPending() *BlockResult {
	t := p.s.t
	t.Helper()
	before := len(p.Pending())
	r := p.s.CommitFromPool()
	if got := len(r.Receipts); got != before {
		t.Fatalf("block built %d txs but pool had %d pending", got, before)
	}
	return r
}

// ExpectNonceGapFree asserts every sender's pending txs form a contiguous
// nonce sequence starting at their account nonce (the invariant broken by
// the historical tail-eviction bug).
func (p *PoolProbe) ExpectNonceGapFree(t *testing.T) {
	t.Helper()
	head := p.s.bc.CurrentBlock()
	st, err := p.s.bc.StateAtBlock(head)
	if err != nil {
		t.Fatalf("state: %v", err)
	}
	bySender := map[types.Address][]uint64{}
	for _, tx := range p.Pending() {
		sender := tx.Sender()
		if sender == nil {
			t.Fatalf("pending tx %s has no sender", tx.Hash().Hex())
		}
		bySender[*sender] = append(bySender[*sender], tx.Nonce())
	}
	for sender, nonces := range bySender {
		next := st.GetNonce(sender)
		for _, n := range nonces {
			if n != next {
				t.Fatalf("sender %s: pending nonce %d, want %d (gap)", sender.Hex(), n, next)
			}
			next++
		}
	}
}
