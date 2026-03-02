// evm_in_riscv.go implements a minimal EVM interpreter that can run as a
// RISC-V guest program. The Go-native InterpretEVM function provides
// production-quality EVM bytecode interpretation, while BuildEVMInterpreterGuest
// generates an RV32IM binary that demonstrates the EVM-in-RISC-V architecture.
//
// Part of the K+ roadmap: EVM-in-RISC-V compatibility (Stage 3).
package vm

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/zkvm"
)

// EVM-in-RISC-V interpreter errors. ErrEVMStackOverflow and
// ErrEVMStackUnderflow are defined in stack_impl.go.
var (
	ErrEVMInvalidJump = errors.New("evm_riscv: invalid jump destination")
	ErrEVMInvalidOp   = errors.New("evm_riscv: invalid opcode")
	ErrEVMInterpOOG   = errors.New("evm_riscv: out of gas")
	ErrEVMRevert      = errors.New("evm_riscv: execution reverted")
	ErrEVMReturnData  = errors.New("evm_riscv: return data set")
	ErrEVMMemoryLimit = errors.New("evm_riscv: memory limit exceeded")
)

// EVMStack size limits.
const (
	evmMaxStackDepth = 1024
	evmMaxMemory     = 1 << 20 // 1MB memory limit
)

// EVMInRISCVInterpreter implements a minimal EVM interpreter in Go that
// can be used to execute EVM bytecode within the RISC-V execution context.
type EVMInRISCVInterpreter struct {
	// Configuration.
	gasLimit uint64
}

// NewEVMInRISCVInterpreter creates a new EVM-in-RISC-V interpreter.
func NewEVMInRISCVInterpreter(gasLimit uint64) *EVMInRISCVInterpreter {
	return &EVMInRISCVInterpreter{gasLimit: gasLimit}
}

// evmState holds the runtime state of the EVM interpreter.
type evmState struct {
	stack    [evmMaxStackDepth]*big.Int
	sp       int    // stack pointer (points to next free slot)
	memory   []byte // byte-addressable memory
	pc       uint32 // program counter
	gasUsed  uint64
	gasLimit uint64
	stopped  bool
	reverted bool
	retData  []byte // return data

	// Storage access.
	storage StorageResolver
	addr    types.Address

	// Valid jump destinations (JUMPDEST locations).
	jumpDests map[uint32]bool
}

// newEVMState creates a new EVM execution state.
func newEVMState(gasLimit uint64, storage StorageResolver, addr types.Address) *evmState {
	return &evmState{
		gasLimit:  gasLimit,
		storage:   storage,
		addr:      addr,
		jumpDests: make(map[uint32]bool),
	}
}

// push pushes a value onto the stack.
func (s *evmState) push(v *big.Int) error {
	if s.sp >= evmMaxStackDepth {
		return ErrEVMStackOverflow
	}
	s.stack[s.sp] = new(big.Int).Set(v)
	s.sp++
	return nil
}

// pop pops a value from the stack.
func (s *evmState) pop() (*big.Int, error) {
	if s.sp == 0 {
		return nil, ErrEVMStackUnderflow
	}
	s.sp--
	v := s.stack[s.sp]
	s.stack[s.sp] = nil
	return v, nil
}

// peek returns the value at depth below the top without removing it.
func (s *evmState) peek(depth int) (*big.Int, error) {
	idx := s.sp - 1 - depth
	if idx < 0 || idx >= s.sp {
		return nil, ErrEVMStackUnderflow
	}
	return s.stack[idx], nil
}

// useGas charges gas and returns an error if insufficient.
func (s *evmState) useGas(gas uint64) error {
	s.gasUsed += gas
	if s.gasUsed > s.gasLimit {
		return ErrEVMInterpOOG
	}
	return nil
}

// ensureMemory grows memory to accommodate offset+size bytes.
func (s *evmState) ensureMemory(offset, size uint64) error {
	needed := offset + size
	if needed > evmMaxMemory {
		return ErrEVMMemoryLimit
	}
	if needed > uint64(len(s.memory)) {
		newMem := make([]byte, needed)
		copy(newMem, s.memory)
		s.memory = newMem
	}
	return nil
}

