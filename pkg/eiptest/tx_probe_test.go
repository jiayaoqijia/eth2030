package eiptest_test

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

func TestTransactionBuildersRoundTrip(t *testing.T) {
	s := eiptest.New(t)
	alice := s.Alice()
	chainID := s.Config().ChainID
	to := eiptest.ToPtr(s.Bob().Address)
	fees := [3]*big.Int{bigFeeCap, bigFeeCap, bigFeeCap}
	tips := [3]*big.Int{bigTipCap, bigTipCap, bigTipCap}
	tests := []struct {
		name   string
		tx     *types.Transaction
		signed bool
	}{
		{"legacy", eiptest.Legacy(chainID.Uint64(), alice, 0, to, eiptest.Wei(1), nil, 21_000, bigFeeCap), true},
		{"accessList", eiptest.AccessList(chainID, alice, 0, to, nil, nil, 30_000, bigFeeCap, nil), true},
		{"dynamicFee", eiptest.DynamicFee(chainID, alice, 0, to, nil, nil, 21_000, bigFeeCap, bigTipCap), true},
		{"blob", eiptest.Blob(chainID, alice, 0, *to, nil, 21_000, bigFeeCap, bigTipCap, bigFeeCap, []types.Hash{eiptest.BlobHash(1)}, nil), true},
		{"setCode", eiptest.SetCode(chainID, alice, 0, *to, 100_000, bigFeeCap, bigTipCap, []types.Authorization{eiptest.Auth(chainID, alice, *to, 1)}), true},
		{"aa", eiptest.UnsignedAA(alice.Address, chainID, 0, []byte{1}, 100_000, bigFeeCap, bigTipCap), false},
		{"frame", eiptest.UnsignedFrame(alice.Address, chainID, big.NewInt(0), 100_000, bigFeeCap, bigTipCap, []types.Frame{{Mode: types.ModeVerify, GasLimit: 60_000}}), false},
		{"local", eiptest.UnsignedLocal(alice.Address, chainID, 0, to, nil, []byte{1}, 25_000, bigFeeCap, bigTipCap), false},
		{"multiDimFee", eiptest.MultiDimFee(chainID, alice, 0, to, nil, nil, 21_000, fees, tips), true},
		{"eofCreate", eiptest.EOFCreate(chainID, alice, 0, []byte{0xef, 0, 1}, 100_000, bigFeeCap, bigTipCap), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decoded := s.Encoding().RLPRoundTrip(t, tc.tx)
			if !bytes.Equal(decoded.Data(), tc.tx.Data()) {
				t.Fatal("RLP changed calldata")
			}
			if tc.signed {
				signer := types.MakeSigner(chainID.Uint64(), decoded.Type())
				got, err := signer.Sender(decoded)
				if err != nil || got != alice.Address {
					t.Fatalf("sender %s, want %s: %v", got.Hex(), alice.Address.Hex(), err)
				}
			}
		})
	}
}

func TestEncoding_EIP155Vector(t *testing.T) {
	s := eiptest.New(t, eiptest.WithChainID(1))
	for _, vector := range eiptest.LoadVectors(t, "eip155") {
		t.Run(vector.Name, func(t *testing.T) {
			tx, err := types.DecodeTxRLP(vector.VectorBytes(t, "rlp"))
			if err != nil {
				t.Fatal(err)
			}
			decoded := s.Encoding().RLPRoundTrip(t, tx)
			if got := decoded.SigningHash().Hex(); got != vector.VectorString(t, "signingHash") {
				t.Fatalf("signing hash %s", got)
			}
			got, err := types.NewEIP155Signer(1).Sender(decoded)
			if err != nil || got.Hex() != vector.VectorString(t, "sender") {
				t.Fatalf("sender %s: %v", got.Hex(), err)
			}
		})
	}
}
