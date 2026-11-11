package gossip

import (
	"testing"

	"github.com/eth2030/eth2030/crypto/bls"
	"github.com/eth2030/eth2030/das/dastypes"
)

// --- helpers ---

func colBuilderTestBlobs(numBlobs, blobSize int) [][]byte {
	blobs := make([][]byte, numBlobs)
	for i := 0; i < numBlobs; i++ {
		blobs[i] = make([]byte, bls.KZGBytesPerBlob)
		for j := 0; j < blobSize/32; j++ {
			blobs[i][j*32+31] = byte(i + j + 1)
		}
	}
	return blobs
}

// --- tests ---

func TestColumnBuilderBuildColumnsBasic(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 8
	cfg.MaxBlobs = 4
	cfg.CellSize = 16
	cb := NewColumnBuilder(cfg)

	blobs := colBuilderTestBlobs(2, 8*16) // 2 blobs, each 128 bytes
	cols, err := cb.BuildColumns(blobs)
	if err != nil {
		t.Fatalf("BuildColumns: %v", err)
	}
	if len(cols) != 8 {
		t.Fatalf("expected 8 columns, got %d", len(cols))
	}
	for _, col := range cols {
		if col.BlobCount != 2 {
			t.Errorf("column %d: BlobCount = %d, want 2", col.Index, col.BlobCount)
		}
		if len(col.Cells) != 2 {
			t.Errorf("column %d: len(Cells) = %d, want 2", col.Index, len(col.Cells))
		}
		if len(col.Proofs) != 2 {
			t.Errorf("column %d: len(Proofs) = %d, want 2", col.Index, len(col.Proofs))
		}
		if col.Commitment == [32]byte{} {
			t.Errorf("column %d: commitment is zero", col.Index)
		}
	}
}

func TestColumnBuilderBuildColumnsNilBlobs(t *testing.T) {
	cb := NewColumnBuilder(DefaultColumnBuilderConfig())
	_, err := cb.BuildColumns(nil)
	if err != ErrBuilderNilBlobs {
		t.Fatalf("expected ErrBuilderNilBlobs, got %v", err)
	}
}

func TestColumnBuilderBuildColumnsEmpty(t *testing.T) {
	cb := NewColumnBuilder(DefaultColumnBuilderConfig())
	_, err := cb.BuildColumns([][]byte{})
	if err != ErrBuilderEmptyBlobs {
		t.Fatalf("expected ErrBuilderEmptyBlobs, got %v", err)
	}
}

func TestColumnBuilderBuildColumnsTooManyBlobs(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.MaxBlobs = 2
	cb := NewColumnBuilder(cfg)

	blobs := colBuilderTestBlobs(3, 128)
	_, err := cb.BuildColumns(blobs)
	if err == nil {
		t.Fatal("expected error for too many blobs")
	}
}

func TestColumnBuilderBuildColumnSubset(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 16
	cfg.CellSize = 8
	cb := NewColumnBuilder(cfg)

	blobs := colBuilderTestBlobs(2, 16*8)
	indices := []dastypes.ColumnIndex{3, 7, 12}
	cols, err := cb.BuildColumnSubset(blobs, indices)
	if err != nil {
		t.Fatalf("BuildColumnSubset: %v", err)
	}
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns, got %d", len(cols))
	}
	// Verify sorted order.
	for i := 1; i < len(cols); i++ {
		if cols[i].Index <= cols[i-1].Index {
			t.Errorf("columns not sorted: %d <= %d", cols[i].Index, cols[i-1].Index)
		}
	}
}

func TestColumnBuilderBuildColumnSubsetOOB(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 8
	cb := NewColumnBuilder(cfg)

	blobs := colBuilderTestBlobs(1, 128)
	_, err := cb.BuildColumnSubset(blobs, []dastypes.ColumnIndex{10})
	if err == nil {
		t.Fatal("expected error for OOB column index")
	}
}

