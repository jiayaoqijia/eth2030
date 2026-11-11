package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/golang-jwt/jwt/v4"
)

func TestEngineJWTAuthentication(t *testing.T) {
	api := NewEngineAPI(nil)
	secret := [32]byte{1}
	api.SetJWTSecret(secret)
	now := time.Now()
	for _, tc := range []struct {
		name   string
		issued time.Time
		method jwt.SigningMethod
		want   int
	}{
		{"valid", now, jwt.SigningMethodHS256, http.StatusMethodNotAllowed},
		{"stale", now.Add(-2 * time.Minute), jwt.SigningMethodHS256, http.StatusUnauthorized},
		{"other algorithm", now, jwt.SigningMethodHS384, http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token, err := jwt.NewWithClaims(tc.method, jwt.RegisteredClaims{IssuedAt: jwt.NewNumericDate(tc.issued)}).SignedString(secret[:])
			if err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			res := httptest.NewRecorder()
			api.httpHandler(res, req)
			if res.Code != tc.want {
				t.Fatalf("HTTP %d, want %d", res.Code, tc.want)
			}
		})
	}
}

// mockBackend is a test implementation of the Backend interface.
type mockBackend struct {
	processBlockFn     func(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash) (PayloadStatusV1, error)
	processBlockV4Fn   func(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash, requests [][]byte) (PayloadStatusV1, error)
	processBlockV5Fn   func(payload *ExecutionPayloadV5, hashes []types.Hash, root types.Hash, requests [][]byte) (PayloadStatusV1, error)
	forkchoiceUpdFn    func(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error)
	forkchoiceUpdV4Fn  func(state ForkchoiceStateV1, attrs *PayloadAttributesV4) (ForkchoiceUpdatedResult, error)
	getPayloadByIDFn   func(id PayloadID) (*GetPayloadResponse, error)
	getPayloadV4ByIDFn func(id PayloadID) (*GetPayloadV4Response, error)
	getPayloadV6ByIDFn func(id PayloadID) (*GetPayloadV6Response, error)
	headTimestamp      uint64
	isCancunFn         func(timestamp uint64) bool
	isPragueFn         func(timestamp uint64) bool
	isAmsterdamFn      func(timestamp uint64) bool
}

func (m *mockBackend) ProcessBlock(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash) (PayloadStatusV1, error) {
	if m.processBlockFn != nil {
		return m.processBlockFn(payload, hashes, root)
	}
	return PayloadStatusV1{Status: StatusValid}, nil
}

func (m *mockBackend) ProcessBlockV4(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash, requests [][]byte) (PayloadStatusV1, error) {
	if m.processBlockV4Fn != nil {
		return m.processBlockV4Fn(payload, hashes, root, requests)
	}
	return PayloadStatusV1{Status: StatusValid}, nil
}

func (m *mockBackend) ProcessBlockV5(payload *ExecutionPayloadV5, hashes []types.Hash, root types.Hash, requests [][]byte) (PayloadStatusV1, error) {
	if m.processBlockV5Fn != nil {
		return m.processBlockV5Fn(payload, hashes, root, requests)
	}
	return PayloadStatusV1{Status: StatusValid}, nil
}

func (m *mockBackend) ForkchoiceUpdated(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
	if m.forkchoiceUpdFn != nil {
		return m.forkchoiceUpdFn(state, attrs)
	}
	return ForkchoiceUpdatedResult{
		PayloadStatus: PayloadStatusV1{Status: StatusValid},
	}, nil
}

func (m *mockBackend) ForkchoiceUpdatedV4(state ForkchoiceStateV1, attrs *PayloadAttributesV4) (ForkchoiceUpdatedResult, error) {
	if m.forkchoiceUpdV4Fn != nil {
		return m.forkchoiceUpdV4Fn(state, attrs)
	}
	return ForkchoiceUpdatedResult{
		PayloadStatus: PayloadStatusV1{Status: StatusValid},
	}, nil
}

func (m *mockBackend) GetPayloadByID(id PayloadID) (*GetPayloadResponse, error) {
	if m.getPayloadByIDFn != nil {
		return m.getPayloadByIDFn(id)
	}
	return nil, ErrUnknownPayload
}

func (m *mockBackend) GetPayloadV4ByID(id PayloadID) (*GetPayloadV4Response, error) {
	if m.getPayloadV4ByIDFn != nil {
		return m.getPayloadV4ByIDFn(id)
	}
	return nil, ErrUnknownPayload
}

