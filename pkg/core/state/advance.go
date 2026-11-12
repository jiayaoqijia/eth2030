package state

import (
	"container/list"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

var (
	ErrAdvancerNoParent   = errors.New("advancer: parent state not found")
	ErrAdvancerTooManyTxs = errors.New("advancer: too many transactions")
	ErrAdvancerMaxReached = errors.New("advancer: max speculations reached")
	ErrAdvancerInvalidTx  = errors.New("advancer: invalid transaction data")
)

// AdvancerConfig configures the StateAdvancer.
type AdvancerConfig struct {
	MaxSpeculations      int // maximum concurrent speculative states
	MaxTxsPerSpeculation int // maximum transactions per speculation
	CacheSize            int // LRU cache capacity for speculations
	SpeculationDepth     int // how many blocks ahead to speculate
}

// DefaultAdvancerConfig returns sensible defaults.
func DefaultAdvancerConfig() AdvancerConfig {
	return AdvancerConfig{
		MaxSpeculations:      64,
		MaxTxsPerSpeculation: 1000,
		CacheSize:            128,
		SpeculationDepth:     4,
	}
}

// SpeculativeState represents the result of speculatively executing
// transactions against a parent state root.
type SpeculativeState struct {
	Root       types.Hash // resulting state root
	Receipts   [][]byte   // RLP-encoded receipts (placeholder)
	GasUsed    uint64     // total gas consumed
	IsValid    bool       // whether speculation was validated
	ParentRoot types.Hash // the parent state root this was built on
}

// speculationEntry is used inside the LRU cache.
type speculationEntry struct {
	key   types.Hash // parent root hash
	specs []*SpeculativeState
}

// speculationCache is a concurrency-safe LRU cache for speculative states.
type speculationCache struct {
	mu       sync.Mutex
	capacity int
	items    map[types.Hash]*list.Element
	order    *list.List // front = most recent
}

func newSpeculationCache(capacity int) *speculationCache {
	if capacity <= 0 {
		capacity = 128
	}
	return &speculationCache{
		capacity: capacity,
		items:    make(map[types.Hash]*list.Element, capacity),
		order:    list.New(),
	}
}

func (c *speculationCache) get(key types.Hash) ([]*SpeculativeState, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		return append([]*SpeculativeState(nil), elem.Value.(*speculationEntry).specs...), true
	}
	return nil, false
}

func (c *speculationCache) put(key types.Hash, specs []*SpeculativeState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putLocked(key, append([]*SpeculativeState(nil), specs...))
}

func (c *speculationCache) appendSpec(key types.Hash, spec *SpeculativeState) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		entry := elem.Value.(*speculationEntry)
		entry.specs = append(entry.specs, spec)
		return
	}
	c.putLocked(key, []*SpeculativeState{spec})
}

func (c *speculationCache) putLocked(key types.Hash, specs []*SpeculativeState) {
	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		elem.Value.(*speculationEntry).specs = specs
		return
	}

	// Evict if at capacity.
	if c.order.Len() >= c.capacity {
		back := c.order.Back()
		if back != nil {
			entry := back.Value.(*speculationEntry)
			delete(c.items, entry.key)
			c.order.Remove(back)
		}
	}

	entry := &speculationEntry{key: key, specs: specs}
	elem := c.order.PushFront(entry)
	c.items[key] = elem
}

func (c *speculationCache) remove(key types.Hash) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		delete(c.items, key)
		c.order.Remove(elem)
	}
}

func (c *speculationCache) len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}