func TestColumnBuilderBuildColumnSubsetDuplicate(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 8
	cb := NewColumnBuilder(cfg)

	blobs := colBuilderTestBlobs(1, 128)
	_, err := cb.BuildColumnSubset(blobs, []dastypes.ColumnIndex{3, 3})
	if err == nil {
		t.Fatal("expected error for duplicate column index")
	}
}

func TestColumnBuilderGetBuiltColumn(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 4
	cfg.CellSize = 8
	cb := NewColumnBuilder(cfg)

	blobs := colBuilderTestBlobs(1, 4*8)
	_, err := cb.BuildColumns(blobs)
	if err != nil {
		t.Fatalf("BuildColumns: %v", err)
	}

	col := cb.GetBuiltColumn(2)
	if col == nil {
		t.Fatal("GetBuiltColumn(2) returned nil")
	}
	if col.Index != 2 {
		t.Errorf("Index = %d, want 2", col.Index)
	}

	// Non-existent column returns nil.
	if cb.GetBuiltColumn(100) != nil {
		t.Error("expected nil for non-existent column")
	}
}

func TestColumnBuilderBuiltColumnCount(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 4
	cfg.CellSize = 8
	cb := NewColumnBuilder(cfg)

	if cb.BuiltColumnCount() != 0 {
		t.Error("expected 0 initially")
	}

	blobs := colBuilderTestBlobs(1, 4*8)
	_, _ = cb.BuildColumns(blobs)
	if cb.BuiltColumnCount() != 4 {
		t.Errorf("BuiltColumnCount = %d, want 4", cb.BuiltColumnCount())
	}
}

func TestColumnBuilderAssignCustodyColumns(t *testing.T) {
	cb := NewColumnBuilder(DefaultColumnBuilderConfig())

	var nodeID [32]byte
	nodeID[0] = 0xAB
	result, err := cb.AssignCustodyColumns(nodeID, dastypes.CustodyRequirement)
	if err != nil {
		t.Fatalf("AssignCustodyColumns: %v", err)
	}
	if len(result.ColumnIndices) == 0 {
		t.Error("expected non-empty column indices")
	}
	if len(result.GroupIndices) != int(dastypes.CustodyRequirement) {
		t.Errorf("GroupIndices count = %d, want %d", len(result.GroupIndices), dastypes.CustodyRequirement)
	}
	if result.NodeID != nodeID {
		t.Error("NodeID mismatch")
	}

	// Verify columns are sorted.
	for i := 1; i < len(result.ColumnIndices); i++ {
		if result.ColumnIndices[i] <= result.ColumnIndices[i-1] {
			t.Errorf("column indices not sorted at position %d", i)
		}
	}
}

func TestColumnBuilderCreateGossipMessage(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 16
	cfg.CellSize = 64
	cb := NewColumnBuilder(cfg)

	cellData := make([]byte, 32)
	for i := range cellData {
		cellData[i] = byte(i)
	}
	var proof dastypes.KZGProof
	proof[0] = 0xFF

	msg, err := cb.CreateGossipMessage(100, 2, 5, cellData, proof)
	if err != nil {
		t.Fatalf("CreateGossipMessage: %v", err)
	}
	if msg.Slot != 100 {
		t.Errorf("Slot = %d, want 100", msg.Slot)
	}
	if msg.BlobIndex != 2 {
		t.Errorf("BlobIndex = %d, want 2", msg.BlobIndex)
	}
	if msg.ColumnIndex != 5 {
		t.Errorf("ColumnIndex = %d, want 5", msg.ColumnIndex)
	}
	if msg.MessageHash == [32]byte{} {
		t.Error("MessageHash should be non-zero")
	}
}

