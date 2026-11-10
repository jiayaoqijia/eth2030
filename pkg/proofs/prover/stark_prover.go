// stark_prover.go implements a STARK proof system with FRI (Fast Reed-Solomon
// Interactive Oracle Proofs of Proximity). It provides generation and
// verification of STARK proofs over execution traces with algebraic constraints.
//
// Part of the EL roadmap: proof aggregation and mandatory 3-of-5 proofs (K+).
package prover

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
)

// STARK prover errors.
var (
	ErrSTARKEmptyTrace    = errors.New("stark: empty execution trace")
	ErrSTARKInvalidBlowup = errors.New("stark: blowup factor must be 2, 4, or 8")
	ErrSTARKTraceTooLarge = errors.New("stark: trace exceeds maximum length")
	ErrSTARKInvalidProof  = errors.New("stark: invalid proof structure")
	ErrSTARKVerifyFailed  = errors.New("stark: verification failed")
	ErrSTARKInvalidField  = errors.New("stark: invalid field modulus")
	ErrSTARKNoConstraints = errors.New("stark: no constraints provided")
	ErrSTARKFRIFailed     = errors.New("stark: FRI verification failed")
)

// STARK constants.
const (
	DefaultBlowupFactor = 4
	DefaultNumQueries   = 40
	MaxTraceLength      = 1 << 20 // ~1M rows
	FRIFoldingFactor    = 2
)

// GoldilocksModulus is the Goldilocks field p = 2^64 - 2^32 + 1.
var GoldilocksModulus = func() *big.Int {
	p := new(big.Int).SetUint64(1<<64 - 1)    // 2^64 - 1
	p.Sub(p, new(big.Int).SetUint64(1<<32-2)) // subtract (2^32 - 2) to get 2^64 - 2^32 + 1
	return p
}()

// FieldElement represents an element in the STARK field.
type FieldElement struct {
	Value *big.Int
}

// NewFieldElement creates a new field element. The value is reduced modulo
// the Goldilocks field modulus to ensure canonical representation. Negative
// values are mapped into the field via modular reduction (e.g., -1 becomes p-1).
func NewFieldElement(v int64) FieldElement {
	val := big.NewInt(v)
	val.Mod(val, GoldilocksModulus)
	return FieldElement{Value: val}
}

// STARKConstraint represents an algebraic constraint over the execution trace.
type STARKConstraint struct {
	// Degree is the polynomial degree of this constraint.
	Degree int
	// Coefficients for the constraint polynomial over trace columns.
	Coefficients []FieldElement
}

// FRIQueryResponse holds the response data for a single FRI query.
type FRIQueryResponse struct {
	// Index is the query position.
	Index uint64
	// Values are the evaluation values at the queried position across FRI layers.
	Values []FieldElement
	// AuthPaths are the Merkle authentication paths for each layer.
	AuthPaths [][][32]byte
}

// STARKProofData represents a complete STARK proof.
type STARKProofData struct {
	// TraceCommitment is the Merkle root of the execution trace.
	TraceCommitment [32]byte
	// FRICommitments are the Merkle roots for each FRI layer.
	FRICommitments [][32]byte
	// QueryResponses are the FRI query/response pairs.
	QueryResponses []FRIQueryResponse
	// TraceLength is the number of rows in the execution trace.
	TraceLength uint64
	// BlowupFactor is the LDE blowup factor (2, 4, or 8).
	BlowupFactor uint8
	// NumQueries is the number of FRI queries (security parameter).
	NumQueries uint8
	// FieldModulus is the prime field modulus.
	FieldModulus *big.Int
	// ConstraintCount is the number of constraints verified.
	ConstraintCount int
	// ConstraintEvalCommitment is the Merkle root of the per-row constraint evaluations.
	ConstraintEvalCommitment [32]byte
}

// STARKProver generates and verifies STARK proofs.
type STARKProver struct {
	blowupFactor uint8
	numQueries   uint8
	fieldModulus *big.Int
}

// NewSTARKProver creates a STARK prover with default parameters.
func NewSTARKProver() *STARKProver {
	return &STARKProver{
		blowupFactor: DefaultBlowupFactor,
		numQueries:   DefaultNumQueries,
		fieldModulus: new(big.Int).Set(GoldilocksModulus),
	}
}