func (m *mockBackend) GetPayloadV6ByID(id PayloadID) (*GetPayloadV6Response, error) {
	if m.getPayloadV6ByIDFn != nil {
		return m.getPayloadV6ByIDFn(id)
	}
	return nil, ErrUnknownPayload
}

func (m *mockBackend) GetHeadTimestamp() uint64 {
	return m.headTimestamp
}

func (m *mockBackend) GetBlockTimestamp(_ types.Hash) uint64 {
	return 0
}

func (m *mockBackend) IsCancun(timestamp uint64) bool {
	if m.isCancunFn != nil {
		return m.isCancunFn(timestamp)
	}
	return true
}

func (m *mockBackend) IsPrague(timestamp uint64) bool {
	if m.isPragueFn != nil {
		return m.isPragueFn(timestamp)
	}
	return true
}

func (m *mockBackend) IsAmsterdam(timestamp uint64) bool {
	if m.isAmsterdamFn != nil {
		return m.isAmsterdamFn(timestamp)
	}
	return true
}
func (m *mockBackend) GetHeadHash() types.Hash      { return types.Hash{} }
func (m *mockBackend) GetSafeHash() types.Hash      { return types.Hash{} }
func (m *mockBackend) GetFinalizedHash() types.Hash { return types.Hash{} }

// --- Test NewPayloadV3 ---

func TestNewPayloadV3_Valid(t *testing.T) {
	blockHash := types.HexToHash("0xabcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890")
	backend := &mockBackend{
		processBlockFn: func(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash) (PayloadStatusV1, error) {
			if payload.BlockNumber != 100 {
				t.Errorf("expected block number 100, got %d", payload.BlockNumber)
			}
			if payload.GasLimit != 30000000 {
				t.Errorf("expected gas limit 30000000, got %d", payload.GasLimit)
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
		t.Errorf("expected status VALID, got %s", status.Status)
	}
	if status.LatestValidHash == nil {
		t.Error("expected latestValidHash to be set")
	}
}

func TestNewPayloadV3_InvalidPayload(t *testing.T) {
	errMsg := "block hash mismatch"
	backend := &mockBackend{
		processBlockFn: func(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash) (PayloadStatusV1, error) {
			return PayloadStatusV1{
				Status:          StatusInvalid,
				ValidationError: &errMsg,
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(types.Hash{}, 200)
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":[%s,%s,%s],"id":2}`,
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
		t.Errorf("expected status INVALID, got %s", status.Status)
	}
	if status.ValidationError == nil || *status.ValidationError != errMsg {
		t.Errorf("expected validation error %q", errMsg)
	}
}

func TestNewPayloadV3_BackendError(t *testing.T) {
	backend := &mockBackend{
		processBlockFn: func(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash) (PayloadStatusV1, error) {
			return PayloadStatusV1{}, ErrInvalidParams
		},
	}

	api := NewEngineAPI(backend)

	payload := makeTestPayloadV3(types.Hash{}, 1)
	payloadJSON, _ := json.Marshal(payload)
	hashesJSON, _ := json.Marshal([]types.Hash{})
	beaconRoot := types.HexToHash("0xbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeaconbeac")
	rootJSON, _ := json.Marshal(beaconRoot)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":[%s,%s,%s],"id":3}`,
		payloadJSON, hashesJSON, rootJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error response")
	}
	if rpcResp.Error.Code != InvalidParamsCode {
		t.Errorf("expected error code %d, got %d", InvalidParamsCode, rpcResp.Error.Code)
	}
}

func TestNewPayloadV3_WrongParamCount(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	// Only 2 params instead of 3.
	req := `{"jsonrpc":"2.0","method":"engine_newPayloadV3","params":[{},{}],"id":4}`
	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for wrong param count")
	}
	if rpcResp.Error.Code != InvalidParamsCode {
		t.Errorf("expected error code %d, got %d", InvalidParamsCode, rpcResp.Error.Code)
	}
}

// --- Test ForkchoiceUpdatedV3 ---

func TestForkchoiceUpdatedV3_WithoutAttributes(t *testing.T) {
	headHash := types.HexToHash("0x1111")
	backend := &mockBackend{
		forkchoiceUpdFn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
			if state.HeadBlockHash != headHash {
				t.Errorf("unexpected head hash")
			}
			if attrs != nil {
				t.Error("expected nil payload attributes")
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

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s,null],"id":5}`, stateJSON)

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
		t.Errorf("expected status VALID, got %s", result.PayloadStatus.Status)
	}
	if result.PayloadID != nil {
		t.Error("expected nil payload ID without attributes")
	}
}

func TestForkchoiceUpdatedV3_WithAttributes(t *testing.T) {
	expectedID := PayloadID{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	backend := &mockBackend{
		forkchoiceUpdFn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
			if attrs == nil {
				t.Fatal("expected non-nil payload attributes")
			}
			if attrs.Timestamp != 1700000000 {
				t.Errorf("expected timestamp 1700000000, got %d", attrs.Timestamp)
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
				Timestamp:             1700000000,
				PrevRandao:            types.HexToHash("0xaaaa"),
				SuggestedFeeRecipient: types.HexToAddress("0xbbbb"),
			},
			Withdrawals: []*Withdrawal{},
		},
		ParentBeaconBlockRoot: types.HexToHash("0xcccc"),
	}

	stateJSON, _ := json.Marshal(state)
	attrsJSON, _ := json.Marshal(attrs)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s,%s],"id":6}`,
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
		t.Errorf("expected status VALID, got %s", result.PayloadStatus.Status)
	}
	if result.PayloadID == nil {
		t.Fatal("expected non-nil payload ID")
	}
	if *result.PayloadID != expectedID {
		t.Errorf("payload ID mismatch: got %s, want %s", result.PayloadID.String(), expectedID.String())
	}
}

func TestForkchoiceUpdatedV3_InvalidState(t *testing.T) {
	backend := &mockBackend{
		forkchoiceUpdFn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
			return ForkchoiceUpdatedResult{}, ErrInvalidForkchoiceState
		},
	}

	api := NewEngineAPI(backend)

	state := ForkchoiceStateV1{}
	stateJSON, _ := json.Marshal(state)

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s,null],"id":7}`, stateJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error response")
	}
	if rpcResp.Error.Code != InvalidForkchoiceStateCode {
		t.Errorf("expected error code %d, got %d", InvalidForkchoiceStateCode, rpcResp.Error.Code)
	}
}

// --- Test GetPayloadV3 ---

func TestGetPayloadV3_Valid(t *testing.T) {
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
								BlockNumber:   100,
								GasLimit:      30000000,
								BaseFeePerGas: big.NewInt(1000000000),
							},
						},
					},
				},
				BlockValue:  big.NewInt(1000000),
				BlobsBundle: &BlobsBundleV1{},
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	idJSON, _ := json.Marshal(payloadID)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getPayloadV3","params":[%s],"id":8}`, idJSON)

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
	if result.ExecutionPayload.BlockNumber != 100 {
		t.Errorf("expected block number 100, got %d", result.ExecutionPayload.BlockNumber)
	}
	if result.BlobsBundle == nil {
		t.Error("expected non-nil blobsBundle in V3 response")
	}
}

