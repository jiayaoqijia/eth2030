// evm_transpiler.go implements an ahead-of-time transpiler from EVM bytecode
// to native RV32IM instructions. Each EVM opcode is mapped to a deterministic
// sequence of RISC-V instructions, enabling direct execution on the canonical
// RISC-V CPU without interpretation overhead.
//
// Key register assignments:
//   - x8  (s0) = EVM stack pointer (grows downward from 0x80000000)
//   - x9  (s1) = EVM memory pointer (base at 0x40000000)
//   - x10 (a0) = scratch / ECALL argument
//   - x11 (a1) = scratch / ECALL argument
//   - x12 (s2) = EVM program counter
//   - x17 (a7) = ECALL function number
//
// Part of the K+ roadmap: EVM-in-RISC-V compatibility (Stage 3).
package vm

import (
	"encoding/binary"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/zkvm"
)

// Transpiler register assignments.
const (
	rvRegSP      uint32 = 8  // EVM stack pointer
	rvRegMemBase uint32 = 9  // EVM memory base
	rvRegScratch uint32 = 10 // a0: scratch / ecall arg
	rvRegScratch2 uint32 = 11 // a1: scratch / ecall arg
	rvRegEVMPC   uint32 = 12 // EVM program counter
	rvRegEcall   uint32 = 17 // a7: ecall function
	rvRegZero    uint32 = 0  // x0: hardwired zero
	rvRegTmp     uint32 = 13 // temporary
	rvRegTmp2    uint32 = 14 // temporary
)

// EVMTranspiler transpiles EVM bytecode to RV32IM instruction sequences.
// Results are cached by code hash for reuse.
type EVMTranspiler struct {
	mu    sync.RWMutex
	cache map[types.Hash][]byte
}

// NewEVMTranspiler creates a new transpiler with an empty cache.
func NewEVMTranspiler() *EVMTranspiler {
	return &EVMTranspiler{
		cache: make(map[types.Hash][]byte),
	}
}

// Transpile converts EVM bytecode to an RV32IM binary. Results are cached
// by code hash, so repeated calls with the same bytecode are fast.
func (t *EVMTranspiler) Transpile(evmCode []byte) ([]byte, error) {
	h := crypto.Keccak256Hash(evmCode)

	t.mu.RLock()
	if cached, ok := t.cache[h]; ok {
		t.mu.RUnlock()
		return cached, nil
	}
	t.mu.RUnlock()

	// Transpile each opcode.
	var allInstrs []uint32

	// Prologue: set up EVM stack pointer.
	// LUI x8, 0x80000    -- sp = 0x80000000
	allInstrs = append(allInstrs, zkvm.EncodeUType(0x37, rvRegSP, 0x80000000))
	// ADDI x12, x0, 0    -- EVM PC = 0
	allInstrs = append(allInstrs, zkvm.EncodeIType(0x13, rvRegEVMPC, 0, rvRegZero, 0))

	i := 0
	for i < len(evmCode) {
		op := evmCode[i]
		instrs, err := t.TranspileOpcode(op)
		if err != nil {
			// Unknown opcodes: emit halt.
			instrs = transpileHalt()
		}
		allInstrs = append(allInstrs, instrs...)

		// Skip PUSH data bytes.
		if op >= 0x60 && op <= 0x7F {
			n := int(op-0x60) + 1
			i += n + 1
		} else {
			i++
		}
	}

	// Epilogue: halt.
	allInstrs = append(allInstrs, transpileHalt()...)

	// Encode to bytes.
	buf := make([]byte, len(allInstrs)*4)
	for idx, instr := range allInstrs {
		binary.LittleEndian.PutUint32(buf[idx*4:], instr)
	}

	t.mu.Lock()
	t.cache[h] = buf
	t.mu.Unlock()

	return buf, nil
}

// CacheSize returns the number of cached transpiled programs.
func (t *EVMTranspiler) CacheSize() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.cache)
}

// TranspileOpcode converts a single EVM opcode to a sequence of RV32IM
// instruction words.
func (t *EVMTranspiler) TranspileOpcode(op byte) ([]uint32, error) {
	switch op {
	case 0x00: // STOP
		return transpileHalt(), nil

	case 0x01: // ADD
		return transpileAdd(), nil

	case 0x02: // MUL
		return transpileMul(), nil

	case 0x03: // SUB
		return transpileSub(), nil

	case 0x50: // POP
		return transpilePop(), nil

	case 0x51: // MLOAD
		return transpileMload(), nil

	case 0x52: // MSTORE
		return transpileMstore(), nil

	case 0x54: // SLOAD
		return transpileSload(), nil

	case 0x55: // SSTORE
		return transpileSstore(), nil

	case 0x56: // JUMP
		return transpileJump(), nil

	case 0x57: // JUMPI
		return transpileJumpi(), nil

	case 0x5B: // JUMPDEST
		return transpileJumpdest(), nil

	case 0xF3: // RETURN
		return transpileReturn(), nil
	}

	// PUSH1-PUSH32
	if op >= 0x60 && op <= 0x7F {
		return transpilePush1(), nil
	}

	// DUP1-DUP16
	if op >= 0x80 && op <= 0x8F {
		return transpileDup1(), nil
	}

	return nil, ErrEVMInvalidOp
}

