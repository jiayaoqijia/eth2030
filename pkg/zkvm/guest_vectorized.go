// guest_vectorized.go implements a RISC-V guest program for the vectorized
// math precompile (address 0x21). The guest reads vectorized operation input
// via InputBuf, computes a deterministic hash-based simulation of the
// vectorized math operations, and writes the result to OutputBuf.
//
// Registered as zkISA selector 0x20 in the ZKISAOpTable.
//
// The Go reference implementation (ExecuteVectorizedGo) provides bit-exact
// results for all 11 operations. The RISC-V guest produces deterministic
// output for proof generation; the precompile itself is executed natively.
//
// Part of the K+ roadmap: RISC-V precompile coverage.
package zkvm

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Vectorized zkISA operation selector.
const ZKISAOpVectorized uint32 = 0x20

// Gas cost for vectorized zkISA operation.
const zkisaGasVectorized uint64 = 5000

// Vectorized opcodes (matching core/vm constants).
const (
	guestVOpADD    byte = 0x01
	guestVOpMUL    byte = 0x02
	guestVOpSUB    byte = 0x03
	guestVOpAND    byte = 0x04
	guestVOpOR     byte = 0x05
	guestVOpXOR    byte = 0x06
	guestVOpSHL    byte = 0x07
	guestVOpSHR    byte = 0x08
	guestVOpMOD    byte = 0x09
	guestVOpREDUCE byte = 0x0A
	guestVOpDOT    byte = 0x0B
)

// Errors for guest vectorized.
var (
	ErrGuestVecInputTooShort = errors.New("guest_vec: input too short")
	ErrGuestVecInvalidOp     = errors.New("guest_vec: invalid opcode")
	ErrGuestVecInvalidWidth  = errors.New("guest_vec: invalid width")
	ErrGuestVecZeroCount     = errors.New("guest_vec: zero count")
	ErrGuestVecDataTooShort  = errors.New("guest_vec: insufficient data")
)

// BuildVectorizedGuest returns a RISC-V guest program that implements the
// vectorized math precompile simulation. The guest:
//  1. Reads all input bytes via ECALL(2), mixing into a hash accumulator
//  2. Uses the opcode byte (first input byte) as differentiation seed
//  3. Outputs deterministic result bytes via ECALL(1)
//
// This follows the same pattern as other precompile guests in the codebase
// (guest_bls12381.go, guest_misc.go). The output length is determined by
// the operation: reduction/dot ops produce a single element (4 bytes),
// while binary ops produce count*width bytes.
//
// For the RISC-V guest, we output a fixed 32 bytes (sufficient for any
// single vectorized result) derived from the hash of all input.
func BuildVectorizedGuest() []byte {
	// Use the existing buildVecHashGuest pattern: read all input, hash it,
	// output deterministic bytes.
	return buildVecHashGuest(0x21, 32)
}

// buildVecHashGuest builds a RISC-V guest that reads input, hashes it via
// register arithmetic, and outputs resultLen bytes. The tag distinguishes
// this guest from others.
func buildVecHashGuest(tag byte, resultLen int) []byte {
	instrs := []uint32{
		// x5 = tag (hash accumulator seed)
		EncodeIType(0x13, 5, 0, 0, int32(tag)),
		// x6 = 0 (byte counter)
		EncodeIType(0x13, 6, 0, 0, 0),
		// x7 = resultLen
		EncodeIType(0x13, 7, 0, 0, int32(resultLen)),

		// INPUT_LOOP:
		EncodeIType(0x13, 17, 0, 0, 2), // [3] a7 = 2
		0x00000073,                      // [4] ECALL

		// EOF check
		EncodeUType(0x37, 8, 0xFFFFF000), // [5]
		EncodeIType(0x13, 8, 6, 8, -1),   // [6]
		EncodeBType(0x63, 0, 10, 8, 20),  // [7] BEQ -> OUTPUT

		// Mix: x5 = x5 * 41 + a0
		EncodeIType(0x13, 9, 0, 0, 41),       // [8]
		EncodeRType(0x33, 5, 0, 5, 9, 0x01),  // [9] MUL
		EncodeRType(0x33, 5, 0, 5, 10, 0x00), // [10] ADD

		// Back to INPUT_LOOP
		EncodeJType(0x6F, 0, -32), // [11]

		// OUTPUT:
		EncodeIType(0x13, 6, 0, 0, 0), // [12] x6 = 0

		// OUTPUT_LOOP:
		EncodeBType(0x63, 0, 6, 7, 32), // [13] BEQ x6, x7, +32 -> HALT

		// a0 = x5 & 0xFF
		EncodeIType(0x13, 10, 7, 5, 0xFF), // [14] ANDI

		// a7 = 1, ECALL
		EncodeIType(0x13, 17, 0, 0, 1), // [15]
		0x00000073,                      // [16] ECALL

		// x5 = x5 >> 3 (SRLI x5, x5, 3)
		(0 << 25) | (3 << 20) | (5 << 15) | (5 << 12) | (5 << 7) | 0x13, // [17]

		// x5 = x5 XOR tag
		EncodeIType(0x13, 5, 4, 5, int32(tag)), // [18] XORI

		// x6 = x6 + 1
		EncodeIType(0x13, 6, 0, 6, 1), // [19]

		// Back to OUTPUT_LOOP
		EncodeJType(0x6F, 0, -28), // [20]

		// HALT:
		EncodeIType(0x13, 17, 0, 0, 0), // [21]
		EncodeIType(0x13, 10, 0, 0, 0), // [22]
		0x00000073,                      // [23]
	}

	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}
	return code
}

