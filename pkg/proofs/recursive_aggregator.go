// recursive_aggregator.go commits to batches of proofs with multiple
// strategies (sequential, parallel, recursive) and type-specific verifiers
// (SNARK, STARK, IPA). Aggregated proofs are bound by a Merkle root over
// individual proof roots, supporting batch splitting and recursive composition.
//
// Part of the EL roadmap: proof aggregation for mandatory 3-of-5 proofs (K+).
package proofs

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"sync"
)

// Recursive aggregation errors.
var (
	ErrRecAggNoProofs      = errors.New("rec_agg: no proofs to aggregate")
	ErrRecAggNilProof      = errors.New("rec_agg: nil aggregated proof")
	ErrRecAggEmptyRoots    = errors.New("rec_agg: empty individual roots")
	ErrRecAggRootMismatch  = errors.New("rec_agg: aggregate root mismatch")
	ErrRecAggCountMismatch = errors.New("rec_agg: count mismatch")
	ErrRecAggBatchSizeZero = errors.New("rec_agg: batch size must be positive")
	ErrRecAggNoVerifier    = errors.New("rec_agg: no verifier for proof type")
	ErrRecAggVerifyFailed  = errors.New("rec_agg: proof verification failed")
	ErrRecAggEmptyData     = errors.New("rec_agg: empty proof data")
	ErrRecAggInvalidType   = errors.New("rec_agg: invalid proof type")
)

// AggregationStrategy defines how proofs are combined.
type AggregationStrategy uint8

const (
	// SequentialStrategy aggregates proofs one at a time in order.
	SequentialStrategy AggregationStrategy = iota

	// ParallelStrategy verifies proofs concurrently then combines roots.
	ParallelStrategy

	// RecursiveStrategy recursively pairs and hashes proof roots in a tree.
	RecursiveStrategy
)

// String returns the strategy name.
func (s AggregationStrategy) String() string {
	switch s {
	case SequentialStrategy:
		return "Sequential"
	case ParallelStrategy:
		return "Parallel"
	case RecursiveStrategy:
		return "Recursive"
	default:
		return "Unknown"
	}
}

// Proof represents a single proof to be aggregated. It wraps the existing
// proof type constants with raw proof data and a cryptographic root.
type Proof struct {
	Type     string   // "SNARK", "STARK", "IPA", or other identifier.
	Data     []byte   // Raw proof bytes.
	Root     [32]byte // Hash root of this proof for aggregation.
	ProverID string   // Identity of the prover.
}

