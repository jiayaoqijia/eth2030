// state_sync.go implements StateSyncScheduler, a state machine orchestrating
// snap-sync state download: account range scheduling, storage trie sync, code
// download, healing pass, and progress reporting.
package statesync

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// State sync tuning constants.
const (
	MaxAccountPeers            = 4
	MaxStoragePeers            = 4
	MaxCodePeers               = 2
	AccountTaskSize            = 16
	TaskRetryLimit             = 5
	TaskRetryBackoff           = 500 * time.Millisecond
	PivotUpdateInterval        = 30 * time.Second
	MaxPivotAge         uint64 = 128
)

// StateSyncScheduler state machine phases.
const (
	SyncPhaseInit uint32 = iota
	SyncPhaseAccounts
	SyncPhaseStorage
	SyncPhaseCodes
	SyncPhaseHeal
	SyncPhaseDone
	SyncPhaseFailed
)

// SyncPhaseName returns a human-readable name for a sync phase.
func SyncPhaseName(phase uint32) string {
	names := [...]string{"init", "accounts", "storage", "codes", "heal", "done", "failed"}
	if int(phase) < len(names) {
		return names[phase]
	}
	return fmt.Sprintf("unknown(%d)", phase)
}

// State sync scheduler errors.
var (
	ErrStateSyncRunning  = errors.New("state sync: already running")
	ErrStateSyncStopped  = errors.New("state sync: stopped")
	ErrPivotTooOld       = errors.New("state sync: pivot block too far behind chain head")
	ErrTaskFailed        = errors.New("state sync: task permanently failed")
	ErrAllTasksExhausted = errors.New("state sync: all account range tasks exhausted")
)

// StateSyncProgress holds a snapshot of state sync progress.
type StateSyncProgress struct {
	Phase                uint32
	PivotBlock           uint64
	PivotRoot            types.Hash
	AccountRangesTotal   int
	AccountRangesDone    int
	AccountsDownloaded   uint64
	AccountBytes         uint64
	StorageAccountsTotal int
	StorageAccountsDone  int
	StorageSlotsTotal    uint64
	StorageBytes         uint64
	CodeHashesTotal      int
	CodeHashesDone       int
	CodeBytes            uint64
	HealNodesTotal       uint64
	HealNodesDone        uint64
	HealBytes            uint64
	StartTime            time.Time
	LastUpdate           time.Time
}

// TotalBytes returns the total bytes downloaded across all categories.
func (p *StateSyncProgress) TotalBytes() uint64 {
	return p.AccountBytes + p.StorageBytes + p.CodeBytes + p.HealBytes
}

// PercentComplete returns an estimated completion percentage.
func (p *StateSyncProgress) PercentComplete() float64 {
	if p.AccountRangesTotal == 0 {
		return 0
	}
	pct := float64(p.AccountRangesDone) / float64(p.AccountRangesTotal) * 40.0
	if p.StorageAccountsTotal > 0 {
		pct += float64(p.StorageAccountsDone) / float64(p.StorageAccountsTotal) * 30.0
	}
	if p.CodeHashesTotal > 0 {
		pct += float64(p.CodeHashesDone) / float64(p.CodeHashesTotal) * 10.0
	}
	if p.HealNodesTotal > 0 {
		pct += float64(p.HealNodesDone) / float64(p.HealNodesTotal) * 20.0
	}
	if pct > 100 {
		pct = 100
	}
	return pct
}

type accountRangeTask struct {
	id      uint64
	origin  types.Hash
	limit   types.Hash
	done    bool
	retries int
	peerID  string
}

type storageTask struct {
	accountHash types.Hash
	done        bool
	retries     int
}

type codeTask struct {
	hash    types.Hash
	done    bool
	retries int
}

// ProgressCallback is called periodically with a progress snapshot.
type ProgressCallback func(progress StateSyncProgress)

// StateSyncScheduler orchestrates the full state sync pipeline.
type StateSyncScheduler struct {
	mu           sync.Mutex
	phase        atomic.Uint32
	running      atomic.Bool
	cancel       chan struct{}
	pivotBlock   uint64
	pivotRoot    types.Hash
	pivotHeader  *types.Header
	accountTasks []*accountRangeTask
	nextTaskID   atomic.Uint64
	storageTasks []*storageTask
	codeTasks    map[types.Hash]*codeTask
	progress     StateSyncProgress
	onProgress   ProgressCallback
	writer       StateWriter
}

