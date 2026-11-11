package consensus

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

func TestFinalityBLSAdapterVoteDigest(t *testing.T) {
	adapter := NewFinalityBLSAdapter()
	vote := &SSFRoundVote{
		Slot:      100,
		BlockRoot: types.BytesToHash([]byte("block-root-1")),
	}
	digest := adapter.VoteDigest(vote)
	if len(digest) != 44 {
		t.Fatalf("expected digest length 44, got %d", len(digest))
	}
	// Domain bytes should be at the start.
	if digest[0] != 0x00 || digest[1] != 0x00 || digest[2] != 0x00 || digest[3] != 0x0E {
		t.Errorf("unexpected domain bytes: %x", digest[:4])
	}

	// Nil vote produces nil digest.
	if d := adapter.VoteDigest(nil); d != nil {
		t.Errorf("expected nil digest for nil vote, got %v", d)
	}
}

func TestFinalityBLSAdapterVoteDigestDeterministic(t *testing.T) {
	adapter := NewFinalityBLSAdapter()
	vote := &SSFRoundVote{
		Slot:      42,
		BlockRoot: types.BytesToHash([]byte("root")),
	}
	d1 := adapter.VoteDigest(vote)
	d2 := adapter.VoteDigest(vote)
	for i := range d1 {
		if d1[i] != d2[i] {
			t.Fatalf("digest not deterministic at byte %d", i)
		}
	}
}

func TestFinalityBLSAdapterSignAndVerifyVote(t *testing.T) {
	adapter := NewFinalityBLSAdapter()
	secret := big.NewInt(12345)
	pk := crypto.BLSPubkeyFromSecret(secret)

	vote := &SSFRoundVote{
		Slot:      200,
		BlockRoot: types.BytesToHash([]byte("test-block-root")),
		Stake:     32 * GweiPerETH,
	}

	sig := adapter.SignVote(secret, vote)

	// Verify with correct pubkey.
	if !adapter.VerifyVote(pk, vote, sig) {
		t.Fatal("valid signature should verify")
	}

	// Verify with wrong pubkey should fail.
	wrongPK := crypto.BLSPubkeyFromSecret(big.NewInt(99999))
	if adapter.VerifyVote(wrongPK, vote, sig) {
		t.Fatal("wrong pubkey should not verify")
	}

	// Modified vote should fail.
	modifiedVote := *vote
	modifiedVote.Slot = 201
	if adapter.VerifyVote(pk, &modifiedVote, sig) {
		t.Fatal("modified vote should not verify")
	}
}

func TestFinalityBLSAdapterNilInputs(t *testing.T) {
	adapter := NewFinalityBLSAdapter()

	// SignVote with nil.
	sig := adapter.SignVote(nil, nil)
	if sig != [96]byte{} {
		t.Error("expected zero sig for nil inputs")
	}

	// VerifyVote with nil.
	if adapter.VerifyVote([48]byte{}, nil, [96]byte{}) {
		t.Error("expected false for nil vote verify")
	}
}

func TestFinalityBLSAdapterAggregateVotes(t *testing.T) {
	adapter := NewFinalityBLSAdapter()

	secrets := []*big.Int{
		big.NewInt(111),
		big.NewInt(222),
		big.NewInt(333),
	}

	blockRoot := types.BytesToHash([]byte("shared-block-root"))

	var votes []SSFRoundVote
	var pubkeys [][48]byte

	for i, sk := range secrets {
		pk := crypto.BLSPubkeyFromSecret(sk)
		pubkeys = append(pubkeys, pk)

		vote := SSFRoundVote{
			Slot:      500,
			BlockRoot: blockRoot,
			Stake:     32 * GweiPerETH,
		}
		vote.Signature = adapter.SignVote(sk, &vote)
		vote.ValidatorPubkeyHash = types.BytesToHash(pk[:20])
		_ = i
		votes = append(votes, vote)
	}

	// Aggregate.
	aggSig, bitfield, err := adapter.AggregateVoteSignatures(votes)
	if err != nil {
		t.Fatalf("aggregation failed: %v", err)
	}
	if len(bitfield) == 0 {
		t.Fatal("empty bitfield")
	}
	if aggSig == [96]byte{} {
		t.Fatal("zero aggregate signature")
	}

	// Verify aggregate (all same message = fast aggregate path).
	if !adapter.VerifyAggregateVotes(pubkeys, votes, aggSig) {
		t.Fatal("aggregate verification should succeed")
	}
}

