package bn254

import (
	"bytes"
	"math/big"
	"testing"
)

// --- BN254 G1 point at infinity edge cases ---

// TestBN254G1InfinityDouble tests that 2*O = O.
func TestBN254G1InfinityDouble(t *testing.T) {
	inf := G1Infinity()
	r := g1Double(inf)
	if !r.g1IsInfinity() {
		t.Fatal("2*O should be O")
	}
}

// TestBN254G1InfinityScalarMul tests that k*O = O for various k.
func TestBN254G1InfinityScalarMul(t *testing.T) {
	inf := G1Infinity()
	scalars := []*big.Int{big.NewInt(0), big.NewInt(1), big.NewInt(42), bn254N}
	for _, k := range scalars {
		r := G1ScalarMul(inf, k)
		if !r.g1IsInfinity() {
			t.Fatalf("k*O should be O for k=%s", k)
		}
	}
}

// TestBN254G1NegInfinity tests that -O = O.
func TestBN254G1NegInfinity(t *testing.T) {
	inf := G1Infinity()
	neg := g1Neg(inf)
	if !neg.g1IsInfinity() {
		t.Fatal("-O should be O")
	}
}

// TestBN254G1NegDoubleNeg tests that -(-P) = P.
func TestBN254G1NegDoubleNeg(t *testing.T) {
	gen := G1Generator()
	neg := g1Neg(gen)
	doubleNeg := g1Neg(neg)
	x1, y1 := gen.g1ToAffine()
	x2, y2 := doubleNeg.g1ToAffine()
	if x1.Cmp(x2) != 0 || y1.Cmp(y2) != 0 {
		t.Fatal("-(-G) should equal G")
	}
}

// TestBN254G1IdentityAddSelf tests that O + O = O.
func TestBN254G1IdentityAddSelf(t *testing.T) {
	inf := G1Infinity()
	r := g1Add(inf, inf)
	if !r.g1IsInfinity() {
		t.Fatal("O + O should be O")
	}
}

// --- BN254 G1 invalid point detection ---

