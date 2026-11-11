package core

import (
	"math/big"
	"strings"
	"testing"

	coreblock "github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

func pragueConfig() *config.ChainConfig {
	return config.TestConfig
}

func prePragueConfig() *config.ChainConfig {
	cfg := *config.TestConfig
	cfg.PragueTime = nil
	return &cfg
}

func requestsHeader() *types.Header {
	return &types.Header{Number: big.NewInt(1), GasLimit: 30_000_000, Time: 12}
}

func TestProcessRequests_ForkGatingAndMissingContracts(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	invalidLog := &types.Receipt{Logs: []*types.Log{{
		Address: types.DepositContractAddress, Topics: []types.Hash{types.DepositEventTopic},
	}}}
	for _, cfg := range []*config.ChainConfig{nil, prePragueConfig()} {
		result, err := execution.ProcessRequests(cfg, statedb, requestsHeader(), invalidLog)
		if err != nil || len(result) != 0 {
			t.Fatalf("inactive Prague processed requests: %v, %v", result, err)
		}
	}
	if result, err := execution.ProcessRequests(pragueConfig(), statedb, requestsHeader()); err != nil || len(result) != 0 {
		t.Fatalf("minimal chain with no request contracts: %v, %v", result, err)
	}
	strict := *pragueConfig()
	strict.RequireRequestPredeploys = true
	if _, err := execution.ProcessRequests(&strict, statedb, requestsHeader()); err == nil || !strings.Contains(err.Error(), "no code") {
		t.Fatalf("missing canonical contract: %v", err)
	}
}

func TestProcessRequests_RejectsMalformedMatchingDeposit(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	log := &types.Log{Address: types.DepositContractAddress, Topics: []types.Hash{types.DepositEventTopic}, Data: make([]byte, 576)}
	receipt := &types.Receipt{Logs: []*types.Log{log}}
	if _, err := execution.ProcessRequests(pragueConfig(), statedb, requestsHeader(), receipt); err == nil || !strings.Contains(err.Error(), "offset") {
		t.Fatalf("invalid deposit layout: %v", err)
	}
	log.Address = types.Address{1}
	if requests, err := execution.ProcessRequests(pragueConfig(), statedb, requestsHeader(), receipt); err != nil || len(requests) != 0 {
		t.Fatalf("unrelated emitter produced a request: %v, %v", requests, err)
	}
}

func TestProcessRequests_RevertsEarlierQueueIfLaterCallFails(t *testing.T) {
	statedb := state.NewMemoryStateDB()
	alloc := config.ExecutionRequestContractAlloc()
	config.ApplyGenesisAlloc(statedb, alloc)
	var countSlot, pending types.Hash
	countSlot[31], pending[31] = 1, 5
	statedb.SetState(types.WithdrawalRequestAddress, countSlot, pending)
	statedb.SetCode(types.ConsolidationRequestAddress, []byte{0x5f, 0x5f, 0xfd}) // REVERT
	if _, err := execution.ProcessRequests(pragueConfig(), statedb, requestsHeader()); err == nil || !strings.Contains(err.Error(), "type 2 system call") {
		t.Fatalf("failing consolidation call: %v", err)
	}
	if got := statedb.GetState(types.WithdrawalRequestAddress, countSlot); got != pending {
		t.Fatalf("earlier withdrawal queue state changed on failure: %s", got.Hex())
	}

	statedb.SetCode(types.ConsolidationRequestAddress, alloc[types.ConsolidationRequestAddress].Code)
	if _, err := execution.ProcessRequests(pragueConfig(), statedb, requestsHeader()); err != nil {
		t.Fatal(err)
	}
	if got := statedb.GetState(types.WithdrawalRequestAddress, countSlot); !got.IsZero() {
		t.Fatalf("withdrawal count was not cleared after success: %s", got.Hex())
	}
}

func TestValidateRequests_PostPrague_ValidHash(t *testing.T) {
	config := pragueConfig()
	v := coreblock.NewBlockValidator(config)

	requests := types.Requests{
		types.NewRequest(types.DepositRequestType, []byte{0xAA, 0xBB}),
		types.NewRequest(types.WithdrawalRequestType, []byte{0xCC}),
	}

	hash := types.ComputeRequestsHash(requests)
	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: &hash,
	}

	if err := v.ValidateRequests(header, requests); err != nil {
		t.Fatalf("valid requests hash rejected: %v", err)
	}
}

func TestValidateRequests_PostPrague_InvalidHash(t *testing.T) {
	config := pragueConfig()
	v := coreblock.NewBlockValidator(config)

	requests := types.Requests{
		types.NewRequest(types.DepositRequestType, []byte{0xAA, 0xBB}),
	}

	// Use a wrong hash.
	wrongHash := types.HexToHash("0xdeadbeef")
	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: &wrongHash,
	}

	if err := v.ValidateRequests(header, requests); err == nil {
		t.Fatal("expected error for invalid requests hash")
	}
}

func TestValidateRequests_PostPrague_MissingHash(t *testing.T) {
	config := pragueConfig()
	v := coreblock.NewBlockValidator(config)

	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: nil, // missing
	}

	if err := v.ValidateRequests(header, nil); err == nil {
		t.Fatal("expected error for missing requests_hash in post-Prague block")
	}
}

func TestValidateRequests_PrePrague_NoHash(t *testing.T) {
	config := prePragueConfig()
	v := coreblock.NewBlockValidator(config)

	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: nil,
	}

	// Pre-Prague with no hash should be valid.
	if err := v.ValidateRequests(header, nil); err != nil {
		t.Fatalf("pre-Prague block without requests_hash should be valid: %v", err)
	}
}

func TestValidateRequests_PrePrague_HasHash(t *testing.T) {
	config := prePragueConfig()
	v := coreblock.NewBlockValidator(config)

	hash := types.Hash{0x01}
	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: &hash,
	}

	// Pre-Prague with requests_hash should fail.
	if err := v.ValidateRequests(header, nil); err == nil {
		t.Fatal("expected error for pre-Prague block with requests_hash")
	}
}

func TestValidateRequests_PostPrague_EmptyRequests(t *testing.T) {
	config := pragueConfig()
	v := coreblock.NewBlockValidator(config)

	// Empty requests list should produce a valid hash.
	var requests types.Requests
	hash := types.ComputeRequestsHash(requests)
	header := &types.Header{
		Number:       big.NewInt(1),
		Time:         1000,
		RequestsHash: &hash,
	}

	if err := v.ValidateRequests(header, requests); err != nil {
		t.Fatalf("valid empty requests hash rejected: %v", err)
	}
}

// Test helper functions.
