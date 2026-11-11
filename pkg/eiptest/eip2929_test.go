package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP2929_ColdWarmAccess verifies EIP-2929 via observable gas: within one
// transaction a second SLOAD of the same slot costs 100 (warm) instead of
// 2100 (cold).
func TestEIP2929_ColdWarmAccess(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// Contract: two SLOADs of slot 0 then return.
	twoLoads := []byte{
		0x60, 0x00, // PUSH1 0
		0x54,       // SLOAD (cold: 2100)
		0x50,       // POP
		0x60, 0x00, // PUSH1 0
		0x54, // SLOAD (warm: 100)
		0x50, // POP
		0x00, // STOP
	}
	addr := stf.Deploy(s.Alice(), twoLoads)
	withWarm := stf.Tx(s.Alice(), addr, nil)

	// One SLOAD only.
	oneLoad := []byte{
		0x60, 0x00,
		0x54,
		0x50,
		0x00,
	}
	addr2 := stf.Deploy(s.Alice(), oneLoad)
	withoutWarm := stf.Tx(s.Alice(), addr2, nil)

	// diff = warm SLOAD (100) + extra PUSH1 (2) + extra POP (3) = 105.
	diff := withWarm.GasUsed() - withoutWarm.GasUsed()
	if diff != 105 {
		t.Fatalf("warm SLOAD surcharge = %d, want 105 (100 warm + 5 bytecode)", diff)
	}
}
