package payload

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// TestNewPayloadBuilder_Init creates a PayloadBuilder and verifies initial state.
func TestNewPayloadBuilder_Init(t *testing.T) {
	pb := NewPayloadBuilder(nil, nil, nil)
	if pb == nil {
		t.Fatal("NewPayloadBuilder returned nil")
	}
	if pb.payloads == nil {
		t.Fatal("payloads map should be initialized")
	}
}

// TestPayloadBuilder_GetPayload_Missing tests retrieving a non-existent payload.
func TestPayloadBuilder_GetPayload_Missing(t *testing.T) {
	pb := NewPayloadBuilder(nil, nil, nil)
	var id PayloadID
	_, err := pb.GetPayload(id)
	if err != ErrUnknownPayload {
		t.Fatalf("want ErrUnknownPayload, got %v", err)
	}
}

// TestPayloadBuilder_GetPayload_Stored tests storing and retrieving a payload directly.
func TestPayloadBuilder_GetPayload_Stored(t *testing.T) {
	pb := NewPayloadBuilder(nil, nil, nil)

	var id PayloadID
	id[0] = 0x01

	block := types.NewBlock(&types.Header{
		Number: big.NewInt(1),
	}, nil)

	pb.mu.Lock()
	pb.payloads[id] = &BuiltPayload{
		Block:       block,
		Receipts:    []*types.Receipt{},
		BlockValue:  big.NewInt(100),
		BlobsBundle: &BlobsBundleV1{},
	}
	pb.mu.Unlock()

	built, err := pb.GetPayload(id)
	if err != nil {
		t.Fatalf("GetPayload error: %v", err)
	}
	if built.Block == nil {
		t.Fatal("Block should not be nil")
	}
	if built.BlockValue.Cmp(big.NewInt(100)) != 0 {
		t.Fatalf("want block value 100, got %s", built.BlockValue.String())
	}
}

// TestCalcBlockValue_NilBaseFee tests block value calculation with nil base fee.
func TestCalcBlockValue_NilBaseFee(t *testing.T) {
	block := types.NewBlock(&types.Header{
		Number: big.NewInt(1),
	}, nil)
	receipts := []*types.Receipt{}

	value := calcBlockValue(block, receipts, nil)
	if value.Sign() != 0 {
		t.Fatalf("expected zero value with nil baseFee, got %s", value.String())
	}
}

// TestCalcBlockValue_EmptyBlock tests block value for an empty block.
func TestCalcBlockValue_EmptyBlock(t *testing.T) {
	block := types.NewBlock(&types.Header{
		Number: big.NewInt(1),
	}, nil)
	receipts := []*types.Receipt{}
	baseFee := big.NewInt(1000000000) // 1 Gwei

	value := calcBlockValue(block, receipts, baseFee)
	if value.Sign() != 0 {
		t.Fatalf("expected zero value for empty block, got %s", value.String())
	}
}

// TestEffectiveTipPerGas_LegacyTx tests tip calculation for legacy transactions.
func TestEffectiveTipPerGas_LegacyTx(t *testing.T) {
	baseFee := big.NewInt(1000000000)  // 1 Gwei
	gasPrice := big.NewInt(3000000000) // 3 Gwei

	tx := types.NewTransaction(&types.LegacyTx{
		GasPrice: gasPrice,
		Gas:      21000,
	})

	tip := effectiveTipPerGas(tx, baseFee)
	expected := big.NewInt(2000000000)
	if tip.Cmp(expected) != 0 {
		t.Fatalf("want tip %s, got %s", expected.String(), tip.String())
	}
}

// TestEffectiveTipPerGas_NilGasPrice tests tip for a tx with nil gas price.
func TestEffectiveTipPerGas_NilGasPrice(t *testing.T) {
	baseFee := big.NewInt(1000000000)
	tx := types.NewTransaction(&types.LegacyTx{})

	tip := effectiveTipPerGas(tx, baseFee)
	if tip == nil {
		t.Fatal("tip should not be nil")
	}
}

// TestBuiltPayload_AllFields verifies all BuiltPayload fields can be set and read.
func TestBuiltPayload_AllFields(t *testing.T) {
	bp := &BuiltPayload{
		Block:             types.NewBlock(&types.Header{Number: big.NewInt(42)}, nil),
		Receipts:          []*types.Receipt{{GasUsed: 21000}},
		BlockValue:        big.NewInt(12345),
		BlobsBundle:       &BlobsBundleV1{Commitments: [][]byte{{0x01}}},
		Override:          true,
		ExecutionRequests: [][]byte{{0x02}},
	}

	if bp.Block.NumberU64() != 42 {
		t.Fatalf("want block 42, got %d", bp.Block.NumberU64())
	}
	if len(bp.Receipts) != 1 {
		t.Fatalf("want 1 receipt, got %d", len(bp.Receipts))
	}
	if bp.BlockValue.Cmp(big.NewInt(12345)) != 0 {
		t.Fatalf("want value 12345, got %s", bp.BlockValue.String())
	}
	if !bp.Override {
		t.Fatal("Override should be true")
	}
	if len(bp.ExecutionRequests) != 1 {
		t.Fatalf("want 1 execution request, got %d", len(bp.ExecutionRequests))
	}
}

// TestBlockToPayloadV5_Conversion tests the conversion of a block to V5 payload format.
func TestBlockToPayloadV5_Conversion(t *testing.T) {
	header := &types.Header{
		Number:   big.NewInt(10),
		GasLimit: 30000000,
		GasUsed:  15000000,
		Time:     1700000000,
		BaseFee:  big.NewInt(1000000000),
	}
	block := types.NewBlock(header, nil)
	prevRandao := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")

	ep := BlockToPayloadV5(block, prevRandao, nil, nil, 0)
	if ep == nil {
		t.Fatal("BlockToPayloadV5 returned nil")
	}
	if ep.BlockNumber != 10 {
		t.Fatalf("want block number 10, got %d", ep.BlockNumber)
	}
	if ep.GasLimit != 30000000 {
		t.Fatalf("want gas limit 30000000, got %d", ep.GasLimit)
	}
	if ep.Timestamp != 1700000000 {
		t.Fatalf("want timestamp 1700000000, got %d", ep.Timestamp)
	}
}
