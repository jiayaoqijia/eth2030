// PQ transaction validation pipeline for post-quantum resistant transactions.
// Provides signature verification, algorithm whitelisting, gas cost estimation,
// and transaction pool integration for PQ-signed Ethereum transactions.
//
// Supports three PQ signature schemes:
//   - ML-DSA-65 (CRYSTALS-Dilithium): lattice-based, NIST level 3
//   - Falcon-512: NTRU lattice-based, NIST level 1
//   - SPHINCS+-SHA256: stateless hash-based, NIST level 1
package types

import (
	"errors"
	"fmt"
	"sync"
)

// PQAlgorithm identifies a post-quantum signature algorithm for transaction validation.
type PQAlgorithm uint8

const (
	// PQ_MLDSA65 is ML-DSA at security level 3 (recommended for Ethereum transactions).
	PQ_MLDSA65 PQAlgorithm = 0

	// PQ_FALCON512 is Falcon-512 (NTRU lattice-based, compact signatures).
	PQ_FALCON512 PQAlgorithm = 1

	// PQ_SPHINCS_SHA256 is SPHINCS+-SHA256 (stateless hash-based fallback).
	PQ_SPHINCS_SHA256 PQAlgorithm = 2
)

// PQ signature and public key size constants per algorithm.
const (
	// ML-DSA-65 sizes.
	PQMLDSAPubKeySize = DilithiumPubKeySize
	PQMLDSASigSize    = DilithiumSigSize

	// Falcon-512 sizes.
	PQFalconPubKeySize = FalconPubKeySize
	PQFalconSigSize    = FalconSigSize

	// SPHINCS+-SHA256 sizes.
	PQSPHINCSPubKeySize = SPHINCSPlusPubKeySize
	PQSPHINCSPSigSize   = SPHINCSPlusSigSize
)

// Gas cost table for PQ signature verification (per EIP-8051 draft).
const (
	PQGasMLDSA65 uint64 = 4500
	PQGasFalcon  uint64 = 6000
	PQGasSPHINCS uint64 = 8000

	// PQBaseGas is the base gas for any PQ transaction (type prefix, decoding).
	PQBaseGas uint64 = 500
)

// PQ validation errors.
var (
	ErrPQValidateNilTx         = errors.New("pq_validate: nil transaction")
	ErrPQValidateNoSignature   = errors.New("pq_validate: missing PQ signature")
	ErrPQValidateNoPubKey      = errors.New("pq_validate: missing PQ public key")
	ErrPQValidateUnknownAlg    = errors.New("pq_validate: unknown algorithm")
	ErrPQValidateSigSize       = errors.New("pq_validate: signature size mismatch")
	ErrPQValidatePKSize        = errors.New("pq_validate: public key size mismatch")
	ErrPQValidateAlgNotAllowed = errors.New("pq_validate: algorithm not in whitelist")
	ErrPQValidateVerifyFailed  = errors.New("pq_validate: signature verification failed")
	ErrPQValidateGasLimit      = errors.New("pq_validate: insufficient gas for PQ verification")
	ErrPQPoolFull              = errors.New("pq_validate: PQ transaction pool full")
	ErrPQPoolDuplicate         = errors.New("pq_validate: duplicate PQ transaction")
)

// PQVerifyFunc is a function that verifies a PQ signature.
type PQVerifyFunc func(pk, msg, sig []byte) bool

// PQAlgorithmEntry describes a registered PQ algorithm for validation.
type PQAlgorithmEntry struct {
	Algorithm  PQAlgorithm
	Name       string
	SigSize    int
	PubKeySize int
	GasCost    uint64
	Verify     PQVerifyFunc
}

// PQTxValidator validates post-quantum transactions against an algorithm
// registry and configurable whitelist. Thread-safe for concurrent use.
type PQTxValidator struct {
	mu         sync.RWMutex
	algorithms map[PQAlgorithm]*PQAlgorithmEntry
	whitelist  map[PQAlgorithm]bool
}

// NewPQTxValidator creates a new validator with default algorithm registrations.
func NewPQTxValidator() *PQTxValidator {
	v := &PQTxValidator{
		algorithms: make(map[PQAlgorithm]*PQAlgorithmEntry),
		whitelist:  make(map[PQAlgorithm]bool),
	}
	v.registerDefaults()
	return v
}

