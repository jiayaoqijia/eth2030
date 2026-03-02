package trie

import (
	"math"
)

// MigrateFromMPT converts an MPT trie to a binary Merkle trie. Each key-value
// pair from the MPT is re-inserted into the binary trie with the key hashed
// via keccak256 (matching the binary trie's key derivation).
//
// This is a convenience wrapper that uses the incremental engine with a
// batch size of MaxInt32, performing the entire migration in one call.
func MigrateFromMPT(mpt *Trie) *BinaryTrie {
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{
		BatchSize: math.MaxInt32,
		CursorKey: "migration_cursor",
	}
	m := NewIncrementalMigration(mpt, bt, config)
	m.MigrateBlock(math.MaxInt32)
	return bt
}
