# Track 3 remaining-work acceptance tests

These are acceptance requirements for work that is still open in `CHECK.md`.
They are not claims that the tests or features already exist. Unit mocks may
exercise failure branches, but the integration gates must observe the native
node, its real database, the protocol messages, and the post-restart consumer.

## Common fixture and oracle

Use deterministic chain fixtures with independent reference execution and
record the genesis, block hashes, state roots, balances, nonces, bytecodes,
storage values, receipts, and both existing and absent account/storage proofs.
Include accounts spread over all range partitions, multiple pages within a
partition, a large contract spanning multiple storage pages, shared bytecode,
and account creation, deletion, recreation, storage deletion, and code change
between pivot states. Generate the reference roots and proofs with Geth, not
with the implementation under test. Keep a machine-readable transcript of
requested roots, origins, limits, returned keys, proof results, peer identity,
and durable checkpoint transitions. Reuse means fewer network requests or
bytes for the relevant data; a successful final root alone does not prove it.

Run both uninterrupted and interrupted executions against the same fixture.
For every successful case compare the adopted pivot, state root, account and
storage values, code, independent proofs, post-pivot block execution, receipts,
and another restart/readback with the reference. Confirm `eth_syncing` remains
active until the verified target is applied. For failures assert that neither
the canonical head nor finalized checkpoint nor snapshot-completion marker is
advanced, and that a later valid attempt can make progress.

## Durable partial resume

* Terminate a separate node process after a verified account page, after the
  page data is durable but before its cursor, after the cursor commit, during
  a storage page, and between the final data batch and completion marker.
  Reopen the same on-disk database each time. Do not use graceful shutdown as a
  substitute for process termination. The cursor must never skip data that was
  not durable; an uncommitted page may be downloaded again.
* Resume from the same pivot with a peer that records requests. Prove that
  committed account and storage intervals are not downloaded again, that the
  first unfinished interval starts at the correct exclusive successor, and
  that bytecode reused from disk matches its hash. A test that merely observes
  eventual completion after a full redownload fails this gate.
* Corrupt, truncate, delete, or swap checkpoint metadata, staged leaves, proof
  records, storage roots, bytecode, and pivot identity independently. The node
  must discard/revalidate affected progress or fail safely, never skip required
  ranges or publish an unverified root. Inject database write, flush, and reopen
  failures and run the same checks after WAL recovery.
* Disconnect one peer after a committed page and resume with another; vary peer
  order and include a peer returning an invalid proof, delayed response, or an
  obsolete root. Assert correct proof binding and no cursor advance for the bad
  response. Exercise cancellation and shutdown while a request is in flight.

## Moving pivot and forkchoice

* Build pivot A and a descendant finalized pivot B with both unchanged and
  changed accounts, deleted/recreated accounts, changed storage roots, and
  unchanged/changed code. Expire A at all peers after a partial A download,
  then nominate B through the authenticated Engine API. The test must finish
  at B without restarting all data from zero, report actual reused and
  revalidated ranges, and produce B's independently computed state and proofs.
* Feed old-root proofs, proofs from a conflicting branch, gaps, duplicates,
  omitted deletions, and a changed leaf presented as unchanged. Verify no
  affected data is accepted under B without proof or equivalent authenticated
  trie reconciliation. Test stale responses arriving after a pivot update.
* Interrupt again while migrating A progress to B and restart with B, then with
  a newer descendant C; also test a conflicting finalized target and a CL
  target replacement during publication. Assert that only the current
  authorized, ancestry-validated target can become canonical. A completed A
  snapshot being adopted after finality advances is useful but does not satisfy
  partial-range migration.

## Resource and scalability gate

Use a real disk-backed production database and generate fixtures outside the
measured process. Measure peak RSS, Go heap, disk space, file descriptors,
request/response bytes, and elapsed time separately for download, healing,
adoption, execution, serving, and restart. Include code and both sparse and
large single-account storage; do not omit snapshot serving or restart.

Run at least three geometrically increasing state sizes with fixed peer count,
page size, cache budget, and transaction workload. Record the fixture bytes,
entry counts, binary revision, environment and exact commands. Establish an
explicit memory budget before claiming production readiness; require the peak
increment from the largest size to remain within that budget and demonstrate
that a 4x input increase does not yield a comparable 4x resident-memory
increase once fixed overhead is amortized. Repeat on disk after reopen and with
interruption/recovery. The current `FileDB` in-memory index, downloaded maps,
sorted keys, execution snapshots, and serving cache are known reasons this
gate cannot presently be claimed as passed. Include disk exhaustion and peer
churn; do not interpret an OOM, timeout, or skipped run as a pass.

## Independent clients and mutation checks

Run a disposable devnet with an independently built Geth snap/eth source, a
Lighthouse consensus client driving the ETH2030 Engine API, and an ETH2030
receiver started with an empty execution database. Confirm capability
negotiation and real protocol messages, advance finality during sync, then
compare selected blocks, roots, receipts, state and independently verified RPC
proofs before and after restart and post-pivot transactions. Record exact
client versions, configs, logs, and the chain fixture. A Geth library codec test
or a manually submitted FCU is not this gate. Never disturb an existing
user-owned enclave or database to perform this test.

Demonstrate test sensitivity using temporary local mutations: advance a cursor
before data commit, accept a proof for the previous root, skip an account
deletion, retain all downloaded leaves in memory, and publish a completion
marker early. Each mutation must be detected by its corresponding test; revert
the mutations before running the final suite. Record which tests actually ran,
their results, and any unavailable infrastructure or untested scale.
