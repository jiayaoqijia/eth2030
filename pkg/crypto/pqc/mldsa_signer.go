package pqc

import (
	"errors"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
)

const (
	MLDSAPublicKeySize  = mldsa65.PublicKeySize
	MLDSAPrivateKeySize = mldsa65.PrivateKeySize
	MLDSASignatureSize  = mldsa65.SignatureSize
)

// MLDSAKeyPair stores packed ML-DSA-65 keys.
type MLDSAKeyPair struct {
	PublicKey []byte
	SecretKey []byte
}

type MLDSASigner struct{}

var (
	ErrMLDSANilKey   = errors.New("mldsa: nil key")
	ErrMLDSAEmptyMsg = errors.New("mldsa: empty message")
)

func NewMLDSASigner() *MLDSASigner { return &MLDSASigner{} }

func (*MLDSASigner) Algorithm() PQAlgorithm { return DILITHIUM3 }

func (*MLDSASigner) GenerateKey() (*MLDSAKeyPair, error) {
	pub, priv, err := mldsa65.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	return &MLDSAKeyPair{PublicKey: pub.Bytes(), SecretKey: priv.Bytes()}, nil
}

func (*MLDSASigner) Sign(key *MLDSAKeyPair, msg []byte) ([]byte, error) {
	if key == nil {
		return nil, ErrMLDSANilKey
	}
	if len(msg) == 0 {
		return nil, ErrMLDSAEmptyMsg
	}
	if len(key.SecretKey) != MLDSAPrivateKeySize {
		return nil, ErrInvalidKeySize
	}
	var private mldsa65.PrivateKey
	if err := private.UnmarshalBinary(key.SecretKey); err != nil {
		return nil, err
	}
	signature := make([]byte, mldsa65.SignatureSize)
	if err := mldsa65.SignTo(&private, msg, nil, false, signature); err != nil {
		return nil, err
	}
	return signature, nil
}

func (*MLDSASigner) Verify(pk, msg, sig []byte) bool {
	var public mldsa65.PublicKey
	return len(msg) != 0 && len(sig) == mldsa65.SignatureSize &&
		public.UnmarshalBinary(pk) == nil &&
		mldsa65.Verify(&public, msg, nil, sig)
}
