package network

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/eth2030/eth2030/das/dastypes"
)

// mockCustodyManager is a test double for CustodyManager.
// It stores peers for specific columns and returns them via FindPeersForColumn.
type mockCustodyManager struct {
	mu            sync.RWMutex
	columnToPeers map[uint64][][32]byte
}

func newMockCustodyManager() *mockCustodyManager {
	return &mockCustodyManager{
		columnToPeers: make(map[uint64][][32]byte),
	}
}

// RegisterPeer registers a peer as custodying all columns (supernode simulation).
func (m *mockCustodyManager) RegisterPeer(peerID [32]byte, custodyGroupCount uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	// Register the peer for all columns up to NumberOfColumns.
	for i := uint64(0); i < dastypes.NumberOfColumns; i++ {
		m.columnToPeers[i] = append(m.columnToPeers[i], peerID)
	}
}

// FindPeersForColumn returns all peers that custody the given column.
func (m *mockCustodyManager) FindPeersForColumn(columnIndex uint64) ([][32]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	peers, ok := m.columnToPeers[columnIndex]
	if !ok || len(peers) == 0 {
		return nil, errors.New("no peers for column")
	}
	result := make([][32]byte, len(peers))
	copy(result, peers)
	return result, nil
}

// --- DASNetworkManager creation and lifecycle ---

func TestNewDASNetworkManager(t *testing.T) {
	cfg := DefaultNetworkConfig()
	custody := newMockCustodyManager()
	nm := NewDASNetworkManager(cfg, custody)
	if nm == nil {
		t.Fatal("expected non-nil manager")
	}
	if nm.IsStarted() {
		t.Fatal("should not be started initially")
	}
}

func TestDASNetworkManagerStartStop(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()
	if !nm.IsStarted() {
		t.Fatal("expected started")
	}
	nm.Stop()
	if nm.IsStarted() {
		t.Fatal("expected stopped")
	}
}

func TestDASNetworkManagerNotStartedErrors(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)

	err := nm.SubscribeToSubnets([]uint64{0})
	if err != ErrNetworkNotStarted {
		t.Fatalf("SubscribeToSubnets: got %v, want ErrNetworkNotStarted", err)
	}

	err = nm.PublishColumn(dastypes.DataColumn{}, 0)
	if err != ErrNetworkNotStarted {
		t.Fatalf("PublishColumn: got %v, want ErrNetworkNotStarted", err)
	}

	_, err = nm.PerformSampling(0, 4)
	if err != ErrNetworkNotStarted {
		t.Fatalf("PerformSampling: got %v, want ErrNetworkNotStarted", err)
	}
}

func TestDefaultNetworkConfig(t *testing.T) {
	cfg := DefaultNetworkConfig()
	if cfg.SamplesPerSlot != dastypes.SamplesPerSlot {
		t.Fatalf("SamplesPerSlot = %d, want %d", cfg.SamplesPerSlot, dastypes.SamplesPerSlot)
	}
	if cfg.NumberOfColumns != dastypes.NumberOfColumns {
		t.Fatalf("NumberOfColumns = %d, want %d", cfg.NumberOfColumns, dastypes.NumberOfColumns)
	}
	if cfg.SubnetCount != dastypes.DataColumnSidecarSubnetCount {
		t.Fatalf("SubnetCount = %d, want %d", cfg.SubnetCount, dastypes.DataColumnSidecarSubnetCount)
	}
	if cfg.AvailabilityThreshold != 0.5 {
		t.Fatalf("AvailabilityThreshold = %f, want 0.5", cfg.AvailabilityThreshold)
	}
}

// --- SubscribeToSubnets ---

