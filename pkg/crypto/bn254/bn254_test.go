package bn254

import (
	"bytes"
	"encoding/hex"
	"math/big"
	"testing"
)

// TestG1OnCurve verifies basic curve membership checks.
// G2 generator point coordinates.
var (
	g2GenXa0, _ = new(big.Int).SetString("10857046999023057135944570762232829481370756359578518086990519993285655852781", 10)
	g2GenXa1, _ = new(big.Int).SetString("11559732032986387107991004021392285783925812861821192530917403151452391805634", 10)
	g2GenYa0, _ = new(big.Int).SetString("8495653923123431417604973247489272438418190587263600148770280649306958101930", 10)
	g2GenYa1, _ = new(big.Int).SetString("4082367875863433681332203403145435568316851327593401208105741076214120093531", 10)
)

func TestG1OnCurve(t *testing.T) {
	// Generator (1, 2) should be on the curve.
	if !g1IsOnCurve(big.NewInt(1), big.NewInt(2)) {
		t.Fatal("generator (1,2) should be on the curve")
	}

	// Identity (0, 0) should be valid.
	if !g1IsOnCurve(big.NewInt(0), big.NewInt(0)) {
		t.Fatal("identity (0,0) should be valid")
	}

	// Random invalid point.
	if g1IsOnCurve(big.NewInt(1), big.NewInt(3)) {
		t.Fatal("(1,3) should not be on the curve")
	}
}

// TestG1AddIdentity verifies P + 0 = P.
func TestG1AddIdentity(t *testing.T) {
	gen := G1Generator()
	inf := G1Infinity()

	r := g1Add(gen, inf)
	rx, ry := r.g1ToAffine()
	if rx.Cmp(big.NewInt(1)) != 0 || ry.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("G + 0 = (%s, %s), want (1, 2)", rx, ry)
	}

	r2 := g1Add(inf, gen)
	rx2, ry2 := r2.g1ToAffine()
	if rx2.Cmp(big.NewInt(1)) != 0 || ry2.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("0 + G = (%s, %s), want (1, 2)", rx2, ry2)
	}
}

// TestG1AddInverse verifies P + (-P) = 0.
func TestG1AddInverse(t *testing.T) {
	gen := G1Generator()
	neg := g1Neg(gen)

	r := g1Add(gen, neg)
	if !r.g1IsInfinity() {
		rx, ry := r.g1ToAffine()
		t.Fatalf("G + (-G) should be infinity, got (%s, %s)", rx, ry)
	}
}

// TestG1Double verifies 2*G via doubling.
func TestG1Double(t *testing.T) {
	gen := G1Generator()
	d := g1Double(gen)
	dx, dy := d.g1ToAffine()

	// 2G should also be on the curve.
	if !g1IsOnCurve(dx, dy) {
		t.Fatal("2*G should be on the curve")
	}

	// Verify via addition.
	a := g1Add(gen, gen)
	ax, ay := a.g1ToAffine()
	if dx.Cmp(ax) != 0 || dy.Cmp(ay) != 0 {
		t.Fatalf("2*G via double != 2*G via add: (%s,%s) != (%s,%s)", dx, dy, ax, ay)
	}
}

// TestG1ScalarMul verifies scalar multiplication.
func TestG1ScalarMul(t *testing.T) {
	gen := G1Generator()

	// 1*G = G
	r := G1ScalarMul(gen, big.NewInt(1))
	rx, ry := r.g1ToAffine()
	if rx.Cmp(big.NewInt(1)) != 0 || ry.Cmp(big.NewInt(2)) != 0 {
		t.Fatalf("1*G = (%s, %s), want (1, 2)", rx, ry)
	}

	// 0*G = infinity
	r0 := G1ScalarMul(gen, big.NewInt(0))
	if !r0.g1IsInfinity() {
		t.Fatal("0*G should be infinity")
	}

	// n*G = infinity (curve order)
	rn := G1ScalarMul(gen, bn254N)
	if !rn.g1IsInfinity() {
		rnx, rny := rn.g1ToAffine()
		t.Fatalf("n*G should be infinity, got (%s, %s)", rnx, rny)
	}

	// 2*G via scalar mul should match doubling.
	r2 := G1ScalarMul(gen, big.NewInt(2))
	r2x, r2y := r2.g1ToAffine()
	d := g1Double(gen)
	dx, dy := d.g1ToAffine()
	if r2x.Cmp(dx) != 0 || r2y.Cmp(dy) != 0 {
		t.Fatalf("2*G scalar mul != double: (%s,%s) != (%s,%s)", r2x, r2y, dx, dy)
	}
}

// TestBN254AddPrecompile tests the precompile interface for BN254Add.
func TestBN254AddPrecompile(t *testing.T) {
	// Add two identity points -> identity.
	input := make([]byte, 128)
	out, err := BN254Add(input)
	if err != nil {
		t.Fatalf("BN254Add(0+0): %v", err)
	}
	if !bytes.Equal(out, make([]byte, 64)) {
		t.Fatalf("0+0 should be identity, got %x", out)
	}

	// G + 0 = G.
	input2 := make([]byte, 128)
	input2[31] = 1 // x=1
	input2[63] = 2 // y=2
	out2, err := BN254Add(input2)
	if err != nil {
		t.Fatalf("BN254Add(G+0): %v", err)
	}
	if out2[31] != 1 || out2[63] != 2 {
		t.Fatalf("G+0 = %x, want G=(1,2)", out2)
	}

	// Short input padding.
	shortInput := []byte{0x01}
	_, err = BN254Add(shortInput)
	// x=1, y=0 is not on the curve (1^3 + 3 = 4, sqrt(4) = 2, not 0)
	if err == nil {
		t.Fatal("expected error for invalid point (1, 0)")
	}
}

