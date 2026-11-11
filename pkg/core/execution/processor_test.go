package execution

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/gaspool"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/pqc"
)

// newTransferTx creates a simple transfer transaction.
func newTransferTx(nonce uint64, to types.Address, value *big.Int, gasLimit uint64, gasPrice *big.Int) *types.Transaction {
	toAddr := to
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       &toAddr,
		Value:    value,
	})
}

func newTestHeader() *types.Header {
	return &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 10_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1), // Low base fee so GasPrice=1 txs pass EIP-1559 validation
		Coinbase: types.HexToAddress("0xfee"),
	}
}

func TestProcessEmptyBlock(t *testing.T) {
	proc := NewStateProcessor(config.TestConfig)
	statedb := state.NewMemoryStateDB()
	header := newTestHeader()

	block := types.NewBlock(header, &types.Body{})
	receipts, err := proc.Process(block, statedb)
	if err != nil {
		t.Fatalf("unexpected error processing empty block: %v", err)
	}
	if len(receipts) != 0 {
		t.Fatalf("expected 0 receipts, got %d", len(receipts))
	}
}

func TestSimpleTransfer(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111")
	recipient := types.HexToAddress("0x2222")

	// Fund sender: 10 ETH
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	oneETH := new(big.Int).SetUint64(1e18)
	gasPrice := big.NewInt(1) // 1 wei gas price for simplicity
	gasLimit := uint64(21000)

	tx := newTransferTx(0, recipient, oneETH, gasLimit, gasPrice)
	msg := config.TransactionToMessage(tx)
	msg.From = sender

	header := newTestHeader()
	gp := new(gaspool.GasPool).AddGas(header.GasLimit)

	snapshot := statedb.Snapshot()
	result, err := ApplyMessage(config.TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		statedb.RevertToSnapshot(snapshot)
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Failed() {
		t.Fatalf("transfer should not fail: %v", result.Err)
	}
	if result.UsedGas != TxGas {
		t.Fatalf("expected gas used %d, got %d", TxGas, result.UsedGas)
	}

	// Recipient should have 1 ETH
	recipientBal := statedb.GetBalance(recipient)
	if recipientBal.Cmp(oneETH) != 0 {
		t.Fatalf("recipient balance: want %v, got %v", oneETH, recipientBal)
	}

	// Sender should have 10 ETH - 1 ETH - gasCost
	gasCost := new(big.Int).Mul(gasPrice, new(big.Int).SetUint64(TxGas))
	expectedSender := new(big.Int).Sub(tenETH, oneETH)
	expectedSender.Sub(expectedSender, gasCost)
	senderBal := statedb.GetBalance(sender)
	if senderBal.Cmp(expectedSender) != 0 {
		t.Fatalf("sender balance: want %v, got %v", expectedSender, senderBal)
	}

	// Nonce should be incremented
	if statedb.GetNonce(sender) != 1 {
		t.Fatalf("sender nonce: want 1, got %d", statedb.GetNonce(sender))
	}
}

func TestInsufficientBalance(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111")
	recipient := types.HexToAddress("0x2222")

	// Fund sender with only 0.5 ETH
	halfETH := new(big.Int).Div(new(big.Int).SetUint64(1e18), big.NewInt(2))
	statedb.AddBalance(sender, halfETH)

	oneETH := new(big.Int).SetUint64(1e18)
	gasPrice := big.NewInt(1)
	gasLimit := uint64(21000)

	tx := newTransferTx(0, recipient, oneETH, gasLimit, gasPrice)
	msg := config.TransactionToMessage(tx)
	msg.From = sender

	header := newTestHeader()
	gp := new(gaspool.GasPool).AddGas(header.GasLimit)

	_, err := ApplyMessage(config.TestConfig, nil, statedb, header, &msg, gp)
	if err == nil {
		t.Fatal("expected insufficient balance error")
	}

	// Gas should be returned to pool on error
	if gp.Gas() != header.GasLimit {
		t.Fatalf("gas pool should be restored, got %d", gp.Gas())
	}
}

func TestGasPoolExhaustion(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111")
	recipient := types.HexToAddress("0x2222")

	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	gasPrice := big.NewInt(1)
	gasLimit := uint64(21000)

	tx := newTransferTx(0, recipient, big.NewInt(100), gasLimit, gasPrice)
	msg := config.TransactionToMessage(tx)
	msg.From = sender

	header := newTestHeader()

	// Gas pool with only 10000 gas (less than 21000 required)
	gp := new(gaspool.GasPool).AddGas(10000)

	_, err := ApplyMessage(config.TestConfig, nil, statedb, header, &msg, gp)
	if err == nil {
		t.Fatal("expected gas pool exhaustion error")
	}
	if err != gaspool.ErrGasPoolExhausted {
		t.Fatalf("expected gaspool.ErrGasPoolExhausted, got: %v", err)
	}
}

func TestNonceValidation(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111")
	recipient := types.HexToAddress("0x2222")

	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)
	statedb.SetNonce(sender, 5) // state nonce = 5

	gasPrice := big.NewInt(1)
	gasLimit := uint64(21000)

	// Test nonce too low
	tx := newTransferTx(3, recipient, big.NewInt(100), gasLimit, gasPrice)
	msg := config.TransactionToMessage(tx)
	msg.From = sender

	header := newTestHeader()
	gp := new(gaspool.GasPool).AddGas(header.GasLimit)

	_, err := ApplyMessage(config.TestConfig, nil, statedb, header, &msg, gp)
	if err == nil {
		t.Fatal("expected nonce too low error")
	}

	// Test nonce too high
	tx = newTransferTx(10, recipient, big.NewInt(100), gasLimit, gasPrice)
	msg = config.TransactionToMessage(tx)
	msg.From = sender

	gp = new(gaspool.GasPool).AddGas(header.GasLimit)
	_, err = ApplyMessage(config.TestConfig, nil, statedb, header, &msg, gp)
	if err == nil {
		t.Fatal("expected nonce too high error")
	}

	// Test correct nonce succeeds
	tx = newTransferTx(5, recipient, big.NewInt(100), gasLimit, gasPrice)
	msg = config.TransactionToMessage(tx)
	msg.From = sender

	gp = new(gaspool.GasPool).AddGas(header.GasLimit)
	result, err := ApplyMessage(config.TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		t.Fatalf("expected correct nonce to succeed, got: %v", err)
	}
	if result.Failed() {
		t.Fatalf("transfer with correct nonce should not fail: %v", result.Err)
	}
	if statedb.GetNonce(sender) != 6 {
		t.Fatalf("nonce should be incremented to 6, got %d", statedb.GetNonce(sender))
	}
}

