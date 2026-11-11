package eiptest_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/engine"
)

func TestEIP7807_ExecutionPayloadAndSummary(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), 0, eiptest.ToPtr(s.Bob().Address), eiptest.Wei(7), nil, 21_000, bigFeeCap, bigTipCap)
	root := types.Hash{0x42}
	result := s.CommitWithBeaconRoot(root, tx)
	eiptest.ExpectSuccess(t, result, 0)
	access, err := result.BAL.EncodeRLP()
	if err != nil {
		t.Fatal(err)
	}
	payload, err := types.NewExecutionPayloadSSZ(result.Block, result.Receipts, types.ExecutionSSZContext{
		BlobGasLimit: 6 * 131_072, BlobBaseFee: eiptest.Wei(1), SlotNumber: 1,
		Requests: result.Requests, BlockAccessList: access,
	})
	if err != nil {
		t.Fatal(err)
	}
	wire, err := payload.MarshalSSZ()
	if err != nil {
		t.Fatal(err)
	}
	var decoded types.ExecutionPayloadSSZ
	if err := decoded.UnmarshalSSZ(wire); err != nil {
		t.Fatal(err)
	}
	txRLP, err := tx.EncodeRLP()
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.Transactions) != 1 || !bytes.Equal(decoded.Transactions[0], txRLP) ||
		decoded.ParentBeaconBlockRoot != root || !bytes.Equal(decoded.BlockAccessList, access) || decoded.SlotNumber != 1 {
		t.Fatal("payload changed execution fields or transaction envelope")
	}

	h := decoded.Header()
	summary, err := h.MarshalSSZ()
	if err != nil {
		t.Fatal(err)
	}
	var restored types.ExecutionHeaderSSZ
	if err := restored.UnmarshalSSZ(summary); err != nil {
		t.Fatal(err)
	}
	payloadRoot, err := decoded.HashTreeRoot()
	if err != nil {
		t.Fatal(err)
	}
	headerRoot, err := restored.HashTreeRoot()
	if err != nil || payloadRoot != headerRoot || payloadRoot == result.Block.Hash() {
		t.Fatalf("payload root %s, header root %s, RLP hash %s: %v", payloadRoot.Hex(), headerRoot.Hex(), result.Block.Hash().Hex(), err)
	}
}

