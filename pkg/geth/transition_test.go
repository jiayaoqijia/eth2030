package geth

import (
	"math/big"
	"testing"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethcore "github.com/ethereum/go-ethereum/core"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethvm "github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	"github.com/eth2030/eth2030/core/types"
)

// newCancunConfig returns a ChainConfig with all forks through Cancun active.
func newCancunConfig() *params.ChainConfig {
	zero := big.NewInt(0)
	ts := uint64(0)
	return &params.ChainConfig{
		ChainID:                 big.NewInt(1),
		HomesteadBlock:          zero,
		EIP150Block:             zero,
		EIP155Block:             zero,
		EIP158Block:             zero,
		ByzantiumBlock:          zero,
		ConstantinopleBlock:     zero,
		PetersburgBlock:         zero,
		IstanbulBlock:           zero,
		BerlinBlock:             zero,
		LondonBlock:             zero,
		TerminalTotalDifficulty: zero,
		ShanghaiTime:            &ts,
		CancunTime:              &ts,
	}
}

// --- EffectiveGasPrice tests ---

func TestEffectiveGasPrice_NilBaseFee(t *testing.T) {
	gasPrice := big.NewInt(100)
	result := EffectiveGasPrice(gasPrice, nil, nil, nil)
	if result.Int64() != 100 {
		t.Errorf("got %d, want 100", result.Int64())
	}
}

func TestEffectiveGasPrice_NilMaxFeePerGas(t *testing.T) {
	gasPrice := big.NewInt(200)
	baseFee := big.NewInt(50)
	result := EffectiveGasPrice(gasPrice, nil, nil, baseFee)
	if result.Int64() != 200 {
		t.Errorf("got %d, want 200", result.Int64())
	}
}

func TestEffectiveGasPrice_NilGasPrice(t *testing.T) {
	// Both baseFee and maxFeePerGas nil, gasPrice nil -> returns zero.
	result := EffectiveGasPrice(nil, nil, nil, nil)
	if result.Int64() != 0 {
		t.Errorf("got %d, want 0", result.Int64())
	}
}

func TestEffectiveGasPrice_TipLimited(t *testing.T) {
	// baseFee + tipCap < maxFeePerGas: result = baseFee + tipCap.
	maxFee := big.NewInt(100)
	tipCap := big.NewInt(10)
	baseFee := big.NewInt(20)
	// effective = min(100, 20+10) = 30
	result := EffectiveGasPrice(nil, maxFee, tipCap, baseFee)
	if result.Int64() != 30 {
		t.Errorf("got %d, want 30", result.Int64())
	}
}

func TestEffectiveGasPrice_CapLimited(t *testing.T) {
	// baseFee + tipCap > maxFeePerGas: result = maxFeePerGas.
	maxFee := big.NewInt(50)
	tipCap := big.NewInt(40)
	baseFee := big.NewInt(30)
	// effective = min(50, 30+40) = 50
	result := EffectiveGasPrice(nil, maxFee, tipCap, baseFee)
	if result.Int64() != 50 {
		t.Errorf("got %d, want 50", result.Int64())
	}
}

func TestEffectiveGasPrice_Exact(t *testing.T) {
	// baseFee + tipCap == maxFeePerGas: result = maxFeePerGas.
	maxFee := big.NewInt(70)
	tipCap := big.NewInt(40)
	baseFee := big.NewInt(30)
	// effective = min(70, 30+40) = 70
	result := EffectiveGasPrice(nil, maxFee, tipCap, baseFee)
	if result.Int64() != 70 {
		t.Errorf("got %d, want 70", result.Int64())
	}
}

func TestEffectiveGasPrice_ZeroTip(t *testing.T) {
	maxFee := big.NewInt(100)
	tipCap := big.NewInt(0)
	baseFee := big.NewInt(50)
	result := EffectiveGasPrice(nil, maxFee, tipCap, baseFee)
	if result.Int64() != 50 {
		t.Errorf("got %d, want 50", result.Int64())
	}
}

// --- TxContextFromMessage tests ---

