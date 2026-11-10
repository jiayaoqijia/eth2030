package bls

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"
)

// --- BLS12-381 G1 edge cases ---

// TestBlsG1InfinityDouble tests that 2*O = O.
func TestBlsG1InfinityDouble(t *testing.T) {
	inf := BlsG1Infinity()
	r := blsG1Double(inf)
	if !r.blsG1IsInfinity() {
		t.Fatal("BLS G1: 2*O should be O")
	}
}

// TestBlsG1InfinityScalarMul tests that k*O = O for various k.
func TestBlsG1InfinityScalarMul(t *testing.T) {
	inf := BlsG1Infinity()
	scalars := []*big.Int{big.NewInt(0), big.NewInt(1), big.NewInt(42), blsR}
	for _, k := range scalars {
		r := blsG1ScalarMul(inf, k)
		if !r.blsG1IsInfinity() {
			t.Fatalf("BLS G1: k*O should be O for k=%s", k)
		}
	}
}

// TestBlsG1NegInfinity tests that -O = O.
func TestBlsG1NegInfinity(t *testing.T) {
	inf := BlsG1Infinity()
	neg := blsG1Neg(inf)
	if !neg.blsG1IsInfinity() {
		t.Fatal("BLS G1: -O should be O")
	}
}

// TestBlsG1NegDoubleNeg tests that -(-P) = P.
func TestBlsG1NegDoubleNeg(t *testing.T) {
	gen := BlsG1Generator()
	neg := blsG1Neg(gen)
	doubleNeg := blsG1Neg(neg)

	x1, y1 := gen.blsG1ToAffine()
	x2, y2 := doubleNeg.blsG1ToAffine()
	if x1.Cmp(x2) != 0 || y1.Cmp(y2) != 0 {
		t.Fatal("BLS G1: -(-G) should equal G")
	}
}

// TestBlsG1RMinusOneTimesGen tests that (r-1)*G = -G.
func TestBlsG1RMinusOneTimesGen(t *testing.T) {
	gen := BlsG1Generator()
	rMinusOne := new(big.Int).Sub(blsR, big.NewInt(1))
	r := blsG1ScalarMul(gen, rMinusOne)
	negG := blsG1Neg(gen)

	rx, ry := r.blsG1ToAffine()
	nx, ny := negG.blsG1ToAffine()
	if rx.Cmp(nx) != 0 || ry.Cmp(ny) != 0 {
		t.Fatal("BLS G1: (r-1)*G != -G")
	}
}

