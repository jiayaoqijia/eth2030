package testutil

import (
	"math/big"

	blkpkg "github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/execution"
	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
)

// NewUint64 returns a pointer to the given uint64 value.
func NewUint64(v uint64) *uint64 { return &v }

// MakeGenesis creates a genesis block with the given gas limit and base fee.
func MakeGenesis(gasLimit uint64, baseFee *big.Int) *types.Block {
	blobGasUsed := uint64(0)
	excessBlobGas := uint64(0)
	calldataGasUsed := uint64(0)
	calldataExcessGas := uint64(0)
	emptyWithdrawalsHash := types.EmptyRootHash
	emptyRoot := types.EmptyRootHash
	header := &types.Header{
		Number:            big.NewInt(0),
		GasLimit:          gasLimit,
		GasUsed:           0,
		Time:              0,
		Difficulty:        new(big.Int),
		BaseFee:           baseFee,
		UncleHash:         blkpkg.EmptyUncleHash,
		WithdrawalsHash:   &emptyWithdrawalsHash,
		BlobGasUsed:       &blobGasUsed,
		ExcessBlobGas:     &excessBlobGas,
		ParentBeaconRoot:  &emptyRoot,
		RequestsHash:      &emptyRoot,
		CalldataGasUsed:   &calldataGasUsed,
		CalldataExcessGas: &calldataExcessGas,
	}
	return types.NewBlock(header, &types.Body{Withdrawals: []*types.Withdrawal{}})
}

// MakeBlockWithState builds a valid child block and computes the correct header
// fields by executing the transactions against the provided state. The state is
// mutated in place so callers can chain multiple blocks.
func MakeBlockWithState(parent *types.Block, txs []*types.Transaction, statedb state.StateDB) *types.Block {
	return MakeBlockWithStateAndConfig(parent, txs, statedb, config.TestConfig)
}

// MakeBlockWithStateAndConfig is like MakeBlockWithState but accepts a custom config.
func MakeBlockWithStateAndConfig(parent *types.Block, txs []*types.Transaction, statedb state.StateDB, cfg *config.ChainConfig) *types.Block {
	parentHeader := parent.Header()
	blobGasUsed := uint64(0)
	var pExcess, pUsed uint64
	if parentHeader.ExcessBlobGas != nil {
		pExcess = *parentHeader.ExcessBlobGas
	}
	if parentHeader.BlobGasUsed != nil {
		pUsed = *parentHeader.BlobGasUsed
	}
	excessBlobGas := gas.CalcExcessBlobGas(pExcess, pUsed)

	calldataGasUsed := uint64(0)
	var pCalldataExcess, pCalldataUsed uint64
	if parentHeader.CalldataExcessGas != nil {
		pCalldataExcess = *parentHeader.CalldataExcessGas
	}
	if parentHeader.CalldataGasUsed != nil {
		pCalldataUsed = *parentHeader.CalldataGasUsed
	}
	calldataExcessGas := gas.CalcCalldataExcessGas(pCalldataExcess, pCalldataUsed, parentHeader.GasLimit)

	emptyWHash := types.EmptyRootHash
	emptyBeaconRoot := types.EmptyRootHash
	emptyRequestsHash := types.EmptyRootHash
	header := &types.Header{
		ParentHash:        parent.Hash(),
		Number:            new(big.Int).Add(parentHeader.Number, big.NewInt(1)),
		GasLimit:          parentHeader.GasLimit,
		Time:              parentHeader.Time + 12,
		Difficulty:        new(big.Int),
		BaseFee:           gas.CalcBaseFee(parentHeader),
		UncleHash:         blkpkg.EmptyUncleHash,
		WithdrawalsHash:   &emptyWHash,
		BlobGasUsed:       &blobGasUsed,
		ExcessBlobGas:     &excessBlobGas,
		ParentBeaconRoot:  &emptyBeaconRoot,
		RequestsHash:      &emptyRequestsHash,
		CalldataGasUsed:   &calldataGasUsed,
		CalldataExcessGas: &calldataExcessGas,
	}

	body := &types.Body{
		Transactions: txs,
		Withdrawals:  []*types.Withdrawal{},
	}

	blk := types.NewBlock(header, body)

	proc := execution.NewStateProcessor(cfg)
	result, err := proc.ProcessWithBAL(blk, statedb)
	if err == nil {
		var gasUsed uint64
		for _, r := range result.Receipts {
			gasUsed += r.GasUsed
		}
		header.GasUsed = gasUsed

		var cdGasUsed uint64
		for _, tx := range txs {
			cdGasUsed += tx.CalldataGas()
		}
		*header.CalldataGasUsed = cdGasUsed

		if result.BlockAccessList != nil {
			h := result.BlockAccessList.Hash()
			header.BlockAccessListHash = &h
		}

		header.Bloom = types.CreateBloom(result.Receipts)
		header.ReceiptHash = blkpkg.DeriveReceiptsRoot(result.Receipts)
		header.Root = statedb.GetRoot()
	}

	header.TxHash = blkpkg.DeriveTxsRoot(txs)
	return types.NewBlock(header, body)
}

// MakeBlock builds a valid child block of parent with the given transactions.
// It uses a fresh empty state. Suitable only for the first block after genesis.
// For chains of blocks use MakeBlockWithState with a shared state.
func MakeBlock(parent *types.Block, txs []*types.Transaction) *types.Block {
	return MakeBlockWithState(parent, txs, state.NewMemoryStateDB())
}
