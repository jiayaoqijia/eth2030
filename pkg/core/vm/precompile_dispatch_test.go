package vm

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNewPrecompileDispatchRouter(t *testing.T) {
	fm := NewPrecompileForkManager()
	router := NewPrecompileDispatchRouter(fm)
	if router == nil {
		t.Fatal("expected non-nil router")
	}
	if router.IsRISCVActive() {
		t.Error("RISC-V should be inactive by default")
	}
	if router.ZkISACount() != 0 {
		t.Errorf("expected 0 zkISA precompiles, got %d", router.ZkISACount())
	}
}

func TestDispatchRouterSetRISCVActive(t *testing.T) {
	fm := NewPrecompileForkManager()
	router := NewPrecompileDispatchRouter(fm)

	router.SetRISCVActive(true)
	if !router.IsRISCVActive() {
		t.Error("expected RISC-V active")
	}
	router.SetRISCVActive(false)
	if router.IsRISCVActive() {
		t.Error("expected RISC-V inactive")
	}
}

func TestDispatchRouterRegisterZkISA(t *testing.T) {
	fm := NewPrecompileForkManager()
	router := NewPrecompileDispatchRouter(fm)

	addr := types.BytesToAddress([]byte{0x01})
	router.RegisterZkISA(addr, &ZkISAEcrecover{})

	if !router.HasZkISA(addr) {
		t.Error("expected zkISA registered at 0x01")
	}
	if router.ZkISACount() != 1 {
		t.Errorf("expected 1 zkISA, got %d", router.ZkISACount())
	}
}

func TestDispatchRouterRunNative(t *testing.T) {
	fm := NewPrecompileForkManager()
	router := NewPrecompileDispatchRouter(fm)

	// RISC-V inactive -> should use native Go path.
	sha256Addr := types.BytesToAddress([]byte{0x02})
	input := []byte("hello")
	output, gas, err := router.Run(sha256Addr, input, 100000)
	if err != nil {
		t.Fatalf("native run: %v", err)
	}
	if len(output) != 32 {
		t.Fatalf("expected 32 bytes SHA256, got %d", len(output))
	}
	if gas >= 100000 {
		t.Error("expected gas consumption")
	}
}

func TestDispatchRouterRunZkISA(t *testing.T) {
	fm := NewPrecompileForkManager()
	router := NewPrecompileDispatchRouter(fm)

	// Register zkISA ecrecover and activate RISC-V.
	ecAddr := types.BytesToAddress([]byte{0x02})
	router.RegisterZkISA(ecAddr, &ZkISASha256{})
	router.SetRISCVActive(true)

	input := []byte("test input for sha256")
	output, gas, err := router.Run(ecAddr, input, 100000)
	if err != nil {
		t.Fatalf("zkISA run: %v", err)
	}
	if len(output) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(output))
	}
	if gas >= 100000 {
		t.Error("expected gas consumption")
	}
}

func TestDispatchRouterRunOutOfGas(t *testing.T) {
	fm := NewPrecompileForkManager()
	router := NewPrecompileDispatchRouter(fm)

	addr := types.BytesToAddress([]byte{0x02})
	router.RegisterZkISA(addr, &ZkISASha256{})
	router.SetRISCVActive(true)

	// Provide very little gas.
	_, _, err := router.Run(addr, []byte("x"), 1)
	if err != ErrOutOfGas {
		t.Fatalf("expected ErrOutOfGas, got %v", err)
	}
}

func TestDispatchRouterFallbackWhenNoZkISA(t *testing.T) {
	fm := NewPrecompileForkManager()
	router := NewPrecompileDispatchRouter(fm)
	router.SetRISCVActive(true)

	// Even with RISC-V active, if no zkISA for address, fallback to native.
	sha256Addr := types.BytesToAddress([]byte{0x02})
	output, _, err := router.Run(sha256Addr, []byte("fallback"), 100000)
	if err != nil {
		t.Fatalf("fallback run: %v", err)
	}
	if len(output) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(output))
	}
}

func TestDispatchRouterRequiredGas(t *testing.T) {
	fm := NewPrecompileForkManager()
	router := NewPrecompileDispatchRouter(fm)

	addr := types.BytesToAddress([]byte{0x02})
	input := []byte("test")

	// Native path.
	gasNative := router.RequiredGas(addr, input)
	if gasNative == 0 {
		t.Error("expected non-zero gas for native sha256")
	}

	// zkISA path.
	router.RegisterZkISA(addr, &ZkISASha256{})
	router.SetRISCVActive(true)
	gasZkISA := router.RequiredGas(addr, input)
	if gasZkISA == 0 {
		t.Error("expected non-zero gas for zkISA sha256")
	}
}

