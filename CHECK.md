# Lean4 Verification Check-list

## Track 3 Completion Follow-Up (2026-11-12)

- [x] Trace bounded-memory snapshot download, execution and serving against the snap protocol and database implementations; the active FileDB also retains all values in memory.
- [x] Preserve storage pagination on transient failure and avoid quadratic scans of completed storage tasks.
- [x] Verify finalized ancestry before reusing a completed snapshot after a CL finality advance.
- [x] Continue a fixed-pivot download across peer failures, sharing only account roots authenticated against that pivot.
- [x] Audit the candidate stack-trie builder: propagate node-writer failures and correct its bounded-memory claim.
- [x] Stream snapshot validation and trie-node generation; write trie nodes and snapshot leaves in bounded batches with failure-before-publication tests.
- [ ] Implement authenticated moving-pivot continuation and safe restart behavior with regression coverage.
- [ ] Remove remaining sync scalability bottlenecks without weakening ancestry validation.
- [ ] Validate real-client interoperability and record the actual scope and resource limits.
- [ ] Run affected tests, race checks and builds; update status and commit task-owned changes.

## Track 3 Adversarial Review (2026-11-12)

- [x] Re-read specification and design contracts; audit every Track 3 production change and its callers.
- [x] Reproduce and fix forkchoice authority, ancestry, durable publication, restart and reorg defects.
- [x] Reproduce and fix native/snap state execution, rollback, storage, and proof-validation defects.
- [x] Audit protocol encodings, peer cancellation, pagination, lifecycle shutdown and native consensus invariants.
- [x] Add behavioral regressions and targeted mutation checks; run affected race, full, integration, tag, vet and build checks.
- [x] Correct completion claims and document remaining scope accurately; commit and push scoped fixes.

## Track 3 Implementation (2026-11-12)

