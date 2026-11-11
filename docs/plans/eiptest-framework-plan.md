# `eiptest` — EIP Integration Test Framework: Design & Task Plan

Status: implemented with documented compatibility boundaries (2026-11-11)
Target package: `pkg/eiptest/`

---

The [framework README](../../pkg/eiptest/README.md) documents the implemented
API and walkthrough. The [completion record](../../STATUS.md) lists the
verification results and the remaining codec and upstream compatibility
boundaries. The task tables retain the original acceptance criteria.

## 1. Purpose

`eiptest` is an **in-process integration test framework** that serves as the testing
foundation for **future feature development** (new EIPs, opcodes, tx types, engine
methods, precompiles). It is *not* a back-fill of regression tests for historical bugs.

Today, testing a new EIP across its full exposure surface (state transition, txpool
admission, block building, Engine API, RPC, fork gating, encoding) requires each
developer to re-assemble genesis helpers, signers, block builders, and HTTP test
servers by hand. Three separate in-process E2E systems already exist and share no
infrastructure (see §2). `eiptest` provides the shared base once, so a feature test
contains **only feature-specific assertions**.

Design principle: **every surface a new feature exposes gets a ready-made probe**.
Writing an integration test becomes fill-in-the-blank.

## 2. Current State (evidence)

### 2.1 Test tiers and measured timings (24-core local machine, images cached)

| Tier | Scope | Trigger | Measured / budgeted time |
|---|---|---|---|
| Unit | 206 packages, ~19.4k tests, `go test ./...` | CI `ci` job + manual | `-short` full run: **37.3s** locally; CI budget 15–20 min (`-p=4`) |
| EF state tests | 36,284 vectors via geth backend (`pkg/core/eftest/`) | CI dedicated job | CI budget 15 min |
| Tagged variants | `integration` / `blst` / `goethkzg` / `race` matrix | CI only | 15–20 min each |
| Kurtosis devnet | 40 feature configs, serial `run-feature-tests.sh` | **manual only, no CI job** | hours (serial) |

Structural issues relevant to this plan:

1. **Three unshared in-process E2E systems**: 137 test funcs at `pkg/` root (no tag),
   65 under `//go:build integration` in `pkg/core/` + `pkg/p2p/transport/`, 14 in
   `pkg/engine/`. (Correction, 2026-11-11 during implementation: the tag is NOT
   orphaned — CI's `test-variants` matrix runs `-tags integration ./core ./p2p/...`
   and the suite is green locally. `eiptest` supersedes it for future feature work;
   the existing suites stay for their current coverage.)
2. **5 duplicated genesis helpers** (`pkg/e2e_wave10_test.go:28`,
   `pkg/engine/backend_test.go:13`, `pkg/core/testhelpers_test.go:77`,
   `pkg/integration_test.go`, `pkg/e2e_helpers.go`) and **5 duplicated signing helpers**.
3. **No test chain crosses a real fork boundary timestamp**: all `TestConfig*` variants
   pin every fork activation at time 0 (`pkg/core/config/chain_config.go:430-535`).
4. `config.SetupGenesis` (`pkg/core/config/genesis_init.go:48`), the proper
   alloc-based genesis path, is unused by any E2E suite.
5. txpool is a 7-line stub in the core integration suite
   (`pkg/core/e2e_test.go:120`); the engine payload-build test cannot inject user
   transactions (`pkg/engine/eip_spec_e2e_test.go:33` builds empty payloads).
6. Contracts are hand-written bytecode everywhere; no solc/ABI layer (solc 0.8.38 is
   installed locally but unused by the build).
7. The geth adapter (`pkg/geth/`, differential oracle) is absent from E2E surfaces.

### 2.2 What surfaces do new features actually touch?

The git history (914 commits, 288 fix-type, 2026-02 → 2026-11) shows fixes cluster on
a fixed set of surfaces — the same surfaces every new feature must be tested on:

| Surface | Fix commits touching it (approx.) |
|---|---|
| Engine API (JSON shapes, payload lifecycle) | ~30 |
| RPC correctness | ~25 |
| txpool ↔ chain events | ~12 |
| Reorg / chain state | ~15 |
| EIP spec deviation | ~12 |
| Fork gating / genesis config | ~8 |

This motivates the harness set in §4: the framework builds each surface once, and every
future feature tests against it.

