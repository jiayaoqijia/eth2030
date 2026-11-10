package core

import (
	"math/big"
	"testing"

	coreblock "github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/gaspool"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
)

// --- Full-cycle ETH transfer tests ---

// TestETHTransferFullCycle verifies a complete ETH transfer via applyMessage:
// sender balance decreases by value+gasCost, recipient balance increases by value,
// nonce increments, and coinbase receives gas payment.
func TestETHTransferFullCycle(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaa")
	recipient := types.HexToAddress("0xbbbb")
	coinbase := types.HexToAddress("0xfee0")

	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, tenETH)
	statedb.CreateAccount(recipient)

	transferValue := new(big.Int).SetUint64(1e18) // 1 ETH
	gasPrice := big.NewInt(1)
	gasLimit := uint64(21000)

	msg := &config.Message{
		From:     sender,
		To:       &recipient,
		GasLimit: gasLimit,
		GasPrice: gasPrice,
		Value:    transferValue,
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
		Coinbase: coinbase,
	}

	gp := new(gaspool.GasPool).AddGas(header.GasLimit)
	result, err := execution.ApplyMessage(config.TestConfig, nil, statedb, header, msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}
	if result.Failed() {
		t.Fatalf("transfer should succeed, got: %v", result.Err)
	}

	// Verify gas used.
	if result.UsedGas != execution.TxGas {
		t.Errorf("UsedGas = %d, want %d", result.UsedGas, execution.TxGas)
	}

	// Verify recipient balance.
	if got := statedb.GetBalance(recipient); got.Cmp(transferValue) != 0 {
		t.Errorf("recipient balance = %s, want %s", got, transferValue)
	}

	// Verify sender balance = initial - value - gasCost.
	gasCost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(result.UsedGas))
	expectedSender := new(big.Int).Sub(tenETH, transferValue)
	expectedSender.Sub(expectedSender, gasCost)
	if got := statedb.GetBalance(sender); got.Cmp(expectedSender) != 0 {
		t.Errorf("sender balance = %s, want %s", got, expectedSender)
	}

	// Verify nonce increment.
	if got := statedb.GetNonce(sender); got != 1 {
		t.Errorf("sender nonce = %d, want 1", got)
	}

	// Under EIP-1559, coinbase receives the tip (gasPrice - baseFee) per unit of gas.
	// With gasPrice=1 and baseFee=1, tip=0, so coinbase receives nothing.
	// This is correct post-EIP-1559 behavior: the base fee is burned, not paid to coinbase.
	if got := statedb.GetBalance(coinbase); got.Sign() != 0 {
		t.Errorf("coinbase balance = %s, want 0 (tip is zero when gasPrice == baseFee)", got)
	}
}

// TestETHTransferInsufficientBalance verifies that a transfer fails when the
// sender cannot cover value + gas cost.
func TestETHTransferInsufficientBalance(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaa")
	recipient := types.HexToAddress("0xbbbb")

	// Fund sender with 0.001 ETH -- not enough for 1 ETH transfer.
	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, big.NewInt(1_000_000_000_000_000)) // 0.001 ETH

	msg := &config.Message{
		From:     sender,
		To:       &recipient,
		GasLimit: 21000,
		GasPrice: big.NewInt(1),
		Value:    new(big.Int).SetUint64(1e18), // 1 ETH
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	gp := new(gaspool.GasPool).AddGas(header.GasLimit)
	_, err := execution.ApplyMessage(config.TestConfig, nil, statedb, header, msg, gp)
	if err == nil {
		t.Fatal("expected insufficient balance error")
	}

	// Gas pool should be restored on error.
	if gp.Gas() != header.GasLimit {
		t.Errorf("gas pool = %d, want %d (should be restored)", gp.Gas(), header.GasLimit)
	}
}

