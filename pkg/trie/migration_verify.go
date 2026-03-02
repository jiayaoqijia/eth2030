// migration_verify.go provides post-migration verification and rollback
// for MPT-to-binary trie migrations.
package trie

import (
	"fmt"

	"github.com/eth2030/eth2030/crypto"
)

// MigrationVerifier verifies that a migration between MPT and binary trie
// is correct, comparing every account and storage slot.
type MigrationVerifier struct{}

// NewMigrationVerifier creates a new verifier.
func NewMigrationVerifier() *MigrationVerifier {
	return &MigrationVerifier{}
}

// VerifyMigration compares every account/slot between MPT and binary trie.
// Returns a list of human-readable error descriptions and a summary error.
func (v *MigrationVerifier) VerifyMigration(mpt *Trie, bt *BinaryTrie) ([]string, error) {
	var errs []string

	mptCount := 0
	it := NewIterator(mpt)
	for it.Next() {
		hk := crypto.Keccak256Hash(it.Key)
		val, err := bt.GetHashed(hk)
		if err != nil {
			errs = append(errs, fmt.Sprintf("key %x: missing in binary trie", it.Key))
			mptCount++
			continue
		}
		if !bytesEqual(val, it.Value) {
			errs = append(errs, fmt.Sprintf("key %x: value mismatch (mpt=%x, bt=%x)", it.Key, it.Value, val))
		}
		mptCount++
	}

	btCount := bt.Len()
	if btCount != mptCount {
		errs = append(errs, fmt.Sprintf("count mismatch: mpt=%d, bt=%d", mptCount, btCount))
	}

	if len(errs) > 0 {
		return errs, fmt.Errorf("migration verification found %d errors", len(errs))
	}
	return nil, nil
}

// VerifyAccount verifies a single account key between MPT and binary trie.
func (v *MigrationVerifier) VerifyAccount(key []byte, mpt *Trie, bt *BinaryTrie) error {
	mptVal, mptErr := mpt.Get(key)
	hk := crypto.Keccak256Hash(key)
	btVal, btErr := bt.GetHashed(hk)

	if mptErr != nil && btErr != nil {
		// Both missing is OK.
		return nil
	}
	if mptErr != nil {
		return fmt.Errorf("key %x: exists in bt but not mpt", key)
	}
	if btErr != nil {
		return fmt.Errorf("key %x: exists in mpt but not bt", key)
	}
	if !bytesEqual(mptVal, btVal) {
		return fmt.Errorf("key %x: value mismatch", key)
	}
	return nil
}

// Rollback resets the incremental migration by clearing the cursor and
// the binary trie.
func (v *MigrationVerifier) Rollback(m *IncrementalMigration) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.cursor = nil
	m.offset = 0
	m.done = false
	// Replace destination with a fresh empty trie.
	m.dest = NewBinaryTrie()
	return nil
}

// bytesEqual is defined in account_proof.go.
