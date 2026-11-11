// PQ transaction validator with real cryptographic dispatch for post-quantum
// signature verification. Replaces stub verification from pq_tx_validation.go
// with function-based dispatch to actual PQ signers (ML-DSA-65, Falcon-512,
// SPHINCS+-SHA256). Addresses Gap #64 (PQ Transactions) by wiring real PQ
// crypto into the transaction validation pipeline.
//
// The validator uses dependency injection via function callbacks to avoid
// circular imports between core/types and crypto/pqc. Callers wire in the
// real signer implementations at construction time.
package types

import (
	"errors"
	"fmt"
	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"github.com/cloudflare/circl/sign/slhdsa"
	"github.com/pornin/go-fn-dsa/fndsa"
	"sync"

	"golang.org/x/crypto/sha3"
)

// PQ signature type constants for the real validator.
const (
	PQSigTypeMLDSA   uint8 = 0 // ML-DSA-65 (CRYSTALS-Dilithium)
	PQSigTypeFalcon  uint8 = 1 // Falcon-512
	PQSigTypeSPHINCS uint8 = 2 // SPHINCS+-SHA256
)

// Gas costs for PQ signature verification per algorithm.
const (
	PQGasCostMLDSA   uint64 = 8000
	PQGasCostFalcon  uint64 = 12000
	PQGasCostSPHINCS uint64 = 45000
)

// Expected public key sizes per PQ algorithm for the real validator.
var pqRealPubKeySizes = map[uint8]int{
	PQSigTypeMLDSA:   DilithiumPubKeySize,
	PQSigTypeFalcon:  FalconPubKeySize,      // 897
	PQSigTypeSPHINCS: SPHINCSPlusPubKeySize, // 32
}

// Expected signature sizes per PQ algorithm for the real validator.
var pqRealSigSizes = map[uint8]int{
	PQSigTypeMLDSA:   DilithiumSigSize,
	PQSigTypeFalcon:  FalconSigSize,
	PQSigTypeSPHINCS: SPHINCSPlusSigSize,
}

// PQRealVerifyFunc verifies a PQ signature: (pubkey, message, signature) -> valid.
type PQRealVerifyFunc func(pk, msg, sig []byte) bool

// PQRealSignerEntry holds configuration for a single PQ algorithm.
type PQRealSignerEntry struct {
	Name       string
	SigType    uint8
	PubKeySize int
	SigSize    int
	GasCost    uint64
	Verify     PQRealVerifyFunc
}

// Errors for the real PQ transaction validator.
var (
	ErrPQRealNilTx        = errors.New("pq_real_validator: nil transaction")
	ErrPQRealNoSig        = errors.New("pq_real_validator: missing signature")
	ErrPQRealNoPubKey     = errors.New("pq_real_validator: missing public key")
	ErrPQRealUnknownAlg   = errors.New("pq_real_validator: unknown algorithm")
	ErrPQRealSigSizeBad   = errors.New("pq_real_validator: signature size mismatch")
	ErrPQRealPKSizeBad    = errors.New("pq_real_validator: public key size mismatch")
	ErrPQRealVerifyFailed = errors.New("pq_real_validator: signature verification failed")
	ErrPQRealNoVerifier   = errors.New("pq_real_validator: no verifier registered")
	ErrPQRealBatchEmpty   = errors.New("pq_real_validator: empty batch")
	ErrPQRealHybridNoSig  = errors.New("pq_real_validator: hybrid mode requires classic signature")
)

// PQTxValidatorReal validates PQ transactions by dispatching to real PQ
// cryptographic signers. Thread-safe for concurrent use.
type PQTxValidatorReal struct {
	mu      sync.RWMutex
	signers map[uint8]*PQRealSignerEntry
}

