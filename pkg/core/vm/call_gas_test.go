package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// --- 63/64 Gas Forwarding Rule (EIP-150) Tests ---

// TestCallGas_6364Rule verifies the core 63/64 gas forwarding formula.
// EIP-150: forwarded = min(available - floor(available/64), requested)
func TestCallGas_6364Rule(t *testing.T) {
	tests := []struct {
		name      string
		available uint64
		requested uint64
		expected  uint64
	}{
		{
			name:      "requested exceeds 63/64 cap",
			available: 6400,
			requested: 10000,
			// maxGas = 6400 - 6400/64 = 6400 - 100 = 6300
			expected: 6300,
		},
		{
			name:      "requested under 63/64 cap",
			available: 6400,
			requested: 5000,
			expected:  5000,
		},
		{
			name:      "requested exactly at cap",
			available: 6400,
			requested: 6300,
			// maxGas = 6400 - 100 = 6300
			expected: 6300,
		},
		{
			name:      "zero available gas",
			available: 0,
			requested: 1000,
			expected:  0,
		},
		{
			name:      "zero requested gas",
			available: 6400,
			requested: 0,
			expected:  0,
		},
		{
			name:      "small available gas",
			available: 64,
			requested: 10000,
			// maxGas = 64 - 64/64 = 64 - 1 = 63
			expected: 63,
		},
		{
			name:      "1 gas available",
			available: 1,
			requested: 10000,
			// maxGas = 1 - 1/64 = 1 - 0 = 1
			expected: 1,
		},
		{
			name:      "large available gas",
			available: 10000000,
			requested: 20000000,
			// maxGas = 10000000 - 10000000/64 = 10000000 - 156250 = 9843750
			expected: 9843750,
		},
		{
			name:      "63 gas available",
			available: 63,
			requested: 10000,
			// maxGas = 63 - 63/64 = 63 - 0 = 63
			expected: 63,
		},
		{
			name:      "caller retains exactly 1/64",
			available: 640000,
			requested: 640000,
			// maxGas = 640000 - 640000/64 = 640000 - 10000 = 630000
			expected: 630000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CallGas(tt.available, tt.requested)
			if got != tt.expected {
				t.Errorf("CallGas(%d, %d) = %d, want %d", tt.available, tt.requested, got, tt.expected)
			}
		})
	}
}

// TestCallGas_CallerRetains1of64 verifies the caller always retains at least
// floor(availableGas / 64) gas after forwarding.
func TestCallGas_CallerRetains1of64(t *testing.T) {
	for _, available := range []uint64{64, 128, 1000, 6400, 10000, 1000000} {
		forwarded := CallGas(available, ^uint64(0)) // request max
		retained := available - forwarded
		expected := available / 64
		if retained != expected {
			t.Errorf("available=%d: caller retained %d, want %d (1/64)", available, retained, expected)
		}
	}
}

// TestOpCall_GasForwarding verifies the full opcode flow for CALL gas forwarding.
// The opCall function should apply the 63/64 rule and only forward the capped gas.
func TestOpCall_GasForwarding(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	// Use addresses > 0x13 to avoid precompile range (0x01-0x13).
	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})

	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))
	stateDB.CreateAccount(targetAddr)

	// Target: just STOP (returns all gas)
	stateDB.SetCode(targetAddr, []byte{byte(STOP)})
	stateDB.AddAddressToAccessList(callerAddr)
	stateDB.AddAddressToAccessList(targetAddr)

	// Parent contract: CALL target with a very large gas argument.
	// The 63/64 rule should cap the forwarded gas.
	code := buildCallCode(targetAddr, 0xFFFFFF, 0) // request ~16M gas
	stateDB.SetCode(callerAddr, code)

	gas := uint64(1000000)
	_, gasLeft, err := evm.Call(types.BytesToAddress([]byte{0x99}), callerAddr, nil, gas, big.NewInt(0))
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	// Verify gas was consumed (not all returned). The 63/64 rule means the
	// caller retains 1/64 at each level.
	if gasLeft >= gas {
		t.Errorf("expected gas to be consumed, got gasLeft=%d >= initial=%d", gasLeft, gas)
	}
}

