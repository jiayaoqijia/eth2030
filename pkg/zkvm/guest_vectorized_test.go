package zkvm

import (
	"encoding/binary"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestBuildVectorizedGuest(t *testing.T) {
	prog := BuildVectorizedGuest()
	if len(prog) == 0 {
		t.Fatal("expected non-empty program")
	}
	if len(prog)%4 != 0 {
		t.Fatal("program size must be multiple of 4")
	}
}

func TestVectorizedGuestExecution_VADD32(t *testing.T) {
	prog := BuildVectorizedGuest()
	// Build input: opcode=0x01, width=4, count=2, A=[3,7], B=[10,20]
	input := buildVecTestInput(0x01, 4, 2, encodeVecU32(3, 7, 10, 20))
	cpu := NewRVCPU(20000000)
	cpu.InputBuf = input
	if err := cpu.LoadProgram(prog, 0, 0); err != nil {
		t.Fatalf("load: %v", err)
	}
	cpu.Regs[2] = 0x80000000
	if err := cpu.Run(); err != nil {
		t.Fatalf("run: %v", err)
	}
	if !cpu.Halted || cpu.ExitCode != 0 {
		t.Fatalf("expected clean halt, halted=%v exit=%d", cpu.Halted, cpu.ExitCode)
	}
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected non-empty output")
	}
}

