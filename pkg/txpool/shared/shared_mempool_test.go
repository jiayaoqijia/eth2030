package shared

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// testTxHash returns a deterministic hash for testing.
func testTxHash(n byte) types.Hash {
	var h types.Hash
	h[0] = n
	h[types.HashLength-1] = n
	return h
}

func testSender(n byte) types.Address {
	var a types.Address
	a[types.AddressLength-1] = n
	return a
}

func makeSharedMempoolTx(n byte, gasPrice uint64) SharedMempoolTx {
	return SharedMempoolTx{
		Hash:         testTxHash(n),
		Sender:       testSender(n),
		Nonce:        uint64(n),
		GasPrice:     gasPrice,
		Data:         []byte{n},
		ReceivedFrom: fmt.Sprintf("peer-%d", n),
	}
}

func defaultMempool() *SharedMempool {
	return NewSharedMempool(DefaultSharedMempoolConfig())
}

func TestNewSharedMempool(t *testing.T) {
	sm := defaultMempool()
	if sm == nil {
		t.Fatal("SharedMempool should not be nil")
	}
	if sm.TxCount() != 0 {
		t.Errorf("expected 0 txs, got %d", sm.TxCount())
	}
	if sm.PeerCount() != 0 {
		t.Errorf("expected 0 peers, got %d", sm.PeerCount())
	}
}

func TestAddTransaction_Basic(t *testing.T) {
	sm := defaultMempool()
	tx := makeSharedMempoolTx(1, 1000)

	if err := sm.AddTransaction(tx); err != nil {
		t.Fatalf("AddTransaction: %v", err)
	}
	if sm.TxCount() != 1 {
		t.Errorf("expected 1 tx, got %d", sm.TxCount())
	}
}

func TestAddTransaction_Duplicate(t *testing.T) {
	sm := defaultMempool()
	tx := makeSharedMempoolTx(1, 1000)

	if err := sm.AddTransaction(tx); err != nil {
		t.Fatal(err)
	}
	if err := sm.AddTransaction(tx); err != ErrSharedTxDuplicate {
		t.Errorf("expected ErrSharedTxDuplicate, got %v", err)
	}
}

func TestAddTransaction_Invalid(t *testing.T) {
	sm := defaultMempool()
	tx := SharedMempoolTx{Hash: types.Hash{}} // zero hash

	if err := sm.AddTransaction(tx); err != ErrSharedTxInvalid {
		t.Errorf("expected ErrSharedTxInvalid, got %v", err)
	}
}

func TestAddTransaction_Full(t *testing.T) {
	cfg := SharedMempoolConfig{
		MaxPeers:        10,
		MaxCacheSize:    4,
		RelayInterval:   time.Second,
		BloomFilterSize: 1024,
	}
	sm := NewSharedMempool(cfg)

	// Fill to capacity.
	for i := byte(1); i <= 4; i++ {
		if err := sm.AddTransaction(makeSharedMempoolTx(i, uint64(100*i))); err != nil {
			t.Fatalf("add tx %d: %v", i, err)
		}
	}
	// Adding a 5th tx triggers eviction of 25% (1 entry) and then succeeds.
	if err := sm.AddTransaction(makeSharedMempoolTx(5, 500)); err != nil {
		t.Errorf("expected eviction+success, got error: %v", err)
	}
	// Cache should be 4 (3 remaining after eviction + 1 new).
	if got := sm.TxCount(); got != 4 {
		t.Errorf("expected len 4 after eviction, got %d", got)
	}
}

func TestAddTransaction_SetsReceivedAt(t *testing.T) {
	sm := defaultMempool()
	tx := makeSharedMempoolTx(1, 100)

	before := time.Now()
	if err := sm.AddTransaction(tx); err != nil {
		t.Fatal(err)
	}
	after := time.Now()

	got := sm.GetTransaction(testTxHash(1))
	if got == nil {
		t.Fatal("transaction not found")
	}
	if got.ReceivedAt.Before(before) || got.ReceivedAt.After(after) {
		t.Errorf("ReceivedAt %v not between %v and %v", got.ReceivedAt, before, after)
	}
}

func TestGetPendingTxs_Priority(t *testing.T) {
	sm := defaultMempool()

	// Add with varying gas prices.
	for i := byte(1); i <= 5; i++ {
		if err := sm.AddTransaction(makeSharedMempoolTx(i, uint64(i)*100)); err != nil {
			t.Fatal(err)
		}
	}

	pending := sm.GetPendingTxs(0) // no limit
	if len(pending) != 5 {
		t.Fatalf("expected 5 txs, got %d", len(pending))
	}

	// Should be sorted by gas price descending.
	for i := 1; i < len(pending); i++ {
		if pending[i].GasPrice > pending[i-1].GasPrice {
			t.Errorf("not sorted: idx %d gas=%d > idx %d gas=%d",
				i, pending[i].GasPrice, i-1, pending[i-1].GasPrice)
		}
	}
}

