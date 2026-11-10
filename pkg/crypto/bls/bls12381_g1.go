package bls

// BLS12-381 G1 point operations over the curve y^2 = x^3 + 4 in F_p.
//
// Points are represented in Jacobian coordinates (X, Y, Z) where the
// affine point is (X/Z^2, Y/Z^3). The point at infinity has Z=0.

import (
	"math/big"

	bls12381gnark "github.com/consensys/gnark-crypto/ecc/bls12-381"
)

// BlsG1Point represents a point on the BLS12-381 G1 curve in Jacobian coordinates.
type BlsG1Point struct {
	x, y, z *big.Int
}

// BLS12-381 G1 generator point coordinates.
var (
	blsG1GenX, _ = new(big.Int).SetString(
		"17f1d3a73197d7942695638c4fa9ac0fc3688c4f9774b905a14e3a3f171bac586c55e83ff97a1aeffb3af00adb22c6bb", 16)
	blsG1GenY, _ = new(big.Int).SetString(
		"08b3f481e3aaa0f1a09e30ed741d8ae4fcf5e095d5d00af600db18cb2c04b3edd03cc744a2888ae40caa232946c5e7e1", 16)
)

// BlsG1Generator returns the generator of G1.
func BlsG1Generator() *BlsG1Point {
	return &BlsG1Point{
		x: new(big.Int).Set(blsG1GenX),
		y: new(big.Int).Set(blsG1GenY),
		z: big.NewInt(1),
	}
}

// BlsG1Infinity returns the point at infinity.
func BlsG1Infinity() *BlsG1Point {
	return &BlsG1Point{
		x: big.NewInt(1),
		y: big.NewInt(1),
		z: new(big.Int),
	}
}

// blsG1IsInfinity returns true if the point is the identity (Z=0).
func (p *BlsG1Point) blsG1IsInfinity() bool {
	return p.z.Sign() == 0
}

// blsG1FromAffine creates a Jacobian point from affine coordinates.
// The all-zeros encoding represents the point at infinity.
func blsG1FromAffine(x, y *big.Int) *BlsG1Point {
	if x.Sign() == 0 && y.Sign() == 0 {
		return BlsG1Infinity()
	}
	return &BlsG1Point{
		x: new(big.Int).Set(x),
		y: new(big.Int).Set(y),
		z: big.NewInt(1),
	}
}

func (p *BlsG1Point) gnarkAffine() bls12381gnark.G1Affine {
	var jac bls12381gnark.G1Jac
	jac.X.SetBigInt(p.x)
	jac.Y.SetBigInt(p.y)
	jac.Z.SetBigInt(p.z)
	var result bls12381gnark.G1Affine
	result.FromJacobian(&jac)
	return result
}

func blsG1FromGnark(p bls12381gnark.G1Affine) *BlsG1Point {
	return blsG1FromAffine(p.X.BigInt(new(big.Int)), p.Y.BigInt(new(big.Int)))
}

func (p *BlsG1Point) blsG1ToAffine() (x, y *big.Int) {
	point := p.gnarkAffine()
	return point.X.BigInt(new(big.Int)), point.Y.BigInt(new(big.Int))
}

// blsG1IsOnCurve checks if the affine point (x, y) is on y^2 = x^3 + 4.
// The point (0,0) is the identity and considered valid.
func blsG1IsOnCurve(x, y *big.Int) bool {
	if x.Sign() == 0 && y.Sign() == 0 {
		return true
	}
	// Check coordinates are in range.
	if x.Sign() < 0 || x.Cmp(blsP) >= 0 {
		return false
	}
	if y.Sign() < 0 || y.Cmp(blsP) >= 0 {
		return false
	}
	// y^2 == x^3 + 4 (mod p)
	lhs := blsFpSqr(y)
	rhs := blsFpAdd(blsFpMul(blsFpSqr(x), x), blsB)
	return lhs.Cmp(rhs) == 0
}

func blsG1Add(a, b *BlsG1Point) *BlsG1Point {
	left, right := a.gnarkAffine(), b.gnarkAffine()
	var result bls12381gnark.G1Affine
	result.Add(&left, &right)
	return blsG1FromGnark(result)
}

func blsG1Double(a *BlsG1Point) *BlsG1Point {
	point := a.gnarkAffine()
	var result bls12381gnark.G1Affine
	result.Double(&point)
	return blsG1FromGnark(result)
}

// blsG1ScalarMul computes k*P using gnark-crypto for speed.
func blsG1ScalarMul(p *BlsG1Point, k *big.Int) *BlsG1Point {
	if k.Sign() == 0 || p.blsG1IsInfinity() {
		return BlsG1Infinity()
	}

	// Reduce k modulo r.
	kMod := new(big.Int).Mod(k, blsR)
	if kMod.Sign() == 0 {
		return BlsG1Infinity()
	}

	// Convert to affine coordinates.
	ax, ay := p.blsG1ToAffine()

	var ga bls12381gnark.G1Affine
	ga.X.SetBigInt(ax)
	ga.Y.SetBigInt(ay)

	var result bls12381gnark.G1Affine
	result.ScalarMultiplication(&ga, kMod)

	if result.IsInfinity() {
		return BlsG1Infinity()
	}

	rx := result.X.BigInt(new(big.Int))
	ry := result.Y.BigInt(new(big.Int))
	return blsG1FromAffine(rx, ry)
}

// blsG1Neg returns -P.
func blsG1Neg(p *BlsG1Point) *BlsG1Point {
	if p.blsG1IsInfinity() {
		return BlsG1Infinity()
	}
	return &BlsG1Point{
		x: new(big.Int).Set(p.x),
		y: blsFpNeg(p.y),
		z: new(big.Int).Set(p.z),
	}
}

// blsG1InSubgroup checks if a point is in the r-torsion subgroup of G1.
// For BLS12-381, we use the GLV endomorphism: multiply by cofactor h
// then check that [r]*P == O (point at infinity).
// More efficiently, for BLS12-381, we check using the endomorphism
// phi: (x,y) -> (beta*x, y) where beta is a cube root of unity.
// A point P is in G1 iff phi(P) == [x]*P where x is the BLS parameter.
//
// For simplicity and correctness, we just check [r]*P == O.
func blsG1InSubgroup(p *BlsG1Point) bool {
	point := p.gnarkAffine()
	return point.IsInSubGroup()
}