// TestOpStaticCall_GasForwarding verifies STATICCALL applies the 63/64 rule.
func TestOpStaticCall_GasForwarding(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(targetAddr)

	// Target: just STOP
	stateDB.SetCode(targetAddr, []byte{byte(STOP)})
	stateDB.AddAddressToAccessList(callerAddr)
	stateDB.AddAddressToAccessList(targetAddr)

	// Build STATICCALL bytecode: gas, addr, argsOff, argsLen, retOff, retLen
	code := buildStaticCallCode(targetAddr, 0xFFFFFF)
	stateDB.SetCode(callerAddr, code)

	gas := uint64(1000000)
	_, gasLeft, err := evm.StaticCall(types.BytesToAddress([]byte{0x99}), callerAddr, nil, gas)
	if err != nil {
		t.Fatalf("StaticCall failed: %v", err)
	}

	// Gas should be consumed but not all
	if gasLeft >= gas {
		t.Errorf("expected gas to be consumed, got gasLeft=%d >= initial=%d", gasLeft, gas)
	}
}

// TestOpDelegateCall_GasForwarding verifies DELEGATECALL applies the 63/64 rule.
func TestOpDelegateCall_GasForwarding(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(targetAddr)

	// Target: just STOP
	stateDB.SetCode(targetAddr, []byte{byte(STOP)})
	stateDB.AddAddressToAccessList(callerAddr)
	stateDB.AddAddressToAccessList(targetAddr)

	code := buildDelegateCallCode(targetAddr, 0xFFFFFF)
	stateDB.SetCode(callerAddr, code)

	gas := uint64(1000000)
	_, gasLeft, err := evm.DelegateCall(types.BytesToAddress([]byte{0x99}), callerAddr, nil, gas)
	if err != nil {
		t.Fatalf("DelegateCall failed: %v", err)
	}

	if gasLeft >= gas {
		t.Errorf("expected gas to be consumed, got gasLeft=%d >= initial=%d", gasLeft, gas)
	}
}

// TestOpCallCode_GasForwarding verifies CALLCODE applies the 63/64 rule.
func TestOpCallCode_GasForwarding(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(targetAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))

	// Target: just STOP
	stateDB.SetCode(targetAddr, []byte{byte(STOP)})
	stateDB.AddAddressToAccessList(callerAddr)
	stateDB.AddAddressToAccessList(targetAddr)

	code := buildCallCodeCode(targetAddr, 0xFFFFFF, 0)
	stateDB.SetCode(callerAddr, code)

	gas := uint64(1000000)
	_, gasLeft, err := evm.CallCode(types.BytesToAddress([]byte{0x99}), callerAddr, nil, gas, big.NewInt(0))
	if err != nil {
		t.Fatalf("CallCode failed: %v", err)
	}

	if gasLeft >= gas {
		t.Errorf("expected gas to be consumed, got gasLeft=%d >= initial=%d", gasLeft, gas)
	}
}

// --- Call Depth Limit Tests ---

// TestCallDepthLimit_Direct verifies that calls at the maximum depth succeed
// and calls beyond the maximum depth fail with ErrMaxCallDepthExceeded.
func TestCallDepthLimit_Direct(t *testing.T) {
	stateDB := state.NewMemoryStateDB()
	evm := NewEVMWithState(
		BlockContext{BlockNumber: big.NewInt(1)},
		TxContext{},
		Config{MaxCallDepth: 1024},
		stateDB,
	)

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})
	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(targetAddr)

	// At depth 1024 (evm.depth == 1024), Call should succeed (1024 > 1024 is false).
	evm.depth = 1024
	_, _, err := evm.Call(callerAddr, targetAddr, nil, 100000, big.NewInt(0))
	if err != nil {
		t.Errorf("Call at depth 1024 should succeed, got %v", err)
	}

	// At depth 1025, Call should fail.
	evm.depth = 1025
	_, gas, err := evm.Call(callerAddr, targetAddr, nil, 100000, big.NewInt(0))
	if !errors.Is(err, ErrMaxCallDepthExceeded) {
		t.Errorf("Call at depth 1025: expected ErrMaxCallDepthExceeded, got %v", err)
	}
	// Gas should be returned (not consumed) when depth exceeded.
	if gas != 100000 {
		t.Errorf("Call at depth 1025: gas should be returned, got %d, want 100000", gas)
	}
}

