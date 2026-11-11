package bls

// Comprehensive tests for BLS12-381 field arithmetic (Fp, Fp2, Fp6, Fp12),
// G1/G2 point operations, map-to-curve, and pairing internals.
//
// These tests focus on algebraic properties, serialization round-trips,
// and edge cases not covered by the existing test suites.

import (
	"math/big"
	"testing"
)

// ---------------------------------------------------------------------------
// Fp arithmetic
// ---------------------------------------------------------------------------

func TestBlsFpExpIdentity(t *testing.T) {
	a := big.NewInt(7)

	// a^0 = 1
	r := blsFpExp(a, big.NewInt(0))
	if r.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("blsFpExp(7, 0) = %s, want 1", r)
	}

	// a^1 = a
	r = blsFpExp(a, big.NewInt(1))
	if r.Cmp(a) != 0 {
		t.Errorf("blsFpExp(7, 1) = %s, want 7", r)
	}
}

func TestBlsFpExpFermat(t *testing.T) {
	// Fermat's little theorem: a^(p-1) = 1 mod p for non-zero a.
	a := big.NewInt(42)
	pMinus1 := new(big.Int).Sub(blsP, big.NewInt(1))
	r := blsFpExp(a, pMinus1)
	if r.Cmp(big.NewInt(1)) != 0 {
		t.Errorf("42^(p-1) mod p = %s, want 1", r)
	}
}

func TestBlsFpExpConsistency(t *testing.T) {
	a := big.NewInt(3)

	// a^2 via exp should match sqr.
	expResult := blsFpExp(a, big.NewInt(2))
	sqrResult := blsFpSqr(a)
	if expResult.Cmp(sqrResult) != 0 {
		t.Errorf("blsFpExp(3, 2) = %s, blsFpSqr(3) = %s", expResult, sqrResult)
	}

	// a^3 via exp should match mul(sqr(a), a).
	expResult = blsFpExp(a, big.NewInt(3))
	mulResult := blsFpMul(sqrResult, a)
	if expResult.Cmp(mulResult) != 0 {
		t.Errorf("blsFpExp(3, 3) = %s, want %s", expResult, mulResult)
	}
}

func TestBlsFpInvSelf(t *testing.T) {
	// inv(inv(a)) = a
	a := big.NewInt(13)
	inv1 := blsFpInv(a)
	inv2 := blsFpInv(inv1)
	if inv2.Cmp(a) != 0 {
		t.Errorf("inv(inv(13)) = %s, want 13", inv2)
	}
}

func TestBlsFpSqrtNonResidue(t *testing.T) {
	// Find a non-quadratic residue and verify sqrt returns nil.
	// 3 is a known non-residue for BLS12-381.
	if blsFpIsSquare(big.NewInt(3)) {
		// If 3 happens to be a QR, test with something else.
		t.Skip("3 is a QR in this field, skipping")
	}
	r := blsFpSqrt(big.NewInt(3))
	if r != nil {
		t.Errorf("blsFpSqrt(3) should be nil for non-residue, got %s", r)
	}
}

func TestBlsFpArithmeticAssociativity(t *testing.T) {
	a := big.NewInt(100)
	b := big.NewInt(200)
	c := big.NewInt(300)

	// (a + b) + c == a + (b + c)
	lhs := blsFpAdd(blsFpAdd(a, b), c)
	rhs := blsFpAdd(a, blsFpAdd(b, c))
	if lhs.Cmp(rhs) != 0 {
		t.Error("Fp addition is not associative")
	}

	// (a * b) * c == a * (b * c)
	lhs = blsFpMul(blsFpMul(a, b), c)
	rhs = blsFpMul(a, blsFpMul(b, c))
	if lhs.Cmp(rhs) != 0 {
		t.Error("Fp multiplication is not associative")
	}
}

func TestBlsFpArithmeticDistributive(t *testing.T) {
	a := big.NewInt(7)
	b := big.NewInt(11)
	c := big.NewInt(13)

	// a * (b + c) == a*b + a*c
	lhs := blsFpMul(a, blsFpAdd(b, c))
	rhs := blsFpAdd(blsFpMul(a, b), blsFpMul(a, c))
	if lhs.Cmp(rhs) != 0 {
		t.Error("Fp is not distributive")
	}
}

func TestBlsFpNegProperties(t *testing.T) {
	a := big.NewInt(99)

	// a + (-a) = 0
	sum := blsFpAdd(a, blsFpNeg(a))
	if sum.Sign() != 0 {
		t.Errorf("a + (-a) = %s, want 0", sum)
	}

	// -(-a) = a
	doubleNeg := blsFpNeg(blsFpNeg(a))
	if doubleNeg.Cmp(a) != 0 {
		t.Errorf("-(-a) = %s, want %s", doubleNeg, a)
	}
}

