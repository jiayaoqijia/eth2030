package bintrie

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- Task 3.1.1: Expiry Epoch Field Tests ---

func TestExpiryEpochZeroHashBackwardCompat(t *testing.T) {
	// A StemNode with ExpiryEpoch=0 and zero ExpiryBitmap must produce
	// the same hash as the original implementation (no expiry fields).
	stem := make([]byte, StemSize)
	stem[0] = 0x42
	var values [StemNodeWidth][]byte
	values[5] = oneKey[:]

	node := &StemNode{
		Stem:   stem,
		Values: values[:],
		depth:  0,
	}

	// Hash with zero expiry (default) should match.
	hashZero := node.Hash()

	// Explicitly set to zero and verify the same.
	node.ExpiryEpoch = 0
	node.ExpiryBitmap = [BitmapSize]byte{}
	hashExplicitZero := node.Hash()

	if hashZero != hashExplicitZero {
		t.Fatalf("epoch=0 hash mismatch: %x vs %x", hashZero, hashExplicitZero)
	}

	// Verify it's not zero.
	if hashZero == (types.Hash{}) {
		t.Fatal("hash should not be zero for non-empty node")
	}
}

func TestExpiryEpochNonZeroChangesHash(t *testing.T) {
	stem := make([]byte, StemSize)
	var values [StemNodeWidth][]byte
	values[0] = oneKey[:]

	node1 := &StemNode{
		Stem:   stem,
		Values: values[:],
		depth:  0,
	}
	hash1 := node1.Hash()

	node2 := &StemNode{
		Stem:        stem,
		Values:      values[:],
		depth:       0,
		ExpiryEpoch: 100,
	}
	hash2 := node2.Hash()

	if hash1 == hash2 {
		t.Fatal("different epochs should produce different hashes")
	}
}

func TestDifferentEpochsDifferentHashes(t *testing.T) {
	stem := make([]byte, StemSize)
	var values [StemNodeWidth][]byte
	values[0] = oneKey[:]

	node1 := &StemNode{
		Stem:        stem,
		Values:      values[:],
		depth:       0,
		ExpiryEpoch: 10,
	}
	node2 := &StemNode{
		Stem:        stem,
		Values:      values[:],
		depth:       0,
		ExpiryEpoch: 20,
	}

	if node1.Hash() == node2.Hash() {
		t.Fatal("same values, different epochs should produce different hashes")
	}
}

func TestExpiryEpochSerializationRoundTrip(t *testing.T) {
	stem := make([]byte, StemSize)
	for i := range stem {
		stem[i] = byte(i)
	}
	var values [StemNodeWidth][]byte
	values[0] = oneKey[:]
	values[100] = twoKey[:]

	node := &StemNode{
		Stem:        stem,
		Values:      values[:],
		depth:       7,
		ExpiryEpoch: 42,
	}

	serialized := SerializeNode(node)
	deserialized, err := DeserializeNode(serialized, 7)
	if err != nil {
		t.Fatalf("DeserializeNode: %v", err)
	}

	sn, ok := deserialized.(*StemNode)
	if !ok {
		t.Fatalf("expected *StemNode, got %T", deserialized)
	}

	if sn.ExpiryEpoch != 42 {
		t.Fatalf("epoch mismatch: got %d, want 42", sn.ExpiryEpoch)
	}
	if !bytes.Equal(sn.Stem, stem) {
		t.Fatal("stem mismatch")
	}
	if !bytes.Equal(sn.Values[0], oneKey[:]) {
		t.Fatal("value[0] mismatch")
	}
	if !bytes.Equal(sn.Values[100], twoKey[:]) {
		t.Fatal("value[100] mismatch")
	}
}