func TestDispatchRouterIsActive(t *testing.T) {
	fm := NewPrecompileForkManager()
	router := NewPrecompileDispatchRouter(fm)

	sha256Addr := types.BytesToAddress([]byte{0x02})
	if !router.IsActive(sha256Addr) {
		t.Error("SHA256 should be active in Glamsterdan")
	}

	// Unknown address.
	unknownAddr := types.BytesToAddress([]byte{0xFF})
	if router.IsActive(unknownAddr) {
		t.Error("unknown address should not be active")
	}

	// zkISA-registered address when RISC-V active.
	router.RegisterZkISA(unknownAddr, &ZkISASha256{})
	router.SetRISCVActive(true)
	if !router.IsActive(unknownAddr) {
		t.Error("zkISA-registered address should be active when RISC-V is on")
	}
}

func TestRegisterIPlusPrecompiles(t *testing.T) {
	fm := NewPrecompileForkManager()
	RegisterIPlusPrecompiles(fm)

	fps, err := fm.GetForkSet(ForkIPlus)
	if err != nil {
		t.Fatalf("get I+ set: %v", err)
	}

	// Should have Glamsterdan precompiles + NTT.
	nttAddr := types.BytesToAddress([]byte{0x15})
	if !fps.IsActive(nttAddr) {
		t.Error("NTT should be active in I+")
	}

	// ecrecover should still be active.
	ecAddr := types.BytesToAddress([]byte{0x01})
	if !fps.IsActive(ecAddr) {
		t.Error("ecrecover should be active in I+")
	}
}

func TestNewIPlusDispatchRouter(t *testing.T) {
	fm := NewPrecompileForkManager()
	router := NewIPlusDispatchRouter(fm)

	if !router.IsRISCVActive() {
		t.Error("I+ router should have RISC-V active")
	}

	// Should have base 6 + 9 BLS12 + 4 misc = 19 zkISA precompiles.
	if router.ZkISACount() < 15 {
		t.Errorf("expected >= 15 zkISA precompiles, got %d", router.ZkISACount())
	}

	// Check some specific addresses.
	if !router.HasZkISA(types.BytesToAddress([]byte{0x01})) {
		t.Error("expected zkISA at ecrecover 0x01")
	}
	if !router.HasZkISA(types.BytesToAddress([]byte{0x0b})) {
		t.Error("expected zkISA at BLS12 G1Add 0x0b")
	}
	if !router.HasZkISA(types.BytesToAddress([]byte{0x03})) {
		t.Error("expected zkISA at ripemd160 0x03")
	}
	if !router.HasZkISA(types.BytesToAddress([]byte{0x09})) {
		t.Error("expected zkISA at blake2f 0x09")
	}
}

func TestGenericZkISAPrecompileExecute(t *testing.T) {
	p := &genericZkISAPrecompile{
		addr:     types.BytesToAddress([]byte{0x02}),
		name:     "test-sha256",
		fixedGas: 1000,
	}

	output, err := p.Execute([]byte("hello"))
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(output) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(output))
	}
}

func TestGenericZkISAPrecompileProveExecution(t *testing.T) {
	p := &genericZkISAPrecompile{
		addr:     types.BytesToAddress([]byte{0x02}),
		name:     "test-sha256",
		fixedGas: 1000,
	}

	proof, err := p.ProveExecution([]byte("hello"))
	if err != nil {
		t.Fatalf("prove: %v", err)
	}
	if proof == nil {
		t.Fatal("expected non-nil proof")
	}
	if proof.PrecompileAddr != p.addr {
		t.Error("proof addr mismatch")
	}
	if len(proof.Witness) == 0 {
		t.Error("expected non-empty witness")
	}
	if proof.StepCount != 128 {
		t.Errorf("expected 128 steps, got %d", proof.StepCount)
	}
}

func TestDispatchRouterNativeAndZkISAConsistency(t *testing.T) {
	fm := NewPrecompileForkManager()

	// Run SHA256 natively.
	routerNative := NewPrecompileDispatchRouter(fm)
	sha256Addr := types.BytesToAddress([]byte{0x02})
	input := []byte("consistency test data")
	nativeOut, _, err := routerNative.Run(sha256Addr, input, 100000)
	if err != nil {
		t.Fatalf("native: %v", err)
	}

	// Run SHA256 via zkISA.
	routerZkISA := NewPrecompileDispatchRouter(fm)
	routerZkISA.RegisterZkISA(sha256Addr, &ZkISASha256{})
	routerZkISA.SetRISCVActive(true)
	zkisaOut, _, err := routerZkISA.Run(sha256Addr, input, 100000)
	if err != nil {
		t.Fatalf("zkISA: %v", err)
	}

	// Both should produce the same SHA256 output.
	if len(nativeOut) != len(zkisaOut) {
		t.Fatalf("output length mismatch: native=%d zkISA=%d", len(nativeOut), len(zkisaOut))
	}
	for i := range nativeOut {
		if nativeOut[i] != zkisaOut[i] {
			t.Fatalf("output mismatch at byte %d", i)
		}
	}
}
