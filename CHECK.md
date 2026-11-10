# Lean4 Verification Check-list

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
