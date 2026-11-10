package bls

// BLS aggregate signature operations for Ethereum consensus layer.
//
// Implements BLS12-381 aggregate public key aggregation, signature
// aggregation, and aggregate/fast-aggregate verification as required
// by the beacon chain spec (Altair+).
//
// Public keys are 48-byte compressed G1 points. Signatures are 96-byte
// compressed G2 points. The signature path uses RFC 9380 hash-to-curve.
//
// Signature scheme: signatures live in G2, public keys in G1.
// e(pk, H(m)) == e(G1, sig) for single verification.
// For aggregate: e(G1, aggSig) == product(e(pk_i, H(m_i))).

import (
	"crypto/sha256"
	"math/big"

	bls381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
)

// BLS aggregate signature sizes.
const (
	BLSPubkeySize    = 48
	BLSSignatureSize = 96
)

// SerializeG1 compresses a G1 point to 48 bytes (compressed form).
// Uses the high bit of the first byte as flags:
//   - bit 7 (0x80): compressed flag (always set)
//   - bit 6 (0x40): infinity flag
//   - bit 5 (0x20): sort flag (y > -y, i.e., lexicographic choice)
func SerializeG1(p *BlsG1Point) [BLSPubkeySize]byte {
	var out [BLSPubkeySize]byte
	if p.blsG1IsInfinity() {
		out[0] = 0xC0 // compressed + infinity
		return out
	}
	x, y := p.blsG1ToAffine()
	// Write x coordinate in big-endian 48 bytes.
	xBytes := x.Bytes()
	copy(out[BLSPubkeySize-len(xBytes):], xBytes)
	// Set compressed flag.
	out[0] |= 0x80
	// Set sort flag if y > (p-1)/2 (lexicographic largest).
	halfP := new(big.Int).Rsh(blsP, 1)
	if y.Cmp(halfP) > 0 {
		out[0] |= 0x20
	}
	return out
}

// DeserializeG1 decompresses a 48-byte compressed G1 point.
// Returns nil if the point is invalid.
func DeserializeG1(data [BLSPubkeySize]byte) *BlsG1Point {
	if data[0]&0x80 == 0 {
		return nil // not compressed
	}
	if data[0]&0x40 != 0 {
		// Point at infinity.
		return BlsG1Infinity()
	}
	sortFlag := data[0]&0x20 != 0
	// Clear flag bits to get x.
	data[0] &= 0x1F
	x := new(big.Int).SetBytes(data[:])
	if x.Cmp(blsP) >= 0 {
		return nil
	}
	// Compute y^2 = x^3 + 4.
	x3 := blsFpMul(blsFpSqr(x), x)
	rhs := blsFpAdd(x3, blsB)
	y := blsFpSqrt(rhs)
	if y == nil {
		return nil // not on curve
	}
	// Choose correct y based on sort flag.
	halfP := new(big.Int).Rsh(blsP, 1)
	if sortFlag != (y.Cmp(halfP) > 0) {
		y = blsFpNeg(y)
	}
	p := blsG1FromAffine(x, y)
	if !blsG1InSubgroup(p) {
		return nil
	}
	return p
}

// SerializeG2 compresses a G2 point to 96 bytes (compressed form).
func SerializeG2(p *BlsG2Point) [BLSSignatureSize]byte {
	var out [BLSSignatureSize]byte
	if p.blsG2IsInfinity() {
		out[0] = 0xC0 // compressed + infinity
		return out
	}
	x, y := p.blsG2ToAffine()
	// Write x = (c1, c0) as 96 bytes: c1 first (48 bytes), then c0 (48 bytes).
	c1Bytes := x.c1.Bytes()
	c0Bytes := x.c0.Bytes()
	copy(out[BLSPubkeySize-len(c1Bytes):BLSPubkeySize], c1Bytes)
	copy(out[BLSSignatureSize-len(c0Bytes):], c0Bytes)
	// Set compressed flag.
	out[0] |= 0x80
	// Sort flag: lexicographic comparison on y = (c1, c0).
	halfP := new(big.Int).Rsh(blsP, 1)
	if y.c1.Cmp(halfP) > 0 || (y.c1.Sign() == 0 && y.c0.Cmp(halfP) > 0) {
		out[0] |= 0x20
	}
	return out
}