// TestBlsG1IsOnCurveEdgeCases tests curve membership with edge values.
func TestBlsG1IsOnCurveEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		x, y *big.Int
		want bool
	}{
		{"identity (0,0)", big.NewInt(0), big.NewInt(0), true},
		{"off curve (1,1)", big.NewInt(1), big.NewInt(1), false},
		{"x=p out of range", new(big.Int).Set(blsP), big.NewInt(2), false},
		{"negative x", big.NewInt(-1), big.NewInt(2), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := blsG1IsOnCurve(tc.x, tc.y)
			if got != tc.want {
				t.Errorf("blsG1IsOnCurve(%s, %s) = %v, want %v", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

// TestBlsG1ScalarMulDistributive tests that k*(P+Q) = k*P + k*Q.
func TestBlsG1ScalarMulDistributive(t *testing.T) {
	gen := BlsG1Generator()
	twoG := blsG1Double(gen)
	k := big.NewInt(5)

	// k*(G + 2G) = k*3G
	sum := blsG1Add(gen, twoG)
	kSum := blsG1ScalarMul(sum, k)

	// k*G + k*2G
	kG := blsG1ScalarMul(gen, k)
	kTwoG := blsG1ScalarMul(twoG, k)
	kGplusTwoG := blsG1Add(kG, kTwoG)

	kSumX, kSumY := kSum.blsG1ToAffine()
	rX, rY := kGplusTwoG.blsG1ToAffine()
	if kSumX.Cmp(rX) != 0 || kSumY.Cmp(rY) != 0 {
		t.Fatal("BLS G1: k*(P+Q) != k*P + k*Q")
	}
}

// --- BLS12-381 G2 edge cases ---

// TestBlsG2InfinityOperations tests G2 infinity operations.
func TestBlsG2InfinityOperations(t *testing.T) {
	inf := BlsG2Infinity()

	// O + O = O
	r := blsG2Add(inf, inf)
	if !r.blsG2IsInfinity() {
		t.Fatal("BLS G2: O + O should be O")
	}

	// 2*O = O
	r = blsG2Double(inf)
	if !r.blsG2IsInfinity() {
		t.Fatal("BLS G2: 2*O should be O")
	}

	// -O = O
	r = blsG2Neg(inf)
	if !r.blsG2IsInfinity() {
		t.Fatal("BLS G2: -O should be O")
	}

	// k*O = O
	r = blsG2ScalarMul(inf, big.NewInt(42))
	if !r.blsG2IsInfinity() {
		t.Fatal("BLS G2: k*O should be O")
	}
}

// TestBlsG2NegDoubleNeg tests that -(-P) = P.
func TestBlsG2NegDoubleNeg(t *testing.T) {
	gen := BlsG2Generator()
	neg := blsG2Neg(gen)
	doubleNeg := blsG2Neg(neg)

	x1, y1 := gen.blsG2ToAffine()
	x2, y2 := doubleNeg.blsG2ToAffine()
	if !x1.equal(x2) || !y1.equal(y2) {
		t.Fatal("BLS G2: -(-G) should equal G")
	}
}

// TestBlsG2RMinusOneTimesGen tests that (r-1)*G2 = -G2.
func TestBlsG2RMinusOneTimesGen(t *testing.T) {
	gen := BlsG2Generator()
	rMinusOne := new(big.Int).Sub(blsR, big.NewInt(1))
	r := blsG2ScalarMul(gen, rMinusOne)
	negG := blsG2Neg(gen)

	rx, ry := r.blsG2ToAffine()
	nx, ny := negG.blsG2ToAffine()
	if !rx.equal(nx) || !ry.equal(ny) {
		t.Fatal("BLS G2: (r-1)*G2 != -G2")
	}
}

// TestBlsG2SubgroupCheck tests the subgroup check for G2.
func TestBlsG2SubgroupCheck(t *testing.T) {
	gen := BlsG2Generator()
	if !blsG2InSubgroup(gen) {
		t.Error("BLS G2 generator should be in subgroup")
	}

	inf := BlsG2Infinity()
	if !blsG2InSubgroup(inf) {
		t.Error("BLS G2 infinity should be in subgroup")
	}
}

// --- BLS12-381 Fp arithmetic edge cases ---

// TestBlsFpEdgeCases tests field arithmetic with boundary values.
func TestBlsFpEdgeCases(t *testing.T) {
	zero := big.NewInt(0)
	one := big.NewInt(1)
	pMinusOne := new(big.Int).Sub(blsP, one)

	// 0 + 0 = 0
	if blsFpAdd(zero, zero).Sign() != 0 {
		t.Fatal("BLS Fp: 0 + 0 should be 0")
	}

	// p-1 + 1 = 0
	if blsFpAdd(pMinusOne, one).Sign() != 0 {
		t.Fatal("BLS Fp: (p-1) + 1 should be 0")
	}

	// 0 - 1 = p-1
	if blsFpSub(zero, one).Cmp(pMinusOne) != 0 {
		t.Fatal("BLS Fp: 0 - 1 should be p-1")
	}

	// -0 = 0
	if blsFpNeg(zero).Sign() != 0 {
		t.Fatal("BLS Fp: -0 should be 0")
	}

	// 1 * 1 = 1
	if blsFpMul(one, one).Cmp(one) != 0 {
		t.Fatal("BLS Fp: 1 * 1 should be 1")
	}

	// 0 * x = 0
	if blsFpMul(zero, big.NewInt(42)).Sign() != 0 {
		t.Fatal("BLS Fp: 0 * 42 should be 0")
	}

	// sqrt(0) = 0
	sqrt0 := blsFpSqrt(zero)
	if sqrt0 == nil || sqrt0.Sign() != 0 {
		t.Fatal("BLS Fp: sqrt(0) should be 0")
	}

	// sqrt(1) should be 1 or p-1.
	sqrt1 := blsFpSqrt(one)
	if sqrt1 == nil {
		t.Fatal("BLS Fp: sqrt(1) should exist")
	}
	if blsFpSqr(sqrt1).Cmp(one) != 0 {
		t.Fatal("BLS Fp: sqrt(1)^2 should be 1")
	}
}

// TestBlsFpIsSquare tests quadratic residue checking.
func TestBlsFpIsSquare(t *testing.T) {
	if !blsFpIsSquare(big.NewInt(0)) {
		t.Error("0 should be a square")
	}
	if !blsFpIsSquare(big.NewInt(1)) {
		t.Error("1 should be a square")
	}
	if !blsFpIsSquare(big.NewInt(4)) {
		t.Error("4 should be a square")
	}
}

// TestBlsFpSgn0 tests the sign function.
func TestBlsFpSgn0(t *testing.T) {
	if blsFpSgn0(big.NewInt(0)) != 0 {
		t.Error("sgn0(0) should be 0")
	}
	if blsFpSgn0(big.NewInt(1)) != 1 {
		t.Error("sgn0(1) should be 1")
	}
	if blsFpSgn0(big.NewInt(2)) != 0 {
		t.Error("sgn0(2) should be 0")
	}
}

// TestBlsFpCmov tests the conditional move function.
func TestBlsFpCmov(t *testing.T) {
	a := big.NewInt(10)
	c := big.NewInt(20)

	// b=0 returns a.
	r := blsFpCmov(a, c, 0)
	if r.Cmp(a) != 0 {
		t.Errorf("cmov(10, 20, 0) = %s, want 10", r)
	}

	// b=1 returns c.
	r = blsFpCmov(a, c, 1)
	if r.Cmp(c) != 0 {
		t.Errorf("cmov(10, 20, 1) = %s, want 20", r)
	}
}

// --- BLS12-381 Fp2 edge cases ---

// TestBlsFp2EdgeCases tests Fp2 arithmetic with edge values.
func TestBlsFp2EdgeCases(t *testing.T) {
	zero := blsFp2Zero()
	one := blsFp2One()

	// 0 + 0 = 0
	if !blsFp2Add(zero, zero).isZero() {
		t.Fatal("BLS Fp2: 0 + 0 should be 0")
	}

	// 1 * 0 = 0
	if !blsFp2Mul(one, zero).isZero() {
		t.Fatal("BLS Fp2: 1 * 0 should be 0")
	}

	// a + (-a) = 0
	a := &blsFp2{c0: big.NewInt(5), c1: big.NewInt(11)}
	neg := blsFp2Neg(a)
	sum := blsFp2Add(a, neg)
	if !sum.isZero() {
		t.Fatal("BLS Fp2: a + (-a) should be 0")
	}

	// Conjugation: a * conj(a) should be real.
	conj := blsFp2Conj(a)
	prod := blsFp2Mul(a, conj)
	if prod.c1.Sign() != 0 {
		t.Fatal("BLS Fp2: a * conj(a) should have zero imaginary part")
	}

	// Squaring consistency: sqr(a) == mul(a, a).
	sqr := blsFp2Sqr(a)
	mul := blsFp2Mul(a, a)
	if !sqr.equal(mul) {
		t.Fatal("BLS Fp2: sqr(a) != mul(a, a)")
	}
}

// TestBlsFp2MulByU tests multiplication by the non-residue u.
func TestBlsFp2MulByU(t *testing.T) {
	// u * 1 = (0, 1) -> u * (1 + 0*u) = -0 + 1*u = (0, 1)
	// Wait: u * (c0 + c1*u) = -c1 + c0*u
	// So u * (1 + 0*u) = -0 + 1*u = (0, 1)
	r := blsFp2MulByU(blsFp2One())
	expected := &blsFp2{c0: new(big.Int), c1: big.NewInt(1)}
	if !r.equal(expected) {
		t.Errorf("u * 1 = (%s, %s), want (0, 1)", r.c0, r.c1)
	}

	// u * u = -1 (since u^2 = -1)
	u := &blsFp2{c0: new(big.Int), c1: big.NewInt(1)}
	r = blsFp2MulByU(u)
	// u * (0 + 1*u) = -1 + 0*u = (-1, 0) = (p-1, 0)
	expectedC0 := new(big.Int).Sub(blsP, big.NewInt(1))
	expected = &blsFp2{c0: expectedC0, c1: new(big.Int)}
	if !r.equal(expected) {
		t.Errorf("u * u = (%s, %s), want (%s, 0)", r.c0, r.c1, expectedC0)
	}
}

// TestBlsFp2Sqrt tests square root in Fp2.
func TestBlsFp2Sqrt(t *testing.T) {
	// sqrt(0) = 0
	r := blsFp2Sqrt(blsFp2Zero())
	if r == nil || !r.isZero() {
		t.Fatal("BLS Fp2: sqrt(0) should be 0")
	}

	// sqrt(1) should exist and verify sqrt(1)^2 = 1.
	r = blsFp2Sqrt(blsFp2One())
	if r == nil {
		t.Fatal("BLS Fp2: sqrt(1) should exist")
	}
	if !blsFp2Sqr(r).equal(blsFp2One()) {
		t.Fatal("BLS Fp2: sqrt(1)^2 should be 1")
	}
}

// TestBlsFp2IsSquare tests the quadratic residue check in Fp2.
func TestBlsFp2IsSquare(t *testing.T) {
	if !blsFp2IsSquare(blsFp2Zero()) {
		t.Error("BLS Fp2: 0 should be a square")
	}
	if !blsFp2IsSquare(blsFp2One()) {
		t.Error("BLS Fp2: 1 should be a square")
	}
}

// --- BLS12-381 Precompile input validation ---

// TestBLS12G1AddInvalidPoint tests G1 add with a point not on the curve.
func TestBLS12G1AddInvalidPoint(t *testing.T) {
	// Create a point that's not on the curve: (1, 1) is not on y^2 = x^3 + 4.
	input := make([]byte, 256)
	input[63] = 1  // x = 1
	input[127] = 1 // y = 1
	// Second point is infinity (all zeros).
	_, err := BLS12G1Add(input)
	if err == nil {
		t.Error("expected error for point not on curve")
	}
}

// TestBLS12G1MulInvalidPoint tests G1 mul with a point not on the curve.
func TestBLS12G1MulInvalidPoint(t *testing.T) {
	input := make([]byte, 160)
	input[63] = 1  // x = 1
	input[127] = 1 // y = 1
	input[159] = 1 // scalar = 1
	_, err := BLS12G1Mul(input)
	if err == nil {
		t.Error("expected error for point not on curve")
	}
}

// TestBLS12G1MSMEmpty tests MSM with empty input.
func TestBLS12G1MSMEmpty(t *testing.T) {
	_, err := BLS12G1MSM(nil)
	if err == nil {
		t.Error("expected error for empty MSM input")
	}
}

// TestBLS12G2AddInvalidPoint tests G2 add with a point not on the curve.
func TestBLS12G2AddInvalidPoint(t *testing.T) {
	// A point (1, 1) is not on the G2 twist curve.
	input := make([]byte, 512)
	input[127] = 1 // x.re = 1
	input[255] = 1 // y.re = 1
	_, err := BLS12G2Add(input)
	if err == nil {
		t.Error("expected error for G2 point not on curve")
	}
}

// TestBLS12G2MSMEmpty tests MSM with empty input.
func TestBLS12G2MSMEmpty(t *testing.T) {
	_, err := BLS12G2MSM(nil)
	if err == nil {
		t.Error("expected error for empty G2 MSM input")
	}
}

// TestBLS12PairingEmpty tests pairing with empty input.
func TestBLS12PairingEmpty(t *testing.T) {
	_, err := BLS12Pairing(nil)
	if err == nil {
		t.Error("expected error for empty pairing input")
	}
}

// --- BLS12-381 Precompile round-trip consistency ---

// TestBLS12G1MulByZero tests that 0*G = infinity.
func TestBLS12G1MulByZero(t *testing.T) {
	gen := BlsG1Generator()
	genEnc := encodeG1(gen)

	scalar := make([]byte, 32) // zero
	input := append(genEnc, scalar...)
	result, err := BLS12G1Mul(input)
	if err != nil {
		t.Fatalf("BLS12G1Mul error: %v", err)
	}
	// All zeros = infinity.
	for _, b := range result {
		if b != 0 {
			t.Error("0 * G should be infinity")
			break
		}
	}
}

// TestBLS12G2MulByTwo tests that 2*G2 via precompile matches G2+G2.
func TestBLS12G2MulByTwo(t *testing.T) {
	gen := BlsG2Generator()
	genEnc := encodeG2(gen)

	// 2*G2 via mul.
	scalar := make([]byte, 32)
	scalar[31] = 2
	mulInput := append(genEnc, scalar...)
	mulResult, err := BLS12G2Mul(mulInput)
	if err != nil {
		t.Fatalf("BLS12G2Mul error: %v", err)
	}

	// G2 + G2 via add.
	addInput := append(genEnc, genEnc...)
	addResult, err := BLS12G2Add(addInput)
	if err != nil {
		t.Fatalf("BLS12G2Add error: %v", err)
	}

	if !bytes.Equal(mulResult, addResult) {
		t.Error("2*G2 via mul != G2+G2 via add")
	}
}

// TestBLS12G1AddInverseViaPrecompile tests G + (-G) = infinity via precompile.
func TestBLS12G1AddInverseViaPrecompile(t *testing.T) {
	gen := BlsG1Generator()
	neg := blsG1Neg(gen)

	genEnc := encodeG1(gen)
	negEnc := encodeG1(neg)

	input := append(genEnc, negEnc...)
	result, err := BLS12G1Add(input)
	if err != nil {
		t.Fatalf("BLS12G1Add error: %v", err)
	}

	for _, b := range result {
		if b != 0 {
			t.Error("G + (-G) should be infinity")
			break
		}
	}
}

// TestBLS12G2AddInverseViaPrecompile tests G2 + (-G2) = infinity via precompile.
func TestBLS12G2AddInverseViaPrecompile(t *testing.T) {
	gen := BlsG2Generator()
	neg := blsG2Neg(gen)

	genEnc := encodeG2(gen)
	negEnc := encodeG2(neg)

	input := append(genEnc, negEnc...)
	result, err := BLS12G2Add(input)
	if err != nil {
		t.Fatalf("BLS12G2Add error: %v", err)
	}

	for _, b := range result {
		if b != 0 {
			t.Error("G2 + (-G2) should be infinity")
			break
		}
	}
}

// --- BLS12-381 Encoding/Decoding edge cases ---

// TestDecodeFpWrongLength tests decodeFp with wrong lengths.
func TestDecodeFpWrongLength(t *testing.T) {
	for _, l := range []int{0, 1, 32, 63, 65, 128} {
		_, err := decodeFp(make([]byte, l))
		if err == nil {
			t.Errorf("expected error for decodeFp with length %d", l)
		}
	}
}

// TestDecodeG1WrongLength tests decodeG1 with wrong lengths.
func TestDecodeG1WrongLength(t *testing.T) {
	for _, l := range []int{0, 64, 127, 129, 256} {
		_, err := decodeG1(make([]byte, l))
		if err == nil {
			t.Errorf("expected error for decodeG1 with length %d", l)
		}
	}
}

// TestDecodeG2WrongLength tests decodeG2 with wrong lengths.
func TestDecodeG2WrongLength(t *testing.T) {
	for _, l := range []int{0, 128, 255, 257, 512} {
		_, err := decodeG2(make([]byte, l))
		if err == nil {
			t.Errorf("expected error for decodeG2 with length %d", l)
		}
	}
}

// TestEncodeFpRoundTrip tests that encodeFp/decodeFp round-trips correctly.
func TestEncodeFpRoundTrip(t *testing.T) {
	values := []*big.Int{
		big.NewInt(0),
		big.NewInt(1),
		big.NewInt(42),
		new(big.Int).Sub(blsP, big.NewInt(1)),
	}
	for _, v := range values {
		encoded := encodeFp(v)
		if len(encoded) != 64 {
			t.Fatalf("encodeFp length = %d, want 64", len(encoded))
		}
		decoded, err := decodeFp(encoded)
		if err != nil {
			t.Fatalf("decodeFp error: %v", err)
		}
		if v.Cmp(decoded) != 0 {
			t.Errorf("round-trip: %s != %s", v, decoded)
		}
	}
}

// TestEncodeG1RoundTrip tests that encodeG1/decodeG1 round-trips for 2G.
func TestEncodeG1RoundTrip(t *testing.T) {
	gen := BlsG1Generator()
	twoG := blsG1ScalarMul(gen, big.NewInt(2))

	encoded := encodeG1(twoG)
	decoded, err := decodeG1(encoded)
	if err != nil {
		t.Fatalf("decodeG1 error: %v", err)
	}

	x1, y1 := twoG.blsG1ToAffine()
	x2, y2 := decoded.blsG1ToAffine()
	if x1.Cmp(x2) != 0 || y1.Cmp(y2) != 0 {
		t.Error("round-trip encoding failed for 2*G1")
	}
}

// TestEncodeG2RoundTrip tests that encodeG2/decodeG2 round-trips for 2G2.
func TestEncodeG2RoundTrip(t *testing.T) {
	gen := BlsG2Generator()
	twoG := blsG2ScalarMul(gen, big.NewInt(2))

	encoded := encodeG2(twoG)
	decoded, err := decodeG2(encoded)
	if err != nil {
		t.Fatalf("decodeG2 error: %v", err)
	}

	x1, y1 := twoG.blsG2ToAffine()
	x2, y2 := decoded.blsG2ToAffine()
	if !x1.equal(x2) || !y1.equal(y2) {
		t.Error("round-trip encoding failed for 2*G2")
	}
}

// --- BLS12-381 Map-to-curve edge cases ---

// TestBLS12MapFpToG1Deterministic tests that the same input always
// produces the same curve point.
func TestBLS12MapFpToG1Deterministic(t *testing.T) {
	input := make([]byte, 64)
	input[63] = 42
	result1, err := BLS12MapFpToG1(input)
	if err != nil {
		t.Fatalf("BLS12MapFpToG1 error: %v", err)
	}
	result2, err := BLS12MapFpToG1(input)
	if err != nil {
		t.Fatalf("BLS12MapFpToG1 error: %v", err)
	}
	if !bytes.Equal(result1, result2) {
		t.Error("BLS12MapFpToG1 should be deterministic")
	}
}

// TestBLS12MapFpToG1WidelyDifferentInputs tests that widely different
// inputs produce different curve points.
func TestBLS12MapFpToG1WidelyDifferentInputs(t *testing.T) {
	results := make(map[string]int)
	// Use widely spaced values to avoid the try-and-increment overlap.
	values := []byte{0, 50, 100, 150, 200}
	for _, v := range values {
		input := make([]byte, 64)
		input[63] = v
		result, err := BLS12MapFpToG1(input)
		if err != nil {
			t.Fatalf("BLS12MapFpToG1(%d) error: %v", v, err)
		}
		key := hex.EncodeToString(result)
		if prev, ok := results[key]; ok {
			t.Errorf("BLS12MapFpToG1(%d) collides with BLS12MapFpToG1(%d)", v, prev)
		}
		results[key] = int(v)
	}
}

// TestBLS12MapFpToG1ResultInSubgroup tests that the output of map-to-curve
// is in the correct subgroup.
func TestBLS12MapFpToG1ResultInSubgroup(t *testing.T) {
	input := make([]byte, 64)
	input[63] = 7
	result, err := BLS12MapFpToG1(input)
	if err != nil {
		t.Fatalf("BLS12MapFpToG1 error: %v", err)
	}

	p, err := decodeG1(result)
	if err != nil {
		t.Fatalf("decodeG1 error: %v", err)
	}

	// The result should be in the subgroup (verified by decodeG1).
	if !p.blsG1IsInfinity() && !blsG1InSubgroup(p) {
		t.Error("mapped point is not in the subgroup")
	}
}

// --- BLS12-381 Fp6 edge cases ---

// --- BLS12-381 Fp12 edge cases ---

// --- BLS12-381 Pairing edge cases ---

// TestBLS12PairingTwoInfinityPairs tests that two infinity pairs pass pairing.
func TestBLS12PairingTwoInfinityPairs(t *testing.T) {
	input := make([]byte, 768) // two pairs, all zeros = all infinity
	result, err := BLS12Pairing(input)
	if err != nil {
		t.Fatalf("BLS12Pairing error: %v", err)
	}
	if result[31] != 1 {
		t.Error("pairing with all infinity pairs should return 1")
	}
}

// TestBLS12PairingInfinityG1 tests pairing with G1 at infinity.
func TestBLS12PairingInfinityG1(t *testing.T) {
	// Input: G1=infinity, G2=generator.
	g1Inf := make([]byte, 128) // infinity
	g2Gen := encodeG2(BlsG2Generator())

	input := append(g1Inf, g2Gen...)
	result, err := BLS12Pairing(input)
	if err != nil {
		t.Fatalf("BLS12Pairing error: %v", err)
	}
	if result[31] != 1 {
		t.Error("e(O, Q) should be 1")
	}
}

// TestBLS12PairingInfinityG2 tests pairing with G2 at infinity.
func TestBLS12PairingInfinityG2(t *testing.T) {
	// Input: G1=generator, G2=infinity.
	g1Gen := encodeG1(BlsG1Generator())
	g2Inf := make([]byte, 256) // infinity

	input := append(g1Gen, g2Inf...)
	result, err := BLS12Pairing(input)
	if err != nil {
		t.Fatalf("BLS12Pairing error: %v", err)
	}
	if result[31] != 1 {
		t.Error("e(P, O) should be 1")
	}
}
