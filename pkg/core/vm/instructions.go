package vm

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// executionFunc is the signature for opcode execution functions.
type executionFunc func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error)

var (
	big0    = new(big.Int)
	tt256   = new(big.Int).Lsh(big.NewInt(1), 256)   // 2^256
	tt256m1 = new(big.Int).Sub(tt256, big.NewInt(1)) // 2^256 - 1
	tt255   = new(big.Int).Lsh(big.NewInt(1), 255)   // 2^255
)

// toU256 masks val to 256 bits (unsigned).
func toU256(val *big.Int) *big.Int {
	return val.And(val, tt256m1)
}

// toS256 interprets a 256-bit unsigned integer as a signed integer.
func toS256(val *big.Int) *big.Int {
	if val.Cmp(tt255) < 0 {
		return val
	}
	return new(big.Int).Sub(val, tt256)
}

// fromS256 converts a signed 256-bit integer to unsigned representation.
func fromS256(val *big.Int) *big.Int {
	if val.Sign() >= 0 {
		return val
	}
	return new(big.Int).Add(val, tt256)
}

func opAdd(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.Add(x, y)
	toU256(y)
	return nil, nil
}

func opSub(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.Sub(x, y)
	toU256(y)
	return nil, nil
}

func opMul(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.Mul(x, y)
	toU256(y)
	return nil, nil
}

func opDiv(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	if y.Sign() != 0 {
		y.Div(x, y)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opSdiv(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	sx := toS256(new(big.Int).Set(x))
	sy := toS256(new(big.Int).Set(y))
	if sy.Sign() == 0 {
		y.SetUint64(0)
	} else {
		result := new(big.Int).Div(new(big.Int).Abs(sx), new(big.Int).Abs(sy))
		if sx.Sign() != sy.Sign() {
			result.Neg(result)
		}
		y.Set(fromS256(result))
		toU256(y)
	}
	return nil, nil
}

func opMod(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	if y.Sign() != 0 {
		y.Mod(x, y)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opSmod(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	sx := toS256(new(big.Int).Set(x))
	sy := toS256(new(big.Int).Set(y))
	if sy.Sign() == 0 {
		y.SetUint64(0)
	} else {
		result := new(big.Int).Mod(new(big.Int).Abs(sx), new(big.Int).Abs(sy))
		if sx.Sign() < 0 {
			result.Neg(result)
		}
		y.Set(fromS256(result))
		toU256(y)
	}
	return nil, nil
}

func opAddmod(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y, z := stack.Pop(), stack.Pop(), stack.Peek()
	if z.Sign() != 0 {
		add := new(big.Int).Add(x, y)
		z.Mod(add, z)
		toU256(z)
	} else {
		z.SetUint64(0)
	}
	return nil, nil
}

func opMulmod(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y, z := stack.Pop(), stack.Pop(), stack.Peek()
	if z.Sign() != 0 {
		mul := new(big.Int).Mul(x, y)
		z.Mod(mul, z)
		toU256(z)
	} else {
		z.SetUint64(0)
	}
	return nil, nil
}

func opExp(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	base, exponent := stack.Pop(), stack.Peek()
	exponent.Exp(base, exponent, tt256)
	return nil, nil
}

func opSignExtend(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	back, num := stack.Pop(), stack.Peek()
	if back.Cmp(big.NewInt(31)) < 0 {
		bit := uint(back.Uint64()*8 + 7)
		mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), bit), big.NewInt(1))
		if num.Bit(int(bit)) > 0 {
			num.Or(num, new(big.Int).Not(mask))
		} else {
			num.And(num, mask)
		}
		toU256(num)
	}
	return nil, nil
}

func opLt(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	if x.Cmp(y) < 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opGt(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	if x.Cmp(y) > 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opSlt(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	sx := toS256(new(big.Int).Set(x))
	sy := toS256(new(big.Int).Set(y))
	if sx.Cmp(sy) < 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opSgt(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	sx := toS256(new(big.Int).Set(x))
	sy := toS256(new(big.Int).Set(y))
	if sx.Cmp(sy) > 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opEq(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	if x.Cmp(y) == 0 {
		y.SetUint64(1)
	} else {
		y.SetUint64(0)
	}
	return nil, nil
}

func opIsZero(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x := stack.Peek()
	if x.Sign() == 0 {
		x.SetUint64(1)
	} else {
		x.SetUint64(0)
	}
	return nil, nil
}

func opAnd(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.And(x, y)
	return nil, nil
}

func opOr(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.Or(x, y)
	return nil, nil
}

func opXor(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x, y := stack.Pop(), stack.Peek()
	y.Xor(x, y)
	return nil, nil
}

func opNot(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x := stack.Peek()
	x.Not(x)
	toU256(x)
	return nil, nil
}

func opByte(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	th, val := stack.Pop(), stack.Peek()
	if th.Cmp(big.NewInt(32)) < 0 {
		b := val.Bytes()
		padded := make([]byte, 32)
		copy(padded[32-len(b):], b)
		val.SetUint64(uint64(padded[th.Uint64()]))
	} else {
		val.SetUint64(0)
	}
	return nil, nil
}

func opSHL(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	shift, value := stack.Pop(), stack.Peek()
	if shift.Cmp(big.NewInt(256)) >= 0 {
		value.SetUint64(0)
	} else {
		value.Lsh(value, uint(shift.Uint64()))
		toU256(value)
	}
	return nil, nil
}

func opSHR(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	shift, value := stack.Pop(), stack.Peek()
	if shift.Cmp(big.NewInt(256)) >= 0 {
		value.SetUint64(0)
	} else {
		value.Rsh(value, uint(shift.Uint64()))
	}
	return nil, nil
}

func opSAR(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	shift, value := stack.Pop(), stack.Peek()
	signed := toS256(new(big.Int).Set(value))
	if shift.Cmp(big.NewInt(256)) >= 0 {
		if signed.Sign() >= 0 {
			value.SetUint64(0)
		} else {
			value.Set(tt256m1) // all 1s
		}
	} else {
		signed.Rsh(signed, uint(shift.Uint64()))
		value.Set(fromS256(signed))
		toU256(value)
	}
	return nil, nil
}

func opCalldataLoad(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x := stack.Peek()
	offset := x.Uint64()
	data := make([]byte, 32)
	if offset < uint64(len(contract.Input)) {
		copy(data, contract.Input[offset:])
	}
	x.SetBytes(data)
	return nil, nil
}

func opCalldataSize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(uint64(len(contract.Input))))
	return nil, nil
}

func opCalldataCopy(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	memOffset, dataOffset, length := stack.Pop(), stack.Pop(), stack.Pop()
	l := length.Uint64()
	if l == 0 {
		return nil, nil
	}
	dOff := dataOffset.Uint64()
	data := make([]byte, l)
	if dOff < uint64(len(contract.Input)) {
		copy(data, contract.Input[dOff:])
	}
	memory.Set(memOffset.Uint64(), l, data)
	return nil, nil
}

func opCodeSize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(uint64(len(contract.Code))))
	return nil, nil
}

func opCodeCopy(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	memOffset, codeOffset, length := stack.Pop(), stack.Pop(), stack.Pop()
	l := length.Uint64()
	if l == 0 {
		return nil, nil
	}
	cOff := codeOffset.Uint64()
	data := make([]byte, l)
	if cOff < uint64(len(contract.Code)) {
		copy(data, contract.Code[cOff:])
	}
	memory.Set(memOffset.Uint64(), l, data)
	return nil, nil
}

func opAddress(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetBytes(contract.Address[:]))
	return nil, nil
}

