# Package das

PeerDAS (Peer Data Availability Sampling) per EIP-7594.

## Overview

The `das` package implements the full PeerDAS data availability sampling stack for ETH2030. It provides the data layer infrastructure enabling Ethereum nodes to verify data availability without downloading complete blobs, which is foundational to scaling the network towards the Teragas L2 North Star target (1 Gbyte/sec).

The package is organized as a collection of focused sub-packages, each implementing a distinct concern (sampling, reconstruction, gossip, custody, etc.). The top-level `das` package re-exports the most commonly used types and functions from all sub-packages for convenience, while preserving direct sub-package imports for callers that need only a subset of functionality.

PeerDAS builds on the EIP-4844 blob transaction model by introducing column-based data distribution, Reed-Solomon erasure coding, and decentralized custody responsibilities across the validator set. Each node is assigned custody groups and only needs to store and verify a fraction of the total blob data, relying on sampling to gain probabilistic assurance of full availability.

## Table of Contents

- [Functionality](#functionality)
- [Subpackages](#subpackages)
- [Usage](#usage)
- [Documentation References](#documentation-references)

## Functionality

### Core Constants and Types (dastypes)

The package defines EIP-7594 constants governing the sampling geometry:

| Constant | Value | Description |
|----------|-------|-------------|
| `NumberOfColumns` | 128 | Total data columns in the extended blob matrix |
| `NumberOfCustodyGroups` | 128 | Custody group count |
| `CustodyRequirement` | 4 | Minimum custody groups per node |
| `SamplesPerSlot` | 16 | Samples taken per slot |
| `FieldElementsPerBlob` | 4096 | Field elements per blob |
| `FieldElementsPerCell` | 64 | Field elements per cell |
| `CellsPerExtBlob` | 128 | Cells in the extended blob |
| `ReconstructionThreshold` | 50% | Minimum columns needed to reconstruct |

Core types include `DataColumnSidecar`, `Cell`, `KZGCommitment`, `KZGProof`, `MatrixEntry`, `SubnetID`, `CustodyGroup`, and `ColumnIndex`.

### Custody and Sampling (sampling)

Implements the EIP-7594 custody group and column assignment algorithms:

- `GetCustodyGroups(nodeID, custodyGroupCount)` — computes the custody group set for a node
- `ComputeColumnsForCustodyGroup(group)` — maps a custody group to its column indices
- `GetCustodyColumns(nodeID, custodyGroupCount)` — returns all columns a node is responsible for
- `ShouldCustodyColumn(nodeID, custodyGroupCount, column)` — predicate for custody responsibility
- `VerifyDataColumnSidecar(sidecar)` — structural validation of a sidecar
- `ColumnSubnet(column)` — maps a column index to its gossip subnet

### Blob Reconstruction (reconstruction)

Implements Reed-Solomon Lagrange interpolation to recover blobs from partial column data:

- `CanReconstruct(available)` — checks whether enough columns are available to reconstruct
- `ReconstructBlob(cells, indices)` — recovers a complete blob from a subset of cells
- `RecoverMatrix(sidecars)` — reconstructs the full data matrix from available sidecars

Reconstruction requires at least `ReconstructionThreshold` (50%) of columns to be available.

### Cell Gossip (gossip)

Manages pub/sub gossip of individual cells across the p2p network. The `CellGossipHandler` subscribes to column-specific subnets, validates incoming `CellGossipMessage` entries, deduplicates them, and forwards valid cells to the local storage layer.

- `NewCellGossipHandler(config)` — creates a handler for cell gossip routing
- `CellGossipMessage` — carries a cell and its associated KZG proof for a specific column

### Custody Proofs (custody)

Implements the custody proof challenge-response protocol, allowing nodes to prove they hold custody of their assigned data columns without revealing the full data:

- `CreateChallenge(validatorIdx, slot, column)` — generates a custody challenge
- `GenerateCustodyProof(challenge, cells)` — produces a proof for a challenge
- `RespondToChallenge(challenge, cells)` — responds with a proof
- `VerifyCustodyProof(challenge, proof)` — verifies a custody proof response

### Block Erasure Assembly (blockerasure)

Manages the block-in-blobs encoding pipeline where block data is split into erasure-coded pieces for distribution:

- `BlockAssemblyManager` — coordinates piece collection and block reassembly
- `NewBlockAssemblyManager(config)` — creates a manager with configurable redundancy
- `BlockPiece` — represents a single erasure-coded piece of a block

### Variable-Size Blobs (varblob)

Supports the variable-size blob protocol from the J+ roadmap upgrade. `BlobConfig` describes a blob's negotiated size, bounded by `MinBlobSizeBytes` and `MaxBlobSizeBytes`, with `DefaultBlobSize` for backward compatibility.

### Blob Streaming (streaming)

Implements the blob streaming pipeline for efficient propagation of large data volumes. A `BlobStreamer` sends blob data incrementally as `BlobChunk` fragments, enabling receivers to begin processing before the full blob arrives. A `StreamManager` handles concurrent streaming sessions.

### Bandwidth Enforcement and Teragas Pipeline (teragas)

Enforces the Teragas L2 bandwidth budget (targeting 1 Gbyte/sec) through:

- `BandwidthEnforcer` — rate-limits blob data ingestion per the configured budget
- `StreamingPipeline` — orchestrates the full blob streaming and delivery pipeline
- `BandwidthConfig` — configures throughput limits and backpressure parameters

### Blob Futures Market (futures)

Implements the short-dated blob futures protocol (Hegotá upgrade). A `FuturesMarket` allows parties to reserve future blob space at a committed price, enabling L2 sequencers to guarantee data availability capacity in advance.

### Post-Quantum Blob Commitments (pqblob)

Provides post-quantum secure blob commitments using MLWE lattice cryptography (I+ and beyond):

- `CommitBlob(data)` — produces a lattice-based commitment to blob data
- `VerifyBlobCommitment(data, commitment)` — verifies a commitment
- `GenerateBlobProof(data, commitment)` — generates an opening proof

### Sample Size Optimization (sampleopt)

Adaptive sampling that reduces required samples when network conditions allow, per the "decrease sample size" roadmap item:

- `SampleOptimizer` — tracks historical availability and adjusts `SamplingPlan` dynamically
- `SamplingVerdict` — records the outcome of a completed sampling round

## Subpackages

| Subpackage | Description |
|------------|-------------|
| [`blobpool/`](./blobpool/) | Sparse blob pool with WAL and custody tracking (EIP-8070) |
| [`blobs/`](./blobs/) | Block-in-blobs encoding and Teradata throughput manager |
| [`blockerasure/`](./blockerasure/) | Erasure-coded block assembly and reassembly |
| [`cell/`](./cell/) | Cell-level data structures and validation |
| [`custody/`](./custody/) | Custody proof challenge-response protocol |
| [`dastypes/`](./dastypes/) | EIP-7594 constants and core type definitions |
| [`erasure/`](./erasure/) | Reed-Solomon erasure coding for blob reconstruction |
| [`field/`](./field/) | BLS12-381 field arithmetic (Montgomery form) |
| [`futures/`](./futures/) | Short-dated blob futures market |
| [`gossip/`](./gossip/) | Cell gossip handler and subnet pub/sub routing |
| [`network/`](./network/) | DAS network layer and peer coordination |
| [`pqblob/`](./pqblob/) | Post-quantum lattice-based blob commitments |
| [`proof/`](./proof/) | KZG proof generation and batch verification |
| [`reconstruction/`](./reconstruction/) | Reed-Solomon Lagrange blob reconstruction |
| [`rpo/`](./rpo/) | Rescue Prime Optimized hash function |
| [`sampleopt/`](./sampleopt/) | Adaptive sample size optimization |
| [`sampling/`](./sampling/) | Custody group/column assignment per EIP-7594 |
| [`streaming/`](./streaming/) | Blob streaming pipeline with chunked transfer |
| [`teragas/`](./teragas/) | Bandwidth enforcer and streaming pipeline for Teragas |
| [`validator/`](./validator/) | DAS validator duties and sampling verification |
| [`varblob/`](./varblob/) | Variable-size blob configuration (J+ roadmap) |

## Usage

```go
import "github.com/eth2030/eth2030/das"

// Determine which columns a node should custody.
nodeID := myNode.ID()
columns, err := das.GetCustodyColumns(nodeID, das.CustodyRequirement)
if err != nil {
    return err
}

// Verify an incoming data column sidecar.
if err := das.VerifyDataColumnSidecar(sidecar); err != nil {
    return fmt.Errorf("invalid sidecar: %w", err)
}

// Check if reconstruction is possible from available sidecars.
if das.CanReconstruct(availableSidecars) {
    blob, err := das.ReconstructBlob(cells, indices)
    if err != nil {
        return err
    }
    _ = blob
}

// Start the cell gossip handler for custody columns.
handler := das.NewCellGossipHandler(das.CellGossipHandlerConfig{
    CustodyColumns: columns,
    Subnet:         p2pHost,
})

// Custody proof challenge-response.
challenge := das.CreateChallenge(validatorIdx, slot, columnIndex)
proof, err := das.GenerateCustodyProof(challenge, myCells)
if err != nil {
    return err
}
if err := das.VerifyCustodyProof(challenge, proof); err != nil {
    return fmt.Errorf("custody proof invalid: %w", err)
}

// Bandwidth-enforced blob streaming (Teragas).
enforcer := das.NewBandwidthEnforcer(das.DefaultBandwidthConfig())
pipeline := das.NewStreamingPipeline(enforcer)
```

## Documentation References

- [Roadmap Deep-Dive](../../docs/ROADMAP-DEEP-DIVE.md)
- [Design Doc](../../docs/DESIGN.md)
- [GAP Analysis](../../docs/GAP_ANALYSIS.md)
- [EIP-7594: PeerDAS](https://eips.ethereum.org/EIPS/eip-7594)
- [EIP-4844: Shard Blob Transactions](https://eips.ethereum.org/EIPS/eip-4844)
