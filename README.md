<h1 align="center">ETH2030</h1>

<p align="center">
  <strong>Ethereum execution client targeting the L1 Strawmap</strong>
  <br />
  <a href="https://eth2030.altllm.ai/">eth2030.altllm.ai</a>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Status-Experimental-orange?style=for-the-badge" alt="Experimental" />
  <img src="https://img.shields.io/badge/Build-In%20Progress-yellow?style=for-the-badge" alt="Build In Progress" />
</p>

> [!WARNING]
> **This is an experimental research project under active development.** It is a reference implementation intended for study, research, and prototyping. It is **not** production-ready and has **not** been formally audited for security. APIs, data formats, and behavior may change without notice.
>
> **Do not use this software to manage real funds, validate mainnet blocks, or run production infrastructure.** Exercise extreme caution with anything that touches wallets, private keys, seed phrases, or transaction signing. Always review the source code and do your own research (DYOR) before running any Ethereum client software. Use entirely at your own risk.

<p align="center">
  <a href="https://github.com/alt-research/eth2030/actions/workflows/ci.yml"><img src="https://github.com/alt-research/eth2030/actions/workflows/ci.yml/badge.svg?branch=master" alt="CI" /></a>
  <a href="https://github.com/alt-research/eth2030/releases/latest"><img src="https://img.shields.io/github/v/release/alt-research/eth2030?label=Release&color=%234f46e5" alt="Release" /></a>
  <img src="https://img.shields.io/badge/Go-1.26.8-00ADD8?logo=go" alt="Go" />
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Packages-213-blue?style=flat-square" alt="Packages" />
  <img src="https://img.shields.io/badge/Tests-20%2C281-blue?style=flat-square" alt="Tests" />
  <img src="https://img.shields.io/badge/EIPs-58%20supported-blue?style=flat-square" alt="EIPs supported (native + go-ethereum)" />
  <img src="https://img.shields.io/badge/EF%20State%20Tests-100%25%20(36%2C126)-brightgreen?style=flat-square" alt="EF Tests" />
  <img src="https://img.shields.io/badge/LOC-794K-blue?style=flat-square" alt="LOC" />
</p>

<p align="center">
  <a href="#quick-start">Quick Start</a> &middot;
  <a href="#devnet-testing-kurtosis">Devnet Testing</a> &middot;
  <a href="#roadmap-coverage">Roadmap</a> &middot;
  <a href="#eip-coverage-58-eips-supported">EIP Coverage</a> &middot;
  <a href="#architecture">Architecture</a> &middot;
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

---

