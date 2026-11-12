package state

import (
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
	"math/big"
	"testing"
)

func TestStateBackendsRollbackDeletionAndRecreation(t *testing.T) {
	for _, kind := range []string{"memory", "trie", "snap"} {
		t.Run(kind, func(t *testing.T) {
			addr := types.BytesToAddress([]byte{0x12, 0x34})
			fresh := types.BytesToAddress([]byte{0x56, 0x78})
			slot := types.Hash{}
			mem := NewMemoryStateDB()
			mem.AddBalance(addr, big.NewInt(100))
			mem.SetCode(addr, []byte{0})
			mem.SetState(addr, slot, types.BytesToHash([]byte{7}))
			_, _ = mem.Commit()
			var db StateDB = mem
			if kind == "trie" {
				db = NewTrieStateDBFromMemory(rawdb.NewMemoryDB(), mem.Copy())
				if _, err := db.Commit(); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "snap" {
				store := rawdb.NewMemoryDB()
				snapshot, err := ExportStateSnapshot(mem)
				if err != nil {
					t.Fatal(err)
				}
				if err := StoreStateSnapshot(store, mem.GetRoot(), snapshot); err != nil {
					t.Fatal(err)
				}
				db, err = OpenSnapState(store, mem.GetRoot())
				if err != nil {
					t.Fatal(err)
				}
			}
			root := db.GetRoot()
			checkpoint := db.Snapshot()
			db.AddBalance(fresh, big.NewInt(1))
			db.SetState(fresh, slot, types.BytesToHash([]byte{1}))
			db.RevertToSnapshot(checkpoint)
			if db.Exist(fresh) || db.GetRoot() != root {
				t.Fatal("revert leaked an implicitly created account")
			}
			checkpoint = db.Snapshot()
			db.CreateAccount(addr)
			db.RevertToSnapshot(checkpoint)
			if _, err := db.Commit(); err != nil {
				t.Fatal(err)
			}
			if db.GetState(addr, slot) != types.BytesToHash([]byte{7}) || db.GetRoot() != root {
				t.Fatal("reverted recreation purged existing storage")
			}
			checkpoint = db.Snapshot()
			db.SelfDestruct(addr)
			db.(interface{ FinalizeTransaction() }).FinalizeTransaction()
			if db.Exist(addr) || db.GetCodeSize(addr) != 0 || db.GetState(addr, slot) != (types.Hash{}) {
				t.Fatal("finalized deletion remained readable")
			}
			db.RevertToSnapshot(checkpoint)
			if db.GetRoot() != root {
				t.Fatal("reverting finalized candidate did not restore state")
			}
			db.SelfDestruct(addr)
			db.(interface{ FinalizeTransaction() }).FinalizeTransaction()
			db.AddBalance(addr, big.NewInt(9))
			if _, err := db.Commit(); err != nil {
				t.Fatal(err)
			}
			if db.GetBalance(addr).Int64() != 9 || db.GetCodeSize(addr) != 0 || db.GetState(addr, slot) != (types.Hash{}) {
				t.Fatal("recreation inherited deleted code/storage or lost its balance")
			}
		})
	}
}

func TestTrieCommitInvalidatesPreviouslyReadRoot(t *testing.T) {
	db := NewTrieStateDB(rawdb.NewMemoryDB())
	addr := types.BytesToAddress([]byte{1})
	db.AddBalance(addr, big.NewInt(1))
	before := db.GetRoot()
	db.AddBalance(addr, big.NewInt(1))
	committed, err := db.Commit()
	if err != nil {
		t.Fatal(err)
	}
	if committed == before || committed != db.GetRoot() {
		t.Fatal("commit reused a root computed before a mutation")
	}
}
