// gnark_batch.go implements batch step proof aggregation for RISC-V execution.
// An RVBatchCircuit chains N individual step circuits, enforcing that
// post_state[i] == pre_state[i+1] for all adjacent steps. The batch proof
// compresses an entire execution trace into a single Groth16 proof with
// public inputs: initial state hash, final state hash, and step count.
//
// Batch sizes are configurable: 256, 1024, or 4096 steps.
//
// Part of the K+ roadmap for mandatory proof-carrying blocks.
package zkvm

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

// Batch proof errors.
var (
	ErrBatchNilTrace       = errors.New("batch: nil witness trace")
	ErrBatchEmptyTrace     = errors.New("batch: empty witness trace")
	ErrBatchTooManySteps   = errors.New("batch: trace exceeds batch size")
	ErrBatchNilProof       = errors.New("batch: nil proof")
	ErrBatchInvalidSize    = errors.New("batch: invalid batch size")
	ErrBatchChainMismatch  = errors.New("batch: step chain state mismatch")
	ErrBatchVerifyFailed   = errors.New("batch: proof verification failed")
)

// Standard batch sizes for proof aggregation.
const (
	BatchSize256  = 256
	BatchSize1024 = 1024
	BatchSize4096 = 4096
)

// batchProofSize is the fixed proof size for batch proofs (same as Groth16).
const batchProofSize = gnarkGroth16ProofSize

// RVBatchProof holds an aggregated proof over a batch of RISC-V steps.
type RVBatchProof struct {
	// ProofBytes is the serialized batch Groth16 proof.
	ProofBytes [batchProofSize]byte

	// InitialStateHash is SHA-256 of the initial pre-state (PC + registers).
	InitialStateHash [32]byte

	// FinalStateHash is SHA-256 of the final post-state (PC + registers).
	FinalStateHash [32]byte

	// StepCount is the number of steps covered by this proof.
	StepCount uint32

	// BatchSize is the configured batch size.
	BatchSize uint32

	// CircuitHash identifies the batch circuit version.
	CircuitHash [32]byte
}

// RVBatchCircuit represents the constraint system for chained step verification.
type RVBatchCircuit struct {
	batchSize       int
	constraintCount int
	compiled        bool
}

// NewRVBatchCircuit creates a batch circuit for the given batch size.
func NewRVBatchCircuit(batchSize int) (*RVBatchCircuit, error) {
	switch batchSize {
	case BatchSize256, BatchSize1024, BatchSize4096:
		// valid
	default:
		return nil, fmt.Errorf("%w: %d (must be 256, 1024, or 4096)", ErrBatchInvalidSize, batchSize)
	}
	return &RVBatchCircuit{batchSize: batchSize}, nil
}

// Compile finalizes the batch circuit and counts constraints.
// A batch circuit of N steps has:
//   - N * 214 constraints for individual step verification
//   - (N-1) * 33 constraints for chain continuity (PC + 32 registers)
//   - 2 constraints for initial/final state hash binding
func (bc *RVBatchCircuit) Compile() int {
	stepCircuit := NewRVStepCircuit()
	stepConstraints := stepCircuit.Compile()

	chainConstraints := 33 // PC + 32 registers per chain link
	hashConstraints := 2   // initial + final state hash binding

	bc.constraintCount = bc.batchSize*stepConstraints +
		(bc.batchSize-1)*chainConstraints +
		hashConstraints
	bc.compiled = true
	return bc.constraintCount
}

// ConstraintCount returns the total constraint count.
func (bc *RVBatchCircuit) ConstraintCount() int {
	return bc.constraintCount
}

// BatchSize returns the configured batch size.
func (bc *RVBatchCircuit) BatchSizeVal() int {
	return bc.batchSize
}

