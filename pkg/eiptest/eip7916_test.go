package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/ssz"
)

// chunk converts a byte into a 32-byte chunk.
func chunk(b byte) [32]byte {
	var c [32]byte
	c[31] = b
	return c
}

// TestEIP7916_ProgressiveList verifies EIP-7916 progressive list semantics:
// deterministic merkleization, growth changes the root, and appending is
// reflected in Len/Get.
func TestEIP7916_ProgressiveList(t *testing.T) {
	s := eiptest.New(t)
	_ = s

	pl := ssz.NewProgressiveList([][32]byte{chunk(1), chunk(2), chunk(3)})
	if pl.Len() != 3 {
		t.Fatalf("Len = %d, want 3", pl.Len())
	}
	root1 := pl.HashTreeRoot()
	root1b := pl.HashTreeRoot()
	if root1 != root1b {
		t.Fatal("progressive merkleization not deterministic")
	}

	// Growing the list changes the root.
	pl.Append(chunk(4))
	if pl.Len() != 4 {
		t.Fatalf("Len after append = %d, want 4", pl.Len())
	}
	if pl.HashTreeRoot() == root1 {
		t.Fatal("appending an element did not change the progressive root")
	}

	// Element access round-trips.
	got, err := pl.Get(3)
	if err != nil {
		t.Fatalf("Get(3): %v", err)
	}
	if got != chunk(4) {
		t.Fatalf("Get(3) = %x, want chunk(4)", got)
	}

	// Empty list has a defined root too.
	empty := ssz.NewProgressiveListEmpty()
	if empty.Len() != 0 {
		t.Fatalf("empty list Len = %d", empty.Len())
	}
	_ = empty.HashTreeRoot()
}
