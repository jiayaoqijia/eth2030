package focil

import (
	"math/big"
	"strings"
	"testing"

	"github.com/eth2030/eth2030/consensus"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// gossipTestContext provides every GossipContext hook over a 16-member
// committee with deterministic BLS keys, per the EIP-7805 committee size.
type gossipTestContext struct {
	slot    uint64
	pubkeys map[uint64][48]byte
	members map[uint64]bool
	root    types.Hash
}

func newGossipTestContext(t *testing.T) (*GossipContext, []*big.Int, types.Hash) {
	t.Helper()
	state := &gossipTestContext{
		slot:    100,
		pubkeys: make(map[uint64][48]byte),
		members: make(map[uint64]bool),
	}
	secrets := make([]*big.Int, IL_COMMITTEE_SIZE)
	for i := 0; i < IL_COMMITTEE_SIZE; i++ {
		secrets[i] = big.NewInt(int64(9000 + i))
		state.pubkeys[uint64(i)] = crypto.BLSPubkeyFromSecret(secrets[i])
		state.members[uint64(i)] = true
	}
	state.root = crypto.Keccak256Hash([]byte("committee-root"))
	return &GossipContext{
		CurrentSlot:           func() uint64 { return state.slot },
		CommitteeRootForSlot:  func(uint64) (types.Hash, error) { return state.root, nil },
		IsILCommitteeMember:   func(idx, _ uint64) bool { return state.members[idx] },
		ValidatorPubkey:       func(idx uint64) ([48]byte, error) { return state.pubkeys[idx], nil },
		ForkVersion:           func() [4]byte { return [4]byte{} },
		GenesisValidatorsRoot: func() [32]byte { return [32]byte{} },
	}, secrets, state.root
}

// gossipSignValid signs an IL with the member's key over the same signing
// root derivation the gossip validator verifies.
func gossipSignValid(t *testing.T, secret int64, il *InclusionList) *SignedInclusionList {
	t.Helper()
	domain := consensus.DomainSeparation(DomainILCommittee, [4]byte{}, [32]byte{})
	signingRoot := consensus.ComputeSigningRoot(il.HashTreeRoot(), domain)
	sig := crypto.BLSSign(big.NewInt(secret), signingRoot[:])
	return &SignedInclusionList{Message: *il, Signature: sig}
}

// gossipTestTx builds a signed RLP-encoded legacy transfer for IL entries.
func gossipTestTx(t *testing.T, nonce uint64) []byte {
	t.Helper()
	to := types.BytesToAddress([]byte{0x42})
	tx := types.NewTransaction(&types.LegacyTx{
		Nonce:    nonce,
		GasPrice: big.NewInt(1_000_000_000),
		Gas:      21_000,
		To:       &to,
		Value:    big.NewInt(1),
	})
	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("encode tx: %v", err)
	}
	return enc
}

func TestGossipValidatorAcceptsValidIL(t *testing.T) {
	ctx, _, root := newGossipTestContext(t)
	store := NewILStore()
	v := NewGossipValidator(store, ctx)

	il := &InclusionList{
		Slot:          100,
		ProposerIndex: 3,
		CommitteeRoot: root,
		Entries:       []InclusionListEntry{{Transaction: gossipTestTx(t, 0), Index: 0}},
	}
	signed := gossipSignValid(t, 9003, il)

	accepted, err := v.Validate(signed)
	if err != nil {
		t.Fatalf("valid IL rejected: %v", err)
	}
	if accepted != &signed.Message {
		t.Fatal("accepted IL mismatch")
	}
	if store.GetIL(3, 100) != &signed.Message {
		t.Fatal("IL not stored for the committee member")
	}
	// Identical rebroadcast dedups without error.
	if _, err := v.Validate(signed); err != nil {
		t.Fatalf("rebroadcast rejected: %v", err)
	}
}

func TestGossipValidatorRule1SlotWindow(t *testing.T) {
	ctx, _, root := newGossipTestContext(t)
	v := NewGossipValidator(NewILStore(), ctx)

	_ = root
	il := &InclusionList{Slot: 98, ProposerIndex: 0, CommitteeRoot: root,
		Entries: []InclusionListEntry{{Transaction: gossipTestTx(t, 0)}}}
	signed := gossipSignValid(t, 9000, il)
	if _, err := v.Validate(signed); err == nil || !strings.Contains(err.Error(), "current or previous") {
		t.Fatalf("IL older than the previous slot must be rejected, got %v", err)
	}

	// Previous slot is accepted.
	il.Slot = 99
	signed = gossipSignValid(t, 9000, il)
	if _, err := v.Validate(signed); err != nil {
		t.Fatalf("previous-slot IL must be accepted: %v", err)
	}
}

