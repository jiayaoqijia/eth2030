// state_healer.go implements state healing for snap sync. After the bulk
// state download (accounts, storage, bytecodes), the trie may have missing
// interior nodes. The healer detects these gaps, schedules healing tasks,
// and tracks progress until the state trie is complete.
package healer

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// State healer configuration constants.
const (
	// DefaultHealBatchSize is the default number of trie nodes per healing batch.
	DefaultHealBatchSize = 128

	// MaxHealRetries is the maximum times a single path can be retried.
	MaxHealRetries = 3

	// HealProgressInterval is how often to log healing progress.
	HealProgressInterval = 5 * time.Second
)

// State healer errors.
var (
	ErrHealerClosed    = errors.New("state healer: closed")
	ErrHealerRunning   = errors.New("state healer: already running")
	ErrNoGapsFound     = errors.New("state healer: no gaps detected")
	ErrHealBatchEmpty  = errors.New("state healer: empty batch")
	ErrHealNodeInvalid = errors.New("state healer: invalid node data")
)

// HealingTask represents a single trie node that needs to be fetched.
type HealingTask struct {
	// Path is the trie node path (nibble-encoded for MPT, or key prefix for binary).
	Path []byte
	// Root is the state root under which this node belongs.
	Root types.Hash
	// Retries counts how many times this task has been attempted.
	Retries int
	// CreatedAt is when the task was first detected.
	CreatedAt time.Time
}

// HealingProgress tracks the overall state of healing.
type HealingProgress struct {
	// NodesDetected is the total number of missing nodes detected.
	NodesDetected uint64
	// NodesHealed is the number of nodes successfully fetched.
	NodesHealed uint64
	// NodesFailed is the number of nodes that failed after max retries.
	NodesFailed uint64
	// BytesDownloaded is the total bytes of node data fetched.
	BytesDownloaded uint64
	// StartTime is when healing started.
	StartTime time.Time
	// Complete indicates all gaps have been filled.
	Complete bool
}

// Elapsed returns the time since healing started.
func (p *HealingProgress) Elapsed() time.Duration {
	if p.StartTime.IsZero() {
		return 0
	}
	return time.Since(p.StartTime)
}

// Remaining returns the estimated number of nodes still to heal.
func (p *HealingProgress) Remaining() uint64 {
	healed := p.NodesHealed + p.NodesFailed
	if healed >= p.NodesDetected {
		return 0
	}
	return p.NodesDetected - healed
}

// StateHealer detects missing trie nodes in the local database after snap
// sync and coordinates fetching them from peers.
type StateHealer struct {
	mu       sync.Mutex
	root     types.Hash
	writer   StateWriter
	closed   atomic.Bool
	running  atomic.Bool
	progress HealingProgress

	// Pending tasks queue, keyed by hex-encoded path.
	pending map[string]*HealingTask
	// Failed tasks that exceeded max retries.
	failed map[string]*HealingTask

	batchSize int
}

// NewStateHealer creates a new state healer for the given root and state writer.
func NewStateHealer(root types.Hash, writer StateWriter) *StateHealer {
	return &StateHealer{
		root:      root,
		writer:    writer,
		pending:   make(map[string]*HealingTask),
		failed:    make(map[string]*HealingTask),
		batchSize: DefaultHealBatchSize,
	}
}

// SetBatchSize configures the number of trie nodes to request per batch.
func (h *StateHealer) SetBatchSize(size int) {
	if size > 0 {
		h.mu.Lock()
		h.batchSize = size
		h.mu.Unlock()
	}
}

// Root returns the state root being healed.
func (h *StateHealer) Root() types.Hash {
	return h.root
}

// Progress returns a snapshot of the current healing progress.
func (h *StateHealer) Progress() HealingProgress {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.progress
}

// DetectGaps scans the local state database for missing trie nodes under
// the healer's root. It populates the pending task queue with paths that
// need to be fetched. Returns the number of gaps found.
func (h *StateHealer) DetectGaps() (int, error) {
	if h.closed.Load() {
		return 0, ErrHealerClosed
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	missing := h.writer.MissingTrieNodes(h.root, 0)
	if len(missing) == 0 {
		return 0, nil
	}

	now := time.Now()
	added := 0
	for _, path := range missing {
		key := string(path)
		if _, exists := h.pending[key]; exists {
			continue // already queued
		}
		if _, exists := h.failed[key]; exists {
			continue // already failed permanently
		}

		h.pending[key] = &HealingTask{
			Path:      copySlice(path),
			Root:      h.root,
			CreatedAt: now,
		}
		added++
	}
	h.progress.NodesDetected += uint64(added)
	return added, nil
}

// PendingCount returns the number of tasks waiting to be processed.
func (h *StateHealer) PendingCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.pending)
}

// FailedCount returns the number of permanently failed tasks.
func (h *StateHealer) FailedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.failed)
}

