package engine

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/engine/backendapi"
	"github.com/eth2030/eth2030/engine/blobval"
	"github.com/eth2030/eth2030/engine/payload"
	"github.com/eth2030/eth2030/metrics"
)

// Backend is a type alias — canonical definition in engine/backendapi.
type Backend = backendapi.Backend

// EngineAPI implements the Engine API JSON-RPC methods.
type EngineAPI struct {
	backend         Backend
	builderRegistry *BuilderRegistry
	blobValidator   *blobval.BlobValidator
	server          *http.Server
	listener        net.Listener
	ethHandler      http.Handler // optional: handles non-engine_ methods (eth_, web3_, net_)
	authSecret      string
	maxRequestSize  int64
	requestTimeout  time.Duration    // P0: configurable request timeout (default 8s)
	priorityHandler *PriorityHandler // P2: tracks request priorities
	mu              sync.Mutex
}

// NewEngineAPI creates a new Engine API instance with the given backend.
func NewEngineAPI(backend Backend) *EngineAPI {
	return &EngineAPI{
		backend:         backend,
		builderRegistry: NewBuilderRegistry(),
		blobValidator:   blobval.NewBlobValidator(),
		maxRequestSize:  5 * 1024 * 1024,
		requestTimeout:  8 * time.Second,      // P0: default 8s timeout
		priorityHandler: NewPriorityHandler(), // P2: priority tracking
	}
}

// SetAuthSecret sets the bearer token required for Engine API authentication.
// An empty token disables auth checks.
func (api *EngineAPI) SetAuthSecret(secret string) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.authSecret = secret
}

// SetMaxRequestSize updates the maximum allowed request body size.
// A non-positive value restores the default of 5 MiB.
func (api *EngineAPI) SetMaxRequestSize(bytes int64) {
	if bytes <= 0 {
		bytes = 5 * 1024 * 1024
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	api.maxRequestSize = bytes
}

// SetRequestTimeout sets the timeout for Engine API requests.
// A non-positive value disables timeout (not recommended for production).
func (api *EngineAPI) SetRequestTimeout(timeout time.Duration) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.requestTimeout = timeout
}

