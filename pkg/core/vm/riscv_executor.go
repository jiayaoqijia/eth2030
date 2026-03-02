// riscv_executor.go implements the RISC-V contract executor that bridges
// RVCPU execution with EVM StateDB for persistent storage access. It adds
// ECALL-based syscalls for SLOAD, SSTORE, BALANCE, CALLER, CALLVALUE,
// and inter-contract CALL.
//
// Part of the K+ roadmap: user-deployable RISC-V contracts (Stage 2).
package vm

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/zkvm"
)

// ECALL syscall numbers for RISC-V contracts.
const (
	RVSyscallHalt      uint32 = 0 // Halt (existing)
	RVSyscallOutput    uint32 = 1 // Output byte (existing)
	RVSyscallInput     uint32 = 2 // Read input byte (existing)
	RVSyscallSload     uint32 = 3 // Storage load: key in a0, value returned in a0
	RVSyscallSstore    uint32 = 4 // Storage store: key in a0, value in a1
	RVSyscallBalance   uint32 = 5 // Balance: result in a0 (low 32 bits)
	RVSyscallCaller    uint32 = 6 // Caller: result in a0 (low 32 bits)
	RVSyscallCallvalue uint32 = 7 // Call value: result in a0 (low 32 bits)
	RVSyscallCall      uint32 = 8 // Call: addr in a0, value in a1, result in a0
)

// StorageResolver provides state access for RISC-V contract execution.
type StorageResolver interface {
	SLoad(addr types.Address, key types.Hash) types.Hash
	SStore(addr types.Address, key, value types.Hash)
	GetBalance(addr types.Address) *big.Int
	AddressInAccessList(addr types.Address) bool
	AddAddressToAccessList(addr types.Address)
	SlotInAccessList(addr types.Address, slot types.Hash) (addressOk bool, slotOk bool)
	AddSlotToAccessList(addr types.Address, slot types.Hash)
}

// RISC-V executor errors.
var (
	ErrRVExecGasExhausted = errors.New("riscv_exec: gas exhausted")
	ErrRVExecFault        = errors.New("riscv_exec: execution fault")
	ErrRVExecBadExitCode  = errors.New("riscv_exec: non-zero exit code")
)

// RISCVExecutor wraps an RVCPU with StateDB access for storage operations.
type RISCVExecutor struct {
	resolver StorageResolver
	caller   types.Address
	target   types.Address
	value    *big.Int
	gasLimit uint64
	gasUsed  uint64

	// Storage slots accessed during execution for warm/cold tracking.
	warmSlots map[types.Hash]bool
}

// NewRISCVExecutor creates a new executor for running RISC-V contracts.
func NewRISCVExecutor(resolver StorageResolver, caller, target types.Address, value *big.Int, gasLimit uint64) *RISCVExecutor {
	if value == nil {
		value = new(big.Int)
	}
	return &RISCVExecutor{
		resolver:  resolver,
		caller:    caller,
		target:    target,
		value:     value,
		gasLimit:  gasLimit,
		warmSlots: make(map[types.Hash]bool),
	}
}

