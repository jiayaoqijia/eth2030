package vm

import (
	"errors"
	"fmt"
	"math/big"
	"math/bits"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	bn254fft "github.com/consensys/gnark-crypto/ecc/bn254/fr/fft"
	"github.com/consensys/gnark-crypto/field/goldilocks"
	goldilocksfft "github.com/consensys/gnark-crypto/field/goldilocks/fft"
	"github.com/eth2030/eth2030/core/types"
	rollupexecute "github.com/eth2030/eth2030/rollup/execute"
)

// NTT precompile over BN254 scalar field.
// Split precompiles at addresses 0x0f-0x14 (registered for I+ fork).
//
// EIP-7885 NTT Precompile Alignment
//
// This precompile implements the Number Theoretic Transform (NTT) as proposed
// in EIP-7885 by ZKNoxHQ. NTT is the core operation for:
//   - Lattice-based cryptography (Falcon, Dilithium, Kyber)
//   - STARK proof verification (polynomial evaluation over finite fields)
//   - Efficient polynomial multiplication in ZK circuits
//
// Reference implementations:
//   - ZKNoxHQ/NTT (Solidity + Python): refs/ntt-eip/
//   - ZKNoxHQ/ETHFALCON: refs/ethfalcon/
//
// Supported fields:
//   - BN254 scalar field (existing): p = 21888242871839275222246405745257275088548364400416034343698204186575808495617
//   - Goldilocks field (new): p = 2^64 - 2^32 + 1 = 18446744069414584321 (STARK-friendly)

var (
	bn254ScalarField = fr.Modulus()
	goldilocksField  = goldilocks.Modulus()
)

// NTT operation types.
const (
	NTTOpForward           = 0 // Forward NTT (evaluation) over BN254
	NTTOpInverse           = 1 // Inverse NTT (interpolation) over BN254
	NTTOpGoldilocks        = 2 // Forward NTT over Goldilocks field
	NTTOpGoldilocksInverse = 3 // Inverse NTT over Goldilocks field
)

// NTT gas cost constants.
const (
	NTTBaseCost       uint64 = 1000
	NTTPerElementCost uint64 = 10
	NTTMaxDegree             = 1 << 16 // 65536
)

// NTT errors.
var (
	ErrNTTInvalidInput  = errors.New("ntt: invalid input")
	ErrNTTNotPowerOfTwo = errors.New("ntt: size must be a power of two")
	ErrNTTTooLarge      = errors.New("ntt: exceeds maximum degree")
	ErrNTTInvalidOpType = errors.New("ntt: invalid operation type")
	ErrNTTNoRootOfUnity = errors.New("ntt: no root of unity for given size")
	ErrNTTOutputRange   = errors.New("ntt: output element out of field range")
)

type nttPrecompile struct{}

func (c *nttPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 1 {
		return 0
	}
	nBytes := len(input) - 1
	n := uint64(nBytes / 32)
	if n == 0 {
		return NTTBaseCost
	}
	// gas = base + n * log2(n) * perElement
	log2n := uint64(bits.Len(uint(n)) - 1)
	if log2n == 0 {
		log2n = 1
	}
	// Overflow check: compute n * log2n first, then multiply by perElement.
	product := n * log2n
	if n != 0 && product/n != log2n {
		return ^uint64(0) // return max gas on overflow
	}
	gasAdditional := product * NTTPerElementCost
	if product != 0 && gasAdditional/product != NTTPerElementCost {
		return ^uint64(0)
	}
	total := NTTBaseCost + gasAdditional
	if total < NTTBaseCost {
		return ^uint64(0)
	}
	return total
}

func (c *nttPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 1 {
		return nil, ErrNTTInvalidInput
	}

	opType := input[0]
	if opType > 3 {
		return nil, ErrNTTInvalidOpType
	}

	coeffData := input[1:]
	if len(coeffData)%32 != 0 {
		return nil, ErrNTTInvalidInput
	}
	n := len(coeffData) / 32
	if n == 0 {
		return nil, ErrNTTInvalidInput
	}
	if n > NTTMaxDegree {
		return nil, ErrNTTTooLarge
	}
	if n&(n-1) != 0 {
		return nil, ErrNTTNotPowerOfTwo
	}

	useGoldilocks := opType >= NTTOpGoldilocks
	fieldMod := bn254ScalarField
	if useGoldilocks {
		fieldMod = goldilocksField
	}

	// Parse coefficients as big-endian 32-byte big.Ints, reduced mod p.
	coeffs := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		val := new(big.Int).SetBytes(coeffData[i*32 : (i+1)*32])
		val.Mod(val, fieldMod)
		coeffs[i] = val
	}

	inverse := opType == NTTOpInverse || opType == NTTOpGoldilocksInverse
	result, err := computeNTT(coeffs, useGoldilocks, inverse)
	if err != nil {
		return nil, err
	}

	// Validate output field elements are within the field.
	for i, val := range result {
		if val.Sign() < 0 || val.Cmp(fieldMod) >= 0 {
			return nil, fmt.Errorf("%w: element %d = %s", ErrNTTOutputRange, i, val.String())
		}
	}

	// Encode output: n * 32-byte big-endian values.
	out := make([]byte, n*32)
	for i, val := range result {
		b := val.Bytes()
		copy(out[i*32+(32-len(b)):], b)
	}
	return out, nil
}