func TestBlsFpSqrMatchesMul(t *testing.T) {
	// For a large element, sqr and mul should agree.
	a, _ := new(big.Int).SetString("123456789012345678901234567890", 10)
	sqr := blsFpSqr(a)
	mul := blsFpMul(a, a)
	if sqr.Cmp(mul) != 0 {
		t.Error("blsFpSqr does not match blsFpMul(a, a)")
	}
}

// ---------------------------------------------------------------------------
// Fp2 arithmetic
// ---------------------------------------------------------------------------

func TestBlsFp2MulScalarProperties(t *testing.T) {
	a := &blsFp2{c0: big.NewInt(5), c1: big.NewInt(7)}

	// Multiply by 1 gives identity.
	r := blsFp2MulScalar(a, big.NewInt(1))
	if !r.equal(a) {
		t.Error("Fp2 * 1 should be identity")
	}

	// Multiply by 0 gives zero.
	r = blsFp2MulScalar(a, big.NewInt(0))
	if !r.isZero() {
		t.Error("Fp2 * 0 should be zero")
	}

	// Multiply by scalar s: result should equal repeated addition.
	s := big.NewInt(3)
	r = blsFp2MulScalar(a, s)
	expected := blsFp2Add(blsFp2Add(a, a), a)
	if !r.equal(expected) {
		t.Error("Fp2 * 3 does not match 3x add")
	}
}

func TestBlsFp2Sgn0Properties(t *testing.T) {
	// sgn0(0) = 0
	if blsFp2Sgn0(blsFp2Zero()) != 0 {
		t.Error("sgn0(Fp2.zero) should be 0")
	}

	// sgn0(1 + 0*u) = 1 (since 1 is odd)
	if blsFp2Sgn0(blsFp2One()) != 1 {
		t.Error("sgn0(Fp2.one) should be 1")
	}

	// sgn0(0 + 1*u): c0 = 0 so we look at c1 = 1 which is odd, so sgn0 = 1.
	u := &blsFp2{c0: new(big.Int), c1: big.NewInt(1)}
	if blsFp2Sgn0(u) != 1 {
		t.Error("sgn0(0 + 1*u) should be 1")
	}

	// sgn0(2 + 0*u): c0 = 2, even, so sgn0 = 0.
	even := &blsFp2{c0: big.NewInt(2), c1: new(big.Int)}
	if blsFp2Sgn0(even) != 0 {
		t.Error("sgn0(2 + 0*u) should be 0")
	}
}

func TestBlsFp2InvSelf(t *testing.T) {
	a := &blsFp2{c0: big.NewInt(17), c1: big.NewInt(23)}

	// inv(inv(a)) = a
	inv1 := blsFp2Inv(a)
	inv2 := blsFp2Inv(inv1)
	if !inv2.equal(a) {
		t.Error("inv(inv(a)) should equal a in Fp2")
	}
}

func TestBlsFp2ConjMulIsNorm(t *testing.T) {
	// a * conj(a) = norm(a) = c0^2 + c1^2 (real-valued).
	a := &blsFp2{c0: big.NewInt(11), c1: big.NewInt(13)}
	conj := blsFp2Conj(a)
	prod := blsFp2Mul(a, conj)
	if prod.c1.Sign() != 0 {
		t.Error("a * conj(a) should be real in Fp2")
	}
	expectedNorm := blsFpAdd(blsFpSqr(big.NewInt(11)), blsFpSqr(big.NewInt(13)))
	if prod.c0.Cmp(expectedNorm) != 0 {
		t.Errorf("a * conj(a) = %s, want %s", prod.c0, expectedNorm)
	}
}

func TestBlsFp2SqrtRoundTrip(t *testing.T) {
	// Pick a value, square it, then take sqrt and verify.
	a := &blsFp2{c0: big.NewInt(7), c1: big.NewInt(11)}
	aSq := blsFp2Sqr(a)

	r := blsFp2Sqrt(aSq)
	if r == nil {
		t.Fatal("blsFp2Sqrt(a^2) returned nil")
	}
	// r^2 should equal a^2.
	rSq := blsFp2Sqr(r)
	if !rSq.equal(aSq) {
		t.Error("sqrt(a^2)^2 does not equal a^2")
	}
}

// ---------------------------------------------------------------------------
// G1 point operations
// ---------------------------------------------------------------------------

func TestBlsG1AddAssociativity(t *testing.T) {
	gen := BlsG1Generator()
	twoG := blsG1Double(gen)
	threeG := blsG1Add(gen, twoG)

	// (G + 2G) + 3G == G + (2G + 3G)
	lhs := blsG1Add(blsG1Add(gen, twoG), threeG)
	rhs := blsG1Add(gen, blsG1Add(twoG, threeG))

	lx, ly := lhs.blsG1ToAffine()
	rx, ry := rhs.blsG1ToAffine()
	if lx.Cmp(rx) != 0 || ly.Cmp(ry) != 0 {
		t.Error("G1 addition is not associative")
	}
}