func TestTxContextFromMessage_Legacy(t *testing.T) {
	msg := &gethcore.Message{
		From:     gethcommon.Address{0x01},
		GasPrice: uint256.NewInt(200),
	}
	ctx := TxContextFromMessage(msg, nil)
	if ctx.Origin != (gethcommon.Address{0x01}) {
		t.Errorf("Origin = %v, want 0x01", ctx.Origin)
	}
	// Legacy: no baseFee, uses msg.GasPrice directly.
	if ctx.GasPrice.ToBig().Int64() != 200 {
		t.Errorf("GasPrice = %d, want 200", ctx.GasPrice.ToBig().Int64())
	}
}

func TestTxContextFromMessage_EIP1559(t *testing.T) {
	from := gethcommon.HexToAddress("0xdeadbeef")
	msg := &gethcore.Message{
		From:      from,
		GasPrice:  uint256.NewInt(0),
		GasFeeCap: uint256.NewInt(100),
		GasTipCap: uint256.NewInt(10),
	}
	baseFee := big.NewInt(50)
	ctx := TxContextFromMessage(msg, baseFee)
	if ctx.Origin != from {
		t.Errorf("Origin = %v, want %v", ctx.Origin, from)
	}
	// effective = min(100, 50+10) = 60
	if ctx.GasPrice.ToBig().Int64() != 60 {
		t.Errorf("GasPrice = %d, want 60", ctx.GasPrice.ToBig().Int64())
	}
}

func TestTxContextFromMessage_NilBaseFee(t *testing.T) {
	from := gethcommon.HexToAddress("0x1234")
	msg := &gethcore.Message{
		From:      from,
		GasPrice:  uint256.NewInt(300),
		GasFeeCap: uint256.NewInt(400),
		GasTipCap: uint256.NewInt(50),
	}
	// nil baseFee: skip EIP-1559 calc, use msg.GasPrice.
	ctx := TxContextFromMessage(msg, nil)
	if ctx.GasPrice.ToBig().Int64() != 300 {
		t.Errorf("GasPrice = %d, want 300 (legacy path)", ctx.GasPrice.ToBig().Int64())
	}
}

// --- IsEIP158 tests ---

func TestIsEIP158_Active(t *testing.T) {
	config := &params.ChainConfig{
		ChainID:     big.NewInt(1),
		EIP158Block: big.NewInt(5),
	}
	if !IsEIP158(config, big.NewInt(5)) {
		t.Error("IsEIP158 should be active at block 5")
	}
	if !IsEIP158(config, big.NewInt(100)) {
		t.Error("IsEIP158 should be active after block 5")
	}
}

func TestIsEIP158_NotActive(t *testing.T) {
	config := &params.ChainConfig{
		ChainID:     big.NewInt(1),
		EIP158Block: big.NewInt(10),
	}
	if IsEIP158(config, big.NewInt(9)) {
		t.Error("IsEIP158 should not be active before block 10")
	}
}

func TestIsEIP158_NilBlock(t *testing.T) {
	config := &params.ChainConfig{
		ChainID: big.NewInt(1),
		// EIP158Block is nil: never active.
	}
	if IsEIP158(config, big.NewInt(1000)) {
		t.Error("IsEIP158 should not be active when EIP158Block is nil")
	}
}

// --- IsCancun tests ---

func TestIsCancun_Active(t *testing.T) {
	config := newCancunConfig()
	if !IsCancun(config, big.NewInt(1), 0) {
		t.Error("IsCancun should be active with CancunTime=0 at time=0")
	}
	if !IsCancun(config, big.NewInt(1), 1000) {
		t.Error("IsCancun should be active with CancunTime=0 at time=1000")
	}
}

func TestIsCancun_NotActive(t *testing.T) {
	ts := uint64(1000)
	config := &params.ChainConfig{
		ChainID:                 big.NewInt(1),
		HomesteadBlock:          big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		CancunTime:              &ts,
	}
	// Time is before CancunTime.
	if IsCancun(config, big.NewInt(1), 999) {
		t.Error("IsCancun should not be active before CancunTime")
	}
}

func TestIsCancun_NilCancunTime(t *testing.T) {
	config := &params.ChainConfig{
		ChainID:     big.NewInt(1),
		LondonBlock: big.NewInt(0),
		// CancunTime not set.
	}
	if IsCancun(config, big.NewInt(1), 9999) {
		t.Error("IsCancun should not be active when CancunTime is nil")
	}
}

