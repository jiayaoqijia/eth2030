package bn254

// BN254 precompile interface functions.
//
// These functions provide the EVM precompile interface for BN254 (alt_bn128)
// elliptic curve operations as defined in EIP-196 and EIP-197.

import (
	"errors"
	"math/big"

	gethbn "github.com/ethereum/go-ethereum/crypto/bn256"
	"golang.org/x/crypto/sha3"
)

var (
	errBN254InvalidPoint  = errors.New("bn254: invalid point")
	errBN254InvalidG2     = errors.New("bn254: invalid G2 point")
	errBN254InvalidLength = errors.New("bn254: invalid input length")
)

// BN254Add performs EIP-196 point addition. Short inputs are right-padded.
func BN254Add(input []byte) ([]byte, error) {
	input = bn254PadRight(input, 128)
	var p, q gethbn.G1
	if _, err := p.Unmarshal(input[:64]); err != nil {
		return nil, errBN254InvalidPoint
	}
	if _, err := q.Unmarshal(input[64:]); err != nil {
		return nil, errBN254InvalidPoint
	}
	var result gethbn.G1
	result.Add(&p, &q)
	return result.Marshal(), nil
}

// BN254ScalarMul performs EIP-196 scalar multiplication.
func BN254ScalarMul(input []byte) ([]byte, error) {
	input = bn254PadRight(input, 96)
	var p gethbn.G1
	if _, err := p.Unmarshal(input[:64]); err != nil {
		return nil, errBN254InvalidPoint
	}
	var result gethbn.G1
	result.ScalarMult(&p, new(big.Int).SetBytes(input[64:]))
	return result.Marshal(), nil
}

// BN254PairingCheck performs the EIP-197 pairing check using Ethereum's
// coordinate encoding and subgroup validation.
func BN254PairingCheck(input []byte) ([]byte, error) {
	const pairSize = 192
	if len(input)%pairSize != 0 {
		return nil, errBN254InvalidLength
	}

	count := len(input) / pairSize
	g1 := make([]*gethbn.G1, count)
	g2 := make([]*gethbn.G2, count)
	for i := range g1 {
		pair := input[i*pairSize : (i+1)*pairSize]
		g1[i], g2[i] = new(gethbn.G1), new(gethbn.G2)
		if _, err := g1[i].Unmarshal(pair[:64]); err != nil {
			return nil, errBN254InvalidPoint
		}
		if _, err := g2[i].Unmarshal(pair[64:]); err != nil {
			return nil, errBN254InvalidG2
		}
	}
	return bn254PairingResult(count == 0 || gethbn.PairingCheck(g1, g2)), nil
}

// bn254EncodeG1 encodes a G1 point as 64 bytes (x, y) big-endian.
func bn254EncodeG1(x, y *big.Int) []byte {
	out := make([]byte, 64)
	xBytes := x.Bytes()
	yBytes := y.Bytes()
	copy(out[32-len(xBytes):32], xBytes)
	copy(out[64-len(yBytes):64], yBytes)
	return out
}

// bn254PairingResult encodes a pairing check result as 32 bytes.
func bn254PairingResult(ok bool) []byte {
	out := make([]byte, 32)
	if ok {
		out[31] = 1
	}
	return out
}

// bn254PadRight pads data with zeros on the right to reach minLen.
func bn254PadRight(data []byte, minLen int) []byte {
	if len(data) >= minLen {
		return data[:minLen]
	}
	padded := make([]byte, minLen)
	copy(padded, data)
	return padded
}

// keccak256 computes the Keccak-256 hash of the input data.
func keccak256(data ...[]byte) []byte {
	d := sha3.NewLegacyKeccak256()
	for _, b := range data {
		d.Write(b)
	}
	return d.Sum(nil)
}

// keccak256Hash computes Keccak-256 and returns a [32]byte.
func keccak256Hash(data ...[]byte) [32]byte {
	h := keccak256(data...)
	var out [32]byte
	copy(out[:], h)
	return out
}