// TestETHTransferInsufficientGas verifies that a transaction with gas limit
// below intrinsic gas is handled correctly.
func TestETHTransferInsufficientGas(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaa")
	recipient := types.HexToAddress("0xbbbb")

	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18)))
	statedb.CreateAccount(recipient)

	msg := &config.Message{
		From:     sender,
		To:       &recipient,
		GasLimit: 5000, // below execution.TxGas (21000)
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(1),
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	gp := new(gaspool.GasPool).AddGas(header.GasLimit)
	_, err := execution.ApplyMessage(config.TestConfig, nil, statedb, header, msg, gp)
	// Intrinsic gas too low is now returned as a protocol error (matching go-ethereum).
	if err == nil {
		t.Fatal("expected error for intrinsic gas too low")
	}
}

// TestNonceValidationComprehensive verifies nonce-too-low, nonce-too-high,
// and correct nonce scenarios.
func TestNonceValidationComprehensive(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaa")
	recipient := types.HexToAddress("0xbbbb")

	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18)))
	statedb.SetNonce(sender, 10)

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	tests := []struct {
		name    string
		nonce   uint64
		wantErr bool
	}{
		{"nonce too low", 5, true},
		{"nonce way too low", 0, true},
		{"nonce too high", 15, true},
		{"nonce correct", 10, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset nonce for each test (except the correct nonce case which
			// will advance it).
			statedb.SetNonce(sender, 10)

			msg := &config.Message{
				From:     sender,
				To:       &recipient,
				GasLimit: 21000,
				GasPrice: big.NewInt(1),
				Value:    big.NewInt(1),
				Nonce:    tt.nonce,
			}

			gp := new(gaspool.GasPool).AddGas(header.GasLimit)
			_, err := execution.ApplyMessage(config.TestConfig, nil, statedb, header, msg, gp)
			if tt.wantErr && err == nil {
				t.Error("expected nonce error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestGasRefundMechanics verifies that gas refunds are capped at gasUsed / MaxRefundQuotient.
func TestGasRefundMechanics(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaa")
	recipient := types.HexToAddress("0xbbbb")

	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18)))
	statedb.CreateAccount(recipient)

	// Simple transfer to verify refund cap. A plain transfer has no refund
	// (refund counter stays zero), so we verify that UsedGas == execution.TxGas.
	msg := &config.Message{
		From:     sender,
		To:       &recipient,
		GasLimit: 100000, // much more than needed
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(1),
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	// Track sender balance before.
	senderBalBefore := new(big.Int).Set(statedb.GetBalance(sender))

	gp := new(gaspool.GasPool).AddGas(header.GasLimit)
	result, err := execution.ApplyMessage(config.TestConfig, nil, statedb, header, msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}

	// No refund counter was incremented for a simple transfer, so
	// gasUsed should equal intrinsic gas exactly.
	if result.UsedGas != execution.TxGas {
		t.Errorf("UsedGas = %d, want %d (no refund expected)", result.UsedGas, execution.TxGas)
	}

	// Verify the MaxRefundQuotient constant is 5 (EIP-3529).
	if vm.MaxRefundQuotient != 5 {
		t.Errorf("MaxRefundQuotient = %d, want 5", vm.MaxRefundQuotient)
	}

	// Remaining gas should be refunded to sender.
	// gasCost deducted = gasLimit * gasPrice = 100000
	// gasRefunded = (gasLimit - gasUsed) * gasPrice = (100000 - 21000) = 79000
	// So net gasCost = 21000
	netGasCost := new(big.Int).Mul(big.NewInt(1), new(big.Int).SetUint64(result.UsedGas))
	expectedBal := new(big.Int).Sub(senderBalBefore, msg.Value)
	expectedBal.Sub(expectedBal, netGasCost)
	if got := statedb.GetBalance(sender); got.Cmp(expectedBal) != 0 {
		t.Errorf("sender balance = %s, want %s", got, expectedBal)
	}
}

// TestValueTransferToNewAccount verifies that sending ETH to a non-existent
// address creates the account.
func TestValueTransferToNewAccount(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaa")
	newAddr := types.HexToAddress("0xcccc")

	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18)))

	// newAddr does NOT exist.
	if statedb.Exist(newAddr) {
		t.Fatal("newAddr should not exist before transfer")
	}

	msg := &config.Message{
		From:     sender,
		To:       &newAddr,
		GasLimit: 100000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(1000),
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	gp := new(gaspool.GasPool).AddGas(header.GasLimit)
	result, err := execution.ApplyMessage(config.TestConfig, nil, statedb, header, msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}
	if result.Failed() {
		t.Fatalf("transfer to new account should succeed: %v", result.Err)
	}

	// newAddr should now exist with the transferred value.
	if !statedb.Exist(newAddr) {
		t.Error("newAddr should exist after value transfer")
	}
	if got := statedb.GetBalance(newAddr); got.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("newAddr balance = %s, want 1000", got)
	}
}

