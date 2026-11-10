// IPA integration layer bridging the eth2030 IPA implementation with the
// go-ipa reference library patterns (github.com/crate-crypto/go-ipa).
//
// This file provides:
//   - IPABackend interface for pluggable IPA proof verification
//   - PureGoIPABackend using existing ipa.go code with proper halving verification
//   - Structural validation, Fiat-Shamir challenge generation, and b-vector computation
//
// The verifier follows the protocol from go-ipa/ipa/verifier.go:
//  1. Reconstruct challenges via Fiat-Shamir transcript
//  2. Compute folding scalars for each index using bit-decomposition
//  3. Fold generators and b-vector to single elements
//  4. Check final commitment against proof.A * g0 + (proof.A * b0) * Q

package ipa

import (
	"errors"
	"fmt"
	"math/big"
	"sync"
)

// IPA constants for Banderwagon-based proofs.
const (
	// DefaultVectorSize is the polynomial evaluation domain size (256).
	DefaultVectorSize = 256
	// DefaultNumRounds is log2(DefaultVectorSize) = 8 IPA halving rounds.
	DefaultNumRounds = 8
	// BanderwagonFieldSize is the byte length of a Banderwagon field element.
	BanderwagonFieldSize = 32
)

// IPA integration errors.
var (
	ErrIPANilProof          = errors.New("ipa_integration: nil proof")
	ErrIPANilA              = errors.New("ipa_integration: nil final scalar A")
	ErrIPALRLengthMismatch  = errors.New("ipa_integration: L and R length mismatch")
	ErrIPAInvalidRoundCount = errors.New("ipa_integration: invalid round count")
	ErrIPANilLPoint         = errors.New("ipa_integration: nil L point")
	ErrIPANilRPoint         = errors.New("ipa_integration: nil R point")
	ErrIPAInvalidVectorSize = errors.New("ipa_integration: vector size must be power of 2")
	ErrIPABackendNil        = errors.New("ipa_integration: nil backend")
	ErrIPAEmptyCommitment   = errors.New("ipa_integration: empty commitment")
	ErrIPAEmptyEvalPoint    = errors.New("ipa_integration: empty eval point")
)

// IPABackend defines the interface for IPA proof creation and verification.
// Implementations can use pure-Go math or delegate to the go-ipa library.
type IPABackend interface {
	// VerifyProof verifies an IPA proof against a commitment.
	// commitment: serialized Banderwagon point (32 bytes)
	// proof: the IPA proof data (L, R points and final scalar A)
	// evalPoint: the evaluation point (32-byte scalar)
	// result: the claimed evaluation result (32-byte scalar)
	VerifyProof(commitment []byte, proof *IPAProofData, evalPoint, result []byte) (bool, error)

	// CreateProof generates an IPA proof for a vector of values at an evaluation point.
	// values: the committed polynomial evaluations (each 32-byte scalar)
	// evalPoint: the point at which to evaluate (32-byte scalar)
	CreateProof(values [][]byte, evalPoint []byte) (*IPAProofData, error)

	// Name returns the backend implementation name.
	Name() string
}

// IPAIntegrationConfig holds configuration for the IPA integration layer.
type IPAIntegrationConfig struct {
	// VectorSize is the polynomial domain size (must be power of 2).
	VectorSize int
	// NumRounds is log2(VectorSize), the number of halving rounds.
	NumRounds int
	// SRS contains the structured reference string generator points.
	SRS []*BanderPoint
	// Q is the auxiliary generator for inner product binding.
	Q *BanderPoint
}

// DefaultIPAIntegrationConfig returns a standard IPA configuration.
func DefaultIPAIntegrationConfig() *IPAIntegrationConfig {
	return &IPAIntegrationConfig{
		VectorSize: DefaultVectorSize,
		NumRounds:  DefaultNumRounds,
	}
}

// ValidateIPAIntegrationConfig checks that the config is valid.
func ValidateIPAIntegrationConfig(cfg *IPAIntegrationConfig) error {
	if cfg == nil {
		return errors.New("ipa_integration: nil config")
	}
	if cfg.VectorSize <= 0 {
		return ErrIPAInvalidVectorSize
	}
	if cfg.VectorSize&(cfg.VectorSize-1) != 0 {
		return ErrIPAInvalidVectorSize
	}
	expected := IPAProofSize(cfg.VectorSize)
	if cfg.NumRounds != expected {
		return fmt.Errorf("ipa_integration: NumRounds %d != log2(VectorSize) %d", cfg.NumRounds, expected)
	}
	return nil
}

// --- Global backend management ---

var (
	ipaBackendMu      sync.RWMutex
	activeIPABackend  IPABackend
	defaultIPABackend = &PureGoIPABackend{}
)

// DefaultIPABackend returns the currently active IPA backend.
func DefaultIPABackend() IPABackend {
	ipaBackendMu.RLock()
	defer ipaBackendMu.RUnlock()
	if activeIPABackend != nil {
		return activeIPABackend
	}
	return defaultIPABackend
}

