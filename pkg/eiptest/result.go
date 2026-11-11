package eiptest

import (
	"testing"

	"github.com/eth2030/eth2030/bal"
	"github.com/eth2030/eth2030/core/types"
)

// BlockResult is the outcome of one simulated block.
type BlockResult struct {
	Sim      *Sim
	Block    *types.Block
	Receipts []*types.Receipt
	Logs     []*types.Log
	BAL      *bal.BlockAccessList
	Requests types.Requests
}

// TxCount returns the number of transactions in the block.
func (r *BlockResult) TxCount() int { return len(r.Receipts) }

// Tx returns the result of the i-th transaction (index into the block).
func (r *BlockResult) Tx(i int) *TxResult {
	if i < 0 || i >= len(r.Receipts) {
		return &TxResult{missing: true, Sim: r.Sim, Index: i, block: r}
	}
	return &TxResult{
		Sim:     r.Sim,
		Tx:      r.Block.Transactions()[i],
		Receipt: r.Receipts[i],
		Index:   i,
		block:   r,
	}
}

// Txs returns the results of all transactions in order.
func (r *BlockResult) Txs() []*TxResult {
	out := make([]*TxResult, 0, len(r.Receipts))
	for i := range r.Receipts {
		out = append(out, r.Tx(i))
	}
	return out
}

// GasUsed returns the block's total gas used.
func (r *BlockResult) GasUsed() uint64 { return r.Block.GasUsed() }

// TxResult is the outcome of one transaction inside a block.
type TxResult struct {
	Sim     *Sim
	Tx      *types.Transaction
	Receipt *types.Receipt
	Index   int
	missing bool
	block   *BlockResult
}

// Status returns the receipt status (1 success, 0 failure).
func (r *TxResult) Status() uint64 {
	if r.missing {
		return 0
	}
	return r.Receipt.Status
}

// Success reports whether the tx executed successfully.
func (r *TxResult) Success() bool { return r.Status() == types.ReceiptStatusSuccessful }

// Reverted reports whether the tx reverted (EVM-level failure with gas burned).
func (r *TxResult) Reverted() bool {
	return !r.missing && r.Receipt.Status == types.ReceiptStatusFailed
}

// Missing reports whether the tx index was out of range.
func (r *TxResult) Missing() bool { return r.missing }

// GasUsed returns the tx gas used.
func (r *TxResult) GasUsed() uint64 {
	if r.missing {
		return 0
	}
	return r.Receipt.GasUsed
}

// Logs returns the logs emitted by the tx.
func (r *TxResult) Logs() []*types.Log {
	if r.missing {
		return nil
	}
	return r.Receipt.Logs
}

// ContractAddress returns the created contract address for deployment txs.
func (r *TxResult) ContractAddress() types.Address {
	if r.missing {
		return types.Address{}
	}
	return r.Receipt.ContractAddress
}

// ExpectStatus asserts the receipt status and returns the transaction result.
func (r *TxResult) ExpectStatus(t *testing.T, want uint64) *TxResult {
	t.Helper()
	if r.Missing() || r.Status() != want {
		failBlock(t, r.block, "tx %d: want status %d, got status %d (missing=%t)", r.Index, want, r.Status(), r.Missing())
	}
	return r
}

// ExpectGas asserts an inclusive receipt gas range, independent of status.
func (r *TxResult) ExpectGas(t *testing.T, lo, hi uint64) *TxResult {
	t.Helper()
	if r.Missing() || r.GasUsed() < lo || r.GasUsed() > hi {
		failBlock(t, r.block, "tx %d: gas used %d, want [%d,%d]", r.Index, r.GasUsed(), lo, hi)
	}
	return r
}