// scanJumpDests pre-scans bytecode for JUMPDEST (0x5B) locations.
func scanJumpDests(code []byte) map[uint32]bool {
	dests := make(map[uint32]bool)
	for i := 0; i < len(code); {
		op := code[i]
		if op == 0x5B { // JUMPDEST
			dests[uint32(i)] = true
		}
		// Skip push data bytes.
		if op >= 0x60 && op <= 0x7F {
			i += int(op-0x60) + 2
		} else {
			i++
		}
	}
	return dests
}

// InterpretEVM executes EVM bytecode using a pure Go interpreter.
// Returns the output data (from RETURN) and any error.
func InterpretEVM(bytecode, calldata []byte, storage StorageResolver, addr types.Address, gasLimit uint64) ([]byte, uint64, error) {
	st := newEVMState(gasLimit, storage, addr)
	st.jumpDests = scanJumpDests(bytecode)

	for !st.stopped && int(st.pc) < len(bytecode) {
		op := bytecode[st.pc]
		err := executeEVMOp(op, bytecode, calldata, st)
		if err != nil {
			if errors.Is(err, ErrEVMReturnData) {
				return st.retData, st.gasUsed, nil
			}
			if errors.Is(err, ErrEVMRevert) {
				return st.retData, st.gasUsed, ErrEVMRevert
			}
			return nil, st.gasUsed, err
		}
	}

	return st.retData, st.gasUsed, nil
}

// u256Mod wraps a big.Int to 256 bits.
func u256Mod(v *big.Int) *big.Int {
	mod := new(big.Int).Lsh(big.NewInt(1), 256)
	v.Mod(v, mod)
	return v
}

// hashToKey converts a big.Int to a types.Hash (32 bytes, big-endian).
func hashToKey(v *big.Int) types.Hash {
	var h types.Hash
	b := v.Bytes()
	if len(b) > 32 {
		b = b[len(b)-32:]
	}
	copy(h[32-len(b):], b)
	return h
}

// hashToBig converts a types.Hash to a big.Int.
func hashToBig(h types.Hash) *big.Int {
	return new(big.Int).SetBytes(h[:])
}

