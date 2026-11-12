package node

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/chain"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/engine/payload"
	p2psnap "github.com/eth2030/eth2030/p2p/snap"
	syncbeacon "github.com/eth2030/eth2030/sync/beacon"
	"github.com/eth2030/eth2030/sync/healer"
	"github.com/eth2030/eth2030/sync/statesync"
	"github.com/eth2030/eth2030/trie/mpt"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
)

func (a *nodeDepsAdapter) RequestExecutionSync(fc payload.ForkchoiceStateV1) {
	if a.n.executionSync != nil {
		a.n.executionSync.Request(syncbeacon.ExecutionTarget{Head: fc.HeadBlockHash, Safe: fc.SafeBlockHash, Finalized: fc.FinalizedBlockHash}, a.n.blockchain.CurrentBlock().NumberU64())
	}
}

func (a *nodeDepsAdapter) SyncProgress() (bool, uint64, uint64, uint64) {
	current := a.n.blockchain.CurrentBlock().NumberU64()
	if a.n.executionSync == nil {
		return false, 0, current, current
	}
	active, start, highest := a.n.executionSync.Progress()
	if highest < current {
		highest = current
	}
	return active, start, current, highest
}

func (n *Node) applyExecutionTarget(target syncbeacon.ExecutionTarget) error {
	if err := n.blockchain.ApplyForkchoice(target.Head, target.Safe, target.Finalized); err != nil {
		return err
	}
	n.txPool.SetForkRules(n.blockchain.Config(), n.blockchain.CurrentBlock().Time())
	n.txPool.Reset(n.blockchain.State())
	return nil
}