func TestChainConfig(t *testing.T) {
	// config.TestConfig has all forks at time 0
	if !config.TestConfig.IsShanghai(0) {
		t.Fatal("config.TestConfig should have Shanghai at time 0")
	}
	if !config.TestConfig.IsCancun(0) {
		t.Fatal("config.TestConfig should have Cancun at time 0")
	}
	if !config.TestConfig.IsPrague(0) {
		t.Fatal("config.TestConfig should have Prague at time 0")
	}
	if !config.TestConfig.IsAmsterdam(0) {
		t.Fatal("config.TestConfig should have Amsterdam at time 0")
	}

	// config.MainnetConfig: Shanghai and Cancun are active at a high time
	if !config.MainnetConfig.IsShanghai(2_000_000_000) {
		t.Fatal("config.MainnetConfig should have Shanghai active at time 2B")
	}
	if !config.MainnetConfig.IsCancun(2_000_000_000) {
		t.Fatal("config.MainnetConfig should have Cancun active at time 2B")
	}

	const pragueTime = 1_746_612_311
	if config.MainnetConfig.IsPrague(pragueTime-1) || !config.MainnetConfig.IsPrague(pragueTime) {
		t.Fatal("Mainnet Prague activation does not match Geth")
	}
	if config.MainnetConfig.IsAmsterdam(2_000_000_000) {
		t.Fatal("config.MainnetConfig should NOT have Amsterdam active")
	}

	// Fork boundary test
	ts := uint64(1681338455) // exact Shanghai time
	if !config.MainnetConfig.IsShanghai(ts) {
		t.Fatal("config.MainnetConfig should have Shanghai active at exact fork time")
	}
	if config.MainnetConfig.IsShanghai(ts - 1) {
		t.Fatal("config.MainnetConfig should NOT have Shanghai active before fork time")
	}
}

