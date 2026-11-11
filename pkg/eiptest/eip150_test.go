package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// pushAddress returns PUSH20 <addr> bytes.
func pushAddress(addr types.Address) []byte {
	out := []byte{0x73}
	return append(out, addr[:]...)
}

// callPreamble builds the CALL argument pushes (target, value, sizes, gas).
// The VM reads the gas argument from the stack TOP, so it is pushed last.
func callPreamble(target types.Address, gasPUSH []byte) []byte {
	out := []byte{
		0x60, 0x00, // retSize
		0x60, 0x00, // retOffset
		0x60, 0x00, // argsSize
		0x60, 0x00, // argsOffset
		0x60, 0x00, // value
	}
	out = append(out, pushAddress(target)...)
	return append(out, gasPUSH...)
}

// logLeftover appends: MSTORE(0, GAS); LOG0(0, 32); STOP.
func logLeftover() []byte {
	return []byte{
		0x5a,       // leftover L
		0x60, 0x00, // offset 0
		0x52,       // MSTORE(0, L)
		0x60, 0x20, // size 32
		0x60, 0x00, // offset 0
		0xa0, // LOG0
		0x00, // STOP
	}
}

// TestEIP150_CallGasAllButOne64th verifies EIP-150 through its observable
// integration property: a caller that forwards ALL its gas (GAS argument on
// top of the stack) to an out-of-gas callee still retains 1/64 of the gas,
// so the caller survives and logs the leftover instead of running out of gas.
func TestEIP150_CallGasAllButOne64th(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// Infinite loop: PUSH1 0; JUMPDEST; JUMP.
	burn := stf.Deploy(s.Alice(), []byte{0x60, 0x00, 0x5b, 0x56})

	caller := append(callPreamble(burn, []byte{0x5a}), logLeftover()...) // gas = GAS
	callerAddr := stf.Deploy(s.Alice(), caller)

	r := stf.Tx(s.Alice(), callerAddr, nil)
	if len(r.Logs()) == 0 {
		t.Fatal("caller produced no log: 1/64 gas retention violated (full-gas-forwarding regression?)")
	}
	leftover := r.Logs()[0].Data
	if len(leftover) != 32 || allZero32(leftover) {
		t.Fatalf("caller retained no gas after forwarding (leftover %x): EIP-150 1/64 rule violated", leftover)
	}
}

func allZero32(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
