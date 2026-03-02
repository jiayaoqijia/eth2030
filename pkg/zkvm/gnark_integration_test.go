package zkvm

import (
	"encoding/binary"
	"errors"
	"testing"
)

// TestGnarkIntegration_SimulatedBackendProducesValidProof verifies the
// simulated (SHA-256) backend round-trips correctly.
func TestGnarkIntegration_SimulatedBackendProducesValidProof(t *testing.T) {
	// Ensure simulated backend is active.
	origBackend := GetProofBackend()
	SetProofBackend(ProofBackendSimulated)
	defer SetProofBackend(origBackend)

	trace := buildTestTrace(t)
	programHash := HashProgram([]byte("test-program"))

	req := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("public-inputs"),
		ProgramHash:  programHash,
	}
	result, err := ProveExecution(req)
	if err != nil {
		t.Fatalf("ProveExecution (simulated): %v", err)
	}
	if result.BackendType != ProofBackendSimulated {
		t.Errorf("BackendType = %d, want %d", result.BackendType, ProofBackendSimulated)
	}

	valid, err := VerifyExecProof(result, programHash)
	if err != nil {
		t.Fatalf("VerifyExecProof (simulated): %v", err)
	}
	if !valid {
		t.Error("simulated proof should verify")
	}
}

// TestGnarkIntegration_GnarkBackendProducesValidProof verifies the gnark
// backend round-trips correctly.
func TestGnarkIntegration_GnarkBackendProducesValidProof(t *testing.T) {
	origBackend := GetProofBackend()
	SetProofBackend(ProofBackendGnark)
	defer SetProofBackend(origBackend)

	trace := buildGnarkCompatibleTrace(t)
	programHash := HashProgram([]byte("test-program-gnark"))

	req := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("gnark-public-inputs"),
		ProgramHash:  programHash,
	}
	result, err := ProveExecution(req)
	if err != nil {
		t.Fatalf("ProveExecution (gnark): %v", err)
	}
	if result.BackendType != ProofBackendGnark {
		t.Errorf("BackendType = %d, want %d", result.BackendType, ProofBackendGnark)
	}

	valid, err := VerifyExecProof(result, programHash)
	if err != nil {
		t.Fatalf("VerifyExecProof (gnark): %v", err)
	}
	if !valid {
		t.Error("gnark proof should verify")
	}
}

// TestGnarkIntegration_CrossBackendVerificationFails ensures that a simulated
// proof does not verify as gnark and vice versa.
func TestGnarkIntegration_CrossBackendVerificationFails(t *testing.T) {
	trace := buildGnarkCompatibleTrace(t)
	programHash := HashProgram([]byte("cross-backend-test"))

	// Generate a simulated proof.
	SetProofBackend(ProofBackendSimulated)
	reqSim := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("inputs"),
		ProgramHash:  programHash,
	}
	simResult, err := ProveExecution(reqSim)
	if err != nil {
		t.Fatalf("ProveExecution (simulated): %v", err)
	}

	// Generate a gnark proof.
	SetProofBackend(ProofBackendGnark)
	reqGnark := &ProofRequest{
		Trace:        trace,
		PublicInputs: []byte("inputs"),
		ProgramHash:  programHash,
	}
	gnarkResult, err := ProveExecution(reqGnark)
	if err != nil {
		t.Fatalf("ProveExecution (gnark): %v", err)
	}

	// Reset backend.
	SetProofBackend(ProofBackendSimulated)

	// Proofs should be different.
	if simResult.BackendType == gnarkResult.BackendType {
		t.Error("simulated and gnark results should have different BackendType")
	}

	// VK should differ.
	if len(simResult.VerificationKey) == len(gnarkResult.VerificationKey) {
		same := true
		for i := range simResult.VerificationKey {
			if simResult.VerificationKey[i] != gnarkResult.VerificationKey[i] {
				same = false
				break
			}
		}
		if same {
			t.Error("simulated and gnark VKs should differ")
		}
	}

	// Try verifying gnark proof with simulated verifier (should fail).
	// The gnark proof has BackendType=1, so VerifyExecProof dispatches
	// to the gnark verifier automatically.
	// Forge a simulated result with gnark proof bytes to test mismatch.
	forgedSim := &ProofResult{
		ProofBytes:       gnarkResult.ProofBytes,
		VerificationKey:  gnarkResult.VerificationKey,
		TraceCommitment:  gnarkResult.TraceCommitment,
		PublicInputsHash: gnarkResult.PublicInputsHash,
		BackendType:      ProofBackendSimulated, // Force simulated dispatch
	}
	valid, err := VerifyExecProof(forgedSim, programHash)
	if err != nil {
		t.Fatalf("forged simulated verify error: %v", err)
	}
	if valid {
		t.Error("gnark proof should not verify as simulated")
	}
}

