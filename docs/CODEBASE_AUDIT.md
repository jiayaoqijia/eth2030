# eth2030 Codebase Audit (Reachability, Test Coverage, and Dependency Security)

- **Audit and review date** — 2026-11-10.
- **Scope** — The Go module under `pkg/`, the Next.js site under `site/`, and their CI workflows. Read-only `refs/` submodules were outside the code audit and were used for specification and fixture evidence.
- **Code baseline** — `master` at the original report commit `5ffa711`. The review ran at `2d13751`, which has identical `pkg/`, `site/`, and `.github/` trees. `pkg/` contains 2,286 Go files, including 1,092 test files with 440,298 lines. `go list ./...` reports 228 packages.
- **Tools** — Go 1.27.3, `deadcode` from `golang.org/x/tools` v0.52.0, `govulncheck` v1.9.0, Node.js 22.20.0, and npm 10.9.3. The review compared source imports, binary dependency graphs, test assertions, tagged test runs, and dependency scans.
- **Prior audit** — The March 2026 audit provides historical context. Its `pkg/geth` call-site finding still holds at this baseline. [March 2026 audit](UNWIRED_CODE.md)

Sections I through IV refer to the audited baseline. [Section V](#v-follow-up-results) records the implementation and verification results on the follow-up branch.

---

## I. Dead Code and Unused Legacy Code

### Overall Reachability

| Analysis | Unreachable functions |
|---|---|
| `deadcode ./...` (default build tags and executable entry points) | **12,049** (core 2,987 / consensus 1,742 / crypto 951 / p2p 827 / das 821 / engine 513 / trie 492 / txpool 409 / …) |
| `deadcode -test ./...` (including test entry points) | **1,773** |

Adding test entry points makes 10,276 additional functions reachable, including implementation code exercised by tests. The remaining 1,773 are candidates for review within the analyzed build. Package dependencies, alternate build tags, and consumers outside the repository require separate checks before removal. [Go deadcode documentation](https://pkg.go.dev/golang.org/x/tools/cmd/deadcode)

### A. Code Unused by Default Production Entry Points

#### A1. Packages Without Production Importers

Ten packages below have no Go imports from outside their own directory, including imports from tests. `core/ratemeter` has one test-only importer. The nine packages other than `core/eftest` and `core/ratemeter` account for 48 tracked files, including tests and READMEs. The import search establishes in-repository usage and leaves external consumers unverified.

| Package | Finding and evidence |
|---|---|
| `pkg/txpool/validation` | An unused refactored version of `validateTx()` from `txpool.go`. |
| `pkg/txpool/queue` | An alternate implementation of `txSortedList` from `txpool.go`. |
| `pkg/txpool/replacement` | An alternate implementation of `hasSufficientBump()` from `txpool.go`. |
| `pkg/txpool/fees` | Four implementation files and four test files with no importers. The active fee logic uses `txpool/pricing`. |
| `pkg/das/gf` | A GF(2^16) Reed–Solomon library with no importers. `das/blockerasure` uses `das/erasure`, and `das/reconstruction` uses a separate field implementation. |
| `pkg/das/proof` | A block-to-blob encoding and proof package with no importers. It overlaps with `pkg/proofs/` in subject matter. Interchangeability was not established. |
| `pkg/das/rpo` | Rows-per-operation configuration, scheduling, and throughput estimates with no importers. |
| `pkg/p2p/netutil` | IBLT, set reconciliation, and bandwidth tracking with no importers. |
| `pkg/proofs/groth16` | The implementation closely matches `pkg/proofs/groth16_gnark.go`. The root file also has a BN254 scalar-field helper. `node/` uses the root Groth16 path, while `txpool/stark` uses the root package's STARK implementation. The Groth16 subpackage has no importers. |
| `pkg/core/eftest` | A test runner with no production importers. `go test ./...` discovers the package directly. Its tests include standalone checks and fixture-backed runs. See [Section II.2](#2-fixture-coverage-and-other-test-findings). |
| `pkg/core/ratemeter` | Its sole reference is in `core/rate_meter_test.go`. The active implementation uses `GasRateTracker` from `core/gigagas`. |

#### A2. Production-Unreachable Files in the `pkg/core` Root Package

| File | Finding |
|---|---|
| `core/state_transition.go` | All ten functions are unreachable in the default executable analysis. The file declares eight `ErrST*` errors. Production execution goes through `core/execution`, and tests still exercise parts of the root implementation. |
| `core/genesis_commit.go` | `CommitGenesis()` has a caller in `core/genesis_test.go`. It creates a blockchain and stores its configuration. `core/config.CommitGenesisBlock()` writes a supplied genesis block and chain metadata through `ChainDB`. |
| `core/bloombits.go` | Three production-unreachable Bloom query functions are exercised by `core/receipt_test.go`. |
| `core/block_helpers.go` | Nine unexported wrappers for `core/block` are unreachable in the default executable analysis. `verifyGasLimit` remains reachable through tests. |

> Any consolidation should preserve relevant test coverage and account for exported functions that external Go consumers could use.

#### A3. Optional blst Backend

- `bls_blst_init.go`, `bls_blst_adapter.go`, and `bls_blst_adapter_test.go` require `//go:build blst`. Neither the default Makefile targets nor the Dockerfile select it.
- `bls_blst_adapter.go` directly imports `github.com/supranational/blst/bindings/go`. In tagged builds, `bls_blst_init.go` registers `BlstRealBackend` as the default. `go list -tags blst -deps` and `go mod why -m` confirm the dependency serves that build. Removing it would retire the optional backend. [Adapter](../pkg/crypto/bls/bls_blst_adapter.go#L20) · [Registration](../pkg/crypto/bls/bls_blst_init.go#L8)

#### A4. E2E Helpers and Test Callers

- `pkg/e2e_advanced_helpers.go` is compiled as part of the module root package. All 21 functions are unreachable from default executable entry points. With `-test`, only `SimulateAttack` remains unreachable. The E2E tests reach `MockBlockExecutor`, `MockProofValidator`, and `AlwaysValidBLS` through `MakeFinalityPipeline`. [Caller](../pkg/e2e_advanced_test.go#L27) · [Helper](../pkg/e2e_advanced_helpers.go#L62)
- `pkg/e2e_helpers.go` defines `MakeBlobTx` and `MakeContractTx`. Their only callers are tests of the helpers themselves. See [Section II.2](#2-fixture-coverage-and-other-test-findings).

#### A5. Production Use of the geth Compatibility Package

- The default scan reports **42 unreachable functions** within `pkg/geth`. The `eth2030-geth` startup path still calls `InjectIntoGethPrecompiles()`, which uses `pkg/geth.ListCustomPrecompiles()` to register custom precompiles for the selected fork. The EF runner is a separate consumer. [Startup](../pkg/cmd/eth2030-geth/main.go#L154) · [Precompile injection](../pkg/cmd/eth2030-geth/precompiles.go#L67)
- Moving `pkg/geth` aside in a disposable source copy caused `go build ./cmd/eth2030-geth` to fail on the missing import. The proposed joint removal with `core/eftest` is withdrawn.

### B. Compatibility and Parallel Implementations

| Item | Finding |
|---|---|
| `pkg/epbs` and `pkg/rollup` root packages | Both have production consumers. `eth2030` reaches `epbs` through `node → engine` and `rollup` through `node → rollup/anchor`. `eth2030-geth` also reaches `rollup` through `geth → core/vm → rollup/execute`. These paths use ePBS bid types and rollup types or errors. |
| Compatibility re-exports | All 16 cited files exist. They include `log/formatter_compat.go`, `node/{events,health_checker,lifecycle}_compat.go`, `rlp/errors_compat.go`, `proofs/{queue,prover,kzg}_compat.go`, `rollup/anchor_chain_tracker_compat.go`, `eth/protocol_version_compat.go`, `engine/{errors,util}_compat.go`, `consensus/voting_compat.go`, `crypto/{vdf,threshold}_compat.go`, and `witness/cache_compat.go`. Consolidation depends on the callers and API contracts of each re-export. |
| Parallel beacon state implementations in `consensus` | `BsnBeaconState` serves deposit and attestation processing, among other consumers. `BeaconStateV2` serves finality rules, fork choice, and other consumers. Other representations also exist, and `UnifiedBeaconState` already provides migration from `BeaconStateV2`. These observations do not establish interchangeability. |
| `GoIPABackend` in `crypto/ipa` | The implementation in `ipa_integration.go:262-289` describes itself as a placeholder and always falls back to PureGo. The TODO at line 265 to add `ipa_goipa.go` with a `goipa` build tag remains open. |
| `eth/protocol_extended.go:15` | The `EIP-XXXX` comment is unresolved. The value `ExtETH72 = 72` is used in the capability table. |

### C. Legacy Artifacts and Other Findings

| Item | Finding and disposition |
|---|---|
| `cl.log` (repository root, about 40 KB) | An ignored local Lighthouse runtime log. `git ls-files` and the available Git history contain no record of it. |
| Ignored build and coverage output | `pkg/bin/` holds roughly 17 MiB and 57 MiB binaries, about 74 MiB combined. The separate `pkg/eth2030-geth` is about 54 MiB. `pkg/coverage/` holds about 18 MiB of output dated 2026-02-25. |
| `pkg/Dockerfile` | The only tracked Dockerfile omits the Makefile's `-tags goethkzg` in both binary builds. Its effect depends on the KZG call path, as described below. |
| `pkg/cmd/send-frame` | A single-file development CLI for sending frame transactions. No repository automation was found that invokes it. External use remains unknown. |
| `site/` | The Next.js project website is independent of the Go code and was assessed separately. |
| Commented code | The earlier zero-match claim has a counterexample. `crypto/bls/bls_integration.go:322` starts six consecutive matching lines in a commented adapter example. The scan also matches documentation examples, so each hit needs context. |

> The `eth2030` binary has dependency paths to `bal/`, `zkvm/`, `witness/`, and the root `proofs/` package.

#### KZG Backend Selection and Native Point Evaluation

`node.New` attempts to initialize `NewGoEthKZGBackend()` with the Ethereum ceremony setup for blob commitments and PeerDAS cell proofs in both builds. The `goethkzg` tag adds a separate adapter. The native EVM's `0x0a` precompile follows `KZGVerifyFromBytes` to the test setup `[42]G2` under either build. [Node initialization](../pkg/node/node.go#L282) · [Native precompile](../pkg/core/vm/precompiles.go#L490) · [Test setup](../pkg/crypto/bls/kzg.go#L37) · [EIP-7594](https://eips.ethereum.org/EIPS/eip-7594)

The same `pointEvaluation1` fixture from go-ethereum v1.17.10 failed in the native implementation with and without `goethkzg`. The go-ethereum Cancun precompile returned the expected bytes. This reproduces a native EIP-4844 compatibility gap. Adding the Docker build tag alone leaves it unresolved. [Point evaluation specification](https://eips.ethereum.org/EIPS/eip-4844#point-evaluation-precompile)

---

## II. Test Coverage and CI

The baseline workflow runs `gofmt`, `go vet ./...`, and `GOFLAGS=-p=4 go test ./... -timeout=15m -count=1`. It checks out the main repository without submodules and passes no build tags, `-race`, or `-short`. In independent local runs without `refs/`, Go 1.27.3 passed vet and tests, with 213 packages passing and 15 packages without test files. The formatting check flagged `engine/backend.go` and `engine/lighthouse_compat_test.go`. The workflow selects Go 1.26. [CI workflow](../.github/workflows/ci.yml)

Files such as `consensus/ssf_engine_test.go`, `core/vm/gas_table_test.go`, and `crypto/bls/bls12381_test.go` contain substantive assertions. The findings below distinguish tests that skip, weak assertions, and tests that run only with alternate build tags. The original aggregate of about 169 ineffective tests combined these categories and has been withdrawn.

### 1. Verified Skips, Assertions, and Build Tags

#### 1.1 Tests That Always Skip (26)

Each listed test unconditionally skips. The 23 BLS-related tests cite pairing correctness or the blst backend, and the same skips apply with `-tags blst`. The two EF tests direct readers to the geth-backed summary. The point evaluation test cites the native test setup.

| Location | Count |
|---|---|
| `pkg/crypto/bls/bls_aggregate_test.go:99,184,224,282` | 4 |
| `pkg/crypto/bls/bls_aggregate_extended_test.go:82,192,232,250,369` | 5 |
| `pkg/crypto/bls/bls_integration_test.go:11,46,92,360` | 4. Two explicitly named PureGo tests also cite blst. |
| `pkg/consensus/bls_operations_test.go:172,225,302` | 3 |
| `pkg/consensus/finality_bls_adapter_test.go:48,96,228,282` | 4 |
| `pkg/light/bls/bls_verifier_test.go:151,230,250` | 3 |
| `pkg/core/eftest/fixture_loader_test.go:371,698` | 2 |
| `pkg/core/vm/precompiles_json_test.go:239` | 1 |

#### 1.2 Empty or Placeholder Tests (2)

- `pkg/node/backend/backend_test.go:41` — `TestNewRPCBackend` contains only a comment and passes without even calling `t.Skip`.
- `pkg/node/backend/backend_test.go:46` — `TestNewEngineBackend` has the same issue.

#### 1.3 Weak Checks in the Listed Tests (20)

The identified branches log a message, skip the test, do nothing, or discard an error. `TestProposerWeightedSelection`, `TestLogsBloom`, and the stipend test retain other assertions. A disposable mutation made `SampleColumns` ignore both slot and node ID. The corresponding two tests logged identical samples and still passed.

| Location | Test | Issue |
|---|---|---|
| `pkg/das/das_test.go:424` | `TestDifferentNodeIDsGetDifferentGroups` | The failure branch calls `t.Log("warning: ...")` at line 441. |
| `pkg/das/network/network_test.go:348` | `TestAssignCustodyDifferentNodes` | Same logging pattern at line 363. |
| `pkg/das/network/das_network_mgr_test.go:736` | `TestSelectSampleColumnsDifferentSlots` | Same logging pattern at line 750. |
| `pkg/das/cell/cell_gossip_test.go:78` | `TestAssignSubnetsDifferentNodes` | Same logging pattern at line 93. |
| `pkg/das/validator/validator_test.go:309,329` | `TestSampleColumnsDifferentSlots` / `TestSampleColumnsDifferentNodes` | Same logging pattern at lines 325 and 345. |
| `pkg/consensus/committee_selection_test.go:73` | `TestComputeShuffledIndexDifferentSeeds` | Same logging pattern at line 83. |
| `pkg/consensus/committee_assignment_test.go:98,273` | `TestSwapOrNotShuffleDifferentSeeds` / `TestProposerWeightedSelection` | Same logging pattern at lines 110 and 299. The proposer test still fails on an assignment error. |
| `pkg/consensus/committee_rotation_test.go:420` | `TestCREpochTransitionRotation` | Same logging pattern at line 432. |
| `pkg/core/types/bloom_test.go:127,145` | `TestBloomContainsNegative` / `TestLogsBloom` | Negative cases only log possible false positives. `TestLogsBloom` asserts that the address and both topics are present. |
| `pkg/core/types/bloom_methods_test.go:126` | `TestBloomTestNonMembership` | Same logging pattern at line 131. |
| `pkg/txpool/shared/shared_mempool_test.go:222` | `TestIsKnown_UnknownTx` | The failure branch calls `t.Skip(...)` at line 229. The stated reason is incorrect because an empty Bloom filter cannot return a false positive. |
| `pkg/ssz/merkle_test.go:156,328,363` | `TestMerkleizeEmptyChunks` / `TestHashTreeRootByteListEmpty` / `TestHashTreeRootBitlistEmpty` | The root checks have empty branches. |
| `pkg/node/genesis_json_test.go:128` | `TestAddressJSON_WithoutPrefix` | `if err != nil { _ = err }` discards the error, so either result passes. |
| `pkg/e2e_rv_guest_test.go:156` | `TestE2E_RV_PreIPlusForkBoundary` | The test discards the return data and error, leaving the pre-fork execution result unverified. |
| `pkg/core/vm/call_gas_test.go:565` | `TestCallWithValue_StipendNotReturnedToCaller` | The gas comparison only logs at line 616. The value-bearing call still fails the test if it returns an error. |

#### 1.4 Tests Excluded from CI by Build Tags (122 Across 10 Files)

The current workflows do not select these tags. Local tagged runs established the following results.

- **`integration` (76 tests).** The files are `core/e2e_test.go` (15), `core/bal_integration_test.go` (18), `core/e2e_block_test.go` (5), `core/e2e_eip_spec_test.go` (7), `core/e2e_eip_spec_ext_test.go` (11), `core/e2e_local_tx_test.go` (11), `core/dim_storage_chain_test.go` (7), and `p2p/tor_integration_test.go` (2). The `p2p` tests fail to compile because they refer to Tor/Nym helpers defined in `p2p/transport`. The `core` suite runs and fails `TestValidator_RejectsMissingBALHash` and `TestBAL_OnlyRequiredWhenForkActive`. Both expect rejection of a missing post-Amsterdam BAL hash, which the validator explicitly accepts for CL compatibility. [Validator](../pkg/core/block/validator.go#L263)
- **`blst` (25 tagged tests).** The tagged `crypto/bls` run compiles, then fails `TestBLSTBackend`, `TestBLSAttestation`, and `TestBLSIntegrationBackendSwitching`. The first two report key size or invalid key errors. The third still expects a PureGo default when the tagged initializer selects blst. [Tagged tests](../pkg/crypto/bls/bls_blst_adapter_test.go)
- **`goethkzg` (21 tagged tests).** The `crypto/bls` package passes with `-tags goethkzg`. The Makefile applies the tag to its binary targets, so its default build exercises compilation rather than these tests. [Tagged tests](../pkg/crypto/bls/kzg_goeth_adapter_test.go)

#### 1.5 Overlapping Coverage (3 Groups)

- `TestProverBackendInterfaceImplementations` in `pkg/zkvm/types_test.go:190` and `TestProverBackendInterface` in `pkg/zkvm/zkvm_test.go:280` perform the same two compile-time interface checks.
- `TestE2E_PQBinary_ProofAggregation` in `pkg/e2e_pq_binary_test.go:333` checks the proof count and rejects a tampered aggregate. `TestE2E_Roadmap_ProofAggregation` in `pkg/e2e_roadmap_test.go:783` explicitly checks `Valid` and a nonzero aggregate root. Both check aggregate verification, and each has distinct assertions.
- `TestEndToEndTxPool` in `pkg/e2e_test.go:340` checks pending transactions. `TestE2EFullBlockLifecycle` in `pkg/e2e_pipeline_test.go:18` also builds a block and asserts receipt status, balance, and nonce. `TestFullTransactionLifecycle` in `pkg/integration_test.go:102` exercises a signed transaction lifecycle.

### 2. Fixture Coverage and Other Test Findings

#### Ethereum Foundation State Tests

The fixture-free `go test -v -count=1 ./core/eftest` run reported **21 passing and 35 skipped top-level tests**. `fixture_loader_test.go` skipped 27 of 35 and `geth_runner_test.go` skipped 3 of 5. The fixture loader looks for multiple sources, including geth CLI fixtures and execution-spec fixtures as well as `GeneralStateTests`. A parent test can also pass with all of its fixture subtests skipped. [Fixture paths](../pkg/core/eftest/fixture_loader_test.go#L12)

With the pinned `GeneralStateTests` from `refs/go-ethereum` at `0cba803fbafb12e9daaea53b76de847842ab3055` and its `tests/testdata` submodule at `81862e4848585a438d64f911a19b3825f0f4cd95`, `TestGethCategorySummary` reported **36,264 passed, 20 failed, and zero skipped out of 36,284 executed vectors**. Failures were in `stCreate2` (8), `stExtCodeHash` (2), `stRevertTest` (2), and `stSStoreTest` (8). Go still reported `PASS` because the summary test only fails when zero vectors pass. These counts describe the go-ethereum execution backend and the vectors the runner counted. The run provides no result for full conformance of the native EVM. [Runner](../pkg/core/eftest/geth_runner.go#L182) · [Summary assertion](../pkg/core/eftest/geth_runner_test.go#L174)

The current workflow does not obtain these fixtures. Earlier revisions of the workflow, before `14497b5`, did configure recursive submodule checkout. Historical run logs were unavailable during review, so prior execution remains unverified.

#### Other Test Findings

- Several Access List, EIP-7708, subscription, and witness tests exercise panic-free behavior without asserting the resulting state. `p2p/discover/v5_extra_test.go` contains both smoke tests and tests with setup or behavioral assertions. The cited RPC `Accounts` and `ProtocolVersion` tests call `requireSuccess`, which asserts that the response carries no error. [RPC helper](../pkg/rpc/conformance_test.go#L48)
- The default CI lacks race instrumentation. Targeted `-race` runs in `consensus`, `focil`, and `das/custody` passed. Several concurrency tests exercise liveness or panic-free behavior even without instrumentation. The earlier estimates of about 45 smoke tests, 40 concurrency tests, and 110 potentially ineffective tests lack a reproducible case list and are not retained as totals.
- `pkg/e2e_helpers_test.go` contains 15 tests. The two for `MakeBlobTx` and `MakeContractTx` are the only callers of those helpers. Other tests in the file cover helpers with additional consumers. [Section I.A4](#a4-e2e-helpers-and-test-callers)

### 3. Coverage and Timing Observations

- `crypto/bls` statement coverage was 83.7% by default and 83.9% with `goethkzg`. The report's earlier estimate that roughly 80% of apparent BLS coverage was missing had no defined denominator or demonstrated link to the EF fixtures.
- `TestE2E_Roadmap_QuickSlotsTiming` uses `time.Now()`, a six-second slot duration, and an acceptance window of slots 7 through 9 around slot 8. A delay of about 12 seconds between its clock reads can put it outside the window. The review established the time dependency from source and did not observe a flaky run. [Timing test](../pkg/e2e_roadmap_test.go#L376)
- `TestJsonPointEvaluation` unconditionally skips, leaving a gap for the production trusted-setup vector in the native test suite. Ten targeted KZG precompile unit tests passed, including tests built around the native test setup. See [the cross-backend fixture comparison](#kzg-backend-selection-and-native-point-evaluation).
- The Next.js site has no dedicated test suite or `test` script. Its `verify-eips.ts --files` check passed with 364 referenced paths checked and none missing. [Site scripts](../site/package.json) · [EIP verifier](../site/scripts/verify-eips.ts)

---

## III. Dependency Changes and Security Findings

### Go Module (`pkg/`)

| Item | Result |
|---|---|
| Before `bb29933` | `govulncheck -show verbose` reported **GO-2026-5932** at module level for `golang.org/x/crypto v0.58.0`. The advisory concerns the unmaintained `openpgp` packages. The scan found zero affected package imports and zero affected calls. [Go advisory](https://pkg.go.dev/vuln/GO-2026-5932) |
| Committed upgrades | `bb29933` updated `golang.org/x/crypto` to **v0.58.1-0.20261009181718-2f1f834330b2**, which clears the module-level finding, and `golang.org/x/sync` from v0.24.0 to **v0.25.0**. |
| Post-upgrade verification | `go mod tidy -diff` produced no changes and `go build ./...` passed. `govulncheck -show verbose` reported **No vulnerabilities found** for the database snapshot updated on 2026-11-05 UTC. |

The project pins **`gnark v0.16.4`**. The upstream **`v0.16.5`** release was available before `5ffa711` and fixes the high-severity **GHSA-4g5f-3gmm-9q87**. The advisory was published on 2026-11-09, after the Go vulnerability database snapshot used by this scan. It covers emulated scalar multiplication on BLS12-381 and BW6-761 and related circuit issues, including opt-in incomplete arithmetic on BN254. The pinned version is in the affected range. The project uses BN254 Groth16, and the review found neither the affected emulated curve packages in the production dependency graph nor use of `WithIncompleteArithmetic` in project Go source. Project exploitability remains unverified. [Release notes](https://github.com/Consensys-Incorporated/gnark/releases/tag/v0.16.5) · [Upstream advisory](https://github.com/Consensys-Incorporated/gnark/security/advisories/GHSA-4g5f-3gmm-9q87)

An explicit `go list -m -u` query found an update for gnark among the ten pinned direct dependencies. The `blst` dependency remains necessary for the optional build described in [Section I.A3](#a3-optional-blst-backend).

### Next.js Website (`site/`)

| Item | Result |
|---|---|
| `npm audit` before `67c5766` | **Five package findings (one critical and four high).** `next` (critical) had 30 direct advisories and two inherited entries from `postcss` and `sharp`. The four high findings were `postcss`, `sharp`, `nanoid`, and `source-map-js`. |
| Committed upgrades | The declared Next range moved from `^15.1.0` to **`^16.4.5`** and the resolved version moved from `15.5.12` to **`16.4.5`**. The lockfile also updated `postcss`, `nanoid`, `sharp`, and `source-map-js`. The Git diff establishes the resulting versions. Whether the upgrade used `npm audit fix --force` remains unverified. [Next.js release notes](https://github.com/vercel/next.js/releases/tag/v16.4.5) |
| Post-upgrade verification | Both `npm audit --json` and `npm audit --omit=dev --json` reported zero findings. A clean `npm ci` followed by `npm run build` generated **149 pages**, including 142 SSG paths. The audit counts describe dependency advisories returned by the registry for the lockfiles tested. [npm audit documentation](https://docs.npmjs.com/cli/v10/commands/npm-audit/) |

### Git History

The changes were already committed when the report entered Git. `bb29933` changed `pkg/go.mod` and `pkg/go.sum`. `67c5766` changed `site/package.json`, `site/package-lock.json`, `site/tsconfig.json`, and `site/next-env.d.ts`. Both commits precede `5ffa711` in the branch history.

---

## IV. Follow-Up Plan

1. **Fix native point evaluation.** Route `0x0a` through a verifier using the Ethereum ceremony setup and assert an upstream vector through the registered native precompile. Evaluate Docker/Makefile tag parity separately. [KZG findings](#kzg-backend-selection-and-native-point-evaluation)
2. **Make EF fixture coverage enforceable.** Provision the pinned fixture data, verify the intended vectors executed, and surface unexpected vector failures in CI. Keep native EVM coverage and geth-backed coverage separately labeled. [EF results](#ethereum-foundation-state-tests)
3. **Review the gnark upgrade.** Assess the pinned `v0.16.4` dependency against the advisory and verify the project's BN254 circuits and artifacts against the upstream `v0.16.5` migration requirements. [Go dependency findings](#go-module-pkg)
4. **Restore or retire test coverage case by case.** Review the 26 permanent skips and two placeholders, add assertions for the identified weak branches, and decide which tagged suites are supported. Resolve the observed compile and assertion failures before gating on those suites. Add race instrumentation where concurrent behavior is under test. [Test findings](#1-verified-skips-assertions-and-build-tags)
5. **Limit code removal to verified candidates.** Check consumers and preserve useful tests when evaluating unimported packages, individual unreachable functions, and compatibility re-exports. Retain the active `pkg/geth` startup path, preserve the EF runner's test value, and treat `blst` removal as a decision about support for the optional backend. [Reachability findings](#i-dead-code-and-unused-legacy-code)
6. **Clean ignored artifacts locally if needed.** `cl.log` and the listed binaries and coverage output are local artifacts. [Artifact findings](#c-legacy-artifacts-and-other-findings)

---

## V. Follow-Up Results

| Item | Implementation and local verification |
|---|---|
| Native KZG and Docker | Native point evaluation uses the Ethereum ceremony context shared with blob and PeerDAS operations. The upstream `pointEvaluation1` vector is tracked as a regular file so it runs without `refs/`. The registered precompile passes the vector in default and `goethkzg` builds. Both Docker binary builds use the Makefile's tag. [Precompile](../pkg/core/vm/precompiles.go) · [Fixture](../pkg/core/vm/testdata/precompiles/pointEvaluation.json) · [Dockerfile](../pkg/Dockerfile) |
| EF fixtures and CI | The geth-backed job checks out `ethereum/tests` at the pinned revision and requires the direct category fixture suite. The local gate reports **36,264 passed, 20 known legacy outcomes, zero unexpected failures, and zero skips**. Each legacy vector is still executed and its exact observed root is checked. Missing fixtures fail the gate, as do parse errors, unexpected outcomes, and changes in the counts. Nested fixture suites and native EVM conformance are outside this geth-backed gate. [Workflow](../.github/workflows/ci.yml) · [Fixture gate](../pkg/core/eftest/geth_runner_test.go) |
| Gnark migration | Upgraded to `gnark v0.16.5` and its required `gnark-crypto` revision. BN254 AA compilation, setup, proving, verification, and tampered-input rejection passed. A separate probe loaded the prior serialized AA R1CS and keys, verified a proof produced with v0.16.4, and generated a new proof with the prior keys and the recompiled circuit. Recompiled R1CS bytes differed. These results are scoped to this project's AA circuit. [Module versions](../pkg/go.mod) · [BN254 tests](../pkg/proofs/groth16_gnark_test.go) |
| Test coverage | Restored 23 BLS tests and the native KZG vector test. Retired two disabled EF reporting tests while preserving the native fixture regression cases. Replaced two backend constructor placeholders and tightened all 20 weak checks. The DAS seed mutation now fails both affected tests. Tagged `integration`, `blst`, and `goethkzg` suites and the targeted package race suite passed locally. Tor/Nym live tests require configured proxies and endpoints. The local runs exercised the deterministic SOCKS5 tests and skipped the two live cases. [Transport tests](../pkg/p2p/transport/tor_integration_test.go) · [CI variants](../.github/workflows/ci.yml) |
| Code and local artifacts | Removed the nine unexported `core` wrappers and moved their remaining gas-limit test caller to `core/block.VerifyGasLimit`. The active `pkg/geth` path, the EF runner, the optional blst backend, and exported candidates with unverified external usage were retained. The four ignored audit artifacts were moved out of the worktree for recovery. |

The local verification used Go 1.27.3. It covered formatting, vet, builds, the full Go test suite with and without fixtures, the added CI commands, and both container entry points. GitHub Actions and its Go 1.26 runner were outside the local verification. The baseline reproduction commands below retain their original scope.

The first package cleanup removed `txpool/validation`, `txpool/queue`, `txpool/replacement`, `txpool/fees`, `das/gf`, and `proofs/groth16`. The production dependency sets for both executables matched before and after the removal with default tags and with `integration,blst,goethkzg`. `das/proof`, `das/rpo`, and `p2p/netutil` remain pending roadmap decisions.

---

## Appendix — Audit Method and Reproduction Commands

Use a disposable copy of the baseline. Extract `git archive 5ffa711 pkg site .github CLAUDE.md docs/EIP_SPEC_IMPL.md` into an empty directory. This supplies the code and the website verifier's local documentation inputs. To reproduce the fixture-backed run, supply the pinned geth `GeneralStateTests` tree at the path read by `testdataDir()` in a separate copy.

```bash
cd pkg
go list ./...
deadcode -json ./...
deadcode -test -json ./...
go list -deps ./cmd/eth2030 ./cmd/eth2030-geth
go list -tags blst -deps ./cmd/eth2030 ./cmd/eth2030-geth
go mod why -m github.com/supranational/blst
gofmt -l .
go vet ./...
GOFLAGS=-p=4 go test ./... -timeout=15m -count=1
go test -v -count=1 ./core/eftest
go test -count=1 -tags integration ./core ./p2p
go test -count=1 -tags blst ./crypto/bls
go test -count=1 -tags goethkzg ./crypto/bls
go test -count=1 -cover ./crypto/bls
go test -count=1 -cover -tags goethkzg ./crypto/bls
go test -v -count=1 -run 'Test(JsonPointEvaluation|KZG)' ./core/vm
govulncheck -show verbose ./...
go mod tidy -diff
go build ./...
```

In the copy with pinned fixtures, run `go test -v -count=1 -run '^TestGethCategorySummary$' ./core/eftest` and read the vector totals in the log as well as the Go exit status. To reproduce the point evaluation probe, use `pointEvaluation1` from `core/vm/testdata/precompiles/pointEvaluation.json` in go-ethereum v1.17.10. Pass its input to the native `RunPrecompiledContract` at `0x0a` and to go-ethereum's Cancun precompile at the same address. Compare the return bytes and errors, then repeat with `-tags goethkzg`.

The targeted race run used `./consensus ./focil ./das/custody` with the following filter.

```bash
go test -race -count=1 \
  -run 'Test(ForkChoiceStore_ConcurrentAccess|LifecycleConcurrentAccess|EnhancedFOCILConcurrentValidate|EnhancedFOCILConcurrentBuildAndRead|CustodyVerifierConcurrentReads)$' \
  ./consensus ./focil ./das/custody
```

For the before-and-after security comparison, use `pkg/go.mod` and `pkg/go.sum` from `bb29933^` for the previous Go scan. Use `site/package.json` and `site/package-lock.json` from `67c5766^` for the previous npm scan. Advisory results depend on the database or registry snapshot at the time of the run.

From the snapshot root, run the site checks below.

```bash
cd site
npm audit --json
npm audit --omit=dev --json
npm ci
npm run build
node scripts/verify-eips.ts --files
```

To verify importers, search for each non-`main` package's exact import path in all other Go files, including `_test.go` files. Then use the executable dependency graph and `deadcode` results to distinguish production imports, test-only imports, and function-level reachability. [Package inventory](#a1-packages-without-production-importers)
