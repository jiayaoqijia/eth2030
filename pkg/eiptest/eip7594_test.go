package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/das/custody"
	dastypes "github.com/eth2030/eth2030/das/dastypes"
	dasgossip "github.com/eth2030/eth2030/das/gossip"
	"github.com/eth2030/eth2030/das/reconstruction"
	"github.com/eth2030/eth2030/p2p/gossip"
)

// buildEIP7594Sidecar constructs a DataColumnSidecar through the real
// builder path, attaching a signed block header at slot 100.
func buildEIP7594Sidecar(
	columnIndex dastypes.ColumnIndex,
	cells []dastypes.Cell,
	commitments []dastypes.KZGCommitment,
	proofs []dastypes.KZGProof,
) (*dastypes.DataColumnSidecar, error) {
	header := &dastypes.SignedBeaconBlockHeader{Slot: 100, ProposerIndex: 7}
	return dasgossip.BuildDataColumnSidecarWithHeader(columnIndex, cells, commitments, proofs, header)
}

// TestEIP7594_CanonicalColumnSubnetTopics verifies the Fulu gossip naming:
// every column maps into [0, DATA_COLUMN_SIDECARS_SUBNET_COUNT), the
// canonical per-subnet topic strings round-trip through the parser, and
// the base topic name resolves to the DataColumnSidecar gossip topic.
func TestEIP7594_CanonicalColumnSubnetTopics(t *testing.T) {
	for col := dastypes.ColumnIndex(0); col < dastypes.NumberOfColumns; col++ {
		subnet := gossip.ComputeSubnetForDataColumnSidecar(uint64(col))
		if subnet >= gossip.DataColumnSidecarsSubnetCount {
			t.Fatalf("column %d mapped outside the subnet range: %d", col, subnet)
		}
		// dastypes and p2p/gossip must agree on the spec formula.
		if subnet != dastypes.ComputeSubnetForDataColumnSidecar(col) {
			t.Fatalf("column %d: gossip subnet %d != dastypes subnet %d", col, subnet,
				dastypes.ComputeSubnetForDataColumnSidecar(col))
		}
		topic := gossip.DataColumnSidecarTopicString("0xdeadbeef", subnet)
		got, err := gossip.ParseDataColumnSidecarTopic(topic)
		if err != nil || got != subnet {
			t.Fatalf("topic %q round trip: subnet %d, err %v", topic, got, err)
		}
		if col < 64 {
			// The first DATA_COLUMN_SIDECARS_SUBNET_COUNT columns cover each
			// subnet exactly once.
			if subnet != uint64(col) {
				t.Fatalf("column %d should own subnet %d, got %d", col, col, subnet)
			}
		}
	}
	topic, err := gossip.ParseGossipTopic("data_column_sidecar_42")
	if err != nil || topic != gossip.DataColumnSidecar {
		t.Fatalf("base topic parse: topic=%v err=%v", topic, err)
	}
}

// TestEIP7594_CustodyAssignmentUsesCanonicalSubnets ties the custody
// manager's column assignment to the canonical gossip subnets: every
// custodied column must publish on the subnet the topic naming computes.
func TestEIP7594_CustodyAssignmentUsesCanonicalSubnets(t *testing.T) {
	var nodeID [32]byte
	nodeID[0] = 0xA7
	mgr := custody.NewColumnCustodyManager(custody.CustodyManagerParams{
		NumberOfColumns:    dastypes.NumberOfColumns,
		CustodyRequirement: dastypes.CustodyRequirement,
		SamplesPerSlot:     dastypes.SamplesPerSlot,
	}, nodeID)

	assignment, err := mgr.ComputeAssignment(1)
	if err != nil {
		t.Fatalf("ComputeAssignment: %v", err)
	}
	if len(assignment.ColumnIndices) < int(dastypes.CustodyRequirement) {
		t.Fatalf("custody assignment too small: %d columns", len(assignment.ColumnIndices))
	}
	seenSubnets := make(map[uint64]bool)
	for _, col := range assignment.ColumnIndices {
		if col >= dastypes.NumberOfColumns {
			t.Fatalf("assigned column %d out of range", col)
		}
		subnet := gossip.ComputeSubnetForDataColumnSidecar(col)
		seenSubnets[subnet] = true
		// The column's own sidecar must validate against that subnet.
		sidecar := &dastypes.DataColumnSidecar{Index: dastypes.ColumnIndex(col)}
		if err := dastypes.ValidateSidecarSubnet(sidecar, subnet); err != nil {
			t.Fatalf("column %d failed its canonical subnet check: %v", col, err)
		}
	}
	// Custody requirement 4 means at least 4 distinct subnets are subscribed.
	if len(seenSubnets) < int(dastypes.CustodyRequirement) {
		t.Fatalf("assignment covers %d subnets, want >= %d", len(seenSubnets), dastypes.CustodyRequirement)
	}
}