func opOrigin(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// EIP-8141: inside a frame transaction, ORIGIN returns the frame caller,
	// not the traditional tx origin. Per the spec:
	// - VERIFY/DEFAULT frame: ENTRY_POINT
	// - SENDER frame: tx.sender
	if evm.FrameCtx != nil {
		stack.Push(new(big.Int).SetBytes(evm.FrameCtx.CurrentFrameCaller[:]))
		return nil, nil
	}
	stack.Push(new(big.Int).SetBytes(evm.TxContext.Origin[:]))
	return nil, nil
}

func opCaller(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetBytes(contract.CallerAddress[:]))
	return nil, nil
}

func opCallValue(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	v := new(big.Int)
	if contract.Value != nil {
		v.Set(contract.Value)
	}
	stack.Push(v)
	return nil, nil
}

func opGasPrice(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	v := new(big.Int)
	if evm.TxContext.GasPrice != nil {
		v.Set(evm.TxContext.GasPrice)
	}
	stack.Push(v)
	return nil, nil
}

func opCoinbase(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetBytes(evm.Context.Coinbase[:]))
	return nil, nil
}

func opTimestamp(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(evm.Context.Time))
	return nil, nil
}

func opNumber(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	v := new(big.Int)
	if evm.Context.BlockNumber != nil {
		v.Set(evm.Context.BlockNumber)
	}
	stack.Push(v)
	return nil, nil
}

func opPrevRandao(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetBytes(evm.Context.PrevRandao[:]))
	return nil, nil
}

func opGasLimit(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(evm.Context.GasLimit))
	return nil, nil
}

func opChainID(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(evm.chainID))
	return nil, nil
}

func opBaseFee(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	v := new(big.Int)
	if evm.Context.BaseFee != nil {
		v.Set(evm.Context.BaseFee)
	}
	stack.Push(v)
	return nil, nil
}

func opPop(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Pop()
	return nil, nil
}

func opMload(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset := stack.Peek()
	off := offset.Uint64()
	data := memory.Get(int64(off), 32)
	offset.SetBytes(data)
	return nil, nil
}