func TestGetPendingTxs_WithLimit(t *testing.T) {
	sm := defaultMempool()

	for i := byte(1); i <= 10; i++ {
		if err := sm.AddTransaction(makeSharedMempoolTx(i, uint64(i)*10)); err != nil {
			t.Fatal(err)
		}
	}

	pending := sm.GetPendingTxs(3)
	if len(pending) != 3 {
		t.Fatalf("expected 3 txs, got %d", len(pending))
	}

	// Top 3 should have highest gas prices.
	if pending[0].GasPrice != 100 {
		t.Errorf("expected top gas price 100, got %d", pending[0].GasPrice)
	}
}

func TestGetPendingTxs_Empty(t *testing.T) {
	sm := defaultMempool()
	pending := sm.GetPendingTxs(10)
	if len(pending) != 0 {
		t.Errorf("expected 0, got %d", len(pending))
	}
}

func TestMarkRelayed_And_IsRelayedTo(t *testing.T) {
	sm := defaultMempool()
	txHash := testTxHash(1)

	if sm.IsRelayedTo(txHash, "peer-A") {
		t.Error("should not be relayed before marking")
	}

	sm.MarkRelayed(txHash, "peer-A")

	if !sm.IsRelayedTo(txHash, "peer-A") {
		t.Error("should be relayed to peer-A after marking")
	}
	if sm.IsRelayedTo(txHash, "peer-B") {
		t.Error("should not be relayed to peer-B")
	}

	// Mark another peer.
	sm.MarkRelayed(txHash, "peer-B")
	if !sm.IsRelayedTo(txHash, "peer-B") {
		t.Error("should be relayed to peer-B after marking")
	}
}

func TestIsKnown_BloomFilter(t *testing.T) {
	sm := defaultMempool()
	tx := makeSharedMempoolTx(42, 500)

	// Before adding, bloom should not contain it.
	// Note: false positives are possible but unlikely for a single item.
	if err := sm.AddTransaction(tx); err != nil {
		t.Fatal(err)
	}

	if !sm.IsKnown(tx.Hash) {
		t.Error("bloom filter should report known after add")
	}
}

func TestIsKnown_UnknownTx(t *testing.T) {
	sm := defaultMempool()
	// With an empty bloom, unknown hashes should return false.
	unknown := testTxHash(255)
	if sm.IsKnown(unknown) {
		t.Fatal("an empty Bloom filter reported the unknown transaction as present")
	}
}

func TestPeerManagement(t *testing.T) {
	cfg := SharedMempoolConfig{
		MaxPeers:        3,
		MaxCacheSize:    100,
		RelayInterval:   time.Second,
		BloomFilterSize: 1024,
	}
	sm := NewSharedMempool(cfg)

	if !sm.AddPeer("peer-1") {
		t.Error("should add peer-1")
	}
	if !sm.AddPeer("peer-2") {
		t.Error("should add peer-2")
	}
	if !sm.AddPeer("peer-3") {
		t.Error("should add peer-3")
	}
	if sm.AddPeer("peer-4") {
		t.Error("should reject peer-4 (max reached)")
	}

	if sm.PeerCount() != 3 {
		t.Errorf("expected 3 peers, got %d", sm.PeerCount())
	}

	// Duplicate peer.
	if sm.AddPeer("peer-1") {
		t.Error("should reject duplicate peer")
	}

	// Remove and re-add.
	sm.RemovePeer("peer-2")
	if sm.PeerCount() != 2 {
		t.Errorf("expected 2 peers after remove, got %d", sm.PeerCount())
	}

	if !sm.AddPeer("peer-4") {
		t.Error("should accept peer-4 after removal freed a slot")
	}
}

func TestEvictStale(t *testing.T) {
	sm := defaultMempool()

	// Add a tx with a manually set old time.
	oldTx := SharedMempoolTx{
		Hash:       testTxHash(1),
		Sender:     testSender(1),
		GasPrice:   100,
		ReceivedAt: time.Now().Add(-10 * time.Second),
	}
	sm.mu.Lock()
	sm.txCache[oldTx.Hash] = &oldTx
	sm.bloom.add(oldTx.Hash)
	sm.mu.Unlock()

	// Add a fresh tx.
	freshTx := makeSharedMempoolTx(2, 200)
	if err := sm.AddTransaction(freshTx); err != nil {
		t.Fatal(err)
	}

	// Also mark a relay for the old tx to verify cleanup.
	sm.MarkRelayed(oldTx.Hash, "peer-X")

	// Evict transactions older than 5 seconds.
	evicted := sm.EvictStale(5)
	if evicted != 1 {
		t.Errorf("expected 1 evicted, got %d", evicted)
	}
	if sm.TxCount() != 1 {
		t.Errorf("expected 1 tx remaining, got %d", sm.TxCount())
	}

	// Relay record should also be cleaned.
	if sm.IsRelayedTo(oldTx.Hash, "peer-X") {
		t.Error("relay record should be cleaned after eviction")
	}
}