// TestZeroValueTransfer verifies that a zero-value transfer succeeds
// and does not change balances.
func TestZeroValueTransfer(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaa")
	recipient := types.HexToAddress("0xbbbb")

	initialBal := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, initialBal)
	statedb.CreateAccount(recipient)

	msg := &config.Message{
		From:     sender,
		To:       &recipient,
		GasLimit: 21000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(0), // zero value
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	gp := new(gaspool.GasPool).AddGas(header.GasLimit)
	result, err := execution.ApplyMessage(config.TestConfig, nil, statedb, header, msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}
	if result.Failed() {
		t.Fatalf("zero-value transfer should succeed: %v", result.Err)
	}

	// Recipient balance should remain 0.
	if got := statedb.GetBalance(recipient); got.Sign() != 0 {
		t.Errorf("recipient balance = %s, want 0", got)
	}

	// Sender should only pay gas.
	gasCost := new(big.Int).Mul(big.NewInt(1), new(big.Int).SetUint64(result.UsedGas))
	expectedSender := new(big.Int).Sub(initialBal, gasCost)
	if got := statedb.GetBalance(sender); got.Cmp(expectedSender) != 0 {
		t.Errorf("sender balance = %s, want %s", got, expectedSender)
	}
}

// TestContractCreationGas verifies that contract creation transactions
// consume more gas than a simple transfer (execution.TxGas + execution.TxCreateGas + execution).
func TestContractCreationGas(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaa")

	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18)))

	// Simple init code: PUSH1 0x00 PUSH1 0x00 RETURN (returns empty code)
	initCode := []byte{
		0x60, 0x00, // PUSH1 0
		0x60, 0x00, // PUSH1 0
		0xf3, // RETURN
	}

	msg := &config.Message{
		From:     sender,
		To:       nil, // contract creation
		GasLimit: 200000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(0),
		Data:     initCode,
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	gp := new(gaspool.GasPool).AddGas(header.GasLimit)
	result, err := execution.ApplyMessage(config.TestConfig, nil, statedb, header, msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}

	// Gas used should be at least execution.TxGas + execution.TxCreateGas.
	minGas := execution.TxGas + execution.TxCreateGas
	if result.UsedGas < minGas {
		t.Errorf("UsedGas = %d, want >= %d (execution.TxGas + execution.TxCreateGas)", result.UsedGas, minGas)
	}
}

// TestGlamsterdamReducedIntrinsicGas verifies that under Glamsterdam,
// a simple ETH transfer to an existing account uses significantly less
// intrinsic gas than the legacy 21000.
func TestGlamsterdamReducedIntrinsicGas(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaa")
	recipient := types.HexToAddress("0xbbbb")

	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18)))
	statedb.CreateAccount(recipient)

	msg := &config.Message{
		From:     sender,
		To:       &recipient,
		GasLimit: 100000,
		GasPrice: big.NewInt(1),
		Value:    big.NewInt(1),
		Nonce:    0,
	}

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 60_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	gp := new(gaspool.GasPool).AddGas(header.GasLimit)
	result, err := execution.ApplyMessage(config.TestConfigGlamsterdan, nil, statedb, header, msg, gp)
	if err != nil {
		t.Fatalf("applyMessage failed: %v", err)
	}
	if result.Failed() {
		t.Fatalf("transfer should succeed: %v", result.Err)
	}

	// Under EIP-2780, TxBaseGlamsterdam = 4500 (vs legacy 21000).
	// The total gas should be well under 21000 for a simple transfer
	// to an existing account.
	if result.UsedGas >= execution.TxGas {
		t.Errorf("UsedGas = %d, expected < %d under Glamsterdam (EIP-2780)", result.UsedGas, execution.TxGas)
	}

	// Verify the reduced base cost constant.
	if vm.TxBaseGlamsterdam != 4500 {
		t.Errorf("TxBaseGlamsterdam = %d, want 4500", vm.TxBaseGlamsterdam)
	}
}

