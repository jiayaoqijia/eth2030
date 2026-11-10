package vm

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/crypto/bls"
)

// EIP-2537 BLS12-381 precompile addresses (0x0b - 0x13).
// These precompiles provide native support for BLS12-381 curve operations,
// enabling efficient BLS signature verification and other pairing-based
// cryptographic schemes on-chain.

var (
	ErrBLS12InvalidInput  = errors.New("bls12-381: invalid input length")
	ErrBLS12InvalidPoint  = errors.New("bls12-381: invalid point encoding")
	ErrBLS12NotOnCurve    = errors.New("bls12-381: point not on curve")
	ErrBLS12NotInSubgroup = errors.New("bls12-381: point not in correct subgroup")
)

// BLS12-381 field constants.
var (
	// BLS12-381 field modulus p.
	bls12Modulus, _ = new(big.Int).SetString(
		"1a0111ea397fe69a4b1ba7b6434bacd764774b84f38512bf6730d2a0f6b0f6241eabfffeb153ffffb9feffffffffaaab", 16)
	// BLS12-381 subgroup order r.
	bls12Order, _ = new(big.Int).SetString(
		"73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001", 16)
)

// BLS12-381 precompile gas costs per EIP-2537.
const (
	bls12G1AddGas          = 500
	bls12G1MulGas          = 12000
	bls12G2AddGas          = 800
	bls12G2MulGas          = 45000
	bls12PairingBaseGas    = 65000
	bls12PairingPerPairGas = 43000
	bls12MapG1Gas          = 5500
	bls12MapG2Gas          = 75000
	bls12G1MSMBaseGas      = 12000
	bls12G2MSMBaseGas      = 45000
)

// Point sizes for BLS12-381 (uncompressed, zero-padded to 64/128 bytes).
const (
	bls12G1PointSize = 128 // 2 * 64 bytes (Fp padded to 64)
	bls12G2PointSize = 256 // 2 * 128 bytes (Fp2 elements padded to 128)
	bls12ScalarSize  = 32  // Fr scalar
	bls12FpSize      = 64  // field element padded to 64 bytes
	bls12Fp2Size     = 128 // Fp2 element (2 * 64 bytes)
)

// --- bls12G1Add (address 0x0b) ---
// BLS12-381 G1 point addition.

type bls12G1Add struct{}

func (c *bls12G1Add) RequiredGas(input []byte) uint64 {
	return bls12G1AddGas
}

func (c *bls12G1Add) Run(input []byte) ([]byte, error) {
	if len(input) != 2*bls12G1PointSize {
		return nil, ErrBLS12InvalidInput
	}
	return bls.LegacyBLS12G1Add(input)
}

// --- bls12G1Mul (address 0x0c) ---
// BLS12-381 G1 scalar multiplication.

type bls12G1Mul struct{}

func (c *bls12G1Mul) RequiredGas(input []byte) uint64 {
	return bls12G1MulGas
}

func (c *bls12G1Mul) Run(input []byte) ([]byte, error) {
	if len(input) != bls12G1PointSize+bls12ScalarSize {
		return nil, ErrBLS12InvalidInput
	}
	return crypto.BLS12G1Mul(input)
}

// --- bls12G1MSM (address 0x0d) ---
// BLS12-381 G1 multi-scalar multiplication (MSM).

type bls12G1MSM struct{}

func (c *bls12G1MSM) RequiredGas(input []byte) uint64 {
	pairSize := bls12G1PointSize + bls12ScalarSize
	k := uint64(len(input)) / uint64(pairSize)
	if k == 0 {
		return 0
	}
	discount := msmDiscount(k)
	return (bls12G1MSMBaseGas * k * discount) / 1000
}

func (c *bls12G1MSM) Run(input []byte) ([]byte, error) {
	pairSize := bls12G1PointSize + bls12ScalarSize
	if len(input) == 0 || len(input)%pairSize != 0 {
		return nil, ErrBLS12InvalidInput
	}
	return crypto.BLS12G1MSM(input)
}

// --- bls12G2Add (address 0x0e) ---
// BLS12-381 G2 point addition.

type bls12G2Add struct{}

func (c *bls12G2Add) RequiredGas(input []byte) uint64 {
	return bls12G2AddGas
}

func (c *bls12G2Add) Run(input []byte) ([]byte, error) {
	if len(input) != 2*bls12G2PointSize {
		return nil, ErrBLS12InvalidInput
	}
	return bls.LegacyBLS12G2Add(input)
}

// --- bls12G2Mul (address 0x0f) ---
// BLS12-381 G2 scalar multiplication.

