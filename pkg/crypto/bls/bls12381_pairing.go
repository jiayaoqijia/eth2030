package bls

import bls381 "github.com/consensys/gnark-crypto/ecc/bls12-381"

// blsMultiPairing validates the input groups and checks the pairing product.
func blsMultiPairing(g1 []*BlsG1Point, g2 []*BlsG2Point) bool {
	if len(g1) != len(g2) || len(g1) == 0 {
		return false
	}
	p := make([]bls381.G1Affine, len(g1))
	q := make([]bls381.G2Affine, len(g2))
	for i := range p {
		p[i], q[i] = g1[i].gnarkAffine(), g2[i].gnarkAffine()
		if !p[i].IsOnCurve() || !p[i].IsInSubGroup() ||
			!q[i].IsOnCurve() || !q[i].IsInSubGroup() {
			return false
		}
	}
	ok, err := bls381.PairingCheck(p, q)
	return err == nil && ok
}
