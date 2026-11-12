package rawdb

import (
	"os"
	"testing"
)

func TestFileDBBatchFailureCannotRecoverMixedForkchoice(t *testing.T) {
	dir := t.TempDir()
	db, err := NewFileDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	a, b := []byte("head"), []byte("finalized")
	if err := db.Put(a, []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := db.Put(b, []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = NewFileDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	fault := db.keyPath(b) + ".tmp"
	if err := os.Mkdir(fault, 0700); err != nil {
		t.Fatal(err)
	}
	batch := db.NewBatch()
	_ = batch.Put(a, []byte("new"))
	_ = batch.Put(b, []byte("new"))
	if err := batch.Write(); err == nil {
		t.Fatal("write fault did not trigger")
	}
	_ = db.Close()
	_ = os.Remove(fault)
	db, err = NewFileDB(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	head, err := db.Get(a)
	if err != nil {
		t.Fatal(err)
	}
	final, err := db.Get(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(head) != string(final) {
		t.Fatalf("recovered mixed transaction: head=%s finalized=%s", head, final)
	}
}