// NewStateSyncScheduler creates a new state sync scheduler.
func NewStateSyncScheduler(writer StateWriter, callback ProgressCallback) *StateSyncScheduler {
	return &StateSyncScheduler{
		cancel: make(chan struct{}), codeTasks: make(map[types.Hash]*codeTask),
		writer: writer, onProgress: callback,
	}
}

func (s *StateSyncScheduler) Phase() uint32   { return s.phase.Load() }
func (s *StateSyncScheduler) IsRunning() bool { return s.running.Load() }

// Progress returns a snapshot of current progress.
func (s *StateSyncScheduler) Progress() StateSyncProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := s.progress
	cp.Phase = s.phase.Load()
	return cp
}

// SetPivot configures the pivot block for the state sync.
func (s *StateSyncScheduler) SetPivot(header *types.Header) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running.Load() {
		return ErrStateSyncRunning
	}
	if header == nil || header.Number == nil || !header.Number.IsUint64() {
		return fmt.Errorf("state sync: invalid pivot")
	}
	s.pivotBlock = header.Number.Uint64()
	s.pivotRoot = header.Root
	s.pivotHeader = header
	s.progress.PivotBlock = s.pivotBlock
	s.progress.PivotRoot = s.pivotRoot
	return nil
}

// Stop cancels the scheduler.
func (s *StateSyncScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.cancel:
	default:
		close(s.cancel)
	}
}

func (s *StateSyncScheduler) initAccountTasks(n int) {
	if n <= 0 {
		n = 1
	}
	if n > 256 {
		n = 256
	}
	s.accountTasks = make([]*accountRangeTask, n)
	max256 := new(big.Int).Lsh(big.NewInt(1), 256)
	step := new(big.Int).Div(max256, big.NewInt(int64(n)))
	for i := 0; i < n; i++ {
		task := &accountRangeTask{id: s.nextTaskID.Add(1)}
		if i > 0 {
			task.origin = types.IntToHash(new(big.Int).Mul(step, big.NewInt(int64(i))))
		}
		if i == n-1 {
			for j := range task.limit {
				task.limit[j] = 0xff
			}
		} else {
			next := new(big.Int).Mul(step, big.NewInt(int64(i+1)))
			next.Sub(next, big.NewInt(1))
			task.limit = types.IntToHash(next)
		}
		s.accountTasks[i] = task
	}
	s.progress.AccountRangesTotal = n
}

func (s *StateSyncScheduler) nextPendingAccountTask() *accountRangeTask {
	for _, t := range s.accountTasks {
		if !t.done && t.peerID == "" {
			return t
		}
	}
	return nil
}

func (s *StateSyncScheduler) allAccountTasksDone() bool {
	for _, t := range s.accountTasks {
		if !t.done {
			return false
		}
	}
	return true
}

// Run executes the full state sync pipeline. Blocks until done or cancelled.
func (s *StateSyncScheduler) Run(peer SnapPeer) error {
	return s.RunContext(context.Background(), peer)
}

func (s *StateSyncScheduler) RunContext(ctx context.Context, peer SnapPeer) error {
	if !s.running.CompareAndSwap(false, true) {
		return ErrStateSyncRunning
	}
	defer s.running.Store(false)

	s.mu.Lock()
	if s.pivotHeader == nil || s.writer == nil || peer == nil {
		s.mu.Unlock()
		s.phase.Store(SyncPhaseFailed)
		return fmt.Errorf("state sync: missing pivot, writer or peer")
	}
	s.cancel = make(chan struct{})
	s.storageTasks = nil
	s.codeTasks = make(map[types.Hash]*codeTask)
	s.progress = StateSyncProgress{PivotBlock: s.pivotBlock, PivotRoot: s.pivotRoot}
	s.progress.StartTime = time.Now()
	s.initAccountTasks(AccountTaskSize)
	s.phase.Store(SyncPhaseAccounts)
	s.mu.Unlock()
	cancelChannel := s.cancel
	stop := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		select {
		case <-cancelChannel:
		default:
			close(cancelChannel)
		}
	})
	defer stop()

	phases := []struct {
		phase uint32
		fn    func(SnapPeer) error
	}{
		{SyncPhaseAccounts, s.runAccountDownload},
		{SyncPhaseStorage, s.runStorageDownload},
		{SyncPhaseCodes, s.runCodeDownload},
		{SyncPhaseHeal, s.runHealing},
	}
	for _, p := range phases {
		s.phase.Store(p.phase)
		if err := p.fn(peer); err != nil {
			s.phase.Store(SyncPhaseFailed)
			return err
		}
	}
	if finalizer, ok := s.writer.(interface{ Finalize(types.Hash) error }); ok {
		if err := finalizer.Finalize(s.pivotRoot); err != nil {
			s.phase.Store(SyncPhaseFailed)
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		s.phase.Store(SyncPhaseFailed)
		return err
	}
	s.phase.Store(SyncPhaseDone)
	s.reportProgress()
	return nil
}

