package engine

import (
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	coreconfig "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// --- newPayloadV3/V4 validation tests ---

func TestNewPayloadV3_MissingBeaconRoot(t *testing.T) {
	// EIP-4788: parentBeaconBlockRoot must be provided (non-zero).
	backend := &mockBackend{}
	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(types.Hash{}, 100)
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	// Zero beacon root should fail.
	rootJSON, _ := json.Marshal(types.Hash{})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":[%s,%s,%s],"id":1}`,
		payloadJSON, hashesJSON, rootJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for missing beacon root")
	}
	if rpcResp.Error.Code != InvalidParamsCode {
		t.Errorf("expected error code %d, got %d", InvalidParamsCode, rpcResp.Error.Code)
	}
}

func TestNewPayloadV3_UnsupportedFork(t *testing.T) {
	// If Cancun fork is not active, newPayloadV3 should return unsupported fork error.
	backend := &mockBackend{
		isCancunFn: func(timestamp uint64) bool {
			return false // Cancun NOT active
		},
	}
	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(types.Hash{}, 100)
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":[%s,%s,%s],"id":1}`,
		payloadJSON, hashesJSON, rootJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for unsupported fork")
	}
	if rpcResp.Error.Code != UnsupportedForkCode {
		t.Errorf("expected error code %d, got %d", UnsupportedForkCode, rpcResp.Error.Code)
	}
}

func TestNewPayloadV4_MissingBeaconRoot(t *testing.T) {
	backend := &mockBackend{}
	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(types.Hash{}, 100)
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	rootJSON, _ := json.Marshal(types.Hash{}) // zero = missing
	requestsJSON, _ := json.Marshal([][]byte{})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV4","params":[%s,%s,%s,%s],"id":1}`,
		payloadJSON, hashesJSON, rootJSON, requestsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for missing beacon root")
	}
	if rpcResp.Error.Code != InvalidParamsCode {
		t.Errorf("expected error code %d, got %d", InvalidParamsCode, rpcResp.Error.Code)
	}
}

func TestNewPayloadV4_NilExecutionRequests(t *testing.T) {
	// EIP-7685: executionRequests must be provided (not null).
	backend := &mockBackend{}
	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(types.Hash{}, 100)
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)

	// Send null for executionRequests.
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV4","params":[%s,%s,%s,null],"id":1}`,
		payloadJSON, hashesJSON, rootJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for null execution requests")
	}
	if rpcResp.Error.Code != InvalidParamsCode {
		t.Errorf("expected error code %d, got %d", InvalidParamsCode, rpcResp.Error.Code)
	}
}

func TestNewPayloadV4_UnsupportedFork(t *testing.T) {
	backend := &mockBackend{
		isPragueFn: func(timestamp uint64) bool {
			return false // Prague NOT active
		},
	}
	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(types.Hash{}, 100)
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)
	requestsJSON, _ := json.Marshal([][]byte{})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV4","params":[%s,%s,%s,%s],"id":1}`,
		payloadJSON, hashesJSON, rootJSON, requestsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for unsupported fork")
	}
	if rpcResp.Error.Code != UnsupportedForkCode {
		t.Errorf("expected error code %d, got %d", UnsupportedForkCode, rpcResp.Error.Code)
	}
}

// --- forkchoiceUpdatedV3 improvements ---

