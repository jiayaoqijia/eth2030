package backend

import (
	"fmt"
	"log/slog"
	"math/big"

	coregas "github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
	"github.com/eth2030/eth2030/rpc"
	"github.com/eth2030/eth2030/trie"
)

// RPCBackend adapts NodeDeps to the rpc.Backend interface.
type RPCBackend struct {
	node   NodeDeps
	engine *EngineBackend
}

// NewRPCBackend creates a new RPCBackend.
func NewRPCBackend(node NodeDeps, engine *EngineBackend) rpc.Backend {
	return &RPCBackend{node: node, engine: engine}
}

func (b *RPCBackend) HeaderByNumber(number rpc.BlockNumber) *types.Header {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil
	}

	switch number {
	case rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		blk := bc.CurrentBlock()
		if blk != nil {
			return blk.Header()
		}
		return nil
	case rpc.EarliestBlockNumber:
		blk := bc.GetBlockByNumber(0)
		if blk != nil {
			return blk.Header()
		}
		return nil
	case rpc.FinalizedBlockNumber:
		if blk := bc.CurrentFinalBlock(); blk != nil {
			return blk.Header()
		}
		return nil
	case rpc.SafeBlockNumber:
		if blk := bc.CurrentSafeBlock(); blk != nil {
			return blk.Header()
		}
		return nil
	default:
		blk := bc.GetBlockByNumber(uint64(number))
		if blk != nil {
			return blk.Header()
		}
		return nil
	}
}

func (b *RPCBackend) HeaderByHash(hash types.Hash) *types.Header {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil
	}
	blk := bc.GetBlock(hash)
	if blk != nil {
		return blk.Header()
	}
	return nil
}

func (b *RPCBackend) BlockByNumber(number rpc.BlockNumber) *types.Block {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil
	}

	switch number {
	case rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		return bc.CurrentBlock()
	case rpc.EarliestBlockNumber:
		return bc.GetBlockByNumber(0)
	case rpc.FinalizedBlockNumber:
		if b.engine != nil {
			if h := b.engine.GetFinalizedHash(); h != (types.Hash{}) {
				return bc.GetBlock(h)
			}
		}
		return nil
	case rpc.SafeBlockNumber:
		if b.engine != nil {
			if h := b.engine.GetSafeHash(); h != (types.Hash{}) {
				return bc.GetBlock(h)
			}
		}
		return nil
	default:
		return bc.GetBlockByNumber(uint64(number))
	}
}

func (b *RPCBackend) BlockByHash(hash types.Hash) *types.Block {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil
	}
	return bc.GetBlock(hash)
}

func (b *RPCBackend) CurrentHeader() *types.Header {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil
	}
	blk := bc.CurrentBlock()
	if blk != nil {
		return blk.Header()
	}
	return nil
}

func (b *RPCBackend) ChainID() *big.Int {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil
	}
	return bc.Config().ChainID
}

func (b *RPCBackend) StateAt(root types.Hash) (state.StateDB, error) {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil, fmt.Errorf("blockchain not available")
	}
	return bc.StateAtRoot(root)
}

func (b *RPCBackend) GetProof(addr types.Address, storageKeys []types.Hash, blockNumber rpc.BlockNumber) (*trie.AccountProof, error) {
	header := b.HeaderByNumber(blockNumber)
	if header == nil {
		return nil, fmt.Errorf("block not found")
	}

	statedb, err := b.StateAt(header.Root)
	if err != nil {
		return nil, err
	}

	memState, ok := statedb.(*state.MemoryStateDB)
	if !ok {
		return nil, fmt.Errorf("state does not support proof generation")
	}

	stateTrie := memState.BuildStateTrie()
	storageTrie := memState.BuildStorageTrie(addr)

	return trie.ProveAccountWithStorage(stateTrie, addr, storageTrie, storageKeys)
}

func (b *RPCBackend) SendTransaction(tx *types.Transaction) error {
	pool := b.node.TxPool()
	if pool == nil {
		return fmt.Errorf("transaction pool not available")
	}

	if err := pool.AddLocal(tx); err != nil {
		return err
	}

	// Broadcast via eth handler
	if h := b.node.EthHandler(); h != nil {
		h.BroadcastTransactions([]*types.Transaction{tx})
	}

	// Journal transaction
	if journal := b.node.TxJournal(); journal != nil {
		if jerr := journal.Insert(tx, true); jerr != nil {
			slog.Debug("tx journal insert failed", "hash", tx.Hash(), "err", jerr)
		}
	}

	slog.Debug("transaction sent", "hash", tx.Hash())
	return nil
}

