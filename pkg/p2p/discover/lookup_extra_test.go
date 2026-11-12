package discover

import (
	"net"
	"sync/atomic"
	"testing"

	"github.com/eth2030/eth2030/p2p/enode"
)

// --- LookupConfig.defaults ---

func TestLookupConfig_Defaults_ZeroValues(t *testing.T) {
	cfg := LookupConfig{}
	cfg.defaults()

	if cfg.Alpha != Alpha {
		t.Fatalf("Alpha: want %d, got %d", Alpha, cfg.Alpha)
	}
	if cfg.ResultSize != BucketSize {
		t.Fatalf("ResultSize: want %d, got %d", BucketSize, cfg.ResultSize)
	}
}

func TestLookupConfig_Defaults_PreservesPositive(t *testing.T) {
	cfg := LookupConfig{Alpha: 7, ResultSize: 20}
	cfg.defaults()

	if cfg.Alpha != 7 {
		t.Fatalf("Alpha should be preserved: want 7, got %d", cfg.Alpha)
	}
	if cfg.ResultSize != 20 {
		t.Fatalf("ResultSize should be preserved: want 20, got %d", cfg.ResultSize)
	}
}

func TestLookupConfig_Defaults_NegativeValues(t *testing.T) {
	cfg := LookupConfig{Alpha: -1, ResultSize: -5}
	cfg.defaults()

	if cfg.Alpha != Alpha {
		t.Fatalf("negative Alpha should be set to default %d, got %d", Alpha, cfg.Alpha)
	}
	if cfg.ResultSize != BucketSize {
		t.Fatalf("negative ResultSize should be set to default %d, got %d", BucketSize, cfg.ResultSize)
	}
}

// --- XORDistance ---

func TestXORDistance_Identical(t *testing.T) {
	var id enode.NodeID
	id[0] = 0xAB
	dist := XORDistance(id, id)
	for i, b := range dist {
		if b != 0 {
			t.Fatalf("XORDistance(id, id)[%d] = 0x%02x, want 0", i, b)
		}
	}
}

func TestXORDistance_AllOnes(t *testing.T) {
	var a, b enode.NodeID
	for i := range b {
		b[i] = 0xFF
	}
	dist := XORDistance(a, b)
	for i, byt := range dist {
		if byt != 0xFF {
			t.Fatalf("XORDistance(0, max)[%d] = 0x%02x, want 0xFF", i, byt)
		}
	}
}

func TestXORDistance_Specific(t *testing.T) {
	var a, b enode.NodeID
	a[31] = 0x0F
	b[31] = 0xF0
	dist := XORDistance(a, b)
	if dist[31] != 0xFF {
		t.Fatalf("XORDistance last byte: want 0xFF, got 0x%02x", dist[31])
	}
	// All other bytes should be zero.
	for i := range 31 {
		if dist[i] != 0 {
			t.Fatalf("XORDistance[%d] = 0x%02x, want 0", i, dist[i])
		}
	}
}

func TestXORDistance_Symmetric(t *testing.T) {
	var a, b enode.NodeID
	a[0] = 0x12
	b[0] = 0x34
	a[15] = 0xAB
	b[15] = 0xCD

	d1 := XORDistance(a, b)
	d2 := XORDistance(b, a)
	if d1 != d2 {
		t.Fatal("XORDistance should be symmetric")
	}
}

// --- CompareXORDistance ---

func TestCompareXORDistance_ACloser(t *testing.T) {
	var target, a, b enode.NodeID
	target[31] = 0x10
	a[31] = 0x11 // XOR with target = 0x01
	b[31] = 0x20 // XOR with target = 0x30

	result := CompareXORDistance(target, a, b)
	if result >= 0 {
		t.Fatalf("CompareXORDistance: a should be closer, got %d", result)
	}
}

func TestCompareXORDistance_BCloser(t *testing.T) {
	var target, a, b enode.NodeID
	target[31] = 0x10
	a[31] = 0x20 // XOR with target = 0x30
	b[31] = 0x11 // XOR with target = 0x01

	result := CompareXORDistance(target, a, b)
	if result <= 0 {
		t.Fatalf("CompareXORDistance: b should be closer, got %d", result)
	}
}

func TestCompareXORDistance_Equal(t *testing.T) {
	var target, a enode.NodeID
	target[0] = 0xFF
	a[0] = 0x01

	result := CompareXORDistance(target, a, a)
	if result != 0 {
		t.Fatalf("CompareXORDistance(target, a, a): want 0, got %d", result)
	}
}

// --- LogDistance ---