func TestForkchoiceUpdatedV3_TimestampProgression(t *testing.T) {
	// Validate that timestamp must be greater than the head block timestamp.
	backend := &mockBackend{
		headTimestamp: 1700000000, // head block timestamp
	}
	api := NewEngineAPI(backend)

	state := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0x1111"),
		SafeBlockHash:      types.HexToHash("0x2222"),
		FinalizedBlockHash: types.HexToHash("0x3333"),
	}
	// Timestamp <= head timestamp should fail.
	attrs := PayloadAttributesV3{
		PayloadAttributesV2: PayloadAttributesV2{
			PayloadAttributesV1: PayloadAttributesV1{
				Timestamp:             1700000000, // equal to head
				PrevRandao:            types.HexToHash("0xaaaa"),
				SuggestedFeeRecipient: types.HexToAddress("0xbbbb"),
			},
			Withdrawals: []*Withdrawal{},
		},
		ParentBeaconBlockRoot: types.HexToHash("0xcccc"),
	}

	stateJSON, _ := json.Marshal(state)
	attrsJSON, _ := json.Marshal(attrs)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s,%s],"id":1}`,
		stateJSON, attrsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for non-progressing timestamp")
	}
	if rpcResp.Error.Code != InvalidPayloadAttributeCode {
		t.Errorf("expected error code %d, got %d", InvalidPayloadAttributeCode, rpcResp.Error.Code)
	}
}

func TestForkchoiceUpdatedV3_MissingBeaconRoot(t *testing.T) {
	// V3 attributes require parentBeaconBlockRoot to be provided.
	backend := &mockBackend{}
	api := NewEngineAPI(backend)

	state := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0x1111"),
		SafeBlockHash:      types.HexToHash("0x2222"),
		FinalizedBlockHash: types.HexToHash("0x3333"),
	}
	attrs := PayloadAttributesV3{
		PayloadAttributesV2: PayloadAttributesV2{
			PayloadAttributesV1: PayloadAttributesV1{
				Timestamp:             1700000012,
				PrevRandao:            types.HexToHash("0xaaaa"),
				SuggestedFeeRecipient: types.HexToAddress("0xbbbb"),
			},
			Withdrawals: []*Withdrawal{},
		},
		ParentBeaconBlockRoot: types.Hash{}, // zero = missing
	}

	stateJSON, _ := json.Marshal(state)
	attrsJSON, _ := json.Marshal(attrs)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s,%s],"id":1}`,
		stateJSON, attrsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for missing beacon root in V3 attributes")
	}
	if rpcResp.Error.Code != InvalidPayloadAttributeCode {
		t.Errorf("expected error code %d, got %d", InvalidPayloadAttributeCode, rpcResp.Error.Code)
	}
}

func TestForkchoiceUpdatedV3_ZeroTimestamp(t *testing.T) {
	backend := &mockBackend{}
	api := NewEngineAPI(backend)

	state := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0x1111"),
		SafeBlockHash:      types.HexToHash("0x2222"),
		FinalizedBlockHash: types.HexToHash("0x3333"),
	}
	attrs := PayloadAttributesV3{
		PayloadAttributesV2: PayloadAttributesV2{
			PayloadAttributesV1: PayloadAttributesV1{
				Timestamp:             0, // invalid
				PrevRandao:            types.HexToHash("0xaaaa"),
				SuggestedFeeRecipient: types.HexToAddress("0xbbbb"),
			},
			Withdrawals: []*Withdrawal{},
		},
		ParentBeaconBlockRoot: types.HexToHash("0xcccc"),
	}

	stateJSON, _ := json.Marshal(state)
	attrsJSON, _ := json.Marshal(attrs)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s,%s],"id":1}`,
		stateJSON, attrsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for zero timestamp")
	}
	if rpcResp.Error.Code != InvalidPayloadAttributeCode {
		t.Errorf("expected error code %d, got %d", InvalidPayloadAttributeCode, rpcResp.Error.Code)
	}
}

func TestForkchoiceUpdatedV3_WithWithdrawals(t *testing.T) {
	// Verify withdrawals are properly forwarded through V3 attributes.
	expectedID := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	backend := &mockBackend{
		forkchoiceUpdFn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
			if attrs == nil {
				t.Fatal("expected non-nil attributes")
			}
			if len(attrs.Withdrawals) != 2 {
				t.Errorf("expected 2 withdrawals, got %d", len(attrs.Withdrawals))
			}
			if attrs.Withdrawals[0].Amount != 32000000000 {
				t.Errorf("expected withdrawal amount 32000000000, got %d", attrs.Withdrawals[0].Amount)
			}
			return ForkchoiceUpdatedResult{
				PayloadStatus: PayloadStatusV1{Status: StatusValid},
				PayloadID:     &expectedID,
			}, nil
		},
	}
	api := NewEngineAPI(backend)

	state := ForkchoiceStateV1{
		HeadBlockHash:      types.HexToHash("0x1111"),
		SafeBlockHash:      types.HexToHash("0x2222"),
		FinalizedBlockHash: types.HexToHash("0x3333"),
	}
	attrs := PayloadAttributesV3{
		PayloadAttributesV2: PayloadAttributesV2{
			PayloadAttributesV1: PayloadAttributesV1{
				Timestamp:             1700000012,
				PrevRandao:            types.HexToHash("0xaaaa"),
				SuggestedFeeRecipient: types.HexToAddress("0xbbbb"),
			},
			Withdrawals: []*Withdrawal{
				{Index: 1, ValidatorIndex: 100, Address: types.HexToAddress("0xdead"), Amount: 32000000000},
				{Index: 2, ValidatorIndex: 200, Address: types.HexToAddress("0xbeef"), Amount: 16000000000},
			},
		},
		ParentBeaconBlockRoot: types.HexToHash("0xcccc"),
	}

	stateJSON, _ := json.Marshal(state)
	attrsJSON, _ := json.Marshal(attrs)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s,%s],"id":1}`,
		stateJSON, attrsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var result ForkchoiceUpdatedResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.PayloadID == nil {
		t.Fatal("expected non-nil payload ID")
	}
}

