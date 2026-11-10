package bls

// BLS12-381 G2 point operations over the twist curve y^2 = x^3 + 4(1+u)
// in F_p^2 where F_p^2 = F_p[u]/(u^2+1).
//
// Points are represented in Jacobian coordinates (X, Y, Z) where
// X, Y, Z are elements of F_p^2.

import (
	"math/big"

	bls12381gnark "github.com/consensys/gnark-crypto/ecc/bls12-381"
)

// BlsG2Point represents a point on the BLS12-381 G2 twisted curve.
type BlsG2Point struct {
	x, y, z *blsFp2
}

// BLS12-381 twist curve coefficient: b' = 4(1+u)
var blsTwistB = &blsFp2{
	c0: big.NewInt(4),
	c1: big.NewInt(4),
}

// BLS12-381 G2 generator point coordinates.
var (
	blsG2GenXc0, _ = new(big.Int).SetString(
		"024aa2b2f08f0a91260805272dc51051c6e47ad4fa403b02b4510b647ae3d1770bac0326a805bbefd48056c8c121bdb8", 16)
	blsG2GenXc1, _ = new(big.Int).SetString(
		"13e02b6052719f607dacd3a088274f65596bd0d09920b61ab5da61bbdc7f5049334cf11213945d57e5ac7d055d042b7e", 16)
	blsG2GenYc0, _ = new(big.Int).SetString(
		"0ce5d527727d6e118cc9cdc6da2e351aadfd9baa8cbdd3a76d429a695160d12c923ac9cc3baca289e193548608b82801", 16)
	blsG2GenYc1, _ = new(big.Int).SetString(
		"0606c4a02ea734cc32acd2b02bc28b99cb3e287e85a763af267492ab572e99ab3f370d275cec1da1aaa9075ff05f79be", 16)
)

// BlsG2Generator returns the generator of G2.
func BlsG2Generator() *BlsG2Point {
	return &BlsG2Point{
		x: &blsFp2{c0: new(big.Int).Set(blsG2GenXc0), c1: new(big.Int).Set(blsG2GenXc1)},
		y: &blsFp2{c0: new(big.Int).Set(blsG2GenYc0), c1: new(big.Int).Set(blsG2GenYc1)},
		z: blsFp2One(),
	}
}

// BlsG2Infinity returns the point at infinity for G2.
func BlsG2Infinity() *BlsG2Point {
	return &BlsG2Point{
		x: blsFp2One(),
		y: blsFp2One(),
		z: blsFp2Zero(),
	}
}

func (p *BlsG2Point) blsG2IsInfinity() bool {
	return p.z.isZero()
}

// blsG2FromAffine creates a G2 point from affine coordinates.
func blsG2FromAffine(x, y *blsFp2) *BlsG2Point {
	if x.isZero() && y.isZero() {
		return BlsG2Infinity()
	}
	return &BlsG2Point{
		x: newBlsFp2(x.c0, x.c1),
		y: newBlsFp2(y.c0, y.c1),
		z: blsFp2One(),
	}
}

func (p *BlsG2Point) gnarkAffine() bls12381gnark.G2Affine {
	var jac bls12381gnark.G2Jac
	jac.X.A0.SetBigInt(p.x.c0)
	jac.X.A1.SetBigInt(p.x.c1)
	jac.Y.A0.SetBigInt(p.y.c0)
	jac.Y.A1.SetBigInt(p.y.c1)
	jac.Z.A0.SetBigInt(p.z.c0)
	jac.Z.A1.SetBigInt(p.z.c1)
	var result bls12381gnark.G2Affine
	result.FromJacobian(&jac)
	return result
}

func blsG2FromGnark(p bls12381gnark.G2Affine) *BlsG2Point {
	return blsG2FromAffine(
		&blsFp2{c0: p.X.A0.BigInt(new(big.Int)), c1: p.X.A1.BigInt(new(big.Int))},
		&blsFp2{c0: p.Y.A0.BigInt(new(big.Int)), c1: p.Y.A1.BigInt(new(big.Int))},
	)
}

