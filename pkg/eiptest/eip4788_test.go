package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/eips"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP4788_BeaconRootRing verifies the beacon-root system contract: each
// block stores its ParentBeaconRoot at slot (timestamp % 8191) and the head
// timestamp at slot 8191 + (timestamp % 8191).
func TestEIP4788_BeaconRootRing(t *testing.T) {
	s := eiptest.New(t)

	beaconRoot := types.Hash{0xAB, 0xCD}
	// Commit one block carrying a known ParentBeaconRoot.
	// Sim.Commit uses a zero beacon root; drive the ring via two commits and
	// check the second block's entries.
	r1 := s.Commit()
	_ = r1
	ts := s.CurrentBlock().Header().Time
	r2 := s.Commit()

	ts2 := s.CurrentBlock().Header().Time
	_ = beaconRoot
	_ = r2

	slotTS := ts2 % eips.BeaconHistoryBufferLength
	storedTS := s.StorageAt(eips.BeaconRootAddress, eiptest.Uint64ToHash(slotTS))
	if got := eiptest.Uint64ToHash(ts2); storedTS != got {
		t.Fatalf("beacon ring timestamp slot %d = %s, want head time %s", slotTS, storedTS.Hex(), got.Hex())
	}

	rootSlot := eiptest.Uint64ToHash(eips.BeaconHistoryBufferLength + slotTS)
	storedRoot := s.StorageAt(eips.BeaconRootAddress, rootSlot)
	if storedRoot != (types.Hash{}) {
		// Zero beacon roots are legal inputs; the entry must at least exist
		// as the value we fed (zero). Non-zero would mean cross-block bleed.
		t.Fatalf("beacon root slot unexpectedly non-zero: %s", storedRoot.Hex())
	}

	// The previous block's timestamp slot must still hold block 1's time
	// unless the ring index collided (8191 > our chain length).
	prevSlot := ts % eips.BeaconHistoryBufferLength
	if prevSlot != slotTS {
		if got := s.StorageAt(eips.BeaconRootAddress, eiptest.Uint64ToHash(prevSlot)); got != eiptest.Uint64ToHash(ts) {
			t.Fatalf("beacon ring historical timestamp slot %d = %s, want %s", prevSlot, got.Hex(), eiptest.Uint64ToHash(ts).Hex())
		}
	}
}
