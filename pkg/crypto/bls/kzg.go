package bls

import (
	"errors"
	"math/big"
)

// KZGDecompressG1 decodes a compressed commitment or proof point.
func KZGDecompressG1(data []byte) (*BlsG1Point, error) {
	if len(data) != KZGBytesPerCommitment {
		return nil, errors.New("kzg: invalid point length")
	}
	point := DeserializeG1([KZGBytesPerCommitment]byte(data))
	if point == nil {
		return nil, errors.New("kzg: invalid compressed G1 point")
	}
	return point, nil
}

// KZGCompressG1 encodes a commitment or proof point.
func KZGCompressG1(p *BlsG1Point) []byte {
	compressed := SerializeG1(p)
	return compressed[:]
}

// KZGVerifyFromBytes verifies a point opening against the Ethereum ceremony setup.
func KZGVerifyFromBytes(commitment []byte, z, y *big.Int, proof []byte) error {
	if len(commitment) != KZGBytesPerCommitment || len(proof) != KZGBytesPerProof {
		return errors.New("kzg: invalid commitment or proof length")
	}
	if z == nil || y == nil || z.Sign() < 0 || y.Sign() < 0 || z.Cmp(blsR) >= 0 || y.Cmp(blsR) >= 0 {
		return errors.New("kzg: invalid field element")
	}
	var point, claim [KZGBytesPerFieldElement]byte
	z.FillBytes(point[:])
	y.FillBytes(claim[:])
	return VerifyKZGPointProof([KZGBytesPerCommitment]byte(commitment), point, claim, [KZGBytesPerProof]byte(proof))
}