// DeserializeG2 decompresses a 96-byte compressed G2 point.
// Returns nil if the point is invalid.
func DeserializeG2(data [BLSSignatureSize]byte) *BlsG2Point {
	if data[0]&0x80 == 0 {
		return nil
	}
	if data[0]&0x40 != 0 {
		return BlsG2Infinity()
	}
	sortFlag := data[0]&0x20 != 0
	// Clear flags.
	data[0] &= 0x1F
	c1 := new(big.Int).SetBytes(data[:BLSPubkeySize])
	c0 := new(big.Int).SetBytes(data[BLSPubkeySize:])
	if c0.Cmp(blsP) >= 0 || c1.Cmp(blsP) >= 0 {
		return nil
	}
	x := &blsFp2{c0: c0, c1: c1}
	// Compute y^2 = x^3 + 4(1+u).
	x3 := blsFp2Mul(blsFp2Sqr(x), x)
	rhs := blsFp2Add(x3, blsTwistB)
	y := blsFp2Sqrt(rhs)
	if y == nil {
		return nil
	}
	// Choose y based on sort flag.
	halfP := new(big.Int).Rsh(blsP, 1)
	yLarger := y.c1.Cmp(halfP) > 0 || (y.c1.Sign() == 0 && y.c0.Cmp(halfP) > 0)
	if sortFlag != yLarger {
		y = blsFp2Neg(y)
	}
	p := blsG2FromAffine(x, y)
	if !blsG2InSubgroup(p) {
		return nil
	}
	return p
}

// AggregatePublicKeys aggregates multiple BLS12-381 public keys by
// adding the corresponding G1 points.
func AggregatePublicKeys(pubkeys [][48]byte) [48]byte {
	agg := BlsG1Infinity()
	for _, pk := range pubkeys {
		p := DeserializeG1(pk)
		if p == nil {
			continue
		}
		agg = blsG1Add(agg, p)
	}
	return SerializeG1(agg)
}

// AggregateSignatures aggregates multiple BLS signatures by adding
// the corresponding G2 points.
func AggregateSignatures(sigs [][96]byte) [96]byte {
	agg := BlsG2Infinity()
	for _, s := range sigs {
		p := DeserializeG2(s)
		if p == nil {
			continue
		}
		agg = blsG2Add(agg, p)
	}
	return SerializeG2(agg)
}

// HashToG2 hashes a message using the BLS12-381 G2 SSWU ciphersuite.
func HashToG2(msg []byte, dst []byte) *BlsG2Point {
	if len(dst) > 255 {
		h := sha256.New()
		h.Write([]byte("H2C-OVERSIZE-DST-"))
		h.Write(dst)
		dst = h.Sum(nil)
	}
	point, err := bls381.HashToG2(msg, dst)
	if err != nil {
		return BlsG2Infinity()
	}
	return blsG2FromAffine(
		&blsFp2{c0: point.X.A0.BigInt(new(big.Int)), c1: point.X.A1.BigInt(new(big.Int))},
		&blsFp2{c0: point.Y.A0.BigInt(new(big.Int)), c1: point.Y.A1.BigInt(new(big.Int))},
	)
}

// blsSignaturePairing verifies the product of the signature pairings.
// The decoder checks that each input belongs to the correct subgroup.
func blsSignaturePairing(g1 []*BlsG1Point, g2 []*BlsG2Point) bool {
	p := make([]bls381.G1Affine, len(g1))
	q := make([]bls381.G2Affine, len(g2))
	for i := range g1 {
		pubkey, signature := SerializeG1(g1[i]), SerializeG2(g2[i])
		if _, err := p[i].SetBytes(pubkey[:]); err != nil {
			return false
		}
		if _, err := q[i].SetBytes(signature[:]); err != nil {
			return false
		}
	}
	ok, err := bls381.PairingCheck(p, q)
	return err == nil && ok
}

// blsSignDST is the domain separation tag for BLS signatures.
var blsSignDST = []byte("BLS_SIG_BLS12381G2_XMD:SHA-256_SSWU_RO_POP_")

