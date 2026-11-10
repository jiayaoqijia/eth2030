package eiptest

import (
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/chain"
	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/txpool"
)

// Sim is an in-process chain simulator. It composes the real production
// modules — config.SetupGenesis, chain.Blockchain, block.BlockBuilder,
// execution.StateProcessor (via builder/insert) and optionally txpool.TxPool —
// and drives them through a small API. Blocks are built, inserted and
// validated exactly as in production, so insert-time checks (BAL hash,
// header consistency) run on every Commit.
//
// State continuity (plan T0.2): the builder resolves parent state through the
// chain reader (BlockchainReader.StateAtBlock), so every block executes on the
// real post-parent state with no manual rebuilds.
type Sim struct {
	t      *testing.T
	cfg    *config.ChainConfig
	bc     *chain.Blockchain
	db     rawdb.Database
	builder *block.BlockBuilder
	pool   *txpool.TxPool
	genesis *types.Block

	now        uint64 // timestamp of the next block
	timeStep   uint64
	beneficiary types.Address
	keyAccounts []*Account
	scheduledForks map[Fork]uint64 // fork -> genesis-relative offset
}

// New creates a Sim with the given options and runs its lifecycle checks
// against t. Genesis state comes from the alloc options (Fund/FundCode).
func New(t *testing.T, opts ...SimOption) *Sim {
	t.Helper()
	o := defaultSimOptions()
	for _, opt := range opts {
		if err := opt(o); err != nil {
			t.Fatalf("eiptest: option: %v", err)
		}
	}
	s, err := newSim(o)
	if err != nil {
		t.Fatalf("eiptest: %v", err)
	}
	s.t = t
	return s
}

// NewSim creates a Sim without a *testing.T (for programmatic use).
func NewSim(opts ...SimOption) (*Sim, error) {
	o := defaultSimOptions()
	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, fmt.Errorf("eiptest: option: %w", err)
		}
	}
	return newSim(o)
}

func newSim(o *simOptions) (*Sim, error) {
	kr, err := newKeyRing()
	if err != nil {
		return nil, err
	}
	standard := []*Account{kr.Alice, kr.Bob, kr.Charlie, kr.Dave, kr.Erin, kr.Faythe}
	defaultBalance := new(big.Int).Mul(big.NewInt(1000), Ether)
	for _, acc := range standard {
		if _, funded := o.alloc[acc.Address]; !funded {
			o.alloc[acc.Address] = config.GenesisAccount{Balance: new(big.Int).Set(defaultBalance)}
		}
	}

	cfg, err := o.buildChainConfig()
	if err != nil {
		return nil, err
	}
	baseFee := o.baseFee
	if baseFee == nil {
		baseFee = new(big.Int).Mul(big.NewInt(int64(DefaultBaseFeeGwei)), Gwei)
	}
	db := rawdb.NewMemoryDB()
	res, err := config.SetupGenesis(db, &config.Genesis{
		Config:    cfg,
		Timestamp: o.genesisTime,
		GasLimit:  o.gasLimit,
		BaseFee:   baseFee,
		Alloc:     o.alloc,
	})
	if err != nil {
		return nil, fmt.Errorf("setup genesis: %w", err)
	}

	bc, err := chain.NewBlockchain(cfg, res.Block, res.StateDB, db)
	if err != nil {
		return nil, fmt.Errorf("new blockchain: %w", err)
	}

	s := &Sim{
		cfg:            cfg,
		bc:             bc,
		db:             db,
		builder:        block.NewBlockBuilder(cfg, bc, nil),
		genesis:        res.Block,
		now:            o.genesisTime,
		timeStep:       12,
		keyAccounts:    standard,
		scheduledForks: o.forkTimes,
	}
	return s, nil
}

// poolReaderAdapter adapts *txpool.TxPool (map-based Pending) to the
// builder's flat TxPoolReader interface.
type poolReaderAdapter struct{ pool *txpool.TxPool }

func (a *poolReaderAdapter) Pending() []*types.Transaction { return a.pool.PendingFlat() }

// WirePool attaches a real txpool to the simulation. Commit() then pulls from
// the pool, and chain events (insert) are fed back via pool.Reset/RecordBlock
// mirroring node wiring.
func (s *Sim) WirePool() *txpool.TxPool {
	if s.pool != nil {
		return s.pool
	}
	s.pool = txpool.New(txpool.DefaultConfig(), &poolStateReader{sim: s})
	s.builder = block.NewBlockBuilder(s.cfg, s.bc, &poolReaderAdapter{pool: s.pool})
	return s.pool
}