func TestGasPool(t *testing.T) {
	gp := new(gaspool.GasPool).AddGas(100)
	if gp.Gas() != 100 {
		t.Fatalf("expected 100, got %d", gp.Gas())
	}

	if err := gp.SubGas(30); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gp.Gas() != 70 {
		t.Fatalf("expected 70, got %d", gp.Gas())
	}

	if err := gp.SubGas(80); err == nil {
		t.Fatal("expected error for insufficient gas")
	}
	// Gas should not change on failed sub
	if gp.Gas() != 70 {
		t.Fatalf("expected 70 after failed sub, got %d", gp.Gas())
	}

	gp.AddGas(30)
	if gp.Gas() != 100 {
		t.Fatalf("expected 100, got %d", gp.Gas())
	}
}

func TestExecutionResult(t *testing.T) {
	// Successful result
	r := &ExecutionResult{UsedGas: 21000, Err: nil, ReturnData: []byte{0x01}}
	if r.Failed() {
		t.Fatal("should not be failed")
	}
	if r.Unwrap() != nil {
		t.Fatal("should have nil error")
	}
	if len(r.Return()) != 1 {
		t.Fatal("should return data")
	}
	if r.Revert() != nil {
		t.Fatal("revert should be nil for success")
	}

	// Failed result
	r = &ExecutionResult{UsedGas: 21000, Err: ErrGasLimitExceeded, ReturnData: []byte{0x08}}
	if !r.Failed() {
		t.Fatal("should be failed")
	}
	if r.Unwrap() == nil {
		t.Fatal("should have error")
	}
	if r.Return() != nil {
		t.Fatal("return should be nil for failure")
	}
	if len(r.Revert()) != 1 {
		t.Fatal("revert should return data for failure")
	}
}

func TestTransactionToMessage(t *testing.T) {
	to := types.HexToAddress("0xdead")
	inner := &types.LegacyTx{
		Nonce:    5,
		GasPrice: big.NewInt(20_000_000_000),
		Gas:      21000,
		To:       &to,
		Value:    big.NewInt(1e18),
		Data:     []byte{0xab, 0xcd},
	}
	tx := types.NewTransaction(inner)
	msg := config.TransactionToMessage(tx)

	if msg.Nonce != 5 {
		t.Fatalf("nonce mismatch: %d", msg.Nonce)
	}
	if msg.GasLimit != 21000 {
		t.Fatalf("gas limit mismatch: %d", msg.GasLimit)
	}
	if msg.To == nil || *msg.To != to {
		t.Fatal("to address mismatch")
	}
	if msg.Value.Cmp(big.NewInt(1e18)) != 0 {
		t.Fatal("value mismatch")
	}
	if len(msg.Data) != 2 {
		t.Fatal("data mismatch")
	}

	// Contract creation (nil To)
	inner2 := &types.LegacyTx{
		Nonce:    0,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       nil,
		Value:    big.NewInt(0),
	}
	tx2 := types.NewTransaction(inner2)
	msg2 := config.TransactionToMessage(tx2)
	if msg2.To != nil {
		t.Fatal("contract creation should have nil To")
	}
}

func TestApplyMessage_AATxUsesAASpecificIntrinsicGas(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111")
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18)))

	tx := types.NewTransaction(&types.AATx{
		ChainID:              big.NewInt(1),
		Nonce:                0,
		Sender:               sender,
		SenderValidationGas:  50_000,
		SenderExecutionGas:   21_000,
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(2),
		SenderValidationData: []byte{},
		SenderExecutionData:  []byte{},
	})
	tx.SetSender(sender)

	msg := config.TransactionToMessage(tx)
	header := newTestHeader()
	gp := new(gaspool.GasPool).AddGas(header.GasLimit)

	if _, err := ApplyMessage(config.TestConfig, nil, statedb, header, &msg, gp); err != nil {
		t.Fatalf("AA tx should pass intrinsic gas validation, got: %v", err)
	}
}

