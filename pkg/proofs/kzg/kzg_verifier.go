// kzg_verifier.go implements KZG commitment verification for blob data,
// with batch verification and point evaluation matching
// the EIP-4844 specification.
//
// KZG (Kate-Zaverucha-Goldberg) commitments allow proving that a polynomial
// evaluates to a claimed value at a given point, enabling efficient blob data
// availability verification.
//
// Part of the DL roadmap: EIP-4844 blob transactions and PeerDAS.
package kzg

import (
	"crypto/sha256"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/bls"
)

// KZG verifier errors.
var (
	ErrKZGNilCommitment      = errors.New("kzg_verifier: nil commitment")
	ErrKZGNilProof           = errors.New("kzg_verifier: nil proof")
	ErrKZGPointMismatch      = errors.New("kzg_verifier: point evaluation failed")
	ErrKZGBatchEmpty         = errors.New("kzg_verifier: empty batch")
	ErrKZGBatchSizeMismatch  = errors.New("kzg_verifier: batch size mismatch")
	ErrKZGInvalidBlob        = errors.New("kzg_verifier: invalid blob data")
	ErrKZGVerifierClosed     = errors.New("kzg_verifier: verifier is closed")
	ErrKZGCommitmentMismatch = errors.New("kzg_verifier: commitment does not match blob")
)

// KZG constants matching EIP-4844 specification.
const (
	// KZGCommitmentSize is the byte length of a KZG commitment (48 bytes, G1 point).
	KZGCommitmentSize = 48

	// KZGProofPointSize is the byte length of a KZG proof (48 bytes, G1 point).
	KZGProofPointSize = 48

	// BlobFieldElementCount is the number of field elements in a blob (4096).
	BlobFieldElementCount = 4096

	// FieldElementSize is the byte size of a BLS12-381 scalar field element.
	FieldElementSize = 32

	// BlobSize is the total byte size of a blob (4096 * 32 = 131072).
	BlobSize = BlobFieldElementCount * FieldElementSize
)

// KZGCommitment represents a 48-byte KZG commitment (G1 point on BLS12-381).
type KZGCommitment [KZGCommitmentSize]byte

// KZGProofPoint represents a 48-byte KZG proof point (G1 point on BLS12-381).
type KZGProofPoint [KZGProofPointSize]byte

// PointEvaluation holds the inputs and output of a KZG point evaluation.
type PointEvaluation struct {
	Commitment KZGCommitment
	Proof      KZGProofPoint
	Point      [FieldElementSize]byte // z: evaluation point
	Value      [FieldElementSize]byte // y: claimed value p(z) = y
}

// BlobCommitmentPair pairs a blob's data hash with its KZG commitment.
type BlobCommitmentPair struct {
	BlobHash   types.Hash
	Commitment KZGCommitment
	Proof      KZGProofPoint
	BlobIndex  uint64
}

// KZGBatchItem holds a single item in a batch verification.
type KZGBatchItem struct {
	Commitment KZGCommitment
	Proof      KZGProofPoint
	Point      [FieldElementSize]byte
	Value      [FieldElementSize]byte
	Valid      bool
}

// KZGBatchResult holds the result of a batch KZG verification.
type KZGBatchResult struct {
	Items       []KZGBatchItem
	AllValid    bool
	ValidCount  int
	FailedCount int
}

// KZGVerifierConfig configures the KZG verifier.
type KZGVerifierConfig struct {
	// MaxBatchSize is the maximum number of proofs in a single batch.
	MaxBatchSize int

	// ParallelVerify enables concurrent verification in batch mode.
	ParallelVerify bool
}

// DefaultKZGVerifierConfig returns sensible defaults.
func DefaultKZGVerifierConfig() KZGVerifierConfig {
	return KZGVerifierConfig{
		MaxBatchSize:   128,
		ParallelVerify: true,
	}
}

// KZGVerifier performs KZG commitment verification for blob data.
// It supports single-proof and batch verification. Thread-safe.
type KZGVerifier struct {
	mu     sync.RWMutex
	config KZGVerifierConfig
	closed bool

	// Statistics.
	totalVerified uint64
	totalFailed   uint64
	batchesRun    uint64
}

// NewKZGVerifier creates a new KZG verifier with the given config.
func NewKZGVerifier(config KZGVerifierConfig) *KZGVerifier {
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = 128
	}
	return &KZGVerifier{
		config: config,
	}
}

// Close shuts down the verifier.
func (v *KZGVerifier) Close() {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.closed = true
}

