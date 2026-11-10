package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/gas"

	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP7691_BlobThroughputSchedule verifies that BPO fork switches change
// the blob parameter schedule through a real chain crossing (via the fork
// probe), covering the EIP-7691 BPO1/BPO2 throughput bumps.
func TestEIP7691_BlobThroughputSchedule(t *testing.T) {
	s := eiptest.New(t,
		eiptest.ForkAt(eiptest.ForkBPO1, 100),
		eiptest.ForkAt(eiptest.ForkBPO2, 300),
	)

	fp := s.Fork(eiptest.ForkBPO1)
	before := fp.Before()
	before.ExpectInactive(t)
	maxBefore := gas.GetBlobSchedule(s.Config(), before.Time()).Max
	_ = maxBefore

	boundary := fp.AtBoundary()
	boundary.ExpectActive(t)
	sched1 := gas.GetBlobSchedule(s.Config(), boundary.Time())
	target1, max1, frac1 := sched1.Target, sched1.Max, sched1.UpdateFraction
	if max1 <= maxBefore {
		t.Fatalf("BPO1 max blobs = %d, want increase over pre-BPO1 (%d)", max1, maxBefore)
	}
	if target1 == 0 || frac1 == 0 {
		t.Fatalf("BPO1 schedule incomplete: target=%d frac=%d", target1, frac1)
	}

	fp2 := s.Fork(eiptest.ForkBPO2)
	b2 := fp2.AtBoundary()
	b2.ExpectActive(t)
	max2 := gas.GetBlobSchedule(s.Config(), b2.Time()).Max
	if max2 <= max1 {
		t.Fatalf("BPO2 max blobs = %d, want increase over BPO1 (%d)", max2, max1)
	}
}
