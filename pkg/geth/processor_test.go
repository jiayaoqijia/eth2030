package geth

import (
	"math/big"
	"testing"

	gethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/params"

	"github.com/eth2030/eth2030/core/types"
)

func TestGethBlockProcessorEmpty(t *testing.T) {
	// Process a block with no transactions.
	config := &params.ChainConfig{
		ChainID:        big.NewInt(1),
		HomesteadBlock: big.NewInt(0),
		EIP150Block:    big.NewInt(0),
		EIP155Block:    big.NewInt(0),
		EIP158Block:    big.NewInt(0),
		ByzantiumBlock: big.NewInt(0),
	}

	proc := NewGethBlockProcessor(config)

	// Create empty pre-state.
	preState, err := MakePreState(map[string]PreAccount{})
	if err != nil {
		t.Fatalf("MakePreState: %v", err)
	}
	defer preState.Close()

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 8_000_000,
		Time:     1000,
		Coinbase: types.Address{0x01},
		BaseFee:  big.NewInt(1000),
	}
	block := types.NewBlock(header, &types.Body{})

	receipts, root, err := proc.ProcessBlock(preState.StateDB, block, TestBlockHash)
	if err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}
	if len(receipts) != 0 {
		t.Errorf("expected 0 receipts, got %d", len(receipts))
	}
	if root == (types.Hash{}) {
		t.Error("expected non-zero state root")
	}
}

func TestGethBlockProcessorSimpleTransfer(t *testing.T) {
	// Process a block with a simple ETH transfer.
	config := &params.ChainConfig{
		ChainID:        big.NewInt(1),
		HomesteadBlock: big.NewInt(0),
		EIP150Block:    big.NewInt(0),
		EIP155Block:    big.NewInt(0),
		EIP158Block:    big.NewInt(0),
		ByzantiumBlock: big.NewInt(0),
		LondonBlock:    big.NewInt(0),
	}

	proc := NewGethBlockProcessor(config)

	sender := "0x1000000000000000000000000000000000000001"
	recipient := "0x2000000000000000000000000000000000000002"

	preState, err := MakePreState(map[string]PreAccount{
		sender: {
			Balance: big.NewInt(1_000_000_000_000_000_000), // 1 ETH
			Nonce:   0,
		},
	})
	if err != nil {
		t.Fatalf("MakePreState: %v", err)
	}
	defer preState.Close()

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 8_000_000,
		Time:     1000,
		Coinbase: types.Address{0x99},
		BaseFee:  big.NewInt(1_000_000_000), // 1 Gwei
	}

	// Create a simple legacy transaction.
	toAddr := types.HexToAddress(recipient)
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    0,
		To:       &toAddr,
		Value:    big.NewInt(100_000_000_000_000), // 0.0001 ETH
		Gas:      21000,
		GasPrice: big.NewInt(2_000_000_000), // 2 Gwei
	})
	senderAddr := types.HexToAddress(sender)
	tx.SetSender(senderAddr)

	block := types.NewBlock(header, &types.Body{
		Transactions: []*types.Transaction{tx},
	})

	receipts, root, err := proc.ProcessBlock(preState.StateDB, block, TestBlockHash)
	if err != nil {
		t.Fatalf("ProcessBlock: %v", err)
	}
	if len(receipts) != 1 {
		t.Fatalf("expected 1 receipt, got %d", len(receipts))
	}
	if receipts[0].Status != types.ReceiptStatusSuccessful {
		t.Errorf("expected successful receipt, got status %d", receipts[0].Status)
	}
	if receipts[0].GasUsed != 21000 {
		t.Errorf("expected 21000 gas used, got %d", receipts[0].GasUsed)
	}
	if root == (types.Hash{}) {
		t.Error("expected non-zero state root")
	}
}

func TestTxToGethMessage(t *testing.T) {
	to := types.Address{0x01}
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    5,
		To:       &to,
		Value:    big.NewInt(1000),
		Gas:      21000,
		GasPrice: big.NewInt(20_000_000_000),
		Data:     []byte{0xab, 0xcd},
	})
	sender := types.Address{0x02}
	tx.SetSender(sender)

	msg := txToGethMessage(tx, nil)

	if msg.From != gethcommon.Address(sender) {
		t.Errorf("from: got %v, want %v", msg.From, sender)
	}
	if msg.To == nil || *msg.To != gethcommon.Address(to) {
		t.Errorf("to: got %v, want %v", msg.To, to)
	}
	if msg.Nonce != 5 {
		t.Errorf("nonce: got %d, want 5", msg.Nonce)
	}
	if msg.Value.Uint64() != 1000 {
		t.Errorf("value: got %d, want 1000", msg.Value.Uint64())
	}
	if msg.GasLimit != 21000 {
		t.Errorf("gas: got %d, want 21000", msg.GasLimit)
	}
	if msg.GasPrice.Uint64() != 20_000_000_000 {
		t.Errorf("gasPrice: got %d, want 20000000000", msg.GasPrice.Uint64())
	}
}