Base: `origin/master` at `9fde8aa` (PR #50 merged), branch `feat/track3-node`.

- [x] Trace current snap serving, download, proof verification, persistence, pivot adoption, and restart paths against the pinned specifications.
- [x] Replace node snap-sync no-op writers with verified persistent state and trie healing; test sync from an empty database and execution after pivot adoption.
- [x] Connect unknown Engine forkchoice heads to a cancellable CL-driven beacon sync loop with peer download, validated ancestry, retries, and shutdown coverage.
- [x] Connect 3SF, quick-slot scheduling, and PQ attestation components to an explicitly configured node lifecycle and verify their event consumers and shutdown.
- [x] Run meaningful negative-path tests, affected race tests, integration tests, full Go tests, vet, formatting, and local builds.
- [x] Update `STATUS.md` and the Track 3 completion record with implementation and verification evidence; commit scoped changes.

Functional scope and evidence: [Track 3 node integration](docs/track3-node.md).
Track 3 is **not fully complete**. Production snap synchronization still requires
moving-pivot range reuse and bounded-memory assembly. Snap/2 and standalone CL
validator/proposer tooling are additional work; the native observer has no
execution forkchoice authority.

## Next Phase Planning (2026-11-11)

Baseline: `master` at `137006e` (PR #48, `feat/eiptest-framework` merged). Re-verify each
gap against current code before starting — parts of the 2026-03 gap analysis are stale
(BAL header hash and `engine_getInclusionListV1` already exist).

### Track 0 — Housekeeping

- [x] Fast-forward local `master` to `origin/master`.
- [x] Delete merged local branch `feat/eiptest-framework` (remote already deleted).

### Track 1 — Glamsterdam / ePBS closure (P0)

- [x] EIP-7732 ePBS (85%): PTC election/voting wiring into fork-choice (types exist in `pkg/epbs/types.go`); BuilderPendingPayment/Withdrawal payment mechanism; builder registry (balance/deposit/withdrawable epoch); CL integration (payload → SignedBuilderBid in BeaconBlockBody); wire `epbs/bid SubmitBid` to a real builder entry point.
- [x] EIP-7975 (ETH/70 partial block receipts) and EIP-8159 (ETH/71 BAL exchange): move from referenced-only (`pkg/eth/protocol.go`, `pkg/p2p/ethproto`) to implemented and tested.
- [x] EIP-7928 BAL residual gaps: Engine BAL mismatch must reject (probe currently only logs); verify remaining 10% gap items against the 2026-03 analysis. (Rejection already enforced; nil BAL hash now rejected post-Amsterdam.)
- [x] EIP-7594 PeerDAS (80%): column subnet gossip, custody, <50% column reconstruction, txpool `cell_proofs`. (Canonical `data_column_sidecar_<subnet>` topics + SignedBlockHeader added; custody/reconstruction/cell_proofs were already in place.)

### Track 2 — Hegotá closure (P1)

- [x] EIP-7805 FOCIL (85%): CL P2P inclusion-list gossip validation rules; equivocation (duplicate IL per committee member) evidence handling.
- [x] EIP-8141 FrameTx residual: re-verify APPROVE scope semantics (0x0/0x1/0x2) against the current draft; payer gas approval path; frame receipt format. (Payer charging was missing entirely — now collected on APPROVE(1)/(2); receipts carry the payer.)
- [x] EIP-7495 SSZ StableContainer: add tests (currently referenced-only). (HTR aligned with the EIP-7495 ProgressiveContainer spec form; conformance tests added.)
- [x] EIP-8025 Execution Proofs: close out to complete. (Proofs now anchored to the real EIP-7928 BAL with header-commitment verification.)
- [x] SSZ fork activation (EIP-6404/7807/7916): consensus activation, Engine API versioning, RPC migration (explicitly deferred fork work). (SSZBlockTime fork switch; engine_getPayloadV7/newPayloadV7 with SSZ hash binding; transitional header-form block hash.)

### Track 3 — Node fundamentals (cross-cutting)

- [ ] Real snap sync: fixed-pivot proof-verified bootstrap, persistent hashed state, authenticated healing and recovery are implemented and tested. Moving-pivot continuation and bounded-memory assembly remain open; do not equate the small-chain end-to-end test with production snap-sync completion.
- [x] CL-driven beacon sync loop (`pkg/sync/beacon`): authenticated Engine request → hash-bound `eth` download → staged execution → atomic forkchoice publication; retries, target replacement, cancellation and `eth_syncing` are wired.
- [x] Wire own consensus components (3SF, quick slots, PQ attestations) into node lifecycle as an explicit research observer: configuration-bound signatures, source-target quorums, RLPx vote delivery, CL reorg re-anchoring and joined shutdown. The external CL retains execution forkchoice authority; this is not a standalone consensus client.

### Track 4 — I* / PQ direction

- [ ] PQC suite: real implementations for Dilithium3, Falcon-512, SPHINCS+ (ML-DSA-65 is real).
- [ ] EIP-7932 PQ registry and EIP-8051 PQ gas costs: add tests.
- [ ] EIP-7864 Binary Merkle Trie: continue toward complete.
- [ ] Remaining STARK / lattice / aggregation protocol migrations (CODEBASE_AUDIT §VII).

### Track 5 — eiptest framework (incremental)

- [ ] T5.1: opportunistic migration of `pkg/`-root E2E scenarios as files are touched.
- [ ] T4.3: post-first-feature ergonomics notes in the framework README.

### Track 6 — Devnet and site

- [x] Gloas devnet dependency (recorded at `a75a67a`); full-feature devnet run validating ePBS + FOCIL + BAL together. (full-feature-eth2030: fulu@32, gloas@64+Amsterdam BAL enforcement, heze@96 all clean; newPayloadV5 with BAL accepted from lighthouse 8.4.0-rc.0; follow-up: expose blockAccessListHash in the eth_getBlockByNumber serializer.)
- [ ] Site publishing: deployment workflow names `eth2030.com` while GitHub Pages binds `eth2030.altllm.ai`.

Recommended start order: Track 1 (ePBS PTC + fork-choice is the largest single P0), Track 3 snap-sync in parallel, Track 6 devnet as the integration gate.

## Native Execution Requests and SSZ Integration

- [x] Trace native request collection, block commitment, replay, and Engine API handling against the EIPs.
- [x] Collect canonical deposit, withdrawal, and consolidation requests through the native execution paths.
- [x] Verify nonempty requests through block building, validation, Engine API, and the SSZ projection.
- [x] Run affected and default suites and record the results.

## Execution SSZ and Framework Test Effectiveness Review

- [x] Verify the branch and run the default, tagged, and race suites.
- [x] Measure whether targeted behavior changes fail their intended tests.
- [x] Close demonstrated assertion gaps and repeat the targeted checks.
- [x] Record the results in `STATUS.md`.

## Execution SSZ Specification Review

- [x] Compare transaction and block encodings with the pinned and published EIP drafts.
- [x] Normalize EIP-6404 transaction bytes, signatures, and Merkle roots.
- [x] Implement the published EIP-7807 payload and header summary codecs.
- [x] Verify independent vectors and affected suites.

## EIP Test Framework Completion

- [x] Repair simulator results, fork timing, transaction signing, and failure assertions.
- [x] Complete STF, pool, encoding, RPC, Engine, and differential probes with regression tests.
- [x] Enforce the coverage gate and verify generated scaffolds and the README walkthrough.
- [x] Develop and verify a new EIP test-first through the scaffold.
- [x] Complete scenario contracts and run Amsterdam fixture generation and consumption.
- [x] Run the default and variant suites, verify the build, and reconcile the plan and status.

## EIP Test Framework Acceptance Review (2026-11-11)

- [x] Compare the implementation and examples with the plan's acceptance criteria.
- [x] Run the framework suite, default Go suite, and local CI checks.
- [x] Verify failure detection and exercise the documented tooling entry points.
- [x] Record verified results and remaining gaps in `STATUS.md`.
## PR #47 Merge Resolution

- [x] Confirm the pull request head and fetch the current upstream base.
- [x] Create an isolated worktree for the merge.
- [x] Reconcile the status entries and preserve the roadmap and tracker navigation.
- [x] Build the site and Go module, and verify the static export and selected Go packages.
- [x] Scope all merge and staging operations to the isolated worktree.

## Cryptography Regression Fixes

- [x] Wire PQ signature verification through transaction admission and native execution.
- [x] Activate canonical BLS precompiles at Prague and preserve historical native execution rules.
- [x] Correct RLPx identity, PeerDAS cell validation, and ECIES empty messages.
- [x] Repair proof batch deadlines and STARK gossip round-tripping.
- [x] Verify the regression probes, upstream vectors, and the full Go suite.

## Public Cryptography Library Migration (2026-11-10)

- [x] Replace secp256k1 signing and recovery, BN254 precompile arithmetic, and BLAKE2b compression.
- [x] Replace BLS12-381 precompile arithmetic, mappings and compression, and consolidate KZG providers.
- [x] Replace ECIES, keystore, RLPx, and Engine JWT implementations and document protocol migration boundaries.
- [x] Integrate public PQ signature and KEM providers with explicit encoding compatibility.
- [x] Move Groth16 verification, AA circuit startup, Banderwagon group arithmetic, and EVM transforms to gnark.
- [x] Remove superseded implementations and standalone placeholder backends after checking callers.
- [x] Verify a clean snapshot with Go builds, vet, fixture-backed tests, and the tagged suites.
- [ ] Complete experimental proof protocols in the follow-up below.

## Ethereum Client Cryptography Follow-Up (2026-11-10)

- [x] Trace the libraries used by the pinned Geth and consensus references to their actual callers.
- [x] Migrate experimental IPA openings, Poseidon, VDF and VRF to public providers.
- [x] Migrate encrypted-mempool share reconstruction and retire unused standalone schemes.
- [x] Verify actual IPA and KZG openings in batch and require configured SNARK and STARK providers.
- [x] Carry complete ML-DSA keys and signatures through the gossip codec.
- [x] Verify the full source snapshot with pinned EF fixtures, build, vet, tagged, Go-only, and targeted race checks.
- [ ] Migrate the remaining application-specific STARK, lattice commitment and aggregation protocols.

## Unused Package Cleanup (2026-11-10)

- [x] Remove the six packages selected in the first cleanup batch and update package indexes.
- [x] Verify builds, tests, and the executable dependency graphs from a clean snapshot.

## Codebase Audit Follow-Up (2026-11-10)

- [x] Verify native EIP-4844 point evaluation with the Ethereum ceremony setup and align binary build tags.
- [x] Provision pinned EF fixtures in CI and enforce the geth-backed vector results.
- [x] Upgrade gnark and exercise BN254 circuit compilation, setup, proving, and verification.
- [x] Resolve permanent skips, placeholders, weak checks, tagged suites, and race coverage.
- [x] Verify dead-code candidates and clean ignored local audit artifacts.
- [x] Run the supported build and test matrix and record each outcome.

## Codebase Audit Evidence Review (2026-11-10)

- [x] Reconcile reachability findings with production, test, and tagged builds.
- [x] Record the fixture, test coverage, and dependency scan results with their limits.
- [x] Revise the proposed follow-up and verify the documentation and local builds.

## English Documentation Review (2026-11-10)

- [x] Scan project-owned documentation for Chinese text.
- [x] Manually translate `docs/CODEBASE_AUDIT.md` and any other Chinese documentation.
- [x] Record the English documentation requirement in `AGENTS.md`.
- [x] Verify the translation, repeat the language scan, and run local builds.

## Inclusion List JSON Refactor (2026-03-21)
- [x] Extract small inclusion-list JSON parsing helpers in `pkg/engine/inclusion_list.go`.
- [x] Reuse the helpers from the custom marshal/unmarshal code.
- [x] Add or update focused parsing tests.
- [x] Run the relevant Go tests successfully.

## Blob Bundle Refactor (2026-03-21)
- [x] Extract shared blob sidecar / cell-proof expansion helper in `pkg/engine/backend.go`.
- [x] Reuse the helper from the V2 and V6 blob-bundle builders.
- [x] Run the relevant Go tests successfully.

## Payload Retrieval Refactor (2026-03-21)
- [x] Extract shared helpers for async and stored payload lookup in `pkg/engine/backend.go`.
- [x] Reuse the helpers from the V3/V4/V5/V6 payload retrieval methods.
- [x] Run the relevant Go tests successfully.

## Inclusion List Selection Refactor (2026-03-21)
- [x] Extract shared inclusion-list transaction selection helper.
- [x] Switch engine and node backends to the shared helper.
- [x] Add or update tests for oversized transaction skipping.
- [x] Run the relevant Go tests successfully.

## Engine Review Follow-Up (2026-03-21)
- [x] Verify whether `engine_getPayloadV5` JSON encoding has already been fixed on this branch.
- [x] Reject overflowed hex values in `pkg/engine/inclusion_list.go` `flexibleUint64`.
- [x] Make node backend `ProcessInclusionList` fail explicitly when inclusion-list storage is unsupported.
- [x] Add or update tests for the inclusion-list fixes.
- [x] Run the relevant Go tests successfully.

## Engine Lock Wrapper Refactor (2026-03-19)
- [x] Add mutex-backed accessor helpers for `stateMu`, `blocksMu`, `payloadMu`, and `ilMu` in `pkg/engine/backend.go`.
- [x] Migrate direct lock/unlock call sites in `pkg/engine/backend.go` to the accessors.
- [x] Remove remaining direct forkchoice field reads from `ForkchoiceUpdated`.
- [x] Refactor `pkg/engine/backend_bodies.go` to use backend accessors instead of direct mutex operations.
- [x] Run the relevant `pkg/engine` Go tests successfully.

## Go Test Repair (2026-03-19)
- [x] Reproduce the failing or hanging `go test ./...` behavior from `pkg/`.
- [x] Isolate the package or test causing the issue.
- [x] Implement the minimal fix and add or update unit tests.
- [x] Run the affected tests and the full module test suite successfully.

## Ongoing
- [ ] Build the local Lighthouse image `eth2030cl:local`.
- [ ] Ensure the local EL image `eth2030:local` is available.
- [ ] Clean up any existing `eth2030-devnet` enclave.
- [ ] Launch `pkg/devnet/kurtosis` with the `full-feature` config.
- [ ] Verify block production and inspect CL/EL error logs.
- [x] Extend EVM formal core with jump-family opcodes (`JUMP`, `JUMPI`, `JUMPDEST`, `POP`).
- [x] Expand compiler decoding for jump-family opcodes.
- [x] Add execution and run-level correctness lemmas for the expanded opcodes.
- [x] Add bytecode-offset interpreter (`runBytecode`) and decoding helpers (`decodeAt`, `decodeNoImmediate`, `decodePush`).
- [x] Document offset-vs-index jump mismatch using an executable counterexample theorem pair (`run` vs `runBytecode`).
- [x] Configure Lean workspace test driver (`lake test`) and add a unified Lean test module.
- [x] Add `.gitignore` entries for Lean artifacts (`.lake`, `*.olean`, `*.ilean`).
- [ ] Add a full refinement theorem from `compile` to bytecode-offset execution (`compile`↔`runBytecode`).
- [ ] Add machine-state fields for `pc`/memory/calldata/accounting as separate invariants.
- [ ] Prove compilation soundness for a larger instruction subset with controlled jump targets.

## Backlog
- `goals`:
  - define a bytecode operational semantics that respects byte-offset jumps,
  - prove compile-then-run agreement against bytecode-offset semantics,
  - then lift jump semantics from op-index simulation to byte-offset simulation,
  - prove end-to-end `compile`→`run` correctness against bytecode semantics,
  - begin RISC-V interpreter/simulation proof chain once VM core stabilizes.

## Website Strawmap refresh (2026-11-05)

- [x] Inspect the latest EF Strawmap and selected EIPs; distinguish draft milestones from network activation.
- [x] Inspect selected implementation and test evidence; record gaps and limits.
- [x] Replace outdated roadmap and unsupported promotional claims with sourced, reviewable content.
- [x] Verify site build, evidence paths and responsive presentation; update STATUS.md.
- [x] Commit only files changed for this task.
- [ ] Complete a full current-spec conformance and cross-client audit before claiming broader coverage.