// TestGnarkIntegration_CanonicalExecutorSimulated runs ExecuteAndProve with
// the simulated backend and verifies the proof.
func TestGnarkIntegration_CanonicalExecutorSimulated(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	config.ProofBackend = ProofBackendSimulated

	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	program := buildCanonExecHaltProgram()
	programID, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("RegisterGuest: %v", err)
	}

	_, proof, err := exec.ExecuteAndProve(programID, []byte("sim test"))
	if err != nil {
		t.Fatalf("ExecuteAndProve: %v", err)
	}
	if proof == nil || proof.ProofResult == nil {
		t.Fatal("proof should not be nil")
	}
	if proof.ProofResult.BackendType != ProofBackendSimulated {
		t.Errorf("proof BackendType = %d, want %d", proof.ProofResult.BackendType, ProofBackendSimulated)
	}

	if err := exec.VerifyGuestProof(proof); err != nil {
		t.Fatalf("VerifyGuestProof (simulated): %v", err)
	}
}

// TestGnarkIntegration_CanonicalExecutorGnark runs ExecuteAndProve with
// the gnark backend and verifies the proof.
func TestGnarkIntegration_CanonicalExecutorGnark(t *testing.T) {
	reg := NewGuestRegistry()
	config := DefaultCanonicalExecutorConfig()
	config.ProofBackend = ProofBackendGnark

	exec, err := NewCanonicalExecutor(reg, config)
	if err != nil {
		t.Fatalf("NewCanonicalExecutor: %v", err)
	}

	program := buildCanonExecHaltProgram()
	programID, err := reg.RegisterGuest(program)
	if err != nil {
		t.Fatalf("RegisterGuest: %v", err)
	}

	_, proof, err := exec.ExecuteAndProve(programID, []byte("gnark test"))
	if err != nil {
		t.Fatalf("ExecuteAndProve (gnark): %v", err)
	}
	if proof == nil || proof.ProofResult == nil {
		t.Fatal("proof should not be nil")
	}
	if proof.ProofResult.BackendType != ProofBackendGnark {
		t.Errorf("proof BackendType = %d, want %d", proof.ProofResult.BackendType, ProofBackendGnark)
	}

	if err := exec.VerifyGuestProof(proof); err != nil {
		t.Fatalf("VerifyGuestProof (gnark): %v", err)
	}
}

// TestGnarkIntegration_ExistingTestsStillPass runs the same assertions as the
// existing canonical_executor_test.go tests to ensure backward compatibility.
func TestGnarkIntegration_ExistingTestsStillPass(t *testing.T) {
	// Ensure default backend is simulated.
	origBackend := GetProofBackend()
	SetProofBackend(ProofBackendSimulated)
	defer SetProofBackend(origBackend)

	t.Run("ExecuteGuestHalt", func(t *testing.T) {
		reg := NewGuestRegistry()
		config := DefaultCanonicalExecutorConfig()
		exec, err := NewCanonicalExecutor(reg, config)
		if err != nil {
			t.Fatalf("NewCanonicalExecutor: %v", err)
		}

		program := buildCanonExecHaltProgram()
		programID, _ := reg.RegisterGuest(program)

		output, witness, err := exec.ExecuteGuest(programID, nil)
		if err != nil {
			t.Fatalf("ExecuteGuest: %v", err)
		}
		if output.ExitCode != 0 {
			t.Errorf("exit code: got %d, want 0", output.ExitCode)
		}
		if witness == nil || witness.StepCount() == 0 {
			t.Error("witness should have steps")
		}
	})

	t.Run("ExecuteAndProve", func(t *testing.T) {
		reg := NewGuestRegistry()
		config := DefaultCanonicalExecutorConfig()
		exec, err := NewCanonicalExecutor(reg, config)
		if err != nil {
			t.Fatalf("NewCanonicalExecutor: %v", err)
		}

		program := buildCanonExecHaltProgram()
		programID, _ := reg.RegisterGuest(program)

		output, proof, err := exec.ExecuteAndProve(programID, []byte("test input"))
		if err != nil {
			t.Fatalf("ExecuteAndProve: %v", err)
		}
		if output == nil || proof == nil {
			t.Fatal("output or proof is nil")
		}
		if len(proof.ProofResult.ProofBytes) != groth16ProofSize {
			t.Errorf("proof size: got %d, want %d", len(proof.ProofResult.ProofBytes), groth16ProofSize)
		}
	})

	t.Run("VerifyGuestProof", func(t *testing.T) {
		reg := NewGuestRegistry()
		config := DefaultCanonicalExecutorConfig()
		exec, err := NewCanonicalExecutor(reg, config)
		if err != nil {
			t.Fatalf("NewCanonicalExecutor: %v", err)
		}

		program := buildCanonExecHaltProgram()
		programID, _ := reg.RegisterGuest(program)

		_, proof, err := exec.ExecuteAndProve(programID, []byte("verify me"))
		if err != nil {
			t.Fatalf("ExecuteAndProve: %v", err)
		}
		if err := exec.VerifyGuestProof(proof); err != nil {
			t.Fatalf("VerifyGuestProof: %v", err)
		}
	})

	t.Run("NilProof", func(t *testing.T) {
		reg := NewGuestRegistry()
		config := DefaultCanonicalExecutorConfig()
		exec, _ := NewCanonicalExecutor(reg, config)

		err := exec.VerifyGuestProof(nil)
		if !errors.Is(err, ErrCanonExecNilProof) {
			t.Fatalf("expected ErrCanonExecNilProof, got %v", err)
		}
	})
}