// SetIPABackend sets the active IPA backend. Pass nil to reset to default.
func SetIPABackend(b IPABackend) {
	ipaBackendMu.Lock()
	defer ipaBackendMu.Unlock()
	activeIPABackend = b
}

// IPAIntegrationStatus returns the name of the active backend.
func IPAIntegrationStatus() string {
	return DefaultIPABackend().Name()
}

// --- Proof validation ---

// ValidateIPAProof performs structural validation on an IPA proof.
func ValidateIPAProof(proof *IPAProofData) error {
	if proof == nil {
		return ErrIPANilProof
	}
	if proof.A == nil {
		return ErrIPANilA
	}
	if len(proof.L) != len(proof.R) {
		return ErrIPALRLengthMismatch
	}
	for i, lp := range proof.L {
		if lp == nil {
			return fmt.Errorf("%w at index %d", ErrIPANilLPoint, i)
		}
	}
	for i, rp := range proof.R {
		if rp == nil {
			return fmt.Errorf("%w at index %d", ErrIPANilRPoint, i)
		}
	}
	return nil
}

// ValidateIPAProofForConfig validates that the proof matches the expected config.
func ValidateIPAProofForConfig(proof *IPAProofData, cfg *IPAIntegrationConfig) error {
	if err := ValidateIPAProof(proof); err != nil {
		return err
	}
	if cfg == nil {
		return errors.New("ipa_integration: nil config")
	}
	if len(proof.L) != cfg.NumRounds {
		return fmt.Errorf("%w: got %d rounds, expected %d", ErrIPAInvalidRoundCount, len(proof.L), cfg.NumRounds)
	}
	return nil
}

// --- PureGoIPABackend ---

// PureGoIPABackend implements IPABackend using the existing ipa.go code
// with proper halving verification following the go-ipa pattern.
type PureGoIPABackend struct{}

// Name returns the backend name.
func (b *PureGoIPABackend) Name() string {
	return "pure-go"
}

// VerifyProof verifies an IPA proof using the pure-Go implementation.
// This delegates to IPAVerify after deserializing inputs.
func (b *PureGoIPABackend) VerifyProof(commitment []byte, proof *IPAProofData, evalPoint, result []byte) (bool, error) {
	if err := ValidateIPAProof(proof); err != nil {
		return false, err
	}
	if len(commitment) == 0 {
		return false, ErrIPAEmptyCommitment
	}
	if len(evalPoint) == 0 {
		return false, ErrIPAEmptyEvalPoint
	}

	// Deserialize commitment.
	var commitBuf [32]byte
	copy(commitBuf[32-len(commitment):], commitment)
	commitPoint, err := BanderDeserialize(commitBuf)
	if err != nil {
		return false, fmt.Errorf("ipa_integration: invalid commitment: %w", err)
	}

	// Deserialize eval point and result as scalars.
	ep := new(big.Int).SetBytes(evalPoint)
	res := new(big.Int).SetBytes(result)

	rounds := len(proof.L)
	vectorSize := 1 << rounds

	// Generate deterministic generators for the vector size.
	generators := GenerateIPAGenerators(vectorSize)

	// Build b-vector for the eval point.
	bVec := ComputeBVector(ep, vectorSize)

	return IPAVerify(generators, commitPoint, bVec, res, proof)
}

// CreateProof generates an IPA proof from values at an evaluation point.
func (b *PureGoIPABackend) CreateProof(values [][]byte, evalPoint []byte) (*IPAProofData, error) {
	if len(values) == 0 {
		return nil, errors.New("ipa_integration: empty values")
	}
	if len(evalPoint) == 0 {
		return nil, ErrIPAEmptyEvalPoint
	}

	n := len(values)
	if n&(n-1) != 0 {
		return nil, ErrIPAInvalidVectorSize
	}

	// Convert values to scalars.
	aVec := make([]*big.Int, n)
	for i, v := range values {
		aVec[i] = new(big.Int).SetBytes(v)
	}

	ep := new(big.Int).SetBytes(evalPoint)

	// Generate generators and b-vector.
	generators := GenerateIPAGenerators(n)
	bVec := ComputeBVector(ep, n)

	// Compute commitment.
	commitment := BanderMSM(generators, aVec)

	proof, _, err := IPAProve(generators, aVec, bVec, commitment)
	return proof, err
}

// --- Helper functions ---

// GenerateIPAGenerators produces deterministic Banderwagon generator points
// for the existing ETH2030 IPA transcript.
func GenerateIPAGenerators(n int) []*BanderPoint {
	generators := make([]*BanderPoint, n)
	g := BanderGenerator()
	for i := 0; i < n; i++ {
		// Deterministic generation: G_i = (i+1) * G
		s := big.NewInt(int64(i + 1))
		generators[i] = BanderScalarMul(g, s)
	}
	return generators
}

