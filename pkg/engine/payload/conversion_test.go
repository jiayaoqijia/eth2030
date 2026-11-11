package payload

import (
	"encoding/json"
	"math/big"
	"strings"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestPayloadToHeader(t *testing.T) {
	payload := &ExecutionPayloadV4{
		ExecutionPayloadV3: ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{
					ParentHash:    types.HexToHash("0xaaaa"),
					FeeRecipient:  types.HexToAddress("0xbbbb"),
					StateRoot:     types.HexToHash("0xcccc"),
					ReceiptsRoot:  types.HexToHash("0xdddd"),
					PrevRandao:    types.HexToHash("0xeeee"),
					BlockNumber:   500,
					GasLimit:      30_000_000,
					GasUsed:       21_000,
					Timestamp:     1700000000,
					ExtraData:     []byte("ETH2030"),
					BaseFeePerGas: big.NewInt(1_000_000_000),
				},
			},
			BlobGasUsed:   131072,
			ExcessBlobGas: 65536,
		},
	}

	header := PayloadToHeader(payload)

	if header.ParentHash != payload.ParentHash {
		t.Error("ParentHash mismatch")
	}
	if header.Coinbase != payload.FeeRecipient {
		t.Error("Coinbase/FeeRecipient mismatch")
	}
	if header.Root != payload.StateRoot {
		t.Error("StateRoot mismatch")
	}
	if header.ReceiptHash != payload.ReceiptsRoot {
		t.Error("ReceiptsRoot mismatch")
	}
	if header.MixDigest != payload.PrevRandao {
		t.Error("PrevRandao mismatch")
	}
	if header.Number.Uint64() != 500 {
		t.Errorf("Number = %d, want 500", header.Number.Uint64())
	}
	if header.GasLimit != 30_000_000 {
		t.Errorf("GasLimit = %d, want 30000000", header.GasLimit)
	}
	if header.GasUsed != 21_000 {
		t.Errorf("GasUsed = %d, want 21000", header.GasUsed)
	}
	if header.Time != 1700000000 {
		t.Errorf("Time = %d, want 1700000000", header.Time)
	}
	if string(header.Extra) != "ETH2030" {
		t.Errorf("Extra = %s, want ETH2030", string(header.Extra))
	}
	if header.BaseFee.Cmp(big.NewInt(1_000_000_000)) != 0 {
		t.Errorf("BaseFee = %s, want 1000000000", header.BaseFee)
	}
	if header.BlobGasUsed == nil || *header.BlobGasUsed != 131072 {
		t.Error("BlobGasUsed mismatch")
	}
	if header.ExcessBlobGas == nil || *header.ExcessBlobGas != 65536 {
		t.Error("ExcessBlobGas mismatch")
	}
	// Post-merge: difficulty is 0
	if header.Difficulty.Sign() != 0 {
		t.Errorf("Difficulty should be 0, got %s", header.Difficulty)
	}
	if header.UncleHash != types.EmptyUncleHash {
		t.Error("UncleHash should be EmptyUncleHash post-merge")
	}
}

func TestWithdrawalsConversion(t *testing.T) {
	engineWs := []*Withdrawal{
		{Index: 1, ValidatorIndex: 100, Address: types.HexToAddress("0xaaaa"), Amount: 1000},
		{Index: 2, ValidatorIndex: 200, Address: types.HexToAddress("0xbbbb"), Amount: 2000},
	}

	coreWs := WithdrawalsToCore(engineWs)
	if len(coreWs) != 2 {
		t.Fatalf("expected 2 withdrawals, got %d", len(coreWs))
	}
	if coreWs[0].Index != 1 || coreWs[0].ValidatorIndex != 100 || coreWs[0].Amount != 1000 {
		t.Error("first withdrawal mismatch")
	}

	backToEngine := WithdrawalsToEngine(coreWs)
	if len(backToEngine) != 2 {
		t.Fatalf("expected 2 withdrawals after round-trip")
	}
	if backToEngine[1].Index != 2 || backToEngine[1].Amount != 2000 {
		t.Error("second withdrawal mismatch after round-trip")
	}
}

func TestHeaderToPayloadFields(t *testing.T) {
	header := &types.Header{
		ParentHash: types.HexToHash("0x1111"),
		Coinbase:   types.HexToAddress("0x2222"),
		Root:       types.HexToHash("0x3333"),
		Number:     big.NewInt(42),
		GasLimit:   30_000_000,
		GasUsed:    15_000_000,
		Time:       1700000000,
		BaseFee:    big.NewInt(1_000_000_000),
	}

	payload := HeaderToPayloadFields(header)

	if payload.ParentHash != header.ParentHash {
		t.Error("ParentHash mismatch")
	}
	if payload.FeeRecipient != header.Coinbase {
		t.Error("FeeRecipient mismatch")
	}
	if payload.BlockNumber != 42 {
		t.Errorf("BlockNumber = %d, want 42", payload.BlockNumber)
	}
}

func TestGetPayloadV5ResponseJSON(t *testing.T) {
	// Test that blockValue and executionRequests are hex-encoded per Engine API spec.
	// go.mod targets go1.26 which forbids multi-level promoted-field struct
	// literals, so build the embedding chain in steps.
	v3 := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				BlockNumber: 123,
				BlockHash:   [32]byte{0x01, 0x02, 0x03},
			},
		},
	}
	resp := &GetPayloadV5Response{
		ExecutionPayload: &ExecutionPayloadV5{
			ExecutionPayloadV4: ExecutionPayloadV4{ExecutionPayloadV3: *v3},
		},
		BlockValue:        big.NewInt(123456789), // 0x75bcd15
		BlobsBundle:       &BlobsBundleV2{},
		Override:          false,
		ExecutionRequests: [][]byte{{0x01, 0x02, 0x03}, {0xaa, 0xbb}},
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	jsonStr := string(data)

	// Verify blockValue is hex string (not base64 or number)
	if !strings.Contains(jsonStr, `"blockValue":"0x75bcd15"`) {
		t.Errorf("blockValue not hex-encoded correctly. Got: %s", jsonStr)
	}

	// Verify executionRequests is array of hex strings
	if !strings.Contains(jsonStr, `"executionRequests":["0x010203","0xaabb"]`) {
		t.Errorf("executionRequests not hex-encoded correctly. Got: %s", jsonStr)
	}

	// Round-trip test
	var parsed GetPayloadV5Response
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if parsed.BlockValue.Cmp(resp.BlockValue) != 0 {
		t.Errorf("blockValue round-trip failed: got %s, want %s", parsed.BlockValue, resp.BlockValue)
	}
	if len(parsed.ExecutionRequests) != 2 {
		t.Errorf("executionRequests length mismatch")
	}
}
