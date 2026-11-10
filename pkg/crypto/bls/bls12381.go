package bls

// BLS12-381 precompile interface functions.
//
// These functions provide the EVM precompile interface for BLS12-381
// elliptic curve operations as defined in EIP-2537.

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	bls12381gnark "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
)

var (
	errBLS12InvalidPoint  = errors.New("bls12-381: invalid point")
	errBLS12InvalidG2     = errors.New("bls12-381: invalid G2 point")
	errBLS12NotOnCurve    = errors.New("bls12-381: point not on curve")
	errBLS12NotInSubgroup = errors.New("bls12-381: point not in subgroup")
	errBLS12InvalidField  = errors.New("bls12-381: invalid field element")
)

// BLS12-381 precompile encoding sizes.
const (
	blsFpEncSize  = 64  // field element padded to 64 bytes
	blsG1EncSize  = 128 // G1 point: 2 * 64 bytes
	blsG2EncSize  = 256 // G2 point: 2 * 128 bytes
	blsScalarSize = 32  // Fr scalar
)

// decodeFp reads a 64-byte padded field element. The top 16 bytes must be zero,
// and the value must be less than p.
func decodeFp(data []byte) (*big.Int, error) {
	if len(data) != blsFpEncSize {
		return nil, errBLS12InvalidField
	}
	// Top 16 bytes must be zero.
	for i := 0; i < 16; i++ {
		if data[i] != 0 {
			return nil, errBLS12InvalidField
		}
	}
	v := new(big.Int).SetBytes(data)
	if v.Cmp(blsP) >= 0 {
		return nil, errBLS12InvalidField
	}
	return v, nil
}

// encodeFp writes a field element as 64 bytes (big-endian, zero-padded).
func encodeFp(v *big.Int) []byte {
	out := make([]byte, blsFpEncSize)
	b := v.Bytes()
	copy(out[blsFpEncSize-len(b):], b)
	return out
}

// decodeG1 reads a 128-byte encoded G1 point.
// All zeros = point at infinity. Otherwise validates on-curve and subgroup.
func decodeG1(data []byte) (*BlsG1Point, error) {
	return decodeG1Point(data, true)
}

func decodeG1Point(data []byte, checkSubgroup bool) (*BlsG1Point, error) {
	if len(data) != blsG1EncSize {
		return nil, errBLS12InvalidPoint
	}

	x, err := decodeFp(data[:blsFpEncSize])
	if err != nil {
		return nil, errBLS12InvalidPoint
	}
	y, err := decodeFp(data[blsFpEncSize:])
	if err != nil {
		return nil, errBLS12InvalidPoint
	}

	// Point at infinity.
	if x.Sign() == 0 && y.Sign() == 0 {
		return BlsG1Infinity(), nil
	}

	// Check on curve.
	if !blsG1IsOnCurve(x, y) {
		return nil, errBLS12NotOnCurve
	}

	p := blsG1FromAffine(x, y)

	// Subgroup check.
	if checkSubgroup && !blsG1InSubgroup(p) {
		return nil, errBLS12NotInSubgroup
	}

	return p, nil
}

// encodeG1 writes a G1 point as 128 bytes.
func encodeG1(p *BlsG1Point) []byte {
	out := make([]byte, blsG1EncSize)
	if p.blsG1IsInfinity() {
		return out
	}
	x, y := p.blsG1ToAffine()
	copy(out[:blsFpEncSize], encodeFp(x))
	copy(out[blsFpEncSize:], encodeFp(y))
	return out
}

// decodeFp2 reads a 128-byte encoded Fp2 element (imaginary part first, then real).
func decodeFp2(data []byte) (*blsFp2, error) {
	if len(data) != 2*blsFpEncSize {
		return nil, errBLS12InvalidField
	}
	// The historical native ABI uses im || re. Standard EIP-2537 calls
	// use the Geth precompile and its re || im encoding.
	im, err := decodeFp(data[:blsFpEncSize])
	if err != nil {
		return nil, err
	}
	re, err := decodeFp(data[blsFpEncSize:])
	if err != nil {
		return nil, err
	}
	return &blsFp2{c0: re, c1: im}, nil
}

// encodeFp2 writes an Fp2 element as 128 bytes (imaginary part first, then real).
func encodeFp2(e *blsFp2) []byte {
	out := make([]byte, 2*blsFpEncSize)
	copy(out[:blsFpEncSize], encodeFp(e.c1))               // imaginary
	copy(out[blsFpEncSize:2*blsFpEncSize], encodeFp(e.c0)) // real
	return out
}

// decodeG2 reads a 256-byte encoded G2 point.
func decodeG2(data []byte) (*BlsG2Point, error) {
	return decodeG2Point(data, true)
}

