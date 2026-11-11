package eips

import (
	"encoding/binary"

	"github.com/eth2030/eth2030/core/types"
)

const (
	// historyBufferLength is the ring buffer size for the beacon root contract.
	// Per EIP-4788, the contract stores the last 8191 beacon block roots.
	historyBufferLength = 8191

	// BeaconHistoryBufferLength is the exported ring buffer size for callers
	// outside the eips package that need to compute slot indices.
	BeaconHistoryBufferLength = historyBufferLength
)

// BeaconRootAddress is the address of the EIP-4788 beacon block root contract.
// This system contract stores parent beacon block roots so the EVM can access them.
var BeaconRootAddress = types.HexToAddress("0x000F3df6D732807Ef1319fB7B8bB8522d0Beac02")

type systemStorage interface {
	SetState(types.Address, types.Hash, types.Hash)
}

// ProcessBeaconBlockRoot stores the parent beacon block root into the beacon
// root system contract at the start of block processing. This implements
// EIP-4788: Beacon block root in the EVM.
//
// The contract uses a ring buffer with HISTORY_BUFFER_LENGTH=8191 entries:
//   - timestamp_idx = header.Time % HISTORY_BUFFER_LENGTH
//   - root_idx = timestamp_idx + HISTORY_BUFFER_LENGTH
//   - Slot[timestamp_idx] stores header.Time
//   - Slot[root_idx] stores the parent beacon block root
//
// The call is made as a system call with caller = SystemAddress (0xff...fe).
// It does not consume block gas and is not a user-initiated transaction.
func ProcessBeaconBlockRoot(statedb systemStorage, header *types.Header) {
	if header.ParentBeaconRoot == nil {
		return
	}

	// Compute ring buffer indices.
	timestampIdx := header.Time % historyBufferLength
	rootIdx := timestampIdx + historyBufferLength

	// Convert indices to storage slot keys (big-endian uint256).
	timestampSlot := uint64ToHash(timestampIdx)
	rootSlot := uint64ToHash(rootIdx)

	// Store the block timestamp at the timestamp slot.
	timestampValue := uint64ToHash(header.Time)
	statedb.SetState(BeaconRootAddress, timestampSlot, timestampValue)

	// Store the parent beacon block root at the root slot.
	statedb.SetState(BeaconRootAddress, rootSlot, *header.ParentBeaconRoot)
}

// uint64ToHash converts a uint64 to a 32-byte big-endian hash (left-padded).
func uint64ToHash(v uint64) types.Hash {
	var h types.Hash
	binary.BigEndian.PutUint64(h[24:], v)
	return h
}

// Uint64ToHash is the exported version of uint64ToHash for callers outside
// the eips package that need to compute slot keys for system contracts.
func Uint64ToHash(v uint64) types.Hash {
	return uint64ToHash(v)
}
