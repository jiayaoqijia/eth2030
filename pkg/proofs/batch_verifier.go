// batch_verifier.go implements a parallel batch proof verification pipeline
// supporting multiple proof types (ZK-SNARK, ZK-STARK, IPA, KZG). Proofs are
// dispatched to type-specific verifiers, verified concurrently, and results
// are aggregated with failure attribution and timeout management.
//
// Part of the K+ roadmap for mandatory 3-of-5 block proofs and the M+
// roadmap for proof aggregation.
package proofs

import (
	"bytes"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/bls"
	"github.com/eth2030/eth2030/crypto/ipa"
)

// Batch verifier errors.
var (
	ErrBatchVerifyEmpty       = errors.New("batch_verify: no proofs submitted")
	ErrBatchVerifyTimeout     = errors.New("batch_verify: verification timed out")
	ErrBatchVerifyUnknownType = errors.New("batch_verify: unknown proof type")
	ErrBatchVerifyClosed      = errors.New("batch_verify: verifier is closed")
	ErrBatchVerifyNilProof    = errors.New("batch_verify: nil proof in batch")
	ErrBatchVerifyNoProvider  = errors.New("batch_verify: a proof verifier must be registered")
	ErrBatchVerifyEncoding    = errors.New("batch_verify: invalid proof or public input encoding")
)

// VerifiableProof wraps a proof with metadata for batch routing.
type VerifiableProof struct {
	ID        string     // Unique identifier for failure attribution.
	Type      ProofType  // Proof type: ZKSNARK, ZKSTARK, IPA, KZG.
	Data      []byte     // Serialized proof data.
	PublicIn  []byte     // Public inputs for verification.
	BlockHash types.Hash // Associated block hash.
}

// VerificationResult records the outcome of verifying a single proof.
type VerificationResult struct {
	ProofID  string        // Identifier of the verified proof.
	Type     ProofType     // Proof type that was verified.
	Valid    bool          // Whether the proof passed verification.
	Duration time.Duration // Time spent verifying this proof.
	Err      error         // Non-nil if verification encountered an error.
}

// BatchVerificationResult aggregates verification outcomes for a batch.
type BatchVerificationResult struct {
	Results      []VerificationResult // Individual proof results.
	TotalValid   int                  // Count of valid proofs.
	TotalInvalid int                  // Count of invalid proofs.
	TotalErrors  int                  // Count of error results.
	AllValid     bool                 // True if every proof is valid.
	Duration     time.Duration        // Total wall-clock time for the batch.
}

// FailedProofs returns the subset of results that failed verification.
func (br *BatchVerificationResult) FailedProofs() []VerificationResult {
	var failed []VerificationResult
	for _, r := range br.Results {
		if !r.Valid {
			failed = append(failed, r)
		}
	}
	return failed
}

// BatchVerifierConfig configures the parallel verification pipeline.
type BatchVerifierConfig struct {
	// Workers is the number of concurrent verification goroutines.
	Workers int

	// Timeout is the maximum wall-clock time for the entire batch.
	Timeout time.Duration

	// PerProofTimeout is the maximum time for verifying a single proof.
	PerProofTimeout time.Duration
}

// DefaultBatchVerifierConfig returns sensible defaults for batch verification.
func DefaultBatchVerifierConfig() BatchVerifierConfig {
	return BatchVerifierConfig{
		Workers:         8,
		Timeout:         30 * time.Second,
		PerProofTimeout: 5 * time.Second,
	}
}

// TypeVerifier is a verification function for a specific proof type.
// It returns true if the proof is valid.
type TypeVerifier func(data, publicInputs []byte) (bool, error)

// BatchVerifier manages parallel verification of heterogeneous proof batches.
// Thread-safe.
type BatchVerifier struct {
	config    BatchVerifierConfig
	verifiers map[ProofType]TypeVerifier
	mu        sync.RWMutex
	closed    atomic.Bool

	// Statistics.
	totalVerified atomic.Uint64
	totalFailed   atomic.Uint64
	totalTimeout  atomic.Uint64
}

// NewBatchVerifier creates a new batch verifier with the given configuration.
// Default type verifiers are registered for all four proof types.
func NewBatchVerifier(config BatchVerifierConfig) *BatchVerifier {
	if config.Workers <= 0 {
		config.Workers = 8
	}
	if config.Timeout <= 0 {
		config.Timeout = 30 * time.Second
	}
	if config.PerProofTimeout <= 0 {
		config.PerProofTimeout = 5 * time.Second
	}
	bv := &BatchVerifier{
		config:    config,
		verifiers: make(map[ProofType]TypeVerifier),
	}
	bv.verifiers[IPA] = verifyIPAOpening
	bv.verifiers[KZG] = verifyKZGOpening
	return bv
}

// RegisterVerifier sets a custom verifier function for a proof type.
func (bv *BatchVerifier) RegisterVerifier(pt ProofType, fn TypeVerifier) {
	bv.mu.Lock()
	defer bv.mu.Unlock()
	if fn == nil {
		delete(bv.verifiers, pt)
		return
	}
	bv.verifiers[pt] = fn
}

// Close marks the verifier as closed. Subsequent calls to VerifyBatch
// return ErrBatchVerifyClosed.
func (bv *BatchVerifier) Close() {
	bv.closed.Store(true)
}