// registerDefaults registers the three supported PQ algorithms.
func (v *PQTxValidator) registerDefaults() {
	v.RegisterAlgorithm(&PQAlgorithmEntry{
		Algorithm:  PQ_MLDSA65,
		Name:       "ML-DSA-65",
		SigSize:    PQMLDSASigSize,
		PubKeySize: PQMLDSAPubKeySize,
		GasCost:    PQGasMLDSA65,
		Verify:     pqDefaultVerifyMLDSA,
	})

	v.RegisterAlgorithm(&PQAlgorithmEntry{
		Algorithm:  PQ_FALCON512,
		Name:       "Falcon-512",
		SigSize:    PQFalconSigSize,
		PubKeySize: PQFalconPubKeySize,
		GasCost:    PQGasFalcon,
		Verify:     pqDefaultVerifyFalcon,
	})

	v.RegisterAlgorithm(&PQAlgorithmEntry{
		Algorithm:  PQ_SPHINCS_SHA256,
		Name:       "SPHINCS+-SHA256",
		SigSize:    PQSPHINCSPSigSize,
		PubKeySize: PQSPHINCSPubKeySize,
		GasCost:    PQGasSPHINCS,
		Verify:     pqDefaultVerifySPHINCS,
	})

	// All algorithms are whitelisted by default.
	v.whitelist[PQ_MLDSA65] = true
	v.whitelist[PQ_FALCON512] = true
	v.whitelist[PQ_SPHINCS_SHA256] = true
}

// RegisterAlgorithm adds an algorithm to the validator registry.
func (v *PQTxValidator) RegisterAlgorithm(entry *PQAlgorithmEntry) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.algorithms[entry.Algorithm] = entry
}

// SetWhitelist configures which algorithms are accepted for PQ transactions.
func (v *PQTxValidator) SetWhitelist(algs []PQAlgorithm) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.whitelist = make(map[PQAlgorithm]bool)
	for _, alg := range algs {
		v.whitelist[alg] = true
	}
}

// IsWhitelisted returns whether an algorithm is in the whitelist.
func (v *PQTxValidator) IsWhitelisted(alg PQAlgorithm) bool {
	v.mu.RLock()
	defer v.mu.RUnlock()
	return v.whitelist[alg]
}

// ValidatePQTransaction performs full validation of a PQ transaction:
// 1. Checks signature and public key are present
// 2. Verifies algorithm is whitelisted
// 3. Validates signature and public key sizes
// 4. Verifies the signature cryptographically
func (v *PQTxValidator) ValidatePQTransaction(tx *PQTransaction) error {
	if tx == nil {
		return ErrPQValidateNilTx
	}

	// Check signature presence.
	if len(tx.PQSignature) == 0 {
		return ErrPQValidateNoSignature
	}
	if len(tx.PQPublicKey) == 0 {
		return ErrPQValidateNoPubKey
	}

	alg := PQAlgorithm(tx.PQSignatureType)

	// Check algorithm whitelist.
	v.mu.RLock()
	if !v.whitelist[alg] {
		v.mu.RUnlock()
		return fmt.Errorf("%w: algorithm %d", ErrPQValidateAlgNotAllowed, alg)
	}

	entry, exists := v.algorithms[alg]
	v.mu.RUnlock()
	if !exists {
		return fmt.Errorf("%w: %d", ErrPQValidateUnknownAlg, alg)
	}

	// Validate sizes.
	if len(tx.PQSignature) != entry.SigSize {
		return fmt.Errorf("%w: got %d, want %d", ErrPQValidateSigSize, len(tx.PQSignature), entry.SigSize)
	}
	if len(tx.PQPublicKey) != entry.PubKeySize {
		return fmt.Errorf("%w: got %d, want %d", ErrPQValidatePKSize, len(tx.PQPublicKey), entry.PubKeySize)
	}

	// Verify signature.
	return v.ValidatePQSignature(tx)
}

// ValidatePQSignature verifies the PQ signature against the public key.
// The message for signing is the transaction hash (hash-then-sign paradigm).
func (v *PQTxValidator) ValidatePQSignature(tx *PQTransaction) error {
	if tx == nil {
		return ErrPQValidateNilTx
	}

	alg := PQAlgorithm(tx.PQSignatureType)
	v.mu.RLock()
	entry, exists := v.algorithms[alg]
	v.mu.RUnlock()

	if !exists {
		return fmt.Errorf("%w: %d", ErrPQValidateUnknownAlg, alg)
	}

	if entry.Verify == nil {
		return ErrPQValidateVerifyFailed
	}

	// Compute the signing message from the transaction hash.
	txHash := tx.SigningHash()
	if !entry.Verify(tx.PQPublicKey, txHash[:], tx.PQSignature) {
		return ErrPQValidateVerifyFailed
	}
	return nil
}

