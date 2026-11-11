# eiptest — EIP Integration Test Framework

In-process integration testing for ETH2030. `eiptest` wires the real
production modules (blockchain, block builder, state processor, txpool,
engine backend, JSON-RPC server, geth differential adapter) behind a small
probe API, so an EIP's integration test contains **only feature-specific
assertions** — no genesis/signing/builder/server setup code.

Design and task plan: [docs/plans/eiptest-framework-plan.md](../docs/plans/eiptest-framework-plan.md).

## Quick start: test a new EIP

```bash
# 1. Generate the skeleton (test file + registry entry guidance):
go run ./tools/eiptest-scaffold -eip 8200 -name "My Feature" -fork glamsterdan

# 2. Add the Registry entry in registry.go (the scaffold prints the line).

# 3. Fill in the surface sections and run:
cd pkg && go test ./eiptest/ -run TestEIP8200 -v

# Optionally also emit a devnet feature config + verify script stub:
go run ./tools/eiptest-scaffold -eip 8200 -devnet
```

## The Sim

```go
s := eiptest.New(t,
    eiptest.ForkAt(eiptest.ForkGlamsterdan, 100), // scheduled fork (seconds from genesis)
    eiptest.Fund(someAddr, 100*eiptest.Ether),
)
r := s.Commit(tx1, tx2)      // build → insert (BAL hash / header validation enforced)
eiptest.ExpectSuccess(t, r, 0)
eiptest.ExpectStorage(t, s, contract, slot, value)
s.AdvanceTime(200)           // cross fork boundaries with real timestamps
```

`Commit` drives `block.NewBlockBuilder.BuildBlock` → `chain.Blockchain.InsertBlock`
exactly as production does; blocks failing insert-time validation fail the test
with the validator's error. State continuity across blocks is handled by the
chain reader (no manual state plumbing).

Six standard accounts (`s.Alice()` … `s.Faythe()`) are deterministic and
genesis-funded with 1000 ETH each; `s.Account(i)` for i ≥ 6 mints additional
scratch accounts (not funded).

## Surface probes

| Probe | Entry | Covers |
|---|---|---|
| STF | `s.STF()` | deploy / tx / read-only call against production `StateProcessor` |
| Pool | `s.Pool()` | real `txpool.TxPool`: admission, replacement, eviction, nonce-gap invariant |
| Fork | `s.Fork(fork)` | `Before/AtBoundary/After` around real fork-timestamp activation |
| Encoding | `s.Encoding()` | RLP roundtrip, SSZ determinism, JSON vector loading (`eiptest.LoadVectors`) |
| RPC | `s.RPC()` | production `node/backend.RPCBackend` over httptest; field-level assertions |
| Engine | `s.Engine()` | `forkchoiceUpdated → getPayload → newPayload` with **tx injection** (real pool wired) |
| Diff | `s.Diff()` | native vs `pkg/geth` dual-backend execution (receipts parity; see boundary note) |

Example per probe: `eip7702_test.go` (STF+pool+fork+encoding), `eip7805_test.go`
(engine), `eip7825_test.go` (pool), `eip7691_test.go` (fork), `rpc_probe_test.go`,
`diff_probe_test.go`.

## Scenario contracts (solc pipeline)

```bash
make contracts   # solc → eiptest/contracts/*.bin-runtime → gen.go (requires solc)
```

`eiptest/contracts/*.sol` hold scenario contracts (Storage42, SelfDestruct,
Echo). Committed `gen.go` keeps normal builds solc-free. Tests use
`s.Contracts().Storage42()` or `contracts.Runtime(name)` plus
`contracts.ABISelector("set(uint256)")` for calldata.

## Spec vectors (EEST pipeline)

```bash
make eest        # execution-spec-tests fill → eiptest/testdata/eest/ (requires uv + bin/evm)
```

Build the transition tool once: `cd ../refs/go-ethereum && go build -o ../../eth2030/pkg/bin/evm ./cmd/evm`.

**Known blocker (2026-11):** the pinned `refs/execution-spec-tests` checkout is
mid-migration to the "weld"/EELS framework; pytest cannot collect tests there
(rc=4) and `fill` produces no fixtures. The consumption side is verified
end-to-end by `eest_test.go`, which runs a real GeneralStateTests fixture (same
state_test JSON format EEST emits) through `eftest.RunSingleFixture`. When the
EEST pin advances, `make eest` output flows through the same path.

## Coverage gate

`registry_test.go` enforces two invariants in the default suite:

1. every EIP in IFLOW.md's implementation list has a `Registry` entry;
2. every non-exempt entry points to an existing test file (tier D
   network-only and tier E CL-side entries carry documented exemptions).

A new EIP landing without a test file fails `go test ./eiptest/`.

## Performance

Measured on the 24-core dev host (2026-11-11):

| Command | Time |
|---|---|
| `go test ./eiptest/` (full framework suite, ~58 tests) | **0.75s** |
| single EIP test (`-run TestEIP7702`) | < 1s |
| `make contracts` (solc + regenerate) | ~2s |

The suite runs inside the default `go test ./...` (no build tag).

## Known findings & boundaries (recorded by the framework itself)

- **Contract-creation gas divergence** (diff surface): native vs geth differ
  ~360 gas on creation (see `diff_probe_test.go` TestDiffProbe_ContractParity).
  Transfer path is at exact parity.
- **Post-Glamsterdan calldata cliff** (builder): calldata-carrying txs are
  dropped state-dependently between 1 KB and 2 KB payloads (see
  `eip7706_test.go` notes).
- **EIP-8141 frame execution reverts** across all probed variants while the
  envelope/pool/nonce surfaces work (`eip8141_test.go` documents this; the
  devnet `test-frametx.sh` flow historically succeeded — root-cause pending).
- **geth differential adapter** does not apply post-block system contracts
  (EIP-4788/2935), so Diff compares receipts (tx semantics), not full-block
  state roots.

## Relationship to other test layers

- EF state tests (`pkg/core/eftest`): `eiptest` consumes, never replaces.
- Tagged `integration` suite (`pkg/core/*_test.go`): already wired into CI's
  `test-variants` matrix and green locally; superseded by `eiptest` for
  *future* feature work — new EIP tests go here, legacy suites stay for their
  existing coverage.
- Devnet (`pkg/devnet/kurtosis`): multi-node surfaces only (tier D in the
  registry). The scaffold's `--devnet` flag bridges a feature to that layer.
