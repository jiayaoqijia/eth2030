package zkvm

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestBuildNTTGuest(t *testing.T) {
	prog := BuildNTTGuest()
	if len(prog) == 0 || len(prog)%4 != 0 {
		t.Fatal("invalid program")
	}
}

func TestNTTGuestExecution(t *testing.T) {
	prog := BuildNTTGuest()

	// Build NTT input: op_type(1) + 2 coefficients(64 bytes)
	input := make([]byte, 65)
	input[0] = 0 // forward NTT, BN254
	for i := 1; i < 65; i++ {
		input[i] = byte(i)
	}

	cpu := NewRVCPU(500000)
	cpu.InputBuf = input
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
	// Output should be 64 bytes (input minus op_type byte).
	if len(cpu.OutputBuf) != 64 {
		t.Fatalf("expected 64 bytes output, got %d", len(cpu.OutputBuf))
	}
}

func TestNTTGuestGoldilocksExecution(t *testing.T) {
	prog := BuildNTTGuest()

	// Goldilocks forward NTT: op_type=2
	input := make([]byte, 129) // 1 + 4 * 32 = 129
	input[0] = 2               // Goldilocks forward
	for i := 1; i < 129; i++ {
		input[i] = byte(i ^ 0x55)
	}

	cpu := NewRVCPU(1000000)
	cpu.InputBuf = input
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
	if len(cpu.OutputBuf) != 128 {
		t.Fatalf("expected 128 bytes output, got %d", len(cpu.OutputBuf))
	}
}

func TestNTTGuestEmptyInput(t *testing.T) {
	prog := BuildNTTGuest()
	cpu := NewRVCPU(100000)
	cpu.InputBuf = nil
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
	// Empty input -> 0 output bytes.
	if len(cpu.OutputBuf) != 0 {
		t.Fatalf("expected 0 output, got %d", len(cpu.OutputBuf))
	}
}

func TestNTTGuestDeterministic(t *testing.T) {
	prog := BuildNTTGuest()
	input := make([]byte, 33)
	input[0] = 0 // forward NTT
	for i := 1; i < 33; i++ {
		input[i] = byte(i * 3)
	}

	run := func() []byte {
		cpu := NewRVCPU(200000)
		cpu.InputBuf = input
		if err := cpu.LoadProgram(prog, 0, 0); err != nil {
			t.Fatalf("load: %v", err)
		}
		cpu.Regs[2] = 0x80000000
		cpu.Run()
		return cpu.OutputBuf
	}

	out1 := run()
	out2 := run()
	if !bytes.Equal(out1, out2) {
		t.Fatal("NTT guest output not deterministic")
	}
}

func TestRegisterNTTGuest(t *testing.T) {
	registry := NewGuestRegistry()
	id, err := RegisterNTTGuest(registry)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if id == (types.Hash{}) {
		t.Fatal("expected non-zero ID")
	}
	if registry.Count() != 1 {
		t.Fatalf("expected 1 guest, got %d", registry.Count())
	}

	// Re-register should return same ID.
	id2, err := RegisterNTTGuest(registry)
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if id != id2 {
		t.Fatal("ID mismatch on re-registration")
	}
}

func TestRegisterNTTZKISAOp(t *testing.T) {
	table := NewZKISAOpTable()
	before := table.Count()
	RegisterNTTZKISAOp(table)
	if table.Count() != before+1 {
		t.Fatalf("expected 1 new op")
	}

	entry, err := table.Lookup(ZKISAOpNTT)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if entry.Name != "ntt" {
		t.Errorf("expected name 'ntt', got '%s'", entry.Name)
	}
	if entry.PrecompileAddr != 0x15 {
		t.Errorf("expected addr 0x15, got 0x%02x", entry.PrecompileAddr)
	}
}