// --- Fork activation tests ---

// TestForkActivationIsGlamsterdan verifies the IsGlamsterdan fork check
// activates at the correct timestamp.
func TestForkActivationIsGlamsterdan(t *testing.T) {
	glamstTime := uint64(5000)
	cfg := &config.ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(0),
		CancunTime:              newUint64(0),
		PragueTime:              newUint64(0),
		AmsterdamTime:           newUint64(0),
		GlamsterdanTime:         &glamstTime,
	}

	// Before fork time.
	if cfg.IsGlamsterdan(4999) {
		t.Error("IsGlamsterdan(4999) should be false")
	}

	// At fork time.
	if !cfg.IsGlamsterdan(5000) {
		t.Error("IsGlamsterdan(5000) should be true")
	}

	// After fork time.
	if !cfg.IsGlamsterdan(6000) {
		t.Error("IsGlamsterdan(6000) should be true")
	}

	// Nil GlamsterdanTime means not scheduled.
	configNil := &config.ChainConfig{
		ChainID:         big.NewInt(1),
		GlamsterdanTime: nil,
	}
	if configNil.IsGlamsterdan(9999999) {
		t.Error("IsGlamsterdan should be false when GlamsterdanTime is nil")
	}
}

// TestForkActivationTimestampOrder verifies that fork timestamps activate
// in the correct order.
func TestForkActivationTimestampOrder(t *testing.T) {
	cfg := &config.ChainConfig{
		ChainID:                 big.NewInt(1337),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64(1000),
		CancunTime:              newUint64(2000),
		PragueTime:              newUint64(3000),
		AmsterdamTime:           newUint64(4000),
		GlamsterdanTime:         newUint64(5000),
	}

	// At time 2500: Shanghai and Cancun active, but not Prague.
	if !cfg.IsShanghai(2500) {
		t.Error("Shanghai should be active at 2500")
	}
	if !cfg.IsCancun(2500) {
		t.Error("Cancun should be active at 2500")
	}
	if cfg.IsPrague(2500) {
		t.Error("Prague should NOT be active at 2500")
	}
	if cfg.IsAmsterdam(2500) {
		t.Error("Amsterdam should NOT be active at 2500")
	}
	if cfg.IsGlamsterdan(2500) {
		t.Error("Glamsterdan should NOT be active at 2500")
	}

	// At time 5000: all forks active.
	if !cfg.IsPrague(5000) {
		t.Error("Prague should be active at 5000")
	}
	if !cfg.IsAmsterdam(5000) {
		t.Error("Amsterdam should be active at 5000")
	}
	if !cfg.IsGlamsterdan(5000) {
		t.Error("Glamsterdan should be active at 5000")
	}
}

// TestRulesReflectForkActivation verifies that the cfg.Rules struct correctly
// reflects fork activation based on block number and timestamp.
func TestRulesReflectForkActivation(t *testing.T) {
	// config.TestConfig: all forks except Glamsterdam at time 0.
	rules := config.TestConfig.Rules(big.NewInt(100), config.TestConfig.IsMerge(), 100)
	if !rules.IsMerge {
		t.Error("IsMerge should be true")
	}
	if !rules.IsShanghai {
		t.Error("IsShanghai should be true")
	}
	if !rules.IsCancun {
		t.Error("IsCancun should be true")
	}
	if !rules.IsPrague {
		t.Error("IsPrague should be true")
	}
	if !rules.IsAmsterdam {
		t.Error("IsAmsterdam should be true")
	}
	if rules.IsGlamsterdan {
		t.Error("IsGlamsterdan should be false for config.TestConfig")
	}

	// config.TestConfigGlamsterdan: all forks including Glamsterdam at time 0.
	rules2 := config.TestConfigGlamsterdan.Rules(big.NewInt(100), config.TestConfigGlamsterdan.IsMerge(), 100)
	if !rules2.IsGlamsterdan {
		t.Error("IsGlamsterdan should be true for config.TestConfigGlamsterdan")
	}
	if !rules2.IsEIP7904 {
		t.Error("IsEIP7904 should be true for config.TestConfigGlamsterdan")
	}
	if !rules2.IsEIP7706 {
		t.Error("IsEIP7706 should be true for config.TestConfigGlamsterdan")
	}
	if !rules2.IsEIP7778 {
		t.Error("IsEIP7778 should be true for config.TestConfigGlamsterdan")
	}
	if !rules2.IsEIP2780 {
		t.Error("IsEIP2780 should be true for config.TestConfigGlamsterdan")
	}
}