func decodeG2Point(data []byte, checkSubgroup bool) (*BlsG2Point, error) {
	if len(data) != blsG2EncSize {
		return nil, errBLS12InvalidG2
	}

	x, err := decodeFp2(data[:2*blsFpEncSize])
	if err != nil {
		return nil, errBLS12InvalidG2
	}
	y, err := decodeFp2(data[2*blsFpEncSize:])
	if err != nil {
		return nil, errBLS12InvalidG2
	}

	// Point at infinity.
	if x.isZero() && y.isZero() {
		return BlsG2Infinity(), nil
	}

	// Check on curve.
	if !blsG2IsOnCurve(x, y) {
		return nil, errBLS12NotOnCurve
	}

	p := blsG2FromAffine(x, y)

	// Subgroup check.
	if checkSubgroup && !blsG2InSubgroup(p) {
		return nil, errBLS12NotInSubgroup
	}

	return p, nil
}

// encodeG2 writes a G2 point as 256 bytes.
func encodeG2(p *BlsG2Point) []byte {
	out := make([]byte, blsG2EncSize)
	if p.blsG2IsInfinity() {
		return out
	}
	x, y := p.blsG2ToAffine()
	copy(out[:2*blsFpEncSize], encodeFp2(x))
	copy(out[2*blsFpEncSize:], encodeFp2(y))
	return out
}

// --- Precompile entry points ---

// BLS12G1Add performs G1 point addition (precompile 0x0b).
func BLS12G1Add(input []byte) ([]byte, error) {
	return addG1(input, false)
}

// LegacyBLS12G1Add applies the historical native subgroup check.
func LegacyBLS12G1Add(input []byte) ([]byte, error) {
	return addG1(input, true)
}

func addG1(input []byte, subgroup bool) ([]byte, error) {
	if len(input) != 2*blsG1EncSize {
		return nil, errBLS12InvalidPoint
	}

	p1, err := decodeG1Point(input[:blsG1EncSize], subgroup)
	if err != nil {
		return nil, err
	}
	p2, err := decodeG1Point(input[blsG1EncSize:], subgroup)
	if err != nil {
		return nil, err
	}

	r := blsG1Add(p1, p2)
	return encodeG1(r), nil
}

// BLS12G1Mul performs G1 scalar multiplication (precompile 0x0c).
func BLS12G1Mul(input []byte) ([]byte, error) {
	if len(input) != blsG1EncSize+blsScalarSize {
		return nil, errBLS12InvalidPoint
	}

	p, err := decodeG1(input[:blsG1EncSize])
	if err != nil {
		return nil, err
	}

	scalar := new(big.Int).SetBytes(input[blsG1EncSize:])
	r := blsG1ScalarMul(p, scalar)
	return encodeG1(r), nil
}

// BLS12G1MSM performs G1 multi-scalar multiplication (precompile 0x0d).
// Uses gnark-crypto MultiExp (Pippenger) for fast batched MSM.
func BLS12G1MSM(input []byte) ([]byte, error) {
	pairSize := blsG1EncSize + blsScalarSize
	if len(input) == 0 || len(input)%pairSize != 0 {
		return nil, errBLS12InvalidPoint
	}

	k := len(input) / pairSize
	points := make([]bls12381gnark.G1Affine, 0, k)
	scalars := make([]fr.Element, 0, k)

	for i := 0; i < k; i++ {
		offset := i * pairSize
		p, err := decodeG1(input[offset : offset+blsG1EncSize])
		if err != nil {
			return nil, err
		}
		scalarBig := new(big.Int).SetBytes(input[offset+blsG1EncSize : offset+pairSize])

		// Skip identity contributions.
		if scalarBig.Sign() == 0 || p.blsG1IsInfinity() {
			continue
		}

		ax, ay := p.blsG1ToAffine()
		var ga bls12381gnark.G1Affine
		ga.X.SetBigInt(ax)
		ga.Y.SetBigInt(ay)

		var s fr.Element
		s.SetBigInt(scalarBig)

		points = append(points, ga)
		scalars = append(scalars, s)
	}

	if len(points) == 0 {
		return encodeG1(BlsG1Infinity()), nil
	}

	var result bls12381gnark.G1Affine
	if _, err := result.MultiExp(points, scalars, ecc.MultiExpConfig{}); err != nil {
		return nil, fmt.Errorf("bls12-381: MSM failed: %w", err)
	}

	if result.IsInfinity() {
		return encodeG1(BlsG1Infinity()), nil
	}

	rx := result.X.BigInt(new(big.Int))
	ry := result.Y.BigInt(new(big.Int))
	return encodeG1(blsG1FromAffine(rx, ry)), nil
}

