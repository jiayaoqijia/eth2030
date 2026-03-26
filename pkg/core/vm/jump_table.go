package vm

import "math/big"

// dynamicGasFunc calculates dynamic gas cost for an operation.
type dynamicGasFunc func(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error)

// memorySizeFunc returns the required memory size for an operation.
// The bool return indicates whether an overflow occurred during calculation.
// If overflow is true, the caller should treat this as an out-of-gas condition.
type memorySizeFunc func(stack *Stack) (uint64, bool)

// operation represents a single EVM opcode's execution metadata.
type operation struct {
	execute     executionFunc
	constantGas uint64
	dynamicGas  dynamicGasFunc
	minStack    int // minimum stack items required
	maxStack    int // maximum stack items allowed (1024 - net stack items pushed)
	memorySize  memorySizeFunc
	halts       bool // whether this opcode halts execution (STOP, RETURN, SELFDESTRUCT)
	jumps       bool // whether this opcode performs a jump (JUMP, JUMPI)
	writes      bool // whether this opcode modifies state (SSTORE, LOG, CREATE, etc.)
}

// GetConstantGas returns the constant gas cost of the operation.
func (op *operation) GetConstantGas() uint64 {
	return op.constantGas
}

// JumpTable maps every possible opcode to its operation definition.
type JumpTable [256]*operation

// --- Overflow-safe helpers for memory size calculations ---

// bigUint64WithOverflow converts a *big.Int to uint64, returning true if it overflows.
func bigUint64WithOverflow(v *big.Int) (uint64, bool) {
	if v.Sign() < 0 {
		return 0, true
	}
	if !v.IsUint64() {
		return 0, true
	}
	return v.Uint64(), false
}

// safeAddU_val returns a + b and true if the addition overflows.
func safeAddU_val(a, b uint64) (uint64, bool) {
	sum := a + b
	if sum < a {
		return 0, true
	}
	return sum, false
}

// --- Memory size functions for operations that access memory ---

func memoryMload(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(0))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, 32)
}

func memoryMstore(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(0))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, 32)
}

func memoryMstore8(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(0))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, 1)
}

func memoryReturn(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(0))
	if overflow {
		return 0, true
	}
	size, overflow := bigUint64WithOverflow(stack.Back(1))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, size)
}

func memoryKeccak256(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(0))
	if overflow {
		return 0, true
	}
	size, overflow := bigUint64WithOverflow(stack.Back(1))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, size)
}

func memoryCalldataCopy(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(0))
	if overflow {
		return 0, true
	}
	length, overflow := bigUint64WithOverflow(stack.Back(2))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, length)
}

func memoryCodeCopy(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(0))
	if overflow {
		return 0, true
	}
	length, overflow := bigUint64WithOverflow(stack.Back(2))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, length)
}

func memoryLog(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(0))
	if overflow {
		return 0, true
	}
	size, overflow := bigUint64WithOverflow(stack.Back(1))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, size)
}

func memoryReturndataCopy(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(0))
	if overflow {
		return 0, true
	}
	length, overflow := bigUint64WithOverflow(stack.Back(2))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, length)
}

// memoryCall returns the required memory size for CALL.
// Stack: gas, addr, value, argsOffset, argsLength, retOffset, retLength
func memoryCall(stack *Stack) (uint64, bool) {
	argsOff, overflow := bigUint64WithOverflow(stack.Back(3))
	if overflow {
		return 0, true
	}
	argsLen, overflow := bigUint64WithOverflow(stack.Back(4))
	if overflow {
		return 0, true
	}
	retOff, overflow := bigUint64WithOverflow(stack.Back(5))
	if overflow {
		return 0, true
	}
	retLen, overflow := bigUint64WithOverflow(stack.Back(6))
	if overflow {
		return 0, true
	}
	argsEnd, overflow := safeAddU_val(argsOff, argsLen)
	if overflow {
		return 0, true
	}
	retEnd, overflow := safeAddU_val(retOff, retLen)
	if overflow {
		return 0, true
	}
	if argsEnd > retEnd {
		return argsEnd, false
	}
	return retEnd, false
}

// memoryCallCode returns the required memory size for CALLCODE.
func memoryCallCode(stack *Stack) (uint64, bool) {
	return memoryCall(stack)
}

// memoryDelegateCall returns the required memory size for DELEGATECALL.
// Stack: gas, addr, argsOffset, argsLength, retOffset, retLength (no value)
func memoryDelegateCall(stack *Stack) (uint64, bool) {
	argsOff, overflow := bigUint64WithOverflow(stack.Back(2))
	if overflow {
		return 0, true
	}
	argsLen, overflow := bigUint64WithOverflow(stack.Back(3))
	if overflow {
		return 0, true
	}
	retOff, overflow := bigUint64WithOverflow(stack.Back(4))
	if overflow {
		return 0, true
	}
	retLen, overflow := bigUint64WithOverflow(stack.Back(5))
	if overflow {
		return 0, true
	}
	argsEnd, overflow := safeAddU_val(argsOff, argsLen)
	if overflow {
		return 0, true
	}
	retEnd, overflow := safeAddU_val(retOff, retLen)
	if overflow {
		return 0, true
	}
	if argsEnd > retEnd {
		return argsEnd, false
	}
	return retEnd, false
}