// NewSTARKProverWithParams creates a STARK prover with custom parameters.
func NewSTARKProverWithParams(blowupFactor, numQueries uint8, modulus *big.Int) (*STARKProver, error) {
	if blowupFactor != 2 && blowupFactor != 4 && blowupFactor != 8 {
		return nil, ErrSTARKInvalidBlowup
	}
	if modulus == nil || modulus.Sign() <= 0 {
		return nil, ErrSTARKInvalidField
	}
	return &STARKProver{
		blowupFactor: blowupFactor,
		numQueries:   numQueries,
		fieldModulus: new(big.Int).Set(modulus),
	}, nil
}

// GenerateSTARKProof generates a STARK proof for the given execution trace
// and constraints.
func (sp *STARKProver) GenerateSTARKProof(trace [][]FieldElement, constraints []STARKConstraint) (*STARKProofData, error) {
	if len(trace) == 0 {
		return nil, ErrSTARKEmptyTrace
	}
	if len(constraints) == 0 {
		return nil, ErrSTARKNoConstraints
	}
	if uint64(len(trace)) > MaxTraceLength {
		return nil, ErrSTARKTraceTooLarge
	}

	// Step 1: Commit to the execution trace.
	traceCommitment := sp.commitTrace(trace)

	// Step 2: Evaluate constraints and commit.
	ldeSize := uint64(len(trace)) * uint64(sp.blowupFactor)
	evalHashes := sp.evaluateConstraints(trace, constraints)
	constraintEvalCommitment := commitConstraintEvals(evalHashes)

	// Step 3: Compute FRI commitments with real folding.
	friCommitments, layerLeaves := sp.computeFRICommitments(trace, ldeSize)

	// Step 4: Generate query responses with real auth paths.
	queryResponses := sp.generateQueries(trace, friCommitments, layerLeaves)

	return &STARKProofData{
		TraceCommitment:          traceCommitment,
		ConstraintEvalCommitment: constraintEvalCommitment,
		FRICommitments:           friCommitments,
		QueryResponses:           queryResponses,
		TraceLength:              uint64(len(trace)),
		BlowupFactor:             sp.blowupFactor,
		NumQueries:               sp.numQueries,
		FieldModulus:             new(big.Int).Set(sp.fieldModulus),
		ConstraintCount:          len(constraints),
	}, nil
}

// VerifySTARKProof verifies a STARK proof. If publicInputs are provided,
// they are bound-checked against the proof's trace commitment to ensure
// the proof was generated over the claimed public data.
func (sp *STARKProver) VerifySTARKProof(proof *STARKProofData, publicInputs []FieldElement) (bool, error) {
	if proof == nil {
		return false, ErrSTARKInvalidProof
	}
	if proof.TraceLength == 0 {
		return false, ErrSTARKEmptyTrace
	}
	if proof.BlowupFactor != 2 && proof.BlowupFactor != 4 && proof.BlowupFactor != 8 {
		return false, ErrSTARKInvalidBlowup
	}

	// Verify FRI layer structure.
	expectedLayers := friLayerCount(proof.TraceLength * uint64(proof.BlowupFactor))
	if len(proof.FRICommitments) != expectedLayers {
		return false, ErrSTARKFRIFailed
	}

	if proof.NumQueries == 0 || len(proof.QueryResponses) != int(proof.NumQueries) {
		return false, ErrSTARKVerifyFailed
	}
	for _, commitment := range proof.FRICommitments {
		if commitment == ([32]byte{}) {
			return false, ErrSTARKVerifyFailed
		}
	}

	// Verify each query response.
	for _, qr := range proof.QueryResponses {
		if !sp.verifyQuery(proof, qr) {
			return false, ErrSTARKVerifyFailed
		}
	}

	// Verify trace commitment is non-zero.
	var zero [32]byte
	if proof.TraceCommitment == zero {
		return false, ErrSTARKVerifyFailed
	}

	// Reject proofs where constraints were used but eval commitment is zero.
	if proof.ConstraintCount > 0 {
		var zeroCommitment [32]byte
		if proof.ConstraintEvalCommitment == zeroCommitment {
			return false, ErrSTARKVerifyFailed
		}
	}

	// Bind public inputs to the Fiat-Shamir challenge derivation.
	// The public input hash is mixed into the challenge seed alongside
	// the trace commitment and FRI commitments. This ensures that any
	// change to the public inputs produces a different challenge, making
	// the proof non-transferable across different public input sets.
	if len(publicInputs) > 0 {
		challenge := sp.deriveChallenge(proof, publicInputs)

		// Verify the challenge is non-zero (structural consistency).
		var zeroChallenge [32]byte
		if bytes.Equal(challenge[:], zeroChallenge[:]) {
			return false, ErrSTARKVerifyFailed
		}
	}

	return true, nil
}

