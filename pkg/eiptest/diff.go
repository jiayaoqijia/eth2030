package eiptest

import (
	"bytes"
	"math/big"
	"testing"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethstate "github.com/ethereum/go-ethereum/core/state"

	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/geth"
)

// DiffProbe replays the same blocks in the native and Geth processors.
type DiffProbe struct{ s *Sim }

func (s *Sim) Diff() *DiffProbe { return &DiffProbe{s: s} }

// PreAccountView is the initial account state for both processors.
type PreAccountView = geth.PreAccount

// SnapshotAccounts captures balance, nonce, code, and storage at the head.
func (s *Sim) SnapshotAccounts(t *testing.T, addrs ...types.Address) map[types.Address]*PreAccountView {
	t.Helper()
	st := s.State()
	snapshot, ok := st.(interface {
		StorageSnapshot(types.Address) map[types.Hash]types.Hash
	})
	if !ok {
		t.Fatalf("state %T cannot snapshot account storage", st)
	}
	out := make(map[types.Address]*PreAccountView, len(addrs))
	for _, addr := range addrs {
		if st.Exist(addr) {
			out[addr] = &PreAccountView{
				Balance: new(big.Int).Set(st.GetBalance(addr)), Nonce: st.GetNonce(addr),
				Code: bytes.Clone(st.GetCode(addr)), Storage: snapshot.StorageSnapshot(addr),
			}
		}
	}
	return out
}

// DiffBlock compares execution results for one block.
func (d *DiffProbe) DiffBlock(t *testing.T, blk *types.Block, accounts map[types.Address]*PreAccountView) {
	t.Helper()
	d.DiffBlocks(t, accounts, blk)
}

// DiffBlocks compares state roots and receipts after each block of a
// contiguous sequence. The caller supplies the complete pre-state.
func (d *DiffProbe) DiffBlocks(t *testing.T, accounts map[types.Address]*PreAccountView, blocks ...*types.Block) {
	t.Helper()
	if len(blocks) == 0 {
		t.Fatal("diff: no blocks supplied")
	}
	native := state.NewMemoryStateDB()
	for addr, account := range accounts {
		native.CreateAccount(addr)
		if account.Balance != nil {
			native.AddBalance(addr, account.Balance)
		}
		native.SetNonce(addr, account.Nonce)
		if len(account.Code) > 0 {
			native.SetCode(addr, account.Code)
		}
		for slot, value := range account.Storage {
			native.SetState(addr, slot, value)
		}
	}
	if _, err := native.Commit(); err != nil {
		t.Fatal(err)
	}
	gstate := makeGethPreState(t, accounts)
	defer gstate.Close()
	gproc := geth.NewGethBlockProcessorWithEth2028(geth.ToGethChainConfig(d.s.cfg), d.s.cfg)
	nproc := execution.NewStateProcessor(d.s.cfg)
	nproc.SetGetHash(d.s.bc.GetHashFn())
	for i, blk := range blocks {
		if i > 0 && (blk.ParentHash() != blocks[i-1].Hash() || blk.NumberU64() != blocks[i-1].NumberU64()+1) {
			t.Fatalf("diff: block %d does not follow its predecessor", blk.NumberU64())
		}
		result, err := nproc.ProcessWithRequests(blk, native)
		if err != nil {
			t.Fatalf("block %d native: %v", blk.NumberU64(), err)
		}
		nroot, err := native.Commit()
		if err != nil {
			t.Fatal(err)
		}
		receipts, groot, err := gproc.ProcessBlock(gstate.StateDB, blk, d.getHash)
		if err != nil {
			t.Fatalf("block %d Geth: %v", blk.NumberU64(), err)
		}
		compareReceipts(t, blk.NumberU64(), result.Receipts, receipts)
		if nroot != groot || nroot != blk.Header().Root {
			t.Fatalf("block %d roots: native %s, Geth %s, header %s", blk.NumberU64(), nroot.Hex(), groot.Hex(), blk.Header().Root.Hex())
		}
		if i+1 < len(blocks) {
			gstate.StateDB, err = gethstate.New(geth.ToGethHash(groot), gstate.StateDB.Database())
			if err != nil {
				t.Fatalf("open Geth state at block %d: %v", blk.NumberU64(), err)
			}
		}
	}
}

// DiffReceipts compares an already validated block's receipts against Geth.
func (d *DiffProbe) DiffReceipts(t *testing.T, blk *types.Block, accounts map[types.Address]*PreAccountView, native []*types.Receipt) {
	t.Helper()
	st := makeGethPreState(t, accounts)
	defer st.Close()
	proc := geth.NewGethBlockProcessorWithEth2028(geth.ToGethChainConfig(d.s.cfg), d.s.cfg)
	receipts, _, err := proc.ProcessBlock(st.StateDB, blk, d.getHash)
	if err != nil {
		t.Fatalf("block %d Geth: %v", blk.NumberU64(), err)
	}
	compareReceipts(t, blk.NumberU64(), native, receipts)
}

func (d *DiffProbe) getHash(n uint64) gethcommon.Hash {
	return geth.ToGethHash(d.s.bc.GetHashFn()(n))
}

func makeGethPreState(t *testing.T, accounts map[types.Address]*PreAccountView) *geth.StateTestState {
	t.Helper()
	pre := make(map[string]geth.PreAccount, len(accounts))
	for addr, account := range accounts {
		pre[addr.Hex()] = *account
	}
	st, err := geth.MakePreState(pre)
	if err != nil {
		t.Fatalf("Geth pre-state: %v", err)
	}
	return st
}

func compareReceipts(t *testing.T, block uint64, native, geth []*types.Receipt) {
	t.Helper()
	if len(native) != len(geth) {
		t.Fatalf("block %d receipt count: native %d, Geth %d", block, len(native), len(geth))
	}
	for i := range native {
		n, g := native[i], geth[i]
		if n.GasUsed != g.GasUsed || n.ContractAddress != g.ContractAddress {
			t.Fatalf("block %d tx %d: native gas/address %d/%s, Geth %d/%s", block, i, n.GasUsed, n.ContractAddress.Hex(), g.GasUsed, g.ContractAddress.Hex())
		}
		nr, nerr := n.EncodeRLP()
		gr, gerr := g.EncodeRLP()
		if nerr != nil || gerr != nil || !bytes.Equal(nr, gr) {
			t.Fatalf("block %d tx %d receipt: native %x (%v), Geth %x (%v)", block, i, nr, nerr, gr, gerr)
		}
	}
}