func TestGetPayloadV3_UnknownPayload(t *testing.T) {
	api := NewEngineAPI(&mockBackend{}) // default returns ErrUnknownPayload

	idJSON, _ := json.Marshal(PayloadID{0xff})
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getPayloadV3","params":[%s],"id":9}`, idJSON)

	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error response")
	}
	if rpcResp.Error.Code != UnknownPayloadCode {
		t.Errorf("expected error code %d, got %d", UnknownPayloadCode, rpcResp.Error.Code)
	}
}

// --- Test JSON-RPC Routing ---

func TestHandleRequest_MethodNotFound(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	req := `{"jsonrpc":"2.0","method":"engine_nonexistent","params":[],"id":10}`
	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for unknown method")
	}
	if rpcResp.Error.Code != methodNotFoundCode {
		t.Errorf("expected error code %d, got %d", methodNotFoundCode, rpcResp.Error.Code)
	}
	if !strings.Contains(rpcResp.Error.Message, "engine_nonexistent") {
		t.Errorf("error message should mention the method name, got: %s", rpcResp.Error.Message)
	}
}

func TestHandleRequest_ParseError(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	resp := api.HandleRequest([]byte("not json"))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if rpcResp.Error.Code != parseErrorCode {
		t.Errorf("expected error code %d, got %d", parseErrorCode, rpcResp.Error.Code)
	}
}

