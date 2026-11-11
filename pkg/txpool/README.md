# txpool

Transaction pool for ETH2030, implementing standard Ethereum mempool semantics with EIP-4844 blob transaction support, EIP-8141 frame transaction validation, sharded mempool throughput, and an encrypted commit-reveal pool.

## Overview

The `txpool` package provides the core pending transaction queue for the ETH2030 client. It maintains two tiers of transactions per sender: a `pending` set of sequentially executable transactions and a `queue` set of future transactions whose nonces are not yet reachable. Transactions are sorted by nonce within each sender's list and can be retrieved sorted by effective gas price for block building.

The package is organized as a thin orchestration layer around several focused subpackages. The top-level `TxPool` type handles standard transaction types (legacy, access-list, EIP-1559, blob, SetCode, frame), while subpackages provide specialized pools for blob data (`blobpool`), encrypted mempool semantics (`encrypted`), horizontal scaling via sharding (`sharding`), and proof aggregation for STARK-based mempool commitments (`stark`).

The pool enforces EIP-2930 access list gas accounting, EIP-1559 base fee demotion, EIP-4844 blob fee validation, and EIP-8141 frame transaction structural checks including paymaster registry verification (AA-1.2) and VERIFY frame code checks (AA-3.1).

## Table of Contents

- [Functionality](#functionality)
- [Subpackages](#subpackages)
- [Usage](#usage)
- [Documentation References](#documentation-references)

## Functionality

### Core Transaction Pool

`TxPool` is the main pool type created with `New(config Config, state StateReader)`.

Key constants:
- `MaxPoolSize = 4096` — total transaction capacity
- `MaxPerSender = 16` — per-address transaction limit
- `MaxTxSize = 128 * 1024` — maximum encoded transaction size (128 KB)
- `MaxNonceGap = 64` — maximum nonce look-ahead to prevent memory exhaustion
- `PriceBump = 10` — minimum gas price bump percentage for replace-by-fee

Core methods:
- `AddLocal(tx)` / `AddRemote(tx)` — submit transactions from local node or network peers
- `Pending()` — returns all processable transactions grouped by sender, sorted by nonce
- `PendingFlat()` / `PendingSorted()` — flat slices sorted by gas price (descending)
- `Get(hash)` — retrieve a transaction by hash
- `Remove(hash)` — remove a mined or invalidated transaction, with automatic queue promotion
- `Count()` / `PendingCount()` / `QueuedCount()` — cardinality helpers
- `Reset(stateReader)` — purge stale transactions after a new block, then re-promote queued ones
- `SetBaseFee(baseFee)` — update the EIP-1559 base fee and demote transactions that can no longer afford it
- `SetBlobBaseFee(blobBaseFee)` — update the EIP-4844 blob base fee threshold
- `SetCodeReader(r)` — wire a `FrameStateReader` to enable VERIFY frame pre-flight code checks

### Transaction Validation

`validateTx` performs comprehensive admission checks:
1. Type gate: `LocalTx` (type `0x08`) requires `Config.AllowLocalTx`
2. Value, gas price, and fee cap sign checks
3. Gas limit vs block gas limit
4. RLP-encoded size check (128 KB limit)
5. EIP-8141 frame structure: minimum gas, frame count, mode validity, VERIFY+SENDER relationship
6. Paymaster registry check (AA-1.2): external VERIFY targets must be staked
7. VERIFY frame code check (AA-3.1) via `FrameStateReader`
8. EIP-2930 intrinsic gas including access list costs
9. EIP-1559 fee cap >= tip cap and fee cap >= base fee
10. EIP-4844 blob hash presence and blob fee cap >= blob base fee
11. Sender balance >= `gas * gasPrice + value + blobGas * blobFeeCap`

### Replace-by-Fee

`checkReplacement` enforces the 10% price bump rule. For EIP-1559-style transactions both the fee cap and tip cap must individually meet the bump threshold, preventing fee cap gaming.

### Eviction

When the pool reaches `MaxSize`, `evictLowest` removes the transaction with the lowest effective gas price. Each sender's highest-nonce pending transaction is protected from eviction (ensuring every sender retains at least one position).

### Gas Utilities

- `EffectiveGasPrice(tx, baseFee)` — computes `min(feeCap, baseFee + tipCap)` for EIP-1559 transactions; returns `GasPrice` for legacy
- `IntrinsicGas(data, isCreate)` — base 21,000 (53,000 for create) plus calldata costs (16 per non-zero byte, 4 per zero byte)
- `AccessListGas(al)` — 2,400 per address + 1,900 per storage key

### EIP-8070 Sparse Blob Pool

The `blobpool` subpackage implements a sparse blob pool with WAL-backed persistence. It tracks blob transactions via `BlobMetadata` (avoiding full blob data in memory), enforces per-account limits (`DefaultMaxBlobsPerAccount = 16`), a soft datacap (`DefaultDatacap = 2.5 GB`), and 100% price bump for blob replacements. EIP-7594 PeerDAS constants are embedded: `CellsPerBlob = 128`, `DefaultCustodyColumns = 4`.

### Encrypted Mempool

The `encrypted` subpackage implements the commit-reveal scheme for MEV protection. `EncryptedPool` stores commitments (`CommitTx` with a hash commitment) and reveals (`RevealTx` with the plaintext transaction). Threshold decryption (`threshold_decrypt.go`) enables multi-party decryption ordering. `OrderingPolicy` determines the ordering of revealed transactions. `VDFTimer` provides verifiable delay for timed reveals.

### Sharded Mempool

`ShardedPool` distributes transactions across `NumShards` shards (default 16) using consistent hashing on the transaction hash. Each shard is independently locked, reducing contention at high throughput. `ShardStats` exposes per-shard utilization metrics.

### STARK Aggregation

The `stark` subpackage provides `STARKAggregator` for mempool commitment aggregation. `MempoolAggregationTick` triggers periodic commitment updates that bundle pending transaction hashes into a STARK-verifiable commitment, enabling efficient light client mempool proofs.

### Frame Transaction Subpackages

- `frametx/` — `PaymasterApprover` interface and `SimulateVerifyFrame` for VERIFY frame pre-flight checks (AA-3.1)
- `fees/` — fee computation helpers for EIP-1559 and EIP-4844 fee markets
- `pricing/` — replacement price bump logic
- `queue/` — isolated queue management for future transactions
- `replacement/` — replace-by-fee policy enforcement
- `tracking/` — sender nonce and balance tracking
- `validation/` — transaction validation helpers
- `journal/` — persistent transaction journal for crash recovery
- `shared/` — shared types and utilities across subpackages
- `peertick/` — `PeerTickCache` for peer-level transaction tick tracking

## Subpackages

| Subpackage | Description |
|------------|-------------|
| [`blobpool/`](./blobpool/) | EIP-8070 sparse blob pool with WAL, custody, and price eviction |
| [`encrypted/`](./encrypted/) | Encrypted mempool: commit-reveal scheme, threshold decryption, VDF timer |
| [`sharding/`](./sharding/) | Sharded mempool with consistent hashing for parallel access |
| [`stark/`](./stark/) | STARK-based mempool commitment aggregation |
| [`frametx/`](./frametx/) | EIP-8141 frame transaction paymaster and VERIFY simulation |
| [`pricing/`](./pricing/) | Replace-by-fee price bump logic |
| [`tracking/`](./tracking/) | Sender nonce and balance tracking |
| [`journal/`](./journal/) | Persistent transaction journal for crash recovery |
| [`shared/`](./shared/) | Shared types used across subpackages |
| [`peertick/`](./peertick/) | Per-peer transaction tick cache |

## Usage

```go
import "github.com/eth2030/eth2030/txpool"

// Create a pool with default configuration.
cfg := txpool.DefaultConfig()
cfg.BlockGasLimit = 30_000_000

pool := txpool.New(cfg, stateReader)

// Optionally wire frame transaction VERIFY code checks.
pool.SetCodeReader(codeReader)

// Update fee market parameters after each block.
pool.SetBaseFee(newBaseFee)
pool.SetBlobBaseFee(newBlobBaseFee)

// Accept a transaction.
if err := pool.AddRemote(tx); err != nil {
    log.Warn("rejected transaction", "err", err)
}

// Retrieve pending transactions for block building.
pending := pool.PendingSorted() // sorted by effective gas price descending

// Remove mined transactions.
for _, tx := range minedTxs {
    pool.Remove(tx.Hash())
}

// Update state after a new block.
pool.Reset(newStateReader)
```

```go
import (
    "github.com/eth2030/eth2030/txpool"
    "github.com/eth2030/eth2030/txpool/blobpool"
)

// Use the sparse blob pool for EIP-4844 transactions.
blobCfg := txpool.DefaultBlobPoolConfig()
bp := txpool.NewBlobPool(blobCfg, dataDir)

// Use the sharded pool for high-throughput scenarios.
shardCfg := txpool.DefaultShardConfig()
sp := txpool.NewShardedPool(shardCfg)
```

## Documentation References

- [EIP-4844: Blob Transactions](https://eips.ethereum.org/EIPS/eip-4844)
- [EIP-8070: Sparse Blob Pool](https://eips.ethereum.org/EIPS/eip-8070)
- [EIP-8141: Frame Transactions](https://eips.ethereum.org/EIPS/eip-8141)
- [EIP-1559: Fee Market](https://eips.ethereum.org/EIPS/eip-1559)
- [EIP-2930: Access Lists](https://eips.ethereum.org/EIPS/eip-2930)
- [L1 Strawmap Roadmap](../../docs/ROADMAP.md)
