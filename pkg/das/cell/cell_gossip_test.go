package cell

import (
	"testing"
)

func TestDefaultSubnetConfig(t *testing.T) {
	cfg := DefaultSubnetConfig()
	if cfg.NumSubnets != DataColumnSidecarSubnetCount {
		t.Errorf("NumSubnets = %d, want %d", cfg.NumSubnets, DataColumnSidecarSubnetCount)
	}
	if cfg.SubnetsPerNode != CustodyRequirement {
		t.Errorf("SubnetsPerNode = %d, want %d", cfg.SubnetsPerNode, CustodyRequirement)
	}
}

func TestAssignSubnets(t *testing.T) {
	nodeID := [32]byte{0x01, 0x02, 0x03}
	config := DefaultSubnetConfig()

	subnets := AssignSubnets(nodeID, config)
	if uint64(len(subnets)) != config.SubnetsPerNode {
		t.Fatalf("got %d subnets, want %d", len(subnets), config.SubnetsPerNode)
	}

	// All subnets should be in range.
	for _, s := range subnets {
		if s >= config.NumSubnets {
			t.Errorf("subnet %d out of range [0, %d)", s, config.NumSubnets)
		}
	}

	// All subnets should be unique.
	seen := make(map[uint64]bool)
	for _, s := range subnets {
		if seen[s] {
			t.Errorf("duplicate subnet %d", s)
		}
		seen[s] = true
	}
}

func TestAssignSubnetsDeterministic(t *testing.T) {
	nodeID := [32]byte{0xAA, 0xBB}
	config := DefaultSubnetConfig()

	subnets1 := AssignSubnets(nodeID, config)
	subnets2 := AssignSubnets(nodeID, config)

	if len(subnets1) != len(subnets2) {
		t.Fatal("non-deterministic subnet count")
	}
	for i := range subnets1 {
		if subnets1[i] != subnets2[i] {
			t.Fatalf("non-deterministic: subnets1[%d]=%d != subnets2[%d]=%d",
				i, subnets1[i], i, subnets2[i])
		}
	}
}

func TestAssignSubnetsAllSubnets(t *testing.T) {
	nodeID := [32]byte{0xFF}
	config := SubnetConfig{NumSubnets: 8, SubnetsPerNode: 8}

	subnets := AssignSubnets(nodeID, config)
	if len(subnets) != 8 {
		t.Fatalf("got %d subnets, want 8", len(subnets))
	}

	// Should contain all subnets 0-7.
	for i := uint64(0); i < 8; i++ {
		if subnets[i] != i {
			t.Errorf("subnets[%d] = %d, want %d", i, subnets[i], i)
		}
	}
}

func TestAssignSubnetsDifferentNodes(t *testing.T) {
	config := DefaultSubnetConfig()

	subnets1 := AssignSubnets([32]byte{0x01}, config)
	subnets2 := AssignSubnets([32]byte{0x02}, config)

	// Different nodes should (almost certainly) get different assignments.
	allSame := true
	for i := range subnets1 {
		if subnets1[i] != subnets2[i] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Error("fixed nodes produced identical subnets")
	}
}

func TestCellSubnet(t *testing.T) {
	tests := []struct {
		cellIndex  uint64
		numSubnets uint64
		want       uint64
	}{
		{0, 64, 0},
		{1, 64, 1},
		{63, 64, 63},
		{64, 64, 0},
		{127, 64, 63},
		{0, 0, 0}, // edge case: zero subnets
	}
	for _, tt := range tests {
		got := CellSubnet(tt.cellIndex, tt.numSubnets)
		if got != tt.want {
			t.Errorf("CellSubnet(%d, %d) = %d, want %d",
				tt.cellIndex, tt.numSubnets, got, tt.want)
		}
	}
}

func TestGossipRouterRegisterNode(t *testing.T) {
	config := DefaultSubnetConfig()
	router := NewGossipRouter(config)

	nodeID := [32]byte{0x01}
	subnets := router.RegisterNode(nodeID)

	if uint64(len(subnets)) != config.SubnetsPerNode {
		t.Fatalf("got %d subnets, want %d", len(subnets), config.SubnetsPerNode)
	}

	// Registering again should return the same subnets.
	subnets2 := router.RegisterNode(nodeID)
	for i := range subnets {
		if subnets[i] != subnets2[i] {
			t.Error("re-registration changed subnet assignment")
		}
	}
}

