package chain

import (
	"fmt"

	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
)

// InsertSyncedBlock validates and stores a downloaded block without publishing
// it as canonical. Only the completed CL target is selected by ApplyForkchoice.
func (bc *Blockchain) InsertSyncedBlock(block *types.Block) error {
	bc.insertMu.Lock()
	defer bc.insertMu.Unlock()
	var result *execution.ProcessResult
	return bc.insertBlock(block, &result, true)
}

// ApplyForkchoice atomically publishes a fully validated CL-nominated head,
// safe block and finalized block (EIP-3675; Engine Paris forkchoice rules).
func (bc *Blockchain) ApplyForkchoice(headHash, safeHash, finalHash types.Hash) error {
	bc.insertMu.Lock()
	defer bc.insertMu.Unlock()
	head := bc.GetBlock(headHash)
	if head == nil {
		return fmt.Errorf("forkchoice: unknown head")
	}
	var safe, final *types.Block
	if safeHash != (types.Hash{}) {
		safe = bc.GetBlock(safeHash)
		if safe == nil {
			return fmt.Errorf("forkchoice: unknown safe block")
		}
	}
	if finalHash != (types.Hash{}) {
		final = bc.GetBlock(finalHash)
		if final == nil {
			return fmt.Errorf("forkchoice: unknown finalized block")
		}
	}
	prior := bc.CurrentFinalBlock()
	if prior != nil && final != nil && final.NumberU64() < prior.NumberU64() {
		return fmt.Errorf("forkchoice: finalized block regresses")
	}
	ancestors := make(map[types.Hash]*types.Block)
	minimum := head.NumberU64()
	for _, required := range []*types.Block{safe, final, prior} {
		if required != nil && required.NumberU64() < minimum {
			minimum = required.NumberU64()
		}
	}
	for block := head; block != nil; block = bc.GetBlock(block.ParentHash()) {
		ancestors[block.Hash()] = block
		canonical, err := rawdb.ReadCanonicalHash(bc.db, block.NumberU64())
		if block.NumberU64() <= minimum && err == nil && canonical == block.Hash() {
			break
		}
		if block.NumberU64() == 0 {
			break
		}
	}
	for _, required := range []*types.Block{safe, final, prior} {
		if required != nil && ancestors[required.Hash()] == nil {
			return fmt.Errorf("forkchoice: checkpoint is not a head ancestor")
		}
	}
	st, err := bc.StateAtBlock(head)
	if err != nil {
		return err
	}
	if _, err := st.Commit(); err != nil {
		return err
	}
	for _, block := range ancestors {
		if receipts := bc.GetReceipts(block.Hash()); receipts != nil {
			bc.writeReceipts(block.NumberU64(), block.Hash(), receipts)
		}
		bc.writeTxLookups(block.Transactions(), block.NumberU64())
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()
	batch := bc.db.NewBatch()
	for _, block := range ancestors {
		if err := rawdb.WriteCanonicalHash(batch, block.NumberU64(), block.Hash()); err != nil {
			return err
		}
	}
	for number := head.NumberU64() + 1; number <= bc.currentBlock.NumberU64(); number++ {
		if err := rawdb.DeleteCanonicalHash(batch, number); err != nil {
			return err
		}
	}
	if err := rawdb.WriteHeadBlockHash(batch, headHash); err != nil {
		return err
	}
	if err := rawdb.WriteHeadHeaderHash(batch, headHash); err != nil {
		return err
	}
	if final != nil {
		if err := rawdb.WriteFinalizedBlockHash(batch, finalHash); err != nil {
			return err
		}
	}
	if err := batch.Write(); err != nil {
		return err
	}
	for _, block := range ancestors {
		bc.canonCache[block.NumberU64()] = block.Hash()
	}
	for number := range bc.canonCache {
		if number > head.NumberU64() {
			delete(bc.canonCache, number)
		}
	}
	bc.currentBlock, bc.currentState = head, st
	if safe != nil {
		bc.currentSafeBlock = safe
	}
	if final != nil {
		bc.currentFinalBlock = final
	}
	bc.sc.put(headHash, head.NumberU64(), st)
	bc.sc.protect(headHash)
	bc.memSC.put(headHash, head.NumberU64(), st)
	bc.memSC.protect(headHash)
	bc.hc.mu.Lock()
	for _, block := range ancestors {
		bc.hc.headers[block.NumberU64()] = block.Header()
		bc.hc.headersByHash[block.Hash()] = block.Header()
	}
	bc.hc.currentHeader = head.Header()
	bc.hc.mu.Unlock()
	return nil
}