// Execute loads and runs a RISC-V binary with the given calldata, returning
// the output bytes, gas used, and any error.
func (e *RISCVExecutor) Execute(code, calldata []byte) ([]byte, uint64, error) {
	cpu := zkvm.NewRVCPU(0) // Gas is tracked by the executor, not the CPU.

	// Load program at standard base address.
	const programBase uint32 = 0x00010000
	const inputBase uint32 = 0x40000000
	const stackBase uint32 = 0x80000000

	if err := cpu.LoadProgram(code, programBase, programBase); err != nil {
		return nil, 0, err
	}

	// Load calldata into memory.
	if len(calldata) > 0 {
		if err := cpu.Memory.LoadSegment(inputBase, calldata); err != nil {
			return nil, 0, err
		}
	}

	// Set up registers: sp, a0 (input base), a1 (input length).
	cpu.Regs[2] = stackBase
	cpu.Regs[10] = inputBase
	cpu.Regs[11] = uint32(len(calldata))
	cpu.InputBuf = calldata

	// Run instruction-by-instruction to intercept ECALLs.
	for !cpu.Halted {
		// Check gas before each instruction.
		instrGas := e.instructionGasCost(cpu)
		if e.gasUsed+instrGas > e.gasLimit {
			return nil, e.gasUsed, ErrRVExecGasExhausted
		}

		// Peek at the current instruction to detect ECALL.
		instr, err := cpu.Memory.ReadWord(cpu.PC)
		if err != nil {
			return nil, e.gasUsed, ErrRVExecFault
		}

		opcode := instr & 0x7F
		if opcode == 0x73 && (instr>>12)&0x7 == 0 && (instr>>20) == 0 {
			// ECALL: handle storage syscalls.
			syscall := cpu.Regs[17] // a7
			if syscall >= RVSyscallSload && syscall <= RVSyscallCall {
				ecallGas, ecallErr := e.handleStorageEcall(cpu, syscall)
				if ecallErr != nil {
					return nil, e.gasUsed, ecallErr
				}
				e.gasUsed += ecallGas
				cpu.PC += 4
				continue
			}
		}

		// Normal instruction execution.
		if err := cpu.Step(); err != nil {
			if errors.Is(err, zkvm.ErrRVHalted) {
				break
			}
			return nil, e.gasUsed, err
		}
		e.gasUsed += instrGas
	}

	return cpu.OutputBuf, e.gasUsed, nil
}

// instructionGasCost returns the gas cost for the instruction at the CPU's
// current PC, using the RISCVGasTable.
func (e *RISCVExecutor) instructionGasCost(cpu *zkvm.RVCPU) uint64 {
	instr, err := cpu.Memory.ReadWord(cpu.PC)
	if err != nil {
		return 1 // Default to 1 if we can't read.
	}
	return RISCVGasCost(instr)
}

// handleStorageEcall processes storage-related ECALL syscalls and returns
// the gas cost of the operation.
func (e *RISCVExecutor) handleStorageEcall(cpu *zkvm.RVCPU, syscall uint32) (uint64, error) {
	switch syscall {
	case RVSyscallSload:
		return e.ecallSload(cpu)
	case RVSyscallSstore:
		return e.ecallSstore(cpu)
	case RVSyscallBalance:
		return e.ecallBalance(cpu)
	case RVSyscallCaller:
		return e.ecallCaller(cpu)
	case RVSyscallCallvalue:
		return e.ecallCallvalue(cpu)
	case RVSyscallCall:
		return e.ecallCall(cpu)
	default:
		return 0, ErrRVExecFault
	}
}

// ecallSload: reads storage key from CPU memory at address in a0 (32 bytes),
// writes the 32-byte value back to the same memory location, returns low
// 32 bits in a0.
func (e *RISCVExecutor) ecallSload(cpu *zkvm.RVCPU) (uint64, error) {
	keyAddr := cpu.Regs[10] // a0: memory address of the 32-byte key
	key := e.readHash(cpu, keyAddr)

	// Determine warm/cold gas cost.
	_, slotOk := e.resolver.SlotInAccessList(e.target, key)
	var gasCost uint64
	if slotOk {
		gasCost = GasSloadWarm
	} else {
		gasCost = GasSloadCold
		e.resolver.AddSlotToAccessList(e.target, key)
	}

	if e.gasUsed+gasCost > e.gasLimit {
		return 0, ErrRVExecGasExhausted
	}

	value := e.resolver.SLoad(e.target, key)

	// Write value back to memory and set a0 to low 32 bits.
	e.writeHash(cpu, keyAddr, value)
	cpu.Regs[10] = binary.LittleEndian.Uint32(value[28:32])

	return gasCost, nil
}