// --- Transpiled instruction sequences ---

// transpileHalt: ECALL(0) — halt execution.
// 3 instructions.
func transpileHalt() []uint32 {
	return []uint32{
		// ADDI x10, x0, 0    -- a0 = 0
		zkvm.EncodeIType(0x13, rvRegScratch, 0, rvRegZero, 0),
		// ADDI x17, x0, 0    -- a7 = ECALL_HALT
		zkvm.EncodeIType(0x13, rvRegEcall, 0, rvRegZero, 0),
		// ECALL
		0x00000073,
	}
}

// transpileAdd: POP two values, ADD, PUSH result.
// ~6 instructions.
func transpileAdd() []uint32 {
	return []uint32{
		// LW x10, 0(x8)      -- pop top
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegSP, 0),
		// ADDI x8, x8, 4     -- sp += 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// LW x11, 0(x8)      -- pop second
		zkvm.EncodeIType(0x03, rvRegScratch2, 2, rvRegSP, 0),
		// ADDI x8, x8, 4     -- sp += 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// ADD x10, x10, x11  -- result = a + b
		zkvm.EncodeRType(0x33, rvRegScratch, 0, rvRegScratch, rvRegScratch2, 0),
		// ADDI x8, x8, -4    -- sp -= 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, -4),
		// SW x10, 0(x8)      -- push result
		zkvm.EncodeSType(0x23, 2, rvRegSP, rvRegScratch, 0),
	}
}

// transpileMul: POP two values, MUL, PUSH result.
// ~7 instructions.
func transpileMul() []uint32 {
	return []uint32{
		// LW x10, 0(x8)      -- pop top
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// LW x11, 0(x8)      -- pop second
		zkvm.EncodeIType(0x03, rvRegScratch2, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// MUL x10, x10, x11  -- result = a * b
		zkvm.EncodeRType(0x33, rvRegScratch, 0, rvRegScratch, rvRegScratch2, 0x01),
		// ADDI x8, x8, -4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, -4),
		// SW x10, 0(x8)
		zkvm.EncodeSType(0x23, 2, rvRegSP, rvRegScratch, 0),
	}
}

// transpileSub: POP two values, SUB, PUSH result.
// ~7 instructions.
func transpileSub() []uint32 {
	return []uint32{
		// LW x10, 0(x8)
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// LW x11, 0(x8)
		zkvm.EncodeIType(0x03, rvRegScratch2, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// SUB x10, x10, x11  -- result = a - b
		zkvm.EncodeRType(0x33, rvRegScratch, 0, rvRegScratch, rvRegScratch2, 0x20),
		// ADDI x8, x8, -4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, -4),
		// SW x10, 0(x8)
		zkvm.EncodeSType(0x23, 2, rvRegSP, rvRegScratch, 0),
	}
}

// transpilePop: discard stack top.
// 1 instruction.
func transpilePop() []uint32 {
	return []uint32{
		// ADDI x8, x8, 4     -- sp += 4 (discard top)
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
	}
}

// transpilePush1: load immediate value onto stack.
// Note: the actual immediate value would need to be patched in by the
// transpiler's main loop. This emits a template that pushes 0.
// 3 instructions.
func transpilePush1() []uint32 {
	return []uint32{
		// ADDI x10, x0, 0    -- value (to be patched)
		zkvm.EncodeIType(0x13, rvRegScratch, 0, rvRegZero, 0),
		// ADDI x8, x8, -4    -- sp -= 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, -4),
		// SW x10, 0(x8)      -- push value
		zkvm.EncodeSType(0x23, 2, rvRegSP, rvRegScratch, 0),
	}
}

// transpileDup1: duplicate stack top.
// 3 instructions.
func transpileDup1() []uint32 {
	return []uint32{
		// LW x10, 0(x8)      -- read top
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegSP, 0),
		// ADDI x8, x8, -4    -- sp -= 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, -4),
		// SW x10, 0(x8)      -- push copy
		zkvm.EncodeSType(0x23, 2, rvRegSP, rvRegScratch, 0),
	}
}

// transpileMload: POP offset, load 32-bit word from memory, PUSH.
// 4 instructions.
func transpileMload() []uint32 {
	return []uint32{
		// LW x10, 0(x8)      -- pop offset
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegSP, 0),
		// ADD x10, x9, x10   -- addr = membase + offset
		zkvm.EncodeRType(0x33, rvRegScratch, 0, rvRegMemBase, rvRegScratch, 0),
		// LW x10, 0(x10)     -- load word
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegScratch, 0),
		// SW x10, 0(x8)      -- overwrite top with loaded value
		zkvm.EncodeSType(0x23, 2, rvRegSP, rvRegScratch, 0),
	}
}

