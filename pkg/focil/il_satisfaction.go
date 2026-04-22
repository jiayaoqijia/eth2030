package focil

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// InclusionListUnsatisfied is the status returned by engine_newPayload when a
// valid IL transaction is absent from the block (EIP-7805 §engine-api).
const InclusionListUnsatisfied = "INCLUSION_LIST_UNSATISFIED"

// ILSatisfactionResult indicates whether the inclusion list was satisfied.
type ILSatisfactionResult int

const (
	// ILSatisfied means all IL txs are either in the block or exempt.
	ILSatisfied ILSatisfactionResult = iota
	// ILUnsatisfied means a valid IL tx was absent and gas was available.
	ILUnsatisfied
)

// PostStateReader provides account state needed for IL satisfaction checks.
type PostStateReader interface {
	GetNonce(addr [20]byte) uint64
	GetBalance(addr [20]byte) uint64
}

// FrameVerifier provides code-existence checks for EIP-8141 frame transaction
// VERIFY targets. If nil, frame tx VERIFY validation is skipped (all frame txs
// are treated as EOA-equivalent for IL purposes).
type FrameVerifier interface {
	// GetCodeSize returns the byte length of contract code at addr.
	// Returns 0 for EOAs and non-existent accounts.
	GetCodeSize(addr types.Address) int
}

// CheckILSatisfaction implements the EIP-7805 §satisfaction algorithm with
// EIP-8141 frame transaction awareness:
//
// For each tx T in ILs:
//  1. If T is in block → skip (satisfied).
//  2. If gasRemaining < T.gasLimit → skip (gas exemption).
//  3. Validate T's state validity against postState:
//     a. For EOA txs: check nonce and balance.
//     b. For frame txs (type 0x06): check nonce, skip balance check (payer
//     determined at APPROVE time), and verify VERIFY target(s) have code.
//     If state-invalid → exempt. If valid but T absent → ILUnsatisfied.
//
// The frame tx balance check is intentionally skipped because EIP-8141 defers
// fee collection to the APPROVE opcode, which may designate a payer different
// from the sender. The sender may have zero balance and the tx is still valid
// if the payer (determined during VERIFY frame execution) has sufficient funds.
//
// The VERIFY code-existence check catches the most common invalidation vector:
// a frame tx whose VERIFY target has no code cannot call APPROVE, making the
// tx consensus-invalid. Without this check, the builder would be penalized for
// not including an unincludable transaction.
func CheckILSatisfaction(block *types.Block, ils []*InclusionList, postState PostStateReader, gasRemaining uint64) ILSatisfactionResult {
	return CheckILSatisfactionWithVerifier(block, ils, postState, nil, gasRemaining)
}

// CheckILSatisfactionWithVerifier extends CheckILSatisfaction with an optional
// FrameVerifier for EIP-8141 frame transaction VERIFY target validation.
func CheckILSatisfactionWithVerifier(block *types.Block, ils []*InclusionList, postState PostStateReader, verifier FrameVerifier, gasRemaining uint64) ILSatisfactionResult {
	// Build set of tx hashes in the block.
	blockTxHashes := make(map[types.Hash]bool, len(block.Transactions()))
	for _, tx := range block.Transactions() {
		blockTxHashes[tx.Hash()] = true
	}

	for _, il := range ils {
		for _, entry := range il.Entries {
			tx, err := types.DecodeTxRLP(entry.Transaction)
			if err != nil {
				// Invalid tx — skip (per spec).
				continue
			}
			// Rule 1: tx in block → satisfied.
			if blockTxHashes[tx.Hash()] {
				continue
			}
			// Rule 2: insufficient gas remaining → skip (gas exemption).
			if gasRemaining < tx.Gas() {
				continue
			}
			// Rule 3: validate state for includability.
			if postState != nil {
				// Resolve sender: frame txs embed the sender directly,
				// while EOA txs recover it from the ECDSA signature.
				var from types.Address
				if tx.Type() == types.FrameTxType {
					from = tx.FrameSender()
				} else if senderPtr := tx.Sender(); senderPtr != nil {
					from = *senderPtr
				} else {
					// Can't determine sender → can't prove tx is invalid.
					// Per EIP-7805: ILs can contain any tx, and we can only
					// exempt txs we can prove are state-invalid. Without a
					// sender, skip state checks and fall through to unsatisfied.
					return ILUnsatisfied
				}
				fromKey := [20]byte(from)

				// Nonce check: applies to all tx types including frame txs.
				if postState.GetNonce(fromKey) != tx.Nonce() {
					continue // invalid nonce → exempt
				}

				if tx.Type() == types.FrameTxType {
					// EIP-8141 frame tx: skip balance check (payer is
					// determined at APPROVE time, may differ from sender).
					// Instead, verify VERIFY target(s) have code.
					if verifier != nil && !frameTxVerifyTargetsHaveCode(tx, verifier) {
						continue // VERIFY target has no code → can't APPROVE → exempt
					}
				} else {
					// EOA tx: standard balance check.
					gasPrice := tx.GasPrice()
					if gasPrice == nil {
						gasPrice = new(big.Int)
					}
					cost := new(big.Int).Mul(new(big.Int).SetUint64(tx.Gas()), gasPrice)
					if v := tx.Value(); v != nil {
						cost.Add(cost, v)
					}
					if cost.IsUint64() && postState.GetBalance(fromKey) < cost.Uint64() {
						continue // insufficient balance → exempt
					}
				}
			}
			// Valid tx absent from block → unsatisfied.
			return ILUnsatisfied
		}
	}
	return ILSatisfied
}

// frameTxVerifyTargetsHaveCode checks that all VERIFY frame targets in a frame
// transaction have deployed code. A VERIFY frame whose target has no code
// cannot call APPROVE (the opcode requires contract execution), making the
// entire frame transaction consensus-invalid per EIP-8141.
func frameTxVerifyTargetsHaveCode(tx *types.Transaction, verifier FrameVerifier) bool {
	frames := tx.Frames()
	if len(frames) == 0 {
		return true // not a frame tx or no frames
	}
	sender := tx.FrameSender()
	for _, f := range frames {
		if f.Mode != types.ModeVerify {
			continue
		}
		target := sender
		if f.Target != nil {
			target = *f.Target
		}
		if verifier.GetCodeSize(target) == 0 {
			return false // VERIFY target is EOA → APPROVE impossible
		}
	}
	return true
}