## 3. Goals / Non-Goals

**Goals**

- G1: A new EIP's integration test file contains only feature-specific assertions;
  zero genesis/signing/builder/server setup code.
- G2: Single-EIP iteration < 1s; entire `pkg/eiptest` suite < 2 min; runs as part of
  default `go test ./...` (no build tag — avoids repeating the orphaned-tag failure).
- G3: Every surface (STF, pool, fork, encoding, RPC, engine, differential) is reachable
  through one framework call.
- G4: Coverage gate: an EIP present in the implementation list without a test file
  fails the default suite (D-tier network surfaces exempt).
- G5: A scaffold generator creates the test skeleton + registry entry for a new EIP.

**Non-Goals**

- Multi-node/network behavior (PeerDAS sampling, gossip, discovery) — stays with the
  devnet feature-test layer (`pkg/devnet/kurtosis/`).
- Replacing EF state tests or consensus-spec vectors — `eiptest` *consumes* them.
- CL-side consensus logic (3SF, quick slots) beyond EL-side coupling (requests, queues).
- Regression back-fill of historical bugs; migrating legacy E2E tests is a separate
  optional phase (P5).

## 4. Architecture

### 4.1 Package layout

```
pkg/eiptest/
├── sim.go            # Sim: in-process chain simulator (core)
├── option.go         # Fork schedule, prefunded accounts, chain config options
├── keyring.go        # deterministic prefunded keys, signers
├── tx.go             # builders for all 10 tx types (signed + SetSender fast path)
├── assert.go         # expectation helpers with diff output + failure dumps
├── stf.go            # STF probe   (StateProcessor / ProcessWithBAL)
├── fork.go           # Fork probe  (real-timestamp activation boundaries)
├── encoding.go       # Encoding probe (RLP/SSZ roundtrip + vector loader)
├── rpc.go            # RPC probe   (httptest server over the Sim chain)
├── engine.go         # Engine probe (FCU → GetPayload(tx-injectable) → ProcessBlock)
├── pool.go           # Pool probe  (real txpool wired to chain events)
├── diff.go           # Diff probe  (native vs pkg/geth dual backend)
├── registry.go       # EIP → test-file/surface coverage registry + gate
├── contracts/        # .sol sources + generated hex (see T4.1)
├── testdata/         # per-EIP vector directories
├── README.md         # API reference + "test a new EIP" walkthrough
└── eip<id>_test.go   # one file per EIP (framework's users)
```

### 4.2 Sim composition (production modules only)

| Sim method | Underlying production path |
|---|---|
| `New(t, opts...)` | genesis alloc via `config.SetupGenesis` (or `testutil.MakeGenesis` for minimal cases); chain config from scheduled-fork builder (T0.1) |
| `Commit(txs...)` | `block.NewBlockBuilder.BuildBlock(parent, attrs)` → `chain.Blockchain.InsertBlock` (insert-time validation incl. BAL hash enforced) |
| `Call(from, to, data)` | read-only `vm.EVM.Call` against current chain state |
| `AdvanceTime(d)` / `Slot(n)` | timestamp progression; fork boundaries derived from config schedule |
| `State()` | `chain.Blockchain.State()` after insert (state continuity by design, no manual rebuild) |

Reference pattern for the closed loop: `pkg/core/e2e_test.go:27` (`e2eChain`) and
`:129` (`buildAndInsert`); block result backfill semantics follow
`internal/testutil/block_helpers.go:47` (`MakeBlockWithState`).

### 4.3 API sketch (what a feature test looks like)

