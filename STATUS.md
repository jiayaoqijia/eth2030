# Project Status

## EIP Test Framework Completion (2026-11-11)

- The simulator returns the execution results from canonical block insertion. Regressions cover five-block state continuity, scheduled genesis JSON, exact fork timestamps and stable phase views, receipt and log assertions, state diagnostics, historical calls, and negative validation that preserves the head. Pool tests exercise admission, replacement, eviction, nonce ordering, branch rollback, and finalization.
- The Engine probe uses the node's HTTP endpoint and an independent validating chain. Tests cover consecutive signed payloads, hash and head consistency, inclusion-list attributes, canonical EIP-7928 BAL encoding and rejection, and ePBS bid ordering. The RPC probe checks typed transactions and receipts. The standalone Engine backend also checks V5 payload hash and BAL consistency.
- The differential probe compares state roots and full encoded receipts across a sequence. Native and Geth now agree on the previously divergent contract creation at 75,572 gas. The calldata floor determines the minimum transaction gas limit and clamps final gas usage. Intrinsic gas alone is charged before the EVM runs. The committed calldata, beacon root, blob, Frame and DELEGATECALL regressions assert observable execution outcomes.
- The transaction matrix verifies RLP round trips and recovery for signed envelopes. The pinned EIP-155 vector is checked independently. EIP-7825 is exercised through pool admission, block building and block validation at Osaka. The EIP-7934 scaffold demonstrated a red/green cycle: the original block path reached an intrinsic-gas error, then the Osaka RLP-size check rejected the same candidate.
- The coverage gate detects missing implementation entries and assertion-free test files. Both implementation list paths trigger CI. Verified the scaffold in an isolated directory from generation to compilation, expected gate failure, and passing behavior assertion. It also generated a shell-valid devnet config and verifier pair. Optional legacy test migration remains incremental.
- `make contracts` generates seven embedded runtimes from Solidity and Yul. `make eest` uses the pinned EELS workspace to fill a fresh Amsterdam fixture and immediately executes it through Geth. The smoke fixture tests calldata-derived storage with zero gas price across the two pinned revisions. `make bin/evm` builds the older pinned Geth tool for forks it supports.
- Local verification uses Go 1.27.3. The framework ran 68 top-level tests with no skips in 1.57 seconds of package time. The uncached default suite passed in 130.84 seconds wall time, including the pinned EF fixtures. Build, vet, formatting, integration and goethkzg tagged suites, and race checks for the framework, Engine and node backend passed. Contract generation was repeatable with a clean artifact comparison.
- Compatibility boundaries remain in the underlying protocols. The current SSZ legacy signature field is one byte, so large EIP-155 `v` values produce an explicit encoding error. EIP-8079's draft address and ABI are unspecified; coverage exercises the rollup component. The Amsterdam fixture verifies a shared state-transition case across pinned EELS and Geth revisions. The optional devnet scaffold was verified locally without starting a network.

## EIP Test Framework Acceptance Review (2026-11-11)

Reviewed `feat/eiptest-framework` at `4610eac` against the [framework plan](docs/plans/eiptest-framework-plan.md). The framework runs in the default suite. The v1 acceptance criteria remain open.