func TestHandleRequest_InvalidJSONRPCVersion(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	req := `{"jsonrpc":"1.0","method":"engine_newPayloadV3","params":[],"id":11}`
	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for wrong jsonrpc version")
	}
	if rpcResp.Error.Code != InvalidParamsCode {
		t.Errorf("expected error code %d, got %d", InvalidParamsCode, rpcResp.Error.Code)
	}
}

func TestHandleRequest_IDPreserved(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	// Test with numeric ID.
	req := `{"jsonrpc":"2.0","method":"engine_nonexistent","params":[],"id":42}`
	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if string(rpcResp.ID) != "42" {
		t.Errorf("expected ID 42, got %s", string(rpcResp.ID))
	}

	// Test with string ID.
	req = `{"jsonrpc":"2.0","method":"engine_nonexistent","params":[],"id":"abc"}`
	resp = api.HandleRequest([]byte(req))

	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if string(rpcResp.ID) != `"abc"` {
		t.Errorf("expected ID \"abc\", got %s", string(rpcResp.ID))
	}
}

// --- Test Error Mapping ---

func TestEngineErrorToRPC(t *testing.T) {
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
		{fmt.Errorf("some other error"), internalErrorCode},
	}

	for _, tt := range tests {
		rpcErr := engineErrorToRPC(tt.err)
		if rpcErr.Code != tt.wantCode {
			t.Errorf("engineErrorToRPC(%v): code=%d, want %d", tt.err, rpcErr.Code, tt.wantCode)
		}
		if rpcErr.Message != tt.err.Error() {
			t.Errorf("engineErrorToRPC(%v): message=%q, want %q", tt.err, rpcErr.Message, tt.err.Error())
		}
	}
}

// --- Test HTTP Server ---

func startTestServer(t *testing.T, api *EngineAPI) string {
	t.Helper()
	errCh := make(chan error, 1)
	go func() {
		errCh <- api.Start("127.0.0.1:0")
	}()

	// Wait for the listener to be ready.
	for i := 0; i < 50; i++ {
		if addr := api.Addr(); addr != nil {
			return addr.String()
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case err := <-errCh:
		t.Fatalf("server failed to start: %v", err)
	default:
	}
	t.Fatal("server did not bind in time")
	return ""
}

func TestHTTPServer(t *testing.T) {
	headHash := types.HexToHash("0x1111")
	backend := &mockBackend{
		forkchoiceUpdFn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
			return ForkchoiceUpdatedResult{
				PayloadStatus: PayloadStatusV1{
					Status:          StatusValid,
					LatestValidHash: &headHash,
				},
			}, nil
		},
	}

	api := NewEngineAPI(backend)
	addr := startTestServer(t, api)
	defer api.Stop()

	state := ForkchoiceStateV1{
		HeadBlockHash:      headHash,
		SafeBlockHash:      types.HexToHash("0x2222"),
		FinalizedBlockHash: types.HexToHash("0x3333"),
	}
	stateJSON, _ := json.Marshal(state)

	body := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s,null],"id":1}`, stateJSON)

	resp, err := http.Post("http://"+addr+"/", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("HTTP POST failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read response body: %v", err)
	}

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
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
		t.Errorf("expected VALID status, got %s", result.PayloadStatus.Status)
	}
}

func TestHTTPServer_MethodNotAllowed(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})
	addr := startTestServer(t, api)
	defer api.Stop()

	resp, err := http.Get("http://" + addr + "/")
	if err != nil {
		t.Fatalf("HTTP GET failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", resp.StatusCode)
	}
}

// --- Test EngineAPI Methods Directly ---

func TestNewEngineAPI(t *testing.T) {
	backend := &mockBackend{}
	api := NewEngineAPI(backend)
	if api == nil {
		t.Fatal("NewEngineAPI returned nil")
	}
	if api.backend != backend {
		t.Error("backend not set correctly")
	}
}

func TestStop_NilServer(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})
	// Stopping without starting should not error.
	if err := api.Stop(); err != nil {
		t.Errorf("Stop() on non-started server returned error: %v", err)
	}
}

// --- Test ForkchoiceUpdatedV3 with only state param ---

func TestForkchoiceUpdatedV3_OnlyState(t *testing.T) {
	backend := &mockBackend{
		forkchoiceUpdFn: func(state ForkchoiceStateV1, attrs *PayloadAttributesV3) (ForkchoiceUpdatedResult, error) {
			if attrs != nil {
				t.Error("expected nil attrs when only state is provided")
			}
			return ForkchoiceUpdatedResult{
				PayloadStatus: PayloadStatusV1{Status: StatusSyncing},
			}, nil
		},
	}

	api := NewEngineAPI(backend)

	state := ForkchoiceStateV1{
		HeadBlockHash: types.HexToHash("0xaaaa"),
	}
	stateJSON, _ := json.Marshal(state)

	// Only one param (no attributes).
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_forkchoiceUpdatedV3","params":[%s],"id":12}`, stateJSON)
	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}

	resultJSON, _ := json.Marshal(rpcResp.Result)
	var result ForkchoiceUpdatedResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatalf("failed to unmarshal result: %v", err)
	}
	if result.PayloadStatus.Status != StatusSyncing {
		t.Errorf("expected SYNCING, got %s", result.PayloadStatus.Status)
	}
}

