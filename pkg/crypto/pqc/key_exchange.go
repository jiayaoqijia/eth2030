package pqc

import (
	"crypto/rand"
	"errors"
	"io"

	"github.com/cloudflare/circl/kem/mlkem/mlkem768"
)

// Kyber parameter constants for Kyber-768 (NIST level 3).
const (
	KyberN    = 256  // Polynomial degree.
	KyberQ    = 3329 // Modulus q.
	KyberK    = 3    // Module rank k (Kyber-768).
	KyberEta1 = 2    // CBD noise parameter for keygen and encryption.
	KyberEta2 = 2    // CBD noise parameter for encryption noise.

	// Derived sizes.
	kyberPublicKeySize    = mlkem768.PublicKeySize
	kyberSecretKeySize    = mlkem768.PrivateKeySize
	kyberCiphertextSize   = mlkem768.CiphertextSize
	kyberSharedSecretSize = 32
)

// KyberParams holds the parameter set for a Kyber KEM instance.
type KyberParams struct {
	N    int   // Polynomial degree (256).
	Q    int16 // Modulus (3329).
	K    int   // Module rank.
	Eta1 int   // Noise parameter for key generation.
	Eta2 int   // Noise parameter for encryption.
}

// DefaultKyberParams returns the Kyber-768 parameter set.
func DefaultKyberParams() KyberParams {
	return KyberParams{
		N:    KyberN,
		Q:    KyberQ,
		K:    KyberK,
		Eta1: KyberEta1,
		Eta2: KyberEta2,
	}
}

// KyberKeyPair holds a Kyber key pair for key encapsulation.
type KyberKeyPair struct {
	PublicKey []byte // Encoded public key (pk = A*s + e || seed).
	SecretKey []byte // Encoded secret key (s polynomial vector).
	params    KyberParams
}

// Kyber key exchange errors.
var (
	ErrKyberInvalidPubKeySize = errors.New("pqc: invalid Kyber public key size")
	ErrKyberInvalidSecKeySize = errors.New("pqc: invalid Kyber secret key size")
	ErrKyberInvalidCiphertext = errors.New("pqc: invalid Kyber ciphertext size")
	ErrKyberDecapsFailure     = errors.New("pqc: Kyber decapsulation failure")
	ErrKyberRandFailure       = errors.New("pqc: random number generation failed")
)

// GenerateKyberKeyPair generates a Kyber-768 key pair using the standard
// KEM key generation procedure.
func GenerateKyberKeyPair() (*KyberKeyPair, error) {
	return GenerateKyberKeyPairWithReader(rand.Reader)
}

func GenerateKyberKeyPairWithReader(rng io.Reader) (*KyberKeyPair, error) {
	public, private, err := mlkem768.GenerateKeyPair(rng)
	if err != nil {
		return nil, err
	}
	pk := make([]byte, mlkem768.PublicKeySize)
	sk := make([]byte, mlkem768.PrivateKeySize)
	public.Pack(pk)
	private.Pack(sk)
	return &KyberKeyPair{PublicKey: pk, SecretKey: sk}, nil
}

func KyberEncapsulate(pk []byte) ([]byte, []byte, error) {
	return KyberEncapsulateWithReader(pk, rand.Reader)
}

func KyberEncapsulateWithReader(pk []byte, rng io.Reader) ([]byte, []byte, error) {
	if len(pk) != mlkem768.PublicKeySize {
		return nil, nil, ErrKyberInvalidPubKeySize
	}
	var public mlkem768.PublicKey
	if err := public.Unpack(pk); err != nil {
		return nil, nil, err
	}
	if rng == nil {
		rng = rand.Reader
	}
	seed := make([]byte, mlkem768.EncapsulationSeedSize)
	if _, err := io.ReadFull(rng, seed); err != nil {
		return nil, nil, err
	}
	ciphertext := make([]byte, mlkem768.CiphertextSize)
	shared := make([]byte, mlkem768.SharedKeySize)
	public.EncapsulateTo(ciphertext, shared, seed)
	return ciphertext, shared, nil
}

func KyberDecapsulate(sk, ciphertext []byte) ([]byte, error) {
	if len(sk) != mlkem768.PrivateKeySize {
		return nil, ErrKyberInvalidSecKeySize
	}
	if len(ciphertext) != mlkem768.CiphertextSize {
		return nil, ErrKyberInvalidCiphertext
	}
	var private mlkem768.PrivateKey
	if err := private.Unpack(sk); err != nil {
		return nil, err
	}
	shared := make([]byte, mlkem768.SharedKeySize)
	private.DecapsulateTo(shared, ciphertext)
	return shared, nil
}