// --- getPayloadV3/V4 response format tests ---

func TestGetPayloadV3_ResponseFormat(t *testing.T) {
	// V3 response should have: executionPayload, blockValue, blobsBundle, shouldOverrideBuilder.
	// It should NOT have executionRequests.
	payloadID := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	backend := &mockBackend{
		getPayloadByIDFn: func(id PayloadID) (*GetPayloadResponse, error) {
			return &GetPayloadResponse{
				ExecutionPayload: &ExecutionPayloadV4{
					ExecutionPayloadV3: ExecutionPayloadV3{
						ExecutionPayloadV2: ExecutionPayloadV2{
							ExecutionPayloadV1: ExecutionPayloadV1{
								BlockNumber:   100,
								GasLimit:      30_000_000,
								BaseFeePerGas: big.NewInt(1_000_000_000),
							},
						},
						BlobGasUsed:   131072,
						ExcessBlobGas: 0,
					},
				},
				BlockValue: big.NewInt(5_000_000),
				BlobsBundle: &BlobsBundleV1{
					Commitments: [][]byte{func() []byte { c := make([]byte, 48); c[0] = 0x01; return c }()},
					Proofs:      [][]byte{make([]byte, 48)},
					Blobs:       [][]byte{func() []byte { b := make([]byte, 131072); b[0] = 0x01; return b }()},
				},
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	idJSON, _ := json.Marshal(payloadID)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getPayloadV3","params":[%s],"id":1}`, idJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var result GetPayloadV3Response
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.ExecutionPayload == nil {
		t.Fatal("expected non-nil executionPayload")
	}
	if result.BlockValue == nil || result.BlockValue.Int64() != 5_000_000 {
		t.Errorf("expected blockValue 5000000, got %v", result.BlockValue)
	}
	if result.BlobsBundle == nil {
		t.Fatal("expected non-nil blobsBundle")
	}
	if len(result.BlobsBundle.Commitments) != 1 {
		t.Errorf("expected 1 commitment, got %d", len(result.BlobsBundle.Commitments))
	}
	if len(result.BlobsBundle.Proofs) != 1 {
		t.Errorf("expected 1 proof, got %d", len(result.BlobsBundle.Proofs))
	}
	if len(result.BlobsBundle.Blobs) != 1 {
		t.Errorf("expected 1 blob, got %d", len(result.BlobsBundle.Blobs))
	}
	if result.ExecutionPayload.BlobGasUsed != 131072 {
		t.Errorf("expected blobGasUsed 131072, got %d", result.ExecutionPayload.BlobGasUsed)
	}
}

func TestGetPayloadV4_ResponseFormat(t *testing.T) {
	// V4 response should have: executionPayload, blockValue, blobsBundle, shouldOverrideBuilder, executionRequests.
	payloadID := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	backend := &mockBackend{
		getPayloadV4ByIDFn: func(id PayloadID) (*GetPayloadV4Response, error) {
			return &GetPayloadV4Response{
				ExecutionPayload: &ExecutionPayloadV3{
					ExecutionPayloadV2: ExecutionPayloadV2{
						ExecutionPayloadV1: ExecutionPayloadV1{
							BlockNumber:   100,
							GasLimit:      30_000_000,
							Timestamp:     1700000012,
							BaseFeePerGas: big.NewInt(1_000_000_000),
						},
					},
					BlobGasUsed:   131072,
					ExcessBlobGas: 0,
				},
				BlockValue: big.NewInt(10_000_000),
				BlobsBundle: &BlobsBundleV1{
					Commitments: [][]byte{func() []byte { c := make([]byte, 48); c[0] = 0x01; return c }()},
					Proofs:      [][]byte{make([]byte, 48)},
					Blobs:       [][]byte{func() []byte { b := make([]byte, 131072); b[0] = 0x01; return b }()},
				},
				ExecutionRequests: [][]byte{{0x00, 0x01}, {0x01, 0x02}},
			}, nil
		},
		isPragueFn: func(timestamp uint64) bool {
			return true
		},
	}

	api := NewEngineAPI(backend)

	idJSON, _ := json.Marshal(payloadID)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getPayloadV4","params":[%s],"id":1}`, idJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var result GetPayloadV4Response
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.ExecutionPayload == nil {
		t.Fatal("expected non-nil executionPayload")
	}
	if result.BlockValue == nil || result.BlockValue.Int64() != 10_000_000 {
		t.Errorf("expected blockValue 10000000, got %v", result.BlockValue)
	}
	if result.BlobsBundle == nil {
		t.Fatal("expected non-nil blobsBundle")
	}
	if len(result.ExecutionRequests) != 2 {
		t.Errorf("expected 2 execution requests, got %d", len(result.ExecutionRequests))
	}
}

// --- PayloadStatusV1 proper response codes tests ---

func TestPayloadStatus_InvalidBlockHash(t *testing.T) {
	if StatusInvalidBlockHash != "INVALID_BLOCK_HASH" {
		t.Errorf("StatusInvalidBlockHash = %s, want INVALID_BLOCK_HASH", StatusInvalidBlockHash)
	}
}

func TestProcessBlock_InvalidBlockHash(t *testing.T) {
	// Verify INVALID_BLOCK_HASH is returned when blockHash doesn't match.
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(coreconfig.TestConfig, statedb, genesis)
	genesisHash := genesis.Hash()

	// Create a payload with a fake block hash that won't match the computed hash.
	payload := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    genesisHash,
				FeeRecipient:  types.HexToAddress("0xfee"),
				PrevRandao:    types.HexToHash("0xrandao"),
				BlockNumber:   1,
				GasLimit:      30_000_000,
				Timestamp:     1700000012,
				BaseFeePerGas: big.NewInt(875_000_000),
				// Set a fake block hash that will not match the computed hash.
				BlockHash:    types.HexToHash("0xfakeblockfakeblockfakeblockfakeblockfakeblockfakeblockfakeblock"),
				Transactions: [][]byte{},
			},
			Withdrawals: []*Withdrawal{},
		},
	}

	status, err := b.ProcessBlock(payload, nil, types.HexToHash("0xbeacon"))
	if err != nil {
		t.Fatalf("ProcessBlock returned error: %v", err)
	}
	if status.Status != StatusInvalidBlockHash {
		t.Errorf("expected INVALID_BLOCK_HASH status, got %s", status.Status)
	}
	if status.ValidationError == nil {
		t.Error("expected validationError to be set")
	}
}

