package bintrie

import (
	"encoding/binary"
	"errors"

	"github.com/eth2030/eth2030/core/types"
)

type (
	// NodeFlushFn is called for each node during tree collection.
	NodeFlushFn func([]byte, BinaryNode)

	// NodeResolverFn resolves a hashed node reference to its serialized blob.
	NodeResolverFn func([]byte, types.Hash) ([]byte, error)
)

// zero is the zero value for a 32-byte array.
var zero [32]byte

const (
	StemNodeWidth = 256 // number of children per stem node
	StemSize      = 31  // bytes of key path before reaching the leaf index
	NodeTypeBytes = 1   // size of node type prefix in serialization
	HashSize      = 32  // size of a hash in bytes
	BitmapSize    = 32  // size of the bitmap in a stem node
)

const (
	nodeTypeStem       = iota + 1 // stem node, contains a stem and a bitmap of values
	nodeTypeInternal              // internal node, two children
	nodeTypeStemExpiry            // stem node with expiry epoch and expiry bitmap

	ExpiryEpochSize  = 8  // 8 bytes for big-endian uint64 epoch
	ExpiryBitmapSize = 32 // 32 bytes for 256-bit expiry bitmap
)

// BinaryNode is the interface for all binary trie node types.
type BinaryNode interface {
	Get([]byte, NodeResolverFn) ([]byte, error)
	Insert([]byte, []byte, NodeResolverFn, int) (BinaryNode, error)
	Copy() BinaryNode
	Hash() types.Hash
	// HashWith computes the node hash using the specified TrieHasher.
	// This enables pluggable hash functions (BLAKE3, Poseidon2, etc.).
	HashWith(hasher TrieHasher) types.Hash
	GetValuesAtStem([]byte, NodeResolverFn) ([][]byte, error)
	InsertValuesAtStem([]byte, [][]byte, NodeResolverFn, int) (BinaryNode, error)
	CollectNodes([]byte, NodeFlushFn) error
	GetHeight() int
}

// SerializeNode serializes a binary trie node into a byte slice.
// StemNodes with non-zero ExpiryEpoch or ExpiryBitmap use nodeTypeStemExpiry,
// which appends 8 bytes of epoch + 32 bytes of expiry bitmap after the values.
func SerializeNode(node BinaryNode) []byte {
	switch n := (node).(type) {
	case *InternalNode:
		var serialized [NodeTypeBytes + HashSize + HashSize]byte
		serialized[0] = nodeTypeInternal
		copy(serialized[1:33], n.left.Hash().Bytes())
		copy(serialized[33:65], n.right.Hash().Bytes())
		return serialized[:]
	case *StemNode:
		hasExpiry := n.ExpiryEpoch != 0 || n.ExpiryBitmap != [BitmapSize]byte{}
		var serialized [NodeTypeBytes + StemSize + BitmapSize + StemNodeWidth*HashSize + ExpiryEpochSize + ExpiryBitmapSize]byte
		if hasExpiry {
			serialized[0] = nodeTypeStemExpiry
		} else {
			serialized[0] = nodeTypeStem
		}
		copy(serialized[NodeTypeBytes:NodeTypeBytes+StemSize], n.Stem)
		bitmap := serialized[NodeTypeBytes+StemSize : NodeTypeBytes+StemSize+BitmapSize]
		offset := NodeTypeBytes + StemSize + BitmapSize
		for i, v := range n.Values {
			if v != nil {
				bitmap[i/8] |= 1 << (7 - (i % 8))
				copy(serialized[offset:offset+HashSize], v)
				offset += HashSize
			}
		}
		if hasExpiry {
			binary.BigEndian.PutUint64(serialized[offset:offset+ExpiryEpochSize], n.ExpiryEpoch)
			offset += ExpiryEpochSize
			copy(serialized[offset:offset+ExpiryBitmapSize], n.ExpiryBitmap[:])
			offset += ExpiryBitmapSize
		}
		return serialized[:offset]
	default:
		panic("invalid node type")
	}
}

var errInvalidSerializedLength = errors.New("invalid serialized node length")

// DeserializeNode deserializes a binary trie node from a byte slice.
func DeserializeNode(serialized []byte, depth int) (BinaryNode, error) {
	if len(serialized) == 0 {
		return Empty{}, nil
	}

	switch serialized[0] {
	case nodeTypeInternal:
		if len(serialized) != 65 {
			return nil, errInvalidSerializedLength
		}
		return &InternalNode{
			depth: depth,
			left:  HashedNode(types.BytesToHash(serialized[1:33])),
			right: HashedNode(types.BytesToHash(serialized[33:65])),
		}, nil
	case nodeTypeStem, nodeTypeStemExpiry:
		if len(serialized) < 64 {
			return nil, errInvalidSerializedLength
		}
		var values [StemNodeWidth][]byte
		bitmap := serialized[NodeTypeBytes+StemSize : NodeTypeBytes+StemSize+BitmapSize]
		offset := NodeTypeBytes + StemSize + BitmapSize

		for i := range StemNodeWidth {
			if bitmap[i/8]>>(7-(i%8))&1 == 1 {
				if len(serialized) < offset+HashSize {
					return nil, errInvalidSerializedLength
				}
				values[i] = serialized[offset : offset+HashSize]
				offset += HashSize
			}
		}

		node := &StemNode{
			Stem:   serialized[NodeTypeBytes : NodeTypeBytes+StemSize],
			Values: values[:],
			depth:  depth,
		}

		if serialized[0] == nodeTypeStemExpiry {
			if len(serialized) < offset+ExpiryEpochSize+ExpiryBitmapSize {
				return nil, errInvalidSerializedLength
			}
			node.ExpiryEpoch = binary.BigEndian.Uint64(serialized[offset : offset+ExpiryEpochSize])
			offset += ExpiryEpochSize
			copy(node.ExpiryBitmap[:], serialized[offset:offset+ExpiryBitmapSize])
		}

		return node, nil
	default:
		return nil, errors.New("invalid node type")
	}
}
