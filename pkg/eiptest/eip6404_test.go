package eiptest_test

import (
	"errors"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

func TestEIP6404_SSZTransactions(t *testing.T) {
	s := eiptest.New(t)
	enc := s.Encoding()
	chainID := s.Config().ChainID
	alice := s.Alice()
	bob := eiptest.ToPtr(s.Bob().Address)

	cases := []struct {
		name string
		tx   *types.Transaction
	}{
		{"legacy-replayable-call", eiptest.Legacy(0, alice, 1, bob, eiptest.Wei(1), nil, 21_000, bigFeeCap)},
		{"legacy-replayable-create", eiptest.Legacy(0, alice, 2, nil, nil, []byte{1}, 100_000, bigFeeCap)},
		{"legacy-eip155-call", eiptest.Legacy(chainID.Uint64(), alice, 3, bob, eiptest.Wei(1), nil, 21_000, bigFeeCap)},
		{"legacy-eip155-create", eiptest.Legacy(chainID.Uint64(), alice, 4, nil, nil, []byte{1}, 100_000, bigFeeCap)},
		{"access-list-call", eiptest.AccessList(chainID, alice, 5, bob, eiptest.Wei(1), nil, 21_000, bigFeeCap, nil)},
		{"access-list-create", eiptest.AccessList(chainID, alice, 6, nil, nil, []byte{1}, 100_000, bigFeeCap, nil)},
		{"dynamic-fee-call", eiptest.DynamicFee(chainID, alice, 7, bob, eiptest.Wei(1), nil, 21_000, bigFeeCap, bigTipCap)},
		{"dynamic-fee-create", eiptest.DynamicFee(chainID, alice, 8, nil, nil, []byte{1}, 100_000, bigFeeCap, bigTipCap)},
		{"blob", eiptest.Blob(chainID, alice, 9, *bob, eiptest.Wei(1), 100_000, bigFeeCap, bigTipCap, bigFeeCap, []types.Hash{eiptest.BlobHash(1)}, nil)},
		{"set-code", eiptest.SetCode(chainID, alice, 10, *bob, 100_000, bigFeeCap, bigTipCap, []types.Authorization{
			eiptest.Auth(eiptest.Wei(0), alice, *bob, 11),
			eiptest.Auth(chainID, alice, *bob, 12),
		})},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc.SSZRoundTrip(t, tc.tx)
			wire, err := types.TransactionToSSZ(tc.tx)
			if err != nil || wire[8] != byte(i+1) {
				t.Fatalf("selector %x: %v", wire, err)
			}
			decoded, err := types.SSZToTransaction(wire)
			if err != nil {
				t.Fatal(err)
			}
			from, err := types.MakeSigner(decoded.ChainId().Uint64(), decoded.Type()).Sender(decoded)
			if err != nil || from != alice.Address {
				t.Fatalf("sender %s: %v", from.Hex(), err)
			}
		})
	}
}

func TestEIP6404_RejectsUnsignedTransaction(t *testing.T) {
	s := eiptest.New(t)
	unsigned := eiptest.UnsignedLegacy(s.Alice().Address, 1, eiptest.ToPtr(s.Bob().Address), nil, nil, 21_000, bigFeeCap)
	if _, err := types.TransactionToSSZ(unsigned); !errors.Is(err, types.ErrSSZTransactionSignature) {
		t.Fatalf("unsigned transaction: %v", err)
	}
}