func TestProcessBlock_TimestampValidation(t *testing.T) {
	// Verify that a block with timestamp <= parent timestamp is rejected.
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(coreconfig.TestConfig, statedb, genesis)
	genesisHash := genesis.Hash()

	// Create a payload with the same timestamp as genesis (1700000000).
	payload := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    genesisHash,
				FeeRecipient:  types.HexToAddress("0xfee"),
				PrevRandao:    types.HexToHash("0xrandao"),
				BlockNumber:   1,
				GasLimit:      30_000_000,
				Timestamp:     1700000000, // same as genesis
				BaseFeePerGas: big.NewInt(875_000_000),
				Transactions:  [][]byte{},
			},
			Withdrawals: []*Withdrawal{},
		},
	}

	status, err := b.ProcessBlock(payload, nil, types.HexToHash("0xbeacon"))
	if err != nil {
		t.Fatalf("ProcessBlock returned error: %v", err)
	}
	if status.Status != StatusInvalid {
		t.Errorf("expected INVALID status for non-progressing timestamp, got %s", status.Status)
	}
	if status.ValidationError == nil {
		t.Error("expected validationError to be set")
	}
}

// --- Engine API error codes tests ---

func TestAllErrorCodes(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		expected int
	}{
		{"ParseError", ParseErrorCode, -32700},
		{"InvalidRequest", InvalidRequestCode, -32600},
		{"MethodNotFound", MethodNotFoundCode, -32601},
		{"InvalidParams", InvalidParamsCode, -32602},
		{"InternalError", InternalErrorCode, -32603},
		{"UnknownPayload", UnknownPayloadCode, -38001},
		{"InvalidForkchoiceState", InvalidForkchoiceStateCode, -38002},
		{"InvalidPayloadAttribute", InvalidPayloadAttributeCode, -38003},
		{"TooLargeRequest", TooLargeRequestCode, -38004},
		{"UnsupportedFork", UnsupportedForkCode, -38005},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.code != tt.expected {
				t.Errorf("%s = %d, want %d", tt.name, tt.code, tt.expected)
			}
		})
	}
}