// TestBN254ScalarMulPrecompile tests the precompile interface for BN254ScalarMul.
func TestBN254ScalarMulPrecompile(t *testing.T) {
	// 0 * G = identity
	input := make([]byte, 96)
	input[31] = 1 // x=1
	input[63] = 2 // y=2
	// scalar = 0
	out, err := BN254ScalarMul(input)
	if err != nil {
		t.Fatalf("BN254ScalarMul(0*G): %v", err)
	}
	if !bytes.Equal(out, make([]byte, 64)) {
		t.Fatalf("0*G should be identity, got %x", out)
	}

	// 1 * G = G
	input2 := make([]byte, 96)
	input2[31] = 1 // x=1
	input2[63] = 2 // y=2
	input2[95] = 1 // scalar=1
	out2, err := BN254ScalarMul(input2)
	if err != nil {
		t.Fatalf("BN254ScalarMul(1*G): %v", err)
	}
	if out2[31] != 1 || out2[63] != 2 {
		t.Fatalf("1*G = %x, want G=(1,2)", out2)
	}

	// 2 * G
	input3 := make([]byte, 96)
	input3[31] = 1 // x=1
	input3[63] = 2 // y=2
	input3[95] = 2 // scalar=2
	out3, err := BN254ScalarMul(input3)
	if err != nil {
		t.Fatalf("BN254ScalarMul(2*G): %v", err)
	}
	// Verify 2*G is on the curve.
	x3 := new(big.Int).SetBytes(out3[0:32])
	y3 := new(big.Int).SetBytes(out3[32:64])
	if !g1IsOnCurve(x3, y3) {
		t.Fatalf("2*G = (%s, %s) is not on the curve", x3, y3)
	}
}

// TestBN254ScalarMul_KnownVector verifies against known 2*G coordinates.
func TestBN254ScalarMul_KnownVector(t *testing.T) {
	// 2*G on BN254 is known to be:
	// x = 1368015179489954701390400359078579693043519447331113978918064868415326638035
	// y = 9918110051302171585080402603319702774565515993150576347155970296011118125764
	gen := G1Generator()
	r := G1ScalarMul(gen, big.NewInt(2))
	rx, ry := r.g1ToAffine()

	wantX, _ := new(big.Int).SetString("1368015179489954701390400359078579693043519447331113978918064868415326638035", 10)
	wantY, _ := new(big.Int).SetString("9918110051302171585080402603319702774565515993150576347155970296011118125764", 10)

	if rx.Cmp(wantX) != 0 || ry.Cmp(wantY) != 0 {
		t.Fatalf("2*G = (%s, %s), want (%s, %s)", rx, ry, wantX, wantY)
	}
}

// TestBN254AddKnownVector verifies G + G = 2G using the precompile interface.
func TestBN254AddKnownVector(t *testing.T) {
	input := make([]byte, 128)
	input[31] = 1  // x1=1
	input[63] = 2  // y1=2
	input[95] = 1  // x2=1
	input[127] = 2 // y2=2

	out, err := BN254Add(input)
	if err != nil {
		t.Fatalf("BN254Add(G+G): %v", err)
	}

	x := new(big.Int).SetBytes(out[0:32])
	y := new(big.Int).SetBytes(out[32:64])

	wantX, _ := new(big.Int).SetString("1368015179489954701390400359078579693043519447331113978918064868415326638035", 10)
	wantY, _ := new(big.Int).SetString("9918110051302171585080402603319702774565515993150576347155970296011118125764", 10)

	if x.Cmp(wantX) != 0 || y.Cmp(wantY) != 0 {
		t.Fatalf("G+G = (%s, %s), want (%s, %s)", x, y, wantX, wantY)
	}
}

// TestBN254InvalidPoints tests that invalid points are rejected.
func TestBN254InvalidPoints(t *testing.T) {
	// Point not on curve for add.
	input := make([]byte, 128)
	input[31] = 1 // x=1
	input[63] = 3 // y=3 (not on curve)
	_, err := BN254Add(input)
	if err == nil {
		t.Fatal("expected error for invalid point in add")
	}

	// Point not on curve for scalar mul.
	input2 := make([]byte, 96)
	input2[31] = 1 // x=1
	input2[63] = 3 // y=3
	input2[95] = 1 // scalar=1
	_, err = BN254ScalarMul(input2)
	if err == nil {
		t.Fatal("expected error for invalid point in scalar mul")
	}
}

// TestBN254PairingEmptyInput tests that empty pairing input returns true (1).
func TestBN254PairingEmptyInput(t *testing.T) {
	out, err := BN254PairingCheck(nil)
	if err != nil {
		t.Fatalf("empty pairing: %v", err)
	}
	if out[31] != 1 {
		t.Fatalf("empty pairing should return 1, got %x", out)
	}
}

// TestBN254PairingInvalidLength tests that non-192-multiple lengths are rejected.
func TestBN254PairingInvalidLength(t *testing.T) {
	_, err := BN254PairingCheck(make([]byte, 100))
	if err == nil {
		t.Fatal("expected error for invalid pairing input length")
	}
}

