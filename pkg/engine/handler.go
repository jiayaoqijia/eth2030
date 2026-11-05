package engine

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/engine/backendapi"
	"github.com/eth2030/eth2030/log"
)

var engineLog = log.Default().Module("engine/api")

// jsonrpcRequest represents a JSON-RPC 2.0 request.
type jsonrpcRequest struct {
	JSONRPC string            `json:"jsonrpc"`
	Method  string            `json:"method"`
	Params  []json.RawMessage `json:"params"`
	ID      json.RawMessage   `json:"id"`
}

// jsonrpcResponse represents a JSON-RPC 2.0 response.
type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  any             `json:"result,omitempty"`
	Error   *jsonrpcError   `json:"error,omitempty"`
	ID      json.RawMessage `json:"id"`
}

// jsonrpcError represents a JSON-RPC 2.0 error object.
type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Aliases for backward compat within handler (reference the canonical constants in errors.go).
var (
	methodNotFoundCode = MethodNotFoundCode
	parseErrorCode     = ParseErrorCode
	internalErrorCode  = InternalErrorCode
)

// HandleRequest processes a raw JSON-RPC request and returns the raw JSON response.
func (api *EngineAPI) HandleRequest(data []byte) []byte {
	start := time.Now()

	var req jsonrpcRequest
	if err := json.Unmarshal(data, &req); err != nil {
		engineLog.Debug("engine_parse_error",
			"event", "engine_parse_error",
			"error", err,
		)
		resp := jsonrpcResponse{
			JSONRPC: "2.0",
			Error: &jsonrpcError{
				Code:    parseErrorCode,
				Message: "parse error",
			},
			ID: nil,
		}
		out, _ := json.Marshal(resp)
		return out
	}

	if req.JSONRPC != "2.0" {
		resp := jsonrpcResponse{
			JSONRPC: "2.0",
			Error: &jsonrpcError{
				Code:    InvalidParamsCode,
				Message: "invalid jsonrpc version",
			},
			ID: req.ID,
		}
		out, _ := json.Marshal(resp)
		return out
	}

	engineLog.Debug("engine_request",
		"event", "engine_request",
		"method", req.Method,
	)

	result, rpcErr := api.dispatch(req.Method, req.Params)
	latency := time.Since(start)

	if rpcErr != nil {
		engineLog.Debug("engine_response",
			"event", "engine_response",
			"method", req.Method,
			"latency_ms", latency.Milliseconds(),
			"error", true,
			"err_code", rpcErr.Code,
			"err_msg", rpcErr.Message,
		)
	} else {
		engineLog.Debug("engine_response",
			"event", "engine_response",
			"method", req.Method,
			"latency_ms", latency.Milliseconds(),
			"error", false,
		)
	}

	resp := jsonrpcResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
	}
	if rpcErr != nil {
		resp.Error = rpcErr
	} else {
		resp.Result = result
	}

	out, err := json.Marshal(resp)
	if err != nil {
		errResp := jsonrpcResponse{
			JSONRPC: "2.0",
			Error: &jsonrpcError{
				Code:    internalErrorCode,
				Message: fmt.Sprintf("failed to marshal response: %v", err),
			},
			ID: req.ID,
		}
		out, _ = json.Marshal(errResp)
	}
	return out
}

// dispatch routes the JSON-RPC method to the appropriate handler.
func (api *EngineAPI) dispatch(method string, params []json.RawMessage) (any, *jsonrpcError) {
	switch method {
	case "engine_newPayloadV3":
		return api.handleNewPayloadV3(params)
	case "engine_newPayloadV4":
		return api.handleNewPayloadV4(params)
	case "engine_newPayloadV5":
		return api.handleNewPayloadV5(params)
	case "engine_forkchoiceUpdatedV3":
		return api.handleForkchoiceUpdatedV3(params)
	case "engine_forkchoiceUpdatedV4":
		return api.handleForkchoiceUpdatedV4(params)
	case "engine_forkchoiceUpdatedV5":
		return api.handleForkchoiceUpdatedV5(params)
	case "engine_getPayloadV3":
		return api.handleGetPayloadV3(params)
	case "engine_getPayloadV4":
		return api.handleGetPayloadV4(params)
	case "engine_getPayloadV5", "engine_getPayloadV6":
		return api.handleGetPayloadV6(params)
	case "engine_exchangeCapabilities":
		return api.handleExchangeCapabilities(params)
	case "engine_getClientVersionV1":
		return api.handleGetClientVersionV1(params)
	case "engine_submitBuilderBidV1":
		return api.handleSubmitBuilderBidV1(params)
	case "engine_getBuilderBidsV1":
		return api.handleGetBuilderBidsV1(params)
	case "engine_newInclusionListV1":
		return api.handleNewInclusionListV1(params)
	case "engine_getInclusionListV1":
		return api.handleGetInclusionListV1(params)
	case "engine_getPayloadBodiesByHashV1":
		return api.handleGetPayloadBodiesByHashV1(params)
	case "engine_getPayloadBodiesByRangeV1":
		return api.handleGetPayloadBodiesByRangeV1(params)
	case "engine_getPayloadBodiesByHashV2":
		return api.handleGetPayloadBodiesByHashV2(params)
	case "engine_getPayloadBodiesByRangeV2":
		return api.handleGetPayloadBodiesByRangeV2(params)
	case "engine_getPayloadHeaderV1":
		return api.handleGetPayloadHeaderV1(params)
	case "engine_submitBlindedBlockV1":
		return api.handleSubmitBlindedBlockV1(params)
	case "engine_getBlobsV1":
		return api.handleGetBlobsV1(params)
	case "engine_getBlobsV2":
		return api.handleGetBlobsV2(params)
	case "engine_getBlobsV3":
		return api.handleGetBlobsV3(params)
	default:
		return nil, &jsonrpcError{
			Code:    methodNotFoundCode,
			Message: fmt.Sprintf("method %q not found", method),
		}
	}
}

