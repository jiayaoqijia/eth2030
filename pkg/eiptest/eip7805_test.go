package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/focil"
)

// TestEIP7805_FOCILSatisfactionAndCompliance verifies EIP-7805: ILs stored
// for a slot gate the next block through the Engine API path, and the
// satisfaction check distinguishes satisfying vs violating blocks.
func TestEIP7805_FOCILSatisfactionAndCompliance(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
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
	il := &focil.InclusionList{Slot: 1, ProposerIndex: 1, Entries: []focil.InclusionListEntry{{Transaction: enc}}}

	ep := s.Engine()
	attrs := ep.Attributes()
	attrs.InclusionListTransactions = [][]byte{enc}
	id, err := ep.FCUWith(attrs)
	if err != nil {
		t.Fatal(err)
	}
	built, err := ep.GetPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	status, err := ep.Process(built.ExecutionPayload)
	if err != nil {
		t.Fatal(err)
	}
	ep.ExpectStatusValid(t, status)
	ep.ExpectPayloadTxCount(t, built.ExecutionPayload, 1)

	// Satisfaction semantics on the sim chain: a block containing the IL tx
	// satisfies it; a subsequent empty block does not.
	head := ep.HeadBlock()
	simRes := focil.CheckILSatisfaction(head, []*focil.InclusionList{il}, nil, head.GasLimit()-head.GasUsed())
	if simRes != focil.ILSatisfied {
		t.Fatal("sim block containing the IL tx reported unsatisfied")
	}
	empty := s.Commit()
	emptyRes := focil.CheckILSatisfaction(empty.Block, []*focil.InclusionList{il}, nil, empty.Block.GasLimit())
	if emptyRes == focil.ILSatisfied {
		t.Fatal("empty block reported as satisfying a non-empty IL")
	}
}
