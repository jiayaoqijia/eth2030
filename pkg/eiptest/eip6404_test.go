package eiptest_test

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP6404_SSZTransactions verifies SSZ serialization of typed
// transactions (EIP-6404): decoding and canonical re-encoding across the envelope types
// that support it.
func TestEIP6404_SSZTransactions(t *testing.T) {
	s := eiptest.New(t)
	enc := s.Encoding()
	chainID := s.Config().ChainID
	alice := s.Alice()
	bob := eiptest.ToPtr(s.Bob().Address)

	cases := map[string]*types.Transaction{
		"legacy":      eiptest.UnsignedLegacy(alice.Address, 1, bob, big.NewInt(1), nil, 21000, big.NewInt(2_000_000_000)),
		"access-list": eiptest.UnsignedAccessList(alice.Address, chainID, 2, bob, big.NewInt(1), nil, 21000, big.NewInt(2_000_000_000), big.NewInt(1), nil),
		"dynamic-fee": eiptest.UnsignedDynamicFee(alice.Address, chainID, 3, bob, big.NewInt(1), nil, 21000, big.NewInt(2_000_000_000), big.NewInt(1)),
		"blob":        eiptest.UnsignedBlob(alice.Address, chainID, 4, *bob, eiptest.Wei(1), 100_000, bigFeeCap, bigTipCap, bigFeeCap, []types.Hash{eiptest.BlobHash(1)}),
		"set-code":    eiptest.SetCode(chainID, alice, 5, *bob, 100_000, bigFeeCap, bigTipCap, []types.Authorization{eiptest.Auth(chainID, alice, *bob, 6)}),
	}
	for name, tx := range cases {
		t.Run(name, func(t *testing.T) {
			enc.SSZRoundTrip(t, tx)
		})
	}
}

func TestEIP6404_LegacySignatureOverflow(t *testing.T) {
	s := eiptest.New(t)
	legacy := eiptest.Legacy(s.Config().ChainID.Uint64(), s.Alice(), 0, eiptest.ToPtr(s.Bob().Address), nil, nil, 21_000, bigFeeCap)
	if _, err := types.TransactionToSSZ(legacy); !errors.Is(err, types.ErrSSZLegacySignature) {
		t.Fatalf("legacy signature with large v: %v", err)
	}
}
