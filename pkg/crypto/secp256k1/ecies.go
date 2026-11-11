package secp256k1

import (
	"crypto/aes"
	"crypto/ecdsa"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
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
	if len(plaintext) != 0 {
		return ecies.Encrypt(rand.Reader, ecies.ImportECDSAPublic(pub), plaintext, nil, nil)
	}
	// Geth's decryptor accepts an IV-only encrypted body. Its encryptor
	// rejects that body, so assemble the standard envelope for this case.
	ephemeral, err := GenerateKey()
	if err != nil {
		return nil, err
	}
	secret, err := GenerateSharedSecret(ephemeral, pub)
	if err != nil {
		return nil, err
	}
	var kdfInput [4 + 32]byte
	kdfInput[3] = 1
	copy(kdfInput[4:], secret)
	keys := sha256.Sum256(kdfInput[:])
	macKey := sha256.Sum256(keys[aes.BlockSize:])
	var iv [aes.BlockSize]byte
	if _, err := rand.Read(iv[:]); err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, macKey[:])
	mac.Write(iv[:])
	out := append(FromECDSAPub(&ephemeral.PublicKey), iv[:]...)
	return mac.Sum(out), nil
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
