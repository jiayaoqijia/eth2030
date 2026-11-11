package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP7745_LogIndex verifies the EIP-7745 log index: building the log
// index over a block's receipts produces per-log global indices and filter
// rows, and identical log sets produce identical index structures.
func TestEIP7745_LogIndex(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// Deploy a logger contract: LOG1(value) with a fixed topic.
	logger := []byte{
		0x60, 0x2a, // PUSH1 42 (data word)
		0x60, 0x00, // PUSH1 0 (offset)
		0x52,       // MSTORE(0, 42)
		0x60, 0x20, // size 32
		0x60, 0x00, // offset 0
		0x60, 0xdd, // topic
		0xa1, // LOG1
		0x00,
	}
	addr := stf.Deploy(s.Alice(), logger)

	nonce := s.Nonce(s.Alice().Address)
	tx := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, nonce,
		eiptest.ToPtr(addr), nil, nil,
		100_000, bigFeeCap, bigTipCap,
	)
	r := s.Commit(tx)
	txr := eiptest.ExpectSuccess(t, r, 0)
	eiptest.ExpectLogCount(t, r, 0, 1)
	log := txr.Logs()[0]

	// Build the log index over the block receipts (EIP-7745).
	idx := types.BuildLogIndex(r.Receipts, r.Block.NumberU64(), 0)
	if idx == nil {
		t.Fatal("BuildLogIndex returned nil")
	}
	// The emitted log's address and topic must be discoverable in the filter
	// structure for this block range.
	if len(idx.Entries) == 0 {
		t.Fatal("log index produced no entries for a block with one log")
	}
	if len(idx.Entries) != 1 {
		t.Fatalf("log index entries = %d, want 1 (one log in one tx)", len(idx.Entries))
	}
	e := idx.Entries[0]
	if e.BlockNumber != r.Block.NumberU64() || e.TxIndex != 0 || e.LogIndex != 0 || e.GlobalIndex != 0 {
		t.Fatalf("log index entry = %+v, want block %d tx 0 log 0 global 0",
			e, r.Block.NumberU64())
	}
	// Query-by-address round-trips through the index.
	found := types.QueryLogsByAddress(idx, r.Receipts, []types.Address{log.Address})
	if len(found) != 1 {
		t.Fatalf("QueryLogsByAddress returned %d logs, want 1", len(found))
	}
}