// handleNewPayloadV3 processes an engine_newPayloadV3 request.
func (api *EngineAPI) handleNewPayloadV3(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 3 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 3 params, got %d", len(params)),
		}
	}

	var payload ExecutionPayloadV3
	if err := json.Unmarshal(params[0], &payload); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid payload: %v", err),
		}
	}

	var expectedBlobVersionedHashes []types.Hash
	if err := json.Unmarshal(params[1], &expectedBlobVersionedHashes); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid expectedBlobVersionedHashes: %v", err),
		}
	}

	var parentBeaconBlockRoot types.Hash
	if err := json.Unmarshal(params[2], &parentBeaconBlockRoot); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid parentBeaconBlockRoot: %v", err),
		}
	}

	result, err := api.NewPayloadV3(payload, expectedBlobVersionedHashes, parentBeaconBlockRoot)
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return result, nil
}

// handleForkchoiceUpdatedV3 processes an engine_forkchoiceUpdatedV3 request.
func (api *EngineAPI) handleForkchoiceUpdatedV3(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) < 1 || len(params) > 2 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1-2 params, got %d", len(params)),
		}
	}

	var state ForkchoiceStateV1
	if err := json.Unmarshal(params[0], &state); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid forkchoice state: %v", err),
		}
	}

	var payloadAttributes *PayloadAttributesV3
	if len(params) == 2 {
		// Check if the second param is null.
		if string(params[1]) != "null" {
			payloadAttributes = new(PayloadAttributesV3)
			if err := json.Unmarshal(params[1], payloadAttributes); err != nil {
				return nil, &jsonrpcError{
					Code:    InvalidParamsCode,
					Message: fmt.Sprintf("invalid payload attributes: %v", err),
				}
			}
		}
	}

	result, err := api.ForkchoiceUpdatedV3(state, payloadAttributes)
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return result, nil
}

// handleGetPayloadV3 processes an engine_getPayloadV3 request.
func (api *EngineAPI) handleGetPayloadV3(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 1 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1 param, got %d", len(params)),
		}
	}

	var payloadID PayloadID
	if err := json.Unmarshal(params[0], &payloadID); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid payload ID: %v", err),
		}
	}

	resp, err := api.GetPayloadV3(payloadID)
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return resp, nil
}

// handleNewPayloadV4 processes an engine_newPayloadV4 request (Prague/Electra).
func (api *EngineAPI) handleNewPayloadV4(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 4 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 4 params, got %d", len(params)),
		}
	}

	var payload ExecutionPayloadV3
	if err := json.Unmarshal(params[0], &payload); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid payload: %v", err),
		}
	}

	var expectedBlobVersionedHashes []types.Hash
	if err := json.Unmarshal(params[1], &expectedBlobVersionedHashes); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid expectedBlobVersionedHashes: %v", err),
		}
	}

	var parentBeaconBlockRoot types.Hash
	if err := json.Unmarshal(params[2], &parentBeaconBlockRoot); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid parentBeaconBlockRoot: %v", err),
		}
	}

	var executionRequests [][]byte
	if err := json.Unmarshal(params[3], &executionRequests); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid executionRequests: %v", err),
		}
	}

	result, err := api.NewPayloadV4(payload, expectedBlobVersionedHashes, parentBeaconBlockRoot, executionRequests)
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return result, nil
}

