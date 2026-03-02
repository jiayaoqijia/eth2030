package zkvm

import (
	"encoding/binary"
	"testing"
)

// makeADDWitness creates a step witness for ADD rd, rs1, rs2.
func makeADDWitness(rd, rs1, rs2 uint32, val1, val2 uint32) *RVStepWitness {
	instr := EncodeRType(0x33, rd, 0, rs1, rs2, 0) // ADD
	w := &RVStepWitness{
		PrePC:       0x1000,
		Instruction: instr,
		PostPC:      0x1004,
	}
	w.PreRegs[rs1] = val1
	w.PreRegs[rs2] = val2
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[rd] = val1 + val2
	w.PostRegs[0] = 0
	return w
}

// makeSUBWitness creates a step witness for SUB rd, rs1, rs2.
func makeSUBWitness(rd, rs1, rs2 uint32, val1, val2 uint32) *RVStepWitness {
	instr := EncodeRType(0x33, rd, 0, rs1, rs2, 0x20) // SUB (funct7=0x20)
	w := &RVStepWitness{
		PrePC:       0x2000,
		Instruction: instr,
		PostPC:      0x2004,
	}
	w.PreRegs[rs1] = val1
	w.PreRegs[rs2] = val2
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[rd] = uint32(int32(val1) - int32(val2))
	w.PostRegs[0] = 0
	return w
}

// makeMULWitness creates a step witness for MUL rd, rs1, rs2.
func makeMULWitness(rd, rs1, rs2 uint32, val1, val2 uint32) *RVStepWitness {
	instr := EncodeRType(0x33, rd, 0, rs1, rs2, 1) // MUL (funct7=1, funct3=0)
	w := &RVStepWitness{
		PrePC:       0x3000,
		Instruction: instr,
		PostPC:      0x3004,
	}
	w.PreRegs[rs1] = val1
	w.PreRegs[rs2] = val2
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[rd] = uint32(int32(val1) * int32(val2))
	w.PostRegs[0] = 0
	return w
}

func TestGnarkCircuit_Compile(t *testing.T) {
	c := NewRVStepCircuit()
	if c.IsCompiled() {
		t.Fatal("circuit should not be compiled initially")
	}
	n := c.Compile()
	if n == 0 {
		t.Fatal("compiled circuit should have > 0 constraints")
	}
	if !c.IsCompiled() {
		t.Fatal("circuit should be compiled after Compile()")
	}
	if c.ConstraintCount() != n {
		t.Errorf("ConstraintCount() = %d, want %d", c.ConstraintCount(), n)
	}
}

func TestGnarkCircuit_ADD(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	w := makeADDWitness(3, 1, 2, 100, 200)
	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep ADD: %v", err)
	}
	if proof == nil {
		t.Fatal("proof should not be nil")
	}

	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("valid ADD proof should verify")
	}
}

func TestGnarkCircuit_SUB(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	w := makeSUBWitness(3, 1, 2, 300, 100)
	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep SUB: %v", err)
	}

	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("valid SUB proof should verify")
	}
}

func TestGnarkCircuit_MUL(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	w := makeMULWitness(3, 1, 2, 7, 6)
	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep MUL: %v", err)
	}

	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("valid MUL proof should verify")
	}
}

func TestGnarkCircuit_InvalidWitness(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	// Create ADD witness but set wrong PostRegs.
	w := makeADDWitness(3, 1, 2, 100, 200)
	w.PostRegs[3] = 999 // wrong: should be 300

	_, err := ProveStep(c, w)
	if err == nil {
		t.Fatal("ProveStep should fail with invalid witness")
	}
}

func TestGnarkCircuit_ProofSizeConstant(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	// Different inputs should produce same-size proofs.
	w1 := makeADDWitness(3, 1, 2, 1, 1)
	w2 := makeADDWitness(5, 1, 2, 1000000, 2000000)
	w3 := makeMULWitness(7, 1, 2, 42, 99)

	p1, err := ProveStep(c, w1)
	if err != nil {
		t.Fatalf("ProveStep 1: %v", err)
	}
	p2, err := ProveStep(c, w2)
	if err != nil {
		t.Fatalf("ProveStep 2: %v", err)
	}
	p3, err := ProveStep(c, w3)
	if err != nil {
		t.Fatalf("ProveStep 3: %v", err)
	}

	if len(p1.ProofBytes) != gnarkGroth16ProofSize {
		t.Errorf("proof 1 size: got %d, want %d", len(p1.ProofBytes), gnarkGroth16ProofSize)
	}
	if len(p2.ProofBytes) != gnarkGroth16ProofSize {
		t.Errorf("proof 2 size: got %d, want %d", len(p2.ProofBytes), gnarkGroth16ProofSize)
	}
	if len(p3.ProofBytes) != gnarkGroth16ProofSize {
		t.Errorf("proof 3 size: got %d, want %d", len(p3.ProofBytes), gnarkGroth16ProofSize)
	}
}

