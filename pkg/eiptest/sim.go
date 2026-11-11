package eiptest

import (
	"errors"
	"fmt"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/chain"
	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest/contracts"
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
	t          *testing.T
	cfg        *config.ChainConfig
	bc         *chain.Blockchain
	pool       *txpool.TxPool
	poolConfig *txpool.Config
	genesis    *types.Block

	now            uint64 // timestamp of the next block
	timeStep       uint64
	beneficiary    types.Address
	keyAccounts    []*Account
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
		poolConfig:     o.poolConfig,
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

// WirePool attaches a real txpool to the simulation. CommitFromPool selects
// pending transactions and canonical head changes refresh admission state.
func (s *Sim) WirePool() *txpool.TxPool {
	if s.pool != nil {
		return s.pool
	}
	cfg := txpool.DefaultConfig()
	if s.poolConfig != nil {
		cfg = *s.poolConfig
	}
	rules := s.cfg.Rules(s.CurrentBlock().Number(), s.cfg.IsMerge(), s.CurrentBlock().Time())
	cfg.IsPrague, cfg.IsGlamsterdan = rules.IsPrague, rules.IsGlamsterdan
	cfg.IsOsaka = s.cfg.IsOsaka(s.CurrentBlock().Time())
	reader := &poolStateReader{sim: s}
	s.pool = txpool.New(cfg, reader)
	s.pool.SetCodeReader(reader)
	s.pool.SetBaseFee(s.CurrentBlock().Header().BaseFee)
	return s.pool
}

// poolStateReader adapts the chain state to the txpool StateReader interface.
type poolStateReader struct{ sim *Sim }

func (r *poolStateReader) snapshot() state.StateDB {
	st, err := r.sim.bc.StateAtBlock(r.sim.bc.CurrentBlock())
	if err != nil {
		panic(fmt.Errorf("eiptest: read pool head state: %w", err))
	}
	return st
}

func (r *poolStateReader) GetCodeSize(addr types.Address) int { return r.snapshot().GetCodeSize(addr) }

func (r *poolStateReader) GetNonce(addr types.Address) uint64 { return r.snapshot().GetNonce(addr) }
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
	parent := s.CurrentBlock().Time()
	if s.now > parent {
		return s.now
	}
	return parent + s.timeStep
}

// AdvanceTime moves the next block timestamp by d seconds from the staged time.
func (s *Sim) AdvanceTime(d uint64) { s.now += d }

// AdvanceTo stages an exact timestamp for the next block if it is after the head.
func (s *Sim) AdvanceTo(abs uint64) {
	if abs > s.now {
		s.now = abs
	}
}

// Slot stages a timestamp at a genesis-relative 12-second slot.
func (s *Sim) Slot(n uint64) { s.AdvanceTo(s.genesis.Time() + n*s.timeStep) }

