package secp256k1

import (
	"crypto/ecdsa"
	"crypto/rand"
	"errors"

	"github.com/ethereum/go-ethereum/crypto/ecies"
)

const eciesOverhead = 65 + 16 + 32

var (
	ErrInvalidPublicKey = ecies.ErrInvalidPublicKey
	ErrECIESCiphertext  = errors.New("ecies: invalid ciphertext")
	ErrMACMismatch      = ecies.ErrInvalidMessage
	ErrKeyAgreement     = errors.New("ecies: key agreement failed")
)

func validPublicKey(pub *ecdsa.PublicKey) bool {
	return pub != nil && pub.Curve == s256 && pub.X != nil && pub.Y != nil && s256.IsOnCurve(pub.X, pub.Y)
}

// ECIESEncrypt uses the same ECIES encoding as go-ethereum's devp2p transport.
func ECIESEncrypt(pub *ecdsa.PublicKey, plaintext []byte) ([]byte, error) {
	if !validPublicKey(pub) {
		return nil, ErrInvalidPublicKey
	}
	return ecies.Encrypt(rand.Reader, ecies.ImportECDSAPublic(pub), plaintext, nil, nil)
}

func ECIESDecrypt(prv *ecdsa.PrivateKey, data []byte) ([]byte, error) {
	if prv == nil || prv.D == nil || !validPublicKey(&prv.PublicKey) {
		return nil, ErrKeyAgreement
	}
	if len(data) < eciesOverhead {
		return nil, ErrECIESCiphertext
	}
	return ecies.ImportECDSA(prv).Decrypt(data, nil, nil)
}

// GenerateSharedSecret returns the padded 32-byte secp256k1 ECDH secret.
func GenerateSharedSecret(prv *ecdsa.PrivateKey, pub *ecdsa.PublicKey) ([]byte, error) {
	if !validPublicKey(pub) {
		return nil, ErrInvalidPublicKey
	}
	if prv == nil || prv.D == nil || !validPublicKey(&prv.PublicKey) {
		return nil, ErrKeyAgreement
	}
	return ecies.ImportECDSA(prv).GenerateShared(ecies.ImportECDSAPublic(pub), 16, 16)
}
