package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP3529_RefundReduction verifies EIP-3529: SELFDESTRUCT no longer
// grants the 24,000 selfdestruct refund, so a selfdestructing tx's gasUsed
// reflects the raw execution cost with zero refund applied.
func TestEIP3529_RefundReduction(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// Contract that selfdestructs to Alice when called.
	destroy := []byte{
		0x73, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, // PUSH20 beneficiary
		0xff, // SELFDESTROY
	}
	copy(destroy[1:21], s.Alice().Address[:])
	addr := stf.Deploy(s.Alice(), destroy)

	// The tx that selfdestructs: base 21000 + warm target (beneficiary is
	// the sender) + 5000 destruct + PUSH20 (3) = 26003. The pre-EIP-3529
	// 24,000 selfdestruct refund would make this 2,003 — so the exact value
	// proves the refund is gone.
	r := stf.Tx(s.Alice(), addr, nil)
	if r.GasUsed() != 26003 {
		t.Fatalf("selfdestruct gasUsed = %d, want exactly 26003 under EIP-3529 (no refund)", r.GasUsed())
	}

	// Balance moves to the beneficiary; the code survives (EIP-6780 keeps
	// code/storage for pre-existing contracts on Cancun+).
	if code := s.Code(addr); len(code) == 0 {
		t.Fatal("selfdestructed pre-existing contract lost its code (EIP-6780 regression)")
	}
}
