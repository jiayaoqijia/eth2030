# EIP integration tests

`eiptest` runs the native execution client in-process. Its simulator builds and
validates blocks using the production builder and chain. The RPC and Engine
probes use the node's HTTP handlers. The differential probe replays blocks
through native execution and Geth.

See the [framework plan](../../docs/plans/eiptest-framework-plan.md) for acceptance
criteria and [project status](../../STATUS.md) for verification results.

## Test a new EIP

Run these commands from the repository root. The preview uses an example ID.
Supply the actual EIP number, name, fork and surfaces when generating files.

```bash
go -C pkg run ./tools/eiptest-scaffold -eip 8200 -name "My feature" -fork osaka -surfaces stf,fork -dry-run
go -C pkg run ./tools/eiptest-scaffold -eip 8200 -name "My feature" -fork osaka -surfaces stf,fork
```

The generator creates `pkg/eiptest/eip8200_test.go` and inserts its registry
entry. Replace the TODOs with observable assertions. Start by running the
new test and checking it fails on the behavior the implementation is meant
to change. Run it again after implementing the feature.

```bash
go -C pkg test ./eiptest -count=1 -run '^TestEIP8200($|_)' -v
go -C pkg test ./eiptest/...
```

Add the completed EIP to the implementation status in `CLAUDE.md`, which
`IFLOW.md` points to. The default suite checks the status list, registry, and
test source. The gate catches missing entries, missing files, build-tagged
files, and assertion-free scaffolds. Declare only surfaces exercised by the
test. Use `-tier D -exempt "Covered by ..."` for a documented network exemption.

[EIP-7934](eip7934_test.go) is the test-first example. The test checks the
Osaka boundary and requires the execution block size rejection to come
from block validation. The first run reached the unrelated intrinsic-gas
check. The implementation made the size assertion pass.

## Common operations

```go
s := eiptest.New(t,
    eiptest.WithGenesisTime(100),
    eiptest.ForkAt(eiptest.ForkGlamsterdan, 18),
)
fork := s.Fork(eiptest.ForkGlamsterdan)
fork.Before().ExpectInactive(t)
at := fork.AtBoundary()
at.ExpectActive(t)
if at.Time() != 118 || at.Header().CalldataGasUsed == nil {
    t.Fatal("calldata header fields were not activated at the boundary")
}
s.STF().Transfer(s.Alice(), s.Bob().Address, eiptest.Wei(5))
```

| Probe | Use |
|---|---|
| Sim | `Commit(txs...)` builds with explicit candidates. `Commit()` builds an empty block. Use `CommitFromPool()` to select from the pool. `TryBlock` mutates a candidate for a negative validation check. |
| STF | `s.STF().Deploy`, `Tx`, and `Transfer` require success. Use `Execute(...).ExpectStatus(t, status).ExpectGas(t, min, max)` to assert reverts. `CallAtBlock` selects historical state and fork context. |
| Results | `BlockResult` carries validated receipts, block logs, BAL and execution requests. Transaction assertion failures include receipt and state diagnostics. |
| Fork | Pass candidate transactions to `Before(txs...)` or `AtBoundary(txs...)` to execute at the exact phase. The returned view retains its block and result. |
| Pool | `s.Pool()` uses the real pool. Use `ExpectAccepted`, `ExpectRejected`, `ExpectPresent`, and `CommitPending`. `s.Finalize` and `s.Reorg` exercise finality and pool state updates. |
| Encoding | `RLPRoundTrip` checks the decoded transaction and recovers signed senders. `SSZRoundTrip` decodes and re-encodes the supported transaction formats. `LoadVectors(t, "eip155")` reads fixture JSON under `testdata`. |
| RPC | `s.RPC().Call` returns raw JSON for `ExpectField`. `CallRaw` exposes errors for negative cases. |
| Engine | `s.Engine().Submit` admits real pool transactions. `BuildAndProcess` drives HTTP FCU, getPayload, and newPayload using independent proposer and verifier state. `FCUWith` accepts custom attributes. |
| Diff | Capture the complete pre-state with `SnapshotAccounts` before committing. `DiffBlocks` replays a contiguous sequence and compares state roots and receipt contents after each block. |

Six deterministic accounts are funded with 1,000 ETH at genesis. Additional
accounts returned by `s.Account(i)` start unfunded. The legacy, access-list,
dynamic-fee, blob, set-code, and multidimensional builders support ECDSA
signing and sender-cache variants. AA and Frame use their protocol-specific
authorization mechanisms. Local transactions are sender-cached, and EOF
creation uses an ordinary create envelope with EOF initcode.

The EIP-6404 probe round-trips all ten normalized transaction variants.
EIP-155 chain IDs and signature parity are stored separately and converted back
to the original RLP `v`. The EIP-7807 test builds a real Amsterdam block and
projects its receipts, requests and access list into the published SSZ payload
schema. Its header summary and payload produce the same SSZ root. The
[execution type documentation](../core/types/README.md) describes the draft
revisions and the available codec entry points.

## Scenario contracts and fixtures

```bash
make -C pkg contracts
make -C pkg eest
```

The contract target compiles Solidity and the Frame `APPROVE` Yul scenario,
then regenerates the embedded runtime accessors. The Go build reads the
committed runtimes. Examples cover storage, self-destruct, echo, delegate
calls, the AA entry point, rollup anchors, and frame approval.

The EEST target uses Python 3.11 and `uv` with the pinned
`refs/execution-specs` workspace. It fills an Amsterdam state test in a
fresh directory, runs it through the Go Geth-backed runner, and copies the
verified output to `pkg/eiptest/testdata/eest-generated`. The default Go suite
also runs the committed fixture. The smoke scenario uses a zero base fee
and zero gas price to compare storage behavior shared by the two pinned
Amsterdam revisions. The EELS transition tool is the default filler for
Amsterdam. Use `EVM_BIN` for another fork-compatible transition tool, or
`make -C pkg bin/evm` to build the pinned Geth binary for forks it supports.

## Optional devnet scenarios

Use `-devnet` to generate the feature config and executable verifier in
`pkg/devnet/kurtosis`. Implement the feature-specific verification, then run:

```bash
cd pkg/devnet/kurtosis
./scripts/run-feature-tests.sh eip8200
```

The generated verifier exits unsuccessfully until it has a feature assertion.
The legacy tagged integration suite remains in CI.
