package crypto

// secp256k1_compat.go provides backward-compatible re-exports from crypto/secp256k1.
// Existing code that imports "github.com/eth2030/eth2030/crypto" and uses
// secp256k1/ECIES/P256 functions continues to work unchanged. New code should
// import "github.com/eth2030/eth2030/crypto/secp256k1" directly.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"math/big"

	ctypes "github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/secp256k1"
)

// secp256k1N and secp256k1halfN mirror the unexported vars for legacy callers
// within this package (e.g. signature_recovery.go).
var (
	secp256k1N     = secp256k1.Secp256k1N()
	secp256k1halfN = secp256k1.Secp256k1HalfN()
)

// S256 returns the secp256k1 elliptic curve.
func S256() elliptic.Curve { return secp256k1.S256() }

// secp256k1 function wrappers.

func GenerateKey() (*ecdsa.PrivateKey, error) { return secp256k1.GenerateKey() }
func Sign(hash []byte, prv *ecdsa.PrivateKey) ([]byte, error) {
	return secp256k1.Sign(hash, prv)
}
func SigToPub(hash, sig []byte) (*ecdsa.PublicKey, error) { return secp256k1.SigToPub(hash, sig) }
func Ecrecover(hash, sig []byte) ([]byte, error)          { return secp256k1.Ecrecover(hash, sig) }
func ValidateSignature(pubkey, hash, sig []byte) bool {
	return secp256k1.ValidateSignature(pubkey, hash, sig)
}
func ValidateSignatureValues(v byte, r, s *big.Int, homestead bool) bool {
	return secp256k1.ValidateSignatureValues(v, r, s, homestead)
}
func FromECDSAPub(pub *ecdsa.PublicKey) []byte { return secp256k1.FromECDSAPub(pub) }
func DecompressPubkey(pubkey []byte) (*ecdsa.PublicKey, error) {
	return secp256k1.DecompressPubkey(pubkey)
}
func CompressPubkey(pubkey *ecdsa.PublicKey) []byte { return secp256k1.CompressPubkey(pubkey) }
func PubkeyToAddress(p ecdsa.PublicKey) ctypes.Address {
	return secp256k1.PubkeyToAddress(p)
}

// ECIES function wrappers.

func ECIESEncrypt(pub *ecdsa.PublicKey, plaintext []byte) ([]byte, error) {
	return secp256k1.ECIESEncrypt(pub, plaintext)
}
func ECIESDecrypt(prv *ecdsa.PrivateKey, data []byte) ([]byte, error) {
	return secp256k1.ECIESDecrypt(prv, data)
}
func GenerateSharedSecret(prv *ecdsa.PrivateKey, pub *ecdsa.PublicKey) ([]byte, error) {
	return secp256k1.GenerateSharedSecret(prv, pub)
}

// P256 function wrappers.

func P256GenerateKey() (*ecdsa.PrivateKey, error) { return secp256k1.P256GenerateKey() }
func P256Sign(hash []byte, prv *ecdsa.PrivateKey) ([]byte, error) {
	return secp256k1.P256Sign(hash, prv)
}
func P256Verify(hash []byte, r, s, x, y *big.Int) bool {
	return secp256k1.P256Verify(hash, r, s, x, y)
}
func P256ValidateSignatureValues(r, s *big.Int, lowS bool) bool {
	return secp256k1.P256ValidateSignatureValues(r, s, lowS)
}
func P256IsOnCurve(x, y *big.Int) bool { return secp256k1.P256IsOnCurve(x, y) }
func P256ScalarMult(px, py, k *big.Int) (x, y *big.Int) {
	return secp256k1.P256ScalarMult(px, py, k)
}
func P256ScalarBaseMult(k *big.Int) (x, y *big.Int) { return secp256k1.P256ScalarBaseMult(k) }
func P256PointAdd(x1, y1, x2, y2 *big.Int) (x, y *big.Int) {
	return secp256k1.P256PointAdd(x1, y1, x2, y2)
}
func P256UnmarshalPubkey(data []byte) (*ecdsa.PublicKey, error) {
	return secp256k1.P256UnmarshalPubkey(data)
}
func P256MarshalUncompressed(pub *ecdsa.PublicKey) ([]byte, error) {
	return secp256k1.P256MarshalUncompressed(pub)
}
func P256CompressPubkey(pub *ecdsa.PublicKey) ([]byte, error) {
	return secp256k1.P256CompressPubkey(pub)
}
func P256DecompressPubkey(compressed []byte) (*ecdsa.PublicKey, error) {
	return secp256k1.P256DecompressPubkey(compressed)
}
func P256SignDER(hash []byte, prv *ecdsa.PrivateKey) ([]byte, error) {
	return secp256k1.P256SignDER(hash, prv)
}
func P256VerifyDER(hash, derSig []byte, pub *ecdsa.PublicKey) bool {
	return secp256k1.P256VerifyDER(hash, derSig, pub)
}
func P256MarshalDER(r, s *big.Int) ([]byte, error) { return secp256k1.P256MarshalDER(r, s) }
func P256UnmarshalDER(der []byte) (r, s *big.Int, err error) {
	return secp256k1.P256UnmarshalDER(der)
}
func P256VerifyCompact(hash, sig []byte, pub *ecdsa.PublicKey) bool {
	return secp256k1.P256VerifyCompact(hash, sig, pub)
}
func P256RecoverPubkey(hash []byte, sig []byte, recID byte) (*ecdsa.PublicKey, error) {
	return secp256k1.P256RecoverPubkey(hash, sig, recID)
}