func TestSubscribeToSubnets(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	err := nm.SubscribeToSubnets([]uint64{0, 5, 10})
	if err != nil {
		t.Fatalf("SubscribeToSubnets: %v", err)
	}

	subs := nm.Subscriptions()
	if len(subs) != 3 {
		t.Fatalf("expected 3 subscriptions, got %d", len(subs))
	}

	// Verify sorted.
	for i := 1; i < len(subs); i++ {
		if subs[i] < subs[i-1] {
			t.Fatalf("subscriptions not sorted: %v", subs)
		}
	}

	if !nm.IsSubscribed(5) {
		t.Fatal("should be subscribed to subnet 5")
	}
	if nm.IsSubscribed(99) {
		t.Fatal("should not be subscribed to subnet 99")
	}
}

func TestSubscribeToSubnetsInvalid(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	err := nm.SubscribeToSubnets([]uint64{dastypes.DataColumnSidecarSubnetCount})
	if err == nil {
		t.Fatal("expected error for out-of-range subnet")
	}
}

func TestSubscribeToSubnetsIdempotent(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	nm.SubscribeToSubnets([]uint64{5})
	nm.SubscribeToSubnets([]uint64{5})

	subs := nm.Subscriptions()
	if len(subs) != 1 {
		t.Fatalf("expected 1 subscription (idempotent), got %d", len(subs))
	}
}

// --- PublishColumn ---

func TestPublishColumn(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	col := dastypes.DataColumn{
		Index: 5,
		Cells: []dastypes.Cell{{}},
	}

	err := nm.PublishColumn(col, 5)
	if err != nil {
		t.Fatalf("PublishColumn: %v", err)
	}

	published := nm.PublishedColumns()
	if len(published) != 1 {
		t.Fatalf("expected 1 published column, got %d", len(published))
	}
	if uint64(published[0].Column.Index) != 5 {
		t.Fatalf("published column index = %d, want 5", published[0].Column.Index)
	}
	if published[0].Subnet != 5 {
		t.Fatalf("published subnet = %d, want 5", published[0].Subnet)
	}

	// Should be stored locally.
	stored, ok := nm.GetStoredColumn(5)
	if !ok {
		t.Fatal("column should be stored locally after publish")
	}
	if uint64(stored.Index) != 5 {
		t.Fatalf("stored column index = %d, want 5", stored.Index)
	}
}

func TestPublishColumnInvalidSubnet(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	err := nm.PublishColumn(dastypes.DataColumn{Index: 0}, dastypes.DataColumnSidecarSubnetCount)
	if err == nil {
		t.Fatal("expected error for out-of-range subnet")
	}
}

func TestPublishColumnInvalidIndex(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	err := nm.PublishColumn(dastypes.DataColumn{Index: dastypes.ColumnIndex(dastypes.NumberOfColumns)}, 0)
	if err == nil {
		t.Fatal("expected error for out-of-range column index")
	}
}

// --- PerformSampling ---

func TestPerformSamplingLocalStore(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	// Store all 128 columns locally so sampling always succeeds.
	for i := uint64(0); i < dastypes.NumberOfColumns; i++ {
		nm.StoreColumn(&dastypes.DataColumn{
			Index: dastypes.ColumnIndex(i),
			Cells: []dastypes.Cell{{}},
		})
	}

	result, err := nm.PerformSampling(100, 8)
	if err != nil {
		t.Fatalf("PerformSampling: %v", err)
	}
	if result.TotalSamples != 8 {
		t.Fatalf("TotalSamples = %d, want 8", result.TotalSamples)
	}
	if result.Successful != 8 {
		t.Fatalf("Successful = %d, want 8", result.Successful)
	}
	if result.Failed != 0 {
		t.Fatalf("Failed = %d, want 0", result.Failed)
	}
	if !result.AvailabilityConfirmed {
		t.Fatal("expected availability confirmed")
	}
	if result.Slot != 100 {
		t.Fatalf("Slot = %d, want 100", result.Slot)
	}
}

