# L1 Strawmap — ETH2030 Implementation

> Derived from the [L1 Strawmap](https://strawmap.org/) — the EF Architecture team's Ethereum protocol roadmap.
> Last updated: 2026-02-25
>
> **Note (2026-11-10):** this document reflects the Feb 2026 strawmap draft. The strawmap was
> since revised (Nov 2, 2026) — FOCIL and frame transactions (EIP-8141) are now Hegotá
> headliners, native rollups moved to L*, multidim pricing / partitioned binary tree to I*,
> plus new leanCL / leanDA / leanXMSS / leanSPHINCS / leanPCS items. The `/eips` pages on the
> site follow the **Nov 2026 revision** (see `site/src/data/eips.ts` and `docs/EIP_TRACKER.md`);
> fork placements here are kept as the historical derivation.

## Timeline Overview

| Year | Phase | Key Features | Status |
|------|-------|-------------|--------|
| 2026 | Glamsterdam | ePBS, FOCIL, BALs, native AA, repricing, sparse blobpool | ~99% |
| 2026-2027 | Hegotá | Blob throughput, multidim gas, SSZ blocks/tx, payload chunking | ~97% |
| 2027 | I+ | Native rollups, zkVM, VOPS, proof aggregation, PQ crypto | ~97% |
| 2027-2028 | J+ | Encrypted mempool, light client, variable blobs | ~95% |
| 2028 | K+ | 3SF, quick slots, mandatory proofs, canonical guest | ~97% |
| 2029 | L+ | Endgame finality, PQ attestations, blob streaming | ~97% |
| 2029+ | M+ | Gigagas, canonical zkVM, gas futures, PQ transactions | ~95% |
| 2030++ | Long term | VDF, distributed builders, shielded transfers | ~95% |

## Strawmap Layers

### Consensus Layer (CL)

| Track | Feature | Phase | Status |
|-------|---------|-------|--------|
| Latency | Fast confirmation | Glamsterdam | Done |
| Latency | Single-slot finality | K+ | Done |
| Latency | 1-epoch finality | K+ | Done |
| Latency | 4-slot epochs | K+ | Done |
| Latency | 6-sec slots (quick slots) | K+ | Done |
| Latency | Endgame finality | L+ | Done |
| Latency | Fast L1 finality in seconds | M+ | Done |
| Accessibility | ePBS | Glamsterdam | Done |
| Accessibility | FOCIL | Glamsterdam | Done |
| Accessibility | Modernized beacon state | Hegotá | Done |
| Accessibility | Attester stake cap | L+ | Done |
| Accessibility | 1 ETH includers | L+ | Done |
| Accessibility | APS (committee selection) | L+ | Done |
| Accessibility | PQ attestations | L+ | Done |
| Accessibility | Distributed block building | 2030++ | Done |
| Cryptography | PQ custody replacer | I+ | Done |
| Cryptography | PQ signature share | L+ | Done |
| Cryptography | Real-time CL proofs | L+ | Done |
| Cryptography | PQ L1 hash-based | M+ | Done |
| Cryptography | VDF randomness | 2030++ | Done |

### Data Layer (DL)

| Track | Feature | Phase | Status |
|-------|---------|-------|--------|
| Throughput | PeerDAS | Glamsterdam | Done |
| Throughput | Sparse blobpool (EIP-8070) | Glamsterdam | Done |
| Throughput | Blob throughput increase | Hegotá | Done |
| Throughput | Local blob reconstruction | Hegotá | Done |
| Throughput | Decrease sample size | I+ | Done |
| Throughput | PQ blobs | M+ | Done |
| Throughput | Teradata L2 | 2030++ | Done |
| Types | Blob streaming | L+ | Done |
| Types | Short-dated blob futures | L+ | Done |
| Types | Variable-size blobs | I+ | Done |
| Types | Custody proofs | L+ | Done |
| Types | Forward-cast blobs | M+ | Done |

### Execution Layer (EL)

| Track | Feature | Phase | Status |
|-------|---------|-------|--------|
| Throughput | Conversion repricing | Glamsterdam | Done |
| Throughput | Natural gas limit | Hegotá | Done |
| Throughput | Access gas limit | Hegotá | Done |
| Throughput | Multidimensional pricing | Hegotá | Done |
| Throughput | Block in blobs | K+ | Done |
| Throughput | Mandatory 3-of-5 proofs | K+ | Done |
| Throughput | Canonical guest | K+ | Done |
| Throughput | Canonical zkVM | M+ | Done |
| Throughput | Gas futures | M+ | Done |
| Throughput | Shared mempools | M+ | Done |
| Throughput | Gigagas L1 (1 Ggas/sec) | M+ | Done |
| Sustainability | BALS | Glamsterdam | Done |
| Sustainability | Binary tree | Hegotá | Done |
| Sustainability | Payload shrinking | Hegotá | Done |
| Sustainability | Advance state | L+ | Done |
| Sustainability | Native rollups | L+ | Done |
| Sustainability | Exposed ELSA | 2030++ | Done |
| EVM | Native AA | Glamsterdam | Done |
| EVM | Misc purges | Hegotá | Done |
| EVM | Transaction assertions | Hegotá | Done |
| EVM | NTT precompile(s) | I+ | Done |
| EVM | Precompiles in zkISA | J+ | Done |
| EVM | STF in zkISA | J+ | Done |
| EVM | Proof aggregation | L+ | Done |
| EVM | PQ transactions | M+ | Done |
| EVM | AA proofs | M+ | Done |
| Cryptography | Encrypted mempool | I+ | Done |
| Cryptography | NII precompile | I+ | Done |

## Post-Quantum Roadmap Alignment

Based on Vitalik's quantum resistance roadmap (Feb 27, 2026), eth2030 addresses all 4 vulnerable areas:

| Vulnerable Area | Solution | Package |
|----------------|----------|---------|
| CL BLS Signatures | STARK-aggregated Dilithium sigs | `pkg/consensus/stark_sig_aggregation.go` |
| DA KZG Commitments | Lattice-based blob commitments (MLWE) | `pkg/crypto/pqc/lattice_blob.go` |
| EOA ECDSA Signatures | EIP-8141 frame txs + PQ registry | `pkg/core/`, `pkg/crypto/pqc/registry.go` |
| Application-layer Proofs | Recursive STARK mempool aggregation | `pkg/proofs/stark_prover.go`, `pkg/txpool/stark_aggregation.go` |

Key infrastructure:
- **Pluggable hash backend**: Keccak256, SHA-256, BLAKE3 (Poseidon2 future)
- **STARK prover**: FRI-based over Goldilocks field (p = 2^64 - 2^32 + 1)
- **NTT precompile**: EIP-7885 aligned, BN254 + Goldilocks fields
- **500ms mempool ticks**: STARK aggregate of all validated transactions

See [PQ Roadmap Report](PQ_ROADMAP_REPORT.md) for full details.

## Key EIPs

- **EIP-7732**: Enshrined Proposer-Builder Separation (ePBS)
- **EIP-7885**: NTT precompile for lattice crypto and STARKs
- **EIP-7928**: Parallel EVM execution via access lists
- **EIP-4844**: Blob transactions with KZG commitments
- **EIP-7594**: PeerDAS (data availability sampling)
- **EIP-7702**: Set code for EOAs (native account abstraction)
- **EIP-7805**: FOCIL (fork-choice enforced inclusion lists)
- **EIP-8079**: Native rollups (EXECUTE precompile)
- **EIP-8141**: Frame transactions (programmable tx validation)
- **EIP-7251**: Increase MAX_EFFECTIVE_BALANCE (flexible staking)
- **EIP-4444**: History expiry (bound historical data retrieval)

## Project Stats

| Metric | Value |
|--------|-------|
| Packages | 48 |
| Source files | 991 |
| Test files | 918 |
| Source LOC | ~316,000 |
| Test LOC | ~397,000 |
| Passing tests | 18,257 |
| EIPs implemented | 58+ (complete), 5 (substantial) |
| Roadmap items | 65/65 COMPLETE |
| EF State Tests | 36,126/36,126 (100%) |
| Reference submodules | 30 |