// TestG2OnCurve verifies the G2 generator is on the twist curve.
func TestG2OnCurve(t *testing.T) {
	gen := G2Generator()
	x, y := gen.g2ToAffine()
	if !g2IsOnCurve(x, y) {
		t.Fatal("G2 generator should be on the twist curve")
	}
}

// TestBN254PairingWithIdentityG1 tests pairing with G1 identity.
func TestBN254PairingWithIdentityG1(t *testing.T) {
	// e(0, Q) = 1 for any Q.
	// Input: G1=(0,0), G2=generator
	input := make([]byte, 192)
	// G1 is (0,0) - identity.
	// G2 generator coordinates (big-endian 32-byte each):
	// x_imag, x_real, y_imag, y_real
	xImag := g2GenXa1.Bytes()
	xReal := g2GenXa0.Bytes()
	yImag := g2GenYa1.Bytes()
	yReal := g2GenYa0.Bytes()
	copy(input[96-len(xImag):96], xImag)
	copy(input[128-len(xReal):128], xReal)
	copy(input[160-len(yImag):160], yImag)
	copy(input[192-len(yReal):192], yReal)

	out, err := BN254PairingCheck(input)
	if err != nil {
		t.Fatalf("pairing with identity G1: %v", err)
	}
	if out[31] != 1 {
		t.Fatalf("e(0, Q) should be 1, got %x", out)
	}
}

// TestBN254PairingWithIdentityG2 tests pairing with G2 identity.
func TestBN254PairingWithIdentityG2(t *testing.T) {
	// e(P, 0) = 1 for any P.
	input := make([]byte, 192)
	input[31] = 1 // G1 x=1
	input[63] = 2 // G1 y=2
	// G2 is (0,0,0,0) - identity (all zeros already).

	out, err := BN254PairingCheck(input)
	if err != nil {
		t.Fatalf("pairing with identity G2: %v", err)
	}
	if out[31] != 1 {
		t.Fatalf("e(P, 0) should be 1, got %x", out)
	}
}

// TestBN254PairingBilinearity tests e(a*P, b*Q) = e(P, Q)^(ab).
// We check e(2*G1, G2) * e(G1, -G2)^2 = 1
// i.e., e(2G, Q) = e(G, Q)^2, which means e(2G, Q) * e(G, -Q)^2 = 1.
// Simpler: e(G, Q) * e(-G, Q) = 1.
func TestBN254PairingBasicBilinearity(t *testing.T) {
	// e(G, Q) * e(-G, Q) should equal 1.
	// This is because e(G, Q) * e(-G, Q) = e(G + (-G), Q) = e(0, Q) = 1.
	input := make([]byte, 384) // 2 pairs

	// Pair 1: (G1, G2)
	input[31] = 1 // G1 x=1
	input[63] = 2 // G1 y=2
	xImag := g2GenXa1.Bytes()
	xReal := g2GenXa0.Bytes()
	yImag := g2GenYa1.Bytes()
	yReal := g2GenYa0.Bytes()
	copy(input[96-len(xImag):96], xImag)
	copy(input[128-len(xReal):128], xReal)
	copy(input[160-len(yImag):160], yImag)
	copy(input[192-len(yReal):192], yReal)

	// Pair 2: (-G1, G2)
	negY := fpNeg(big.NewInt(2)) // -2 mod p
	input[192+31] = 1            // -G1 x=1
	negYBytes := negY.Bytes()
	copy(input[192+64-len(negYBytes):192+64], negYBytes) // -G1 y = p-2
	copy(input[192+96-len(xImag):192+96], xImag)
	copy(input[192+128-len(xReal):192+128], xReal)
	copy(input[192+160-len(yImag):192+160], yImag)
	copy(input[192+192-len(yReal):192+192], yReal)

	out, err := BN254PairingCheck(input)
	if err != nil {
		t.Fatalf("pairing bilinearity check: %v", err)
	}
	if out[31] != 1 {
		t.Fatalf("e(G,Q)*e(-G,Q) should be 1, got %x", out)
	}
}

// TestBN254AddPrecompileHex tests with go-ethereum test vectors.
func TestBN254AddPrecompileHex(t *testing.T) {
	// Test vector: G + G = 2G
	// G = (1, 2), 2G is known.
	input := make([]byte, 128)
	input[31] = 1
	input[63] = 2
	input[95] = 1
	input[127] = 2

	out, err := BN254Add(input)
	if err != nil {
		t.Fatalf("add G+G: %v", err)
	}

	wantHex := "030644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd3" +
		"15ed738c0e0a7c92e7845f96b2ae9c0a68a6a449e3538fc7ff3ebf7a5a18a2c4"
	want, _ := hex.DecodeString(wantHex)
	if !bytes.Equal(out, want) {
		t.Fatalf("G+G:\n  got  %x\n  want %s", out, wantHex)
	}
}

// TestBN254ScalarMulPrecompileHex tests scalar mul with known hex vectors.
func TestBN254ScalarMulPrecompileHex(t *testing.T) {
	// 2 * G = 2G
	input := make([]byte, 96)
	input[31] = 1
	input[63] = 2
	input[95] = 2

	out, err := BN254ScalarMul(input)
	if err != nil {
		t.Fatalf("scalarMul 2*G: %v", err)
	}

	wantHex := "030644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd3" +
		"15ed738c0e0a7c92e7845f96b2ae9c0a68a6a449e3538fc7ff3ebf7a5a18a2c4"
	want, _ := hex.DecodeString(wantHex)
	if !bytes.Equal(out, want) {
		t.Fatalf("2*G:\n  got  %x\n  want %s", out, wantHex)
	}
}

