package eiptest

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
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
	txr := p.execute(from, nil, nil, wrapInitcode(runtime), 1_000_000).ExpectStatus(p.s.t, types.ReceiptStatusSuccessful)
	addr := txr.ContractAddress()
	if addr == (types.Address{}) {
		p.s.t.Fatalf("deployment produced empty contract address (code %x)", runtime)
	}
	return addr
}

// wrapInitcode builds constructor code that returns the given runtime:
//
//	PUSH2 len; PUSH2 hdrLen; PUSH1 0; CODECOPY; PUSH2 len; PUSH1 0; RETURN
const initcodeHeaderLen = 15

func wrapInitcode(runtime []byte) []byte {
	if len(runtime) > 0xffff {
		panic("eiptest: runtime too large for initcode wrapper")
	}
	l := len(runtime)
	ic := make([]byte, 0, initcodeHeaderLen+l)
	ic = append(ic, 0x61, byte(l>>8), byte(l))     // PUSH2 len
	ic = append(ic, 0x61, 0x00, initcodeHeaderLen) // PUSH2 headerLen (src offset)
	ic = append(ic, 0x60, 0x00)                    // PUSH1 0 (dst)
	ic = append(ic, 0x39)                          // CODECOPY
	ic = append(ic, 0x61, byte(l>>8), byte(l))     // PUSH2 len
	ic = append(ic, 0x60, 0x00)                    // PUSH1 0
	ic = append(ic, 0xf3)                          // RETURN
	return append(ic, runtime...)
}

// Tx sends a call transaction and requires successful execution.
func (p *STF) Tx(from *Account, to types.Address, data []byte) *TxResult {
	p.s.t.Helper()
	return p.Execute(from, to, data).ExpectStatus(p.s.t, types.ReceiptStatusSuccessful)
}

// Execute sends a call transaction and exposes its receipt, including reverts.
func (p *STF) Execute(from *Account, to types.Address, data []byte) *TxResult {
	p.s.t.Helper()
	return p.execute(from, &to, nil, data, 1_000_000)
}

// Transfer sends value and requires successful execution.
func (p *STF) Transfer(from *Account, to types.Address, wei *big.Int) *TxResult {
	p.s.t.Helper()
	return p.execute(from, &to, wei, nil, 100_000).ExpectStatus(p.s.t, types.ReceiptStatusSuccessful)
}

func (p *STF) execute(from *Account, to *types.Address, value *big.Int, data []byte, gas uint64) *TxResult {
	p.s.t.Helper()
	tip := GweiWei(1)
	fee := new(big.Int).Add(p.s.CurrentBlock().Header().BaseFee, tip)
	tx := UnsignedDynamicFee(from.Address, p.s.cfg.ChainID, p.s.Nonce(from.Address), to, value, data, gas, fee, tip)
	return p.s.Commit(tx).Tx(0)
}

// Call simulates an eth_call at the head and discards any state changes.
func (p *STF) Call(from types.Address, to types.Address, data []byte) ([]byte, error) {
	return p.callAtBlock(from, to, data, p.s.CurrentBlock())
}

// CallAt simulates an eth_call at the latest canonical block with root.
// Use CallAtBlock to disambiguate equal roots at different block heights.
func (p *STF) CallAt(from types.Address, to types.Address, data []byte, root types.Hash) ([]byte, error) {
	for n := p.s.CurrentBlock().NumberU64(); ; n-- {
		blk := p.s.bc.GetBlockByNumber(n)
		if blk != nil && blk.Header().Root == root {
			return p.callAtBlock(from, to, data, blk)
		}
		if n == 0 {
			return nil, fmt.Errorf("eiptest: no canonical block with root %s", root.Hex())
		}
	}
}

// CallAtBlock simulates an eth_call at a canonical block number.
func (p *STF) CallAtBlock(from, to types.Address, data []byte, n uint64) ([]byte, error) {
	blk := p.s.bc.GetBlockByNumber(n)
	if blk == nil {
		return nil, fmt.Errorf("eiptest: no canonical block %d", n)
	}
	return p.callAtBlock(from, to, data, blk)
}

func (p *STF) callAtBlock(from, to types.Address, data []byte, blk *types.Block) ([]byte, error) {
	statedb, err := p.s.bc.StateAtBlock(blk)
	if err != nil {
		return nil, err
	}
	return p.evmCall(statedb.Dup(), from, to, data, blk.Header())
}

func (p *STF) evmCall(statedb state.StateDB, from, to types.Address, data []byte, header *types.Header) ([]byte, error) {
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
	rules := p.s.cfg.Rules(header.Number, p.s.cfg.IsMerge(), header.Time)
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
	ret, _, err := evm.Call(from, to, data, header.GasLimit, new(big.Int))
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