func TestPerformSamplingAllFail(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	// No columns stored, no provider; all samples fail.
	result, err := nm.PerformSampling(50, 4)
	if err != nil {
		t.Fatalf("PerformSampling: %v", err)
	}
	if result.Successful != 0 {
		t.Fatalf("Successful = %d, want 0", result.Successful)
	}
	if result.Failed != 4 {
		t.Fatalf("Failed = %d, want 4", result.Failed)
	}
	if result.AvailabilityConfirmed {
		t.Fatal("expected availability NOT confirmed")
	}
}

func TestPerformSamplingWithProvider(t *testing.T) {
	custody := newMockCustodyManager()
	nm := NewDASNetworkManager(DefaultNetworkConfig(), custody)
	nm.Start()

	// Register a peer that custodies all columns (supernode).
	peer := [32]byte{0x42}
	custody.RegisterPeer(peer, dastypes.NumberOfCustodyGroups)

	// Provider that always succeeds.
	nm.SetSampleProvider(func(peerID [32]byte, columnIndex uint64) (*dastypes.DataColumn, time.Duration, error) {
		return &dastypes.DataColumn{
			Index: dastypes.ColumnIndex(columnIndex),
			Cells: []dastypes.Cell{{}},
		}, 10 * time.Millisecond, nil
	})

	result, err := nm.PerformSampling(1, 8)
	if err != nil {
		t.Fatalf("PerformSampling: %v", err)
	}
	if result.Successful != 8 {
		t.Fatalf("Successful = %d, want 8", result.Successful)
	}
	if !result.AvailabilityConfirmed {
		t.Fatal("expected availability confirmed")
	}

	// Peer should have a good score now.
	score, ok := nm.PeerScore(peer)
	if !ok {
		t.Fatal("expected peer score to exist")
	}
	if score <= DefaultNetworkConfig().InitialPeerScore {
		t.Fatalf("expected score > initial, got %f", score)
	}
}

func TestPerformSamplingWithFailingProvider(t *testing.T) {
	custody := newMockCustodyManager()
	nm := NewDASNetworkManager(DefaultNetworkConfig(), custody)
	nm.Start()

	peer := [32]byte{0x42}
	custody.RegisterPeer(peer, dastypes.NumberOfCustodyGroups)

	// Provider that always fails.
	nm.SetSampleProvider(func(peerID [32]byte, columnIndex uint64) (*dastypes.DataColumn, time.Duration, error) {
		return nil, 0, errors.New("peer offline")
	})

	result, err := nm.PerformSampling(1, 4)
	if err != nil {
		t.Fatalf("PerformSampling: %v", err)
	}
	if result.Successful != 0 {
		t.Fatalf("Successful = %d, want 0", result.Successful)
	}
	if result.AvailabilityConfirmed {
		t.Fatal("expected availability NOT confirmed")
	}

	// Peer should have a degraded score.
	score, _ := nm.PeerScore(peer)
	if score >= DefaultNetworkConfig().InitialPeerScore {
		t.Fatalf("expected score < initial after failures, got %f", score)
	}
}

func TestPerformSamplingInvalidCount(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	_, err := nm.PerformSampling(0, 0)
	if err == nil {
		t.Fatal("expected error for sampleCount 0")
	}

	_, err = nm.PerformSampling(0, -1)
	if err == nil {
		t.Fatal("expected error for negative sampleCount")
	}
}

func TestPerformSamplingDeterministic(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	// Store all columns.
	for i := uint64(0); i < dastypes.NumberOfColumns; i++ {
		nm.StoreColumn(&dastypes.DataColumn{Index: dastypes.ColumnIndex(i), Cells: []dastypes.Cell{{}}})
	}

	r1, _ := nm.PerformSampling(42, 8)
	r2, _ := nm.PerformSampling(42, 8)

	// Same slot should produce same column selection.
	if len(r1.Results) != len(r2.Results) {
		t.Fatal("non-deterministic result count")
	}
	for i := range r1.Results {
		if r1.Results[i].ColumnIndex != r2.Results[i].ColumnIndex {
			t.Fatalf("non-deterministic column at [%d]: %d vs %d",
				i, r1.Results[i].ColumnIndex, r2.Results[i].ColumnIndex)
		}
	}
}

