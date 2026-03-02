package state

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// --- Task 2.1.1: BinaryTrieBackedStateDB tests ---

func TestBinaryTrieStateDB_EmptyRoot(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	root := db.GetRoot()
	// An empty binary trie hashes to the zero hash (unlike MPT's EmptyRootHash).
	if root != (types.Hash{}) {
		t.Errorf("empty binary trie root = %s, want zero hash", root)
	}
	// It must differ from the MPT empty root hash.
	if root == types.EmptyRootHash {
		t.Error("empty binary trie root should differ from MPT EmptyRootHash")
	}
}

func TestBinaryTrieStateDB_AccountCRUD(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	// Create account with balance.
	db.AddBalance(addr, big.NewInt(1000))
	if got := db.GetBalance(addr); got.Cmp(big.NewInt(1000)) != 0 {
		t.Errorf("balance = %v, want 1000", got)
	}

	// Set nonce.
	db.SetNonce(addr, 5)
	if got := db.GetNonce(addr); got != 5 {
		t.Errorf("nonce = %d, want 5", got)
	}

	// Set code.
	code := []byte{0x60, 0x00, 0x60, 0x00, 0xf3}
	db.SetCode(addr, code)
	if got := db.GetCode(addr); len(got) != len(code) {
		t.Errorf("code length = %d, want %d", len(got), len(code))
	}
	if db.GetCodeHash(addr) == (types.Hash{}) {
		t.Error("code hash should not be zero")
	}
	if db.GetCodeSize(addr) != len(code) {
		t.Errorf("code size = %d, want %d", db.GetCodeSize(addr), len(code))
	}

	// Sub balance.
	db.SubBalance(addr, big.NewInt(300))
	if got := db.GetBalance(addr); got.Cmp(big.NewInt(700)) != 0 {
		t.Errorf("balance after sub = %v, want 700", got)
	}

	// Self-destruct.
	db.SelfDestruct(addr)
	if !db.HasSelfDestructed(addr) {
		t.Error("account should be self-destructed")
	}
}

func TestBinaryTrieStateDB_StorageRoundTrip(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0x2222222222222222222222222222222222222222")
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xdeadbeef")

	db.CreateAccount(addr)
	db.SetState(addr, key, val)

	// Read back from dirty storage.
	got := db.GetState(addr, key)
	if got != val {
		t.Errorf("storage = %s, want %s", got, val)
	}

	// Committed state should be empty before commit.
	committed := db.GetCommittedState(addr, key)
	if committed != (types.Hash{}) {
		t.Errorf("committed state = %s, want zero", committed)
	}

	// After commit, committed state should have the value.
	_, err := db.Commit()
	if err != nil {
		t.Fatalf("commit error: %v", err)
	}
	committed = db.GetCommittedState(addr, key)
	if committed != val {
		t.Errorf("committed state after commit = %s, want %s", committed, val)
	}
}

func TestBinaryTrieStateDB_SnapshotRevert(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0x3333333333333333333333333333333333333333")

	db.AddBalance(addr, big.NewInt(500))
	snap := db.Snapshot()

	db.AddBalance(addr, big.NewInt(200))
	if got := db.GetBalance(addr); got.Cmp(big.NewInt(700)) != 0 {
		t.Errorf("balance before revert = %v, want 700", got)
	}

	db.RevertToSnapshot(snap)
	if got := db.GetBalance(addr); got.Cmp(big.NewInt(500)) != 0 {
		t.Errorf("balance after revert = %v, want 500", got)
	}
}

func TestBinaryTrieStateDB_RootDeterministic(t *testing.T) {
	db1 := NewBinaryTrieBackedStateDB()
	db2 := NewBinaryTrieBackedStateDB()

	addr1 := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	addr2 := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	// Same state, different insertion order.
	db1.AddBalance(addr1, big.NewInt(100))
	db1.AddBalance(addr2, big.NewInt(200))

	db2.AddBalance(addr2, big.NewInt(200))
	db2.AddBalance(addr1, big.NewInt(100))

	root1 := db1.GetRoot()
	root2 := db2.GetRoot()
	if root1 != root2 {
		t.Errorf("roots should be equal: %s vs %s", root1, root2)
	}
}

func TestBinaryTrieStateDB_RootChangesOnMutation(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0x4444444444444444444444444444444444444444")

	root1 := db.GetRoot()
	db.AddBalance(addr, big.NewInt(1))
	root2 := db.GetRoot()

	if root1 == root2 {
		t.Error("root should change after adding balance")
	}

	db.SetNonce(addr, 1)
	root3 := db.GetRoot()
	if root2 == root3 {
		t.Error("root should change after setting nonce")
	}
}

func TestBinaryTrieStateDB_RootRepeatable(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0x5555555555555555555555555555555555555555")
	db.AddBalance(addr, big.NewInt(42))

	root1 := db.GetRoot()
	root2 := db.GetRoot()
	if root1 != root2 {
		t.Errorf("repeated root computation differs: %s vs %s", root1, root2)
	}
}

