package eips

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// simpleCallFn returns a FrameCallFunc that always succeeds, uses half the gas,
// and reports the given approveScope whenever the mode is ModeVerify.
func simpleCallFn(approveScope uint8) FrameCallFunc {
	return func(caller, target types.Address, gasLimit uint64, data []byte, mode uint8, frameIndex int) (uint64, uint64, []*types.Log, bool, uint8, error) {
		gasUsed := gasLimit / 2
		approved := mode == types.ModeVerify
		return types.ReceiptStatusSuccessful, gasUsed, nil, approved, approveScope, nil
	}
}

// customCallFn lets each frame specify its own approve behavior.
type frameApproval struct {
	approved bool
	scope    uint8
}

func customCallFn(approvals []frameApproval) FrameCallFunc {
	return func(caller, target types.Address, gasLimit uint64, data []byte, mode uint8, frameIndex int) (uint64, uint64, []*types.Log, bool, uint8, error) {
		gasUsed := gasLimit / 2
		a := approvals[frameIndex]
		return types.ReceiptStatusSuccessful, gasUsed, nil, a.approved, a.scope, nil
	}
}

func TestExecuteFrameTxSimpleApproveScope2(t *testing.T) {
	// Example 1: Simple transaction with VERIFY + SENDER.
	// VERIFY calls APPROVE(2) to approve both sender and payer.
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	target := types.HexToAddress("0x2222222222222222222222222222222222222222")

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  sender,
		Frames: []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
			{Mode: types.ModeSender, Target: &target, GasLimit: 100000, Data: []byte("call")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	ctx, err := ExecuteFrameTx(tx, 0, simpleCallFn(2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ctx.SenderApproved {
		t.Fatal("sender should be approved")
	}
	if !ctx.PayerApproved {
		t.Fatal("payer should be approved")
	}
	if ctx.Payer != sender {
		t.Fatalf("payer should be sender, got %s", ctx.Payer.Hex())
	}
	if len(ctx.FrameResults) != 2 {
		t.Fatalf("expected 2 frame results, got %d", len(ctx.FrameResults))
	}
	// Each frame should have used half its gas.
	if ctx.FrameResults[0].GasUsed != 25000 {
		t.Fatalf("frame 0 gas: expected 25000, got %d", ctx.FrameResults[0].GasUsed)
	}
	if ctx.FrameResults[1].GasUsed != 50000 {
		t.Fatalf("frame 1 gas: expected 50000, got %d", ctx.FrameResults[1].GasUsed)
	}
}

func TestExecuteFrameTxSponsoredTransaction(t *testing.T) {
	// Example 2: Sponsored transaction.
	// Frame 0: VERIFY on sender -> APPROVE(0) sender execution
	// Frame 1: VERIFY on sponsor -> APPROVE(1) payment
	// Frame 2: SENDER mode on ERC-20
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	sponsor := types.HexToAddress("0x5555555555555555555555555555555555555555")
	erc20 := types.HexToAddress("0x6666666666666666666666666666666666666666")

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  5,
		Sender:  sender,
		Frames: []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sender-sig")},
			{Mode: types.ModeVerify, Target: &sponsor, GasLimit: 30000, Data: []byte("sponsor-sig")},
			{Mode: types.ModeSender, Target: &erc20, GasLimit: 60000, Data: []byte("transfer")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	approvals := []frameApproval{
		{approved: true, scope: 0},  // sender VERIFY -> APPROVE(0)
		{approved: true, scope: 1},  // sponsor VERIFY -> APPROVE(1)
		{approved: false, scope: 0}, // SENDER mode, no APPROVE
	}

	ctx, err := ExecuteFrameTx(tx, 5, customCallFn(approvals))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ctx.SenderApproved {
		t.Fatal("sender should be approved")
	}
	if !ctx.PayerApproved {
		t.Fatal("payer should be approved")
	}
	if ctx.Payer != sponsor {
		t.Fatalf("payer should be sponsor %s, got %s", sponsor.Hex(), ctx.Payer.Hex())
	}
}

func TestExecuteFrameTxSenderModeWithoutApproval(t *testing.T) {
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	target := types.HexToAddress("0x2222222222222222222222222222222222222222")

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  sender,
		Frames: []types.Frame{
			// No VERIFY frame first, directly try SENDER mode.
			{Mode: types.ModeSender, Target: &target, GasLimit: 100000, Data: []byte("call")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	_, err := ExecuteFrameTx(tx, 0, simpleCallFn(2))
	if err != ErrFrameSenderNotApproved {
		t.Fatalf("expected ErrFrameSenderNotApproved, got: %v", err)
	}
}

func TestExecuteFrameTxVerifyNoApprove(t *testing.T) {
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  sender,
		Frames: []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	// Call function that does NOT trigger approve on VERIFY.
	noApproveFn := func(caller, target types.Address, gasLimit uint64, data []byte, mode uint8, frameIndex int) (uint64, uint64, []*types.Log, bool, uint8, error) {
		return types.ReceiptStatusSuccessful, gasLimit / 2, nil, false, 0, nil
	}

	_, err := ExecuteFrameTx(tx, 0, noApproveFn)
	if err != ErrFrameVerifyNoApprove {
		t.Fatalf("expected ErrFrameVerifyNoApprove, got: %v", err)
	}
}

func TestExecuteFrameTxPayerNotApproved(t *testing.T) {
	// VERIFY approves sender (scope=0) but no payer approval.
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	target := types.HexToAddress("0x2222222222222222222222222222222222222222")

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  sender,
		Frames: []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
			{Mode: types.ModeSender, Target: &target, GasLimit: 100000, Data: []byte("call")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	// Only approve sender, not payer.
	approvals := []frameApproval{
		{approved: true, scope: 0},  // APPROVE(0) = sender only
		{approved: false, scope: 0}, // no approve
	}

	_, err := ExecuteFrameTx(tx, 0, customCallFn(approvals))
	if err != ErrFramePayerNotApproved {
		t.Fatalf("expected ErrFramePayerNotApproved, got: %v", err)
	}
}

func TestExecuteFrameTxNonceMismatch(t *testing.T) {
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  5,
		Sender:  sender,
		Frames: []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	_, err := ExecuteFrameTx(tx, 10, simpleCallFn(2))
	if err == nil {
		t.Fatal("expected nonce mismatch error")
	}
}

func TestExecuteFrameTxGasIsolation(t *testing.T) {
	// Verify that gas is isolated per frame -- each frame gets its own gas_limit.
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	target := types.HexToAddress("0x2222222222222222222222222222222222222222")

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  sender,
		Frames: []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
			{Mode: types.ModeSender, Target: &target, GasLimit: 100000, Data: []byte("call1")},
			{Mode: types.ModeSender, Target: &target, GasLimit: 200000, Data: []byte("call2")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	// Each frame uses exactly its gas limit (simulate full consumption).
	fullGasFn := func(caller, target types.Address, gasLimit uint64, data []byte, mode uint8, frameIndex int) (uint64, uint64, []*types.Log, bool, uint8, error) {
		approved := mode == types.ModeVerify
		return types.ReceiptStatusSuccessful, gasLimit, nil, approved, 2, nil
	}

	ctx, err := ExecuteFrameTx(tx, 0, fullGasFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify each frame used exactly its own gas_limit.
	expectedGas := []uint64{50000, 100000, 200000}
	for i, expected := range expectedGas {
		if ctx.FrameResults[i].GasUsed != expected {
			t.Fatalf("frame %d: expected gas %d, got %d", i, expected, ctx.FrameResults[i].GasUsed)
		}
	}
}

func TestExecuteFrameTxDoubleApproveRejects(t *testing.T) {
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  sender,
		Frames: []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig1")},
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig2")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	// Both frames try APPROVE(2), second should fail.
	approvals := []frameApproval{
		{approved: true, scope: 2},
		{approved: true, scope: 2},
	}

	_, err := ExecuteFrameTx(tx, 0, customCallFn(approvals))
	if err == nil {
		t.Fatal("expected error for double approve")
	}
}

func TestCalcFrameRefund(t *testing.T) {
	sender := types.HexToAddress("0xaaaa")
	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  sender,
		Frames: []types.Frame{
			{Mode: types.ModeVerify, GasLimit: 50000},
			{Mode: types.ModeSender, GasLimit: 100000},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	ctx := &FrameExecutionContext{
		FrameResults: []types.FrameResult{
			{GasUsed: 20000},
			{GasUsed: 60000},
		},
	}

	refund := CalcFrameRefund(tx, ctx)
	// total limit = 150000, total used = 80000, refund = 70000
	if refund != 70000 {
		t.Fatalf("expected refund 70000, got %d", refund)
	}
}

func TestCalcFrameRefundNoRefund(t *testing.T) {
	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  types.HexToAddress("0xaaaa"),
		Frames: []types.Frame{
			{Mode: types.ModeDefault, GasLimit: 50000},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	ctx := &FrameExecutionContext{
		FrameResults: []types.FrameResult{
			{GasUsed: 50000},
		},
	}

	refund := CalcFrameRefund(tx, ctx)
	if refund != 0 {
		t.Fatalf("expected refund 0, got %d", refund)
	}
}

func TestBuildFrameReceipt(t *testing.T) {
	ctx := &FrameExecutionContext{
		Payer: types.HexToAddress("0xpayer"),
		FrameResults: []types.FrameResult{
			{Status: 1, GasUsed: 25000, Logs: []*types.Log{{Address: types.HexToAddress("0xlog1")}}},
			{Status: 1, GasUsed: 50000, Logs: nil},
		},
	}

	receipt := BuildFrameReceipt(ctx, 100000)
	if receipt.CumulativeGasUsed != 100000 {
		t.Fatalf("CumulativeGasUsed: expected 100000, got %d", receipt.CumulativeGasUsed)
	}
	if receipt.Payer != ctx.Payer {
		t.Fatal("Payer mismatch")
	}
	if len(receipt.FrameResults) != 2 {
		t.Fatalf("expected 2 frame results, got %d", len(receipt.FrameResults))
	}
	if receipt.TotalGasUsed() != 75000 {
		t.Fatalf("TotalGasUsed: expected 75000, got %d", receipt.TotalGasUsed())
	}
	if len(receipt.AllLogs()) != 1 {
		t.Fatalf("AllLogs: expected 1, got %d", len(receipt.AllLogs()))
	}
}

func TestMaxFrameTxCost(t *testing.T) {
	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  types.HexToAddress("0xaaaa"),
		Frames: []types.Frame{
			{Mode: types.ModeDefault, GasLimit: 100000},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(100),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	cost := MaxFrameTxCost(tx)
	gasLimit := types.CalcFrameTxGas(tx)
	expectedMin := new(big.Int).Mul(new(big.Int).SetUint64(gasLimit), big.NewInt(100))
	if cost.Cmp(expectedMin) != 0 {
		t.Fatalf("expected cost %s, got %s", expectedMin.String(), cost.String())
	}
}

func TestMaxFrameTxCostWithBlobs(t *testing.T) {
	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  types.HexToAddress("0xaaaa"),
		Frames: []types.Frame{
			{Mode: types.ModeDefault, GasLimit: 100000},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(100),
		MaxFeePerBlobGas:     big.NewInt(50),
		BlobVersionedHashes:  []types.Hash{types.HexToHash("0x01"), types.HexToHash("0x02")},
	}

	cost := MaxFrameTxCost(tx)
	gasLimit := types.CalcFrameTxGas(tx)
	execCost := new(big.Int).Mul(new(big.Int).SetUint64(gasLimit), big.NewInt(100))
	blobCost := new(big.Int).Mul(new(big.Int).SetUint64(2*131072), big.NewInt(50))
	expected := new(big.Int).Add(execCost, blobCost)
	if cost.Cmp(expected) != 0 {
		t.Fatalf("expected cost %s, got %s", expected.String(), cost.String())
	}
}

func TestFrameExecutionDefaultModeCaller(t *testing.T) {
	// Verify that DEFAULT mode uses EntryPointAddress as caller.
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	target := types.HexToAddress("0x2222222222222222222222222222222222222222")

	var callersSeen []types.Address

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  sender,
		Frames: []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
			{Mode: types.ModeDefault, Target: &target, GasLimit: 100000, Data: []byte("data")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	trackingFn := func(caller, target types.Address, gasLimit uint64, data []byte, mode uint8, frameIndex int) (uint64, uint64, []*types.Log, bool, uint8, error) {
		callersSeen = append(callersSeen, caller)
		if mode == types.ModeVerify {
			return types.ReceiptStatusSuccessful, gasLimit / 2, nil, true, 2, nil
		}
		return types.ReceiptStatusSuccessful, gasLimit / 2, nil, false, 0, nil
	}

	_, err := ExecuteFrameTx(tx, 0, trackingFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(callersSeen) != 2 {
		t.Fatalf("expected 2 callers, got %d", len(callersSeen))
	}
	// Both VERIFY and DEFAULT should use EntryPointAddress.
	if callersSeen[0] != types.EntryPointAddress {
		t.Fatalf("frame 0 caller: expected EntryPoint %s, got %s",
			types.EntryPointAddress.Hex(), callersSeen[0].Hex())
	}
	if callersSeen[1] != types.EntryPointAddress {
		t.Fatalf("frame 1 caller: expected EntryPoint %s, got %s",
			types.EntryPointAddress.Hex(), callersSeen[1].Hex())
	}
}

func TestFrameExecutionSenderModeCaller(t *testing.T) {
	// Verify that SENDER mode uses tx.Sender as caller.
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	target := types.HexToAddress("0x2222222222222222222222222222222222222222")

	var callersSeen []types.Address

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  sender,
		Frames: []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
			{Mode: types.ModeSender, Target: &target, GasLimit: 100000, Data: []byte("call")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	trackingFn := func(caller, target types.Address, gasLimit uint64, data []byte, mode uint8, frameIndex int) (uint64, uint64, []*types.Log, bool, uint8, error) {
		callersSeen = append(callersSeen, caller)
		if mode == types.ModeVerify {
			return types.ReceiptStatusSuccessful, gasLimit / 2, nil, true, 2, nil
		}
		return types.ReceiptStatusSuccessful, gasLimit / 2, nil, false, 0, nil
	}

	_, err := ExecuteFrameTx(tx, 0, trackingFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(callersSeen) != 2 {
		t.Fatalf("expected 2 callers, got %d", len(callersSeen))
	}
	if callersSeen[1] != sender {
		t.Fatalf("frame 1 caller: expected sender %s, got %s",
			sender.Hex(), callersSeen[1].Hex())
	}
}

func TestFrameExecutionNilTargetDefaultsToSender(t *testing.T) {
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")

	var targetsSeen []types.Address

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:  0,
		Sender:  sender,
		Frames: []types.Frame{
			{Mode: types.ModeVerify, Target: nil, GasLimit: 50000, Data: []byte("sig")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	trackingFn := func(caller, target types.Address, gasLimit uint64, data []byte, mode uint8, frameIndex int) (uint64, uint64, []*types.Log, bool, uint8, error) {
		targetsSeen = append(targetsSeen, target)
		return types.ReceiptStatusSuccessful, gasLimit / 2, nil, true, 2, nil
	}

	_, err := ExecuteFrameTx(tx, 0, trackingFn)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if targetsSeen[0] != sender {
		t.Fatalf("nil target should default to sender %s, got %s",
			sender.Hex(), targetsSeen[0].Hex())
	}
}

// TestApproveScope2WithVerifierContract tests that APPROVE(2) works correctly
// when the VERIFY frame targets a verifier contract that is different from sender.
// This is the common pattern for EIP-8141: a verifier contract validates the
// signature and calls APPROVE(2) to approve both sender execution and payment.
func TestApproveScope2WithVerifierContract(t *testing.T) {
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	verifier := types.HexToAddress("0x2222222222222222222222222222222222222222") // Different from sender!
	target := types.HexToAddress("0x3333333333333333333333333333333333333333")

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  sender,
		Frames: []types.Frame{
			// VERIFY frame targets a verifier contract (not sender)
			{Mode: types.ModeVerify, Target: &verifier, GasLimit: 50000, Data: []byte("signature")},
			// SENDER frame executes user action
			{Mode: types.ModeSender, Target: &target, GasLimit: 100000, Data: []byte("call")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	ctx, err := ExecuteFrameTx(tx, 0, simpleCallFn(2))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ctx.SenderApproved {
		t.Fatal("sender should be approved")
	}
	if !ctx.PayerApproved {
		t.Fatal("payer should be approved")
	}
	// Per EIP-8141: APPROVE(2) sets payer = sender
	if ctx.Payer != sender {
		t.Fatalf("payer should be sender %s, got %s", sender.Hex(), ctx.Payer.Hex())
	}
}

// TestApproveScope0WithVerifierContract tests that APPROVE(0) works correctly
// when the VERIFY frame targets a verifier contract different from sender.
func TestApproveScope0WithVerifierContract(t *testing.T) {
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	verifier := types.HexToAddress("0x2222222222222222222222222222222222222222")
	sponsor := types.HexToAddress("0x555555555555555555555555555555555555555")
	target := types.HexToAddress("0x3333333333333333333333333333333333333333")

	tx := &types.FrameTx{
		ChainID: big.NewInt(1),
		Nonce:   0,
		Sender:  sender,
		Frames: []types.Frame{
			// VERIFY frame 0: verifier calls APPROVE(0) to approve sender
			{Mode: types.ModeVerify, Target: &verifier, GasLimit: 50000, Data: []byte("sig1")},
			// VERIFY frame 1: sponsor calls APPROVE(1) to approve payment
			{Mode: types.ModeVerify, Target: &sponsor, GasLimit: 30000, Data: []byte("sig2")},
			// SENDER frame: execute user action
			{Mode: types.ModeSender, Target: &target, GasLimit: 100000, Data: []byte("call")},
		},
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(10),
		MaxFeePerBlobGas:     big.NewInt(0),
	}

	approvals := []frameApproval{
		{approved: true, scope: 0}, // APPROVE(0) = sender only
		{approved: true, scope: 1}, // APPROVE(1) = payer = sponsor
		{approved: false, scope: 0},
	}

	ctx, err := ExecuteFrameTx(tx, 0, customCallFn(approvals))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ctx.SenderApproved {
		t.Fatal("sender should be approved")
	}
	if !ctx.PayerApproved {
		t.Fatal("payer should be approved")
	}
	// Per EIP-8141: APPROVE(1) sets payer = frame.target (sponsor)
	if ctx.Payer != sponsor {
		t.Fatalf("payer should be sponsor %s, got %s", sponsor.Hex(), ctx.Payer.Hex())
	}
}
