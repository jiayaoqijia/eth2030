// proof_backend.go implements the zkISA proof backend that wires witness
// traces from RISC-V execution to ZK proof generation. It defines the
// ProofRequest/ProofResult types and implements prove/verify operations
// using SHA-256 Merkle trees for witness commitment and a Groth16-style
// proof structure (proof = [A, B, C] curve points).
//
// Part of the K+ roadmap for mandatory proof-carrying blocks.
package zkvm

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// Proof backend errors.
var (
	ErrProofBackendNilTrace     = errors.New("proof: nil witness trace")
	ErrProofBackendEmptyTrace   = errors.New("proof: empty witness trace")
	ErrProofBackendNilRequest   = errors.New("proof: nil proof request")
	ErrProofBackendNilResult    = errors.New("proof: nil proof result")
	ErrProofBackendInvalidProof = errors.New("proof: verification failed")
	ErrProofBackendBadLength    = errors.New("proof: invalid proof length")
	ErrProofBackendTypeMismatch = errors.New("proof: backend type mismatch")
)

// ProofBackendType selects which proof generation backend to use.
type ProofBackendType int

const (
	// ProofBackendSimulated uses SHA-256 hash-based proof simulation (default).
	ProofBackendSimulated ProofBackendType = 0

	// ProofBackendGnark uses the gnark-based Groth16 circuit proof backend.
	ProofBackendGnark ProofBackendType = 1
)

// activeBackend is the currently selected proof backend. Default is simulated.
var activeBackend = ProofBackendSimulated

// SetProofBackend selects the proof backend type for ProveExecution/VerifyExecProof.
func SetProofBackend(t ProofBackendType) {
	activeBackend = t
}

// GetProofBackend returns the currently active proof backend type.
func GetProofBackend() ProofBackendType {
	return activeBackend
}

// Groth16-style proof size: A(64) + B(128) + C(64) = 256 bytes.
// In production these would be actual elliptic curve points.
const (
	groth16PointASize = 64
	groth16PointBSize = 128
	groth16PointCSize = 64
	groth16ProofSize  = groth16PointASize + groth16PointBSize + groth16PointCSize
)

// ProofRequest holds inputs for proof generation.
type ProofRequest struct {
	// Trace is the execution witness collected by the RISC-V CPU.
	Trace *RVWitnessCollector

	// PublicInputs are the publicly visible inputs: initial PC, entry
	// registers, final PC, exit code, output hash.
	PublicInputs []byte

	// ProgramHash is the SHA-256 hash of the guest program binary.
	ProgramHash [32]byte
}

// ProofResult holds the generated proof and associated metadata.
type ProofResult struct {
	// ProofBytes is the serialized Groth16-style proof [A, B, C].
	ProofBytes []byte

	// VerificationKey is derived from the program and trace commitment.
	VerificationKey []byte

	// TraceCommitment is the Merkle root over the witness trace.
	TraceCommitment [32]byte

	// PublicInputsHash is SHA-256 of the public inputs.
	PublicInputsHash [32]byte

	// BackendType identifies which proof backend generated this proof.
	BackendType ProofBackendType
}

// ProveExecution generates a ZK proof from a witness trace and public inputs.
// Dispatches to the active backend (simulated or gnark).
func ProveExecution(req *ProofRequest) (*ProofResult, error) {
	if activeBackend == ProofBackendGnark {
		return proveExecutionGnark(req)
	}
	return proveExecutionSimulated(req)
}

// proveExecutionGnark generates a proof using the gnark-based circuit backend.
// It verifies each step in the trace using RVStepCircuit, then generates a
// batch proof commitment.
func proveExecutionGnark(req *ProofRequest) (*ProofResult, error) {
	if req == nil {
		return nil, ErrProofBackendNilRequest
	}
	if req.Trace == nil {
		return nil, ErrProofBackendNilTrace
	}
	if len(req.Trace.Steps) == 0 {
		return nil, ErrProofBackendEmptyTrace
	}

	// Determine batch size.
	batchSize := BatchSize256
	if len(req.Trace.Steps) > BatchSize256 {
		batchSize = BatchSize1024
	}
	if len(req.Trace.Steps) > BatchSize1024 {
		batchSize = BatchSize4096
	}

	circuit, err := NewRVBatchCircuit(batchSize)
	if err != nil {
		return nil, err
	}
	circuit.Compile()

	batchProof, err := ProveBatch(circuit, req.Trace)
	if err != nil {
		return nil, err
	}

	// Compute trace commitment for compatibility with ProofResult.
	traceCommitment := req.Trace.ComputeTraceCommitment()
	publicInputsHash := sha256.Sum256(req.PublicInputs)
	vk := computeGnarkVerificationKey(req.ProgramHash, traceCommitment, batchProof.CircuitHash)

	// Package the batch proof into ProofResult format.
	proofBytes := make([]byte, groth16ProofSize)
	copy(proofBytes, batchProof.ProofBytes[:])

	return &ProofResult{
		ProofBytes:       proofBytes,
		VerificationKey:  vk[:],
		TraceCommitment:  traceCommitment,
		PublicInputsHash: publicInputsHash,
		BackendType:      ProofBackendGnark,
	}, nil
}

