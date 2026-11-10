package bls

import (
	"errors"

	bls12381gnark "github.com/consensys/gnark-crypto/ecc/bls12-381"
)

// HashToCurveG1 maps a message to BLS12-381 G1 using the RFC 9380 random oracle suite.
func HashToCurveG1(msg, dst []byte) (*BlsG1Point, error) {
	point, err := bls12381gnark.HashToG1(msg, dst)
	if err != nil {
		return nil, err
	}
	return blsG1FromGnark(point), nil
}

// EncodeToG1 maps a message to BLS12-381 G1 using the RFC 9380 non-uniform suite.
func EncodeToG1(msg, dst []byte) (*BlsG1Point, error) {
	point, err := bls12381gnark.EncodeToG1(msg, dst)
	if err != nil {
		return nil, err
	}
	return blsG1FromGnark(point), nil
}

var (
	// DSTHashToG1 is the standard DST for hashing to G1 in the BLS signature scheme.
	DSTHashToG1 = []byte("BLS_SIG_BLS12381G1_XMD:SHA-256_SSWU_RO_POP_")
)

// ValidateDST checks that a domain separation tag conforms to the spec
// requirements: non-empty and at most 255 bytes.
func ValidateDST(dst []byte) error {
	if len(dst) == 0 {
		return errors.New("hash_to_curve: empty DST")
	}
	if len(dst) > 255 {
		return errors.New("hash_to_curve: DST exceeds 255 bytes")
	}
	return nil
}
