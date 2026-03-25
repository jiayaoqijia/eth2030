package vm

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// EIP-8141: Frame Transaction opcodes.
// See https://eips.ethereum.org/EIPS/eip-8141

var (
	ErrInvalidApproveScope   = errors.New("invalid APPROVE scope")
	ErrAlreadyApproved       = errors.New("scope already approved")
	ErrSenderNotApproved     = errors.New("sender not approved")
	ErrCallerNotFrameTarget  = errors.New("caller != frame target")
	ErrCallerNotSender       = errors.New("caller != tx sender")
	ErrInsufficientBalance   = errors.New("insufficient balance for approval")
	ErrNoFrameContext        = errors.New("no frame transaction context")
	ErrInvalidTxParamIndex   = errors.New("invalid TXPARAM index")
	ErrFrameIndexOutOfBounds = errors.New("frame index out of bounds")
	ErrTxParamIn2MustBeZero  = errors.New("TXPARAM in2 must be zero")
)

// FrameMode identifies how a frame executes within a frame transaction.
const (
	FrameModeDefault uint64 = 0
	FrameModeVerify  uint64 = 1
	FrameModeSender  uint64 = 2
)

// Frame represents a single execution frame in a frame transaction.
type Frame struct {
	Mode     uint64
	Target   types.Address
	GasLimit uint64
	Data     []byte
	// Status is set after execution: 0=failure, 1=success.
	Status uint64
}

// FrameContext holds transaction-scoped state for EIP-8141 frame transactions.
type FrameContext struct {
	// Approval state (transaction-scoped).
	SenderApproved bool
	PayerApproved  bool
	Payer          types.Address // The address that pays for gas

	// APPROVE tracking: records the most recent APPROVE call within the
	// current frame so the callFn can distinguish APPROVE(2) from
	// APPROVE(0)+APPROVE(1) without relying solely on boolean flags.
	LastApproveScope       uint8
	ApproveCalledThisFrame bool

	// CurrentFrameCaller is the caller for the current frame.
	// Per EIP-8141: ORIGIN opcode returns frame caller throughout all call depths.
	// - VERIFY/DEFAULT frame: ENTRY_POINT
	// - SENDER frame: tx.sender
	CurrentFrameCaller types.Address

	// Transaction parameters exposed via TXPARAM* opcodes.
	TxType            uint64
	Nonce             *big.Int // 256-bit 2D nonce
	Sender            types.Address
	MaxPriorityFee    *big.Int
	MaxFee            *big.Int
	MaxBlobFee        *big.Int
	MaxCost           *big.Int
	BlobCount         uint64
	SigHash           types.Hash
	Frames            []Frame
	CurrentFrameIndex uint64
}

// opApprove implements the APPROVE opcode (0xaa) per EIP-8141.
// Stack: [offset, length, scope] (top-0 = offset, top-1 = length, top-2 = scope)
// Behaves like RETURN but also updates the transaction-scoped approval context.
//
// Per EIP-8141 and revm-eip8141 reference:
// - APPROVE is only valid inside a VERIFY frame (mode == 1)
// - Scope 0x0: sender approval only
// - Scope 0x1: payer approval (payer = current frame target), requires sender_approved
// - Scope 0x2: combined sender+payer approval (payer = tx sender)
func opApprove(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	offset := stack.Pop()
	length := stack.Pop()
	scope := stack.Pop()

	fc := evm.FrameCtx
	if fc == nil {
		return nil, ErrNoFrameContext
	}

	scopeVal := scope.Uint64()

	// Validate scope is 0, 1, or 2.
	if scopeVal > 2 {
		return nil, ErrInvalidApproveScope
	}

	// Per revm-eip8141: APPROVE is only valid inside a VERIFY frame (mode == 1).
	if fc.CurrentFrameIndex >= uint64(len(fc.Frames)) {
		return nil, ErrFrameIndexOutOfBounds
	}
	if fc.Frames[fc.CurrentFrameIndex].Mode != FrameModeVerify {
		return nil, ErrInvalidApproveScope
	}

	switch scopeVal {
	case 0: // Execution approval (sender only)
		if fc.SenderApproved {
			return nil, ErrAlreadyApproved
		}
		fc.SenderApproved = true

	case 1: // Payment approval
		if !fc.SenderApproved {
			return nil, ErrSenderNotApproved
		}
		if fc.PayerApproved {
			return nil, ErrAlreadyApproved
		}
		// EIP-8141: check if frame.target has sufficient balance for max cost.
		payerAddr := fc.Frames[fc.CurrentFrameIndex].Target
		if evm.StateDB != nil && fc.MaxCost != nil {
			balance := evm.StateDB.GetBalance(payerAddr)
			if balance.Cmp(fc.MaxCost) < 0 {
				return nil, ErrInsufficientBalance
			}
		}
		// Payment approval delegates gas payment to the current frame target.
		fc.Payer = payerAddr
		fc.PayerApproved = true

	case 2: // Combined execution + payment approval
		if fc.SenderApproved || fc.PayerApproved {
			return nil, ErrAlreadyApproved
		}
		// EIP-8141: check if tx.sender has sufficient balance for max cost.
		if evm.StateDB != nil && fc.MaxCost != nil {
			balance := evm.StateDB.GetBalance(fc.Sender)
			if balance.Cmp(fc.MaxCost) < 0 {
				return nil, ErrInsufficientBalance
			}
		}
		// Combined approval means sender and payer are the transaction sender.
		fc.Payer = fc.Sender
		fc.SenderApproved = true
		fc.PayerApproved = true
	}

	// Track the APPROVE call for the callFn to read.
	fc.ApproveCalledThisFrame = true
	fc.LastApproveScope = uint8(scopeVal)

	// Return data from memory at [offset, offset+length), like RETURN.
	ret := memory.Get(int64(offset.Uint64()), int64(length.Uint64()))
	return ret, nil
}

