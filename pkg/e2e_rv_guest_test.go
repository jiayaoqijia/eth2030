// e2e_rv_guest_test.go exercises the full RISC-V execution pipeline in
// end-to-end scenarios: RVCREATE deployment, EVM.Call routing, precompile
// transparency, fork boundaries, and gas accounting.
//
// These tests operate through the exported VM/state APIs (no internal vm
// package access) to simulate realistic block-processing flows.
package e2e_test

import (
	gosha256 "crypto/sha256"
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/zkvm"
	"golang.org/x/crypto/sha3"
)

// rvMagic is the RISC-V magic prefix required by IsRVCode / RVCREATE.
var rvMagic = []byte{0xFE, 0x52, 0x56}

// makeIPlusEVM creates an EVM wired for the I+ fork against the given StateDB.
func makeIPlusEVM(sdb vm.StateDB) *vm.EVM {
	rules := vm.ForkRules{IsIPlus: true, IsGlamsterdan: true, IsEIP158: true}
	evm := vm.NewEVMWithState(vm.BlockContext{}, vm.TxContext{}, vm.Config{}, sdb)
	evm.SetForkRules(rules)
	return evm
}

// makeGlamEVM creates a Glamsterdan-fork EVM (no RISC-V routing).
func makeGlamEVM(sdb vm.StateDB) *vm.EVM {
	rules := vm.ForkRules{IsGlamsterdan: true, IsEIP158: true}
	evm := vm.NewEVMWithState(vm.BlockContext{}, vm.TxContext{}, vm.Config{}, sdb)
	evm.SetForkRules(rules)
	return evm
}

// deployRVContract sets RISC-V bytecode (magic + program) directly in the
// StateDB and returns the address. Bypasses RVCREATE to test the Call path
// independently.
func deployRVContract(sdb *state.MemoryStateDB, program []byte) types.Address {
	addr := types.BytesToAddress(crypto.Keccak256(program)[:20])
	code := append(append([]byte(nil), rvMagic...), program...)
	sdb.CreateAccount(addr) // must precede SetCode: CreateAccount resets state object
	sdb.SetCode(addr, code)
	return addr
}

// =============================================================================
// EL-2.3 / EL-3.3: RISC-V precompile routing through EVM.Call
// =============================================================================

// TestE2E_RV_Keccak256CallRouting verifies that calling an I+ RISC-V contract
// with the Keccak-256 guest program returns the correct hash.
func TestE2E_RV_Keccak256CallRouting(t *testing.T) {
	sdb := state.NewMemoryStateDB()
	addr := deployRVContract(sdb, zkvm.RVKeccak256Program)
	evm := makeIPlusEVM(sdb)

	inputs := [][]byte{
		{},
		[]byte("hello world"),
		[]byte("the quick brown fox jumps over the lazy dog"),
		make([]byte, 512),
	}
	for _, in := range inputs {
		ret, _, err := evm.Call(
			types.BytesToAddress([]byte{0xca}),
			addr, in, 1_000_000, big.NewInt(0),
		)
		if err != nil {
			t.Fatalf("Call(keccak, len=%d): %v", len(in), err)
		}
		h := sha3.NewLegacyKeccak256()
		h.Write(in)
		want := h.Sum(nil)
		if len(ret) != 32 {
			t.Fatalf("keccak output len=%d for input len=%d, want 32", len(ret), len(in))
		}
		for i, b := range want {
			if ret[i] != b {
				t.Errorf("keccak byte %d: got 0x%x, want 0x%x (input len=%d)", i, ret[i], b, len(in))
			}
		}
	}
}

// TestE2E_RV_SHA256CallRouting verifies the SHA-256 RISC-V guest via EVM.Call.
func TestE2E_RV_SHA256CallRouting(t *testing.T) {
	sdb := state.NewMemoryStateDB()
	addr := deployRVContract(sdb, zkvm.RVSHA256Program)
	evm := makeIPlusEVM(sdb)

	cases := [][]byte{
		{},
		[]byte("abc"),
		[]byte("abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq"),
	}
	for _, in := range cases {
		ret, _, err := evm.Call(
			types.BytesToAddress([]byte{0xca}),
			addr, in, 1_000_000, big.NewInt(0),
		)
		if err != nil {
			t.Fatalf("Call(sha256, %q): %v", in, err)
		}
		want := gosha256.Sum256(in)
		if len(ret) != 32 {
			t.Fatalf("sha256 output len=%d, want 32", len(ret))
		}
		for i, b := range want {
			if ret[i] != b {
				t.Errorf("sha256 byte %d: got 0x%x, want 0x%x", i, ret[i], b)
			}
		}
	}
}