func TestExpiryEpochZeroSerializationUsesLegacyFormat(t *testing.T) {
	stem := make([]byte, StemSize)
	var values [StemNodeWidth][]byte
	values[0] = oneKey[:]

	node := &StemNode{
		Stem:   stem,
		Values: values[:],
		depth:  0,
	}

	serialized := SerializeNode(node)
	if serialized[0] != nodeTypeStem {
		t.Fatalf("epoch=0 should use legacy nodeTypeStem, got %d", serialized[0])
	}
}

func TestExpiryEpochCopy(t *testing.T) {
	stem := make([]byte, StemSize)
	var values [StemNodeWidth][]byte
	values[0] = oneKey[:]

	node := &StemNode{
		Stem:        stem,
		Values:      values[:],
		depth:       0,
		ExpiryEpoch: 999,
	}

	cp := node.Copy().(*StemNode)
	if cp.ExpiryEpoch != 999 {
		t.Fatalf("copy epoch mismatch: got %d, want 999", cp.ExpiryEpoch)
	}

	// Modify copy, original unchanged.
	cp.ExpiryEpoch = 0
	if node.ExpiryEpoch != 999 {
		t.Fatal("copy should be independent")
	}
}

// --- Task 3.1.2: Per-Value Expiry Bitmap Tests ---

