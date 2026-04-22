// Package bal implements EIP-7928 Block Access Lists.
//
// prefetch.go provides a BAL-aware state prefetcher that uses the block
// access list to pre-load state data before parallel transaction execution.
// This mirrors the architecture from go-ethereum's reader_eip_7928.go:
//
//	[Block Access List] ← hint
//	       ↓
//	[PrefetchReader] → async background state loading
//	       ↓
//	[MutationOverlay] → merges pre-state with prior tx mutations
//	       ↓
//	[ReadTracker] → captures per-tx state accesses for BAL construction
package bal

import (
	"sync"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
)

// StateReader provides read access to account and storage state.
type StateReader interface {
	GetBalance(addr types.Address) uint64
	GetNonce(addr types.Address) uint64
	GetCode(addr types.Address) []byte
	GetCodeSize(addr types.Address) int
	GetState(addr types.Address, key types.Hash) types.Hash
	Exist(addr types.Address) bool
}

// BALPrefetcher pre-loads state data based on block access list hints.
// It asynchronously fetches accounts and storage slots that the BAL predicts
// will be accessed during block execution, minimizing I/O blocking.
type BALPrefetcher struct {
	reader  StateReader
	workers int
	tasks   chan prefetchTask
	wg      sync.WaitGroup
	stopCh  chan struct{}
	once    sync.Once

	// Stats
	prefetched atomic.Uint64
	missed     atomic.Uint64
}

type prefetchTask struct {
	addr types.Address
	keys []types.Hash // nil = account-only
}