// --- TestBlockHash tests ---

func TestTestBlockHash_Deterministic(t *testing.T) {
	h1 := TestBlockHash(42)
	h2 := TestBlockHash(42)
	if h1 != h2 {
		t.Error("TestBlockHash is not deterministic")
	}
}

func TestTestBlockHash_Unique(t *testing.T) {
	h0 := TestBlockHash(0)
	h1 := TestBlockHash(1)
	if h0 == h1 {
		t.Error("TestBlockHash(0) == TestBlockHash(1), expected distinct hashes")
	}
}

func TestTestBlockHash_NonZero(t *testing.T) {
	h := TestBlockHash(0)
	if h == (gethcommon.Hash{}) {
		t.Error("TestBlockHash(0) returned zero hash")
	}
}

// --- MakeMessage tests ---

func TestMakeMessage_Basic(t *testing.T) {
	from := gethcommon.Address{0x01}
	to := gethcommon.Address{0x02}
	msg := MakeMessage(
		from,
		&to,
		5,                // nonce
		big.NewInt(1000), // value
		21000,            // gasLimit
		big.NewInt(100),  // gasPrice
		nil,              // gasFeeCap
		nil,              // gasTipCap
		[]byte{0xab},     // data
		nil,              // accessList
		nil,              // blobHashes
		nil,              // blobGasFeeCap
		nil,              // authList
	)
	if msg.From != from {
		t.Errorf("From = %v, want %v", msg.From, from)
	}
	if msg.To == nil || *msg.To != to {
		t.Errorf("To = %v, want %v", msg.To, to)
	}
	if msg.Nonce != 5 {
		t.Errorf("Nonce = %d, want 5", msg.Nonce)
	}
	if msg.Value.Uint64() != 1000 {
		t.Errorf("Value = %d, want 1000", msg.Value.Uint64())
	}
	if msg.GasLimit != 21000 {
		t.Errorf("GasLimit = %d, want 21000", msg.GasLimit)
	}
	if msg.GasPrice.Uint64() != 100 {
		t.Errorf("GasPrice = %d, want 100", msg.GasPrice.Uint64())
	}
	if len(msg.Data) != 1 || msg.Data[0] != 0xab {
		t.Errorf("Data = %v, want [0xab]", msg.Data)
	}
}

func TestMakeMessage_NilTo(t *testing.T) {
	from := gethcommon.Address{0xaa}
	msg := MakeMessage(
		from, nil,
		0, big.NewInt(0), 100000, big.NewInt(1),
		nil, nil, nil, nil, nil, nil, nil,
	)
	if msg.To != nil {
		t.Errorf("To should be nil for contract creation, got %v", msg.To)
	}
}

func TestMakeMessage_WithAccessList(t *testing.T) {
	from := gethcommon.Address{0x01}
	to := gethcommon.Address{0x02}
	al := gethtypes.AccessList{
		{Address: gethcommon.Address{0x03}, StorageKeys: []gethcommon.Hash{{0x01}}},
	}
	msg := MakeMessage(
		from, &to, 0, big.NewInt(0), 50000,
		big.NewInt(10), nil, nil, nil,
		al, nil, nil, nil,
	)
	if len(msg.AccessList) != 1 {
		t.Errorf("AccessList length = %d, want 1", len(msg.AccessList))
	}
}

// --- MakeBlockContext tests ---

func TestMakeBlockContext_Basic(t *testing.T) {
	header := &types.Header{
		Number:   big.NewInt(100),
		GasLimit: 8_000_000,
		Time:     1234,
		Coinbase: types.Address{0xAA},
		BaseFee:  big.NewInt(1_000_000_000),
	}

	ctx := MakeBlockContext(header, TestBlockHash)
	if ctx.BlockNumber.Int64() != 100 {
		t.Errorf("BlockNumber = %d, want 100", ctx.BlockNumber.Int64())
	}
	if ctx.GasLimit != 8_000_000 {
		t.Errorf("GasLimit = %d, want 8000000", ctx.GasLimit)
	}
	if ctx.Time != 1234 {
		t.Errorf("Time = %d, want 1234", ctx.Time)
	}
	if ctx.Coinbase != (gethcommon.Address{0xAA}) {
		t.Errorf("Coinbase = %v", ctx.Coinbase)
	}
	if ctx.BaseFee == nil || ctx.BaseFee.Int64() != 1_000_000_000 {
		t.Errorf("BaseFee = %v, want 1000000000", ctx.BaseFee)
	}
}