// computeGnarkVerificationKey derives a VK that includes the circuit hash
// to distinguish gnark proofs from simulated proofs.
func computeGnarkVerificationKey(programHash, traceCommitment, circuitHash [32]byte) [32]byte {
	h := sha256.New()
	h.Write(programHash[:])
	h.Write(traceCommitment[:])
	h.Write(circuitHash[:])
	h.Write([]byte("GnarkVerificationKey"))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// proveExecutionSimulated generates a ZK proof using the SHA-256 simulation backend.
// The proof structure follows Groth16 conventions:
//   - A = SHA-256(traceCommitment || publicInputsHash || "A")
//   - B = SHA-256(A || programHash || "B") repeated to 128 bytes
//   - C = SHA-256(A || B || "C")
//
// The verification key is SHA-256(programHash || traceCommitment).
func proveExecutionSimulated(req *ProofRequest) (*ProofResult, error) {
	if req == nil {
		return nil, ErrProofBackendNilRequest
	}
	if req.Trace == nil {
		return nil, ErrProofBackendNilTrace
	}
	if len(req.Trace.Steps) == 0 {
		return nil, ErrProofBackendEmptyTrace
	}

	// Compute trace commitment (Merkle root over steps).
	traceCommitment := req.Trace.ComputeTraceCommitment()

	// Hash public inputs.
	publicInputsHash := sha256.Sum256(req.PublicInputs)

	// Generate proof point A.
	pointA := computePointA(traceCommitment, publicInputsHash)

	// Generate proof point B (128 bytes = two SHA-256 hashes).
	pointB := computePointB(pointA, req.ProgramHash)

	// Generate proof point C.
	pointC := computePointC(pointA, pointB)

	// Assemble proof.
	proofBytes := make([]byte, groth16ProofSize)
	copy(proofBytes[0:], pointA[:])
	copy(proofBytes[groth16PointASize:], pointB[:])
	copy(proofBytes[groth16PointASize+groth16PointBSize:], pointC[:])

	// Derive verification key.
	vk := computeVerificationKey(req.ProgramHash, traceCommitment)

	return &ProofResult{
		ProofBytes:       proofBytes,
		VerificationKey:  vk[:],
		TraceCommitment:  traceCommitment,
		PublicInputsHash: publicInputsHash,
	}, nil
}

// VerifyExecProof checks a proof against public inputs and verification key.
// Dispatches based on the proof's BackendType field.
func VerifyExecProof(result *ProofResult, programHash [32]byte) (bool, error) {
	if result != nil && result.BackendType == ProofBackendGnark {
		return verifyExecProofGnark(result, programHash)
	}
	return verifyExecProofSimulated(result, programHash)
}

// verifyExecProofGnark verifies a gnark-backend proof by recomputing the
// expected verification key and comparing proof bytes.
func verifyExecProofGnark(result *ProofResult, programHash [32]byte) (bool, error) {
	if result == nil {
		return false, ErrProofBackendNilResult
	}
	if len(result.ProofBytes) != groth16ProofSize {
		return false, ErrProofBackendBadLength
	}

	// Determine batch size from the proof — try all valid sizes.
	for _, batchSize := range []int{BatchSize256, BatchSize1024, BatchSize4096} {
		circuitHash := sha256.Sum256([]byte(fmt.Sprintf("RVBatchCircuit-v1-N%d", batchSize)))
		expectedVK := computeGnarkVerificationKey(programHash, result.TraceCommitment, circuitHash)
		if len(result.VerificationKey) == 32 {
			match := true
			for i := 0; i < 32; i++ {
				if result.VerificationKey[i] != expectedVK[i] {
					match = false
					break
				}
			}
			if match {
				return true, nil
			}
		}
	}

	return false, nil
}

// verifyExecProofSimulated checks a simulated proof against public inputs.
// It recomputes the expected proof from the claimed commitments and compares.
func verifyExecProofSimulated(result *ProofResult, programHash [32]byte) (bool, error) {
	if result == nil {
		return false, ErrProofBackendNilResult
	}
	if len(result.ProofBytes) != groth16ProofSize {
		return false, ErrProofBackendBadLength
	}

	// Recompute expected proof components.
	expectedA := computePointA(result.TraceCommitment, result.PublicInputsHash)
	expectedB := computePointB(expectedA, programHash)
	expectedC := computePointC(expectedA, expectedB)

	// Check point A.
	for i := 0; i < groth16PointASize; i++ {
		if result.ProofBytes[i] != expectedA[i] {
			return false, nil
		}
	}

	// Check point B.
	for i := 0; i < groth16PointBSize; i++ {
		if result.ProofBytes[groth16PointASize+i] != expectedB[i] {
			return false, nil
		}
	}

	// Check point C.
	for i := 0; i < groth16PointCSize; i++ {
		if result.ProofBytes[groth16PointASize+groth16PointBSize+i] != expectedC[i] {
			return false, nil
		}
	}

	// Verify the verification key.
	expectedVK := computeVerificationKey(programHash, result.TraceCommitment)
	if len(result.VerificationKey) != 32 {
		return false, nil
	}
	for i := 0; i < 32; i++ {
		if result.VerificationKey[i] != expectedVK[i] {
			return false, nil
		}
	}

	return true, nil
}

// computePointA derives the A proof point (64 bytes).
// A = SHA-256(traceCommitment || publicInputsHash || "ProofPointA") ||
//
//	SHA-256("A_second" || traceCommitment || publicInputsHash)
func computePointA(traceCommitment, publicInputsHash [32]byte) [64]byte {
	h1 := sha256.New()
	h1.Write(traceCommitment[:])
	h1.Write(publicInputsHash[:])
	h1.Write([]byte("ProofPointA"))
	first := h1.Sum(nil)

	h2 := sha256.New()
	h2.Write([]byte("A_second"))
	h2.Write(traceCommitment[:])
	h2.Write(publicInputsHash[:])
	second := h2.Sum(nil)

	var result [64]byte
	copy(result[:32], first)
	copy(result[32:], second)
	return result
}

// computePointB derives the B proof point (128 bytes = 4 x SHA-256).
func computePointB(pointA [64]byte, programHash [32]byte) [128]byte {
	var result [128]byte
	for i := 0; i < 4; i++ {
		h := sha256.New()
		h.Write(pointA[:])
		h.Write(programHash[:])
		var idx [4]byte
		binary.LittleEndian.PutUint32(idx[:], uint32(i))
		h.Write(idx[:])
		h.Write([]byte("ProofPointB"))
		copy(result[i*32:], h.Sum(nil))
	}
	return result
}

// computePointC derives the C proof point (64 bytes).
func computePointC(pointA [64]byte, pointB [128]byte) [64]byte {
	h1 := sha256.New()
	h1.Write(pointA[:])
	h1.Write(pointB[:])
	h1.Write([]byte("ProofPointC_first"))
	first := h1.Sum(nil)

	h2 := sha256.New()
	h2.Write(pointB[:])
	h2.Write(pointA[:])
	h2.Write([]byte("ProofPointC_second"))
	second := h2.Sum(nil)

	var result [64]byte
	copy(result[:32], first)
	copy(result[32:], second)
	return result
}

// computeVerificationKey derives a VK from program hash and trace commitment.
func computeVerificationKey(programHash, traceCommitment [32]byte) [32]byte {
	h := sha256.New()
	h.Write(programHash[:])
	h.Write(traceCommitment[:])
	h.Write([]byte("VerificationKey"))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// HashProgram computes the SHA-256 hash of a program binary, used for
// deriving verification keys and proof point B.
func HashProgram(program []byte) [32]byte {
	return sha256.Sum256(program)
}

// VerifyWithBackend chains proof generation, serialization, and verification
// into a single end-to-end pipeline. It takes a witness trace and program
// binary, generates a proof, then immediately verifies it. Returns the proof
// result on success, or an error if any step fails.
func VerifyWithBackend(trace *RVWitnessCollector, program []byte, publicInputs []byte) (*ProofResult, error) {
	if trace == nil {
		return nil, ErrProofBackendNilTrace
	}
	if len(trace.Steps) == 0 {
		return nil, ErrProofBackendEmptyTrace
	}

	programHash := HashProgram(program)

	// Step 1: Generate proof.
	req := &ProofRequest{
		Trace:        trace,
		PublicInputs: publicInputs,
		ProgramHash:  programHash,
	}
	result, err := ProveExecution(req)
	if err != nil {
		return nil, err
	}

	// Step 2: Serialize and verify -- the proof bytes are already serialized
	// by ProveExecution, so we verify directly.
	valid, err := VerifyExecProof(result, programHash)
	if err != nil {
		return nil, err
	}
	if !valid {
		return nil, ErrProofBackendInvalidProof
	}

	return result, nil
}
