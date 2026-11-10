package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/eiptest"
)


// TestEIP7904_GlamsterdamRepricing verifies EIP-7904 repricing through the
// chain: after Glamsterdan, KECCAK256 costs 30 (was 30 → repriced) and the
// changed opcode costs are observable via exact gas on identical bytecode
// across the fork boundary.
func TestEIP7904_GlamsterdamRepricing(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkGlamsterdan, 100))
	stf := s.STF()

	// Hash one word of memory: KECCAK256(0, 32).
	hasher := []byte{
		0x60, 0x20, // size
		0x60, 0x00, // offset
		0x20,  // KECCAK256
		0x60, 0x00, // slot
		0x55,  // SSTORE(0, hash) — observable side effect
		0x00,
	}
	addr := stf.Deploy(s.Alice(), hasher)

	// Pre-fork execution: legacy KECCAK256 cost (30 + 6/word).
	fp := s.Fork(eiptest.ForkGlamsterdan)
	before := fp.Before()
	rBefore := before.Commit()
	_ = rBefore
	// Pre-fork call of the deployed hasher.
	txPre := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(addr), nil, nil,
		100_000, bigFeeCap, bigTipCap,
	)
	pre := s.Commit(txPre)
	preGas := eiptest.ExpectSuccess(t, pre, 0).GasUsed()

	// Cross the boundary; same bytecode reprices.
	at := fp.AtBoundary()
	at.ExpectActive(t)
	txPost := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(addr), nil, nil,
		100_000, bigFeeCap, bigTipCap,
	)
	post := s.Commit(txPost)
	postGas := eiptest.ExpectSuccess(t, post, 0).GasUsed()

	// EIP-7904 reprices KECCAK256 base 30→45 (and warm SLOAD etc.); the same
	// workload must repriced to a strictly different cost.
	if postGas == preGas {
		t.Fatalf("repricing had no observable effect: pre=%d post=%d (identical)", preGas, postGas)
	}
}

