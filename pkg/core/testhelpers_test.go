package core

import (
	"math/big"
	"testing"

	blkpkg "github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/chain"
	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/internal/testutil"
)

// newUint64 is a test helper that returns a pointer to a uint64 value.
// It mirrors config.newUint64 for use in core package tests.
func newUint64(v uint64) *uint64 { return &v }

// testChain creates a blockchain with a genesis block for use in tests.
func testChain(t *testing.T) (*chain.Blockchain, *state.MemoryStateDB) {
	t.Helper()
	statedb := state.NewMemoryStateDB()
	genesis := makeGenesis(30_000_000, big.NewInt(1))
	db := rawdb.NewMemoryDB()
	bc, err := chain.NewBlockchain(config.TestConfig, genesis, statedb, db)
	if err != nil {
		t.Fatalf("NewBlockchain: %v", err)
	}
	return bc, statedb
}

// makeGenesis creates a genesis block with the given gas limit and base fee.
func makeGenesis(gasLimit uint64, baseFee *big.Int) *types.Block {
	return testutil.MakeGenesis(gasLimit, baseFee)
}

// makeBlock builds a valid child block of parent with the given transactions.
func makeBlock(parent *types.Block, txs []*types.Transaction) *types.Block {
	return testutil.MakeBlock(parent, txs)
}

// makeBlockWithState builds a valid child block executing txs against statedb.
func makeBlockWithState(parent *types.Block, txs []*types.Transaction, statedb state.StateDB) *types.Block {
	return testutil.MakeBlockWithState(parent, txs, statedb)
}

// makeBlockWithStateAndConfig builds a valid child block with custom config.
func makeBlockWithStateAndConfig(parent *types.Block, txs []*types.Transaction, statedb state.StateDB, cfg *config.ChainConfig) *types.Block {
	return testutil.MakeBlockWithStateAndConfig(parent, txs, statedb, cfg)
}

// makeChainBlocks builds a chain of empty blocks from the given parent using
// the provided state (which is mutated in place).
func makeChainBlocks(parent *types.Block, count int, statedb state.StateDB) []*types.Block {
	blocks := make([]*types.Block, count)
	for i := 0; i < count; i++ {
		blocks[i] = makeBlockWithState(parent, nil, statedb)
		parent = blocks[i]
	}
	return blocks
}

// mockTxPool implements block.TxPoolReader for testing.
type mockTxPool struct {
	txs []*types.Transaction
}

func (p *mockTxPool) Pending() []*types.Transaction {
	return p.txs
}

// newLegacyBuilder creates a block builder for testing using the legacy interface.
func newLegacyBuilder(cfg *config.ChainConfig, statedb state.StateDB) *blkpkg.BlockBuilder {
	b := blkpkg.NewBlockBuilder(cfg, nil, nil)
	b.SetState(statedb)
	return b
}

// makeValidParent creates a valid parent header for block validation tests.
func makeValidParent() *types.Header {
	blobGasUsed := uint64(0)
	excessBlobGas := uint64(0)
	calldataGasUsed := uint64(0)
	calldataExcessGas := uint64(0)
	emptyBeaconRoot := types.EmptyRootHash
	emptyRequestsHash := types.EmptyRootHash
	return &types.Header{
		Number:            big.NewInt(100),
		GasLimit:          30000000,
		GasUsed:           15000000,
		Time:              1000,
		Difficulty:        new(big.Int),
		BaseFee:           big.NewInt(1000000000), // 1 Gwei
		BlobGasUsed:       &blobGasUsed,
		ExcessBlobGas:     &excessBlobGas,
		ParentBeaconRoot:  &emptyBeaconRoot,
		RequestsHash:      &emptyRequestsHash,
		CalldataGasUsed:   &calldataGasUsed,
		CalldataExcessGas: &calldataExcessGas,
	}
}

// makeValidChild creates a valid child header for the given parent.
func makeValidChild(parent *types.Header) *types.Header {
	blobGasUsed := uint64(0)
	var parentExcess, parentUsed uint64
	if parent.ExcessBlobGas != nil {
		parentExcess = *parent.ExcessBlobGas
	}
	if parent.BlobGasUsed != nil {
		parentUsed = *parent.BlobGasUsed
	}
	excessBlobGas := gas.CalcExcessBlobGas(parentExcess, parentUsed)

	calldataGasUsed := uint64(0)
	var pCalldataExcess, pCalldataUsed uint64
	if parent.CalldataExcessGas != nil {
		pCalldataExcess = *parent.CalldataExcessGas
	}
	if parent.CalldataGasUsed != nil {
		pCalldataUsed = *parent.CalldataGasUsed
	}
	calldataExcessGas := gas.CalcCalldataExcessGas(pCalldataExcess, pCalldataUsed, parent.GasLimit)

	emptyBeaconRoot := types.EmptyRootHash
	emptyRequestsHash := types.EmptyRootHash

	return &types.Header{
		ParentHash:        parent.Hash(),
		Number:            new(big.Int).Add(parent.Number, big.NewInt(1)),
		GasLimit:          parent.GasLimit,
		GasUsed:           10000000,
		Time:              parent.Time + 12,
		Difficulty:        new(big.Int),
		BaseFee:           gas.CalcBaseFee(parent),
		BlobGasUsed:       &blobGasUsed,
		ExcessBlobGas:     &excessBlobGas,
		ParentBeaconRoot:  &emptyBeaconRoot,
		RequestsHash:      &emptyRequestsHash,
		CalldataGasUsed:   &calldataGasUsed,
		CalldataExcessGas: &calldataExcessGas,
	}
}
