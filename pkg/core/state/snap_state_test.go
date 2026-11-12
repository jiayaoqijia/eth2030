package state

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

type failSnapshotBatchDB struct {
	rawdb.Database
	writes int
	err    error
}

type failSnapshotBatch struct {
	rawdb.Batch
	db *failSnapshotBatchDB
}

func (db *failSnapshotBatchDB) NewBatch() rawdb.Batch {
	return &failSnapshotBatch{Batch: db.Database.NewBatch(), db: db}
}

func (b *failSnapshotBatch) Write() error {
	b.db.writes++
	if b.db.writes == 2 {
		return b.db.err
	}
	return b.Batch.Write()
}

func TestSnapshotBatchFailureDoesNotPublish(t *testing.T) {
	source := NewMemoryStateDB()
	source.SetCode(types.BytesToAddress([]byte{1}), bytes.Repeat([]byte{1}, snapshotBatchLimit))
	source.SetCode(types.BytesToAddress([]byte{2}), bytes.Repeat([]byte{2}, snapshotBatchLimit))
	snap, err := ExportStateSnapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("batch failure")
	db := &failSnapshotBatchDB{Database: rawdb.NewMemoryDB(), err: failure}
	root := source.GetRoot()
	if err := StoreStateSnapshot(db, root, snap); !errors.Is(err, failure) || db.writes != 2 {
		t.Fatalf("expected second batch failure, writes=%d err=%v", db.writes, err)
	}
	if _, err := LoadStateSnapshot(db, root); err == nil {
		t.Fatal("partially stored snapshot was published")
	}
}

func TestSnapshotValidationRootMatchesNativeTrie(t *testing.T) {
	entries := make(map[types.Hash][]byte)
	for i := 0; i < 500; i++ {
		key := crypto.Keccak256Hash([]byte{byte(i >> 8), byte(i)})
		entries[key] = bytes.Repeat([]byte{byte(i%255 + 1)}, i%96+1)
	}
	got, err := snapshotValidationRoot(entries)
	if err != nil || got != snapshotTrie(entries).Hash() {
		t.Fatalf("streamed root differs from native trie: root=%s err=%v", got, err)
	}
	entries[types.Hash{}] = nil
	if _, err := snapshotValidationRoot(entries); err == nil {
		t.Fatal("accepted an empty snapshot trie value")
	}
}

func TestSnapStateExecutionAndReopen(t *testing.T) {
	addr := types.BytesToAddress([]byte{1})
	slot := types.BytesToHash([]byte{2})
	value := types.BytesToHash([]byte{3})
	source := NewMemoryStateDB()
	source.AddBalance(addr, big.NewInt(100))
	source.SetNonce(addr, 4)
	source.SetCode(addr, []byte{0x60, 0x00})
	source.SetState(addr, slot, value)
	snap, err := ExportStateSnapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	db := rawdb.NewMemoryDB()
	root := source.GetRoot()
	if err := StoreStateSnapshot(db, root, snap); err != nil {
		t.Fatal(err)
	}
	imported, err := OpenSnapState(db, root)
	if err != nil {
		t.Fatal(err)
	}
	if imported.GetBalance(addr).Int64() != 100 || imported.GetNonce(addr) != 4 || imported.GetState(addr, slot) != value {
		t.Fatal("hashed snapshot is not readable through address and slot preimages")
	}
	rev := imported.Snapshot()
	imported.SubBalance(addr, big.NewInt(7))
	imported.SetState(addr, slot, types.Hash{})
	imported.RevertToSnapshot(rev)
	if imported.GetRoot() != root {
		t.Fatal("reverted snapshot root differs")
	}
	copyState := imported.Dup()
	imported.SubBalance(addr, big.NewInt(7))
	imported.SetState(addr, slot, types.Hash{})
	imported.SetNonce(addr, 5)
	if copyState.GetState(addr, slot) != value || copyState.GetRoot() != root {
		t.Fatal("dirty state copy lost imported storage root")
	}
	source.SubBalance(addr, big.NewInt(7))
	source.SetState(addr, slot, types.Hash{})
	source.SetNonce(addr, 5)
	if imported.GetRoot() != source.GetRoot() {
		t.Fatal("post-sync execution root differs from independently executed state")
	}
	newRoot, err := imported.Commit()
	if err != nil {
		t.Fatal(err)
	}
	if copyState.GetRoot() != root || copyState.GetBalance(addr).Int64() != 100 {
		t.Fatal("commit mutated an independent state copy")
	}
	reopened, err := OpenSnapState(db, newRoot)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.GetRoot() != newRoot || reopened.GetBalance(addr).Int64() != 93 || reopened.GetState(addr, slot) != (types.Hash{}) || reopened.GetCodeSize(addr) != 2 {
		t.Fatal("reopen lost executed state")
	}
	reopened.SelfDestruct(addr)
	source.SelfDestruct(addr)
	if reopened.GetRoot() != source.GetRoot() {
		t.Fatal("selfdestruct did not delete imported account")
	}
}

func TestSnapStateRejectsIncompleteSnapshot(t *testing.T) {
	addr := types.BytesToAddress([]byte{1})
	source := NewMemoryStateDB()
	source.SetCode(addr, []byte{1, 2, 3})
	source.SetState(addr, types.Hash{}, types.BytesToHash([]byte{1}))
	original, err := ExportStateSnapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"account", "storage", "code"} {
		t.Run(kind, func(t *testing.T) {
			snap := original.copy()
			hash := crypto.Keccak256Hash(addr[:])
			switch kind {
			case "account":
				delete(snap.Accounts, hash)
			case "storage":
				delete(snap.Storage, hash)
			case "code":
				snap.Codes = make(map[types.Hash][]byte)
			}
			db := rawdb.NewMemoryDB()
			if err := StoreStateSnapshot(db, source.GetRoot(), snap); err == nil {
				t.Fatal("incomplete snapshot accepted")
			}
			if _, err := OpenSnapState(db, source.GetRoot()); err == nil {
				t.Fatal("invalid snapshot published")
			}
		})
	}
}
