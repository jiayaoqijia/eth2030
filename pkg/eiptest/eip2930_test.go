package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP2930_AccessListWarmsStorage verifies EIP-2930: pre-declaring a
// storage slot in the access list makes its first SLOAD warm (100) instead of
// cold (2100), at a cost of 1900 per storage key — a net saving of 200.
func TestEIP2930_AccessListWarmsStorage(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	loader := []byte{
		0x60, 0x00,
		0x54, // SLOAD slot 0
		0x50,
		0x00,
	}
	addr := stf.Deploy(s.Alice(), loader)

	slot := types.Hash{}
	accessList := types.AccessList{
		{Address: addr, StorageKeys: []types.Hash{slot}},
	}

	withAL := eiptest.UnsignedAccessList(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(addr), nil, nil, 200_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000), accessList,
	)
	rWith := s.Commit(withAL)
	txWith := eiptest.ExpectSuccess(t, rWith, 0)

	withoutAL := eiptest.UnsignedAccessList(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(addr), nil, nil, 200_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000), nil,
	)
	rWithout := s.Commit(withoutAL)
	txWithout := eiptest.ExpectSuccess(t, rWithout, 0)

	// Exact intrinsic math (the AL target IS the tx recipient, so the
	// address charge applies): with-AL pays 2400 (address) + 1900 (key)
	// intrinsic and a warm SLOAD (100); without-AL pays a cold SLOAD (2100).
	// Expected delta = 2400 + 1900 + 100 - 2100 = 2300.
	delta := txWith.GasUsed() - txWithout.GasUsed()
	if delta != 2300 {
		t.Fatalf("access list gas delta = %d, want 2300 (2400 addr + 1900 key + 100 warm - 2100 cold)", delta)
	}
}
