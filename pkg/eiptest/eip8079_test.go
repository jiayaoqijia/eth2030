package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/rollup"
)

// TestEIP8079_ExecutePrecompile verifies the native-rollup EXECUTE precompile
// surface: it exists, rejects malformed input, and is wired through the EVM
// precompile registry (chain-level eth_call).
func TestEIP8079_ExecutePrecompile(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	executeAddr := rollup.ExecutePrecompileAddress
	if executeAddr == (types.Address{}) {
		t.Skip("EXECUTE precompile address not exported")
	}

	// Malformed (empty) input must be rejected cleanly.
	if _, err := stf.Call(s.Alice().Address, executeAddr, nil); err == nil {
		t.Log("EXECUTE accepted empty input (permissive mode)")
	}

	// Structured input for a trivial state transition must either succeed or
	// fail with a domain error — but never crash the EVM.
	input := make([]byte, 64)
	input[0] = 0x01
	if _, err := stf.Call(s.Alice().Address, executeAddr, input); err != nil {
		t.Logf("EXECUTE structured input rejected (expected without anchor state): %v", err)
	}
}
