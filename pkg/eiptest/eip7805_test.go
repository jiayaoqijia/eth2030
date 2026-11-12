package eiptest_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/eth2030/eth2030/consensus"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
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

// eip7805GossipContext provides every gossip hook over a 16-member IL
// committee with deterministic BLS keys, per EIP-7805 IL_COMMITTEE_SIZE.
type eip7805GossipContext struct {
	slot    uint64
	pubkeys map[uint64][48]byte
	members map[uint64]bool
	root    types.Hash
}

func newEIP7805GossipContext() (*focil.GossipContext, []*big.Int, types.Hash) {
	state := &eip7805GossipContext{
		slot:    100,
		pubkeys: make(map[uint64][48]byte),
		members: make(map[uint64]bool),
	}
	secrets := make([]*big.Int, focil.IL_COMMITTEE_SIZE)
	for i := 0; i < focil.IL_COMMITTEE_SIZE; i++ {
		secrets[i] = big.NewInt(int64(7000 + i))
		state.pubkeys[uint64(i)] = crypto.BLSPubkeyFromSecret(secrets[i])
		state.members[uint64(i)] = true
	}
	state.root = crypto.Keccak256Hash([]byte("il-committee-root"))
	return &focil.GossipContext{
		CurrentSlot:           func() uint64 { return state.slot },
		CommitteeRootForSlot:  func(uint64) (types.Hash, error) { return state.root, nil },
		IsILCommitteeMember:   func(idx, _ uint64) bool { return state.members[idx] },
		ValidatorPubkey:       func(idx uint64) ([48]byte, error) { return state.pubkeys[idx], nil },
		ForkVersion:           func() [4]byte { return [4]byte{} },
		GenesisValidatorsRoot: func() [32]byte { return [32]byte{} },
	}, secrets, state.root
}

// eip7805SignIL signs the IL with DOMAIN_IL_COMMITTEE, mirroring what an IL
// committee member does before gossiping.
func eip7805SignIL(t *testing.T, secret *big.Int, il *focil.InclusionList) *focil.SignedInclusionList {
	t.Helper()
	domain := consensus.DomainSeparation(focil.DomainILCommittee, [4]byte{}, [32]byte{})
	signingRoot := consensus.ComputeSigningRoot(il.HashTreeRoot(), domain)
	return &focil.SignedInclusionList{
		Message:   *il,
		Signature: crypto.BLSSign(secret, signingRoot[:]),
	}
}

// TestEIP7805_GossipValidationSurface drives the CL P2P validation rules
// end-to-end over the gossip wire format on top of a real Sim chain: an IL
// committee member signs a list carrying a real pending transaction, the
// signed IL crosses the gossip binary encoding, and the six spec rules gate
// it into the IL store — with the tampered path rejected.
func TestEIP7805_GossipValidationSurface(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	alice := s.Alice()

	// A real pending transfer the IL will carry.
	tx := eiptest.DynamicFee(s.Config().ChainID, alice, s.Nonce(alice.Address),
		eiptest.ToPtr(s.Bob().Address), big.NewInt(5), nil, 21_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000))
	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatal(err)
	}

	ctx, secrets, root := newEIP7805GossipContext()
	store := focil.NewILStore()
	validator := focil.NewGossipValidator(store, ctx)

	il := &focil.InclusionList{
		Slot:          100,
		ProposerIndex: 4,
		CommitteeRoot: root,
		Entries:       []focil.InclusionListEntry{{Transaction: enc, Index: 0}},
	}
	signed := eip7805SignIL(t, secrets[4], il)

	// The gossip transport round trip: what a peer receives on the wire.
	wire, err := signed.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var received focil.SignedInclusionList
	if err := received.UnmarshalBinary(wire); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	accepted, err := validator.Validate(&received)
	if err != nil {
		t.Fatalf("valid IL rejected by gossip rules: %v", err)
	}
	if accepted == nil || accepted.ProposerIndex != 4 {
		t.Fatal("accepted IL not returned")
	}
	if store.GetIL(4, 100) == nil {
		t.Fatal("IL not stored")
	}

	// A tampered signature is rejected on the gossip path.
	received.Signature[10] ^= 0xFF
	if _, err := validator.Validate(&received); err == nil || !strings.Contains(err.Error(), "invalid IL signature") {
		t.Fatalf("tampered gossip IL must be rejected, got %v", err)
	}

	// An IL carrying the same tx satisfies a block built from it (EL side).
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)
}
