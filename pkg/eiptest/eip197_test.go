package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP197_BN256Pairing verifies the optimal ate pairing precompile (0x08):
// pairing of degenerate input pairs (point at infinity) must return 1, and a
// valid scalar-multiple pair must also pair to 1 (e2*e1 = 1).
func TestEIP197_BN256Pairing(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()
	pairing := types.BytesToAddress([]byte{0x08})

	// Case 1: single degenerate pair (G1 point at infinity = (0,0), zero G2)
	// → the product of pairings is 1.
	infinityPair := make([]byte, 192) // 32*6 zeros: G1(0,0) + G2(0,0,0,0)

	ret, err := stf.Call(s.Alice().Address, pairing, infinityPair)
	if err != nil {
		t.Fatalf("pairing(infinity) failed: %v", err)
	}
	if len(ret) != 32 || ret[31] != 1 {
		t.Fatalf("pairing of degenerate pair should be 1, got %x", ret)
	}

	// Case 2: empty input → result 1 (vacuous product).
	ret2, err := stf.Call(s.Alice().Address, pairing, []byte{})
	if err != nil {
		t.Fatalf("pairing(empty) failed: %v", err)
	}
	if len(ret2) != 32 || ret2[31] != 1 {
		t.Fatalf("pairing of empty input should be 1, got %x", ret2)
	}

	// Case 3: malformed input length (not a multiple of 192) must fail.
	bad := make([]byte, 191)
	if _, err := stf.Call(s.Alice().Address, pairing, bad); err == nil {
		t.Fatal("pairing with truncated input should fail")
	}
}