func TestGnarkCircuit_TamperedProofFails(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	w := makeADDWitness(3, 1, 2, 50, 50)
	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep: %v", err)
	}

	// Tamper with proof bytes.
	proof.ProofBytes[0] ^= 0xFF

	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof error: %v", err)
	}
	if valid {
		t.Error("tampered proof should not verify")
	}
}

func TestGnarkCircuit_NilWitness(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	_, err := ProveStep(c, nil)
	if err == nil {
		t.Fatal("ProveStep should fail with nil witness")
	}
}

func TestGnarkCircuit_NotCompiled(t *testing.T) {
	c := NewRVStepCircuit()
	w := makeADDWitness(3, 1, 2, 1, 1)

	_, err := ProveStep(c, w)
	if err == nil {
		t.Fatal("ProveStep should fail on uncompiled circuit")
	}
}

func TestGnarkCircuit_LUI(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	instr := EncodeUType(0x37, 5, 0x12345000) // LUI x5, 0x12345
	w := &RVStepWitness{
		PrePC:       0x4000,
		Instruction: instr,
		PostPC:      0x4004,
	}
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[5] = 0x12345000

	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep LUI: %v", err)
	}
	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("LUI proof should verify")
	}
}

func TestGnarkCircuit_ADDI(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	instr := EncodeIType(0x13, 3, 0, 1, 42) // ADDI x3, x1, 42
	w := &RVStepWitness{
		PrePC:       0x5000,
		Instruction: instr,
		PostPC:      0x5004,
	}
	w.PreRegs[1] = 100
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[3] = 142

	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep ADDI: %v", err)
	}
	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("ADDI proof should verify")
	}
}

func TestGnarkCircuit_BEQ_Taken(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	offset := int32(16)
	instr := EncodeBType(0x63, 0, 1, 2, offset) // BEQ x1, x2, +16
	w := &RVStepWitness{
		PrePC:       0x6000,
		Instruction: instr,
		PostPC:      0x6000 + uint32(offset), // branch taken
	}
	w.PreRegs[1] = 42
	w.PreRegs[2] = 42 // equal -> taken
	copy(w.PostRegs[:], w.PreRegs[:])

	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep BEQ: %v", err)
	}
	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("BEQ taken proof should verify")
	}
}

func TestGnarkCircuit_BEQ_NotTaken(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	offset := int32(16)
	instr := EncodeBType(0x63, 0, 1, 2, offset) // BEQ x1, x2, +16
	w := &RVStepWitness{
		PrePC:       0x6000,
		Instruction: instr,
		PostPC:      0x6004, // not taken
	}
	w.PreRegs[1] = 42
	w.PreRegs[2] = 99 // not equal -> not taken
	copy(w.PostRegs[:], w.PreRegs[:])

	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep BEQ not taken: %v", err)
	}
	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("BEQ not-taken proof should verify")
	}
}

func TestGnarkCircuit_WitnessFromTrace(t *testing.T) {
	// Run a small program and extract witness from trace.
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 7),    // ADDI x1, x0, 7
		EncodeIType(0x13, 2, 0, 0, 6),    // ADDI x2, x0, 6
		EncodeRType(0x33, 3, 0, 1, 2, 0), // ADD x3, x1, x2
		EncodeIType(0x13, 17, 0, 0, 0),   // ADDI x17, x0, 0 (a7=halt)
		EncodeIType(0x13, 10, 0, 0, 0),   // ADDI x10, x0, 0 (a0=0)
		0x00000073,                       // ECALL
	}
	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}

	cpu := NewRVCPU(100)
	if err := cpu.LoadProgram(code, 0, 0); err != nil {
		t.Fatalf("LoadProgram: %v", err)
	}
	cpu.Witness = NewRVWitnessCollector()
	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Check that WitnessFromTrace produces valid witnesses.
	c := NewRVStepCircuit()
	c.Compile()

	for i, step := range cpu.Witness.Steps {
		w := WitnessFromTrace(step)
		if err := c.SetWitness(w); err != nil {
			t.Fatalf("step %d SetWitness: %v", i, err)
		}
		// ECALL steps are pass-through in circuit.
		if step.Instruction == 0x00000073 {
			continue
		}
		if err := c.CheckWitness(); err != nil {
			t.Fatalf("step %d CheckWitness: %v", i, err)
		}
	}
}