func (b *RPCBackend) GetTransaction(hash types.Hash) (*types.Transaction, uint64, uint64) {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil, 0, 0
	}

	// Check blockchain first
	if entry, found := bc.GetTxLookupEntry(hash); found {
		blk := bc.GetBlock(entry.BlockHash)
		if blk != nil {
			txs := blk.Transactions()
			if int(entry.TxIndex) < len(txs) {
				return txs[entry.TxIndex], entry.BlockNumber, entry.TxIndex
			}
		}
	}

	// Check tx pool
	pool := b.node.TxPool()
	if pool != nil {
		tx := pool.Get(hash)
		if tx != nil {
			return tx, 0, 0
		}
	}

	return nil, 0, 0
}

func (b *RPCBackend) SuggestGasPrice() *big.Int {
	if oracle := b.node.GasOracle(); oracle != nil {
		if oracle.BaseFee().Sign() > 0 {
			return oracle.SuggestGasPrice()
		}
	}

	bc := b.node.Blockchain()
	if bc != nil {
		blk := bc.CurrentBlock()
		if blk != nil && blk.Header().BaseFee != nil {
			return new(big.Int).Set(blk.Header().BaseFee)
		}
	}
	return big.NewInt(1_000_000_000)
}

func (b *RPCBackend) GetReceipts(blockHash types.Hash) []*types.Receipt {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil
	}
	return bc.GetReceipts(blockHash)
}

func (b *RPCBackend) GetLogs(blockHash types.Hash) []*types.Log {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil
	}
	return bc.GetLogs(blockHash)
}

func (b *RPCBackend) GetBlockReceipts(number uint64) []*types.Receipt {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil
	}
	return bc.GetBlockReceipts(number)
}

func (b *RPCBackend) EVMCall(from types.Address, to *types.Address, data []byte, gas uint64, value *big.Int, blockNumber rpc.BlockNumber) ([]byte, uint64, error) {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil, 0, fmt.Errorf("blockchain not available")
	}

	var header *types.Header
	switch blockNumber {
	case rpc.LatestBlockNumber, rpc.PendingBlockNumber:
		blk := bc.CurrentBlock()
		if blk != nil {
			header = blk.Header()
		}
	default:
		blk := bc.GetBlockByNumber(uint64(blockNumber))
		if blk != nil {
			header = blk.Header()
		}
	}
	if header == nil {
		return nil, 0, fmt.Errorf("block not found")
	}

	statedb, err := b.StateAt(header.Root)
	if err != nil {
		return nil, 0, fmt.Errorf("state not found: %w", err)
	}

	if gas == 0 {
		gas = 50_000_000
	}
	if value == nil {
		value = new(big.Int)
	}

	blockCtx := vm.BlockContext{
		GetHash:     bc.GetHashFn(),
		BlockNumber: header.Number,
		Time:        header.Time,
		GasLimit:    header.GasLimit,
		BaseFee:     header.BaseFee,
	}
	txCtx := vm.TxContext{
		Origin:   from,
		GasPrice: header.BaseFee,
	}

	evm := vm.NewEVMWithState(blockCtx, txCtx, vm.Config{}, statedb)

	if chainCfg := bc.Config(); chainCfg != nil {
		rules := chainCfg.Rules(header.Number, chainCfg.IsMerge(), header.Time)
		forkRules := vm.ForkRules{
			IsGlamsterdan:    rules.IsGlamsterdan,
			IsPrague:         rules.IsPrague,
			IsCancun:         rules.IsCancun,
			IsShanghai:       rules.IsShanghai,
			IsMerge:          rules.IsMerge,
			IsLondon:         rules.IsLondon,
			IsBerlin:         rules.IsBerlin,
			IsIstanbul:       rules.IsIstanbul,
			IsConstantinople: rules.IsConstantinople,
			IsByzantium:      rules.IsByzantium,
			IsHomestead:      rules.IsHomestead,
			IsEIP158:         rules.IsEIP158,
			IsEIP7708:        rules.IsEIP7708,
			IsEIP7954:        rules.IsEIP7954,
			IsIPlus:          rules.IsIPlus,
			LegacyNativeBLS:  rules.LegacyNativeBLS,
		}
		evm.SetJumpTable(vm.SelectJumpTable(forkRules))
		evm.SetPrecompiles(vm.SelectPrecompiles(forkRules))
		evm.SetForkRules(forkRules)
	}

	if to == nil {
		return nil, gas, nil
	}

	ret, gasLeft, err := evm.Call(from, *to, data, gas, value)
	return ret, gasLeft, err
}

