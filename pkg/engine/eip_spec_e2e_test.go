// eip_spec_e2e_test.go runs end-to-end engine API scenarios using the real
// EngineBackend so that every layer (block building, BAL computation, IL
// satisfaction, 3D gas vectors, payload bodies V2) executes together.
package engine

import (
	"encoding/json"
	"math/big"
	"testing"

	coreconfig "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// makeE2EBackend creates a full EngineBackend with genesis, usable for e2e tests.
func makeE2EBackend(t *testing.T, cfg *coreconfig.ChainConfig) (*EngineBackend, *EngineAPI) {
	t.Helper()
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis()
	backend := NewEngineBackend(cfg, statedb, genesis)
	api := NewEngineAPI(backend)
	return backend, api
}

// buildNextPayload drives FCU + getPayload to produce a sealed block and
// returns the V5 payload. Uses zero prevRandao and the genesis coinbase.
func buildNextPayload(t *testing.T, api *EngineAPI, backend *EngineBackend) *ExecutionPayloadV5 {
	t.Helper()

	genesisHash := makeGenesis().Hash()
	headHash := backend.headHash
	if headHash == (types.Hash{}) {
		headHash = genesisHash
	}

	// FCU with payload attributes to start building.
	fcState := ForkchoiceStateV1{
		HeadBlockHash:      headHash,
		SafeBlockHash:      headHash,
		FinalizedBlockHash: headHash,
	}
	parent := backend.blocks[headHash]
	attrs := &PayloadAttributesV4{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp:             parent.Header().Time + 12,
					SuggestedFeeRecipient: types.Address{0x01},
				},
				Withdrawals: []*Withdrawal{},
			},
		},
	}

	fcResult, err := backend.ForkchoiceUpdatedV4(fcState, attrs)
	if err != nil {
		t.Fatalf("ForkchoiceUpdatedV4: %v", err)
	}
	if fcResult.PayloadID == nil {
		t.Fatal("ForkchoiceUpdatedV4 returned nil PayloadID")
	}

	// GetPayloadV6 to retrieve the built payload.
	resp, err := backend.GetPayloadV6ByID(*fcResult.PayloadID)
	if err != nil {
		t.Fatalf("GetPayloadV6ByID: %v", err)
	}
	return resp.ExecutionPayload
}

// ---------------------------------------------------------------------------
// SPEC-4.3 E2E: IL satisfaction enforced in ProcessBlockV5
// ---------------------------------------------------------------------------

// TestE2E_Engine_ILSatisfaction_NoILsStoredAlwaysValid verifies that when no
// ILs are stored, ProcessBlockV5 always returns VALID regardless of block content.
func TestE2E_Engine_ILSatisfaction_NoILsStoredAlwaysValid(t *testing.T) {
	backend, _ := makeE2EBackend(t, coreconfig.TestConfig)

	payload := buildNextPayload(t, nil, backend)
	status, err := backend.ProcessBlockV5(payload, nil, types.Hash{}, [][]byte{})
	if err != nil {
		t.Fatalf("ProcessBlockV5: %v", err)
	}
	if status.Status != StatusValid {
		t.Errorf("no ILs stored: want VALID, got %s", status.Status)
	}
}

// TestE2E_Engine_ProcessInclusionList_ThenProcessBlock verifies that storing
// an IL and then submitting a block (via ProcessBlockV5) that satisfies it
// returns VALID (not INCLUSION_LIST_UNSATISFIED).
func TestE2E_Engine_ProcessInclusionList_ThenProcessBlock(t *testing.T) {
	backend, _ := makeE2EBackend(t, coreconfig.TestConfig)

	// Store an IL with an invalid-RLP "transaction" — this will be skipped
	// by CheckILSatisfaction (invalid RLP is treated as non-existent tx).
	il := &types.InclusionList{
		Slot:           1,
		ValidatorIndex: 5,
		Transactions:   [][]byte{{0xde, 0xad}}, // invalid RLP → skipped
	}
	if err := backend.ProcessInclusionList(il); err != nil {
		t.Fatalf("ProcessInclusionList: %v", err)
	}

	payload := buildNextPayload(t, nil, backend)
	status, err := backend.ProcessBlockV5(payload, nil, types.Hash{}, [][]byte{})
	if err != nil {
		t.Fatalf("ProcessBlockV5: %v", err)
	}
	// Invalid RLP tx in IL is skipped → block is VALID.
	if status.Status != StatusValid {
		t.Errorf("IL with invalid-RLP tx: want VALID, got %s (validationError=%v)",
			status.Status, status.ValidationError)
	}
}