// TestGnarkIntegration_BatchProofRoundTrip tests batch proof generation and
// verification through the gnark backend.
func TestGnarkIntegration_BatchProofRoundTrip(t *testing.T) {
	trace := buildGnarkCompatibleTrace(t)

	circuit, err := NewRVBatchCircuit(BatchSize256)
	if err != nil {
		t.Fatalf("NewRVBatchCircuit: %v", err)
	}
	circuit.Compile()

	proof, err := ProveBatch(circuit, trace)
	if err != nil {
		t.Fatalf("ProveBatch: %v", err)
	}
	if proof.StepCount != uint32(len(trace.Steps)) {
		t.Errorf("StepCount: got %d, want %d", proof.StepCount, len(trace.Steps))
	}

	valid, err := VerifyBatchProof(proof, trace)
	if err != nil {
		t.Fatalf("VerifyBatchProof: %v", err)
	}
	if !valid {
		t.Error("batch proof should verify")
	}
}

// TestGnarkIntegration_BatchProofToMandatory tests conversion for the
// MandatoryProofSystem.
func TestGnarkIntegration_BatchProofToMandatory(t *testing.T) {
	trace := buildGnarkCompatibleTrace(t)

	circuit, err := NewRVBatchCircuit(BatchSize256)
	if err != nil {
		t.Fatalf("NewRVBatchCircuit: %v", err)
	}
	circuit.Compile()

	proof, err := ProveBatch(circuit, trace)
	if err != nil {
		t.Fatalf("ProveBatch: %v", err)
	}

	proofType, data := BatchProofToMandatory(proof)
	if proofType != "ZK-SNARK-BATCH-256" {
		t.Errorf("proofType = %s, want ZK-SNARK-BATCH-256", proofType)
	}
	if len(data) != 360 {
		t.Errorf("data length = %d, want 360", len(data))
	}
}

// TestGnarkIntegration_BackendTypePreserved ensures backend type is preserved
// across SetProofBackend calls.
func TestGnarkIntegration_BackendTypePreserved(t *testing.T) {
	origBackend := GetProofBackend()
	defer SetProofBackend(origBackend)

	SetProofBackend(ProofBackendSimulated)
	if GetProofBackend() != ProofBackendSimulated {
		t.Error("expected simulated backend")
	}

	SetProofBackend(ProofBackendGnark)
	if GetProofBackend() != ProofBackendGnark {
		t.Error("expected gnark backend")
	}

	SetProofBackend(ProofBackendSimulated)
	if GetProofBackend() != ProofBackendSimulated {
		t.Error("expected simulated backend after reset")
	}
}

// buildGnarkCompatibleTrace builds a trace from a simple arithmetic program
// that is compatible with the gnark circuit checker. Unlike buildTestTrace,
// this produces steps where every step has correct pre/post register state.
func buildGnarkCompatibleTrace(t *testing.T) *RVWitnessCollector {
	t.Helper()
	instrs := []uint32{
		EncodeIType(0x13, 1, 0, 0, 7),    // ADDI x1, x0, 7
		EncodeIType(0x13, 2, 0, 0, 6),    // ADDI x2, x0, 6
		EncodeRType(0x33, 3, 0, 1, 2, 0), // ADD x3, x1, x2 = 13
		EncodeIType(0x13, 17, 0, 0, 0),   // ADDI x17, x0, 0 (a7=halt)
		EncodeIType(0x13, 10, 0, 0, 0),   // ADDI x10, x0, 0 (exit code 0)
		0x00000073,                       // ECALL (halt)
	}
	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}

	cpu := NewRVCPU(100)
	if err := cpu.LoadProgram(code, 0, 0); err != nil {
		t.Fatalf("LoadProgram: %v", err)
	}
	cpu.Regs[17] = RVEcallHalt
	cpu.Witness = NewRVWitnessCollector()

	if err := cpu.Run(); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return cpu.Witness
}