func (s *StateSyncScheduler) runAccountDownload(peer SnapPeer) error {
	for {
		select {
		case <-s.cancel:
			return ErrStateSyncStopped
		default:
		}
		s.mu.Lock()
		task := s.nextPendingAccountTask()
		if task == nil {
			done := s.allAccountTasksDone()
			s.mu.Unlock()
			if done {
				return nil
			}
			return ErrAllTasksExhausted
		}
		root := s.pivotRoot
		task.peerID = peer.ID()
		s.mu.Unlock()

		resp, err := peer.RequestAccountRange(AccountRangeRequest{
			ID: s.nextTaskID.Add(1), Root: root,
			Origin: task.origin, Limit: task.limit, Bytes: 512 * 1024,
		})
		if err != nil {
			s.mu.Lock()
			task.peerID = ""
			task.retries++
			if task.retries >= TaskRetryLimit {
				s.mu.Unlock()
				return fmt.Errorf("%w: account range: %v", ErrTaskFailed, err)
			}
			s.mu.Unlock()
			if err := s.retryWait(); err != nil {
				return err
			}
			continue
		}
		for _, acct := range resp.Accounts {
			if e := s.writer.WriteAccount(acct.Hash, acct); e != nil {
				return fmt.Errorf("state sync: write account: %w", e)
			}
			s.mu.Lock()
			s.progress.AccountsDownloaded++
			s.progress.AccountBytes += estimateAccountSize(acct)
			if acct.Root != types.EmptyRootHash {
				s.storageTasks = append(s.storageTasks, &storageTask{accountHash: acct.Hash})
			}
			if acct.CodeHash != types.EmptyCodeHash {
				if _, ok := s.codeTasks[acct.CodeHash]; !ok {
					s.codeTasks[acct.CodeHash] = &codeTask{hash: acct.CodeHash}
				}
			}
			s.mu.Unlock()
		}
		s.mu.Lock()
		if !resp.More || len(resp.Accounts) == 0 {
			task.done = true
			s.progress.AccountRangesDone++
		} else {
			task.origin = incrementHash(resp.Accounts[len(resp.Accounts)-1].Hash)
		}
		task.peerID = ""
		s.progress.LastUpdate = time.Now()
		s.mu.Unlock()
		s.reportProgress()
	}
}

func (s *StateSyncScheduler) runStorageDownload(peer SnapPeer) error {
	for {
		select {
		case <-s.cancel:
			return ErrStateSyncStopped
		default:
		}
		s.mu.Lock()
		var task *storageTask
		for _, t := range s.storageTasks {
			if !t.done {
				task = t
				break
			}
		}
		if task == nil {
			s.mu.Unlock()
			return nil
		}
		s.progress.StorageAccountsTotal = len(s.storageTasks)
		root := s.pivotRoot
		s.mu.Unlock()

		origin := types.Hash{}
		for {
			select {
			case <-s.cancel:
				return ErrStateSyncStopped
			default:
			}
			var limit types.Hash
			for i := range limit {
				limit[i] = 0xff
			}
			resp, err := peer.RequestStorageRange(StorageRangeRequest{
				ID: s.nextTaskID.Add(1), Root: root,
				Accounts: []types.Hash{task.accountHash},
				Origin:   origin, Limit: limit, Bytes: 512 * 1024,
			})
			if err != nil {
				task.retries++
				if task.retries >= TaskRetryLimit {
					return fmt.Errorf("%w: storage range: %v", ErrTaskFailed, err)
				}
				if err := s.retryWait(); err != nil {
					return err
				}
				break
			}
			for _, slot := range resp.Slots {
				if e := s.writer.WriteStorage(slot.AccountHash, slot.SlotHash, slot.Value); e != nil {
					return fmt.Errorf("state sync: write storage: %w", e)
				}
				s.mu.Lock()
				s.progress.StorageSlotsTotal++
				s.progress.StorageBytes += uint64(len(slot.Value)) + 32
				s.mu.Unlock()
			}
			if !resp.More || len(resp.Slots) == 0 {
				s.mu.Lock()
				task.done = true
				s.progress.StorageAccountsDone++
				s.progress.LastUpdate = time.Now()
				s.mu.Unlock()
				break
			}
			origin = incrementHash(resp.Slots[len(resp.Slots)-1].SlotHash)
		}
		s.reportProgress()
	}
}