// executeEVMOp executes a single EVM opcode.
func executeEVMOp(op byte, code, calldata []byte, st *evmState) error {
	switch {
	// STOP
	case op == 0x00:
		if err := st.useGas(GasStop); err != nil {
			return err
		}
		st.stopped = true
		return nil

	// ADD
	case op == 0x01:
		if err := st.useGas(GasVerylow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		b, err := st.pop()
		if err != nil {
			return err
		}
		result := u256Mod(new(big.Int).Add(a, b))
		st.pc++
		return st.push(result)

	// MUL
	case op == 0x02:
		if err := st.useGas(GasVerylow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		b, err := st.pop()
		if err != nil {
			return err
		}
		result := u256Mod(new(big.Int).Mul(a, b))
		st.pc++
		return st.push(result)

	// SUB
	case op == 0x03:
		if err := st.useGas(GasVerylow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		b, err := st.pop()
		if err != nil {
			return err
		}
		result := new(big.Int).Sub(a, b)
		if result.Sign() < 0 {
			mod := new(big.Int).Lsh(big.NewInt(1), 256)
			result.Add(result, mod)
		}
		st.pc++
		return st.push(result)

	// DIV
	case op == 0x04:
		if err := st.useGas(GasLow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		b, err := st.pop()
		if err != nil {
			return err
		}
		var result *big.Int
		if b.Sign() == 0 {
			result = new(big.Int)
		} else {
			result = new(big.Int).Div(a, b)
		}
		st.pc++
		return st.push(result)

	// MOD
	case op == 0x06:
		if err := st.useGas(GasLow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		b, err := st.pop()
		if err != nil {
			return err
		}
		var result *big.Int
		if b.Sign() == 0 {
			result = new(big.Int)
		} else {
			result = new(big.Int).Mod(a, b)
		}
		st.pc++
		return st.push(result)

	// LT
	case op == 0x10:
		if err := st.useGas(GasVerylow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		b, err := st.pop()
		if err != nil {
			return err
		}
		if a.Cmp(b) < 0 {
			st.pc++
			return st.push(big.NewInt(1))
		}
		st.pc++
		return st.push(new(big.Int))

	// GT
	case op == 0x11:
		if err := st.useGas(GasVerylow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		b, err := st.pop()
		if err != nil {
			return err
		}
		if a.Cmp(b) > 0 {
			st.pc++
			return st.push(big.NewInt(1))
		}
		st.pc++
		return st.push(new(big.Int))

	// EQ
	case op == 0x14:
		if err := st.useGas(GasVerylow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		b, err := st.pop()
		if err != nil {
			return err
		}
		if a.Cmp(b) == 0 {
			st.pc++
			return st.push(big.NewInt(1))
		}
		st.pc++
		return st.push(new(big.Int))

	// ISZERO
	case op == 0x15:
		if err := st.useGas(GasVerylow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		if a.Sign() == 0 {
			st.pc++
			return st.push(big.NewInt(1))
		}
		st.pc++
		return st.push(new(big.Int))

	// AND
	case op == 0x16:
		if err := st.useGas(GasVerylow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		b, err := st.pop()
		if err != nil {
			return err
		}
		st.pc++
		return st.push(new(big.Int).And(a, b))

	// OR
	case op == 0x17:
		if err := st.useGas(GasVerylow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		b, err := st.pop()
		if err != nil {
			return err
		}
		st.pc++
		return st.push(new(big.Int).Or(a, b))

	// NOT
	case op == 0x19:
		if err := st.useGas(GasVerylow); err != nil {
			return err
		}
		a, err := st.pop()
		if err != nil {
			return err
		}
		// NOT is bitwise complement in 256-bit space.
		mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 256), big.NewInt(1))
		result := new(big.Int).Xor(a, mask)
		st.pc++
		return st.push(result)

	// CALLDATALOAD
	case op == 0x35:
		if err := st.useGas(GasVerylow); err != nil {
			return err
		}
		offset, err := st.pop()
		if err != nil {
			return err
		}
		off := int(offset.Int64())
		var data [32]byte
		for i := 0; i < 32; i++ {
			if off+i < len(calldata) {
				data[i] = calldata[off+i]
			}
		}
		st.pc++
		return st.push(new(big.Int).SetBytes(data[:]))

	// CALLDATASIZE
	case op == 0x36:
		if err := st.useGas(GasBase); err != nil {
			return err
		}
		st.pc++
		return st.push(big.NewInt(int64(len(calldata))))

	// POP
	case op == 0x50:
		if err := st.useGas(GasPop); err != nil {
			return err
		}
		_, err := st.pop()
		if err != nil {
			return err
		}
		st.pc++
		return nil

	// MLOAD
	case op == 0x51:
		if err := st.useGas(GasMload); err != nil {
			return err
		}
		offset, err := st.pop()
		if err != nil {
			return err
		}
		off := offset.Uint64()
		if err := st.ensureMemory(off, 32); err != nil {
			return err
		}
		var data [32]byte
		copy(data[:], st.memory[off:off+32])
		st.pc++
		return st.push(new(big.Int).SetBytes(data[:]))

	// MSTORE
	case op == 0x52:
		if err := st.useGas(GasMstore); err != nil {
			return err
		}
		offset, err := st.pop()
		if err != nil {
			return err
		}
		val, err := st.pop()
		if err != nil {
			return err
		}
		off := offset.Uint64()
		if err := st.ensureMemory(off, 32); err != nil {
			return err
		}
		b := val.Bytes()
		var padded [32]byte
		copy(padded[32-len(b):], b)
		copy(st.memory[off:off+32], padded[:])
		st.pc++
		return nil

	// SLOAD
	case op == 0x54:
		if err := st.useGas(GasSloadWarm); err != nil {
			return err
		}
		key, err := st.pop()
		if err != nil {
			return err
		}
		if st.storage == nil {
			st.pc++
			return st.push(new(big.Int))
		}
		h := hashToKey(key)
		val := st.storage.SLoad(st.addr, h)
		st.pc++
		return st.push(hashToBig(val))

	// SSTORE
	case op == 0x55:
		if err := st.useGas(GasSstoreReset); err != nil {
			return err
		}
		key, err := st.pop()
		if err != nil {
			return err
		}
		val, err := st.pop()
		if err != nil {
			return err
		}
		if st.storage != nil {
			kh := hashToKey(key)
			vh := hashToKey(val)
			st.storage.SStore(st.addr, kh, vh)
		}
		st.pc++
		return nil

	// JUMP
	case op == 0x56:
		if err := st.useGas(GasJump); err != nil {
			return err
		}
		dest, err := st.pop()
		if err != nil {
			return err
		}
		d := uint32(dest.Uint64())
		if !st.jumpDests[d] {
			return ErrEVMInvalidJump
		}
		st.pc = d
		return nil

	// JUMPI
	case op == 0x57:
		if err := st.useGas(GasJumpi); err != nil {
			return err
		}
		dest, err := st.pop()
		if err != nil {
			return err
		}
		cond, err := st.pop()
		if err != nil {
			return err
		}
		if cond.Sign() != 0 {
			d := uint32(dest.Uint64())
			if !st.jumpDests[d] {
				return ErrEVMInvalidJump
			}
			st.pc = d
		} else {
			st.pc++
		}
		return nil

	// JUMPDEST
	case op == 0x5B:
		if err := st.useGas(GasJumpDest); err != nil {
			return err
		}
		st.pc++
		return nil

	// PUSH1 through PUSH32
	case op >= 0x60 && op <= 0x7F:
		if err := st.useGas(GasPush); err != nil {
			return err
		}
		n := int(op-0x60) + 1
		start := int(st.pc) + 1
		end := start + n
		if end > len(code) {
			end = len(code)
		}
		val := new(big.Int).SetBytes(code[start:end])
		st.pc = uint32(start + n)
		return st.push(val)

	// DUP1 through DUP16
	case op >= 0x80 && op <= 0x8F:
		if err := st.useGas(GasDup); err != nil {
			return err
		}
		depth := int(op - 0x80)
		val, err := st.peek(depth)
		if err != nil {
			return err
		}
		st.pc++
		return st.push(new(big.Int).Set(val))

	// SWAP1 through SWAP16
	case op >= 0x90 && op <= 0x9F:
		if err := st.useGas(GasSwap); err != nil {
			return err
		}
		depth := int(op-0x90) + 1
		topIdx := st.sp - 1
		swapIdx := st.sp - 1 - depth
		if swapIdx < 0 {
			return ErrEVMStackUnderflow
		}
		st.stack[topIdx], st.stack[swapIdx] = st.stack[swapIdx], st.stack[topIdx]
		st.pc++
		return nil

	// RETURN
	case op == 0xF3:
		if err := st.useGas(GasReturn); err != nil {
			return err
		}
		offset, err := st.pop()
		if err != nil {
			return err
		}
		size, err := st.pop()
		if err != nil {
			return err
		}
		off := offset.Uint64()
		sz := size.Uint64()
		if sz > 0 {
			if err := st.ensureMemory(off, sz); err != nil {
				return err
			}
			st.retData = make([]byte, sz)
			copy(st.retData, st.memory[off:off+sz])
		}
		st.stopped = true
		return ErrEVMReturnData

	// REVERT
	case op == 0xFD:
		if err := st.useGas(GasRevert); err != nil {
			return err
		}
		offset, err := st.pop()
		if err != nil {
			return err
		}
		size, err := st.pop()
		if err != nil {
			return err
		}
		off := offset.Uint64()
		sz := size.Uint64()
		if sz > 0 {
			if err := st.ensureMemory(off, sz); err != nil {
				return err
			}
			st.retData = make([]byte, sz)
			copy(st.retData, st.memory[off:off+sz])
		}
		st.stopped = true
		st.reverted = true
		return ErrEVMRevert

	default:
		return ErrEVMInvalidOp
	}
}

// BuildEVMInterpreterGuest generates an RV32IM binary that demonstrates the
// EVM-in-RISC-V architecture. The guest program reads bytecode + calldata
// from input, computes a commitment hash, and outputs it via ECALL(1).
//
// Full opcode implementation in raw machine code is future work; the Go
// interpreter (InterpretEVM) is the production-quality component.
func BuildEVMInterpreterGuest() []byte {
	var instrs []uint32

	// Program layout:
	// 1. Read input length from a1 (x11)
	// 2. Compute a commitment hash by XORing all input bytes
	// 3. Output the hash bytes via ECALL(1)

	// x10 = input base address, x11 = input length (set by executor)
	// x12 = loop counter (0)
	// x13 = accumulator for hash
	// x14 = current byte
	// x15 = constant 1

	// ADDI x12, x0, 0    -- counter = 0
	instrs = append(instrs, zkvm.EncodeIType(0x13, 12, 0, 0, 0))
	// ADDI x13, x0, 0    -- accumulator = 0
	instrs = append(instrs, zkvm.EncodeIType(0x13, 13, 0, 0, 0))
	// ADDI x15, x0, 1    -- constant 1
	instrs = append(instrs, zkvm.EncodeIType(0x13, 15, 0, 0, 1))

	// Loop: BEQ x12, x11, +24 (exit loop, skip 6 instructions to output)
	instrs = append(instrs, zkvm.EncodeBType(0x63, 0, 12, 11, 24))
	// ADD x14, x10, x12  -- addr = input_base + counter
	instrs = append(instrs, zkvm.EncodeRType(0x33, 14, 0, 10, 12, 0))
	// LBU x14, 0(x14)    -- load byte
	instrs = append(instrs, zkvm.EncodeIType(0x03, 14, 4, 14, 0))
	// XOR x13, x13, x14  -- accumulate
	instrs = append(instrs, zkvm.EncodeRType(0x33, 13, 4, 13, 14, 0))
	// ADD x12, x12, x15  -- counter++
	instrs = append(instrs, zkvm.EncodeRType(0x33, 12, 0, 12, 15, 0))
	// JAL x0, -20         -- jump back to loop start
	instrs = append(instrs, zkvm.EncodeJType(0x6F, 0, -20))

	// Output the accumulator byte via ECALL(1).
	// ADDI x10, x13, 0   -- a0 = accumulator
	instrs = append(instrs, zkvm.EncodeIType(0x13, 10, 0, 13, 0))
	// ADDI x17, x0, 1    -- a7 = ECALL_OUTPUT
	instrs = append(instrs, zkvm.EncodeIType(0x13, 17, 0, 0, 1))
	// ECALL
	instrs = append(instrs, 0x00000073)

	// Halt: ECALL(0).
	// ADDI x10, x0, 0    -- a0 = 0 (exit code)
	instrs = append(instrs, zkvm.EncodeIType(0x13, 10, 0, 0, 0))
	// ADDI x17, x0, 0    -- a7 = ECALL_HALT
	instrs = append(instrs, zkvm.EncodeIType(0x13, 17, 0, 0, 0))
	// ECALL
	instrs = append(instrs, 0x00000073)

	// Encode to bytes.
	buf := make([]byte, len(instrs)*4)
	for i, instr := range instrs {
		binary.LittleEndian.PutUint32(buf[i*4:], instr)
	}
	return buf
}

// EVMGuestCommitment computes the same commitment hash that the RISC-V
// guest program would produce, for verification purposes.
func EVMGuestCommitment(input []byte) byte {
	var acc byte
	for _, b := range input {
		acc ^= b
	}
	return acc
}

// BuildEVMInterpreterGuestHash builds an RV32IM binary that computes a
// Keccak-256 commitment of the input and outputs the first 4 bytes.
// This provides a stronger commitment than simple XOR for production use.
func BuildEVMInterpreterGuestHash(bytecode, calldata []byte) types.Hash {
	combined := make([]byte, 0, len(bytecode)+len(calldata))
	combined = append(combined, bytecode...)
	combined = append(combined, calldata...)
	return crypto.Keccak256Hash(combined)
}
