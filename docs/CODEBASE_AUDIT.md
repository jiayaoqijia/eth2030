# eth2030 Codebase Audit (Dead Code, Ineffective Tests, and Dependency Security)

- **Date** — 2026-11-10
- **Scope** — The `eth2030` repository, excluding other repositories and the read-only submodules under `refs/`.
- **Code baseline** — The `master` branch. Under `pkg/`, there are 2,286 `.go` files (excluding `refs/`), including 1,092 `*_test.go` files (about 440,000 lines). `go list ./...` reports 228 packages. The repository also includes the Next.js project website under `site/`.
- **Tools** — `govulncheck` for security scanning, `deadcode` with and without `-test`, repository-wide import-path searches for cross-checking, and `npm audit`.
- **Prior audit** — Several findings in [the 2026-03-09 audit](UNWIRED_CODE.md) are outdated. This audit used that report as a starting point, cross-checked every finding with fresh searches, and flags discrepancies where they arise.

---

## I. Dead Code and Unused Legacy Code

### Overall Reachability

| Analysis | Unreachable functions |
|---|---|
| `deadcode ./...` (production entry points only) | **12,049** (core 2,987 / consensus 1,742 / crypto 951 / p2p 827 / das 821 / engine 513 / trie 492 / txpool 409 / …) |
| `deadcode -test ./...` (including test entry points) | **1,773** |

Most functions flagged by the first run are helpers referenced only by tests. About 1,773 functions remain unreachable when the analysis includes tests. The confirmed dead code is concentrated in **orphaned packages**.

### A. Confirmed Dead Code (40+ Files Eligible for Removal)

#### A1. Orphaned Packages

The import-path search covered the entire repository, including `_test.go` files. The packages below had zero references, with one test-only exception noted in the table.