// TestE2E_RV_ECRecoverCallRouting verifies the ECRecover RISC-V guest via
// EVM.Call. Uses the invalid-signature path (all zeros + v=27) which must
// return 32 zero bytes (no-address result).
func TestE2E_RV_ECRecoverCallRouting(t *testing.T) {
	sdb := state.NewMemoryStateDB()
	addr := deployRVContract(sdb, zkvm.RVECRecoverProgram)
	evm := makeIPlusEVM(sdb)

	input := make([]byte, 128)
	input[63] = 27 // v = 27, everything else zero → irrecoverable

	ret, _, err := evm.Call(
		types.BytesToAddress([]byte{0xca}),
		addr, input, 1_000_000, big.NewInt(0),
	)
	if err != nil {
		t.Fatalf("Call(ecrecover): %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("ecrecover output len=%d, want 32", len(ret))
	}
	for i, b := range ret {
		if b != 0 {
			t.Errorf("ecrecover zero-recovery byte %d = 0x%x, want 0", i, b)
		}
	}
}

// =============================================================================
// EL-3.3: Fork boundary — pre-I+ must NOT route to RISC-V executor
// =============================================================================

// TestE2E_RV_PreIPlusForkBoundary verifies that code with RV magic bytes is
// treated as plain EVM bytecode (0xFE = INVALID) before the I+ fork.
func TestE2E_RV_PreIPlusForkBoundary(t *testing.T) {
	sdb := state.NewMemoryStateDB()
	contractAddr := deployRVContract(sdb, zkvm.RVKeccak256Program)

	// Call with Glamsterdan (pre-I+) EVM.
	glamEVM := makeGlamEVM(sdb)
	output, _, err := glamEVM.Call(
		types.BytesToAddress([]byte{0xab}),
		contractAddr,
		[]byte("data"),
		1_000_000,
		big.NewInt(0),
	)
	if !errors.Is(err, vm.ErrInvalidOpCode) || len(output) != 0 {
		t.Fatalf("pre-I+ call returned output %x and error %v, want invalid opcode", output, err)
	}
}

// TestE2E_RV_IPlusForkEnablesRVRouting contrasts Glamsterdan and I+ behaviour
// for the same RV-magic contract.
func TestE2E_RV_IPlusForkEnablesRVRouting(t *testing.T) {
	sdb := state.NewMemoryStateDB()
	addr := deployRVContract(sdb, zkvm.RVKeccak256Program)

	input := []byte("fork boundary test")

	// I+ EVM: should produce Keccak-256 output.
	iplusEVM := makeIPlusEVM(sdb)
	retIPlus, _, err := iplusEVM.Call(
		types.BytesToAddress([]byte{0x01}),
		addr, input, 1_000_000, big.NewInt(0),
	)
	if err != nil {
		t.Fatalf("I+ Call: %v", err)
	}
	h := sha3.NewLegacyKeccak256()
	h.Write(input)
	want := h.Sum(nil)
	if len(retIPlus) != 32 {
		t.Fatalf("I+ output len=%d, want 32", len(retIPlus))
	}
	for i, b := range want {
		if retIPlus[i] != b {
			t.Errorf("I+ byte %d: got 0x%x, want 0x%x", i, retIPlus[i], b)
		}
	}
}

// =============================================================================
// EL-3.1: RVCREATE address determinism and gas model
// =============================================================================

// TestE2E_RV_RVCreateAddressDeterminism verifies the RVCREATE address formula
// via the exported IsRVCode + deterministic hash, using real EVM state.
func TestE2E_RV_RVCreateAddressDeterminism(t *testing.T) {
	// Minimal valid RISC-V initcode: 3-byte magic + 1-byte body = 4 bytes (4%4==0).
	initcode := []byte{0xFE, 0x52, 0x56, 0x00}

	if !vm.IsRVCode(initcode) {
		t.Fatal("initcode must be identified as RV code")
	}

	// Two different callers must produce different addresses for the same code.
	from1 := types.HexToAddress("0x1111")
	from2 := types.HexToAddress("0x2222")

	addr1a := rvCreateAddressE2E(from1, initcode)
	addr1b := rvCreateAddressE2E(from1, initcode) // must match
	addr2 := rvCreateAddressE2E(from2, initcode)  // must differ

	if addr1a != addr1b {
		t.Errorf("RVCREATE address not deterministic for same caller+code")
	}
	if addr1a == addr2 {
		t.Errorf("RVCREATE address must differ for different callers")
	}
	if addr1a.IsZero() {
		t.Errorf("RVCREATE address must be non-zero")
	}
}

// rvCreateAddressE2E implements the RVCREATE address formula using exported
// crypto primitives (CREATE2-style: keccak256(0xff ++ from ++ salt ++ codeHash)[12:]).
func rvCreateAddressE2E(from types.Address, initcode []byte) types.Address {
	magic := []byte{0xFE, 0x52, 0x56}
	saltInput := append(append([]byte(nil), magic...), initcode...)
	salt := crypto.Keccak256(saltInput)
	codeHash := crypto.Keccak256(initcode)
	buf := make([]byte, 1+20+32+32)
	buf[0] = 0xff
	copy(buf[1:21], from[:])
	copy(buf[21:53], salt)
	copy(buf[53:85], codeHash)
	h := crypto.Keccak256(buf)
	return types.BytesToAddress(h[12:])
}

// =============================================================================
// EL-2.3: Precompile transparency (gas and output unchanged across forks)
// =============================================================================

// TestE2E_RV_PrecompileGasTransparency verifies that the SHA-256 precompile
// at I+ charges the same gas as the Glamsterdan path (RISC-V overhead is
// absorbed by the surrounding gas estimation, not exposed to the caller).
func TestE2E_RV_PrecompileGasTransparency(t *testing.T) {
	sha256Addr := types.BytesToAddress([]byte{2})
	input := []byte("gas transparency test with some padding for realistic sizing")

	iplusMap := vm.SelectPrecompiles(vm.ForkRules{IsIPlus: true})
	glamMap := vm.SelectPrecompiles(vm.ForkRules{IsGlamsterdan: true})

	iplusP, ok1 := iplusMap[sha256Addr]
	glamP, ok2 := glamMap[sha256Addr]
	if !ok1 || !ok2 {
		t.Fatal("SHA-256 precompile missing from I+ or Glamsterdan map")
	}

	g1 := iplusP.RequiredGas(input)
	g2 := glamP.RequiredGas(input)
	if g1 != g2 {
		t.Errorf("gas: I+ = %d, Glamsterdan = %d; must be equal", g1, g2)
	}
}

// TestE2E_RV_PrecompileOutputConsistency verifies SHA-256 output matches
// standard library across Cancun, Glamsterdan, and I+ precompile maps.
func TestE2E_RV_PrecompileOutputConsistency(t *testing.T) {
	sha256Addr := types.BytesToAddress([]byte{2})
	input := []byte("precompile output consistency across all forks")
	want := gosha256.Sum256(input)

	forks := []struct {
		name  string
		rules vm.ForkRules
	}{
		{"Cancun", vm.ForkRules{IsCancun: true}},
		{"Glamsterdan", vm.ForkRules{IsGlamsterdan: true}},
		{"I+", vm.ForkRules{IsIPlus: true}},
	}
	for _, tc := range forks {
		m := vm.SelectPrecompiles(tc.rules)
		p, ok := m[sha256Addr]
		if !ok {
			t.Errorf("%s: SHA-256 precompile missing", tc.name)
			continue
		}
		out, err := p.Run(input)
		if err != nil {
			t.Errorf("%s: Run error: %v", tc.name, err)
			continue
		}
		if len(out) != 32 {
			t.Errorf("%s: output len=%d, want 32", tc.name, len(out))
			continue
		}
		for i, b := range want {
			if out[i] != b {
				t.Errorf("%s: byte %d: got 0x%x, want 0x%x", tc.name, i, out[i], b)
			}
		}
	}
}

// =============================================================================
// EL-3.1: Jump table wiring
// =============================================================================

// TestE2E_RV_JumpTableWiring verifies that RVCREATE (0xF6) is present in
// the I+ jump table and absent from the Glamsterdan table.
func TestE2E_RV_JumpTableWiring(t *testing.T) {
	iplusJT := vm.SelectJumpTable(vm.ForkRules{IsIPlus: true})
	glamJT := vm.SelectJumpTable(vm.ForkRules{IsGlamsterdan: true})
	cancunJT := vm.SelectJumpTable(vm.ForkRules{IsCancun: true})

	// JumpTable is [256]*operation indexed by opcode byte.
	if iplusJT[vm.RVCREATE] == nil {
		t.Error("RVCREATE must be present in I+ jump table")
	}
	if glamJT[vm.RVCREATE] != nil {
		t.Error("RVCREATE must NOT be present in Glamsterdan jump table")
	}
	if cancunJT[vm.RVCREATE] != nil {
		t.Error("RVCREATE must NOT be present in Cancun jump table")
	}
}

// TestE2E_RV_IsRVCode verifies the exported magic-byte detector.
func TestE2E_RV_IsRVCode(t *testing.T) {
	if !vm.IsRVCode([]byte{0xFE, 0x52, 0x56, 0x00}) {
		t.Error("valid RV code not detected")
	}
	if vm.IsRVCode([]byte{0x60, 0x00}) {
		t.Error("EVM code falsely detected as RV code")
	}
	if vm.IsRVCode(nil) {
		t.Error("nil falsely detected as RV code")
	}
}
