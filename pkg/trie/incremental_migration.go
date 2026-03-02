// incremental_migration.go implements cursor-based incremental migration from
// MPT to binary Merkle trie with dual-write support and persistence hooks.
package trie

import (
	"bytes"
	"math"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// IncrementalMigrationConfig controls the incremental migration behavior.
type IncrementalMigrationConfig struct {
	// BatchSize is the default number of accounts to migrate per block.
	BatchSize int

	// DualWriteEnabled enables writing to both MPT and binary trie.
	DualWriteEnabled bool

	// CursorKey is the DB key for persisting the migration cursor.
	CursorKey string
}

// DefaultIncrementalMigrationConfig returns a config with sensible defaults.
func DefaultIncrementalMigrationConfig() IncrementalMigrationConfig {
	return IncrementalMigrationConfig{
		BatchSize:        1000,
		DualWriteEnabled: false,
		CursorKey:        "migration_cursor",
	}
}

// IncrementalMigration performs cursor-based incremental MPT-to-BinaryTrie
// migration. Each call to MigrateBlock processes a bounded batch, allowing
// migration to be spread across many blocks.
type IncrementalMigration struct {
	mu     sync.Mutex
	source *Trie
	dest   *BinaryTrie
	config IncrementalMigrationConfig

	// cursor is the last migrated key (hashed). Keys <= cursor are migrated.
	cursor []byte

	// pairs is the lazily-collected sorted list of all (hashedKey, value) pairs.
	pairs []incrPair

	// offset tracks how many pairs have been migrated so far.
	offset int

	// done indicates the migration is complete.
	done bool
}

type incrPair struct {
	hashedKey types.Hash
	rawKey    []byte
	value     []byte
}

// NewIncrementalMigration creates a new incremental migration engine.
func NewIncrementalMigration(mpt *Trie, bt *BinaryTrie, config IncrementalMigrationConfig) *IncrementalMigration {
	if config.BatchSize <= 0 {
		config.BatchSize = 1000
	}
	if config.CursorKey == "" {
		config.CursorKey = "migration_cursor"
	}
	return &IncrementalMigration{
		source: mpt,
		dest:   bt,
		config: config,
	}
}

// collectPairs lazily collects and sorts all MPT key-value pairs.
func (m *IncrementalMigration) collectPairs() {
	if m.pairs != nil {
		return
	}
	it := NewIterator(m.source)
	for it.Next() {
		hk := crypto.Keccak256Hash(it.Key)
		rawKey := make([]byte, len(it.Key))
		copy(rawKey, it.Key)
		val := make([]byte, len(it.Value))
		copy(val, it.Value)
		m.pairs = append(m.pairs, incrPair{hashedKey: hk, rawKey: rawKey, value: val})
	}
	// Sort by hashed key for deterministic cursor-based ordering.
	sortIncrPairs(m.pairs)
}

// sortIncrPairs sorts pairs by hashed key lexicographically.
func sortIncrPairs(pairs []incrPair) {
	// Simple insertion sort (sufficient for our use case; no sort import needed).
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && bytes.Compare(pairs[j].hashedKey[:], pairs[j-1].hashedKey[:]) < 0; j-- {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
		}
	}
}

// MigrateBlock migrates up to batchSize accounts from the MPT into the binary
// trie. Returns the number of accounts migrated and whether migration is done.
func (m *IncrementalMigration) MigrateBlock(batchSize int) (migrated int, isDone bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.done {
		return 0, true, nil
	}

	m.collectPairs()

	if batchSize <= 0 {
		batchSize = m.config.BatchSize
	}

	start := m.offset
	end := start + batchSize
	if end > len(m.pairs) {
		end = len(m.pairs)
	}

	for i := start; i < end; i++ {
		m.dest.PutHashed(m.pairs[i].hashedKey, m.pairs[i].value)
	}

	count := end - start
	m.offset = end

	if count > 0 {
		m.cursor = m.pairs[end-1].hashedKey[:]
	}

	if m.offset >= len(m.pairs) {
		m.done = true
	}

	return count, m.done, nil
}