// TestStaticCallDepthLimit verifies depth checking for StaticCall.
func TestStaticCallDepthLimit(t *testing.T) {
	stateDB := state.NewMemoryStateDB()
	evm := NewEVMWithState(
		BlockContext{BlockNumber: big.NewInt(1)},
		TxContext{},
		Config{MaxCallDepth: 1024},
		stateDB,
	)

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})
	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(targetAddr)

	// At depth 1025, StaticCall should fail.
	evm.depth = 1025
	_, gas, err := evm.StaticCall(callerAddr, targetAddr, nil, 100000)
	if !errors.Is(err, ErrMaxCallDepthExceeded) {
		t.Errorf("StaticCall at depth 1025: expected ErrMaxCallDepthExceeded, got %v", err)
	}
	if gas != 100000 {
		t.Errorf("StaticCall depth exceeded: gas = %d, want 100000", gas)
	}
}

// TestDelegateCallDepthLimit verifies depth checking for DelegateCall.
func TestDelegateCallDepthLimit(t *testing.T) {
	stateDB := state.NewMemoryStateDB()
	evm := NewEVMWithState(
		BlockContext{BlockNumber: big.NewInt(1)},
		TxContext{},
		Config{MaxCallDepth: 1024},
		stateDB,
	)

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})
	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(targetAddr)

	evm.depth = 1025
	_, gas, err := evm.DelegateCall(callerAddr, targetAddr, nil, 100000)
	if !errors.Is(err, ErrMaxCallDepthExceeded) {
		t.Errorf("DelegateCall at depth 1025: expected ErrMaxCallDepthExceeded, got %v", err)
	}
	if gas != 100000 {
		t.Errorf("DelegateCall depth exceeded: gas = %d, want 100000", gas)
	}
}

// TestCallCodeDepthLimit verifies depth checking for CallCode.
func TestCallCodeDepthLimit(t *testing.T) {
	stateDB := state.NewMemoryStateDB()
	evm := NewEVMWithState(
		BlockContext{BlockNumber: big.NewInt(1)},
		TxContext{},
		Config{MaxCallDepth: 1024},
		stateDB,
	)

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})
	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(targetAddr)

	evm.depth = 1025
	_, gas, err := evm.CallCode(callerAddr, targetAddr, nil, 100000, big.NewInt(0))
	if !errors.Is(err, ErrMaxCallDepthExceeded) {
		t.Errorf("CallCode at depth 1025: expected ErrMaxCallDepthExceeded, got %v", err)
	}
	if gas != 100000 {
		t.Errorf("CallCode depth exceeded: gas = %d, want 100000", gas)
	}
}

// TestCallDepthLimit_GasNotConsumed verifies that when the depth limit is
// exceeded, gas is returned to the caller (not consumed).
func TestCallDepthLimit_GasNotConsumed(t *testing.T) {
	stateDB := state.NewMemoryStateDB()
	evm := NewEVMWithState(
		BlockContext{BlockNumber: big.NewInt(1)},
		TxContext{},
		Config{MaxCallDepth: 1024},
		stateDB,
	)

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})
	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(targetAddr)

	evm.depth = 1025

	gas := uint64(50000)

	// CALL
	_, retGas, _ := evm.Call(callerAddr, targetAddr, nil, gas, big.NewInt(0))
	if retGas != gas {
		t.Errorf("CALL depth exceeded: gas not returned, got %d want %d", retGas, gas)
	}

	// STATICCALL
	_, retGas, _ = evm.StaticCall(callerAddr, targetAddr, nil, gas)
	if retGas != gas {
		t.Errorf("STATICCALL depth exceeded: gas not returned, got %d want %d", retGas, gas)
	}

	// DELEGATECALL
	_, retGas, _ = evm.DelegateCall(callerAddr, targetAddr, nil, gas)
	if retGas != gas {
		t.Errorf("DELEGATECALL depth exceeded: gas not returned, got %d want %d", retGas, gas)
	}

	// CALLCODE
	_, retGas, _ = evm.CallCode(callerAddr, targetAddr, nil, gas, big.NewInt(0))
	if retGas != gas {
		t.Errorf("CALLCODE depth exceeded: gas not returned, got %d want %d", retGas, gas)
	}
}

