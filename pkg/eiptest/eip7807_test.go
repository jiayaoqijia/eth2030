package eiptest_test

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
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