// ScheduleHealing returns the next batch of healing tasks to process.
// The batch size is limited by the configured batch size. Tasks are
// removed from the pending queue and returned for processing.
func (h *StateHealer) ScheduleHealing() ([]*HealingTask, error) {
	if h.closed.Load() {
		return nil, ErrHealerClosed
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.pending) == 0 {
		return nil, nil
	}

	batchSize := h.batchSize
	if batchSize > len(h.pending) {
		batchSize = len(h.pending)
	}

	batch := make([]*HealingTask, 0, batchSize)
	for key, task := range h.pending {
		batch = append(batch, task)
		delete(h.pending, key)
		if len(batch) >= batchSize {
			break
		}
	}
	return batch, nil
}

// ProcessHealingBatch processes a batch of healing results. Each result
// is a (path, data) pair from the peer response. Empty data means the
// node was not available.
func (h *StateHealer) ProcessHealingBatch(tasks []*HealingTask, results [][]byte) error {
	if h.closed.Load() {
		return ErrHealerClosed
	}
	if len(tasks) == 0 {
		return ErrHealBatchEmpty
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for i, task := range tasks {
		var data []byte
		if i < len(results) {
			data = results[i]
		}

		if len(data) == 0 {
			// Node not available; retry or mark as failed.
			task.Retries++
			if task.Retries >= MaxHealRetries {
				h.failed[string(task.Path)] = task
				h.progress.NodesFailed++
			} else {
				h.pending[string(task.Path)] = task
			}
			continue
		}

		// Validate the node data by checking it hashes to something reasonable.
		nodeHash := crypto.Keccak256Hash(data)
		if nodeHash == (types.Hash{}) {
			task.Retries++
			if task.Retries >= MaxHealRetries {
				h.failed[string(task.Path)] = task
				h.progress.NodesFailed++
			} else {
				h.pending[string(task.Path)] = task
			}
			continue
		}

		// Write the node to the database.
		if err := h.writer.WriteTrieNode(task.Path, data); err != nil {
			return fmt.Errorf("state healer: write trie node: %w", err)
		}

		h.progress.NodesHealed++
		h.progress.BytesDownloaded += uint64(len(data))
	}

	// Check if healing is complete.
	if len(h.pending) == 0 {
		// Re-detect to see if more gaps emerged.
		missing := h.writer.MissingTrieNodes(h.root, 1)
		if len(missing) == 0 {
			h.progress.Complete = true
		}
	}

	return nil
}

// IsComplete returns true if all gaps have been healed.
func (h *StateHealer) IsComplete() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.progress.Complete
}

// Reset clears all pending and failed tasks, allowing a fresh scan.
func (h *StateHealer) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending = make(map[string]*HealingTask)
	h.failed = make(map[string]*HealingTask)
	h.progress = HealingProgress{}
}

// Close shuts down the healer.
func (h *StateHealer) Close() {
	h.closed.Store(true)
}

// Run executes a full healing loop using the given peer. It repeatedly
// detects gaps, schedules batches, requests data, and processes results
// until the state is complete or the healer is closed.
func (h *StateHealer) Run(peer SnapPeer) error {
	if !h.running.CompareAndSwap(false, true) {
		return ErrHealerRunning
	}
	defer h.running.Store(false)

	h.mu.Lock()
	h.progress.StartTime = time.Now()
	h.mu.Unlock()

	for !h.closed.Load() {
		// Detect gaps.
		n, err := h.DetectGaps()
		if err != nil {
			return err
		}
		if n == 0 && h.PendingCount() == 0 {
			if h.FailedCount() > 0 {
				return ErrHealNodeInvalid
			}
			h.mu.Lock()
			h.progress.Complete = true
			h.mu.Unlock()
			return nil
		}

		// Schedule a batch.
		tasks, err := h.ScheduleHealing()
		if err != nil {
			return err
		}
		if len(tasks) == 0 {
			if h.FailedCount() > 0 {
				return ErrHealNodeInvalid
			}
			h.mu.Lock()
			h.progress.Complete = true
			h.mu.Unlock()
			return nil
		}

		// Build paths for the request.
		paths := make([][]byte, len(tasks))
		for i, t := range tasks {
			paths[i] = t.Path
		}

		// Request trie nodes from the peer.
		results, err := peer.RequestTrieNodes(h.root, paths)
		if err != nil {
			// On network error, re-queue tasks.
			h.mu.Lock()
			for _, task := range tasks {
				h.pending[string(task.Path)] = task
			}
			h.mu.Unlock()
			return fmt.Errorf("state healer: peer request failed: %w", err)
		}

		// Process the results.
		if err := h.ProcessHealingBatch(tasks, results); err != nil {
			return err
		}
	}

	return ErrHealerClosed
}

// copySlice returns a copy of the input byte slice.
func copySlice(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