type bls12G2Mul struct{}

func (c *bls12G2Mul) RequiredGas(input []byte) uint64 {
	return bls12G2MulGas
}

func (c *bls12G2Mul) Run(input []byte) ([]byte, error) {
	if len(input) != bls12G2PointSize+bls12ScalarSize {
		return nil, ErrBLS12InvalidInput
	}
	return crypto.BLS12G2Mul(input)
}

// --- bls12G2MSM (address 0x10) ---
// BLS12-381 G2 multi-scalar multiplication.

type bls12G2MSM struct{}

func (c *bls12G2MSM) RequiredGas(input []byte) uint64 {
	pairSize := bls12G2PointSize + bls12ScalarSize
	k := uint64(len(input)) / uint64(pairSize)
	if k == 0 {
		return 0
	}
	discount := msmDiscount(k)
	return (bls12G2MSMBaseGas * k * discount) / 1000
}

func (c *bls12G2MSM) Run(input []byte) ([]byte, error) {
	pairSize := bls12G2PointSize + bls12ScalarSize
	if len(input) == 0 || len(input)%pairSize != 0 {
		return nil, ErrBLS12InvalidInput
	}
	return crypto.BLS12G2MSM(input)
}

// --- bls12Pairing (address 0x11) ---
// BLS12-381 pairing check.

type bls12Pairing struct{}

func (c *bls12Pairing) RequiredGas(input []byte) uint64 {
	pairSize := bls12G1PointSize + bls12G2PointSize
	k := uint64(len(input)) / uint64(pairSize)
	return bls12PairingBaseGas + bls12PairingPerPairGas*k
}

func (c *bls12Pairing) Run(input []byte) ([]byte, error) {
	pairSize := bls12G1PointSize + bls12G2PointSize
	if len(input) == 0 || len(input)%pairSize != 0 {
		return nil, ErrBLS12InvalidInput
	}
	return crypto.BLS12Pairing(input)
}

// --- bls12MapFpToG1 (address 0x12) ---
// BLS12-381 map field element to G1 point.

type bls12MapFpToG1 struct{}

func (c *bls12MapFpToG1) RequiredGas(input []byte) uint64 {
	return bls12MapG1Gas
}

func (c *bls12MapFpToG1) Run(input []byte) ([]byte, error) {
	if len(input) != bls12FpSize {
		return nil, ErrBLS12InvalidInput
	}
	return bls.LegacyBLS12MapFpToG1(input)
}

// --- bls12MapFp2ToG2 (address 0x13) ---
// BLS12-381 map Fp2 element to G2 point.

type bls12MapFp2ToG2 struct{}

func (c *bls12MapFp2ToG2) RequiredGas(input []byte) uint64 {
	return bls12MapG2Gas
}

func (c *bls12MapFp2ToG2) Run(input []byte) ([]byte, error) {
	if len(input) != bls12Fp2Size {
		return nil, ErrBLS12InvalidInput
	}
	return bls.LegacyBLS12MapFp2ToG2(input)
}

// --- helpers ---

// isZeroBytes checks if all bytes are zero.
func isZeroBytes(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return false
		}
	}
	return true
}

// msmDiscount returns the MSM discount factor (per 1000) for k pairs.
// From EIP-2537 discount table.
func msmDiscount(k uint64) uint64 {
	if k == 0 {
		return 0
	}
	// Pippenger discount table from EIP-2537.
	discountTable := []uint64{
		0, 1200, 888, 764, 641, 594, 547, 500, 453, 438,
		423, 408, 394, 379, 364, 349, 334, 330, 326, 322,
		318, 314, 310, 306, 302, 298, 294, 289, 285, 281,
		277, 273, 269, 265, 261, 257, 253, 249, 245, 241,
		237, 234, 230, 226, 222, 218, 214, 210, 206, 202,
		199, 195, 191, 187, 183, 179, 176, 172, 168, 164,
		160, 157, 153, 149, 145, 141, 138, 134, 130, 126,
		123, 119, 115, 111, 107, 104, 100, 96, 92, 89,
		85, 81, 77, 73, 70, 66, 62, 58, 55, 51,
		47, 43, 39, 36, 32, 28, 24, 21, 17, 13,
		9, 6, 2, 2, 2, 2, 2, 2, 2, 2,
		2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
		2, 2, 2, 2, 2, 2, 2, 2, 2, 2,
	}
	if k >= uint64(len(discountTable)) {
		return 2 // minimum discount
	}
	return discountTable[k]
}