func TestBinaryTrieStateDB_Copy(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0x6666666666666666666666666666666666666666")
	db.AddBalance(addr, big.NewInt(100))

	cp := db.Copy()
	cp.AddBalance(addr, big.NewInt(50))

	// Original should be unchanged.
	if got := db.GetBalance(addr); got.Cmp(big.NewInt(100)) != 0 {
		t.Errorf("original balance = %v, want 100", got)
	}
	if got := cp.GetBalance(addr); got.Cmp(big.NewInt(150)) != 0 {
		t.Errorf("copy balance = %v, want 150", got)
	}
}

// --- Task 2.1.2: StemAccessList tests ---

func TestStemAccessList_ColdWarm(t *testing.T) {
	sal := NewStemAccessList()
	addr := types.HexToAddress("0x1111111111111111111111111111111111111111")

	// First access to address stem should be cold.
	stem := sal.StemForAddress(addr)
	if sal.IsStemWarm(stem) {
		t.Error("stem should be cold on first check")
	}

	alreadyWarm := sal.AddStem(stem)
	if alreadyWarm {
		t.Error("first AddStem should return false (was cold)")
	}

	// Second access should be warm.
	if !sal.IsStemWarm(stem) {
		t.Error("stem should be warm after AddStem")
	}
	alreadyWarm = sal.AddStem(stem)
	if !alreadyWarm {
		t.Error("second AddStem should return true (was warm)")
	}
}

func TestStemAccessList_Slot0SameAsAddress(t *testing.T) {
	sal := NewStemAccessList()
	addr := types.HexToAddress("0x2222222222222222222222222222222222222222")

	addrStem := sal.StemForAddress(addr)
	slot0Stem := sal.StemForSlot(addr, 0)
	slot5Stem := sal.StemForSlot(addr, 5)
	slot63Stem := sal.StemForSlot(addr, 63)

	if addrStem != slot0Stem {
		t.Error("address stem should equal slot 0 stem")
	}
	if addrStem != slot5Stem {
		t.Error("address stem should equal slot 5 stem (header storage)")
	}
	if addrStem != slot63Stem {
		t.Error("address stem should equal slot 63 stem (header storage)")
	}
}

func TestStemAccessList_Slot0ColdThenSlot5Warm(t *testing.T) {
	sal := NewStemAccessList()
	addr := types.HexToAddress("0x3333333333333333333333333333333333333333")

	// Access slot 0 (cold).
	stem0 := sal.StemForSlot(addr, 0)
	if sal.AddStem(stem0) {
		t.Error("slot 0 first access should be cold")
	}

	// Access slot 5 (same stem, should be warm).
	stem5 := sal.StemForSlot(addr, 5)
	if !sal.AddStem(stem5) {
		t.Error("slot 5 should be warm (shares stem with slot 0)")
	}
}

