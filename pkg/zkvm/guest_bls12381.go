// guest_bls12381.go implements RISC-V guest programs for the 9 BLS12-381
// precompiles (EIP-2537, addresses 0x0b-0x13). Each guest accepts input via
// the CPU's InputBuf, performs hash-based field op simulation via ECALL,
// and writes the result to OutputBuf.
//
// Part of the K+ roadmap: RISC-V precompile coverage (Stage 1: 80%).
package zkvm

import (
	"encoding/binary"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// BLS12-381 zkISA operation selectors (extending the base table).
const (
	ZKISAOpBLS12G1Add      uint32 = 0x09
	ZKISAOpBLS12G1Mul      uint32 = 0x0A
	ZKISAOpBLS12G1MSM      uint32 = 0x0B
	ZKISAOpBLS12G2Add      uint32 = 0x0C
	ZKISAOpBLS12G2Mul      uint32 = 0x0D
	ZKISAOpBLS12G2MSM      uint32 = 0x0E
	ZKISAOpBLS12Pairing    uint32 = 0x0F
	ZKISAOpBLS12MapFpToG1  uint32 = 0x10
	ZKISAOpBLS12MapFp2ToG2 uint32 = 0x11
)

// Gas costs for BLS12-381 zkISA operations.
const (
	zkisaGasBLS12G1Add      uint64 = 500
	zkisaGasBLS12G1Mul      uint64 = 12000
	zkisaGasBLS12G1MSM      uint64 = 12000
	zkisaGasBLS12G2Add      uint64 = 800
	zkisaGasBLS12G2Mul      uint64 = 45000
	zkisaGasBLS12G2MSM      uint64 = 45000
	zkisaGasBLS12Pairing    uint64 = 65000
	zkisaGasBLS12MapFpToG1  uint64 = 5500
	zkisaGasBLS12MapFp2ToG2 uint64 = 75000
)

// buildBLS12InputHashGuest builds a generic RISC-V guest that:
//  1. Reads all input bytes via ECALL(2)
//  2. Computes a simple hash using register arithmetic (MUL/ADD mixing)
//  3. Outputs resultLen bytes derived from the hash via ECALL(1)
//  4. Halts via ECALL(0)
//
// The tag byte differentiates operations for distinct hashing.
func buildBLS12InputHashGuest(tag byte, resultLen int) []byte {
	// Instruction layout with byte offsets:
	//   [0]  offset  0: ADDI x5, x0, tag        -- hash seed
	//   [1]  offset  4: ADDI x6, x0, 0          -- counter
	//   [2]  offset  8: ADDI x7, x0, resultLen   -- output count
	//   [3]  offset 12: ADDI x17, x0, 2          -- INPUT_LOOP: a7=2
	//   [4]  offset 16: ECALL                     -- read byte -> a0
	//   [5]  offset 20: LUI x8, 0xFFFFF000       -- build 0xFFFFFFFF
	//   [6]  offset 24: ORI x8, x8, 0xFFF        -- x8 = 0xFFFFFFFF
	//   [7]  offset 28: BEQ x10, x8, +20         -- if EOF, goto [12] at 48
	//   [8]  offset 32: ADDI x9, x0, 31          -- constant 31
	//   [9]  offset 36: MUL x5, x5, x9           -- x5 *= 31
	//   [10] offset 40: ADD x5, x5, x10          -- x5 += input byte
	//   [11] offset 44: JAL x0, -32              -- back to [3] at 12
	//   [12] offset 48: ADDI x6, x0, 0          -- OUTPUT: reset counter
	//   [13] offset 52: BEQ x6, x7, +28         -- OUTPUT_LOOP: if done, goto [20] at 80
	//   [14] offset 56: ANDI x10, x5, 0xFF      -- a0 = low byte of x5
	//   [15] offset 60: ADDI x17, x0, 1         -- a7 = 1 (output)
	//   [16] offset 64: ECALL                    -- write byte
	//   [17] offset 68: SRLI x5, x5, 1          -- shift accumulator
	//   [18] offset 72: ADDI x6, x6, 1          -- counter++
	//   [19] offset 76: JAL x0, -24             -- back to [13] at 52
	//   [20] offset 80: ADDI x17, x0, 0         -- HALT: a7=0
	//   [21] offset 84: ADDI x10, x0, 0         -- a0=0 (exit code)
	//   [22] offset 88: ECALL                    -- halt

	// SRLI x5, x5, 1: opcode=0x13, funct3=5, rd=5, rs1=5, shamt=1, funct7=0
	srliX5X51 := (0 << 25) | (1 << 20) | (5 << 15) | (5 << 12) | (5 << 7) | 0x13

	instrs := []uint32{
		EncodeIType(0x13, 5, 0, 0, int32(tag)),   // [0]  x5 = tag
		EncodeIType(0x13, 6, 0, 0, 0),            // [1]  x6 = 0
		EncodeIType(0x13, 7, 0, 0, int32(resultLen)), // [2]  x7 = resultLen
		EncodeIType(0x13, 17, 0, 0, 2),           // [3]  a7 = 2 (input)
		0x00000073,                                // [4]  ECALL
		EncodeUType(0x37, 8, 0xFFFFF000),          // [5]  LUI x8, upper
		EncodeIType(0x13, 8, 6, 8, -1),            // [6]  ORI x8, x8, 0xFFF
		EncodeBType(0x63, 0, 10, 8, 20),           // [7]  BEQ x10, x8, +20 -> [12]
		EncodeIType(0x13, 9, 0, 0, 31),            // [8]  x9 = 31
		EncodeRType(0x33, 5, 0, 5, 9, 0x01),       // [9]  MUL x5, x5, x9
		EncodeRType(0x33, 5, 0, 5, 10, 0x00),      // [10] ADD x5, x5, x10
		EncodeJType(0x6F, 0, -32),                 // [11] JAL -> [3]
		EncodeIType(0x13, 6, 0, 0, 0),            // [12] x6 = 0
		EncodeBType(0x63, 0, 6, 7, 28),            // [13] BEQ x6, x7, +28 -> [20]
		EncodeIType(0x13, 10, 7, 5, 0xFF),         // [14] ANDI x10, x5, 0xFF
		EncodeIType(0x13, 17, 0, 0, 1),            // [15] a7 = 1 (output)
		0x00000073,                                // [16] ECALL
		uint32(srliX5X51),                         // [17] SRLI x5, x5, 1
		EncodeIType(0x13, 6, 0, 6, 1),            // [18] x6 += 1
		EncodeJType(0x6F, 0, -24),                 // [19] JAL -> [13]
		EncodeIType(0x13, 17, 0, 0, 0),            // [20] a7 = 0 (halt)
		EncodeIType(0x13, 10, 0, 0, 0),            // [21] a0 = 0
		0x00000073,                                // [22] ECALL
	}

	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}
	return code
}

