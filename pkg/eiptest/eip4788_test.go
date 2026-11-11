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
	first := s.CommitWithBeaconRoot(types.Hash{1})
	second := s.CommitWithBeaconRoot(beaconRoot)
	ts, ts2 := first.Block.Time(), second.Block.Time()

	slotTS := ts2 % eips.BeaconHistoryBufferLength
	storedTS := s.StorageAt(eips.BeaconRootAddress, eiptest.Uint64ToHash(slotTS))
	if got := eiptest.Uint64ToHash(ts2); storedTS != got {
		t.Fatalf("beacon ring timestamp slot %d = %s, want head time %s", slotTS, storedTS.Hex(), got.Hex())
	}

	rootSlot := eiptest.Uint64ToHash(eips.BeaconHistoryBufferLength + slotTS)
	storedRoot := s.StorageAt(eips.BeaconRootAddress, rootSlot)
	if storedRoot != beaconRoot {
		t.Fatalf("beacon root = %s, want %s", storedRoot.Hex(), beaconRoot.Hex())
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