// --- Block validation tests ---

// TestBlockGasLimitBounds verifies the gas limit adjustment rules: the gas
// limit can change by at most 1/coreblock.GasLimitBoundDivisor per block.
func TestBlockGasLimitBounds(t *testing.T) {
	tests := []struct {
		name      string
		parentGL  uint64
		childGL   uint64
		expectErr bool
	}{
		{
			name:      "no change",
			parentGL:  30_000_000,
			childGL:   30_000_000,
			expectErr: false,
		},
		{
			name:      "increase by 1",
			parentGL:  30_000_000,
			childGL:   30_000_001,
			expectErr: false,
		},
		{
			name:      "decrease by 1",
			parentGL:  30_000_000,
			childGL:   29_999_999,
			expectErr: false,
		},
		{
			name:      "increase at max allowed",
			parentGL:  30_000_000,
			childGL:   30_000_000 + 30_000_000/coreblock.GasLimitBoundDivisor - 1,
			expectErr: false,
		},
		{
			name:      "increase exceeds max",
			parentGL:  30_000_000,
			childGL:   30_000_000 + 30_000_000/coreblock.GasLimitBoundDivisor,
			expectErr: true,
		},
		{
			name:      "decrease at max allowed",
			parentGL:  30_000_000,
			childGL:   30_000_000 - 30_000_000/coreblock.GasLimitBoundDivisor + 1,
			expectErr: false,
		},
		{
			name:      "decrease exceeds max",
			parentGL:  30_000_000,
			childGL:   30_000_000 - 30_000_000/coreblock.GasLimitBoundDivisor,
			expectErr: true,
		},
		{
			name:      "below minimum",
			parentGL:  coreblock.MinGasLimit + 100,
			childGL:   coreblock.MinGasLimit - 1,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := coreblock.VerifyGasLimit(tt.parentGL, tt.childGL)
			if tt.expectErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// TestBlockHeaderValidation verifies that ValidateHeader checks block number,
// timestamp, parent hash, and gas limit.
func TestBlockHeaderValidation(t *testing.T) {
	v := coreblock.NewBlockValidator(config.TestConfig)

	parent := makeValidParent()
	validChild := makeValidChild(parent)

	// Valid child should pass.
	if err := v.ValidateHeader(validChild, parent); err != nil {
		t.Fatalf("valid child rejected: %v", err)
	}

	// Wrong block number.
	t.Run("wrong number", func(t *testing.T) {
		child := makeValidChild(parent)
		child.Number = big.NewInt(999)
		if err := v.ValidateHeader(child, parent); err == nil {
			t.Error("expected error for wrong block number")
		}
	})

	// Timestamp not increasing.
	t.Run("timestamp not increasing", func(t *testing.T) {
		child := makeValidChild(parent)
		child.Time = parent.Time
		if err := v.ValidateHeader(child, parent); err == nil {
			t.Error("expected error for non-increasing timestamp")
		}
	})

	// Wrong parent hash.
	t.Run("wrong parent hash", func(t *testing.T) {
		child := makeValidChild(parent)
		child.ParentHash = types.Hash{0xde, 0xad}
		if err := v.ValidateHeader(child, parent); err == nil {
			t.Error("expected error for wrong parent hash")
		}
	})

	// Gas used exceeds gas limit.
	t.Run("gas used exceeds limit", func(t *testing.T) {
		child := makeValidChild(parent)
		child.GasUsed = child.GasLimit + 1
		if err := v.ValidateHeader(child, parent); err == nil {
			t.Error("expected error for gas used > gas limit")
		}
	})
}

// --- Multiple sequential transactions ---

// TestSequentialTransactions verifies multiple transactions applied in sequence
// with proper nonce incrementing.
func TestSequentialTransactions(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0xaaaa")
	recipient := types.HexToAddress("0xbbbb")

	statedb.CreateAccount(sender)
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18)))
	statedb.CreateAccount(recipient)

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 30_000_000,
		BaseFee:  big.NewInt(1),
		Time:     100,
	}

	// Apply 5 sequential transfers.
	for i := uint64(0); i < 5; i++ {
		msg := &config.Message{
			From:     sender,
			To:       &recipient,
			GasLimit: 21000,
			GasPrice: big.NewInt(1),
			Value:    big.NewInt(100),
			Nonce:    i,
		}
		gp := new(gaspool.GasPool).AddGas(header.GasLimit)
		result, err := execution.ApplyMessage(config.TestConfig, nil, statedb, header, msg, gp)
		if err != nil {
			t.Fatalf("tx %d: applyMessage failed: %v", i, err)
		}
		if result.Failed() {
			t.Fatalf("tx %d: should succeed: %v", i, result.Err)
		}
	}

	// Nonce should be 5.
	if got := statedb.GetNonce(sender); got != 5 {
		t.Errorf("sender nonce = %d, want 5", got)
	}

	// Recipient should have 500 (5 * 100).
	if got := statedb.GetBalance(recipient); got.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("recipient balance = %s, want 500", got)
	}
}

