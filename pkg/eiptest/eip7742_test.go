package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/gas"

	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP7742_UncoupledBlobTargetMax verifies that the blob schedule exposes
// decoupled target and max blob counts that change independently at BPO
// boundaries.
func TestEIP7742_UncoupledBlobTargetMax(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkBPO1, 100))

	fp := s.Fork(eiptest.ForkBPO1)
	before := fp.Before()
	schedB := gas.GetBlobSchedule(s.Config(), before.Time())
	tb, mb := schedB.Target, schedB.Max
	if mb == 0 {
		t.Fatal("pre-BPO1 max blobs = 0")
	}
	if tb >= mb {
		t.Fatalf("pre-BPO1 schedule must have target < max: target=%d max=%d", tb, mb)
	}

	at := fp.AtBoundary()
	schedA := gas.GetBlobSchedule(s.Config(), at.Time())
	ta, ma := schedA.Target, schedA.Max
	if ma <= mb {
		t.Fatalf("BPO1 did not raise max blobs: %d -> %d", mb, ma)
	}
	if ta == tb {
		t.Fatalf("BPO1 did not adjust target blobs: %d -> %d (uncoupled schedule violated)", tb, ta)
	}
}