// --- Peer scoring ---

func TestPeerScoring(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)

	peer := [32]byte{0x01}

	// No score initially.
	_, ok := nm.PeerScore(peer)
	if ok {
		t.Fatal("expected no score for unknown peer")
	}

	// Record success.
	nm.recordPeerSuccess(peer, 5*time.Millisecond)

	score, ok := nm.PeerScore(peer)
	if !ok {
		t.Fatal("expected score after success")
	}
	if score <= 0 {
		t.Fatalf("expected positive score, got %f", score)
	}

	// Record failure.
	nm.recordPeerFailure(peer)
	scoreAfterFail, _ := nm.PeerScore(peer)
	if scoreAfterFail >= score {
		t.Fatalf("score should decrease after failure: %f >= %f", scoreAfterFail, score)
	}
}

func TestPeerSuccessRate(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	peer := [32]byte{0x02}

	// No data initially.
	_, ok := nm.PeerSuccessRate(peer)
	if ok {
		t.Fatal("expected no rate for unknown peer")
	}

	nm.recordPeerSuccess(peer, time.Millisecond)
	nm.recordPeerSuccess(peer, time.Millisecond)
	nm.recordPeerFailure(peer)

	rate, ok := nm.PeerSuccessRate(peer)
	if !ok {
		t.Fatal("expected rate after queries")
	}
	// 2 successes out of 3 total = 0.666...
	expectedRate := 2.0 / 3.0
	if rate < expectedRate-0.01 || rate > expectedRate+0.01 {
		t.Fatalf("expected rate ~%.3f, got %.3f", expectedRate, rate)
	}
}

func TestPeerAverageLatency(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	peer := [32]byte{0x03}

	_, ok := nm.PeerAverageLatency(peer)
	if ok {
		t.Fatal("expected no latency for unknown peer")
	}

	nm.recordPeerSuccess(peer, 10*time.Millisecond)
	nm.recordPeerSuccess(peer, 20*time.Millisecond)

	avg, ok := nm.PeerAverageLatency(peer)
	if !ok {
		t.Fatal("expected latency after queries")
	}
	// Average of 10ms and 20ms = 15ms.
	if avg != 15*time.Millisecond {
		t.Fatalf("expected avg latency 15ms, got %v", avg)
	}
}

func TestPeerScoreCapping(t *testing.T) {
	cfg := DefaultNetworkConfig()
	nm := NewDASNetworkManager(cfg, nil)
	peer := [32]byte{0x04}

	// Record many successes to try to exceed MaxPeerScore.
	for i := 0; i < 200; i++ {
		nm.recordPeerSuccess(peer, time.Millisecond)
	}
	score, _ := nm.PeerScore(peer)
	if score > cfg.MaxPeerScore {
		t.Fatalf("score %f exceeds max %f", score, cfg.MaxPeerScore)
	}

	// Record many failures to try to go below MinPeerScore.
	for i := 0; i < 200; i++ {
		nm.recordPeerFailure(peer)
	}
	score, _ = nm.PeerScore(peer)
	if score < cfg.MinPeerScore {
		t.Fatalf("score %f below min %f", score, cfg.MinPeerScore)
	}
}

// --- Metrics ---

