// Tests for the e2e_helpers.go shared test utilities.
// Validates that helper functions produce well-formed objects.
package e2e_test

import (
	"math/big"
	"testing"

	e2e "github.com/eth2030/eth2030"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/das"
	"github.com/eth2030/eth2030/proofs"
)

// ---------------------------------------------------------------------------
// Transaction helper tests
// ---------------------------------------------------------------------------

func TestRoadmapHelper_MakeLegacyTx(t *testing.T) {
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	tx := e2e.MakeLegacyTx(sender, receiver, 5, 1000)
	if tx == nil {
		t.Fatal("MakeLegacyTx returned nil")
	}
	if tx.Nonce() != 5 {
		t.Errorf("nonce: got %d, want 5", tx.Nonce())
	}
	if tx.Value().Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("value: got %s, want 1000", tx.Value())
	}
	if tx.Gas() != e2e.RoadmapTestTxGas {
		t.Errorf("gas: got %d, want %d", tx.Gas(), e2e.RoadmapTestTxGas)
	}
	if tx.To() == nil || *tx.To() != receiver {
		t.Errorf("to: got %v, want %x", tx.To(), receiver)
	}
}

func TestRoadmapHelper_MakeDynamicFeeTx(t *testing.T) {
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	tx := e2e.MakeDynamicFeeTx(sender, receiver, 3, 500, 100, 200)
	if tx == nil {
		t.Fatal("MakeDynamicFeeTx returned nil")
	}
	if tx.Nonce() != 3 {
		t.Errorf("nonce: got %d, want 3", tx.Nonce())
	}
	if tx.Value().Cmp(big.NewInt(500)) != 0 {
		t.Errorf("value: got %s, want 500", tx.Value())
	}
	if tx.Type() != types.DynamicFeeTxType {
		t.Errorf("type: got %d, want DynamicFeeTxType", tx.Type())
	}
}

func TestRoadmapHelper_MakeBlobTx(t *testing.T) {
	sender := types.BytesToAddress([]byte{0x01})
	receiver := types.BytesToAddress([]byte{0x02})
	tx := e2e.MakeBlobTx(sender, receiver, 1, 0x42)
	if tx == nil {
		t.Fatal("MakeBlobTx returned nil")
	}
	if tx.Nonce() != 1 {
		t.Errorf("nonce: got %d, want 1", tx.Nonce())
	}
	hashes := tx.BlobHashes()
	if len(hashes) != 1 {
		t.Fatalf("blob hashes: got %d, want 1", len(hashes))
	}
	if hashes[0][0] != 0x01 || hashes[0][1] != 0x42 {
		t.Errorf("blob hash: got %x, want [0x01 0x42 ...]", hashes[0][:4])
	}
}

func TestRoadmapHelper_MakeContractTx(t *testing.T) {
	sender := types.BytesToAddress([]byte{0x01})
	code := []byte{0x60, 0x01, 0x60, 0x00, 0x55}
	tx := e2e.MakeContractTx(sender, 0, code)
	if tx == nil {
		t.Fatal("MakeContractTx returned nil")
	}
	if tx.To() != nil {
		t.Errorf("contract creation tx should have nil To, got %v", tx.To())
	}
	if len(tx.Data()) != len(code) {
		t.Errorf("data len: got %d, want %d", len(tx.Data()), len(code))
	}
}

// ---------------------------------------------------------------------------
// Header helper tests
// ---------------------------------------------------------------------------

func TestRoadmapHelper_MakeParentHeader(t *testing.T) {
	h := e2e.MakeParentHeader()
	if h == nil {
		t.Fatal("MakeParentHeader returned nil")
	}
	if h.Number.Uint64() != 0 {
		t.Errorf("number: got %d, want 0", h.Number.Uint64())
	}
	if h.GasLimit != e2e.RoadmapTestGasLimit {
		t.Errorf("gas limit: got %d, want %d", h.GasLimit, e2e.RoadmapTestGasLimit)
	}
	if h.BaseFee.Cmp(big.NewInt(e2e.RoadmapTestBaseFee)) != 0 {
		t.Errorf("base fee: got %s, want %d", h.BaseFee, e2e.RoadmapTestBaseFee)
	}
	if h.BlobGasUsed == nil || *h.BlobGasUsed != 0 {
		t.Error("BlobGasUsed should be non-nil and zero")
	}
	if h.ExcessBlobGas == nil || *h.ExcessBlobGas != 0 {
		t.Error("ExcessBlobGas should be non-nil and zero")
	}
}

// ---------------------------------------------------------------------------
// ePBS bid helper tests
// ---------------------------------------------------------------------------

