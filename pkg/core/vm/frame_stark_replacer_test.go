package vm

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/proofs"
)

// errorProver is a prover that always returns an error.
type errorProver struct{}

func (e *errorProver) ProveValidationFrame(_, _ []byte) (*proofs.STARKProofData, error) {
	return nil, errors.New("prover error")
}

func (e *errorProver) ProveAllValidationFrames(_ [][]byte) (*proofs.STARKProofData, error) {
	return nil, errors.New("prover error")
}

func (e *errorProver) Verify(_ *proofs.STARKProofData) bool {
	return false
}

func makeTestBlock(txs []*types.Transaction) *types.Block {
	header := &types.Header{
		Number: big.NewInt(1),
	}
	body := &types.Body{Transactions: txs}
	return types.NewBlock(header, body)
}

func makeFrameTx(data []byte) *types.Transaction {
	return types.NewTransaction(&types.FrameTx{
		ChainID:              big.NewInt(1),
		Nonce:                1,
		MaxPriorityFeePerGas: big.NewInt(1),
		MaxFeePerGas:         big.NewInt(1),
		Frames: []types.Frame{
			{Mode: types.ModeVerify, Data: data, GasLimit: 21000},
		},
	})
}

func makeLegacyTx() *types.Transaction {
	return types.NewTransaction(&types.LegacyTx{
		Nonce:    1,
		GasPrice: big.NewInt(1),
		Gas:      21000,
		Value:    big.NewInt(0),
		Data:     []byte{0x01, 0x02},
	})
}

func TestReplaceValidationFrames_NoFrameTxs(t *testing.T) {
	block := makeTestBlock([]*types.Transaction{makeLegacyTx()})
	stub := &proofs.StubValidationFrameProver{}

	result, proof, err := ReplaceValidationFrames(block, stub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proof != nil {
		t.Fatal("expected nil proof for block with no frame txs")
	}
	if result != block {
		t.Fatal("expected same block returned")
	}
}

func TestReplaceValidationFrames_WithFrameTxs(t *testing.T) {
	txs := []*types.Transaction{
		makeLegacyTx(),
		makeFrameTx([]byte{0xAA, 0xBB}),
		makeFrameTx([]byte{0xCC, 0xDD}),
	}
	block := makeTestBlock(txs)
	stub := &proofs.StubValidationFrameProver{}

	result, proof, err := ReplaceValidationFrames(block, stub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proof == nil {
		t.Fatal("expected non-nil proof for block with frame txs")
	}
	if result != block {
		t.Fatal("expected same block returned")
	}
}

func TestReplaceValidationFrames_ProverError(t *testing.T) {
	txs := []*types.Transaction{
		makeFrameTx([]byte{0xAA}),
	}
	block := makeTestBlock(txs)
	ep := &errorProver{}

	result, proof, err := ReplaceValidationFrames(block, ep)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proof != nil {
		t.Fatal("expected nil proof when prover fails")
	}
	if result != block {
		t.Fatal("expected original block returned")
	}
}

func TestReplaceValidationFrames_BlockUnchanged(t *testing.T) {
	txs := []*types.Transaction{
		makeFrameTx([]byte{0xAA, 0xBB}),
		makeLegacyTx(),
	}
	block := makeTestBlock(txs)
	hashBefore := block.Hash()
	stub := &proofs.StubValidationFrameProver{}

	result, _, err := ReplaceValidationFrames(block, stub)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	hashAfter := result.Hash()
	if hashBefore != hashAfter {
		t.Fatalf("block hash changed: before=%x after=%x", hashBefore, hashAfter)
	}
}