func TestLogDistance_Identical(t *testing.T) {
	var id enode.NodeID
	id[0] = 0xAB
	if d := LogDistance(id, id); d != 0 {
		t.Fatalf("LogDistance(id, id) = %d, want 0", d)
	}
}

func TestLogDistance_OneStep(t *testing.T) {
	var a, b enode.NodeID
	b[31] = 0x01
	d := LogDistance(a, b)
	if d != 1 {
		t.Fatalf("LogDistance(0, 1): want 1, got %d", d)
	}
}

func TestLogDistance_MaxDistance(t *testing.T) {
	var a, b enode.NodeID
	b[0] = 0x80
	d := LogDistance(a, b)
	if d != 256 {
		t.Fatalf("LogDistance max: want 256, got %d", d)
	}
}

func TestLogDistance_Symmetric(t *testing.T) {
	var a, b enode.NodeID
	a[5] = 0x42
	b[10] = 0xFF

	if LogDistance(a, b) != LogDistance(b, a) {
		t.Fatal("LogDistance should be symmetric")
	}
}

// --- closestSet ---

func TestClosestSet_Push_Basic(t *testing.T) {
	var target enode.NodeID
	target[31] = 5

	cs := newClosestSet(target, 3)

	n1 := makeNode(5) // exact match
	n2 := makeNode(6)
	n3 := makeNode(7)

	if !cs.push(n1) {
		t.Fatal("push n1: expected true")
	}
	if !cs.push(n2) {
		t.Fatal("push n2: expected true")
	}
	if !cs.push(n3) {
		t.Fatal("push n3: expected true")
	}

	result := cs.result()
	if len(result) != 3 {
		t.Fatalf("result len: want 3, got %d", len(result))
	}
	// First should be n1 (exact match, distance 0).
	if result[0].ID != n1.ID {
		t.Fatalf("result[0] should be n1")
	}
}

func TestClosestSet_Push_Duplicate(t *testing.T) {
	var target enode.NodeID
	cs := newClosestSet(target, 5)

	n := makeNode(1)
	cs.push(n)
	if cs.push(n) {
		t.Fatal("pushing duplicate should return false")
	}
	if len(cs.nodes) != 1 {
		t.Fatalf("duplicate should not increase size, got %d", len(cs.nodes))
	}
}

func TestClosestSet_Push_Evicts_Farthest(t *testing.T) {
	var target enode.NodeID
	target[31] = 0x10

	cs := newClosestSet(target, 2)

	near := makeNode(0x11) // XOR = 0x01
	mid := makeNode(0x20)  // XOR = 0x30
	far := makeNode(0x50)  // XOR = 0x60

	cs.push(near)
	cs.push(far)
	// now full; pushing mid should evict far (which is farther than mid)
	improved := cs.push(mid)
	if !improved {
		t.Fatal("mid should improve the set by replacing far")
	}
	if len(cs.nodes) != 2 {
		t.Fatalf("size should stay at limit 2, got %d", len(cs.nodes))
	}
	// far should be gone.
	for _, n := range cs.nodes {
		if n.ID == far.ID {
			t.Fatal("far should have been evicted")
		}
	}
}

func TestClosestSet_Push_NotCloserThanFarthest(t *testing.T) {
	var target enode.NodeID
	// Fill with two close nodes.
	cs := newClosestSet(target, 2)
	cs.push(makeNode(1))
	cs.push(makeNode(2))

	// A very far node should not improve the set.
	var farID enode.NodeID
	farID[0] = 0xFF
	farNode := enode.NewNode(farID, net.ParseIP("10.0.0.1"), 30303, 30303)
	if cs.push(farNode) {
		t.Fatal("very far node should not improve the full closest set")
	}
}

func TestClosestSet_Result_IsCopy(t *testing.T) {
	var target enode.NodeID
	cs := newClosestSet(target, 5)
	cs.push(makeNode(1))
	cs.push(makeNode(2))

	r1 := cs.result()
	r2 := cs.result()
	if len(r1) != len(r2) {
		t.Fatalf("result lengths differ: %d vs %d", len(r1), len(r2))
	}
	// Mutate r1 and verify r2 is unaffected (shallow copy is sufficient here).
	r1[0] = nil
	if r2[0] == nil {
		t.Fatal("result() should return independent copies")
	}
}

// --- IterativeLookup ---