func (b *RPCBackend) HistoryOldestBlock() uint64 {
	bc := b.node.Blockchain()
	if bc == nil {
		return 0
	}
	return bc.HistoryOldestBlock()
}

func (b *RPCBackend) BlobSchedule(blockTime uint64) (target, max, updateFraction uint64) {
	bc := b.node.Blockchain()
	if bc == nil {
		return 0, 0, 0
	}
	chainCfg := bc.Config()
	sched := coregas.GetBlobSchedule(chainCfg, blockTime)
	return sched.Target, sched.Max, sched.UpdateFraction
}

func (b *RPCBackend) TraceTransaction(txHash types.Hash) (*vm.StructLogTracer, error) {
	bc := b.node.Blockchain()
	if bc == nil {
		return nil, fmt.Errorf("blockchain not available")
	}

	entry, found := bc.GetTxLookupEntry(txHash)
	if !found {
		return nil, fmt.Errorf("transaction %v not found", txHash)
	}

	blk := bc.GetBlock(entry.BlockHash)
	if blk == nil {
		return nil, fmt.Errorf("block %v not found", entry.BlockHash)
	}

	txs := blk.Transactions()
	if int(entry.TxIndex) >= len(txs) {
		return nil, fmt.Errorf("transaction index %d out of range", entry.TxIndex)
	}

	header := blk.Header()
	parentBlock := bc.GetBlock(header.ParentHash)
	if parentBlock == nil {
		return nil, fmt.Errorf("parent block %v not found", header.ParentHash)
	}
	statedb, err := b.StateAt(parentBlock.Header().Root)
	if err != nil {
		return nil, fmt.Errorf("state not found for parent block: %w", err)
	}

	blockCtx := vm.BlockContext{
		GetHash:     bc.GetHashFn(),
		BlockNumber: header.Number,
		Time:        header.Time,
		GasLimit:    header.GasLimit,
		BaseFee:     header.BaseFee,
	}

	// Replay preceding transactions
	for i := uint64(0); i < entry.TxIndex; i++ {
		tx := txs[i]
		from := types.Address{}
		if sender := tx.Sender(); sender != nil {
			from = *sender
		}
		txCtx := vm.TxContext{
			Origin:   from,
			GasPrice: tx.GasPrice(),
		}
		evm := vm.NewEVMWithState(blockCtx, txCtx, vm.Config{}, statedb)
		to := tx.To()
		if to != nil {
			evm.Call(from, *to, tx.Data(), tx.Gas(), tx.Value())
		}
		statedb.SetNonce(from, statedb.GetNonce(from)+1)
	}

	// Trace target transaction
	targetTx := txs[entry.TxIndex]
	from := types.Address{}
	if sender := targetTx.Sender(); sender != nil {
		from = *sender
	}
	txCtx := vm.TxContext{
		Origin:   from,
		GasPrice: targetTx.GasPrice(),
	}

	tracer := vm.NewStructLogTracer()
	tracingCfg := vm.Config{
		Debug:  true,
		Tracer: tracer,
	}
	evm := vm.NewEVMWithState(blockCtx, txCtx, tracingCfg, statedb)

	intrinsicGas := uint64(21000)
	for _, b := range targetTx.Data() {
		if b == 0 {
			intrinsicGas += 4
		} else {
			intrinsicGas += 16
		}
	}
	if targetTx.To() == nil {
		intrinsicGas += 32000
	}
	evmGas := uint64(0)
	if targetTx.Gas() > intrinsicGas {
		evmGas = targetTx.Gas() - intrinsicGas
	}

	to := targetTx.To()
	if to != nil {
		ret, gasLeft, err := evm.Call(from, *to, targetTx.Data(), evmGas, targetTx.Value())
		gasUsed := evmGas - gasLeft
		tracer.CaptureEnd(ret, gasUsed, err)
	} else if evmGas > 0 {
		tracer.CaptureEnd(nil, evmGas, fmt.Errorf("contract creation tracing not supported"))
	}

	return tracer, nil
}
