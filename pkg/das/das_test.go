package das

import (
	"testing"
)

func TestConstants(t *testing.T) {
	if NumberOfColumns != 128 {
		t.Errorf("NumberOfColumns = %d, want 128", NumberOfColumns)
	}
	if NumberOfCustodyGroups != 128 {
		t.Errorf("NumberOfCustodyGroups = %d, want 128", NumberOfCustodyGroups)
	}
	if CustodyRequirement != 4 {
		t.Errorf("CustodyRequirement = %d, want 4", CustodyRequirement)
	}
	if SamplesPerSlot != 8 {
		t.Errorf("SamplesPerSlot = %d, want 8", SamplesPerSlot)
	}
	if DataColumnSidecarSubnetCount != 64 {
		t.Errorf("DataColumnSidecarSubnetCount = %d, want 64", DataColumnSidecarSubnetCount)
	}
	if CellsPerExtBlob != 128 {
		t.Errorf("CellsPerExtBlob = %d, want 128", CellsPerExtBlob)
	}
	if FieldElementsPerCell != 64 {
		t.Errorf("FieldElementsPerCell = %d, want 64", FieldElementsPerCell)
	}
	if BytesPerCell != 2048 {
		t.Errorf("BytesPerCell = %d, want 2048", BytesPerCell)
	}
	if ReconstructionThreshold != 64 {
		t.Errorf("ReconstructionThreshold = %d, want 64", ReconstructionThreshold)
	}
}

func TestGetCustodyGroups(t *testing.T) {
	nodeID := [32]byte{0x01, 0x02, 0x03}

	groups, err := GetCustodyGroups(nodeID, CustodyRequirement)
	if err != nil {
		t.Fatalf("GetCustodyGroups: %v", err)
	}

	if len(groups) != int(CustodyRequirement) {
		t.Fatalf("expected %d groups, got %d", CustodyRequirement, len(groups))
	}

	// Verify groups are sorted.
	for i := 1; i < len(groups); i++ {
		if groups[i] <= groups[i-1] {
			t.Errorf("groups not sorted: groups[%d]=%d <= groups[%d]=%d",
				i, groups[i], i-1, groups[i-1])
		}
	}

	// Verify all groups are in valid range.
	for _, g := range groups {
		if uint64(g) >= NumberOfCustodyGroups {
			t.Errorf("group %d out of range [0, %d)", g, NumberOfCustodyGroups)
		}
	}

	// Verify uniqueness.
	seen := make(map[CustodyGroup]bool)
	for _, g := range groups {
		if seen[g] {
			t.Errorf("duplicate group %d", g)
		}
		seen[g] = true
	}
}

func TestGetCustodyGroupsAllGroups(t *testing.T) {
	nodeID := [32]byte{0xff}

	groups, err := GetCustodyGroups(nodeID, NumberOfCustodyGroups)
	if err != nil {
		t.Fatalf("GetCustodyGroups (all): %v", err)
	}

	if len(groups) != NumberOfCustodyGroups {
		t.Fatalf("expected %d groups, got %d", NumberOfCustodyGroups, len(groups))
	}

	// All groups should be present.
	for i := uint64(0); i < NumberOfCustodyGroups; i++ {
		if groups[i] != CustodyGroup(i) {
			t.Errorf("groups[%d] = %d, want %d", i, groups[i], i)
		}
	}
}

func TestGetCustodyGroupsInvalidCount(t *testing.T) {
	nodeID := [32]byte{}
	_, err := GetCustodyGroups(nodeID, NumberOfCustodyGroups+1)
	if err != ErrInvalidCustodyCount {
		t.Fatalf("expected ErrInvalidCustodyCount, got %v", err)
	}
}

func TestGetCustodyGroupsDeterministic(t *testing.T) {
	nodeID := [32]byte{0xaa, 0xbb, 0xcc}
	groups1, _ := GetCustodyGroups(nodeID, CustodyRequirement)
	groups2, _ := GetCustodyGroups(nodeID, CustodyRequirement)

	if len(groups1) != len(groups2) {
		t.Fatal("non-deterministic result lengths")
	}
	for i := range groups1 {
		if groups1[i] != groups2[i] {
			t.Fatalf("non-deterministic: groups1[%d]=%d != groups2[%d]=%d",
				i, groups1[i], i, groups2[i])
		}
	}
}

func TestGetCustodyGroupsExtension(t *testing.T) {
	// The spec says increasing custody_size extends the returned list.
	nodeID := [32]byte{0x42}
	small, _ := GetCustodyGroups(nodeID, 4)
	large, _ := GetCustodyGroups(nodeID, 8)

	// All groups from the smaller set should appear in the larger set.
	largeSet := make(map[CustodyGroup]bool)
	for _, g := range large {
		largeSet[g] = true
	}
	for _, g := range small {
		if !largeSet[g] {
			t.Errorf("group %d in small set but not in large set", g)
		}
	}
}