// handleNewPayloadV5 processes an engine_newPayloadV5 request (Amsterdam).
func (api *EngineAPI) handleNewPayloadV5(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 4 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 4 params, got %d", len(params)),
		}
	}

	var payload ExecutionPayloadV5
	if err := json.Unmarshal(params[0], &payload); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid payload: %v", err),
		}
	}

	var expectedBlobVersionedHashes []types.Hash
	if err := json.Unmarshal(params[1], &expectedBlobVersionedHashes); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid expectedBlobVersionedHashes: %v", err),
		}
	}

	var parentBeaconBlockRoot types.Hash
	if err := json.Unmarshal(params[2], &parentBeaconBlockRoot); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid parentBeaconBlockRoot: %v", err),
		}
	}

	var executionRequests [][]byte
	if err := json.Unmarshal(params[3], &executionRequests); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid executionRequests: %v", err),
		}
	}

	result, err := api.NewPayloadV5(payload, expectedBlobVersionedHashes, parentBeaconBlockRoot, executionRequests)
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return result, nil
}

// handleGetPayloadV4 processes an engine_getPayloadV4 request (Prague).
func (api *EngineAPI) handleGetPayloadV4(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 1 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1 param, got %d", len(params)),
		}
	}

	var payloadID PayloadID
	if err := json.Unmarshal(params[0], &payloadID); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid payload ID: %v", err),
		}
	}

	result, err := api.GetPayloadV4(payloadID)
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return result, nil
}

// handleGetPayloadV6 processes an engine_getPayloadV6 request (Amsterdam).
func (api *EngineAPI) handleGetPayloadV6(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 1 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1 param, got %d", len(params)),
		}
	}

	var payloadID PayloadID
	if err := json.Unmarshal(params[0], &payloadID); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid payload ID: %v", err),
		}
	}

	result, err := api.GetPayloadV6(payloadID)
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return result, nil
}

// handleForkchoiceUpdatedV4 processes an engine_forkchoiceUpdatedV4 request
// (Amsterdam). Lighthouse >= 8.4 adds an optional third `custodyColumns`
// bitarray param (Fulu/PeerDAS custody set, may be null) — accepted and
// validated here, not yet consumed (see lighthouse_compat.go).
func (api *EngineAPI) handleForkchoiceUpdatedV4(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) < 1 || len(params) > 3 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1-3 params, got %d", len(params)),
		}
	}

	var state ForkchoiceStateV1
	if err := json.Unmarshal(params[0], &state); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid forkchoice state: %v", err),
		}
	}

	if len(params) == 3 {
		if err := validateCustodyColumns(params[2]); err != nil {
			return nil, &jsonrpcError{
				Code:    InvalidParamsCode,
				Message: err.Error(),
			}
		}
	}

	var payloadAttributes *PayloadAttributesV4
	if len(params) >= 2 {
		attrs, err := parsePayloadAttributes(params[1])
		if err != nil {
			return nil, &jsonrpcError{
				Code:    InvalidParamsCode,
				Message: fmt.Sprintf("invalid payload attributes: %v", err),
			}
		}
		payloadAttributes = attrs
	}

	result, err := api.ForkchoiceUpdatedV4(state, payloadAttributes)
	if err != nil {
		return nil, engineErrorToRPC(err)
	}
	return result, nil
}

// handleForkchoiceUpdatedV5 processes an engine_forkchoiceUpdatedV5 request
// (Gloas). Wire shape matches V4: [state, payloadAttributes|null,
// custodyColumns|null]; V5 payload attributes may carry inclusion list
// transactions upstream (decoded best-effort, see lighthouse_compat.go).
func (api *EngineAPI) handleForkchoiceUpdatedV5(params []json.RawMessage) (any, *jsonrpcError) {
	return api.handleForkchoiceUpdatedV4(params)
}

// handleExchangeCapabilities processes an engine_exchangeCapabilities request.
func (api *EngineAPI) handleExchangeCapabilities(params []json.RawMessage) (any, *jsonrpcError) {
	var requested []string
	if len(params) > 0 {
		if err := json.Unmarshal(params[0], &requested); err != nil {
			return nil, &jsonrpcError{
				Code:    InvalidParamsCode,
				Message: fmt.Sprintf("invalid capabilities list: %v", err),
			}
		}
	}
	return api.ExchangeCapabilities(requested), nil
}

// handleGetClientVersionV1 processes an engine_getClientVersionV1 request.
func (api *EngineAPI) handleGetClientVersionV1(params []json.RawMessage) (any, *jsonrpcError) {
	var peerVersion ClientVersionV1
	if len(params) > 0 {
		if err := json.Unmarshal(params[0], &peerVersion); err != nil {
			return nil, &jsonrpcError{
				Code:    InvalidParamsCode,
				Message: fmt.Sprintf("invalid client version: %v", err),
			}
		}
	}
	return api.GetClientVersionV1(peerVersion), nil
}