// computeNTT returns cyclic evaluations or coefficients in natural order.
func computeNTT(coefficients []*big.Int, useGoldilocks, inverse bool) ([]*big.Int, error) {
	n := len(coefficients)
	if n == 0 || n&(n-1) != 0 {
		return nil, ErrNTTNotPowerOfTwo
	}
	if n == 1 {
		return []*big.Int{new(big.Int).Set(coefficients[0])}, nil
	}
	if useGoldilocks {
		if uint64(n) > 1<<32 {
			return nil, ErrNTTNoRootOfUnity
		}
		return goldilocksNTT(coefficients, inverse), nil
	}
	if uint64(n) > 1<<28 {
		return nil, ErrNTTNoRootOfUnity
	}
	return bn254NTT(coefficients, inverse), nil
}

func bn254NTT(coefficients []*big.Int, inverse bool) []*big.Int {
	values := make([]fr.Element, len(coefficients))
	for i, value := range coefficients {
		values[i].SetBigInt(value)
	}
	domain := bn254fft.NewDomain(uint64(len(values)), bn254fft.WithoutPrecompute())
	if inverse {
		domain.FFTInverse(values, bn254fft.DIF, bn254fft.WithNbTasks(1))
	} else {
		domain.FFT(values, bn254fft.DIF, bn254fft.WithNbTasks(1))
	}
	bn254fft.BitReverse(values)
	result := make([]*big.Int, len(values))
	for i, value := range values {
		result[i] = value.BigInt(new(big.Int))
	}
	return result
}

func goldilocksNTT(coefficients []*big.Int, inverse bool) []*big.Int {
	values := make([]goldilocks.Element, len(coefficients))
	for i, value := range coefficients {
		values[i].SetBigInt(value)
	}
	domain := goldilocksfft.NewDomain(uint64(len(values)), goldilocksfft.WithoutPrecompute())
	if inverse {
		domain.FFTInverse(values, goldilocksfft.DIF, goldilocksfft.WithNbTasks(1))
	} else {
		domain.FFT(values, goldilocksfft.DIF, goldilocksfft.WithNbTasks(1))
	}
	goldilocksfft.BitReverse(values)
	result := make([]*big.Int, len(values))
	for i, value := range values {
		result[i] = value.BigInt(new(big.Int))
	}
	return result
}

// bitReverse reverses the lower numBits bits of v.
func bitReverse(v, numBits int) int {
	result := 0
	for i := 0; i < numBits; i++ {
		result = (result << 1) | (v & 1)
		v >>= 1
	}
	return result
}

// ValidateNTTInput checks that raw NTT precompile input is well-formed before execution:
//   - Input must have at least 1 byte (op type) + 32 bytes (at least 1 element)
//   - Op type must be 0-3 (forward/inverse for BN254 or Goldilocks)
//   - Element count must be a power of two and at most NTTMaxDegree
func ValidateNTTInput(input []byte) error {
	if len(input) < 1 {
		return ErrNTTInvalidInput
	}
	if input[0] > 3 {
		return ErrNTTInvalidOpType
	}
	coeffData := input[1:]
	if len(coeffData) == 0 || len(coeffData)%32 != 0 {
		return ErrNTTInvalidInput
	}
	n := len(coeffData) / 32
	if n > NTTMaxDegree {
		return ErrNTTTooLarge
	}
	if n&(n-1) != 0 {
		return ErrNTTNotPowerOfTwo
	}
	return nil
}

// GoldilocksNTT performs NTT over the Goldilocks field.
// This is the STARK-friendly variant for use with FRI-based proof systems.
func GoldilocksNTT(coefficients []*big.Int) ([]*big.Int, error) {
	return computeNTT(coefficients, true, false)
}

// GoldilocksINTT performs inverse NTT over the Goldilocks field.
func GoldilocksINTT(coefficients []*big.Int) ([]*big.Int, error) {
	return computeNTT(coefficients, true, true)
}

// Split NTT precompile structs (EIP-7885 aligned addresses 0x0f-0x14).

// nttFWPrecompile performs forward NTT over BN254 scalar field.
// Address: 0x0f
type nttFWPrecompile struct{}

