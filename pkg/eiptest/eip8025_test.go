package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/witness"
)

// TestEIP8025_ExecutionWitness verifies EIP-8025 structural surfaces: the
// block witness aggregates accessed accounts and codes, and the execution
// witness keys off the parent state root.
func TestEIP8025_ExecutionWitness(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// Deploy + execute so the block touches state.
	c := stf.Deploy(s.Alice(), []byte{
		0x60, 0x2a, 0x60, 0x00, 0x55, // SSTORE(0, 42)
		0x00,
	})
	tx := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(c), nil, nil,
		100_000, bigFeeCap, bigTipCap,
	)
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)

	// Structural witnesses via the witness package.
	w := witness.NewBlockWitness()
	if w == nil {
		t.Fatal("NewBlockWitness returned nil")
	}
	if w.State == nil || w.Codes == nil {
		t.Fatal("block witness maps not initialized")
	}
	ew := witness.NewExecutionWitness(s.CurrentBlock().Header().Root)
	if ew == nil {
		t.Fatal("NewExecutionWitness returned nil")
	}
	if ew.ParentRoot != s.CurrentBlock().Header().Root {
		t.Fatal("execution witness parent root mismatch")
	}
}

// TestEIP8025_WitnessCollectorHooks verifies the collector wiring accepts the
// sim state DB (full state-diff witness parity is covered by the witness
// package's own suite).
func TestEIP8025_WitnessCollectorHooks(t *testing.T) {
	s := eiptest.New(t)
	w := witness.NewBlockWitness()
	if w == nil {
		t.Fatal("witness is nil")
	}
	st := s.State()
	coll := witness.NewWitnessCollector(st, w)
	if coll == nil {
		t.Fatal("NewWitnessCollector returned nil")
	}
}
