package engine

import (
	"encoding/json"
	"fmt"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// ---------- NewPayload: invalid parent hash ----------

func TestNewPayloadInvalidParentHash(t *testing.T) {
	// Backend returns SYNCING for unknown parent.
	backend := &mockBackend{
		processBlockFn: func(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash) (PayloadStatusV1, error) {
			return PayloadStatusV1{Status: StatusSyncing}, nil
		},
	}

	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(types.HexToHash("0xabc123"), 50)
	payload.ParentHash = types.HexToHash("0xdeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddeaddead0001")
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
	if rpcResp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var status PayloadStatusV1
	if err := json.Unmarshal(resultJSON, &status); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if status.Status != StatusSyncing {
		t.Errorf("expected SYNCING for unknown parent, got %s", status.Status)
	}
}

// ---------- NewPayload: invalid block hash ----------

func TestNewPayloadInvalidBlockHash(t *testing.T) {
	// Backend returns INVALID_BLOCK_HASH when block hash doesn't match.
	errMsg := "block hash mismatch"
	backend := &mockBackend{
		processBlockFn: func(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash) (PayloadStatusV1, error) {
			return PayloadStatusV1{
				Status:          StatusInvalidBlockHash,
				ValidationError: &errMsg,
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(types.HexToHash("0xfakeblockfakeblockfakeblockfakeblockfakeblockfakeblockfakeblock"), 100)
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
	if rpcResp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var status PayloadStatusV1
	if err := json.Unmarshal(resultJSON, &status); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if status.Status != StatusInvalidBlockHash {
		t.Errorf("expected INVALID_BLOCK_HASH, got %s", status.Status)
	}
	if status.ValidationError == nil || *status.ValidationError != errMsg {
		t.Errorf("expected validation error %q", errMsg)
	}
}

// ---------- NewPayload: invalid timestamp ----------

func TestNewPayloadInvalidTimestamp(t *testing.T) {
	// Backend returns INVALID for a payload with timestamp <= parent.
	errMsg := "timestamp not progressing"
	backend := &mockBackend{
		processBlockFn: func(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash) (PayloadStatusV1, error) {
			return PayloadStatusV1{
				Status:          StatusInvalid,
				ValidationError: &errMsg,
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(types.Hash{}, 100)
	payload.Timestamp = 1699999999 // before parent
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
	if rpcResp.Error != nil {
		t.Fatalf("unexpected JSON-RPC error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var status PayloadStatusV1
	if err := json.Unmarshal(resultJSON, &status); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if status.Status != StatusInvalid {
		t.Errorf("expected INVALID for bad timestamp, got %s", status.Status)
	}
}

// ---------- NewPayload: valid empty payload ----------

func TestNewPayloadValidEmpty(t *testing.T) {
	blockHash := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	backend := &mockBackend{
		processBlockFn: func(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash) (PayloadStatusV1, error) {
			if len(payload.Transactions) != 0 {
				t.Errorf("expected 0 transactions, got %d", len(payload.Transactions))
			}
			latestValid := payload.BlockHash
			return PayloadStatusV1{
				Status:          StatusValid,
				LatestValidHash: &latestValid,
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(blockHash, 100)
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
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var status PayloadStatusV1
	if err := json.Unmarshal(resultJSON, &status); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if status.Status != StatusValid {
		t.Errorf("expected VALID, got %s", status.Status)
	}
	if status.LatestValidHash == nil {
		t.Error("expected latestValidHash to be set")
	}
}

// ---------- ForkchoiceUpdated: valid with head/safe/finalized ----------

func TestForkchoiceUpdatedValid(t *testing.T) {
	headHash := types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111")
	safeHash := types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222")
	finalHash := types.HexToHash("0x3333333333333333333333333333333333333333333333333333333333333333")

	backend := &mockBackend{
		forkchoiceUpdFn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
			if state.HeadBlockHash != headHash {
				t.Errorf("unexpected head hash")
			}
			if state.SafeBlockHash != safeHash {
				t.Errorf("unexpected safe hash")
			}
			if state.FinalizedBlockHash != finalHash {
				t.Errorf("unexpected finalized hash")
			}
			return ForkchoiceUpdatedResult{
				PayloadStatus: PayloadStatusV1{
					Status:          StatusValid,
					LatestValidHash: &headHash,
				},
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	state := ForkchoiceStateV1{
		HeadBlockHash:      headHash,
		SafeBlockHash:      safeHash,
		FinalizedBlockHash: finalHash,
	}
	stateJSON, _ := json.Marshal(state)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s,null],"id":1}`, stateJSON)

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
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID, got %s", result.PayloadStatus.Status)
	}
	if result.PayloadID != nil {
		t.Error("expected nil PayloadID without attributes")
	}
}

// ---------- ForkchoiceUpdated: invalid/unknown head ----------

func TestForkchoiceUpdatedInvalidHead(t *testing.T) {
	backend := &mockBackend{
		forkchoiceUpdFn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
			// Return SYNCING for unknown head.
			return ForkchoiceUpdatedResult{
				PayloadStatus: PayloadStatusV1{Status: StatusSyncing},
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	unknownHead := types.HexToHash("0xdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef")
	state := ForkchoiceStateV1{
		HeadBlockHash:      unknownHead,
		SafeBlockHash:      types.HexToHash("0x2222"),
		FinalizedBlockHash: types.HexToHash("0x3333"),
	}
	stateJSON, _ := json.Marshal(state)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s,null],"id":1}`, stateJSON)

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
	if result.PayloadStatus.Status != StatusSyncing {
		t.Errorf("expected SYNCING for unknown head, got %s", result.PayloadStatus.Status)
	}
}

// ---------- GetPayload: unknown ID ----------

func TestGetPayloadUnknownID(t *testing.T) {
	// Default mock returns ErrUnknownPayload.
	api := NewEngineAPI(&mockBackend{})

	unknownID := PayloadID{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	idJSON, _ := json.Marshal(unknownID)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getPayloadV3","params":[%s],"id":1}`, idJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for unknown payload ID")
	}
	if rpcResp.Error.Code != UnknownPayloadCode {
		t.Errorf("expected error code %d, got %d", UnknownPayloadCode, rpcResp.Error.Code)
	}
}

func TestGetPayloadV4UnknownID(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	unknownID := PayloadID{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00, 0x11}
	idJSON, _ := json.Marshal(unknownID)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getPayloadV4","params":[%s],"id":1}`, idJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for unknown payload ID")
	}
	if rpcResp.Error.Code != UnknownPayloadCode {
		t.Errorf("expected error code %d, got %d", UnknownPayloadCode, rpcResp.Error.Code)
	}
}

func TestGetPayloadV6UnknownID(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	unknownID := PayloadID{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88}
	idJSON, _ := json.Marshal(unknownID)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getPayloadV6","params":[%s],"id":1}`, idJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for unknown payload ID")
	}
	if rpcResp.Error.Code != UnknownPayloadCode {
		t.Errorf("expected error code %d, got %d", UnknownPayloadCode, rpcResp.Error.Code)
	}
}

// ---------- ExchangeCapabilities ----------

func TestExchangeCapabilities_ReturnsSupportedMethods(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	requestedJSON, _ := json.Marshal([]string{
		"engine_newPayloadV3",
		"engine_forkchoiceUpdatedV3",
		"engine_getPayloadV3",
	})
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_exchangeCapabilities","params":[%s],"id":1}`, requestedJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var capabilities []string
	if err := json.Unmarshal(resultJSON, &capabilities); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if len(capabilities) == 0 {
		t.Fatal("expected non-empty capabilities list")
	}

	// Verify all expected methods are present.
	found := make(map[string]bool)
	for _, c := range capabilities {
		found[c] = true
	}

	expected := []string{
		"engine_newPayloadV3",
		"engine_newPayloadV4",
		"engine_newPayloadV5",
		"engine_forkchoiceUpdatedV3",
		"engine_forkchoiceUpdatedV4",
		"engine_getPayloadV3",
		"engine_getPayloadV4",
		"engine_getPayloadV6",
		"engine_exchangeCapabilities",
		"engine_getClientVersionV1",
	}

	for _, want := range expected {
		if !found[want] {
			t.Errorf("expected capability %q in response", want)
		}
	}
}

func TestExchangeCapabilities_EmptyRequest(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	req := `{"jsonrpc":"2.0","method":"engine_exchangeCapabilities","params":[[]],"id":1}`
	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var capabilities []string
	if err := json.Unmarshal(resultJSON, &capabilities); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	// Even with empty request, should still return our supported capabilities.
	if len(capabilities) == 0 {
		t.Fatal("expected non-empty capabilities even with empty request")
	}
}

// ---------- NewPayloadV4: valid with execution requests ----------

func TestNewPayloadV4_ValidWithRequests(t *testing.T) {
	blockHash := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	backend := &mockBackend{
		processBlockV4Fn: func(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash, requests [][]byte) (PayloadStatusV1, error) {
			if len(requests) != 2 {
				t.Errorf("expected 2 execution requests, got %d", len(requests))
			}
			latestValid := payload.BlockHash
			return PayloadStatusV1{
				Status:          StatusValid,
				LatestValidHash: &latestValid,
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(blockHash, 200)
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)
	requestsJSON, _ := json.Marshal([]string{"0x000102", "0x010304"})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV4","params":[%s,%s,%s,%s],"id":1}`,
		payloadJSON, hashesJSON, rootJSON, requestsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var status PayloadStatusV1
	if err := json.Unmarshal(resultJSON, &status); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if status.Status != StatusValid {
		t.Errorf("expected VALID, got %s", status.Status)
	}
}

// ---------- ForkchoiceUpdatedV3 with payload attributes and timestamp ----------

func TestForkchoiceUpdatedV3_ValidWithAttributes(t *testing.T) {
	expectedID := PayloadID{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x00, 0x11}
	backend := &mockBackend{
		forkchoiceUpdFn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
			if attrs == nil {
				t.Fatal("expected non-nil payload attributes")
			}
			if attrs.Timestamp != 1700000012 {
				t.Errorf("expected timestamp 1700000012, got %d", attrs.Timestamp)
			}
			if attrs.ParentBeaconBlockRoot == (types.Hash{}) {
				t.Error("expected non-zero parent beacon block root")
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
			Withdrawals: []*Withdrawal{},
		},
		ParentBeaconBlockRoot: types.HexToHash("0xcccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"),
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
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID, got %s", result.PayloadStatus.Status)
	}
	if result.PayloadID == nil {
		t.Fatal("expected non-nil PayloadID")
	}
	if *result.PayloadID != expectedID {
		t.Errorf("payload ID mismatch: got %s, want %s", result.PayloadID.String(), expectedID.String())
	}
}

// ---------- ForkchoiceUpdatedV4 ----------

func TestForkchoiceUpdatedV4_Valid(t *testing.T) {
	headHash := types.HexToHash("0x1111")
	backend := &mockBackend{
		forkchoiceUpdV4Fn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV4) (ForkchoiceUpdatedResult, error) {
			if state.HeadBlockHash != headHash {
				t.Errorf("unexpected head hash")
			}
			return ForkchoiceUpdatedResult{
				PayloadStatus: PayloadStatusV1{
					Status:          StatusValid,
					LatestValidHash: &headHash,
				},
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	state := ForkchoiceStateV1{
		HeadBlockHash:      headHash,
		SafeBlockHash:      types.HexToHash("0x2222"),
		FinalizedBlockHash: types.HexToHash("0x3333"),
	}
	stateJSON, _ := json.Marshal(state)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV4","params":[%s,null],"id":1}`, stateJSON)

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
	if result.PayloadStatus.Status != StatusValid {
		t.Errorf("expected VALID, got %s", result.PayloadStatus.Status)
	}
}

// ---------- NewPayloadV5: Amsterdam payload ----------

func TestNewPayloadV5_Valid(t *testing.T) {
	blockHash := types.HexToHash("0xabcdef")
	backend := &mockBackend{
		processBlockV5Fn: func(payload *ExecutionPayloadV5, hashes []types.Hash, root types.Hash, requests [][]byte) (PayloadStatusV1, error) {
			if payload.BlockAccessList == nil {
				t.Error("expected non-nil block access list")
			}
			latestValid := payload.BlockHash
			return PayloadStatusV1{
				Status:          StatusValid,
				LatestValidHash: &latestValid,
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	bal := json.RawMessage(`[{"address":"0xaaaa","storageKeys":["0x01"]}]`)
	payload := ExecutionPayloadV5{
		ExecutionPayloadV4: ExecutionPayloadV4{
			ExecutionPayloadV3: makeTestPayloadV3(blockHash, 300),
		},
		BlockAccessList: bal,
	}
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)
	requestsJSON, _ := json.Marshal([]string{"0x00"})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV5","params":[%s,%s,%s,%s],"id":1}`,
		payloadJSON, hashesJSON, rootJSON, requestsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var status PayloadStatusV1
	if err := json.Unmarshal(resultJSON, &status); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if status.Status != StatusValid {
		t.Errorf("expected VALID, got %s", status.Status)
	}
}

func TestNewPayloadV5_MissingBAL_Comprehensive(t *testing.T) {
	backend := &mockBackend{}
	api := NewEngineAPI(backend)

	payload := ExecutionPayloadV5{
		ExecutionPayloadV4: ExecutionPayloadV4{
			ExecutionPayloadV3: makeTestPayloadV3(types.Hash{}, 300),
		},
		BlockAccessList: nil, // missing BAL
	}
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)
	requestsJSON, _ := json.Marshal([][]byte{})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV5","params":[%s,%s,%s,%s],"id":1}`,
		payloadJSON, hashesJSON, rootJSON, requestsJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for missing BAL")
	}
	if rpcResp.Error.Code != InvalidParamsCode {
		t.Errorf("expected error code %d, got %d", InvalidParamsCode, rpcResp.Error.Code)
	}
}

// ---------- GetPayload valid retrieval ----------

func TestGetPayloadV3_ValidRetrieval(t *testing.T) {
	payloadID := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	backend := &mockBackend{
		getPayloadByIDFn: func(id PayloadID) (*GetPayloadResponse, error) {
			if id != payloadID {
				t.Errorf("unexpected payload ID")
			}
			return &GetPayloadResponse{
				ExecutionPayload: &ExecutionPayloadV4{
					ExecutionPayloadV3: ExecutionPayloadV3{
						ExecutionPayloadV2: ExecutionPayloadV2{
							ExecutionPayloadV1: ExecutionPayloadV1{
								BlockNumber:   50,
								GasLimit:      30000000,
								Timestamp:     1700000012,
								BaseFeePerGas: big.NewInt(1000000000),
							},
						},
					},
				},
				BlockValue:  big.NewInt(500000),
				BlobsBundle: &BlobsBundleV1{},
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
		t.Fatal("expected non-nil execution payload")
	}
	if result.ExecutionPayload.BlockNumber != 50 {
		t.Errorf("expected block number 50, got %d", result.ExecutionPayload.BlockNumber)
	}
	if result.BlockValue == nil || result.BlockValue.Int64() != 500000 {
		t.Errorf("expected blockValue 500000, got %v", result.BlockValue)
	}
}

// ---------- Client version ----------

func TestGetClientVersionV1_ReturnsVersion(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	peerVersion := ClientVersionV1{Code: "GE", Name: "geth", Version: "1.15.0", Commit: "def456"}
	peerJSON, _ := json.Marshal(peerVersion)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getClientVersionV1","params":[%s],"id":1}`, peerJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: code=%d msg=%s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var versions []ClientVersionV1
	if err := json.Unmarshal(resultJSON, &versions); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version, got %d", len(versions))
	}
	if versions[0].Code != "ET" {
		t.Errorf("expected code ET, got %s", versions[0].Code)
	}
	if versions[0].Name != "ETH2030" {
		t.Errorf("expected name ETH2030, got %s", versions[0].Name)
	}
	if versions[0].Version != "v0.1.0" {
		t.Errorf("expected version v0.1.0, got %s", versions[0].Version)
	}
}

// ---------- ForkchoiceUpdated: invalid forkchoice state error ----------

func TestForkchoiceUpdatedV3_InvalidState_Comprehensive(t *testing.T) {
	backend := &mockBackend{
		forkchoiceUpdFn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
			return ForkchoiceUpdatedResult{}, ErrInvalidForkchoiceState
		},
	}

	api := NewEngineAPI(backend)

	state := ForkchoiceStateV1{}
	stateJSON, _ := json.Marshal(state)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s,null],"id":1}`, stateJSON)
	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for invalid forkchoice state")
	}
	if rpcResp.Error.Code != InvalidForkchoiceStateCode {
		t.Errorf("expected error code %d, got %d", InvalidForkchoiceStateCode, rpcResp.Error.Code)
	}
}

// ---------- NewPayloadV5: unsupported fork ----------

func TestNewPayloadV5_UnsupportedFork_Comprehensive(t *testing.T) {
	backend := &mockBackend{
		isAmsterdamFn: func(timestamp uint64) bool {
			return false // Amsterdam NOT active
		},
	}
	api := NewEngineAPI(backend)

	bal := json.RawMessage(`[]`)
	payload := ExecutionPayloadV5{
		ExecutionPayloadV4: ExecutionPayloadV4{
			ExecutionPayloadV3: makeTestPayloadV3(types.Hash{}, 100),
		},
		BlockAccessList: bal,
	}
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)
	requestsJSON, _ := json.Marshal([][]byte{})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV5","params":[%s,%s,%s,%s],"id":1}`,
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