// IsDone returns true if all accounts have been migrated.
func (m *IncrementalMigration) IsDone() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.done
}

// Progress returns migration statistics.
func (m *IncrementalMigration) Progress() (migrated, total int, pct float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.collectPairs()
	total = len(m.pairs)
	migrated = m.offset
	if total > 0 {
		pct = float64(migrated) / float64(total) * 100.0
	}
	return
}

// GetCursor returns the current migration cursor (last migrated hashed key).
func (m *IncrementalMigration) GetCursor() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cursor == nil {
		return nil
	}
	c := make([]byte, len(m.cursor))
	copy(c, m.cursor)
	return c
}

// SetCursor restores the migration cursor from a persisted value.
// This allows resuming migration after a restart.
func (m *IncrementalMigration) SetCursor(cursor []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cursor == nil {
		m.cursor = nil
		m.offset = 0
		m.done = false
		return
	}

	m.collectPairs()
	m.cursor = make([]byte, len(cursor))
	copy(m.cursor, cursor)

	// Find the offset corresponding to this cursor.
	m.offset = 0
	for i, p := range m.pairs {
		if bytes.Compare(p.hashedKey[:], cursor) <= 0 {
			m.offset = i + 1
		} else {
			break
		}
	}
	m.done = m.offset >= len(m.pairs)
}

// Source returns the source MPT.
func (m *IncrementalMigration) Source() *Trie {
	return m.source
}

// Dest returns the destination binary trie.
func (m *IncrementalMigration) Dest() *BinaryTrie {
	return m.dest
}

// DualWriteStateManager manages reads and writes during a partial migration.
// New writes go to both MPT and binary trie. Reads prefer the binary trie
// for migrated accounts and fall back to MPT for not-yet-migrated ones.
type DualWriteStateManager struct {
	mu        sync.Mutex
	mpt       *Trie
	bt        *BinaryTrie
	migration *IncrementalMigration
}

// NewDualWriteStateManager creates a dual-write manager wrapping the
// incremental migration state.
func NewDualWriteStateManager(migration *IncrementalMigration) *DualWriteStateManager {
	return &DualWriteStateManager{
		mpt:       migration.source,
		bt:        migration.dest,
		migration: migration,
	}
}

// Put writes a value to both MPT and binary trie.
func (d *DualWriteStateManager) Put(key, value []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if err := d.mpt.Put(key, value); err != nil {
		return err
	}
	hk := crypto.Keccak256Hash(key)
	return d.bt.PutHashed(hk, value)
}

// Get reads a value, preferring the binary trie for migrated keys.
func (d *DualWriteStateManager) Get(key []byte) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	hk := crypto.Keccak256Hash(key)
	if d.IsAccountMigrated(hk[:]) {
		val, err := d.bt.GetHashed(hk)
		if err == nil {
			return val, nil
		}
	}
	return d.mpt.Get(key)
}

// IsAccountMigrated returns true if the given hashed key has been migrated
// (i.e., key <= cursor).
func (d *DualWriteStateManager) IsAccountMigrated(hashedKey []byte) bool {
	cursor := d.migration.GetCursor()
	if cursor == nil {
		return false
	}
	return bytes.Compare(hashedKey, cursor) <= 0
}

// MigrateFromMPTIncremental wraps the one-shot MigrateFromMPT to use the
// incremental engine with a batch size of MaxInt (i.e., all at once).
// This maintains backward compatibility.
func MigrateFromMPTIncremental(mpt *Trie) *BinaryTrie {
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{
		BatchSize: math.MaxInt32,
		CursorKey: "migration_cursor",
	}
	m := NewIncrementalMigration(mpt, bt, config)
	m.MigrateBlock(math.MaxInt32)
	return bt
}