```go
// pkg/eiptest/eip8200_test.go — the only file a feature developer writes
func TestEIP8200(t *testing.T) {
    s := eiptest.New(t,
        eiptest.ForkAt(eiptest.Hegota, 100),
        eiptest.Fund(eiptest.Alice, 100*eiptest.Ether),
    )

    s.STF(t, func(st *eiptest.STF) {          // ① opcode/precompile behavior
        c := st.Deploy(eiptest.Contracts.MyFeature())
        st.Tx(s.Alice, c, "run()").ExpectStatus(eiptest.Success).ExpectGas(eiptest.Between(21_000, 30_000))
    })
    s.Pool(t, func(p *eiptest.PoolProbe) {     // ② admission / replacement / eviction
        p.Submit(badTx).ExpectReject(eiptest.ErrValidation)
    })
    s.Fork(t, eiptest.Hegota, func(f *eiptest.ForkProbe) { // ③ activation gating
        f.Before().ExpectTxReject(...); f.AtBoundary().ExpectHeaderField(...)
    })
    s.Encoding(t, eiptest.RoundTrip(&types.Tx8200{}), eiptest.Vectors("testdata/eip8200")) // ④
    s.RPC(t, func(r *eiptest.RPCProbe) {       // ⑤ new fields/methods exposed correctly
        r.Call("eth_getBlockByNumber", "latest").ExpectField("myNewField", ...)
    })
    s.Engine(t, func(e *eiptest.EngineProbe) { // ⑥ payload carries the new feature
        e.BuildPayloadWith(txs).ExpectPayloadField(...)
    })
    s.Diff(t, blocks)                          // ⑦ optional: native vs geth parity
}
```

### 4.4 Speed

- In-process, in-memory, no Docker/Kurtosis; crypto backends injectable with fast
  fakes where needed (pattern: `pkg/e2e_advanced_helpers.go:55` `AlwaysValidBLS`).
- Per-EIP test < 1s; full framework suite target < 2 min; go test caching applies on
  repeat runs.
- Default-suite integration (no tag) keeps it from rotting (lesson from `53362ff`).

## 5. Detailed Task Plan

Effort: S ≤ 0.5 dev-day, M ≈ 1 dev-day, L ≈ 2–3 dev-days.
Phases P0→P4 are sequential; T-tasks inside a phase are parallelizable unless noted.

### Phase 0 — Prerequisites (blocking, small)

| ID | Task | Files / anchors | Acceptance criteria | Effort |
|---|---|---|---|---|
| T0.1 | **Scheduled-fork chain config builder.** Current `TestConfig*` pins all fork times at 0. Provide a builder producing a `*config.ChainConfig` with real activation timestamps (Amsterdam → Hegotá → BPO2) plus matching genesis timestamp. | new `pkg/eiptest/option.go`; extends `core/config/chain_config.go` API if needed (additive only) | `Rules(timestamp)` flips at chosen boundaries; config JSON roundtrip survives (genesis parse path exercised) | M |
| T0.2 | **State-continuity strategy.** Verify multi-block state reuse: after `InsertBlock`, next block builds from `bc.State()` — eliminating the manual state rebuild hack at `pkg/core/e2e_eip_spec_test.go:443-448`. Document the chosen strategy in `sim.go`. | `pkg/eiptest/sim.go` | 5-block chain with contract storage carried across blocks, no manual state plumbing | M |

### Phase 1 — Framework core

| ID | Task | Files / anchors | Acceptance criteria | Effort |
|---|---|---|---|---|
| T1.1 | **Sim core.** `New(t, opts...)`, `Commit(txs...)` (BuildBlock → InsertBlock), `Call()`, `AdvanceTime/Slot`, `State()`, `BlockResult{Receipts, Logs, BAL, Requests}`. Failure mode: on unexpected insert rejection, dump block + validation error. | `pkg/eiptest/sim.go`, `option.go` | Demo test: genesis → 3 blocks (transfer, deploy, call) → assertions; < 1s; no docker | L |
| T1.2 | **KeyRing + tx DSL.** Deterministic prefunded keys; builders for legacy, 2930, 1559, 4844 blob (sidecar), 7702 setCode, local 0x08, multidim 0x09, frame 0x06, AA 0x05, EOF 0xF; signed and `SetSender` fast paths. Retires the 5 duplicated helper sets for new tests. | `pkg/eiptest/keyring.go`, `tx.go` | Roundtrip + signer-recovery unit tests per tx type | M |
| T1.3 | **Assert library.** Status/gas/logs/balance/code/storage/BAL/requests expectations with readable diffs; automatic dump of receipts + BAL + state delta on failure. | `pkg/eiptest/assert.go` | Helper unit tests + one intentional-failure golden output test | M |
| T1.4 | **Registry + coverage gate.** `registry.go` maps EIP → {title, spec path, test file, surfaces, tier A/B/C/D}; meta-test parses the implementation list (IFLOW.md "EIP Implementation Status") and fails the default suite if an EIP lacks a test file (D-tier exempt; N/A allowed with recorded justification). | `pkg/eiptest/registry.go`, `registry_test.go` | Gate fails on a synthetic missing EIP; passes for the seeded current set | M |
| T1.5 | **CI integration.** No build tag; runs inside default `go test ./...`. Record suite wall time in README. | none (default inclusion) | `go test ./eiptest/...` green within default CI budget; time recorded | S |