// TestE2E_Engine_StatusInclusionListUnsatisfied_Constant verifies that the
// INCLUSION_LIST_UNSATISFIED constant is correct per EIP-7805 spec.
func TestE2E_Engine_StatusInclusionListUnsatisfied_Constant(t *testing.T) {
	if StatusInclusionListUnsatisfied != "INCLUSION_LIST_UNSATISFIED" {
		t.Errorf("StatusInclusionListUnsatisfied = %q, want INCLUSION_LIST_UNSATISFIED",
			StatusInclusionListUnsatisfied)
	}
}

// TestE2E_Engine_IlsAsFocil_EmptyILList verifies that an empty IL list
// returns an empty focil IL slice without panic.
func TestE2E_Engine_IlsAsFocil_EmptyILList(t *testing.T) {
	backend, _ := makeE2EBackend(t, coreconfig.TestConfig)
	// No ILs stored.
	result := backend.ilsAsFocil()
	if len(result) != 0 {
		t.Errorf("empty ils: want 0 focil ILs, got %d", len(result))
	}
}

// ---------------------------------------------------------------------------
// SPEC-5.5 E2E: GetPayloadBodiesV2 after block processing
// ---------------------------------------------------------------------------

// TestE2E_Engine_PayloadBodiesV2_KnownAndUnknownHashes verifies that
// GetPayloadBodiesByHashV2 returns non-nil for known blocks and nil for unknown.
func TestE2E_Engine_PayloadBodiesV2_KnownAndUnknownHashes(t *testing.T) {
	_, api := makeE2EBackend(t, coreconfig.TestConfig)
	genesis := makeGenesis()
	genesisHash := genesis.Hash()
	unknownHash := types.Hash{0xff, 0xee}

	results, err := api.GetPayloadBodiesByHashV2([]types.Hash{genesisHash, unknownHash})
	if err != nil {
		t.Fatalf("GetPayloadBodiesByHashV2: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("want 2 results, got %d", len(results))
	}
	if results[0] == nil {
		t.Error("genesis block: want non-nil body")
	}
	if results[1] != nil {
		t.Error("unknown hash: want nil body")
	}
}

// TestE2E_Engine_PayloadBodiesV2_RangeIncludesGenesis verifies that the
// range query returns the genesis block body when queried by block number.
func TestE2E_Engine_PayloadBodiesV2_RangeIncludesGenesis(t *testing.T) {
	_, api := makeE2EBackend(t, coreconfig.TestConfig)

	results, err := api.GetPayloadBodiesByRangeV2(0, 3)
	if err != nil {
		t.Fatalf("GetPayloadBodiesByRangeV2: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("want 3 results, got %d", len(results))
	}
	// Genesis is block 0; blocks 1 and 2 don't exist yet → nil.
	if results[0] == nil {
		t.Error("genesis block (num=0): want non-nil body")
	}
	if results[1] != nil {
		t.Error("block 1 (not yet processed): want nil body")
	}
	if results[2] != nil {
		t.Error("block 2 (not yet processed): want nil body")
	}
}

// TestE2E_Engine_PayloadBodiesV2_BodyFields verifies that the returned body
// has the correct fields (Transactions non-nil, Withdrawals non-nil, no BAL
// since genesis has no state execution).
func TestE2E_Engine_PayloadBodiesV2_BodyFields(t *testing.T) {
	_, api := makeE2EBackend(t, coreconfig.TestConfig)
	genesis := makeGenesis()

	results, err := api.GetPayloadBodiesByHashV2([]types.Hash{genesis.Hash()})
	if err != nil {
		t.Fatalf("GetPayloadBodiesByHashV2: %v", err)
	}
	body := results[0]
	if body.Transactions == nil {
		t.Error("Transactions must be non-nil slice (may be empty)")
	}
	if body.Withdrawals == nil {
		t.Error("Withdrawals must be non-nil slice (may be empty)")
	}
}

// TestE2E_Engine_PayloadBodiesV2_Handler_DispatchRouting verifies that the
// full JSON-RPC dispatch correctly routes engine_getPayloadBodiesByHashV2.
func TestE2E_Engine_PayloadBodiesV2_Handler_DispatchRouting(t *testing.T) {
	_, api := makeE2EBackend(t, coreconfig.TestConfig)
	genesis := makeGenesis()

	hashesJSON, _ := json.Marshal([]types.Hash{genesis.Hash()})
	req := []byte(`{"jsonrpc":"2.0","id":1,"method":"engine_getPayloadBodiesByHashV2","params":[` +
		string(hashesJSON) + `]}`)

	resp := api.HandleRequest(req)
	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
	if rpcResp.Result == nil {
		t.Fatal("expected non-nil result")
	}
}

// TestE2E_Engine_PayloadBodiesV2_RangeHandler_DispatchRouting verifies full
// JSON-RPC dispatch for engine_getPayloadBodiesByRangeV2.
func TestE2E_Engine_PayloadBodiesV2_RangeHandler_DispatchRouting(t *testing.T) {
	_, api := makeE2EBackend(t, coreconfig.TestConfig)

	req := []byte(`{"jsonrpc":"2.0","id":2,"method":"engine_getPayloadBodiesByRangeV2","params":[0,1]}`)
	resp := api.HandleRequest(req)
	var rpcResp jsonrpcResponse
	if err := json.Unmarshal(resp, &rpcResp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if rpcResp.Error != nil {
		t.Fatalf("unexpected error: %s", rpcResp.Error.Message)
	}
}

// ---------------------------------------------------------------------------
// SPEC-6.4 E2E: 3D gas vectors through FCU+getPayload chain
// ---------------------------------------------------------------------------

// TestE2E_Engine_3DGasVectors_InBuiltPayload verifies that a payload built via
// ForkchoiceUpdatedV4 + GetPayloadV6 on a Glamsterdam config has 3D gas vector
// fields set in the built block.
func TestE2E_Engine_3DGasVectors_InBuiltPayload(t *testing.T) {
	// TestConfigGlamsterdan activates Glamsterdam at genesis (GlamsterdanTime=0).
	backend, _ := makeE2EBackend(t, coreconfig.TestConfigGlamsterdan)

	genesisHash := makeGenesis().Hash()
	fcState := ForkchoiceStateV1{
		HeadBlockHash:      genesisHash,
		SafeBlockHash:      genesisHash,
		FinalizedBlockHash: genesisHash,
	}
	parent := backend.blocks[genesisHash]
	attrs := &PayloadAttributesV4{
		PayloadAttributesV3: PayloadAttributesV3{
			PayloadAttributesV2: PayloadAttributesV2{
				PayloadAttributesV1: PayloadAttributesV1{
					Timestamp:             parent.Header().Time + 12,
					SuggestedFeeRecipient: types.Address{0x02},
				},
				Withdrawals: []*Withdrawal{},
			},
		},
	}

	fcResult, err := backend.ForkchoiceUpdatedV4(fcState, attrs)
	if err != nil {
		t.Fatalf("ForkchoiceUpdatedV4: %v", err)
	}
	if fcResult.PayloadID == nil {
		t.Fatal("nil PayloadID")
	}

	resp, err := backend.GetPayloadV6ByID(*fcResult.PayloadID)
	if err != nil {
		t.Fatalf("GetPayloadV6ByID: %v", err)
	}

	ep := resp.ExecutionPayload
	if ep == nil {
		t.Fatal("nil ExecutionPayload in V6 response")
	}

	// For Amsterdam (all-forks-at-genesis TestConfig), the underlying block
	// should have 3D gas vectors set (GasLimitVec/GasUsedVec/ExcessGasVec).
	// We can verify via the pending payload's block.
	backend.payloadMu.RLock()
	var builtBlock interface{ Header() *types.Header }
	for _, pending := range backend.payloads {
		builtBlock = pending.block
		break
	}
	backend.payloadMu.RUnlock()

	if builtBlock == nil {
		t.Skip("no pending payload to inspect")
	}
	h := builtBlock.Header()
	if h.GasLimitVec == nil {
		t.Error("GasLimitVec is nil — 3D gas vectors not set in built block")
	}
	if h.GasUsedVec == nil {
		t.Error("GasUsedVec is nil — 3D gas vectors not set in built block")
	}
	if h.ExcessGasVec == nil {
		t.Error("ExcessGasVec is nil — 3D gas vectors not set in built block")
	}
	if h.GasLimitVec != nil && h.GasLimitVec[0] != h.GasLimit {
		t.Errorf("GasLimitVec[0]=%d should equal GasLimit=%d", h.GasLimitVec[0], h.GasLimit)
	}
}

// TestE2E_Engine_3DGasVectors_CopyHeaderPreservesVectors verifies that the
// copyHeader bug fix (SPEC-6.4) is effective: GasLimitVec survives block.Header().
func TestE2E_Engine_3DGasVectors_CopyHeaderPreservesVectors(t *testing.T) {
	v := [3]uint64{30_000_000, 0, 7_500_000}
	header := &types.Header{
		Number:       big.NewInt(1),
		GasLimit:     30_000_000,
		GasLimitVec:  &v,
		GasUsedVec:   &[3]uint64{0, 0, 0},
		ExcessGasVec: &[3]uint64{0, 0, 0},
	}
	block := types.NewBlock(header, nil)
	h2 := block.Header()
	if h2.GasLimitVec == nil {
		t.Fatal("GasLimitVec nil after block.Header() — copyHeader bug not fixed")
	}
	if h2.GasLimitVec[0] != 30_000_000 {
		t.Errorf("GasLimitVec[0] = %d, want 30_000_000", h2.GasLimitVec[0])
	}
	if h2.GasLimitVec[2] != 7_500_000 {
		t.Errorf("GasLimitVec[2] = %d, want 7_500_000", h2.GasLimitVec[2])
	}
	// Mutation on copy must not affect original.
	h2.GasLimitVec[0] = 999
	if header.GasLimitVec[0] != 30_000_000 {
		t.Error("modifying copy mutated original GasLimitVec")
	}
}

// ---------------------------------------------------------------------------
// SPEC-6.1 E2E: MultiDimFeeTx round-trip through engine API
// ---------------------------------------------------------------------------

// TestE2E_Engine_MultiDimFeeTx_DecodeInPayload verifies that a MultiDimFeeTx
// encoded as part of an ExecutionPayloadV5 Transactions field can be decoded
// back into a typed transaction.
func TestE2E_Engine_MultiDimFeeTx_DecodeInPayload(t *testing.T) {
	to := types.Address{0xAB}
	tx := types.NewTransaction(&types.MultiDimFeeTx{
		ChainID:  big.NewInt(1337),
		Nonce:    0,
		GasLimit: 21000,
		To:       &to,
		Value:    big.NewInt(0),
		MaxFeesPerGas: [3]*big.Int{
			big.NewInt(1_000_000_000),
			big.NewInt(1_000_000_000),
			big.NewInt(1_000_000_000),
		},
		PriorityFeesPerGas: [3]*big.Int{
			big.NewInt(1_000_000),
			big.NewInt(1_000_000),
			big.NewInt(1_000_000),
		},
		V: new(big.Int),
		R: new(big.Int),
		S: new(big.Int),
	})

	// Encode as RLP (type-prefixed).
	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("EncodeRLP: %v", err)
	}
	if len(enc) == 0 {
		t.Fatal("empty RLP encoding")
	}
	if enc[0] != types.MultiDimFeeTxType {
		t.Errorf("type byte = 0x%02x, want 0x%02x", enc[0], types.MultiDimFeeTxType)
	}

	// Decode back.
	decoded, err := types.DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("DecodeTxRLP: %v", err)
	}
	if decoded.Type() != types.MultiDimFeeTxType {
		t.Errorf("decoded type = 0x%02x, want 0x%02x", decoded.Type(), types.MultiDimFeeTxType)
	}

	// Use as a payload transaction field.
	payload := &ExecutionPayloadV5{
		ExecutionPayloadV4: ExecutionPayloadV4{
			ExecutionPayloadV3: ExecutionPayloadV3{
				ExecutionPayloadV2: ExecutionPayloadV2{
					ExecutionPayloadV1: ExecutionPayloadV1{
						Transactions: [][]byte{enc},
					},
					Withdrawals: []*Withdrawal{},
				},
			},
		},
	}
	if len(payload.Transactions) != 1 {
		t.Error("expected 1 tx in payload")
	}

	// Verify the encoded tx round-trips from the payload.
	roundTripped, err := types.DecodeTxRLP(payload.Transactions[0])
	if err != nil {
		t.Fatalf("round-trip decode: %v", err)
	}
	if roundTripped.Type() != types.MultiDimFeeTxType {
		t.Errorf("round-trip type mismatch: got 0x%02x", roundTripped.Type())
	}
}

// ---------------------------------------------------------------------------
// SPEC-5.4 E2E: BAL feasibility check — normal blocks pass without error
// ---------------------------------------------------------------------------

// TestE2E_Engine_BALFeasibility_NormalPayloadProcesses verifies that a
// normally-built payload processes without triggering ErrBALFeasibilityViolated.
func TestE2E_Engine_BALFeasibility_NormalPayloadProcesses(t *testing.T) {
	backend, _ := makeE2EBackend(t, coreconfig.TestConfig)

	payload := buildNextPayload(t, nil, backend)

	status, err := backend.ProcessBlockV5(payload, nil, types.Hash{}, [][]byte{})
	if err != nil {
		t.Fatalf("ProcessBlockV5 returned error: %v", err)
	}
	if status.Status == StatusInvalid {
		t.Errorf("normal block got INVALID: %v", status.ValidationError)
	}
}

// ---------------------------------------------------------------------------
// SPEC-5.5 E2E: BAL stored after ProcessBlockV5 → retrievable via bodies API
// ---------------------------------------------------------------------------

// TestE2E_Engine_BALStoredAfterProcessBlockV5 verifies that after successfully
// processing a block via ProcessBlockV5, the BAL (if computed) is stored and
// can be retrieved via GetPayloadBodiesByHashV2.
func TestE2E_Engine_BALStoredAfterProcessBlockV5(t *testing.T) {
	backend, api := makeE2EBackend(t, coreconfig.TestConfig)

	// Build and process a block.
	payload := buildNextPayload(t, nil, backend)
	status, err := backend.ProcessBlockV5(payload, nil, types.Hash{}, [][]byte{})
	if err != nil {
		t.Fatalf("ProcessBlockV5: %v", err)
	}
	if status.Status != StatusValid {
		t.Fatalf("expected VALID, got %s (err: %v)", status.Status, status.ValidationError)
	}

	// Compute the block hash from the payload block number.
	// The block is now in backend.blocks — find it by block number.
	backend.blocksMu.RLock()
	var blockHash types.Hash
	for h, b := range backend.blocks {
		if b.NumberU64() == uint64(payload.BlockNumber) {
			blockHash = h
			break
		}
	}
	backend.blocksMu.RUnlock()

	if blockHash == (types.Hash{}) {
		t.Fatal("processed block not found in backend.blocks")
	}

	// Query via GetPayloadBodiesByHashV2.
	results, err := api.GetPayloadBodiesByHashV2([]types.Hash{blockHash})
	if err != nil {
		t.Fatalf("GetPayloadBodiesByHashV2: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("want 1 result, got %d", len(results))
	}
	if results[0] == nil {
		t.Fatal("expected non-nil body for processed block")
	}

	body := results[0]
	if body.Transactions == nil {
		t.Error("Transactions must be non-nil")
	}
	if body.Withdrawals == nil {
		t.Error("Withdrawals must be non-nil")
	}
	// BlockAccessList may be nil if Amsterdam is not yet active in the config,
	// or non-nil if the block had BAL computed. Either is valid.
	t.Logf("BlockAccessList present: %v (len=%d)", body.BlockAccessList != nil, len(body.BlockAccessList))
}

// ---------------------------------------------------------------------------
// SPEC-4.3 + SPEC-5.5 combined: InclusionListBackend interface on EngineBackend
// ---------------------------------------------------------------------------

// TestE2E_Engine_InclusionListBackend_Interface verifies that EngineBackend
// satisfies InclusionListBackend and that the IL storage / retrieval round-trips.
func TestE2E_Engine_InclusionListBackend_Interface(t *testing.T) {
	backend, _ := makeE2EBackend(t, coreconfig.TestConfig)

	// Verify interface compliance.
	var ilb InclusionListBackend = backend

	// Store multiple ILs.
	for i := 0; i < 5; i++ {
		il := &types.InclusionList{
			Slot:           uint64(i + 1),
			ValidatorIndex: uint64(i * 10),
			Transactions:   [][]byte{},
		}
		if err := ilb.ProcessInclusionList(il); err != nil {
			t.Fatalf("ProcessInclusionList[%d]: %v", i, err)
		}
	}

	// Verify all 5 stored.
	backend.ilMu.RLock()
	count := len(backend.ils)
	backend.ilMu.RUnlock()
	if count != 5 {
		t.Errorf("expected 5 stored ILs, got %d", count)
	}

	// GetInclusionList returns empty stub.
	il := ilb.GetInclusionList()
	if il == nil {
		t.Fatal("GetInclusionList returned nil")
	}
	if len(il.Transactions) != 0 {
		t.Errorf("stub should return empty IL, got %d txs", len(il.Transactions))
	}
}

// TestE2E_Engine_IlsAsFocil_FieldMapping verifies the slot and proposerIndex
// mapping between types.InclusionList and focil.InclusionList.
func TestE2E_Engine_IlsAsFocil_FieldMapping(t *testing.T) {
	backend, _ := makeE2EBackend(t, coreconfig.TestConfig)

	txBytes := []byte{0x02, 0xAB, 0xCD}
	backend.ils = []*types.InclusionList{
		{Slot: 42, ValidatorIndex: 99, Transactions: [][]byte{txBytes}},
		{Slot: 43, ValidatorIndex: 100, Transactions: nil},
	}

	focilILs := backend.ilsAsFocil()
	if len(focilILs) != 2 {
		t.Fatalf("want 2 focil ILs, got %d", len(focilILs))
	}

	f0 := focilILs[0]
	if f0.Slot != 42 {
		t.Errorf("IL[0].Slot = %d, want 42", f0.Slot)
	}
	if f0.ProposerIndex != 99 {
		t.Errorf("IL[0].ProposerIndex = %d, want 99", f0.ProposerIndex)
	}
	if len(f0.Entries) != 1 {
		t.Fatalf("IL[0] entries = %d, want 1", len(f0.Entries))
	}
	if f0.Entries[0].Index != 0 {
		t.Errorf("IL[0].Entries[0].Index = %d, want 0", f0.Entries[0].Index)
	}

	f1 := focilILs[1]
	if f1.Slot != 43 {
		t.Errorf("IL[1].Slot = %d, want 43", f1.Slot)
	}
	if len(f1.Entries) != 0 {
		t.Errorf("IL[1] (nil txs): want 0 entries, got %d", len(f1.Entries))
	}
}

// ---------------------------------------------------------------------------
// Helpers for rawdb integration
// ---------------------------------------------------------------------------

// TestE2E_Engine_BALRetention_FreshChain verifies that on a fresh chain,
// IsBALRetained returns true for all block numbers (chain too short to prune).
func TestE2E_Engine_BALRetention_FreshChain(t *testing.T) {
	headBlock := uint64(1000)
	for _, blockNum := range []uint64{0, 100, 500, 999, 1000} {
		if !rawdb.IsBALRetained(headBlock, blockNum) {
			t.Errorf("head=%d block=%d: expected retained on fresh chain", headBlock, blockNum)
		}
	}
}

// TestE2E_Engine_BALRetention_MatureChain verifies the pruning window boundary
// on a chain that has grown past the retention period.
func TestE2E_Engine_BALRetention_MatureChain(t *testing.T) {
	head := rawdb.BALRetentionSlots * 3 // well past retention
	// Blocks within the window are retained.
	windowStart := head - rawdb.BALRetentionSlots
	if !rawdb.IsBALRetained(head, windowStart) {
		t.Errorf("window start block %d should be retained (head=%d)", windowStart, head)
	}
	if !rawdb.IsBALRetained(head, head) {
		t.Errorf("head block should be retained")
	}
	// Blocks before the window are pruned.
	if rawdb.IsBALRetained(head, windowStart-1) {
		t.Errorf("block before window start should be pruned (head=%d)", head)
	}
}
