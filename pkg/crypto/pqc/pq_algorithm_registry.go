// EIP-7932 Algorithm Registry: maps algorithm type IDs to post-quantum
// verification functions, gas costs, and metadata. Provides a unified
// dispatch interface for verifying PQ signatures across the Ethereum protocol
// (transactions, attestations, withdrawals) with algorithm agility.
package pqc

import (
	"errors"
	"fmt"
	"github.com/cloudflare/circl/sign/mldsa/mldsa44"
	"github.com/cloudflare/circl/sign/mldsa/mldsa87"
	"sync"
)

// AlgorithmType identifies a post-quantum algorithm in the EIP-7932 registry.
type AlgorithmType uint8

const (
	// MLDSA44 is ML-DSA at security level 2 (NIST PQC round 3).
	MLDSA44 AlgorithmType = 1
	// MLDSA65 is ML-DSA at security level 3 (recommended for Ethereum).
	MLDSA65 AlgorithmType = 2
	// MLDSA87 is ML-DSA at security level 5 (highest security).
	MLDSA87 AlgorithmType = 3
	// ALG_FALCON512 is Falcon-512 (NTRU lattice, NIST level 1).
	ALG_FALCON512 AlgorithmType = 4
	// ALG_SLHDSA is SLH-DSA / SPHINCS+ (stateless hash-based).
	ALG_SLHDSA AlgorithmType = 5
)

// VerifyFunc is a signature verification function.
// Returns true if the signature is valid for the given public key and message.
type VerifyFunc func(pubkey, msg, sig []byte) bool

// AlgorithmDescriptor holds metadata and verification logic for a PQ algorithm.
type AlgorithmDescriptor struct {
	Type       AlgorithmType
	Name       string
	SigSize    int
	PubKeySize int
	GasCost    uint64
	VerifyFn   VerifyFunc
}

// PQ algorithm gas costs per EIP-8051.
const (
	GasCostMLDSA44   uint64 = 3500
	GasCostMLDSA65   uint64 = 4500
	GasCostMLDSA87   uint64 = 5500
	GasCostFalcon512 uint64 = 3000
	GasCostSLHDSA    uint64 = 8000
)

// Errors for algorithm registry operations.
var (
	ErrAlgUnknown       = errors.New("pq_registry: unknown algorithm type")
	ErrAlgAlreadyExists = errors.New("pq_registry: algorithm already registered")
	ErrAlgNilVerifyFn   = errors.New("pq_registry: nil verification function")
	ErrAlgSigMismatch   = errors.New("pq_registry: signature size mismatch")
	ErrAlgPKMismatch    = errors.New("pq_registry: public key size mismatch")
	ErrAlgRecoverFail   = errors.New("pq_registry: public key recovery not supported")
)

// PQAlgorithmRegistry maintains a thread-safe mapping of algorithm types
// to their verification functions and metadata.
type PQAlgorithmRegistry struct {
	mu         sync.RWMutex
	algorithms map[AlgorithmType]*AlgorithmDescriptor
}

// globalRegistry is the default algorithm registry, initialized on package load.
var globalRegistry *PQAlgorithmRegistry

func init() {
	globalRegistry = NewPQAlgorithmRegistry()
	globalRegistry.registerDefaults()
}

// NewPQAlgorithmRegistry creates a new empty algorithm registry.
func NewPQAlgorithmRegistry() *PQAlgorithmRegistry {
	return &PQAlgorithmRegistry{
		algorithms: make(map[AlgorithmType]*AlgorithmDescriptor),
	}
}

// GlobalRegistry returns the global algorithm registry.
func GlobalRegistry() *PQAlgorithmRegistry {
	return globalRegistry
}