**Phase 1 exit:** demo EIP test (e.g. a repricing EIP) written only with framework API,
green, < 1s.

### Phase 2 — Surface probes + scaffold

| ID | Task | Files / anchors | Acceptance criteria | Effort |
|---|---|---|---|---|
| T2.1 | **STF probe.** Deploy/Tx/Call with expectation chaining; per-tx and per-block views; BAL + requests surfaced in results. | `stf.go` | Example test: opcode behavior + gas bounds + log shape | M |
| T2.2 | **Fork probe.** `Before/AtBoundary/After` around a scheduled fork; header-field gating assertions; genesis/config JSON roundtrip. Fixes the "no chain crosses a fork boundary" gap. | `fork.go` | Example: Glamsterdam→Hegotá crossing asserting a gated header field | M |
| T2.3 | **Encoding probe.** RLP/SSZ roundtrip helpers + JSON vector loader (`eiptest.Vectors(dir)`), reusing `core/eftest` state-fixture consumption for opcode/precompile vectors where applicable. | `encoding.go` | Example: table-driven vector test for one tx type | M |
| T2.4 | **RPC probe.** httptest JSON-RPC server over the Sim chain; field-level golden assertions for tx/block/receipt responses. Pattern anchor: `pkg/e2e_wave10_test.go:388`. | `rpc.go` | Example: assert a typed-tx response field set | M |
| T2.5 | **Engine probe.** FCU → GetPayload → ProcessBlock loop with **tx injection into payload building** (current engine E2E builds empty payloads); payload-attributes builder incl. IL transactions field. Anchor: `pkg/engine/eip_spec_e2e_test.go:22-75`. | `engine.go` | Example: user tx carried through full Engine flow and validated | L |
| T2.6 | **Pool probe.** Real `txpool.New` wired to Sim chain events (insert/finalize/reorg); probes for admission, replacement, eviction, nonce ordering. | `pool.go` | Example: admission-rule + nonce-ordering test | L |
| T2.7 | **Diff probe.** Identical block sequences through native `StateProcessor` and `pkg/geth` processor; diff state root/receipts; opt-in per test. | `diff.go`; uses `pkg/geth/processor.go` | Example: plain transfer sequence parity | M |
| T2.8 | **Scaffold generator.** `tools/eiptest-scaffold -eip N -name X -fork F` emits `eipN_test.go` skeleton (per-surface TODO sections) + registry entry (N/A flags allowed). | `tools/eiptest-scaffold/main.go` | Generated file compiles; registry gate recognizes it | S |
| T2.9 | **Framework README.** API reference + "test a new EIP in 10 minutes" walkthrough. | `pkg/eiptest/README.md` | Walkthrough steps execute as written | S |

**Phase 2 exit:** one example test per probe, all green in default suite, total < 2 min.

### Phase 3 — Validation on a real feature (the real acceptance test)

| ID | Task | Acceptance criteria | Effort |
|---|---|---|---|
| T3.1 | Pick the next EIP feature to implement. Author its integration test via scaffold **before** implementation (red), then implement (green). | Feature test file contains zero setup code; single-EIP iteration < 1s | — |
| T3.2 | Ergonomics feedback: adjust probe APIs; record learnings in README. | API changes committed; walkthrough updated | M |

### Phase 4 — Tooling pipelines

| ID | Task | Files / anchors | Acceptance criteria | Effort |
|---|---|---|---|---|
| T4.1 | **solc contract pipeline.** `pkg/eiptest/contracts/*.sol` + `make contracts` (solc → hex → `contracts_gen.go`, `go:embed`). Dockerfile stays solc-free. Scenario contracts: storage-heavy, selfdestruct, delegate-caller, AA entrypoint, rollup anchor, frame APPROVE. | `pkg/eiptest/contracts/`, root `Makefile` or `pkg/Makefile` target | `eiptest.Contracts.X()` returns loadable runtime code for each scenario contract | M |
| T4.2 | **EEST vector pipeline.** `make eest`: run `execution-spec-tests` `fill` (uv + geth `evm` binary from `refs/go-ethereum/cmd/evm`) for target forks; output consumed by `eiptest.Vectors` / geth-backed runner. | `Makefile` target; `testdata/` layout | ≥ 1 generated Amsterdam fixture suite passes through the framework | L |
| T4.3 | **Docs refresh** after first real feature use (T3.1): ergonomics notes, pitfalls. | README | — | S |