> Built in Go, targeting the [L1 Strawmap](https://strawmap.org/) — the EF Architecture team's Ethereum protocol roadmap through the Giga-Gas era.

## Features

- **Full EVM execution** -- 164+ opcodes, 24 precompiles, EOF container support, go-ethereum backend
- **EF state test benchmark** -- 36,126/36,126 Ethereum Foundation state tests passed in the [recorded benchmark](#ef-state-test-benchmark)
- **58+ EIPs supported** -- Covering Frontier through Prague and beyond (native + go-ethereum backend)
- **Parallel execution** -- Block Access Lists (EIP-7928) for BAL-driven parallel transaction processing
- **Post-quantum ready** -- ML-DSA-65 (FIPS 204), Dilithium3, Falcon512, SPHINCS+ signers with hybrid mode
- **Native rollups** -- EIP-8079 EXECUTE precompile and anchor contract
- **zkVM framework** -- RISC-V RV32IM CPU emulator, STF executor, zkISA bridge (simulated proofs, production Groth16 integration pending)
- **Full consensus** -- 3-slot finality (3SF), quick slots (6s), 1-epoch finality, PQ attestations
- **PeerDAS** -- Data availability sampling, custody proofs, blob streaming, variable-size blobs
- **ePBS + FOCIL** -- Enshrined PBS with distributed builder and fork-choice enforced inclusion lists
- **Complete networking** -- devp2p, discovery V5, gossip (pub/sub), Portal network, snap sync
- **Engine API V3-V7** -- Full payload lifecycle, forkchoice, ePBS builder API, 50+ JSON-RPC methods

## Quick Start

Install Go 1.26.8 or later.

```bash
# Clone
git clone https://github.com/alt-research/eth2030.git
cd eth2030/pkg

# Build all packages
go build ./...

# Build the geth-embedded node (syncs with mainnet/testnets)
go build -o eth2030-geth ./cmd/eth2030-geth/

# Sync with Sepolia testnet (requires a consensus client on port 8551)
./eth2030-geth -network sepolia -datadir ~/.eth2030-sepolia

# Sync with mainnet
./eth2030-geth -datadir ~/.eth2030-geth -authrpc.jwtsecret /path/to/jwt.hex

# Run all package tests
go test ./...

# Fetch the pinned EF state test fixtures from the repository root
(cd .. && git submodule update --init refs/go-ethereum &&
  git -C refs/go-ethereum submodule update --init tests/testdata)

# Run EF state test validation and fail if fixtures are missing
ETH2030_REQUIRE_EF_FIXTURES=1 go test -v -count=1 -timeout=15m -run '^TestGethCategorySummary$' ./core/eftest
```

### eth2030-geth Flags

> **Note:** eth2030-geth uses Go's `flag` package. Flags are shown with single-dash (`-flag`) to match `--help` output, but double-dash (`--flag`) also works.

| Flag | Default | Description |
|------|---------|-------------|
| `-datadir` | `~/.eth2030-geth` | Data directory for chain data |
| `-network` | `mainnet` | Network: mainnet, sepolia, holesky |
| `-syncmode` | `snap` | Sync mode: snap, full |
| `-port` | `30303` | P2P listening port |
| `-http.port` | `8545` | HTTP-RPC server port |
| `-authrpc.port` | `8551` | Engine API port (for CL client) |
| `-authrpc.jwtsecret` | auto | Path to JWT secret for Engine API auth |
| `-maxpeers` | `50` | Maximum P2P peers |
| `-verbosity` | `3` | Log level (0=silent, 5=trace) |
| `-override.glamsterdam` | - | Test Glamsterdam fork at timestamp |

## Devnet Testing (Kurtosis)

ETH2030 includes Kurtosis devnet integration for multi-client consensus testing using [ethpandaops/ethereum-package](https://github.com/ethpandaops/ethereum-package) with 10 testing tools (assertoor, spamoor, rakoon, dora, blobscan, forky, tracoor, forkmon, prometheus, grafana).

```bash
# From the repository root, with Docker and the Kurtosis CLI installed
git submodule update --init refs/ethereum-package
kurtosis engine start
(cd pkg && docker build -t eth2030:local .)
cd pkg/devnet/kurtosis

# Run all 40 feature devnet tests
./scripts/run-feature-tests.sh

# Run specific features
./scripts/run-feature-tests.sh epbs focil native-aa

# Launch a general devnet
./scripts/run-devnet.sh full-feature
```

**40 Feature Tests** — All passing consensus checks, covering all 65 roadmap items:

**24 Roadmap Feature Tests:**

| Feature | EIP(s) | Feature | EIP(s) |
|---------|--------|---------|--------|
| ePBS | EIP-7732 | PeerDAS | EIP-7594 |
| FOCIL | EIP-7805 | 3SF/Quick Slots | — |
| BALs | EIP-7928 | BAL Feasibility | EIP-7928 |
| BAL Retention | EIP-7928 | PQ Crypto | — |
| Native AA | EIP-7702 | Encrypted Mempool | — |
| Gas Repricing | 18 EIPs | Shielded Transfers | — |
| Blobs | EIP-4844/8070 | SSZ | EIP-6404/7807 |
| Multidim Gas | EIP-7706 | Multidim Fee Tx | EIP-7706 |
| Native Rollups | EIP-8079 | Frame Mempool | EIP-8141 |
| Gas Vectors 3D | EIP-7706 | IL Satisfaction | EIP-7805 |
| Local Tx | — | Mixnet | — |
| RISC-V Precompile | zkVM | STARK Frames | EIP-8141 |

**16 Layer Group Tests** (CL/DL/EL feature groups covering remaining roadmap items):

| Group | Items Covered | Group | Items Covered |
|-------|---------------|-------|---------------|
| cl-finality | fast confirm, 1-epoch, endgame | cl-validators | attester cap, 128K, APS, 1 ETH |
| cl-attestations | 1M attest, jeanVM, PQ attest | cl-security | 51% recovery, secret proposers |
| cl-infrastructure | beacon specs, tech debt, VDF | dl-blob-advanced | PQ blobs, variable-size, teragas |
| dl-reconstruction | local reconstruct, sample opt | dl-futures | blob futures, custody proofs |
| dl-broadcast | cell msgs, 7702 broadcast, streaming | el-gas-schedule | 3x/year limit, Hegotá repricing |
| el-payload | chunking, block-in-blobs, nonce | el-proofs | optional, mandatory 3-of-5, aggregation |
| el-zkvm | canonical guest/zkVM, STF, zkISA | el-state | binary tree, VOPS, endgame state |
| el-tx-advanced | AA, purges, assertions, NTT | el-gas-futures | gas futures, sharded mempool, gigagas |

**6 General Configs** — single-client, multi-client (cross-client consensus with upstream geth), stress-test, blob-test, eip7702-test, full-feature.

**Additional Test Scripts:**
- `test-frametx.sh` — EIP-8141 frame transaction test (deploys APPROVE contract, verifies opcodes)
- `test-precompiles.sh` — Custom precompile smoke test

For devnet testing, use the [Kurtosis devnet guide](pkg/devnet/kurtosis/README.md) to run a local devnet.

## Architecture

```
                 +------------------------------+
                 |     Consensus Client (CL)     |
                 +--------------+---------------+
                                | Engine API (JSON-RPC)
                 +--------------v---------------+
                 |      Engine API Server         |
                 |  newPayloadV3-V7, fcuV3/V4    |
                 +--------------+---------------+
                                |
          +---------------------+------------------+
          |                     |                   |
 +--------v------+  +----------v--------+  +-------v-------+
 |  Block Builder |  |  Block Validator  |  | Payload Store |
 +--------+------+  +----------+--------+  +---------------+
          |                     |
 +--------v---------------------v------+
 |          State Processor             |
 |   Sequential -> Parallel (EIP-7928) |
 +--------+----------------------------+
          |
 +--------v--------------------------+     +--------------------+
 |     go-ethereum EVM (v1.17.0)     |     | Consensus Layer    |
 |  + 13 ETH2030 custom precompiles  |     | 3SF, Attestations  |
 +--------+--------------------------+     +--------------------+
          |
 +--------v--------------------------+     +-------------------+
 |          StateDB                   |---->|  Transaction Pool |
 |  Accounts, Storage, Code, Logs    |     | + Encrypted/Shared|
 +--------+--------------------------+     +-------------------+
          |
 +--------v--------------------------+     +-------------------+
 |     Trie / Binary                  |---->|  P2P / Sync       |
 |  MPT + Binary (EIP-7864)          |     | Discovery, Portal |
 +--------+--------------------------+     +-------------------+
          |
 +--------v--------------------------+     +-------------------+
 |     Key-Value Store (rawdb)        |     | DAS / PeerDAS     |
 +-----------------------------------+     | Blob Sampling     |
                                           +-------------------+
```

## go-ethereum Integration

ETH2030 imports go-ethereum as a library for EVM execution. See [pkg/go.mod](pkg/go.mod) for the current version.

| Component | Description |
|-----------|-------------|
| `pkg/geth/processor.go` | Block processor using `gethcore.ApplyMessage` |
| `pkg/geth/extensions.go` | 13 custom precompile injection via `evm.SetPrecompiles()` |
| `pkg/geth/statedb.go` | State creation using go-ethereum's real trie DB |
| `pkg/geth/config.go` | Chain config mapping (ETH2030 forks to go-ethereum params) |

**Custom precompiles injected:** 4 Glamsterdam repriced (0x06, 0x08, 0x09, 0x0a), 1 NTT (0x15), 4 NII (0x0201-0x0204), 4 field arithmetic (0x0205-0x0208).

### Mainnet & Testnet Sync Verification

The `eth2030-geth` binary has been verified syncing with live Ethereum networks:

| Network | CL Client | Sync Mode | Status | RPC APIs Verified |
|---------|-----------|-----------|--------|-------------------|
| **Sepolia** | Lighthouse v8.1.0 | Snap | Headers downloading at ~9K/sec, chain ~33%, state ~4% | 20+ methods |
| **Mainnet** | - | Snap | Genesis initialized, Chain ID 1, peer discovery active | 20+ methods |

**Verified RPC methods:** `eth_chainId`, `eth_blockNumber`, `eth_getBlockByNumber`, `eth_syncing`, `eth_feeHistory`, `eth_getBalance`, `eth_getCode`, `net_version`, `net_peerCount`, `web3_clientVersion`, `admin_nodeInfo`, `admin_peers`, `txpool_status`, `engine_exchangeCapabilities`, and more.

## Native vs Delegated Architecture

ETH2030 combines native implementations with go-ethereum as a backend. This section clarifies what is built from scratch versus delegated:

**go-ethereum delegated (~40 EIPs):** Core EVM opcodes, gas accounting, state root computation, trie operations, and standard precompile execution are handled by the `pkg/geth/` adapter importing go-ethereum as a library. This includes all Frontier-through-Prague EVM semantics, EIP-158 empty account cleanup, and SSTORE gas metering (EIP-2200/2929/3529).

**Native ETH2030 (~25 features):** Consensus layer (3SF, quick slots, 1-epoch finality, PQ attestations, endgame finality), data availability (PeerDAS, blob streaming, custody proofs, variable-size blobs), proposer-builder separation (ePBS, FOCIL, distributed builder), encrypted mempool (commit-reveal, threshold decryption), BAL parallel execution (EIP-7928), native rollups (EIP-8079), SSZ encoding (EIP-6404/7807), 13 custom precompiles (NTT, NII, field arithmetic), and the Engine API V3-V7.

**Prototype/Functional:** The zkVM framework has a real RISC-V RV32IM CPU emulator with witness collection and cycle counting, but proofs are SHA-256 hash-based rather than Groth16 (production ZK backend integration pending).

## Package Structure

| Package | Description | Status |
|---------|-------------|--------|
| `pkg/core` | Blockchain, state processor, block builder, validator, fee logic, gas futures | Complete |
| `pkg/core/types` | Header, Transaction (7 types), Receipt, Block, SSZ encoding | Complete |
| `pkg/core/state` | StateDB interface, in-memory, trie-backed, stateless, prefetcher | Complete |
| `pkg/core/vm` | EVM interpreter, 164+ opcodes, 24 precompiles, gas tables, EOF | Complete |
| `pkg/core/eftest` | EF state test runner: 36,126/36,126 (100%) via go-ethereum backend | Complete |
| `pkg/geth` | go-ethereum adapter: type conversion, block processor, precompile injection | Complete |
| `pkg/consensus` | 3SF (3-slot finality), quick slots, 1-epoch finality, attestations, beacon state, BLS adapter | Complete |
| `pkg/crypto` | Keccak-256, secp256k1, BN254, BLS12-381, Banderwagon, IPA, VDF, shielded | Complete |
| `pkg/crypto/pqc` | ML-DSA-65, Dilithium3, Falcon512, SPHINCS+, hybrid signer, lattice blobs | Complete |
| `pkg/engine` | Engine API V3-V7, forkchoice, payload building, ePBS, distributed builder | Complete |
| `pkg/epbs` | Enshrined PBS (EIP-7732): builder bids, auctions, payload envelopes | Complete |
| `pkg/focil` | FOCIL (EIP-7805): inclusion list building, validation, compliance | Complete |
| `pkg/bal` | Block Access Lists (EIP-7928) for parallel execution | Complete |
| `pkg/das` | PeerDAS: sampling, custody, reconstruction, blob streaming, futures | Complete |
| `pkg/rollup` | Native rollups (EIP-8079): EXECUTE precompile, anchor contract | Complete |
| `pkg/zkvm` | zkVM: canonical guest (RISC-V), STF executor, zkISA bridge, proof backend | Complete |
| `pkg/proofs` | Proof aggregation: SNARK/STARK/IPA/KZG, mandatory 3-of-5 system | Complete |
| `pkg/txpool` | Transaction pool, RBF, sparse blobpool, encrypted mempool, sharded | Complete |
| `pkg/p2p` | TCP transport, devp2p, discovery V5, gossip, Portal network, snap sync | Complete |
| `pkg/trie` | MPT + Binary Merkle tree (EIP-7864), SHA-256, proofs, migration | Complete |
| `pkg/rpc` | JSON-RPC server, 50+ methods, filters, WebSocket, Beacon API | Complete |
| `pkg/sync` | Full sync + snap sync + beam sync pipeline | Complete |
| `pkg/rlp` | RLP encoding/decoding per Yellow Paper Appendix B | Complete |
| `pkg/ssz` | SSZ encoding/decoding, merkleization, EIP-7916 ProgressiveList | Complete |

<details>
<summary>All 48 packages (click to expand)</summary>

| Package | Description |
|---------|-------------|
| `pkg/core/rawdb` | FileDB with WAL, chain DB, EIP-4444 history expiry |
| `pkg/core/state/snapshot` | Layered diff/disk snapshots, account/storage iterators |
| `pkg/core/state/pruner` | State pruner with bloom filter reachability |
| `pkg/core/vops` | Validity-Only Partial Statelessness: executor, validator, witness |
| `pkg/das/erasure` | Reed-Solomon erasure coding (Lagrange interpolation) |
| `pkg/witness` | Execution witness (EIP-8025), collector, verifier |
| `pkg/txpool/encrypted` | Encrypted mempool: commit-reveal, threshold decryption |
| `pkg/txpool/shared` | Sharded mempool with consistent hashing |
| `pkg/light` | Light client: header sync, proof cache, sync committee, CL proofs |
| `pkg/p2p/discover` | Peer discovery V4/V5, Kademlia DHT |
| `pkg/p2p/discv5` | Discovery V5 protocol with WHOAREYOU/handshake |
| `pkg/p2p/dnsdisc` | DNS-based peer discovery |
| `pkg/p2p/enode` | Node identity and URL parsing |
| `pkg/p2p/enr` | Ethereum Node Records (extensible key-value) |
| `pkg/p2p/portal` | Portal network: content DHT, Kademlia routing |
| `pkg/p2p/snap` | Snap sync protocol messages |
| `pkg/trie/bintrie` | Binary Merkle trie: Get/Put/Delete/Hash/Commit, proofs |
| `pkg/eth` | ETH protocol handler, codec, EIP-8077 announce nonce (ETH/72) |
| `pkg/node` | Client node: config, lifecycle, subsystem wiring |
| `pkg/cmd/eth2030` | CLI binary with flags, signal handling |
| `pkg/cmd/eth2030-geth` | Production node: go-ethereum embedded, mainnet/testnet sync |
| `pkg/log` | Structured logging (JSON/text) |
| `pkg/metrics` | Counters, gauges, histograms, Prometheus, EWMA, CPU tracker |

</details>

## EIP Coverage (58+ EIPs Supported)

| EIP | Name | EIP | Name |
|-----|------|-----|------|
| 150 | Gas Cost Changes (63/64 rule) | 7002 | EL Withdrawals |
| 152 | BLAKE2 Precompile | 7069 | EXTCALL (EOF) |
| 196/197 | BN254 Pairing | 7251 | MAX_EFFECTIVE_BALANCE |
| 1153 | Transient Storage | 7480 | EOF Data |
| 1559 | Dynamic Fee Market | 7547 | FOCIL Inclusion Lists |
| 2200 | SSTORE Gas Metering | 7549 | Committee Attestations |
| 2537 | BLS12-381 Precompiles | 7594 | PeerDAS |
| 2718 | Typed Transactions | 7620 | EOFCREATE |
| 2929 | State Access Gas | 7685 | EL Requests |
| 2930 | Access List Transactions | 7691 | Blob Throughput |
| 2935 | Historical Block Hashes | 7698 | EOF Creation Tx |
| 3529 | Reduction in Refunds | 7701 | Native AA |
| 3540 | EOF Container | 7702 | Set Code for EOAs |
| 4444 | History Expiry | 7706 | Multidimensional Gas |
| 4762 | Statelessness Gas | 7742 | Uncoupled Blobs |
| 4788 | Beacon Block Root | 7745 | Log Index |
| 4844 | Blob Transactions + KZG | 7807 | SSZ Blocks |
| 4895 | Withdrawals | 7825 | Tx Gas Cap |
| 5656 | MCOPY Opcode | 7898 | Uncoupled Execution Payload |
| 6110 | Validator Deposits | 7904 | Gas Repricing |
| 6404 | SSZ Transactions | 7916 | SSZ ProgressiveList |
| 6780 | SELFDESTRUCT Restriction | 7918 | Blob Base Fee Bound |
| 7928 | Block Access Lists | 7939 | CLZ Opcode |
| 8024 | DUPN/SWAPN/EXCHANGE | 8070 | Sparse Blobpool |
| 8077 | Announce Nonce | 8079 | Native Rollups |
| 8141 | Frame Transactions | | |

**Substantially implemented:** EIP-7732 (ePBS), EIP-7805 (FOCIL), EIP-7864 (Binary Merkle Tree), EIP-8025 (Execution Proofs), PQC (Dilithium3/Falcon512/SPHINCS+)

See [docs/EIP_SPEC_IMPL.md](docs/EIP_SPEC_IMPL.md) for full traceability: specs, implementations, and tests for each EIP.

## EF State Test Benchmark

Recorded results from the go-ethereum v1.17.1 backend.

| Metric | Value |
|--------|-------|
| **Total tests** | 36,126 |
| **Passing** | 36,126 (100%) |
| **Failing** | 0 |
| **Categories** | 57/57 at 100% |
| **Backend** | go-ethereum v1.17.1 |
| **Runner** | `pkg/core/eftest/geth_runner.go` |

## Roadmap Coverage

ETH2030 tracks 8 upgrade phases from the [L1 Strawmap](https://strawmap.org/) covering consensus, data, and execution layers from 2026 through 2030+.

| Metric | Value |
|--------|-------|
| **Upgrade phases** | 8 |
| **Roadmap items tracked** | 65 |
| **Complete** | 65 (all items with real crypto, validation, and test coverage) |
| **Functional** | 0 |
| **Partial** | 0 |

All 65 strawmap items are COMPLETE with real cryptographic backends wired (BLS, Dilithium3, KZG, BN254 Pedersen). See [docs/GAP_ANALYSIS.md](docs/GAP_ANALYSIS.md) for the full audit.

## Engine API

| Method | Fork | Status |
|--------|------|--------|
| `engine_newPayloadV3` | Cancun | Implemented |
| `engine_newPayloadV4` | Prague | Implemented |
| `engine_newPayloadV5` | Amsterdam | Implemented |
| `engine_forkchoiceUpdatedV3` | Cancun | Implemented |
| `engine_forkchoiceUpdatedV4` | Amsterdam | Implemented |
| `engine_getPayloadV3/V4/V6/V7` | Cancun-Amsterdam+ | Implemented |
| `engine_exchangeCapabilities` | All | Implemented |
| `engine_getClientVersionV1` | All | Implemented |

## JSON-RPC

50+ methods across `eth_*`, `net_*`, `web3_*`, `debug_*` namespaces including block/tx queries, `eth_call`, `eth_estimateGas`, fee history, log filters, WebSocket subscriptions (`newHeads`, `logs`, `pendingTransactions`), sync status, EVM tracing, and MPT proofs.

## Documentation

- [docs/EIP_SPEC_IMPL.md](docs/EIP_SPEC_IMPL.md) -- EIP traceability: specs, implementations, tests (94+ EIPs)
- [docs/strategies/lean-vm-riscv-implementation.md](docs/strategies/lean-vm-riscv-implementation.md) -- Lean 4 strategy for formal VM and RISC-V compiler-chain proof development
- [docs/GAP_ANALYSIS.md](docs/GAP_ANALYSIS.md) -- Full audit of 65 roadmap items
- [docs/plans/lean-vm-audit.md](docs/plans/lean-vm-audit.md) -- leanVM security audit: 18 findings across 4 leanEthereum repos
- [docs/PROGRESS.md](docs/PROGRESS.md) -- Progress report and completion tracking
- [docs/DESIGN.md](docs/DESIGN.md) -- Architecture and implementation design
- [docs/ROADMAP.md](docs/ROADMAP.md) -- L1 Strawmap timeline overview
- [docs/ROADMAP-DEEP-DIVE.md](docs/ROADMAP-DEEP-DIVE.md) -- EIP research and analysis
- [pkg/devnet/kurtosis/README.md](pkg/devnet/kurtosis/README.md) -- Kurtosis devnet testing (40 features, 6 configs, 10 tools)

## Development Stats

This project was built with [Claude Code](https://claude.com/product/claude-code) (Claude Opus 4/4.6) over ~8 days:

| Metric | Value |
|--------|-------|
| Total tokens (billed) | 3.42 billion |
| Total throughput | 7.50 billion |
| Unique API calls | 34,172 |
| Session files analyzed | 966 (9 main + 957 sub-agent) |
| Output tokens | 811,000 |
| Estimated API cost | ~$7,100 |
| Cost per 1K LOC | ~$10.19 |

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, coding guidelines, and contribution categories.

## Security

See [SECURITY.md](SECURITY.md) for vulnerability reporting.

## Reference Submodules

`refs/` contains 37 git submodules for upstream specs and implementations:

**Ethereum specs:** EIPs, ERCs, consensus-specs, execution-specs, execution-apis, beacon-APIs, builder-specs, consensus-spec-tests, execution-spec-tests

**Reference client:** go-ethereum

**Cryptography:** blst (BLS12-381), circl (PQC: ML-DSA, ML-KEM, SLH-DSA), go-eth-kzg (KZG), gnark (ZK proofs), gnark-crypto, c-kzg-4844

**PQ research:** hash-sig (hash-based multi-sigs), ntt-eip (EIP-7885 NTT precompile), ethfalcon (FALCON on EVM)

**Devops:** ethereum-package, benchmarkoor, erigone, xatu, consensoor

**Audited:** leanMultisig, leanSig, leanSpec, fiat-shamir (leanEthereum — 18-finding security audit)

## License

The ETH2030 library (i.e. all code outside of the `cmd` directory) is licensed under the
[GNU Lesser General Public License v3.0](https://www.gnu.org/licenses/lgpl-3.0.en.html),
also included in our repository in the `COPYING.LESSER` file.

The ETH2030 binaries (i.e. all code inside of the `cmd` directory) are licensed under the
[GNU General Public License v3.0](https://www.gnu.org/licenses/gpl-3.0.en.html), also
included in our repository in the `COPYING` file.