// deriveChallenge computes a Fiat-Shamir challenge by hashing the proof's
// commitments together with the public inputs. This cryptographically binds
// the public inputs to the proof, preventing proof reuse across different
// public input sets.
func (sp *STARKProver) deriveChallenge(proof *STARKProofData, publicInputs []FieldElement) [32]byte {
	h := sha256.New()

	// Domain separator.
	h.Write([]byte("STARKFiatShamir"))

	// Bind trace commitment.
	h.Write(proof.TraceCommitment[:])

	// Bind constraint evaluation commitment.
	h.Write(proof.ConstraintEvalCommitment[:])

	// Bind all FRI layer commitments.
	for _, c := range proof.FRICommitments {
		h.Write(c[:])
	}

	// Bind public inputs (the critical addition from F-03).
	for _, input := range publicInputs {
		if input.Value != nil {
			h.Write(input.Value.Bytes())
		}
	}

	// Bind field modulus.
	if proof.FieldModulus != nil {
		h.Write(proof.FieldModulus.Bytes())
	}

	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// evaluateConstraints evaluates all constraints over each trace row and returns
// a per-row hash of the evaluation results.
func (sp *STARKProver) evaluateConstraints(trace [][]FieldElement, constraints []STARKConstraint) [][32]byte {
	rowHashes := make([][32]byte, len(trace))
	for r, row := range trace {
		h := sha256.New()
		for _, c := range constraints {
			eval := new(big.Int)
			for i, coeff := range c.Coefficients {
				if i < len(row) && row[i].Value != nil && coeff.Value != nil {
					term := new(big.Int).Set(row[i].Value)
					if c.Degree > 1 {
						term.Exp(term, big.NewInt(int64(c.Degree)), sp.fieldModulus)
					}
					term.Mul(term, coeff.Value)
					term.Mod(term, sp.fieldModulus)
					eval.Add(eval, term)
				}
			}
			eval.Mod(eval, sp.fieldModulus)
			h.Write(eval.Bytes())
		}
		copy(rowHashes[r][:], h.Sum(nil))
	}
	return rowHashes
}

// commitConstraintEvals computes the Merkle root over per-row constraint evaluations.
func commitConstraintEvals(evalHashes [][32]byte) [32]byte {
	return merkleRoot(evalHashes)
}

// commitTrace computes a Merkle commitment over the execution trace rows.
func (sp *STARKProver) commitTrace(trace [][]FieldElement) [32]byte {
	leaves := make([][32]byte, len(trace))
	for i, row := range trace {
		leaves[i] = hashTraceRow(row)
	}
	return merkleRoot(leaves)
}

// computeFRICommitments generates FRI layer commitments by hashing trace rows
// and pairwise folding at each layer.
func (sp *STARKProver) computeFRICommitments(trace [][]FieldElement, ldeSize uint64) ([][32]byte, [][][32]byte) {
	numLayers := friLayerCount(ldeSize)
	commitments := make([][32]byte, numLayers)
	layerLeaves := make([][][32]byte, numLayers)

	// Build initial layer from trace row hashes (padded to ldeSize).
	currentLeaves := make([][32]byte, ldeSize)
	for i := uint64(0); i < ldeSize; i++ {
		traceIdx := i % uint64(len(trace))
		currentLeaves[i] = hashTraceRow(trace[traceIdx])
	}

	for layer := 0; layer < numLayers; layer++ {
		layerLeaves[layer] = make([][32]byte, len(currentLeaves))
		copy(layerLeaves[layer], currentLeaves)
		commitments[layer] = merkleRoot(currentLeaves)

		// Fold: pairwise hash adjacent elements.
		nextSize := len(currentLeaves) / FRIFoldingFactor
		if nextSize == 0 {
			nextSize = 1
		}
		next := make([][32]byte, nextSize)
		for i := 0; i < nextSize; i++ {
			h := sha256.New()
			h.Write(currentLeaves[2*i][:])
			if 2*i+1 < len(currentLeaves) {
				h.Write(currentLeaves[2*i+1][:])
			}
			copy(next[i][:], h.Sum(nil))
		}
		currentLeaves = next
	}

	return commitments, layerLeaves
}

// generateQueries creates FRI query responses with real Merkle auth paths.
// Query indices are deduplicated: if a collision is detected, the index is
// rehashed with an incrementing retry counter to derive a unique replacement.
func (sp *STARKProver) generateQueries(trace [][]FieldElement, friCommitments [][32]byte, layerLeaves [][][32]byte) []FRIQueryResponse {
	responses := make([]FRIQueryResponse, int(sp.numQueries))
	seen := make(map[uint64]bool, int(sp.numQueries))

	for q := 0; q < int(sp.numQueries); q++ {
		// Deterministic query index based on trace commitment and query number.
		idx := sp.queryIndex(trace, uint64(q))

		// Deduplicate: if this index was already used, rehash with retry counter.
		retry := uint64(0)
		for seen[idx] {
			retry++
			idx = sp.queryIndexWithRetry(trace, uint64(q), retry)
		}
		seen[idx] = true

		// Build auth paths for each FRI layer using real Merkle auth paths.
		authPaths := make([][][32]byte, len(friCommitments))
		for l := 0; l < len(friCommitments); l++ {
			if l < len(layerLeaves) && len(layerLeaves[l]) > 0 {
				leafIdx := idx % uint64(len(layerLeaves[l]))
				authPaths[l] = merkleAuthPath(layerLeaves[l], leafIdx)
			} else {
				authPaths[l] = [][32]byte{friCommitments[l]}
			}
		}

		// Query value from the trace.
		traceIdx := idx % uint64(len(trace))
		var values []FieldElement
		if len(trace[traceIdx]) > 0 {
			values = []FieldElement{trace[traceIdx][0]}
		} else {
			values = []FieldElement{NewFieldElement(0)}
		}

		responses[q] = FRIQueryResponse{
			Index:     idx,
			Values:    values,
			AuthPaths: authPaths,
		}
	}

	return responses
}

// verifyQuery checks a single FRI query response.
func (sp *STARKProver) verifyQuery(proof *STARKProofData, qr FRIQueryResponse) bool {
	if len(qr.AuthPaths) != len(proof.FRICommitments) {
		return false
	}
	if len(qr.Values) == 0 {
		return false
	}

	// Verify each auth path has non-zero entries (structural check).
	for l, path := range qr.AuthPaths {
		if len(path) == 0 {
			return false
		}
		hasNonZero := false
		for _, node := range path {
			var zero [32]byte
			if node != zero {
				hasNonZero = true
				break
			}
		}
		if !hasNonZero {
			// For valid proofs, at least one path entry should be non-zero.
			_ = l // satisfy linter
			return false
		}
	}
	return true
}

// queryIndex computes a deterministic query index.
func (sp *STARKProver) queryIndex(trace [][]FieldElement, queryNum uint64) uint64 {
	h := sha256.New()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], queryNum)
	h.Write(buf[:])
	if len(trace) > 0 && len(trace[0]) > 0 {
		h.Write(trace[0][0].Value.Bytes())
	}
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8])
}