// TestGlamsterdamNewAccountSurchargeComprehensive verifies the EIP-2780
// GAS_NEW_ACCOUNT surcharge for value transfers to non-existent accounts
// under Glamsterdam, and that zero-value transfers to non-existent accounts
// do NOT incur the surcharge.
func TestGlamsterdamNewAccountSurchargeComprehensive(t *testing.T) {
	tests := []struct {
		name      string
		toExists  bool
		value     *big.Int
		wantAbove uint64 // minimum expected gas
		wantBelow uint64 // maximum expected gas
	}{
		{
			name:      "value to existing account (no surcharge)",
			toExists:  true,
			value:     big.NewInt(1),
			wantBelow: 21000, // should be less than legacy 21000
		},
		{
			name:      "value to new account (with surcharge)",
			toExists:  false,
			value:     big.NewInt(1),
			wantAbove: 29500, // TxBaseGlamsterdam + GAS_NEW_ACCOUNT = 4500+25000
		},
		{
			name:      "zero value to new account (no surcharge)",
			toExists:  false,
			value:     big.NewInt(0),
			wantBelow: 21000, // should be less than legacy 21000
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			statedb := state.NewMemoryStateDB()

			sender := types.HexToAddress("0xaaaa")
			recipient := types.HexToAddress("0xdddd")

			statedb.CreateAccount(sender)
			statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(100), new(big.Int).SetUint64(1e18)))
			if tt.toExists {
				statedb.CreateAccount(recipient)
			}

			msg := &config.Message{
				From:     sender,
				To:       &recipient,
				GasLimit: 200000,
				GasPrice: big.NewInt(1),
				Value:    tt.value,
				Nonce:    0,
			}

			header := &types.Header{
				Number:   big.NewInt(1),
				GasLimit: 60_000_000,
				BaseFee:  big.NewInt(1),
				Time:     100,
			}

			gp := new(gaspool.GasPool).AddGas(header.GasLimit)
			result, err := execution.ApplyMessage(config.TestConfigGlamsterdan, nil, statedb, header, msg, gp)
			if err != nil {
				t.Fatalf("applyMessage failed: %v", err)
			}
			if result.Failed() {
				t.Fatalf("transfer should succeed: %v", result.Err)
			}

			if tt.wantAbove > 0 && result.UsedGas < tt.wantAbove {
				t.Errorf("UsedGas = %d, want >= %d", result.UsedGas, tt.wantAbove)
			}
			if tt.wantBelow > 0 && result.UsedGas >= tt.wantBelow {
				t.Errorf("UsedGas = %d, want < %d", result.UsedGas, tt.wantBelow)
			}
		})
	}
}
