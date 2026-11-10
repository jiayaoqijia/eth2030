package vm

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// TestForkPrecompileRouterGetPrecompileCancun verifies that the router returns
// the correct precompile for Cancun fork rules.
func TestForkPrecompileRouterGetPrecompileCancun(t *testing.T) {
	r := NewForkPrecompileRouter()
	rules := ForkRules{IsCancun: true}

	// ecrecover at 0x01 should be present.
	ecAddr := types.BytesToAddress([]byte{0x01})
	p, ok := r.GetPrecompile(ecAddr, rules)
	if !ok {
		t.Fatal("expected ecrecover precompile at 0x01 for Cancun")
	}
	if p == nil {
		t.Fatal("expected non-nil precompile")
	}

	// Verify RequiredGas returns the known ecrecover cost.
	if gas := p.RequiredGas(nil); gas != 3000 {
		t.Fatalf("ecrecover gas: got %d, want 3000", gas)
	}
}

// TestForkPrecompileRouterGetPrecompileGlamsterdan verifies that the router
// returns the Glamsterdan-repriced precompiles.
func TestForkPrecompileRouterGetPrecompileGlamsterdan(t *testing.T) {
	r := NewForkPrecompileRouter()
	rules := ForkRules{IsGlamsterdan: true, IsCancun: true}

	// bn256Add at 0x06 should have the Glamsterdan gas cost.
	ecAddAddr := types.BytesToAddress([]byte{0x06})
	p, ok := r.GetPrecompile(ecAddAddr, rules)
	if !ok {
		t.Fatal("expected bn256Add precompile at 0x06 for Glamsterdan")
	}
	gas := p.RequiredGas(nil)
	if gas != GasECADDGlamsterdan {
		t.Fatalf("bn256Add gas: got %d, want %d", gas, GasECADDGlamsterdan)
	}
}

// TestForkPrecompileRouterActivePrecompiles checks that ActivePrecompiles
// returns a sorted list of correct length.
func TestForkPrecompileRouterActivePrecompiles(t *testing.T) {
	r := NewForkPrecompileRouter()
	rules := ForkRules{IsCancun: true}

	addrs := r.ActivePrecompiles(rules)
	if len(addrs) == 0 {
		t.Fatal("expected non-empty precompile list for Cancun")
	}

	// Verify sorted order.
	for i := 1; i < len(addrs); i++ {
		if !routerAddrLess(addrs[i-1], addrs[i]) {
			t.Fatalf("addresses not sorted at index %d: %s >= %s",
				i, addrs[i-1].Hex(), addrs[i].Hex())
		}
	}
}

// TestForkPrecompileRouterGlamsterdanRemoves0x12 verifies that address 0x12
// (bls12MapFpToG1) is not present in the Glamsterdan set per EIP-7997.
func TestForkPrecompileRouterGlamsterdanRemoves0x12(t *testing.T) {
	r := NewForkPrecompileRouter()
	rules := ForkRules{IsGlamsterdan: true, IsCancun: true}

	addr12 := types.BytesToAddress([]byte{0x12})
	if r.IsActive(addr12, rules) {
		t.Fatal("address 0x12 should not be active in Glamsterdan")
	}

	// But it should be active in Cancun.
	cancunRules := ForkRules{IsCancun: true, LegacyNativeBLS: true}
	if !r.IsActive(addr12, cancunRules) {
		t.Fatal("address 0x12 should be active in Cancun")
	}
}

// TestForkPrecompileRouterIsActive checks IsActive for known addresses.
func TestForkPrecompileRouterIsActive(t *testing.T) {
	r := NewForkPrecompileRouter()
	rules := ForkRules{IsCancun: true, LegacyNativeBLS: true}

	tests := []struct {
		addr   types.Address
		active bool
	}{
		{types.BytesToAddress([]byte{0x01}), true},  // ecrecover
		{types.BytesToAddress([]byte{0x0a}), true},  // pointEval
		{types.BytesToAddress([]byte{0x0b}), true},  // bls12G1Add
		{types.BytesToAddress([]byte{0x13}), true},  // bls12MapFp2ToG2
		{types.BytesToAddress([]byte{0x14}), false}, // nonexistent
		{types.BytesToAddress([]byte{0xff}), false}, // nonexistent
	}

	for _, tt := range tests {
		got := r.IsActive(tt.addr, rules)
		if got != tt.active {
			t.Errorf("IsActive(%s): got %v, want %v", tt.addr.Hex(), got, tt.active)
		}
	}
}

