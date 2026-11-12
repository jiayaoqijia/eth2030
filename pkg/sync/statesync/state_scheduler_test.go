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
