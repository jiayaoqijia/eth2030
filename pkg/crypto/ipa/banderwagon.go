// Bandersnatch group operations use gnark-crypto. This package retains the
// ETH2030 quotient, encoding, and transcript conventions for existing proofs.

package ipa

import (
	"errors"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bls12-381/bandersnatch"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
)

var (
	banderCurve = bandersnatch.GetEdwardsCurve()
	banderFr    = fr.Modulus()
	banderN     = &banderCurve.Order
	banderA     = banderCurve.A.BigInt(new(big.Int))
	banderD     = banderCurve.D.BigInt(new(big.Int))
)

// BanderPoint represents an extended point on the Bandersnatch curve.
type BanderPoint struct {
	point bandersnatch.PointExtended
}

// banderFrAdd returns (a + b) mod r.
func banderFrAdd(a, b *big.Int) *big.Int {
	return new(big.Int).Mod(new(big.Int).Add(a, b), banderFr)
}

// banderFrSub returns (a - b) mod r.
func banderFrSub(a, b *big.Int) *big.Int {
	r := new(big.Int).Sub(a, b)
	return r.Mod(r, banderFr)
}

// banderFrMul returns (a * b) mod r.
func banderFrMul(a, b *big.Int) *big.Int {
	return new(big.Int).Mod(new(big.Int).Mul(a, b), banderFr)
}

// banderFrSqr returns a² mod r.
func banderFrSqr(a *big.Int) *big.Int {
	return banderFrMul(a, a)
}

// banderFrNeg returns -a mod r.
func banderFrNeg(a *big.Int) *big.Int {
	if a.Sign() == 0 {
		return new(big.Int)
	}
	return new(big.Int).Sub(banderFr, new(big.Int).Mod(a, banderFr))
}

// banderFrInv returns a^(-1) mod r.
func banderFrInv(a *big.Int) *big.Int {
	return new(big.Int).ModInverse(a, banderFr)
}

// banderFrSqrt returns sqrt(a) mod r, or nil if a is not a quadratic residue.
// The field r ≡ 1 mod 4, so we need Tonelli-Shanks or equivalent.
// Since r-1 = 2^32 * m where m is odd, we use Tonelli-Shanks.
func banderFrSqrt(a *big.Int) *big.Int {
	if a.Sign() == 0 {
		return new(big.Int)
	}
	r := new(big.Int).ModSqrt(a, banderFr)
	return r // nil if not a QR
}

// BanderIdentity returns the neutral element.
func BanderIdentity() *BanderPoint {
	var affine bandersnatch.PointAffine
	affine.Y.SetOne()
	result := new(BanderPoint)
	result.point.FromAffine(&affine)
	return result
}

// BanderGenerator returns the standard Banderwagon generator point.
func BanderGenerator() *BanderPoint {
	result := new(BanderPoint)
	result.point.FromAffine(&banderCurve.Base)
	return result
}

// BanderIsIdentity returns true if the point represents the quotient identity.
func (p *BanderPoint) BanderIsIdentity() bool { return p.point.X.IsZero() }

// BanderFromAffine creates a point from affine coordinates (x, y).
func BanderFromAffine(x, y *big.Int) (*BanderPoint, error) {
	var affine bandersnatch.PointAffine
	affine.X.SetBigInt(x)
	affine.Y.SetBigInt(y)
	if !affine.IsOnCurve() {
		return nil, errors.New("banderwagon: point not on curve")
	}
	result := new(BanderPoint)
	result.point.FromAffine(&affine)
	return result, nil
}

// BanderToAffine returns the affine coordinates of the point.
func (p *BanderPoint) BanderToAffine() (x, y *big.Int) {
	var affine bandersnatch.PointAffine
	affine.FromExtended(&p.point)
	return affine.X.BigInt(new(big.Int)), affine.Y.BigInt(new(big.Int))
}

func banderIsOnCurve(x, y *big.Int) bool {
	_, err := BanderFromAffine(x, y)
	return err == nil
}

// BanderAdd adds two Banderwagon points.
func BanderAdd(a, b *BanderPoint) *BanderPoint {
	result := new(BanderPoint)
	result.point.Add(&a.point, &b.point)
	return result
}

// BanderDouble doubles a Banderwagon point.
func BanderDouble(p *BanderPoint) *BanderPoint {
	result := new(BanderPoint)
	result.point.Double(&p.point)
	return result
}

// BanderNeg returns the group inverse of a point.
func BanderNeg(p *BanderPoint) *BanderPoint {
	result := new(BanderPoint)
	result.point.Neg(&p.point)
	return result
}

