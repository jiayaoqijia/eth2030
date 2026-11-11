package eips

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// EIP-7002: Execution Layer Triggerable Withdrawals
//
// This file implements the withdrawal request queue and fee mechanism
// defined in EIP-7002. Validators can trigger exits and partial
// withdrawals via their execution layer (0x01) withdrawal credentials.

// System contract address for EIP-7002 withdrawal requests.
var WithdrawalRequestContract = types.WithdrawalRequestAddress

// Storage slot layout for the withdrawal request contract.
const (
	ExcessWithdrawalRequestsStorageSlot   = 0
	WithdrawalRequestCountStorageSlot     = 1
	WithdrawalRequestQueueHeadStorageSlot = 2
	WithdrawalRequestQueueTailStorageSlot = 3
	WithdrawalRequestQueueStorageOffset   = 4
)

// EIP-7002 protocol parameters.
const (
	MaxWithdrawalRequestsPerBlock      = 16
	TargetWithdrawalRequestsPerBlock   = 2
	MinWithdrawalRequestFee            = 1 // wei
	WithdrawalRequestFeeUpdateFraction = 17
)

// Errors for withdrawal request processing.
var (
	ErrWithdrawalRequestEmptyPubkey     = errors.New("withdrawal request: empty validator pubkey")
	ErrWithdrawalRequestFeeInsufficient = errors.New("withdrawal request: insufficient fee")
)

// WithdrawalRequestQueue holds queued withdrawal requests for processing.
type WithdrawalRequestQueue struct {
	Requests []types.WithdrawalRequest
	Fee      *big.Int
}

// CalcWithdrawalFee computes the current withdrawal request fee using the
// EIP-1559-style fake exponential: fee = MIN_FEE * e^(excess / UPDATE_FRACTION).
func CalcWithdrawalFee(excessRequests uint64) *big.Int {
	return gas.FakeExponentialV2(
		big.NewInt(MinWithdrawalRequestFee),
		new(big.Int).SetUint64(excessRequests),
		big.NewInt(WithdrawalRequestFeeUpdateFraction),
	)
}

// UpdateExcessWithdrawalRequests calculates the new excess withdrawal requests
// after processing a block, using the same accumulation logic as EIP-1559 blob gas.
func UpdateExcessWithdrawalRequests(previousExcess, count uint64) uint64 {
	if previousExcess+count > TargetWithdrawalRequestsPerBlock {
		return previousExcess + count - TargetWithdrawalRequestsPerBlock
	}
	return 0
}

// ProcessWithdrawalRequests reads queued withdrawal requests from the system
// contract's storage and returns up to MaxWithdrawalRequestsPerBlock requests.
// It updates the queue head/tail pointers and excess/count in state.
func ProcessWithdrawalRequests(statedb state.StateDB, header *types.Header) []types.WithdrawalRequest {
	addr := WithdrawalRequestContract

	// Read queue head and tail indices.
	headSlot := types.Hash{}
	headSlot[31] = WithdrawalRequestQueueHeadStorageSlot
	tailSlot := types.Hash{}
	tailSlot[31] = WithdrawalRequestQueueTailStorageSlot

	headHash := statedb.GetState(addr, headSlot)
	tailHash := statedb.GetState(addr, tailSlot)
	queueHead := hashToUint64V2(headHash)
	queueTail := hashToUint64V2(tailHash)

	numInQueue := queueTail - queueHead
	numDequeued := numInQueue
	if numDequeued > MaxWithdrawalRequestsPerBlock {
		numDequeued = MaxWithdrawalRequestsPerBlock
	}

	requests := make([]types.WithdrawalRequest, 0, numDequeued)
	for i := uint64(0); i < numDequeued; i++ {
		storageSlot := WithdrawalRequestQueueStorageOffset + (queueHead+i)*3

		// Slot 0: source address (in low 20 bytes of 32-byte word)
		slot0Key := uint64ToHash(storageSlot)
		slot0Val := statedb.GetState(addr, slot0Key)
		var sourceAddr types.Address
		copy(sourceAddr[:], slot0Val[12:32]) // address is right-aligned

		// Slot 1: first 32 bytes of validator pubkey
		slot1Key := uint64ToHash(storageSlot + 1)
		slot1Val := statedb.GetState(addr, slot1Key)

		// Slot 2: last 16 bytes of pubkey + 8 bytes amount (little-endian)
		slot2Key := uint64ToHash(storageSlot + 2)
		slot2Val := statedb.GetState(addr, slot2Key)

		var pubkey [48]byte
		copy(pubkey[0:32], slot1Val[:])
		copy(pubkey[32:48], slot2Val[0:16])

		amount := littleEndianToUint64(slot2Val[16:24])

		requests = append(requests, types.WithdrawalRequest{
			SourceAddress:   sourceAddr,
			ValidatorPubkey: pubkey,
			Amount:          amount,
		})
	}

	// Update queue pointers.
	newHead := queueHead + numDequeued
	if newHead == queueTail {
		// Queue is empty, reset both pointers.
		statedb.SetState(addr, headSlot, types.Hash{})
		statedb.SetState(addr, tailSlot, types.Hash{})
	} else {
		statedb.SetState(addr, headSlot, uint64ToHash(newHead))
	}

	// Update excess withdrawal requests.
	excessSlot := types.Hash{}
	excessSlot[31] = ExcessWithdrawalRequestsStorageSlot
	countSlot := types.Hash{}
	countSlot[31] = WithdrawalRequestCountStorageSlot

	excessHash := statedb.GetState(addr, excessSlot)
	countHash := statedb.GetState(addr, countSlot)
	previousExcess := hashToUint64V2(excessHash)
	count := hashToUint64V2(countHash)

	newExcess := UpdateExcessWithdrawalRequests(previousExcess, count)
	statedb.SetState(addr, excessSlot, uint64ToHash(newExcess))

	// Reset count to 0.
	statedb.SetState(addr, countSlot, types.Hash{})

	return requests
}

// ValidateWithdrawalRequest checks that a withdrawal request is well-formed.
func ValidateWithdrawalRequest(req *types.WithdrawalRequest) error {
	// Pubkey must not be all zeros.
	empty := [48]byte{}
	if req.ValidatorPubkey == empty {
		return ErrWithdrawalRequestEmptyPubkey
	}
	return nil
}

// AddWithdrawalRequest appends a withdrawal request to the queue after
// validating it.
func AddWithdrawalRequest(queue *WithdrawalRequestQueue, req types.WithdrawalRequest) error {
	if err := ValidateWithdrawalRequest(&req); err != nil {
		return err
	}
	queue.Requests = append(queue.Requests, req)
	return nil
}

// hashToUint64V2 interprets the low 8 bytes of a hash as a big-endian uint64.
// Uses a different name to avoid conflict with beacon_root.go's uint64ToHash.
func hashToUint64V2(h types.Hash) uint64 {
	var result uint64
	for i := 24; i < 32; i++ {
		result = result<<8 | uint64(h[i])
	}
	return result
}

// littleEndianToUint64 reads 8 bytes as a little-endian uint64.
func littleEndianToUint64(data []byte) uint64 {
	var result uint64
	for i := 7; i >= 0; i-- {
		result = result<<8 | uint64(data[i])
	}
	return result
}