// memoryStaticCall returns the required memory size for STATICCALL.
func memoryStaticCall(stack *Stack) (uint64, bool) {
	return memoryDelegateCall(stack)
}

// memoryExtcodecopy returns the required memory size for EXTCODECOPY.
// Stack: addr, memOffset, codeOffset, length
func memoryExtcodecopy(stack *Stack) (uint64, bool) {
	memOff, overflow := bigUint64WithOverflow(stack.Back(1))
	if overflow {
		return 0, true
	}
	length, overflow := bigUint64WithOverflow(stack.Back(3))
	if overflow {
		return 0, true
	}
	return safeAddU_val(memOff, length)
}

// memoryCreate returns the required memory size for CREATE.
// Stack: value, offset, length
func memoryCreate(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(1))
	if overflow {
		return 0, true
	}
	length, overflow := bigUint64WithOverflow(stack.Back(2))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, length)
}

// memoryCreate2 returns the required memory size for CREATE2.
// Stack: value, offset, length, salt
func memoryCreate2(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(1))
	if overflow {
		return 0, true
	}
	length, overflow := bigUint64WithOverflow(stack.Back(2))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, length)
}

// memoryMcopy returns the required memory size for MCOPY.
// Stack: dest, src, size. We need max(dest+size, src+size).
func memoryMcopy(stack *Stack) (uint64, bool) {
	dest, overflow := bigUint64WithOverflow(stack.Back(0))
	if overflow {
		return 0, true
	}
	src, overflow := bigUint64WithOverflow(stack.Back(1))
	if overflow {
		return 0, true
	}
	size, overflow := bigUint64WithOverflow(stack.Back(2))
	if overflow {
		return 0, true
	}
	if size == 0 {
		return 0, false
	}
	destEnd, overflow := safeAddU_val(dest, size)
	if overflow {
		return 0, true
	}
	srcEnd, overflow := safeAddU_val(src, size)
	if overflow {
		return 0, true
	}
	if destEnd > srcEnd {
		return destEnd, false
	}
	return srcEnd, false
}

// memoryApprove returns the required memory size for APPROVE (EIP-8141).
// Stack: [offset, length, scope] (top-0 = offset, top-1 = length)
func memoryApprove(stack *Stack) (uint64, bool) {
	offset, overflow := bigUint64WithOverflow(stack.Back(0))
	if overflow {
		return 0, true
	}
	length, overflow := bigUint64WithOverflow(stack.Back(1))
	if overflow {
		return 0, true
	}
	return safeAddU_val(offset, length)
}

// memoryTxParamCopy returns the required memory size for TXPARAMCOPY (EIP-8141).
// Stack: [in1, in2, destOffset, offset, length]
func memoryTxParamCopy(stack *Stack) (uint64, bool) {
	destOffset, overflow := bigUint64WithOverflow(stack.Back(2))
	if overflow {
		return 0, true
	}
	length, overflow := bigUint64WithOverflow(stack.Back(4))
	if overflow {
		return 0, true
	}
	if length == 0 {
		return 0, false
	}
	return safeAddU_val(destOffset, length)
}

// gasMcopy calculates dynamic gas for MCOPY: 3 * wordSize + memory expansion.
func gasMcopy(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	size := stack.Back(2).Uint64()
	words := (size + 31) / 32
	copyGas := safeMul(GasCopy, words)
	memGas, err := gasMemExpansion(evm, contract, stack, mem, memorySize)
	if err != nil {
		return 0, err
	}
	return safeAdd(copyGas, memGas), nil
}

// gasMemExpansion calculates dynamic gas for memory expansion.
func gasMemExpansion(evm *EVM, contract *Contract, stack *Stack, mem *Memory, memorySize uint64) (uint64, error) {
	if memorySize == 0 {
		return 0, nil
	}
	words := (memorySize + 31) / 32
	newCost := words*GasMemory + (words*words)/512
	if uint64(mem.Len()) == 0 {
		return newCost, nil
	}
	oldWords := (uint64(mem.Len()) + 31) / 32
	oldCost := oldWords*GasMemory + (oldWords*oldWords)/512
	if newCost > oldCost {
		return newCost - oldCost, nil
	}
	return 0, nil
}