| Package | Finding and evidence |
|---|---|
| `pkg/txpool/validation` | An unused refactored version of `validateTx()` from `txpool.go`. |
| `pkg/txpool/queue` | An alternate implementation of `txSortedList` from `txpool.go`. |
| `pkg/txpool/replacement` | An alternate implementation of `hasSufficientBump()` from `txpool.go`. |
| `pkg/txpool/fees` | Four implementation files and four test files with no importers. The active fee logic uses `txpool/pricing`. |
| `pkg/das/gf` | An unused GF(256)/Reed–Solomon library. `das/blockerasure` and `das/reconstruction` each implement their own erasure coding. |
| `pkg/das/proof` | `block_blob_proof.go` overlaps with the responsibilities of `pkg/proofs/`. |
| `pkg/das/rpo` | RPO storage slots with no references. |
| `pkg/p2p/netutil` | IBLT, set reconciliation, and bandwidth tracking with no importers. |
| `pkg/proofs/groth16` | **Duplicates `pkg/proofs/groth16_gnark.go` almost line for line.** The package declarations are `package groth16` and `package proofs`, respectively. `node/` and `txpool/stark` use the root package implementation. The subpackage copy is unused. |
| `pkg/core/eftest` | A seven-file Ethereum Foundation (EF) state test runner with no callers on production paths. The execution path for the 37,126 EF vectors is reachable through tests, which CI skips when the fixtures are missing. See [Section II](#ii-ineffective-tests). |
| `pkg/core/ratemeter` | Its sole reference is in `core/rate_meter_test.go`. The active implementation uses `GasRateTracker` from `core/gigagas`. |

#### A2. Dead Files in the `pkg/core` Root Package

| File | Finding |
|---|---|
| `core/state_transition.go` | A complete `StateTransition` implementation and eight `ErrST*` errors. The active execution path runs through `core/execution`, leaving this copy of the state machine unused in production. |
| `core/genesis_commit.go` | `CommitGenesis()` is an unused copy of `core/config.CommitGenesisBlock()` with no callers. |
| `core/bloombits.go` | `BloomMatchesAddresses` and related functions have no external references. |
| `core/block_helpers.go` | The comments explain that the implementations moved to `core/block`. The remaining unexported wrappers serve only tests in the root package. |

> Removing these files requires addressing any `_test.go` files in the `core` root package that depend on them.

#### A3. Files Excluded from Standard Builds and Their Unused Dependency

- `pkg/crypto/bls/bls_blst_init.go`, `pkg/crypto/bls/bls_blst_adapter.go`, and their tests require `//go:build blst`. The standard build in `pkg/Makefile` uses `BUILD_TAGS := goethkzg` and excludes those files.
- Those two implementation files are the **only direct importers in this repository** of `supranational/blst v0.3.17` in `go.mod`. The audit identifies it as an unused direct dependency eligible for removal.

#### A4. Dead Symbols in Production Files

- `pkg/e2e_advanced_helpers.go` is compiled into standard builds. `deadcode` flags `MockBlockExecutor` (line 31), `MockProofValidator.ValidateProof` (line 46), all four methods of `AlwaysValidBLS` (lines 55–58), and `SimulateAttack` (line 155) as unreachable. A repository-wide search confirmed that **no `_test.go` file uses them**. The file contains 21 unreachable functions in total.
- `pkg/e2e_helpers.go` defines `MakeBlobTx` and `MakeContractTx`. Their only callers are tests of the helpers themselves. See [Section II.2](#2-potentially-ineffective-tests-about-110).

#### A5. Transitively Dead Code

- **`pkg/geth` (geth compatibility layer, 42 unreachable functions)** is imported only by `core/eftest/geth_runner.go`. Since `core/eftest` itself has no importers, the package is unreachable from production entry points. Both packages can be removed together.
- The prior audit at `docs/UNWIRED_CODE.md:750` claims that `cmd/eth2030-geth/precompiles.go:77` calls `ListCustomPrecompiles()`. The current import search found no `pkg/geth` import in `cmd/eth2030-geth`, so that finding is outdated.

### B. Suspected Dead Code (Requires Confirmation)

| Item | Finding |
|---|---|
| `pkg/epbs` and `pkg/rollup` root packages | Imported only by the root end-to-end test package and by their own subpackages. Neither root package is reachable from `cmd` or `node`, while their subpackages are wired in. Confirm whether the public root-package types should be retained. |
| More than 15 `*_compat.go` re-export shims | `log/formatter_compat.go`, `node/events_compat.go`, `node/health_checker_compat.go`, `node/lifecycle_compat.go`, `rlp/errors_compat.go`, `proofs/{queue,prover,kzg}_compat.go`, `rollup/anchor_chain_tracker_compat.go`, `eth/protocol_version_compat.go`, `engine/{errors,util}_compat.go`, `consensus/voting_compat.go`, `crypto/{vdf,threshold}_compat.go`, and `witness/cache_compat.go`. These symbols still have callers, but the migration shims are candidates for consolidation. |
| Two parallel beacon state implementations in `consensus` | `bsn_beacon_state.go` defines `BsnBeaconState` for `depproc_*`. `beacon_state_v2.go` defines `BeaconStateV2` for `finality_rules` and `fork_choice_lmd`. The two similarly structured state machines coexist and may eventually converge on one implementation. |
| `GoIPABackend` in `crypto/ipa` | The implementation in `ipa_integration.go:262-289` describes itself as a placeholder and always falls back to PureGo. The TODO at line 265 to add `ipa_goipa.go` with a `goipa` build tag remains open. |
| `eth/protocol_extended.go:15` | `ExtETH72 = 72 // EIP-XXXX` is a placeholder constant with an unresolved EIP reference. |

### C. Legacy Artifacts and Other Findings

| Item | Finding and disposition |
|---|---|
| `cl.log` (repository root, 40 KB) | Git still tracks this Lighthouse runtime log because it was committed before being ignored. Remove it with `git rm`. |
| `pkg/bin/` (two 74 MB binaries), `pkg/eth2030-geth` (55 MB), and `pkg/coverage/` (18 MB, stale output from 2026-02-25) | Git ignores these artifacts. They can be removed locally to reclaim disk space. |
| `pkg/Dockerfile` | The repository root has no Dockerfile, so this file is unique. Its `go build` omits `-tags goethkzg`, unlike the Makefile's `BUILD_TAGS` setting. The mismatch selects the non-production KZG backend in containers. |
| `pkg/cmd/send-frame` | A single-file development tool for sending frame transactions, used manually on local systems. |
| `site/` | The Next.js project website is independent of the Go code and was assessed separately. |
| Large blocks of commented-out code | A search for consecutive lines matching patterns such as `// if`, `// for`, `// func`, `// return`, and `// :=` found no blocks longer than five lines. |

> Symbol-level spot checks found external references to exports from `bal/`, `zkvm/`, `witness/`, and the root `proofs/` package, indicating that these packages are wired into the codebase.

---

## II. Ineffective Tests

For Go tests, `.github/workflows/ci.yml` runs only `go test ./...`. It passes no build tags, `-race`, or `-short` and **does not check out submodules**.

The overall baseline is good. Files such as `consensus/ssf_engine_test.go`, `core/vm/gas_table_test.go`, and `crypto/bls/bls12381_test.go` contain substantial assertions. Most initially suspicious tests proved effective because they delegate assertions to helpers. The audit confirmed the issues below.

### 1. Confirmed Ineffective Tests (About 169)

#### 1.1 Tests That Always Skip (25)

Each test starts with an unconditional `t.Skip(...)`, so its test logic never runs. The skips are associated with building the `refs/blst` submodule and still occur when blst has been built.

| Location | Count |
|---|---|
| `pkg/crypto/bls/bls_aggregate_test.go:99,184,224,282` | 4 |
| `pkg/crypto/bls/bls_aggregate_extended_test.go:82,192,232,250,369` | 5 |
| `pkg/crypto/bls/bls_integration_test.go:11,46,92,360` | 4. The tests are for the PureGo backend, but the copied skip reason cites blst. |
| `pkg/consensus/bls_operations_test.go:172,225,302` | 3 |
| `pkg/consensus/finality_bls_adapter_test.go:48,96,228,282` | 4 |
| `pkg/light/bls/bls_verifier_test.go:151,230,250` | 3 |
| `pkg/core/eftest/fixture_loader_test.go:371,698` | 2 |

#### 1.2 Empty or Placeholder Tests (2)

- `pkg/node/backend/backend_test.go:41` — `TestNewRPCBackend` contains only a comment and passes without even calling `t.Skip`.
- `pkg/node/backend/backend_test.go:46` — `TestNewEngineBackend` has the same issue.

#### 1.3 Checks That Cannot Fail the Test (20)

The failure paths log a message, skip the test, leave an empty branch, or discard an error.

| Location | Test | Issue |
|---|---|---|
| `pkg/das/das_test.go:424` | `TestDifferentNodeIDsGetDifferentGroups` | The failure branch calls `t.Log("warning: ...")` at line 441. |
| `pkg/das/network/network_test.go:348` | `TestAssignCustodyDifferentNodes` | Same logging pattern at line 363. |
| `pkg/das/network/das_network_mgr_test.go:736` | `TestSelectSampleColumnsDifferentSlots` | Same logging pattern at line 750. |
| `pkg/das/cell/cell_gossip_test.go:78` | `TestAssignSubnetsDifferentNodes` | Same logging pattern at line 93. |
| `pkg/das/validator/validator_test.go:309,329` | `TestSampleColumnsDifferentSlots` / `TestSampleColumnsDifferentNodes` | Same logging pattern at lines 325 and 345. |
| `pkg/consensus/committee_selection_test.go:73` | `TestComputeShuffledIndexDifferentSeeds` | Same logging pattern at line 83. |
| `pkg/consensus/committee_assignment_test.go:98,273` | `TestSwapOrNotShuffleDifferentSeeds` / `TestProposerWeightedSelection` | Same logging pattern at lines 110 and 299. |
| `pkg/consensus/committee_rotation_test.go:420` | `TestCREpochTransitionRotation` | Same logging pattern at line 432. |
| `pkg/core/types/bloom_test.go:127,~168` | `TestBloomContainsNegative` and related tests | Calls `t.Log("false positive...")` at lines 134 and 175. |
| `pkg/core/types/bloom_methods_test.go:126` | `TestBloomTestNonMembership` | Same logging pattern at line 131. |
| `pkg/txpool/shared/shared_mempool_test.go:222` | `TestIsKnown_UnknownTx` | The failure branch calls `t.Skip(...)` at line 229. The stated reason is incorrect because an empty Bloom filter cannot return a false positive. |
| `pkg/ssz/merkle_test.go:156,328,363` | `TestMerkleizeEmptyChunks` and related tests | The branch `if root == [32]byte{} { /* comment only */ }` contains no executable code. |
| `pkg/node/genesis_json_test.go:128` | `TestAddressJSON_WithoutPrefix` | `if err != nil { _ = err }` discards the error, so either result passes. |
| `pkg/e2e_rv_guest_test.go:156` | `TestE2E_RV_PreIPlusForkBoundary` | `_ = err` discards the result. The comment says the test is meant to “just verify no panic”. |
| `pkg/core/vm/call_gas_test.go:565` | `TestCallWithValue_StipendNotReturnedToCaller` | The failure branch calls `t.Logf("note: ...")` at line 616. |

#### 1.4 Tests Excluded from CI by Build Tags (122 Across 10 Files)

CI passes no `-tags` flag, and no workflow runs the tests below with their required tags. These files are therefore excluded from CI compilation.

- `//go:build integration` (76 tests) — `core/e2e_test.go` (15), `core/bal_integration_test.go` (18), `core/e2e_block_test.go` (5), `core/e2e_eip_spec_test.go` (7), `core/e2e_eip_spec_ext_test.go` (11), `core/e2e_local_tx_test.go` (11), `core/dim_storage_chain_test.go` (7), and `p2p/tor_integration_test.go` (2).
- `//go:build blst` (25 tests) — `crypto/bls/bls_blst_adapter_test.go`.
- `//go:build goethkzg` (21 tests) — `crypto/bls/kzg_goeth_adapter_test.go`. The Makefile uses this tag only when building binaries.

#### 1.5 Duplicated Coverage (3 Groups)

- `TestProverBackendInterfaceImplementations` in `pkg/zkvm/types_test.go:190` and `TestProverBackendInterface` in `pkg/zkvm/zkvm_test.go:280` have identical bodies.
- `TestE2E_PQBinary_ProofAggregation` in `pkg/e2e_pq_binary_test.go:333` covers everything in `TestE2E_Roadmap_ProofAggregation` in `pkg/e2e_roadmap_test.go:783` and adds a tampering check.
- Three tests cover the same transaction lifecycle in `e2e_test.go:340`, `e2e_pipeline_test.go:18`, and `integration_test.go:102`. Each funds an account, submits a transaction to the pool, produces a block, and checks the result. Only the constants differ.

### 2. Potentially Ineffective Tests (About 110)

- **Fixture-dependent tests that skip in CI (about 40).** `core/eftest/fixture_loader_test.go` (35 tests) and `geth_runner_test.go` (5 tests) depend on `GeneralStateTests` from the `refs/go-ethereum` submodule. CI uses `actions/checkout@v4` without `submodules: recursive`, so all of them skip. The EF vectors are described as the **primary validation target**, yet CI has never run them.
- **Smoke tests that only check for panics (about 45).** These tests contain no assertions and can pass when functionality regresses. Examples include `p2p/discover/v5_extra_test.go` (15 tests), `rpc/conformance_test.go:1004,1011`, `core/state/access_list_test.go:177,202,208`, `core/vm/eip7708_test.go:127,375`, the `NoSubscribers` tests in `rpc/subscription/manager_test.go`, and `witness/collector_test.go:276`, whose comment says it makes no assertion about the witness.
- **Concurrency tests without assertions (about 40).** CI omits `-race`, so these tests can fail only on a panic or a deadlock that times out. Examples include `consensus/{epoch_processor,forkchoice,validator_lifecycle}_test.go`, `focil/enhanced_test.go:678,724`, and `das/custody/custody_verify_test.go:667`. If these tests are intended to detect data races, CI omits the check they need.
- **Tests of test helpers (16).** Every test in `pkg/e2e_helpers_test.go` exercises a factory in `e2e_helpers.go`. `MakeBlobTx` and `MakeContractTx` have no other callers, so their tests protect unused helpers. See [Section I.A4](#a4-dead-symbols-in-production-files).

### 3. Recommendations

1. **Enable `-race` in CI** so the roughly 40 concurrency tests can detect data races.
2. **Set `submodules: recursive` in CI** so the EF vectors run. The audit estimates that roughly 80% of the apparent `crypto/bls` coverage is absent under the current setup.
3. Decide whether to keep the `integration`, `blst`, and `goethkzg` test files. Add tagged CI jobs for the files retained. The 122 excluded tests and 25 unconditional skips account for most of the apparent coverage identified as ineffective.
4. `TestE2E_Roadmap_QuickSlotsTiming` in `pkg/e2e_roadmap_test.go:376` derives the slot from `time.Now()` and may be flaky on slow machines.
5. `TestJsonPointEvaluation` in `pkg/core/vm/precompiles_json_test.go:239` always skips, leaving the point evaluation precompile without vector coverage.
6. The Next.js site under `site/` has zero tests and no test script in `package.json`.

---

## III. Security Advisories and Dependency Upgrades (Completed)

### Go Module (`pkg/`)

| Item | Result |
|---|---|
| `govulncheck` scan | The sole finding was **GO-2026-5932**, a module-level advisory for the unmaintained `golang.org/x/crypto/openpgp` package, which has known security issues. Project code does not call it. |
| Upgrades | `golang.org/x/crypto` v0.58.0 → **v0.58.1-0.20261009181718-2f1f834330b2** (patched version). `golang.org/x/sync` v0.24.0 → **v0.25.0** (the only other direct dependency with an available update). |
| Verification | `go mod tidy` and `go build ./...` completed with **BUILD_OK**. The follow-up `govulncheck` reported **No vulnerabilities found**. |

At the time of the audit, the other direct dependencies, including `go-ethereum v1.17.10`, `gnark`, `go-eth-kzg`, `uint256`, and `blake3`, were already on their latest available versions.

The audit identifies `supranational/blst v0.3.17` as an unused direct dependency to remove alongside the files that require the `blst` build tag. See [Section I.A3](#a3-files-excluded-from-standard-builds-and-their-unused-dependency).

### Next.js Website (`site/`)

| Item | Result |
|---|---|
| `npm audit` before upgrades | **Five vulnerabilities (one critical and four high).** `next` (critical, 30 GitHub Security Advisories including request smuggling, RCE, SSRF, and cache poisoning), `postcss` (high, path traversal and arbitrary file reads), `sharp` (high, inherited vulnerabilities in libvips, libheif, and librsvg), `nanoid` (high), and `source-map-js` (high, event-loop DoS). |
| Upgrades | `next` ^15.1.0 → **^16.4.5** via `npm audit fix --force` for the major-version upgrade. The lockfile update patched `nanoid`, `sharp`, and `source-map-js`. |
| Verification | `npm audit` reported **found 0 vulnerabilities**. `npm run build` succeeded and generated **all 149 pages** (142 SSG paths plus static pages). |

### Changes Recorded by the Audit (Uncommitted at the Time)

```text
pkg/go.mod            x/crypto security fix and x/sync upgrade
pkg/go.sum            Corresponding checksum updates
site/package.json     next ^16.4.5
site/package-lock.json
site/tsconfig.json    Updated automatically by Next 16
site/next-env.d.ts    Updated automatically by Next 16
```

> The audit proposed separate commits with concise, action-oriented messages and no AI `Co-Authored-By` trailer, in line with repository rules.
>
> - `deps: bump x/crypto to security fix, x/sync to v0.25.0`
> - `site: upgrade next to 16.4.5, fix npm audit vulnerabilities`

---

## IV. Proposed Follow-Up Work (Pending Decisions)

In priority order, the audit proposes the following work.

1. **Remove confirmed dead code** listed in [Section I.A](#a-confirmed-dead-code-40-files-eligible-for-removal). This includes the 11 orphaned packages, four dead files in the `core` root package, the `core/eftest` → `pkg/geth` chain, the `blst`-tagged files and their unused dependency, and dead symbols in `e2e_advanced_helpers.go`.
2. **Run `git rm cl.log`** and clean up the local artifacts under `pkg/bin/`, `pkg/coverage/`, and `pkg/eth2030-geth`.
3. **Add `-tags goethkzg` to the Dockerfile build** to match the Makefile.
4. **Clean up the tests** listed in [Section II.1](#1-confirmed-ineffective-tests-about-169). Remove the 25 permanently skipped tests and the two empty tests, add failing assertions to the 20 ineffective checks, and decide which of the 122 tagged tests to retain.
5. **Strengthen CI** by enabling `-race`, checking out submodules with `submodules: recursive`, and deciding which tagged jobs to run.
6. **Review the suspected dead code** listed in [Section I.B](#b-suspected-dead-code-requires-confirmation). Decide which compatibility shims to consolidate, whether to converge on a single beacon state implementation, and whether to implement or remove `GoIPABackend`.

---

## Appendix — Audit Method and Reproduction Commands

```bash
cd pkg
govulncheck ./...                     # Security scan. Use -show verbose for module-level advisories.
deadcode ./...                        # 12,049 unreachable functions.
deadcode -test ./...                  # 1,773 unreachable functions with test entry points included.
go list ./...                         # 228 packages. Search for each import path to verify orphaned packages.
cd ../site && npm audit --omit=dev    # npm vulnerability audit.
```

To verify orphaned packages, list packages with `go list ./...`. For each non-`main` package, search the entire repository for its import path, excluding the package itself and `_test.go` files. Packages with zero references are listed in [Section I.A1](#a1-orphaned-packages).