func opMstore(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, val := stack.Pop(), stack.Pop()
	memory.Set32(offset.Uint64(), val)
	return nil, nil
}

func opMstore8(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, val := stack.Pop(), stack.Pop()
	memory.Set(offset.Uint64(), 1, []byte{byte(val.Uint64())})
	return nil, nil
}

func opJump(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	pos := stack.Pop()
	if !contract.validJumpdest(pos) {
		return nil, ErrInvalidJump
	}
	*pc = pos.Uint64()
	return nil, nil
}

func opJumpi(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	pos, cond := stack.Pop(), stack.Pop()
	if cond.Sign() != 0 {
		if !contract.validJumpdest(pos) {
			return nil, ErrInvalidJump
		}
		*pc = pos.Uint64()
	} else {
		*pc++
	}
	return nil, nil
}

func opJumpdest(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	return nil, nil
}

func opPc(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(*pc))
	return nil, nil
}

func opMsize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(uint64(memory.Len())))
	return nil, nil
}

func opGas(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(contract.Gas))
	return nil, nil
}

func opPush0(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int))
	return nil, nil
}

func opPush1(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	var b uint64
	if *pc+1 < uint64(len(contract.Code)) {
		b = uint64(contract.Code[*pc+1])
	}
	stack.Push(new(big.Int).SetUint64(b))
	*pc += 1
	return nil, nil
}

// makePush returns an executionFunc that pushes n bytes from code.
func makePush(size uint64) executionFunc {
	return func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
		start := *pc + 1
		end := start + size
		codeLen := uint64(len(contract.Code))

		var data []byte
		if start >= codeLen {
			data = make([]byte, size)
		} else if end > codeLen {
			data = make([]byte, size)
			copy(data, contract.Code[start:codeLen])
		} else {
			data = contract.Code[start:end]
		}

		stack.Push(new(big.Int).SetBytes(data))
		*pc += size
		return nil, nil
	}
}

// makeDup returns an executionFunc that duplicates the nth stack item.
func makeDup(n int) executionFunc {
	return func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
		stack.Dup(n)
		return nil, nil
	}
}

// makeSwap returns an executionFunc that swaps the top with the nth item.
func makeSwap(n int) executionFunc {
	return func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
		stack.Swap(n)
		return nil, nil
	}
}

func opStop(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	return nil, nil
}

func opReturn(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, size := stack.Pop(), stack.Pop()
	ret := memory.Get(int64(offset.Uint64()), int64(size.Uint64()))
	return ret, nil
}

func opRevert(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, size := stack.Pop(), stack.Pop()
	ret := memory.Get(int64(offset.Uint64()), int64(size.Uint64()))
	return ret, ErrExecutionReverted
}

func opInvalid(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	return nil, ErrInvalidOpCode
}

func opKeccak256(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset, size := stack.Pop(), stack.Peek()
	data := memory.Get(int64(offset.Uint64()), int64(size.Uint64()))
	hash := crypto.Keccak256(data)
	size.SetBytes(hash)
	return nil, nil
}

func opSload(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	loc := stack.Peek()
	if evm.StateDB != nil {
		key := bigToHash(loc)
		val := evm.StateDB.GetState(contract.Address, key)
		// EIP-7928: record storage read for BAL tracking.
		if evm.balTracker != nil {
			evm.balTracker.RecordStorageRead(contract.Address, key, val)
		}
		loc.SetBytes(val[:])
	} else {
		loc.SetUint64(0)
	}
	return nil, nil
}

// bigToHash converts a big.Int to a types.Hash (big-endian, zero-padded).
func bigToHash(b *big.Int) types.Hash {
	return types.BytesToHash(b.Bytes())
}

func opSstore(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	if evm.readOnly {
		return nil, ErrWriteProtection
	}
	loc, val := stack.Pop(), stack.Pop()
	if evm.StateDB != nil {
		key := bigToHash(loc)
		newVal := bigToHash(val)

		// Get current and original (committed) values for refund calculation.
		current := evm.StateDB.GetState(contract.Address, key)
		original := evm.StateDB.GetCommittedState(contract.Address, key)

		// Calculate refund per EIP-2200/EIP-3529.
		var currentBytes, originalBytes, newBytes [32]byte
		copy(currentBytes[:], current[:])
		copy(originalBytes[:], original[:])
		copy(newBytes[:], newVal[:])

		_, refund := SstoreGas(originalBytes, currentBytes, newBytes, false)
		if refund > 0 {
			evm.StateDB.AddRefund(uint64(refund))
		} else if refund < 0 {
			evm.StateDB.SubRefund(uint64(-refund))
		}

		evm.StateDB.SetState(contract.Address, key, newVal)
		// EIP-7928: record storage access for BAL tracking.
		if evm.balTracker != nil {
			if current == newVal {
				evm.balTracker.RecordStorageRead(contract.Address, key, current)
			} else {
				evm.balTracker.RecordStorageChange(contract.Address, key, current, newVal)
			}
		}
	}
	return nil, nil
}