// NewPayloadV3 validates and executes a new Cancun/Deneb payload.
// Per EIP-4844: blob versioned hashes must match commitments in transactions.
// Per EIP-4788: parentBeaconBlockRoot must be provided (non-zero).
func (api *EngineAPI) NewPayloadV3(
	payload ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
) (PayloadStatusV1, error) {
	// Validate that the payload timestamp falls within the Cancun fork.
	if !api.backend.IsCancun(payload.Timestamp) {
		return PayloadStatusV1{}, ErrUnsupportedFork
	}

	// EIP-4788: parentBeaconBlockRoot must be provided.
	if parentBeaconBlockRoot == (types.Hash{}) {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// EIP-4844: validate blob versioned hashes match transaction contents.
	if err := validateBlobHashes(&payload, expectedBlobVersionedHashes); err != nil {
		errMsg := err.Error()
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	return api.backend.ProcessBlock(&payload, expectedBlobVersionedHashes, parentBeaconBlockRoot)
}

// ForkchoiceUpdatedV3 processes a forkchoice update with optional V3 payload attributes.
// Validates withdrawals and parentBeaconBlockRoot in payload attributes.
// Validates timestamp progression when attributes are provided.
func (api *EngineAPI) ForkchoiceUpdatedV3(
	state ForkchoiceStateV1,
	payloadAttributes *PayloadAttributesV3,
) (ForkchoiceUpdatedResult, error) {
	if payloadAttributes != nil {
		// Validate timestamp is non-zero.
		if payloadAttributes.Timestamp == 0 {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}
		// Validate parentBeaconBlockRoot is provided (V3 requires it).
		if payloadAttributes.ParentBeaconBlockRoot == (types.Hash{}) {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}
		// Validate timestamp progression: new payload must be strictly after the
		// FCU head block. Use the head block's own timestamp (not the canonical
		// chain head) so that fork-choice updates to non-canonical heads are
		// validated correctly during reorgs.
		headTimestamp := api.backend.GetBlockTimestamp(state.HeadBlockHash)
		if headTimestamp == 0 {
			// FCU head not yet known; fall back to global canonical head.
			headTimestamp = api.backend.GetHeadTimestamp()
		}
		if headTimestamp > 0 && payloadAttributes.Timestamp <= headTimestamp {
			return ForkchoiceUpdatedResult{}, ErrInvalidPayloadAttributes
		}
	}
	return api.backend.ForkchoiceUpdated(state, payloadAttributes)
}

// GetPayloadV3 retrieves a previously built payload by ID.
// Returns ExecutionPayloadV3 + blockValue + blobsBundle (no executionRequests).
func (api *EngineAPI) GetPayloadV3(payloadID PayloadID) (*GetPayloadV3Response, error) {
	resp, err := api.backend.GetPayloadByID(payloadID)
	if err != nil {
		return nil, err
	}
	if err := api.validateBlobsBundle(resp.BlobsBundle); err != nil {
		return nil, fmt.Errorf("invalid blobs bundle: %w", err)
	}
	// Build the V3 response (no executionRequests in V3).
	v3Resp := &GetPayloadV3Response{
		ExecutionPayload: &resp.ExecutionPayload.ExecutionPayloadV3,
		BlockValue:       resp.BlockValue,
		BlobsBundle:      resp.BlobsBundle,
		Override:         resp.Override,
	}
	return v3Resp, nil
}

// validateBlobsBundle validates the KZG commitments and blob counts in a blobs bundle.
// It converts the dynamic [][]byte slices to fixed-size arrays required by blobval.
func (api *EngineAPI) validateBlobsBundle(bundle *payload.BlobsBundleV1) error {
	if bundle == nil || len(bundle.Blobs) == 0 {
		return nil
	}
	commitments := make([][48]byte, len(bundle.Commitments))
	for i, c := range bundle.Commitments {
		if len(c) != 48 {
			return fmt.Errorf("commitment[%d]: expected 48 bytes, got %d", i, len(c))
		}
		copy(commitments[i][:], c)
	}
	blobs := make([][131072]byte, len(bundle.Blobs))
	for i, b := range bundle.Blobs {
		if len(b) != 131072 {
			return fmt.Errorf("blob[%d]: expected 131072 bytes, got %d", i, len(b))
		}
		copy(blobs[i][:], b)
	}
	return api.blobValidator.ValidateKZGCommitments(commitments, blobs)
}

// NewPayloadV4 validates and executes a Prague/Electra payload with execution requests.
// Per EIP-7685: executionRequests must be provided.
func (api *EngineAPI) NewPayloadV4(
	payload ExecutionPayloadV3,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (PayloadStatusV1, error) {
	// Validate that the payload timestamp falls within the Prague fork.
	if !api.backend.IsPrague(payload.Timestamp) {
		return PayloadStatusV1{}, ErrUnsupportedFork
	}

	// EIP-4788: parentBeaconBlockRoot must be provided.
	if parentBeaconBlockRoot == (types.Hash{}) {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// EIP-7685: executionRequests must be provided (can be empty list, not nil).
	if executionRequests == nil {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// EIP-4844: validate blob versioned hashes match transaction contents.
	if err := validateBlobHashes(&payload, expectedBlobVersionedHashes); err != nil {
		errMsg := err.Error()
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	return api.backend.ProcessBlockV4(&payload, expectedBlobVersionedHashes, parentBeaconBlockRoot, executionRequests)
}

// GetPayloadV4 retrieves a previously built payload for Prague.
// Returns ExecutionPayloadV3 + blockValue + blobsBundle + executionRequests.
func (api *EngineAPI) GetPayloadV4(payloadID PayloadID) (*GetPayloadV4Response, error) {
	resp, err := api.backend.GetPayloadV4ByID(payloadID)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// NewPayloadV5 validates and executes an Amsterdam payload with BAL.
func (api *EngineAPI) NewPayloadV5(
	payload ExecutionPayloadV5,
	expectedBlobVersionedHashes []types.Hash,
	parentBeaconBlockRoot types.Hash,
	executionRequests [][]byte,
) (PayloadStatusV1, error) {
	// Check that the timestamp falls within the Amsterdam fork.
	if !api.backend.IsAmsterdam(payload.Timestamp) {
		return PayloadStatusV1{}, ErrUnsupportedFork
	}

	// EIP-4788: parentBeaconBlockRoot must be provided.
	if parentBeaconBlockRoot == (types.Hash{}) {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// EIP-7685: executionRequests must be provided.
	if executionRequests == nil {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// The blockAccessList field must be present.
	if payload.BlockAccessList == nil {
		return PayloadStatusV1{}, ErrInvalidParams
	}

	// EIP-4844: validate blob versioned hashes match transaction contents.
	if err := validateBlobHashes(&payload.ExecutionPayloadV3, expectedBlobVersionedHashes); err != nil {
		errMsg := err.Error()
		return PayloadStatusV1{
			Status:          StatusInvalid,
			ValidationError: &errMsg,
		}, nil
	}

	return api.backend.ProcessBlockV5(&payload, expectedBlobVersionedHashes, parentBeaconBlockRoot, executionRequests)
}

// GetPayloadV6 retrieves a previously built payload for Amsterdam.
// Returns ExecutionPayloadV4 (which includes BAL) with execution requests.
func (api *EngineAPI) GetPayloadV6(payloadID PayloadID) (*GetPayloadV6Response, error) {
	resp, err := api.backend.GetPayloadV6ByID(payloadID)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// ForkchoiceUpdatedV4 processes a forkchoice update with V4 payload attributes (Amsterdam).
func (api *EngineAPI) ForkchoiceUpdatedV4(
	state ForkchoiceStateV1,
	payloadAttributes *PayloadAttributesV4,
) (ForkchoiceUpdatedResult, error) {
	// If payload attributes are provided, validate the timestamp.
	if payloadAttributes != nil {
		if !api.backend.IsAmsterdam(payloadAttributes.Timestamp) {
			// Per spec: forkchoice state update must NOT be rolled back,
			// but we still return the error. The backend handles the state update
			// before attribute validation in the full implementation.
			// For now, delegate entirely to the backend which handles ordering.
			return ForkchoiceUpdatedResult{}, ErrUnsupportedFork
		}
	}
	return api.backend.ForkchoiceUpdatedV4(state, payloadAttributes)
}

// ExchangeCapabilities returns the list of Engine API methods this node supports.
func (api *EngineAPI) ExchangeCapabilities(requested []string) []string {
	supported := []string{
		"engine_newPayloadV3",
		"engine_newPayloadV4",
		"engine_newPayloadV5",
		"engine_forkchoiceUpdatedV3",
		"engine_forkchoiceUpdatedV4",
		"engine_forkchoiceUpdatedV5",
		"engine_getPayloadV3",
		"engine_getPayloadV4",
		"engine_getPayloadV5",
		"engine_getPayloadV6",
		"engine_exchangeCapabilities",
		"engine_getClientVersionV1",
		"engine_getBlobsV1",
		"engine_getBlobsV2",
		"engine_getBlobsV3",
		"engine_getPayloadBodiesByHashV1",
		"engine_getPayloadBodiesByRangeV1",
		"engine_getPayloadBodiesByHashV2",
		"engine_getPayloadBodiesByRangeV2",
		"engine_submitBuilderBidV1",
		"engine_getBuilderBidsV1",
		"engine_newInclusionListV1",
		"engine_getInclusionListV1",
		"engine_getPayloadHeaderV1",
		"engine_submitBlindedBlockV1",
	}
	return supported
}

// ClientVersionV1 represents the client version information.
type ClientVersionV1 struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
}

// GetClientVersionV1 returns the client version information.
func (api *EngineAPI) GetClientVersionV1(peerVersion ClientVersionV1) []ClientVersionV1 {
	return []ClientVersionV1{
		{
			Code:    "ET",
			Name:    "ETH2030",
			Version: "v0.1.0",
			Commit:  "00000000",
		},
	}
}

// SetEthHandler registers an HTTP handler for non-engine_ JSON-RPC methods.
// The authenticated engine endpoint must also serve eth_ namespace methods
// (e.g. eth_syncing, eth_getBlockByNumber) per the Engine API specification.
func (api *EngineAPI) SetEthHandler(h http.Handler) {
	api.mu.Lock()
	defer api.mu.Unlock()
	api.ethHandler = h
}

// Start starts the HTTP JSON-RPC server on the given address.
func (api *EngineAPI) Start(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", api.httpHandler)

	api.mu.Lock()
	api.listener = ln
	api.server = &http.Server{Handler: mux}
	api.mu.Unlock()

	log.Printf("Engine API server starting on %s", ln.Addr())
	if err := api.server.Serve(ln); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("engine API server error: %w", err)
	}
	return nil
}

// Addr returns the listener address, useful when started on port 0.
func (api *EngineAPI) Addr() net.Addr {
	api.mu.Lock()
	defer api.mu.Unlock()
	if api.listener == nil {
		return nil
	}
	return api.listener.Addr()
}

// Stop gracefully shuts down the HTTP server.
func (api *EngineAPI) Stop() error {
	api.mu.Lock()
	srv := api.server
	api.mu.Unlock()

	if srv == nil {
		return nil
	}
	return srv.Shutdown(context.Background())
}

// httpHandler handles incoming HTTP requests and dispatches them as JSON-RPC.
// P0: Implements request timeout protection to prevent CL-EL cascade failures.
// P2: Tracks request priorities for monitoring and potential scheduling.
func (api *EngineAPI) httpHandler(w http.ResponseWriter, r *http.Request) {
	if !api.authorizedRequest(r) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","error":{"code":-32000,"message":"unauthorized"},"id":null}`))
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	api.mu.Lock()
	maxRequestSize := api.maxRequestSize
	requestTimeout := api.requestTimeout
	api.mu.Unlock()
	if r.ContentLength > maxRequestSize {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestSize+1))
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}
	if int64(len(body)) > maxRequestSize {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	// P2: Determine request priority from method name.
	var peek struct {
		Method string `json:"method"`
	}
	priority := PriorityNormal
	if json.Unmarshal(body, &peek) == nil {
		priority = MethodPriority(peek.Method)
	}
	api.priorityHandler.StartRequest(priority)
	defer api.priorityHandler.EndRequest(priority)

	// Forward non-engine_ methods (eth_, web3_, net_, admin_) to the eth handler
	// if one has been registered. Per the Engine API spec, the authenticated
	// endpoint must also serve eth_syncing, eth_getBlockByNumber, etc.
	api.mu.Lock()
	ethH := api.ethHandler
	api.mu.Unlock()

	if ethH != nil {
		if peek.Method != "" && !strings.HasPrefix(peek.Method, "engine_") {
			r2 := r.Clone(r.Context())
			r2.Body = io.NopCloser(bytes.NewReader(body))
			ethH.ServeHTTP(w, r2)
			return
		}
	}

	// P0: Handle request with timeout protection.
	if requestTimeout <= 0 {
		// No timeout - direct handling (not recommended for production).
		resp := api.HandleRequest(body)
		w.Header().Set("Content-Type", "application/json")
		w.Write(resp)
		return
	}

	// Use channel-based timeout for async handling.
	type result struct {
		data []byte
		err  error
	}
	resultCh := make(chan result, 1)

	go func() {
		defer close(resultCh)
		resp := api.HandleRequest(body)
		resultCh <- result{data: resp}
	}()

	select {
	case res := <-resultCh:
		w.Header().Set("Content-Type", "application/json")
		w.Write(res.data)
	case <-time.After(requestTimeout):
		// P0: Return timeout error response.
		// Per Engine API spec, CL should retry on timeout.
		metrics.EngineRequestTimeoutTotal.Inc()
		engineLog.Warn("engine_request_timeout",
			"event", "engine_request_timeout",
			"timeout_ms", requestTimeout.Milliseconds(),
		)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		resp := []byte(`{"jsonrpc":"2.0","error":{"code":-32603,"message":"request timeout"},"id":null}`)
		w.Write(resp)
	case <-r.Context().Done():
		// Client disconnected.
		engineLog.Debug("engine_request_cancelled",
			"event", "engine_request_cancelled",
		)
	}
}

func (api *EngineAPI) authorizedRequest(r *http.Request) bool {
	api.mu.Lock()
	secret := api.authSecret
	api.mu.Unlock()

	if secret == "" {
		return true
	}

	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(auth[7:]), []byte(secret)) == 1
}

// validateBlobHashes checks that the blob versioned hashes from the CL
// match the versioned hashes found in blob transactions within the payload.
// Per EIP-4844, each blob transaction carries versioned hashes that must
// appear in the same order in the expectedBlobVersionedHashes list.
func validateBlobHashes(payload *ExecutionPayloadV3, expected []types.Hash) error {
	// Collect all blob versioned hashes from transactions in the payload.
	var actual []types.Hash
	for _, txBytes := range payload.Transactions {
		tx, err := types.DecodeTxRLP(txBytes)
		if err != nil {
			// Invalid transaction encoding will be caught later during block processing.
			continue
		}
		hashes := tx.BlobHashes()
		if len(hashes) > 0 {
			actual = append(actual, hashes...)
		}
	}

	// The expected list must match the actual list exactly (same length, same order).
	if len(expected) != len(actual) {
		return fmt.Errorf("%w: expected %d blob hashes, got %d from transactions",
			ErrInvalidBlobHashes, len(expected), len(actual))
	}
	for i := range expected {
		if expected[i] != actual[i] {
			return fmt.Errorf("%w: hash mismatch at index %d", ErrInvalidBlobHashes, i)
		}
	}
	return nil
}