func TestGossipRouterShouldAccept(t *testing.T) {
	config := SubnetConfig{NumSubnets: 4, SubnetsPerNode: 2}
	router := NewGossipRouter(config)

	nodeID := [32]byte{0x01}
	subnets := router.RegisterNode(nodeID)

	// A cell in one of the node's subnets should be accepted.
	msg := &CellMessage{
		BlobIndex: 0,
		CellIndex: subnets[0], // cell index == subnet for small configs
		Data:      make([]byte, BytesPerCell),
	}
	if !router.ShouldAccept(nodeID, msg) {
		t.Error("should accept cell in node's custody subnet")
	}

	// Find a subnet the node is NOT assigned to.
	assigned := make(map[uint64]bool)
	for _, s := range subnets {
		assigned[s] = true
	}
	var otherSubnet uint64
	for i := uint64(0); i < config.NumSubnets; i++ {
		if !assigned[i] {
			otherSubnet = i
			break
		}
	}

	msg2 := &CellMessage{
		BlobIndex: 0,
		CellIndex: otherSubnet,
		Data:      make([]byte, BytesPerCell),
	}
	if router.ShouldAccept(nodeID, msg2) {
		t.Error("should not accept cell outside node's custody subnets")
	}
}

func TestGossipRouterShouldAcceptNil(t *testing.T) {
	config := DefaultSubnetConfig()
	router := NewGossipRouter(config)

	if router.ShouldAccept([32]byte{}, nil) {
		t.Error("should not accept nil message")
	}
}

func TestGossipRouterShouldAcceptUnregistered(t *testing.T) {
	config := SubnetConfig{NumSubnets: 4, SubnetsPerNode: 2}
	router := NewGossipRouter(config)

	// Unregistered node should compute subnets on the fly.
	nodeID := [32]byte{0x01}
	subnets := AssignSubnets(nodeID, config)

	msg := &CellMessage{
		BlobIndex: 0,
		CellIndex: subnets[0],
		Data:      make([]byte, BytesPerCell),
	}
	if !router.ShouldAccept(nodeID, msg) {
		t.Error("unregistered node should still accept via on-the-fly computation")
	}
}

func TestGossipRouterBroadcastCell(t *testing.T) {
	config := SubnetConfig{NumSubnets: 4, SubnetsPerNode: 2}
	router := NewGossipRouter(config)

	// Register a few nodes.
	node1 := [32]byte{0x01}
	node2 := [32]byte{0x02}
	node3 := [32]byte{0x03}
	router.RegisterNode(node1)
	router.RegisterNode(node2)
	router.RegisterNode(node3)

	// Broadcast a cell; should get at least some subscribers.
	msg := &CellMessage{
		BlobIndex: 0,
		CellIndex: 0, // subnet 0
		Data:      make([]byte, BytesPerCell),
	}

	nodes, err := router.BroadcastCell(msg)
	if err != nil {
		t.Fatalf("BroadcastCell: %v", err)
	}

	if len(nodes) == 0 {
		t.Fatal("expected at least one broadcast target")
	}
}

func TestGossipRouterBroadcastCellNil(t *testing.T) {
	config := DefaultSubnetConfig()
	router := NewGossipRouter(config)

	_, err := router.BroadcastCell(nil)
	if err != ErrInvalidCellData {
		t.Fatalf("expected ErrInvalidCellData, got %v", err)
	}
}

func TestGossipRouterBroadcastCellOutOfRange(t *testing.T) {
	config := DefaultSubnetConfig()
	router := NewGossipRouter(config)

	msg := &CellMessage{CellIndex: NumberOfColumns, Data: make([]byte, 1)}
	_, err := router.BroadcastCell(msg)
	if err == nil {
		t.Fatal("expected error for cell index out of range")
	}
}

func TestGossipRouterBroadcastCellNoSubscribers(t *testing.T) {
	config := DefaultSubnetConfig()
	router := NewGossipRouter(config) // no nodes registered

	msg := &CellMessage{CellIndex: 0, Data: make([]byte, BytesPerCell)}
	_, err := router.BroadcastCell(msg)
	if err != ErrNoBroadcastTarget {
		t.Fatalf("expected ErrNoBroadcastTarget, got %v", err)
	}
}

