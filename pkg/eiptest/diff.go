package eiptest

import (
	"math/big"
	"testing"

	gethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/geth"
)

// DiffProbe runs identical block sequences through the native eth2030 state
// processor and the go-ethereum-backed processor (pkg/geth) over an identical
// pre-state, and compares the resulting state roots. This is the
// differential-oracle surface: a divergence means one of the two EVMs (or the
// gas/receipt semantics between them) disagrees.
type DiffProbe struct {
	s *Sim
}

// Diff returns the differential probe.
func (s *Sim) Diff() *DiffProbe { return &DiffProbe{s: s} }

// PreAccountView is the framework's pre-state view of one account.
type PreAccountView struct {
	Balance *big.Int
	Nonce   uint64
	Code    []byte
	Storage map[types.Hash]types.Hash
}

// DiffBlock executes blk's transactions over the same pre-state through both
// backends and compares the resulting state roots. `accounts` defines the
// pre-state visible to both backends; it must cover every account the
// transactions touch (senders, recipients, contracts, coinbase).
func (d *DiffProbe) DiffBlock(t *testing.T, blk *types.Block, accounts map[types.Address]*PreAccountView) {
	t.Helper()

	// --- native execution ---
	nativeStatedb := state.NewMemoryStateDB()
	for addr, view := range accounts {
		nativeStatedb.AddBalance(addr, view.Balance)
		nativeStatedb.SetNonce(addr, view.Nonce)
		if len(view.Code) > 0 {
			nativeStatedb.SetCode(addr, view.Code)
		}
		for k, v := range view.Storage {
			nativeStatedb.SetState(addr, k, v)
		}
	}
	proc := execution.NewStateProcessor(d.s.cfg)
	if _, err := proc.ProcessWithBAL(blk, nativeStatedb); err != nil {
		t.Fatalf("native execution failed: %v", err)
	}
	nativeRoot := nativeStatedb.GetRoot()

	// --- geth execution ---
	pre := map[string]geth.PreAccount{}
	for addr, view := range accounts {
		pre[addr.Hex()[2:]] = geth.PreAccount{
			Balance: view.Balance, Nonce: view.Nonce, Code: view.Code, Storage: view.Storage,
		}
	}
	st, err := geth.MakePreState(pre)
	if err != nil {
		t.Fatalf("geth pre-state: %v", err)
	}
	defer st.Close()

	gcfg := geth.ToGethChainConfig(d.s.cfg)
	if gcfg == nil {
		t.Fatal("geth config mapping returned nil")
	}
	gproc := geth.NewGethBlockProcessorWithEth2028(gcfg, d.s.cfg)
	getHash := func(n uint64) gethcommon.Hash {
		return geth.ToGethHash(d.s.bc.GetHashFn()(n))
	}
	_, gethRoot, err := gproc.ProcessBlock(st.StateDB, blk, getHash)
	if err != nil {
		t.Fatalf("geth execution failed: %v", err)
	}

	if nativeRoot != gethRoot {
		t.Fatalf("state root divergence: native %s vs geth %s (block %d)",
			nativeRoot.Hex(), gethRoot.Hex(), blk.NumberU64())
	}
}

// SnapshotAccounts builds a PreAccountView map for the given addresses from
// the sim's current head state. Storage is included for contract accounts
// that expose iteration via the state DB; balance/nonce/code always are.
func (s *Sim) SnapshotAccounts(t *testing.T, addrs ...types.Address) map[types.Address]*PreAccountView {
	t.Helper()
	out := map[types.Address]*PreAccountView{}
	st := s.State()
	for _, addr := range addrs {
		out[addr] = &PreAccountView{
			Balance: new(big.Int).Set(st.GetBalance(addr)),
			Nonce:   st.GetNonce(addr),
			Code:    st.GetCode(addr),
			Storage: map[types.Hash]types.Hash{},
		}
	}
	return out
}
