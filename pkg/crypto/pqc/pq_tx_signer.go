// Package pqc provides post-quantum cryptographic primitives for Ethereum.
// pq_tx_signer.go implements a transaction signing framework supporting
// multiple PQ schemes (Dilithium3, Falcon-512, SPHINCS+-128) with batch
// verification and the hash-then-sign pattern.
package pqc

import (
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// PQSignatureType enumerates supported post-quantum signature schemes.
type PQSignatureType uint8

const (
	// SigDilithium3 is CRYSTALS-Dilithium at NIST level 3.
	SigDilithium3 PQSignatureType = 0
	// SigFalcon512 is Falcon at NIST level 1.
	SigFalcon512 PQSignatureType = 1
	// SigSPHINCS128 is SPHINCS+-128 (stateless hash-based, NIST level 1).
	SigSPHINCS128 PQSignatureType = 2
)

// Canonical signature sizes per scheme, matching actual signer output.
const (
	Dilithium3SignatureSize = DSign3SigBytes       // matches real lattice DilithiumSigner output
	Falcon512SignatureSize  = Falcon512SigSize     // matches FalconSigner output
	SPHINCS128SignatureSize = SPHINCSSignerSigSize // matches SPHINCSSigner output
)

// SignatureSizeForType returns the expected signature size for a scheme.
func SignatureSizeForType(st PQSignatureType) (int, error) {
	switch st {
	case SigDilithium3:
		return Dilithium3SignatureSize, nil
	case SigFalcon512:
		return Falcon512SignatureSize, nil
	case SigSPHINCS128:
		return SPHINCS128SignatureSize, nil
	default:
		return 0, ErrUnsupportedScheme
	}
}

// Errors returned by PQTxSigner operations.
var (
	ErrUnsupportedScheme = errors.New("pq_tx_signer: unsupported PQ scheme")
	ErrNilTransaction    = errors.New("pq_tx_signer: nil transaction")
	ErrNilKey            = errors.New("pq_tx_signer: nil key")
	ErrSchemeMismatch    = errors.New("pq_tx_signer: key scheme does not match signer")
	ErrInvalidSignature  = errors.New("pq_tx_signer: invalid signature")
	ErrBatchLenMismatch  = errors.New("pq_tx_signer: batch length mismatch")
)

// PQPrivateKey holds a post-quantum private key with its scheme identifier.
type PQPrivateKey struct {
	Scheme PQSignatureType
	Raw    []byte
}

// PQPublicKey holds a post-quantum public key with its scheme identifier.
type PQPublicKey struct {
	Scheme PQSignatureType
	Raw    []byte
}

// PQTxSigner signs and verifies Ethereum transactions using post-quantum
// signature schemes. It is safe for concurrent use.
type PQTxSigner struct {
	mu     sync.RWMutex
	scheme PQSignatureType
}

// NewPQTxSigner creates a signer for the given PQ scheme.
func NewPQTxSigner(scheme PQSignatureType) (*PQTxSigner, error) {
	if _, err := SignatureSizeForType(scheme); err != nil {
		return nil, err
	}
	return &PQTxSigner{scheme: scheme}, nil
}

// Scheme returns the signer's PQ scheme.
func (s *PQTxSigner) Scheme() PQSignatureType {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.scheme
}

// GenerateKey generates a new PQ key pair for the signer's scheme.
// For Falcon-512 and SPHINCS+, uses the real lattice/hash-based key generation.
// For Dilithium3, uses the DilithiumSigner (stub with matching Sign/Verify).
func (s *PQTxSigner) GenerateKey() (*PQPrivateKey, *PQPublicKey, error) {
	s.mu.RLock()
	scheme := s.scheme
	s.mu.RUnlock()

	switch scheme {
	case SigFalcon512:
		signer := &FalconSigner{}
		kp, err := signer.GenerateKey()
		if err != nil {
			return nil, nil, err
		}
		return &PQPrivateKey{Scheme: scheme, Raw: kp.SecretKey},
			&PQPublicKey{Scheme: scheme, Raw: kp.PublicKey}, nil

	case SigSPHINCS128:
		signer := NewSPHINCSSigner()
		kp, err := signer.GenerateKey()
		if err != nil {
			return nil, nil, err
		}
		return &PQPrivateKey{Scheme: scheme, Raw: kp.SecretKey},
			&PQPublicKey{Scheme: scheme, Raw: kp.PublicKey}, nil

	default:
		// Dilithium3: use real lattice-based key generation.
		d := &DilithiumSigner{}
		kp, err := d.GenerateKey()
		if err != nil {
			return nil, nil, err
		}
		return &PQPrivateKey{Scheme: scheme, Raw: kp.SecretKey},
			&PQPublicKey{Scheme: scheme, Raw: kp.PublicKey}, nil
	}
}

// SignTransaction signs a transaction using the hash-then-sign pattern:
// signature = Sign(Keccak256(RLP(tx)), privateKey).
// The returned signature has the canonical size for the scheme.
func (s *PQTxSigner) SignTransaction(tx *types.Transaction, key *PQPrivateKey) ([]byte, error) {
	if tx == nil {
		return nil, ErrNilTransaction
	}
	if key == nil {
		return nil, ErrNilKey
	}

	s.mu.RLock()
	scheme := s.scheme
	s.mu.RUnlock()

	if key.Scheme != scheme {
		return nil, ErrSchemeMismatch
	}

	// The PQ envelope authenticates the algorithm. Set it on the signing
	// copy so callers can still attach the signature after signing.
	if pq, ok := tx.Inner().(*types.PQTransaction); ok {
		unsigned := *pq
		unsigned.PQSignatureType = uint8(scheme)
		digest := unsigned.SigningHash()
		return signHash(scheme, key.Raw, digest[:])
	}
	txHash := hashTransaction(tx)
	return signHash(scheme, key.Raw, txHash[:])
}

// VerifyTransaction verifies a PQ signature over a transaction.
func (s *PQTxSigner) VerifyTransaction(tx *types.Transaction, sig []byte, pubkey *PQPublicKey) (bool, error) {
	if tx == nil {
		return false, ErrNilTransaction
	}
	if pubkey == nil {
		return false, ErrNilKey
	}

	s.mu.RLock()
	scheme := s.scheme
	s.mu.RUnlock()

	if pubkey.Scheme != scheme || tx.Type() == types.PQTransactionType && tx.PQSigType() != uint8(scheme) {
		return false, ErrSchemeMismatch
	}

	expectedSize, _ := SignatureSizeForType(scheme)
	if len(sig) != expectedSize {
		return false, ErrInvalidSignature
	}

	txHash := hashTransaction(tx)
	return verifyHash(scheme, pubkey.Raw, txHash[:], sig), nil
}

// VerifyBatch verifies multiple transaction signatures in parallel.
// Returns a bool slice indicating validity of each signature, plus an error
// if input slices have mismatched lengths.
func (s *PQTxSigner) VerifyBatch(
	txs []*types.Transaction,
	sigs [][]byte,
	pubkeys []*PQPublicKey,
) ([]bool, error) {
	n := len(txs)
	if n != len(sigs) || n != len(pubkeys) {
		return nil, ErrBatchLenMismatch
	}
	if n == 0 {
		return nil, nil
	}

	results := make([]bool, n)
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			ok, err := s.VerifyTransaction(txs[idx], sigs[idx], pubkeys[idx])
			results[idx] = ok && err == nil
		}(i)
	}

	wg.Wait()
	return results, nil
}

