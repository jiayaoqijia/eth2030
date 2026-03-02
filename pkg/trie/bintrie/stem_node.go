package bintrie

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"github.com/eth2030/eth2030/core/types"
)

// StemNode represents a group of StemNodeWidth values sharing the same stem.
type StemNode struct {
	Stem         []byte       // stem path to reach this group of values
	Values       [][]byte     // all values, indexed by the last byte of the key
	depth        int          // depth of the node in the trie
	ExpiryEpoch  uint64       // epoch when this stem was last touched (blockNumber / 8192)
	ExpiryBitmap [BitmapSize]byte // 256-bit bitmap: bit i set means value[i] is expired
}

// Get retrieves the value for the given key. Returns nil for expired values.
func (bt *StemNode) Get(key []byte, _ NodeResolverFn) ([]byte, error) {
	if !bytes.Equal(bt.Stem, key[:StemSize]) {
		return nil, nil
	}
	idx := key[StemSize]
	if bt.IsValueExpired(idx) {
		return nil, nil
	}
	return bt.Values[idx], nil
}

// Insert inserts a new key-value pair into the node.
func (bt *StemNode) Insert(key []byte, value []byte, _ NodeResolverFn, depth int) (BinaryNode, error) {
	if !bytes.Equal(bt.Stem, key[:StemSize]) {
		bitStem := bt.Stem[bt.depth/8] >> (7 - (bt.depth % 8)) & 1

		n := &InternalNode{depth: bt.depth}
		bt.depth++
		var child, other *BinaryNode
		if bitStem == 0 {
			n.left = bt
			child = &n.left
			other = &n.right
		} else {
			n.right = bt
			child = &n.right
			other = &n.left
		}

		bitKey := key[n.depth/8] >> (7 - (n.depth % 8)) & 1
		if bitKey == bitStem {
			var err error
			*child, err = (*child).Insert(key, value, nil, depth+1)
			if err != nil {
				return n, fmt.Errorf("insert error: %w", err)
			}
			*other = Empty{}
		} else {
			var values [StemNodeWidth][]byte
			values[key[StemSize]] = value
			*other = &StemNode{
				Stem:   slices.Clone(key[:StemSize]),
				Values: values[:],
				depth:  depth + 1,
			}
		}
		return n, nil
	}
	if len(value) != HashSize {
		return bt, errors.New("invalid insertion: value length")
	}
	idx := key[StemSize]
	bt.Values[idx] = value
	bt.UnexpireValue(idx)
	return bt, nil
}

// Copy creates a deep copy of the node.
func (bt *StemNode) Copy() BinaryNode {
	var values [StemNodeWidth][]byte
	for i, v := range bt.Values {
		values[i] = slices.Clone(v)
	}
	return &StemNode{
		Stem:         slices.Clone(bt.Stem),
		Values:       values[:],
		depth:        bt.depth,
		ExpiryEpoch:  bt.ExpiryEpoch,
		ExpiryBitmap: bt.ExpiryBitmap,
	}
}

// GetHeight returns the height of the node.
func (bt *StemNode) GetHeight() int {
	return 1
}

// Hash returns the hash of the node. Values are hashed leaf-by-leaf
// then combined in a binary Merkle tree, then mixed with the stem.
// When ExpiryEpoch > 0 or ExpiryBitmap is non-zero:
//
//	SHA256(stem || epoch_be8 || expiryBitmap || 0x00 || subtree_root)
//
// When ExpiryEpoch == 0 and ExpiryBitmap is all zeros (backward compat):
//
//	SHA256(stem || 0x00 || subtree_root)
func (bt *StemNode) Hash() types.Hash {
	var data [StemNodeWidth]types.Hash
	for i, v := range bt.Values {
		if v != nil {
			h := sha256.Sum256(v)
			data[i] = types.BytesToHash(h[:])
		}
	}

	h := sha256.New()
	for level := 1; level <= 8; level++ {
		for i := range StemNodeWidth / (1 << level) {
			h.Reset()

			if data[i*2] == (types.Hash{}) && data[i*2+1] == (types.Hash{}) {
				data[i] = types.Hash{}
				continue
			}

			h.Write(data[i*2][:])
			h.Write(data[i*2+1][:])
			data[i] = types.BytesToHash(h.Sum(nil))
		}
	}

	h.Reset()
	h.Write(bt.Stem)
	if bt.ExpiryEpoch != 0 || bt.ExpiryBitmap != [BitmapSize]byte{} {
		var epochBE [8]byte
		binary.BigEndian.PutUint64(epochBE[:], bt.ExpiryEpoch)
		h.Write(epochBE[:])
		h.Write(bt.ExpiryBitmap[:])
	}
	h.Write([]byte{0})
	h.Write(data[0][:])
	return types.BytesToHash(h.Sum(nil))
}