// --- Frontier gas constants (pre-EIP-150) ---
const (
	GasBalanceFrontier     uint64 = 20 // BALANCE before EIP-150
	GasExtcodeSizeFrontier uint64 = 20 // EXTCODESIZE before EIP-150
	GasSloadFrontier       uint64 = 50 // SLOAD before EIP-150
	GasCallFrontier        uint64 = 40 // CALL base before EIP-150
	GasExtcodeCopyFrontier uint64 = 20 // EXTCODECOPY base before EIP-150

	// EIP-150 (Tangerine Whistle) gas constants.
	GasBalanceEIP150     uint64 = 400 // BALANCE after EIP-150
	GasExtcodeSizeEIP150 uint64 = 700 // EXTCODESIZE after EIP-150
	GasSloadEIP150       uint64 = 200 // SLOAD after EIP-150
	GasCallEIP150        uint64 = 700 // CALL base after EIP-150
	GasExtcodeCopyEIP150 uint64 = 700 // EXTCODECOPY base after EIP-150

	// Istanbul (EIP-1884) gas constants.
	GasBalanceEIP1884      uint64 = 700 // BALANCE after EIP-1884
	GasSloadEIP1884        uint64 = 800 // SLOAD after EIP-1884
	GasExtcodeHashConst    uint64 = 400 // EXTCODEHASH after Constantinople
	GasExtcodeHashIstanbul uint64 = 700 // EXTCODEHASH after Istanbul (EIP-1884)
)

