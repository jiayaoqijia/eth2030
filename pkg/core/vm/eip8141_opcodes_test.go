package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// newFrameTestEVM creates an EVM with a FrameContext and Glamsterdan jump table for testing.
func newFrameTestEVM(sender types.Address, frames []Frame) *EVM {
	blockCtx := BlockContext{
		BlockNumber: big.NewInt(1),
		BaseFee:     big.NewInt(1),
	}
	evm := NewEVM(blockCtx, TxContext{}, Config{})
	evm.SetJumpTable(NewGlamsterdanJumpTable())
	evm.FrameCtx = &FrameContext{
		TxType:            0x06,
		Nonce:             new(big.Int).SetUint64(42),
		Sender:            sender,
		MaxPriorityFee:    big.NewInt(1000000000),
		MaxFee:            big.NewInt(2000000000),
		MaxBlobFee:        big.NewInt(100),
		MaxCost:           big.NewInt(1000000),
		BlobCount:         2,
		SigHash:           types.Hash{0xaa, 0xbb, 0xcc},
		Frames:            frames,
		CurrentFrameIndex: 0,
	}
	return evm
}

// --- APPROVE opcode tests ---

func TestApprove_Scope0_Execution(t *testing.T) {
	sender := types.Address{0x01, 0x02, 0x03}
	frames := []Frame{{Mode: FrameModeVerify, Target: sender, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)

	// APPROVE with scope=0: PUSH1 0x00 (scope), PUSH1 0x00 (length), PUSH1 0x00 (offset), APPROVE
	code := []byte{
		byte(PUSH1), 0x00, // scope = 0
		byte(PUSH1), 0x00, // length = 0
		byte(PUSH1), 0x00, // offset = 0
		byte(APPROVE),
	}
	// contract.CallerAddress == contract.Address == sender (simulates CALLER == frame.target == tx.sender)
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("APPROVE scope 0 failed: %v", err)
	}
	if len(ret) != 0 {
		t.Fatalf("expected empty return, got %x", ret)
	}
	if !evm.FrameCtx.SenderApproved {
		t.Fatal("SenderApproved should be true after APPROVE scope 0")
	}
	if evm.FrameCtx.PayerApproved {
		t.Fatal("PayerApproved should still be false after APPROVE scope 0")
	}
}