// --- Test NewPayloadV4 ---

func TestNewPayloadV4_Valid(t *testing.T) {
	blockHash := types.HexToHash("0xabcdef")
	backend := &mockBackend{
		processBlockV4Fn: func(payload *ExecutionPayloadV3, hashes []types.Hash, root types.Hash, requests [][]byte) (PayloadStatusV1, error) {
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
	requestsJSON, _ := json.Marshal([][]byte{{0x00, 0x01}, {0x01, 0x02}})

	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_newPayloadV4","params":[%s,%s,%s,%s],"id":20}`,
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

func TestNewPayloadV4_WrongParamCount(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	// Only 3 params instead of 4.
	req := `{"jsonrpc":"2.0","method":"engine_newPayloadV4","params":[{},{},{}],"id":21}`
	resp := api.HandleRequest([]byte(req))

	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if rpcResp.Error == nil {
		t.Fatal("expected error for wrong param count")
	}
	if rpcResp.Error.Code != InvalidParamsCode {
		t.Errorf("expected error code %d, got %d", InvalidParamsCode, rpcResp.Error.Code)
	}
}

// --- Test ExchangeCapabilities ---

func TestExchangeCapabilities(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	requestedJSON, _ := json.Marshal([]string{"engine_newPayloadV3", "engine_newPayloadV4"})
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_exchangeCapabilities","params":[%s],"id":22}`, requestedJSON)

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
		t.Fatal("expected non-empty capabilities")
	}
	// Check that known methods are in the list.
	found := map[string]bool{}
	for _, c := range capabilities {
		found[c] = true
	}
	for _, want := range []string{"engine_newPayloadV3", "engine_newPayloadV4", "engine_newPayloadV5", "engine_forkchoiceUpdatedV3", "engine_forkchoiceUpdatedV4", "engine_getPayloadV4", "engine_getPayloadV6"} {
		if !found[want] {
			t.Errorf("expected capability %q in response", want)
		}
	}
}

// --- Test GetClientVersionV1 ---

func TestGetClientVersionV1(t *testing.T) {
	api := NewEngineAPI(&mockBackend{})

	peerVersion := ClientVersionV1{Code: "GE", Name: "geth", Version: "1.14.0", Commit: "abc123"}
	peerJSON, _ := json.Marshal(peerVersion)
	req := fmt.Sprintf(`{"jsonrpc":"2.0","method":"engine_getClientVersionV1","params":[%s],"id":23}`, peerJSON)

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
}

// --- Helpers ---

func makeTestPayloadV3(blockHash types.Hash, blockNumber uint64) ExecutionPayloadV3 {
	return ExecutionPayloadV3{
		ExecutionPayloadV2: ExecutionPayloadV2{
			ExecutionPayloadV1: ExecutionPayloadV1{
				ParentHash:    types.HexToHash("0xparent"),
				FeeRecipient:  types.HexToAddress("0xfee"),
				StateRoot:     types.HexToHash("0xstate"),
				ReceiptsRoot:  types.HexToHash("0xreceipts"),
				PrevRandao:    types.HexToHash("0xrandao"),
				BlockNumber:   blockNumber,
				GasLimit:      30000000,
				GasUsed:       21000,
				Timestamp:     1700000000,
				ExtraData:     []byte("test"),
				BaseFeePerGas: big.NewInt(1000000000),
				BlockHash:     blockHash,
				Transactions:  [][]byte{},
			},
			Withdrawals: []*Withdrawal{},
		},
		BlobGasUsed:   0,
		ExcessBlobGas: 0,
	}
}