// NewFrontierJumpTable returns the Frontier (genesis) jump table.
func NewFrontierJumpTable() JumpTable {
	var tbl JumpTable

	// Arithmetic
	tbl[STOP] = &operation{execute: opStop, constantGas: GasStop, minStack: 0, maxStack: 1024, halts: true}
	tbl[ADD] = &operation{execute: opAdd, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[MUL] = &operation{execute: opMul, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[SUB] = &operation{execute: opSub, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[DIV] = &operation{execute: opDiv, constantGas: GasLow, minStack: 2, maxStack: 1024}
	tbl[SDIV] = &operation{execute: opSdiv, constantGas: GasLow, minStack: 2, maxStack: 1024}
	tbl[MOD] = &operation{execute: opMod, constantGas: GasLow, minStack: 2, maxStack: 1024}
	tbl[SMOD] = &operation{execute: opSmod, constantGas: GasLow, minStack: 2, maxStack: 1024}
	tbl[ADDMOD] = &operation{execute: opAddmod, constantGas: GasMid, minStack: 3, maxStack: 1024}
	tbl[MULMOD] = &operation{execute: opMulmod, constantGas: GasMid, minStack: 3, maxStack: 1024}
	tbl[EXP] = &operation{execute: opExp, constantGas: GasHigh, dynamicGas: gasExp, minStack: 2, maxStack: 1024}
	tbl[SIGNEXTEND] = &operation{execute: opSignExtend, constantGas: GasLow, minStack: 2, maxStack: 1024}

	// Comparison
	tbl[LT] = &operation{execute: opLt, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[GT] = &operation{execute: opGt, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[SLT] = &operation{execute: opSlt, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[SGT] = &operation{execute: opSgt, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[EQ] = &operation{execute: opEq, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[ISZERO] = &operation{execute: opIsZero, constantGas: GasVerylow, minStack: 1, maxStack: 1024}

	// Bitwise
	tbl[AND] = &operation{execute: opAnd, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[OR] = &operation{execute: opOr, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[XOR] = &operation{execute: opXor, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[NOT] = &operation{execute: opNot, constantGas: GasVerylow, minStack: 1, maxStack: 1024}
	tbl[BYTE] = &operation{execute: opByte, constantGas: GasVerylow, minStack: 2, maxStack: 1024}

	// Environment
	tbl[ADDRESS] = &operation{execute: opAddress, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[ORIGIN] = &operation{execute: opOrigin, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[CALLER] = &operation{execute: opCaller, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[CALLVALUE] = &operation{execute: opCallValue, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[CALLDATALOAD] = &operation{execute: opCalldataLoad, constantGas: GasVerylow, minStack: 1, maxStack: 1024}
	tbl[CALLDATASIZE] = &operation{execute: opCalldataSize, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[CALLDATACOPY] = &operation{execute: opCalldataCopy, constantGas: GasVerylow, minStack: 3, maxStack: 1024, memorySize: memoryCalldataCopy, dynamicGas: gasCopy}
	tbl[CODESIZE] = &operation{execute: opCodeSize, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[CODECOPY] = &operation{execute: opCodeCopy, constantGas: GasVerylow, minStack: 3, maxStack: 1024, memorySize: memoryCodeCopy, dynamicGas: gasCopy}
	tbl[GASPRICE] = &operation{execute: opGasPrice, constantGas: GasBase, minStack: 0, maxStack: 1023}

	// Block
	tbl[BLOCKHASH] = &operation{execute: opBlockhash, constantGas: GasExt, minStack: 1, maxStack: 1024}
	tbl[COINBASE] = &operation{execute: opCoinbase, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[TIMESTAMP] = &operation{execute: opTimestamp, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[NUMBER] = &operation{execute: opNumber, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[PREVRANDAO] = &operation{execute: opPrevRandao, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[GASLIMIT] = &operation{execute: opGasLimit, constantGas: GasBase, minStack: 0, maxStack: 1023}

	// Stack, memory, flow
	tbl[POP] = &operation{execute: opPop, constantGas: GasPop, minStack: 1, maxStack: 1024}
	tbl[MLOAD] = &operation{execute: opMload, constantGas: GasMload, minStack: 1, maxStack: 1024, memorySize: memoryMload, dynamicGas: gasMemExpansion}
	tbl[MSTORE] = &operation{execute: opMstore, constantGas: GasMstore, minStack: 2, maxStack: 1024, memorySize: memoryMstore, dynamicGas: gasMemExpansion}
	tbl[MSTORE8] = &operation{execute: opMstore8, constantGas: GasMstore8, minStack: 2, maxStack: 1024, memorySize: memoryMstore8, dynamicGas: gasMemExpansion}
	tbl[JUMP] = &operation{execute: opJump, constantGas: GasJump, minStack: 1, maxStack: 1024, jumps: true}
	tbl[JUMPI] = &operation{execute: opJumpi, constantGas: GasJumpi, minStack: 2, maxStack: 1024, jumps: true}
	tbl[PC] = &operation{execute: opPc, constantGas: GasPc, minStack: 0, maxStack: 1023}
	tbl[MSIZE] = &operation{execute: opMsize, constantGas: GasMsize, minStack: 0, maxStack: 1023}
	tbl[GAS] = &operation{execute: opGas, constantGas: GasGas, minStack: 0, maxStack: 1023}
	tbl[JUMPDEST] = &operation{execute: opJumpdest, constantGas: GasJumpDest, minStack: 0, maxStack: 1024}

	// Push
	tbl[PUSH1] = &operation{execute: opPush1, constantGas: GasPush, minStack: 0, maxStack: 1023}
	for i := 2; i <= 32; i++ {
		tbl[PUSH1+OpCode(i-1)] = &operation{
			execute:     makePush(uint64(i)),
			constantGas: GasPush,
			minStack:    0,
			maxStack:    1023,
		}
	}

	// Dup
	for i := 1; i <= 16; i++ {
		tbl[DUP1+OpCode(i-1)] = &operation{
			execute:     makeDup(i),
			constantGas: GasDup,
			minStack:    i,
			maxStack:    1023,
		}
	}

	// Swap
	for i := 1; i <= 16; i++ {
		tbl[SWAP1+OpCode(i-1)] = &operation{
			execute:     makeSwap(i),
			constantGas: GasSwap,
			minStack:    i + 1,
			maxStack:    1024,
		}
	}

	// Hash
	tbl[KECCAK256] = &operation{execute: opKeccak256, constantGas: GasKeccak256, minStack: 2, maxStack: 1024, memorySize: memoryKeccak256, dynamicGas: gasSha3}

	// State access -- Frontier gas costs (pre-EIP-150).
	tbl[BALANCE] = &operation{execute: opBalance, constantGas: GasBalanceFrontier, minStack: 1, maxStack: 1024}
	tbl[SLOAD] = &operation{execute: opSload, constantGas: GasSloadFrontier, minStack: 1, maxStack: 1024}
	tbl[SSTORE] = &operation{execute: opSstore, constantGas: GasSstoreSet, minStack: 2, maxStack: 1024, writes: true}

	// Log
	for i := 0; i <= 4; i++ {
		n := i
		tbl[LOG0+OpCode(i)] = &operation{
			execute:     makeLog(n),
			constantGas: GasLog,
			minStack:    2 + n,
			maxStack:    1024,
			writes:      true,
			memorySize:  memoryLog,
			dynamicGas:  makeGasLog(uint64(n)),
		}
	}

	// Ext code -- Frontier gas costs (pre-EIP-150).
	tbl[EXTCODESIZE] = &operation{execute: opExtcodesize, constantGas: GasExtcodeSizeFrontier, minStack: 1, maxStack: 1024}
	tbl[EXTCODECOPY] = &operation{execute: opExtcodecopy, constantGas: GasExtcodeCopyFrontier, minStack: 4, maxStack: 1024, memorySize: memoryExtcodecopy, dynamicGas: gasExtCodeCopyCopy}

	// CALL-family -- Frontier gas costs.
	tbl[CALL] = &operation{execute: opCall, constantGas: GasCallFrontier, minStack: 7, maxStack: 1024, memorySize: memoryCall, dynamicGas: gasCallFrontier}
	tbl[CALLCODE] = &operation{execute: opCallCode, constantGas: GasCallFrontier, minStack: 7, maxStack: 1024, memorySize: memoryCallCode, dynamicGas: gasCallCodeFrontier}

	// CREATE
	tbl[CREATE] = &operation{execute: opCreate, constantGas: GasCreate, minStack: 3, maxStack: 1024, memorySize: memoryCreate, dynamicGas: gasCreateDynamic, writes: true}

	// Return / Invalid / Selfdestruct
	tbl[RETURN] = &operation{execute: opReturn, constantGas: GasReturn, minStack: 2, maxStack: 1024, halts: true, memorySize: memoryReturn, dynamicGas: gasMemExpansion}
	tbl[INVALID] = &operation{execute: opInvalid, constantGas: 0, minStack: 0, maxStack: 1024}
	tbl[SELFDESTRUCT] = &operation{execute: opSelfdestruct, constantGas: GasSelfdestruct, dynamicGas: gasSelfdestructFrontier, minStack: 1, maxStack: 1024, halts: true, writes: true}

	return tbl
}

// NewHomesteadJumpTable returns the Homestead fork jump table.
func NewHomesteadJumpTable() JumpTable {
	tbl := NewFrontierJumpTable()
	// Homestead added DELEGATECALL with Frontier-era gas costs.
	tbl[DELEGATECALL] = &operation{execute: opDelegateCall, constantGas: GasCallFrontier, minStack: 6, maxStack: 1024, memorySize: memoryDelegateCall, dynamicGas: gasMemExpansion}
	return tbl
}

// NewTangerineWhistleJumpTable returns the Tangerine Whistle (EIP-150) fork jump table.
func NewTangerineWhistleJumpTable() JumpTable {
	tbl := NewHomesteadJumpTable()

	// EIP-150: increased gas costs for state access.
	tbl[BALANCE] = &operation{execute: opBalance, constantGas: GasBalanceEIP150, minStack: 1, maxStack: 1024}
	tbl[SLOAD] = &operation{execute: opSload, constantGas: GasSloadEIP150, minStack: 1, maxStack: 1024}
	tbl[EXTCODESIZE] = &operation{execute: opExtcodesize, constantGas: GasExtcodeSizeEIP150, minStack: 1, maxStack: 1024}
	tbl[EXTCODECOPY] = &operation{execute: opExtcodecopy, constantGas: GasExtcodeCopyEIP150, minStack: 4, maxStack: 1024, memorySize: memoryExtcodecopy, dynamicGas: gasExtCodeCopyCopy}
	tbl[CALL] = &operation{execute: opCall, constantGas: GasCallEIP150, minStack: 7, maxStack: 1024, memorySize: memoryCall, dynamicGas: gasCallFrontier}
	tbl[CALLCODE] = &operation{execute: opCallCode, constantGas: GasCallEIP150, minStack: 7, maxStack: 1024, memorySize: memoryCallCode, dynamicGas: gasCallCodeFrontier}
	tbl[DELEGATECALL] = &operation{execute: opDelegateCall, constantGas: GasCallEIP150, minStack: 6, maxStack: 1024, memorySize: memoryDelegateCall, dynamicGas: gasMemExpansion}

	return tbl
}

// NewSpuriousDragonJumpTable returns the Spurious Dragon fork jump table.
func NewSpuriousDragonJumpTable() JumpTable {
	tbl := NewTangerineWhistleJumpTable()
	return tbl
}

// NewByzantiumJumpTable returns the Byzantium fork jump table.
func NewByzantiumJumpTable() JumpTable {
	tbl := NewSpuriousDragonJumpTable()
	tbl[REVERT] = &operation{execute: opRevert, constantGas: GasRevert, minStack: 2, maxStack: 1024, halts: true, memorySize: memoryReturn, dynamicGas: gasMemExpansion}
	tbl[RETURNDATASIZE] = &operation{execute: opReturndataSize, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[RETURNDATACOPY] = &operation{execute: opReturndataCopy, constantGas: GasVerylow, minStack: 3, maxStack: 1024, memorySize: memoryReturndataCopy, dynamicGas: gasCopy}
	tbl[STATICCALL] = &operation{execute: opStaticCall, constantGas: GasCallEIP150, minStack: 6, maxStack: 1024, memorySize: memoryStaticCall, dynamicGas: gasMemExpansion}
	return tbl
}

// NewConstantinopleJumpTable returns the Constantinople fork jump table.
func NewConstantinopleJumpTable() JumpTable {
	tbl := NewByzantiumJumpTable()
	tbl[SHL] = &operation{execute: opSHL, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[SHR] = &operation{execute: opSHR, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[SAR] = &operation{execute: opSAR, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[EXTCODEHASH] = &operation{execute: opExtcodehash, constantGas: GasExtcodeHashConst, minStack: 1, maxStack: 1024}
	tbl[CREATE2] = &operation{execute: opCreate2, constantGas: GasCreate, minStack: 4, maxStack: 1024, memorySize: memoryCreate2, dynamicGas: gasCreate2Dynamic, writes: true}
	return tbl
}

// NewIstanbulJumpTable returns the Istanbul fork jump table.
func NewIstanbulJumpTable() JumpTable {
	tbl := NewConstantinopleJumpTable()
	tbl[CHAINID] = &operation{execute: opChainID, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[SELFBALANCE] = &operation{execute: opSelfBalance, constantGas: GasLow, minStack: 0, maxStack: 1023}
	tbl[BALANCE] = &operation{execute: opBalance, constantGas: GasBalanceEIP1884, minStack: 1, maxStack: 1024}
	tbl[SLOAD] = &operation{execute: opSload, constantGas: GasSloadEIP1884, minStack: 1, maxStack: 1024}
	tbl[EXTCODEHASH] = &operation{execute: opExtcodehash, constantGas: GasExtcodeHashIstanbul, minStack: 1, maxStack: 1024}
	return tbl
}

// NewBerlinJumpTable returns the Berlin fork jump table.
func NewBerlinJumpTable() JumpTable {
	tbl := NewIstanbulJumpTable()

	// EIP-2929: warm/cold gas accounting.
	tbl[SLOAD] = &operation{execute: opSload, constantGas: WarmStorageReadCost, dynamicGas: gasSloadEIP2929, minStack: 1, maxStack: 1024}
	tbl[BALANCE] = &operation{execute: opBalance, constantGas: WarmStorageReadCost, dynamicGas: gasBalanceEIP2929, minStack: 1, maxStack: 1024}
	tbl[EXTCODESIZE] = &operation{execute: opExtcodesize, constantGas: WarmStorageReadCost, dynamicGas: gasExtCodeSizeEIP2929, minStack: 1, maxStack: 1024}
	tbl[EXTCODECOPY] = &operation{execute: opExtcodecopy, constantGas: WarmStorageReadCost, dynamicGas: gasExtCodeCopyEIP2929, minStack: 4, maxStack: 1024, memorySize: memoryExtcodecopy}
	tbl[EXTCODEHASH] = &operation{execute: opExtcodehash, constantGas: WarmStorageReadCost, dynamicGas: gasExtCodeHashEIP2929, minStack: 1, maxStack: 1024}
	tbl[CALL] = &operation{execute: opCall, constantGas: WarmStorageReadCost, dynamicGas: gasCallEIP2929, minStack: 7, maxStack: 1024, memorySize: memoryCall}
	tbl[CALLCODE] = &operation{execute: opCallCode, constantGas: WarmStorageReadCost, dynamicGas: gasCallCodeEIP2929, minStack: 7, maxStack: 1024, memorySize: memoryCallCode}
	tbl[STATICCALL] = &operation{execute: opStaticCall, constantGas: WarmStorageReadCost, dynamicGas: gasStaticCallEIP2929, minStack: 6, maxStack: 1024, memorySize: memoryStaticCall}
	tbl[DELEGATECALL] = &operation{execute: opDelegateCall, constantGas: WarmStorageReadCost, dynamicGas: gasDelegateCallEIP2929, minStack: 6, maxStack: 1024, memorySize: memoryDelegateCall}
	tbl[SSTORE] = &operation{execute: opSstore, constantGas: 0, dynamicGas: gasSstoreEIP2929, minStack: 2, maxStack: 1024, writes: true}
	tbl[SELFDESTRUCT] = &operation{execute: opSelfdestruct, constantGas: GasSelfdestruct, dynamicGas: gasSelfdestructEIP2929, minStack: 1, maxStack: 1024, halts: true, writes: true}

	return tbl
}

// NewLondonJumpTable returns the London fork jump table.
func NewLondonJumpTable() JumpTable {
	tbl := NewBerlinJumpTable()
	tbl[BASEFEE] = &operation{execute: opBaseFee, constantGas: GasBase, minStack: 0, maxStack: 1023}
	return tbl
}

// NewMergeJumpTable returns the Merge (Paris) fork jump table.
func NewMergeJumpTable() JumpTable {
	tbl := NewLondonJumpTable()
	return tbl
}

// NewShanghaiJumpTable returns the Shanghai fork jump table.
func NewShanghaiJumpTable() JumpTable {
	tbl := NewMergeJumpTable()
	tbl[PUSH0] = &operation{execute: opPush0, constantGas: GasPush0, minStack: 0, maxStack: 1023}
	return tbl
}

// NewCancunJumpTable returns the Cancun fork jump table.
func NewCancunJumpTable() JumpTable {
	tbl := NewShanghaiJumpTable()

	tbl[TLOAD] = &operation{execute: opTload, constantGas: GasTload, minStack: 1, maxStack: 1024}
	tbl[TSTORE] = &operation{execute: opTstore, constantGas: GasTstore, minStack: 2, maxStack: 1024, writes: true}
	tbl[MCOPY] = &operation{execute: opMcopy, constantGas: GasMcopyBase, minStack: 3, maxStack: 1024, memorySize: memoryMcopy, dynamicGas: gasMcopy}
	tbl[BLOBHASH] = &operation{execute: opBlobHash, constantGas: GasBlobHash, minStack: 1, maxStack: 1024}
	tbl[BLOBBASEFEE] = &operation{execute: opBlobBaseFee, constantGas: GasBlobBaseFee, minStack: 0, maxStack: 1023}

	return tbl
}

// NewPragueJumpTable returns the Prague fork jump table.
func NewPragueJumpTable() JumpTable {
	tbl := NewCancunJumpTable()

	// TODO: Move APPROVE to Glamsterdan jump table when fork is ready.
	// EIP-8141 FrameTx requires APPROVE opcode (0xaa) support.
	// Currently enabling it directly in Prague for development.
	tbl[APPROVE] = &operation{execute: opApprove, constantGas: GasLow, minStack: 3, maxStack: 1024, halts: true, memorySize: memoryApprove, dynamicGas: gasMemExpansion}

	return tbl
}

// NewVerkleJumpTable returns the Verkle fork jump table.
func NewVerkleJumpTable() JumpTable {
	tbl := NewGlamsterdanJumpTable()

	tbl[SLOAD] = &operation{execute: opSload, constantGas: 0, dynamicGas: gasSloadEIP4762, minStack: 1, maxStack: 1024}
	tbl[SSTORE] = &operation{execute: opSstore, constantGas: 0, dynamicGas: gasSstoreEIP4762, minStack: 2, maxStack: 1024, writes: true}
	tbl[BALANCE] = &operation{execute: opBalance, constantGas: 0, dynamicGas: gasBalanceEIP4762, minStack: 1, maxStack: 1024}
	tbl[EXTCODESIZE] = &operation{execute: opExtcodesize, constantGas: 0, dynamicGas: gasExtCodeSizeEIP4762, minStack: 1, maxStack: 1024}
	tbl[EXTCODECOPY] = &operation{execute: opExtcodecopy, constantGas: 0, dynamicGas: gasExtCodeCopyEIP4762, minStack: 4, maxStack: 1024, memorySize: memoryExtcodecopy}
	tbl[EXTCODEHASH] = &operation{execute: opExtcodehash, constantGas: 0, dynamicGas: gasExtCodeHashEIP4762, minStack: 1, maxStack: 1024}
	tbl[CALL] = &operation{execute: opCall, constantGas: 0, dynamicGas: gasCallEIP4762, minStack: 7, maxStack: 1024, memorySize: memoryCall}
	tbl[CALLCODE] = &operation{execute: opCallCode, constantGas: 0, dynamicGas: gasCallCodeEIP4762, minStack: 7, maxStack: 1024, memorySize: memoryCallCode}
	tbl[DELEGATECALL] = &operation{execute: opDelegateCall, constantGas: 0, dynamicGas: gasDelegateCallEIP4762, minStack: 6, maxStack: 1024, memorySize: memoryDelegateCall}
	tbl[STATICCALL] = &operation{execute: opStaticCall, constantGas: 0, dynamicGas: gasStaticCallEIP4762, minStack: 6, maxStack: 1024, memorySize: memoryStaticCall}
	tbl[SELFDESTRUCT] = &operation{execute: opSelfdestruct, constantGas: 0, dynamicGas: gasSelfdestructEIP4762, minStack: 1, maxStack: 1024, halts: true, writes: true}

	return tbl
}

// NewGlamsterdanJumpTable returns the Glamsterdan fork jump table.
func NewGlamsterdanJumpTable() JumpTable {
	tbl := NewPragueJumpTable()

	tbl[DIV] = &operation{execute: opDiv, constantGas: GasDivGlamsterdan, minStack: 2, maxStack: 1024}
	tbl[SDIV] = &operation{execute: opSdiv, constantGas: GasSdivGlamsterdan, minStack: 2, maxStack: 1024}
	tbl[MOD] = &operation{execute: opMod, constantGas: GasModGlamsterdan, minStack: 2, maxStack: 1024}
	tbl[MULMOD] = &operation{execute: opMulmod, constantGas: GasMulmodGlamsterdan, minStack: 3, maxStack: 1024}
	tbl[KECCAK256] = &operation{execute: opKeccak256, constantGas: GasKeccak256Glamsterdan, minStack: 2, maxStack: 1024, memorySize: memoryKeccak256, dynamicGas: gasSha3}

	tbl[SLOAD] = &operation{execute: opSload, constantGas: WarmStorageReadGlamst, dynamicGas: gasSloadGlamst, minStack: 1, maxStack: 1024}
	tbl[BALANCE] = &operation{execute: opBalance, constantGas: WarmStorageReadGlamst, dynamicGas: gasBalanceGlamst, minStack: 1, maxStack: 1024}
	tbl[EXTCODESIZE] = &operation{execute: opExtcodesize, constantGas: WarmStorageReadGlamst, dynamicGas: gasExtCodeSizeGlamst, minStack: 1, maxStack: 1024}
	tbl[EXTCODECOPY] = &operation{execute: opExtcodecopy, constantGas: WarmStorageReadGlamst, dynamicGas: gasExtCodeCopyGlamst, minStack: 4, maxStack: 1024, memorySize: memoryExtcodecopy}
	tbl[EXTCODEHASH] = &operation{execute: opExtcodehash, constantGas: WarmStorageReadGlamst, dynamicGas: gasExtCodeHashGlamst, minStack: 1, maxStack: 1024}

	tbl[SSTORE] = &operation{execute: opSstoreGlamst, constantGas: 0, dynamicGas: gasSstoreGlamst, minStack: 2, maxStack: 1024, writes: true}

	tbl[CALL] = &operation{execute: opCall, constantGas: WarmStorageReadGlamst, dynamicGas: gasCallGlamst, minStack: 7, maxStack: 1024, memorySize: memoryCall}
	tbl[CALLCODE] = &operation{execute: opCallCode, constantGas: WarmStorageReadGlamst, dynamicGas: gasCallCodeGlamst, minStack: 7, maxStack: 1024, memorySize: memoryCallCode}
	tbl[STATICCALL] = &operation{execute: opStaticCall, constantGas: WarmStorageReadGlamst, dynamicGas: gasStaticCallGlamst, minStack: 6, maxStack: 1024, memorySize: memoryStaticCall}
	tbl[DELEGATECALL] = &operation{execute: opDelegateCall, constantGas: WarmStorageReadGlamst, dynamicGas: gasDelegateCallGlamst, minStack: 6, maxStack: 1024, memorySize: memoryDelegateCall}
	tbl[SELFDESTRUCT] = &operation{execute: opSelfdestruct, constantGas: GasSelfdestruct, dynamicGas: gasSelfdestructGlamst, minStack: 1, maxStack: 1024, halts: true, writes: true}

	tbl[SLOTNUM] = &operation{execute: opSlotNum, constantGas: GasBase, minStack: 0, maxStack: 1023}
	tbl[CLZ] = &operation{execute: opCLZ, constantGas: GasFastStep, minStack: 1, maxStack: 1024}

	tbl[DUPN] = &operation{execute: opDupN, constantGas: GasVerylow, minStack: 1, maxStack: 1023}
	tbl[SWAPN] = &operation{execute: opSwapN, constantGas: GasVerylow, minStack: 2, maxStack: 1024}
	tbl[EXCHANGE] = &operation{execute: opExchange, constantGas: GasVerylow, minStack: 2, maxStack: 1024}

	tbl[APPROVE] = &operation{execute: opApprove, constantGas: GasLow, minStack: 3, maxStack: 1024, halts: true, memorySize: memoryApprove, dynamicGas: gasMemExpansion}
	tbl[TXPARAMLOAD] = &operation{execute: opTxParamLoad, constantGas: GasBase, minStack: 2, maxStack: 1024}
	tbl[TXPARAMSIZE] = &operation{execute: opTxParamSize, constantGas: GasBase, minStack: 2, maxStack: 1024}
	tbl[TXPARAMCOPY] = &operation{execute: opTxParamCopy, constantGas: GasVerylow, minStack: 5, maxStack: 1024, memorySize: memoryTxParamCopy, dynamicGas: gasMemExpansion}

	return tbl
}

// NewIPlusJumpTable returns the I+ fork jump table extending Glamsterdan
// with RVCREATE (0xF6) for RISC-V contract deployment (EL-3.1).
func NewIPlusJumpTable() JumpTable {
	tbl := NewGlamsterdanJumpTable()
	tbl[RVCREATE] = &operation{
		execute:     opRVCreate,
		constantGas: GasCreate,
		dynamicGas:  gasRVCreate,
		minStack:    3,
		maxStack:    1024,
		memorySize:  memoryCreate,
		writes:      true,
	}
	return tbl
}

// NewFrameVerifyJumpTable returns a restricted jump table for VERIFY frames.
// Per EIP-8141 spec, VERIFY frames MUST NOT use state-modifying opcodes
// to prevent mempool DoS attacks.
func NewFrameVerifyJumpTable() JumpTable {
	tbl := NewGlamsterdanJumpTable()

	// Remove state-modifying opcodes.
	tbl[SSTORE] = nil
	tbl[CREATE] = nil
	tbl[CREATE2] = nil
	tbl[SELFDESTRUCT] = nil
	tbl[LOG0] = nil
	tbl[LOG1] = nil
	tbl[LOG2] = nil
	tbl[LOG3] = nil
	tbl[LOG4] = nil

	return tbl
}