// VerifyPointEvaluation verifies a KZG point evaluation proof. It checks
// that the polynomial committed to by `commitment` evaluates to `value`
// at `point`, as attested by `proof`.
func (v *KZGVerifier) VerifyPointEvaluation(eval *PointEvaluation) (bool, error) {
	v.mu.RLock()
	if v.closed {
		v.mu.RUnlock()
		return false, ErrKZGVerifierClosed
	}
	v.mu.RUnlock()

	if eval == nil {
		return false, ErrKZGNilProof
	}

	// Check for zero commitment.
	if isZeroKZG(eval.Commitment[:]) {
		return false, ErrKZGNilCommitment
	}

	valid := verifyPointEval(eval.Commitment, eval.Proof, eval.Point, eval.Value)

	v.mu.Lock()
	if valid {
		v.totalVerified++
	} else {
		v.totalFailed++
	}
	v.mu.Unlock()

	return valid, nil
}

// VerifyBlobCommitment verifies that a blob's KZG commitment matches the
// versioned hash (as defined in EIP-4844). The versioned hash is computed as:
//
//	0x01 || SHA256(commitment)[1:]
func (v *KZGVerifier) VerifyBlobCommitment(pair *BlobCommitmentPair) (bool, error) {
	if pair == nil {
		return false, ErrKZGNilCommitment
	}
	if isZeroKZG(pair.Commitment[:]) {
		return false, ErrKZGNilCommitment
	}

	expected := computeVersionedHash(pair.Commitment)
	return expected == pair.BlobHash, nil
}

// BatchVerify verifies multiple point evaluations in a single batch.
// When ParallelVerify is enabled, evaluations are checked concurrently.
func (v *KZGVerifier) BatchVerify(evals []PointEvaluation) (*KZGBatchResult, error) {
	v.mu.RLock()
	if v.closed {
		v.mu.RUnlock()
		return nil, ErrKZGVerifierClosed
	}
	v.mu.RUnlock()

	if len(evals) == 0 {
		return nil, ErrKZGBatchEmpty
	}

	result := &KZGBatchResult{
		Items:    make([]KZGBatchItem, len(evals)),
		AllValid: true,
	}

	if v.config.ParallelVerify && len(evals) > 1 {
		var wg sync.WaitGroup
		for i := range evals {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				e := &evals[idx]
				valid := verifyPointEval(e.Commitment, e.Proof, e.Point, e.Value)
				result.Items[idx] = KZGBatchItem{
					Commitment: e.Commitment,
					Proof:      e.Proof,
					Point:      e.Point,
					Value:      e.Value,
					Valid:      valid,
				}
			}(i)
		}
		wg.Wait()
	} else {
		for i := range evals {
			e := &evals[i]
			valid := verifyPointEval(e.Commitment, e.Proof, e.Point, e.Value)
			result.Items[i] = KZGBatchItem{
				Commitment: e.Commitment,
				Proof:      e.Proof,
				Point:      e.Point,
				Value:      e.Value,
				Valid:      valid,
			}
		}
	}

	// Aggregate results.
	for i := range result.Items {
		if result.Items[i].Valid {
			result.ValidCount++
		} else {
			result.FailedCount++
			result.AllValid = false
		}
	}

	v.mu.Lock()
	v.totalVerified += uint64(result.ValidCount)
	v.totalFailed += uint64(result.FailedCount)
	v.batchesRun++
	v.mu.Unlock()

	return result, nil
}

// Stats returns the verifier's statistics.
func (v *KZGVerifier) Stats() (verified, failed, batches uint64) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.totalVerified, v.totalFailed, v.batchesRun
}

// --- Internal helpers ---

// verifyPointEval performs hash-based KZG point evaluation verification.
// In production this would use BLS12-381 pairing checks.
func verifyPointEval(
	commitment KZGCommitment,
	proof KZGProofPoint,
	point [FieldElementSize]byte,
	value [FieldElementSize]byte,
) bool {
	return bls.VerifyKZGPointProof(commitment, point, value, proof) == nil
}

// computeVersionedHash computes the EIP-4844 versioned hash from a commitment.
// Format: 0x01 || SHA256(commitment)[1:]
func computeVersionedHash(commitment KZGCommitment) types.Hash {
	inner := sha256.Sum256(commitment[:])
	var result types.Hash
	result[0] = 0x01 // version byte
	copy(result[1:], inner[1:])
	return result
}

// isZeroKZG checks if all bytes are zero.
func isZeroKZG(data []byte) bool {
	for _, b := range data {
		if b != 0 {
			return false
		}
	}
	return true
}