func TestApplyTransactionInternal_RecoversAATxSenderFromEmbeddedField(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111")
	statedb.AddBalance(sender, new(big.Int).Mul(big.NewInt(10), big.NewInt(1e18)))

	tx := types.NewTransaction(&types.AATx{
		ChainID:              big.NewInt(1),
		Nonce:                0,
		Sender:               sender,
		SenderValidationGas:  50_000,
		SenderExecutionGas:   21_000,
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(2),
		SenderValidationData: []byte{},
		SenderExecutionData:  []byte{},
	})

	if tx.Sender() != nil {
		t.Fatal("test requires AA tx sender cache to start empty")
	}

	header := newTestHeader()
	gp := new(gaspool.GasPool).AddGas(header.GasLimit)

	if _, _, err := applyTransactionInternal(config.TestConfig, nil, statedb, header, tx, gp, nil, nil); err != nil {
		t.Fatalf("AA tx should recover sender from embedded field, got: %v", err)
	}
	if recovered := tx.Sender(); recovered == nil || *recovered != sender {
		t.Fatalf("cached sender: want %s, got %v", sender.Hex(), recovered)
	}
}

func TestContractCreation(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111")

	// Fund sender
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	gasPrice := big.NewInt(1)
	gasLimit := uint64(100000)

	// Contract creation tx: To is nil, Data is init code.
	// Simple init code: PUSH1 0x42, PUSH1 0x00, MSTORE, PUSH1 0x01, PUSH1 0x1f, RETURN
	// Stores 0x42 in memory and returns 1 byte (the deployed "code" is [0x42]).
	initCode := []byte{
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE (stores 0x42 at memory[0:32])
		0x60, 0x01, // PUSH1 0x01 (return size = 1 byte)
		0x60, 0x1f, // PUSH1 0x1f (return offset = 31, to get last byte of the word)
		0xf3, // RETURN
	}
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       nil, // contract creation
		Value:    big.NewInt(0),
		Data:     initCode,
	})
	msg := config.TransactionToMessage(tx)
	msg.From = sender

	header := newTestHeader()
	gp := new(gaspool.GasPool).AddGas(header.GasLimit)

	result, err := ApplyMessage(config.TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		t.Fatalf("ApplyMessage should not return protocol error for contract creation, got: %v", err)
	}
	if result.Failed() {
		t.Fatalf("contract creation should succeed, got error: %v", result.Err)
	}

	// Nonce should be incremented
	if statedb.GetNonce(sender) != 1 {
		t.Fatalf("nonce should be incremented, got %d", statedb.GetNonce(sender))
	}

	// Gas should be consumed (more than base TxGas due to create overhead + execution)
	if result.UsedGas <= TxGas {
		t.Fatalf("contract creation should use more gas than simple transfer, got %d", result.UsedGas)
	}
}

func TestContractCall(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111")
	contractAddr := types.HexToAddress("0x3333")

	// Fund sender
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	// Deploy contract code: PUSH1 0x42, PUSH1 0x00, MSTORE, PUSH1 0x20, PUSH1 0x00, RETURN
	// Returns 32 bytes with value 0x42
	contractCode := []byte{
		0x60, 0x42, // PUSH1 0x42
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 0x20 (32 bytes)
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	}
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, contractCode)

	gasPrice := big.NewInt(1)
	gasLimit := uint64(100000)

	// Call the contract
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		GasPrice: gasPrice,
		Gas:      gasLimit,
		To:       &contractAddr,
		Value:    big.NewInt(0),
		Data:     []byte{}, // no calldata needed
	})
	msg := config.TransactionToMessage(tx)
	msg.From = sender

	header := newTestHeader()
	gp := new(gaspool.GasPool).AddGas(header.GasLimit)

	result, err := ApplyMessage(config.TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		t.Fatalf("ApplyMessage failed: %v", err)
	}
	if result.Failed() {
		t.Fatalf("contract call should succeed, got error: %v", result.Err)
	}

	// Return data should be 32 bytes with value 0x42
	if len(result.ReturnData) != 32 {
		t.Fatalf("return data length = %d, want 32", len(result.ReturnData))
	}
	if result.ReturnData[31] != 0x42 {
		t.Fatalf("return data[31] = 0x%02x, want 0x42", result.ReturnData[31])
	}
}

