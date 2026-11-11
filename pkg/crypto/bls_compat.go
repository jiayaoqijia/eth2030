package crypto

// bls_compat.go provides backward-compatible re-exports from crypto/bls.
// Existing code that imports "github.com/eth2030/eth2030/crypto" and uses
// BLS12-381 or KZG types continues to work unchanged. New code should import
// "github.com/eth2030/eth2030/crypto/bls" directly.

import (
	"math/big"

	"github.com/eth2030/eth2030/crypto/bls"
)

// BLS type aliases.
type (
	BlsG1Point         = bls.BlsG1Point
	BlsG2Point         = bls.BlsG2Point
	BLSBackend         = bls.BLSBackend
	PureGoBLSBackend   = bls.PureGoBLSBackend
	KZGCeremonyBackend = bls.KZGCeremonyBackend
)

// BLS constants.
const (
	BLSPubkeySize         = bls.BLSPubkeySize
	BLSSignatureSize      = bls.BLSSignatureSize
	KZGBytesPerBlob       = bls.KZGBytesPerBlob
	KZGBytesPerCell       = bls.KZGBytesPerCell
	KZGBytesPerCommitment = bls.KZGBytesPerCommitment
	KZGBytesPerProof      = bls.KZGBytesPerProof
)

// BLS function wrappers.

func DefaultBLSBackend() BLSBackend         { return bls.DefaultBLSBackend() }
func DefaultKZGBackend() KZGCeremonyBackend { return bls.DefaultKZGBackend() }

func SerializeG1(p *BlsG1Point) [BLSPubkeySize]byte         { return bls.SerializeG1(p) }
func DeserializeG1(data [BLSPubkeySize]byte) *BlsG1Point    { return bls.DeserializeG1(data) }
func SerializeG2(p *BlsG2Point) [BLSSignatureSize]byte      { return bls.SerializeG2(p) }
func DeserializeG2(data [BLSSignatureSize]byte) *BlsG2Point { return bls.DeserializeG2(data) }

func AggregatePublicKeys(pubkeys [][48]byte) [48]byte { return bls.AggregatePublicKeys(pubkeys) }
func AggregateSignatures(sigs [][96]byte) [96]byte    { return bls.AggregateSignatures(sigs) }
func FastAggregateVerify(pubkeys [][48]byte, msg []byte, sig [96]byte) bool {
	return bls.FastAggregateVerify(pubkeys, msg, sig)
}
func VerifyAggregate(pubkeys [][48]byte, msgs [][]byte, sig [96]byte) bool {
	return bls.VerifyAggregate(pubkeys, msgs, sig)
}
func SetBLSBackend(b BLSBackend) { bls.SetBLSBackend(b) }

func BlsG1Generator() *BlsG1Point                                { return bls.BlsG1Generator() }
func BlsG1Infinity() *BlsG1Point                                 { return bls.BlsG1Infinity() }
func BlsG2Generator() *BlsG2Point                                { return bls.BlsG2Generator() }
func BLSScalarOrder() *big.Int                                   { return bls.BLSScalarOrder() }
func BLSPubkeyFromSecret(secret *big.Int) [BLSPubkeySize]byte    { return bls.BLSPubkeyFromSecret(secret) }
func BLSSign(secret *big.Int, msg []byte) [BLSSignatureSize]byte { return bls.BLSSign(secret, msg) }
func BLSVerify(pubkey [BLSPubkeySize]byte, msg []byte, sig [BLSSignatureSize]byte) bool {
	return bls.BLSVerify(pubkey, msg, sig)
}

// KZG function wrappers.

func KZGCompressG1(p *BlsG1Point) []byte { return bls.KZGCompressG1(p) }
func KZGVerifyFromBytes(commitment []byte, z, y *big.Int, proof []byte) error {
	return bls.KZGVerifyFromBytes(commitment, z, y, proof)
}

// BLS12-381 precompile-compatible function wrappers.

func BLS12G1Add(input []byte) ([]byte, error)      { return bls.BLS12G1Add(input) }
func BLS12G1Mul(input []byte) ([]byte, error)      { return bls.BLS12G1Mul(input) }
func BLS12G1MSM(input []byte) ([]byte, error)      { return bls.BLS12G1MSM(input) }
func BLS12G2Add(input []byte) ([]byte, error)      { return bls.BLS12G2Add(input) }
func BLS12G2Mul(input []byte) ([]byte, error)      { return bls.BLS12G2Mul(input) }
func BLS12G2MSM(input []byte) ([]byte, error)      { return bls.BLS12G2MSM(input) }
func BLS12Pairing(input []byte) ([]byte, error)    { return bls.BLS12Pairing(input) }
func BLS12MapFpToG1(input []byte) ([]byte, error)  { return bls.BLS12MapFpToG1(input) }
func BLS12MapFp2ToG2(input []byte) ([]byte, error) { return bls.BLS12MapFp2ToG2(input) }
func HashToCurveG1(msg, dst []byte) (*BlsG1Point, error) {
	return bls.HashToCurveG1(msg, dst)
}