func TestGnarkCircuit_DIV(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	// DIV: funct7=1, funct3=4
	instr := EncodeRType(0x33, 3, 4, 1, 2, 1) // DIV x3, x1, x2
	w := &RVStepWitness{
		PrePC:       0x7000,
		Instruction: instr,
		PostPC:      0x7004,
	}
	w.PreRegs[1] = 42
	w.PreRegs[2] = 7
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[3] = 6

	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep DIV: %v", err)
	}
	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("DIV proof should verify")
	}
}

func TestGnarkCircuit_DIV_ByZero(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	instr := EncodeRType(0x33, 3, 4, 1, 2, 1) // DIV x3, x1, x2
	w := &RVStepWitness{
		PrePC:       0x7000,
		Instruction: instr,
		PostPC:      0x7004,
	}
	w.PreRegs[1] = 42
	w.PreRegs[2] = 0 // divide by zero
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[3] = 0xFFFFFFFF // RISC-V spec: div by zero returns -1

	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep DIV/0: %v", err)
	}
	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("DIV by zero proof should verify")
	}
}

func TestGnarkCircuit_SLL(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	instr := EncodeRType(0x33, 3, 1, 1, 2, 0) // SLL x3, x1, x2
	w := &RVStepWitness{
		PrePC:       0x8000,
		Instruction: instr,
		PostPC:      0x8004,
	}
	w.PreRegs[1] = 1
	w.PreRegs[2] = 4
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[3] = 16 // 1 << 4

	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep SLL: %v", err)
	}
	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("SLL proof should verify")
	}
}

func TestGnarkCircuit_XOR(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	instr := EncodeRType(0x33, 3, 4, 1, 2, 0) // XOR x3, x1, x2
	w := &RVStepWitness{
		PrePC:       0x9000,
		Instruction: instr,
		PostPC:      0x9004,
	}
	w.PreRegs[1] = 0xFF00
	w.PreRegs[2] = 0x0FF0
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[3] = 0xF0F0

	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep XOR: %v", err)
	}
	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("XOR proof should verify")
	}
}

func TestGnarkCircuit_AUIPC(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	instr := EncodeUType(0x17, 5, 0x10000000) // AUIPC x5, 0x10000
	w := &RVStepWitness{
		PrePC:       0xA000,
		Instruction: instr,
		PostPC:      0xA004,
	}
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[5] = 0xA000 + 0x10000000

	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep AUIPC: %v", err)
	}
	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("AUIPC proof should verify")
	}
}

func TestGnarkCircuit_JAL(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	offset := int32(100)
	instr := EncodeJType(0x6F, 1, offset) // JAL x1, +100
	w := &RVStepWitness{
		PrePC:       0xB000,
		Instruction: instr,
		PostPC:      0xB000 + uint32(offset),
	}
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[1] = 0xB004 // return address

	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep JAL: %v", err)
	}
	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("JAL proof should verify")
	}
}

func TestGnarkCircuit_REM(t *testing.T) {
	c := NewRVStepCircuit()
	c.Compile()

	// REM: funct7=1, funct3=6
	instr := EncodeRType(0x33, 3, 6, 1, 2, 1) // REM x3, x1, x2
	w := &RVStepWitness{
		PrePC:       0xC000,
		Instruction: instr,
		PostPC:      0xC004,
	}
	w.PreRegs[1] = 17
	w.PreRegs[2] = 5
	copy(w.PostRegs[:], w.PreRegs[:])
	w.PostRegs[3] = 2 // 17 % 5 = 2

	proof, err := ProveStep(c, w)
	if err != nil {
		t.Fatalf("ProveStep REM: %v", err)
	}
	valid, err := VerifyStepProof(proof, w)
	if err != nil {
		t.Fatalf("VerifyStepProof: %v", err)
	}
	if !valid {
		t.Error("REM proof should verify")
	}
}
