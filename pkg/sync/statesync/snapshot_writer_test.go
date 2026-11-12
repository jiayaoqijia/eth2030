package statesync

import (
	"bytes"
	"errors"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
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

type unexpectedCodePeer struct{ SnapPeer }

func (unexpectedCodePeer) RequestBytecodes(BytecodeRequest) (*BytecodeResponse, error) {
	return nil, errors.New("unexpected network request")
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

func TestSnapshotWriterReusesVerifiedCodeAfterReopen(t *testing.T) {
	dir := t.TempDir()
	db, err := rawdb.NewFileDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	code := []byte{0x60, 0x01, 0x60, 0x00}
	hash := crypto.Keccak256Hash(code)
	writer := NewSnapshotWriter(db)
	if err := writer.WriteBytecode(hash, code); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = rawdb.NewFileDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	writer = NewSnapshotWriter(db)
	if !writer.HasBytecode(hash) || !bytes.Equal(writer.snapshot.Codes[hash], code) {
		t.Fatal("verified code was not restored after reopen")
	}
	scheduler := NewStateSyncScheduler(NewSnapshotWriter(db), nil)
	scheduler.codeTasks[hash] = &codeTask{hash: hash}
	if err := scheduler.runCodeDownload(unexpectedCodePeer{}); err != nil || scheduler.Progress().CodeHashesDone != 1 {
		t.Fatalf("scheduler did not reuse cached code: %v", err)
	}
	if err := db.Put(append([]byte("snap-code/"), hash[:]...), []byte{0xff}); err != nil {
		t.Fatal(err)
	}
	if NewSnapshotWriter(db).HasBytecode(hash) {
		t.Fatal("corrupt cached code was accepted")
	}
}

func TestSnapshotWriterCodeBatchRejectsMismatchWithoutPublishing(t *testing.T) {
	db := rawdb.NewMemoryDB()
	writer := NewSnapshotWriter(db)
	code := []byte{1, 2, 3}
	hash := crypto.Keccak256Hash(code)
	if err := writer.WriteBytecodes([]BytecodeData{{Hash: hash, Code: code}, {Hash: types.Hash{}, Code: code}}); err == nil {
		t.Fatal("accepted mismatched code")
	}
	if ok, _ := db.Has(append([]byte("snap-code/"), hash[:]...)); ok || writer.HasBytecode(hash) {
		t.Fatal("part of a rejected code batch was published")
	}
}
