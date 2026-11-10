package stark

import (
	"crypto/sha256"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestMergeTick_PreservesLocalValidation(t *testing.T) {
	local := NewSTARKAggregator("node-local")
	remote := NewSTARKAggregator("node-remote")

	// Add a local tx.
	var localHash types.Hash
	localHash[0] = 0xAA
	local.AddValidatedTx(localHash, []byte("local-proof"), 21000)

	// Add a remote tx.
	var remoteHash types.Hash
	remoteHash[0] = 0xBB
	remote.AddValidatedTx(remoteHash, []byte("remote-proof"), 42000)

	// Generate tick from remote.
	remoteTick, err := remote.GenerateTick()
	if err != nil {
		t.Fatalf("remote GenerateTick failed: %v", err)
	}

	// Merge remote tick into local.
	if err := local.MergeTick(remoteTick); err != nil {
		t.Fatalf("MergeTick failed: %v", err)
	}

	// Remote hints cannot change the local validated set.
	local.mu.RLock()
	_, hasLocal := local.validTxs[localHash]
	_, hasRemote := local.validTxs[remoteHash]
	local.mu.RUnlock()

	if !hasLocal {
		t.Error("local tx hash missing after merge")
	}
	if hasRemote {
		t.Error("remote tx was incorrectly treated as validated")
	}
	if local.PendingCount() != 1 {
		t.Errorf("expected 1 pending tx, got %d", local.PendingCount())
	}
}

func TestMergeTick_ProofWireRoundTrip(t *testing.T) {
	local := NewSTARKAggregator("local")
	remote := NewSTARKAggregator("remote")
	hash := types.Hash{1}
	remote.AddValidatedTx(hash, []byte("proof"), 21000)
	tick, err := remote.GenerateTick()
	if err != nil {
		t.Fatal(err)
	}
	wire, err := tick.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	var decoded MempoolAggregationTick
	if err := decoded.UnmarshalBinary(wire); err != nil {
		t.Fatal(err)
	}
	if err := local.MergeTickAtSlot(&decoded, 1); err != nil {
		t.Fatal(err)
	}
	if local.PendingCount() != 0 || local.PeerCache().IsPeerValidated(hash) {
		t.Fatal("remote hint gained validation authority")
	}
	decoded.AggregateProof.QueryResponses = nil
	if err := local.MergeTickAtSlot(&decoded, 2); err == nil {
		t.Fatal("incomplete proof was accepted")
	}
}

func TestMergeTick_SkipsDuplicates(t *testing.T) {
	local := NewSTARKAggregator("node-local")
	remote := NewSTARKAggregator("node-remote")

	// Add the same tx hash to both local and remote.
	var sharedHash types.Hash
	sharedHash[0] = 0xFF
	local.AddValidatedTx(sharedHash, []byte("local-proof"), 21000)
	remote.AddValidatedTx(sharedHash, []byte("remote-proof"), 42000)

	remoteTick, err := remote.GenerateTick()
	if err != nil {
		t.Fatal(err)
	}
	if err := local.MergeTick(remoteTick); err != nil {
		t.Fatal(err)
	}

	local.mu.RLock()
	defer local.mu.RUnlock()

	// The local entry should be preserved (not overwritten by remote).
	vtx := local.validTxs[sharedHash]
	if vtx == nil {
		t.Fatal("shared hash should still exist")
	}
	if vtx.RemoteProven {
		t.Error("local entry should NOT be overwritten with RemoteProven=true")
	}
	if vtx.GasUsed != 21000 {
		t.Errorf("expected local GasUsed 21000, got %d", vtx.GasUsed)
	}
}

func TestGenerateTick_Bitfield(t *testing.T) {
	agg := NewSTARKAggregator("node-1")

	for i := 0; i < 5; i++ {
		var h types.Hash
		h[0] = byte(i + 1)
		agg.AddValidatedTx(h, []byte("proof"), uint64(21000*(i+1)))
	}

	tick, err := agg.GenerateTick()
	if err != nil {
		t.Fatal(err)
	}

	// With 5 txs, bitfield should be 1 byte (ceil(5/8) = 1).
	if len(tick.ValidBitfield) != 1 {
		t.Fatalf("expected bitfield length 1, got %d", len(tick.ValidBitfield))
	}

	// Count the set bits. All 5 should be set.
	setBits := 0
	for _, b := range tick.ValidBitfield {
		for b != 0 {
			setBits++
			b &= b - 1
		}
	}
	if setBits != 5 {
		t.Errorf("expected 5 set bits, got %d", setBits)
	}
}

func TestGenerateTick_MerkleRoot(t *testing.T) {
	agg := NewSTARKAggregator("node-1")

	var h types.Hash
	h[0] = 0x42
	agg.AddValidatedTx(h, []byte("proof"), 21000)

	tick, err := agg.GenerateTick()
	if err != nil {
		t.Fatal(err)
	}

	// Merkle root should be non-zero.
	if tick.TxMerkleRoot == (types.Hash{}) {
		t.Error("TxMerkleRoot should be non-zero")
	}
}

func TestMergeTick_BandwidthLimit(t *testing.T) {
	local := NewSTARKAggregator("node-local")
	remote := NewSTARKAggregator("node-remote")

	// Add enough txs to exceed MaxTickSize when serialized.
	// Each tx hash = 32 bytes in serialized form, so ~4100 txs = ~131KB > 128KB.
	numTxs := 4100
	for i := 0; i < numTxs; i++ {
		var h types.Hash
		h[0] = byte(i & 0xFF)
		h[1] = byte((i >> 8) & 0xFF)
		h[2] = byte((i >> 16) & 0xFF)
		remote.AddValidatedTx(h, []byte("proof"), 21000)
	}

	remoteTick, err := remote.GenerateTick()
	if err != nil {
		t.Fatal(err)
	}

	// Merging an oversized tick should return ErrAggTickTooLarge.
	err = local.MergeTick(remoteTick)
	if err != ErrAggTickTooLarge {
		t.Errorf("expected ErrAggTickTooLarge, got %v", err)
	}
}

func TestComputeTxMerkleRoot_SingleHash(t *testing.T) {
	var h types.Hash
	h[0] = 0xDE
	h[1] = 0xAD

	root := computeTxMerkleRoot([]types.Hash{h})

	// A single hash should return the hash itself.
	if root != h {
		t.Errorf("single-hash root should equal the input hash, got %x", root)
	}
}

func TestComputeTxMerkleRoot_TwoHashes(t *testing.T) {
	var h1, h2 types.Hash
	h1[0] = 0x01
	h2[0] = 0x02

	root := computeTxMerkleRoot([]types.Hash{h1, h2})

	// Expected: sha256(h1 || h2).
	hasher := sha256.New()
	hasher.Write(h1[:])
	hasher.Write(h2[:])
	var expected types.Hash
	copy(expected[:], hasher.Sum(nil))

	if root != expected {
		t.Errorf("two-hash root mismatch:\n  got  %x\n  want %x", root, expected)
	}
}

func TestGenerateTick_MeaningfulConstraints(t *testing.T) {
	agg := NewSTARKAggregator("node-1")

	var h types.Hash
	h[0] = 0x42
	agg.AddValidatedTx(h, []byte("proof"), 21000)

	tick, err := agg.GenerateTick()
	if err != nil {
		t.Fatal(err)
	}

	if tick.AggregateProof == nil {
		t.Fatal("aggregate proof should not be nil")
	}
	if tick.AggregateProof.ConstraintCount != 2 {
		t.Errorf("expected 2 constraints, got %d", tick.AggregateProof.ConstraintCount)
	}
}

func TestMergeTick_ActualSerializedSize(t *testing.T) {
	remote := NewSTARKAggregator("node-remote")

	// Add a few txs and generate a tick.
	for i := 0; i < 10; i++ {
		var h types.Hash
		h[0] = byte(i + 1)
		remote.AddValidatedTx(h, []byte("proof"), uint64(21000*(i+1)))
	}

	tick, err := remote.GenerateTick()
	if err != nil {
		t.Fatal(err)
	}

	// Verify the tick can be serialized.
	serialized, err := tick.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	// The serialized size should be well under 128KB for 10 txs.
	if len(serialized) > MaxTickSize {
		t.Errorf("serialized size %d exceeds MaxTickSize %d", len(serialized), MaxTickSize)
	}
	if len(serialized) == 0 {
		t.Error("serialized tick should not be empty")
	}
}

func TestSTARKTickGossipBandwidth(t *testing.T) {
	agg := NewSTARKAggregator("node-1")

	// Add enough txs for a realistic tick.
	for i := 0; i < 50; i++ {
		var h types.Hash
		h[0] = byte(i + 1)
		h[1] = byte((i >> 8) & 0xFF)
		agg.AddValidatedTx(h, []byte("proof"), uint64(21000*(i+1)))
	}

	tick, err := agg.GenerateTick()
	if err != nil {
		t.Fatal(err)
	}

	// Serialize the tick.
	serialized, err := tick.MarshalBinary()
	if err != nil {
		t.Fatalf("MarshalBinary failed: %v", err)
	}

	// Verify it's under the 128KB limit.
	if len(serialized) > MaxTickSize {
		t.Errorf("serialized tick %d bytes exceeds MaxTickSize %d", len(serialized), MaxTickSize)
	}

	// Verify it has meaningful content.
	if len(serialized) < 50*32 {
		t.Errorf("serialized tick %d bytes seems too small for 50 txs", len(serialized))
	}

	// Verify proof has 2 constraints.
	if tick.AggregateProof.ConstraintCount != 2 {
		t.Errorf("expected 2 constraints, got %d", tick.AggregateProof.ConstraintCount)
	}

	// Verify constraint eval commitment is non-zero.
	var zero [32]byte
	if tick.AggregateProof.ConstraintEvalCommitment == zero {
		t.Error("aggregate proof should have non-zero constraint eval commitment")
	}
}