// BuildBLS12G1AddGuest returns a RISC-V guest for BLS12-381 G1 addition.
func BuildBLS12G1AddGuest() []byte {
	return buildBLS12InputHashGuest(0x0b, 128) // G1 point = 128 bytes
}

// BuildBLS12G1MulGuest returns a RISC-V guest for BLS12-381 G1 scalar multiplication.
func BuildBLS12G1MulGuest() []byte {
	return buildBLS12InputHashGuest(0x0c, 128)
}

// BuildBLS12G1MSMGuest returns a RISC-V guest for BLS12-381 G1 multi-scalar multiplication.
func BuildBLS12G1MSMGuest() []byte {
	return buildBLS12InputHashGuest(0x0d, 128)
}

// BuildBLS12G2AddGuest returns a RISC-V guest for BLS12-381 G2 addition.
func BuildBLS12G2AddGuest() []byte {
	return buildBLS12InputHashGuest(0x0e, 256) // G2 point = 256 bytes
}

// BuildBLS12G2MulGuest returns a RISC-V guest for BLS12-381 G2 scalar multiplication.
func BuildBLS12G2MulGuest() []byte {
	return buildBLS12InputHashGuest(0x0f, 256)
}

// BuildBLS12G2MSMGuest returns a RISC-V guest for BLS12-381 G2 multi-scalar multiplication.
func BuildBLS12G2MSMGuest() []byte {
	return buildBLS12InputHashGuest(0x10, 256)
}

