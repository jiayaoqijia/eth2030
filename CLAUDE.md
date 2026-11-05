# Repository Guidelines

- Project: ETH2030 -- Ethereum client targeting the L1 Strawmap roadmap
- GitHub issues/comments/PR comments: use literal multiline strings or `-F - <<'EOF'` for real newlines; never embed "\\n".

## Related Repositories (Sibling Directories)

This repo is part of a larger project structure with separate git repositories:
- `../dora/` - Dora block explorer with FOCIL (EIP-7805) support for viewing Inclusion Lists in Heze blocks
- `../lighthouse/` - Lighthouse CL fork with ePBS/FOCIL support
- `../spamoor/` - Transaction spammer for devnet testing

Each is an independent git repo; commit separately in each directory.

## L1 Strawmap (strawmap.org)

Source: [strawmap.org](https://strawmap.org/) — EF Architecture team (Ansgar Dietrichs, Barnabé Monnot, Francesco D'Amato, Justin Drake), published Feb 25, 2026.
Three layers, each with sub-tracks:

### Consensus Layer (CL)
- **Latency**: fast confirmation -> quick slots -> 1-epoch finality -> 4-slot epochs -> 6-sec slots (K+) -> endgame finality -> fast L1 finality in seconds (M+)
- **Accessibility**: ePBS -> FOCIL -> modernized beacon specs -> beacon & lean specs merge -> attester stake cap -> 128K attester cap -> APS -> 1 ETH includers -> tech debt reset -> post quantum attestations -> jeanVM aggregation -> 1M attestations/slot -> 51% attack auto-recovery -> distributed block building
- **Cryptography**: post quantum pubkey registry -> post quantum available chain -> real-time CL proofs -> post quantum L1 hash-based (M+) -> VDF randomness -> secret proposers

### Data Layer (DL)
- **Throughput**: sparse blobpool -> cell-level messages -> EIP-7702 broadcast -> Hegotá BPO blobs increase -> local blob reconstruction -> decrease sample size -> J* BPO blobs increase -> L* BPO blobs increase -> teragas L2 (1 Gbyte/sec)
- **Types**: blob streaming -> short-dated blob futures -> post quantum blobs -> variable-size blobs -> proofs of custody

### Execution Layer (EL)
- **Throughput**: Glamsterdam repricing -> optional proofs -> Hegotá repricing -> 3x/year gas limit -> multidimensional pricing -> payload chunking -> block in blobs -> announce nonce -> mandatory 3-of-5 proofs -> canonical guest -> canonical zkVM -> long-dated gas futures -> sharded mempool -> gigagas L1 (1 Ggas/sec)
- **Sustainability**: BALs -> binary tree -> announce nonce -> validity-only partial state -> endgame state
- **EVM**: native AA -> misc purges -> transaction assertions -> NTT precompile(s) -> precompiles in zkISA -> STF in zkISA -> native rollups -> proof aggregation -> exposed zkISA -> AA proofs
- **Cryptography**: encrypted mempool -> post quantum transactions -> private L1 shielded transfers

### Five North Stars
- **Fast L1**: Finality in seconds
- **Gigagas L1**: 1 Ggas/sec (10K TPS)
- **Teragas L2**: 1 Gbyte/sec (10M TPS)
- **Post-Quantum L1**: Quantum-resistant cryptography throughout
- **Private L1**: Shielded transfers and encrypted mempool

### Upgrade Timeline (implementation status)
- **Glamsterdam** (2026) ~99%: fast confirmation, ePBS, FOCIL, sparse blobpool (EIP-8070), native AA (EIP-7702+7701), BALs (EIP-7928), repricing (18 EIPs), EIP-7708 ETH logs, EIP-7685 requests, EIP-8141 frame transactions (APPROVE, TXPARAM opcodes), EIP-7918 blob base fee bound
- **Hegotá** (2026-2027) ~97%: BPO blob schedules (BPO1/BPO2), EIP-7742 uncoupled blobs, EIP-7898 uncoupled execution payload, multidim gas (EIP-7706), gas limit schedule, binary tree, payload chunking, block-in-blobs, tx assertions, NTT precompile, SSZ transactions (EIP-6404), EIP-7807 SSZ blocks, EIP-7745 log index, EIP-7916 SSZ ProgressiveList, EIP-8077 announce nonce, encrypted mempool (threshold crypto + ordering + reveal), blob streaming, cell-level messages
- **I+** (2027) ~97%: binary trie, state access events, native rollups (EIP-8079 + anchor state), zkVM framework + STF executor + RISC-V CPU + zkISA bridge + proof round-trip, VOPS (partial statelessness + complete validator), proof aggregation, post-quantum crypto (ML-DSA-65 real signer, algorithm registry), EIP-7251 MAX_EFFECTIVE_BALANCE, validator consolidation, CL proof generator, stateless execution, beam sync, blob futures, sample optimization
- **J+** (2027-2028) ~95%: STF in zkISA framework, light client (proof cache + sync committee), variable-size blobs, Reed-Solomon blob reconstruction pipeline, block-in-blobs encoding, BPO3 schedule
- **K+** (2028) ~97%: 6-sec slots (quick slots framework), 3SF (3-slot finality), 4-slot epochs, 1-epoch finality, mandatory 3-of-5 proofs, canonical guest (RISC-V CPU), announce nonce, 1M attestations (parallel BLS, 64 subnets, batch verifier, scaler), CL proof circuits, proof aggregation round-trip
- **L+** (2029) ~97%: endgame finality (BLS adapter with real pairing hooks), PQ attestations (Dilithium + PQ chain security), BPO4 blobs increase, validity-only state, attester stake cap, APS (committee selection), blob streaming, custody proofs, cell gossip routing, distributed block builder + ePBS bid adapter, 1 ETH includers, jeanVM aggregation (Groth16 ZK-circuit BLS), secret proposers (VRF election)
- **M+** (2029+) ~95%: fast L1 finality in seconds, PQ L1 hash-based (unified hash signer XMSS/WOTS+), gigagas L1 (work-stealing + sharded state + integration tests), canonical zkVM (RISC-V RV32IM + witness + proof backend + verify), gas futures market (contracts + settlement), PQ blob commitments (MLWE lattice), sharded mempool (resize), proof aggregation, real-time CL proofs
- **Longer term** (2030++) ~95%: distributed block building, VDF randomness (Wesolowski scheme), teragas L2 (bandwidth enforcer + streaming pipeline), private L1 shielded transfers (BN254 Pedersen commitments + nullifier set + ZK circuits), 51% attack auto-recovery, AA proof circuits (nonce/sig/gas constraints + Groth16), exposed zkISA (bridge with 9 op selectors), unified beacon state

### Gap Summary (see docs/GAP_ANALYSIS.md for full audit of 65 roadmap items)
- **COMPLETE**: 65 items (all roadmap items have validation, edge-case handling, and test coverage)
- **FUNCTIONAL**: 0 items
- **PARTIAL**: 0 items
- **MISSING**: 0
- **STUB**: 0

### Statistics
- **Packages**: 213 (all passing)
- **Source**: 1,184 files, ~350K LOC
- **Tests**: 1,090 files, ~444K LOC, 20,281 tests
- **Total**: 2,274 files, ~794K LOC
- **EIPs**: 58 complete, 5 substantial
- **EF State Tests**: 36,126/36,126 passing (100%) via go-ethereum v1.17.10 backend in `pkg/core/eftest/`
- **go-ethereum integration**: `pkg/geth/` adapter with 13 custom precompile injection + `eth2030-geth` binary for mainnet/testnet sync (verified: Sepolia snap sync via Lighthouse CL, mainnet startup, 20+ RPC methods tested)
- **Security audits**: leanVM audit (18 findings across 4 leanEthereum repos, 9 PRs submitted: 2 merged, 4 open, 3 closed)

### EIP Implementation Status
- **Complete** (58): EIP-150, EIP-152, EIP-196/197, EIP-1153, EIP-1559, EIP-2200, EIP-2537, EIP-2718, EIP-2929, EIP-2930, EIP-2935, EIP-3529, EIP-3540, EIP-4444, EIP-4762, EIP-4788, EIP-4844 (incl. KZG), EIP-4895, EIP-5656, EIP-6110, EIP-6404, EIP-6780, EIP-7002, EIP-7069, EIP-7251, EIP-7480, EIP-7547, EIP-7549, EIP-7594, EIP-7620, EIP-7685, EIP-7691, EIP-7698, EIP-7701, EIP-7702, EIP-7706, EIP-7742, EIP-7745, EIP-7807, EIP-7825, EIP-7898, EIP-7904, EIP-7916, EIP-7918, EIP-7928, EIP-7939, EIP-8024, EIP-8070, EIP-8077, EIP-8079, EIP-8141
- **Substantial** (5): EIP-7732 (ePBS), EIP-7864 (binary tree), EIP-7805 (FOCIL, incl. Dora explorer support + EIP-8141 frame tx IL satisfaction), EIP-8025 (execution proofs), PQC (Dilithium3/Falcon512/SPHINCS+)

## Project Structure & Module Organization

- `pkg/` - Go module root (`github.com/eth2030/eth2030`, go.mod here)
  - `core/types/` - Core types: Header, Transaction (7 types incl. FrameTx, AATx), Receipt, Block, Account, SSZ encoding (EIP-6404/7807), SetCode auth (EIP-7702), tx assertions, EL requests (EIP-7685), log index (EIP-7745), EIP-4844 blob tx utilities, EIP-4895 withdrawals
  - `core/state/` - StateDB interface, in-memory and trie-backed implementations, access events (EIP-4762), stateless StateDB (witness-backed), state prefetcher
  - `core/vm/` - EVM interpreter, 164+ opcodes (incl. CLZ, DUPN/SWAPN/EXCHANGE, APPROVE, TXPARAM*, CURRENT_ROLE, ACCEPT_ROLE, EOF: EXTCALL/EXTDELEGATECALL/EXTSTATICCALL/RETURNDATALOAD/DATALOAD/DATALOADN/DATASIZE/DATACOPY/EOFCREATE/RETURNCONTRACT), 24 precompiles (incl. 9 BLS12-381, NTT, NII: modexp/field-mul/field-inv/batch-verify), gas tables, EIP-4762 statelessness gas, EIP-7708 ETH transfer logs, EIP-8141 frame opcodes, EIP-7701 AA opcodes, EOF container (EIP-3540)
  - `core/rawdb/` - FileDB with WAL, block/receipt/tx storage, EIP-4444 history expiry
  - `core/` - State transition, gas repricing (18 EIPs), multidim gas (EIP-7706/7999), blob gas (BPO1/BPO2 + EIP-7918 base fee floor), gas limit schedule, payload chunking, block-in-blobs, frame execution (EIP-8141), EIP-6110 deposits, EIP-7002 withdrawals, EIP-7825 gas cap, EIP-7691 blob schedule, gas futures market (contracts + settlement), gigagas infrastructure, MEV protection (commit-reveal ordering), chain config extensions (fork ordering, validation, rules)
  - `core/state/snapshot/` - State snapshots: layered diff/disk architecture, account/storage iterators, pruner
  - `core/state/pruner/` - State pruner with bloom filter reachability
  - `core/vops/` - Validity-Only Partial Statelessness: partial executor, validator, witness integration, complete VOPS validator (access lists, storage proofs)
  - `core/eftest/` - EF state test runner: JSON parser, fixture loader, go-ethereum-backed execution (36,126/36,126 = 100%)
  - `geth/` - go-ethereum adapter: type conversion, chain config mapping, pre-state creation, state transition, block processor, custom precompile injection (13 precompiles: 4 Glamsterdam repriced, 1 NTT, 4 NII, 4 field), ETH2030 fork-level detection (imports go-ethereum v1.17.10 as library)
  - `rlp/` - RLP encoding/decoding
  - `consensus/` - Consensus layer: 3SF (3-slot finality), quick slots (6s, 4-slot epochs), 1-epoch finality, EIP-7251 validator balance (2048 ETH max EB), consolidation, APS (committee selection), EIP-7549 attestations, PQ attestations (Dilithium), attester stake cap, endgame finality, UnifiedBeaconState (merged v1/v2/modern), finality BLS adapter, CL proof circuits (SHA-256 Merkle), jeanVM aggregation (Groth16 ZK-circuit BLS), PQ chain security (SHA-3 fork choice)
  - `ssz/` - SSZ encoding/decoding, merkleization, EIP-7916 ProgressiveList
  - `crypto/` - Keccak-256, secp256k1 ECDSA, BN254, BLS12-381, Banderwagon, IPA proofs, VDF (Wesolowski), shielded transfers (Pedersen commitments + ZK circuit), threshold crypto (Shamir SSS, Feldman VSS, ElGamal encryption)
  - `crypto/pqc/` - Post-quantum crypto: ML-DSA-65 (real lattice signer, FIPS 204), Dilithium3 (real lattice ops), Falcon512, SPHINCS+SHA256, unified hash signer (XMSS/WOTS+), hybrid signer, PQ algorithm registry, lattice-based blob commitments
  - `engine/` - Engine API server (V3-V7), forkchoice, payload building, ePBS builder API, EIP-7898 uncoupled payload, distributed block builder (registration, bids, auctions), Vickrey builder auction (second-price sealed-bid, slashing)
  - `trie/` - Binary Merkle tree (EIP-7864), SHA-256 hashing, proofs, MPT migration
  - `trie/bintrie/` - Binary Merkle trie (from go-ethereum), Get/Put/Delete/Hash/Commit, proofs
  - `bal/` - Block Access Lists (EIP-7928) for parallel execution, opcode-level state tracking (15 opcodes hooked via BALTracker interface), system contract tracking, SSTORE no-op detection, BAL-aware state prefetcher (async background prefetch + mutation overlay + read tracker)
  - `witness/` - Execution witness (EIP-8025), collector, verifier
  - `epbs/` - Enshrined Proposer-Builder Separation (EIP-7732): BuilderBid, PayloadEnvelope, auctions
  - `focil/` - Fork-Choice Enforced Inclusion Lists (EIP-7805): building, validation, compliance, EIP-8141 frame tx awareness in IL satisfaction (VERIFY target code-existence check, balance skip for AA payers)
  - `das/` - PeerDAS (EIP-7594): DataColumn, ColumnSidecar, sampling, custody, reconstruction, BLS12-381 field arithmetic, variable-size blobs, Reed-Solomon Lagrange interpolation, blob streaming, blob futures, custody proofs, cell gossip
  - `das/erasure/` - Reed-Solomon erasure coding for blob reconstruction
  - `rollup/` - Native rollups (EIP-8079): EXECUTE precompile, anchor contract
  - `zkvm/` - zkVM framework: guest programs, verification keys, prover backend, canonical guest (RISC-V executor, guest registry, precompile), STF executor (state transition proofs for zkISA), zkISA bridge (host ABI, EVM translation)
  - `proofs/` - Proof aggregation framework: ZKSNARK, ZKSTARK, IPA, KZG registry and aggregator, mandatory 3-of-5 proof system (prover assignment, submission, verification, penalties), AA proof circuits
  - `light/` - Light client: header sync, checkpoint store, verification, proof cache (LRU), sync committee verification, CL proof generator (state root, validator, balance proofs)
  - `txpool/` - Transaction pool with validation, replace-by-fee, eviction, EIP-8070 sparse blobpool (custody, WAL, price eviction), sharded mempool (consistent hashing)
  - `txpool/encrypted/` - Encrypted mempool: commit-reveal scheme, threshold decryption ordering
  - `p2p/` - P2P peer management, ETH wire protocol, discovery (V5 Kademlia DHT), ENR, enode, DNS discovery, Snap/1 protocol, Portal network (content DHT, Kademlia routing, history, state), gossip protocol (pub/sub, peer scoring, banning, deduplication), SetCode broadcast (EIP-7702 auth list gossip)
  - `sync/` - Full sync + snap sync pipeline, beam sync (stateless), trie sync (concurrent healing)
  - `rpc/` - JSON-RPC server, 50+ methods, filters, subscriptions, WebSocket, Beacon API (16 endpoints)
  - `eth/` - ETH protocol handler and codec, EIP-8077 announce nonce (ETH/72)
  - `node/` - Client node: config, lifecycle, subsystem integration
  - `log/` - Structured logging (JSON/text)
  - `metrics/` - Counters, gauges, histograms, Prometheus export, EWMA, meter, CPU tracker
  - `devnet/kurtosis/` - Kurtosis devnet configs (30 features + 6 general), scripts (consensus check, assertoor, precompile smoke test, EIP-8141 frame tx test, feature verify), ethpandaops tools integration (assertoor, spamoor, rakoon, dora, blockscout, blobscan, forky, tracoor, forkmon, prometheus, grafana). Public endpoints: devnet.eth2030.com (RPC), devnet-dora.eth2030.com (Dora), devnet-blockscout.eth2030.com (Blockscout)
- `cmd/eth2030/` - CLI binary with flags, signal handling
  - `cmd/eth2030-geth/` - Production binary embedding go-ethereum v1.17.10 as a library for real mainnet/testnet sync (Pebble DB, RLPx P2P, snap/full sync, Engine API on port 8551, JSON-RPC on port 8545, 13 custom precompiles injected at Glamsterdam/Hegotá/I+ fork levels; supports mainnet, sepolia, hoodi (holesky mapped to hoodi since geth v1.17.10 removed it))
- `internal/testutil/` - Shared test utilities
- `refs/` - Reference submodules (47 total, read-only, do NOT modify). See **[`refs/REFERENCES.md`](refs/REFERENCES.md)** for the full catalog: per-submodule details, ETH2030 relevance, and a priority matrix mapping each North Star goal to the relevant refs. Main upstream: https://github.com/orgs/ethereum/repositories
  - **Ethereum specs**: consensus-specs, execution-specs, consensus-spec-tests, execution-spec-tests, execution-apis, beacon-APIs, builder-specs, EIPs, ERCs
  - **Ethereum core**: go-ethereum
  - **Cryptography**: blst (BLS12-381), circl (PQC: ML-DSA/ML-KEM/SLH-DSA), go-eth-kzg (KZG commitments), gnark (ZK proofs), gnark-crypto (ZK curves), c-kzg-4844 (C-based KZG)
  - **PQ research**: hash-sig (Rust hash-based multi-sigs), ntt-eip (EIP-7885 NTT precompile), ethfalcon (FALCON on EVM with NTT)
  - **Utilities**: eth-utils, web3.py
  - **Governance**: pm, eip-review-bot, iptf-pocs
  - **Devops**: benchmarkoor, benchmarkoor-tests, ethereum-package, erigone, xatu, execution-processor, consensoor
  - **Audited**: leanMultisig, leanSig, leanSpec, fiat-shamir (leanEthereum — 18-finding security audit)
  - **Lean Consensus**: ream (Rust Lean CL client), leanroadmap (research progress tracker), ream-study-group (study notes + 60 weekly meeting notes), lean-spec-tests (Lean spec test vectors)
  - **EF Research**: research (Vitalik's PoCs: 3SF, erasure codes, KZG, Verkle, ZK, etc.)
- `tools/` - Research and data fetching tools
- `data/` - Downloaded research data (gitignored)
- `docs/` - Design docs, roadmap, deep-dive
- `.claude/` - Claude Code skills and settings

## Build, Test, and Development Commands

```bash
# Build all packages
cd pkg && go build ./...

# Build the eth2030-geth binary (embeds go-ethereum for real sync)
cd pkg && go build -o eth2030-geth ./cmd/eth2030-geth/

# Run all tests
cd pkg && go test ./...

# Run tests for a specific package
cd pkg && go test ./core/types/...
cd pkg && go test ./rlp/...
cd pkg && go test ./crypto/...
cd pkg && go test ./engine/...
cd pkg && go test ./bal/...
cd pkg && go test ./witness/...
cd pkg && go test ./core/state/...
cd pkg && go test ./core/vm/...
cd pkg && go test ./epbs/...
cd pkg && go test ./focil/...
cd pkg && go test ./das/...
cd pkg && go test ./rollup/...
cd pkg && go test ./zkvm/...
cd pkg && go test ./trie/bintrie/...
cd pkg && go test ./core/vops/...
cd pkg && go test ./crypto/pqc/...
cd pkg && go test ./light/...
cd pkg && go test ./proofs/...
cd pkg && go test ./txpool/encrypted/...
cd pkg && go test ./consensus/...
cd pkg && go test ./ssz/...

# Run tests with verbose output
cd pkg && go test -v ./...

# Run fuzz tests (38+ fuzz targets across 8 packages)
cd pkg && go test -fuzz=FuzzDecode ./rlp/ -fuzztime=30s
cd pkg && go test -fuzz=FuzzKeccak256 ./crypto/ -fuzztime=30s
cd pkg && go test -fuzz=FuzzArithmeticOps ./core/vm/ -fuzztime=30s
cd pkg && go test -fuzz=FuzzTransactionRLPRoundtrip ./core/types/ -fuzztime=30s

# --- Kurtosis Devnet Testing ---
# Prerequisites: Docker + Kurtosis CLI (https://docs.kurtosis.com/install/)
kurtosis engine start
cd pkg && docker build -t eth2030:local .

# Run all 30 feature devnet tests (consensus check + assertoor + feature verify)
cd pkg/devnet/kurtosis && ./scripts/run-feature-tests.sh

# Run specific features
cd pkg/devnet/kurtosis && ./scripts/run-feature-tests.sh epbs focil native-aa blobs

# Launch a general devnet (single-client, multi-client, stress-test, blob-test, eip7702-test, full-feature)
cd pkg/devnet/kurtosis && ./scripts/run-devnet.sh full-feature

# Check consensus across EL nodes
cd pkg/devnet/kurtosis && ./scripts/check-consensus.sh eth2030-devnet

# Smoke-test custom precompiles
cd pkg/devnet/kurtosis && ./scripts/test-precompiles.sh eth2030-devnet

# Cleanup
cd pkg/devnet/kurtosis && ./scripts/cleanup.sh eth2030-devnet
```

NOTE: The go.mod is in pkg/ (not project root) to avoid module conflicts with refs/ submodules.

## Test devnet chain

Use kurtosis to boot devnet then to check if blockchain is work well:

```bash
cd pkg/devnet/kurtosis && ./scripts/cleanup.sh eth2030-devnet && ./scripts/run-devnet.sh full-feature
```

When run finished check cl node 's log no error:

```bash
kurtosis service logs eth2030-devnet cl-1-lighthouse-geth
```

it will show logs from cl node, if had errors and failed, it means have some bugs for chain, also for el node:

```bash
kurtosis service logs eth2030-devnet el-1-geth-lighthouse
```

we need use this logs to find where cause chain errors, and use cast to got newest block:

```bash
cast bn -r http://$(kurtosis port print eth2030-devnet el-1-geth-lighthouse rpc)
```

if always 0, it means devnet boot failed, should check log and codes.

### FOCIL (EIP-7805) Testing

To verify FOCIL Inclusion Lists are working:

1. **Check CL API for IL data**: 
```bash
curl -s "http://$(kurtosis port print eth2030-devnet cl-1-lighthouse-geth http)/eth/v1/beacon/blocks/{slot}/inclusion_lists" | jq .
```

2. **View ILs in Dora explorer**: Open the Dora URL from the devnet output, navigate to a slot page, and click the "Inclusion Lists" tab to see:
   - Inclusion List Bits (satisfaction bitmap)
   - Satisfied/Not Satisfied status per IL
   - Transaction count and data for each IL

3. **Build and run local Dora** (for development):
```bash
cd ../dora && go build -o ../dora-config/dora-explorer ./cmd/dora-explorer
cd ../dora-config && ./dora-explorer --config dora-config-local.yaml

cast bn -r http://$(kurtosis port print eth2030-devnet el-1-geth-lighthouse rpc)
```

if always 0, it means devnet boot failed, should check log and codes.


## Coding Style & Naming Conventions

- Prefer strict typing; avoid loose types.
- Add brief code comments for tricky or non-obvious logic.
- Keep files concise; aim for under ~500 LOC.

## Testing Guidelines

- Naming: match source names with corresponding test files.
- Run tests before pushing when you touch logic.

## Commit Rules

- **Never add Co-Authored-By lines for Claude or any AI assistant in commits.** All commits are authored solely by the human committer.
- Follow concise, action-oriented commit messages (e.g., `evm: add EOF support`).
- Group related changes; avoid bundling unrelated refactors.
- Lint/format churn: auto-resolve formatting-only diffs without asking.

## Security & Configuration Tips

- Never commit or publish real private keys, mnemonics, or live configuration values.
- Use obviously fake placeholders in docs, tests, and examples.
- Environment variables for secrets; use cloud secrets managers in production.

## Reusable Libraries (refs/ submodules & ecosystem)

> **Full reference catalog**: [`refs/REFERENCES.md`](refs/REFERENCES.md) — details, ETH2030 relevance, and a North Star priority matrix for all 47 submodules across 8 categories (Specs, Test Vectors, Clients, Research, Crypto, Lean Ethereum, DevOps, Utilities).

When implementing missing functionality, check these production-grade Go libraries first before writing from scratch. All are in `refs/` or available as Go module dependencies.

### How to find reusable code for missing libs
1. Check `refs/REFERENCES.md` first for a quick map of which submodule to use for a given feature
2. Check `refs/` submodules directly (already cloned, read-only): `ls refs/` then explore relevant dirs
2. Search GitHub orgs: ethereum, consensys, ethpandaops, prysmaticlabs, cloudflare, supranational
3. Check go-ethereum's `go.sum` for vetted dependencies: `grep <lib> refs/go-ethereum/go.sum`
4. Validate license compatibility (Apache-2.0, MIT, BSD-3 are OK; GPL/AGPL are NOT)
5. Prefer pure-Go (no CGO) when possible for simpler builds

### Tier 1 -- Core (in refs/, production-ready)

| Library | Module | CGO | License | Use for |
|---------|--------|-----|---------|---------|
| **blst** | `github.com/supranational/blst` | Yes | Apache-2.0 | BLS12-381 signatures, pairing, aggregate verify |
| **go-eth-kzg** | `github.com/crate-crypto/go-eth-kzg` | No | Apache-2.0 | KZG commitments (EIP-4844 + EIP-7594 PeerDAS) |
| **c-kzg-4844** | `github.com/ethereum/c-kzg-4844/v2` | Yes | Apache-2.0 | KZG commitments (C, audited, faster) |
| **gnark** | `github.com/consensys/gnark` | No | Apache-2.0 | ZK-SNARK circuits (Groth16, PLONK) |
| **gnark-crypto** | `github.com/consensys/gnark-crypto` | No | Apache-2.0 | BLS12-381/BN254 field arithmetic, curves |
| **circl** | `github.com/cloudflare/circl` | No | BSD-3 | PQC: ML-DSA (FIPS 204), ML-KEM, SLH-DSA |

### Tier 2 -- Strong candidates

| Library | Module | CGO | License | Use for |
|---------|--------|-----|---------|---------|
| **fastssz** | `github.com/ferranbt/fastssz` | No | MIT | SSZ codec + code generator |
| **uint256** | `github.com/holiman/uint256` | No | BSD-3 | 256-bit math (replaces big.Int) |
| **gohashtree** | `github.com/prysmaticlabs/gohashtree` | No | MIT | Fast SHA256 for Merkle trees (2-4x) |
| **go-libp2p** | `github.com/libp2p/go-libp2p` | No | MIT | CL gossipsub, peer discovery |
| **go-eth2-client** | `github.com/attestantio/go-eth2-client` | No | Apache-2.0 | Beacon API client |
| **zrnt** | `github.com/protolambda/zrnt` | No | MIT | Executable consensus spec (reference) |
| **liboqs-go** | `github.com/open-quantum-safe/liboqs-go` | Yes | MIT | Broader PQC algorithm coverage |

### Current crypto backend status
- **BLS**: PureGoBLSBackend wired (Verify, FastAggregateVerify, G2 aggregation) — upgrade to `blst` for production performance
- **KZG**: PlaceholderKZGBackend with real polynomial evaluation (test SRS s=42) — upgrade to `go-eth-kzg` for production trusted setup
- **PQ crypto**: Real Dilithium3 lattice ops, ML-DSA-65 (FIPS 204), Falcon512, SPHINCS+ all wired
- **ZK circuits**: Groth16 proof size validation wired — upgrade to `gnark` for full circuit proving
- **Shielded**: Real BN254 Pedersen commitments (v*G + r*H), range proof structure, nullifier verification

## Agent-Specific Notes

- When answering questions, respond with high-confidence answers only: verify in code; do not guess.
- **Multi-agent safety:** do **not** create/apply/drop `git stash` entries unless explicitly requested.
- **Multi-agent safety:** do **not** switch branches unless explicitly requested.
- **Multi-agent safety:** scope commits to your own changes only.
- **Multi-agent safety:** when you see unrecognized files, keep going; focus on your changes.