// NewBALPrefetcher creates a prefetcher that uses the given block access list
// to pre-load state. Workers controls parallelism (default 4).
func NewBALPrefetcher(reader StateReader, workers int) *BALPrefetcher {
	if workers <= 0 {
		workers = 4
	}
	p := &BALPrefetcher{
		reader:  reader,
		workers: workers,
		tasks:   make(chan prefetchTask, 256),
		stopCh:  make(chan struct{}),
	}
	for i := 0; i < workers; i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

// PrefetchFromBAL schedules prefetch tasks for all accounts and storage slots
// referenced in the block access list up to the given access index.
func (p *BALPrefetcher) PrefetchFromBAL(bal *BlockAccessList, upToAccessIdx int) {
	if bal == nil {
		return
	}
	// Collect all unique addresses and their storage keys from the BAL.
	addrKeys := make(map[types.Address]map[types.Hash]bool)
	for _, entry := range bal.Entries {
		if int(entry.AccessIndex) > upToAccessIdx {
			continue
		}
		addr := entry.Address
		if _, ok := addrKeys[addr]; !ok {
			addrKeys[addr] = make(map[types.Hash]bool)
		}
		for _, sr := range entry.StorageReads {
			addrKeys[addr][sr.Slot] = true
		}
		for _, sc := range entry.StorageChanges {
			addrKeys[addr][sc.Slot] = true
		}
	}
	// Submit prefetch tasks.
	for addr, keys := range addrKeys {
		var keySlice []types.Hash
		for k := range keys {
			keySlice = append(keySlice, k)
		}
		select {
		case p.tasks <- prefetchTask{addr: addr, keys: keySlice}:
			p.prefetched.Add(1)
		case <-p.stopCh:
			return
		}
	}
}

// Stop shuts down the prefetcher workers.
func (p *BALPrefetcher) Stop() {
	p.once.Do(func() {
		close(p.stopCh)
		p.wg.Wait()
	})
}

// Stats returns prefetch statistics.
func (p *BALPrefetcher) Stats() (prefetched, missed uint64) {
	return p.prefetched.Load(), p.missed.Load()
}

func (p *BALPrefetcher) worker() {
	defer p.wg.Done()
	for {
		select {
		case task := <-p.tasks:
			// Touch account to warm the cache.
			p.reader.Exist(task.addr)
			p.reader.GetBalance(task.addr)
			p.reader.GetNonce(task.addr)
			if len(task.keys) > 0 {
				p.reader.GetCodeSize(task.addr)
				for _, key := range task.keys {
					p.reader.GetState(task.addr, key)
				}
			}
		case <-p.stopCh:
			return
		}
	}
}

// MutationOverlay wraps a StateReader and overlays mutations from prior
// transactions in the block. This provides a "unified view" that includes
// both the pre-transition state and changes from preceding txs, enabling
// parallel execution where each tx sees a consistent state snapshot.
type MutationOverlay struct {
	base      StateReader
	mutations []TxMutation // ordered by tx index
	upToIdx   int          // apply mutations [0..upToIdx)
}

// TxMutation records the state changes from a single transaction.
type TxMutation struct {
	TxIndex       int
	BalanceDeltas map[types.Address]int64 // balance changes (can be negative)
	NonceDeltas   map[types.Address]int64 // nonce increments
	StorageWrites map[types.Address]map[types.Hash]types.Hash
}

// NewMutationOverlay creates an overlay that applies mutations from txs
// [0..upToIdx) on top of the base state reader.
func NewMutationOverlay(base StateReader, mutations []TxMutation, upToIdx int) *MutationOverlay {
	return &MutationOverlay{base: base, mutations: mutations, upToIdx: upToIdx}
}

func (m *MutationOverlay) GetBalance(addr types.Address) uint64 {
	balance := int64(m.base.GetBalance(addr))
	for i := 0; i < m.upToIdx && i < len(m.mutations); i++ {
		if delta, ok := m.mutations[i].BalanceDeltas[addr]; ok {
			balance += delta
		}
	}
	if balance < 0 {
		return 0
	}
	return uint64(balance)
}

func (m *MutationOverlay) GetNonce(addr types.Address) uint64 {
	nonce := int64(m.base.GetNonce(addr))
	for i := 0; i < m.upToIdx && i < len(m.mutations); i++ {
		if delta, ok := m.mutations[i].NonceDeltas[addr]; ok {
			nonce += delta
		}
	}
	if nonce < 0 {
		return 0
	}
	return uint64(nonce)
}

func (m *MutationOverlay) GetCode(addr types.Address) []byte {
	return m.base.GetCode(addr)
}

func (m *MutationOverlay) GetCodeSize(addr types.Address) int {
	return m.base.GetCodeSize(addr)
}

func (m *MutationOverlay) GetState(addr types.Address, key types.Hash) types.Hash {
	// Check mutations in reverse order (latest wins).
	for i := m.upToIdx - 1; i >= 0 && i < len(m.mutations); i-- {
		if writes, ok := m.mutations[i].StorageWrites[addr]; ok {
			if val, ok := writes[key]; ok {
				return val
			}
		}
	}
	return m.base.GetState(addr, key)
}

func (m *MutationOverlay) Exist(addr types.Address) bool {
	return m.base.Exist(addr)
}

// ReadTracker wraps a StateReader and records all state accesses for
// constructing the per-tx portion of the block access list.
type ReadTracker struct {
	inner    StateReader
	mu       sync.Mutex
	accounts map[types.Address]bool
	storage  map[types.Address]map[types.Hash]bool
}

// NewReadTracker creates a tracker that records accesses to inner.
func NewReadTracker(inner StateReader) *ReadTracker {
	return &ReadTracker{
		inner:    inner,
		accounts: make(map[types.Address]bool),
		storage:  make(map[types.Address]map[types.Hash]bool),
	}
}

func (t *ReadTracker) track(addr types.Address) {
	t.mu.Lock()
	t.accounts[addr] = true
	t.mu.Unlock()
}

func (t *ReadTracker) trackStorage(addr types.Address, key types.Hash) {
	t.mu.Lock()
	t.accounts[addr] = true
	if t.storage[addr] == nil {
		t.storage[addr] = make(map[types.Hash]bool)
	}
	t.storage[addr][key] = true
	t.mu.Unlock()
}

// AccessedAccounts returns all addresses accessed during execution.
func (t *ReadTracker) AccessedAccounts() []types.Address {
	t.mu.Lock()
	defer t.mu.Unlock()
	addrs := make([]types.Address, 0, len(t.accounts))
	for addr := range t.accounts {
		addrs = append(addrs, addr)
	}
	return addrs
}

// AccessedStorage returns all storage keys accessed per address.
func (t *ReadTracker) AccessedStorage() map[types.Address][]types.Hash {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make(map[types.Address][]types.Hash, len(t.storage))
	for addr, keys := range t.storage {
		keySlice := make([]types.Hash, 0, len(keys))
		for k := range keys {
			keySlice = append(keySlice, k)
		}
		result[addr] = keySlice
	}
	return result
}

func (t *ReadTracker) GetBalance(addr types.Address) uint64 {
	t.track(addr)
	return t.inner.GetBalance(addr)
}

func (t *ReadTracker) GetNonce(addr types.Address) uint64 {
	t.track(addr)
	return t.inner.GetNonce(addr)
}

func (t *ReadTracker) GetCode(addr types.Address) []byte {
	t.track(addr)
	return t.inner.GetCode(addr)
}

func (t *ReadTracker) GetCodeSize(addr types.Address) int {
	t.track(addr)
	return t.inner.GetCodeSize(addr)
}

func (t *ReadTracker) GetState(addr types.Address, key types.Hash) types.Hash {
	t.trackStorage(addr, key)
	return t.inner.GetState(addr, key)
}

func (t *ReadTracker) Exist(addr types.Address) bool {
	t.track(addr)
	return t.inner.Exist(addr)
}