// TestBN254G1IsOnCurveEdgeCases tests various edge cases for curve membership.
func TestBN254G1IsOnCurveEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		x, y *big.Int
		want bool
	}{
		{"identity (0,0)", big.NewInt(0), big.NewInt(0), true},
		{"generator (1,2)", big.NewInt(1), big.NewInt(2), true},
		{"off curve (1,3)", big.NewInt(1), big.NewInt(3), false},
		{"off curve (2,3)", big.NewInt(2), big.NewInt(3), false},
		{"x=p (out of range)", bn254P, big.NewInt(2), false},
		{"y=p (out of range)", big.NewInt(1), bn254P, false},
		{"negative x", big.NewInt(-1), big.NewInt(2), false},
		{"negative y", big.NewInt(1), big.NewInt(-1), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := g1IsOnCurve(tc.x, tc.y)
			if got != tc.want {
				t.Errorf("g1IsOnCurve(%s, %s) = %v, want %v", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

// --- BN254 precompile invalid input edge cases ---

// TestBN254AddFieldElementP tests that field element equal to p is rejected.
func TestBN254AddFieldElementP(t *testing.T) {
	input := make([]byte, 128)
	pBytes := bn254P.Bytes()
	copy(input[32-len(pBytes):32], pBytes) // x1 = p
	_, err := BN254Add(input)
	if err == nil {
		t.Fatal("expected error for x1 = p")
	}
}

// TestBN254AddFieldElementAboveP tests field element > p.
func TestBN254AddFieldElementAboveP(t *testing.T) {
	input := make([]byte, 128)
	pPlus1 := new(big.Int).Add(bn254P, big.NewInt(1))
	pBytes := pPlus1.Bytes()
	copy(input[32-len(pBytes):32], pBytes) // x1 = p+1
	_, err := BN254Add(input)
	if err == nil {
		t.Fatal("expected error for x1 = p+1")
	}
}

// TestBN254ScalarMulIdentityPoint tests scalar mul with the identity point.
func TestBN254ScalarMulIdentityPoint(t *testing.T) {
	// identity point (0,0) * any scalar = identity
	input := make([]byte, 96)
	input[95] = 42 // scalar=42
	out, err := BN254ScalarMul(input)
	if err != nil {
		t.Fatalf("BN254ScalarMul(0, 42): %v", err)
	}
	if !bytes.Equal(out, make([]byte, 64)) {
		t.Fatalf("0*42 should be identity, got %x", out)
	}
}

// TestBN254ScalarMulMaxScalar tests scalar multiplication with the largest
// possible 256-bit scalar.
func TestBN254ScalarMulMaxScalar(t *testing.T) {
	input := make([]byte, 96)
	input[31] = 1 // G1 x=1
	input[63] = 2 // G1 y=2
	// scalar = 2^256 - 1 (all ff)
	for i := 64; i < 96; i++ {
		input[i] = 0xff
	}
	out, err := BN254ScalarMul(input)
	if err != nil {
		t.Fatalf("BN254ScalarMul(G, max_scalar): %v", err)
	}
	// Result should be on the curve.
	x := new(big.Int).SetBytes(out[0:32])
	y := new(big.Int).SetBytes(out[32:64])
	if x.Sign() != 0 || y.Sign() != 0 {
		if !g1IsOnCurve(x, y) {
			t.Fatalf("result (%s, %s) is not on the curve", x, y)
		}
	}
}

// TestBN254PairingInvalidLengths tests various invalid input lengths for pairing.
func TestBN254PairingInvalidLengths(t *testing.T) {
	for _, l := range []int{1, 64, 100, 191, 193, 383, 385} {
		_, err := BN254PairingCheck(make([]byte, l))
		if err == nil {
			t.Errorf("expected error for pairing input length %d", l)
		}
	}
}

// TestBN254PairingTwoIdentityPairs tests that two identity pairs return true.
func TestBN254PairingTwoIdentityPairs(t *testing.T) {
	input := make([]byte, 384) // two identity pairs
	out, err := BN254PairingCheck(input)
	if err != nil {
		t.Fatalf("pairing two identity pairs: %v", err)
	}
	if out[31] != 1 {
		t.Fatalf("two identity pairs should return 1, got %x", out)
	}
}

// --- BN254 G1 scalar multiplication algebraic properties ---

// TestBN254G1ScalarMulDistributive tests that k*(P+Q) = k*P + k*Q.
func TestBN254G1ScalarMulDistributive(t *testing.T) {
	gen := G1Generator()
	twoG := g1Double(gen)
	k := big.NewInt(7)

	// k*(G + 2G) = k*3G
	sum := g1Add(gen, twoG)
	kSum := G1ScalarMul(sum, k)

	// k*G + k*2G
	kG := G1ScalarMul(gen, k)
	kTwoG := G1ScalarMul(twoG, k)
	kGplusTwoG := g1Add(kG, kTwoG)

	kSumX, kSumY := kSum.g1ToAffine()
	rX, rY := kGplusTwoG.g1ToAffine()
	if kSumX.Cmp(rX) != 0 || kSumY.Cmp(rY) != 0 {
		t.Fatal("k*(P+Q) != k*P + k*Q")
	}
}

// TestBN254G1ScalarMulNMinusOne tests that (n-1)*G = -G.
func TestBN254G1ScalarMulNMinusOne(t *testing.T) {
	gen := G1Generator()
	nMinusOne := new(big.Int).Sub(bn254N, big.NewInt(1))
	r := G1ScalarMul(gen, nMinusOne)
	negG := g1Neg(gen)

	rx, ry := r.g1ToAffine()
	nx, ny := negG.g1ToAffine()
	if rx.Cmp(nx) != 0 || ry.Cmp(ny) != 0 {
		t.Fatal("(n-1)*G != -G")
	}
}

// --- BN254 G2 edge cases ---

// TestBN254G2InfinityOperations tests G2 infinity operations.
func TestBN254G2InfinityOperations(t *testing.T) {
	inf := G2Infinity()
	gen := G2Generator()

	// O + O = O
	r := g2Add(inf, inf)
	if !r.g2IsInfinity() {
		t.Fatal("G2: O + O should be O")
	}

	// O + G = G
	r = g2Add(inf, gen)
	rx, ry := r.g2ToAffine()
	gx, gy := gen.g2ToAffine()
	if !rx.equal(gx) || !ry.equal(gy) {
		t.Fatal("G2: O + G should be G")
	}

	// 2*O = O
	r = g2Double(inf)
	if !r.g2IsInfinity() {
		t.Fatal("G2: 2*O should be O")
	}

	// -O = O
	r = g2Neg(inf)
	if !r.g2IsInfinity() {
		t.Fatal("G2: -O should be O")
	}
}

// TestBN254G2OnCurveEdgeCases tests the G2 curve membership check.
func TestBN254G2OnCurveEdgeCases(t *testing.T) {
	// Identity is valid.
	if !g2IsOnCurve(fp2Zero(), fp2Zero()) {
		t.Fatal("G2 identity should be valid")
	}

	// Generator is valid.
	gen := G2Generator()
	gx, gy := gen.g2ToAffine()
	if !g2IsOnCurve(gx, gy) {
		t.Fatal("G2 generator should be valid")
	}

	// A point with coordinates >= p should be invalid.
	badX := &fp2{a0: new(big.Int).Set(bn254P), a1: big.NewInt(0)}
	badY := fp2Zero()
	if g2IsOnCurve(badX, badY) {
		t.Fatal("G2 point with x.a0 = p should be invalid")
	}
}

// TestBN254G2NMinusOneTimesGen tests (n-1)*G2 = -G2.
func TestBN254G2NMinusOneTimesGen(t *testing.T) {
	gen := G2Generator()
	nMinusOne := new(big.Int).Sub(bn254N, big.NewInt(1))
	r := g2ScalarMul(gen, nMinusOne)
	negG := g2Neg(gen)

	rx, ry := r.g2ToAffine()
	nx, ny := negG.g2ToAffine()
	if !rx.equal(nx) || !ry.equal(ny) {
		t.Fatal("G2: (n-1)*G != -G")
	}
}

// --- BN254 Fp arithmetic edge cases ---

// TestBN254FpEdgeCases tests field arithmetic with boundary values.
func TestBN254FpEdgeCases(t *testing.T) {
	zero := big.NewInt(0)
	one := big.NewInt(1)
	pMinusOne := new(big.Int).Sub(bn254P, one)

	// 0 + 0 = 0
	if fpAdd(zero, zero).Sign() != 0 {
		t.Fatal("0 + 0 should be 0")
	}

	// p-1 + 1 = 0
	if fpAdd(pMinusOne, one).Sign() != 0 {
		t.Fatal("(p-1) + 1 should be 0")
	}

	// p-1 + p-1 = p-2
	sum := fpAdd(pMinusOne, pMinusOne)
	want := new(big.Int).Sub(bn254P, big.NewInt(2))
	if sum.Cmp(want) != 0 {
		t.Fatalf("(p-1)+(p-1) = %s, want %s", sum, want)
	}

	// 0 - 1 = p-1
	if fpSub(zero, one).Cmp(pMinusOne) != 0 {
		t.Fatal("0 - 1 should be p-1")
	}

	// 0 * anything = 0
	if fpMul(zero, big.NewInt(42)).Sign() != 0 {
		t.Fatal("0 * 42 should be 0")
	}

	// 1 * x = x
	if fpMul(one, big.NewInt(42)).Cmp(big.NewInt(42)) != 0 {
		t.Fatal("1 * 42 should be 42")
	}

	// x^2 via mul == x^2 via sqr
	x := big.NewInt(12345)
	mul := fpMul(x, x)
	sqr := fpSqr(x)
	if mul.Cmp(sqr) != 0 {
		t.Fatalf("fpMul(x,x) = %s, fpSqr(x) = %s", mul, sqr)
	}
}

// --- BN254 Fp2 arithmetic edge cases ---

// TestBN254Fp2EdgeCases tests Fp2 arithmetic with edge values.
func TestBN254Fp2EdgeCases(t *testing.T) {
	// Zero operations.
	zero := fp2Zero()
	one := fp2One()

	// 0 + 0 = 0
	sum := fp2Add(zero, zero)
	if !sum.isZero() {
		t.Fatal("fp2: 0 + 0 should be 0")
	}

	// 1 * 0 = 0
	prod := fp2Mul(one, zero)
	if !prod.isZero() {
		t.Fatal("fp2: 1 * 0 should be 0")
	}

	// 1 * 1 = 1
	prod = fp2Mul(one, one)
	if !prod.equal(one) {
		t.Fatal("fp2: 1 * 1 should be 1")
	}

	// Conjugation: conj(a + bi) = a - bi.
	a := &fp2{a0: big.NewInt(3), a1: big.NewInt(7)}
	conj := fp2Conj(a)
	if conj.a0.Cmp(big.NewInt(3)) != 0 {
		t.Fatal("fp2: conj real part should be unchanged")
	}
	// conj.a1 should be -7 mod p = p - 7.
	expected := new(big.Int).Sub(bn254P, big.NewInt(7))
	if conj.a1.Cmp(expected) != 0 {
		t.Fatalf("fp2: conj imaginary part = %s, want %s", conj.a1, expected)
	}

	// a * conj(a) should be real (imaginary part = 0).
	// (a + bi)(a - bi) = a^2 + b^2.
	norm := fp2Mul(a, conj)
	if norm.a1.Sign() != 0 {
		t.Fatalf("fp2: a * conj(a) should be real, got imaginary part %s", norm.a1)
	}

	// Negation: a + (-a) = 0.
	neg := fp2Neg(a)
	sum = fp2Add(a, neg)
	if !sum.isZero() {
		t.Fatalf("fp2: a + (-a) should be 0, got (%s, %s)", sum.a0, sum.a1)
	}
}

// TestBN254Fp2SquaringConsistency tests that fp2Sqr(a) == fp2Mul(a, a).
func TestBN254Fp2SquaringConsistency(t *testing.T) {
	a := &fp2{a0: big.NewInt(17), a1: big.NewInt(23)}
	sqr := fp2Sqr(a)
	mul := fp2Mul(a, a)
	if !sqr.equal(mul) {
		t.Fatalf("fp2Sqr(a) != fp2Mul(a, a): sqr=(%s,%s), mul=(%s,%s)",
			sqr.a0, sqr.a1, mul.a0, mul.a1)
	}
}

// --- BN254 precompile: pad right tests ---

// TestBN254PadRight tests the padding utility.
func TestBN254PadRight(t *testing.T) {
	tests := []struct {
		name   string
		data   []byte
		minLen int
		wantL  int
	}{
		{"empty to 128", nil, 128, 128},
		{"short to 128", []byte{1, 2, 3}, 128, 128},
		{"exact", make([]byte, 128), 128, 128},
		{"long truncate", make([]byte, 200), 128, 128},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := bn254PadRight(tc.data, tc.minLen)
			if len(result) != tc.wantL {
				t.Errorf("length = %d, want %d", len(result), tc.wantL)
			}
		})
	}
}

// TestBN254AddShortInput tests that short inputs are zero-padded correctly.
func TestBN254AddShortInput(t *testing.T) {
	// Empty input = all zeros = identity + identity = identity.
	out, err := BN254Add(nil)
	if err != nil {
		t.Fatalf("BN254Add(nil): %v", err)
	}
	if !bytes.Equal(out, make([]byte, 64)) {
		t.Fatalf("BN254Add(nil) should be identity, got %x", out)
	}
}

// TestBN254ScalarMulShortInput tests that short scalar mul input is padded.
func TestBN254ScalarMulShortInput(t *testing.T) {
	// Empty input = (0,0) * 0 = identity.
	out, err := BN254ScalarMul(nil)
	if err != nil {
		t.Fatalf("BN254ScalarMul(nil): %v", err)
	}
	if !bytes.Equal(out, make([]byte, 64)) {
		t.Fatalf("BN254ScalarMul(nil) should be identity, got %x", out)
	}
}

// TestBN254G1AddCommutativity verifies P+Q == Q+P at the precompile level.
func TestBN254G1AddCommutativity(t *testing.T) {
	gen := G1Generator()
	twoG := G1ScalarMul(gen, big.NewInt(2))
	gx, gy := gen.g1ToAffine()
	tgx, tgy := twoG.g1ToAffine()

	// G + 2G
	input1 := make([]byte, 128)
	gxBytes := gx.Bytes()
	gyBytes := gy.Bytes()
	tgxBytes := tgx.Bytes()
	tgyBytes := tgy.Bytes()
	copy(input1[32-len(gxBytes):32], gxBytes)
	copy(input1[64-len(gyBytes):64], gyBytes)
	copy(input1[96-len(tgxBytes):96], tgxBytes)
	copy(input1[128-len(tgyBytes):128], tgyBytes)

	// 2G + G
	input2 := make([]byte, 128)
	copy(input2[32-len(tgxBytes):32], tgxBytes)
	copy(input2[64-len(tgyBytes):64], tgyBytes)
	copy(input2[96-len(gxBytes):96], gxBytes)
	copy(input2[128-len(gyBytes):128], gyBytes)

	out1, err := BN254Add(input1)
	if err != nil {
		t.Fatalf("BN254Add(G + 2G): %v", err)
	}
	out2, err := BN254Add(input2)
	if err != nil {
		t.Fatalf("BN254Add(2G + G): %v", err)
	}
	if !bytes.Equal(out1, out2) {
		t.Fatalf("G+2G != 2G+G: %x != %x", out1, out2)
	}
}

// TestBN254G1ScalarMulConsistency verifies that k*G via precompile matches
// the direct G1ScalarMul result.
func TestBN254G1ScalarMulConsistency(t *testing.T) {
	scalars := []*big.Int{
		big.NewInt(3),
		big.NewInt(100),
		big.NewInt(255),
	}
	for _, k := range scalars {
		gen := G1Generator()
		direct := G1ScalarMul(gen, k)
		dx, dy := direct.g1ToAffine()

		input := make([]byte, 96)
		input[31] = 1
		input[63] = 2
		kBytes := k.Bytes()
		copy(input[96-len(kBytes):96], kBytes)
		out, err := BN254ScalarMul(input)
		if err != nil {
			t.Fatalf("BN254ScalarMul(%s*G): %v", k, err)
		}
		px := new(big.Int).SetBytes(out[0:32])
		py := new(big.Int).SetBytes(out[32:64])
		if px.Cmp(dx) != 0 || py.Cmp(dy) != 0 {
			t.Fatalf("k=%s: precompile (%s,%s) != direct (%s,%s)", k, px, py, dx, dy)
		}
	}
}

// --- BN254 Fp12 edge cases ---