// RegisterAlgorithm adds a new algorithm to the registry.
func (r *PQAlgorithmRegistry) RegisterAlgorithm(
	algType AlgorithmType,
	name string,
	sigSize, pubkeySize int,
	gasCost uint64,
	verifyFn VerifyFunc,
) error {
	if verifyFn == nil {
		return ErrAlgNilVerifyFn
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.algorithms[algType]; exists {
		return ErrAlgAlreadyExists
	}

	r.algorithms[algType] = &AlgorithmDescriptor{
		Type:       algType,
		Name:       name,
		SigSize:    sigSize,
		PubKeySize: pubkeySize,
		GasCost:    gasCost,
		VerifyFn:   verifyFn,
	}
	return nil
}

// VerifySignature dispatches signature verification to the correct algorithm.
func (r *PQAlgorithmRegistry) VerifySignature(algType AlgorithmType, pubkey, msg, sig []byte) (bool, error) {
	r.mu.RLock()
	desc, exists := r.algorithms[algType]
	r.mu.RUnlock()

	if !exists {
		return false, fmt.Errorf("%w: %d", ErrAlgUnknown, algType)
	}

	if len(sig) != desc.SigSize {
		return false, fmt.Errorf("%w: got %d, want %d", ErrAlgSigMismatch, len(sig), desc.SigSize)
	}

	if len(pubkey) != desc.PubKeySize {
		return false, fmt.Errorf("%w: got %d, want %d", ErrAlgPKMismatch, len(pubkey), desc.PubKeySize)
	}

	return desc.VerifyFn(pubkey, msg, sig), nil
}

// GasCost returns the gas cost for verifying a signature of the given algorithm.
func (r *PQAlgorithmRegistry) GasCost(algType AlgorithmType) (uint64, error) {
	r.mu.RLock()
	desc, exists := r.algorithms[algType]
	r.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("%w: %d", ErrAlgUnknown, algType)
	}
	return desc.GasCost, nil
}

// GetAlgorithm returns the descriptor for a registered algorithm.
func (r *PQAlgorithmRegistry) GetAlgorithm(algType AlgorithmType) (*AlgorithmDescriptor, error) {
	r.mu.RLock()
	desc, exists := r.algorithms[algType]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%w: %d", ErrAlgUnknown, algType)
	}
	// Return a copy to prevent external mutation.
	cpy := *desc
	return &cpy, nil
}

// SupportedAlgorithms returns a list of all registered algorithm type IDs.
func (r *PQAlgorithmRegistry) SupportedAlgorithms() []AlgorithmType {
	r.mu.RLock()
	defer r.mu.RUnlock()

	types := make([]AlgorithmType, 0, len(r.algorithms))
	for t := range r.algorithms {
		types = append(types, t)
	}
	return types
}

// Size returns the number of registered algorithms.
func (r *PQAlgorithmRegistry) Size() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.algorithms)
}

// RecoverPublicKey attempts to recover a public key from a signature.
// For PQ schemes this is generally not possible (unlike ECDSA), so this
// returns the algorithm type and a best-effort extraction from the signature
// if the scheme supports it, or an error otherwise.
// Per EIP-7932 SIGRECOVER, lattice-based schemes do not support recovery;
// the public key must be transmitted alongside the signature.
func (r *PQAlgorithmRegistry) RecoverPublicKey(algType AlgorithmType, msg, sig []byte) ([]byte, error) {
	r.mu.RLock()
	_, exists := r.algorithms[algType]
	r.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("%w: %d", ErrAlgUnknown, algType)
	}

	// PQ schemes do not support public key recovery. The public key must
	// be explicitly provided in the transaction or attestation envelope.
	return nil, ErrAlgRecoverFail
}

// AlgorithmName returns the human-readable name for an algorithm type.
func (r *PQAlgorithmRegistry) AlgorithmName(algType AlgorithmType) string {
	r.mu.RLock()
	desc, exists := r.algorithms[algType]
	r.mu.RUnlock()

	if !exists {
		return "unknown"
	}
	return desc.Name
}

// IsRegistered returns whether an algorithm type is registered.
func (r *PQAlgorithmRegistry) IsRegistered(algType AlgorithmType) bool {
	r.mu.RLock()
	_, exists := r.algorithms[algType]
	r.mu.RUnlock()
	return exists
}