func TestComputeColumnsForCustodyGroup(t *testing.T) {
	// Group 0 should get columns {0, 128, 256, ...} but since
	// NumberOfColumns == NumberOfCustodyGroups == 128, columns_per_group = 1.
	cols, err := ComputeColumnsForCustodyGroup(0)
	if err != nil {
		t.Fatalf("ComputeColumnsForCustodyGroup(0): %v", err)
	}
	if len(cols) != 1 {
		t.Fatalf("expected 1 column for group 0, got %d", len(cols))
	}
	if cols[0] != 0 {
		t.Errorf("expected column 0, got %d", cols[0])
	}

	// Group 5 should get column 5.
	cols, err = ComputeColumnsForCustodyGroup(5)
	if err != nil {
		t.Fatalf("ComputeColumnsForCustodyGroup(5): %v", err)
	}
	if cols[0] != 5 {
		t.Errorf("expected column 5, got %d", cols[0])
	}

	// Group 127 should get column 127.
	cols, err = ComputeColumnsForCustodyGroup(127)
	if err != nil {
		t.Fatalf("ComputeColumnsForCustodyGroup(127): %v", err)
	}
	if cols[0] != 127 {
		t.Errorf("expected column 127, got %d", cols[0])
	}

	// Invalid group.
	_, err = ComputeColumnsForCustodyGroup(128)
	if err == nil {
		t.Fatal("expected error for group 128")
	}
}

func TestGetCustodyColumns(t *testing.T) {
	nodeID := [32]byte{0x01, 0x02}
	columns, err := GetCustodyColumns(nodeID, CustodyRequirement)
	if err != nil {
		t.Fatalf("GetCustodyColumns: %v", err)
	}

	// With CustodyRequirement=4 and columns_per_group=1, we get 4 columns.
	if len(columns) != int(CustodyRequirement) {
		t.Fatalf("expected %d columns, got %d", CustodyRequirement, len(columns))
	}

	// Columns should be sorted.
	for i := 1; i < len(columns); i++ {
		if columns[i] <= columns[i-1] {
			t.Errorf("columns not sorted at index %d", i)
		}
	}

	// All columns in range.
	for _, c := range columns {
		if uint64(c) >= NumberOfColumns {
			t.Errorf("column %d out of range", c)
		}
	}
}

func TestShouldCustodyColumn(t *testing.T) {
	columns := []ColumnIndex{3, 17, 42, 100}

	if !ShouldCustodyColumn(3, columns) {
		t.Error("should custody column 3")
	}
	if !ShouldCustodyColumn(42, columns) {
		t.Error("should custody column 42")
	}
	if ShouldCustodyColumn(5, columns) {
		t.Error("should not custody column 5")
	}
	if ShouldCustodyColumn(127, columns) {
		t.Error("should not custody column 127")
	}
}

func TestVerifyDataColumnSidecar(t *testing.T) {
	// Valid sidecar.
	sidecar := &DataColumnSidecar{
		Index:          5,
		Column:         []Cell{{}},
		KZGCommitments: []KZGCommitment{{}},
		KZGProofs:      []KZGProof{{}},
	}
	if err := VerifyDataColumnSidecar(sidecar); err != nil {
		t.Fatalf("valid sidecar failed: %v", err)
	}

	// Nil sidecar.
	if err := VerifyDataColumnSidecar(nil); err != ErrInvalidSidecar {
		t.Errorf("expected ErrInvalidSidecar for nil, got %v", err)
	}

	// Column index out of range.
	bad := &DataColumnSidecar{
		Index:          128,
		Column:         []Cell{{}},
		KZGCommitments: []KZGCommitment{{}},
		KZGProofs:      []KZGProof{{}},
	}
	if err := VerifyDataColumnSidecar(bad); err == nil {
		t.Error("expected error for column index 128")
	}

	// Empty column.
	bad = &DataColumnSidecar{
		Index:          0,
		Column:         []Cell{},
		KZGCommitments: []KZGCommitment{},
		KZGProofs:      []KZGProof{},
	}
	if err := VerifyDataColumnSidecar(bad); err == nil {
		t.Error("expected error for empty column")
	}

	// Mismatched lengths.
	bad = &DataColumnSidecar{
		Index:          0,
		Column:         []Cell{{}, {}},
		KZGCommitments: []KZGCommitment{{}},
		KZGProofs:      []KZGProof{{}, {}},
	}
	if err := VerifyDataColumnSidecar(bad); err == nil {
		t.Error("expected error for mismatched lengths")
	}

	// Too many cells.
	manyCells := make([]Cell, MaxBlobCommitmentsPerBlock+1)
	manyCommits := make([]KZGCommitment, MaxBlobCommitmentsPerBlock+1)
	manyProofs := make([]KZGProof, MaxBlobCommitmentsPerBlock+1)
	bad = &DataColumnSidecar{
		Index:          0,
		Column:         manyCells,
		KZGCommitments: manyCommits,
		KZGProofs:      manyProofs,
	}
	if err := VerifyDataColumnSidecar(bad); err == nil {
		t.Error("expected error for too many cells")
	}
}