// --- internal helpers ---

// hashTransaction computes Keccak256(RLP(tx)) for the hash-then-sign pattern.
func hashTransaction(tx *types.Transaction) types.Hash {
	if tx.Type() == types.PQTransactionType {
		return tx.SigningHash()
	}
	encoded, err := tx.EncodeRLP()
	if err != nil {
		return types.Hash{}
	}
	return crypto.Keccak256Hash(encoded)
}

// signHash produces a PQ signature over a hash using the given scheme.
// Dispatches to the real signer for each scheme.
func signHash(scheme PQSignatureType, privKey, hash []byte) ([]byte, error) {
	switch scheme {
	case SigFalcon512:
		signer := &FalconSigner{}
		return signer.Sign(privKey, hash)

	case SigSPHINCS128:
		signer := NewSPHINCSSigner()
		return signer.Sign(privKey, hash)

	case SigDilithium3:
		// DilithiumSigner uses real lattice-based signing.
		d := &DilithiumSigner{}
		return d.Sign(privKey, hash)

	default:
		return nil, ErrUnsupportedScheme
	}
}

// verifyHash verifies a PQ signature over a hash using the given scheme.
// Dispatches to the real verifier for each scheme.
func verifyHash(scheme PQSignatureType, pubKey, hash, sig []byte) bool {
	if len(pubKey) == 0 || len(hash) == 0 || len(sig) == 0 {
		return false
	}
	switch scheme {
	case SigFalcon512:
		signer := &FalconSigner{}
		return signer.Verify(pubKey, hash, sig)

	case SigSPHINCS128:
		signer := NewSPHINCSSigner()
		return signer.Verify(pubKey, hash, sig)

	case SigDilithium3:
		// DilithiumSigner uses real lattice-based verification.
		d := &DilithiumSigner{}
		return d.Verify(pubKey, hash, sig)

	default:
		return false
	}
}

// keySizesForScheme returns (privateKeySize, publicKeySize) for a scheme.
func keySizesForScheme(scheme PQSignatureType) (int, int, error) {
	switch scheme {
	case SigDilithium3:
		return DSign3SecKeyBytes, DSign3PubKeyBytes, nil
	case SigFalcon512:
		return Falcon512SecKeySize, Falcon512PubKeySize, nil
	case SigSPHINCS128:
		// SPHINCS+-128s key sizes.
		return 64, 32, nil
	default:
		return 0, 0, ErrUnsupportedScheme
	}
}