// TestEIP7594_SidecarGossipRules drives the Fulu gossip validation on a
// sidecar built through the real builder path: subnet REJECT rule, the
// future-slot and finalized-slot IGNORE rules, and the signed-header
// requirement.
func TestEIP7594_SidecarGossipRules(t *testing.T) {
	cells := make([]dastypes.Cell, 4)
	for i := range cells {
		for j := range cells[i] {
			cells[i][j] = byte(i*j + 1)
		}
	}
	commitments := []dastypes.KZGCommitment{{0x01}, {0x02}, {0x03}, {0x04}}
	proofs := []dastypes.KZGProof{{0x05}, {0x06}, {0x07}, {0x08}}
	columnIndex := dastypes.ColumnIndex(65) // subnet 1

	sidecar, err := buildEIP7594Sidecar(columnIndex, cells, commitments, proofs)
	if err != nil {
		t.Fatalf("build sidecar: %v", err)
	}
	if sidecar.SignedBlockHeader == nil {
		t.Fatal("sidecar builder did not attach the signed block header")
	}
	if sidecar.SignedBlockHeader.Slot != 100 {
		t.Fatalf("sidecar header slot = %d, want 100", sidecar.SignedBlockHeader.Slot)
	}

	// Subnet REJECT rule.
	subnet := gossip.ComputeSubnetForDataColumnSidecar(uint64(columnIndex))
	if subnet != 1 {
		t.Fatalf("column 65 subnet = %d, want 1", subnet)
	}
	if err := dastypes.ValidateSidecarSubnet(sidecar, subnet); err != nil {
		t.Fatalf("correct subnet rejected: %v", err)
	}
	if err := dastypes.ValidateSidecarSubnet(sidecar, subnet+1); err == nil {
		t.Fatal("sidecar on the wrong subnet must be rejected")
	}

	// Slot IGNORE rules.
	if err := dastypes.ValidateSidecarSlot(sidecar, 100, 32); err != nil {
		t.Fatalf("current-slot sidecar rejected: %v", err)
	}
	if err := dastypes.ValidateSidecarSlot(sidecar, 99, 0); err == nil {
		t.Fatal("future-slot sidecar must be rejected")
	}
	if err := dastypes.ValidateSidecarSlot(sidecar, 200, 100); err == nil {
		t.Fatal("finalized sidecar must be rejected")
	}
}

// TestEIP7594_ReconstructionThresholdAcrossSubnets verifies that columns
// drawn one per canonical subnet reach the 50% reconstruction threshold
// and reconstruct the blob data (EIP-7594: a node can reconstruct the
// entire matrix from at least 50% of the columns).
func TestEIP7594_ReconstructionThresholdAcrossSubnets(t *testing.T) {
	if !reconstruction.CanReconstruct(dastypes.ReconstructionThreshold) {
		t.Fatal("threshold columns must satisfy CanReconstruct")
	}
	if reconstruction.CanReconstruct(dastypes.ReconstructionThreshold - 1) {
		t.Fatal("sub-threshold columns must not satisfy CanReconstruct")
	}

	// Columns 0..63 cover each canonical subnet exactly once; selecting one
	// column per subnet reaches the threshold with full subnet diversity.
	cells := make([]dastypes.Cell, dastypes.ReconstructionThreshold)
	indices := make([]uint64, dastypes.ReconstructionThreshold)
	subnetsSeen := make(map[uint64]bool)
	for i := 0; i < dastypes.ReconstructionThreshold; i++ {
		indices[i] = uint64(i)
		subnetsSeen[gossip.ComputeSubnetForDataColumnSidecar(uint64(i))] = true
	}
	if len(subnetsSeen) != dastypes.ReconstructionThreshold {
		t.Fatalf("first %d columns span %d subnets, want %d",
			dastypes.ReconstructionThreshold, len(subnetsSeen), dastypes.ReconstructionThreshold)
	}

	blob, err := reconstruction.ReconstructBlob(cells, indices)
	if err != nil {
		t.Fatalf("ReconstructBlob: %v", err)
	}
	expectedSize := dastypes.FieldElementsPerBlob * dastypes.BytesPerFieldElement
	if len(blob) != expectedSize {
		t.Fatalf("reconstructed blob size = %d, want %d", len(blob), expectedSize)
	}
}