func (p *BlsG2Point) blsG2ToAffine() (x, y *blsFp2) {
	point := p.gnarkAffine()
	return &blsFp2{point.X.A0.BigInt(new(big.Int)), point.X.A1.BigInt(new(big.Int))},
		&blsFp2{point.Y.A0.BigInt(new(big.Int)), point.Y.A1.BigInt(new(big.Int))}
}

// blsG2IsOnCurve checks if the affine point is on y^2 = x^3 + 4(1+u).
func blsG2IsOnCurve(x, y *blsFp2) bool {
	if x.isZero() && y.isZero() {
		return true
	}
	// Check coordinates are in range [0, p).
	xr0 := new(big.Int).Mod(x.c0, blsP)
	xr1 := new(big.Int).Mod(x.c1, blsP)
	yr0 := new(big.Int).Mod(y.c0, blsP)
	yr1 := new(big.Int).Mod(y.c1, blsP)
	if xr0.Cmp(x.c0) != 0 || xr1.Cmp(x.c1) != 0 {
		return false
	}
	if yr0.Cmp(y.c0) != 0 || yr1.Cmp(y.c1) != 0 {
		return false
	}
	// y^2 == x^3 + b'
	lhs := blsFp2Sqr(y)
	rhs := blsFp2Add(blsFp2Mul(blsFp2Sqr(x), x), blsTwistB)
	return lhs.equal(rhs)
}

func blsG2Add(a, b *BlsG2Point) *BlsG2Point {
	left, right := a.gnarkAffine(), b.gnarkAffine()
	var result bls12381gnark.G2Affine
	result.Add(&left, &right)
	return blsG2FromGnark(result)
}

func blsG2Double(a *BlsG2Point) *BlsG2Point {
	point := a.gnarkAffine()
	var result bls12381gnark.G2Affine
	result.Double(&point)
	return blsG2FromGnark(result)
}

// blsG2Neg returns -P.
func blsG2Neg(p *BlsG2Point) *BlsG2Point {
	if p.blsG2IsInfinity() {
		return BlsG2Infinity()
	}
	return &BlsG2Point{
		x: newBlsFp2(p.x.c0, p.x.c1),
		y: blsFp2Neg(p.y),
		z: newBlsFp2(p.z.c0, p.z.c1),
	}
}

// blsG2ScalarMul computes k*P for a G2 point using gnark-crypto for speed.
func blsG2ScalarMul(p *BlsG2Point, k *big.Int) *BlsG2Point {
	if k.Sign() == 0 || p.blsG2IsInfinity() {
		return BlsG2Infinity()
	}
	kMod := new(big.Int).Mod(k, blsR)
	if kMod.Sign() == 0 {
		return BlsG2Infinity()
	}

	ax, ay := p.blsG2ToAffine()
	var ga bls12381gnark.G2Affine
	ga.X.A0.SetBigInt(ax.c0)
	ga.X.A1.SetBigInt(ax.c1)
	ga.Y.A0.SetBigInt(ay.c0)
	ga.Y.A1.SetBigInt(ay.c1)

	var result bls12381gnark.G2Affine
	result.ScalarMultiplication(&ga, kMod)

	if result.IsInfinity() {
		return BlsG2Infinity()
	}
	rxc0 := result.X.A0.BigInt(new(big.Int))
	rxc1 := result.X.A1.BigInt(new(big.Int))
	ryc0 := result.Y.A0.BigInt(new(big.Int))
	ryc1 := result.Y.A1.BigInt(new(big.Int))
	return blsG2FromAffine(
		&blsFp2{c0: rxc0, c1: rxc1},
		&blsFp2{c0: ryc0, c1: ryc1},
	)
}

// blsG2InSubgroup checks if a point is in the r-torsion subgroup of G2.
// We check [r]*P == O.
func blsG2InSubgroup(p *BlsG2Point) bool {
	point := p.gnarkAffine()
	return point.IsInSubGroup()
}