func TestMakeBlockContext_WithDifficulty(t *testing.T) {
	header := &types.Header{
		Number:     big.NewInt(10),
		GasLimit:   1_000_000,
		Time:       500,
		Difficulty: big.NewInt(123456),
		BaseFee:    big.NewInt(0),
	}
	ctx := MakeBlockContext(header, TestBlockHash)
	if ctx.Difficulty == nil || ctx.Difficulty.Int64() != 123456 {
		t.Errorf("Difficulty = %v, want 123456", ctx.Difficulty)
	}
	// Random should be nil when MixDigest is zero.
	if ctx.Random != nil {
		t.Errorf("Random should be nil for zero MixDigest")
	}
}

func TestMakeBlockContext_WithMixDigest(t *testing.T) {
	header := &types.Header{
		Number:     big.NewInt(10),
		GasLimit:   1_000_000,
		Time:       500,
		MixDigest:  types.Hash{0x01, 0x02, 0x03},
		Difficulty: new(big.Int),
		BaseFee:    big.NewInt(0),
	}
	ctx := MakeBlockContext(header, TestBlockHash)
	if ctx.Random == nil {
		t.Error("Random should be set for non-zero MixDigest")
	}
}

func TestMakeBlockContext_WithExcessBlobGas(t *testing.T) {
	excessBlobGas := uint64(1000)
	header := &types.Header{
		Number:        big.NewInt(10),
		GasLimit:      1_000_000,
		Time:          500,
		BaseFee:       big.NewInt(0),
		ExcessBlobGas: &excessBlobGas,
	}
	ctx := MakeBlockContext(header, TestBlockHash)
	// BlobBaseFee should be set when ExcessBlobGas is set.
	if ctx.BlobBaseFee == nil {
		t.Error("BlobBaseFee should be set when ExcessBlobGas is set")
	}
}

func TestMakeBlockContext_GetHash(t *testing.T) {
	header := &types.Header{
		Number:   big.NewInt(5),
		GasLimit: 1_000_000,
		Time:     100,
		BaseFee:  big.NewInt(0),
	}
	called := false
	getHash := func(n uint64) gethcommon.Hash {
		called = true
		return TestBlockHash(n)
	}
	ctx := MakeBlockContext(header, getHash)
	// Call GetHash to verify it's wired.
	ctx.GetHash(4)
	if !called {
		t.Error("GetHash was not wired correctly")
	}
}

// --- ApplyMessage integration test ---