// txParamValue returns the 32-byte padded value for a given TXPARAM index pair (in1, in2).
func txParamValue(fc *FrameContext, in1, in2 uint64) ([]byte, error) {
	result := make([]byte, 32)

	switch in1 {
	case 0x00: // tx type
		if in2 != 0 {
			return nil, ErrTxParamIn2MustBeZero
		}
		new(big.Int).SetUint64(fc.TxType).FillBytes(result)

	case 0x01: // nonce (256-bit)
		if in2 != 0 {
			return nil, ErrTxParamIn2MustBeZero
		}
		if fc.Nonce != nil {
			b := fc.Nonce.Bytes()
			copy(result[32-len(b):], b)
		}

	case 0x02: // sender
		if in2 != 0 {
			return nil, ErrTxParamIn2MustBeZero
		}
		copy(result[12:], fc.Sender[:])

	case 0x03: // max_priority_fee_per_gas
		if in2 != 0 {
			return nil, ErrTxParamIn2MustBeZero
		}
		if fc.MaxPriorityFee != nil {
			b := fc.MaxPriorityFee.Bytes()
			copy(result[32-len(b):], b)
		}

	case 0x04: // max_fee_per_gas
		if in2 != 0 {
			return nil, ErrTxParamIn2MustBeZero
		}
		if fc.MaxFee != nil {
			b := fc.MaxFee.Bytes()
			copy(result[32-len(b):], b)
		}

	case 0x05: // max_fee_per_blob_gas
		if in2 != 0 {
			return nil, ErrTxParamIn2MustBeZero
		}
		if fc.MaxBlobFee != nil {
			b := fc.MaxBlobFee.Bytes()
			copy(result[32-len(b):], b)
		}

	case 0x06: // max cost
		if in2 != 0 {
			return nil, ErrTxParamIn2MustBeZero
		}
		if fc.MaxCost != nil {
			b := fc.MaxCost.Bytes()
			copy(result[32-len(b):], b)
		}

	case 0x07: // len(blob_versioned_hashes)
		if in2 != 0 {
			return nil, ErrTxParamIn2MustBeZero
		}
		new(big.Int).SetUint64(fc.BlobCount).FillBytes(result)

	case 0x08: // compute_sig_hash(tx)
		if in2 != 0 {
			return nil, ErrTxParamIn2MustBeZero
		}
		copy(result, fc.SigHash[:])

	case 0x09: // len(frames)
		if in2 != 0 {
			return nil, ErrTxParamIn2MustBeZero
		}
		new(big.Int).SetUint64(uint64(len(fc.Frames))).FillBytes(result)

	case 0x10: // current frame index
		if in2 != 0 {
			return nil, ErrTxParamIn2MustBeZero
		}
		new(big.Int).SetUint64(fc.CurrentFrameIndex).FillBytes(result)

	case 0x11: // frame target (in2 = frame index)
		if in2 >= uint64(len(fc.Frames)) {
			return nil, ErrFrameIndexOutOfBounds
		}
		copy(result[12:], fc.Frames[in2].Target[:])

	case 0x12: // frame data (dynamic) - returns size 0 for VERIFY frames
		if in2 >= uint64(len(fc.Frames)) {
			return nil, ErrFrameIndexOutOfBounds
		}
		// For VERIFY frames, data is elided (returns empty).
		if fc.Frames[in2].Mode == FrameModeVerify {
			return nil, nil
		}
		return fc.Frames[in2].Data, nil

	case 0x13: // frame gas_limit (in2 = frame index)
		if in2 >= uint64(len(fc.Frames)) {
			return nil, ErrFrameIndexOutOfBounds
		}
		new(big.Int).SetUint64(fc.Frames[in2].GasLimit).FillBytes(result)

	case 0x14: // frame mode (in2 = frame index)
		if in2 >= uint64(len(fc.Frames)) {
			return nil, ErrFrameIndexOutOfBounds
		}
		new(big.Int).SetUint64(fc.Frames[in2].Mode).FillBytes(result)

	case 0x15: // frame status (in2 = frame index)
		if in2 >= uint64(len(fc.Frames)) {
			return nil, ErrFrameIndexOutOfBounds
		}
		// Cannot access status of current or future frames.
		if in2 >= fc.CurrentFrameIndex {
			return nil, ErrFrameIndexOutOfBounds
		}
		new(big.Int).SetUint64(fc.Frames[in2].Status).FillBytes(result)

	default:
		return nil, ErrInvalidTxParamIndex
	}

	return result, nil
}

