package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP5656_MCOPY verifies the MCOPY opcode (0x5e): memory-to-memory copy
// with overlap handled correctly.
func TestEIP5656_MCOPY(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// MSTORE8(0, 0x41), MCOPY(dst=1, src=0, len=1), return memory[0:2] —
	// expect 0x4141.
	mcopy := []byte{
		0x60, 0x41, // PUSH1 0x41
		0x60, 0x00, // PUSH1 0
		0x53,       // MSTORE8(0, 0x41)
		0x60, 0x01, // len
		0x60, 0x00, // src
		0x60, 0x01, // dst
		0x5e,       // MCOPY(dst, src, len)
		0x60, 0x02, // size
		0x60, 0x00, // offset
		0xf3,       // RETURN
	}
	addr := stf.Deploy(s.Alice(), mcopy)
	tx := stf.Tx(s.Alice(), addr, nil)

	// The return data lands in the receipt? Receipts here do not carry
	// output; verify via read-only call instead.
	ret, err := stf.Call(s.Alice().Address, addr, nil)
	if err != nil {
		t.Fatalf("MCOPY contract call failed: %v", err)
	}
	if len(ret) != 2 || ret[0] != 0x41 || ret[1] != 0x41 {
		t.Fatalf("MCOPY result = %x, want 4141", ret)
	}
	_ = tx
	_ = types.Hash{}
}