func TestStemAccessList_DifferentAddressCold(t *testing.T) {
	sal := NewStemAccessList()
	addr1 := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	addr2 := types.HexToAddress("0xbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	stem1 := sal.StemForAddress(addr1)
	sal.AddStem(stem1)

	// Different address should be cold.
	stem2 := sal.StemForAddress(addr2)
	if sal.IsStemWarm(stem2) {
		t.Error("different address stem should be cold")
	}
}

func TestStemAccessList_LargeSlotDifferentStem(t *testing.T) {
	sal := NewStemAccessList()
	addr := types.HexToAddress("0x4444444444444444444444444444444444444444")

	addrStem := sal.StemForAddress(addr)
	// Slot 64 is in main storage, should have a different stem.
	slot64Stem := sal.StemForSlot(addr, 64)

	if addrStem == slot64Stem {
		t.Error("slot 64 (main storage) should have different stem from address")
	}
}

func TestStemAccessList_Copy(t *testing.T) {
	sal := NewStemAccessList()
	addr := types.HexToAddress("0x5555555555555555555555555555555555555555")
	stem := sal.StemForAddress(addr)
	sal.AddStem(stem)

	cp := sal.Copy()
	if !cp.IsStemWarm(stem) {
		t.Error("copy should preserve warm stems")
	}

	// Adding to copy should not affect original's count.
	addr2 := types.HexToAddress("0x6666666666666666666666666666666666666666")
	stem2 := cp.StemForAddress(addr2)
	cp.AddStem(stem2)

	if sal.IsStemWarm(stem2) {
		t.Error("original should not be affected by copy mutation")
	}
}

func TestStemAccessList_Reset(t *testing.T) {
	sal := NewStemAccessList()
	addr := types.HexToAddress("0x7777777777777777777777777777777777777777")
	stem := sal.StemForAddress(addr)
	sal.AddStem(stem)

	sal.Reset()
	if sal.IsStemWarm(stem) {
		t.Error("stem should be cold after reset")
	}
}

// --- Task 2.1.3: Commit and persistence tests ---

func TestBinaryTrieStateDB_CommitProducesRoot(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0x7777777777777777777777777777777777777777")
	db.AddBalance(addr, big.NewInt(999))
	db.SetNonce(addr, 3)

	root, err := db.Commit()
	if err != nil {
		t.Fatalf("commit error: %v", err)
	}
	if root == (types.Hash{}) {
		t.Error("committed root should not be zero")
	}

	// Root should match GetRoot after commit.
	got := db.GetRoot()
	if got != root {
		t.Errorf("GetRoot after commit = %s, want %s", got, root)
	}
}

func TestBinaryTrieStateDB_CommitDeterministic(t *testing.T) {
	db1 := NewBinaryTrieBackedStateDB()
	db2 := NewBinaryTrieBackedStateDB()

	addr := types.HexToAddress("0x8888888888888888888888888888888888888888")

	db1.AddBalance(addr, big.NewInt(500))
	db1.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xff"))

	db2.AddBalance(addr, big.NewInt(500))
	db2.SetState(addr, types.HexToHash("0x01"), types.HexToHash("0xff"))

	root1, _ := db1.Commit()
	root2, _ := db2.Commit()

	if root1 != root2 {
		t.Errorf("committed roots should match: %s vs %s", root1, root2)
	}
}

func TestBinaryTrieStateDB_CommitChangesRoot(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0x9999999999999999999999999999999999999999")

	db.AddBalance(addr, big.NewInt(100))
	root1, _ := db.Commit()

	db.AddBalance(addr, big.NewInt(100))
	root2, _ := db.Commit()

	if root1 == root2 {
		t.Error("root should change after second commit with new state")
	}
}

func TestBinaryTrieStateDB_LastCommittedRoot(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	db.AddBalance(addr, big.NewInt(42))

	root, _ := db.Commit()
	if db.LastCommittedRoot() != root {
		t.Errorf("LastCommittedRoot = %s, want %s", db.LastCommittedRoot(), root)
	}
}

func TestBinaryTrieStateDB_NodeDBPopulated(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0xcccccccccccccccccccccccccccccccccccccccc")
	db.AddBalance(addr, big.NewInt(1))

	_, err := db.Commit()
	if err != nil {
		t.Fatalf("commit error: %v", err)
	}

	if len(db.NodeDB()) == 0 {
		t.Error("node DB should be populated after commit")
	}
}

func TestBinaryTrieStateDB_InterfaceCompliance(t *testing.T) {
	// Compile-time check is in the source file, but verify at runtime too.
	var _ StateDB = NewBinaryTrieBackedStateDB()
}

func TestBinaryTrieStateDB_AccessListOperations(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0xdddddddddddddddddddddddddddddddddddddd")
	slot := types.HexToHash("0x01")

	db.AddAddressToAccessList(addr)
	if !db.AddressInAccessList(addr) {
		t.Error("address should be in access list")
	}

	db.AddSlotToAccessList(addr, slot)
	addrOk, slotOk := db.SlotInAccessList(addr, slot)
	if !addrOk || !slotOk {
		t.Error("address and slot should be in access list")
	}
}

func TestBinaryTrieStateDB_TransientStorage(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	addr := types.HexToAddress("0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee")
	key := types.HexToHash("0x01")
	val := types.HexToHash("0xab")

	db.SetTransientState(addr, key, val)
	if got := db.GetTransientState(addr, key); got != val {
		t.Errorf("transient state = %s, want %s", got, val)
	}

	db.ClearTransientStorage()
	if got := db.GetTransientState(addr, key); got != (types.Hash{}) {
		t.Error("transient state should be cleared")
	}
}

func TestBinaryTrieStateDB_Logs(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	txHash := types.HexToHash("0xf00d")
	db.SetTxContext(txHash, 0)

	db.AddLog(&types.Log{Address: types.HexToAddress("0x01")})
	logs := db.GetLogs(txHash)
	if len(logs) != 1 {
		t.Errorf("log count = %d, want 1", len(logs))
	}
}

func TestBinaryTrieStateDB_Refund(t *testing.T) {
	db := NewBinaryTrieBackedStateDB()
	db.AddRefund(100)
	if db.GetRefund() != 100 {
		t.Errorf("refund = %d, want 100", db.GetRefund())
	}
	db.SubRefund(30)
	if db.GetRefund() != 70 {
		t.Errorf("refund = %d, want 70", db.GetRefund())
	}
}

func TestStemAccessList_CodeAndSlot0ShareStem(t *testing.T) {
	sal := NewStemAccessList()
	addr := types.HexToAddress("0xaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")

	// Code chunks 0-127 share the same stem as basic data and slots 0-63.
	addrStem := sal.StemForAddress(addr)
	sal.AddStem(addrStem)

	// Slot 0 should be warm (same stem as address).
	slot0Stem := sal.StemForSlot(addr, 0)
	if !sal.IsStemWarm(slot0Stem) {
		t.Error("slot 0 should share stem with address (warm after address access)")
	}
}

func TestStemAccessList_GasConstants(t *testing.T) {
	if StemColdAccessGas != 2600 {
		t.Errorf("StemColdAccessGas = %d, want 2600", StemColdAccessGas)
	}
	if StemWarmAccessGas != 100 {
		t.Errorf("StemWarmAccessGas = %d, want 100", StemWarmAccessGas)
	}
}
