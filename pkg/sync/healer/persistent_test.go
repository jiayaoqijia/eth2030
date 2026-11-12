package healer

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/trie/mpt"
)

type healingPeer struct {
	SnapPeer
	accounts *mpt.Trie
	storage  map[types.Hash]*mpt.Trie
	empty    bool
}

func (p healingPeer) RequestTrieNodes(root types.Hash, paths [][]byte) ([][]byte, error) {
	out := make([][]byte, len(paths))
	if p.empty {
		return out, nil
	}
	for i, path := range paths {
		tr := p.accounts
		if len(path) > 32 {
			tr = p.storage[types.BytesToHash(path[:32])]
			path = path[32:]
		}
		var err error
		out[i], err = tr.NodeAtPath(path)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
func TestPersistentHealerAuthenticatesAccountAndStorageNodes(t *testing.T) {
	source := state.NewMemoryStateDB()
	for i := byte(1); i <= 4; i++ {
		addr := types.BytesToAddress([]byte{i})
		source.AddBalance(addr, big.NewInt(1))
		source.SetState(addr, types.BytesToHash([]byte{i}), types.BytesToHash([]byte{9}))
	}
	snapshot, err := state.ExportStateSnapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	peer := healingPeer{accounts: mpt.New(), storage: make(map[types.Hash]*mpt.Trie)}
	for k, v := range snapshot.Accounts {
		_ = peer.accounts.Put(k[:], v)
	}
	for account, slots := range snapshot.Storage {
		tr := mpt.New()
		for k, v := range slots {
			_ = tr.Put(k[:], v)
		}
		peer.storage[account] = tr
	}
	db := rawdb.NewMemoryDB()
	store := NewTrieStore(db)
	paths := store.MissingTrieNodes(source.GetRoot(), 128)
	if len(paths) != 1 {
		t.Fatalf("missing root: %d", len(paths))
	}
	if err := store.WriteTrieNode(paths[0], []byte{0xc0}); err == nil {
		t.Fatal("unauthenticated trie bytes accepted")
	}
	healer := NewStateHealer(source.GetRoot(), store)
	if err := healer.Run(peer); err != nil {
		t.Fatal(err)
	}
	missing, err := mpt.MissingNodes(source.GetRoot(), mpt.NewRawDBNodeReader(db.Get), 0)
	if err != nil || len(missing) != 0 || healer.Progress().NodesHealed < 5 {
		t.Fatalf("healing incomplete: %+v %v", healer.Progress(), err)
	}
}
func TestPersistentHealerDoesNotCompleteExhaustedTasks(t *testing.T) {
	root := types.BytesToHash([]byte{1})
	healer := NewStateHealer(root, NewTrieStore(rawdb.NewMemoryDB()))
	if err := healer.Run(healingPeer{empty: true}); !errors.Is(err, ErrHealNodeInvalid) {
		t.Fatalf("want exhausted task error, got %v", err)
	}
	if healer.IsComplete() {
		t.Fatal("failed healing reported complete")
	}
}