func TestErrorMapping_AllCases(t *testing.T) {
	tests := []struct {
		err      error
		wantCode int
	}{
		{ErrUnknownPayload, UnknownPayloadCode},
		{ErrInvalidForkchoiceState, InvalidForkchoiceStateCode},
		{ErrInvalidPayloadAttributes, InvalidPayloadAttributeCode},
		{ErrInvalidParams, InvalidParamsCode},
		{ErrTooLargeRequest, TooLargeRequestCode},
		{ErrUnsupportedFork, UnsupportedForkCode},
	}

	for _, tt := range tests {
		rpcErr := engineErrorToRPC(tt.err)
		if rpcErr.Code != tt.wantCode {
			t.Errorf("engineErrorToRPC(%v): code=%d, want %d", tt.err, rpcErr.Code, tt.wantCode)
		}
	}
}

// --- Backend GetHeadTimestamp + IsCancun tests ---

func TestBackend_GetHeadTimestamp(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(coreconfig.TestConfig, statedb, genesis)

	ts := b.GetHeadTimestamp()
	if ts != genesis.Header().Time {
		t.Errorf("expected head timestamp %d, got %d", genesis.Header().Time, ts)
	}
}

func TestBackend_IsCancun(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(coreconfig.TestConfig, statedb, genesis)

	// TestConfig has Cancun at timestamp 0, so any timestamp should be Cancun.
	if !b.IsCancun(1700000000) {
		t.Error("expected IsCancun to return true with TestConfig")
	}
}

func TestBackend_ProcessBlockV4(t *testing.T) {
	cfg := *coreconfig.TestConfig
	cfg.AmsterdamTime = nil
	backend, api := makeE2EBackend(t, &cfg)
	t.Cleanup(backend.Close)
	built := buildNextPayload(t, api, backend)

	status, err := backend.ProcessBlockV4(&built.ExecutionPayloadV3, nil, types.Hash{}, [][]byte{{0, 1}})
	if err != nil || status.Status != StatusInvalid || status.LatestValidHash != nil {
		t.Fatalf("payload with a changed requests commitment: %+v: %v", status, err)
	}
	status, err = backend.ProcessBlockV4(&built.ExecutionPayloadV3, nil, types.Hash{}, [][]byte{})
	if err != nil || status.Status != StatusValid || status.LatestValidHash == nil || *status.LatestValidHash != built.BlockHash {
		t.Fatalf("valid Prague payload: %+v: %v", status, err)
	}
}