// transpileMstore: POP offset, POP value, store value at memory offset.
// 5 instructions.
func transpileMstore() []uint32 {
	return []uint32{
		// LW x10, 0(x8)      -- pop offset
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// LW x11, 0(x8)      -- pop value
		zkvm.EncodeIType(0x03, rvRegScratch2, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// ADD x10, x9, x10   -- addr = membase + offset
		zkvm.EncodeRType(0x33, rvRegScratch, 0, rvRegMemBase, rvRegScratch, 0),
		// SW x11, 0(x10)     -- store value
		zkvm.EncodeSType(0x23, 2, rvRegScratch, rvRegScratch2, 0),
	}
}

// transpileSload: POP key, ECALL(3), PUSH value.
// 4 instructions.
func transpileSload() []uint32 {
	return []uint32{
		// LW x10, 0(x8)      -- pop key into a0
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegSP, 0),
		// ADDI x17, x0, 3    -- a7 = ECALL_SLOAD
		zkvm.EncodeIType(0x13, rvRegEcall, 0, rvRegZero, 3),
		// ECALL              -- result in a0
		0x00000073,
		// SW x10, 0(x8)      -- push result (overwrite key)
		zkvm.EncodeSType(0x23, 2, rvRegSP, rvRegScratch, 0),
	}
}

// transpileSstore: POP key, POP value, ECALL(4).
// 5 instructions.
func transpileSstore() []uint32 {
	return []uint32{
		// LW x10, 0(x8)      -- pop key into a0
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// LW x11, 0(x8)      -- pop value into a1
		zkvm.EncodeIType(0x03, rvRegScratch2, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// ADDI x17, x0, 4    -- a7 = ECALL_SSTORE
		zkvm.EncodeIType(0x13, rvRegEcall, 0, rvRegZero, 4),
		// ECALL
		0x00000073,
	}
}

// transpileJump: POP dest, set EVM PC.
// Note: in a full implementation, this would validate JUMPDEST and
// translate to the corresponding RV32IM address. For now, emit a
// simplified sequence.
// 5 instructions.
func transpileJump() []uint32 {
	return []uint32{
		// LW x10, 0(x8)      -- pop dest
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegSP, 0),
		// ADDI x8, x8, 4     -- sp += 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// ADDI x12, x10, 0   -- EVM PC = dest
		zkvm.EncodeIType(0x13, rvRegEVMPC, 0, rvRegScratch, 0),
		// NOP (placeholder for JUMPDEST validation)
		zkvm.EncodeIType(0x13, rvRegZero, 0, rvRegZero, 0),
		// NOP (placeholder for indirect jump)
		zkvm.EncodeIType(0x13, rvRegZero, 0, rvRegZero, 0),
	}
}

// transpileJumpi: POP dest, POP cond, conditional set of EVM PC.
// 7 instructions.
func transpileJumpi() []uint32 {
	return []uint32{
		// LW x10, 0(x8)      -- pop dest
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// LW x11, 0(x8)      -- pop condition
		zkvm.EncodeIType(0x03, rvRegScratch2, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// BEQ x11, x0, +8    -- if cond == 0, skip
		zkvm.EncodeBType(0x63, 0, rvRegScratch2, rvRegZero, 8),
		// ADDI x12, x10, 0   -- EVM PC = dest (taken)
		zkvm.EncodeIType(0x13, rvRegEVMPC, 0, rvRegScratch, 0),
		// NOP (fall-through for not-taken)
		zkvm.EncodeIType(0x13, rvRegZero, 0, rvRegZero, 0),
	}
}

// transpileJumpdest: no-op marker.
// 1 instruction.
func transpileJumpdest() []uint32 {
	return []uint32{
		// NOP
		zkvm.EncodeIType(0x13, rvRegZero, 0, rvRegZero, 0),
	}
}

// transpileReturn: POP offset, POP size, output via ECALL(1), halt.
// This is simplified — outputs a single byte.
// 6 instructions.
func transpileReturn() []uint32 {
	return []uint32{
		// LW x10, 0(x8)      -- pop offset
		zkvm.EncodeIType(0x03, rvRegScratch, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// LW x11, 0(x8)      -- pop size (unused in simplified version)
		zkvm.EncodeIType(0x03, rvRegScratch2, 2, rvRegSP, 0),
		// ADDI x8, x8, 4
		zkvm.EncodeIType(0x13, rvRegSP, 0, rvRegSP, 4),
		// Output + halt.
		// ADDI x17, x0, 1    -- a7 = ECALL_OUTPUT
		zkvm.EncodeIType(0x13, rvRegEcall, 0, rvRegZero, 1),
		// ECALL
		0x00000073,
	}
}