// NewPQTxValidatorReal creates a validator with the standard PQ providers.
func NewPQTxValidatorReal() *PQTxValidatorReal {
	v := &PQTxValidatorReal{
		signers: make(map[uint8]*PQRealSignerEntry),
	}
	v.signers[PQSigTypeMLDSA] = &PQRealSignerEntry{
		Name:       "ML-DSA-65",
		SigType:    PQSigTypeMLDSA,
		PubKeySize: pqRealPubKeySizes[PQSigTypeMLDSA],
		SigSize:    pqRealSigSizes[PQSigTypeMLDSA],
		GasCost:    PQGasCostMLDSA,
		Verify:     pqDefaultVerifyMLDSA,
	}
	v.signers[PQSigTypeFalcon] = &PQRealSignerEntry{
		Name:       "Falcon-512",
		SigType:    PQSigTypeFalcon,
		PubKeySize: pqRealPubKeySizes[PQSigTypeFalcon],
		SigSize:    pqRealSigSizes[PQSigTypeFalcon],
		GasCost:    PQGasCostFalcon,
		Verify:     pqDefaultVerifyFalcon,
	}
	v.signers[PQSigTypeSPHINCS] = &PQRealSignerEntry{
		Name:       "SPHINCS+-SHA256",
		SigType:    PQSigTypeSPHINCS,
		PubKeySize: pqRealPubKeySizes[PQSigTypeSPHINCS],
		SigSize:    pqRealSigSizes[PQSigTypeSPHINCS],
		GasCost:    PQGasCostSPHINCS,
		Verify:     pqDefaultVerifySPHINCS,
	}
	return v
}

// DefaultPQValidator is the package-level shared PQ transaction validator.
var DefaultPQValidator = NewPQTxValidatorReal()

// RegisterSigner registers a real PQ verifier function for the given algorithm.
func (v *PQTxValidatorReal) RegisterSigner(sigType uint8, verify PQRealVerifyFunc) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if entry, ok := v.signers[sigType]; ok {
		entry.Verify = verify
	}
}

// ValidatePQSignatureReal dispatches to the correct real PQ signer for
// cryptographic verification. The signing message is the transaction hash.
func (v *PQTxValidatorReal) ValidatePQSignatureReal(tx *PQTransaction) error {
	if tx == nil {
		return ErrPQRealNilTx
	}
	if len(tx.PQSignature) == 0 {
		return ErrPQRealNoSig
	}
	if len(tx.PQPublicKey) == 0 {
		return ErrPQRealNoPubKey
	}

	v.mu.RLock()
	entry, ok := v.signers[tx.PQSignatureType]
	v.mu.RUnlock()

	if !ok {
		return fmt.Errorf("%w: type %d", ErrPQRealUnknownAlg, tx.PQSignatureType)
	}

	// Validate key size.
	if len(tx.PQPublicKey) != entry.PubKeySize {
		return fmt.Errorf("%w: got %d, want %d for %s",
			ErrPQRealPKSizeBad, len(tx.PQPublicKey), entry.PubKeySize, entry.Name)
	}

	// Validate signature size.
	if len(tx.PQSignature) != entry.SigSize {
		return fmt.Errorf("%w: got %d, want %d for %s",
			ErrPQRealSigSizeBad, len(tx.PQSignature), entry.SigSize, entry.Name)
	}

	if entry.Verify == nil {
		return ErrPQRealNoVerifier
	}

	// Compute signing message: hash of the unsigned transaction fields.
	msg := tx.SigningHash()

	if !entry.Verify(tx.PQPublicKey, msg[:], tx.PQSignature) {
		return ErrPQRealVerifyFailed
	}
	return nil
}

// EstimatePQGasReal returns the gas cost for verifying a PQ signature of
// the given algorithm type.
func (v *PQTxValidatorReal) EstimatePQGasReal(sigType uint8) (uint64, error) {
	v.mu.RLock()
	entry, ok := v.signers[sigType]
	v.mu.RUnlock()
	if !ok {
		return 0, fmt.Errorf("%w: type %d", ErrPQRealUnknownAlg, sigType)
	}
	return entry.GasCost, nil
}

// RecoverPQSender derives an Ethereum address from a PQ public key by
// computing keccak256(pubkey) and taking the last 20 bytes, analogous
// to how secp256k1 public keys derive addresses.
func (v *PQTxValidatorReal) RecoverPQSender(tx *PQTransaction) (Address, error) {
	if tx == nil {
		return Address{}, ErrPQRealNilTx
	}
	if len(tx.PQPublicKey) == 0 {
		return Address{}, ErrPQRealNoPubKey
	}
	return PQPubKeyToAddress(tx.PQPublicKey), nil
}

// PQPubKeyToAddress converts a PQ public key to an Ethereum address using
// keccak256(pubkey)[12:32]. This is the same derivation as secp256k1 but
// applied to a post-quantum public key.
func PQPubKeyToAddress(pubkey []byte) Address {
	d := sha3.NewLegacyKeccak256()
	d.Write(pubkey)
	hash := d.Sum(nil)
	var addr Address
	copy(addr[:], hash[12:32])
	return addr
}

