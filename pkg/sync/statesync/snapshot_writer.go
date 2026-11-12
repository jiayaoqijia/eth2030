package statesync

import (
	"bytes"
	"fmt"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/trie/mpt"
)

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
	if crypto.Keccak256Hash(code) != hash {
		return fmt.Errorf("snap: code hash mismatch")
	}
	w.snapshot.Codes[hash] = bytes.Clone(code)
	return nil
}
func (w *SnapshotWriter) HasBytecode(hash types.Hash) bool {
	_, ok := w.snapshot.Codes[hash]
	return ok
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
	nodes := mpt.NewNodeDatabase(nil)
	tries := []map[types.Hash][]byte{w.snapshot.Accounts}
	for _, slots := range w.snapshot.Storage {
		tries = append(tries, slots)
	}
	for _, entries := range tries {
		tr := mpt.New()
		for k, v := range entries {
			if err := tr.Put(k[:], v); err != nil {
				return err
			}
		}
		if _, err := mpt.CommitTrie(tr, nodes); err != nil {
			return err
		}
	}
	if err := nodes.Commit(mpt.NewRawDBNodeWriter(w.db.Put)); err != nil {
		return err
	}
	return state.StoreStateSnapshot(w.db, root, w.snapshot)
}