func TestEvictStale_NoneEvicted(t *testing.T) {
	sm := defaultMempool()
	if err := sm.AddTransaction(makeSharedMempoolTx(1, 100)); err != nil {
		t.Fatal(err)
	}

	evicted := sm.EvictStale(3600) // 1 hour - nothing should be evicted
	if evicted != 0 {
		t.Errorf("expected 0 evicted, got %d", evicted)
	}
}

func TestGetTransaction(t *testing.T) {
	sm := defaultMempool()
	tx := makeSharedMempoolTx(1, 500)

	if err := sm.AddTransaction(tx); err != nil {
		t.Fatal(err)
	}

	got := sm.GetTransaction(testTxHash(1))
	if got == nil {
		t.Fatal("expected to find transaction")
	}
	if got.GasPrice != 500 {
		t.Errorf("expected gas price 500, got %d", got.GasPrice)
	}
	if got.Nonce != 1 {
		t.Errorf("expected nonce 1, got %d", got.Nonce)
	}
}

func TestGetTransaction_NotFound(t *testing.T) {
	sm := defaultMempool()
	got := sm.GetTransaction(testTxHash(99))
	if got != nil {
		t.Error("expected nil for unknown tx")
	}
}

func TestRemoveTransaction(t *testing.T) {
	sm := defaultMempool()
	tx := makeSharedMempoolTx(1, 100)
	if err := sm.AddTransaction(tx); err != nil {
		t.Fatal(err)
	}

	sm.MarkRelayed(tx.Hash, "peer-X")

	if !sm.RemoveTransaction(tx.Hash) {
		t.Error("expected true for successful removal")
	}
	if sm.TxCount() != 0 {
		t.Errorf("expected 0 txs after removal, got %d", sm.TxCount())
	}
	if sm.IsRelayedTo(tx.Hash, "peer-X") {
		t.Error("relay should be cleaned on removal")
	}
}

func TestRemoveTransaction_NotFound(t *testing.T) {
	sm := defaultMempool()
	if sm.RemoveTransaction(testTxHash(99)) {
		t.Error("expected false for removing unknown tx")
	}
}

func TestSharedMempool_ConcurrentAccess(t *testing.T) {
	sm := defaultMempool()
	var wg sync.WaitGroup
	errs := make(chan error, 500)

	// Concurrent writers.
	for i := byte(1); i <= 50; i++ {
		wg.Add(1)
		go func(n byte) {
			defer wg.Done()
			tx := makeSharedMempoolTx(n, uint64(n)*10)
			if err := sm.AddTransaction(tx); err != nil {
				errs <- err
			}
		}(i)
	}

	// Concurrent readers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sm.GetPendingTxs(10)
			sm.TxCount()
			sm.PeerCount()
			sm.IsKnown(testTxHash(1))
		}()
	}

	// Concurrent peer operations.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			peerID := fmt.Sprintf("concurrent-peer-%d", n)
			sm.AddPeer(peerID)
			sm.PeerCount()
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}

	if sm.TxCount() != 50 {
		t.Errorf("expected 50 txs, got %d", sm.TxCount())
	}
}

func TestSharedMempool_ConcurrentMarkRelayed(t *testing.T) {
	sm := defaultMempool()
	txHash := testTxHash(1)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			peerID := fmt.Sprintf("relay-peer-%d", n)
			sm.MarkRelayed(txHash, peerID)
		}(i)
	}

	wg.Wait()

	// All 20 peers should be recorded.
	for i := 0; i < 20; i++ {
		peerID := fmt.Sprintf("relay-peer-%d", i)
		if !sm.IsRelayedTo(txHash, peerID) {
			t.Errorf("expected tx to be relayed to %s", peerID)
		}
	}
}

func TestSharedMempoolConfig_Defaults(t *testing.T) {
	cfg := DefaultSharedMempoolConfig()
	if cfg.MaxPeers != 50 {
		t.Errorf("expected MaxPeers 50, got %d", cfg.MaxPeers)
	}
	if cfg.MaxCacheSize != 4096 {
		t.Errorf("expected MaxCacheSize 4096, got %d", cfg.MaxCacheSize)
	}
	if cfg.RelayInterval != 500*time.Millisecond {
		t.Errorf("expected RelayInterval 500ms, got %v", cfg.RelayInterval)
	}
	if cfg.BloomFilterSize != 65536 {
		t.Errorf("expected BloomFilterSize 65536, got %d", cfg.BloomFilterSize)
	}
}