// opSstoreGlamst implements SSTORE for Glamsterdam (EIP-7778).
// Per EIP-7778: no gas refunds are issued for any SSTORE operation.
// The gas charging is handled by gasSstoreGlamst; this function only
// performs the state write without tracking refunds.
func opSstoreGlamst(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	if evm.readOnly {
		return nil, ErrWriteProtection
	}
	loc, val := stack.Pop(), stack.Pop()
	if evm.StateDB != nil {
		key := bigToHash(loc)
		newVal := bigToHash(val)
		// EIP-7928: read current value for BAL no-op detection.
		if evm.balTracker != nil {
			current := evm.StateDB.GetState(contract.Address, key)
			if current == newVal {
				evm.balTracker.RecordStorageRead(contract.Address, key, current)
			} else {
				evm.balTracker.RecordStorageChange(contract.Address, key, current, newVal)
			}
		}
		// EIP-7778: no refund tracking. Just write the state.
		evm.StateDB.SetState(contract.Address, key, newVal)
	}
	return nil, nil
}

func opReturndataSize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(uint64(len(evm.returnData))))
	return nil, nil
}

func opReturndataCopy(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	memOffset, dataOffset, length := stack.Pop(), stack.Pop(), stack.Pop()
	l := length.Uint64()
	if l == 0 {
		return nil, nil
	}
	dOff := dataOffset.Uint64()
	end := dOff + l

	// Check for uint64 overflow in dOff + l.
	if end < dOff {
		return nil, ErrReturnDataOutOfBounds
	}

	// Bounds check against return data.
	if end > uint64(len(evm.returnData)) {
		return nil, ErrReturnDataOutOfBounds
	}

	data := make([]byte, l)
	copy(data, evm.returnData[dOff:end])
	memory.Set(memOffset.Uint64(), l, data)
	return nil, nil
}

func opSelfBalance(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	if evm.StateDB != nil {
		balance := evm.StateDB.GetBalance(contract.Address)
		// EIP-7928: record address touch for BAL tracking.
		if evm.balTracker != nil {
			evm.balTracker.RecordAddressTouch(contract.Address)
		}
		stack.Push(new(big.Int).Set(balance))
	} else {
		stack.Push(new(big.Int))
	}
	return nil, nil
}

func opBalance(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	slot := stack.Peek()
	if evm.StateDB != nil {
		addr := types.BytesToAddress(slot.Bytes())
		balance := evm.StateDB.GetBalance(addr)
		// EIP-7928: record address touch for BAL tracking.
		if evm.balTracker != nil {
			evm.balTracker.RecordAddressTouch(addr)
		}
		slot.Set(balance)
	} else {
		slot.SetUint64(0)
	}
	return nil, nil
}

// makeLog returns an executionFunc for LOG0..LOG4.
func makeLog(n int) executionFunc {
	return func(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
		if evm.readOnly {
			return nil, ErrWriteProtection
		}
		offset, size := stack.Pop(), stack.Pop()
		topics := make([]types.Hash, n)
		for i := 0; i < n; i++ {
			topics[i] = bigToHash(stack.Pop())
		}
		data := memory.Get(int64(offset.Uint64()), int64(size.Uint64()))
		if evm.StateDB != nil {
			evm.StateDB.AddLog(&types.Log{
				Address: contract.Address,
				Topics:  topics,
				Data:    data,
			})
		}
		return nil, nil
	}
}

// bigToAddress converts a big.Int to a types.Address (takes the lower 20 bytes).
func bigToAddress(b *big.Int) types.Address {
	return types.BytesToAddress(b.Bytes())
}