func newSloadContractState() (*state.MemoryStateDB, types.Address, types.Address) {
	statedb := state.NewMemoryStateDB()
	sender := types.HexToAddress("0x1111")
	contractAddr := types.HexToAddress("0x3333")

	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)
	statedb.CreateAccount(contractAddr)
	statedb.SetCode(contractAddr, []byte{
		0x60, 0x00, // PUSH1 0x00
		0x54,       // SLOAD
		0x60, 0x00, // PUSH1 0x00
		0x52,       // MSTORE
		0x60, 0x20, // PUSH1 0x20
		0x60, 0x00, // PUSH1 0x00
		0xf3, // RETURN
	})

	slot0 := types.Hash{}
	value := types.Hash{}
	value[31] = 0x42
	statedb.SetState(contractAddr, slot0, value)
	statedb.FinalizePreState()

	return statedb, sender, contractAddr
}

func newContractCallTx(nonce uint64, to types.Address) *types.Transaction {
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1),
		Gas:      100000,
		To:       &to,
		Value:    big.NewInt(0),
		Data:     nil,
	})
	return tx
}

func TestApplyTransactionResetsAccessListPerTransaction(t *testing.T) {
	statedb, sender, contractAddr := newSloadContractState()
	header := newTestHeader()

	tx1 := newContractCallTx(0, contractAddr)
	tx1.SetSender(sender)
	receipt1, _, err := applyTransactionInternal(config.TestConfig, nil, statedb, header, tx1, new(gaspool.GasPool).AddGas(header.GasLimit), nil, nil)
	if err != nil {
		t.Fatalf("first applyTransactionInternal: %v", err)
	}

	tx2 := newContractCallTx(1, contractAddr)
	tx2.SetSender(sender)
	receipt2, _, err := applyTransactionInternal(config.TestConfig, nil, statedb, header, tx2, new(gaspool.GasPool).AddGas(header.GasLimit), nil, nil)
	if err != nil {
		t.Fatalf("second applyTransactionInternal: %v", err)
	}

	if receipt1.GasUsed != receipt2.GasUsed {
		t.Fatalf("gas used mismatch across transactions: first=%d second=%d", receipt1.GasUsed, receipt2.GasUsed)
	}
}

func TestApplyTransactionClearsPreexistingWarmAccesses(t *testing.T) {
	freshState, sender, contractAddr := newSloadContractState()
	header := newTestHeader()

	freshTx := newContractCallTx(0, contractAddr)
	freshTx.SetSender(sender)
	freshReceipt, _, err := applyTransactionInternal(config.TestConfig, nil, freshState, header, freshTx, new(gaspool.GasPool).AddGas(header.GasLimit), nil, nil)
	if err != nil {
		t.Fatalf("fresh applyTransactionInternal: %v", err)
	}

	pollutedState, sender, contractAddr := newSloadContractState()
	slot0 := types.Hash{}
	pollutedState.AddAddressToAccessList(contractAddr)
	pollutedState.AddSlotToAccessList(contractAddr, slot0)

	pollutedTx := newContractCallTx(0, contractAddr)
	pollutedTx.SetSender(sender)
	pollutedReceipt, _, err := applyTransactionInternal(config.TestConfig, nil, pollutedState, header, pollutedTx, new(gaspool.GasPool).AddGas(header.GasLimit), nil, nil)
	if err != nil {
		t.Fatalf("polluted applyTransactionInternal: %v", err)
	}

	if freshReceipt.GasUsed != pollutedReceipt.GasUsed {
		t.Fatalf("gas used should not depend on preexisting warm accesses: fresh=%d polluted=%d", freshReceipt.GasUsed, pollutedReceipt.GasUsed)
	}
}

