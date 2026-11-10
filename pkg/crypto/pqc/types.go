// Package pqc provides post-quantum cryptographic primitives for Ethereum.
// Algorithm IDs are adapted to ML-DSA-65, FN-DSA-512, and SLH-DSA-SHA2-128f.
package pqc

import "errors"

// PQAlgorithm identifies a post-quantum signature algorithm.
type PQAlgorithm uint8

const (
	DILITHIUM3    PQAlgorithm = 0
	FALCON512     PQAlgorithm = 1
	SPHINCSSHA256 PQAlgorithm = 2
)

// Size constants for Dilithium3 (CRYSTALS-Dilithium, NIST level 3).
const (
	Dilithium3PubKeySize = MLDSAPublicKeySize
	Dilithium3SecKeySize = MLDSAPrivateKeySize
	Dilithium3SigSize    = MLDSASignatureSize
)

// Size constants for Falcon-512 (NIST level 1).
const (
	Falcon512PubKeySize = 897
	Falcon512SecKeySize = 1345
	Falcon512SigSize    = 666
)

// Size constants for SPHINCS+-SHA256 (stateless hash-based, NIST level 1).
const (
	SPHINCSSha256PubKeySize = 32
	SPHINCSSha256SecKeySize = 64
	SPHINCSSha256SigSize    = 17088
)

// PQKeyPair holds a post-quantum key pair.
type PQKeyPair struct {
	Algorithm PQAlgorithm
	PublicKey []byte
	SecretKey []byte
}

// PQSignature holds a post-quantum signature.
type PQSignature struct {
	Algorithm PQAlgorithm
	PublicKey []byte
	Signature []byte
}

// Errors returned by PQ operations.
var (
	ErrUnknownAlgorithm = errors.New("pqc: unknown algorithm")
	ErrInvalidKeySize   = errors.New("pqc: invalid key size")
	ErrInvalidSigSize   = errors.New("pqc: invalid signature size")
	ErrVerifyFailed     = errors.New("pqc: verification failed")
)

// PubKeySize returns the public key size for the given algorithm.
func PubKeySize(alg PQAlgorithm) int {
	switch alg {
	case DILITHIUM3:
		return DSign3PubKeyBytes
	case FALCON512:
		return Falcon512PubKeySize
	case SPHINCSSHA256:
		return SPHINCSSha256PubKeySize
	default:
		return 0
	}
}

// SecKeySize returns the secret key size for the given algorithm.
func SecKeySize(alg PQAlgorithm) int {
	switch alg {
	case DILITHIUM3:
		return DSign3SecKeyBytes
	case FALCON512:
		return Falcon512SecKeySize
	case SPHINCSSHA256:
		return SPHINCSSha256SecKeySize
	default:
		return 0
	}
}

// SigSize returns the signature size for the given algorithm.
func SigSize(alg PQAlgorithm) int {
	switch alg {
	case DILITHIUM3:
		return DSign3SigBytes
	case FALCON512:
		return Falcon512SigSize
	case SPHINCSSHA256:
		return SPHINCSSha256SigSize
	default:
		return 0
	}
}
