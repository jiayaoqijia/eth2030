package bls

import "math/big"

// This mapping is retained only for historical native precompile replay.
// LegacyBLS12MapFp2ToG2 maps an Fp2 element to G2 (precompile 0x13).
func LegacyBLS12MapFp2ToG2(input []byte) ([]byte, error) {
	if len(input) != 2*blsFpEncSize {
		return nil, errBLS12InvalidField
	}

	u, err := decodeFp2(input)
	if err != nil {
		return nil, err
	}

	p := legacyMapFp2ToG2(u)

	// Clear cofactor for G2.
	// h2 = 0x5d543a95414e7f1091d50792876a202cd91de4547085abaa68a205b2e5a7ddfa628f1cb4d9e82ef21537e293a6691ae1616ec6e786f0c70cf1c38e31c7238e5
	cofactor, _ := new(big.Int).SetString(
		"5d543a95414e7f1091d50792876a202cd91de4547085abaa68a205b2e5a7ddfa628f1cb4d9e82ef21537e293a6691ae1616ec6e786f0c70cf1c38e31c7238e5", 16)
	p = blsG2ScalarMul(p, cofactor)

	return encodeG2(p), nil
}

func legacyMapFp2ToG2(u *blsFp2) *BlsG2Point {
	x := newBlsFp2(u.c0, u.c1)

	for i := 0; i < 256; i++ {
		// Compute y^2 = x^3 + 4(1+u)
		x3 := blsFp2Mul(blsFp2Sqr(x), x)
		rhs := blsFp2Add(x3, blsTwistB)

		y := blsFp2Sqrt(rhs)
		if y != nil {
			// Verify: y^2 == rhs
			if blsFp2Sqr(y).equal(rhs) {
				// Choose sign based on input.
				if blsFp2Sgn0(u) != blsFp2Sgn0(y) {
					y = blsFp2Neg(y)
				}
				return blsG2FromAffine(x, y)
			}
		}

		// Try next x by incrementing the real part.
		x = blsFp2Add(x, blsFp2One())
	}

	return BlsG2Infinity()
}

// LegacyBLS12MapFpToG1 maps a field element to G1 (precompile 0x12).
func LegacyBLS12MapFpToG1(input []byte) ([]byte, error) {
	if len(input) != blsFpEncSize {
		return nil, errBLS12InvalidField
	}

	u, err := decodeFp(input)
	if err != nil {
		return nil, err
	}

	p := legacyMapFpToG1(u)

	// Clear cofactor to ensure the result is in the prime-order subgroup.
	// For G1, the cofactor is h = (x-1)^2 / 3 where x is the BLS parameter.
	// h = 0x396c8c005555e1568c00aaab0000aaab
	cofactor, _ := new(big.Int).SetString("396c8c005555e1568c00aaab0000aaab", 16)
	p = blsG1ScalarMul(p, cofactor)

	return encodeG1(p), nil
}

func legacyMapFpToG1(u *big.Int) *BlsG1Point {
	// Use the "try-and-increment" method to find a curve point.
	// Start with x = u, then x = u+1, u+2, ... until we find a valid point.
	// This is not constant-time but is correct.
	x := new(big.Int).Mod(u, blsP)

	for i := 0; i < 256; i++ {
		// Compute y^2 = x^3 + 4
		x3 := blsFpMul(blsFpSqr(x), x)
		rhs := blsFpAdd(x3, blsB)

		y := blsFpSqrt(rhs)
		if y != nil {
			// Choose the y with the same "sign" as u.
			if blsFpSgn0(u) != blsFpSgn0(y) {
				y = blsFpNeg(y)
			}
			return blsG1FromAffine(x, y)
		}

		// Try next x.
		x = blsFpAdd(x, big.NewInt(1))
	}

	// Extremely unlikely to reach here. Return infinity as fallback.
	return BlsG1Infinity()
}