func (c *nttFWPrecompile) RequiredGas(input []byte) uint64 {
	n := uint64(len(input) / 32)
	if n == 0 {
		return 0
	}
	log2n := uint64(bits.Len(uint(n)) - 1)
	if log2n == 0 {
		log2n = 1
	}
	gas := n * log2n / 8
	if gas < 600 {
		return 600
	}
	return gas
}

func (c *nttFWPrecompile) Run(input []byte) ([]byte, error) {
	return runNTTSplit(input, false)
}

// nttINVPrecompile performs inverse NTT over BN254 scalar field.
// Address: 0x10
type nttINVPrecompile struct{}

func (c *nttINVPrecompile) RequiredGas(input []byte) uint64 {
	return (&nttFWPrecompile{}).RequiredGas(input)
}

func (c *nttINVPrecompile) Run(input []byte) ([]byte, error) {
	return runNTTSplit(input, true)
}

// runNTTSplit is the common implementation for forward/inverse NTT precompiles.
func runNTTSplit(input []byte, inverse bool) ([]byte, error) {
	if len(input) == 0 || len(input)%32 != 0 {
		return nil, ErrNTTInvalidInput
	}
	n := len(input) / 32
	if n == 0 {
		return nil, ErrNTTInvalidInput
	}
	if n > NTTMaxDegree {
		return nil, ErrNTTTooLarge
	}
	if n&(n-1) != 0 {
		return nil, ErrNTTNotPowerOfTwo
	}

	coeffs := make([]*big.Int, n)
	for i := 0; i < n; i++ {
		val := new(big.Int).SetBytes(input[i*32 : (i+1)*32])
		val.Mod(val, bn254ScalarField)
		coeffs[i] = val
	}

	result, err := computeNTT(coeffs, false, inverse)
	if err != nil {
		return nil, err
	}

	out := make([]byte, n*32)
	for i, val := range result {
		b := val.Bytes()
		copy(out[i*32+(32-len(b)):], b)
	}
	return out, nil
}

// nttVecMulModPrecompile performs element-wise multiplication mod BN254.
// Address: 0x11
// Input: 2N 32-byte elements (A[0..N-1] || B[0..N-1]).
// Output: N 32-byte elements where result[i] = A[i]*B[i] mod BN254.
type nttVecMulModPrecompile struct{}

func (c *nttVecMulModPrecompile) RequiredGas(input []byte) uint64 {
	n := uint64(len(input) / 64) // half-input element count
	if n == 0 {
		return 0
	}
	log2n := uint64(bits.Len(uint(n)) - 1)
	if log2n == 0 {
		log2n = 1
	}
	gas := n * log2n / 8
	if gas < 600 {
		return 600
	}
	return gas
}

func (c *nttVecMulModPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) == 0 || len(input)%32 != 0 {
		return nil, ErrNTTInvalidInput
	}
	totalElems := len(input) / 32
	if totalElems%2 != 0 {
		return nil, ErrNTTInvalidInput
	}
	n := totalElems / 2

	out := make([]byte, n*32)
	for i := 0; i < n; i++ {
		a := new(big.Int).SetBytes(input[i*32 : (i+1)*32])
		b := new(big.Int).SetBytes(input[(n+i)*32 : (n+i+1)*32])
		r := new(big.Int).Mul(a, b)
		r.Mod(r, bn254ScalarField)
		rb := r.Bytes()
		copy(out[i*32+(32-len(rb)):], rb)
	}
	return out, nil
}

// nttVecAddModPrecompile performs element-wise addition mod BN254.
// Address: 0x12
// Input: 2N 32-byte elements (A[0..N-1] || B[0..N-1]).
// Output: N 32-byte elements where result[i] = A[i]+B[i] mod BN254.
type nttVecAddModPrecompile struct{}

func (c *nttVecAddModPrecompile) RequiredGas(input []byte) uint64 {
	n := uint64(len(input) / 64)
	if n == 0 {
		return 0
	}
	log2n := uint64(bits.Len(uint(n)) - 1)
	if log2n == 0 {
		log2n = 1
	}
	gas := n * log2n / 32
	if gas < 100 {
		return 100
	}
	return gas
}

func (c *nttVecAddModPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) == 0 || len(input)%32 != 0 {
		return nil, ErrNTTInvalidInput
	}
	totalElems := len(input) / 32
	if totalElems%2 != 0 {
		return nil, ErrNTTInvalidInput
	}
	n := totalElems / 2

	out := make([]byte, n*32)
	for i := 0; i < n; i++ {
		a := new(big.Int).SetBytes(input[i*32 : (i+1)*32])
		b := new(big.Int).SetBytes(input[(n+i)*32 : (n+i+1)*32])
		r := new(big.Int).Add(a, b)
		r.Mod(r, bn254ScalarField)
		rb := r.Bytes()
		copy(out[i*32+(32-len(rb)):], rb)
	}
	return out, nil
}