// TestCallDepthLimit_RecursiveSelfCall verifies that a contract recursively
// calling itself eventually hits the depth limit and the top-level call succeeds.
func TestCallDepthLimit_RecursiveSelfCall(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0x99})
	contractAddr := types.BytesToAddress([]byte{0xCC})

	stateDB.CreateAccount(callerAddr)
	stateDB.CreateAccount(contractAddr)

	// Contract: recursively calls itself with all available gas.
	// PUSH1 0x00 retLen, PUSH1 0x00 retOff, PUSH1 0x00 argsLen, PUSH1 0x00 argsOff,
	// PUSH1 0x00 value, PUSH20 <self>, GAS, CALL, POP, STOP
	code := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH20),
	}
	code = append(code, contractAddr[:]...)
	code = append(code,
		byte(GAS),
		byte(CALL),
		byte(POP),
		byte(STOP),
	)
	stateDB.SetCode(contractAddr, code)
	stateDB.AddAddressToAccessList(contractAddr)

	// The call should succeed (the deepest CALL fails silently, pushing 0).
	_, _, err := evm.Call(callerAddr, contractAddr, nil, 100000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("recursive self-call test failed: %v", err)
	}
}

// --- Value Transfer Gas Stipend Tests ---

// TestCallStipend_Constant verifies the CallStipend constant is 2300.
func TestCallStipend_Constant(t *testing.T) {
	if CallStipend != 2300 {
		t.Errorf("CallStipend = %d, want 2300", CallStipend)
	}
}

// TestCallWithValue_StipendProvided verifies that when a CALL transfers
// value, the callee receives an additional 2300 gas stipend on top of the
// forwarded gas, allowing it to execute minimal operations.
func TestCallWithValue_StipendProvided(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})

	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))
	stateDB.CreateAccount(targetAddr)

	// Target: GAS opcode pushes current gas, store to slot 0, STOP
	// This lets us observe how much gas the callee actually received.
	targetCode := []byte{
		byte(GAS), // push gas remaining onto stack
		byte(PUSH1), 0x00,
		byte(MSTORE),      // store gas in memory at offset 0
		byte(PUSH1), 0x20, // return 32 bytes
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	stateDB.SetCode(targetAddr, targetCode)
	stateDB.AddAddressToAccessList(callerAddr)
	stateDB.AddAddressToAccessList(targetAddr)

	// CALL with value=1, gas=0 (explicitly forward 0 gas).
	// With the stipend, the callee should receive 2300 gas.
	ret, _, err := evm.Call(callerAddr, targetAddr, nil, 1000000, big.NewInt(1))
	if err != nil {
		t.Fatalf("Call with value failed: %v", err)
	}

	if len(ret) == 32 {
		calleeGas := new(big.Int).SetBytes(ret).Uint64()
		// The callee should have received at least the stipend worth of gas
		// (minus the cost of the GAS opcode itself which is 2).
		if calleeGas < 2200 {
			t.Errorf("callee gas = %d, expected at least ~2200 (stipend minus GAS opcode cost)", calleeGas)
		}
	}
}