func TestBlsG1AddCommutativity(t *testing.T) {
	gen := BlsG1Generator()
	twoG := blsG1Double(gen)

	ab := blsG1Add(gen, twoG)
	ba := blsG1Add(twoG, gen)

	abx, aby := ab.blsG1ToAffine()
	bax, bay := ba.blsG1ToAffine()
	if abx.Cmp(bax) != 0 || aby.Cmp(bay) != 0 {
		t.Error("G1 addition is not commutative")
	}
}

func TestBlsG1ScalarMulConsistency(t *testing.T) {
	gen := BlsG1Generator()

	// 3*G == G + G + G
	threeG := blsG1ScalarMul(gen, big.NewInt(3))
	added := blsG1Add(blsG1Add(gen, gen), gen)

	tx, ty := threeG.blsG1ToAffine()
	ax, ay := added.blsG1ToAffine()
	if tx.Cmp(ax) != 0 || ty.Cmp(ay) != 0 {
		t.Error("3*G via scalar mul does not match G+G+G")
	}
}

func TestBlsG1ScalarMulLargeScalar(t *testing.T) {
	gen := BlsG1Generator()

	// (r+1)*G should equal G (since r*G = O, so (r+1)*G = G).
	rPlusOne := new(big.Int).Add(blsR, big.NewInt(1))
	r := blsG1ScalarMul(gen, rPlusOne)
	rx, ry := r.blsG1ToAffine()
	gx, gy := gen.blsG1ToAffine()
	if rx.Cmp(gx) != 0 || ry.Cmp(gy) != 0 {
		t.Error("(r+1)*G should equal G")
	}
}

func TestBlsG1AffineRoundTrip(t *testing.T) {
	gen := BlsG1Generator()
	fiveG := blsG1ScalarMul(gen, big.NewInt(5))

	// Convert to affine and back.
	ax, ay := fiveG.blsG1ToAffine()
	if !blsG1IsOnCurve(ax, ay) {
		t.Fatal("5*G is not on the curve")
	}

	reconstructed := blsG1FromAffine(ax, ay)
	rx, ry := reconstructed.blsG1ToAffine()
	if ax.Cmp(rx) != 0 || ay.Cmp(ry) != 0 {
		t.Error("affine round-trip failed for 5*G")
	}
}

func TestBlsG1InfinityAffineRoundTrip(t *testing.T) {
	inf := BlsG1Infinity()
	x, y := inf.blsG1ToAffine()
	if x.Sign() != 0 || y.Sign() != 0 {
		t.Error("infinity affine should be (0, 0)")
	}

	reconstructed := blsG1FromAffine(x, y)
	if !reconstructed.blsG1IsInfinity() {
		t.Error("reconstructed from (0,0) should be infinity")
	}
}

// ---------------------------------------------------------------------------
// G2 point operations
// ---------------------------------------------------------------------------

func TestBlsG2AddAssociativity(t *testing.T) {
	gen := BlsG2Generator()
	twoG := blsG2Double(gen)
	threeG := blsG2Add(gen, twoG)

	lhs := blsG2Add(blsG2Add(gen, twoG), threeG)
	rhs := blsG2Add(gen, blsG2Add(twoG, threeG))

	lx, ly := lhs.blsG2ToAffine()
	rx, ry := rhs.blsG2ToAffine()
	if !lx.equal(rx) || !ly.equal(ry) {
		t.Error("G2 addition is not associative")
	}
}

func TestBlsG2AddCommutativity(t *testing.T) {
	gen := BlsG2Generator()
	twoG := blsG2Double(gen)

	ab := blsG2Add(gen, twoG)
	ba := blsG2Add(twoG, gen)

	abx, aby := ab.blsG2ToAffine()
	bax, bay := ba.blsG2ToAffine()
	if !abx.equal(bax) || !aby.equal(bay) {
		t.Error("G2 addition is not commutative")
	}
}

func TestBlsG2ScalarMulConsistency(t *testing.T) {
	gen := BlsG2Generator()

	// 3*G2 == G2 + G2 + G2
	threeG := blsG2ScalarMul(gen, big.NewInt(3))
	added := blsG2Add(blsG2Add(gen, gen), gen)

	tx, ty := threeG.blsG2ToAffine()
	ax, ay := added.blsG2ToAffine()
	if !tx.equal(ax) || !ty.equal(ay) {
		t.Error("3*G2 via scalar mul does not match G2+G2+G2")
	}
}

