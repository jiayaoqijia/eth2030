package eth

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// TestProtocolVersionConstants verifies the eth protocol version constants.
func TestProtocolVersionConstants(t *testing.T) {
	if ETH68 != 68 {
		t.Fatalf("ETH68 = %d, want 68", ETH68)
	}
	if ETH70 != 70 {
		t.Fatalf("ETH70 = %d, want 70", ETH70)
	}
	if ETH71 != 71 {
		t.Fatalf("ETH71 = %d, want 71", ETH71)
	}
}

// TestMaxLimits verifies the protocol limit constants.
func TestMaxLimits(t *testing.T) {
	if MaxHeaders != 1024 {
		t.Fatalf("MaxHeaders = %d, want 1024", MaxHeaders)
	}
	if MaxBodies != 512 {
		t.Fatalf("MaxBodies = %d, want 512", MaxBodies)
	}
	if MaxReceiptsResponseSize != 10<<20 {
		t.Fatalf("MaxReceiptsResponseSize = %d, want %d", MaxReceiptsResponseSize, 10<<20)
	}
	if MaxAccessLists != 64 {
		t.Fatalf("MaxAccessLists = %d, want 64", MaxAccessLists)
	}
}

// TestStatusInfo_Fields tests that StatusInfo fields can be populated.
func TestStatusInfo_Fields(t *testing.T) {
	info := StatusInfo{
		ProtocolVersion: ETH68,
		NetworkID:       1,
		Head:            types.HexToHash("0x1111111111111111111111111111111111111111111111111111111111111111"),
		Genesis:         types.HexToHash("0x2222222222222222222222222222222222222222222222222222222222222222"),
		OldestBlock:     100,
	}
	if info.ProtocolVersion != 68 {
		t.Fatalf("want 68, got %d", info.ProtocolVersion)
	}
	if info.NetworkID != 1 {
		t.Fatalf("want 1, got %d", info.NetworkID)
	}
	if info.OldestBlock != 100 {
		t.Fatalf("want 100, got %d", info.OldestBlock)
	}
	if info.Head == (types.Hash{}) {
		t.Fatal("Head should not be zero")
	}
	if info.Genesis == (types.Hash{}) {
		t.Fatal("Genesis should not be zero")
	}
}

// TestAccessListProvider_Interface pins the eth/71 provider signature.
func TestAccessListProvider_Interface(t *testing.T) {
	alp := newMockAccessListProvider()
	var _ AccessListProvider = alp
	alp.accessLists[types.HexToHash("0xaaaa")] = []byte{0xc0}
	bal, ok := alp.GetBlockAccessListRLP(types.HexToHash("0xaaaa"))
	if !ok || len(bal) != 1 || bal[0] != 0xc0 {
		t.Fatalf("unexpected provider response: %x ok=%v", bal, ok)
	}
	if _, ok := alp.GetBlockAccessListRLP(types.HexToHash("0xbbbb")); ok {
		t.Fatal("unavailable BAL must report ok=false")
	}
}

// Compile-time interface satisfaction checks for mock types used in tests.
var (
	_ Blockchain = (*mockBlockchain)(nil)
	_ TxPool     = (*mockTxPool)(nil)
)

// TestStatusInfo_ZeroValues tests default zero values for StatusInfo.
func TestStatusInfo_ZeroValues(t *testing.T) {
	var info StatusInfo
	if info.ProtocolVersion != 0 {
		t.Fatalf("want 0, got %d", info.ProtocolVersion)
	}
	if info.NetworkID != 0 {
		t.Fatalf("want 0, got %d", info.NetworkID)
	}
	if info.TD != nil {
		t.Fatal("TD should be nil by default")
	}
	if info.OldestBlock != 0 {
		t.Fatalf("want 0, got %d", info.OldestBlock)
	}
}

// TestReceiptProvider_Interface pins the eth/70 provider signature.
func TestReceiptProvider_Interface(t *testing.T) {
	var _ ReceiptProvider = (*mockReceiptProvider)(nil)
	rp := newMockReceiptProvider()
	if got := rp.GetReceipts(types.HexToHash("0xaaaa")); got != nil {
		t.Fatalf("unknown block must return nil, got %v", got)
	}
}