// TotalGasCost returns the aggregate gas cost for verifying multiple signatures.
func (r *PQAlgorithmRegistry) TotalGasCost(algTypes []AlgorithmType) (uint64, error) {
	var total uint64
	for _, t := range algTypes {
		cost, err := r.GasCost(t)
		if err != nil {
			return 0, err
		}
		total += cost
	}
	return total, nil
}

// ValidatePQSignature validates a PQ signature using the global algorithm registry.
// It looks up the algorithm by ID and calls its verification method.
// This bridges the registry to transaction validation.
func ValidatePQSignature(algorithmID uint8, publicKey, message, signature []byte) error {
	registry := GlobalRegistry()
	algType := AlgorithmType(algorithmID)
	valid, err := registry.VerifySignature(algType, publicKey, message, signature)
	if err != nil {
		return fmt.Errorf("pqc: algorithm %d: %w", algorithmID, err)
	}
	if !valid {
		return errors.New("pqc: signature verification failed")
	}
	return nil
}

// registerDefaults populates the registry with all supported PQ algorithms.
func (r *PQAlgorithmRegistry) registerDefaults() {
	mldsa := NewMLDSASigner()

	r.RegisterAlgorithm(MLDSA44, "ML-DSA-44", mldsa44.SignatureSize, mldsa44.PublicKeySize, GasCostMLDSA44,
		func(pubkey, msg, sig []byte) bool {
			var key mldsa44.PublicKey
			return key.UnmarshalBinary(pubkey) == nil && mldsa44.Verify(&key, msg, nil, sig)
		},
	)

	// ML-DSA-65 (security level 3) - uses real FIPS 204 lattice verification.
	r.RegisterAlgorithm(MLDSA65, "ML-DSA-65", MLDSASignatureSize, MLDSAPublicKeySize, GasCostMLDSA65,
		func(pubkey, msg, sig []byte) bool {
			return mldsa.Verify(pubkey, msg, sig)
		},
	)

	r.RegisterAlgorithm(MLDSA87, "ML-DSA-87", mldsa87.SignatureSize, mldsa87.PublicKeySize, GasCostMLDSA87,
		func(pubkey, msg, sig []byte) bool {
			var key mldsa87.PublicKey
			return key.UnmarshalBinary(pubkey) == nil && mldsa87.Verify(&key, msg, nil, sig)
		},
	)

	// Falcon-512.
	falcon := &FalconSigner{}
	r.RegisterAlgorithm(ALG_FALCON512, "Falcon-512", Falcon512SigSize, Falcon512PubKeySize, GasCostFalcon512,
		func(pubkey, msg, sig []byte) bool {
			return falcon.Verify(pubkey, msg, sig)
		},
	)

	// SLH-DSA (SPHINCS+).
	r.RegisterAlgorithm(ALG_SLHDSA, "SLH-DSA-SHA2-128f", SPHINCSSha256SigSize, SPHINCSSha256PubKeySize, GasCostSLHDSA,
		func(pubkey, msg, sig []byte) bool {
			return SPHINCSVerify(SPHINCSPublicKey(pubkey), msg, SPHINCSSignature(sig))
		},
	)
}

// EVMGasLookup is a function type that maps a PQ algorithm ID to its EVM gas cost.
type EVMGasLookup func(algorithmID uint8) uint64

// ValidateGasCostsMatch cross-checks the registry's gas costs against an
// EVM gas lookup function to ensure consistency between the PQ registry
// and the EVM gas tables (RISK-PQ2).
func (r *PQAlgorithmRegistry) ValidateGasCostsMatch(evmGasLookup EVMGasLookup) error {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for algType, desc := range r.algorithms {
		evmGas := evmGasLookup(uint8(algType))
		if evmGas != desc.GasCost {
			return fmt.Errorf("pq_registry: gas mismatch for algorithm %d (%s): registry=%d, evm=%d",
				algType, desc.Name, desc.GasCost, evmGas)
		}
	}
	return nil
}