func TestGossipValidatorRule2CommitteeRoot(t *testing.T) {
	ctx, _, _ := newGossipTestContext(t)
	v := NewGossipValidator(NewILStore(), ctx)

	il := &InclusionList{Slot: 100, ProposerIndex: 0, CommitteeRoot: types.Hash{0x99},
		Entries: []InclusionListEntry{{Transaction: gossipTestTx(t, 0)}}}
	signed := gossipSignValid(t, 9000, il)
	_, err := v.Validate(signed)
	if err == nil || !strings.Contains(err.Error(), "committee root mismatch") {
		t.Fatalf("wrong committee root must be rejected, got %v", err)
	}
}

func TestGossipValidatorRule4Signature(t *testing.T) {
	ctx, _, root := newGossipTestContext(t)
	v := NewGossipValidator(NewILStore(), ctx)

	il := &InclusionList{Slot: 100, ProposerIndex: 5, CommitteeRoot: root,
		Entries: []InclusionListEntry{{Transaction: gossipTestTx(t, 0)}}}
	signed := gossipSignValid(t, 9005, il)
	signed.Signature[0] ^= 0xFF // tamper
	if _, err := v.Validate(signed); err == nil || !strings.Contains(err.Error(), "invalid IL signature") {
		t.Fatalf("tampered signature must be rejected, got %v", err)
	}

	// A signature from a different validator is rejected.
	wrong := gossipSignValid(t, 9006, il)
	if _, err := v.Validate(wrong); err == nil || !strings.Contains(err.Error(), "invalid IL signature") {
		t.Fatalf("wrong signer must be rejected, got %v", err)
	}
}

func TestGossipValidatorRule5Membership(t *testing.T) {
	ctx, _, root := newGossipTestContext(t)
	v := NewGossipValidator(NewILStore(), ctx)

	il := &InclusionList{Slot: 100, ProposerIndex: 77, CommitteeRoot: root,
		Entries: []InclusionListEntry{{Transaction: gossipTestTx(t, 0)}}}
	signed := gossipSignValid(t, 9000, il)
	if _, err := v.Validate(signed); err == nil || !strings.Contains(err.Error(), "not an IL committee member") {
		t.Fatalf("non-member IL must be rejected, got %v", err)
	}
}

func TestGossipValidatorRule6Size(t *testing.T) {
	ctx, _, root := newGossipTestContext(t)
	v := NewGossipValidator(NewILStore(), ctx)

	// Entry-count overflow.
	var entries []InclusionListEntry
	for i := 0; i < MAX_TRANSACTIONS_PER_INCLUSION_LIST+1; i++ {
		entries = append(entries, InclusionListEntry{Transaction: gossipTestTx(t, uint64(i)), Index: uint64(i)})
	}
	il := &InclusionList{Slot: 100, ProposerIndex: 0, CommitteeRoot: root, Entries: entries}
	signed := gossipSignValid(t, 9000, il)
	if _, err := v.Validate(signed); err == nil || !strings.Contains(err.Error(), "MAX_TRANSACTIONS_PER_INCLUSION_LIST") {
		t.Fatalf("entry-count overflow must be rejected, got %v", err)
	}

	// Byte-size overflow within the entry cap.
	blob := make([]byte, MAX_BYTES_PER_INCLUSION_LIST/IL_COMMITTEE_SIZE+40)
	big := &InclusionList{Slot: 100, ProposerIndex: 0, CommitteeRoot: root}
	for i := 0; i < IL_COMMITTEE_SIZE; i++ {
		big.Entries = append(big.Entries, InclusionListEntry{Transaction: blob, Index: uint64(i)})
	}
	signed = gossipSignValid(t, 9000, big)
	if _, err := v.Validate(signed); err == nil || !strings.Contains(err.Error(), "MAX_BYTES_PER_INCLUSION_LIST") {
		t.Fatalf("byte-size overflow must be rejected, got %v", err)
	}
}