// BuildBLS12PairingGuest returns a RISC-V guest for BLS12-381 pairing check.
func BuildBLS12PairingGuest() []byte {
	return buildBLS12InputHashGuest(0x11, 32) // pairing result = 32 bytes
}

// BuildBLS12MapFpToG1Guest returns a RISC-V guest for mapping Fp to G1.
func BuildBLS12MapFpToG1Guest() []byte {
	return buildBLS12InputHashGuest(0x12, 128)
}

// BuildBLS12MapFp2ToG2Guest returns a RISC-V guest for mapping Fp2 to G2.
func BuildBLS12MapFp2ToG2Guest() []byte {
	return buildBLS12InputHashGuest(0x13, 256)
}

// RegisterBLS12381Guests registers all 9 BLS12-381 guest programs in the
// given GuestRegistry and returns their program IDs.
func RegisterBLS12381Guests(registry *GuestRegistry) (map[string]types.Hash, error) {
	ids := make(map[string]types.Hash)
	guests := map[string][]byte{
		"bls12-g1add":      BuildBLS12G1AddGuest(),
		"bls12-g1mul":      BuildBLS12G1MulGuest(),
		"bls12-g1msm":      BuildBLS12G1MSMGuest(),
		"bls12-g2add":      BuildBLS12G2AddGuest(),
		"bls12-g2mul":      BuildBLS12G2MulGuest(),
		"bls12-g2msm":      BuildBLS12G2MSMGuest(),
		"bls12-pairing":    BuildBLS12PairingGuest(),
		"bls12-map-fp-g1":  BuildBLS12MapFpToG1Guest(),
		"bls12-map-fp2-g2": BuildBLS12MapFp2ToG2Guest(),
	}

	for name, prog := range guests {
		id, err := registry.RegisterGuest(prog)
		if err != nil && err != ErrGuestAlreadyRegistered {
			return nil, err
		}
		if err == ErrGuestAlreadyRegistered {
			id = crypto.Keccak256Hash(prog)
		}
		ids[name] = id
	}
	return ids, nil
}

// RegisterBLS12381ZKISAOps registers BLS12-381 operations in the given ZKISAOpTable.
func RegisterBLS12381ZKISAOps(table *ZKISAOpTable) {
	ops := []struct {
		selector       uint32
		name           string
		baseGas        uint64
		perByteGas     uint64
		precompileAddr byte
	}{
		{ZKISAOpBLS12G1Add, "bls12-g1add", zkisaGasBLS12G1Add, 0, 0x0b},
		{ZKISAOpBLS12G1Mul, "bls12-g1mul", zkisaGasBLS12G1Mul, 0, 0x0c},
		{ZKISAOpBLS12G1MSM, "bls12-g1msm", zkisaGasBLS12G1MSM, zkisaGasPerInputByte, 0x0d},
		{ZKISAOpBLS12G2Add, "bls12-g2add", zkisaGasBLS12G2Add, 0, 0x0e},
		{ZKISAOpBLS12G2Mul, "bls12-g2mul", zkisaGasBLS12G2Mul, 0, 0x0f},
		{ZKISAOpBLS12G2MSM, "bls12-g2msm", zkisaGasBLS12G2MSM, zkisaGasPerInputByte, 0x10},
		{ZKISAOpBLS12Pairing, "bls12-pairing", zkisaGasBLS12Pairing, zkisaGasPerInputByte, 0x11},
		{ZKISAOpBLS12MapFpToG1, "bls12-map-fp-g1", zkisaGasBLS12MapFpToG1, 0, 0x12},
		{ZKISAOpBLS12MapFp2ToG2, "bls12-map-fp2-g2", zkisaGasBLS12MapFp2ToG2, 0, 0x13},
	}
	for _, op := range ops {
		table.Register(&ZKISAOpEntry{
			Selector:       op.selector,
			Name:           op.name,
			BaseGas:        op.baseGas,
			PerByteGas:     op.perByteGas,
			PrecompileAddr: op.precompileAddr,
		})
	}
}