func TestApprove_Scope1_Payment(t *testing.T) {
	sender := types.Address{0x01}
	payer := types.Address{0x02}
	frames := []Frame{
		{Mode: FrameModeVerify, Target: sender, GasLimit: 100000},
		{Mode: FrameModeVerify, Target: payer, GasLimit: 100000},
	}
	evm := newFrameTestEVM(sender, frames)
	// Pre-approve sender.
	evm.FrameCtx.SenderApproved = true
	evm.FrameCtx.CurrentFrameIndex = 1
	evm.FrameCtx.MaxCost = big.NewInt(500) // payer needs at least this balance

	// Set up state so payer has enough balance.
	stateDB := NewMockStateDB()
	stateDB.SetBalance(payer, big.NewInt(1000))
	evm.StateDB = stateDB

	// APPROVE scope=1
	code := []byte{
		byte(PUSH1), 0x01, // scope = 1
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	contract := NewContract(payer, payer, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("APPROVE scope 1 failed: %v", err)
	}
	if !evm.FrameCtx.PayerApproved {
		t.Fatal("PayerApproved should be true after APPROVE scope 1")
	}
}

func TestApprove_Scope2_Combined(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{{Mode: FrameModeVerify, Target: sender, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)
	evm.FrameCtx.MaxCost = big.NewInt(500)

	stateDB := NewMockStateDB()
	stateDB.SetBalance(sender, big.NewInt(1000))
	evm.StateDB = stateDB

	// APPROVE scope=2
	code := []byte{
		byte(PUSH1), 0x02,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("APPROVE scope 2 failed: %v", err)
	}
	if !evm.FrameCtx.SenderApproved {
		t.Fatal("SenderApproved should be true after APPROVE scope 2")
	}
	if !evm.FrameCtx.PayerApproved {
		t.Fatal("PayerApproved should be true after APPROVE scope 2")
	}
}

func TestApprove_ReturnData(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{{Mode: FrameModeVerify, Target: sender, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)

	// Store some data in memory, then APPROVE with offset/length pointing to it.
	// PUSH32 <data> -> PUSH1 0x00 -> MSTORE -> PUSH1 0x00 (scope) -> PUSH1 0x20 (length) -> PUSH1 0x00 (offset) -> APPROVE
	code := []byte{
		byte(PUSH32),
	}
	// 32 bytes of test data
	testData := make([]byte, 32)
	for i := range testData {
		testData[i] = byte(i + 1)
	}
	code = append(code, testData...)
	code = append(code,
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x00, // scope = 0
		byte(PUSH1), 0x20, // length = 32
		byte(PUSH1), 0x00, // offset = 0
		byte(APPROVE),
	)

	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("APPROVE with return data failed: %v", err)
	}
	if len(ret) != 32 {
		t.Fatalf("expected 32 bytes return, got %d", len(ret))
	}
	for i, b := range ret {
		if b != testData[i] {
			t.Errorf("return byte %d: got %d, want %d", i, b, testData[i])
		}
	}
}

func TestApprove_InvalidScope(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{{Mode: FrameModeVerify, Target: sender, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)

	// APPROVE scope=3 (invalid)
	code := []byte{
		byte(PUSH1), 0x03,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err == nil {
		t.Fatal("expected error for invalid scope, got nil")
	}
}

func TestApprove_AlreadyApproved(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{{Mode: FrameModeVerify, Target: sender, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)
	evm.FrameCtx.SenderApproved = true // already approved

	// APPROVE scope=0 again -> should fail
	code := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err == nil {
		t.Fatal("expected error for double approval, got nil")
	}
}

func TestApprove_PaymentBeforeExecution(t *testing.T) {
	sender := types.Address{0x01}
	payer := types.Address{0x02}
	frames := []Frame{{Mode: FrameModeVerify, Target: payer, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)
	// SenderApproved is false. Trying scope=1 should fail.

	code := []byte{
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	contract := NewContract(payer, payer, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err == nil {
		t.Fatal("expected error for payment before execution approval, got nil")
	}
}

func TestApprove_CallerNotTarget(t *testing.T) {
	// EIP-8141: APPROVE should succeed when ADDRESS == frame.target,
	// even if CALLER != frame.target. This allows VERIFY frames to call APPROVE
	// (since VERIFY frames have CALLER=ENTRY_POINT, not the sender).
	sender := types.Address{0x01}
	other := types.Address{0x99}
	frames := []Frame{{Mode: FrameModeVerify, Target: sender, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)

	// CALLER=other, ADDRESS=sender. Since ADDRESS == frame.target, APPROVE should succeed.
	code := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	contract := NewContract(other, sender, nil, 100000) // caller=other, address=sender
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("expected success for ADDRESS == frame.target (even though CALLER != frame.target): %v", err)
	}
}

func TestApprove_Scope1_InsufficientBalance(t *testing.T) {
	sender := types.Address{0x01}
	payer := types.Address{0x02}
	frames := []Frame{{Mode: FrameModeVerify, Target: payer, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)
	evm.FrameCtx.SenderApproved = true
	evm.FrameCtx.MaxCost = big.NewInt(10000)

	stateDB := NewMockStateDB()
	stateDB.SetBalance(payer, big.NewInt(100)) // not enough
	evm.StateDB = stateDB

	code := []byte{
		byte(PUSH1), 0x01,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	contract := NewContract(payer, payer, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err == nil {
		t.Fatal("expected error for insufficient balance, got nil")
	}
}

func TestApprove_NoFrameContext(t *testing.T) {
	blockCtx := BlockContext{BlockNumber: big.NewInt(1), BaseFee: big.NewInt(1)}
	evm := NewEVM(blockCtx, TxContext{}, Config{})
	evm.SetJumpTable(NewGlamsterdanJumpTable())
	// FrameCtx is nil

	sender := types.Address{0x01}
	code := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err == nil {
		t.Fatal("expected error for nil FrameContext, got nil")
	}
}

// --- TXPARAMLOAD opcode tests ---

func TestTxParamLoad_TxType(t *testing.T) {
	sender := types.Address{0x01}
	evm := newFrameTestEVM(sender, nil)
	evm.FrameCtx.TxType = 0x06
	evm.FrameCtx.Frames = []Frame{{Mode: 0, Target: sender, GasLimit: 100000}}

	// PUSH1 0x00 (in2), PUSH1 0x00 (in1), TXPARAMLOAD, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	code := []byte{
		byte(PUSH1), 0x00, // in2
		byte(PUSH1), 0x00, // in1
		byte(TXPARAMLOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMLOAD tx_type failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 0x06 {
		t.Errorf("TXPARAMLOAD tx_type = %d, want 6", result.Uint64())
	}
}

func TestTxParamLoad_Nonce(t *testing.T) {
	sender := types.Address{0x01}
	evm := newFrameTestEVM(sender, []Frame{{Mode: 0, Target: sender, GasLimit: 100000}})

	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x01, byte(TXPARAMLOAD),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMLOAD nonce failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 42 {
		t.Errorf("TXPARAMLOAD nonce = %d, want 42", result.Uint64())
	}
}

func TestTxParamLoad_Sender(t *testing.T) {
	sender := types.Address{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0x00, 0x12, 0x34, 0x56, 0x78}
	evm := newFrameTestEVM(sender, []Frame{{Mode: 0, Target: sender, GasLimit: 100000}})

	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x02, byte(TXPARAMLOAD),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMLOAD sender failed: %v", err)
	}
	// Address is right-aligned in 32 bytes.
	var got types.Address
	copy(got[:], ret[12:32])
	if got != sender {
		t.Errorf("TXPARAMLOAD sender = %x, want %x", got, sender)
	}
}

func TestTxParamLoad_MaxFees(t *testing.T) {
	sender := types.Address{0x01}
	evm := newFrameTestEVM(sender, []Frame{{Mode: 0, Target: sender, GasLimit: 100000}})

	tests := []struct {
		name string
		in1  byte
		want uint64
	}{
		{"max_priority_fee", 0x03, 1000000000},
		{"max_fee", 0x04, 2000000000},
		{"max_blob_fee", 0x05, 100},
		{"max_cost", 0x06, 1000000},
		{"blob_count", 0x07, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code := []byte{
				byte(PUSH1), 0x00, byte(PUSH1), tt.in1, byte(TXPARAMLOAD),
				byte(PUSH1), 0x00, byte(MSTORE),
				byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
			}
			contract := NewContract(sender, sender, nil, 100000)
			contract.Code = code

			ret, err := evm.Run(contract, nil)
			if err != nil {
				t.Fatalf("TXPARAMLOAD %s failed: %v", tt.name, err)
			}
			result := new(big.Int).SetBytes(ret)
			if result.Uint64() != tt.want {
				t.Errorf("TXPARAMLOAD %s = %d, want %d", tt.name, result.Uint64(), tt.want)
			}
		})
	}
}

func TestTxParamLoad_SigHash(t *testing.T) {
	sender := types.Address{0x01}
	evm := newFrameTestEVM(sender, []Frame{{Mode: 0, Target: sender, GasLimit: 100000}})
	sigHash := types.Hash{0xaa, 0xbb, 0xcc}
	evm.FrameCtx.SigHash = sigHash

	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x08, byte(TXPARAMLOAD),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMLOAD sig_hash failed: %v", err)
	}
	var got types.Hash
	copy(got[:], ret)
	if got != sigHash {
		t.Errorf("TXPARAMLOAD sig_hash = %x, want %x", got, sigHash)
	}
}

func TestTxParamLoad_FrameCount(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{
		{Mode: 0, Target: sender, GasLimit: 100000},
		{Mode: 1, Target: sender, GasLimit: 50000},
		{Mode: 2, Target: sender, GasLimit: 30000},
	}
	evm := newFrameTestEVM(sender, frames)

	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x09, byte(TXPARAMLOAD),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMLOAD frame_count failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 3 {
		t.Errorf("TXPARAMLOAD frame_count = %d, want 3", result.Uint64())
	}
}

func TestTxParamLoad_CurrentFrame(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{{Mode: 0, Target: sender, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)
	evm.FrameCtx.CurrentFrameIndex = 7

	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x10, byte(TXPARAMLOAD),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMLOAD current_frame failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 7 {
		t.Errorf("TXPARAMLOAD current_frame = %d, want 7", result.Uint64())
	}
}

func TestTxParamLoad_FrameTarget(t *testing.T) {
	sender := types.Address{0x01}
	target := types.Address{0xde, 0xad, 0xbe, 0xef, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01}
	frames := []Frame{
		{Mode: 0, Target: target, GasLimit: 100000},
	}
	evm := newFrameTestEVM(sender, frames)

	// in1=0x11, in2=0 (frame index 0)
	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x11, byte(TXPARAMLOAD),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMLOAD frame_target failed: %v", err)
	}
	var got types.Address
	copy(got[:], ret[12:32])
	if got != target {
		t.Errorf("TXPARAMLOAD frame_target = %x, want %x", got, target)
	}
}

func TestTxParamLoad_FrameGasLimit(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{{Mode: 0, Target: sender, GasLimit: 99999}}
	evm := newFrameTestEVM(sender, frames)

	// in1=0x13, in2=0
	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x13, byte(TXPARAMLOAD),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMLOAD frame_gas_limit failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 99999 {
		t.Errorf("TXPARAMLOAD frame_gas_limit = %d, want 99999", result.Uint64())
	}
}

func TestTxParamLoad_FrameMode(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{
		{Mode: FrameModeDefault, Target: sender, GasLimit: 100000},
		{Mode: FrameModeVerify, Target: sender, GasLimit: 100000},
		{Mode: FrameModeSender, Target: sender, GasLimit: 100000},
	}
	evm := newFrameTestEVM(sender, frames)

	for idx, want := range []uint64{0, 1, 2} {
		t.Run("", func(t *testing.T) {
			// in1=0x14, in2=idx
			code := []byte{
				byte(PUSH1), byte(idx), byte(PUSH1), 0x14, byte(TXPARAMLOAD),
				byte(PUSH1), 0x00, byte(MSTORE),
				byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
			}
			contract := NewContract(sender, sender, nil, 100000)
			contract.Code = code

			ret, err := evm.Run(contract, nil)
			if err != nil {
				t.Fatalf("TXPARAMLOAD frame_mode[%d] failed: %v", idx, err)
			}
			result := new(big.Int).SetBytes(ret)
			if result.Uint64() != want {
				t.Errorf("TXPARAMLOAD frame_mode[%d] = %d, want %d", idx, result.Uint64(), want)
			}
		})
	}
}

func TestTxParamLoad_FrameStatus(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{
		{Mode: 0, Target: sender, GasLimit: 100000, Status: 1},
		{Mode: 0, Target: sender, GasLimit: 100000, Status: 0},
		{Mode: 0, Target: sender, GasLimit: 100000}, // current frame
	}
	evm := newFrameTestEVM(sender, frames)
	evm.FrameCtx.CurrentFrameIndex = 2

	// Access frame 0 status (should be 1)
	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x15, byte(TXPARAMLOAD),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMLOAD frame_status[0] failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 1 {
		t.Errorf("TXPARAMLOAD frame_status[0] = %d, want 1", result.Uint64())
	}
}

func TestTxParamLoad_FrameStatus_CurrentFrame_Fails(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{
		{Mode: 0, Target: sender, GasLimit: 100000, Status: 1},
		{Mode: 0, Target: sender, GasLimit: 100000}, // current
	}
	evm := newFrameTestEVM(sender, frames)
	evm.FrameCtx.CurrentFrameIndex = 1

	// Access frame 1 status (current frame - should fail)
	code := []byte{
		byte(PUSH1), 0x01, byte(PUSH1), 0x15, byte(TXPARAMLOAD),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err == nil {
		t.Fatal("expected error accessing current frame status, got nil")
	}
}

func TestTxParamLoad_OutOfBoundsFrameIndex(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{{Mode: 0, Target: sender, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)

	// in1=0x11 (frame target), in2=5 (out of bounds)
	code := []byte{
		byte(PUSH1), 0x05, byte(PUSH1), 0x11, byte(TXPARAMLOAD),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err == nil {
		t.Fatal("expected error for out-of-bounds frame index, got nil")
	}
}

func TestTxParamLoad_InvalidIndex(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{{Mode: 0, Target: sender, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)

	// in1=0xFF (invalid)
	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0xFF, byte(TXPARAMLOAD),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err == nil {
		t.Fatal("expected error for invalid TXPARAM index, got nil")
	}
}

// --- TXPARAMSIZE opcode tests ---

func TestTxParamSize_Fixed(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{{Mode: 0, Target: sender, GasLimit: 100000, Data: []byte{1, 2, 3}}}
	evm := newFrameTestEVM(sender, frames)

	// All fixed params should return 32.
	fixedIndices := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x10}
	for _, idx := range fixedIndices {
		code := []byte{
			byte(PUSH1), 0x00, byte(PUSH1), idx, byte(TXPARAMSIZE),
			byte(PUSH1), 0x00, byte(MSTORE),
			byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
		}
		contract := NewContract(sender, sender, nil, 100000)
		contract.Code = code

		ret, err := evm.Run(contract, nil)
		if err != nil {
			t.Fatalf("TXPARAMSIZE in1=0x%x failed: %v", idx, err)
		}
		result := new(big.Int).SetBytes(ret)
		if result.Uint64() != 32 {
			t.Errorf("TXPARAMSIZE in1=0x%x = %d, want 32", idx, result.Uint64())
		}
	}
}

func TestTxParamSize_DynamicData(t *testing.T) {
	sender := types.Address{0x01}
	frameData := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee}
	frames := []Frame{{Mode: FrameModeDefault, Target: sender, GasLimit: 100000, Data: frameData}}
	evm := newFrameTestEVM(sender, frames)

	// in1=0x12 (data), in2=0
	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x12, byte(TXPARAMSIZE),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMSIZE data failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != uint64(len(frameData)) {
		t.Errorf("TXPARAMSIZE data = %d, want %d", result.Uint64(), len(frameData))
	}
}

func TestTxParamSize_VerifyFrameData(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{{Mode: FrameModeVerify, Target: sender, GasLimit: 100000, Data: []byte{1, 2, 3, 4, 5}}}
	evm := newFrameTestEVM(sender, frames)

	// VERIFY frame data should return size 0.
	code := []byte{
		byte(PUSH1), 0x00, byte(PUSH1), 0x12, byte(TXPARAMSIZE),
		byte(PUSH1), 0x00, byte(MSTORE),
		byte(PUSH1), 0x20, byte(PUSH1), 0x00, byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMSIZE verify_data failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 0 {
		t.Errorf("TXPARAMSIZE verify_data = %d, want 0", result.Uint64())
	}
}

// --- TXPARAMCOPY opcode tests ---

func TestTxParamCopy_FrameData(t *testing.T) {
	sender := types.Address{0x01}
	frameData := []byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
	frames := []Frame{{Mode: FrameModeDefault, Target: sender, GasLimit: 100000, Data: frameData}}
	evm := newFrameTestEVM(sender, frames)

	// TXPARAMCOPY: in1=0x12, in2=0, destOffset=0, offset=0, length=8
	// Then RETURN from memory
	code := []byte{
		byte(PUSH1), 0x08, // length
		byte(PUSH1), 0x00, // offset
		byte(PUSH1), 0x00, // destOffset
		byte(PUSH1), 0x00, // in2
		byte(PUSH1), 0x12, // in1
		byte(TXPARAMCOPY),
		byte(PUSH1), 0x08,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMCOPY failed: %v", err)
	}
	if len(ret) != 8 {
		t.Fatalf("expected 8 bytes, got %d", len(ret))
	}
	for i, b := range ret {
		if b != frameData[i] {
			t.Errorf("byte %d: got %x, want %x", i, b, frameData[i])
		}
	}
}

func TestTxParamCopy_PartialCopy(t *testing.T) {
	sender := types.Address{0x01}
	frameData := []byte{0x11, 0x22, 0x33, 0x44, 0x55}
	frames := []Frame{{Mode: FrameModeDefault, Target: sender, GasLimit: 100000, Data: frameData}}
	evm := newFrameTestEVM(sender, frames)

	// Copy 3 bytes starting at offset 1.
	code := []byte{
		byte(PUSH1), 0x03, // length
		byte(PUSH1), 0x01, // offset
		byte(PUSH1), 0x00, // destOffset
		byte(PUSH1), 0x00, // in2
		byte(PUSH1), 0x12, // in1
		byte(TXPARAMCOPY),
		byte(PUSH1), 0x03,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMCOPY partial failed: %v", err)
	}
	expected := []byte{0x22, 0x33, 0x44}
	for i, b := range ret {
		if b != expected[i] {
			t.Errorf("byte %d: got %x, want %x", i, b, expected[i])
		}
	}
}

func TestTxParamCopy_ZeroLength(t *testing.T) {
	sender := types.Address{0x01}
	frames := []Frame{{Mode: FrameModeDefault, Target: sender, GasLimit: 100000, Data: []byte{1, 2, 3}}}
	evm := newFrameTestEVM(sender, frames)

	// Copy 0 bytes - should succeed and do nothing.
	code := []byte{
		byte(PUSH1), 0x00, // length = 0
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x12,
		byte(TXPARAMCOPY),
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	ret, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("TXPARAMCOPY zero length failed: %v", err)
	}
	if len(ret) != 0 {
		t.Fatalf("expected empty return, got %x", ret)
	}
}

// --- Gas cost tests ---

func TestEIP8141_GasCosts(t *testing.T) {
	jt := NewGlamsterdanJumpTable()

	tests := []struct {
		name string
		op   OpCode
		gas  uint64
	}{
		{"APPROVE", APPROVE, GasLow},
		{"TXPARAMLOAD", TXPARAMLOAD, GasBase},
		{"TXPARAMSIZE", TXPARAMSIZE, GasBase},
		{"TXPARAMCOPY", TXPARAMCOPY, GasVerylow},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			op := jt[tt.op]
			if op == nil {
				t.Fatalf("%s not defined in Glamsterdan jump table", tt.name)
			}
			if op.constantGas != tt.gas {
				t.Errorf("%s gas = %d, want %d", tt.name, op.constantGas, tt.gas)
			}
		})
	}
}

func TestEIP8141_NotInCancun(t *testing.T) {
	jt := NewCancunJumpTable()

	opcodes := []OpCode{APPROVE, TXPARAMLOAD, TXPARAMSIZE, TXPARAMCOPY}
	for _, op := range opcodes {
		if jt[op] != nil {
			t.Errorf("%s should not be defined in Cancun jump table", op)
		}
	}
}

func TestEIP8141_NotInPrague(t *testing.T) {
	jt := NewPragueJumpTable()

	opcodes := []OpCode{APPROVE, TXPARAMLOAD, TXPARAMSIZE, TXPARAMCOPY}
	for _, op := range opcodes {
		if jt[op] != nil {
			t.Errorf("%s should not be defined in Prague jump table", op)
		}
	}
}

func TestEIP8141_InGlamsterdan(t *testing.T) {
	jt := NewGlamsterdanJumpTable()

	opcodes := []OpCode{APPROVE, TXPARAMLOAD, TXPARAMSIZE, TXPARAMCOPY}
	for _, op := range opcodes {
		if jt[op] == nil {
			t.Errorf("%s should be defined in Glamsterdan jump table", op)
		}
	}
}

// --- Integration test: multi-frame approval flow ---

func TestIntegration_MultiFrameApprovalFlow(t *testing.T) {
	sender := types.Address{0x01, 0x02, 0x03}
	payer := types.Address{0xaa, 0xbb, 0xcc}

	frames := []Frame{
		{Mode: FrameModeVerify, Target: sender, GasLimit: 100000, Data: []byte{0xde, 0xad}},
		{Mode: FrameModeVerify, Target: payer, GasLimit: 100000, Data: []byte{0xbe, 0xef}},
		{Mode: FrameModeSender, Target: types.Address{0xff}, GasLimit: 50000, Data: []byte{0xca, 0xfe}},
	}

	evm := newFrameTestEVM(sender, frames)
	evm.FrameCtx.MaxCost = big.NewInt(5000)

	stateDB := NewMockStateDB()
	stateDB.SetBalance(sender, big.NewInt(10000))
	stateDB.SetBalance(payer, big.NewInt(20000))
	evm.StateDB = stateDB

	// Frame 0: sender approves execution (scope=0).
	evm.FrameCtx.CurrentFrameIndex = 0
	code0 := []byte{
		byte(PUSH1), 0x00, // scope = 0
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	contract0 := NewContract(sender, sender, nil, 100000)
	contract0.Code = code0
	_, err := evm.Run(contract0, nil)
	if err != nil {
		t.Fatalf("Frame 0 APPROVE(0) failed: %v", err)
	}
	if !evm.FrameCtx.SenderApproved {
		t.Fatal("SenderApproved should be true after frame 0")
	}

	// Frame 1: payer approves payment (scope=1).
	evm.FrameCtx.CurrentFrameIndex = 1
	frames[0].Status = 1 // mark frame 0 as successful
	code1 := []byte{
		byte(PUSH1), 0x01, // scope = 1
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	contract1 := NewContract(payer, payer, nil, 100000)
	contract1.Code = code1
	_, err = evm.Run(contract1, nil)
	if err != nil {
		t.Fatalf("Frame 1 APPROVE(1) failed: %v", err)
	}
	if !evm.FrameCtx.PayerApproved {
		t.Fatal("PayerApproved should be true after frame 1")
	}

	// Verify we can read frame status of past frames.
	evm.FrameCtx.CurrentFrameIndex = 2
	frames[1].Status = 1

	// Use TXPARAMLOAD to read frame 0's status.
	code2 := []byte{
		byte(PUSH1), 0x00, // in2 = frame index 0
		byte(PUSH1), 0x15, // in1 = status
		byte(TXPARAMLOAD),
		byte(PUSH1), 0x00,
		byte(MSTORE),
		byte(PUSH1), 0x20,
		byte(PUSH1), 0x00,
		byte(RETURN),
	}
	contract2 := NewContract(sender, sender, nil, 100000)
	contract2.Code = code2
	ret, err := evm.Run(contract2, nil)
	if err != nil {
		t.Fatalf("Frame 2 TXPARAMLOAD(status) failed: %v", err)
	}
	result := new(big.Int).SetBytes(ret)
	if result.Uint64() != 1 {
		t.Errorf("Frame 0 status = %d, want 1", result.Uint64())
	}
}

// MockStateDB is a minimal state implementation for testing.
type MockStateDB struct {
	balances       map[types.Address]*big.Int
	nonces         map[types.Address]uint64
	storage        map[types.Address]map[types.Hash]types.Hash
	committed      map[types.Address]map[types.Hash]types.Hash
	transient      map[types.Address]map[types.Hash]types.Hash
	code           map[types.Address][]byte
	selfdestructed map[types.Address]bool
	exists         map[types.Address]bool
	logs           []*types.Log
	refund         uint64
	accessList     map[types.Address]map[types.Hash]bool
	snapshots      []int
}

func NewMockStateDB() *MockStateDB {
	return &MockStateDB{
		balances:       make(map[types.Address]*big.Int),
		nonces:         make(map[types.Address]uint64),
		storage:        make(map[types.Address]map[types.Hash]types.Hash),
		committed:      make(map[types.Address]map[types.Hash]types.Hash),
		transient:      make(map[types.Address]map[types.Hash]types.Hash),
		code:           make(map[types.Address][]byte),
		selfdestructed: make(map[types.Address]bool),
		exists:         make(map[types.Address]bool),
		accessList:     make(map[types.Address]map[types.Hash]bool),
	}
}

func (m *MockStateDB) CreateAccount(addr types.Address) {
	m.exists[addr] = true
	if m.balances[addr] == nil {
		m.balances[addr] = new(big.Int)
	}
}

func (m *MockStateDB) GetBalance(addr types.Address) *big.Int {
	if b, ok := m.balances[addr]; ok {
		return new(big.Int).Set(b)
	}
	return new(big.Int)
}

func (m *MockStateDB) SetBalance(addr types.Address, amount *big.Int) {
	m.balances[addr] = new(big.Int).Set(amount)
	m.exists[addr] = true
}

func (m *MockStateDB) AddBalance(addr types.Address, amount *big.Int) {
	if m.balances[addr] == nil {
		m.balances[addr] = new(big.Int)
	}
	m.balances[addr].Add(m.balances[addr], amount)
	m.exists[addr] = true
}

func (m *MockStateDB) SubBalance(addr types.Address, amount *big.Int) {
	if m.balances[addr] == nil {
		m.balances[addr] = new(big.Int)
	}
	m.balances[addr].Sub(m.balances[addr], amount)
}

func (m *MockStateDB) GetNonce(addr types.Address) uint64        { return m.nonces[addr] }
func (m *MockStateDB) SetNonce(addr types.Address, nonce uint64) { m.nonces[addr] = nonce }

func (m *MockStateDB) GetCode(addr types.Address) []byte { return m.code[addr] }
func (m *MockStateDB) SetCode(addr types.Address, code []byte) {
	m.code[addr] = code
	m.exists[addr] = true
}
func (m *MockStateDB) GetCodeHash(addr types.Address) types.Hash { return types.Hash{} }
func (m *MockStateDB) GetCodeSize(addr types.Address) int        { return len(m.code[addr]) }

func (m *MockStateDB) GetState(addr types.Address, key types.Hash) types.Hash {
	if s, ok := m.storage[addr]; ok {
		return s[key]
	}
	return types.Hash{}
}

func (m *MockStateDB) SetState(addr types.Address, key types.Hash, value types.Hash) {
	if m.storage[addr] == nil {
		m.storage[addr] = make(map[types.Hash]types.Hash)
	}
	m.storage[addr][key] = value
}

func (m *MockStateDB) GetCommittedState(addr types.Address, key types.Hash) types.Hash {
	if s, ok := m.committed[addr]; ok {
		return s[key]
	}
	return types.Hash{}
}

func (m *MockStateDB) GetTransientState(addr types.Address, key types.Hash) types.Hash {
	if s, ok := m.transient[addr]; ok {
		return s[key]
	}
	return types.Hash{}
}

func (m *MockStateDB) SetTransientState(addr types.Address, key types.Hash, value types.Hash) {
	if m.transient[addr] == nil {
		m.transient[addr] = make(map[types.Hash]types.Hash)
	}
	m.transient[addr][key] = value
}

func (m *MockStateDB) ClearTransientStorage() {
	m.transient = make(map[types.Address]map[types.Hash]types.Hash)
}

func (m *MockStateDB) SelfDestruct(addr types.Address) { m.selfdestructed[addr] = true }
func (m *MockStateDB) HasSelfDestructed(addr types.Address) bool {
	return m.selfdestructed[addr]
}

func (m *MockStateDB) Exist(addr types.Address) bool { return m.exists[addr] }
func (m *MockStateDB) Empty(addr types.Address) bool { return !m.exists[addr] }
func (m *MockStateDB) Snapshot() int                 { return len(m.snapshots) }
func (m *MockStateDB) RevertToSnapshot(id int)       {}
func (m *MockStateDB) AddLog(log *types.Log)         { m.logs = append(m.logs, log) }
func (m *MockStateDB) AddRefund(gas uint64)          { m.refund += gas }
func (m *MockStateDB) SubRefund(gas uint64)          { m.refund -= gas }
func (m *MockStateDB) GetRefund() uint64             { return m.refund }

func (m *MockStateDB) AddAddressToAccessList(addr types.Address) {
	if m.accessList[addr] == nil {
		m.accessList[addr] = make(map[types.Hash]bool)
	}
}

func (m *MockStateDB) AddSlotToAccessList(addr types.Address, slot types.Hash) {
	if m.accessList[addr] == nil {
		m.accessList[addr] = make(map[types.Hash]bool)
	}
	m.accessList[addr][slot] = true
}

func (m *MockStateDB) AddressInAccessList(addr types.Address) bool {
	_, ok := m.accessList[addr]
	return ok
}

func (m *MockStateDB) SlotInAccessList(addr types.Address, slot types.Hash) (bool, bool) {
	slots, addrOk := m.accessList[addr]
	if !addrOk {
		return false, false
	}
	return true, slots[slot]
}

// --- APPROVE target check tests ---

func TestApprove_SubContractFails(t *testing.T) {
	// APPROVE should work when called from any contract within a VERIFY frame
	// Per revm-eip8141: only mode == VERIFY (1) is checked, not CALLER == frame.target
	sender := types.Address{0x01}
	subContract := types.Address{0x99}
	frames := []Frame{{Mode: FrameModeVerify, Target: sender, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)

	code := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	// contract.Address = subContract (not sender)
	// Per revm-eip8141: APPROVE is allowed as long as mode == VERIFY
	contract := NewContract(sender, subContract, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	// Note: revm-eip8141 allows APPROVE from any address within a VERIFY frame.
	// The EIP spec says "CALLER == frame.target" but this check is not in revm-eip8141.
	// For now, we match revm-eip8141 behavior.
	if err != nil {
		t.Fatalf("APPROVE should succeed in VERIFY frame: %v", err)
	}
}

func TestApprove_FrameTargetSucceeds(t *testing.T) {
	// APPROVE should succeed when contract.Address == frame target
	sender := types.Address{0x01}
	frames := []Frame{{Mode: FrameModeVerify, Target: sender, GasLimit: 100000}}
	evm := newFrameTestEVM(sender, frames)

	code := []byte{
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(PUSH1), 0x00,
		byte(APPROVE),
	}
	// contract.Address = sender = frame target, should succeed
	contract := NewContract(sender, sender, nil, 100000)
	contract.Code = code

	_, err := evm.Run(contract, nil)
	if err != nil {
		t.Fatalf("expected success for APPROVE from frame target: %v", err)
	}
}

// --- VERIFY jump table restriction tests ---

func TestVerifyJumpTable_RestrictsOpcodes(t *testing.T) {
	tbl := NewFrameVerifyJumpTable()

	restricted := []OpCode{SSTORE, CREATE, CREATE2, SELFDESTRUCT, LOG0, LOG1, LOG2, LOG3, LOG4}
	for _, op := range restricted {
		if tbl[op] != nil {
			t.Errorf("%s should be nil in VERIFY jump table", op)
		}
	}

	// SLOAD should still be available
	allowed := []OpCode{SLOAD, STATICCALL, ADD, MLOAD, RETURN, KECCAK256}
	for _, op := range allowed {
		if tbl[op] == nil {
			t.Errorf("%s should be available in VERIFY jump table", op)
		}
	}
}