func TestMetrics(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	// Store all columns.
	for i := uint64(0); i < dastypes.NumberOfColumns; i++ {
		nm.StoreColumn(&dastypes.DataColumn{Index: dastypes.ColumnIndex(i), Cells: []dastypes.Cell{{}}})
	}

	nm.PerformSampling(1, 4)
	nm.PerformSampling(2, 4)

	metrics := nm.Metrics()
	if metrics.TotalSamplingRounds != 2 {
		t.Fatalf("TotalSamplingRounds = %d, want 2", metrics.TotalSamplingRounds)
	}
	if metrics.TotalSamplesRequested != 8 {
		t.Fatalf("TotalSamplesRequested = %d, want 8", metrics.TotalSamplesRequested)
	}
	if metrics.TotalSamplesSucceeded != 8 {
		t.Fatalf("TotalSamplesSucceeded = %d, want 8", metrics.TotalSamplesSucceeded)
	}
	if metrics.AvailabilityConfirmedCount != 2 {
		t.Fatalf("AvailabilityConfirmedCount = %d, want 2", metrics.AvailabilityConfirmedCount)
	}
}

func TestSamplingSuccessRate(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	nm.Start()

	// Initially zero.
	if nm.SamplingSuccessRate() != 0 {
		t.Fatalf("expected 0 initially, got %f", nm.SamplingSuccessRate())
	}

	// Store half the columns, so half the samples succeed.
	for i := uint64(0); i < dastypes.NumberOfColumns/2; i++ {
		nm.StoreColumn(&dastypes.DataColumn{Index: dastypes.ColumnIndex(i), Cells: []dastypes.Cell{{}}})
	}

	// Perform multiple rounds to get a meaningful rate.
	for slot := uint64(0); slot < 10; slot++ {
		nm.PerformSampling(slot, 8)
	}

	rate := nm.SamplingSuccessRate()
	// Should be between 0 and 1. The exact value depends on which columns
	// are selected, but statistically ~50%.
	if rate < 0 || rate > 1 {
		t.Fatalf("invalid success rate: %f", rate)
	}
}

// --- StoreColumn / GetStoredColumn ---

func TestStoreAndGetColumn(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)

	col := &dastypes.DataColumn{
		Index: 42,
		Cells: []dastypes.Cell{{}},
	}
	nm.StoreColumn(col)

	stored, ok := nm.GetStoredColumn(42)
	if !ok {
		t.Fatal("column should be stored")
	}
	if uint64(stored.Index) != 42 {
		t.Fatalf("stored index = %d, want 42", stored.Index)
	}

	// Non-existent column.
	_, ok = nm.GetStoredColumn(999)
	if ok {
		t.Fatal("should not find non-existent column")
	}

	// Nil column should be silently ignored.
	nm.StoreColumn(nil)
}

// --- selectBestPeer ---

func TestSelectBestPeer(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)

	p1 := [32]byte{0x01}
	p2 := [32]byte{0x02}
	p3 := [32]byte{0x03}

	// Give p2 a better score.
	nm.recordPeerSuccess(p2, time.Millisecond)
	nm.recordPeerSuccess(p2, time.Millisecond)
	nm.recordPeerSuccess(p2, time.Millisecond)
	nm.recordPeerFailure(p1)

	best := nm.selectBestPeer([][32]byte{p1, p2, p3})
	if best != p2 {
		t.Fatalf("expected p2 (best score), got %x", best[:4])
	}
}

func TestSelectBestPeerEmpty(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	best := nm.selectBestPeer(nil)
	if best != ([32]byte{}) {
		t.Fatal("expected zero peer for empty input")
	}
}

// --- Concurrency ---