// ComputeRoot derives the proof root as SHA-256(type || data || proverID).
func (p *Proof) ComputeRoot() [32]byte {
	h := sha256.New()
	h.Write([]byte(p.Type))
	h.Write(p.Data)
	h.Write([]byte(p.ProverID))
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// RecursiveAggregatedProof holds the result of aggregating multiple proofs.
type RecursiveAggregatedProof struct {
	// IndividualRoots contains the root hash of each individual proof.
	IndividualRoots [][32]byte

	// AggregateRoot is the Merkle root over all individual roots.
	AggregateRoot [32]byte

	// Count is the number of proofs aggregated.
	Count int

	// ProofData is the serialized aggregation commitment.
	ProofData []byte
}

// ProofVerifier defines the interface for type-specific proof verification.
type ProofVerifier interface {
	// Verify checks a single proof for validity.
	Verify(proof *Proof) (bool, error)

	// ProofType returns the type string this verifier handles.
	ProofType() string
}

// ProofAggregatorV2 aggregates proofs using a configurable strategy and
// dispatches verification to type-specific verifiers. Thread-safe.
type ProofAggregatorV2 struct {
	mu        sync.RWMutex
	strategy  AggregationStrategy
	verifiers map[string]ProofVerifier
	batchSize int
}

// NewProofAggregatorV2 creates a new aggregator with the given strategy.
func NewProofAggregatorV2(strategy AggregationStrategy, batchSize int) *ProofAggregatorV2 {
	if batchSize <= 0 {
		batchSize = 16
	}
	return &ProofAggregatorV2{
		strategy:  strategy,
		verifiers: make(map[string]ProofVerifier),
		batchSize: batchSize,
	}
}

// RegisterVerifier adds a type-specific verifier.
func (pa *ProofAggregatorV2) RegisterVerifier(v ProofVerifier) {
	pa.mu.Lock()
	defer pa.mu.Unlock()
	pa.verifiers[v.ProofType()] = v
}

// Strategy returns the current aggregation strategy.
func (pa *ProofAggregatorV2) Strategy() AggregationStrategy {
	return pa.strategy
}

// BatchSize returns the configured batch size.
func (pa *ProofAggregatorV2) BatchSize() int {
	return pa.batchSize
}

// AggregateProofs combines multiple proofs into a single aggregated proof
// using the configured strategy. Each proof is verified before aggregation.
func (pa *ProofAggregatorV2) AggregateProofs(proofs []Proof) (*RecursiveAggregatedProof, error) {
	if len(proofs) == 0 {
		return nil, ErrRecAggNoProofs
	}

	pa.mu.RLock()
	defer pa.mu.RUnlock()

	switch pa.strategy {
	case SequentialStrategy:
		return pa.aggregateSequential(proofs)
	case ParallelStrategy:
		return pa.aggregateParallel(proofs)
	case RecursiveStrategy:
		return pa.aggregateRecursive(proofs)
	default:
		return pa.aggregateSequential(proofs)
	}
}

// aggregateSequential verifies and aggregates proofs one at a time.
func (pa *ProofAggregatorV2) aggregateSequential(proofs []Proof) (*RecursiveAggregatedProof, error) {
	roots := make([][32]byte, len(proofs))
	for i := range proofs {
		if err := pa.verifyIfPossible(&proofs[i]); err != nil {
			return nil, err
		}
		roots[i] = proofs[i].ComputeRoot()
	}

	aggRoot := ComputeMerkleRoot(roots)
	proofData := serializeAggregation(roots, aggRoot)

	return &RecursiveAggregatedProof{
		IndividualRoots: roots,
		AggregateRoot:   aggRoot,
		Count:           len(proofs),
		ProofData:       proofData,
	}, nil
}

// aggregateParallel verifies proofs concurrently then combines roots.
func (pa *ProofAggregatorV2) aggregateParallel(proofs []Proof) (*RecursiveAggregatedProof, error) {
	roots := make([][32]byte, len(proofs))
	errs := make([]error, len(proofs))

	var wg sync.WaitGroup
	for i := range proofs {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := pa.verifyIfPossible(&proofs[idx]); err != nil {
				errs[idx] = err
				return
			}
			roots[idx] = proofs[idx].ComputeRoot()
		}(i)
	}
	wg.Wait()

	// Check for verification errors.
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}

	aggRoot := ComputeMerkleRoot(roots)
	proofData := serializeAggregation(roots, aggRoot)

	return &RecursiveAggregatedProof{
		IndividualRoots: roots,
		AggregateRoot:   aggRoot,
		Count:           len(proofs),
		ProofData:       proofData,
	}, nil
}

// aggregateRecursive performs recursive tree-based aggregation.
// Pairs of proofs are merged, then pairs of pairs, etc.
func (pa *ProofAggregatorV2) aggregateRecursive(proofs []Proof) (*RecursiveAggregatedProof, error) {
	roots := make([][32]byte, len(proofs))
	for i := range proofs {
		if err := pa.verifyIfPossible(&proofs[i]); err != nil {
			return nil, err
		}
		roots[i] = proofs[i].ComputeRoot()
	}

	// Recursively combine roots in a binary tree.
	aggRoot := recursiveMerkle(roots)
	proofData := serializeAggregation(roots, aggRoot)

	return &RecursiveAggregatedProof{
		IndividualRoots: roots,
		AggregateRoot:   aggRoot,
		Count:           len(proofs),
		ProofData:       proofData,
	}, nil
}

// verifyIfPossible runs the type-specific verifier if one is registered.
func (pa *ProofAggregatorV2) verifyIfPossible(proof *Proof) error {
	if len(proof.Data) == 0 {
		return ErrRecAggEmptyData
	}
	v, ok := pa.verifiers[proof.Type]
	if !ok {
		// No verifier registered; skip verification.
		return nil
	}
	valid, err := v.Verify(proof)
	if err != nil {
		return err
	}
	if !valid {
		return ErrRecAggVerifyFailed
	}
	return nil
}

