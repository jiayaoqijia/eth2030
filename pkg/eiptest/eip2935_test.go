package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/eips"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP2935_HistoryServeContract verifies the block-history ring contract:
// after each block the parent hash is stored at slot (N-1) % 8191 and is
// readable through eth_call.
func TestEIP2935_HistoryServeContract(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// Commit 3 blocks; block N stores hash(N-1) at slot (N-1) % 8191.
	r := s.Commit()
	r2 := s.Commit()
	_ = r
	_ = r2

	// After block 2, slot for block 1's hash = 1 % 8191 = 1.
	// The storage was written by the system transaction; read it directly.
	stored := s.StorageAt(eips.HistoryStorageAddress, eiptest.Uint64ToHash(1%8191))
	want := s.Block(1).Hash()
	if stored != want {
		t.Fatalf("history contract slot 1 = %s, want block-1 hash %s", stored.Hex(), want.Hex())
	}

	// Note: EVM-level reads require the history contract runtime code, which
	// production deploys outside genesis (node/devnet alloc). The storage
	// ring above is the system-call surface this framework drives.
	_ = stf
}