func TestDASNetworkManagerConcurrency(t *testing.T) {
	custody := newMockCustodyManager()
	nm := NewDASNetworkManager(DefaultNetworkConfig(), custody)
	nm.Start()

	// Register a supernode peer.
	peer := [32]byte{0xff}
	custody.RegisterPeer(peer, dastypes.NumberOfCustodyGroups)

	nm.SetSampleProvider(func(peerID [32]byte, columnIndex uint64) (*dastypes.DataColumn, time.Duration, error) {
		return &dastypes.DataColumn{Index: dastypes.ColumnIndex(columnIndex), Cells: []dastypes.Cell{{}}}, time.Millisecond, nil
	})

	var wg sync.WaitGroup

	// Concurrent sampling.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(slot int) {
			defer wg.Done()
			nm.PerformSampling(uint64(slot), 4)
		}(i)
	}

	// Concurrent subscriptions.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(subnet int) {
			defer wg.Done()
			nm.SubscribeToSubnets([]uint64{uint64(subnet % int(dastypes.DataColumnSidecarSubnetCount))})
		}(i)
	}

	// Concurrent publishes.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			col := dastypes.DataColumn{
				Index: dastypes.ColumnIndex(idx % int(dastypes.NumberOfColumns)),
				Cells: []dastypes.Cell{{}},
			}
			subnet := uint64(idx % int(dastypes.DataColumnSidecarSubnetCount))
			nm.PublishColumn(col, subnet)
		}(i)
	}

	// Concurrent metric reads.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			nm.Metrics()
			nm.SamplingSuccessRate()
			nm.Subscriptions()
		}()
	}

	wg.Wait()

	metrics := nm.Metrics()
	if metrics.TotalSamplingRounds != 20 {
		t.Fatalf("expected 20 sampling rounds, got %d", metrics.TotalSamplingRounds)
	}
}

// --- Availability threshold ---

func TestAvailabilityThreshold(t *testing.T) {
	cfg := DefaultNetworkConfig()
	// Set a strict threshold: need all samples.
	cfg.AvailabilityThreshold = 1.0
	nm := NewDASNetworkManager(cfg, nil)
	nm.Start()

	// Store only half the columns.
	for i := uint64(0); i < dastypes.NumberOfColumns/2; i++ {
		nm.StoreColumn(&dastypes.DataColumn{Index: dastypes.ColumnIndex(i), Cells: []dastypes.Cell{{}}})
	}

	// With many samples, we'll likely miss some.
	result, _ := nm.PerformSampling(1, 32)
	// At 100% threshold, any failure means unavailable.
	if result.Failed > 0 && result.AvailabilityConfirmed {
		t.Fatal("with 100% threshold, any failure should deny availability")
	}
}

// --- selectSampleColumns ---

func TestSelectSampleColumnsDeterministic(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)

	cols1 := nm.selectSampleColumns(42, 8)
	cols2 := nm.selectSampleColumns(42, 8)

	if len(cols1) != len(cols2) {
		t.Fatal("non-deterministic column count")
	}
	for i := range cols1 {
		if cols1[i] != cols2[i] {
			t.Fatalf("non-deterministic: cols[%d] = %d vs %d", i, cols1[i], cols2[i])
		}
	}
}

func TestSelectSampleColumnsUnique(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	cols := nm.selectSampleColumns(99, 16)

	seen := make(map[uint64]bool)
	for _, c := range cols {
		if seen[c] {
			t.Fatalf("duplicate column %d", c)
		}
		seen[c] = true
		if c >= dastypes.NumberOfColumns {
			t.Fatalf("column %d out of range", c)
		}
	}
}

func TestSelectSampleColumnsSorted(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	cols := nm.selectSampleColumns(1, 10)

	for i := 1; i < len(cols); i++ {
		if cols[i] < cols[i-1] {
			t.Fatalf("columns not sorted: [%d]=%d < [%d]=%d",
				i, cols[i], i-1, cols[i-1])
		}
	}
}

func TestSelectSampleColumnsDifferentSlots(t *testing.T) {
	nm := NewDASNetworkManager(DefaultNetworkConfig(), nil)
	cols1 := nm.selectSampleColumns(1, 8)
	cols2 := nm.selectSampleColumns(2, 8)

	// Different slots should (almost certainly) produce different columns.
	allSame := true
	for i := range cols1 {
		if cols1[i] != cols2[i] {
			allSame = false
			break
		}
	}
	if allSame {
		t.Error("fixed slots produced identical sample columns")
	}
}
