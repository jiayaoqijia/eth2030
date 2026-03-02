// guest_misc.go implements RISC-V guest programs for miscellaneous precompiles:
// RIPEMD-160 (0x03), BLAKE2f (0x09), Data Copy (0x04), and KZG point
// evaluation (0x0a). Each guest reads input via InputBuf, performs a
// hash-based simulation, and writes the result to OutputBuf.
//
// Part of the K+ roadmap: RISC-V precompile coverage (Stage 1: 80%).
package zkvm

import (
	"encoding/binary"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Misc zkISA operation selectors.
const (
	ZKISAOpRIPEMD160 uint32 = 0x12
	ZKISAOpBLAKE2f   uint32 = 0x13
	ZKISAOpDataCopy  uint32 = 0x14
	ZKISAOpKZGPoint  uint32 = 0x15
)

// Gas costs for misc zkISA operations.
const (
	zkisaGasRIPEMD160   uint64 = 3000
	zkisaGasBLAKE2f     uint64 = 3000
	zkisaGasDataCopy    uint64 = 500
	zkisaGasKZGPoint    uint64 = 50000
	zkisaGasMiscPerByte uint64 = 8
)

// BuildRIPEMD160Guest returns a RISC-V guest for RIPEMD-160 hashing.
func BuildRIPEMD160Guest() []byte {
	return buildMiscHashGuest(0x03, 32) // ripemd160 returns 32 bytes (padded)
}

// BuildBLAKE2fGuest returns a RISC-V guest for BLAKE2f compression.
func BuildBLAKE2fGuest() []byte {
	return buildMiscHashGuest(0x09, 64) // blake2f returns 64 bytes
}

// BuildDataCopyGuest returns a RISC-V guest for the identity (data copy)
// precompile. It reads all input bytes and outputs them unchanged.
func BuildDataCopyGuest() []byte {
	// Layout:
	//   [0]  offset  0: ADDI x17, x0, 2        -- a7 = 2 (read)
	//   [1]  offset  4: ECALL                   -- a0 = byte or EOF
	//   [2]  offset  8: LUI x8, 0xFFFFF000     -- build EOF marker
	//   [3]  offset 12: ORI x8, x8, 0xFFF      -- x8 = 0xFFFFFFFF
	//   [4]  offset 16: BEQ x10, x8, +16       -- if EOF, goto [8] at 32
	//   [5]  offset 20: ADDI x17, x0, 1        -- a7 = 1 (output)
	//   [6]  offset 24: ECALL                   -- write byte
	//   [7]  offset 28: JAL x0, -28             -- back to [0] at 0
	//   [8]  offset 32: ADDI x17, x0, 0        -- HALT: a7 = 0
	//   [9]  offset 36: ADDI x10, x0, 0        -- a0 = 0
	//   [10] offset 40: ECALL

	instrs := []uint32{
		EncodeIType(0x13, 17, 0, 0, 2),        // [0]
		0x00000073,                             // [1]
		EncodeUType(0x37, 8, 0xFFFFF000),       // [2]
		EncodeIType(0x13, 8, 6, 8, -1),         // [3]
		EncodeBType(0x63, 0, 10, 8, 16),        // [4] BEQ -> [8]
		EncodeIType(0x13, 17, 0, 0, 1),         // [5]
		0x00000073,                             // [6]
		EncodeJType(0x6F, 0, -28),              // [7] JAL -> [0]
		EncodeIType(0x13, 17, 0, 0, 0),         // [8]
		EncodeIType(0x13, 10, 0, 0, 0),         // [9]
		0x00000073,                             // [10]
	}

	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}
	return code
}

// BuildKZGPointEvalGuest returns a RISC-V guest for KZG point evaluation.
func BuildKZGPointEvalGuest() []byte {
	return buildMiscHashGuest(0x0a, 64) // KZG returns 64 bytes
}

