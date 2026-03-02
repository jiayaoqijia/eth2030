package bintrie

import (
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
)

// HashedNode is a node that has been hashed and stored in the database.
// It holds the hash but not the underlying data, which must be resolved
// via a NodeResolverFn to access.
type HashedNode types.Hash

func (h HashedNode) Get(_ []byte, _ NodeResolverFn) ([]byte, error) {
	return nil, errors.New("cannot get from unresolved hashed node")
}

func (h HashedNode) Insert(_ []byte, _ []byte, _ NodeResolverFn, _ int) (BinaryNode, error) {
	return nil, errors.New("insert not implemented for hashed node")
}

func (h HashedNode) Copy() BinaryNode {
	nh := types.Hash(h)
	return HashedNode(nh)
}

func (h HashedNode) Hash() types.Hash {
	return types.Hash(h)
}

// HashWith returns the stored hash regardless of the hasher.
// HashedNode cannot be re-hashed without resolving the underlying data.
func (h HashedNode) HashWith(_ TrieHasher) types.Hash {
	return types.Hash(h)
}

func (h HashedNode) GetValuesAtStem(_ []byte, _ NodeResolverFn) ([][]byte, error) {
	return nil, errors.New("attempted to get values from an unresolved node")
}

func (h HashedNode) InsertValuesAtStem(stem []byte, values [][]byte, resolver NodeResolverFn, depth int) (BinaryNode, error) {
	path, err := keyToPath(depth, stem)
	if err != nil {
		return nil, fmt.Errorf("InsertValuesAtStem path generation error: %w", err)
	}
	if resolver == nil {
		return nil, errors.New("InsertValuesAtStem resolve error: resolver is nil")
	}
	data, err := resolver(path, types.Hash(h))
	if err != nil {
		return nil, fmt.Errorf("InsertValuesAtStem resolve error: %w", err)
	}
	node, err := DeserializeNode(data, depth)
	if err != nil {
		return nil, fmt.Errorf("InsertValuesAtStem node deserialization error: %w", err)
	}
	return node.InsertValuesAtStem(stem, values, resolver, depth)
}

func (h HashedNode) CollectNodes([]byte, NodeFlushFn) error {
	// HashedNodes are already persisted and don't need collection.
	return nil
}

func (h HashedNode) GetHeight() int {
	panic("tried to get the height of a hashed node")
}