func TestColumnBuilderCreateGossipMessageOOB(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 8
	cfg.MaxBlobs = 4
	cfg.CellSize = 16
	cb := NewColumnBuilder(cfg)

	cellData := make([]byte, 8)
	var proof dastypes.KZGProof

	// Column out of range.
	_, err := cb.CreateGossipMessage(1, 0, dastypes.ColumnIndex(10), cellData, proof)
	if err == nil {
		t.Error("expected error for column OOB")
	}

	// Blob out of range.
	_, err = cb.CreateGossipMessage(1, 10, 0, cellData, proof)
	if err == nil {
		t.Error("expected error for blob OOB")
	}

	// Empty cell data.
	_, err = cb.CreateGossipMessage(1, 0, 0, nil, proof)
	if err == nil {
		t.Error("expected error for empty cell data")
	}
}

func TestColumnBuilderValidateGossipMessage(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 8
	cfg.MaxBlobs = 4
	cfg.CellSize = 64
	cb := NewColumnBuilder(cfg)

	// Valid message.
	msg := &ColumnGossipMessage{
		ColumnIndex: 3,
		Slot:        10,
		BlobIndex:   1,
		CellData:    make([]byte, 32),
	}
	if err := cb.ValidateGossipMessage(msg); err != nil {
		t.Fatalf("ValidateGossipMessage: %v", err)
	}

	// Nil message.
	if err := cb.ValidateGossipMessage(nil); err != ErrBuilderMsgNil {
		t.Errorf("expected ErrBuilderMsgNil, got %v", err)
	}

	// Column OOB.
	badMsg := &ColumnGossipMessage{ColumnIndex: 100, CellData: make([]byte, 8)}
	if err := cb.ValidateGossipMessage(badMsg); err == nil {
		t.Error("expected error for column OOB")
	}

	// Blob OOB.
	badMsg2 := &ColumnGossipMessage{ColumnIndex: 1, BlobIndex: 100, CellData: make([]byte, 8)}
	if err := cb.ValidateGossipMessage(badMsg2); err == nil {
		t.Error("expected error for blob OOB")
	}

	// Empty cell data.
	badMsg3 := &ColumnGossipMessage{ColumnIndex: 1, BlobIndex: 0, CellData: nil}
	if err := cb.ValidateGossipMessage(badMsg3); err == nil {
		t.Error("expected error for empty cell data")
	}
}

func TestColumnBuilderCheckDuplicate(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 16
	cfg.CellSize = 32
	cfg.MaxDedup = 100
	cb := NewColumnBuilder(cfg)

	msg, _ := cb.CreateGossipMessage(1, 0, 3, make([]byte, 8), dastypes.KZGProof{})

	// First check should succeed.
	if err := cb.CheckDuplicate(msg); err != nil {
		t.Fatalf("first CheckDuplicate: %v", err)
	}

	// Second check should return duplicate.
	if err := cb.CheckDuplicate(msg); err != ErrBuilderAlreadySeen {
		t.Fatalf("expected ErrBuilderAlreadySeen, got %v", err)
	}
}

func TestColumnBuilderCheckDuplicateNil(t *testing.T) {
	cb := NewColumnBuilder(DefaultColumnBuilderConfig())
	if err := cb.CheckDuplicate(nil); err != ErrBuilderMsgNil {
		t.Fatalf("expected ErrBuilderMsgNil, got %v", err)
	}
}

func TestColumnBuilderDedupEviction(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 256
	cfg.CellSize = 32
	cfg.MaxDedup = 5
	cb := NewColumnBuilder(cfg)

	// Insert more than MaxDedup messages.
	for i := 0; i < 10; i++ {
		msg, _ := cb.CreateGossipMessage(uint64(i), 0, dastypes.ColumnIndex(i%256), make([]byte, 8), dastypes.KZGProof{})
		_ = cb.CheckDuplicate(msg)
	}

	// Dedup set should not exceed MaxDedup.
	if cb.DedupSize() > 5 {
		t.Errorf("DedupSize = %d, want <= 5", cb.DedupSize())
	}
}

func TestColumnBuilderClearBuilt(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 4
	cfg.CellSize = 8
	cb := NewColumnBuilder(cfg)

	blobs := colBuilderTestBlobs(1, 4*8)
	_, _ = cb.BuildColumns(blobs)
	if cb.BuiltColumnCount() == 0 {
		t.Fatal("expected non-zero count after build")
	}

	cb.ClearBuilt()
	if cb.BuiltColumnCount() != 0 {
		t.Errorf("BuiltColumnCount = %d after clear, want 0", cb.BuiltColumnCount())
	}
}