// opCall implements the CALL opcode.
// Stack: gas, addr, value, argsOffset, argsLength, retOffset, retLength
// Pushes 1 on success, 0 on failure.
// The call gas is pre-computed by the dynamic gas function and stored in
// evm.callGasTemp. The gas value on the stack is popped but not used
// directly (the 63/64 rule is applied in the dynamic gas function).
func opCall(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// Pop gas from stack (already accounted for by dynamic gas function).
	_ = stack.Pop()
	addr := bigToAddress(stack.Pop())
	// EIP-7928: record address touch for BAL tracking.
	if evm.balTracker != nil {
		evm.balTracker.RecordAddressTouch(addr)
	}
	value := stack.Pop()
	inOffset, inSize := stack.Pop(), stack.Pop()
	retOffset, retSize := stack.Pop(), stack.Pop()

	// Get input data from memory
	args := memory.Get(int64(inOffset.Uint64()), int64(inSize.Uint64()))

	// Use the pre-computed call gas from dynamic gas function.
	// The Run loop already deducted callGasTemp from contract.Gas (as part of dynamic gas).
	callGas := evm.callGasTemp

	// Add stipend for value transfer (free gas, not charged from caller).
	transfersValue := value != nil && value.Sign() > 0
	if transfersValue {
		callGas += CallStipend
	}

	// GAP-1.2: forward the entire state-creation reservoir to the sub-call.
	evm.callReservoirForward = contract.StateGasReservoir
	contract.StateGasReservoir = 0

	ret, returnGas, err := evm.Call(contract.Address, addr, args, callGas, value)

	// GAP-1.2: restore parent reservoir with whatever the child did not consume.
	contract.StateGasReservoir += evm.callReservoirReturn
	evm.callReservoirReturn = 0

	// Refund unused gas to caller (matching go-ethereum: no stipend subtraction).
	contract.Gas += returnGas

	// Push success/failure
	if err != nil {
		stack.Push(new(big.Int))
	} else {
		stack.Push(big.NewInt(1))
	}

	// Copy return data to memory (only on success or REVERT, per spec).
	evm.returnData = ret
	if err == nil || errors.Is(err, ErrExecutionReverted) {
		if retSize.Uint64() > 0 && len(ret) > 0 {
			retLen := retSize.Uint64()
			if uint64(len(ret)) < retLen {
				retLen = uint64(len(ret))
			}
			memory.Set(retOffset.Uint64(), retLen, ret[:retLen])
		}
	}

	return nil, nil
}

// opCallCode implements the CALLCODE opcode.
// Stack: gas, addr, value, argsOffset, argsLength, retOffset, retLength
func opCallCode(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// Pop gas from stack (already accounted for by dynamic gas function).
	_ = stack.Pop()
	addr := bigToAddress(stack.Pop())
	// EIP-7928: record address touch for BAL tracking.
	if evm.balTracker != nil {
		evm.balTracker.RecordAddressTouch(addr)
	}
	value := stack.Pop()
	inOffset, inSize := stack.Pop(), stack.Pop()
	retOffset, retSize := stack.Pop(), stack.Pop()

	args := memory.Get(int64(inOffset.Uint64()), int64(inSize.Uint64()))

	// Use the pre-computed call gas from dynamic gas function.
	// The Run loop already deducted callGasTemp from contract.Gas (as part of dynamic gas).
	callGas := evm.callGasTemp

	// Add stipend for value transfer (free gas, not charged from caller).
	transfersValue := value != nil && value.Sign() > 0
	if transfersValue {
		callGas += CallStipend
	}

	ret, returnGas, err := evm.CallCode(contract.Address, addr, args, callGas, value)

	// Refund unused gas to caller (matching go-ethereum: no stipend subtraction).
	contract.Gas += returnGas

	if err != nil {
		stack.Push(new(big.Int))
	} else {
		stack.Push(big.NewInt(1))
	}

	evm.returnData = ret
	if err == nil || errors.Is(err, ErrExecutionReverted) {
		if retSize.Uint64() > 0 && len(ret) > 0 {
			retLen := retSize.Uint64()
			if uint64(len(ret)) < retLen {
				retLen = uint64(len(ret))
			}
			memory.Set(retOffset.Uint64(), retLen, ret[:retLen])
		}
	}

	return nil, nil
}

// opDelegateCall implements the DELEGATECALL opcode.
// Stack: gas, addr, argsOffset, argsLength, retOffset, retLength (no value)
func opDelegateCall(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// Pop gas from stack (already accounted for by dynamic gas function).
	_ = stack.Pop()
	addr := bigToAddress(stack.Pop())
	// EIP-7928: record address touch for BAL tracking.
	if evm.balTracker != nil {
		evm.balTracker.RecordAddressTouch(addr)
	}
	inOffset, inSize := stack.Pop(), stack.Pop()
	retOffset, retSize := stack.Pop(), stack.Pop()

	args := memory.Get(int64(inOffset.Uint64()), int64(inSize.Uint64()))

	// Use the pre-computed call gas from dynamic gas function. No value transfer, so no stipend.
	// The Run loop already deducted callGasTemp from contract.Gas (as part of dynamic gas).
	callGas := evm.callGasTemp

	ret, returnGas, err := evm.DelegateCall(contract.CallerAddress, addr, args, callGas)

	contract.Gas += returnGas

	if err != nil {
		stack.Push(new(big.Int))
	} else {
		stack.Push(big.NewInt(1))
	}

	evm.returnData = ret
	if err == nil || errors.Is(err, ErrExecutionReverted) {
		if retSize.Uint64() > 0 && len(ret) > 0 {
			retLen := retSize.Uint64()
			if uint64(len(ret)) < retLen {
				retLen = uint64(len(ret))
			}
			memory.Set(retOffset.Uint64(), retLen, ret[:retLen])
		}
	}

	return nil, nil
}

