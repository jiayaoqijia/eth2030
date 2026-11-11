package stark

import (
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// mockBroadcaster records calls to GossipMempoolStarkTick.
type mockBroadcaster struct {
	mu    sync.Mutex
	calls [][]byte
}

func (m *mockBroadcaster) GossipMempoolStarkTick(data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]byte, len(data))
	copy(cp, data)
	m.calls = append(m.calls, cp)
	return nil
}

func (m *mockBroadcaster) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *mockBroadcaster) lastCall() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return nil
	}
	return m.calls[len(m.calls)-1]
}

func TestP2PBroadcasterSet(t *testing.T) {
	agg := NewSTARKAggregator("node-1")
	b := &mockBroadcaster{}
	agg.SetBroadcaster(b)

	if agg.broadcaster != b {
		t.Error("expected broadcaster to be set")
	}
}

func TestStarkTickBroadcast(t *testing.T) {
	agg := NewSTARKAggregator("node-1")
	b := &mockBroadcaster{}
	agg.SetBroadcaster(b)

	var h types.Hash
	h[0] = 0xAB
	agg.AddValidatedTx(h, []byte("proof"), 21000)

	tick, err := agg.GenerateTick()
	if err != nil {
		t.Fatalf("GenerateTick: %v", err)
	}

	// BroadcastTick should call the broadcaster.
	agg.BroadcastTick(tick)

	if b.callCount() != 1 {
		t.Fatalf("expected 1 broadcast call, got %d", b.callCount())
	}

	// Verify the data is valid by unmarshaling.
	var decoded MempoolAggregationTick
	if err := decoded.UnmarshalBinary(b.lastCall()); err != nil {
		t.Fatalf("UnmarshalBinary: %v", err)
	}
	if decoded.TickNumber != tick.TickNumber {
		t.Errorf("tick number mismatch: got %d, want %d", decoded.TickNumber, tick.TickNumber)
	}
}

func TestStarkTickBroadcastSizeLimit(t *testing.T) {
	agg := NewSTARKAggregator("node-1")
	b := &mockBroadcaster{}
	agg.SetBroadcaster(b)

	// Create a tick with enough txs to exceed MaxTickSize (128KB).
	// Each hash is 32 bytes, so ~4000 hashes = ~128KB of hash data alone.
	for i := 0; i < 4100; i++ {
		var h types.Hash
		h[0] = byte(i >> 8)
		h[1] = byte(i)
		h[2] = 0xFF
		agg.AddValidatedTx(h, []byte("proof"), 21000)
	}

	tick, err := agg.GenerateTick()
	if err != nil {
		t.Fatalf("GenerateTick: %v", err)
	}

	if _, err := tick.MarshalBinary(); err != ErrAggTickTooLarge {
		t.Fatalf("oversized encoding: %v", err)
	}

	agg.BroadcastTick(tick)

	if b.callCount() != 0 {
		t.Errorf("expected 0 broadcast calls for oversized tick, got %d", b.callCount())
	}
}

func TestP2PBroadcasterNil(t *testing.T) {
	agg := NewSTARKAggregator("node-1")
	// No broadcaster set.

	var h types.Hash
	h[0] = 0x01
	agg.AddValidatedTx(h, []byte("proof"), 21000)

	tick, err := agg.GenerateTick()
	if err != nil {
		t.Fatalf("GenerateTick: %v", err)
	}

	// Should not panic.
	agg.BroadcastTick(tick)
}
