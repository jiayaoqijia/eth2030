package bintrie

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
)

func keyToPath(depth int, key []byte) ([]byte, error) {
	if depth > 31*8 {
		return nil, errors.New("node too deep")
	}
	path := make([]byte, 0, depth+1)
	for i := range depth + 1 {
		bit := key[i/8] >> (7 - (i % 8)) & 1
		path = append(path, bit)
	}
	return path, nil
}

// InternalNode is a binary trie internal node with left and right children.
type InternalNode struct {
	left, right BinaryNode
	depth       int
}

// GetValuesAtStem retrieves the group of values located at the given stem key.
func (bt *InternalNode) GetValuesAtStem(stem []byte, resolver NodeResolverFn) ([][]byte, error) {
	if bt.depth > 31*8 {
		return nil, errors.New("node too deep")
	}

	bit := stem[bt.depth/8] >> (7 - (bt.depth % 8)) & 1
	if bit == 0 {
		if hn, ok := bt.left.(HashedNode); ok {
			path, err := keyToPath(bt.depth, stem)
			if err != nil {
				return nil, fmt.Errorf("GetValuesAtStem resolve error: %w", err)
			}
			data, err := resolver(path, types.Hash(hn))
			if err != nil {
				return nil, fmt.Errorf("GetValuesAtStem resolve error: %w", err)
			}
			node, err := DeserializeNode(data, bt.depth+1)
			if err != nil {
				return nil, fmt.Errorf("GetValuesAtStem node deserialization error: %w", err)
			}
			bt.left = node
		}
		return bt.left.GetValuesAtStem(stem, resolver)
	}

	if hn, ok := bt.right.(HashedNode); ok {
		path, err := keyToPath(bt.depth, stem)
		if err != nil {
			return nil, fmt.Errorf("GetValuesAtStem resolve error: %w", err)
		}
		data, err := resolver(path, types.Hash(hn))
		if err != nil {
			return nil, fmt.Errorf("GetValuesAtStem resolve error: %w", err)
		}
		node, err := DeserializeNode(data, bt.depth+1)
		if err != nil {
			return nil, fmt.Errorf("GetValuesAtStem node deserialization error: %w", err)
		}
		bt.right = node
	}
	return bt.right.GetValuesAtStem(stem, resolver)
}

// Get retrieves the value for the given key.
func (bt *InternalNode) Get(key []byte, resolver NodeResolverFn) ([]byte, error) {
	values, err := bt.GetValuesAtStem(key[:31], resolver)
	if err != nil {
		return nil, fmt.Errorf("get error: %w", err)
	}
	if values == nil {
		return nil, nil
	}
	return values[key[31]], nil
}

// Insert inserts a new key-value pair into the trie.
func (bt *InternalNode) Insert(key []byte, value []byte, resolver NodeResolverFn, depth int) (BinaryNode, error) {
	var values [256][]byte
	values[key[31]] = value
	return bt.InsertValuesAtStem(key[:31], values[:], resolver, depth)
}

// Copy creates a deep copy of the node.
func (bt *InternalNode) Copy() BinaryNode {
	return &InternalNode{
		left:  bt.left.Copy(),
		right: bt.right.Copy(),
		depth: bt.depth,
	}
}

// Hash returns the SHA-256 hash of the node (H(left || right)).
func (bt *InternalNode) Hash() types.Hash {
	h := sha256.New()
	if bt.left != nil {
		h.Write(bt.left.Hash().Bytes())
	} else {
		h.Write(zero[:])
	}
	if bt.right != nil {
		h.Write(bt.right.Hash().Bytes())
	} else {
		h.Write(zero[:])
	}
	return types.BytesToHash(h.Sum(nil))
}

// HashWith computes the internal node hash using the specified TrieHasher.
func (bt *InternalNode) HashWith(hasher TrieHasher) types.Hash {
	var left, right [32]byte
	if bt.left != nil {
		lh := bt.left.HashWith(hasher)
		copy(left[:], lh[:])
	}
	if bt.right != nil {
		rh := bt.right.HashWith(hasher)
		copy(right[:], rh[:])
	}
	result := hasher.HashPair(left, right)
	return types.BytesToHash(result[:])
}

// InsertValuesAtStem inserts a full value group at the given stem in the internal node.
func (bt *InternalNode) InsertValuesAtStem(stem []byte, values [][]byte, resolver NodeResolverFn, depth int) (BinaryNode, error) {
	var err error
	bit := stem[bt.depth/8] >> (7 - (bt.depth % 8)) & 1
	if bit == 0 {
		if bt.left == nil {
			bt.left = Empty{}
		}
		if hn, ok := bt.left.(HashedNode); ok {
			path, err := keyToPath(bt.depth, stem)
			if err != nil {
				return nil, fmt.Errorf("InsertValuesAtStem resolve error: %w", err)
			}
			data, err := resolver(path, types.Hash(hn))
			if err != nil {
				return nil, fmt.Errorf("InsertValuesAtStem resolve error: %w", err)
			}
			node, err := DeserializeNode(data, bt.depth+1)
			if err != nil {
				return nil, fmt.Errorf("InsertValuesAtStem node deserialization error: %w", err)
			}
			bt.left = node
		}
		bt.left, err = bt.left.InsertValuesAtStem(stem, values, resolver, depth+1)
		return bt, err
	}

	if bt.right == nil {
		bt.right = Empty{}
	}
	if hn, ok := bt.right.(HashedNode); ok {
		path, err := keyToPath(bt.depth, stem)
		if err != nil {
			return nil, fmt.Errorf("InsertValuesAtStem resolve error: %w", err)
		}
		data, err := resolver(path, types.Hash(hn))
		if err != nil {
			return nil, fmt.Errorf("InsertValuesAtStem resolve error: %w", err)
		}
		node, err := DeserializeNode(data, bt.depth+1)
		if err != nil {
			return nil, fmt.Errorf("InsertValuesAtStem node deserialization error: %w", err)
		}
		bt.right = node
	}
	bt.right, err = bt.right.InsertValuesAtStem(stem, values, resolver, depth+1)
	return bt, err
}

// CollectNodes collects all child nodes at a given path, and flushes them
// into the provided node collector.
func (bt *InternalNode) CollectNodes(path []byte, flushfn NodeFlushFn) error {
	if bt.left != nil {
		var p [256]byte
		copy(p[:], path)
		childpath := p[:len(path)]
		childpath = append(childpath, 0)
		if err := bt.left.CollectNodes(childpath, flushfn); err != nil {
			return err
		}
	}
	if bt.right != nil {
		var p [256]byte
		copy(p[:], path)
		childpath := p[:len(path)]
		childpath = append(childpath, 1)
		if err := bt.right.CollectNodes(childpath, flushfn); err != nil {
			return err
		}
	}
	flushfn(path, bt)
	return nil
}

// GetHeight returns the height of the node.
func (bt *InternalNode) GetHeight() int {
	var leftHeight, rightHeight int
	if bt.left != nil {
		leftHeight = bt.left.GetHeight()
	}
	if bt.right != nil {
		rightHeight = bt.right.GetHeight()
	}
	return 1 + max(leftHeight, rightHeight)
}
