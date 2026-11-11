// VDF batching and aggregation over the class-group evaluator.
package vdf

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

// Errors for enhanced VDF operations.
var (
	errVDFv2NilInput       = errors.New("vdfv2: nil input")
	errVDFv2EmptyInput     = errors.New("vdfv2: empty input")
	errVDFv2ZeroIterations = errors.New("vdfv2: zero iterations")
	errVDFv2NoResults      = errors.New("vdfv2: no results to aggregate")
	errVDFv2NilResult      = errors.New("vdfv2: nil result in list")
	errVDFv2NoInputs       = errors.New("vdfv2: no inputs for parallel evaluation")
)

// VDFv2Config holds configuration for the enhanced VDF.
type VDFv2Config struct {
	Parallelism int
}

func DefaultVDFv2Config() VDFv2Config {
	return VDFv2Config{Parallelism: 4}
}

// VDFv2Result holds the output of a single VDF evaluation.
type VDFv2Result struct {
	Input      []byte        // original input
	Output     []byte        // VDF output after iterations
	Proof      []byte        // proof of correct computation
	Iterations uint64        // number of sequential hash steps
	Duration   time.Duration // wall-clock time taken
}

// AggregatedVDFProof holds multiple VDF results combined into one proof.
type AggregatedVDFProof struct {
	Results        []*VDFv2Result // individual results
	AggregateProof []byte         // combined proof over all results
	Count          int            // number of aggregated proofs
}

// VDFv2 implements the enhanced VDF with proof aggregation.
// All methods are safe for concurrent use.
type VDFv2 struct {
	config VDFv2Config
}

func NewVDFv2(config VDFv2Config) *VDFv2 {
	config.Parallelism = max(config.Parallelism, 1)
	return &VDFv2{config: config}
}

func (v *VDFv2) Config() VDFv2Config { return v.config }

func (*VDFv2) Evaluate(input []byte, iterations uint64) (*VDFv2Result, error) {
	return Evaluate(input, iterations)
}

func (*VDFv2) Verify(result *VDFv2Result) bool {
	return result != nil && verify(result.Input, result.Iterations, result.Output, result.Proof)
}

// AggregateProofs combines multiple VDF results into a single aggregated proof.
// The aggregate proof is a hash over all individual proofs, enabling batch
// verification.
func (*VDFv2) AggregateProofs(results []*VDFv2Result) (*AggregatedVDFProof, error) {
	if len(results) == 0 {
		return nil, errVDFv2NoResults
	}
	for _, r := range results {
		if r == nil {
			return nil, errVDFv2NilResult
		}
	}
	return &AggregatedVDFProof{
		Results: results, AggregateProof: aggregateDigest(results), Count: len(results),
	}, nil
}

func (v *VDFv2) VerifyAggregated(proof *AggregatedVDFProof) bool {
	if proof == nil || len(proof.Results) == 0 || proof.Count != len(proof.Results) {
		return false
	}
	for _, r := range proof.Results {
		if !v.Verify(r) {
			return false
		}
	}
	return bytesEqual(aggregateDigest(proof.Results), proof.AggregateProof)
}

func aggregateDigest(results []*VDFv2Result) []byte {
	var count [8]byte
	binary.BigEndian.PutUint64(count[:], uint64(len(results)))
	data := append([]byte("eth2030/vdf-aggregate/v1"), count[:]...)
	for _, r := range results {
		seed := vdfSeed(r.Input)
		var iterations [8]byte
		binary.BigEndian.PutUint64(iterations[:], r.Iterations)
		data = append(data, seed[:]...)
		data = append(data, iterations[:]...)
		data = append(data, r.Output...)
		data = append(data, r.Proof...)
	}
	return keccak256(data)
}

// ParallelEvaluate evaluates multiple VDF inputs concurrently, respecting
// the configured parallelism limit.
func (v *VDFv2) ParallelEvaluate(inputs [][]byte, iterations uint64) ([]*VDFv2Result, error) {
	if len(inputs) == 0 {
		return nil, errVDFv2NoInputs
	}
	if iterations == 0 {
		return nil, errVDFv2ZeroIterations
	}

	parallelism := v.config.Parallelism

	results := make([]*VDFv2Result, len(inputs))
	errs := make([]error, len(inputs))

	sem := make(chan struct{}, parallelism)
	var wg sync.WaitGroup

	for i, input := range inputs {
		wg.Add(1)
		go func(idx int, inp []byte) {
			defer wg.Done()
			sem <- struct{}{}        // acquire
			defer func() { <-sem }() // release

			result, err := v.Evaluate(inp, iterations)
			results[idx] = result
			errs[idx] = err
		}(i, input)
	}

	wg.Wait()

	// Return the first error encountered.
	for _, err := range errs {
		if err != nil {
			return nil, errors.Join(errors.New("vdfv2: parallel evaluation failed"), err)
		}
	}

	return results, nil
}

// copyBytes returns a copy of a byte slice.
func copyBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}

// bytesEqual performs constant-time comparison of two byte slices.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := range a {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
