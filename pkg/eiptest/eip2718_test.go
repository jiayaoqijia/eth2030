package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP2718_TypedEnvelopeMatrix verifies the typed transaction envelope:
// every supported tx type commits successfully, and the type byte round-trips.
func TestEIP2718_TypedEnvelopeMatrix(t *testing.T) {
	s := eiptest.New(t)
	enc := s.Encoding()
	chainID := s.Config().ChainID
	alice := s.Alice()
	bob := eiptest.ToPtr(s.Bob().Address)
	one := big.NewInt(1)
	gas := uint64(100_000)
	feeCap := big.NewInt(2_000_000_000)
	tip := big.NewInt(1_000_000_000)

	// Builders run lazily so each tx gets a fresh nonce.
	cases := []struct {
		name string
		build func() *types.Transaction
	}{
		{"legacy", func() *types.Transaction {
			return eiptest.Legacy(s.Config().ChainID.Uint64(), alice, s.Nonce(alice.Address), bob, one, nil, gas, big.NewInt(2_000_000_000))
		}},
		{"access-list-2930", func() *types.Transaction {
			return eiptest.UnsignedAccessList(alice.Address, chainID, s.Nonce(alice.Address), bob, one, nil, gas, feeCap, tip, nil)
		}},
		{"dynamic-fee-1559", func() *types.Transaction {
			return eiptest.UnsignedDynamicFee(alice.Address, chainID, s.Nonce(alice.Address), bob, one, nil, gas, feeCap, tip)
		}},
		{"blob-4844", func() *types.Transaction {
			return eiptest.UnsignedBlob(alice.Address, chainID, s.Nonce(alice.Address), s.Bob().Address, one, gas, feeCap, tip, big.NewInt(1_000_000_000), []types.Hash{eiptest.BlobHash(1)})
		}},
		{"setcode-7702", func() *types.Transaction {
			return eiptest.UnsignedSetCode(alice.Address, chainID, s.Nonce(alice.Address), s.Bob().Address, gas, feeCap, tip, nil)
		}},
	}
	sentTypes := map[uint8]bool{}
	for _, tc := range cases {
		tx := tc.build()
		r := s.Commit(tc.build()) // one block per tx (distinct nonces)
		// Documented builder behavior (b683107): sidecar-less blob txs are
		// excluded from payloads until BlobsBundle support is wired.
		if tx.Type() == types.BlobTxType {
			if r.TxCount() != 0 {
				t.Fatalf("%s: sidecar-less blob tx unexpectedly included", tc.name)
			}
			sentTypes[tx.Type()] = true
			continue
		}
		eiptest.ExpectSuccess(t, r, 0)
		sentTypes[tx.Type()] = true
		// RLP roundtrip preserves the type byte (EIP-2718 envelope core).
		enc.RLPRoundTrip(t, tx)
		if got := r.Block.Transactions()[0].Type(); got != tx.Type() {
			t.Fatalf("%s: block tx type = %d, want %d", tc.name, got, tx.Type())
		}
	}
	for _, want := range []uint8{types.LegacyTxType, types.AccessListTxType, types.DynamicFeeTxType, types.BlobTxType, types.SetCodeTxType} {
		if !sentTypes[want] {
			t.Fatalf("type 0x%02x missing from matrix", want)
		}
	}
}
