package eiptest

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
	"github.com/eth2030/eth2030/core/state"
)

// STF is the state-transition probe: deploy contracts, send transactions and
// read-only calls against the simulated chain. Each Tx commits its own block.
type STF struct {
	s *Sim
}

// STF returns the state-transition probe.
func (s *Sim) STF() *STF { return &STF{s: s} }

// Deploy deploys runtime bytecode from the account and returns the created
// address. The runtime is wrapped in a minimal constructor (CODECOPY +
// RETURN) so callers pass plain runtime code. Commits one block.
func (p *STF) Deploy(from *Account, runtime []byte) types.Address {
	p.s.t.Helper()
	nonce := p.s.Nonce(from.Address)
	tx := UnsignedDynamicFee(
		from.Address, p.s.cfg.ChainID, nonce,
		nil, big.NewInt(0), wrapInitcode(runtime),
		1_000_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r := p.s.Commit(tx)
	txr := ExpectSuccess(p.s.t, r, 0)
	addr := txr.ContractAddress()
	if addr == (types.Address{}) {
		p.s.t.Fatalf("deployment produced empty contract address (code %x)", runtime)
	}
	return addr
}

// wrapInitcode builds constructor code that returns the given runtime:
//   PUSH2 len; PUSH2 hdrLen; PUSH1 0; CODECOPY; PUSH2 len; PUSH1 0; RETURN
const initcodeHeaderLen = 15

func wrapInitcode(runtime []byte) []byte {
	if len(runtime) > 0xffff {
		panic("eiptest: runtime too large for initcode wrapper")
	}
	l := len(runtime)
	ic := make([]byte, 0, initcodeHeaderLen+l)
	ic = append(ic, 0x61, byte(l>>8), byte(l))          // PUSH2 len
	ic = append(ic, 0x61, 0x00, initcodeHeaderLen)      // PUSH2 headerLen (src offset)
	ic = append(ic, 0x60, 0x00)                         // PUSH1 0 (dst)
	ic = append(ic, 0x39)                               // CODECOPY
	ic = append(ic, 0x61, byte(l>>8), byte(l))          // PUSH2 len
	ic = append(ic, 0x60, 0x00)                         // PUSH1 0
	ic = append(ic, 0xf3)                               // RETURN
	return append(ic, runtime...)
}

// Tx sends a transaction to `to` with call data and commits one block.
func (p *STF) Tx(from *Account, to types.Address, data []byte) *TxResult {
	p.s.t.Helper()
	nonce := p.s.Nonce(from.Address)
	tx := UnsignedDynamicFee(
		from.Address, p.s.cfg.ChainID, nonce,
		ToPtr(to), big.NewInt(0), data,
		1_000_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r := p.s.Commit(tx)
	return ExpectSuccess(p.s.t, r, 0)
}

// Transfer sends value and commits one block.
func (p *STF) Transfer(from *Account, to types.Address, wei *big.Int) *TxResult {
	p.s.t.Helper()
	nonce := p.s.Nonce(from.Address)
	tx := UnsignedDynamicFee(
		from.Address, p.s.cfg.ChainID, nonce,
		ToPtr(to), wei, nil,
		100_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r := p.s.Commit(tx)
	return ExpectSuccess(p.s.t, r, 0)
}

// Call performs a read-only EVM call (eth_call semantics) against the head
// state. It does not produce a block.
func (p *STF) Call(from types.Address, to types.Address, data []byte) ([]byte, error) {
	p.s.t.Helper()
	return p.CallAt(from, to, data, p.s.bc.CurrentBlock().Header().Root)
}

// CallAt performs the read-only call against the state at the given root.
func (p *STF) CallAt(from types.Address, to types.Address, data []byte, root types.Hash) ([]byte, error) {
	p.s.t.Helper()
	statedb, err := p.s.bc.StateAtRoot(root)
	if err != nil {
		return nil, err
	}
	return p.evmCall(statedb, from, to, data)
}

func (p *STF) evmCall(statedb state.StateDB, from types.Address, to types.Address, data []byte) ([]byte, error) {
	p.s.t.Helper()
	header := p.s.bc.CurrentBlock().Header()
	blockCtx := vm.BlockContext{
		GetHash:     p.s.bc.GetHashFn(),
		BlockNumber: header.Number,
		Time:        header.Time,
		Coinbase:    header.Coinbase,
		GasLimit:    header.GasLimit,
		BaseFee:     header.BaseFee,
		PrevRandao:  header.MixDigest,
	}
	txCtx := vm.TxContext{
		Origin:   from,
		GasPrice: header.BaseFee,
	}
	evm := vm.NewEVMWithState(blockCtx, txCtx, vm.Config{}, statedb)
	// Mirror the state processor: select jump table + precompiles from the
	// head header's fork rules.
	head := p.s.bc.CurrentBlock().Header()
	rules := p.s.cfg.Rules(head.Number, p.s.cfg.IsMerge(), head.Time)
	forkRules := vm.ForkRules{
		IsGlamsterdan:    rules.IsGlamsterdan,
		IsPrague:         rules.IsPrague,
		IsCancun:         rules.IsCancun,
		IsShanghai:       rules.IsShanghai,
		IsMerge:          rules.IsMerge,
		IsLondon:         rules.IsLondon,
		IsBerlin:         rules.IsBerlin,
		IsIstanbul:       rules.IsIstanbul,
		IsConstantinople: rules.IsConstantinople,
		IsByzantium:      rules.IsByzantium,
		IsHomestead:      rules.IsHomestead,
		IsEIP158:         rules.IsEIP158,
		IsEIP7708:        rules.IsEIP7708,
		IsEIP7954:        rules.IsEIP7954,
		IsIPlus:          rules.IsIPlus,
		LegacyNativeBLS:  rules.LegacyNativeBLS,
	}
	evm.SetJumpTable(vm.SelectJumpTable(forkRules))
	evm.SetPrecompiles(vm.SelectPrecompiles(forkRules))
	evm.SetForkRules(forkRules)
	ret, _, err := evm.StaticCall(from, to, data, 30_000_000)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

// ExpectCallSuccess asserts the read-only call returns without error.
func (p *STF) ExpectCallSuccess(t *testing.T, from types.Address, to types.Address, data []byte) []byte {
	t.Helper()
	ret, err := p.Call(from, to, data)
	if err != nil {
		t.Fatalf("call to %s failed: %v", to.Hex(), err)
	}
	return ret
}
