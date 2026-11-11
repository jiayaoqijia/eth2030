package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/focil"
)

// TestEIP7547_ILSatisfaction verifies EIP-7547: a block that includes the
// transactions of a stored inclusion list satisfies it, and a block that
// skips them does not.
func TestEIP7547_ILSatisfaction(t *testing.T) {
	s := eiptest.New(t)
	alice := s.Alice()

	// Build a tx and its RLP for the IL.
	tx := eiptest.UnsignedDynamicFee(
		alice.Address, s.Config().ChainID, s.Nonce(alice.Address),
		eiptest.ToPtr(s.Bob().Address), big.NewInt(7), nil,
		100_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("encode tx: %v", err)
	}
	il := &focil.InclusionList{
		Slot:          1,
		ProposerIndex: 3,
		Entries:       []focil.InclusionListEntry{{Transaction: enc}},
	}

	// Commit the tx (satisfies the IL) and check satisfaction against the
	// built block.
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)
	result := focil.CheckILSatisfaction(r.Block, []*focil.InclusionList{il}, nil, r.Block.GasLimit()-r.Block.GasUsed())
	if result != focil.ILSatisfied {
		t.Fatalf("block containing the IL tx reported unsatisfied: %+v", result)
	}

	// A follow-up empty block does NOT satisfy the IL (tx absent).
	empty := s.Commit()
	result2 := focil.CheckILSatisfaction(empty.Block, []*focil.InclusionList{il}, nil, empty.Block.GasLimit()-empty.Block.GasUsed())
	if result2 == focil.ILSatisfied {
		t.Fatal("empty block reported as satisfying a non-empty IL")
	}
}