func TestGossipValidatorRule3EquivocationEvidence(t *testing.T) {
	ctx, _, root := newGossipTestContext(t)
	store := NewILStore()
	v := NewGossipValidator(store, ctx)

	ilA := &InclusionList{Slot: 100, ProposerIndex: 2, CommitteeRoot: root,
		Entries: []InclusionListEntry{{Transaction: gossipTestTx(t, 0), Index: 0}}}
	ilB := &InclusionList{Slot: 100, ProposerIndex: 2, CommitteeRoot: root,
		Entries: []InclusionListEntry{{Transaction: gossipTestTx(t, 1), Index: 0}}}

	if _, err := v.Validate(gossipSignValid(t, 9002, ilA)); err != nil {
		t.Fatalf("first IL rejected: %v", err)
	}
	// Second distinct IL: equivocation recorded, error returned.
	if _, err := v.Validate(gossipSignValid(t, 9002, ilB)); err != ErrILEquivocation {
		t.Fatalf("conflicting second IL must report equivocation, got %v", err)
	}
	if !store.IsEquivocator(2, 100) {
		t.Fatal("member not marked as equivocator")
	}
	if store.EquivocatorCount(100) != 1 {
		t.Fatalf("equivocator count = %d, want 1", store.EquivocatorCount(100))
	}
	ev := store.EquivocationEvidence(100)
	if len(ev) != 1 || ev[0].ValidatorIndex != 2 || !ilsEqual(ev[0].First, ilA) || !ilsEqual(ev[0].Second, ilB) {
		t.Fatalf("evidence not recorded correctly: %+v", ev)
	}
	// The trusted IL is withdrawn: attesters ignore equivocators.
	if store.GetIL(2, 100) != nil {
		t.Fatal("equivocator's IL must no longer be served")
	}
	// A third distinct IL is rejected outright.
	ilC := &InclusionList{Slot: 100, ProposerIndex: 2, CommitteeRoot: root,
		Entries: []InclusionListEntry{{Transaction: gossipTestTx(t, 2), Index: 0}}}
	if _, err := v.Validate(gossipSignValid(t, 9002, ilC)); err != ErrILTooManyILs && err != ErrILKnownEquivoker {
		t.Fatalf("third IL must be rejected outright, got %v", err)
	}

	// A different member's IL still passes.
	ilD := &InclusionList{Slot: 100, ProposerIndex: 9, CommitteeRoot: root,
		Entries: []InclusionListEntry{{Transaction: gossipTestTx(t, 3), Index: 0}}}
	if _, err := v.Validate(gossipSignValid(t, 9009, ilD)); err != nil {
		t.Fatalf("other member's IL rejected: %v", err)
	}
}

func TestILStorePrune(t *testing.T) {
	store := NewILStore()
	il := &InclusionList{Slot: 5, ProposerIndex: 1, Entries: []InclusionListEntry{{Transaction: []byte{1}}}}
	store.AddIL(1, 5, il)
	store.AddIL(2, 7, il)
	if store.PruneBefore(7) == 0 {
		t.Fatal("expected pruned entries")
	}
	if store.GetIL(1, 5) != nil {
		t.Fatal("old slot not pruned")
	}
	if store.GetIL(2, 7) == nil {
		t.Fatal("kept slot pruned by mistake")
	}
}

func TestSignedInclusionListBinaryRoundTrip(t *testing.T) {
	il := &InclusionList{Slot: 9, ProposerIndex: 4, CommitteeRoot: types.Hash{0xAB},
		Entries: []InclusionListEntry{{Transaction: []byte{0xDE, 0xAD}, Index: 0}}}
	signed := &SignedInclusionList{Message: *il, Signature: [96]byte{0x11}}

	enc, err := signed.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back SignedInclusionList
	if err := back.UnmarshalBinary(enc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Message.Slot != signed.Message.Slot ||
		back.Message.ProposerIndex != signed.Message.ProposerIndex ||
		back.Message.CommitteeRoot != signed.Message.CommitteeRoot ||
		len(back.Message.Entries) != len(signed.Message.Entries) ||
		string(back.Message.Entries[0].Transaction) != string(signed.Message.Entries[0].Transaction) ||
		back.Signature != signed.Signature {
		t.Fatal("binary round trip mismatch")
	}
}
