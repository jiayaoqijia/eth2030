package eips

import (
	"math/big"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// EIP-2935: Serve historical block hashes from state.
//
// A system contract at HistoryStorageAddress stores the last
// HISTORY_SERVE_WINDOW block hashes. At the start of processing each block,
// the parent block's hash is stored in the contract's storage at:
//   slot = parent.Number % HISTORY_SERVE_WINDOW
//
// This enables the BLOCKHASH opcode to serve hashes beyond the 256-block
// limit by reading from the system contract's storage.

const (
	// HistoryServeWindow is the number of historical block hashes stored.
	// Per EIP-2935 (updated in Prague): 8192 slots.
	HistoryServeWindow = 8192
)

// HistoryStorageAddress is the system contract address for EIP-2935.
// This is the address specified in EIP-2935 for the history storage contract.
var HistoryStorageAddress = types.HexToAddress("0x0F792be4B0c0cb4DAE440Ef133E90C0eCD48CCCC")

// ProcessParentBlockHash stores the parent block hash in the history
// storage contract as specified by EIP-2935. This should be called at the
// start of block processing, before executing any transactions.
//
// The parent's hash is stored at slot = parentNumber % HISTORY_SERVE_WINDOW.
// If the contract does not exist yet, it is created with empty code.
func ProcessParentBlockHash(statedb systemStorage, parentNumber uint64, parentHash types.Hash) {

	// Store the parent hash at slot = parentNumber % HISTORY_SERVE_WINDOW.
	slot := new(big.Int).SetUint64(parentNumber % HistoryServeWindow)
	var slotHash types.Hash
	slot.FillBytes(slotHash[32-len(slot.Bytes()):])

	statedb.SetState(HistoryStorageAddress, slotHash, parentHash)
}

// GetHistoricalBlockHash retrieves a historical block hash from the EIP-2935
// system contract. Returns the zero hash if the requested block number is not
// available or the contract doesn't exist.
func GetHistoricalBlockHash(statedb state.StateDB, blockNumber uint64) types.Hash {
	if !statedb.Exist(HistoryStorageAddress) {
		return types.Hash{}
	}

	slot := new(big.Int).SetUint64(blockNumber % HistoryServeWindow)
	var slotHash types.Hash
	slot.FillBytes(slotHash[32-len(slot.Bytes()):])

	return statedb.GetState(HistoryStorageAddress, slotHash)
}
