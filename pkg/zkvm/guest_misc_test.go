package zkvm

import (
	"bytes"
	"testing"
)

func TestBuildRIPEMD160Guest(t *testing.T) {
	prog := BuildRIPEMD160Guest()
	if len(prog) == 0 || len(prog)%4 != 0 {
		t.Fatal("invalid program")
	}
}

func TestBuildBLAKE2fGuest(t *testing.T) {
	prog := BuildBLAKE2fGuest()
	if len(prog) == 0 || len(prog)%4 != 0 {
		t.Fatal("invalid program")
	}
}

func TestBuildDataCopyGuest(t *testing.T) {
	prog := BuildDataCopyGuest()
	if len(prog) == 0 || len(prog)%4 != 0 {
		t.Fatal("invalid program")
	}
}

func TestBuildKZGPointEvalGuest(t *testing.T) {
	prog := BuildKZGPointEvalGuest()
	if len(prog) == 0 || len(prog)%4 != 0 {
		t.Fatal("invalid program")
	}
}

func TestDataCopyGuestExecution(t *testing.T) {
	prog := BuildDataCopyGuest()
	input := []byte("hello world")

	cpu := NewRVCPU(100000)
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
	if !bytes.Equal(cpu.OutputBuf, input) {
		t.Fatalf("output mismatch: got %q, want %q", cpu.OutputBuf, input)
	}
}

func TestDataCopyGuestEmptyInput(t *testing.T) {
	prog := BuildDataCopyGuest()
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
	if len(cpu.OutputBuf) != 0 {
		t.Fatalf("expected empty output, got %d bytes", len(cpu.OutputBuf))
	}
}

func TestRIPEMD160GuestExecution(t *testing.T) {
	prog := BuildRIPEMD160Guest()
	cpu := NewRVCPU(100000)
	cpu.InputBuf = []byte("test data for ripemd")
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
	if len(cpu.OutputBuf) != 32 {
		t.Fatalf("expected 32 bytes, got %d", len(cpu.OutputBuf))
	}
}

func TestBLAKE2fGuestExecution(t *testing.T) {
	prog := BuildBLAKE2fGuest()
	cpu := NewRVCPU(200000)
	cpu.InputBuf = make([]byte, 213) // standard blake2f input
	cpu.InputBuf[0] = 12             // 12 rounds
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
	if len(cpu.OutputBuf) != 64 {
		t.Fatalf("expected 64 bytes, got %d", len(cpu.OutputBuf))
	}
}

func TestKZGPointEvalGuestExecution(t *testing.T) {
	prog := BuildKZGPointEvalGuest()
	cpu := NewRVCPU(200000)
	cpu.InputBuf = make([]byte, 192) // standard KZG input
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
	if !cpu.Halted || cpu.ExitCode != 0 {
		t.Fatal("expected clean halt")
	}
	if len(cpu.OutputBuf) != 64 {
		t.Fatalf("expected 64 bytes, got %d", len(cpu.OutputBuf))
	}
}

func TestRegisterMiscGuests(t *testing.T) {
	registry := NewGuestRegistry()
	ids, err := RegisterMiscGuests(registry)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if len(ids) != 4 {
		t.Fatalf("expected 4 guests, got %d", len(ids))
	}
	if registry.Count() != 4 {
		t.Fatalf("expected 4 in registry, got %d", registry.Count())
	}
}

func TestRegisterMiscZKISAOps(t *testing.T) {
	table := NewZKISAOpTable()
	before := table.Count()
	RegisterMiscZKISAOps(table)
	after := table.Count()

	if after-before != 4 {
		t.Fatalf("expected 4 new ops, got %d", after-before)
	}

	entry, err := table.Lookup(ZKISAOpDataCopy)
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if entry.Name != "datacopy" {
		t.Errorf("expected name datacopy, got %s", entry.Name)
	}
	if entry.PrecompileAddr != 0x04 {
		t.Errorf("expected addr 0x04, got 0x%02x", entry.PrecompileAddr)
	}
}

func TestMiscGuestDeterministic(t *testing.T) {
	prog := BuildRIPEMD160Guest()
	input := []byte{0xAA, 0xBB, 0xCC}

	run := func() []byte {
		cpu := NewRVCPU(100000)
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
		t.Fatal("output not deterministic")
	}
}