// EstimatePQGas returns the gas cost for PQ signature verification of the
// given algorithm, including the base transaction cost.
func (v *PQTxValidator) EstimatePQGas(alg PQAlgorithm) (uint64, error) {
	v.mu.RLock()
	entry, exists := v.algorithms[alg]
	v.mu.RUnlock()

	if !exists {
		return 0, fmt.Errorf("%w: %d", ErrPQValidateUnknownAlg, alg)
	}
	return PQBaseGas + entry.GasCost, nil
}

// EstimatePQGasStatic returns gas cost without requiring a validator instance.
func EstimatePQGasStatic(alg PQAlgorithm) uint64 {
	switch alg {
	case PQ_MLDSA65:
		return PQBaseGas + PQGasMLDSA65
	case PQ_FALCON512:
		return PQBaseGas + PQGasFalcon
	case PQ_SPHINCS_SHA256:
		return PQBaseGas + PQGasSPHINCS
	default:
		return 0
	}
}

// AlgorithmName returns the human-readable name for a PQ algorithm.
func (v *PQTxValidator) AlgorithmName(alg PQAlgorithm) string {
	v.mu.RLock()
	entry, exists := v.algorithms[alg]
	v.mu.RUnlock()
	if !exists {
		return "unknown"
	}
	return entry.Name
}

// PQTxPool manages a pool of validated PQ transactions with algorithm
// whitelisting and capacity limits.
type PQTxPool struct {
	mu        sync.RWMutex
	validator *PQTxValidator
	pending   map[Hash]*PQTransaction
	maxSize   int
}

// NewPQTxPool creates a new PQ transaction pool with the given capacity.
func NewPQTxPool(validator *PQTxValidator, maxSize int) *PQTxPool {
	if maxSize <= 0 {
		maxSize = 4096
	}
	return &PQTxPool{
		validator: validator,
		pending:   make(map[Hash]*PQTransaction),
		maxSize:   maxSize,
	}
}

// AcceptPQTx validates and adds a PQ transaction to the pool.
func (p *PQTxPool) AcceptPQTx(tx *PQTransaction) error {
	if tx == nil {
		return ErrPQValidateNilTx
	}

	// Check capacity.
	p.mu.RLock()
	if len(p.pending) >= p.maxSize {
		p.mu.RUnlock()
		return ErrPQPoolFull
	}

	// Check for duplicates.
	txHash := tx.Hash()
	if _, exists := p.pending[txHash]; exists {
		p.mu.RUnlock()
		return ErrPQPoolDuplicate
	}
	p.mu.RUnlock()

	// Validate the transaction.
	if err := p.validator.ValidatePQTransaction(tx); err != nil {
		return err
	}

	// Check gas sufficiency.
	alg := PQAlgorithm(tx.PQSignatureType)
	requiredGas, err := p.validator.EstimatePQGas(alg)
	if err != nil {
		return err
	}
	if tx.Gas < requiredGas {
		return fmt.Errorf("%w: tx gas %d < required %d", ErrPQValidateGasLimit, tx.Gas, requiredGas)
	}

	// Add to pool.
	p.mu.Lock()
	p.pending[txHash] = tx
	p.mu.Unlock()
	return nil
}

// PendingCount returns the number of PQ transactions in the pool.
func (p *PQTxPool) PendingCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.pending)
}

// RemovePQTx removes a transaction from the pool by hash.
func (p *PQTxPool) RemovePQTx(hash Hash) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pending, hash)
}

// GetPQTx retrieves a transaction from the pool by hash.
func (p *PQTxPool) GetPQTx(hash Hash) *PQTransaction {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.pending[hash]
}

// PQAlgorithmString returns the string representation of a PQ algorithm.
func PQAlgorithmString(alg PQAlgorithm) string {
	switch alg {
	case PQ_MLDSA65:
		return "ML-DSA-65"
	case PQ_FALCON512:
		return "Falcon-512"
	case PQ_SPHINCS_SHA256:
		return "SPHINCS+-SHA256"
	default:
		return fmt.Sprintf("unknown(%d)", alg)
	}
}

// PQSupportedAlgorithms returns all supported PQ algorithm identifiers.
func PQSupportedAlgorithms() []PQAlgorithm {
	return []PQAlgorithm{PQ_MLDSA65, PQ_FALCON512, PQ_SPHINCS_SHA256}
}