// handleGetBlobsV1 processes an engine_getBlobsV1 request.
// It returns blob data from the txpool for each versioned hash, or null when
// the blob is not locally available (CL falls back to P2P blob-sidecar gossip).
func (api *EngineAPI) handleGetBlobsV1(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 1 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1 param, got %d", len(params)),
		}
	}
	var hashes []types.Hash
	if err := json.Unmarshal(params[0], &hashes); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid versioned hashes: %v", err),
		}
	}
	// If the backend can serve blob data from the txpool, use it.
	if bb, ok := api.backend.(backendapi.BlobsV1Backend); ok {
		result := bb.GetBlobsByVersionedHashes(hashes)
		found := 0
		for _, r := range result {
			if r != nil {
				found++
			}
		}
		if found == 0 {
			engineLog.Info("engine_getBlobsV1: no blobs in local pool, CL uses P2P fallback",
				"requested", len(hashes))
		} else {
			engineLog.Debug("engine_getBlobsV1: serving blobs",
				"requested", len(hashes), "found", found)
		}
		return result, nil
	}
	// Fallback: return null for each hash.
	result := make([]*BlobAndProofV1, len(hashes))
	return result, nil
}

// handleGetBlobsV2 processes an engine_getBlobsV2 request (Osaka/Fulu PeerDAS).
// Returns null for the entire result if ANY requested blob is absent from the
// local pool (all-or-nothing semantics per the Osaka spec).
func (api *EngineAPI) handleGetBlobsV2(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 1 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1 param, got %d", len(params)),
		}
	}
	var hashes []types.Hash
	if err := json.Unmarshal(params[0], &hashes); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid versioned hashes: %v", err),
		}
	}
	if bb, ok := api.backend.(backendapi.BlobsV2Backend); ok {
		result := bb.GetBlobsV2ByVersionedHashes(hashes)
		// All-or-nothing: return null if any blob is missing.
		for _, r := range result {
			if r == nil {
				engineLog.Debug("engine_getBlobsV2: missing blob, returning null",
					"requested", len(hashes))
				return nil, nil
			}
		}
		engineLog.Debug("engine_getBlobsV2: serving blobs with cell proofs",
			"requested", len(hashes))
		return result, nil
	}
	return nil, nil
}

// handleGetBlobsV3 processes an engine_getBlobsV3 request (Osaka/Fulu PeerDAS).
// Returns a sparse array: null at positions where the blob is not in the local
// pool (partial-response semantics per the Osaka spec).
func (api *EngineAPI) handleGetBlobsV3(params []json.RawMessage) (any, *jsonrpcError) {
	if len(params) != 1 {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("expected 1 param, got %d", len(params)),
		}
	}
	var hashes []types.Hash
	if err := json.Unmarshal(params[0], &hashes); err != nil {
		return nil, &jsonrpcError{
			Code:    InvalidParamsCode,
			Message: fmt.Sprintf("invalid versioned hashes: %v", err),
		}
	}
	if bb, ok := api.backend.(backendapi.BlobsV3Backend); ok {
		result := bb.GetBlobsV3ByVersionedHashes(hashes)
		found := 0
		for _, r := range result {
			if r != nil {
				found++
			}
		}
		engineLog.Debug("engine_getBlobsV3: serving blobs (sparse)",
			"requested", len(hashes), "found", found)
		return result, nil
	}
	result := make([]*BlobAndProofV2, len(hashes))
	return result, nil
}

func engineErrorToRPC(err error) *jsonrpcError {
	switch err {
	case ErrUnknownPayload:
		return &jsonrpcError{Code: UnknownPayloadCode, Message: err.Error()}
	case ErrInvalidForkchoiceState:
		return &jsonrpcError{Code: InvalidForkchoiceStateCode, Message: err.Error()}
	case ErrInvalidPayloadAttributes:
		return &jsonrpcError{Code: InvalidPayloadAttributeCode, Message: err.Error()}
	case ErrInvalidParams:
		return &jsonrpcError{Code: InvalidParamsCode, Message: err.Error()}
	case ErrTooLargeRequest:
		return &jsonrpcError{Code: TooLargeRequestCode, Message: err.Error()}
	case ErrUnsupportedFork:
		return &jsonrpcError{Code: UnsupportedForkCode, Message: err.Error()}
	default:
		return &jsonrpcError{Code: internalErrorCode, Message: err.Error()}
	}
}
