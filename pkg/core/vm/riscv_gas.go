// riscv_gas.go implements gas metering calibration for RISC-V contract
// execution. Each RV32IM instruction class has a calibrated gas cost that
// maintains economic neutrality with EVM gas costs.
//
// Part of the K+ roadmap: user-deployable RISC-V contracts (Stage 2).
package vm

// RISCVGasTable defines per-instruction-class gas costs for RISC-V execution.
var RISCVGasTable = struct {
	Arithmetic uint64 // ADD, SUB, AND, OR, XOR, SLT, etc.
	Multiply   uint64 // MUL, MULH, MULHSU, MULHU
	Divide     uint64 // DIV, DIVU, REM, REMU
	Memory     uint64 // LW, LH, LB, SW, SH, SB
	Branch     uint64 // BEQ, BNE, BLT, BGE, etc.
	Jump       uint64 // JAL, JALR
	Immediate  uint64 // LUI, AUIPC
	System     uint64 // ECALL itself (gas charged per-operation in ecall handler)
}{
	Arithmetic: 1,
	Multiply:   3,
	Divide:     5,
	Memory:     3,
	Branch:     2,
	Jump:       2,
	Immediate:  1,
	System:     0,
}

// RISCVGasCost returns the gas cost for a 32-bit RISC-V instruction.
func RISCVGasCost(instr uint32) uint64 {
	opcode := instr & 0x7F

	switch opcode {
	case 0x33: // R-type: register arithmetic / M extension
		funct7 := (instr >> 25) & 0x7F
		if funct7 == 0x01 {
			// M extension: check funct3 for multiply vs divide.
			funct3 := (instr >> 12) & 0x7
			if funct3 <= 3 {
				return RISCVGasTable.Multiply // MUL, MULH, MULHSU, MULHU
			}
			return RISCVGasTable.Divide // DIV, DIVU, REM, REMU
		}
		return RISCVGasTable.Arithmetic

	case 0x13: // I-type: immediate arithmetic (ADDI, SLTI, etc.)
		return RISCVGasTable.Arithmetic

	case 0x03: // Load (LB, LH, LW, LBU, LHU)
		return RISCVGasTable.Memory

	case 0x23: // Store (SB, SH, SW)
		return RISCVGasTable.Memory

	case 0x63: // Branch (BEQ, BNE, BLT, BGE, etc.)
		return RISCVGasTable.Branch

	case 0x67: // JALR
		return RISCVGasTable.Jump

	case 0x6F: // JAL
		return RISCVGasTable.Jump

	case 0x37: // LUI
		return RISCVGasTable.Immediate

	case 0x17: // AUIPC
		return RISCVGasTable.Immediate

	case 0x73: // SYSTEM (ECALL, EBREAK)
		return RISCVGasTable.System

	default:
		return 1 // Unknown instructions cost 1 gas.
	}
}

// CalibrateRISCVGas computes an equivalent RISC-V gas budget given an EVM
// gas budget. This maintains economic neutrality: the same computation
// should cost roughly the same regardless of whether it runs on EVM or RISC-V.
//
// The ratio is: riscvGas = evmGas * riscvOps / evmOps
// where evmOps is the number of EVM operations for a given workload and
// riscvOps is the equivalent number of RISC-V instructions.
func CalibrateRISCVGas(evmOps, riscvOps int, evmGas uint64) uint64 {
	if evmOps <= 0 {
		return evmGas
	}
	return evmGas * uint64(riscvOps) / uint64(evmOps)
}