// ComputeBVector computes the barycentric evaluation vector for a given point.
// For evalPoint inside the domain [0, VectorSize-1], b is the unit vector
// with 1 at index evalPoint. For points outside the domain, barycentric
// interpolation coefficients are computed.
//
// This mirrors go-ipa/ipa/prover.go:computeBVector.
func ComputeBVector(evalPoint *big.Int, vectorSize int) []*big.Int {
	b := make([]*big.Int, vectorSize)

	// Check if evalPoint is inside the domain.
	maxDomain := big.NewInt(int64(vectorSize - 1))
	if evalPoint.Sign() >= 0 && evalPoint.Cmp(maxDomain) <= 0 {
		idx := evalPoint.Int64()
		for i := range b {
			if int64(i) == idx {
				b[i] = big.NewInt(1)
			} else {
				b[i] = big.NewInt(0)
			}
		}
		return b
	}

	// Outside domain: compute barycentric Lagrange coefficients.
	// L_i(z) = [product_{j!=i}(z - j)] / [product_{j!=i}(i - j)]
	// = w_i / (z - i) where w_i = 1/product_{j!=i}(i - j)
	n := BanderN()

	// Compute denominators: w_i = product_{j!=i}(i-j) mod n
	weights := make([]*big.Int, vectorSize)
	for i := 0; i < vectorSize; i++ {
		w := big.NewInt(1)
		iFr := big.NewInt(int64(i))
		for j := 0; j < vectorSize; j++ {
			if j == i {
				continue
			}
			jFr := big.NewInt(int64(j))
			diff := new(big.Int).Sub(iFr, jFr)
			diff.Mod(diff, n)
			w.Mul(w, diff)
			w.Mod(w, n)
		}
		weights[i] = new(big.Int).ModInverse(w, n)
		if weights[i] == nil {
			weights[i] = big.NewInt(0)
		}
	}

	// Compute total product: prod(z - i) for i in [0, vectorSize-1]
	totalProd := big.NewInt(1)
	for i := 0; i < vectorSize; i++ {
		iFr := big.NewInt(int64(i))
		diff := new(big.Int).Sub(evalPoint, iFr)
		diff.Mod(diff, n)
		totalProd.Mul(totalProd, diff)
		totalProd.Mod(totalProd, n)
	}

	// b_i = totalProd * w_i / (z - i)
	for i := 0; i < vectorSize; i++ {
		iFr := big.NewInt(int64(i))
		denom := new(big.Int).Sub(evalPoint, iFr)
		denom.Mod(denom, n)
		denomInv := new(big.Int).ModInverse(denom, n)
		if denomInv == nil {
			b[i] = big.NewInt(0)
			continue
		}
		b[i] = new(big.Int).Mul(totalProd, weights[i])
		b[i].Mul(b[i], denomInv)
		b[i].Mod(b[i], n)
	}

	return b
}

// GenerateIPAChallenges reconstructs the Fiat-Shamir challenges from a
// proof and commitment, following the go-ipa transcript protocol.
func GenerateIPAChallenges(commitment *BanderPoint, v *big.Int, proof *IPAProofData) ([]*big.Int, error) {
	if err := ValidateIPAProof(proof); err != nil {
		return nil, err
	}
	if commitment == nil {
		return nil, ErrIPAEmptyCommitment
	}

	rounds := len(proof.L)
	transcript := newIPATranscript("ipa")
	transcript.appendPoint(commitment)
	transcript.appendScalar(v)

	challenges := make([]*big.Int, rounds)
	for i := 0; i < rounds; i++ {
		transcript.appendPoint(proof.L[i])
		transcript.appendPoint(proof.R[i])
		challenges[i] = transcript.challenge()
	}
	return challenges, nil
}

// FoldScalar computes the folding scalar for a given index using the
// challenge values from the IPA protocol. This implements the bit-
// decomposition approach from go-ipa/ipa/verifier.go:
//
//	scalar = product over rounds of: x_inv[j] if bit j of index is set
//
// where x_inv[j] is the inverse of challenge j.
func FoldScalar(challenges []*big.Int, index int) *big.Int {
	n := BanderN()
	scalar := big.NewInt(1)
	numRounds := len(challenges)

	// Precompute challenge inverses.
	invChallenges := make([]*big.Int, numRounds)
	for i, c := range challenges {
		invChallenges[i] = new(big.Int).ModInverse(c, n)
		if invChallenges[i] == nil {
			invChallenges[i] = big.NewInt(0)
		}
	}

	// For each round, if the corresponding bit of index is set, multiply by x_inv.
	// Bit numbering: round 0 checks the highest bit.
	for j := 0; j < numRounds; j++ {
		bitPos := numRounds - 1 - j
		if index&(1<<bitPos) > 0 {
			scalar.Mul(scalar, invChallenges[j])
			scalar.Mod(scalar, n)
		}
	}
	return scalar
}

// DefaultIPAProofSize returns the expected proof size (number of rounds)
// for the standard vector size (256).
func DefaultIPAProofSize() int {
	return DefaultNumRounds
}