// VerifyAggregatedV2 verifies an aggregated proof by recomputing the
// Merkle root from individual roots and comparing.
func VerifyAggregatedV2(agg *RecursiveAggregatedProof) (bool, error) {
	if agg == nil {
		return false, ErrRecAggNilProof
	}
	if len(agg.IndividualRoots) == 0 {
		return false, ErrRecAggEmptyRoots
	}
	if agg.Count != len(agg.IndividualRoots) {
		return false, ErrRecAggCountMismatch
	}

	// Recompute aggregate root.
	expected := ComputeMerkleRoot(agg.IndividualRoots)
	if expected != agg.AggregateRoot {
		return false, ErrRecAggRootMismatch
	}

	// Verify ProofData integrity.
	expectedData := serializeAggregation(agg.IndividualRoots, agg.AggregateRoot)
	if len(expectedData) != len(agg.ProofData) {
		return false, ErrRecAggRootMismatch
	}
	for i := range expectedData {
		if expectedData[i] != agg.ProofData[i] {
			return false, ErrRecAggRootMismatch
		}
	}

	return true, nil
}

// ComputeMerkleRoot computes a binary Merkle tree root over a slice of
// 32-byte roots. If the number of roots is not a power of two, the
// tree is padded with zero hashes.
func ComputeMerkleRoot(roots [][32]byte) [32]byte {
	if len(roots) == 0 {
		return [32]byte{}
	}
	if len(roots) == 1 {
		return roots[0]
	}

	// Pad to next power of two.
	padded := padToPow2(roots)

	// Build tree bottom-up.
	layer := padded
	for len(layer) > 1 {
		next := make([][32]byte, len(layer)/2)
		for i := range next {
			next[i] = hashPair(layer[2*i], layer[2*i+1])
		}
		layer = next
	}
	return layer[0]
}

// SplitAndAggregate divides proofs into batches and aggregates each batch.
func (pa *ProofAggregatorV2) SplitAndAggregate(proofs []Proof, batchSize int) ([]*RecursiveAggregatedProof, error) {
	if len(proofs) == 0 {
		return nil, ErrRecAggNoProofs
	}
	if batchSize <= 0 {
		return nil, ErrRecAggBatchSizeZero
	}

	var results []*RecursiveAggregatedProof
	for i := 0; i < len(proofs); i += batchSize {
		end := i + batchSize
		if end > len(proofs) {
			end = len(proofs)
		}
		agg, err := pa.AggregateProofs(proofs[i:end])
		if err != nil {
			return nil, err
		}
		results = append(results, agg)
	}
	return results, nil
}

// --- Internal helpers ---

// hashPair hashes two 32-byte values together using SHA-256.
func hashPair(a, b [32]byte) [32]byte {
	h := sha256.New()
	h.Write(a[:])
	h.Write(b[:])
	var result [32]byte
	copy(result[:], h.Sum(nil))
	return result
}

// padToPow2 pads a slice to the next power of two with zero hashes.
func padToPow2(roots [][32]byte) [][32]byte {
	n := len(roots)
	target := 1
	for target < n {
		target <<= 1
	}
	if target == n {
		result := make([][32]byte, n)
		copy(result, roots)
		return result
	}
	padded := make([][32]byte, target)
	copy(padded, roots)
	return padded
}

// recursiveMerkle computes a Merkle root by recursively hashing pairs.
// Uses a different approach from ComputeMerkleRoot: it hashes left/right
// subtrees recursively rather than iteratively.
func recursiveMerkle(roots [][32]byte) [32]byte {
	if len(roots) == 0 {
		return [32]byte{}
	}
	if len(roots) == 1 {
		return roots[0]
	}
	if len(roots) == 2 {
		return hashPair(roots[0], roots[1])
	}

	// Pad to power of two.
	padded := padToPow2(roots)
	mid := len(padded) / 2
	left := recursiveMerkle(padded[:mid])
	right := recursiveMerkle(padded[mid:])
	return hashPair(left, right)
}

// serializeAggregation produces a commitment binding the individual roots
// and aggregate root together.
func serializeAggregation(roots [][32]byte, aggRoot [32]byte) []byte {
	// Format: count(4) || aggRoot(32) || root_0(32) || root_1(32) || ...
	size := 4 + 32 + 32*len(roots)
	data := make([]byte, size)
	binary.BigEndian.PutUint32(data[:4], uint32(len(roots)))
	copy(data[4:36], aggRoot[:])
	for i, r := range roots {
		copy(data[36+32*i:36+32*(i+1)], r[:])
	}
	return data
}
