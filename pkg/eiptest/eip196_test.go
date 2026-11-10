package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// bn254 field modulus (alt_bn128, py_ecc: 21888242871839275222246405745257275088696311157297823662689037894645226208583).
var bn254P = mustBig("30644e72e131a029b85045b68181585d97816a916871ca8d3c208c16d87cfd47", 16)

func mustBig(s string, base int) *big.Int {
	n, ok := new(big.Int).SetString(s, base)
	if !ok {
		panic("bad big int literal " + s)
	}
	return n
}

// TestEIP196_BN256AddMul verifies the BN254 curve addition (0x06) and scalar
// multiplication (0x07) precompiles for cross-consistency:
// add(G, G) == mul(G, 2) and the outputs are on-curve points.
func TestEIP196_BN256AddMul(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()
	bnAdd := types.BytesToAddress([]byte{0x06})
	bnMul := types.BytesToAddress([]byte{0x07})

	// G1 generator of alt_bn128.
	gx := big.NewInt(1)
	gy := big.NewInt(2)
	if !onCurve(gx, gy) {
		t.Fatal("test setup: (1,2) is not on the curve")
	}

	pad32 := func(n *big.Int) []byte {
		b := make([]byte, 32)
		n.FillBytes(b)
		return b
	}

	// --- add(G, G) ---
	addIn := append(append(pad32(gx), pad32(gy)...), append(pad32(gx), pad32(gy)...)...)
	sum, err := stf.Call(s.Alice().Address, bnAdd, addIn)
	if err != nil {
		t.Fatalf("bn256Add(G, G) failed: %v", err)
	}
	if len(sum) != 64 {
		t.Fatalf("bn256Add output length = %d, want 64", len(sum))
	}
	x2 := new(big.Int).SetBytes(sum[:32])
	y2 := new(big.Int).SetBytes(sum[32:])
	if !onCurve(x2, y2) {
		t.Fatalf("add output (%x, %x) is not on the curve", x2, y2)
	}

	// --- mul(G, 2) must equal add(G, G) ---
	mulIn := append(pad32(gx), pad32(gy)...)
	mulIn = append(mulIn, pad32(big.NewInt(2))...)
	dbl, err := stf.Call(s.Alice().Address, bnMul, mulIn)
	if err != nil {
		t.Fatalf("bn256ScalarMul failed: %v", err)
	}
	if !bytesEqual(sum, dbl) {
		t.Fatalf("add(G,G) = %x but mul(G,2) = %x", sum, dbl)
	}

	// --- mul(G, 1) must leave G unchanged ---
	mulIn1 := append(pad32(gx), pad32(gy)...)
	mulIn1 = append(mulIn1, pad32(big.NewInt(1))...)
	one, err := stf.Call(s.Alice().Address, bnMul, mulIn1)
	if err != nil {
		t.Fatalf("bn256ScalarMul(1) failed: %v", err)
	}
	if !bytesEqual(one, append(pad32(gx), pad32(gy)...)) {
		t.Fatalf("mul(G,1) = %x, want G unchanged", one)
	}
}

// onCurve reports whether (x, y) satisfies y^2 = x^3 + 3 mod p.
func onCurve(x, y *big.Int) bool {
	lhs := new(big.Int).Mul(y, y)
	lhs.Mod(lhs, bn254P)
	rhs := new(big.Int).Mul(x, x)
	rhs.Mul(rhs, x)
	rhs.Add(rhs, big.NewInt(3))
	rhs.Mod(rhs, bn254P)
	return lhs.Cmp(rhs) == 0
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