func TestRoadmapHelper_MakeBuilderBid(t *testing.T) {
	bid := e2e.MakeBuilderBid(10, 5000, 2)
	if bid == nil {
		t.Fatal("MakeBuilderBid returned nil")
	}
	if bid.Message.Slot != 10 {
		t.Errorf("slot: got %d, want 10", bid.Message.Slot)
	}
	if bid.Message.Value != 5000 {
		t.Errorf("value: got %d, want 5000", bid.Message.Value)
	}
	if bid.Message.GasLimit != e2e.RoadmapTestGasLimit {
		t.Errorf("gas limit: got %d, want %d", bid.Message.GasLimit, e2e.RoadmapTestGasLimit)
	}
	if bid.Signature[0] != 0x01 {
		t.Errorf("signature[0]: got %d, want 0x01", bid.Signature[0])
	}
}

// ---------------------------------------------------------------------------
// DAS blob helper tests
// ---------------------------------------------------------------------------

func TestRoadmapHelper_MakeBlobData(t *testing.T) {
	data := e2e.MakeBlobData(1024, 0xAA)
	if len(data) != 1024 {
		t.Fatalf("length: got %d, want 1024", len(data))
	}
	if data[0] != 0xAA {
		t.Errorf("data[0]: got %d, want 0xAA", data[0])
	}
	if data[1] != (1 ^ 0xAA) {
		t.Errorf("data[1]: got %d, want %d", data[1], 1^0xAA)
	}
}

func TestRoadmapHelper_MakeCells(t *testing.T) {
	data := e2e.MakeBlobData(das.BytesPerCell*4, 0x11)
	cells, indices := e2e.MakeCells(data, 4)
	if len(cells) != 4 || len(indices) != 4 {
		t.Fatalf("cells=%d, indices=%d, want 4", len(cells), len(indices))
	}
	for i, idx := range indices {
		if idx != uint64(i) {
			t.Errorf("index[%d]: got %d, want %d", i, idx, i)
		}
	}
}

// ---------------------------------------------------------------------------
// Proof system helper tests
// ---------------------------------------------------------------------------

func TestRoadmapHelper_RegisterProvers(t *testing.T) {
	sys := proofs.NewMandatoryProofSystem(proofs.DefaultMandatoryProofConfig())
	ids, err := e2e.RegisterProvers(sys, 5)
	if err != nil {
		t.Fatalf("RegisterProvers: %v", err)
	}
	if len(ids) != 5 {
		t.Fatalf("ids: got %d, want 5", len(ids))
	}
	// Verify uniqueness.
	seen := make(map[types.Hash]bool)
	for i, id := range ids {
		if seen[id] {
			t.Errorf("duplicate prover ID at index %d", i)
		}
		seen[id] = true
		if id == (types.Hash{}) {
			t.Errorf("zero prover ID at index %d", i)
		}
	}
}

func TestRoadmapHelper_MakeExecutionProof(t *testing.T) {
	proof := e2e.MakeExecutionProof(100)
	if proof.BlockHash == (types.Hash{}) {
		t.Error("zero block hash")
	}
	if proof.StateRoot == (types.Hash{}) {
		t.Error("zero state root")
	}
	if len(proof.ProofData) == 0 {
		t.Error("empty proof data")
	}
	if proof.Type != proofs.ZKSNARK {
		t.Errorf("type: got %d, want ZKSNARK", proof.Type)
	}
}

// ---------------------------------------------------------------------------
// Deterministic hash tests
// ---------------------------------------------------------------------------

func TestRoadmapHelper_DeterministicHash(t *testing.T) {
	h1 := e2e.DeterministicHash(1)
	h2 := e2e.DeterministicHash(2)
	h1b := e2e.DeterministicHash(1)
	if h1 == (types.Hash{}) {
		t.Error("zero hash for seed 1")
	}
	if h1 == h2 {
		t.Error("seeds 1 and 2 produced same hash")
	}
	if h1 != h1b {
		t.Error("same seed produced different hashes")
	}
}

func TestRoadmapHelper_DeterministicNodeID(t *testing.T) {
	id := e2e.DeterministicNodeID(42)
	if id == [32]byte{} {
		t.Error("zero node ID")
	}
	id2 := e2e.DeterministicNodeID(42)
	if id != id2 {
		t.Error("same seed produced different node IDs")
	}
}

func TestRoadmapHelper_DeterministicAddress(t *testing.T) {
	addr := e2e.DeterministicAddress(0x10)
	if addr == (types.Address{}) {
		t.Error("zero address")
	}
	addr2 := e2e.DeterministicAddress(0x20)
	if addr == addr2 {
		t.Error("different seeds produced same address")
	}
}

// ---------------------------------------------------------------------------
// Variable blob config tests
// ---------------------------------------------------------------------------

func TestRoadmapHelper_MakeVariableBlobConfig(t *testing.T) {
	cfg := e2e.MakeVariableBlobConfig(12, 6, das.DefaultBlobSize)
	if cfg == nil {
		t.Fatal("nil config")
	}
	if cfg.MaxBlobsPerBlock != 12 {
		t.Errorf("max blobs: got %d, want 12", cfg.MaxBlobsPerBlock)
	}
	if cfg.TargetBlobsPerBlock != 6 {
		t.Errorf("target blobs: got %d, want 6", cfg.TargetBlobsPerBlock)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}
