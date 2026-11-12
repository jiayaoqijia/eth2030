package chain

import (
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
)

// Headers remain available independently of bodies, including the authenticated
// BLOCKHASH window below a snap pivot.
func (bc *Blockchain) GetHeaderByHash(hash types.Hash) *types.Header {
	bc.mu.RLock()
	defer bc.mu.RUnlock()
	if block := bc.blockCache[hash]; block != nil {
		return block.Header()
	}
	number, err := rawdb.ReadHeaderNumber(bc.db, hash)
	if err != nil {
		return nil
	}
	encoded, err := rawdb.ReadHeader(bc.db, number, hash)
	if err != nil {
		return nil
	}
	header, err := types.DecodeHeaderRLP(encoded)
	if err != nil || header.Hash() != hash || header.Number == nil || !header.Number.IsUint64() || header.Number.Uint64() != number {
		return nil
	}
	return header
}

func (bc *Blockchain) GetHeaderByNumber(number uint64) *types.Header {
	bc.mu.RLock()
	hash, ok := bc.canonCache[number]
	bc.mu.RUnlock()
	if !ok {
		var err error
		hash, err = rawdb.ReadCanonicalHash(bc.db, number)
		if err != nil {
			return nil
		}
	}
	return bc.GetHeaderByHash(hash)
}