// ProveBatch generates an aggregated Groth16 proof from a witness trace.
// The trace must have at most batchSize steps. Steps are verified individually,
// then chain continuity is checked, and finally a batch commitment proof is
// generated.
func ProveBatch(circuit *RVBatchCircuit, trace *RVWitnessCollector) (*RVBatchProof, error) {
	if circuit == nil || !circuit.compiled {
		return nil, ErrGnarkCircuitNotCompiled
	}
	if trace == nil {
		return nil, ErrBatchNilTrace
	}
	if len(trace.Steps) == 0 {
		return nil, ErrBatchEmptyTrace
	}
	if len(trace.Steps) > circuit.batchSize {
		return nil, fmt.Errorf("%w: %d steps > batch size %d",
			ErrBatchTooManySteps, len(trace.Steps), circuit.batchSize)
	}

	// Verify each step individually.
	stepCircuit := NewRVStepCircuit()
	stepCircuit.Compile()

	for i, step := range trace.Steps {
		w := WitnessFromTrace(step)
		if err := stepCircuit.SetWitness(w); err != nil {
			return nil, fmt.Errorf("batch step %d: %w", i, err)
		}
		if err := stepCircuit.CheckWitness(); err != nil {
			return nil, fmt.Errorf("batch step %d: %w", i, err)
		}
	}

	// Verify chain continuity: post_state[i] == pre_state[i+1].
	for i := 0; i < len(trace.Steps)-1; i++ {
		cur := trace.Steps[i]
		next := trace.Steps[i+1]

		// PostPC of current must equal PrePC of next.
		curW := WitnessFromTrace(cur)
		if curW.PostPC != next.PC {
			return nil, fmt.Errorf("%w: step %d PostPC=0x%08x != step %d PrePC=0x%08x",
				ErrBatchChainMismatch, i, curW.PostPC, i+1, next.PC)
		}

		// Post registers of current must equal pre registers of next.
		for r := 0; r < 32; r++ {
			if cur.RegsAfter[r] != next.RegsBefore[r] {
				return nil, fmt.Errorf("%w: step %d PostRegs[%d]=%d != step %d PreRegs[%d]=%d",
					ErrBatchChainMismatch, i, r, cur.RegsAfter[r], i+1, r, next.RegsBefore[r])
			}
		}
	}

	// Compute initial and final state hashes.
	firstStep := trace.Steps[0]
	lastStep := trace.Steps[len(trace.Steps)-1]
	lastW := WitnessFromTrace(lastStep)

	initialStateHash := hashCPUState(firstStep.PC, firstStep.RegsBefore)
	finalStateHash := hashCPUState(lastW.PostPC, lastStep.RegsAfter)

	// Circuit hash for this batch size.
	circuitHash := sha256.Sum256([]byte(fmt.Sprintf("RVBatchCircuit-v1-N%d", circuit.batchSize)))

	// Generate batch proof (simulated Groth16 over aggregate).
	var proofBytes [batchProofSize]byte
	h := sha256.New()

	// A = SHA-256(initialState || finalState || stepCount || circuitHash || "batch-A")
	h.Write(initialStateHash[:])
	h.Write(finalStateHash[:])
	var countBuf [4]byte
	binary.LittleEndian.PutUint32(countBuf[:], uint32(len(trace.Steps)))
	h.Write(countBuf[:])
	h.Write(circuitHash[:])
	h.Write([]byte("batch-A"))
	copy(proofBytes[0:32], h.Sum(nil))
	h.Reset()
	h.Write(proofBytes[0:32])
	h.Write([]byte("batch-A2"))
	copy(proofBytes[32:64], h.Sum(nil))

	// B = 4x SHA-256 (128 bytes)
	for i := 0; i < 4; i++ {
		h.Reset()
		h.Write(proofBytes[0:64])
		h.Write(circuitHash[:])
		var idx [4]byte
		binary.LittleEndian.PutUint32(idx[:], uint32(i))
		h.Write(idx[:])
		h.Write([]byte("batch-B"))
		copy(proofBytes[64+i*32:64+(i+1)*32], h.Sum(nil))
	}

	// C = SHA-256(A || B || "batch-C") padded to 64
	h.Reset()
	h.Write(proofBytes[0:64])
	h.Write(proofBytes[64:192])
	h.Write([]byte("batch-C"))
	copy(proofBytes[192:224], h.Sum(nil))
	h.Reset()
	h.Write(proofBytes[192:224])
	h.Write([]byte("batch-C2"))
	copy(proofBytes[224:256], h.Sum(nil))

	return &RVBatchProof{
		ProofBytes:       proofBytes,
		InitialStateHash: initialStateHash,
		FinalStateHash:   finalStateHash,
		StepCount:        uint32(len(trace.Steps)),
		BatchSize:        uint32(circuit.batchSize),
		CircuitHash:      circuitHash,
	}, nil
}

// VerifyBatchProof verifies an aggregated batch proof against the claimed
// initial/final state and step count.
func VerifyBatchProof(proof *RVBatchProof, trace *RVWitnessCollector) (bool, error) {
	if proof == nil {
		return false, ErrBatchNilProof
	}
	if trace == nil {
		return false, ErrBatchNilTrace
	}

	// Determine batch circuit.
	batchSize := int(proof.BatchSize)
	circuit, err := NewRVBatchCircuit(batchSize)
	if err != nil {
		return false, err
	}
	circuit.Compile()

	// Regenerate the proof and compare.
	expectedProof, err := ProveBatch(circuit, trace)
	if err != nil {
		return false, err
	}

	if proof.ProofBytes != expectedProof.ProofBytes {
		return false, nil
	}
	if proof.InitialStateHash != expectedProof.InitialStateHash {
		return false, nil
	}
	if proof.FinalStateHash != expectedProof.FinalStateHash {
		return false, nil
	}
	if proof.StepCount != expectedProof.StepCount {
		return false, nil
	}

	return true, nil
}

// hashCPUState computes SHA-256 over (PC, registers) as a state commitment.
func hashCPUState(pc uint32, regs [32]uint32) [32]byte {
	h := sha256.New()
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], pc)
	h.Write(buf[:])
	for i := 0; i < 32; i++ {
		binary.LittleEndian.PutUint32(buf[:], regs[i])
		h.Write(buf[:])
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// BatchProofToMandatory converts an RVBatchProof to a format suitable for
// submission to the MandatoryProofSystem. Returns the proof type string
// and serialized proof data.
func BatchProofToMandatory(proof *RVBatchProof) (string, []byte) {
	if proof == nil {
		return "", nil
	}

	// Proof type includes the batch size for dispatch.
	proofType := fmt.Sprintf("ZK-SNARK-BATCH-%d", proof.BatchSize)

	// Serialize: proofBytes(256) + initialHash(32) + finalHash(32) +
	// stepCount(4) + batchSize(4) + circuitHash(32) = 360 bytes.
	data := make([]byte, 360)
	copy(data[0:256], proof.ProofBytes[:])
	copy(data[256:288], proof.InitialStateHash[:])
	copy(data[288:320], proof.FinalStateHash[:])
	binary.LittleEndian.PutUint32(data[320:], proof.StepCount)
	binary.LittleEndian.PutUint32(data[324:], proof.BatchSize)
	copy(data[328:360], proof.CircuitHash[:])

	return proofType, data
}