// opStaticCall implements the STATICCALL opcode.
// Stack: gas, addr, argsOffset, argsLength, retOffset, retLength (no value)
func opStaticCall(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	// Pop gas from stack (already accounted for by dynamic gas function).
	_ = stack.Pop()
	addr := bigToAddress(stack.Pop())
	// EIP-7928: record address touch for BAL tracking.
	if evm.balTracker != nil {
		evm.balTracker.RecordAddressTouch(addr)
	}
	inOffset, inSize := stack.Pop(), stack.Pop()
	retOffset, retSize := stack.Pop(), stack.Pop()

	args := memory.Get(int64(inOffset.Uint64()), int64(inSize.Uint64()))

	// Use the pre-computed call gas from dynamic gas function. No value transfer, so no stipend.
	// The Run loop already deducted callGasTemp from contract.Gas (as part of dynamic gas).
	callGas := evm.callGasTemp

	ret, returnGas, err := evm.StaticCall(contract.Address, addr, args, callGas)

	contract.Gas += returnGas

	if err != nil {
		stack.Push(new(big.Int))
	} else {
		stack.Push(big.NewInt(1))
	}

	evm.returnData = ret
	if err == nil || errors.Is(err, ErrExecutionReverted) {
		if retSize.Uint64() > 0 && len(ret) > 0 {
			retLen := retSize.Uint64()
			if uint64(len(ret)) < retLen {
				retLen = uint64(len(ret))
			}
			memory.Set(retOffset.Uint64(), retLen, ret[:retLen])
		}
	}

	return nil, nil
}

// opCreate implements the CREATE opcode.
// Stack: value, offset, length
// Pushes the new contract address on success, 0 on failure.
func opCreate(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	if evm.readOnly {
		return nil, ErrWriteProtection
	}

	value := stack.Pop()
	offset, size := stack.Pop(), stack.Pop()

	// Get init code from memory
	initCode := memory.Get(int64(offset.Uint64()), int64(size.Uint64()))

	// Pass all remaining gas to create; the create() method handles the 63/64
	// rule and base gas deductions internally.
	gas := contract.Gas
	contract.Gas = 0

	ret, addr, returnGas, err := evm.Create(contract.Address, initCode, gas, value)

	contract.Gas += returnGas
	evm.returnData = ret

	if err != nil {
		stack.Push(new(big.Int))
	} else {
		// EIP-7928: record address touch for newly created contract.
		if evm.balTracker != nil {
			evm.balTracker.RecordAddressTouch(addr)
		}
		stack.Push(new(big.Int).SetBytes(addr[:]))
	}

	return nil, nil
}

// opCreate2 implements the CREATE2 opcode.
// Stack: value, offset, length, salt
func opCreate2(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	if evm.readOnly {
		return nil, ErrWriteProtection
	}

	value := stack.Pop()
	offset, size := stack.Pop(), stack.Pop()
	salt := stack.Pop()

	initCode := memory.Get(int64(offset.Uint64()), int64(size.Uint64()))

	// Pass all remaining gas to create; the create() method handles the 63/64
	// rule and base gas deductions internally.
	gas := contract.Gas
	contract.Gas = 0

	ret, addr, returnGas, err := evm.Create2(contract.Address, initCode, gas, value, salt)

	contract.Gas += returnGas
	evm.returnData = ret

	if err != nil {
		stack.Push(new(big.Int))
	} else {
		// EIP-7928: record address touch for newly created contract.
		if evm.balTracker != nil {
			evm.balTracker.RecordAddressTouch(addr)
		}
		stack.Push(new(big.Int).SetBytes(addr[:]))
	}

	return nil, nil
}

// opExtcodesize implements the EXTCODESIZE opcode.
func opExtcodesize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	slot := stack.Peek()
	if evm.StateDB != nil {
		addr := types.BytesToAddress(slot.Bytes())
		code := evm.StateDB.GetCode(addr)
		// EIP-7928: record address touch for BAL tracking.
		if evm.balTracker != nil {
			evm.balTracker.RecordAddressTouch(addr)
		}
		slot.SetUint64(uint64(len(code)))
	} else {
		slot.SetUint64(0)
	}
	return nil, nil
}

// opExtcodecopy implements the EXTCODECOPY opcode.
func opExtcodecopy(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	addrVal := stack.Pop()
	memOffset := stack.Pop()
	codeOffset := stack.Pop()
	length := stack.Pop()

	l := length.Uint64()
	if l == 0 {
		return nil, nil
	}

	var code []byte
	if evm.StateDB != nil {
		addr := types.BytesToAddress(addrVal.Bytes())
		code = evm.StateDB.GetCode(addr)
		// EIP-7928: record address touch for BAL tracking.
		if evm.balTracker != nil {
			evm.balTracker.RecordAddressTouch(addr)
		}
	}

	cOff := codeOffset.Uint64()
	data := make([]byte, l)
	if cOff < uint64(len(code)) {
		copy(data, code[cOff:])
	}
	memory.Set(memOffset.Uint64(), l, data)
	return nil, nil
}

