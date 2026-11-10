package eiptest

import (
	"github.com/eth2030/eth2030/core/types"
)

// BlockResult is the outcome of one simulated block.
type BlockResult struct {
	Sim      *Sim
	Block    *types.Block
	Receipts []*types.Receipt
}

// TxCount returns the number of transactions in the block.
func (r *BlockResult) TxCount() int { return len(r.Receipts) }

// Tx returns the result of the i-th transaction (index into the block).
func (r *BlockResult) Tx(i int) *TxResult {
	if i < 0 || i >= len(r.Receipts) {
		return &TxResult{missing: true, Sim: r.Sim}
	}
	return &TxResult{
		Sim:     r.Sim,
		Tx:      r.Block.Transactions()[i],
		Receipt: r.Receipts[i],
		Index:   i,
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