func TestVectorizedGuestExecution_VREDUCE32(t *testing.T) {
	prog := BuildVectorizedGuest()
	// VREDUCE: opcode=0x0A, width=4, count=3, data=[5,10,15]
	input := buildVecTestInput(0x0A, 4, 3, encodeVecU32(5, 10, 15))
	cpu := NewRVCPU(20000000)
	cpu.InputBuf = input
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
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestVectorizedGuestExecution_VDOT32(t *testing.T) {
	prog := BuildVectorizedGuest()
	// VDOT: opcode=0x0B, width=4, count=2, A=[2,3], B=[4,5]
	// Expected dot: 2*4 + 3*5 = 23
	input := buildVecTestInput(0x0B, 4, 2, encodeVecU32(2, 3, 4, 5))
	cpu := NewRVCPU(20000000)
	cpu.InputBuf = input
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
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestVectorizedGuestExecution_VMUL32(t *testing.T) {
	prog := BuildVectorizedGuest()
	// VMUL: opcode=0x02, width=4, count=1, A=[6], B=[7]
	input := buildVecTestInput(0x02, 4, 1, encodeVecU32(6, 7))
	cpu := NewRVCPU(20000000)
	cpu.InputBuf = input
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
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestVectorizedGuestExecution_VSUB32(t *testing.T) {
	prog := BuildVectorizedGuest()
	input := buildVecTestInput(0x03, 4, 1, encodeVecU32(100, 30))
	cpu := NewRVCPU(20000000)
	cpu.InputBuf = input
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
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestVectorizedGuestExecution_VXOR32(t *testing.T) {
	prog := BuildVectorizedGuest()
	input := buildVecTestInput(0x06, 4, 1, encodeVecU32(0xFF, 0xFF))
	cpu := NewRVCPU(20000000)
	cpu.InputBuf = input
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
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestVectorizedGuestExecution_VMOD32(t *testing.T) {
	prog := BuildVectorizedGuest()
	input := buildVecTestInput(0x09, 4, 1, encodeVecU32(17, 5))
	cpu := NewRVCPU(20000000)
	cpu.InputBuf = input
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
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestVectorizedGuestExecution_VSHL32(t *testing.T) {
	prog := BuildVectorizedGuest()
	input := buildVecTestInput(0x07, 4, 1, encodeVecU32(1, 4))
	cpu := NewRVCPU(20000000)
	cpu.InputBuf = input
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
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestVectorizedGuestExecution_VSHR32(t *testing.T) {
	prog := BuildVectorizedGuest()
	input := buildVecTestInput(0x08, 4, 1, encodeVecU32(256, 4))
	cpu := NewRVCPU(20000000)
	cpu.InputBuf = input
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
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestVectorizedGuestExecution_VAND32(t *testing.T) {
	prog := BuildVectorizedGuest()
	input := buildVecTestInput(0x04, 4, 1, encodeVecU32(0xFF, 0x0F))
	cpu := NewRVCPU(20000000)
	cpu.InputBuf = input
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
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestVectorizedGuestExecution_VOR32(t *testing.T) {
	prog := BuildVectorizedGuest()
	input := buildVecTestInput(0x05, 4, 1, encodeVecU32(0xF0, 0x0F))
	cpu := NewRVCPU(20000000)
	cpu.InputBuf = input
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
	if len(cpu.OutputBuf) == 0 {
		t.Fatal("expected output")
	}
}

func TestExecuteVectorizedGo_AllOps(t *testing.T) {
	tests := []struct {
		name     string
		opcode   byte
		width    byte
		count    uint32
		data     []byte
		wantLen  int
		wantErr  bool
	}{
		{"VADD32", 0x01, 4, 2, encodeVecU32(1, 2, 3, 4), 8, false},
		{"VMUL32", 0x02, 4, 1, encodeVecU32(6, 7), 4, false},
		{"VSUB32", 0x03, 4, 1, encodeVecU32(10, 3), 4, false},
		{"VAND32", 0x04, 4, 1, encodeVecU32(0xFF, 0x0F), 4, false},
		{"VOR32", 0x05, 4, 1, encodeVecU32(0xF0, 0x0F), 4, false},
		{"VXOR32", 0x06, 4, 1, encodeVecU32(0xFF, 0xFF), 4, false},
		{"VSHL32", 0x07, 4, 1, encodeVecU32(1, 8), 4, false},
		{"VSHR32", 0x08, 4, 1, encodeVecU32(256, 4), 4, false},
		{"VMOD32", 0x09, 4, 1, encodeVecU32(10, 3), 4, false},
		{"VREDUCE32", 0x0A, 4, 3, encodeVecU32(1, 2, 3), 4, false},
		{"VDOT32", 0x0B, 4, 2, encodeVecU32(2, 3, 4, 5), 4, false},
		{"invalid op", 0xFF, 4, 1, encodeVecU32(1, 2), 0, true},
		{"invalid width", 0x01, 3, 1, encodeVecU32(1, 2), 0, true},
		{"zero count", 0x01, 4, 0, nil, 0, true},
		{"short input", 0x01, 4, 2, encodeVecU32(1), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := buildVecTestInput(tt.opcode, tt.width, tt.count, tt.data)
			out, err := ExecuteVectorizedGo(input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(out) != tt.wantLen {
				t.Errorf("output len=%d, want %d", len(out), tt.wantLen)
			}
		})
	}
}

func TestExecuteVectorizedGo_Values(t *testing.T) {
	// VADD: [1,2] + [3,4] = [4,6]
	input := buildVecTestInput(0x01, 4, 2, encodeVecU32(1, 2, 3, 4))
	out, err := ExecuteVectorizedGo(input)
	if err != nil {
		t.Fatalf("VADD: %v", err)
	}
	gotA := binary.BigEndian.Uint32(out[0:4])
	gotB := binary.BigEndian.Uint32(out[4:8])
	if gotA != 4 || gotB != 6 {
		t.Errorf("VADD: got [%d,%d], want [4,6]", gotA, gotB)
	}

	// VREDUCE: sum([5,10,15]) = 30
	input = buildVecTestInput(0x0A, 4, 3, encodeVecU32(5, 10, 15))
	out, err = ExecuteVectorizedGo(input)
	if err != nil {
		t.Fatalf("VREDUCE: %v", err)
	}
	got := binary.BigEndian.Uint32(out)
	if got != 30 {
		t.Errorf("VREDUCE: got %d, want 30", got)
	}

	// VDOT: [2,3] . [4,5] = 8 + 15 = 23
	input = buildVecTestInput(0x0B, 4, 2, encodeVecU32(2, 3, 4, 5))
	out, err = ExecuteVectorizedGo(input)
	if err != nil {
		t.Fatalf("VDOT: %v", err)
	}
	got = binary.BigEndian.Uint32(out)
	if got != 23 {
		t.Errorf("VDOT: got %d, want 23", got)
	}
}

func TestRegisterVectorizedGuest(t *testing.T) {
	reg := NewGuestRegistry()
	id, err := RegisterVectorizedGuest(reg)
	if err != nil {
		t.Fatalf("registration failed: %v", err)
	}
	if id == (types.Hash{}) {
		t.Fatal("expected non-zero program ID")
	}

	// Should be retrievable.
	prog, err := reg.GetGuest(id)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if len(prog) == 0 {
		t.Fatal("expected non-empty program")
	}
}

func TestRegisterVectorizedZKISAOp(t *testing.T) {
	table := NewZKISAOpTable()
	RegisterVectorizedZKISAOp(table)

	entry, err := table.Lookup(ZKISAOpVectorized)
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if entry.Name != "vectorized" {
		t.Errorf("name = %s, want vectorized", entry.Name)
	}
	if entry.Selector != 0x20 {
		t.Errorf("selector = 0x%02x, want 0x20", entry.Selector)
	}
	if entry.PrecompileAddr != 0x21 {
		t.Errorf("precompile addr = 0x%02x, want 0x21", entry.PrecompileAddr)
	}
}

func TestVectorizedGuestProofGeneration(t *testing.T) {
	reg := NewGuestRegistry()
	id, err := RegisterVectorizedGuest(reg)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	config := DefaultCanonicalExecutorConfig()
	config.GasLimit = 1 << 25
	executor, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("executor: %v", err)
	}

	input := buildVecTestInput(0x01, 4, 1, encodeVecU32(10, 20))
	output, proof, err := executor.ExecuteAndProve(id, input)
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if output == nil {
		t.Fatal("expected non-nil output")
	}
	if proof == nil {
		t.Fatal("expected non-nil proof")
	}
	if proof.ProofResult == nil {
		t.Fatal("expected non-nil proof result")
	}
}

// --- helpers ---

func buildVecTestInput(opcode, width byte, count uint32, data []byte) []byte {
	buf := make([]byte, 6+len(data))
	buf[0] = opcode
	buf[1] = width
	binary.BigEndian.PutUint32(buf[2:6], count)
	copy(buf[6:], data)
	return buf
}

func encodeVecU32(vals ...uint32) []byte {
	out := make([]byte, len(vals)*4)
	for i, v := range vals {
		binary.BigEndian.PutUint32(out[i*4:], v)
	}
	return out
}
