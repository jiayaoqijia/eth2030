package bls

import (
	"math/big"

	bls381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fp"
)

func blsMapFpToG1(u *big.Int) *BlsG1Point {
	var element fp.Element
	element.SetBigInt(u)
	return blsG1FromGnark(bls381.MapToG1(element))
}

func blsMapFp2ToG2(u *blsFp2) *BlsG2Point {
	var element bls381.E2
	element.A0.SetBigInt(u.c0)
	element.A1.SetBigInt(u.c1)
	return blsG2FromGnark(bls381.MapToG2(element))
}