// SupportedAlgorithmsReal returns the names of all registered PQ algorithms.
func (v *PQTxValidatorReal) SupportedAlgorithmsReal() []string {
	v.mu.RLock()
	defer v.mu.RUnlock()
	names := make([]string, 0, len(v.signers))
	// Return in deterministic order.
	for sigType := uint8(0); sigType <= PQSigTypeSPHINCS; sigType++ {
		if entry, ok := v.signers[sigType]; ok {
			names = append(names, entry.Name)
		}
	}
	return names
}

// ValidatePQKeySize checks that the given public key has the correct size
// for the specified algorithm.
func (v *PQTxValidatorReal) ValidatePQKeySize(sigType uint8, pubkey []byte) error {
	v.mu.RLock()
	entry, ok := v.signers[sigType]
	v.mu.RUnlock()
	if !ok {
		return fmt.Errorf("%w: type %d", ErrPQRealUnknownAlg, sigType)
	}
	if len(pubkey) != entry.PubKeySize {
		return fmt.Errorf("%w: got %d, want %d for %s",
			ErrPQRealPKSizeBad, len(pubkey), entry.PubKeySize, entry.Name)
	}
	return nil
}

// ValidatePQHybrid validates a PQ transaction in hybrid mode, requiring
// both a PQ signature and a classic (ECDSA) signature to be present and
// non-empty.
func (v *PQTxValidatorReal) ValidatePQHybrid(tx *PQTransaction) error {
	if tx == nil {
		return ErrPQRealNilTx
	}
	if len(tx.ClassicSignature) == 0 {
		return ErrPQRealHybridNoSig
	}
	// Validate the PQ portion.
	return v.ValidatePQSignatureReal(tx)
}

// ValidatePQBatch validates multiple PQ transactions concurrently.
// Returns a slice of errors, one per transaction (nil means valid).
func (v *PQTxValidatorReal) ValidatePQBatch(txs []*PQTransaction) []error {
	if len(txs) == 0 {
		return nil
	}
	errs := make([]error, len(txs))
	var wg sync.WaitGroup
	wg.Add(len(txs))
	for i, tx := range txs {
		go func(idx int, t *PQTransaction) {
			defer wg.Done()
			errs[idx] = v.ValidatePQSignatureReal(t)
		}(i, tx)
	}
	wg.Wait()
	return errs
}

// GetSignerEntry returns the signer entry for a given algorithm type.
func (v *PQTxValidatorReal) GetSignerEntry(sigType uint8) (*PQRealSignerEntry, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	entry, ok := v.signers[sigType]
	return entry, ok
}

func pqDefaultVerifyMLDSA(pk, msg, sig []byte) bool {
	var key mldsa65.PublicKey
	return len(pk) == DilithiumPubKeySize && len(sig) == DilithiumSigSize &&
		key.UnmarshalBinary(pk) == nil && mldsa65.Verify(&key, msg, nil, sig)
}

func pqDefaultVerifyFalcon(pk, msg, sig []byte) bool {
	return len(pk) == FalconPubKeySize && len(sig) == FalconSigSize &&
		fndsa.Verify(pk, nil, 0, msg, sig)
}

func pqDefaultVerifySPHINCS(pk, msg, sig []byte) bool {
	key := slhdsa.PublicKey{ID: slhdsa.SHA2_128f}
	return len(pk) == SPHINCSPlusPubKeySize && len(sig) == SPHINCSPlusSigSize &&
		key.UnmarshalBinary(pk) == nil && slhdsa.Verify(&key, slhdsa.NewMessage(msg), sig, nil)
}

// ValidatePQTransactionSignature uses the fixed providers for consensus and admission.
func ValidatePQTransactionSignature(tx *PQTransaction) error {
	if tx == nil {
		return ErrPQRealNilTx
	}
	hash := tx.SigningHash()
	if hash == (Hash{}) {
		return ErrPQRealVerifyFailed
	}
	var valid bool
	switch tx.PQSignatureType {
	case PQSigDilithium:
		valid = pqDefaultVerifyMLDSA(tx.PQPublicKey, hash[:], tx.PQSignature)
	case PQSigFalcon:
		valid = pqDefaultVerifyFalcon(tx.PQPublicKey, hash[:], tx.PQSignature)
	case PQSigSPHINCS:
		valid = pqDefaultVerifySPHINCS(tx.PQPublicKey, hash[:], tx.PQSignature)
	default:
		return ErrPQRealUnknownAlg
	}
	if !valid {
		return ErrPQRealVerifyFailed
	}
	return nil
}
