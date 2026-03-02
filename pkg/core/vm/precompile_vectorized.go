// precompile_vectorized.go implements the vectorized math precompile at
// address 0x21, providing SIMD-style batch operations for crypto speedup.
//
// Input format: opcode(1) || width(1) || count(4 big-endian) || data(variable)
//
// Supports 11 opcodes: VADD, VMUL, VSUB, VAND, VOR, VXOR, VSHL, VSHR, VMOD,
// VREDUCE, VDOT. Widths: 4 (uint32) or 8 (uint64).
package vm

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
)

// Vectorized precompile address.
var VectorizedPrecompileAddr = types.BytesToAddress([]byte{0x21})

// Vectorized opcodes.
const (
	VOpADD    byte = 0x01
	VOpMUL    byte = 0x02
	VOpSUB    byte = 0x03
	VOpAND    byte = 0x04
	VOpOR     byte = 0x05
	VOpXOR    byte = 0x06
	VOpSHL    byte = 0x07
	VOpSHR    byte = 0x08
	VOpMOD    byte = 0x09
	VOpREDUCE byte = 0x0A
	VOpDOT    byte = 0x0B
)

// Width constants.
const (
	VWidth32 byte = 4 // 32-bit uint32
	VWidth64 byte = 8 // 64-bit uint64
)

// Gas cost constants.
const (
	vecGasBase       uint64 = 100
	vecGasAddPerElem uint64 = 3
	vecGasSubPerElem uint64 = 3
	vecGasMulPerElem uint64 = 5
	vecGasAndPerElem uint64 = 3
	vecGasOrPerElem  uint64 = 3
	vecGasXorPerElem uint64 = 3
	vecGasShlPerElem uint64 = 3
	vecGasShrPerElem uint64 = 3
	vecGasModPerElem uint64 = 8
	vecGasRedPerElem uint64 = 3
	vecGasDotPerElem uint64 = 10
)

// Limits.
const vecMaxCount uint32 = 1_000_000

// Errors.
var (
	ErrVecInvalidOpcode = errors.New("vectorized: invalid opcode")
	ErrVecInvalidWidth  = errors.New("vectorized: invalid width (must be 4 or 8)")
	ErrVecInputTooShort = errors.New("vectorized: input too short")
	ErrVecZeroCount     = errors.New("vectorized: count must be > 0")
	ErrVecCountTooLarge = errors.New("vectorized: count exceeds maximum")
	ErrVecDataTooShort  = errors.New("vectorized: insufficient data for count")
)

type vectorizedPrecompile struct{}

func vecPerElemGas(opcode byte) (uint64, bool) {
	switch opcode {
	case VOpADD:
		return vecGasAddPerElem, true
	case VOpMUL:
		return vecGasMulPerElem, true
	case VOpSUB:
		return vecGasSubPerElem, true
	case VOpAND:
		return vecGasAndPerElem, true
	case VOpOR:
		return vecGasOrPerElem, true
	case VOpXOR:
		return vecGasXorPerElem, true
	case VOpSHL:
		return vecGasShlPerElem, true
	case VOpSHR:
		return vecGasShrPerElem, true
	case VOpMOD:
		return vecGasModPerElem, true
	case VOpREDUCE:
		return vecGasRedPerElem, true
	case VOpDOT:
		return vecGasDotPerElem, true
	default:
		return 0, false
	}
}

func (c *vectorizedPrecompile) RequiredGas(input []byte) uint64 {
	if len(input) < 6 {
		return 0
	}
	opcode := input[0]
	perElem, ok := vecPerElemGas(opcode)
	if !ok {
		return 0
	}
	count := binary.BigEndian.Uint32(input[2:6])
	return vecGasBase + uint64(count)*perElem
}