func TestApplyMessage_SimpleTransfer(t *testing.T) {
	config := &params.ChainConfig{
		ChainID:        big.NewInt(1),
		HomesteadBlock: big.NewInt(0),
		EIP150Block:    big.NewInt(0),
		EIP155Block:    big.NewInt(0),
		EIP158Block:    big.NewInt(0),
		ByzantiumBlock: big.NewInt(0),
		LondonBlock:    big.NewInt(0),
	}

	sender := "0x1000000000000000000000000000000000000001"
	recipient := "0x2000000000000000000000000000000000000002"

	preState, err := MakePreState(map[string]PreAccount{
		sender: {Balance: big.NewInt(1_000_000_000_000_000_000), Nonce: 0},
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
		BaseFee:  big.NewInt(1_000_000_000),
	}
	blockCtx := MakeBlockContext(header, TestBlockHash)

	toAddr := gethcommon.HexToAddress(recipient)
	msg := &gethcore.Message{
		From:      gethcommon.HexToAddress(sender),
		To:        &toAddr,
		Nonce:     0,
		Value:     uint256.NewInt(1000),
		GasLimit:  21000,
		GasPrice:  uint256.NewInt(2_000_000_000),
		GasFeeCap: uint256.NewInt(2_000_000_000),
		GasTipCap: uint256.NewInt(1_000_000_000),
	}

	result, err := ApplyMessage(preState.StateDB, config, blockCtx, msg, 8_000_000)
	if err != nil {
		t.Fatalf("ApplyMessage: %v", err)
	}
	if result.Failed() {
		t.Errorf("expected success, got failure: %v", result.Err)
	}
	if result.UsedGas != 21000 {
		t.Errorf("UsedGas = %d, want 21000", result.UsedGas)
	}
}

func TestApplyMessage_InsufficientBalance(t *testing.T) {
	config := &params.ChainConfig{
		ChainID:        big.NewInt(1),
		HomesteadBlock: big.NewInt(0),
		EIP150Block:    big.NewInt(0),
		EIP158Block:    big.NewInt(0),
		ByzantiumBlock: big.NewInt(0),
		LondonBlock:    big.NewInt(0),
	}

	sender := "0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	recipient := "0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	// Sender has only 1 wei.
	preState, err := MakePreState(map[string]PreAccount{
		sender: {Balance: big.NewInt(1), Nonce: 0},
	})
	if err != nil {
		t.Fatalf("MakePreState: %v", err)
	}
	defer preState.Close()

	header := &types.Header{
		Number:   big.NewInt(1),
		GasLimit: 8_000_000,
		Time:     1000,
		BaseFee:  big.NewInt(1_000_000_000),
	}
	blockCtx := MakeBlockContext(header, TestBlockHash)

	toAddr := gethcommon.HexToAddress(recipient)
	msg := &gethcore.Message{
		From:      gethcommon.HexToAddress(sender),
		To:        &toAddr,
		Nonce:     0,
		Value:     uint256.NewInt(1_000_000_000_000_000_000), // 1 ETH, more than balance
		GasLimit:  21000,
		GasPrice:  uint256.NewInt(2_000_000_000),
		GasFeeCap: uint256.NewInt(2_000_000_000),
		GasTipCap: uint256.NewInt(1_000_000_000),
	}

	_, err = ApplyMessage(preState.StateDB, config, blockCtx, msg, 8_000_000)
	// Insufficient balance to pay gas cost should return an error.
	if err == nil {
		t.Error("expected error for insufficient balance, got nil")
	}
}

// --- getBlobBaseFee tests ---

func TestGetBlobBaseFee_ZeroExcess(t *testing.T) {
	excess := uint64(0)
	header := &gethtypes.Header{ExcessBlobGas: &excess}
	fee := getBlobBaseFee(header)
	if fee == nil {
		t.Fatal("getBlobBaseFee returned nil")
	}
	if fee.Int64() != 1 {
		t.Errorf("getBlobBaseFee(0) = %d, want 1", fee.Int64())
	}
}

func TestGetBlobBaseFee_NilExcess(t *testing.T) {
	header := &gethtypes.Header{ExcessBlobGas: nil}
	fee := getBlobBaseFee(header)
	if fee != nil {
		t.Errorf("getBlobBaseFee with nil ExcessBlobGas = %v, want nil", fee)
	}
}

func TestGetBlobBaseFee_NonZeroExcess(t *testing.T) {
	excess := uint64(100)
	header := &gethtypes.Header{ExcessBlobGas: &excess}
	fee := getBlobBaseFee(header)
	if fee == nil {
		t.Fatal("getBlobBaseFee returned nil")
	}
	// Any non-nil result is acceptable for non-zero excess.
	if fee.Sign() < 0 {
		t.Error("getBlobBaseFee should return non-negative value")
	}
}

// --- IsCancun / IsEIP158 boundary tests ---

func TestIsEIP158_AtExactBlock(t *testing.T) {
	block := big.NewInt(100)
	config := &params.ChainConfig{
		ChainID:     big.NewInt(1),
		EIP158Block: block,
	}
	// Exactly at activation block.
	if !IsEIP158(config, big.NewInt(100)) {
		t.Error("IsEIP158 should be active at exact activation block")
	}
	// One block before.
	if IsEIP158(config, big.NewInt(99)) {
		t.Error("IsEIP158 should not be active one block before activation")
	}
}

func TestIsCancun_AtExactTime(t *testing.T) {
	ts := uint64(500)
	config := &params.ChainConfig{
		ChainID:                 big.NewInt(1),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		CancunTime:              &ts,
	}
	// Exactly at activation time.
	if !IsCancun(config, big.NewInt(1), 500) {
		t.Error("IsCancun should be active at exact activation time")
	}
	// One second before.
	if IsCancun(config, big.NewInt(1), 499) {
		t.Error("IsCancun should not be active one second before activation time")
	}
}

// --- TxContextFromMessage with EIP-1559 cap equals base + tip ---

func TestTxContextFromMessage_CapEqualsTip(t *testing.T) {
	from := gethcommon.Address{0x05}
	msg := &gethcore.Message{
		From:      from,
		GasFeeCap: uint256.NewInt(60),
		GasTipCap: uint256.NewInt(10),
	}
	baseFee := big.NewInt(50)
	// baseFee(50) + tipCap(10) = 60 == maxFeePerGas(60) -> returns 60.
	ctx := TxContextFromMessage(msg, baseFee)
	if ctx.GasPrice.ToBig().Int64() != 60 {
		t.Errorf("GasPrice = %d, want 60", ctx.GasPrice.ToBig().Int64())
	}
}

func TestTxContextFromMessage_ZeroGasPrice(t *testing.T) {
	from := gethcommon.Address{0x06}
	msg := &gethcore.Message{
		From:     from,
		GasPrice: uint256.NewInt(0),
	}
	ctx := TxContextFromMessage(msg, nil)
	if ctx.GasPrice.ToBig().Sign() != 0 {
		t.Errorf("GasPrice = %d, want 0", ctx.GasPrice.ToBig().Int64())
	}
}

// --- EffectiveGasPrice does not mutate inputs ---

func TestEffectiveGasPrice_NoMutation(t *testing.T) {
	maxFee := big.NewInt(100)
	tipCap := big.NewInt(20)
	baseFee := big.NewInt(30)

	origMaxFee := new(big.Int).Set(maxFee)
	origTipCap := new(big.Int).Set(tipCap)
	origBaseFee := new(big.Int).Set(baseFee)

	EffectiveGasPrice(nil, maxFee, tipCap, baseFee)

	if maxFee.Cmp(origMaxFee) != 0 {
		t.Error("maxFee was mutated")
	}
	if tipCap.Cmp(origTipCap) != 0 {
		t.Error("tipCap was mutated")
	}
	if baseFee.Cmp(origBaseFee) != 0 {
		t.Error("baseFee was mutated")
	}
}

// --- MakeMessage with blobHashes ---

func TestMakeMessage_WithBlobHashes(t *testing.T) {
	from := gethcommon.Address{0x01}
	to := gethcommon.Address{0x02}
	blobHashes := []gethcommon.Hash{{0xAA}, {0xBB}}
	blobGasFeeCap := big.NewInt(500)

	msg := MakeMessage(
		from, &to, 0, big.NewInt(0), 100000,
		big.NewInt(0), big.NewInt(100), big.NewInt(5),
		nil, nil, blobHashes, blobGasFeeCap, nil,
	)
	if len(msg.BlobHashes) != 2 {
		t.Errorf("BlobHashes length = %d, want 2", len(msg.BlobHashes))
	}
	if msg.BlobGasFeeCap.Uint64() != 500 {
		t.Errorf("BlobGasFeeCap = %d, want 500", msg.BlobGasFeeCap.Uint64())
	}
}

// --- gethvm.TxContext fields ---

func TestTxContextFromMessage_OriginSet(t *testing.T) {
	expected := gethcommon.HexToAddress("0xcafebabe")
	msg := &gethcore.Message{
		From:     expected,
		GasPrice: uint256.NewInt(50),
	}
	ctx := TxContextFromMessage(msg, nil)
	if ctx.Origin != expected {
		t.Errorf("Origin = %v, want %v", ctx.Origin, expected)
	}
}

// Verify TxContextFromMessage returns a gethvm.TxContext (compile-time check).
var _ gethvm.TxContext = TxContextFromMessage(&gethcore.Message{GasPrice: uint256.NewInt(0)}, nil)
