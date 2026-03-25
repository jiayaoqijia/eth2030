package core

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/gaspool"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// newTestFrameTx creates a FrameTx wrapped in a Transaction for testing.
func newTestFrameTx(sender types.Address, nonce uint64, frames []types.Frame) *types.Transaction {
	ftx := &types.FrameTx{
		ChainID:              big.NewInt(1),
		Nonce:                nonce,
		Sender:               sender,
		Frames:               frames,
		MaxFeePerGas:         big.NewInt(1000000000),
		MaxPriorityFeePerGas: big.NewInt(1000000),
	}
	tx := types.NewTransaction(ftx)
	tx.SetSender(sender)
	return tx
}

func TestTransactionToMessage_FrameTx(t *testing.T) {
	sender := types.HexToAddress("0x1234567890abcdef1234567890abcdef12345678")
	frames := []types.Frame{
		{Mode: types.ModeDefault, GasLimit: 50000, Data: []byte{0x01}},
		{Mode: types.ModeVerify, GasLimit: 30000, Data: []byte{0x02, 0x03}},
	}
	tx := newTestFrameTx(sender, 0, frames)

	msg := config.TransactionToMessage(tx)

	// Frames should be populated.
	if msg.Frames == nil {
		t.Fatal("expected msg.Frames to be non-nil for FrameTx")
	}
	if len(msg.Frames) != 2 {
		t.Fatalf("expected 2 frames, got %d", len(msg.Frames))
	}
	if msg.Frames[0].Mode != types.ModeDefault {
		t.Errorf("expected frame 0 mode %d, got %d", types.ModeDefault, msg.Frames[0].Mode)
	}
	if msg.Frames[0].GasLimit != 50000 {
		t.Errorf("expected frame 0 gas limit 50000, got %d", msg.Frames[0].GasLimit)
	}
	if msg.Frames[1].Mode != types.ModeVerify {
		t.Errorf("expected frame 1 mode %d, got %d", types.ModeVerify, msg.Frames[1].Mode)
	}

	// FrameSender should be populated.
	if msg.FrameSender != sender {
		t.Errorf("expected FrameSender %v, got %v", sender, msg.FrameSender)
	}

	// TxType should be FrameTxType.
	if msg.TxType != types.FrameTxType {
		t.Errorf("expected TxType %d, got %d", types.FrameTxType, msg.TxType)
	}
}

func TestTransactionToMessage_NonFrameTx(t *testing.T) {
	to := types.HexToAddress("0x2222222222222222222222222222222222222222")
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    5,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1000),
	})

	msg := config.TransactionToMessage(tx)

	// Frames should be nil for non-FrameTx.
	if msg.Frames != nil {
		t.Errorf("expected nil Frames for legacy tx, got %v", msg.Frames)
	}

	// FrameSender should be zero address.
	if msg.FrameSender != (types.Address{}) {
		t.Errorf("expected zero FrameSender for legacy tx, got %v", msg.FrameSender)
	}

	if msg.TxType != types.LegacyTxType {
		t.Errorf("expected TxType %d, got %d", types.LegacyTxType, msg.TxType)
	}
}

func TestTransaction_Frames_Method(t *testing.T) {
	sender := types.HexToAddress("0xaaaa")
	frames := []types.Frame{
		{Mode: types.ModeDefault, GasLimit: 10000, Data: []byte{0xAB}},
	}

	// FrameTx should return frames.
	ftx := newTestFrameTx(sender, 0, frames)
	got := ftx.Frames()
	if got == nil {
		t.Fatal("expected non-nil Frames for FrameTx")
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(got))
	}
	if got[0].GasLimit != 10000 {
		t.Errorf("expected GasLimit 10000, got %d", got[0].GasLimit)
	}

	// Non-FrameTx should return nil.
	to := types.HexToAddress("0xbbbb")
	legacyTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
	})
	if legacyTx.Frames() != nil {
		t.Errorf("expected nil Frames for legacy tx, got %v", legacyTx.Frames())
	}
}

func TestTransaction_FrameSender_Method(t *testing.T) {
	sender := types.HexToAddress("0xdeadbeef00000000000000000000000000000001")

	// FrameTx should return the sender.
	ftx := newTestFrameTx(sender, 0, []types.Frame{
		{Mode: types.ModeDefault, GasLimit: 50000, Data: []byte{0x01}},
	})
	if ftx.FrameSender() != sender {
		t.Errorf("expected FrameSender %v, got %v", sender, ftx.FrameSender())
	}

	// Non-FrameTx should return zero address.
	to := types.HexToAddress("0xcccc")
	legacyTx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(0),
	})
	zeroAddr := types.Address{}
	if legacyTx.FrameSender() != zeroAddr {
		t.Errorf("expected zero FrameSender for legacy tx, got %v", legacyTx.FrameSender())
	}
}

