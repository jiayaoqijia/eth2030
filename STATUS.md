# Project Status

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