// BLSSign signs a message with a secret key (scalar), returning a G2 signature.
// secret is the private key scalar.
func BLSSign(secret *big.Int, msg []byte) [BLSSignatureSize]byte {
	// Hash message to G2.
	hm := HashToG2(msg, blsSignDST)
	// sig = secret * H(m)
	sig := blsG2ScalarMul(hm, secret)
	return SerializeG2(sig)
}

// BLSVerify verifies a single BLS signature.
// Checks: e(pk, H(m)) == e(G1, sig)
// Equivalent to: e(pk, H(m)) * e(-G1, sig) == 1
func BLSVerify(pubkey [BLSPubkeySize]byte, msg []byte, sig [BLSSignatureSize]byte) bool {
	pk := DeserializeG1(pubkey)
	if pk == nil || pk.blsG1IsInfinity() {
		return false
	}
	s := DeserializeG2(sig)
	if s == nil || s.blsG2IsInfinity() {
		return false
	}
	hm := HashToG2(msg, blsSignDST)

	// Check e(pk, H(m)) * e(-G1, sig) == 1.
	g1 := BlsG1Generator()
	negG1 := blsG1Neg(g1)

	return blsSignaturePairing(
		[]*BlsG1Point{pk, negG1},
		[]*BlsG2Point{hm, s},
	)
}

// VerifyAggregate verifies an aggregate signature where each signer
// signed a different message.
// Checks: e(G1, aggSig) == product(e(pk_i, H(m_i)))
// Equivalent to: product(e(pk_i, H(m_i))) * e(-G1, aggSig) == 1
func VerifyAggregate(pubkeys [][48]byte, msgs [][]byte, sig [96]byte) bool {
	if len(pubkeys) == 0 || len(pubkeys) != len(msgs) {
		return false
	}

	s := DeserializeG2(sig)
	if s == nil || s.blsG2IsInfinity() {
		return false
	}

	// Build the pairing inputs: (pk_i, H(m_i)) for each signer, plus (-G1, aggSig).
	n := len(pubkeys)
	g1Points := make([]*BlsG1Point, n+1)
	g2Points := make([]*BlsG2Point, n+1)

	for i := 0; i < n; i++ {
		pk := DeserializeG1(pubkeys[i])
		if pk == nil || pk.blsG1IsInfinity() {
			return false
		}
		g1Points[i] = pk
		g2Points[i] = HashToG2(msgs[i], blsSignDST)
	}

	// Add (-G1, aggSig) for the check.
	g1Points[n] = blsG1Neg(BlsG1Generator())
	g2Points[n] = s

	return blsSignaturePairing(g1Points, g2Points)
}

// FastAggregateVerify verifies an aggregate signature where all signers
// signed the same message. This is the common case for sync committee
// and attestation signatures.
// Checks: e(aggPK, H(m)) == e(G1, aggSig)
func FastAggregateVerify(pubkeys [][48]byte, msg []byte, sig [96]byte) bool {
	if len(pubkeys) == 0 {
		return false
	}

	s := DeserializeG2(sig)
	if s == nil || s.blsG2IsInfinity() {
		return false
	}

	// Aggregate the public keys.
	aggPK := BlsG1Infinity()
	for _, pk := range pubkeys {
		p := DeserializeG1(pk)
		if p == nil || p.blsG1IsInfinity() {
			return false
		}
		aggPK = blsG1Add(aggPK, p)
	}
	if aggPK.blsG1IsInfinity() {
		return false
	}

	// Hash the message to G2.
	hm := HashToG2(msg, blsSignDST)

	// Check e(aggPK, H(m)) * e(-G1, sig) == 1.
	negG1 := blsG1Neg(BlsG1Generator())
	return blsSignaturePairing(
		[]*BlsG1Point{aggPK, negG1},
		[]*BlsG2Point{hm, s},
	)
}

// BLSPubkeyFromSecret derives the public key from a secret scalar.
// pk = secret * G1
func BLSPubkeyFromSecret(secret *big.Int) [BLSPubkeySize]byte {
	g1 := BlsG1Generator()
	pk := blsG1ScalarMul(g1, secret)
	return SerializeG1(pk)
}
