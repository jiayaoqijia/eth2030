package bintrie

import (
	"slices"

	"github.com/eth2030/eth2030/core/types"
)

// Empty represents an empty node in the binary trie.
type Empty struct{}

func (e Empty) Get(_ []byte, _ NodeResolverFn) ([]byte, error) {
	return nil, nil
}

func (e Empty) Insert(key []byte, value []byte, _ NodeResolverFn, depth int) (BinaryNode, error) {
	var values [256][]byte
	values[key[31]] = value
	return &StemNode{
		Stem:   slices.Clone(key[:31]),
		Values: values[:],
		depth:  depth,
	}, nil
}

func (e Empty) Copy() BinaryNode {
	return Empty{}
}

func (e Empty) Hash() types.Hash {
	return types.Hash{}
}

func (e Empty) HashWith(_ TrieHasher) types.Hash {
	return types.Hash{}
}

func (e Empty) GetValuesAtStem(_ []byte, _ NodeResolverFn) ([][]byte, error) {
	var values [256][]byte
	return values[:], nil
}

func (e Empty) InsertValuesAtStem(key []byte, values [][]byte, _ NodeResolverFn, depth int) (BinaryNode, error) {
	return &StemNode{
		Stem:   slices.Clone(key[:31]),
		Values: values,
		depth:  depth,
	}, nil
}

func (e Empty) CollectNodes(_ []byte, _ NodeFlushFn) error {
	return nil
}

func (e Empty) GetHeight() int {
	return 0
}
