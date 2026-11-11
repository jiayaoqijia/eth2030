# execution

Transaction execution pipeline: processor, receipts, parallel execution, and dependency graphs.

[← core](../README.md)

## Overview

Package `execution` is the core transaction execution engine. It applies
transactions to a `StateDB` through the EVM, generates typed receipts, and
tracks Block Access List (BAL) data (EIP-7928) for post-execution parallel
optimization. It also provides a `ParallelProcessor` that uses BAL-derived
dependency groups to execute independent transactions concurrently.

## Functionality

### TxExecutor Interface (`iface.go`)

```go
type TxExecutor interface {
    Process(block *types.Block, statedb state.StateDB) ([]*types.Receipt, error)
    ProcessWithBAL(block *types.Block, statedb state.StateDB) (*ProcessResult, error)
    SetGetHash(fn vm.GetHashFunc)
}
```

`StateProcessor` implements `TxExecutor`.

### StateProcessor (`processor.go`)

- `NewStateProcessor(config)` — sequential transaction processor.
- `Process(block, statedb)` — applies all transactions; returns receipts.
- `ProcessWithBAL(block, statedb)` — applies all transactions with per-tx BAL
  tracking; returns a `ProcessResult` that includes the computed `BlockAccessList`.
- `SetGetHash(fn)` — wires the `BLOCKHASH` opcode lookup function.
- `SetPaymasterSlasher(slasher)` — registers an optional paymaster slasher for
  EIP-7701 settlement failures.

Key package-level helpers:
- `ApplyTransactionWithBAL(config, statedb, header, tx, gp, tracker)` — applies
  a single transaction against the EVM and populates the BAL tracker.
- `CalldataFloorGas(data, isCreate)` — EIP-7623 calldata floor gas.
- `CalcBlobBaseFee(excessBlobGas)` — EIP-4844 blob base fee.
- `CapturePreState(config, statedb, tx, coinbase)` — snapshots balances and nonces before a tx.
- `PopulateTracker(tracker, statedb, preBalances, preNonces)` — fills the BAL
  tracker with post-execution state deltas.
- `BalTrackerOrNil(t)` — safely converts `*bal.AccessTracker` to the
  `vm.BALTracker` interface, avoiding the typed-nil-interface pitfall.
- `ProcessRequests(config, statedb, header)` — accumulates EIP-7685 EL
  requests (deposits, withdrawals, consolidations).

### Gas Constants

| Constant | Value |
|---|---|
| `TxGas` | 21000 |
| `TxDataZeroGas` | 4 |
| `TxDataNonZeroGas` | 16 |
| `TxCreateGas` | 32000 |
| `PerAuthBaseCost` | 12500 (EIP-7702) |
| `PerEmptyAccountCost` | 25000 (EIP-7702) |

### Receipt Generation (`receipt_generation.go`, `receipt_processor.go`)

- `ReceiptGeneration` — builds `*types.Receipt` from EVM execution output.
- `ReceiptProcessor` — encodes receipts to RLP and computes receipt trie roots.
- `ComputeReceiptRoot(receipts)` — MPT-based receipt root.

### Parallel Execution (`parallel.go`)

- `ParallelProcessor` — constructed with `NewParallelProcessor(config)`.
- `ProcessParallel(statedb, block, accessList)` — groups transactions by BAL
  dependency sets and executes independent groups concurrently via goroutines.
  Falls back to sequential execution if the BAL is nil or state is not a
  `*MemoryStateDB`.

### Dependency Graph (`dependency_graph.go`)

- `DependencyGraph` — directed acyclic graph of transaction dependencies
  derived from BAL read/write sets. Used to determine parallel execution sets.
- `BuildDependencyGraph(accessList)` — constructs a graph from a
  `*bal.BlockAccessList`.

### Rich Data (`rich_data.go`)

Schema-validated on-chain data storage with field-level indexing:
- `DataType` — `TypeUint256`, `TypeAddress`, `TypeBytes32`, `TypeString`,
  `TypeBool`, `TypeArray`.
- `RichDataRegistry` — stores schemas and keyed data entries.

## Usage

```go
proc := execution.NewStateProcessor(chainConfig)
proc.SetGetHash(blockchain.GetBlockHashFn())

result, err := proc.ProcessWithBAL(block, statedb)
// result.Receipts — ordered receipts
// result.BAL — BlockAccessList for the block
// result.GasUsed — total gas consumed
```