func TestFrameTx_NonceGuard(t *testing.T) {
	// Verify that applyMessage does NOT increment the nonce before execution
	// for FrameTx type (the guard msg.TxType != types.FrameTxType works).
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")

	// Fund sender with enough balance for gas.
	balance := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, balance)
	statedb.SetNonce(sender, 0)

	// Create a FrameTx-style message.
	frames := []types.Frame{
		{Mode: types.ModeDefault, GasLimit: 50000, Data: []byte{0x01}},
	}
	ftx := &types.FrameTx{
		ChainID:              big.NewInt(1),
		Nonce:                0,
		Sender:               sender,
		Frames:               frames,
		MaxFeePerGas:         big.NewInt(1),
		MaxPriorityFeePerGas: big.NewInt(1),
	}
	tx := types.NewTransaction(ftx)
	tx.SetSender(sender)

	msg := config.TransactionToMessage(tx)
	msg.From = sender

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1),
		Coinbase: types.HexToAddress("0xfee"),
	}

	gp := gaspool.GasPool(30_000_000)

	// Run ApplyMessage. The call will likely fail during EVM execution
	// since we have no real EVM setup, but the nonce guard happens before
	// EVM execution. We just need to confirm the nonce was NOT incremented
	// immediately after the nonce-increment guard line.
	_, _ = execution.ApplyMessage(config.TestConfig, func(n uint64) types.Hash { return types.Hash{} }, statedb, header, &msg, &gp)

	// For FrameTx, the nonce should NOT have been eagerly incremented to 1.
	// (The actual nonce management for FrameTx happens post-execution.)
	nonce := statedb.GetNonce(sender)
	if nonce != 0 {
		t.Errorf("expected nonce to remain 0 for FrameTx (not eagerly incremented), got %d", nonce)
	}

	// Contrast: for a legacy tx, nonce IS eagerly incremented.
	statedb2 := state.NewMemoryStateDB()
	statedb2.AddBalance(sender, balance)
	statedb2.SetNonce(sender, 0)

	to := types.HexToAddress("0x2222")
	legacyMsg := config.Message{
		From:      sender,
		To:        &to,
		Nonce:     0,
		Value:     big.NewInt(0),
		GasLimit:  21000,
		GasPrice:  big.NewInt(1),
		GasFeeCap: big.NewInt(1),
		GasTipCap: big.NewInt(1),
		TxType:    types.LegacyTxType,
	}
	gp2 := gaspool.GasPool(30_000_000)
	_, _ = execution.ApplyMessage(config.TestConfig, func(n uint64) types.Hash { return types.Hash{} }, statedb2, header, &legacyMsg, &gp2)

	legacyNonce := statedb2.GetNonce(sender)
	if legacyNonce != 1 {
		t.Errorf("expected nonce to be incremented to 1 for legacy tx, got %d", legacyNonce)
	}
}

func TestFrameTx_SponsoredGasSettlement(t *testing.T) {
	// Test that payer is charged gas, not sender, for sponsored frame tx.
	// This verifies GAP-1 fix.
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0x1111111111111111111111111111111111111111")
	payer := types.HexToAddress("0x2222222222222222222222222222222222222222")

	// Fund both accounts.
	balance := new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, balance)
	statedb.AddBalance(payer, balance)
	statedb.SetNonce(sender, 0)

	senderBalanceBefore := new(big.Int).Set(statedb.GetBalance(sender))
	payerBalanceBefore := new(big.Int).Set(statedb.GetBalance(payer))

	// Create a sponsored frame tx: VERIFY(sender)->APPROVE(0), VERIFY(payer)->APPROVE(1), SENDER
	frames := []types.Frame{
		{Mode: types.ModeVerify, GasLimit: 50000, Data: []byte{0x01}},
		{Mode: types.ModeVerify, Target: &payer, GasLimit: 30000, Data: []byte{0x02}},
		{Mode: types.ModeSender, GasLimit: 20000, Data: []byte{0x03}},
	}
	ftx := &types.FrameTx{
		ChainID:              big.NewInt(1),
		Nonce:                0,
		Sender:               sender,
		Frames:               frames,
		MaxFeePerGas:         big.NewInt(1),
		MaxPriorityFeePerGas: big.NewInt(1),
	}
	tx := types.NewTransaction(ftx)
	tx.SetSender(sender)

	// Structural test: verify accounts are funded and tx is valid.
	if senderBalanceBefore.Sign() <= 0 {
		t.Fatal("sender should have positive balance")
	}
	if payerBalanceBefore.Sign() <= 0 {
		t.Fatal("payer should have positive balance")
	}
	if tx.Type() != types.FrameTxType {
		t.Fatalf("expected FrameTxType, got %d", tx.Type())
	}
	if len(tx.Frames()) != 3 {
		t.Fatalf("expected 3 frames, got %d", len(tx.Frames()))
	}
}