// nttDotProductPrecompile computes the dot product of two vectors mod BN254.
// Address: 0x13
// Input: 2N 32-byte elements (A[0..N-1] || B[0..N-1]).
// Output: single 32-byte result = sum(A[i]*B[i]) mod BN254.
type nttDotProductPrecompile struct{}

func (c *nttDotProductPrecompile) RequiredGas(input []byte) uint64 {
	n := uint64(len(input) / 64)
	if n == 0 {
		return 0
	}
	if n < 600 {
		return 600
	}
	return n
}

func (c *nttDotProductPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) == 0 || len(input)%32 != 0 {
		return nil, ErrNTTInvalidInput
	}
	totalElems := len(input) / 32
	if totalElems%2 != 0 {
		return nil, ErrNTTInvalidInput
	}
	n := totalElems / 2

	sum := new(big.Int)
	for i := 0; i < n; i++ {
		a := new(big.Int).SetBytes(input[i*32 : (i+1)*32])
		b := new(big.Int).SetBytes(input[(n+i)*32 : (n+i+1)*32])
		prod := new(big.Int).Mul(a, b)
		prod.Mod(prod, bn254ScalarField)
		sum.Add(sum, prod)
		sum.Mod(sum, bn254ScalarField)
	}

	out := make([]byte, 32)
	sb := sum.Bytes()
	copy(out[32-len(sb):], sb)
	return out, nil
}

// nttButterflyPrecompile applies bit-reversal permutation to input elements.
// Address: 0x14
// Input: N 32-byte elements (power-of-two N).
// Output: N 32-byte elements with bit-reversal permutation applied.
type nttButterflyPrecompile struct{}

func (c *nttButterflyPrecompile) RequiredGas(input []byte) uint64 {
	n := uint64(len(input) / 32)
	if n == 0 {
		return 0
	}
	if n < 300 {
		return 300
	}
	return n
}

func (c *nttButterflyPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) == 0 || len(input)%32 != 0 {
		return nil, ErrNTTInvalidInput
	}
	n := len(input) / 32
	if n == 0 {
		return nil, ErrNTTInvalidInput
	}
	if n&(n-1) != 0 {
		return nil, ErrNTTNotPowerOfTwo
	}

	logN := bits.Len(uint(n)) - 1
	out := make([]byte, n*32)
	for i := 0; i < n; i++ {
		j := bitReverse(i, logN)
		copy(out[i*32:(i+1)*32], input[j*32:(j+1)*32])
	}
	return out, nil
}

// PrecompiledContractsIPlus extends Glamsterdan with I+ fork precompiles:
// EIP-7885 NTT precompiles (0x0f-0x14) and NII precompiles (0x0201-0x0204).
var PrecompiledContractsIPlus = func() map[types.Address]PrecompiledContract {
	m := make(map[types.Address]PrecompiledContract, len(PrecompiledContractsGlamsterdan)+10)
	for addr, c := range PrecompiledContractsGlamsterdan {
		m[addr] = c
	}
	// EIP-7885: Split NTT precompiles.
	m[types.BytesToAddress([]byte{0x0f})] = &nttFWPrecompile{}
	m[types.BytesToAddress([]byte{0x10})] = &nttINVPrecompile{}
	m[types.BytesToAddress([]byte{0x11})] = &nttVecMulModPrecompile{}
	m[types.BytesToAddress([]byte{0x12})] = &nttVecAddModPrecompile{}
	m[types.BytesToAddress([]byte{0x13})] = &nttDotProductPrecompile{}
	m[types.BytesToAddress([]byte{0x14})] = &nttButterflyPrecompile{}
	// NII precompiles.
	m[NiiModExpAddr] = &NiiModExpPrecompile{}
	m[NiiFieldMulAddr] = &NiiFieldMulPrecompile{}
	m[NiiFieldInvAddr] = &NiiFieldInvPrecompile{}
	m[NiiBatchVerifyAddr] = &NiiBatchVerifyPrecompile{}
	// EIP-8079: EXECUTE precompile for native rollup state transition verification.
	m[types.HexToAddress(rollupexecute.PrecompileAddr)] = &rollupexecute.ExecutePrecompile{}
	return m
}()

// The project's experimental NTT ABI uses private addresses when the
// standard BLS and CREATE2 factory allocations are in force.
var precompiledContractsIPlusStandardBLS = func() map[types.Address]PrecompiledContract {
	out := standardBLSPrecompiles(PrecompiledContractsIPlus)
	for offset := byte(0); offset < 6; offset++ {
		out[types.Address{18: 0x02, 19: 0x10 + offset}] = PrecompiledContractsIPlus[types.Address{19: 0x0f + offset}]
	}
	for id := byte(0x12); id <= 0x14; id++ {
		delete(out, types.Address{19: id})
	}
	return out
}()