// TestFp2Arithmetic tests basic F_p^2 operations.
func TestFp2Arithmetic(t *testing.T) {
	a := &fp2{a0: big.NewInt(3), a1: big.NewInt(4)}
	b := &fp2{a0: big.NewInt(5), a1: big.NewInt(7)}

	// (3+4i)(5+7i) = 15+21i+20i+28i^2 = 15+41i-28 = -13+41i
	c := fp2Mul(a, b)
	want0 := fpSub(big.NewInt(15), big.NewInt(28)) // -13 mod p
	want1 := big.NewInt(41)

	if c.a0.Cmp(want0) != 0 || c.a1.Cmp(want1) != 0 {
		t.Fatalf("fp2Mul = (%s, %s), want (%s, 41)", c.a0, c.a1, want0)
	}

	// Inverse: a * a^(-1) = 1
	aInv := fp2Inv(a)
	prod := fp2Mul(a, aInv)
	if prod.a0.Cmp(big.NewInt(1)) != 0 || prod.a1.Sign() != 0 {
		t.Fatalf("a * a^(-1) = (%s, %s), want (1, 0)", prod.a0, prod.a1)
	}
}

// TestBN254PairingNonTrivialFail tests that an incorrect pairing returns 0.
func TestBN254PairingNonTrivialFail(t *testing.T) {
	// e(G, Q) alone (without cancellation) should not be 1.
	input := make([]byte, 192)
	input[31] = 1 // G1 x=1
	input[63] = 2 // G1 y=2
	xImag := g2GenXa1.Bytes()
	xReal := g2GenXa0.Bytes()
	yImag := g2GenYa1.Bytes()
	yReal := g2GenYa0.Bytes()
	copy(input[96-len(xImag):96], xImag)
	copy(input[128-len(xReal):128], xReal)
	copy(input[160-len(yImag):160], yImag)
	copy(input[192-len(yReal):192], yReal)

	out, err := BN254PairingCheck(input)
	if err != nil {
		t.Fatalf("pairing single: %v", err)
	}
	if out[31] != 0 {
		t.Fatalf("e(G,Q) alone should NOT be 1, got %x", out)
	}
}

// TestBN254ScalarMulLargeScalar tests scalar mul with a large scalar.
func TestBN254ScalarMulLargeScalar(t *testing.T) {
	// Use n-1 as scalar, should give -G.
	gen := G1Generator()
	nMinus1 := new(big.Int).Sub(bn254N, big.NewInt(1))
	r := G1ScalarMul(gen, nMinus1)
	rx, ry := r.g1ToAffine()

	// -G = (1, p-2)
	if rx.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("(n-1)*G x = %s, want 1", rx)
	}
	negY := new(big.Int).Sub(bn254P, big.NewInt(2))
	if ry.Cmp(negY) != 0 {
		t.Fatalf("(n-1)*G y = %s, want %s", ry, negY)
	}
}

// TestBN254CoordinatesOutOfRange tests that coordinates >= p are rejected.
func TestBN254CoordinatesOutOfRange(t *testing.T) {
	// x = p (out of range)
	input := make([]byte, 128)
	pBytes := bn254P.Bytes()
	copy(input[32-len(pBytes):32], pBytes) // x = p
	// y = 0
	_, err := BN254Add(input)
	if err == nil {
		t.Fatal("expected error for x >= p")
	}
}

// --- Ethereum test suite vectors (go-ethereum precompile test data) ---

// bn254TestVector holds a precompile test vector.
type bn254TestVector struct {
	Name     string
	Input    string
	Expected string
}

// decodeHexInput decodes a hex string, returning nil for empty strings.
func decodeHexInput(s string) []byte {
	if s == "" {
		return nil
	}
	b, _ := hex.DecodeString(s)
	return b
}