// purgeOlderThan removes entries whose speculations all have GasUsed below
// the given threshold. In a real implementation this would use block numbers.
func (c *speculationCache) purge(predicate func(*SpeculativeState) bool) int {
	c.mu.Lock()
	defer c.mu.Unlock()

	var toRemove []*list.Element
	for e := c.order.Front(); e != nil; e = e.Next() {
		entry := e.Value.(*speculationEntry)
		allMatch := true
		for _, spec := range entry.specs {
			if !predicate(spec) {
				allMatch = false
				break
			}
		}
		if allMatch {
			toRemove = append(toRemove, e)
		}
	}
	for _, e := range toRemove {
		entry := e.Value.(*speculationEntry)
		delete(c.items, entry.key)
		c.order.Remove(e)
	}
	return len(toRemove)
}

// StateAdvancer speculatively computes future state to reduce latency
// when blocks arrive. It maintains a cache of speculative states keyed
// by parent root hash.
type StateAdvancer struct {
	mu     sync.RWMutex
	config AdvancerConfig
	cache  *speculationCache

	// states maps parent root -> MemoryStateDB for building speculations.
	states map[types.Hash]*MemoryStateDB

	// Stats tracking.
	hits   atomic.Int64
	misses atomic.Int64
}

// NewStateAdvancer creates a new StateAdvancer.
func NewStateAdvancer(config AdvancerConfig) *StateAdvancer {
	if config.MaxSpeculations <= 0 {
		config.MaxSpeculations = DefaultAdvancerConfig().MaxSpeculations
	}
	if config.MaxTxsPerSpeculation <= 0 {
		config.MaxTxsPerSpeculation = DefaultAdvancerConfig().MaxTxsPerSpeculation
	}
	if config.CacheSize <= 0 {
		config.CacheSize = DefaultAdvancerConfig().CacheSize
	}
	if config.SpeculationDepth <= 0 {
		config.SpeculationDepth = DefaultAdvancerConfig().SpeculationDepth
	}
	return &StateAdvancer{
		config: config,
		cache:  newSpeculationCache(config.CacheSize),
		states: make(map[types.Hash]*MemoryStateDB),
	}
}

// RegisterState associates a MemoryStateDB with a state root so that
// subsequent SpeculateBlock calls can build upon it.
func (sa *StateAdvancer) RegisterState(root types.Hash, db *MemoryStateDB) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.states[root] = db
}

// SpeculateBlock speculatively applies transactions against the parent
// state, computing a new state root. The txs are raw transaction bytes;
// actual EVM execution is beyond ELSA's scope, so we simulate by hashing
// each transaction to derive deterministic state mutations.
func (sa *StateAdvancer) SpeculateBlock(parentRoot types.Hash, txs [][]byte) (*SpeculativeState, error) {
	if len(txs) > sa.config.MaxTxsPerSpeculation {
		return nil, ErrAdvancerTooManyTxs
	}

	sa.mu.RLock()
	parentDB, ok := sa.states[parentRoot]
	sa.mu.RUnlock()

	if !ok {
		return nil, ErrAdvancerNoParent
	}

	if sa.ActiveSpeculations() >= sa.config.MaxSpeculations {
		return nil, ErrAdvancerMaxReached
	}

	// Deep copy the parent state so speculation is isolated.
	specDB := parentDB.Copy()

	var gasUsed uint64
	receipts := make([][]byte, 0, len(txs))

	for _, tx := range txs {
		if len(tx) == 0 {
			return nil, ErrAdvancerInvalidTx
		}

		// Derive a deterministic address and state change from the tx hash.
		// In a real implementation, this would decode and execute the tx.
		txHash := crypto.Keccak256Hash(tx)
		addr := types.BytesToAddress(txHash[:20])

		if !specDB.Exist(addr) {
			specDB.CreateAccount(addr)
		}
		// Increment nonce to simulate execution.
		nonce := specDB.GetNonce(addr)
		specDB.SetNonce(addr, nonce+1)

		// Each tx costs a base gas of 21000.
		gasUsed += 21000

		// Receipt is a simple hash-based placeholder.
		receipts = append(receipts, txHash.Bytes())
	}

	newRoot, err := specDB.Commit()
	if err != nil {
		return nil, err
	}

	spec := &SpeculativeState{
		Root:       newRoot,
		Receipts:   receipts,
		GasUsed:    gasUsed,
		IsValid:    false, // not validated until actual block arrives
		ParentRoot: parentRoot,
	}

	// Cache the speculation.
	sa.cache.appendSpec(parentRoot, spec)

	// Register the new state for chained speculations.
	sa.mu.Lock()
	sa.states[newRoot] = specDB
	sa.mu.Unlock()

	return spec, nil
}