// poolStateReader adapts the chain state to the txpool StateReader interface.
type poolStateReader struct{ sim *Sim }

func (r *poolStateReader) snapshot() state.StateDB {
	st, err := r.sim.bc.StateAtBlock(r.sim.bc.CurrentBlock())
	if err != nil {
		return state.NewMemoryStateDB()
	}
	return st
}

func (r *poolStateReader) GetNonce(addr types.Address) uint64  { return r.snapshot().GetNonce(addr) }
func (r *poolStateReader) GetBalance(addr types.Address) *big.Int {
	return r.snapshot().GetBalance(addr)
}

// Config returns the chain config.
func (s *Sim) Config() *config.ChainConfig { return s.cfg }

// Genesis returns the genesis block.
func (s *Sim) Genesis() *types.Block { return s.genesis }

// Chain returns the underlying blockchain.
func (s *Sim) Chain() *chain.Blockchain { return s.bc }

// CurrentBlock returns the chain head.
func (s *Sim) CurrentBlock() *types.Block { return s.bc.CurrentBlock() }

// Now returns the timestamp the next block will carry.
func (s *Sim) Now() uint64 { return s.nextTime() }

func (s *Sim) nextTime() uint64 {
	if t := s.bc.CurrentBlock().Time(); t+ s.timeStep > s.now {
		return t + s.timeStep
	}
	return s.now
}

// AdvanceTime moves the simulated clock forward by d seconds. The next block
// carries at least this timestamp. Fork activations scheduled via ForkAt are
// crossed by advancing time past the boundary.
func (s *Sim) AdvanceTime(d uint64) {
	s.now += d
}

// AdvanceTo moves the simulated clock to the absolute timestamp. The next
// Commit block carries exactly this time (bounded by the parent time).
func (s *Sim) AdvanceTo(abs uint64) {
	if abs < s.now {
		abs = s.now
	}
	s.now = abs
}

// forkActiveAt reports whether the fork rules are active at the given
// timestamp, via the config's fork checkers.
func (s *Sim) forkActiveAt(fork Fork, ts uint64) bool {
	switch fork {
	case ForkAmsterdam:
		return s.cfg.IsAmsterdam(ts)
	case ForkGlamsterdan:
		return s.cfg.IsGlamsterdan(ts)
	case ForkHogota:
		return s.cfg.IsHogota(ts)
	case ForkIPlus:
		return s.cfg.IsIPlus(ts)
	case ForkBPO1:
		return s.cfg.IsBPO1(ts)
	case ForkBPO2:
		return s.cfg.IsBPO2(ts)
	}
	return false
}

// SetBeneficiary sets the fee recipient for subsequent blocks.
func (s *Sim) SetBeneficiary(addr types.Address) { s.beneficiary = addr }

// Commit builds a block from the given explicit transactions, inserts it into
// the chain and returns the result. An empty argument list produces an empty
// block (still running system contracts).
func (s *Sim) Commit(txs ...*types.Transaction) *BlockResult {
	s.t.Helper()
	return s.commit(txs)
}

// CommitFromPool builds a block from the wired pool's pending transactions.
func (s *Sim) CommitFromPool() *BlockResult {
	s.t.Helper()
	if s.pool == nil {
		s.t.Fatal("eiptest: CommitFromPool requires WirePool()")
	}
	return s.commit(nil)
}