func TestExpiryBitmapGetReturnsNil(t *testing.T) {
	stem := make([]byte, StemSize)
	var values [StemNodeWidth][]byte
	values[5] = oneKey[:]

	node := &StemNode{
		Stem:   stem,
		Values: values[:],
		depth:  0,
	}

	// Before expiry, Get returns the value.
	key := make([]byte, HashSize)
	copy(key, stem)
	key[StemSize] = 5

	val, err := node.Get(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(val, oneKey[:]) {
		t.Fatalf("expected value before expiry, got %x", val)
	}

	// Mark value[5] as expired.
	node.ExpireValue(5)

	val, err = node.Get(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if val != nil {
		t.Fatalf("expired value should return nil, got %x", val)
	}
}

func TestExpiryBitmapInsertClearsBit(t *testing.T) {
	stem := make([]byte, StemSize)
	var values [StemNodeWidth][]byte
	values[5] = oneKey[:]

	node := &StemNode{
		Stem:   stem,
		Values: values[:],
		depth:  0,
	}

	node.ExpireValue(5)
	if !node.IsValueExpired(5) {
		t.Fatal("value should be expired")
	}

	// Insert a new value at index 5; should clear the expiry bit.
	key := make([]byte, HashSize)
	copy(key, stem)
	key[StemSize] = 5
	_, err := node.Insert(key, twoKey[:], nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	if node.IsValueExpired(5) {
		t.Fatal("insert should clear expiry bit")
	}
}

func TestExpiryBitmapRoundTrip(t *testing.T) {
	stem := make([]byte, StemSize)
	var values [StemNodeWidth][]byte
	values[0] = oneKey[:]
	values[5] = twoKey[:]
	values[200] = threeKey[:]

	node := &StemNode{
		Stem:        stem,
		Values:      values[:],
		depth:       3,
		ExpiryEpoch: 50,
	}
	node.ExpireValue(5)
	node.ExpireValue(200)

	serialized := SerializeNode(node)
	deserialized, err := DeserializeNode(serialized, 3)
	if err != nil {
		t.Fatalf("DeserializeNode: %v", err)
	}

	sn := deserialized.(*StemNode)
	if !sn.IsValueExpired(5) {
		t.Fatal("value 5 should be expired after round-trip")
	}
	if !sn.IsValueExpired(200) {
		t.Fatal("value 200 should be expired after round-trip")
	}
	if sn.IsValueExpired(0) {
		t.Fatal("value 0 should not be expired")
	}
	if sn.ExpiredCount() != 2 {
		t.Fatalf("expected 2 expired, got %d", sn.ExpiredCount())
	}
}

func TestExpiryBitmapAllBitsSet(t *testing.T) {
	stem := make([]byte, StemSize)
	var values [StemNodeWidth][]byte
	for i := range StemNodeWidth {
		var v [HashSize]byte
		binary.BigEndian.PutUint64(v[24:], uint64(i+1))
		values[i] = v[:]
	}

	node := &StemNode{
		Stem:   stem,
		Values: values[:],
		depth:  0,
	}

	// Expire all values.
	for i := range StemNodeWidth {
		node.ExpireValue(byte(i))
	}

	if node.ExpiredCount() != StemNodeWidth {
		t.Fatalf("expected %d expired, got %d", StemNodeWidth, node.ExpiredCount())
	}

	// All Gets should return nil.
	for i := range StemNodeWidth {
		key := make([]byte, HashSize)
		copy(key, stem)
		key[StemSize] = byte(i)
		val, err := node.Get(key, nil)
		if err != nil {
			t.Fatal(err)
		}
		if val != nil {
			t.Fatalf("value[%d] should be nil when expired, got %x", i, val)
		}
	}
}

func TestDifferentExpiryStatesDifferentHashes(t *testing.T) {
	stem := make([]byte, StemSize)
	var values [StemNodeWidth][]byte
	values[0] = oneKey[:]
	values[1] = twoKey[:]

	node1 := &StemNode{
		Stem:        stem,
		Values:      values[:],
		depth:       0,
		ExpiryEpoch: 10,
	}

	// Make a copy with different expiry bitmap.
	node2 := node1.Copy().(*StemNode)
	node2.ExpireValue(1)

	if node1.Hash() == node2.Hash() {
		t.Fatal("different expiry states should produce different hashes")
	}
}

func TestExpiryBitmapCopy(t *testing.T) {
	stem := make([]byte, StemSize)
	var values [StemNodeWidth][]byte
	values[0] = oneKey[:]

	node := &StemNode{
		Stem:   stem,
		Values: values[:],
		depth:  0,
	}
	node.ExpireValue(7)
	node.ExpireValue(100)

	cp := node.Copy().(*StemNode)
	if !cp.IsValueExpired(7) {
		t.Fatal("copy should preserve expiry bit 7")
	}
	if !cp.IsValueExpired(100) {
		t.Fatal("copy should preserve expiry bit 100")
	}

	// Modify copy, original unchanged.
	cp.UnexpireValue(7)
	if !node.IsValueExpired(7) {
		t.Fatal("original should not be affected by copy modification")
	}
}

func TestIsValueExpiredUnexpireValue(t *testing.T) {
	node := &StemNode{
		Stem:   make([]byte, StemSize),
		Values: make([][]byte, StemNodeWidth),
	}

	if node.IsValueExpired(42) {
		t.Fatal("fresh node should have no expired values")
	}

	node.ExpireValue(42)
	if !node.IsValueExpired(42) {
		t.Fatal("should be expired after ExpireValue")
	}

	node.UnexpireValue(42)
	if node.IsValueExpired(42) {
		t.Fatal("should not be expired after UnexpireValue")
	}
}

func TestExpiredCount(t *testing.T) {
	node := &StemNode{
		Stem:   make([]byte, StemSize),
		Values: make([][]byte, StemNodeWidth),
	}

	if node.ExpiredCount() != 0 {
		t.Fatal("empty bitmap should have 0 expired")
	}

	node.ExpireValue(0)
	node.ExpireValue(128)
	node.ExpireValue(255)
	if node.ExpiredCount() != 3 {
		t.Fatalf("expected 3 expired, got %d", node.ExpiredCount())
	}
}

// --- Task 3.1.3: Revival Proof Verification Tests ---

func TestReviveStemValueBasic(t *testing.T) {
	trie := New()
	key := makeKey(0x00, 5)
	val := makeVal(0xAA)
	if err := trie.Put(key[:], val[:]); err != nil {
		t.Fatal(err)
	}

	// Build proof before expiry.
	proof, err := BuildInclusionProof(trie, key[:])
	if err != nil {
		t.Fatalf("BuildInclusionProof: %v", err)
	}

	// Expire the value.
	stemNode := findStemNode(trie.root, key[:StemSize], 0)
	if stemNode == nil {
		t.Fatal("stem node not found")
	}
	stemNode.ExpireValue(5)
	stemNode.ExpiryEpoch = 10

	// Confirm value is expired.
	got, err := trie.Get(key[:])
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("expired value should return nil from Get")
	}

	// Revive using the proof.
	err = trie.ReviveStemValue(key[:], val[:], proof)
	if err != nil {
		t.Fatalf("ReviveStemValue: %v", err)
	}

	// Value should be restored.
	got, err = trie.Get(key[:])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, val[:]) {
		t.Fatalf("revived value mismatch: got %x, want %x", got, val[:])
	}

	// Expiry bit should be cleared.
	if stemNode.IsValueExpired(5) {
		t.Fatal("expiry bit should be cleared after revival")
	}
}

func TestReviveStemValueInvalidProof(t *testing.T) {
	trie := New()
	key := makeKey(0x00, 5)
	val := makeVal(0xAA)
	if err := trie.Put(key[:], val[:]); err != nil {
		t.Fatal(err)
	}

	// Build proof.
	proof, err := BuildInclusionProof(trie, key[:])
	if err != nil {
		t.Fatal(err)
	}

	// Expire.
	stemNode := findStemNode(trie.root, key[:StemSize], 0)
	stemNode.ExpireValue(5)
	stemNode.ExpiryEpoch = 10

	// Tamper with the proof value.
	proof.Value[0] ^= 0xFF

	err = trie.ReviveStemValue(key[:], val[:], proof)
	if err == nil {
		t.Fatal("should reject tampered proof")
	}
}

func TestReviveStemValueWrongValue(t *testing.T) {
	trie := New()
	key := makeKey(0x00, 5)
	val := makeVal(0xAA)
	if err := trie.Put(key[:], val[:]); err != nil {
		t.Fatal(err)
	}

	proof, err := BuildInclusionProof(trie, key[:])
	if err != nil {
		t.Fatal(err)
	}

	stemNode := findStemNode(trie.root, key[:StemSize], 0)
	stemNode.ExpireValue(5)
	stemNode.ExpiryEpoch = 10

	// Provide wrong value.
	wrongVal := makeVal(0xBB)
	err = trie.ReviveStemValue(key[:], wrongVal[:], proof)
	if err != ErrRevivalValueMismatch {
		t.Fatalf("expected ErrRevivalValueMismatch, got %v", err)
	}
}

func TestReviveStemValueNotExpiredNoop(t *testing.T) {
	trie := New()
	key := makeKey(0x00, 5)
	val := makeVal(0xAA)
	if err := trie.Put(key[:], val[:]); err != nil {
		t.Fatal(err)
	}

	proof, err := BuildInclusionProof(trie, key[:])
	if err != nil {
		t.Fatal(err)
	}

	// Revival of non-expired value = no-op.
	err = trie.ReviveStemValue(key[:], val[:], proof)
	if err != nil {
		t.Fatalf("revival of non-expired value should be no-op, got: %v", err)
	}
}

func TestRevivalGasCost(t *testing.T) {
	cost := RevivalGasCost()
	if cost != 25000 {
		t.Fatalf("expected gas cost 25000, got %d", cost)
	}
}

func TestBlockToEpoch(t *testing.T) {
	tests := []struct {
		block uint64
		epoch uint64
	}{
		{0, 0},
		{1, 0},
		{8191, 0},
		{8192, 1},
		{16384, 2},
		{100000, 12},
	}
	for _, tt := range tests {
		got := BlockToEpoch(tt.block)
		if got != tt.epoch {
			t.Errorf("BlockToEpoch(%d) = %d, want %d", tt.block, got, tt.epoch)
		}
	}
}
