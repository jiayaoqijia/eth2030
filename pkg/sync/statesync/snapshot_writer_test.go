package statesync

import (
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

type failingTrieBatch struct {
	rawdb.Batch
	err error
}

func (b *failingTrieBatch) Write() error { return b.err }

type failingTrieDB struct {
	rawdb.Database
	err error
}

func (d *failingTrieDB) NewBatch() rawdb.Batch {
	return &failingTrieBatch{Batch: d.Database.NewBatch(), err: d.err}
}

func TestSnapshotWriterStreamingTrieAndFailure(t *testing.T) {
	source := state.NewMemoryStateDB()
	for i := byte(1); i <= 8; i++ {
		addr := types.BytesToAddress([]byte{i})
		source.AddBalance(addr, big.NewInt(int64(i)))
		source.SetState(addr, types.BytesToHash([]byte{i}), types.BytesToHash([]byte{i + 1}))
	}
	snapshot, err := state.ExportStateSnapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	root := source.GetRoot()
	db := rawdb.NewMemoryDB()
	writer := NewSnapshotWriter(db)
	writer.snapshot = snapshot
	if err := writer.Finalize(root); err != nil {
		t.Fatal(err)
	}
	if ok, err := db.Has(append([]byte("t"), root[:]...)); err != nil || !ok {
		t.Fatalf("account root node was not persisted: %v", err)
	}
	loaded, err := state.LoadStateSnapshot(db, root)
	if err != nil || loaded.Root() != root {
		t.Fatalf("snapshot did not survive readback: %v", err)
	}

	writeErr := errors.New("write failed")
	failing := &failingTrieDB{Database: rawdb.NewMemoryDB(), err: writeErr}
	writer = NewSnapshotWriter(failing)
	writer.snapshot = snapshot
	if err := writer.Finalize(root); !errors.Is(err, writeErr) {
		t.Fatalf("trie write failure was not returned: %v", err)
	}
	if _, err := state.LoadStateSnapshot(failing, root); err == nil {
		t.Fatal("failed snapshot was published")
	}
}

func TestSnapshotWriterRejectsStreamedRootMismatch(t *testing.T) {
	writer := NewSnapshotWriter(rawdb.NewMemoryDB())
	entries := map[types.Hash][]byte{types.BytesToHash([]byte{1}): {1}}
	if err := writer.writeTrie(entries, types.Hash{}); err == nil {
		t.Fatal("accepted incorrect trie root")
	}
}