func TestColumnSubnet(t *testing.T) {
	tests := []struct {
		col  ColumnIndex
		want SubnetID
	}{
		{0, 0},
		{1, 1},
		{63, 63},
		{64, 0},
		{65, 1},
		{127, 63},
	}
	for _, tt := range tests {
		got := ColumnSubnet(tt.col)
		if got != tt.want {
			t.Errorf("ColumnSubnet(%d) = %d, want %d", tt.col, got, tt.want)
		}
	}
}

func TestCanReconstruct(t *testing.T) {
	if CanReconstruct(0) {
		t.Error("should not reconstruct with 0 cells")
	}
	if CanReconstruct(63) {
		t.Error("should not reconstruct with 63 cells")
	}
	if !CanReconstruct(64) {
		t.Error("should reconstruct with 64 cells")
	}
	if !CanReconstruct(128) {
		t.Error("should reconstruct with 128 cells")
	}
}

func TestReconstructBlobValidation(t *testing.T) {
	// Mismatched cells and indices.
	_, err := ReconstructBlob([]Cell{{}}, []uint64{0, 1})
	if err == nil {
		t.Error("expected error for mismatched lengths")
	}

	// Too few cells.
	cells := make([]Cell, 10)
	indices := make([]uint64, 10)
	for i := range indices {
		indices[i] = uint64(i)
	}
	_, err = ReconstructBlob(cells, indices)
	if err == nil {
		t.Error("expected error for too few cells")
	}

	// Index out of range.
	cells = make([]Cell, ReconstructionThreshold)
	indices = make([]uint64, ReconstructionThreshold)
	for i := range indices {
		indices[i] = uint64(i)
	}
	indices[0] = CellsPerExtBlob // Out of range
	_, err = ReconstructBlob(cells, indices)
	if err == nil {
		t.Error("expected error for index out of range")
	}

	// Duplicate indices.
	for i := range indices {
		indices[i] = uint64(i)
	}
	indices[1] = indices[0]
	_, err = ReconstructBlob(cells, indices)
	if err == nil {
		t.Error("expected error for duplicate indices")
	}
}

func TestReconstructBlobSuccess(t *testing.T) {
	// Create cells for the first 64 cells (original blob indices).
	// Use zero-filled cells so we can verify that the RS reconstruction
	// produces the correct blob size.
	cells := make([]Cell, ReconstructionThreshold)
	indices := make([]uint64, ReconstructionThreshold)
	for i := range cells {
		indices[i] = uint64(i)
		// Zero-filled cells: RS reconstruction of all-zero evaluations
		// should produce all-zero blob data.
	}

	result, err := ReconstructBlob(cells, indices)
	if err != nil {
		t.Fatalf("ReconstructBlob: %v", err)
	}

	expectedSize := FieldElementsPerBlob * BytesPerFieldElement
	if len(result) != expectedSize {
		t.Fatalf("result size = %d, want %d", len(result), expectedSize)
	}

	// With all-zero input cells, the interpolated polynomial is zero,
	// so all output bytes should be zero.
	for j, b := range result {
		if b != 0 {
			t.Fatalf("result[%d] = %d, want 0 (zero polynomial)", j, b)
			break
		}
	}
}

func TestRecoverMatrix(t *testing.T) {
	// Empty blob count should fail.
	_, err := RecoverMatrix(nil, 0)
	if err == nil {
		t.Error("expected error for zero blob count")
	}

	// Insufficient entries for one row.
	entries := make([]MatrixEntry, 10)
	for i := range entries {
		entries[i] = MatrixEntry{RowIndex: 0, ColumnIndex: ColumnIndex(i)}
	}
	_, err = RecoverMatrix(entries, 1)
	if err == nil {
		t.Error("expected error for insufficient cells")
	}

	// Sufficient entries (64+ per row).
	entries = make([]MatrixEntry, ReconstructionThreshold)
	for i := range entries {
		entries[i] = MatrixEntry{RowIndex: 0, ColumnIndex: ColumnIndex(i)}
	}
	result, err := RecoverMatrix(entries, 1)
	if err != nil {
		t.Fatalf("RecoverMatrix: %v", err)
	}
	// After RS recovery, we get all CellsPerExtBlob cells per row.
	if len(result) != CellsPerExtBlob {
		t.Fatalf("expected %d entries, got %d", CellsPerExtBlob, len(result))
	}
}

func TestDifferentNodeIDsGetDifferentGroups(t *testing.T) {
	id1 := [32]byte{0x01}
	id2 := [32]byte{0x02}

	g1, _ := GetCustodyGroups(id1, CustodyRequirement)
	g2, _ := GetCustodyGroups(id2, CustodyRequirement)

	// Not guaranteed to be different, but with different inputs it's extremely
	// likely. Check that at least the first group differs (probabilistic).
	allSame := true
	for i := range g1 {
		if g1[i] != g2[i] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Error("fixed node IDs produced identical custody groups")
	}
}
