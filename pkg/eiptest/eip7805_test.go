package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/focil"
)

// TestEIP7805_FOCILSatisfactionAndCompliance verifies EIP-7805: ILs stored
// for a slot gate the next block through the Engine API path, and the
// satisfaction check distinguishes satisfying vs violating blocks.
func TestEIP7805_FOCILSatisfactionAndCompliance(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, eiptest.Never))
	alice := s.Alice()

	// IL carrying one transfer tx.
	tx := eiptest.DynamicFee(
		s.Config().ChainID, alice, s.Nonce(alice.Address),
		eiptest.ToPtr(s.Bob().Address), big.NewInt(3), nil,
		100_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatal(err)
	}
	ilCore := &types.InclusionList{Slot: 1, ValidatorIndex: 1, Transactions: [][]byte{enc}}
	il := &focil.InclusionList{Slot: 1, ProposerIndex: 1, Entries: []focil.InclusionListEntry{{Transaction: enc}}}

	// Engine-side: store the IL, then a block containing the tx is VALID.
	ep := s.Engine()
	if err := ep.Backend().ProcessInclusionList(ilCore); err != nil {
		t.Fatalf("ProcessInclusionList: %v", err)
	}
	ep.Submit(tx)
	payload, status, err := ep.BuildAndProcess()
	if err != nil {
		t.Fatalf("BuildAndProcess: %v", err)
	}
	ep.ExpectStatusValid(t, status)
	ep.ExpectPayloadTxCount(t, payload, 1)

	// Satisfaction semantics on the sim chain: a block containing the IL tx
	// satisfies it; a subsequent empty block does not.
	simR := s.Commit(tx)
	eiptest.ExpectSuccess(t, simR, 0)
	simRes := focil.CheckILSatisfaction(simR.Block, []*focil.InclusionList{il}, nil, simR.Block.GasLimit()-simR.Block.GasUsed())
	if simRes != focil.ILSatisfied {
		t.Fatal("sim block containing the IL tx reported unsatisfied")
	}
	empty := s.Commit()
	emptyRes := focil.CheckILSatisfaction(empty.Block, []*focil.InclusionList{il}, nil, empty.Block.GasLimit())
	if emptyRes == focil.ILSatisfied {
		t.Fatal("empty block reported as satisfying a non-empty IL")
	}
}