// buildMiscHashGuest builds a generic RISC-V guest that reads input,
// accumulates a hash via MUL/ADD, and outputs resultLen bytes.
//
// Instruction layout:
//
//	[0]  offset  0: ADDI x5, x0, tag
//	[1]  offset  4: ADDI x6, x0, 0
//	[2]  offset  8: ADDI x7, x0, resultLen
//	[3]  offset 12: ADDI x17, x0, 2           -- INPUT_LOOP
//	[4]  offset 16: ECALL
//	[5]  offset 20: LUI x8, 0xFFFFF000
//	[6]  offset 24: ORI x8, x8, 0xFFF
//	[7]  offset 28: BEQ x10, x8, +20          -- if EOF, goto [12] at 48
//	[8]  offset 32: ADDI x9, x0, 37
//	[9]  offset 36: MUL x5, x5, x9
//	[10] offset 40: ADD x5, x5, x10
//	[11] offset 44: JAL x0, -32               -- back to [3] at 12
//	[12] offset 48: ADDI x6, x0, 0            -- OUTPUT
//	[13] offset 52: BEQ x6, x7, +28           -- OUTPUT_LOOP: if done, goto [20] at 80
//	[14] offset 56: ANDI x10, x5, 0xFF
//	[15] offset 60: ADDI x17, x0, 1
//	[16] offset 64: ECALL
//	[17] offset 68: SRLI x5, x5, 3
//	[18] offset 72: XORI x5, x5, tag
//	[19] offset 76: ADDI x6, x6, 1
//	[20] offset 80: JAL x0, -28               -- back to [13] at 52
//	[21] offset 84: ADDI x17, x0, 0           -- HALT
//	[22] offset 88: ADDI x10, x0, 0
//	[23] offset 92: ECALL
func buildMiscHashGuest(tag byte, resultLen int) []byte {
	// SRLI x5, x5, 3
	srliX5X53 := (0 << 25) | (3 << 20) | (5 << 15) | (5 << 12) | (5 << 7) | 0x13

	instrs := []uint32{
		EncodeIType(0x13, 5, 0, 0, int32(tag)),       // [0]
		EncodeIType(0x13, 6, 0, 0, 0),                // [1]
		EncodeIType(0x13, 7, 0, 0, int32(resultLen)),  // [2]
		EncodeIType(0x13, 17, 0, 0, 2),                // [3] INPUT_LOOP
		0x00000073,                                    // [4]
		EncodeUType(0x37, 8, 0xFFFFF000),               // [5]
		EncodeIType(0x13, 8, 6, 8, -1),                 // [6]
		EncodeBType(0x63, 0, 10, 8, 20),                // [7] BEQ -> [12]
		EncodeIType(0x13, 9, 0, 0, 37),                 // [8]
		EncodeRType(0x33, 5, 0, 5, 9, 0x01),            // [9] MUL
		EncodeRType(0x33, 5, 0, 5, 10, 0x00),           // [10] ADD
		EncodeJType(0x6F, 0, -32),                      // [11] -> [3]
		EncodeIType(0x13, 6, 0, 0, 0),                 // [12] OUTPUT
		EncodeBType(0x63, 0, 6, 7, 32),                 // [13] BEQ -> [21] at 84
		EncodeIType(0x13, 10, 7, 5, 0xFF),              // [14] ANDI
		EncodeIType(0x13, 17, 0, 0, 1),                 // [15]
		0x00000073,                                    // [16]
		uint32(srliX5X53),                             // [17] SRLI
		EncodeIType(0x13, 5, 4, 5, int32(tag)),         // [18] XORI
		EncodeIType(0x13, 6, 0, 6, 1),                 // [19] x6++
		EncodeJType(0x6F, 0, -28),                      // [20] -> [13]
		EncodeIType(0x13, 17, 0, 0, 0),                 // [21] HALT
		EncodeIType(0x13, 10, 0, 0, 0),                 // [22]
		0x00000073,                                    // [23]
	}

	code := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(code[i*4:], instr)
	}
	return code
}

// RegisterMiscGuests registers RIPEMD-160, BLAKE2f, DataCopy, and KZG
// guest programs in the given GuestRegistry.
func RegisterMiscGuests(registry *GuestRegistry) (map[string]types.Hash, error) {
	ids := make(map[string]types.Hash)
	guests := map[string][]byte{
		"ripemd160": BuildRIPEMD160Guest(),
		"blake2f":   BuildBLAKE2fGuest(),
		"datacopy":  BuildDataCopyGuest(),
		"kzg-point": BuildKZGPointEvalGuest(),
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

// RegisterMiscZKISAOps registers misc precompile operations in the ZKISAOpTable.
func RegisterMiscZKISAOps(table *ZKISAOpTable) {
	ops := []struct {
		selector       uint32
		name           string
		baseGas        uint64
		perByteGas     uint64
		precompileAddr byte
	}{
		{ZKISAOpRIPEMD160, "ripemd160", zkisaGasRIPEMD160, zkisaGasMiscPerByte, 0x03},
		{ZKISAOpBLAKE2f, "blake2f", zkisaGasBLAKE2f, 0, 0x09},
		{ZKISAOpDataCopy, "datacopy", zkisaGasDataCopy, zkisaGasMiscPerByte, 0x04},
		{ZKISAOpKZGPoint, "kzg-point", zkisaGasKZGPoint, 0, 0x0a},
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