// ExecuteVectorizedGo performs the vectorized math operation in pure Go,
// matching the precompile behavior exactly. Used as the reference
// implementation for bit-exact comparison.
func ExecuteVectorizedGo(input []byte) ([]byte, error) {
	if len(input) < 6 {
		return nil, ErrGuestVecInputTooShort
	}
	opcode := input[0]
	width := input[1]
	count := binary.BigEndian.Uint32(input[2:6])
	data := input[6:]

	if opcode < guestVOpADD || opcode > guestVOpDOT {
		return nil, fmt.Errorf("%w: 0x%02x", ErrGuestVecInvalidOp, opcode)
	}
	if width != 4 && width != 8 {
		return nil, ErrGuestVecInvalidWidth
	}
	if count == 0 {
		return nil, ErrGuestVecZeroCount
	}

	isReduction := opcode == guestVOpREDUCE
	var requiredElems int
	if isReduction {
		requiredElems = int(count)
	} else {
		requiredElems = int(count) * 2
	}
	if len(data) < requiredElems*int(width) {
		return nil, ErrGuestVecDataTooShort
	}

	if width == 4 {
		return executeVec32Go(opcode, count, data)
	}
	return executeVec64Go(opcode, count, data)
}

func executeVec32Go(opcode byte, count uint32, data []byte) ([]byte, error) {
	n := int(count)
	readU32 := func(offset int) uint32 {
		return binary.BigEndian.Uint32(data[offset*4 : (offset+1)*4])
	}

	switch opcode {
	case guestVOpREDUCE:
		var sum uint32
		for i := 0; i < n; i++ {
			sum += readU32(i)
		}
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, sum)
		return out, nil
	case guestVOpDOT:
		var dot uint32
		for i := 0; i < n; i++ {
			dot += readU32(i) * readU32(n+i)
		}
		out := make([]byte, 4)
		binary.BigEndian.PutUint32(out, dot)
		return out, nil
	default:
		out := make([]byte, n*4)
		for i := 0; i < n; i++ {
			a := readU32(i)
			b := readU32(n + i)
			var r uint32
			switch opcode {
			case guestVOpADD:
				r = a + b
			case guestVOpMUL:
				r = a * b
			case guestVOpSUB:
				r = a - b
			case guestVOpAND:
				r = a & b
			case guestVOpOR:
				r = a | b
			case guestVOpXOR:
				r = a ^ b
			case guestVOpSHL:
				r = a << (b & 31)
			case guestVOpSHR:
				r = a >> (b & 31)
			case guestVOpMOD:
				if b == 0 {
					r = 0
				} else {
					r = a % b
				}
			}
			binary.BigEndian.PutUint32(out[i*4:], r)
		}
		return out, nil
	}
}

func executeVec64Go(opcode byte, count uint32, data []byte) ([]byte, error) {
	n := int(count)
	readU64 := func(offset int) uint64 {
		return binary.BigEndian.Uint64(data[offset*8 : (offset+1)*8])
	}

	switch opcode {
	case guestVOpREDUCE:
		var sum uint64
		for i := 0; i < n; i++ {
			sum += readU64(i)
		}
		out := make([]byte, 8)
		binary.BigEndian.PutUint64(out, sum)
		return out, nil
	case guestVOpDOT:
		var dot uint64
		for i := 0; i < n; i++ {
			dot += readU64(i) * readU64(n+i)
		}
		out := make([]byte, 8)
		binary.BigEndian.PutUint64(out, dot)
		return out, nil
	default:
		out := make([]byte, n*8)
		for i := 0; i < n; i++ {
			a := readU64(i)
			b := readU64(n + i)
			var r uint64
			switch opcode {
			case guestVOpADD:
				r = a + b
			case guestVOpMUL:
				r = a * b
			case guestVOpSUB:
				r = a - b
			case guestVOpAND:
				r = a & b
			case guestVOpOR:
				r = a | b
			case guestVOpXOR:
				r = a ^ b
			case guestVOpSHL:
				r = a << (b & 63)
			case guestVOpSHR:
				r = a >> (b & 63)
			case guestVOpMOD:
				if b == 0 {
					r = 0
				} else {
					r = a % b
				}
			}
			binary.BigEndian.PutUint64(out[i*8:], r)
		}
		return out, nil
	}
}

// RegisterVectorizedGuest registers the vectorized guest program in the
// given GuestRegistry and returns its program ID.
func RegisterVectorizedGuest(registry *GuestRegistry) (types.Hash, error) {
	prog := BuildVectorizedGuest()
	id, err := registry.RegisterGuest(prog)
	if err != nil && err != ErrGuestAlreadyRegistered {
		return types.Hash{}, err
	}
	if err == ErrGuestAlreadyRegistered {
		id = crypto.Keccak256Hash(prog)
	}
	return id, nil
}

// RegisterVectorizedZKISAOp registers the vectorized math operation in the
// ZKISAOpTable with selector 0x20.
func RegisterVectorizedZKISAOp(table *ZKISAOpTable) {
	table.Register(&ZKISAOpEntry{
		Selector:       ZKISAOpVectorized,
		Name:           "vectorized",
		BaseGas:        zkisaGasVectorized,
		PerByteGas:     zkisaGasPerInputByte,
		PrecompileAddr: 0x21,
	})
}
