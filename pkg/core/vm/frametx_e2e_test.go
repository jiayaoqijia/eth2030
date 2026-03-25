package vm

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// TestE2E_FrameTx_WithDelegation tests a complete FrameTx execution with EIP-7702 delegation.
// This simulates the exact scenario in the devnet:
// 1. Sender has delegated to SimpleApprover (EIP-7702)
// 2. FrameTx has a VERIFY frame targeting sender (which delegates to SimpleApprover)
// 3. SimpleApprover calls APPROVE(2) to approve both sender and payer
func TestE2E_FrameTx_WithDelegation(t *testing.T) {
	// Set up state using MockStateDB.
	stateDB := NewMockStateDB()

	sender := types.HexToAddress("0xde9D246563adB29D7762617103B6f061DD4a4558")
	approver := types.HexToAddress("0x6D264Bf712D5F9698e82E97E0BaEE255eA4BD1c3")
	target := types.HexToAddress("0xabcdefabcdefabcdefabcdefabcdefabcdefabcd")

	// Set up sender with delegation to approver (EIP-7702).
	// Designator: 0xef0100 || approver_address
	designator := make([]byte, 23)
	designator[0] = 0xef
	designator[1] = 0x01
	designator[2] = 0x00
	copy(designator[3:], approver[:])

	stateDB.CreateAccount(sender)
	stateDB.SetCode(sender, designator)
	stateDB.SetNonce(sender, 2)
	stateDB.AddBalance(sender, big.NewInt(1000000000000000000)) // 1 ETH

	// Set up approver contract with SimpleApprover code.
	// PUSH1 0x02, PUSH1 0x00, PUSH1 0x00, APPROVE
	simpleApproverCode := []byte{0x60, 0x02, 0x60, 0x00, 0x60, 0x00, 0xaa}
	stateDB.CreateAccount(approver)
	stateDB.SetCode(approver, simpleApproverCode)
	stateDB.AddBalance(approver, big.NewInt(1000000000000000000))

	// Set up target.
	stateDB.CreateAccount(target)

	// Create EVM with Glamsterdan jump table.
	blockCtx := BlockContext{
		BlockNumber: big.NewInt(100),
		BaseFee:     big.NewInt(1000000000),
	}
	evm := NewEVM(blockCtx, TxContext{}, Config{})
	evm.SetJumpTable(NewGlamsterdanJumpTable())
	evm.StateDB = stateDB

	// Set up frame context - this matches the processor.go setup.
	evm.FrameCtx = &FrameContext{
		TxType:            0x06,
		Nonce:             new(big.Int).SetUint64(2),
		Sender:            sender,
		MaxPriorityFee:    big.NewInt(1000000000),
		MaxFee:            big.NewInt(2000000000),
		MaxCost:           big.NewInt(100000000000), // 100 ETH
		Frames: []Frame{
			{Mode: 1, Target: sender, GasLimit: 50000, Data: []byte{}}, // ModeVerify
			{Mode: 2, Target: target, GasLimit: 21000, Data: []byte{}}, // ModeSender
		},
		CurrentFrameIndex: 0,
	}

	// Execute frame 0 (VERIFY).
	evm.FrameCtx.CurrentFrameIndex = 0
	// For VERIFY, caller is EntryPoint.
	caller := types.EntryPointAddress
	// Target is sender (which delegates to approver).
	frameTarget := sender

	t.Logf("Executing VERIFY frame: caller=%s, target=%s", caller.Hex(), frameTarget.Hex())
	t.Logf("Target code (delegation): %x", stateDB.GetCode(frameTarget))
	t.Logf("Approver code: %x", stateDB.GetCode(approver))

	ret, gasLeft, err := evm.StaticCall(caller, frameTarget, []byte{}, 50000)
	t.Logf("Frame 0 (VERIFY): ret=%x, gasLeft=%d, err=%v", ret, gasLeft, err)
	t.Logf("After execution: ApproveCalledThisFrame=%v, LastApproveScope=%d",
		evm.FrameCtx.ApproveCalledThisFrame, evm.FrameCtx.LastApproveScope)
	t.Logf("SenderApproved=%v, PayerApproved=%v",
		evm.FrameCtx.SenderApproved, evm.FrameCtx.PayerApproved)

	if err != nil {
		t.Fatalf("Frame 0 execution failed: %v", err)
	}

	// Check if APPROVE was called.
	if !evm.FrameCtx.SenderApproved {
		t.Fatal("Sender should be approved after frame 0")
	}
	if !evm.FrameCtx.PayerApproved {
		t.Fatal("Payer should be approved after frame 0 (APPROVE(2))")
	}

	t.Logf("Frame 0 success: SenderApproved=%v, PayerApproved=%v",
		evm.FrameCtx.SenderApproved, evm.FrameCtx.PayerApproved)
}

// TestE2E_FrameTx_VerifyFrameWithExplicitTarget tests VERIFY frame with explicit target.
func TestE2E_FrameTx_VerifyFrameWithExplicitTarget(t *testing.T) {
	stateDB := NewMockStateDB()

	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	verifier := types.HexToAddress("0x6D264Bf712D5F9698e82E97E0BaEE255eA4BD1c3")
	target := types.HexToAddress("0x2222222222222222222222222222222222222222")

	// Set up sender.
	stateDB.CreateAccount(sender)
	stateDB.SetNonce(sender, 0)
	stateDB.AddBalance(sender, big.NewInt(1000000000000000000))

	// Set up verifier with SimpleApprover code.
	simpleApproverCode := []byte{0x60, 0x02, 0x60, 0x00, 0x60, 0x00, 0xaa}
	stateDB.CreateAccount(verifier)
	stateDB.SetCode(verifier, simpleApproverCode)
	stateDB.AddBalance(verifier, big.NewInt(1000000000000000000))

	// Set up target.
	stateDB.CreateAccount(target)

	blockCtx := BlockContext{
		BlockNumber: big.NewInt(100),
		BaseFee:     big.NewInt(1000000000),
	}
	evm := NewEVM(blockCtx, TxContext{}, Config{})
	evm.SetJumpTable(NewGlamsterdanJumpTable())
	evm.StateDB = stateDB

	evm.FrameCtx = &FrameContext{
		TxType:            0x06,
		Nonce:             new(big.Int),
		Sender:            sender,
		MaxPriorityFee:    big.NewInt(1000000000),
		MaxFee:            big.NewInt(2000000000),
		MaxCost:           big.NewInt(100000000000),
		Frames: []Frame{
			{Mode: 1, Target: verifier, GasLimit: 50000, Data: []byte{}}, // VERIFY with explicit verifier
			{Mode: 2, Target: target, GasLimit: 21000, Data: []byte{}},
		},
		CurrentFrameIndex: 0,
	}

	// Execute VERIFY frame targeting verifier.
	ret, gasLeft, err := evm.StaticCall(types.EntryPointAddress, verifier, []byte{}, 50000)
	t.Logf("Frame 0 (VERIFY): ret=%x, gasLeft=%d, err=%v", ret, gasLeft, err)

	if err != nil {
		t.Fatalf("Frame 0 execution failed: %v", err)
	}

	if !evm.FrameCtx.SenderApproved {
		t.Fatal("Sender should be approved")
	}
	if !evm.FrameCtx.PayerApproved {
		t.Fatal("Payer should be approved")
	}
}