// VerifyBatch verifies all proofs in the batch concurrently. Proofs are
// dispatched to type-specific verifiers via a bounded worker pool. Returns
// aggregated results with failure attribution.
func (bv *BatchVerifier) VerifyBatch(proofs []VerifiableProof) (*BatchVerificationResult, error) {
	if bv.closed.Load() {
		return nil, ErrBatchVerifyClosed
	}
	if len(proofs) == 0 {
		return nil, ErrBatchVerifyEmpty
	}

	batchStart := time.Now()
	results := make([]VerificationResult, len(proofs))

	// Bounded worker pool via semaphore channel.
	sem := make(chan struct{}, bv.config.Workers)
	var wg sync.WaitGroup

	// Batch-level timeout.
	done := make(chan struct{})
	timedOut := atomic.Bool{}

	go func() {
		select {
		case <-done:
		case <-time.After(bv.config.Timeout):
			timedOut.Store(true)
		}
	}()

	for i := range proofs {
		if timedOut.Load() {
			break
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			p := &proofs[idx]
			results[idx] = bv.verifySingle(p, &timedOut)
		}(i)
	}

	wg.Wait()
	close(done)

	// Aggregate results.
	br := &BatchVerificationResult{
		Results:  results,
		Duration: time.Since(batchStart),
		AllValid: true,
	}
	for i := range results {
		if results[i].Valid {
			br.TotalValid++
			bv.totalVerified.Add(1)
		} else {
			br.AllValid = false
			if results[i].Err != nil {
				br.TotalErrors++
			} else {
				br.TotalInvalid++
			}
			bv.totalFailed.Add(1)
		}
	}

	// Check for unprocessed proofs due to timeout.
	for i := range results {
		if results[i].ProofID == "" && i < len(proofs) {
			results[i] = VerificationResult{
				ProofID: proofs[i].ID,
				Type:    proofs[i].Type,
				Valid:   false,
				Err:     ErrBatchVerifyTimeout,
			}
			br.AllValid = false
			br.TotalErrors++
			bv.totalTimeout.Add(1)
		}
	}

	return br, nil
}

// verifySingle runs verification for a single proof with per-proof timeout.
func (bv *BatchVerifier) verifySingle(p *VerifiableProof, batchTimeout *atomic.Bool) VerificationResult {
	start := time.Now()

	if p == nil {
		return VerificationResult{
			Valid: false, Duration: time.Since(start),
			Err: ErrBatchVerifyNilProof,
		}
	}

	result := VerificationResult{
		ProofID: p.ID,
		Type:    p.Type,
	}

	if batchTimeout.Load() {
		result.Err = ErrBatchVerifyTimeout
		result.Duration = time.Since(start)
		bv.totalTimeout.Add(1)
		return result
	}

	bv.mu.RLock()
	verifier, ok := bv.verifiers[p.Type]
	bv.mu.RUnlock()

	if !ok {
		switch p.Type {
		case ZKSNARK, ZKSTARK, IPA, KZG:
			result.Err = ErrBatchVerifyNoProvider
		default:
			result.Err = ErrBatchVerifyUnknownType
		}
		result.Duration = time.Since(start)
		return result
	}

	// Per-proof timeout.
	type verifyOut struct {
		valid bool
		err   error
	}
	ch := make(chan verifyOut, 1)
	data, publicInputs := bytes.Clone(p.Data), bytes.Clone(p.PublicIn)
	go func() {
		v, e := verifier(data, publicInputs)
		ch <- verifyOut{v, e}
	}()

	select {
	case out := <-ch:
		result.Valid = out.valid
		result.Err = out.err
	case <-time.After(bv.config.PerProofTimeout):
		result.Valid = false
		result.Err = ErrBatchVerifyTimeout
		bv.totalTimeout.Add(1)
	}

	result.Duration = time.Since(start)
	return result
}

// Stats returns cumulative verification statistics.
func (bv *BatchVerifier) Stats() (verified, failed, timedOut uint64) {
	return bv.totalVerified.Load(), bv.totalFailed.Load(), bv.totalTimeout.Load()
}

// IPA public inputs are the compressed commitment and two little-endian
// scalars (evaluation point and claimed value).
func verifyIPAOpening(proof, inputs []byte) (bool, error) {
	if len(inputs) != 96 || len(proof) != ipa.ProofSize {
		return false, ErrBatchVerifyEncoding
	}
	return ipa.Verify([32]byte(inputs[:32]), [32]byte(inputs[32:64]), [32]byte(inputs[64:]), proof)
}

// KZG public inputs are the Ethereum ceremony commitment and two big-endian
// scalars (evaluation point and claimed value).
func verifyKZGOpening(proof, inputs []byte) (bool, error) {
	const commitment = bls.KZGBytesPerCommitment
	const scalar = bls.KZGBytesPerFieldElement
	if len(inputs) != commitment+2*scalar || len(proof) != bls.KZGBytesPerProof {
		return false, ErrBatchVerifyEncoding
	}
	err := bls.VerifyKZGPointProof(
		[commitment]byte(inputs[:commitment]),
		[scalar]byte(inputs[commitment:commitment+scalar]),
		[scalar]byte(inputs[commitment+scalar:]),
		[bls.KZGBytesPerProof]byte(proof),
	)
	return err == nil, err
}