### Phase 5 — Adoption & consolidation (optional, incremental)

| ID | Task | Notes | Effort |
|---|---|---|---|
| T5.1 | Migrate unique scenarios from `pkg/`-root E2E files into `eiptest` as features are touched. Opportunistic only; no big-bang rewrite. | 137 test funcs exist today | — |
| T5.2 | Devnet promotion: scaffold option `--devnet` also emits a feature config + `verify-*.sh` stub for network-level surfaces. | Bridges to `pkg/devnet/kurtosis/` conventions | M |
| T5.3 | Decide fate of the orphaned `integration` tag suite: either run it in CI (`test-variants` matrix) or fold its unique cases into `eiptest` and delete. | 65 tagged tests currently unrun | M |

## 6. Dependency graph

```
T0.1 ─┐
T0.2 ─┴─→ T1.1 ─→ T1.2 ─→ T1.3 ─→ T1.4 ─→ T1.5
                     │
                     └─→ T2.1 … T2.7 (parallel) ─→ T2.8 ─→ T2.9
                                    │
                                    └─→ T3.1 → T3.2 → T4.1/T4.2 → T5.*
```

## 7. Risks & Open Questions

| # | Risk / question | Mitigation |
|---|---|---|
| R1 | Fork-scheduled config may expose latent assumptions (code assuming fork time = 0). | T0.1 is deliberately first and small; additive config API only. |
| R2 | Crypto-heavy paths slow the suite (historical PR #22 "go-test-cost-too-much"). | Inject fast fakes for BLS/KZG where the probe's assertions don't depend on them; keep real-crypto cases behind explicit subtests. |
| R3 | Engine probe tx-injection may require touching builder internals. | Confine changes to test-visible options; if builder refactor is needed, scope it as a separate pre-task. |
| R4 | Registry gate strictness (new EIPs land before tests do). | Gate reads the implementation list at merge time; N/A flags + tier D exemption; relax to warning for one release if it blocks hotfixes. |
| R5 | AA/EOF/frame tx formats still evolving (EIP-7701/8141 adjacent work). | tx DSL builders live in one file (`tx.go`); format changes are localized. |
| Q1 | Should `eiptest` tests live in the same package (white-box) or `eiptest_test` (black-box)? Default: black-box `package eiptest_test` for user files; probes exported. | Decide at T1.1. |
| Q2 | Does the RPC probe need WebSocket/subscription coverage at P2, or defer? | Default: defer; HTTP JSON-RPC first. |

## 8. Definition of Done (framework v1)

1. All Phase 0–2 tasks meet their acceptance criteria.
2. One real EIP has been developed test-first through the framework (T3.1) with zero
   setup code in its test file.
3. `go test ./eiptest/...` green in the default suite, total < 2 min, no build tags.
4. Registry gate active: implementation list and test coverage cannot drift silently.
5. README walkthrough reproduces end-to-end.

## Appendix A — Measured baselines (2026-11-11, 24-core / 62 GB host)

| Command | Time |
|---|---|
| `go build ./...` (incremental) | 10.5s |
| `go vet ./...` | 5.7s |
| `go test -short -count=1 ./...` (206 pkgs, all pass) | 37.3s |
| `kurtosis engine start` | 5s |
| devnet `single-client` boot → block 10 (cached images) | ~50s |
| slowest unit packages (`-short`) | `core/eftest` 29s, `consensus` 15s |

## Appendix B — Git fix-commit distribution by subsystem (914 commits, 288 fix-type)

```
engine 81 | rpc 54 | devnet 36 | txpool 31 | consensus 25 | p2p 24 | chain 21 |
das 16 | crypto 16 | state 15 | sync 13 | evm 12 | eip-8141 7 | focil 4 | aa 5 |
epbs 2 | eip7702 2 | eip7706 2 | eip7805 2 | genesis 2 | blob 2 | kzg 1 | eip7928 1
```

Roughly half of all fix commits fall on in-process-testable surfaces (§2.2) — the same
surfaces this framework makes first-class for future features.