// TestForkPrecompileRouterExecute tests executing a precompile through the router.
func TestForkPrecompileRouterExecute(t *testing.T) {
	r := NewForkPrecompileRouter()
	rules := ForkRules{IsCancun: true}

	// Execute the identity (dataCopy) precompile at 0x04.
	identityAddr := types.BytesToAddress([]byte{0x04})
	input := []byte{0xde, 0xad, 0xbe, 0xef}
	gas := uint64(1000)

	output, gasLeft, err := r.Execute(identityAddr, rules, input, gas)
	if err != nil {
		t.Fatalf("Execute identity: %v", err)
	}
	if len(output) != len(input) {
		t.Fatalf("output length: got %d, want %d", len(output), len(input))
	}
	for i := range input {
		if output[i] != input[i] {
			t.Fatalf("output byte %d: got 0x%02x, want 0x%02x", i, output[i], input[i])
		}
	}

	// Identity precompile cost: 15 + 3*1 = 18 (4 bytes = 1 word).
	expectedGas := uint64(15 + 3*1)
	if gasLeft != gas-expectedGas {
		t.Fatalf("gasLeft: got %d, want %d", gasLeft, gas-expectedGas)
	}
}

// TestForkPrecompileRouterExecuteOutOfGas tests that Execute returns
// ErrOutOfGas when insufficient gas is provided.
func TestForkPrecompileRouterExecuteOutOfGas(t *testing.T) {
	r := NewForkPrecompileRouter()
	rules := ForkRules{IsCancun: true}

	ecAddr := types.BytesToAddress([]byte{0x01})
	_, _, err := r.Execute(ecAddr, rules, nil, 100) // ecrecover needs 3000
	if err != ErrOutOfGas {
		t.Fatalf("expected ErrOutOfGas, got: %v", err)
	}
}

// TestForkPrecompileRouterExecuteNotFound tests that Execute returns an error
// for a nonexistent precompile address.
func TestForkPrecompileRouterExecuteNotFound(t *testing.T) {
	r := NewForkPrecompileRouter()
	rules := ForkRules{IsCancun: true}

	badAddr := types.BytesToAddress([]byte{0xff})
	_, _, err := r.Execute(badAddr, rules, nil, 10000)
	if err != ErrRouterNoPrecompile {
		t.Fatalf("expected ErrRouterNoPrecompile, got: %v", err)
	}
}

// TestForkPrecompileRouterGasCost tests the GasCost method.
func TestForkPrecompileRouterGasCost(t *testing.T) {
	r := NewForkPrecompileRouter()
	rules := ForkRules{IsCancun: true}

	ecAddr := types.BytesToAddress([]byte{0x01})
	gas, err := r.GasCost(ecAddr, rules, nil)
	if err != nil {
		t.Fatalf("GasCost: %v", err)
	}
	if gas != 3000 {
		t.Fatalf("GasCost for ecrecover: got %d, want 3000", gas)
	}

	// Nonexistent address should return error.
	_, err = r.GasCost(types.BytesToAddress([]byte{0xff}), rules, nil)
	if err != ErrRouterNoPrecompile {
		t.Fatalf("expected ErrRouterNoPrecompile, got: %v", err)
	}
}

// TestForkPrecompileRouterCount verifies the count of active precompiles.
func TestForkPrecompileRouterCount(t *testing.T) {
	r := NewForkPrecompileRouter()

	cancunCount := r.Count(ForkRules{IsCancun: true})
	glamCount := r.Count(ForkRules{IsGlamsterdan: true, IsCancun: true})

	// Glamsterdan retains the seven standard BLS contracts activated at Prague.
	if glamCount-cancunCount != 7 {
		t.Fatalf("expected seven BLS additions, got %d", glamCount-cancunCount)
	}
}

// TestForkPrecompileRouterRegisterFork tests registering a custom fork.
func TestForkPrecompileRouterRegisterFork(t *testing.T) {
	r := NewForkPrecompileRouter()

	// Register a minimal fork with only ecrecover.
	customMap := map[types.Address]PrecompiledContract{
		types.BytesToAddress([]byte{0x01}): &ecrecover{},
	}
	r.RegisterFork("TestFork", customMap)

	// The custom fork isn't auto-selected by ForkRules, so count with
	// default Cancun rules should not show the custom fork.
	cancunCount := r.Count(ForkRules{IsCancun: true})
	if cancunCount < 2 {
		t.Fatal("Cancun should have more than 1 precompile")
	}
}