func (s *StateSyncScheduler) runCodeDownload(peer SnapPeer) error {
	failures := 0
	for {
		select {
		case <-s.cancel:
			return ErrStateSyncStopped
		default:
		}
		s.mu.Lock()
		s.progress.CodeHashesTotal = len(s.codeTasks)
		batch := make([]types.Hash, 0, MaxBytecodeItems)
		for _, ct := range s.codeTasks {
			if ct.done {
				continue
			}
			if s.writer.HasBytecode(ct.hash) {
				ct.done = true
				s.progress.CodeHashesDone++
				continue
			}
			batch = append(batch, ct.hash)
			if len(batch) >= MaxBytecodeItems {
				break
			}
		}
		if len(batch) == 0 {
			s.mu.Unlock()
			return nil
		}
		s.mu.Unlock()

		resp, err := peer.RequestBytecodes(BytecodeRequest{ID: s.nextTaskID.Add(1), Hashes: batch})
		if err != nil {
			failures++
			if failures >= TaskRetryLimit {
				return fmt.Errorf("%w: bytecode: %v", ErrTaskFailed, err)
			}
			if err := s.retryWait(); err != nil {
				return err
			}
			continue
		}
		if resp == nil || len(resp.Codes) == 0 {
			failures++
			if failures >= TaskRetryLimit {
				return fmt.Errorf("%w: no bytecode progress", ErrTaskFailed)
			}
			if err := s.retryWait(); err != nil {
				return err
			}
			continue
		}
		failures = 0
		for _, code := range resp.Codes {
			if e := s.writer.WriteBytecode(code.Hash, code.Code); e != nil {
				return fmt.Errorf("state sync: write bytecode: %w", e)
			}
			s.mu.Lock()
			if ct, ok := s.codeTasks[code.Hash]; ok {
				ct.done = true
				s.progress.CodeHashesDone++
			}
			s.progress.CodeBytes += uint64(len(code.Code))
			s.progress.LastUpdate = time.Now()
			s.mu.Unlock()
		}
		s.reportProgress()
	}
}

func (s *StateSyncScheduler) runHealing(peer SnapPeer) error {
	root := s.pivotRoot
	stalled := 0
	for {
		select {
		case <-s.cancel:
			return ErrStateSyncStopped
		default:
		}
		missing := s.writer.MissingTrieNodes(root, MaxHealNodes)
		if len(missing) == 0 {
			return nil
		}
		s.mu.Lock()
		s.progress.HealNodesTotal += uint64(len(missing))
		s.mu.Unlock()

		nodes, err := peer.RequestTrieNodes(root, missing)
		if err != nil {
			return fmt.Errorf("state sync: heal request failed: %w", err)
		}
		progress := false
		for i, data := range nodes {
			if len(data) == 0 || i >= len(missing) {
				continue
			}
			if e := s.writer.WriteTrieNode(missing[i], data); e != nil {
				return fmt.Errorf("state sync: write trie node: %w", e)
			}
			progress = true
			s.mu.Lock()
			s.progress.HealNodesDone++
			s.progress.HealBytes += uint64(len(data))
			s.progress.LastUpdate = time.Now()
			s.mu.Unlock()
		}
		if !progress {
			stalled++
			if stalled >= TaskRetryLimit {
				return fmt.Errorf("%w: no healing progress", ErrTaskFailed)
			}
		} else {
			stalled = 0
		}
		s.reportProgress()
	}
}

func (s *StateSyncScheduler) retryWait() error {
	timer := time.NewTimer(TaskRetryBackoff)
	defer timer.Stop()
	select {
	case <-s.cancel:
		return ErrStateSyncStopped
	case <-timer.C:
		return nil
	}
}

func (s *StateSyncScheduler) reportProgress() {
	if s.onProgress != nil {
		s.onProgress(s.Progress())
	}
}