func TestColumnBuilderClearDedup(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 16
	cfg.CellSize = 32
	cb := NewColumnBuilder(cfg)

	msg, _ := cb.CreateGossipMessage(1, 0, 3, make([]byte, 8), dastypes.KZGProof{})
	_ = cb.CheckDuplicate(msg)
	if cb.DedupSize() != 1 {
		t.Fatal("expected 1 dedup entry")
	}

	cb.ClearDedup()
	if cb.DedupSize() != 0 {
		t.Error("DedupSize != 0 after clear")
	}

	// Should be able to re-add the same message.
	if err := cb.CheckDuplicate(msg); err != nil {
		t.Errorf("re-add after clear: %v", err)
	}
}

func TestColumnBuilderToDataColumnSidecar(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 4
	cfg.CellSize = 8
	cb := NewColumnBuilder(cfg)

	blobs := colBuilderTestBlobs(2, 4*8)
	cols, err := cb.BuildColumns(blobs)
	if err != nil {
		t.Fatalf("BuildColumns: %v", err)
	}

	backend := bls.DefaultKZGBackend()
	commitments := make([]dastypes.KZGCommitment, len(blobs))
	for i, blob := range blobs {
		commitment, err := backend.BlobToCommitment(blob)
		if err != nil {
			t.Fatal(err)
		}
		commitments[i] = commitment
	}

	message, err := cb.CreateGossipMessage(1, 0, cols[1].Index, cols[1].Cells[0][:], cols[1].Proofs[0])
	if err != nil {
		t.Fatalf("constructed cell rejected by gossip: %v", err)
	}
	if err := cb.ValidateGossipMessage(message); err != nil {
		t.Fatalf("validate constructed cell: %v", err)
	}

	sidecar := cb.ToDataColumnSidecar(cols[1], commitments)
	if sidecar.Index != cols[1].Index || len(sidecar.Column) != len(blobs) ||
		len(sidecar.KZGCommitments) != len(blobs) || len(sidecar.KZGProofs) != len(blobs) {
		t.Fatalf("unexpected sidecar shape for column %d", sidecar.Index)
	}
	for i, cell := range sidecar.Column {
		valid, err := backend.VerifyCellProof(sidecar.KZGCommitments[i][:], cell[:],
			sidecar.KZGProofs[i][:], uint64(sidecar.Index))
		if err != nil || !valid {
			t.Fatalf("sidecar cell %d proof = %v, %v", i, valid, err)
		}
	}
}

func TestColumnBuilderColumnCommitmentDeterminism(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	cfg.NumColumns = 4
	cfg.CellSize = 8
	cb := NewColumnBuilder(cfg)

	blobs := colBuilderTestBlobs(1, 4*8)

	cols1, _ := cb.BuildColumns(blobs)
	cb.ClearBuilt()
	cols2, _ := cb.BuildColumns(blobs)

	for i := range cols1 {
		if cols1[i].Commitment != cols2[i].Commitment {
			t.Errorf("column %d: commitments differ across builds", i)
		}
	}
}

func TestColumnBuilderDefaultConfig(t *testing.T) {
	cfg := DefaultColumnBuilderConfig()
	if cfg.NumColumns != dastypes.NumberOfColumns {
		t.Errorf("NumColumns = %d, want %d", cfg.NumColumns, dastypes.NumberOfColumns)
	}
	if cfg.MaxBlobs != dastypes.MaxBlobCommitmentsPerBlock {
		t.Errorf("MaxBlobs = %d, want %d", cfg.MaxBlobs, dastypes.MaxBlobCommitmentsPerBlock)
	}
	if cfg.CellSize != dastypes.BytesPerCell {
		t.Errorf("CellSize = %d, want %d", cfg.CellSize, dastypes.BytesPerCell)
	}
}
