// Package secp256k1 adapts go-ethereum's secp256k1 implementation to ETH2030.
package secp256k1

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

var (
	s256                 = gethcrypto.S256()
	secp256k1N           = s256.Params().N
	secp256k1halfN       = new(big.Int).Rsh(secp256k1N, 1)
	errInvalidRecoveryID = errors.New("invalid recovery ID")
)

func S256() elliptic.Curve { return s256 }

func GenerateKey() (*ecdsa.PrivateKey, error) { return gethcrypto.GenerateKey() }

func Sign(hash []byte, prv *ecdsa.PrivateKey) ([]byte, error) {
	if prv == nil || prv.D == nil || prv.D.Sign() <= 0 || prv.D.Cmp(secp256k1N) >= 0 || prv.Curve != s256 {
		return nil, errors.New("invalid secp256k1 private key")
	}
	return gethcrypto.Sign(hash, prv)
}

func Ecrecover(hash, sig []byte) ([]byte, error) {
	if err := validateRecoveryInput(hash, sig); err != nil {
		return nil, err
	}
	return gethcrypto.Ecrecover(hash, sig)
}

func SigToPub(hash, sig []byte) (*ecdsa.PublicKey, error) {
	if err := validateRecoveryInput(hash, sig); err != nil {
		return nil, err
	}
	return gethcrypto.SigToPub(hash, sig)
}

func validateRecoveryInput(hash, sig []byte) error {
	if len(sig) != gethcrypto.SignatureLength {
		return errors.New("signature must be 65 bytes [R || S || V]")
	}
	if len(hash) != gethcrypto.DigestLength {
		return errors.New("hash must be 32 bytes")
	}
	if sig[gethcrypto.RecoveryIDOffset] > 1 {
		return errInvalidRecoveryID
	}
	return nil
}

// ValidateSignature accepts an uncompressed public key and a 64-byte signature.
func ValidateSignature(pubkey, hash, sig []byte) bool {
	if len(sig) != 64 || len(hash) != 32 || len(pubkey) != 65 || pubkey[0] != 4 {
		return false
	}
	s := new(big.Int).SetBytes(sig[32:])
	if s.Cmp(secp256k1N) >= 0 {
		return false
	}
	// The existing verification API accepts either S representative.
	if s.Cmp(secp256k1halfN) > 0 {
		var canonical [64]byte
		copy(canonical[:32], sig[:32])
		s.Sub(secp256k1N, s).FillBytes(canonical[32:])
		return gethcrypto.VerifySignature(pubkey, hash, canonical[:])
	}
	return gethcrypto.VerifySignature(pubkey, hash, sig)
}

func ValidateSignatureValues(v byte, r, s *big.Int, homestead bool) bool {
	return r != nil && s != nil && gethcrypto.ValidateSignatureValues(v, r, s, homestead)
}

func PubkeyToAddress(p ecdsa.PublicKey) types.Address {
	if p.X == nil || p.Y == nil {
		return types.Address{}
	}
	return types.Address(gethcrypto.PubkeyToAddress(p))
}

func CompressPubkey(pub *ecdsa.PublicKey) []byte {
	if pub == nil || pub.X == nil || pub.Y == nil || !s256.IsOnCurve(pub.X, pub.Y) {
		return nil
	}
	return gethcrypto.CompressPubkey(pub)
}

func DecompressPubkey(pubkey []byte) (*ecdsa.PublicKey, error) {
	return gethcrypto.DecompressPubkey(pubkey)
}

func FromECDSAPub(pub *ecdsa.PublicKey) []byte {
	return gethcrypto.FromECDSAPub(pub)
}

func Secp256k1N() *big.Int { return new(big.Int).Set(secp256k1N) }

func Secp256k1HalfN() *big.Int { return new(big.Int).Set(secp256k1halfN) }
