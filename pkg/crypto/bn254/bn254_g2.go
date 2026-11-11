package bn254

import (
	"math/big"

	gnarkbn "github.com/consensys/gnark-crypto/ecc/bn254"
)

// G2Point represents a point on the BN254 twist.
type G2Point struct {
	point gnarkbn.G2Affine
}

// G2Generator returns the generator of G2.
func G2Generator() *G2Point {
	_, _, _, point := gnarkbn.Generators()
	return &G2Point{point: point}
}

// G2Infinity returns the point at infinity.
func G2Infinity() *G2Point { return new(G2Point) }

func (p *G2Point) g2IsInfinity() bool { return p.point.IsInfinity() }

func g2FromAffine(x, y *fp2) *G2Point {
	point := new(G2Point)
	point.point.X.A0.SetBigInt(x.a0)
	point.point.X.A1.SetBigInt(x.a1)
	point.point.Y.A0.SetBigInt(y.a0)
	point.point.Y.A1.SetBigInt(y.a1)
	return point
}

func (p *G2Point) g2ToAffine() (x, y *fp2) {
	return &fp2{p.point.X.A0.BigInt(new(big.Int)), p.point.X.A1.BigInt(new(big.Int))},
		&fp2{p.point.Y.A0.BigInt(new(big.Int)), p.point.Y.A1.BigInt(new(big.Int))}
}

func g2IsOnCurve(x, y *fp2) bool {
	for _, coordinate := range []*big.Int{x.a0, x.a1, y.a0, y.a1} {
		if coordinate.Sign() < 0 || coordinate.Cmp(bn254P) >= 0 {
			return false
		}
	}
	return g2FromAffine(x, y).point.IsOnCurve()
}

func g2Add(a, b *G2Point) *G2Point {
	result := new(G2Point)
	result.point.Add(&a.point, &b.point)
	return result
}

func g2Double(a *G2Point) *G2Point {
	result := new(G2Point)
	result.point.Double(&a.point)
	return result
}

func g2Neg(p *G2Point) *G2Point {
	result := new(G2Point)
	result.point.Neg(&p.point)
	return result
}

func g2ScalarMul(p *G2Point, k *big.Int) *G2Point {
	result := new(G2Point)
	result.point.ScalarMultiplication(&p.point, new(big.Int).Mod(k, bn254N))
	return result
}

func g2IsOnCurveSubgroup(x, y *fp2) bool {
	return g2IsOnCurve(x, y) && g2FromAffine(x, y).point.IsInSubGroup()
}