func TestProcessBlockWithTransfer(t *testing.T) {
	statedb := state.NewMemoryStateDB()

	sender := types.HexToAddress("0x1111")
	recipient := types.HexToAddress("0x2222")

	// Fund sender: 10 ETH
	tenETH := new(big.Int).Mul(big.NewInt(10), new(big.Int).SetUint64(1e18))
	statedb.AddBalance(sender, tenETH)

	oneETH := new(big.Int).SetUint64(1e18)
	gasPrice := big.NewInt(1)
	gasLimit := uint64(21000)

	tx := newTransferTx(0, recipient, oneETH, gasLimit, gasPrice)

	header := newTestHeader()
	gp := new(gaspool.GasPool).AddGas(header.GasLimit)

	msg := config.TransactionToMessage(tx)
	msg.From = sender

	snapshot := statedb.Snapshot()
	result, err := ApplyMessage(config.TestConfig, nil, statedb, header, &msg, gp)
	if err != nil {
		statedb.RevertToSnapshot(snapshot)
		t.Fatalf("unexpected error: %v", err)
	}

	// Create receipt
	var receiptStatus uint64
	if result.Failed() {
		receiptStatus = types.ReceiptStatusFailed
	} else {
		receiptStatus = types.ReceiptStatusSuccessful
	}

	receipt := types.NewReceipt(receiptStatus, result.UsedGas)
	if receipt.Status != types.ReceiptStatusSuccessful {
		t.Fatal("receipt should be successful")
	}
	if receipt.CumulativeGasUsed != TxGas {
		t.Fatalf("receipt cumulative gas: want %d, got %d", TxGas, receipt.CumulativeGasUsed)
	}
}

func TestApplyTransactionAuthenticatesPQSender(t *testing.T) {
	cfg := *config.MainnetConfig
	fork := uint64(0)
	cfg.IPlusTime = &fork
	cfg.TerminalTotalDifficulty = big.NewInt(0)
	header := &types.Header{
		Number: big.NewInt(24_000_000), Time: 1_800_000_000,
		GasLimit: 30_000_000, Difficulty: new(big.Int), BaseFee: big.NewInt(1),
	}
	for _, scheme := range []pqc.PQSignatureType{pqc.SigDilithium3, pqc.SigFalcon512, pqc.SigSPHINCS128} {
		t.Run(fmt.Sprint(scheme), func(t *testing.T) {
			signer, err := pqc.NewPQTxSigner(scheme)
			if err != nil {
				t.Fatal(err)
			}
			sk, pk, err := signer.GenerateKey()
			if err != nil {
				t.Fatal(err)
			}
			to := types.Address{0x42}
			pq := types.NewPQTransaction(cfg.ChainID, 0, &to, big.NewInt(100), 300_000, big.NewInt(10), nil)
			from := types.PQPubKeyToAddress(pk.Raw)
			db := state.NewMemoryStateDB()
			db.AddBalance(from, big.NewInt(100_000_000))
			gp := new(gaspool.GasPool).AddGas(header.GasLimit)
			unsigned := types.NewTransaction(pq)
			unsigned.SetSender(from)
			if _, _, err := ApplyTransaction(&cfg, db, header, unsigned, gp); err == nil {
				t.Fatal("unsigned transaction accepted with cached sender")
			}
			sig, err := signer.SignTransaction(unsigned, sk)
			if err != nil {
				t.Fatal(err)
			}
			pq.SignWithPQ(uint8(scheme), pk.Raw, sig)
			if err := types.NewPQTxPool(types.NewPQTxValidator(), 1).AcceptPQTx(pq); err != nil {
				t.Fatalf("pool rejected valid signature: %v", err)
			}
			pq.Data = []byte{1}
			if _, _, err := ApplyTransaction(&cfg, db, header, types.NewTransaction(pq), gp); err == nil {
				t.Fatal("transaction with changed data accepted")
			}
			pq.Data = nil
			signed := types.NewTransaction(pq)
			signed.SetSender(types.Address{0xff})
			if _, _, err := ApplyTransaction(&cfg, db, header, signed, gp); err != nil {
				t.Fatalf("valid transaction failed: %v", err)
			}
			if db.GetBalance(to).Cmp(big.NewInt(100)) != 0 || signed.Sender() == nil || *signed.Sender() != from {
				t.Fatal("transfer did not use the verified sender")
			}
		})
	}
}
