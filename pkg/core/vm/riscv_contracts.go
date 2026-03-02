// riscv_contracts.go implements user-deployable RISC-V contract support
// for the EVM. Contracts can be deployed with RISC-V RV32IM binaries as
// code, distinguished from EVM bytecode by a code type prefix byte.
//
// Part of the K+ roadmap: user-deployable RISC-V contracts (Stage 2).
package vm

import (
	"errors"

	"github.com/eth2030/eth2030/core/types"
)

// Code type constants distinguish EVM bytecode from RISC-V binaries.
const (
	CodeTypeEVM   byte = 0x01
	CodeTypeRISCV byte = 0x02
)

// Maximum RISC-V contract binary size (64KB).
const MaxRISCVBinarySize = 64 * 1024

// RISC-V contract errors.
var (
	ErrRISCVEmptyBinary    = errors.New("riscv_contract: empty binary")
	ErrRISCVUnaligned      = errors.New("riscv_contract: length not multiple of 4")
	ErrRISCVBinaryTooLarge = errors.New("riscv_contract: binary exceeds 64KB limit")
)

// validRV32IOpcodes contains the set of valid RISC-V RV32IM base opcode
// fields (bits [6:0]) used to detect RISC-V binaries.
var validRV32IOpcodes = map[byte]bool{
	0x33: true, // R-type (ADD, SUB, MUL, etc.)
	0x13: true, // I-type arithmetic (ADDI, SLTI, etc.)
	0x03: true, // Load (LB, LH, LW, LBU, LHU)
	0x23: true, // Store (SB, SH, SW)
	0x63: true, // Branch (BEQ, BNE, BLT, BGE, etc.)
	0x67: true, // JALR
	0x6F: true, // JAL
	0x37: true, // LUI
	0x17: true, // AUIPC
	0x73: true, // SYSTEM (ECALL, EBREAK)
}

// DetectCodeType returns CodeTypeRISCV if the code starts with a valid
// RV32IM instruction (checking opcode field bits [6:0]), otherwise CodeTypeEVM.
func DetectCodeType(code []byte) byte {
	if len(code) < 4 {
		return CodeTypeEVM
	}
	// RISC-V is little-endian; the opcode field is bits [6:0] of the first byte.
	opcodeBits := code[0] & 0x7F
	if validRV32IOpcodes[opcodeBits] {
		return CodeTypeRISCV
	}
	return CodeTypeEVM
}

// ValidateRISCVBinary performs basic validation on a RISC-V binary:
// non-empty, length is a multiple of 4 (instruction alignment), and
// size does not exceed MaxRISCVBinarySize.
func ValidateRISCVBinary(code []byte) error {
	if len(code) == 0 {
		return ErrRISCVEmptyBinary
	}
	if len(code)%4 != 0 {
		return ErrRISCVUnaligned
	}
	if len(code) > MaxRISCVBinarySize {
		return ErrRISCVBinaryTooLarge
	}
	return nil
}

// CodeStore is the interface for persisting contract code.
type CodeStore interface {
	SetCode(addr types.Address, code []byte)
	GetCode(addr types.Address) []byte
}

// RISCVContractDeployer handles deployment of RISC-V contract binaries.
// It stores the binary with a 1-byte prefix indicating the code type.
type RISCVContractDeployer struct {
	store CodeStore
}

// NewRISCVContractDeployer creates a deployer backed by the given store.
func NewRISCVContractDeployer(store CodeStore) *RISCVContractDeployer {
	return &RISCVContractDeployer{store: store}
}

// Deploy validates and stores a RISC-V binary as contract code at addr.
// The stored code is prefixed with CodeTypeRISCV (0x02).
func (d *RISCVContractDeployer) Deploy(code []byte, addr types.Address) error {
	if err := ValidateRISCVBinary(code); err != nil {
		return err
	}
	// Prefix with code type byte.
	stored := make([]byte, 1+len(code))
	stored[0] = CodeTypeRISCV
	copy(stored[1:], code)
	d.store.SetCode(addr, stored)
	return nil
}

// IsRISCVContract checks whether the stored code has the RISC-V type prefix.
func IsRISCVContract(code []byte) bool {
	return len(code) > 0 && code[0] == CodeTypeRISCV
}

// GetRISCVBinary extracts the raw RISC-V binary from prefixed contract code.
// Returns nil if the code is not a RISC-V contract.
func GetRISCVBinary(code []byte) []byte {
	if !IsRISCVContract(code) {
		return nil
	}
	return code[1:]
}