func TestForkchoiceUpdated_TimestampValidation_RealBackend(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(coreconfig.TestConfig, statedb, genesis)
	genesisHash := genesis.Hash()

	// Try to build a payload with timestamp equal to genesis (should fail).
	attrs := &PayloadAttributesV3{
		PayloadAttributesV2: PayloadAttributesV2{
			PayloadAttributesV1: PayloadAttributesV1{
				Timestamp:             1700000000, // same as genesis
				PrevRandao:            types.HexToHash("0xrandao"),
				SuggestedFeeRecipient: types.HexToAddress("0xfee"),
			},
			Withdrawals: []*Withdrawal{},
		},
		ParentBeaconBlockRoot: types.HexToHash("0xbeacon"),
	}

	_, err := b.ForkchoiceUpdated(
		ForkchoiceStateV1{
			HeadBlockHash:      genesisHash,
			SafeBlockHash:      genesisHash,
			FinalizedBlockHash: genesisHash,
		},
		attrs,
	)
	if err != ErrInvalidPayloadAttributes {
		t.Errorf("expected ErrInvalidPayloadAttributes for non-progressing timestamp, got %v", err)
	}
}

// --- Full round-trip test: forkchoice -> getPayload with response format ---

func TestFullRoundtrip_ForkchoiceToGetPayload(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	b := NewEngineBackend(coreconfig.TestConfig, statedb, genesis)
	genesisHash := genesis.Hash()

	// Step 1: forkchoiceUpdated with attributes to trigger payload build.
	attrs := &PayloadAttributesV3{
		PayloadAttributesV2: PayloadAttributesV2{
			PayloadAttributesV1: PayloadAttributesV1{
				Timestamp:             1700000012,
				PrevRandao:            types.HexToHash("0xrandao"),
				SuggestedFeeRecipient: types.HexToAddress("0xfee"),
			},
			Withdrawals: []*Withdrawal{
				{Index: 1, ValidatorIndex: 100, Address: types.HexToAddress("0xdead"), Amount: 32000000000},
			},
		},
		ParentBeaconBlockRoot: types.HexToHash("0xbeacon"),
	}

	fcResult, err := b.ForkchoiceUpdated(
		ForkchoiceStateV1{
			HeadBlockHash:      genesisHash,
			SafeBlockHash:      genesisHash,
			FinalizedBlockHash: genesisHash,
		},
		attrs,
	)
	if err != nil {
		t.Fatalf("ForkchoiceUpdated error: %v", err)
	}
	if fcResult.PayloadStatus.Status != StatusValid {
		t.Fatalf("expected VALID, got %s", fcResult.PayloadStatus.Status)
	}
	if fcResult.PayloadID == nil {
		t.Fatal("expected non-nil PayloadID")
	}

	// Step 2: getPayloadV4 to retrieve the built payload.
	resp, err := b.GetPayloadV4ByID(*fcResult.PayloadID)
	if err != nil {
		t.Fatalf("GetPayloadV4ByID error: %v", err)
	}

	// Verify response format.
	if resp.ExecutionPayload == nil {
		t.Fatal("expected non-nil executionPayload")
	}
	if resp.ExecutionPayload.BlockNumber != 1 {
		t.Errorf("expected block number 1, got %d", resp.ExecutionPayload.BlockNumber)
	}
	if resp.ExecutionPayload.Timestamp != 1700000012 {
		t.Errorf("expected timestamp 1700000012, got %d", resp.ExecutionPayload.Timestamp)
	}
	if resp.BlockValue == nil {
		t.Fatal("expected non-nil blockValue")
	}
	// Empty block (no transactions) should have zero block value.
	if resp.BlockValue.Sign() != 0 {
		t.Errorf("expected zero block value for empty block, got %s", resp.BlockValue)
	}
	if resp.BlobsBundle == nil {
		t.Fatal("expected non-nil blobsBundle")
	}
	if resp.ExecutionRequests == nil {
		t.Fatal("expected non-nil executionRequests")
	}
}

// --- Blob versioned hash validation tests ---

func TestValidateBlobHashes_EmptyPayloadEmptyExpected(t *testing.T) {
	// No blob transactions, empty expected list => valid.
	payload := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				Transactions: [][]byte{},
			},
		},
	}
	err := validateBlobHashes(payload, []types.Hash{})
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
}

func TestValidateBlobHashes_MismatchedCount(t *testing.T) {
	// No blob transactions but expected hashes provided => invalid.
	payload := &ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				Transactions: [][]byte{},
			},
		},
	}
	err := validateBlobHashes(payload, []types.Hash{types.HexToHash("0x01")})
	if err == nil {
		t.Fatal("expected error for mismatched count")
	}
}