func (s *Sim) commit(explicit []*types.Transaction) *BlockResult {
	s.t.Helper()
	parent := s.bc.CurrentBlock()
	ts := s.nextTime()
	s.now = ts

	attrs := &block.BuildBlockAttributes{
		Timestamp:    ts,
		FeeRecipient: s.beneficiary,
		BeaconRoot:   new(types.Hash),
		GasLimit:     parent.GasLimit(), // keep constant; avoids elasticity-boundary jitter
	}

	if s.pool != nil && len(explicit) == 0 {
		// Pool-driven build: builder pulls pending txs itself.
	} else {
		// Explicit tx list overrides the pool for this block.
		s.builder = block.NewBlockBuilder(s.cfg, s.bc, &slicePool{txs: explicit})
	}

	blk, receipts, err := s.builder.BuildBlock(parent.Header(), attrs)
	if err != nil {
		s.t.Fatalf("eiptest: BuildBlock: %v", err)
	}
	if err := s.bc.InsertBlock(blk); err != nil {
		s.t.Fatalf("eiptest: InsertBlock(block %d): %v", blk.NumberU64(), err)
	}
	if s.pool != nil {
		// Mirror node wiring: reset pool state from the post-block state and
		// feed fee data back to the price suggestor.
		s.pool.Reset(&poolStateReader{sim: s})
		s.pool.RecordBlock(blk.Header(), blk.Transactions())
	}
	// Restore a pool-driven builder for the next block.
	if s.pool != nil {
		s.builder = block.NewBlockBuilder(s.cfg, s.bc, &poolReaderAdapter{pool: s.pool})
	}
	return &BlockResult{Sim: s, Block: blk, Receipts: receipts}
}

// slicePool adapts an explicit tx list to the builder's TxPoolReader.
type slicePool struct{ txs []*types.Transaction }

func (p *slicePool) Pending() []*types.Transaction { return p.txs }

// CommitInvalid builds and inserts a block that is expected to fail. It
// returns the insertion error instead of failing the test, for negative
// header/BAL validation tests.
func (s *Sim) CommitInvalid(txs ...*types.Transaction) error {
	s.t.Helper()
	parent := s.bc.CurrentBlock()
	ts := s.nextTime()
	s.now = ts
	attrs := &block.BuildBlockAttributes{Timestamp: ts, BeaconRoot: new(types.Hash)}
	builder := block.NewBlockBuilder(s.cfg, s.bc, &slicePool{txs: txs})
	blk, _, err := builder.BuildBlock(parent.Header(), attrs)
	if err != nil {
		return fmt.Errorf("build: %w", err)
	}
	if err := s.bc.InsertBlock(blk); err != nil {
		return err
	}
	return errors.New("eiptest: expected invalid block to be rejected, but insert succeeded")
}

// ---------------------------------------------------------------------------
// State accessors
// ---------------------------------------------------------------------------

// State returns the post-head state.
func (s *Sim) State() state.StateDB {
	st, err := s.bc.StateAtBlock(s.bc.CurrentBlock())
	if err != nil {
		s.t.Fatalf("eiptest: state at head: %v", err)
	}
	return st
}

// Balance returns the balance of addr in wei.
func (s *Sim) Balance(addr types.Address) *big.Int { return s.State().GetBalance(addr) }

// Nonce returns the nonce of addr.
func (s *Sim) Nonce(addr types.Address) uint64 { return s.State().GetNonce(addr) }

// Code returns the code at addr.
func (s *Sim) Code(addr types.Address) []byte { return s.State().GetCode(addr) }

// StorageAt returns the storage value of slot for addr.
func (s *Sim) StorageAt(addr types.Address, slot types.Hash) types.Hash {
	return s.State().GetState(addr, slot)
}

// Block returns the canonical block at n.
func (s *Sim) Block(n uint64) *types.Block {
	blk := s.bc.GetBlockByNumber(n)
	if blk == nil {
		s.t.Fatalf("eiptest: no canonical block at %d", n)
	}
	return blk
}

// CommitWithWithdrawals builds an empty block carrying beacon withdrawals
// (EIP-4895) and inserts it.
func (s *Sim) CommitWithWithdrawals(wds ...*types.Withdrawal) *BlockResult {
	s.t.Helper()
	parent := s.bc.CurrentBlock()
	ts := s.nextTime()
	s.now = ts
	attrs := &block.BuildBlockAttributes{
		Timestamp:    ts,
		FeeRecipient: s.beneficiary,
		BeaconRoot:   new(types.Hash),
		GasLimit:     parent.GasLimit(),
		Withdrawals:  wds,
	}
	blk, receipts, err := s.builder.BuildBlock(parent.Header(), attrs)
	if err != nil {
		s.t.Fatalf("eiptest: BuildBlock(withdrawals): %v", err)
	}
	if err := s.bc.InsertBlock(blk); err != nil {
		s.t.Fatalf("eiptest: InsertBlock(withdrawals block %d): %v", blk.NumberU64(), err)
	}
	return &BlockResult{Sim: s, Block: blk, Receipts: receipts}
}