func (c *vectorizedPrecompile) Run(input []byte) ([]byte, error) {
	if len(input) < 6 {
		return nil, ErrVecInputTooShort
	}

	opcode := input[0]
	width := input[1]
	count := binary.BigEndian.Uint32(input[2:6])
	data := input[6:]

	// Validate opcode.
	if opcode < VOpADD || opcode > VOpDOT {
		return nil, fmt.Errorf("%w: 0x%02x", ErrVecInvalidOpcode, opcode)
	}

	// Validate width.
	if width != VWidth32 && width != VWidth64 {
		return nil, ErrVecInvalidWidth
	}

	// Validate count.
	if count == 0 {
		return nil, ErrVecZeroCount
	}
	if count > vecMaxCount {
		return nil, ErrVecCountTooLarge
	}

	elemSize := int(width)

	// Determine required data length.
	isReduction := opcode == VOpREDUCE
	isBinary := !isReduction // VDOT and binary ops both need 2*count elements
	var requiredElems int
	if isReduction {
		requiredElems = int(count)
	} else if isBinary {
		requiredElems = int(count) * 2
	}
	requiredBytes := requiredElems * elemSize
	if len(data) < requiredBytes {
		return nil, ErrVecDataTooShort
	}

	if width == VWidth32 {
		return runVec32(opcode, count, data)
	}
	return runVec64(opcode, count, data)
}

func runVec32(opcode byte, count uint32, data []byte) ([]byte, error) {
	n := int(count)
	elemSize := 4

	readU32 := func(offset int) uint32 {
		return binary.BigEndian.Uint32(data[offset*elemSize : (offset+1)*elemSize])
	}

	switch opcode {
	case VOpREDUCE:
		var sum uint32
		for i := 0; i < n; i++ {
			sum += readU32(i)
		}
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, sum)
		return out, nil

	case VOpDOT:
		var dot uint32
		for i := 0; i < n; i++ {
			a := readU32(i)
			b := readU32(n + i)
			dot += a * b
		}
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, dot)
		return out, nil

	default:
		// Binary element-wise ops.
		out := make([]byte, n*elemSize)
		for i := 0; i < n; i++ {
			a := readU32(i)
			b := readU32(n + i)
			var r uint32
			switch opcode {
			case VOpADD:
				r = a + b
			case VOpMUL:
				r = a * b
			case VOpSUB:
				r = a - b
			case VOpAND:
				r = a & b
			case VOpOR:
				r = a | b
			case VOpXOR:
				r = a ^ b
			case VOpSHL:
				r = a << (b & 31)
			case VOpSHR:
				r = a >> (b & 31)
			case VOpMOD:
				if b == 0 {
					r = 0
				} else {
					r = a % b
				}
			}
			binary.BigEndian.PutUint32(out[i*elemSize:], r)
		}
		return out, nil
	}
}

func runVec64(opcode byte, count uint32, data []byte) ([]byte, error) {
	n := int(count)
	elemSize := 8

	readU64 := func(offset int) uint64 {
		return binary.BigEndian.Uint64(data[offset*elemSize : (offset+1)*elemSize])
	}

	switch opcode {
	case VOpREDUCE:
		var sum uint64
		for i := 0; i < n; i++ {
			sum += readU64(i)
		}
		out := make([]byte, 8)
		binary.BigEndian.PutUint64(out, sum)
		return out, nil

	case VOpDOT:
		var dot uint64
		for i := 0; i < n; i++ {
			a := readU64(i)
			b := readU64(n + i)
			dot += a * b
		}
		out := make([]byte, 8)
		binary.BigEndian.PutUint64(out, dot)
		return out, nil

	default:
		// Binary element-wise ops.
		out := make([]byte, n*elemSize)
		for i := 0; i < n; i++ {
			a := readU64(i)
			b := readU64(n + i)
			var r uint64
			switch opcode {
			case VOpADD:
				r = a + b
			case VOpMUL:
				r = a * b
			case VOpSUB:
				r = a - b
			case VOpAND:
				r = a & b
			case VOpOR:
				r = a | b
			case VOpXOR:
				r = a ^ b
			case VOpSHL:
				r = a << (b & 63)
			case VOpSHR:
				r = a >> (b & 63)
			case VOpMOD:
				if b == 0 {
					r = 0
				} else {
					r = a % b
				}
			}
			binary.BigEndian.PutUint64(out[i*elemSize:], r)
		}
		return out, nil
	}
}

// init registers the vectorized math precompile in the K+ fork set.
func init() {
	PrecompiledContractsKPlus[VectorizedPrecompileAddr] = &vectorizedPrecompile{}
}