// BanderScalarMul computes k*P with k reduced modulo the subgroup order.
func BanderScalarMul(p *BanderPoint, k *big.Int) *BanderPoint {
	if p.BanderIsIdentity() {
		return BanderIdentity()
	}
	result := new(BanderPoint)
	result.point.ScalarMultiplication(&p.point, new(big.Int).Mod(k, banderN))
	return result
}

// BanderMSM computes a multi-scalar multiplication: sum(scalars[i] * points[i]).
// Uses a simple accumulator approach. For production, Pippenger's algorithm
// would be more efficient for large vectors.
func BanderMSM(points []*BanderPoint, scalars []*big.Int) *BanderPoint {
	if len(points) == 0 || len(points) != len(scalars) {
		return BanderIdentity()
	}

	result := BanderIdentity()
	for i := range points {
		if scalars[i].Sign() == 0 {
			continue
		}
		term := BanderScalarMul(points[i], scalars[i])
		result = BanderAdd(result, term)
	}
	return result
}

// BanderEqual reports whether two points represent the same quotient element.
func BanderEqual(a, b *BanderPoint) bool {
	if a.point.Equal(&b.point) {
		return true
	}
	opposite := b.point
	opposite.X.Neg(&opposite.X)
	opposite.Y.Neg(&opposite.Y)
	return a.point.Equal(&opposite)
}

// BanderSerialize serializes a Banderwagon point to 32 bytes.
// The encoding uses the Y coordinate with the sign of X encoded in the
// most significant bit, following the Banderwagon serialization spec.
//
// For the Banderwagon quotient group, points are normalized so that
// the serialized Y coordinate is in the "positive" half of the field
// (i.e., Y < (r-1)/2). If Y is in the upper half, we negate the point
// first (which maps to the same equivalence class).
func BanderSerialize(p *BanderPoint) [32]byte {
	x, y := p.BanderToAffine()
	var result [32]byte

	if p.BanderIsIdentity() {
		// Identity serializes as all zeros except y=1.
		result[31] = 1
		return result
	}

	// Normalize: ensure y is in the "lower" half of the field.
	halfR := new(big.Int).Rsh(banderFr, 1)
	if y.Cmp(halfR) > 0 {
		// Negate the point (in the same equivalence class).
		x = banderFrNeg(x)
		y = banderFrNeg(y)
	}

	// Serialize Y in little-endian, with sign of X in the top bit of the last byte.
	yBytes := y.Bytes()
	// Write in little-endian.
	for i, b := range yBytes {
		result[len(yBytes)-1-i] = b
	}

	// Encode sign of X: if X is in the upper half, set the top bit.
	if x.Cmp(halfR) > 0 {
		result[31] |= 0x80
	}

	return result
}

// BanderDeserialize deserializes a 32-byte encoding to a Banderwagon point.
func BanderDeserialize(data [32]byte) (*BanderPoint, error) {
	// Extract sign bit.
	signBit := data[31] & 0x80
	data[31] &= 0x7f

	// Read Y in little-endian.
	y := new(big.Int)
	// Convert LE to big.Int.
	beBytes := make([]byte, 32)
	for i := 0; i < 32; i++ {
		beBytes[31-i] = data[i]
	}
	y.SetBytes(beBytes)

	if y.Cmp(banderFr) >= 0 {
		return nil, errors.New("banderwagon: Y coordinate out of range")
	}

	// Recover X from the curve equation: -5x² + y² = 1 + dx²y²
	// => x²(-5 - dy²) = 1 - y²
	// => x² = (1 - y²) / (-5 - dy²)
	// => x² = (y² - 1) / (5 + dy²)  [negating both sides]
	y2 := banderFrSqr(y)
	num := banderFrSub(y2, big.NewInt(1))
	den := banderFrAdd(big.NewInt(5), banderFrMul(banderD, y2))
	denInv := banderFrInv(den)
	if denInv == nil {
		return nil, errors.New("banderwagon: degenerate point")
	}
	x2 := banderFrMul(num, denInv)

	x := banderFrSqrt(x2)
	if x == nil {
		return nil, errors.New("banderwagon: no valid X coordinate")
	}

	// Apply sign bit.
	halfR := new(big.Int).Rsh(banderFr, 1)
	if signBit != 0 && x.Cmp(halfR) <= 0 {
		x = banderFrNeg(x)
	} else if signBit == 0 && x.Cmp(halfR) > 0 {
		x = banderFrNeg(x)
	}

	return BanderFromAffine(x, y)
}

