// Dilithium post-quantum lattice-based signature scheme for PQ attestations.
// This implements a simulated Dilithium-style scheme (L+ upgrade) with
// configurable security levels. The cryptographic operations use Keccak256
// as a stand-in for real lattice math until a production library is integrated.
package pqc

import "errors"

// Dilithium parameter constants (CRYSTALS-Dilithium specification).
const (
	DilithiumN = 256     // polynomial degree
	DilithiumQ = 8380417 // prime modulus
	DilithiumD = 13      // dropped bits for rounding
)

// Security levels corresponding to NIST post-quantum security levels.
const (
	DilithiumSecurityLevel2 = 2
	DilithiumSecurityLevel3 = 3
	DilithiumSecurityLevel5 = 5
)

// DilithiumParams holds Dilithium lattice parameters.
type DilithiumParams struct {
	N             int // polynomial degree
	Q             int // prime modulus
	D             int // dropped bits
	SecurityLevel int // NIST security level (2, 3, or 5)
}

// DefaultDilithiumParams describes the selected ML-DSA-65 parameter set.
func DefaultDilithiumParams() *DilithiumParams {
	return &DilithiumParams{
		N:             DilithiumN,
		Q:             DilithiumQ,
		D:             DilithiumD,
		SecurityLevel: DilithiumSecurityLevel3,
	}
}

// dilithiumSigSize is the packed ML-DSA-65 signature size.
const dilithiumSigSize = MLDSASignatureSize

// DilithiumSignatureSize returns the signature size in bytes.
func DilithiumSignatureSize() int {
	return dilithiumSigSize
}

// Errors for Dilithium operations.
var (
	ErrDilithiumNilKey   = errors.New("dilithium: nil key pair")
	ErrDilithiumEmptyMsg = errors.New("dilithium: empty message")
)

// DilithiumKeyPair holds a Dilithium key pair with associated parameters.
type DilithiumKeyPair struct {
	PublicKey []byte
	SecretKey []byte
	Params    *DilithiumParams
}

// GenerateDilithiumKey generates a new Dilithium key pair using default params.
// SecretKey is 32 random bytes (seed). PublicKey is derived as
// Keccak256(SecretKey || "dilithium-pk") to simulate lattice key derivation.
func GenerateDilithiumKey() (*DilithiumKeyPair, error) {
	key, err := (&DilithiumSigner{}).GenerateKey()
	if err != nil {
		return nil, err
	}
	return &DilithiumKeyPair{PublicKey: key.PublicKey, SecretKey: key.SecretKey, Params: DefaultDilithiumParams()}, nil
}

func (kp *DilithiumKeyPair) Sign(message []byte) ([]byte, error) {
	if kp == nil || len(kp.SecretKey) == 0 {
		return nil, ErrDilithiumNilKey
	}
	return (&DilithiumSigner{}).Sign(kp.SecretKey, message)
}

func (kp *DilithiumKeyPair) Verify(message, signature []byte) bool {
	return kp != nil && VerifyDilithium(kp.PublicKey, message, signature)
}

func VerifyDilithium(publicKey, message, signature []byte) bool {
	return (&DilithiumSigner{}).Verify(publicKey, message, signature)
}