func TestValidateCellMessage(t *testing.T) {
	// Valid message.
	msg := &CellMessage{
		BlobIndex: 0,
		CellIndex: 0,
		Data:      make([]byte, BytesPerCell),
		Proof:     make([]byte, 48),
	}
	if err := ValidateCellMessage(msg); err != nil {
		t.Fatalf("valid message rejected: %v", err)
	}

	// Nil message.
	if err := ValidateCellMessage(nil); err == nil {
		t.Error("expected error for nil message")
	}

	// Cell index out of range.
	bad := &CellMessage{CellIndex: NumberOfColumns, Data: make([]byte, 1)}
	if err := ValidateCellMessage(bad); err == nil {
		t.Error("expected error for cell index out of range")
	}

	// Blob index out of range.
	bad = &CellMessage{CellIndex: 0, BlobIndex: MaxBlobCommitmentsPerBlock, Data: make([]byte, 1)}
	if err := ValidateCellMessage(bad); err == nil {
		t.Error("expected error for blob index out of range")
	}

	// Empty data.
	bad = &CellMessage{CellIndex: 0, Data: nil}
	if err := ValidateCellMessage(bad); err == nil {
		t.Error("expected error for empty data")
	}

	// Data too large.
	bad = &CellMessage{CellIndex: 0, Data: make([]byte, BytesPerCell+1)}
	if err := ValidateCellMessage(bad); err == nil {
		t.Error("expected error for oversized data")
	}
}

func TestGossipTopicForSubnet(t *testing.T) {
	topic := GossipTopicForSubnet(42)
	expected := "/eth2030/das/cell/subnet/42"
	if topic != expected {
		t.Errorf("topic = %q, want %q", topic, expected)
	}

	topic0 := GossipTopicForSubnet(0)
	if topic0 != "/eth2030/das/cell/subnet/0" {
		t.Errorf("topic0 = %q, want /eth2030/das/cell/subnet/0", topic0)
	}
}

func TestRouteCellToGossipTopic(t *testing.T) {
	t.Run("valid message", func(t *testing.T) {
		config := SubnetConfig{NumSubnets: 4, SubnetsPerNode: 2}
		router := NewGossipRouter(config)

		msg := &CellMessage{
			BlobIndex: 0,
			CellIndex: 2,
			Data:      make([]byte, BytesPerCell),
			Proof:     make([]byte, 48),
		}

		topic, data, err := router.RouteCellToGossipTopic(msg)
		if err != nil {
			t.Fatalf("RouteCellToGossipTopic: %v", err)
		}

		expectedTopic := "/eth2030/das/cell/subnet/2"
		if topic != expectedTopic {
			t.Errorf("topic = %q, want %q", topic, expectedTopic)
		}

		// Data should contain 16 bytes header + cell data + proof.
		expectedLen := 16 + BytesPerCell + 48
		if len(data) != expectedLen {
			t.Errorf("data len = %d, want %d", len(data), expectedLen)
		}
	})

	t.Run("nil message", func(t *testing.T) {
		router := NewGossipRouter(DefaultSubnetConfig())
		_, _, err := router.RouteCellToGossipTopic(nil)
		if err == nil {
			t.Error("expected error for nil message")
		}
	})

	t.Run("invalid cell index", func(t *testing.T) {
		router := NewGossipRouter(DefaultSubnetConfig())
		msg := &CellMessage{CellIndex: NumberOfColumns, Data: make([]byte, 1)}
		_, _, err := router.RouteCellToGossipTopic(msg)
		if err == nil {
			t.Error("expected error for invalid cell index")
		}
	})
}

func TestSubnetCount(t *testing.T) {
	config := SubnetConfig{NumSubnets: 4, SubnetsPerNode: 1}
	router := NewGossipRouter(config)

	// No subscribers initially.
	if count := router.SubnetCount(0); count != 0 {
		t.Errorf("initial subnet count = %d, want 0", count)
	}

	// Register a node and check.
	nodeID := [32]byte{0x01}
	subnets := router.RegisterNode(nodeID)

	for _, s := range subnets {
		if count := router.SubnetCount(s); count != 1 {
			t.Errorf("subnet %d count = %d, want 1", s, count)
		}
	}
}