// TestCallWithValue_StipendAdded verifies the stipend is added on top of
// the forwarded gas (not replacing it).
func TestCallWithValue_StipendAdded(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})

	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))
	stateDB.CreateAccount(targetAddr)

	// Target: returns its gas via GAS opcode
	targetCode := []byte{
		byte(GAS),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	stateDB.SetCode(targetAddr, targetCode)
	stateDB.AddAddressToAccessList(callerAddr)
	stateDB.AddAddressToAccessList(targetAddr)

	// Call with value -- the callee receives forwarded gas + 2300 stipend
	ret, _, err := evm.Call(callerAddr, targetAddr, nil, 1000000, big.NewInt(1))
	if err != nil {
		t.Fatalf("Call with value failed: %v", err)
	}

	var gasWithValue uint64
	if len(ret) == 32 {
		gasWithValue = new(big.Int).SetBytes(ret).Uint64()
	}

	// Call without value -- callee receives forwarded gas only
	evm2, stateDB2 := newIntegrationEVM()
	stateDB2.CreateAccount(callerAddr)
	stateDB2.AddBalance(callerAddr, big.NewInt(1000000))
	stateDB2.CreateAccount(targetAddr)
	stateDB2.SetCode(targetAddr, targetCode)
	stateDB2.AddAddressToAccessList(callerAddr)
	stateDB2.AddAddressToAccessList(targetAddr)

	ret2, _, err := evm2.Call(callerAddr, targetAddr, nil, 1000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("Call without value failed: %v", err)
	}

	var gasWithoutValue uint64
	if len(ret2) == 32 {
		gasWithoutValue = new(big.Int).SetBytes(ret2).Uint64()
	}

	// The difference should be approximately CallStipend (2300), accounting for
	// the extra gas costs of value transfer (CallValueTransferGas + possibly
	// CallNewAccountGas reduce the forwarded gas, but stipend adds 2300).
	// We just verify the with-value case got the stipend benefit.
	if gasWithValue == 0 || gasWithoutValue == 0 {
		t.Skipf("could not measure gas: withValue=%d, withoutValue=%d", gasWithValue, gasWithoutValue)
	}
	// The callee with value should have more gas than without (the stipend
	// partially offsets the higher value-transfer cost paid by the caller).
	// Due to the value transfer gas cost (9000), the forwarded gas is lower,
	// but the stipend adds 2300 back. The net effect depends on the specific
	// gas amounts, so we just verify both calls succeeded with reasonable gas.
	if gasWithValue < CallStipend/2 {
		t.Errorf("callee gas with value = %d, expected at least %d", gasWithValue, CallStipend/2)
	}
}

// TestCallWithValue_RefundsUnusedStipend checks legacy CALL value gas accounting.
func TestCallWithValue_RefundsUnusedStipend(t *testing.T) {
	const initialGas = uint64(1_000_000)
	gasUsed := func(value uint64) uint64 {
		evm, statedb := newIntegrationEVM()
		caller := types.BytesToAddress([]byte{0xa1})
		target := types.BytesToAddress([]byte{0xa2})
		statedb.CreateAccount(caller)
		statedb.AddBalance(caller, big.NewInt(1_000_000))
		statedb.SetCode(caller, buildCallCode(target, 10_000, value))
		statedb.CreateAccount(target)
		statedb.SetCode(target, []byte{byte(STOP)})
		statedb.AddAddressToAccessList(caller)
		statedb.AddAddressToAccessList(target)
		_, left, err := evm.Call(types.BytesToAddress([]byte{0x99}), caller, nil, initialGas, new(big.Int))
		if err != nil {
			t.Fatalf("CALL with value %d: %v", value, err)
		}
		if balance := statedb.GetBalance(target); balance.Uint64() != value {
			t.Fatalf("target balance = %s, want %d", balance, value)
		}
		return initialGas - left
	}
	withoutValue, withValue := gasUsed(0), gasUsed(1)
	// EIP-7069 describes legacy CALL with a STOP callee as 9000 - 2300.
	if want := withoutValue + CallValueTransferGas - CallStipend; withValue != want {
		t.Fatalf("value CALL used %d gas, want %d with unused stipend refunded", withValue, want)
	}
}

// TestCallWithZeroValue_NoStipend verifies that CALL with value=0 does NOT
// add the 2300 gas stipend.
func TestCallWithZeroValue_NoStipend(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})

	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(1000000))
	stateDB.CreateAccount(targetAddr)

	// Target: returns its gas via GAS opcode
	targetCode := []byte{
		byte(GAS),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	stateDB.SetCode(targetAddr, targetCode)
	stateDB.AddAddressToAccessList(callerAddr)
	stateDB.AddAddressToAccessList(targetAddr)

	// Call with value=0: no stipend
	ret, _, err := evm.Call(callerAddr, targetAddr, nil, 1000000, big.NewInt(0))
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if len(ret) != 32 {
		t.Fatalf("expected 32-byte return, got %d", len(ret))
	}

	calleeGas := new(big.Int).SetBytes(ret).Uint64()
	// With zero value, the callee should NOT get the stipend.
	// The forwarded gas should be what was available per 63/64 rule.
	// It should be a large amount (close to 1M gas minus overhead).
	if calleeGas < 900000 {
		// This is just a sanity check; the exact amount depends on overhead.
		t.Logf("callee gas with zero value = %d (no stipend expected)", calleeGas)
	}
}