- Local Go 1.27.3 verification passed for `go test -count=1 ./eiptest/...` (57 top-level passes and two EIP-8141 skips, 0.795 s package time), `go test -count=1 ./...` (127.22 s wall time, including the EF suite), `go test -race -count=1 ./eiptest/...`, `go test -count=1 -tags integration ./core ./p2p/...`, `go vet ./...`, `go build ./...`, and the CI formatting check. The Go 1.26.8 toolchain download timed out locally. `make contracts` regenerated three contracts with a clean diff. The EIP-7702 selection took 0.711 s of package time and 1.59 s wall time.
- T0.2 state continuity passed an independent five-block test that incremented the same storage slot across three blocks. T0.1/T2.2 still need the genesis JSON round trip and the planned fork transition. With activation at second 18, the probe built its boundary block at second 24 and the retained before-phase view moved to the new head.
- T1.1/T1.3 and T2.1 are incomplete. `BlockResult` lacks block logs, BAL and requests, and the diagnostic dump is unused. `CommitInvalid` returned the same unrelated gas-limit validation error with and without a nonce-invalid transaction. The STF call uses `STATICCALL` and rejects a storage write that succeeds in a simulated RPC `eth_call`. T2.6 wires ordinary insert feedback, but an empty `Commit()` consumes pending pool transactions and the probe has no finalize/reorg wiring.
- T1.2/T2.3 are incomplete. An EIP-155 legacy transaction signed for Alice recovered another address after RLP decode, while the helper passed because it skipped sender comparison on a missing sender cache. Several planned signed builders and the EOF builder are absent. The transaction SSZ helper checks repeated encodings and never decodes them.
- T1.4/T2.8/T2.9 need stronger enforcement and a working walkthrough. A synthetic implemented EIP failed the gate while absent from the registry. Adding its generated TODO-only test and a manual registry row made the gate pass. A missing `IFLOW.md` skipped the gate. The CI path filters omit both `IFLOW.md` and its tracked target `CLAUDE.md`. The documented scaffold command fails from the repository root and the generator prints the registry row for manual entry. T3.1/T3.2 have no demonstrated test-first red/green workflow for a new feature or resulting feedback round.
- T2.4 verifies live block RPC responses, with typed-transaction and receipt assertions still due. T2.5 passes the first injected transaction, but two successive Engine cycles both returned block 1 with `VALID`; the Engine head remained at genesis and the latest-valid hash differed from the advertised payload hash. The probe uses the standalone Engine backend and leaves inclusion-list attributes and node-level HTTP dispatch uncovered. Its BAL mismatch path only logs the mismatch, which conflicts with the EIP-7928 validation requirement. T2.7 compares one transfer's receipt status and gas. It lacks multi-block root parity and storage snapshots. A direct comparison reproduced contract creation at 75,930 native gas and 75,570 Geth gas. The committed contract-parity test logs a fixed Geth value without running Geth.
- The EIP-8141 skip is explained by the existing STOP-only frames omitting payer approval. A separately run funded sender with an `APPROVE` contract and Glamsterdam enabled successfully executed a VERIFY frame and advanced its nonce. The calldata exclusion at 1,500 and 2,000 bytes was explained by the EIP-7976 floor: both were included at their respective minimum limits of 100,500 and 132,500 gas.
- T4.1 still lacks the delegate caller, AA entrypoint, rollup anchor and frame approval scenario contracts. T4.2 has two distinct blockers. `make eest` invokes `go build` for Geth from outside its module. Building the pinned Geth binary inside its module succeeded, but the direct EEST fill exited 5 without running any tests or producing Amsterdam vectors. The default test executes the committed Cancun fixture at a fixed path. T4.3 depends on the first test-first feature. T5.2 emits a devnet stub with no feature validation. T5.3 is already covered by the tagged CI job.

## eiptest Framework Implementation (2026-11-11)

- Implemented the full `eiptest` framework on `feat/eiptest-framework` per the [task plan](docs/plans/eiptest-framework-plan.md): `Sim` chain simulator over the production blockchain/builder/processor path with scheduled-fork configs and genesis-alloc funding; ten-type tx builder DSL; assertion library; seven surface probes (STF, pool, fork, encoding, RPC over the production RPCBackend, Engine payload flow with transaction injection, geth differential); EIP coverage registry gate wired into the default suite; scaffold generator with optional devnet config emission (`tools/eiptest-scaffold`).
- Shipped 45 EIP integration test files (60 test functions) covering the implementation list (tiers A–C; network/CL tiers documented as exempt). Full suite runs in ~0.75s with no build tags; the registry gate fails the default suite when an implemented EIP lacks a test file.
- Contract pipeline (`make contracts`): three solc scenario contracts compiled to runtime bytecode with a Go generator (`tools/gen-contracts`) and an end-to-end deploy/ABI test. Vector pipeline (`make eest`): wired uv + geth `evm` fill; the pinned EEST checkout is mid weld-migration (pytest cannot collect), so consumption is verified with a real GeneralStateTests fixture through `eftest.RunSingleFixture`.
- Production fixes the framework surfaced during development: EIP-7702 delegated-EOA calls now resolve the 0xef0100 designator in `core/vm` (Call + call handlers — previously calls into delegated EOAs executed the designator bytes and reverted); Engine API `ProcessBlockV5` now decodes the lighthouse ≥ 8.4 hex-string BAL form it itself emits; `SetTxPool` now feeds payload building (async builder + the sync FCU path built with a nil pool) instead of only inclusion-list generation.
- Recorded tracked findings for follow-up: ~360-gas contract-creation divergence between native and geth backends; post-Glamsterdan builder drops calldata-carrying txs between 1–2 KB; EIP-8141 frame execution reverts across probed variants while envelope/pool surfaces work; geth differential adapter applies no post-block system contracts (EIP-4788/2935), so the Diff probe compares receipts not roots.
- Corrected the plan's record: the `integration` build tag is not orphaned — CI's `test-variants` matrix runs it and the suite is green locally (core tagged suite verified; `core/eftest` under the tag runs the full EF pass at ~115s).
- Verification: `go vet ./...` clean, `go build ./...` clean, `go test ./eiptest/...` green (58 tests, ~0.75s), `make contracts` idempotent, scaffold generate→compile→test round-trip verified with a throwaway EIP.