// TestBN254Add_EthTestVectors runs the full set of BN254 addition test vectors
// from the Ethereum precompile test suite.
func TestBN254Add_EthTestVectors(t *testing.T) {
	vectors := []bn254TestVector{
		{
			Name:     "chfast1",
			Input:    "18b18acfb4c2c30276db5411368e7185b311dd124691610c5d3b74034e093dc9063c909c4720840cb5134cb9f59fa749755796819658d32efc0d288198f3726607c2b7f58a84bd6145f00c9c2bc0bb1a187f20ff2c92963a88019e7c6a014eed06614e20c147e940f2d70da3f74c9a17df361706a4485c742bd6788478fa17d7",
			Expected: "2243525c5efd4b9c3d3c45ac0ca3fe4dd85e830a4ce6b65fa1eeaee202839703301d1d33be6da8e509df21cc35964723180eed7532537db9ae5e7d48f195c915",
		},
		{
			Name:     "chfast2",
			Input:    "2243525c5efd4b9c3d3c45ac0ca3fe4dd85e830a4ce6b65fa1eeaee202839703301d1d33be6da8e509df21cc35964723180eed7532537db9ae5e7d48f195c91518b18acfb4c2c30276db5411368e7185b311dd124691610c5d3b74034e093dc9063c909c4720840cb5134cb9f59fa749755796819658d32efc0d288198f37266",
			Expected: "2bd3e6d0f3b142924f5ca7b49ce5b9d54c4703d7ae5648e61d02268b1a0a9fb721611ce0a6af85915e2f1d70300909ce2e49dfad4a4619c8390cae66cefdb204",
		},
		{
			Name:     "cdetrio1_zeros",
			Input:    "0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
			Expected: "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			Name:     "cdetrio4_empty",
			Input:    "",
			Expected: "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			Name:     "cdetrio6_zero_plus_G",
			Input:    "0000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002",
			Expected: "00000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002",
		},
		{
			Name:     "cdetrio11_G_plus_G",
			Input:    "0000000000000000000000000000000000000000000000000000000000000001000000000000000000000000000000000000000000000000000000000000000200000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002",
			Expected: "030644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd315ed738c0e0a7c92e7845f96b2ae9c0a68a6a449e3538fc7ff3ebf7a5a18a2c4",
		},
		{
			Name:     "cdetrio13_non_trivial",
			Input:    "17c139df0efee0f766bc0204762b774362e4ded88953a39ce849a8a7fa163fa901e0559bacb160664764a357af8a9fe70baa9258e0b959273ffc5718c6d4cc7c039730ea8dff1254c0fee9c0ea777d29a9c710b7e616683f194f18c43b43b869073a5ffcc6fc7a28c30723d6e58ce577356982d65b833a5a5c15bf9024b43d98",
			Expected: "15bf2bb17880144b5d1cd2b1f46eff9d617bffd1ca57c37fb5a49bd84e53cf66049c797f9ce0d17083deb32b5e36f2ea2a212ee036598dd7624c168993d1355f",
		},
		{
			Name:     "cdetrio14_P_plus_negP",
			Input:    "17c139df0efee0f766bc0204762b774362e4ded88953a39ce849a8a7fa163fa901e0559bacb160664764a357af8a9fe70baa9258e0b959273ffc5718c6d4cc7c17c139df0efee0f766bc0204762b774362e4ded88953a39ce849a8a7fa163fa92e83f8d734803fc370eba25ed1f6b8768bd6d83887b87165fc2434fe11a830cb00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
			Expected: "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tc := range vectors {
		t.Run(tc.Name, func(t *testing.T) {
			input := decodeHexInput(tc.Input)
			expected, _ := hex.DecodeString(tc.Expected)

			out, err := BN254Add(input)
			if err != nil {
				t.Fatalf("BN254Add error: %v", err)
			}
			if !bytes.Equal(out, expected) {
				t.Fatalf("BN254Add:\n  got  %x\n  want %x", out, expected)
			}
		})
	}
}

// TestBN254ScalarMul_EthTestVectors runs the full set of BN254 scalar multiplication
// test vectors from the Ethereum precompile test suite.
func TestBN254ScalarMul_EthTestVectors(t *testing.T) {
	vectors := []bn254TestVector{
		{
			Name:     "chfast1",
			Input:    "2bd3e6d0f3b142924f5ca7b49ce5b9d54c4703d7ae5648e61d02268b1a0a9fb721611ce0a6af85915e2f1d70300909ce2e49dfad4a4619c8390cae66cefdb20400000000000000000000000000000000000000000000000011138ce750fa15c2",
			Expected: "070a8d6a982153cae4be29d434e8faef8a47b274a053f5a4ee2a6c9c13c31e5c031b8ce914eba3a9ffb989f9cdd5b0f01943074bf4f0f315690ec3cec6981afc",
		},
		{
			Name:     "chfast2",
			Input:    "070a8d6a982153cae4be29d434e8faef8a47b274a053f5a4ee2a6c9c13c31e5c031b8ce914eba3a9ffb989f9cdd5b0f01943074bf4f0f315690ec3cec6981afc30644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd46",
			Expected: "025a6f4181d2b4ea8b724290ffb40156eb0adb514c688556eb79cdea0752c2bb2eff3f31dea215f1eb86023a133a996eb6300b44da664d64251d05381bb8a02e",
		},
		{
			Name:     "chfast3",
			Input:    "025a6f4181d2b4ea8b724290ffb40156eb0adb514c688556eb79cdea0752c2bb2eff3f31dea215f1eb86023a133a996eb6300b44da664d64251d05381bb8a02e183227397098d014dc2822db40c0ac2ecbc0b548b438e5469e10460b6c3e7ea3",
			Expected: "14789d0d4a730b354403b5fac948113739e276c23e0258d8596ee72f9cd9d3230af18a63153e0ec25ff9f2951dd3fa90ed0197bfef6e2a1a62b5095b9d2b4a27",
		},
		{
			Name:     "cdetrio1_max_scalar",
			Input:    "1a87b0584ce92f4593d161480614f2989035225609f08058ccfa3d0f940febe31a2f3c951f6dadcc7ee9007dff81504b0fcd6d7cf59996efdc33d92bf7f9f8f6ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			Expected: "2cde5879ba6f13c0b5aa4ef627f159a3347df9722efce88a9afbb20b763b4c411aa7e43076f6aee272755a7f9b84832e71559ba0d2e0b17d5f9f01755e5b0d11",
		},
		{
			Name:     "cdetrio2_order_scalar",
			Input:    "1a87b0584ce92f4593d161480614f2989035225609f08058ccfa3d0f940febe31a2f3c951f6dadcc7ee9007dff81504b0fcd6d7cf59996efdc33d92bf7f9f8f630644e72e131a029b85045b68181585d2833e84879b9709143e1f593f0000000",
			Expected: "1a87b0584ce92f4593d161480614f2989035225609f08058ccfa3d0f940febe3163511ddc1c3f25d396745388200081287b3fd1472d8339d5fecb2eae0830451",
		},
		{
			Name:     "cdetrio4_scalar_9",
			Input:    "1a87b0584ce92f4593d161480614f2989035225609f08058ccfa3d0f940febe31a2f3c951f6dadcc7ee9007dff81504b0fcd6d7cf59996efdc33d92bf7f9f8f60000000000000000000000000000000000000000000000000000000000000009",
			Expected: "1dbad7d39dbc56379f78fac1bca147dc8e66de1b9d183c7b167351bfe0aeab742cd757d51289cd8dbd0acf9e673ad67d0f0a89f912af47ed1be53664f5692575",
		},
		{
			Name:     "cdetrio5_scalar_1",
			Input:    "1a87b0584ce92f4593d161480614f2989035225609f08058ccfa3d0f940febe31a2f3c951f6dadcc7ee9007dff81504b0fcd6d7cf59996efdc33d92bf7f9f8f60000000000000000000000000000000000000000000000000000000000000001",
			Expected: "1a87b0584ce92f4593d161480614f2989035225609f08058ccfa3d0f940febe31a2f3c951f6dadcc7ee9007dff81504b0fcd6d7cf59996efdc33d92bf7f9f8f6",
		},
		{
			Name:     "cdetrio6_max_scalar_pt2",
			Input:    "17c139df0efee0f766bc0204762b774362e4ded88953a39ce849a8a7fa163fa901e0559bacb160664764a357af8a9fe70baa9258e0b959273ffc5718c6d4cc7cffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			Expected: "29e587aadd7c06722aabba753017c093f70ba7eb1f1c0104ec0564e7e3e21f6022b1143f6a41008e7755c71c3d00b6b915d386de21783ef590486d8afa8453b1",
		},
		{
			Name:     "cdetrio11_max_scalar_pt3",
			Input:    "039730ea8dff1254c0fee9c0ea777d29a9c710b7e616683f194f18c43b43b869073a5ffcc6fc7a28c30723d6e58ce577356982d65b833a5a5c15bf9024b43d98ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
			Expected: "00a1a234d08efaa2616607e31eca1980128b00b415c845ff25bba3afcb81dc00242077290ed33906aeb8e42fd98c41bcb9057ba03421af3f2d08cfc441186024",
		},
		{
			Name:     "zeroScalar",
			Input:    "039730ea8dff1254c0fee9c0ea777d29a9c710b7e616683f194f18c43b43b869073a5ffcc6fc7a28c30723d6e58ce577356982d65b833a5a5c15bf9024b43d980000000000000000000000000000000000000000000000000000000000000000",
			Expected: "00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000",
		},
	}

	for _, tc := range vectors {
		t.Run(tc.Name, func(t *testing.T) {
			input := decodeHexInput(tc.Input)
			expected, _ := hex.DecodeString(tc.Expected)

			out, err := BN254ScalarMul(input)
			if err != nil {
				t.Fatalf("BN254ScalarMul error: %v", err)
			}
			if !bytes.Equal(out, expected) {
				t.Fatalf("BN254ScalarMul:\n  got  %x\n  want %x", out, expected)
			}
		})
	}
}

// TestBN254Pairing_EthTestVectors runs pairing check test vectors from the
// Ethereum precompile test suite (go-ethereum).
func TestBN254Pairing_EthTestVectors(t *testing.T) {
	vectors := []bn254TestVector{
		{
			Name:     "jeff1",
			Input:    "1c76476f4def4bb94541d57ebba1193381ffa7aa76ada664dd31c16024c43f593034dd2920f673e204fee2811c678745fc819b55d3e9d294e45c9b03a76aef41209dd15ebff5d46c4bd888e51a93cf99a7329636c63514396b4a452003a35bf704bf11ca01483bfa8b34b43561848d28905960114c8ac04049af4b6315a416782bb8324af6cfc93537a2ad1a445cfd0ca2a71acd7ac41fadbf933c2a51be344d120a2a4cf30c1bf9845f20c6fe39e07ea2cce61f0c9bb048165fe5e4de877550111e129f1cf1097710d41c4ac70fcdfa5ba2023c6ff1cbeac322de49d1b6df7c2032c61a830e3c17286de9462bf242fca2883585b93870a73853face6a6bf411198e9393920d483a7260bfb731fb5d25f1aa493335a9e71297e485b7aef312c21800deef121f1e76426a00665e5c4479674322d4f75edadd46debd5cd992f6ed090689d0585ff075ec9e99ad690c3395bc4b313370b38ef355acdadcd122975b12c85ea5db8c6deb4aab71808dcb408fe3d1e7690c43d37b4ce6cc0166fa7daa",
			Expected: "0000000000000000000000000000000000000000000000000000000000000001",
		},
		{
			Name:     "jeff2",
			Input:    "2eca0c7238bf16e83e7a1e6c5d49540685ff51380f309842a98561558019fc0203d3260361bb8451de5ff5ecd17f010ff22f5c31cdf184e9020b06fa5997db841213d2149b006137fcfb23036606f848d638d576a120ca981b5b1a5f9300b3ee2276cf730cf493cd95d64677bbb75fc42db72513a4c1e387b476d056f80aa75f21ee6226d31426322afcda621464d0611d226783262e21bb3bc86b537e986237096df1f82dff337dd5972e32a8ad43e28a78a96a823ef1cd4debe12b6552ea5f06967a1237ebfeca9aaae0d6d0bab8e28c198c5a339ef8a2407e31cdac516db922160fa257a5fd5b280642ff47b65eca77e626cb685c84fa6d3b6882a283ddd1198e9393920d483a7260bfb731fb5d25f1aa493335a9e71297e485b7aef312c21800deef121f1e76426a00665e5c4479674322d4f75edadd46debd5cd992f6ed090689d0585ff075ec9e99ad690c3395bc4b313370b38ef355acdadcd122975b12c85ea5db8c6deb4aab71808dcb408fe3d1e7690c43d37b4ce6cc0166fa7daa",
			Expected: "0000000000000000000000000000000000000000000000000000000000000001",
		},
		{
			Name:     "jeff3",
			Input:    "0f25929bcb43d5a57391564615c9e70a992b10eafa4db109709649cf48c50dd216da2f5cb6be7a0aa72c440c53c9bbdfec6c36c7d515536431b3a865468acbba2e89718ad33c8bed92e210e81d1853435399a271913a6520736a4729cf0d51eb01a9e2ffa2e92599b68e44de5bcf354fa2642bd4f26b259daa6f7ce3ed57aeb314a9a87b789a58af499b314e13c3d65bede56c07ea2d418d6874857b70763713178fb49a2d6cd347dc58973ff49613a20757d0fcc22079f9abd10c3baee245901b9e027bd5cfc2cb5db82d4dc9677ac795ec500ecd47deee3b5da006d6d049b811d7511c78158de484232fc68daf8a45cf217d1c2fae693ff5871e8752d73b21198e9393920d483a7260bfb731fb5d25f1aa493335a9e71297e485b7aef312c21800deef121f1e76426a00665e5c4479674322d4f75edadd46debd5cd992f6ed090689d0585ff075ec9e99ad690c3395bc4b313370b38ef355acdadcd122975b12c85ea5db8c6deb4aab71808dcb408fe3d1e7690c43d37b4ce6cc0166fa7daa",
			Expected: "0000000000000000000000000000000000000000000000000000000000000001",
		},
		{
			Name:     "jeff6_fail",
			Input:    "1c76476f4def4bb94541d57ebba1193381ffa7aa76ada664dd31c16024c43f593034dd2920f673e204fee2811c678745fc819b55d3e9d294e45c9b03a76aef41209dd15ebff5d46c4bd888e51a93cf99a7329636c63514396b4a452003a35bf704bf11ca01483bfa8b34b43561848d28905960114c8ac04049af4b6315a416782bb8324af6cfc93537a2ad1a445cfd0ca2a71acd7ac41fadbf933c2a51be344d120a2a4cf30c1bf9845f20c6fe39e07ea2cce61f0c9bb048165fe5e4de877550111e129f1cf1097710d41c4ac70fcdfa5ba2023c6ff1cbeac322de49d1b6df7c103188585e2364128fe25c70558f1560f4f9350baf3959e603cc91486e110936198e9393920d483a7260bfb731fb5d25f1aa493335a9e71297e485b7aef312c21800deef121f1e76426a00665e5c4479674322d4f75edadd46debd5cd992f6ed090689d0585ff075ec9e99ad690c3395bc4b313370b38ef355acdadcd122975b12c85ea5db8c6deb4aab71808dcb408fe3d1e7690c43d37b4ce6cc0166fa7daa",
			Expected: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			Name:     "empty_data",
			Input:    "",
			Expected: "0000000000000000000000000000000000000000000000000000000000000001",
		},
		{
			Name:     "one_point_fail",
			Input:    "00000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002198e9393920d483a7260bfb731fb5d25f1aa493335a9e71297e485b7aef312c21800deef121f1e76426a00665e5c4479674322d4f75edadd46debd5cd992f6ed090689d0585ff075ec9e99ad690c3395bc4b313370b38ef355acdadcd122975b12c85ea5db8c6deb4aab71808dcb408fe3d1e7690c43d37b4ce6cc0166fa7daa",
			Expected: "0000000000000000000000000000000000000000000000000000000000000000",
		},
		{
			Name:     "two_point_match_2",
			Input:    "00000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002198e9393920d483a7260bfb731fb5d25f1aa493335a9e71297e485b7aef312c21800deef121f1e76426a00665e5c4479674322d4f75edadd46debd5cd992f6ed090689d0585ff075ec9e99ad690c3395bc4b313370b38ef355acdadcd122975b12c85ea5db8c6deb4aab71808dcb408fe3d1e7690c43d37b4ce6cc0166fa7daa00000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002198e9393920d483a7260bfb731fb5d25f1aa493335a9e71297e485b7aef312c21800deef121f1e76426a00665e5c4479674322d4f75edadd46debd5cd992f6ed275dc4a288d1afb3cbb1ac09187524c7db36395df7be3b99e673b13a075a65ec1d9befcd05a5323e6da4d435f3b617cdb3af83285c2df711ef39c01571827f9d",
			Expected: "0000000000000000000000000000000000000000000000000000000000000001",
		},
		{
			Name:     "two_point_match_3",
			Input:    "00000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002203e205db4f19b37b60121b83a7333706db86431c6d835849957ed8c3928ad7927dc7234fd11d3e8c36c59277c3e6f149d5cd3cfa9a62aee49f8130962b4b3b9195e8aa5b7827463722b8c153931579d3505566b4edf48d498e185f0509de15204bb53b8977e5f92a0bc372742c4830944a59b4fe6b1c0466e2a6dad122b5d2e030644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd31a76dae6d3272396d0cbe61fced2bc532edac647851e3ac53ce1cc9c7e645a83198e9393920d483a7260bfb731fb5d25f1aa493335a9e71297e485b7aef312c21800deef121f1e76426a00665e5c4479674322d4f75edadd46debd5cd992f6ed090689d0585ff075ec9e99ad690c3395bc4b313370b38ef355acdadcd122975b12c85ea5db8c6deb4aab71808dcb408fe3d1e7690c43d37b4ce6cc0166fa7daa",
			Expected: "0000000000000000000000000000000000000000000000000000000000000001",
		},
		{
			Name:     "two_point_match_4",
			Input:    "105456a333e6d636854f987ea7bb713dfd0ae8371a72aea313ae0c32c0bf10160cf031d41b41557f3e7e3ba0c51bebe5da8e6ecd855ec50fc87efcdeac168bcc0476be093a6d2b4bbf907172049874af11e1b6267606e00804d3ff0037ec57fd3010c68cb50161b7d1d96bb71edfec9880171954e56871abf3d93cc94d745fa114c059d74e5b6c4ec14ae5864ebe23a71781d86c29fb8fb6cce94f70d3de7a2101b33461f39d9e887dbb100f170a2345dde3c07e256d1dfa2b657ba5cd030427000000000000000000000000000000000000000000000000000000000000000100000000000000000000000000000000000000000000000000000000000000021a2c3013d2ea92e13c800cde68ef56a294b883f6ac35d25f587c09b1b3c635f7290158a80cd3d66530f74dc94c94adb88f5cdb481acca997b6e60071f08a115f2f997f3dbd66a7afe07fe7862ce239edba9e05c5afff7f8a1259c9733b2dfbb929d1691530ca701b4a106054688728c9972c8512e9789e9567aae23e302ccd75",
			Expected: "0000000000000000000000000000000000000000000000000000000000000001",
		},
	}

	for _, tc := range vectors {
		t.Run(tc.Name, func(t *testing.T) {
			input := decodeHexInput(tc.Input)
			expected, _ := hex.DecodeString(tc.Expected)

			out, err := BN254PairingCheck(input)
			if err != nil {
				t.Fatalf("BN254PairingCheck error: %v", err)
			}
			if !bytes.Equal(out, expected) {
				t.Fatalf("BN254PairingCheck:\n  got  %x\n  want %x", out, expected)
			}
		})
	}
}

// TestG2AddSubgroup verifies G2 generator operations.
func TestG2AddSubgroup(t *testing.T) {
	gen := G2Generator()

	// 2*G2 via doubling.
	d := g2Double(gen)
	if d.g2IsInfinity() {
		t.Fatal("2*G2 should not be infinity")
	}
	dx, dy := d.g2ToAffine()
	if !g2IsOnCurve(dx, dy) {
		t.Fatal("2*G2 should be on the twist curve")
	}

	// G2 + G2 via addition should equal 2*G2.
	a := g2Add(gen, gen)
	ax, ay := a.g2ToAffine()
	if !dx.equal(ax) || !dy.equal(ay) {
		t.Fatal("G2+G2 != 2*G2")
	}

	// G2 + (-G2) = infinity.
	neg := g2Neg(gen)
	inf := g2Add(gen, neg)
	if !inf.g2IsInfinity() {
		t.Fatal("G2 + (-G2) should be infinity")
	}
}

// TestG2ScalarMul verifies scalar multiplication in G2.
func TestG2ScalarMul(t *testing.T) {
	gen := G2Generator()

	// 1*G2 = G2.
	r := g2ScalarMul(gen, big.NewInt(1))
	rx, ry := r.g2ToAffine()
	gx, gy := gen.g2ToAffine()
	if !rx.equal(gx) || !ry.equal(gy) {
		t.Fatal("1*G2 != G2")
	}

	// 0*G2 = infinity.
	r0 := g2ScalarMul(gen, big.NewInt(0))
	if !r0.g2IsInfinity() {
		t.Fatal("0*G2 should be infinity")
	}

	// n*G2 = infinity (curve order).
	rn := g2ScalarMul(gen, bn254N)
	if !rn.g2IsInfinity() {
		t.Fatal("n*G2 should be infinity")
	}
}

// TestFpArithmetic verifies base field arithmetic properties.
func TestFpArithmetic(t *testing.T) {
	a := big.NewInt(17)
	b := big.NewInt(23)

	// Commutativity: a+b = b+a.
	if fpAdd(a, b).Cmp(fpAdd(b, a)) != 0 {
		t.Fatal("fpAdd not commutative")
	}
	if fpMul(a, b).Cmp(fpMul(b, a)) != 0 {
		t.Fatal("fpMul not commutative")
	}

	// a * a^(-1) = 1.
	inv := fpInv(a)
	if fpMul(a, inv).Cmp(big.NewInt(1)) != 0 {
		t.Fatal("a * a^(-1) != 1")
	}

	// a - a = 0.
	if fpSub(a, a).Sign() != 0 {
		t.Fatal("a - a != 0")
	}

	// -0 = 0.
	if fpNeg(big.NewInt(0)).Sign() != 0 {
		t.Fatal("-0 != 0")
	}

	// (p-1) + 1 = 0 mod p.
	pMinus1 := new(big.Int).Sub(bn254P, big.NewInt(1))
	if fpAdd(pMinus1, big.NewInt(1)).Sign() != 0 {
		t.Fatal("(p-1) + 1 != 0 mod p")
	}
}
