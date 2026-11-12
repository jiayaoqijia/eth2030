package statesync

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
)

type failingRangePeer struct {
	SnapPeer
	requests int
}

func (p *failingRangePeer) ID() string { return "failing" }
func (p *failingRangePeer) RequestAccountRange(AccountRangeRequest) (*AccountRangeResponse, error) {
	p.requests++
	return nil, errors.New("unavailable")
}
func TestStateSchedulerExhaustionIsFailure(t *testing.T) {
	db := rawdb.NewMemoryDB()
	scheduler := NewStateSyncScheduler(NewSnapshotWriter(db), nil)
	if err := scheduler.SetPivot(&types.Header{Number: big.NewInt(10), Root: types.EmptyRootHash}); err != nil {
		t.Fatal(err)
	}
	peer := &failingRangePeer{}
	if err := scheduler.Run(peer); !errors.Is(err, ErrTaskFailed) {
		t.Fatalf("expected task failure, got %v", err)
	}
	if scheduler.Phase() != SyncPhaseFailed || peer.requests != TaskRetryLimit {
		t.Fatalf("exhaustion reported success: %+v", scheduler.Progress())
	}
}
func TestStateSchedulerCancelledRetry(t *testing.T) {
	scheduler := NewStateSyncScheduler(NewSnapshotWriter(rawdb.NewMemoryDB()), nil)
	if err := scheduler.SetPivot(&types.Header{Number: big.NewInt(10), Root: types.EmptyRootHash}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if err := scheduler.RunContext(ctx, &failingRangePeer{}); err == nil {
		t.Fatal("cancelled sync succeeded")
	}
	if time.Since(start) > time.Second || scheduler.Phase() == SyncPhaseDone {
		t.Fatal("cancelled sync did not stop promptly")
	}
}

type retryStoragePeer struct {
	SnapPeer
	origins []types.Hash
	account types.Hash
}

func (p *retryStoragePeer) RequestStorageRange(req StorageRangeRequest) (*StorageRangeResponse, error) {
	p.origins = append(p.origins, req.Origin)
	switch len(p.origins) {
	case 1:
		return &StorageRangeResponse{More: true, Slots: []StorageData{{AccountHash: p.account, SlotHash: types.IntToHash(big.NewInt(7)), Value: []byte{1}}}}, nil
	case 2:
		return nil, errors.New("temporary failure")
	default:
		return &StorageRangeResponse{Slots: []StorageData{{AccountHash: p.account, SlotHash: types.IntToHash(big.NewInt(9)), Value: []byte{2}}}}, nil
	}
}

type recordingStorageWriter struct {
	StateWriter
	slots []types.Hash
}

func (w *recordingStorageWriter) WriteStorage(_ types.Hash, slot types.Hash, _ []byte) error {
	w.slots = append(w.slots, slot)
	return nil
}

func TestStateSchedulerStorageRetryKeepsPage(t *testing.T) {
	account := types.IntToHash(big.NewInt(1))
	writer := &recordingStorageWriter{}
	scheduler := NewStateSyncScheduler(writer, nil)
	scheduler.storageTasks = []*storageTask{{accountHash: account}}
	peer := &retryStoragePeer{account: account}
	if err := scheduler.runStorageDownload(peer); err != nil {
		t.Fatal(err)
	}
	want := types.IntToHash(big.NewInt(8))
	if len(peer.origins) != 3 || peer.origins[1] != want || peer.origins[2] != want {
		t.Fatalf("retry did not preserve page origin: %v", peer.origins)
	}
	if len(writer.slots) != 2 || scheduler.Progress().StorageSlotsTotal != 2 {
		t.Fatalf("retry repeated downloaded slots: %v, %+v", writer.slots, scheduler.Progress())
	}
}