func TestEIP7807_NativeRequestsThroughEngine(t *testing.T) {
	s := eiptest.New(t, eiptest.WithRequestContracts(), eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	probe := s.Engine()
	attrs := probe.Attributes()
	deposit, depositRecord := nativeDeposit(s, 0, 0, 0x41)
	withdrawal, withdrawalRecord := nativeWithdrawal(s, 1)
	consolidationRequest := types.ConsolidationRequest{
		SourceAddress: s.Alice().Address, SourcePubkey: [48]byte{0x77}, TargetPubkey: [48]byte{0x88},
	}
	input := append(bytes.Clone(consolidationRequest.SourcePubkey[:]), consolidationRequest.TargetPubkey[:]...)
	consolidation := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), 2,
		eiptest.ToPtr(types.ConsolidationRequestAddress), eiptest.Wei(1), input, 200_000, bigFeeCap, bigTipCap)
	probe.Submit(deposit, withdrawal, consolidation)

	id, err := probe.FCUWith(attrs)
	if err != nil {
		t.Fatal(err)
	}
	built, err := probe.GetPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	expected := types.Requests{
		types.NewRequest(types.DepositRequestType, depositRecord),
		types.NewRequest(types.WithdrawalRequestType, withdrawalRecord),
		types.NewRequest(types.ConsolidationRequestType, consolidationRequest.Encode()),
	}
	expectedWire := expected.Encode()
	if len(built.ExecutionRequests) != len(expectedWire) {
		t.Fatalf("Engine returned %d request groups, want %d", len(built.ExecutionRequests), len(expectedWire))
	}
	requests := make(types.Requests, len(expectedWire))
	for i, wire := range built.ExecutionRequests {
		if !bytes.Equal(wire, expectedWire[i]) {
			t.Fatalf("Engine request group %d differs from the transaction records", i)
		}
		requests[i], err = types.DecodeRequest(wire)
		if err != nil {
			t.Fatal(err)
		}
	}

	block := s.Chain().GetBlock(built.ExecutionPayload.BlockHash)
	if block == nil {
		t.Fatal("proposer did not retain the built block")
	}
	header := block.Header()
	if header.RequestsHash == nil || *header.RequestsHash != types.ComputeWireRequestsHash(expectedWire) {
		t.Fatal("block header did not commit to the Engine request groups")
	}

	// Alter a group and recompute the advertised header hash, so execution
	// must reject the disagreement between supplied and derived requests.
	wrongWire := expected.Encode()
	wrongWire[0][1] ^= 1
	wrongHash := types.ComputeWireRequestsHash(wrongWire)
	wrongHeader := block.Header()
	wrongHeader.RequestsHash = &wrongHash
	wrong := *built.ExecutionPayload
	wrong.BlockHash = types.NewBlock(wrongHeader, block.Body()).Hash()
	status, err := probe.ProcessWith(&wrong, attrs.ParentBeaconBlockRoot, wrongWire)
	if err != nil || status.Status != engine.StatusInvalid ||
		status.ValidationError == nil || !strings.Contains(*status.ValidationError, "invalid requests hash") ||
		probe.HeadHash() != s.Genesis().Hash() {
		t.Fatalf("incorrect requests were not rejected by execution: %+v: %v", status, err)
	}

	status, err = probe.Process(built.ExecutionPayload)
	if err != nil {
		t.Fatal(err)
	}
	probe.ExpectStatusValid(t, status)

	// Even the already-known proposer block must bind the wire requests.
	status, err = probe.ProcessWith(built.ExecutionPayload, attrs.ParentBeaconBlockRoot, wrongWire)
	if err != nil || status.Status != engine.StatusInvalid || status.LatestValidHash != nil || probe.HeadHash() != block.Hash() {
		t.Fatalf("known block accepted different wire requests: %+v: %v", status, err)
	}

	// State reconstruction uses Process, the same entry used for reorgs
	// and cache misses. It must apply the request queue writes exactly once.
	replayed, err := s.Chain().StateAtBlock(s.Genesis())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execution.NewStateProcessor(s.Config()).Process(block, replayed); err != nil {
		t.Fatal(err)
	}
	if replayed.GetRoot() != header.Root {
		t.Fatal("block replay omitted or repeated the request queue writes")
	}

	var accessHex string
	if err := json.Unmarshal(built.ExecutionPayload.BlockAccessList, &accessHex); err != nil {
		t.Fatal(err)
	}
	receipts := s.Chain().GetReceipts(block.Hash())
	for i, receipt := range receipts {
		if receipt.Status != types.ReceiptStatusSuccessful {
			t.Fatalf("request transaction %d reverted", i)
		}
	}
	projected, err := types.NewExecutionPayloadSSZ(block, receipts, types.ExecutionSSZContext{
		BlobGasLimit: 6 * 131_072, BlobBaseFee: eiptest.Wei(1), SlotNumber: attrs.SlotNumber,
		Requests: requests, BlockAccessList: eiptest.MustHex(t, accessHex),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := types.ExecutionRequestsSSZRoot(expected)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := projected.MarshalSSZ()
	if err != nil {
		t.Fatal(err)
	}
	var decoded types.ExecutionPayloadSSZ
	if err := decoded.UnmarshalSSZ(encoded); err != nil {
		t.Fatal(err)
	}
	if decoded.RequestsHash != wantRoot || decoded.RequestsHash == *header.RequestsHash {
		t.Fatal("SSZ projection did not commit to the typed consensus request lists")
	}

	next, status, err := probe.BuildAndProcess()
	if err != nil {
		t.Fatal(err)
	}
	probe.ExpectStatusValid(t, status)
	if len(next.ExecutionRequests) != 0 {
		t.Fatal("executed requests reappeared in a later block")
	}
}
