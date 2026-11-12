package dastypes

import "testing"

func TestConstants(t *testing.T) {
	if NumberOfColumns != 128 {
		t.Errorf("NumberOfColumns = %d, want 128", NumberOfColumns)
	}
	if BytesPerCell != FieldElementsPerCell*BytesPerFieldElement {
		t.Errorf("BytesPerCell = %d, want %d", BytesPerCell, FieldElementsPerCell*BytesPerFieldElement)
	}
	if CellsPerExtBlob != FieldElementsPerExtBlob/FieldElementsPerCell {
		t.Errorf("CellsPerExtBlob = %d, want %d", CellsPerExtBlob, FieldElementsPerExtBlob/FieldElementsPerCell)
	}
	if ReconstructionThreshold != NumberOfColumns/2 {
		t.Errorf("ReconstructionThreshold = %d, want %d", ReconstructionThreshold, NumberOfColumns/2)
	}
}

func TestCellSize(t *testing.T) {
	var c Cell
	if len(c) != BytesPerCell {
		t.Errorf("Cell size = %d, want %d", len(c), BytesPerCell)
	}
}

func TestKZGTypes(t *testing.T) {
	var commitment KZGCommitment
	var proof KZGProof
	if len(commitment) != 48 {
		t.Errorf("KZGCommitment size = %d, want 48", len(commitment))
	}
	if len(proof) != 48 {
		t.Errorf("KZGProof size = %d, want 48", len(proof))
	}
}

func TestComputeSubnetForDataColumnSidecar(t *testing.T) {
	cases := []struct {
		index ColumnIndex
		want  uint64
	}{
		{0, 0},
		{1, 1},
		{63, 63},
		{64, 0},
		{65, 1},
		{127, 63},
	}
	for _, c := range cases {
		if got := ComputeSubnetForDataColumnSidecar(c.index); got != c.want {
			t.Errorf("column %d: subnet = %d, want %d", c.index, got, c.want)
		}
	}
}

func TestValidateSidecarSubnet(t *testing.T) {
	sidecar := &DataColumnSidecar{Index: 65}
	// Column 65 belongs to subnet 1.
	if err := ValidateSidecarSubnet(sidecar, 1); err != nil {
		t.Fatalf("correct subnet rejected: %v", err)
	}
	if err := ValidateSidecarSubnet(sidecar, 2); err == nil {
		t.Fatal("wrong subnet must be rejected")
	}
	if err := ValidateSidecarSubnet(nil, 0); err == nil {
		t.Fatal("nil sidecar must be rejected")
	}
}

func TestValidateSidecarSlot(t *testing.T) {
	sidecar := &DataColumnSidecar{
		SignedBlockHeader: &SignedBeaconBlockHeader{Slot: 100},
	}
	if err := ValidateSidecarSlot(sidecar, 100, 64); err != nil {
		t.Fatalf("current-slot sidecar rejected: %v", err)
	}
	// Future slot: IGNORE per gossip rules.
	if err := ValidateSidecarSlot(sidecar, 99, 0); err == nil {
		t.Fatal("future sidecar must be rejected")
	}
	// Not newer than finalized: IGNORE per gossip rules.
	if err := ValidateSidecarSlot(sidecar, 200, 100); err == nil {
		t.Fatal("finalized sidecar must be rejected")
	}
	// Missing header.
	if err := ValidateSidecarSlot(&DataColumnSidecar{Index: 1}, 100, 0); err == nil {
		t.Fatal("sidecar without header must be rejected")
	}
}