// ecallSstore: reads key from memory at a0 and value from memory at a1.
func (e *RISCVExecutor) ecallSstore(cpu *zkvm.RVCPU) (uint64, error) {
	keyAddr := cpu.Regs[10]   // a0: memory address of key
	valueAddr := cpu.Regs[11] // a1: memory address of value

	key := e.readHash(cpu, keyAddr)
	value := e.readHash(cpu, valueAddr)

	// Gas cost: simplified model -- new slot vs update.
	existing := e.resolver.SLoad(e.target, key)
	var gasCost uint64
	if existing == (types.Hash{}) && value != (types.Hash{}) {
		gasCost = GasSstoreNew
	} else {
		gasCost = GasSstoreUpdate
	}

	if e.gasUsed+gasCost > e.gasLimit {
		return 0, ErrRVExecGasExhausted
	}

	e.resolver.SStore(e.target, key, value)
	return gasCost, nil
}

// ecallBalance: returns balance of target address (low 32 bits in a0).
func (e *RISCVExecutor) ecallBalance(cpu *zkvm.RVCPU) (uint64, error) {
	warm := e.resolver.AddressInAccessList(e.target)
	var gasCost uint64
	if warm {
		gasCost = GasBalanceWarm
	} else {
		gasCost = GasBalanceCold
		e.resolver.AddAddressToAccessList(e.target)
	}

	if e.gasUsed+gasCost > e.gasLimit {
		return 0, ErrRVExecGasExhausted
	}

	bal := e.resolver.GetBalance(e.target)
	cpu.Regs[10] = uint32(bal.Uint64() & 0xFFFFFFFF)
	return gasCost, nil
}

// ecallCaller: returns caller address (low 32 bits in a0).
func (e *RISCVExecutor) ecallCaller(cpu *zkvm.RVCPU) (uint64, error) {
	// Last 4 bytes of the 20-byte address.
	cpu.Regs[10] = binary.BigEndian.Uint32(e.caller[16:20])
	return 0, nil // No gas cost for caller intrinsic.
}

// ecallCallvalue: returns call value (low 32 bits in a0).
func (e *RISCVExecutor) ecallCallvalue(cpu *zkvm.RVCPU) (uint64, error) {
	cpu.Regs[10] = uint32(e.value.Uint64() & 0xFFFFFFFF)
	return 0, nil // No gas cost for callvalue intrinsic.
}

// ecallCall: calls another contract. addr in a0, value in a1, result in a0.
// Returns 1 on success, 0 on failure.
func (e *RISCVExecutor) ecallCall(cpu *zkvm.RVCPU) (uint64, error) {
	const callBaseGas uint64 = 2600

	if e.gasUsed+callBaseGas > e.gasLimit {
		return 0, ErrRVExecGasExhausted
	}

	// For now, return success (1) as a stub. Full recursive call
	// support requires access to contract code and re-entrant execution.
	cpu.Regs[10] = 1
	return callBaseGas, nil
}

// readHash reads 32 bytes from CPU memory at the given address.
func (e *RISCVExecutor) readHash(cpu *zkvm.RVCPU, addr uint32) types.Hash {
	var h types.Hash
	for i := uint32(0); i < 32; i++ {
		b, err := cpu.Memory.ReadByteAt(addr + i)
		if err != nil {
			return h
		}
		h[i] = b
	}
	return h
}

// writeHash writes 32 bytes to CPU memory at the given address.
func (e *RISCVExecutor) writeHash(cpu *zkvm.RVCPU, addr uint32, h types.Hash) {
	for i := uint32(0); i < 32; i++ {
		_ = cpu.Memory.WriteByteAt(addr+i, h[i])
	}
}

// Gas cost aliases for RISC-V SSTORE operations (reuse EVM gas from gas.go).
const (
	GasSstoreNew    uint64 = GasSstoreSet   // New slot: from zero to non-zero
	GasSstoreUpdate uint64 = GasSstoreReset // Update: from non-zero to non-zero
)
