package statesync

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
	gethtrie "github.com/ethereum/go-ethereum/trie"
)

const trieNodeBatchLimit = 4 * 1024 * 1024

// SnapshotWriter stages one fixed pivot. It only publishes the snapshot after
// reconstructing and verifying all account/storage roots and code hashes.
type SnapshotWriter struct {
	db       rawdb.Database
	snapshot *state.StateSnapshot
}

func NewSnapshotWriter(db rawdb.Database) *SnapshotWriter {
	return &SnapshotWriter{db, state.NewStateSnapshot()}
}
func (w *SnapshotWriter) WriteAccount(hash types.Hash, account AccountData) error {
	if account.Balance == nil || account.Balance.Sign() < 0 || account.Balance.BitLen() > 256 {
		return fmt.Errorf("snap: invalid balance")
	}
	body, err := rlp.EncodeToBytes([]any{account.Nonce, account.Balance, account.Root, account.CodeHash})
	if err != nil {
		return err
	}
	w.snapshot.Accounts[hash] = body
	return nil
}
func (w *SnapshotWriter) WriteStorage(account, slot types.Hash, value []byte) error {
	if _, ok := w.snapshot.Accounts[account]; !ok {
		return fmt.Errorf("snap: storage without verified account")
	}
	if w.snapshot.Storage[account] == nil {
		w.snapshot.Storage[account] = make(map[types.Hash][]byte)
	}
	w.snapshot.Storage[account][slot] = bytes.Clone(value)
	return nil
}
func (w *SnapshotWriter) WriteBytecode(hash types.Hash, code []byte) error {
	return w.WriteBytecodes([]BytecodeData{{Hash: hash, Code: code}})
}

// WriteBytecodes persists hash-verified code independently of any pivot. A
// future attempt may reuse it, even if the current snapshot is never adopted.
func (w *SnapshotWriter) WriteBytecodes(codes []BytecodeData) error {
	batch := w.db.NewBatch()
	for _, item := range codes {
		if crypto.Keccak256Hash(item.Code) != item.Hash {
			return fmt.Errorf("snap: code hash mismatch")
		}
		if err := batch.Put(append([]byte("snap-code/"), item.Hash[:]...), item.Code); err != nil {
			return err
		}
	}
	if len(codes) != 0 {
		if err := batch.Write(); err != nil {
			return err
		}
	}
	for _, item := range codes {
		w.snapshot.Codes[item.Hash] = bytes.Clone(item.Code)
	}
	return nil
}
func (w *SnapshotWriter) HasBytecode(hash types.Hash) bool {
	if _, ok := w.snapshot.Codes[hash]; ok {
		return true
	}
	code, err := w.db.Get(append([]byte("snap-code/"), hash[:]...))
	if err != nil || crypto.Keccak256Hash(code) != hash {
		return false
	}
	w.snapshot.Codes[hash] = bytes.Clone(code)
	return true
}

// A fixed-pivot complete range download reconstructs intermediate trie nodes
// locally. Finalize verifies completeness before storing those nodes.
func (w *SnapshotWriter) MissingTrieNodes(types.Hash, int) [][]byte { return nil }
func (w *SnapshotWriter) HasTrieNode(path []byte) bool {
	ok, _ := w.db.Has(append([]byte("t"), path...))
	return ok
}
func (w *SnapshotWriter) WriteTrieNode(path, data []byte) error {
	if len(path) != 32 || crypto.Keccak256Hash(data) != types.BytesToHash(path) {
		return fmt.Errorf("snap: unexpected trie node")
	}
	return w.db.Put(append([]byte("t"), path...), data)
}
func (w *SnapshotWriter) Finalize(root types.Hash) error {
	if err := w.snapshot.Validate(root); err != nil {
		return err
	}
	if err := w.writeTrie(w.snapshot.Accounts, root); err != nil {
		return err
	}
	for account, slots := range w.snapshot.Storage {
		var decoded gethtypes.StateAccount
		if err := gethrlp.DecodeBytes(w.snapshot.Accounts[account], &decoded); err != nil {
			return err
		}
		if err := w.writeTrie(slots, types.Hash(decoded.Root)); err != nil {
			return err
		}
	}
	return state.StoreStateSnapshot(w.db, root, w.snapshot)
}

// writeTrie streams completed nodes to bounded batches. Leaves still reside in
// the current snapshot and sorting retains one hash per leaf; neither the trie
// nodes nor the write batch grow with the full state size.
func (w *SnapshotWriter) writeTrie(entries map[types.Hash][]byte, expected types.Hash) error {
	keys := make([]types.Hash, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i][:], keys[j][:]) < 0 })
	batch := w.db.NewBatch()
	var writeErr error
	tr := gethtrie.NewStackTrie(func(_ []byte, hash gethcommon.Hash, blob []byte) {
		if writeErr != nil {
			return
		}
		key := append([]byte("t"), hash[:]...)
		writeErr = batch.Put(key, bytes.Clone(blob))
		if writeErr == nil && batch.ValueSize() >= trieNodeBatchLimit {
			writeErr = batch.Write()
			batch.Reset()
		}
	})
	for _, key := range keys {
		if err := tr.Update(key[:], entries[key]); err != nil {
			return err
		}
		if writeErr != nil {
			return writeErr
		}
	}
	actual := types.Hash(tr.Hash())
	if writeErr != nil {
		return writeErr
	}
	if actual != expected {
		return fmt.Errorf("snap: streamed trie root mismatch")
	}
	if batch.ValueSize() > 0 {
		return batch.Write()
	}
	return nil
}