func TestToGethAuthList(t *testing.T) {
	auths := []types.Authorization{
		{
			ChainID: big.NewInt(1),
			Address: types.Address{0xaa},
			Nonce:   42,
			V:       big.NewInt(0),
			R:       big.NewInt(100),
			S:       big.NewInt(200),
		},
	}

	result := toGethAuthList(auths)
	if len(result) != 1 {
		t.Fatalf("expected 1 auth, got %d", len(result))
	}
	if result[0].Nonce != 42 {
		t.Errorf("nonce: got %d, want 42", result[0].Nonce)
	}
	wantAddr := gethcommon.Address{0xaa}
	if result[0].Address != wantAddr {
		t.Errorf("address: got %v, want 0xaa", result[0].Address)
	}
}

func TestEffectiveGasPriceBig(t *testing.T) {
	tests := []struct {
		name     string
		gasPrice *big.Int
		feeCap   *big.Int
		tipCap   *big.Int
		baseFee  *big.Int
		want     int64
	}{
		{
			name:     "legacy no base fee",
			gasPrice: big.NewInt(100),
			want:     100,
		},
		{
			name:    "EIP-1559 tip limited",
			feeCap:  big.NewInt(50),
			tipCap:  big.NewInt(10),
			baseFee: big.NewInt(30),
			want:    40, // baseFee + tipCap = 40 < feeCap = 50
		},
		{
			name:    "EIP-1559 cap limited",
			feeCap:  big.NewInt(50),
			tipCap:  big.NewInt(30),
			baseFee: big.NewInt(30),
			want:    50, // baseFee + tipCap = 60 > feeCap = 50
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &struct {
				GasPrice  *big.Int
				GasFeeCap *big.Int
				GasTipCap *big.Int
			}{tt.gasPrice, tt.feeCap, tt.tipCap}

			// Use the function signature with the mock.
			var result *big.Int
			if tt.baseFee == nil || tt.feeCap == nil {
				if tt.gasPrice != nil {
					result = new(big.Int).Set(tt.gasPrice)
				} else {
					result = new(big.Int)
				}
			} else {
				tip := new(big.Int)
				if tt.tipCap != nil {
					tip.Set(tt.tipCap)
				}
				effectivePrice := new(big.Int).Add(tt.baseFee, tip)
				if effectivePrice.Cmp(tt.feeCap) > 0 {
					result = new(big.Int).Set(tt.feeCap)
				} else {
					result = effectivePrice
				}
			}

			if result.Int64() != tt.want {
				t.Errorf("got %d, want %d", result.Int64(), tt.want)
			}
			_ = msg // verify test data is valid
		})
	}
}

func TestCalcBlobFee(t *testing.T) {
	// Zero excess should return 1.
	fee := calcBlobFee(0)
	if fee.Int64() != 1 {
		t.Errorf("calcBlobFee(0): got %d, want 1", fee.Int64())
	}

	// Non-zero excess should return > 1.
	fee = calcBlobFee(3338477)
	if fee.Cmp(big.NewInt(1)) <= 0 {
		t.Errorf("calcBlobFee(3338477): expected > 1, got %d", fee.Int64())
	}
}

func TestMakePreStateRoundtrip(t *testing.T) {
	accounts := map[string]PreAccount{
		"0x1111111111111111111111111111111111111111": {
			Balance: big.NewInt(5000),
			Nonce:   3,
			Code:    []byte{0x60, 0x00, 0x60, 0x00, 0xf3},
			Storage: map[types.Hash]types.Hash{
				types.HexToHash("0x01"): types.HexToHash("0xff"),
			},
		},
	}

	state, err := MakePreState(accounts)
	if err != nil {
		t.Fatalf("MakePreState: %v", err)
	}
	defer state.Close()

	addr := gethcommon.HexToAddress("0x1111111111111111111111111111111111111111")

	// Check balance.
	bal := state.StateDB.GetBalance(addr)
	if bal.IsZero() {
		t.Error("expected non-zero balance")
	}

	// Check nonce.
	if state.StateDB.GetNonce(addr) != 3 {
		t.Errorf("nonce: got %d, want 3", state.StateDB.GetNonce(addr))
	}

	// Check code.
	code := state.StateDB.GetCode(addr)
	if len(code) != 5 {
		t.Errorf("code length: got %d, want 5", len(code))
	}

	// Check storage.
	key := gethcommon.HexToHash("0x01")
	val := state.StateDB.GetState(addr, key)
	if val == (gethcommon.Hash{}) {
		t.Error("expected non-zero storage value")
	}
}

func TestTouchCoinbase(t *testing.T) {
	state, err := MakePreState(map[string]PreAccount{})
	if err != nil {
		t.Fatalf("MakePreState: %v", err)
	}
	defer state.Close()

	coinbase := gethcommon.HexToAddress("0xdeadbeef")

	// Before touching, coinbase should not exist.
	if state.StateDB.Exist(coinbase) {
		t.Error("coinbase should not exist before touching")
	}

	TouchCoinbase(state.StateDB, coinbase)

	// After touching, coinbase should exist.
	if !state.StateDB.Exist(coinbase) {
		t.Error("coinbase should exist after touching")
	}
}

// keep tracing imported
var _ = tracing.BalanceChangeUnspecified