// opExtcodehash implements the EXTCODEHASH opcode.
func opExtcodehash(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	slot := stack.Peek()
	if evm.StateDB != nil {
		addr := types.BytesToAddress(slot.Bytes())
		// EIP-7928: record address touch for BAL tracking.
		if evm.balTracker != nil {
			evm.balTracker.RecordAddressTouch(addr)
		}
		if !evm.StateDB.Exist(addr) {
			slot.SetUint64(0)
		} else {
			hash := evm.StateDB.GetCodeHash(addr)
			slot.SetBytes(hash[:])
		}
	} else {
		slot.SetUint64(0)
	}
	return nil, nil
}

// opTload implements the TLOAD opcode (EIP-1153).
// Pops a key from the stack, pushes the transient storage value for the
// current contract address at that key.
func opTload(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	loc := stack.Peek()
	if evm.StateDB != nil {
		key := bigToHash(loc)
		val := evm.StateDB.GetTransientState(contract.Address, key)
		loc.SetBytes(val[:])
	} else {
		loc.SetUint64(0)
	}
	return nil, nil
}

// opTstore implements the TSTORE opcode (EIP-1153).
// Pops a key and value from the stack, stores the value in transient storage
// for the current contract address at that key.
func opTstore(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	if evm.readOnly {
		return nil, ErrWriteProtection
	}
	loc, val := stack.Pop(), stack.Pop()
	if evm.StateDB != nil {
		key := bigToHash(loc)
		value := bigToHash(val)
		evm.StateDB.SetTransientState(contract.Address, key, value)
	}
	return nil, nil
}

// opMcopy implements the MCOPY opcode (EIP-5656).
// Pops dest, src, size from the stack and copies memory[src:src+size] to
// memory[dest:dest+size]. Handles overlapping regions correctly.
func opMcopy(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	dest, src, size := stack.Pop(), stack.Pop(), stack.Pop()
	l := size.Uint64()
	if l == 0 {
		return nil, nil
	}
	d := dest.Uint64()
	s := src.Uint64()
	// Get source data as a copy to handle overlapping regions safely.
	data := memory.Get(int64(s), int64(l))
	memory.Set(d, l, data)
	return nil, nil
}

// opBlobHash implements the BLOBHASH opcode (EIP-4844).
// Pops an index from the stack, pushes the versioned hash from
// evm.TxContext.BlobHashes at that index, or zero if out of range.
func opBlobHash(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	idx := stack.Peek()
	if idx.IsUint64() {
		i := idx.Uint64()
		if i < uint64(len(evm.TxContext.BlobHashes)) {
			hash := evm.TxContext.BlobHashes[i]
			idx.SetBytes(hash[:])
			return nil, nil
		}
	}
	idx.SetUint64(0)
	return nil, nil
}

// opBlobBaseFee implements the BLOBBASEFEE opcode (EIP-7516).
// Pushes the current block's blob base fee onto the stack.
func opBlobBaseFee(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	v := new(big.Int)
	if evm.Context.BlobBaseFee != nil {
		v.Set(evm.Context.BlobBaseFee)
	}
	stack.Push(v)
	return nil, nil
}

// opBlockhash implements the BLOCKHASH opcode.
// Returns the hash of one of the 256 most recent complete blocks.
func opBlockhash(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	num := stack.Peek()
	num64 := num.Uint64()

	var upper uint64
	if evm.Context.BlockNumber != nil {
		upper = evm.Context.BlockNumber.Uint64()
	}
	var lower uint64
	if upper > 256 {
		lower = upper - 256
	}

	if num64 >= lower && num64 < upper && evm.Context.GetHash != nil {
		hash := evm.Context.GetHash(num64)
		num.SetBytes(hash[:])
	} else {
		num.SetUint64(0)
	}
	return nil, nil
}