// txParamSize returns the size of a given TXPARAM parameter.
func txParamSize(fc *FrameContext, in1, in2 uint64) (uint64, error) {
	switch in1 {
	case 0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x10:
		if in2 != 0 {
			return 0, ErrTxParamIn2MustBeZero
		}
		return 32, nil
	case 0x11, 0x13, 0x14, 0x15:
		if in2 >= uint64(len(fc.Frames)) {
			return 0, ErrFrameIndexOutOfBounds
		}
		if in1 == 0x15 && in2 >= fc.CurrentFrameIndex {
			return 0, ErrFrameIndexOutOfBounds
		}
		return 32, nil
	case 0x12: // frame data (dynamic size)
		if in2 >= uint64(len(fc.Frames)) {
			return 0, ErrFrameIndexOutOfBounds
		}
		if fc.Frames[in2].Mode == FrameModeVerify {
			return 0, nil
		}
		return uint64(len(fc.Frames[in2].Data)), nil
	default:
		return 0, ErrInvalidTxParamIndex
	}
}

// opTxParamLoad implements the TXPARAMLOAD opcode (0xb0) per EIP-8141.
// Stack input: [in1, in2] -> pops both, pushes 32-byte result.
func opTxParamLoad(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	in1 := stack.Pop()
	in2 := stack.Pop()

	fc := evm.FrameCtx
	if fc == nil {
		return nil, ErrNoFrameContext
	}

	val, err := txParamValue(fc, in1.Uint64(), in2.Uint64())
	if err != nil {
		return nil, err
	}

	if val == nil {
		// Dynamic parameter with empty data (e.g., VERIFY frame data).
		stack.Push(new(big.Int))
	} else if len(val) <= 32 {
		// Fixed-size parameter: left-pad to 32 bytes if needed.
		padded := make([]byte, 32)
		copy(padded[32-len(val):], val)
		stack.Push(new(big.Int).SetBytes(padded))
	} else {
		// Dynamic parameter > 32 bytes: return first 32 bytes.
		stack.Push(new(big.Int).SetBytes(val[:32]))
	}

	return nil, nil
}

// opTxParamSize implements the TXPARAMSIZE opcode (0xb1) per EIP-8141.
// Stack input: [in1, in2] -> pops both, pushes size of parameter.
func opTxParamSize(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	in1 := stack.Pop()
	in2 := stack.Pop()

	fc := evm.FrameCtx
	if fc == nil {
		return nil, ErrNoFrameContext
	}

	size, err := txParamSize(fc, in1.Uint64(), in2.Uint64())
	if err != nil {
		return nil, err
	}

	stack.Push(new(big.Int).SetUint64(size))
	return nil, nil
}

// opTxParamCopy implements the TXPARAMCOPY opcode (0xb2) per EIP-8141.
// Stack input: [in1, in2, destOffset, offset, length] -> copies param data to memory.
func opTxParamCopy(pc *uint64, evm *EVM, contract *Contract, memory *Memory, stack *Stack) ([]byte, error) {
	in1 := stack.Pop()
	in2 := stack.Pop()
	destOffset := stack.Pop()
	dataOffset := stack.Pop()
	length := stack.Pop()

	fc := evm.FrameCtx
	if fc == nil {
		return nil, ErrNoFrameContext
	}

	l := length.Uint64()
	if l == 0 {
		return nil, nil
	}

	val, err := txParamValue(fc, in1.Uint64(), in2.Uint64())
	if err != nil {
		return nil, err
	}

	dOff := dataOffset.Uint64()
	data := make([]byte, l)
	if val != nil && dOff < uint64(len(val)) {
		copy(data, val[dOff:])
	}
	memory.Set(destOffset.Uint64(), l, data)
	return nil, nil
}

// memoryApprove and memoryTxParamCopy are defined in jump_table.go
// with overflow-safe (uint64, bool) return values.