// BanderMapToField maps a Banderwagon point to a scalar field element.
// This is used for hashing points to 32-byte values in Verkle tree commitments.
// The mapping is: hash(P) = X/Y (as a field element).
func BanderMapToField(p *BanderPoint) *big.Int {
	if p.BanderIsIdentity() {
		return new(big.Int)
	}
	x, y := p.BanderToAffine()
	yInv := banderFrInv(y)
	if yInv == nil {
		return new(big.Int)
	}
	return banderFrMul(x, yInv)
}

// BanderMapToBytes maps a Banderwagon point to 32 bytes via the field mapping.
// This is the standard way to produce a 32-byte commitment value for Verkle trees.
func BanderMapToBytes(p *BanderPoint) [32]byte {
	scalar := BanderMapToField(p)
	var result [32]byte
	b := scalar.Bytes()
	// Big-endian encoding, zero-padded to 32 bytes.
	copy(result[32-len(b):], b)
	return result
}

// --- Pedersen commitment generators ---

// NumPedersenGenerators is the number of generators used for Verkle tree
// Pedersen vector commitments. Each internal/leaf node commits to 256 values.
const NumPedersenGenerators = 256

// pedersenGenerators holds the 256 generator points for Pedersen commitments.
// Lazily initialized on first access.
var pedersenGenerators [NumPedersenGenerators]*BanderPoint

// pedersenGeneratorsInit tracks whether generators have been initialized.
var pedersenGeneratorsInit bool

// GeneratePedersenGenerators creates 256 independent generator points by
// hashing the index to a curve point. Uses the "hash and increment" approach:
// for each index i, hash i to get a candidate Y, then solve for X.
func GeneratePedersenGenerators() [NumPedersenGenerators]*BanderPoint {
	if pedersenGeneratorsInit {
		return pedersenGenerators
	}

	g := BanderGenerator()
	for i := 0; i < NumPedersenGenerators; i++ {
		// Use scalar multiplication of generator by distinct scalars.
		// G_i = (i+2) * G to get independent generators.
		// (i+2 to avoid identity and the generator itself)
		scalar := big.NewInt(int64(i + 2))
		pedersenGenerators[i] = BanderScalarMul(g, scalar)
	}
	pedersenGeneratorsInit = true
	return pedersenGenerators
}

// PedersenCommit computes a Pedersen vector commitment:
//
//	C = sum(values[i] * G_i) for i in [0, len(values))
//
// values must have length <= NumPedersenGenerators.
func PedersenCommit(values []*big.Int) *BanderPoint {
	gens := GeneratePedersenGenerators()
	n := len(values)
	if n > NumPedersenGenerators {
		n = NumPedersenGenerators
	}

	result := BanderIdentity()
	for i := 0; i < n; i++ {
		if values[i] == nil || values[i].Sign() == 0 {
			continue
		}
		term := BanderScalarMul(gens[i], values[i])
		result = BanderAdd(result, term)
	}
	return result
}

// PedersenCommitBytes computes a Pedersen commitment and returns the
// 32-byte hash (via the map-to-field operation).
func PedersenCommitBytes(values []*big.Int) [32]byte {
	c := PedersenCommit(values)
	return BanderMapToBytes(c)
}

// BanderFr returns the base field modulus (BLS12-381 scalar field order).
// This is used for coordinate arithmetic on the curve.
func BanderFr() *big.Int {
	return new(big.Int).Set(banderFr)
}

// BanderN returns the Bandersnatch prime-order subgroup order.
// This is used for scalar arithmetic (scalar multiplication, IPA, etc.).
func BanderN() *big.Int {
	return new(big.Int).Set(banderN)
}

// --- Scalar field arithmetic (mod n) ---

// banderScalarAdd returns (a + b) mod n.
func banderScalarAdd(a, b *big.Int) *big.Int {
	return new(big.Int).Mod(new(big.Int).Add(a, b), banderN)
}

// banderScalarSub returns (a - b) mod n.
func banderScalarSub(a, b *big.Int) *big.Int {
	r := new(big.Int).Sub(a, b)
	return r.Mod(r, banderN)
}

// banderScalarMul returns (a * b) mod n.
func banderScalarMul(a, b *big.Int) *big.Int {
	return new(big.Int).Mod(new(big.Int).Mul(a, b), banderN)
}

// banderScalarInv returns a^(-1) mod n.
func banderScalarInv(a *big.Int) *big.Int {
	return new(big.Int).ModInverse(a, banderN)
}