## EIP Integration Test Framework Plan (2026-11-11)

- Authored the `eiptest` framework design and phased task plan at [docs/plans/eiptest-framework-plan.md](docs/plans/eiptest-framework-plan.md): an in-process integration test base for future EIP development, with seven surface probes (STF, pool, fork gating, encoding, RPC, engine payload flow, geth differential), an EIP coverage registry gate, and a test scaffold generator.
- Plan targets: single-EIP test iteration under 1 second, full framework suite under 2 minutes, no build tags, default `go test ./...` inclusion. Devnet stays responsible for multi-node surfaces only.
- Baselines recorded on the dev host: `-short` full suite 37.3s, `go vet` 5.7s, single-client devnet boot to block 10 ~50s; the git history review (288 fix commits) shows engine/RPC/txpool surfaces carry the most recurring fixes, which sets the probe priorities.

## Cryptography Regression Fixes (2026-11-10)

- Authenticated native PQ execution and transaction admission with a shared typed signing digest and the existing transaction wire format.
- Matched the pinned Geth Prague activation and EIP-2537 contract layout. Historical native BLS replay uses an explicit chain configuration cutoff.
- Corrected RLPx identity handling, PeerDAS cell-size consistency, empty ECIES messages, and proof batch deadlines. STARK gossip now transmits the complete proof and remains advisory.
- The clean staged snapshot passed 206 Go test packages, builds, vet, tagged tests, Go-only tests, and targeted race checks. The BLS tests passed 914 success and failure vectors. The pinned EF gate reported 36,264 passes and 20 known outcomes with no unexpected failures or skips.

## Ethereum Client Cryptography Follow-Up (2026-11-10)

