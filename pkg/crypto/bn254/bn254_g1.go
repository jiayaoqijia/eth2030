package bn254

import (
	"math/big"

	gnarkbn "github.com/consensys/gnark-crypto/ecc/bn254"
)

// G1Point represents a point on the BN254 G1 curve.
type G1Point struct {
	point gnarkbn.G1Affine
}

// G1Generator returns the generator of G1.
func G1Generator() *G1Point {
	_, _, point, _ := gnarkbn.Generators()
	return &G1Point{point: point}
}

// G1Infinity returns the point at infinity.
func G1Infinity() *G1Point { return new(G1Point) }

// Marshal serializes the G1 point as 64 uncompressed affine bytes (X || Y).
func (p *G1Point) Marshal() []byte {
	encoded := p.point.RawBytes()
	return encoded[:]
}

func (p *G1Point) g1IsInfinity() bool { return p.point.IsInfinity() }

func g1FromAffine(x, y *big.Int) *G1Point {
	point := new(G1Point)
	point.point.X.SetBigInt(x)
	point.point.Y.SetBigInt(y)
	return point
}

func (p *G1Point) g1ToAffine() (x, y *big.Int) {
	return p.point.X.BigInt(new(big.Int)), p.point.Y.BigInt(new(big.Int))
}

func g1IsOnCurve(x, y *big.Int) bool {
	if x.Sign() < 0 || x.Cmp(bn254P) >= 0 || y.Sign() < 0 || y.Cmp(bn254P) >= 0 {
		return false
	}
	return g1FromAffine(x, y).point.IsOnCurve()
}

func g1Add(a, b *G1Point) *G1Point {
	result := new(G1Point)
	result.point.Add(&a.point, &b.point)
	return result
}

func g1Double(a *G1Point) *G1Point {
	result := new(G1Point)
	result.point.Double(&a.point)
	return result
}

// G1ScalarMul computes k*P.
func G1ScalarMul(p *G1Point, k *big.Int) *G1Point {
	result := new(G1Point)
	result.point.ScalarMultiplication(&p.point, new(big.Int).Mod(k, bn254N))
	return result
}

func g1Neg(p *G1Point) *G1Point {
	result := new(G1Point)
	result.point.Neg(&p.point)
	return result
}