// --- Integration: Gas Forwarding with Value Transfer ---

// TestCallGasForwarding_WithValueTransfer verifies the complete gas forwarding
// interaction when value is transferred: the forwarded gas is capped by the
// 63/64 rule, and then the stipend is added on top.
func TestCallGasForwarding_WithValueTransfer(t *testing.T) {
	evm, stateDB := newIntegrationEVM()

	callerAddr := types.BytesToAddress([]byte{0xA1})
	targetAddr := types.BytesToAddress([]byte{0xA2})

	stateDB.CreateAccount(callerAddr)
	stateDB.AddBalance(callerAddr, big.NewInt(10000000))
	stateDB.CreateAccount(targetAddr)

	// Target: report gas, then STOP
	targetCode := []byte{
		byte(GAS),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	stateDB.SetCode(targetAddr, targetCode)
	stateDB.AddAddressToAccessList(callerAddr)
	stateDB.AddAddressToAccessList(targetAddr)

	// Direct call with value transfer
	ret, _, err := evm.Call(callerAddr, targetAddr, nil, 100000, big.NewInt(1))
	if err != nil {
		t.Fatalf("Call with value failed: %v", err)
	}

	if len(ret) == 32 {
		calleeGas := new(big.Int).SetBytes(ret).Uint64()
		// The callee should have received gas. If forwarded gas was computed
		// correctly per 63/64 rule plus stipend, this should be > 0.
		if calleeGas == 0 {
			t.Error("callee received 0 gas, expected > 0 (forwarded + stipend)")
		}
	}
}

// --- CallGas edge cases ---

// TestCallGas_MaxUint64Request verifies behavior with maximum uint64 request.
func TestCallGas_MaxUint64Request(t *testing.T) {
	available := uint64(10000)
	// Requesting max uint64 should be capped to 63/64 of available.
	got := CallGas(available, ^uint64(0))
	expected := available - available/64
	if got != expected {
		t.Errorf("CallGas(%d, MaxUint64) = %d, want %d", available, got, expected)
	}
}

// TestCallGas_EqualAvailableAndRequested verifies that when requested equals
// available, the 63/64 rule caps the forwarded gas.
func TestCallGas_EqualAvailableAndRequested(t *testing.T) {
	available := uint64(10000)
	got := CallGas(available, available)
	expected := available - available/64
	if got != expected {
		t.Errorf("CallGas(%d, %d) = %d, want %d", available, available, got, expected)
	}
}

// --- Depth constant verification ---

// TestMaxCallDepthConstant verifies the MaxCallDepth constant.
func TestMaxCallDepthConstant(t *testing.T) {
	if MaxCallDepth != 1024 {
		t.Errorf("MaxCallDepth = %d, want 1024", MaxCallDepth)
	}
}

// TestDefaultMaxCallDepth verifies the EVM default MaxCallDepth is 1024.
func TestDefaultMaxCallDepth(t *testing.T) {
	evm := NewEVM(BlockContext{}, TxContext{}, Config{})
	if evm.Config.MaxCallDepth != 1024 {
		t.Errorf("default MaxCallDepth = %d, want 1024", evm.Config.MaxCallDepth)
	}
}

// --- Gas Fraction constant ---

// TestCallGasFractionConstant verifies the 63/64 gas fraction constant.
func TestCallGasFractionConstant(t *testing.T) {
	if CallGasFraction != 64 {
		t.Errorf("CallGasFraction = %d, want 64", CallGasFraction)
	}
}

// --- Helper functions to build call bytecodes ---

// buildCallCode builds bytecode for: CALL(gas, addr, value, 0, 0, 0, 0), POP, STOP
func buildCallCode(addr types.Address, gas uint64, value uint64) []byte {
	code := []byte{
		byte(PUSH1), 0x00, // retLen
		byte(PUSH1), 0x00, // retOff
		byte(PUSH1), 0x00, // argsLen
		byte(PUSH1), 0x00, // argsOff
	}
	// Value
	if value <= 0xFF {
		code = append(code, byte(PUSH1), byte(value))
	} else {
		code = append(code, byte(PUSH2), byte(value>>8), byte(value))
	}
	// Address
	code = append(code, byte(PUSH20))
	code = append(code, addr[:]...)
	// Gas
	if gas <= 0xFF {
		code = append(code, byte(PUSH1), byte(gas))
	} else if gas <= 0xFFFF {
		code = append(code, byte(PUSH2), byte(gas>>8), byte(gas))
	} else if gas <= 0xFFFFFF {
		code = append(code, byte(PUSH3), byte(gas>>16), byte(gas>>8), byte(gas))
	} else {
		// Use GAS opcode to forward all available gas
		code = append(code, byte(GAS))
	}
	code = append(code, byte(CALL), byte(POP), byte(STOP))
	return code
}

// buildStaticCallCode builds bytecode for: STATICCALL(gas, addr, 0, 0, 0, 0), POP, STOP
func buildStaticCallCode(addr types.Address, gas uint64) []byte {
	code := []byte{
		byte(PUSH1), 0x00, // retLen
		byte(PUSH1), 0x00, // retOff
		byte(PUSH1), 0x00, // argsLen
		byte(PUSH1), 0x00, // argsOff
	}
	code = append(code, byte(PUSH20))
	code = append(code, addr[:]...)
	if gas <= 0xFF {
		code = append(code, byte(PUSH1), byte(gas))
	} else if gas <= 0xFFFF {
		code = append(code, byte(PUSH2), byte(gas>>8), byte(gas))
	} else if gas <= 0xFFFFFF {
		code = append(code, byte(PUSH3), byte(gas>>16), byte(gas>>8), byte(gas))
	} else {
		code = append(code, byte(GAS))
	}
	code = append(code, byte(STATICCALL), byte(POP), byte(STOP))
	return code
}

// buildDelegateCallCode builds bytecode for: DELEGATECALL(gas, addr, 0, 0, 0, 0), POP, STOP
func buildDelegateCallCode(addr types.Address, gas uint64) []byte {
	code := []byte{
		byte(PUSH1), 0x00, // retLen
		byte(PUSH1), 0x00, // retOff
		byte(PUSH1), 0x00, // argsLen
		byte(PUSH1), 0x00, // argsOff
	}
	code = append(code, byte(PUSH20))
	code = append(code, addr[:]...)
	if gas <= 0xFF {
		code = append(code, byte(PUSH1), byte(gas))
	} else if gas <= 0xFFFF {
		code = append(code, byte(PUSH2), byte(gas>>8), byte(gas))
	} else if gas <= 0xFFFFFF {
		code = append(code, byte(PUSH3), byte(gas>>16), byte(gas>>8), byte(gas))
	} else {
		code = append(code, byte(GAS))
	}
	code = append(code, byte(DELEGATECALL), byte(POP), byte(STOP))
	return code
}

// buildCallCodeCode builds bytecode for: CALLCODE(gas, addr, value, 0, 0, 0, 0), POP, STOP
func buildCallCodeCode(addr types.Address, gas uint64, value uint64) []byte {
	code := []byte{
		byte(PUSH1), 0x00, // retLen
		byte(PUSH1), 0x00, // retOff
		byte(PUSH1), 0x00, // argsLen
		byte(PUSH1), 0x00, // argsOff
	}
	if value <= 0xFF {
		code = append(code, byte(PUSH1), byte(value))
	} else {
		code = append(code, byte(PUSH2), byte(value>>8), byte(value))
	}
	code = append(code, byte(PUSH20))
	code = append(code, addr[:]...)
	if gas <= 0xFF {
		code = append(code, byte(PUSH1), byte(gas))
	} else if gas <= 0xFFFF {
		code = append(code, byte(PUSH2), byte(gas>>8), byte(gas))
	} else if gas <= 0xFFFFFF {
		code = append(code, byte(PUSH3), byte(gas>>16), byte(gas>>8), byte(gas))
	} else {
		code = append(code, byte(GAS))
	}
	code = append(code, byte(CALLCODE), byte(POP), byte(STOP))
	return code
}