// HashWith computes the StemNode hash using the specified TrieHasher,
// mirroring the logic of Hash() but with a pluggable hash function.
func (bt *StemNode) HashWith(hasher TrieHasher) types.Hash {
	var data [StemNodeWidth][32]byte
	for i, v := range bt.Values {
		if v != nil {
			data[i] = hasher.Hash(v)
		}
	}

	var zeroArr [32]byte
	for level := 1; level <= 8; level++ {
		for i := range StemNodeWidth / (1 << level) {
			if data[i*2] == zeroArr && data[i*2+1] == zeroArr {
				data[i] = zeroArr
				continue
			}
			data[i] = hasher.HashPair(data[i*2], data[i*2+1])
		}
	}

	// Build final: stem || [epoch || expiryBitmap] || 0x00 || subtree_root
	var buf []byte
	buf = append(buf, bt.Stem...)
	if bt.ExpiryEpoch != 0 || bt.ExpiryBitmap != [BitmapSize]byte{} {
		var epochBE [8]byte
		binary.BigEndian.PutUint64(epochBE[:], bt.ExpiryEpoch)
		buf = append(buf, epochBE[:]...)
		buf = append(buf, bt.ExpiryBitmap[:]...)
	}
	buf = append(buf, 0x00)
	buf = append(buf, data[0][:]...)
	result := hasher.Hash(buf)
	return types.BytesToHash(result[:])
}

// CollectNodes flushes this stem node to the collector.
func (bt *StemNode) CollectNodes(path []byte, flush NodeFlushFn) error {
	flush(path, bt)
	return nil
}

// GetValuesAtStem retrieves the group of values at the given stem key.
func (bt *StemNode) GetValuesAtStem(stem []byte, _ NodeResolverFn) ([][]byte, error) {
	if !bytes.Equal(bt.Stem, stem) {
		return nil, nil
	}
	return bt.Values[:], nil
}

// InsertValuesAtStem inserts a full value group at the given stem in the stem node.
func (bt *StemNode) InsertValuesAtStem(key []byte, values [][]byte, _ NodeResolverFn, depth int) (BinaryNode, error) {
	if !bytes.Equal(bt.Stem, key[:StemSize]) {
		bitStem := bt.Stem[bt.depth/8] >> (7 - (bt.depth % 8)) & 1

		n := &InternalNode{depth: bt.depth}
		bt.depth++
		var child, other *BinaryNode
		if bitStem == 0 {
			n.left = bt
			child = &n.left
			other = &n.right
		} else {
			n.right = bt
			child = &n.right
			other = &n.left
		}

		bitKey := key[n.depth/8] >> (7 - (n.depth % 8)) & 1
		if bitKey == bitStem {
			var err error
			*child, err = (*child).InsertValuesAtStem(key, values, nil, depth+1)
			if err != nil {
				return n, fmt.Errorf("insert error: %w", err)
			}
			*other = Empty{}
		} else {
			*other = &StemNode{
				Stem:   slices.Clone(key[:StemSize]),
				Values: values,
				depth:  n.depth + 1,
			}
		}
		return n, nil
	}

	// same stem, merge the two value lists
	for i, v := range values {
		if v != nil {
			bt.Values[i] = v
		}
	}
	return bt, nil
}

// Key returns the full key for the given index.
func (bt *StemNode) Key(i int) []byte {
	var ret [HashSize]byte
	copy(ret[:], bt.Stem)
	ret[StemSize] = byte(i)
	return ret[:]
}

// IsValueExpired returns true if the value at the given index is expired.
func (bt *StemNode) IsValueExpired(index byte) bool {
	return bt.ExpiryBitmap[index/8]>>(7-(index%8))&1 == 1
}

// ExpireValue marks the value at the given index as expired.
func (bt *StemNode) ExpireValue(index byte) {
	bt.ExpiryBitmap[index/8] |= 1 << (7 - (index % 8))
}

// UnexpireValue clears the expiry bit for the given index.
func (bt *StemNode) UnexpireValue(index byte) {
	bt.ExpiryBitmap[index/8] &^= 1 << (7 - (index % 8))
}

// ExpiredCount returns the number of expired values in this stem node.
func (bt *StemNode) ExpiredCount() int {
	count := 0
	for _, b := range bt.ExpiryBitmap {
		for b != 0 {
			count += int(b & 1)
			b >>= 1
		}
	}
	return count
}