func TestBlsG2AffineRoundTrip(t *testing.T) {
	gen := BlsG2Generator()
	fiveG := blsG2ScalarMul(gen, big.NewInt(5))

	ax, ay := fiveG.blsG2ToAffine()
	if !blsG2IsOnCurve(ax, ay) {
		t.Fatal("5*G2 is not on the curve")
	}

	reconstructed := blsG2FromAffine(ax, ay)
	rx, ry := reconstructed.blsG2ToAffine()
	if !ax.equal(rx) || !ay.equal(ry) {
		t.Error("affine round-trip failed for 5*G2")
	}
}

func TestBlsG2InfinityAffineRoundTrip(t *testing.T) {
	inf := BlsG2Infinity()
	x, y := inf.blsG2ToAffine()
	if !x.isZero() || !y.isZero() {
		t.Error("G2 infinity affine should be (0, 0)")
	}

	reconstructed := blsG2FromAffine(x, y)
	if !reconstructed.blsG2IsInfinity() {
		t.Error("G2 reconstructed from (0,0) should be infinity")
	}
}

func TestBlsG2IsOnCurveInvalid(t *testing.T) {
	// A random point not on the curve.
	x := &blsFp2{c0: big.NewInt(1), c1: big.NewInt(2)}
	y := &blsFp2{c0: big.NewInt(3), c1: big.NewInt(4)}
	if blsG2IsOnCurve(x, y) {
		t.Error("random point should not be on G2 curve")
	}
}

// ---------------------------------------------------------------------------
// Map-to-curve
// ---------------------------------------------------------------------------

func TestBlsMapFpToG1ProducesOnCurvePoints(t *testing.T) {
	// Test several field elements.
	for i := int64(0); i < 10; i++ {
		u := big.NewInt(i * 1000)
		p := blsMapFpToG1(u)
		if p.blsG1IsInfinity() {
			continue // infinity is valid
		}
		x, y := p.blsG1ToAffine()
		if !blsG1IsOnCurve(x, y) {
			t.Errorf("blsMapFpToG1(%d) produced off-curve point", i*1000)
		}
	}
}

func TestBlsMapFp2ToG2ProducesOnCurvePoints(t *testing.T) {
	for i := int64(0); i < 5; i++ {
		u := &blsFp2{c0: big.NewInt(i * 100), c1: big.NewInt(i*100 + 1)}
		p := blsMapFp2ToG2(u)
		if p.blsG2IsInfinity() {
			continue
		}
		x, y := p.blsG2ToAffine()
		if !blsG2IsOnCurve(x, y) {
			t.Errorf("blsMapFp2ToG2 produced off-curve point for i=%d", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Fp6 arithmetic
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Fp12 arithmetic
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Pairing internals
// ---------------------------------------------------------------------------

func TestBlsPairingNonDegeneracy(t *testing.T) {
	// e(G1, G2) should not be 1.
	g1 := BlsG1Generator()
	g2 := BlsG2Generator()

	result := blsMultiPairing([]*BlsG1Point{g1}, []*BlsG2Point{g2})
	if result {
		t.Error("e(G1, G2) should not be the identity")
	}
}

func TestBlsPairingBilinearityScalarSwap(t *testing.T) {
	// e(a*G1, b*G2) * e(-b*G1, a*G2) should equal 1.
	a := big.NewInt(2)
	b := big.NewInt(3)

	aG1 := blsG1ScalarMul(BlsG1Generator(), a)
	bG2 := blsG2ScalarMul(BlsG2Generator(), b)
	negBG1 := blsG1Neg(blsG1ScalarMul(BlsG1Generator(), b))
	aG2 := blsG2ScalarMul(BlsG2Generator(), a)

	result := blsMultiPairing(
		[]*BlsG1Point{aG1, negBG1},
		[]*BlsG2Point{bG2, aG2},
	)
	if !result {
		t.Error("e(aG1, bG2) * e(-bG1, aG2) should be 1")
	}
}

func TestBlsPairingLinearity(t *testing.T) {
	// e(a*G1, G2) * e(b*G1, G2) should equal e((a+b)*G1, G2) when
	// the second form is checked via the multi-pairing trick:
	// e(a*G1, G2) * e(b*G1, G2) * e(-(a+b)*G1, G2) = 1
	a := big.NewInt(5)
	b := big.NewInt(7)
	aPlusB := big.NewInt(12)

	aG1 := blsG1ScalarMul(BlsG1Generator(), a)
	bG1 := blsG1ScalarMul(BlsG1Generator(), b)
	negAbG1 := blsG1Neg(blsG1ScalarMul(BlsG1Generator(), aPlusB))
	g2 := BlsG2Generator()

	result := blsMultiPairing(
		[]*BlsG1Point{aG1, bG1, negAbG1},
		[]*BlsG2Point{g2, g2, g2},
	)
	if !result {
		t.Error("e(aG1,G2)*e(bG1,G2)*e(-(a+b)G1,G2) should be 1")
	}
}
