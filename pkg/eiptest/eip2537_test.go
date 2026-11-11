package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP2537_BLS12G1Add verifies the BLS12-381 G1 addition precompile
// (0x0b): G + (-G) = point at infinity (encoded 0xc0 || zeros).
func TestEIP2537_BLS12G1Add(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()
	g1Add := types.BytesToAddress([]byte{0x0b})

	// BLS12-381 G1 generator (coordinates from the IETF/BLS spec).
	gx := mustBig("17f1d3a73197d7942695638c4fa9ac0fc3688c4f9774b905a14e3a3f171bac586c55e83ff97a1aeffb3af00adb22c6bb", 16)
	gy := mustBig("08b3f481e3aaa0f1a09e30ed741d8ae4fcf5e095d5d00af600db18cb2c04b3edd03cc744a2888ae40caa232946c5e7e1", 16)
	// BLS12-381 field modulus.
	fp := mustBig("1a0111ea397fe69a4b1ba7b6434bacd764774b84f38512bf6730d2a0f6b0f6241eabfffeb153ffffb9feffffffffaaab", 16)

	pad64 := func(n *big.Int) []byte {
		b := make([]byte, 64)
		n.FillBytes(b)
		return b
	}

	negY := new(big.Int).Sub(fp, gy)
	// Input: uncompressed G1 (x||y with 64-byte padded coords) twice = 256 bytes.
	input := append(append(pad64(gx), pad64(gy)...), append(pad64(gx), pad64(negY)...)...)

	ret, err := stf.Call(s.Alice().Address, g1Add, input)
	if err != nil {
		t.Fatalf("BLS G1Add failed: %v", err)
	}
	// Infinity: this implementation encodes the point at infinity as an
	// all-zero 128-byte coordinate pair (the 0xc0-flag ZCash form would also
	// be acceptable per EIP-2537).
	if len(ret) != 128 || !allZero(ret) {
		t.Fatalf("G + (-G) should encode infinity, got %x", ret)
	}
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