func TestFinalityBLSAdapterAggregateEmptyVotes(t *testing.T) {
	adapter := NewFinalityBLSAdapter()
	_, _, err := adapter.AggregateVoteSignatures(nil)
	if err != ErrBLSAdapterNoVotes {
		t.Fatalf("expected ErrBLSAdapterNoVotes, got %v", err)
	}
}

func TestFinalityBLSAdapterVerifyAggregateMismatch(t *testing.T) {
	adapter := NewFinalityBLSAdapter()

	// Mismatched lengths.
	if adapter.VerifyAggregateVotes(nil, nil, [96]byte{}) {
		t.Error("empty pubkeys should fail")
	}

	pks := [][48]byte{{1}}
	votes := []SSFRoundVote{{Slot: 1}, {Slot: 2}}
	if adapter.VerifyAggregateVotes(pks, votes, [96]byte{}) {
		t.Error("mismatched lengths should fail")
	}
}

func TestFinalityBLSAdapterGenerateFinalityProof(t *testing.T) {
	adapter := NewFinalityBLSAdapter()
	secret := big.NewInt(42)
	pk := crypto.BLSPubkeyFromSecret(secret)

	blockRoot := types.BytesToHash([]byte("finalized-root"))
	stateRoot := types.BytesToHash([]byte("state-root"))

	vote := SSFRoundVote{
		ValidatorPubkeyHash: types.BytesToHash(pk[:20]),
		Slot:                1000,
		BlockRoot:           blockRoot,
		Stake:               32 * GweiPerETH,
	}
	vote.Signature = adapter.SignVote(secret, &vote)

	round := &SSFRound{
		Slot:      1000,
		BlockRoot: blockRoot,
		Finalized: true,
		Votes:     map[types.Hash]*SSFRoundVote{vote.ValidatorPubkeyHash: &vote},
	}

	proof, err := adapter.GenerateFinalityProof(round, 100, stateRoot)
	if err != nil {
		t.Fatalf("proof generation failed: %v", err)
	}
	if proof.Epoch != 100 {
		t.Errorf("expected epoch 100, got %d", proof.Epoch)
	}
	if proof.Slot != 1000 {
		t.Errorf("expected slot 1000, got %d", proof.Slot)
	}
	if proof.BlockRoot != blockRoot {
		t.Error("block root mismatch")
	}
	if proof.StateRoot != stateRoot {
		t.Error("state root mismatch")
	}
	if proof.ParticipantCount != 1 {
		t.Errorf("expected 1 participant, got %d", proof.ParticipantCount)
	}
}

func TestFinalityBLSAdapterGenerateProofErrors(t *testing.T) {
	adapter := NewFinalityBLSAdapter()

	// Nil round.
	_, err := adapter.GenerateFinalityProof(nil, 0, types.Hash{})
	if err != ErrBLSAdapterNilRound {
		t.Fatalf("expected ErrBLSAdapterNilRound, got %v", err)
	}

	// Non-finalized round.
	round := &SSFRound{Finalized: false}
	_, err = adapter.GenerateFinalityProof(round, 0, types.Hash{})
	if err != ErrBLSAdapterRoundNotFinalized {
		t.Fatalf("expected ErrBLSAdapterRoundNotFinalized, got %v", err)
	}
}