func (n *Node) syncFinalizedPivot(ctx context.Context, pivot *types.Block) (bool, error) {
	if n.blockchain.CurrentBlock().NumberU64() != 0 {
		return false, nil
	}
	// The CL chose this finalized hash. Download enough authenticated ancestry
	// for header validation and the EVM's previous-256 BLOCKHASH window.
	history := make([]*types.Header, 0, 256)
	child := pivot
	for len(history) < 256 && child.NumberU64() > 0 {
		parent := n.blockchain.GetBlock(child.ParentHash())
		if parent == nil {
			var err error
			parent, err = n.ethHandler.FetchBlock(ctx, child.ParentHash())
			if err != nil {
				return false, err
			}
		}
		if parent.NumberU64()+1 != child.NumberU64() {
			return false, fmt.Errorf("snap: non-contiguous pivot history")
		}
		history = append(history, parent.Header())
		child = parent
	}
	peers := n.snapHandler.Clients()
	lastErr := fmt.Errorf("snap: no connected peers")
	for _, client := range peers {
		if n.ethHandler.Peers().Peer(client.ID()) == nil {
			continue
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		writer := statesync.NewSnapshotWriter(n.db)
		scheduler := statesync.NewStateSyncScheduler(writer, nil)
		if err := scheduler.SetPivot(pivot.Header()); err != nil {
			return false, err
		}
		peer := client.WithContext(ctx)
		if err := scheduler.RunContext(ctx, peer); err != nil {
			lastErr = err
			continue
		}
		// Audit reconstructed trie nodes through the same authenticated healer
		// used to repair missing nodes. A fixed pivot normally has no gaps.
		heal := healer.NewStateHealer(pivot.Header().Root, healer.NewTrieStore(n.db))
		stop := context.AfterFunc(ctx, heal.Close)
		err := heal.Run(healPeer{peer})
		stop()
		if err != nil {
			lastErr = err
			continue
		}
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if err := n.blockchain.ImportSnapPivot(pivot, history); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, lastErr
}

// Healers request only trie nodes; range download stays in the scheduler.
type healPeer struct{ *p2psnap.VerifiedPeer }

func (p healPeer) RequestAccountRange(healer.AccountRangeRequest) (*healer.AccountRangeResponse, error) {
	return nil, fmt.Errorf("healer: unexpected account request")
}
func (p healPeer) RequestStorageRange(healer.StorageRangeRequest) (*healer.StorageRangeResponse, error) {
	return nil, fmt.Errorf("healer: unexpected storage request")
}
func (p healPeer) RequestBytecodes(healer.BytecodeRequest) (*healer.BytecodeResponse, error) {
	return nil, fmt.Errorf("healer: unexpected code request")
}

type snapStateBackend struct {
	chain  *chain.Blockchain
	db     rawdb.Database
	mu     sync.Mutex
	root   types.Hash
	cached *state.StateSnapshot
	codes  map[types.Hash][]byte
}

func newSnapStateBackend(chain *chain.Blockchain, db rawdb.Database) *snapStateBackend {
	return &snapStateBackend{chain: chain, db: db}
}
func (b *snapStateBackend) snapshot(root types.Hash) (*state.StateSnapshot, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cached != nil && root == b.root {
		return b.cached, nil
	}
	st, err := b.chain.StateAtRoot(root)
	var snap *state.StateSnapshot
	if err == nil {
		snap, err = state.ExportStateSnapshot(st)
	} else {
		snap, err = state.LoadStateSnapshot(b.db, root)
	}
	if err != nil {
		return nil, p2psnap.ErrMissingRoot
	}
	b.root, b.cached = root, snap
	if b.codes == nil {
		b.codes = make(map[types.Hash][]byte)
	}
	for hash, code := range snap.Codes {
		b.codes[hash] = code
	}
	return snap, nil
}
func sortedSnapshotKeys(entries map[types.Hash][]byte) []types.Hash {
	keys := make([]types.Hash, 0, len(entries))
	for k := range entries {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i][:], keys[j][:]) < 0 })
	return keys
}
func (b *snapStateBackend) AccountIterator(root, origin types.Hash, fn func(types.Hash, []byte) bool) error {
	snap, err := b.snapshot(root)
	if err != nil {
		return err
	}
	for _, key := range sortedSnapshotKeys(snap.Accounts) {
		if bytes.Compare(key[:], origin[:]) < 0 {
			continue
		}
		var account gethtypes.StateAccount
		if err := gethrlp.DecodeBytes(snap.Accounts[key], &account); err != nil {
			return err
		}
		if !fn(key, gethtypes.SlimAccountRLP(account)) {
			break
		}
	}
	return nil
}
func (b *snapStateBackend) StorageIterator(root, account types.Hash, origin []byte, fn func(types.Hash, []byte) bool) error {
	snap, err := b.snapshot(root)
	if err != nil {
		return err
	}
	if _, ok := snap.Accounts[account]; !ok {
		return p2psnap.ErrMissingRoot
	}
	for _, key := range sortedSnapshotKeys(snap.Storage[account]) {
		if bytes.Compare(key[:], origin) >= 0 && !fn(key, bytes.Clone(snap.Storage[account][key])) {
			break
		}
	}
	return nil
}
func (b *snapStateBackend) Code(hash types.Hash) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if code, ok := b.codes[hash]; ok {
		return bytes.Clone(code), nil
	}
	return b.db.Get(append([]byte("snap-code/"), hash[:]...))
}
func (b *snapStateBackend) AccountProof(root, key types.Hash) ([][]byte, error) {
	snap, err := b.snapshot(root)
	if err != nil {
		return nil, err
	}
	return snap.AccountProof(key)
}
func (b *snapStateBackend) StorageProof(root, account, key types.Hash) ([][]byte, error) {
	snap, err := b.snapshot(root)
	if err != nil {
		return nil, err
	}
	return snap.StorageProof(account, key)
}
func (b *snapStateBackend) TrieNode(root types.Hash, path []byte) ([]byte, error) {
	snap, err := b.snapshot(root)
	if err != nil {
		return nil, err
	}
	entries := snap.Accounts
	if len(path) > 32 {
		entries = snap.Storage[types.BytesToHash(path[:32])]
		path = path[32:]
	}
	tr := mpt.New()
	for k, v := range entries {
		if err := tr.Put(k[:], v); err != nil {
			return nil, err
		}
	}
	return tr.NodeAtPath(path)
}