// forkActiveAt reports whether the fork rules are active at the given
// timestamp, via the config's fork checkers.
func (s *Sim) forkActiveAt(fork Fork, ts uint64) bool {
	switch fork {
	case ForkOsaka:
		return s.cfg.IsOsaka(ts)
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

// Commit builds and validates a block from the explicit transaction candidates.
// With no candidates it builds an empty block. Use CommitFromPool for pending txs.
func (s *Sim) Commit(txs ...*types.Transaction) *BlockResult {
	s.helper()
	r, err := s.TryCommit(txs...)
	if err != nil {
		s.fail(err)
	}
	return r
}

// TryCommit is the error-returning form of Commit, also usable with NewSim.
func (s *Sim) TryCommit(txs ...*types.Transaction) (*BlockResult, error) {
	return s.commit(&slicePool{txs: txs}, s.attributes(), nil)
}

// CommitFromPool builds a block from executable pool transactions.
func (s *Sim) CommitFromPool() *BlockResult {
	s.helper()
	if s.pool == nil {
		s.fail(errors.New("eiptest: CommitFromPool requires WirePool"))
	}
	r, err := s.commit(&poolReaderAdapter{pool: s.pool}, s.attributes(), nil)
	if err != nil {
		s.fail(err)
	}
	return r
}

func (s *Sim) helper() {
	if s.t != nil {
		s.t.Helper()
	}
}

func (s *Sim) fail(err error) {
	if s.t != nil {
		s.t.Helper()
		s.t.Fatal(err)
	}
	panic(err)
}

func (s *Sim) attributes() block.BuildBlockAttributes {
	return block.BuildBlockAttributes{
		Timestamp: s.nextTime(), FeeRecipient: s.beneficiary,
		BeaconRoot: new(types.Hash), GasLimit: s.CurrentBlock().GasLimit(),
	}
}

func (s *Sim) commit(pool block.TxPoolReader, attrs block.BuildBlockAttributes, mutate func(*types.Header, *types.Body)) (*BlockResult, error) {
	builder := block.NewBlockBuilder(s.cfg, s.bc, pool)
	blk, _, err := builder.BuildBlock(s.CurrentBlock().Header(), &attrs)
	if err != nil {
		return nil, fmt.Errorf("eiptest: BuildBlock: %w", err)
	}
	if mutate != nil {
		h, body := blk.Header(), blk.Body()
		mutate(h, body)
		blk = types.NewBlock(h, body)
	}
	result, err := s.bc.InsertBlockWithResult(blk)
	if err != nil {
		if mutate != nil {
			return nil, err
		}
		encoded, _ := blk.EncodeRLP()
		return nil, fmt.Errorf("eiptest: InsertBlock %d (%s): %w\nblock RLP (%d bytes, first 4096): %x", blk.NumberU64(), blk.Hash().Hex(), err, len(encoded), encoded[:min(len(encoded), 4096)])
	}
	if result == nil {
		return nil, fmt.Errorf("eiptest: block %s already known", blk.Hash().Hex())
	}
	s.now = blk.Time()
	s.syncPool(blk)
	return blockResult(s, blk, result), nil
}

func blockResult(s *Sim, blk *types.Block, result *execution.ProcessResult) *BlockResult {
	r := &BlockResult{Sim: s, Block: blk, Receipts: result.Receipts, BAL: result.BlockAccessList, Requests: result.Requests}
	for _, receipt := range r.Receipts {
		r.Logs = append(r.Logs, receipt.Logs...)
	}
	return r
}

func (s *Sim) resetPool() {
	if s.pool == nil {
		return
	}
	head := s.CurrentBlock()
	s.pool.SetForkRules(s.cfg, head.Time())
	s.pool.Reset(&poolStateReader{sim: s})
	s.pool.SetBaseFee(head.Header().BaseFee)
}

func (s *Sim) syncPool(blk *types.Block) {
	if s.pool != nil {
		s.resetPool()
		s.pool.RecordBlock(blk.Header(), blk.Transactions())
	}
}

// BuildSideBlock inserts a branch block at or below the current head height.
func (s *Sim) BuildSideBlock(parent *types.Block, timestamp uint64, txs ...*types.Transaction) (*types.Block, error) {
	if parent == nil || parent.NumberU64() >= s.CurrentBlock().NumberU64() || s.bc.GetBlock(parent.Hash()) == nil {
		return nil, fmt.Errorf("eiptest: side block needs a known parent below the head")
	}
	attrs := s.attributes()
	attrs.Timestamp, attrs.GasLimit = timestamp, parent.GasLimit()
	builder := block.NewBlockBuilder(s.cfg, s.bc, &slicePool{txs: txs})
	blk, _, err := builder.BuildBlock(parent.Header(), &attrs)
	if err != nil {
		return nil, err
	}
	if err := s.bc.InsertBlock(blk); err != nil {
		return nil, err
	}
	return blk, nil
}

// Finalize marks a canonical block final and refreshes pool admission state.
func (s *Sim) Finalize(blk *types.Block) error {
	if blk == nil {
		return fmt.Errorf("eiptest: cannot finalize a nil block")
	}
	canonical := s.bc.GetBlockByNumber(blk.NumberU64())
	if canonical == nil || canonical.Hash() != blk.Hash() {
		return fmt.Errorf("eiptest: cannot finalize a non-canonical block")
	}
	if final := s.bc.CurrentFinalBlock(); final != nil && final.NumberU64() > blk.NumberU64() {
		return fmt.Errorf("eiptest: finalization cannot move backward")
	}
	s.bc.SetFinalized(blk)
	s.resetPool()
	return nil
}

// Reorg switches the canonical chain and resubmits transactions orphaned by
// the old branch that still meet the new head's pool admission rules.
func (s *Sim) Reorg(head *types.Block) error {
	if head == nil || s.bc.GetBlock(head.Hash()) == nil {
		return chain.ErrHeadBlockUnknown
	}
	old := s.CurrentBlock()
	if old.Hash() == head.Hash() {
		return nil
	}
	common := chain.FindCommonAncestor(s.bc, old, head)
	if common == nil {
		return chain.ErrCommonAncestorNotFound
	}
	if final := s.bc.CurrentFinalBlock(); final != nil && final.NumberU64() > common.NumberU64() {
		return chain.ErrReorgPastFinalized
	}
	var removed []*types.Block
	for blk := old; blk.Hash() != common.Hash(); blk = s.bc.GetBlock(blk.ParentHash()) {
		removed = append(removed, blk)
	}
	if err := s.bc.Reorg(head); err != nil {
		return err
	}
	s.now = head.Time()
	s.resetPool()
	if s.pool != nil {
		for i := len(removed) - 1; i >= 0; i-- {
			for _, tx := range removed[i].Transactions() {
				_ = s.pool.AddRemote(tx)
			}
		}
	}
	return nil
}

type slicePool struct{ txs []*types.Transaction }

func (p *slicePool) Pending() []*types.Transaction { return p.txs }

// CommitInvalid inserts the supplied transactions directly so the builder cannot
// drop an invalid transaction before validation. A nil error indicates acceptance.
func (s *Sim) CommitInvalid(txs ...*types.Transaction) error {
	return s.TryBlock(func(h *types.Header, body *types.Body) {
		body.Transactions = txs
		h.TxHash = block.DeriveTxsRoot(txs)
	})
}

// TryBlock builds an empty candidate, applies mutate, and returns its validation error.
func (s *Sim) TryBlock(mutate func(*types.Header, *types.Body)) error {
	_, err := s.commit(nil, s.attributes(), mutate)
	return err
}

// ---------------------------------------------------------------------------
// State accessors
// ---------------------------------------------------------------------------

// State returns the post-head state.
func (s *Sim) State() state.StateDB {
	st, err := s.bc.StateAtBlock(s.bc.CurrentBlock())
	if err != nil {
		s.fail(fmt.Errorf("eiptest: state at head: %w", err))
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
		s.fail(fmt.Errorf("eiptest: no canonical block at %d", n))
	}
	return blk
}

// CommitWithWithdrawals builds an empty block carrying beacon withdrawals
// (EIP-4895) and inserts it.
func (s *Sim) CommitWithWithdrawals(wds ...*types.Withdrawal) *BlockResult {
	s.helper()
	attrs := s.attributes()
	attrs.Withdrawals = wds
	r, err := s.commit(nil, attrs, nil)
	if err != nil {
		s.fail(err)
	}
	return r
}

// CommitWithBeaconRoot builds and inserts a block with the supplied beacon root.
func (s *Sim) CommitWithBeaconRoot(root types.Hash, txs ...*types.Transaction) *BlockResult {
	s.helper()
	attrs := s.attributes()
	attrs.BeaconRoot = &root
	r, err := s.commit(&slicePool{txs: txs}, attrs, nil)
	if err != nil {
		s.fail(err)
	}
	return r
}

// Contracts exposes the solc-compiled scenario contracts (pkg/eiptest/contracts).
func (s *Sim) Contracts() contracts.Named { return contracts.Named{} }