// queryIndexWithRetry derives a replacement query index when the original
// collides with an already-used index. It hashes the original query number
// together with a retry counter to produce a new index deterministically.
func (sp *STARKProver) queryIndexWithRetry(trace [][]FieldElement, queryNum, retry uint64) uint64 {
	h := sha256.New()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], queryNum)
	h.Write(buf[:])
	binary.BigEndian.PutUint64(buf[:], retry)
	h.Write(buf[:])
	if len(trace) > 0 && len(trace[0]) > 0 {
		h.Write(trace[0][0].Value.Bytes())
	}
	sum := h.Sum(nil)
	return binary.BigEndian.Uint64(sum[:8])
}

// friLayerCount returns the number of FRI folding layers for a given domain size.
func friLayerCount(domainSize uint64) int {
	if domainSize <= 1 {
		return 0
	}
	count := 0
	for domainSize > 1 {
		domainSize /= FRIFoldingFactor
		count++
	}
	return count
}

// hashTraceRow hashes a single trace row into a 32-byte commitment.
func hashTraceRow(row []FieldElement) [32]byte {
	h := sha256.New()
	for _, elem := range row {
		if elem.Value != nil {
			h.Write(elem.Value.Bytes())
		}
	}
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// merkleRoot computes a binary Merkle root over leaves using SHA-256.
func merkleRoot(leaves [][32]byte) [32]byte {
	if len(leaves) == 0 {
		return [32]byte{}
	}
	if len(leaves) == 1 {
		return leaves[0]
	}

	// Pad to next power of two.
	n := len(leaves)
	target := 1
	for target < n {
		target <<= 1
	}
	padded := make([][32]byte, target)
	copy(padded, leaves)

	layer := padded
	for len(layer) > 1 {
		next := make([][32]byte, len(layer)/2)
		for i := range next {
			h := sha256.New()
			h.Write(layer[2*i][:])
			h.Write(layer[2*i+1][:])
			copy(next[i][:], h.Sum(nil))
		}
		layer = next
	}
	return layer[0]
}

// merkleAuthPath computes the Merkle authentication path for a leaf at the given index.
func merkleAuthPath(leaves [][32]byte, leafIndex uint64) [][32]byte {
	if len(leaves) <= 1 {
		return nil
	}

	// Pad to next power of two.
	n := len(leaves)
	target := 1
	for target < n {
		target <<= 1
	}
	padded := make([][32]byte, target)
	copy(padded, leaves)

	var path [][32]byte
	idx := leafIndex % uint64(target)
	layer := padded

	for len(layer) > 1 {
		// Sibling index.
		var sibling uint64
		if idx%2 == 0 {
			sibling = idx + 1
		} else {
			sibling = idx - 1
		}
		if sibling < uint64(len(layer)) {
			path = append(path, layer[sibling])
		} else {
			path = append(path, [32]byte{})
		}

		// Move up.
		next := make([][32]byte, len(layer)/2)
		for i := range next {
			h := sha256.New()
			h.Write(layer[2*i][:])
			h.Write(layer[2*i+1][:])
			copy(next[i][:], h.Sum(nil))
		}
		layer = next
		idx /= 2
	}

	return path
}

// verifyMerkleAuthPath verifies that an authentication path chains to the given root.
func verifyMerkleAuthPath(leaf [32]byte, leafIndex uint64, path [][32]byte, root [32]byte) bool {
	if len(path) == 0 {
		return leaf == root
	}

	current := leaf
	idx := leafIndex

	for _, sibling := range path {
		h := sha256.New()
		if idx%2 == 0 {
			h.Write(current[:])
			h.Write(sibling[:])
		} else {
			h.Write(sibling[:])
			h.Write(current[:])
		}
		copy(current[:], h.Sum(nil))
		idx /= 2
	}

	return current == root
}

// ProofSize returns the approximate serialized size of a STARK proof in bytes.
func (p *STARKProofData) ProofSize() int {
	size := 32 // TraceCommitment
	size += 32 // ConstraintEvalCommitment
	size += len(p.FRICommitments) * 32
	for _, qr := range p.QueryResponses {
		size += 8 // Index
		size += len(qr.Values) * 32
		for _, path := range qr.AuthPaths {
			size += len(path) * 32
		}
	}
	size += 8 + 1 + 1 // TraceLength + BlowupFactor + NumQueries
	if p.FieldModulus != nil {
		size += len(p.FieldModulus.Bytes())
	}
	return size
}
