package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/bal"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/eiptest"
)


// TestEIP7928_BlockAccessList verifies EIP-7928 at the chain level: a block
// with a value transfer produces a BAL whose entries cover the sender and
// recipient (balance changes), with valid per-account access indexing.
func TestEIP7928_BlockAccessList(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	alice, bob := s.Alice(), s.Bob()

	tx := eiptest.UnsignedDynamicFee(
		alice.Address, s.Config().ChainID, s.Nonce(alice.Address),
		eiptest.ToPtr(s.Bob().Address), big.NewInt(11), nil,
		100_000, bigFeeCap, bigTipCap,
	)
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)

	// Re-execute the block to obtain the BAL object (the builder computes
	// it internally; the processor exposes it for verification).
	parentState, err := s.Chain().StateAtBlock(s.Block(r.Block.NumberU64() - 1))
	if err != nil {
		t.Fatalf("parent state: %v", err)
	}
	proc := execution.NewStateProcessor(s.Config())
	result, err := proc.ProcessWithBAL(r.Block, parentState)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if result.BlockAccessList == nil {
		t.Fatal("BAL is nil for an Amsterdam-active block")
	}
	b := result.BlockAccessList

	foundSender, foundRecipient := false, false
	for _, entry := range b.Entries {
		if entry.Address == alice.Address {
			foundSender = true
		}
		if entry.Address == bob.Address {
			foundRecipient = true
		}
	}
	if !foundSender || !foundRecipient {
		t.Fatalf("BAL missing transfer participants (sender=%v recipient=%v), entries=%d",
			foundSender, foundRecipient, len(b.Entries))
	}

	// The BAL hash must match what the header committed to.
	h := result.BlockAccessList.Hash()
	if r.Block.Header().BlockAccessListHash == nil || *r.Block.Header().BlockAccessListHash != h {
		t.Fatalf("header BAL hash %v != recomputed %s", r.Block.Header().BlockAccessListHash, h.Hex())
	}
}

// TestEIP7928_EmptyBlockSystemContractEntries verifies that even an empty
// block records system-contract accesses (beacon root ring) in the BAL.
func TestEIP7928_EmptyBlockSystemContractEntries(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	r := s.Commit()
	parentState, err := s.Chain().StateAtBlock(s.Block(r.Block.NumberU64() - 1))
	if err != nil {
		t.Fatal(err)
	}
	proc := execution.NewStateProcessor(s.Config())
	result, err := proc.ProcessWithBAL(r.Block, parentState)
	if err != nil {
		t.Fatal(err)
	}
	if result.BlockAccessList == nil {
		t.Fatal("empty block BAL is nil")
	}
	if len(result.BlockAccessList.Entries) == 0 {
		t.Fatal("empty block BAL has no system-contract entries")
	}
}

// TestEIP7928_OrderingInvariant asserts BAL entries are ordered by address
// and per-account storage keys are ordered (validator invariant).
func TestEIP7928_OrderingInvariant(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	stf := s.STF()
	c := stf.Deploy(s.Alice(), []byte{
		0x60, 0x01, 0x60, 0x02, 0x55, // SSTORE(2, 1)
		0x60, 0x02, 0x60, 0x00, 0x55, // SSTORE(0, 2)
		0x00,
	})
	_ = c
	tx := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(s.Bob().Address), nil, nil,
		100_000, bigFeeCap, bigTipCap,
	)
	r := s.Commit(tx)
	parentState, err := s.Chain().StateAtBlock(s.Block(r.Block.NumberU64() - 1))
	if err != nil {
		t.Fatal(err)
	}
	proc := execution.NewStateProcessor(s.Config())
	result, err := proc.ProcessWithBAL(r.Block, parentState)
	if err != nil {
		t.Fatal(err)
	}
	if err := bal.ValidateBALOrdering(result.BlockAccessList); err != nil {
		t.Fatalf("BAL ordering violation: %v", err)
	}
	_ = state.NewMemoryStateDB
}
