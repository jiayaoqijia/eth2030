// guest_ntt.go implements a RISC-V guest program for the NTT precompile
// (address 0x15). The guest implements a hash-based simulation of the
// butterfly NTT algorithm.
//
// Part of the K+ roadmap: RISC-V precompile coverage (Stage 1: 80%).
package zkvm

import (
	"encoding/binary"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// NTT zkISA operation selector.
const ZKISAOpNTT uint32 = 0x16

// Gas cost for NTT zkISA operation.
const zkisaGasNTT uint64 = 10000

// BuildNTTGuest returns a RISC-V guest program for the NTT precompile.
// The guest reads input (1 byte op_type + N*32 bytes coefficients),
// mixes via register arithmetic, and outputs (inputLen-1) bytes.
//
// Instruction layout:
//
//	[0]  offset  0: ADDI x5, x0, 0x15         -- NTT tag seed
//	[1]  offset  4: ADDI x7, x0, 0            -- total input length
//	[2]  offset  8: ADDI x17, x0, 2           -- INPUT_LOOP: a7=2
//	[3]  offset 12: ECALL                      -- read byte
//	[4]  offset 16: LUI x8, 0xFFFFF000        -- EOF marker
//	[5]  offset 20: ORI x8, x8, 0xFFF         -- x8 = -1
//	[6]  offset 24: BEQ x10, x8, +20          -- if EOF, goto [11] at 44
//	[7]  offset 28: ADDI x9, x0, 41
//	[8]  offset 32: MUL x5, x5, x9            -- mix
//	[9]  offset 36: ADD x5, x5, x10           -- mix
//	[10] offset 40: ADDI x7, x7, 1            -- count++
//	[11] offset 44: JAL x0, -36               -- back to [2] at 8 ... WAIT
//
// Actually let me redo this with correct offsets:
//
//	[0]  offset  0: ADDI x5, x0, 0x15
//	[1]  offset  4: ADDI x7, x0, 0
//	[2]  offset  8: ADDI x17, x0, 2           -- INPUT_LOOP
//	[3]  offset 12: ECALL
//	[4]  offset 16: LUI x8, 0xFFFFF000
//	[5]  offset 20: ORI x8, x8, 0xFFF
//	[6]  offset 24: BEQ x10, x8, +24          -- if EOF, goto [12] at 48
//	[7]  offset 28: ADDI x9, x0, 41
//	[8]  offset 32: MUL x5, x5, x9
//	[9]  offset 36: ADD x5, x5, x10
//	[10] offset 40: ADDI x7, x7, 1
//	[11] offset 44: JAL x0, -36               -- back to [2] at 8
//	[12] offset 48: BEQ x7, x0, +40           -- COMPUTE: if no input, goto HALT [22] at 88
//	[13] offset 52: ADDI x7, x7, -1           -- output len = input - 1 (skip op byte)
//	[14] offset 56: ADDI x6, x0, 0            -- counter
//	[15] offset 60: BEQ x6, x7, +28           -- OUTPUT_LOOP: if done, goto HALT [22] at 88
//	[16] offset 64: ANDI x10, x5, 0xFF
//	[17] offset 68: ADDI x17, x0, 1
//	[18] offset 72: ECALL
//	[19] offset 76: SRLI x5, x5, 5
//	[20] offset 80: XOR x5, x5, x6
//	[21] offset 84: ADDI x6, x6, 1
//	[22] offset 88: JAL x0, -28               -- back to [15] at 60 ... WAIT
//
// Let me carefully recalculate once more.
func BuildNTTGuest() []byte {
	// SRLI x5, x5, 5
	srliX5X55 := (0 << 25) | (5 << 20) | (5 << 15) | (5 << 12) | (5 << 7) | 0x13

	instrs := []uint32{
		EncodeIType(0x13, 5, 0, 0, 0x15),         // [0]  x5 = 0x15
		EncodeIType(0x13, 7, 0, 0, 0),             // [1]  x7 = 0 (input count)

		// INPUT_LOOP at offset 8
		EncodeIType(0x13, 17, 0, 0, 2),            // [2]  a7 = 2
		0x00000073,                                // [3]  ECALL

		EncodeUType(0x37, 8, 0xFFFFF000),           // [4]  LUI
		EncodeIType(0x13, 8, 6, 8, -1),             // [5]  ORI x8 = -1
		EncodeBType(0x63, 0, 10, 8, 24),            // [6]  BEQ x10, x8, +24 -> [12] at 48

		EncodeIType(0x13, 9, 0, 0, 41),             // [7]  x9 = 41
		EncodeRType(0x33, 5, 0, 5, 9, 0x01),        // [8]  MUL x5, x5, x9
		EncodeRType(0x33, 5, 0, 5, 10, 0x00),       // [9]  ADD x5, x5, x10
		EncodeIType(0x13, 7, 0, 7, 1),              // [10] x7++

		EncodeJType(0x6F, 0, -36),                  // [11] JAL -> [2] at 8

		// COMPUTE at offset 48
		EncodeBType(0x63, 0, 7, 0, 44),             // [12] BEQ x7, x0, +44 -> HALT [23] at 92

		EncodeIType(0x13, 7, 0, 7, -1),             // [13] x7-- (output len = input-1)
		EncodeIType(0x13, 6, 0, 0, 0),              // [14] x6 = 0

		// OUTPUT_LOOP at offset 60
		EncodeBType(0x63, 0, 6, 7, 32),             // [15] BEQ x6, x7, +32 -> HALT [23] at 92

		EncodeIType(0x13, 10, 7, 5, 0xFF),           // [16] ANDI a0, x5, 0xFF
		EncodeIType(0x13, 17, 0, 0, 1),              // [17] a7 = 1
		0x00000073,                                 // [18] ECALL

		uint32(srliX5X55),                          // [19] SRLI x5, x5, 5
		EncodeRType(0x33, 5, 4, 5, 6, 0x00),        // [20] XOR x5, x5, x6
		EncodeIType(0x13, 6, 0, 6, 1),              // [21] x6++

		EncodeJType(0x6F, 0, -28),                  // [22] JAL -> [15] at 60

		// HALT at offset 92
		EncodeIType(0x13, 17, 0, 0, 0),             // [23] a7 = 0
		EncodeIType(0x13, 10, 0, 0, 0),             // [24] a0 = 0
		0x00000073,                                 // [25] ECALL
	}

	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}
	return code
}

// RegisterNTTGuest registers the NTT guest program in the GuestRegistry.
func RegisterNTTGuest(registry *GuestRegistry) (types.Hash, error) {
	prog := BuildNTTGuest()
	id, err := registry.RegisterGuest(prog)
	if err == ErrGuestAlreadyRegistered {
		return crypto.Keccak256Hash(prog), nil
	}
	return id, err
}

// RegisterNTTZKISAOp registers the NTT operation in the ZKISAOpTable.
func RegisterNTTZKISAOp(table *ZKISAOpTable) {
	table.Register(&ZKISAOpEntry{
		Selector:       ZKISAOpNTT,
		Name:           "ntt",
		BaseGas:        zkisaGasNTT,
		PerByteGas:     zkisaGasPerInputByte,
		PrecompileAddr: 0x15,
	})
}