// opSelfdestruct implements the SELFDESTRUCT opcode.
// Post-EIP-6780 (Cancun): sends remaining balance to the beneficiary but does
// NOT destroy the account. Account destruction only occurs if the contract was
// created in the same transaction, which is tracked externally by the state
// processor. The opcode effectively becomes "send all balance".
func opSelfdestruct(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	if evm.readOnly {
		return nil, ErrWriteProtection
	}

	beneficiary := bigToAddress(stack.Pop())

	if evm.StateDB != nil {
		balance := evm.StateDB.GetBalance(contract.Address)
		if balance.Sign() > 0 {
			// EIP-7708: emit transfer or burn log for SELFDESTRUCT.
			if evm.forkRules.IsEIP7708 {
				if beneficiary != contract.Address {
					EmitTransferLog(evm.StateDB, contract.Address, beneficiary, balance)
				} else {
					EmitBurnLog(evm.StateDB, contract.Address, balance)
				}
			}
			// EIP-7928: capture pre-transfer balances for BAL tracking.
			var preSelfBal, preBenBal *big.Int
			if evm.balTracker != nil {
				preSelfBal = new(big.Int).Set(balance)
				preBenBal = new(big.Int).Set(evm.StateDB.GetBalance(beneficiary))
			}
			evm.StateDB.AddBalance(beneficiary, balance)
			evm.StateDB.SubBalance(contract.Address, balance)
			// EIP-7928: record balance changes for BAL tracking.
			if evm.balTracker != nil {
				evm.balTracker.RecordBalanceChange(contract.Address, preSelfBal, evm.StateDB.GetBalance(contract.Address))
				if beneficiary != contract.Address {
					evm.balTracker.RecordBalanceChange(beneficiary, preBenBal, evm.StateDB.GetBalance(beneficiary))
				}
			}
		}
		// Post-EIP-6780: do NOT call SelfDestruct. The account persists.
		// The state processor may still mark it for destruction if the
		// contract was created in the same transaction.
	}

	return nil, nil
}

// opSlotNum implements the SLOTNUM opcode (EIP-7843).
// Pushes the current beacon chain slot number onto the stack.
func opSlotNum(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	stack.Push(new(big.Int).SetUint64(evm.Context.SlotNumber))
	return nil, nil
}

// opCLZ implements the CLZ opcode (EIP-7939).
// Returns the number of leading zero bits in the 256-bit top stack element.
// If the value is 0, returns 256.
func opCLZ(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	x := stack.Peek()
	bitLen := x.BitLen() // 0 for zero value, otherwise position of highest set bit
	x.SetUint64(uint64(256 - bitLen))
	return nil, nil
}

// decodeSingle decodes a single immediate byte for DUPN/SWAPN per EIP-8024.
// Maps the full byte range [0..255] (excluding 91..127) onto stack depths [17..236].
func decodeSingle(x byte) int {
	if x <= 90 {
		return int(x) + 17
	}
	// x >= 128
	return int(x) - 20
}

// decodePair decodes a pair immediate byte for EXCHANGE per EIP-8024.
// Maps the full byte range [0..255] (excluding 80..127) onto two stack depths.
func decodePair(x byte) (int, int) {
	var k int
	if x <= 79 {
		k = int(x)
	} else {
		k = int(x) - 48
	}
	q, r := k/16, k%16
	if q < r {
		return q + 1, r + 1
	}
	return r + 1, 29 - q
}

// opDupN implements the DUPN opcode (EIP-8024).
// Reads one immediate byte from code[PC+1], decodes it to a stack depth n,
// and duplicates the nth stack item onto the top.
func opDupN(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	var x byte
	if *pc+1 < uint64(len(contract.Code)) {
		x = contract.Code[*pc+1]
	}

	// Range 91..127 is excluded per spec.
	if x > 90 && x < 128 {
		return nil, ErrInvalidOpCode
	}
	n := decodeSingle(x)

	if stack.Len() < n {
		return nil, ErrStackUnderflow
	}

	// Duplicate the nth item (1-indexed from top) onto the stack.
	stack.Dup(n)
	*pc += 1
	return nil, nil
}

// opSwapN implements the SWAPN opcode (EIP-8024).
// Reads one immediate byte from code[PC+1], decodes it to a stack depth n,
// and swaps the top stack item with the (n+1)th item.
func opSwapN(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	var x byte
	if *pc+1 < uint64(len(contract.Code)) {
		x = contract.Code[*pc+1]
	}

	// Range 91..127 is excluded per spec.
	if x > 90 && x < 128 {
		return nil, ErrInvalidOpCode
	}
	n := decodeSingle(x)

	if stack.Len() < n+1 {
		return nil, ErrStackUnderflow
	}

	// Swap the top with the nth item from top.
	stack.Swap(n)
	*pc += 1
	return nil, nil
}

// opExchange implements the EXCHANGE opcode (EIP-8024).
// Reads one immediate byte from code[PC+1], decodes it to two stack depths
// (n, m), and swaps the nth and mth stack items.
func opExchange(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	var x byte
	if *pc+1 < uint64(len(contract.Code)) {
		x = contract.Code[*pc+1]
	}

	// Range 80..127 is excluded per spec.
	if x > 79 && x < 128 {
		return nil, ErrInvalidOpCode
	}
	n, m := decodePair(x)
	need := n
	if m > need {
		need = m
	}
	need++ // need at least max(n,m)+1 items

	if stack.Len() < need {
		return nil, ErrStackUnderflow
	}

	// Swap the nth and mth items from the top (1-indexed).
	data := stack.Data()
	top := len(data) - 1
	data[top-n], data[top-m] = data[top-m], data[top-n]
	*pc += 1
	return nil, nil
}
