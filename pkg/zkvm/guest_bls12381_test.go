package zkvm

import (
	"testing"
)

func TestBuildBLS12G1AddGuest(t *testing.T) {
	prog := BuildBLS12G1AddGuest()
	if len(prog) == 0 {
		t.Fatal("expected non-empty program")
	}
	if len(prog)%4 != 0 {
		t.Fatal("program size must be multiple of 4")
	}
}

func TestBuildBLS12G2AddGuest(t *testing.T) {
	prog := BuildBLS12G2AddGuest()
	if len(prog) == 0 {
		t.Fatal("expected non-empty program")
	}
}

func TestBuildBLS12PairingGuest(t *testing.T) {
	prog := BuildBLS12PairingGuest()
	if len(prog) == 0 {
		t.Fatal("expected non-empty program")
	}
}

func TestBLS12G1AddGuestExecution(t *testing.T) {
	prog := BuildBLS12G1AddGuest()
	cpu := NewRVCPU(100000)
	cpu.InputBuf = make([]byte, 256) // two G1 points
	for i := range cpu.InputBuf {
		cpu.InputBuf[i] = byte(i)
	}
	if err := cpu.LoadProgram(prog, 0, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	cpu.Regs[2] = 0x80000000
	if err := cpu.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !cpu.Halted {
		t.Fatal("CPU should halt")
	}
	if cpu.ExitCode != 0 {
		t.Fatalf("exit code: %d", cpu.ExitCode)
	}
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestBLS12G2MulGuestExecution(t *testing.T) {
	prog := BuildBLS12G2MulGuest()
	cpu := NewRVCPU(100000)
	cpu.InputBuf = make([]byte, 288) // G2 point + scalar
	for i := range cpu.InputBuf {
		cpu.InputBuf[i] = byte(i + 1)
	}
	if err := cpu.LoadProgram(prog, 0, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	cpu.Regs[2] = 0x80000000
	if err := cpu.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !cpu.Halted || cpu.ExitCode != 0 {
		t.Fatalf("expected clean halt, got halted=%v exit=%d", cpu.Halted, cpu.ExitCode)
	}
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestBLS12PairingGuestExecution(t *testing.T) {
	prog := BuildBLS12PairingGuest()
	cpu := NewRVCPU(100000)
	cpu.InputBuf = make([]byte, 384) // one pairing pair
	for i := range cpu.InputBuf {
		cpu.InputBuf[i] = byte(i ^ 0xAA)
	}
	if err := cpu.LoadProgram(prog, 0, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	cpu.Regs[2] = 0x80000000
	if err := cpu.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !cpu.Halted || cpu.ExitCode != 0 {
		t.Fatalf("expected clean halt")
	}
	if len(cpu.OutputBuf) != 32 {
		t.Fatalf("expected 32 bytes output, got %d", len(cpu.OutputBuf))
	}
}

func TestRegisterBLS12381Guests(t *testing.T) {
	registry := NewGuestRegistry()
	ids, err := RegisterBLS12381Guests(registry)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(ids) != 9 {
		t.Fatalf("expected 9 guests, got %d", len(ids))
	}
	if registry.Count() != 9 {
		t.Fatalf("expected 9 in registry, got %d", registry.Count())
	}

	// Double registration should not error.
	ids2, err := RegisterBLS12381Guests(registry)
	if err != nil {
		t.Fatalf("second register: %v", err)
	}
	for name, id := range ids {
		if ids2[name] != id {
			t.Errorf("ID mismatch for %s on re-registration", name)
		}
	}
}

func TestRegisterBLS12381ZKISAOps(t *testing.T) {
	table := NewZKISAOpTable()
	before := table.Count()
	RegisterBLS12381ZKISAOps(table)
	after := table.Count()

	if after-before != 9 {
		t.Fatalf("expected 9 new ops, got %d", after-before)
	}

	// Verify a few lookups.
	entry, err := table.Lookup(ZKISAOpBLS12G1Add)
	if err != nil {
		t.Fatalf("lookup G1Add: %v", err)
	}
	if entry.Name != "bls12-g1add" {
		t.Errorf("expected name bls12-g1add, got %s", entry.Name)
	}
	if entry.BaseGas != zkisaGasBLS12G1Add {
		t.Errorf("expected gas %d, got %d", zkisaGasBLS12G1Add, entry.BaseGas)
	}

	entry, err = table.Lookup(ZKISAOpBLS12Pairing)
	if err != nil {
		t.Fatalf("lookup Pairing: %v", err)
	}
	if entry.PrecompileAddr != 0x11 {
		t.Errorf("expected precompile addr 0x11, got 0x%02x", entry.PrecompileAddr)
	}
}

func TestBLS12MapFpToG1GuestExecution(t *testing.T) {
	prog := BuildBLS12MapFpToG1Guest()
	cpu := NewRVCPU(100000)
	cpu.InputBuf = make([]byte, 64) // one Fp element
	cpu.InputBuf[0] = 0x42
	if err := cpu.LoadProgram(prog, 0, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	cpu.Regs[2] = 0x80000000
	if err := cpu.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !cpu.Halted || cpu.ExitCode != 0 {
		t.Fatal("expected clean halt")
	}
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestBLS12DeterministicOutput(t *testing.T) {
	prog := BuildBLS12G1AddGuest()
	input := []byte{1, 2, 3, 4, 5}

	run := func() []byte {
		cpu := NewRVCPU(100000)
		cpu.InputBuf = input
		if err := cpu.LoadProgram(prog, 0, 0); err != nil {
			t.Fatalf("load: %v", err)
		}
		cpu.Regs[2] = 0x80000000
		if err := cpu.Run(); err != nil {
			t.Fatalf("run: %v", err)
		}
		return cpu.OutputBuf
	}

	out1 := run()
	out2 := run()
	if len(out1) != len(out2) {
		t.Fatalf("output length mismatch: %d vs %d", len(out1), len(out2))
	}
	for i := range out1 {
		if out1[i] != out2[i] {
			t.Fatalf("output mismatch at byte %d", i)
		}
	}
}