// ValidateSpeculation checks whether a speculation matches the actual
// block result. Returns true if the speculated root matches.
func (sa *StateAdvancer) ValidateSpeculation(spec *SpeculativeState, actualRoot types.Hash) bool {
	if spec == nil {
		return false
	}
	valid := spec.Root == actualRoot
	spec.IsValid = valid

	if valid {
		sa.hits.Add(1)
	} else {
		sa.misses.Add(1)
	}
	return valid
}

// PrecomputeState builds multiple speculative states from different
// subsets of pending transactions. It tries progressively larger tx sets
// to increase the chance of a cache hit when the actual block arrives.
func (sa *StateAdvancer) PrecomputeState(parentRoot types.Hash, pendingTxs [][]byte) ([]*SpeculativeState, error) {
	if len(pendingTxs) == 0 {
		return nil, nil
	}

	maxPerSpec := sa.config.MaxTxsPerSpeculation
	if len(pendingTxs) < maxPerSpec {
		maxPerSpec = len(pendingTxs)
	}

	var results []*SpeculativeState

	// Build speculations with increasing tx counts: 25%, 50%, 75%, 100%.
	fractions := []int{25, 50, 75, 100}
	for _, pct := range fractions {
		count := (maxPerSpec * pct) / 100
		if count == 0 {
			count = 1
		}
		if count > len(pendingTxs) {
			count = len(pendingTxs)
		}

		spec, err := sa.SpeculateBlock(parentRoot, pendingTxs[:count])
		if err != nil {
			// If we hit max speculations, stop trying.
			if err == ErrAdvancerMaxReached {
				break
			}
			return results, err
		}
		results = append(results, spec)
	}

	return results, nil
}

// GetBestSpeculation returns the speculation with the highest gas used
// for the given parent root. Higher gas used implies more transactions
// were included, making it the most likely to match an actual block.
func (sa *StateAdvancer) GetBestSpeculation(parentRoot types.Hash) (*SpeculativeState, bool) {
	specs, ok := sa.cache.get(parentRoot)
	if !ok || len(specs) == 0 {
		sa.misses.Add(1)
		return nil, false
	}

	sa.hits.Add(1)
	best := specs[0]
	for _, s := range specs[1:] {
		if s.GasUsed > best.GasUsed {
			best = s
		}
	}
	return best, true
}

// PurgeSpeculations removes all cached speculations with GasUsed less
// than the given threshold. In production, this would purge by block
// number; here we use gas as a proxy for age.
func (sa *StateAdvancer) PurgeSpeculations(olderThan uint64) {
	removed := sa.cache.purge(func(s *SpeculativeState) bool {
		return s.GasUsed < olderThan
	})

	// Also clean up registered states if their entries were purged.
	if removed > 0 {
		sa.mu.Lock()
		// Keep states map bounded; remove entries not in cache.
		for root := range sa.states {
			if _, ok := sa.cache.get(root); !ok {
				// Check if this root is a speculation result.
				delete(sa.states, root)
			}
		}
		sa.mu.Unlock()
	}
}

// CacheHitRate returns the ratio of cache hits to total lookups.
// Returns 0 if no lookups have been made.
func (sa *StateAdvancer) CacheHitRate() float64 {
	hits := sa.hits.Load()
	misses := sa.misses.Load()
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// ActiveSpeculations returns the number of cached speculation entries.
func (sa *StateAdvancer) ActiveSpeculations() int {
	return sa.cache.len()
}