// BLS12G2Add performs G2 point addition (precompile 0x0e).
func BLS12G2Add(input []byte) ([]byte, error) {
	return addG2(input, false)
}

// LegacyBLS12G2Add applies the historical native subgroup check.
func LegacyBLS12G2Add(input []byte) ([]byte, error) {
	return addG2(input, true)
}

func addG2(input []byte, subgroup bool) ([]byte, error) {
	if len(input) != 2*blsG2EncSize {
		return nil, errBLS12InvalidG2
	}

	p1, err := decodeG2Point(input[:blsG2EncSize], subgroup)
	if err != nil {
		return nil, err
	}
	p2, err := decodeG2Point(input[blsG2EncSize:], subgroup)
	if err != nil {
		return nil, err
	}

	r := blsG2Add(p1, p2)
	return encodeG2(r), nil
}

// BLS12G2Mul performs G2 scalar multiplication (precompile 0x0f).
func BLS12G2Mul(input []byte) ([]byte, error) {
	if len(input) != blsG2EncSize+blsScalarSize {
		return nil, errBLS12InvalidG2
	}

	p, err := decodeG2(input[:blsG2EncSize])
	if err != nil {
		return nil, err
	}

	scalar := new(big.Int).SetBytes(input[blsG2EncSize:])
	r := blsG2ScalarMul(p, scalar)
	return encodeG2(r), nil
}

// BLS12G2MSM performs G2 multi-scalar multiplication (precompile 0x10).
func BLS12G2MSM(input []byte) ([]byte, error) {
	pairSize := blsG2EncSize + blsScalarSize
	if len(input) == 0 || len(input)%pairSize != 0 {
		return nil, errBLS12InvalidG2
	}

	k := len(input) / pairSize
	r := BlsG2Infinity()

	for i := 0; i < k; i++ {
		offset := i * pairSize
		p, err := decodeG2(input[offset : offset+blsG2EncSize])
		if err != nil {
			return nil, err
		}
		scalar := new(big.Int).SetBytes(input[offset+blsG2EncSize : offset+pairSize])
		sp := blsG2ScalarMul(p, scalar)
		r = blsG2Add(r, sp)
	}

	return encodeG2(r), nil
}

// BLS12Pairing checks the pairing product for EVM-encoded G1/G2 pairs.
func BLS12Pairing(input []byte) ([]byte, error) {
	pairSize := blsG1EncSize + blsG2EncSize
	if len(input) == 0 || len(input)%pairSize != 0 {
		return nil, errBLS12InvalidPoint
	}

	k := len(input) / pairSize

	// Decode and validate all points.
	g1Points := make([]*BlsG1Point, k)
	g2Points := make([]*BlsG2Point, k)

	allG1Inf := true
	allG2Inf := true

	for i := 0; i < k; i++ {
		offset := i * pairSize
		var err error
		g1Points[i], err = decodeG1(input[offset : offset+blsG1EncSize])
		if err != nil {
			return nil, err
		}
		g2Points[i], err = decodeG2(input[offset+blsG1EncSize : offset+pairSize])
		if err != nil {
			return nil, err
		}
		if !g1Points[i].blsG1IsInfinity() {
			allG1Inf = false
		}
		if !g2Points[i].blsG2IsInfinity() {
			allG2Inf = false
		}
	}

	// If all G1 or all G2 points are infinity, pairing is trivially true.
	if allG1Inf || allG2Inf {
		return blsPairingResult(true), nil
	}

	return blsPairingResult(blsMultiPairing(g1Points, g2Points)), nil
}

// BLS12MapFpToG1 maps a field element to G1 (precompile 0x12).
func BLS12MapFpToG1(input []byte) ([]byte, error) {
	if len(input) != blsFpEncSize {
		return nil, errBLS12InvalidField
	}

	u, err := decodeFp(input)
	if err != nil {
		return nil, err
	}

	return encodeG1(blsMapFpToG1(u)), nil
}

// BLS12MapFp2ToG2 maps an Fp2 element to G2 (precompile 0x13).
func BLS12MapFp2ToG2(input []byte) ([]byte, error) {
	if len(input) != 2*blsFpEncSize {
		return nil, errBLS12InvalidField
	}

	u, err := decodeFp2(input)
	if err != nil {
		return nil, err
	}

	return encodeG2(blsMapFp2ToG2(u)), nil
}

// blsPairingResult encodes a pairing result as 32 bytes.
func blsPairingResult(ok bool) []byte {
	out := make([]byte, 32)
	if ok {
		out[31] = 1
	}
	return out
}