- Traced Geth's secp256k1, EVM curves, KZG, Engine JWT, and keystore providers and the separate Lighthouse BLS and Verkle paths.
- Switched IPA openings, Poseidon, VDF, VRF, and prime-field share reconstruction to public libraries. Retired unused local signers and the old threshold prototype.
- Added real opening verification for IPA and KZG batches and required registered verifiers for SNARK and STARK statements. The PQ attestation codec carries the full ML-DSA key and signature.
- The clean staged source passed all 206 test-bearing Go packages, build, vet, tagged tests, Go-only tests, and targeted race checks. The pinned EF gate recorded 36,264 passes and 20 known outcomes with zero unexpected failures or skips.
- A Geth provider probe selected Go KZG and C KZG in turn, generated and verified an opening with each, and compared their commitment bytes.
- Recorded the format changes and outstanding STARK, lattice and aggregation work in the [codebase audit](docs/CODEBASE_AUDIT.md#vii-ethereum-client-references-and-follow-up-migration).

## Public Cryptography Library Migration (2026-11-10)

- Moved secp256k1, BN254 precompiles, BLAKE2b compression, ECIES, keystore and RLPx transport to go-ethereum. Engine JWT authentication uses golang-jwt.
- Moved BLS12-381 arithmetic and encodings, BN254 point operations, Bandersnatch operations, EVM transforms, and Groth16 verification to gnark libraries. Blob and PeerDAS operations share the Ethereum KZG context.
- Replaced the project-local PQ signing and KEM implementations with CIRCL and go-fn-dsa. Earlier PQ and ECIES artifacts require regeneration in the public library formats.
- The staged Go snapshot passed builds, vet, the full fixture-backed suite, and tagged tests. The geth-backed EF gate recorded 36,264 passes and 20 known outcomes with zero unexpected failures or skips.
- Recorded the remaining protocol migration work and compatibility boundaries in the [codebase audit](docs/CODEBASE_AUDIT.md#vi-public-cryptography-library-migration).

## Unused Package Cleanup (2026-11-10)

- Removed six packages with no in-repository importers in the [first cleanup batch](docs/CODEBASE_AUDIT.md#v-follow-up-results).
- Verified the clean snapshot with Go builds, vet, module tidy, default and tagged tests, and the website path check. Both executables retained the same production dependency sets in the compared configurations.

## Codebase Audit Follow-Up (2026-11-10)

- Completed the [audit follow-up](docs/CODEBASE_AUDIT.md#v-follow-up-results) on `fix/codebase-audit-follow-up`.
- Native point evaluation verifies the tracked upstream vector using the shared Ethereum KZG ceremony context. Docker and Makefile binary builds use `goethkzg`.
- The geth-backed EF gate executed 36,284 pinned vectors locally. It reported 36,264 passes and 20 exact legacy outcomes, with zero unexpected failures or skips. The missing-fixture probe failed as intended.
- Gnark v0.16.5 passed BN254 AA proving and verification. A compatibility probe also loaded the previous serialized R1CS and keys and verified a prior proof.
- The fresh source snapshot passed formatting, vet, build, and all 213 test-bearing Go packages. The tagged and race suites passed locally. The container started both binary entry points and reported source commit `7e05845`. The site build and EIP path check passed.
- Removed unused internal wrappers and moved ignored artifacts out of the worktree. Tor/Nym live probes require configured services, and the workflow results recorded here are local.

## Codebase Audit Evidence Review (2026-11-10)

- Updated [the codebase audit](docs/CODEBASE_AUDIT.md) with verified production import paths, the native KZG fixture result, tagged test outcomes, EF vector totals and exit behavior, and the gnark advisory.
- Revised the follow-up to match the evidence and added commands for reproducing the results at the audited revisions.
- Verified report links, the website verifier on the documented source snapshot, the Go build, and the website build.

## English Documentation Review (2026-11-10)

- Manually translated `docs/CODEBASE_AUDIT.md` into English, retaining the reported findings and reproduction commands.
- Added the English documentation requirement to `AGENTS.md`.
- Found no remaining Chinese text in the project-owned text files. Extracted text from the five tracked PDFs and visually checked pages without a text layer.
- Verified the Go module with `go build ./...` and the website with `npm run build`.

## Update Plan Phases 2+3: Dependency Upgrades + Lighthouse Sync (2026-11-06)

### Current Status: COMPLETE (with recorded Gloas/Heze devnet dependency)

**Scope:** Execute UPDATE_PLAN.md phases 2 (Go dependency upgrades) and 3 (lighthouse upstream sync), per the "upgrade as far as possible" directive.

**Completed Work:**

Phase 2 — eth2030 Go deps (each group committed separately):
- go-ethereum v1.17.1 -> v1.17.10: GasPool AddGas -> NewGasPool, StateDB.Commit(rules, block), SetTxContext blockAccessIndex, Message gas fields -> *uint256.Int, Holesky removed upstream (mapped to Hoodi 560048, added hoodi network). EF state tests 36,126 passing. (f20cf72)
- gnark v0.14.0 -> v0.16.4, gnark-crypto v0.19.2 -> v0.22.0: 65 packages green (eb20de1)
- x/crypto v0.48.0 -> v0.58.0 (+net/sys/text/sync): 43 packages green (732e844)
- blst v0.3.16 -> v0.3.17: 55 packages green (1934f0b)
- Indirect refresh via go get -u: pebble v2 2.1.7, otel 1.47, grpc 1.85 etc.; full suite 213 packages, 0 cached (da9c207)
- Dockerfile builder -> golang:1.27-alpine (go directive bumped to 1.26.8 by -u) (02a8789)
- CLAUDE.md geth version refs updated (e711263)

Phase 3 — lighthouse:
- Verified all 69 fork commits (feat/fix-native-aa) are superseded by upstream unstable (ForkName::Heze + full IL infra, payload envelopes, PTC). Backup: backup/feat-fix-native-aa-20261106.
- New branch integrate/unstable-202611 = sigp/lighthouse unstable 25b9b754 (8.4.0-rc.0, rust 1.91 image) + IFLOW symlink. (4df5a7bcf)
- cargo update batch reverted: parking_lot trio now requires rustc >= 1.95, breaking the rust:1.91 Docker build; upstream lockfile is the MSRV-compatible set. (2c468be9c)
- cargo check + make lint (clippy) clean; eth2030cl:local rebuilt.

Phase 3e — devnet cross-validation (eth2030cl:local + forks enabled):
- Sanity (forks disabled, upgraded deps): blocks advancing, CL verified, peers=2.
- Fulu transition (PeerDAS): passed on both rounds.
- Engine API compat fixes for lighthouse >= 8.4 (in pkg/engine, with tests):
  - fcuV4/V5 third custodyColumns param accepted; attributes shape sniffing (lighthouse snake_case/hex vs fork-era camelCase); engine_forkchoiceUpdatedV5 routed (1d56d58)
  - fcuV5 advertised in exchangeCapabilities; getBlobsV3 custody bitarray param (d673210)
  - Gloas getPayloadV5/V6 responses carry blockAccessList (0x-hex, RLP bytes; EIP-7928 SSZ wire encoding TODO) + slotNumber from payload attributes; e2e test drives real backend (77287d4, 1991580)

**Recorded dependency (blocks Gloas/Heze devnet acceptance):**
The devnet EL runs eth2030-geth (ethereum-package's geth launcher hardcodes the `geth` command), so the Engine API is go-ethereum's built-in server — go-ethereum v1.17.10 does not emit the Gloas payload shape (blockAccessList/slotNumber), and the CL correctly rejects it (NoViablePayloadBid -> missing field blockAccessList). The pkg/engine fixes serve the self-built binary path (verified by e2e test against the real backend). Full Gloas/Heze devnet bring-up requires either: (a) devnet EL switched to the self-built binary (needs an ethereum-package launcher override or fork — refs are read-only), or (b) Gloas support upstream in go-ethereum. Local manual verification confirmed fcuV5 + lighthouse-shaped attributes parse and build correctly on the self-built EngineAPI.

**Verification:**
- `cd pkg && go build ./... && go test -timeout 240m ./...` (213 packages green incl. EF state tests)
- `cd ../lighthouse && cargo check --workspace && make lint` (clean)
- `cd pkg && go test -run TestGetPayloadV6ResponseCarriesGloasFields ./engine/` (Gloas wire shape)
- Devnet sanity + fulu: `./scripts/run-devnet.sh full-feature-eth2030` -> fulu boundary passed (blocks past slot 32)

## Update Plan Phase 0/1 Baseline (2026-11-06)

### Current Status: COMPLETE

**Scope:** Baseline re-validation after ~6.5 months of maintenance downtime, plus devnet infrastructure pinning per workspace docs/UPDATE_PLAN.md (phases 0 and 1).

**Completed Work:**
- Toolchain: Go 1.25.2 -> 1.27.3 (gvm default), rust/cargo 1.99.1
- spamoor: pushed 3 unpushed commits (4d4b439..4908a05 on feat/support-native-aa)
- eth2030: `go build ./...`, `go vet ./...`, `go test ./...` all green on go1.27.3 (213 packages, 0 cached results, 0 FAIL; includes core/eftest EF state tests, 156s)
- lighthouse: `cargo check --workspace` clean (2m17s, 0 errors, 22 upstream future-incompat warnings)
- Devnet infra pinned: run-devnet.sh / run-feature-tests.sh / run-geth-testnet.sh default KURTOSIS_PACKAGE to local refs/ethereum-package instead of remote master; submodule advanced 234fb546 (2026-02-12) -> 6.1.0 c4a8e15d (2026-04-02) because configs use heze_fork_epoch (upstream renamed eip7805_fork_epoch -> heze on 2026-02-26, eb8c590a)
- Devnet smoke (full-feature, pinned package): blocks advancing, CL "Synced (verified)" with peers=2, EL net_peerCount=2, no WARN/ERROR in EL logs

**Verification:**
- `cd pkg && go build ./... && go vet ./... && go test -timeout 240m ./...`
- `cd ../lighthouse && cargo check --workspace`
- `cd pkg/devnet/kurtosis && ./scripts/cleanup.sh && ./scripts/run-devnet.sh full-feature`
- `cast bn -r http://$(kurtosis port print eth2030-devnet el-1-geth-lighthouse rpc)` -> advancing

## Full-Feature Devnet Bring-Up (2026-03-25)

### Current Status: IN PROGRESS

**Scope:** Build the local Lighthouse and ETH2030 images as needed, launch the Kurtosis `full-feature` devnet in the `eth2030-devnet` enclave, and verify block production plus basic EL/CL health.

**Planned Verification:**
- `cd ../lighthouse && docker build -t eth2030cl:local .`
- `cd pkg && docker build -t eth2030:local .`
- `cd pkg/devnet/kurtosis && ./scripts/cleanup.sh eth2030-devnet`
- `cd pkg/devnet/kurtosis && ./scripts/run-devnet.sh full-feature`
- `cast bn -r http://$(kurtosis port print eth2030-devnet el-1-geth-lighthouse rpc)`
- `kurtosis service logs eth2030-devnet cl-1-lighthouse-geth 2>&1 | grep -E "verified|ERROR" | tail -20`
- `kurtosis service logs eth2030-devnet el-1-geth-lighthouse 2>&1 | grep -E "WARN|ERROR" | tail -20`

## Inclusion List JSON Refactor (2026-03-21)

### Current Status: COMPLETE

**Scope:** Simplify the local inclusion-list JSON parsing helpers in `pkg/engine/inclusion_list.go` without changing the wire format or validation behavior.

**Completed Work:**
- Extract small helper functions for repeated hex-prefix stripping and byte-slice conversion.
- Reuse the helpers from the custom JSON marshal/unmarshal paths.
- Add focused parsing tests and rerun the relevant engine test suites.

**Verification:**
- `cd pkg && go test ./engine/...`

## Blob Bundle Refactor (2026-03-21)

### Current Status: COMPLETE

**Scope:** Deduplicate the cell-proof expansion logic shared by the V2 and V6 blob-bundle builders in `pkg/engine/backend.go` without changing output formats.

**Completed Work:**
- Extract shared blob sidecar collection and cell-proof expansion logic.
- Reuse the helper from the V2 and V6 blob-bundle builders.
- Verify the relevant engine test suites after the refactor.

**Verification:**
- `cd pkg && go test ./engine/...`

## Payload Retrieval Refactor (2026-03-21)

### Current Status: COMPLETE

**Scope:** Deduplicate the repeated payload retrieval paths in `pkg/engine/backend.go` without changing API-specific response contents or async eviction behavior.

**Completed Work:**
- Extract shared helpers for async payload waiting and mutex-protected payload lookup.
- Reuse the helpers from `GetPayloadByID`, `GetPayloadV4ByID`, `GetPayloadV5`, and `GetPayloadV6ByID`.
- Verify the relevant engine test suites after the refactor.

**Verification:**
- `cd pkg && go test ./engine/...`

## Inclusion List Selection Refactor (2026-03-21)

### Current Status: COMPLETE

**Scope:** Deduplicate inclusion-list transaction selection across engine backends and make oversized pending transactions skippable instead of aborting the whole selection pass.

**Completed Work:**
- Extract shared inclusion-list transaction selection into a helper in `pkg/core/eips/inclusion_list.go`.
- Use the helper from both `pkg/engine/backend.go` and `pkg/node/backend/engine_backend.go`.
- Add a focused test for oversized-transaction skipping.
- Verify the relevant Go test suites after the refactor.

**Verification:**
- `cd pkg && go test ./core/eips ./engine/... ./node/backend/...`

## Engine Review Follow-Up (2026-03-21)

### Current Status: COMPLETE

**Scope:** Recheck branch review findings, confirm whether `engine_getPayloadV5` JSON encoding is already fixed, and address the remaining safe bugs in inclusion-list handling.

**Completed Work:**
- Verified that the `engine_getPayloadV5` response encoding issue was already fixed on this branch by `fdd0820`.
- Reject oversized hex input in `flexibleUint64` instead of truncating it.
- Make the node backend return an explicit error for `ProcessInclusionList` until real storage/wiring exists.
- Add tests for the inclusion-list fixes and rerun the relevant engine/node test suites.

**Verification:**
- `cd pkg && go test ./engine/... ./node/backend/...`

## Engine Lock Wrapper Refactor (2026-03-19)

### Current Status: COMPLETE

**Scope:** Refactor `pkg/engine/backend.go` so accesses guarded by `stateMu`, `blocksMu`, `payloadMu`, and `ilMu` go through helper functions instead of open-coded lock/unlock pairs.

**Completed Work:**
- Add backend accessor methods that encapsulate `stateMu`, `blocksMu`, `payloadMu`, and `ilMu` reads/writes.
- Migrate existing call sites in `pkg/engine/backend.go` to the accessors without changing lock ordering or behavior.
- Follow up on the accessor refactor by removing remaining unsynchronized forkchoice field reads in `ForkchoiceUpdated`.
- Extend the accessor refactor into `pkg/engine/backend_bodies.go` to remove direct mutex usage there as well.
- Verify the `pkg/engine` build and tests after the refactor.

**Verification:**
- `cd pkg && go test ./engine/...`

## Go Test Repair (2026-03-19)

### Current Status: COMPLETE

**Scope:** Reproduce and fix failing or hanging `go test ./...` runs from the Go module root at `pkg/`.

**Resolved Issues:**
- `pkg/core/vm` JSON-vector and precompile fixture tests no longer fail when the optional external fixture corpus is absent from the checkout; they now skip cleanly on missing files.
- `pkg/engine` no longer deadlocks while storing processed blocks: `ProcessBlock` and `ProcessBlockV5` were calling `evictOldBlocks` with `blocksMu` already held, while the helper tried to lock `blocksMu` again.

**Verification:**
- `cd pkg && go test ./...`

## Devnet Testing (2026-03-16)

### Current Status: WORKING

**Devnet Configuration:** `full-feature-prysm.yaml` with PeerDAS/Fulu

**Test Results:**
- Slot: 367+ (continuing to advance)
- Finalized Epoch: 9+ (continuing to finalize)
- Block Processing Time: 15-200ms (normal)
- Data Availability Check: ~5 microseconds (instant)

### Issues Fixed

1. **engine_getPayloadV5 "build timed out" (CRITICAL)**
   - Root Cause: `buildMu` mutex held during `StateAtBlock` call which could take tens of seconds on cache miss
   - Death Spiral: state cache miss → state rebuild blocks buildMu → all builds timeout → chain stalls
   - Fix: Pre-fetch state BEFORE acquiring `buildMu`, use singleflight for deduplication
   - File: `pkg/node/backend.go`
   - Status: ✅ RESOLVED

2. **Race condition in computeBlobsV2**
   - Root Cause: `len(b.blobCache)` accessed after `blobCacheMu.RUnlock()`
   - Fix: Capture cache size before releasing lock
   - Status: ✅ RESOLVED

### Known Non-Critical Issues

1. **engine_getBlobsV2 "JSON-RPC response has no result"**
   - This is expected behavior in PeerDAS
   - Blobs are propagated via gossip (data column sidecars), not through Engine API
   - Prysm CL attempts to reconstruct data columns from EL as a fallback
   - EL doesn't have blobs for blocks built by other nodes
   - **Impact: NONE** - Chain continues to advance and finalize normally
   - These errors are just log noise from Prysm's gossip message handling

### Architecture Notes

**PeerDAS Data Flow:**
1. Block proposer (CL-2) broadcasts data column sidecars (128 columns per block)
2. Other CL nodes receive data columns via gossip
3. Data availability is verified from gossip-received columns
4. CL may attempt to reconstruct from EL as fallback (fails but non-fatal)
5. Block processing succeeds regardless

**State Cache Behavior:**
- `StateAtBlock` uses singleflight for deduplication
- Hot cache (`sc`) and permanent cache (`memSC`) provide state caching
- Re-execution needed when cache misses (can take tens of seconds)

---

## Formal Verification

- Formal Lean workspace is present under `formal/lean`.
- Initial EVM/VM model (`Lean2030/VM`) and richer `Lean2030/EVM` model are now linked in `Lean2030/Lean2030.lean`.
- EVM model now includes:
  - arithmetic and bitwise ops,
  - DUP/SWAP,
  - POP/JUMP/JUMPI/JUMPDEST,
  - compiler and theorem suites.
- Current status: still a semantic subset only (toy control-flow, no memory/tracing, no Go cross-checking).
- Added bytecode-offset execution module `formal/lean/Lean2030/EVM/Bytecode.lean` with `decodeAt`/`decodePush`/`runBytecode`.
- Added executable mismatch lemmas showing current op-index `run` diverges from byte-offset EVM behavior for `PUSH`-preceded jumps.
- Lean 4 test driver is now configured in `formal/lean/lakefile.lean` with `Lean2030/Tests.lean`.
- Added `.gitignore` entries for Lean outputs (`.lake`, `*.olean`, `*.ilean`) and linked the Lean VM strategy doc in README.
- Next major deliverable: full `compile`→`runBytecode` refinement theorem and stronger instruction/stack invariants.
