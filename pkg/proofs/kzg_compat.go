package proofs

// kzg_compat.go re-exports types from proofs/kzg for backward compatibility.

import "github.com/eth2030/eth2030/proofs/kzg"

// KZG type aliases.
type (
	KZGCommitment      = kzg.KZGCommitment
	KZGProofPoint      = kzg.KZGProofPoint
	PointEvaluation    = kzg.PointEvaluation
	BlobCommitmentPair = kzg.BlobCommitmentPair
	KZGBatchItem       = kzg.KZGBatchItem
	KZGBatchResult     = kzg.KZGBatchResult
	KZGVerifierConfig  = kzg.KZGVerifierConfig
	KZGVerifier        = kzg.KZGVerifier
)

// KZG constants.
const (
	KZGCommitmentSize = kzg.KZGCommitmentSize
	KZGProofPointSize = kzg.KZGProofPointSize
)

// KZG error variables.
var (
	ErrKZGNilCommitment      = kzg.ErrKZGNilCommitment
	ErrKZGNilProof           = kzg.ErrKZGNilProof
	ErrKZGPointMismatch      = kzg.ErrKZGPointMismatch
	ErrKZGBatchEmpty         = kzg.ErrKZGBatchEmpty
	ErrKZGBatchSizeMismatch  = kzg.ErrKZGBatchSizeMismatch
	ErrKZGInvalidBlob        = kzg.ErrKZGInvalidBlob
	ErrKZGVerifierClosed     = kzg.ErrKZGVerifierClosed
	ErrKZGCommitmentMismatch = kzg.ErrKZGCommitmentMismatch
)

// KZG function wrappers.
func DefaultKZGVerifierConfig() KZGVerifierConfig          { return kzg.DefaultKZGVerifierConfig() }
func NewKZGVerifier(config KZGVerifierConfig) *KZGVerifier { return kzg.NewKZGVerifier(config) }