func TestFinalityBLSAdapterVerifyFinalityProof(t *testing.T) {
	adapter := NewFinalityBLSAdapter()

	secrets := []*big.Int{big.NewInt(10), big.NewInt(20)}
	var pubkeys [][48]byte
	var votes []SSFRoundVote

	blockRoot := types.BytesToHash([]byte("proof-root"))

	voteMap := make(map[types.Hash]*SSFRoundVote)

	for _, sk := range secrets {
		pk := crypto.BLSPubkeyFromSecret(sk)
		pubkeys = append(pubkeys, pk)

		vote := SSFRoundVote{
			ValidatorPubkeyHash: types.BytesToHash(pk[:20]),
			Slot:                2000,
			BlockRoot:           blockRoot,
			Stake:               32 * GweiPerETH,
		}
		vote.Signature = adapter.SignVote(sk, &vote)
		votes = append(votes, vote)
		voteMap[vote.ValidatorPubkeyHash] = &votes[len(votes)-1]
	}

	round := &SSFRound{
		Slot:      2000,
		BlockRoot: blockRoot,
		Finalized: true,
		Votes:     voteMap,
	}

	proof, err := adapter.GenerateFinalityProof(round, 50, types.Hash{})
	if err != nil {
		t.Fatalf("proof generation failed: %v", err)
	}

	if !adapter.VerifyFinalityProof(proof, pubkeys) {
		t.Fatal("finality proof verification should succeed")
	}

	// Empty validator set.
	if adapter.VerifyFinalityProof(proof, nil) {
		t.Error("nil validator set should fail")
	}

	// Nil proof.
	if adapter.VerifyFinalityProof(nil, pubkeys) {
		t.Error("nil proof should fail")
	}
}

func TestFinalityBLSAdapterSignAndAttachVote(t *testing.T) {
	adapter := NewFinalityBLSAdapter()
	secret := big.NewInt(777)
	pk := crypto.BLSPubkeyFromSecret(secret)

	vote := SSFRoundVote{
		Slot:      300,
		BlockRoot: types.BytesToHash([]byte("attach-root")),
		Stake:     1,
	}

	signed := adapter.SignAndAttachVote(secret, vote)
	if signed.Signature == [96]byte{} {
		t.Fatal("signature should not be zero")
	}
	if !adapter.VerifyVote(pk, &signed, signed.Signature) {
		t.Fatal("attached signature should verify")
	}
}

func TestFinalityBLSAdapterBitfieldCount(t *testing.T) {
	bf := []byte{0xFF, 0x0F} // 8 + 4 = 12 bits.
	count := bitfieldCount(bf)
	if count != 12 {
		t.Errorf("expected 12, got %d", count)
	}

	if bitfieldCount(nil) != 0 {
		t.Error("nil bitfield should return 0")
	}
}

func TestFinalityBLSAdapterProofMeetsThreshold(t *testing.T) {
	proof := &FinalityProof{TotalStake: 200}

	// 200/300 >= 2/3 -> 200*3 >= 300*2 -> 600 >= 600 -> true.
	if !ProofMeetsThreshold(proof, 300, 2, 3) {
		t.Error("should meet 2/3 threshold")
	}

	// 200/301 >= 2/3 -> 600 >= 602 -> false.
	if ProofMeetsThreshold(proof, 301, 2, 3) {
		t.Error("should not meet 2/3 threshold")
	}

	// Nil proof.
	if ProofMeetsThreshold(nil, 100, 2, 3) {
		t.Error("nil proof should not meet threshold")
	}
}

func TestFinalityBLSAdapterGenerateVoteKeyPair(t *testing.T) {
	pk, sk := GenerateVoteKeyPair(big.NewInt(55555))
	if pk == [48]byte{} {
		t.Fatal("pubkey should not be zero")
	}
	if sk == nil {
		t.Fatal("secret should not be nil")
	}
}

func TestFinalityBLSAdapterComputeAggregatePubkey(t *testing.T) {
	pks := [][48]byte{
		crypto.BLSPubkeyFromSecret(big.NewInt(1)),
		crypto.BLSPubkeyFromSecret(big.NewInt(2)),
	}
	aggPK := ComputeAggregatePublicKey(pks)
	if aggPK == [48]byte{} {
		t.Fatal("aggregate pubkey should not be zero")
	}
}
