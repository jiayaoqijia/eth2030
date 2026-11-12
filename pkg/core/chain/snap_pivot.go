package chain

import (
	"fmt"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

var snapPivotKey = []byte("snap-pivot")

// ImportSnapPivot adopts a CL-finalized pivot after state download. history
// contains up to 256 parents in descending order for BLOCKHASH. This is not a
// general unvalidated-block insertion API: callers must obtain the pivot hash
// from their trusted consensus layer, never from a peer's advertised head.
func (bc *Blockchain) ImportSnapPivot(pivot *types.Block, history []*types.Header) error {
	bc.insertMu.Lock()
	defer bc.insertMu.Unlock()
	if pivot == nil || pivot.Header().Number == nil || !pivot.Header().Number.IsUint64() || pivot.NumberU64() == 0 {
		return fmt.Errorf("snap: invalid pivot")
	}
	if err := bc.validator.ValidateBody(pivot); err != nil {
		return err
	}
	expected := pivot.NumberU64()
	if expected > 256 {
		expected = 256
	}
	if uint64(len(history)) != expected {
		return fmt.Errorf("snap: incomplete BLOCKHASH ancestry")
	}
	child := pivot.Header()
	for _, parent := range history {
		if parent == nil || parent.Number == nil || !parent.Number.IsUint64() || parent.Hash() != child.ParentHash {
			return fmt.Errorf("snap: invalid pivot ancestry")
		}
		if err := bc.validator.ValidateHeader(child, parent); err != nil {
			return err
		}
		child = parent
	}
	if child.Number.Sign() == 0 && child.Hash() != bc.genesis.Hash() {
		return fmt.Errorf("snap: wrong genesis")
	}
	st, err := state.OpenSnapState(bc.db, pivot.Header().Root)
	if err != nil {
		return err
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if bc.currentBlock.NumberU64() != 0 {
		return fmt.Errorf("snap: bootstrap requires a genesis-only chain")
	}
	batch := bc.db.NewBatch()
	headers := append([]*types.Header{pivot.Header()}, history...)
	for _, header := range headers {
		encoded, err := header.EncodeRLP()
		if err != nil {
			return err
		}
		if err := rawdb.WriteHeader(batch, header.Number.Uint64(), header.Hash(), encoded); err != nil {
			return err
		}
		if err := rawdb.WriteCanonicalHash(batch, header.Number.Uint64(), header.Hash()); err != nil {
			return err
		}
	}
	body, err := encodeBlockBody(pivot.Body())
	if err != nil {
		return err
	}
	if err := rawdb.WriteBody(batch, pivot.NumberU64(), pivot.Hash(), body); err != nil {
		return err
	}
	if err := rawdb.WriteHeadBlockHash(batch, pivot.Hash()); err != nil {
		return err
	}
	if err := rawdb.WriteHeadHeaderHash(batch, pivot.Hash()); err != nil {
		return err
	}
	if err := batch.Put(validatedKey(pivot.Hash()), []byte{1}); err != nil {
		return err
	}
	if err := batch.Put(snapPivotKey, pivot.Hash().Bytes()); err != nil {
		return err
	}
	if err := rawdb.WriteFinalizedBlockHash(batch, pivot.Hash()); err != nil {
		return err
	}
	if err := batch.Write(); err != nil {
		return err
	}
	bc.adoptSnapPivot(pivot, st, headers)
	bc.currentFinalBlock = pivot
	return nil
}

func (bc *Blockchain) adoptSnapPivot(pivot *types.Block, st state.StateDB, headers []*types.Header) {
	bc.currentBlock, bc.currentState = pivot, st
	bc.blockCache[pivot.Hash()] = pivot
	bc.blockCacheOrder = append(bc.blockCacheOrder, pivot.Hash())
	for _, header := range headers {
		bc.canonCache[header.Number.Uint64()] = header.Hash()
	}
	bc.sc.put(pivot.Hash(), pivot.NumberU64(), st)
	bc.sc.protect(pivot.Hash())
	bc.memSC.put(pivot.Hash(), pivot.NumberU64(), st)
	bc.memSC.protect(pivot.Hash())
	bc.hc.mu.Lock()
	for _, header := range headers {
		bc.hc.headers[header.Number.Uint64()] = header
		bc.hc.headersByHash[header.Hash()] = header
	}
	bc.hc.currentHeader = pivot.Header()
	bc.hc.mu.Unlock()
}

// RestoreSnapPivot restores the verified checkpoint on restart, then replays
// subsequent persisted canonical blocks through normal execution validation.
func (bc *Blockchain) RestoreSnapPivot() error {
	encoded, err := bc.db.Get(snapPivotKey)
	if err == rawdb.ErrNotFound {
		return bc.replayStoredAfter(bc.genesis)
	}
	if err != nil {
		return err
	}
	if len(encoded) != 32 {
		return fmt.Errorf("snap: corrupt pivot marker")
	}
	pivot := bc.GetBlock(types.BytesToHash(encoded))
	if pivot == nil {
		return fmt.Errorf("snap: missing persisted pivot")
	}
	st, err := state.OpenSnapState(bc.db, pivot.Header().Root)
	if err != nil {
		return err
	}
	headers := []*types.Header{pivot.Header()}
	for n := pivot.NumberU64(); n > 0 && len(headers) <= 256; n-- {
		hash, err := rawdb.ReadCanonicalHash(bc.db, n-1)
		if err != nil {
			return err
		}
		data, err := rawdb.ReadHeader(bc.db, n-1, hash)
		if err != nil {
			return err
		}
		header, err := types.DecodeHeaderRLP(data)
		if err != nil {
			return err
		}
		if header.Number == nil || header.Number.Uint64() != n-1 || header.Hash() != hash || headers[len(headers)-1].ParentHash != header.Hash() {
			return fmt.Errorf("snap: corrupt persisted ancestry")
		}
		headers = append(headers, header)
	}
	if err := bc.validator.ValidateBody(pivot); err != nil {
		return err
	}
	if err := bc.db.Put(validatedKey(pivot.Hash()), []byte{1}); err != nil {
		return err
	}
	bc.mu.Lock()
	bc.adoptSnapPivot(pivot, st, headers)
	bc.mu.Unlock()
	return bc.replayStoredAfter(pivot)
}

func (bc *Blockchain) replayStoredAfter(pivot *types.Block) error {
	headHash, err := rawdb.ReadHeadBlockHash(bc.db)
	if err != nil {
		return err
	}
	head := bc.GetBlock(headHash)
	if head == nil {
		return fmt.Errorf("sync: missing saved head")
	}
	if head.NumberU64() < pivot.NumberU64() {
		return fmt.Errorf("sync: saved head precedes pivot")
	}
	// Recovery may populate validated side data, but never overwrites the
	// durable canonical target before the complete original chain is checked.
	parent := pivot
	for number := pivot.NumberU64(); number < head.NumberU64(); {
		number++
		hash, err := rawdb.ReadCanonicalHash(bc.db, number)
		if err != nil {
			return err
		}
		block := bc.GetBlock(hash)
		if block == nil || block.ParentHash() != parent.Hash() {
			return fmt.Errorf("sync: missing or disconnected replay block %d", number)
		}
		bc.EvictBlockFromCache(hash)
		// Force execution even when a validation marker survived a prior run.
		bc.sc.remove(hash)
		bc.memSC.remove(hash)
		if err := bc.InsertSyncedBlock(block); err != nil {
			return err
		}
		parent = block
	}
	final := rawdb.ReadFinalizedBlockHash(bc.db)
	return bc.ApplyForkchoice(head.Hash(), final, final)
}