func TestIterativeLookup_EmptyTable(t *testing.T) {
	var selfID enode.NodeID
	tab := NewTable(selfID)

	var target enode.NodeID
	target[31] = 42

	result := tab.IterativeLookup(target, func(_ *enode.Node) []*enode.Node {
		return nil
	}, LookupConfig{})

	if result == nil {
		t.Fatal("IterativeLookup should return non-nil result")
	}
	if result.Target != target {
		t.Fatal("result.Target mismatch")
	}
	if len(result.Closest) != 0 {
		t.Fatalf("empty table: want 0 closest, got %d", len(result.Closest))
	}
	if result.QueriedCount != 0 {
		t.Fatalf("empty table: want 0 queried, got %d", result.QueriedCount)
	}
}

func TestIterativeLookup_LocalNodesOnly(t *testing.T) {
	var selfID enode.NodeID
	tab := NewTable(selfID)

	// Seed the table with some nodes.
	for i := byte(1); i <= 5; i++ {
		tab.AddNode(makeNode(i))
	}

	var target enode.NodeID
	target[31] = 3

	// queryFn returns nothing (simulates no further discovery).
	result := tab.IterativeLookup(target, func(_ *enode.Node) []*enode.Node {
		return nil
	}, LookupConfig{})

	if len(result.Closest) == 0 {
		t.Fatal("expected some closest nodes")
	}
	if result.QueriedCount == 0 {
		t.Fatal("expected some queries to be made")
	}
}

func TestIterativeLookup_DiscoversNewNodes(t *testing.T) {
	var selfID enode.NodeID
	tab := NewTable(selfID)

	// Seed with a few nodes.
	for i := byte(1); i <= 3; i++ {
		tab.AddNode(makeNode(i))
	}

	var target enode.NodeID
	target[31] = 10

	// queryFn returns nodes closer to target.
	result := tab.IterativeLookup(target, func(_ *enode.Node) []*enode.Node {
		var resp []*enode.Node
		for i := byte(8); i <= 12; i++ {
			resp = append(resp, makeNode(i))
		}
		return resp
	}, LookupConfig{Alpha: 2, ResultSize: 5})

	if result.QueriedCount == 0 {
		t.Fatal("expected queries to be made")
	}
	if len(result.Closest) == 0 {
		t.Fatal("expected closest nodes in result")
	}
	if len(result.Path) == 0 {
		t.Fatal("expected path entries")
	}
	if result.Rounds == 0 {
		t.Fatal("expected at least one round")
	}
}

func TestIterativeLookup_MaxRounds(t *testing.T) {
	var selfID enode.NodeID
	tab := NewTable(selfID)

	for i := byte(1); i <= 5; i++ {
		tab.AddNode(makeNode(i))
	}

	var target enode.NodeID
	target[31] = 20

	var roundCount atomic.Uint32
	result := tab.IterativeLookup(target, func(_ *enode.Node) []*enode.Node {
		count := roundCount.Add(1)
		// Always return new nodes to keep the lookup going.
		return []*enode.Node{makeNode(byte(count + 30))}
	}, LookupConfig{MaxRounds: 2})

	// The implementation increments round before the MaxRounds check and breaks
	// when round > MaxRounds, so result.Rounds == MaxRounds+1 at the break point.
	if result.Rounds > 3 {
		t.Fatalf("Rounds should be at most MaxRounds+1=3, got %d", result.Rounds)
	}
}

func TestIterativeLookup_LookupHops(t *testing.T) {
	var selfID enode.NodeID
	tab := NewTable(selfID)

	tab.AddNode(makeNode(1))

	var target enode.NodeID
	target[31] = 5

	result := tab.IterativeLookup(target, func(n *enode.Node) []*enode.Node {
		// Return one closer node.
		return []*enode.Node{makeNode(5)}
	}, LookupConfig{Alpha: 1, ResultSize: 4})

	// Path should have at least one hop.
	if len(result.Path) == 0 {
		t.Fatal("expected at least one lookup hop in Path")
	}
	hop := result.Path[0]
	if hop.Round == 0 {
		t.Fatal("hop Round should be >= 1")
	}
}

// --- Refresh (discover.go) ---

func TestTable_Refresh(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	for i := byte(1); i <= 5; i++ {
		tab.AddNode(makeNode(i))
	}

	called := false
	tab.Refresh(func(n *enode.Node) []*enode.Node {
		called = true
		return nil
	})

	if !called {
		t.Fatal("Refresh should invoke the queryFn")
	}
}

func TestTable_Refresh_EmptyTable(t *testing.T) {
	self := makeNodeID(0)
	tab := NewTable(self)

	// With an empty table, Lookup returns early without querying.
	// Refresh should not panic.
	tab.Refresh(func(_ *enode.Node) []*enode.Node {
		return nil
	})
}
