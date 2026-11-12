# Track 3: Node Sync and Native Consensus

Implemented on `feat/track3-node`, based on `master` at `9fde8aa` (PR #50).

## External consensus mode

The default native ETH2030 node continues to obtain forkchoice authority from
its authenticated Engine API. An unknown `engine_forkchoiceUpdated` head queues
an `ExecutionSyncer` target and returns `SYNCING`. The worker requests headers
and bodies over connected `eth` peers, correlates responses by peer and request
ID, checks the requested header hash and ancestry, and executes downloaded
blocks through the normal validator. Downloaded descendants are staged until
the complete head, safe and finalized target can be published. `eth_syncing`
reports the pending task instead of always returning `false`.

`--syncmode full` replays the missing chain. `--syncmode snap` bootstraps a
genesis-only execution database at the finalized hash supplied by the CL. The
peer's advertised head cannot nominate a pivot. A pivot includes 256 parent
headers (or the available history back to genesis) for header validation and
`BLOCKHASH` execution.

Snap account and storage ranges are Merkle-verified against the requested
roots; slim accounts are expanded before verification, code is checked against
its hash, and the complete downloaded account/storage roots are reconstructed
before publication. The persistent state retains hashed account and storage
keys, so unknown address/slot preimages are not required. Execution reads them
by hashing the address or slot and applies a journalled native-state overlay.
Within one download attempt, requests can fail over to another connected snap
peer without discarding completed ranges. Storage roots are shared between
these peers only after an account-range proof authenticates them against the
same pivot. Partial range progress is still discarded on restart. A completely
downloaded snapshot that was persisted before pivot adoption can be revalidated
and reused after restart, including offline when the required ancestry and trie
nodes are present locally.
The authenticated trie healer follows parent hash references, includes storage
tries, and rejects unrequested or mismatched node bytes. Exhausted downloads
and healing tasks fail rather than reporting completion.

The node joins its sync worker on shutdown. A committed pivot and subsequent
canonical blocks survive restart; the latter are replayed through ordinary
execution validation. Full-mode restart also replays the saved canonical head.
Interrupted, unpublished snapshots are never adopted. A finalized snapshot
download is allowed to finish while the CL head advances. If finality also
advances, the worker verifies a hash-linked, height-contiguous path from the
current finalized block to the downloaded pivot and rechecks the target before
adopting it. A conflicting or unavailable path fails closed. The worker then
catches up to the latest target.

## Explicit native consensus observer

`eth2030 --native-consensus /path/to/native.json` enables the research 3SF/PQ
observer. The external CL always retains forkchoice authority. The file contains only timing and validator public keys:

```json
{
  "slot_millis": 6000,
  "slots_per_epoch": 32,
  "validators": {
    "0": "<base64 ML-DSA-65 public key>",
    "1": "<base64 ML-DSA-65 public key>",
    "2": "<base64 ML-DSA-65 public key>"
  }
}
```

Every participating node must use the same genesis, timing and registry.
The runtime uses `QuickSlotScheduler`, real ML-DSA-65 verification through
`PQAttestationVerifier`, and 3SF source/target justification rules. A quorum
justifies a target; it finalizes the source only when the target is the next
eligible slot after that source. Quorums are counted per source-target link. Validator identity, chain/timing/registry domain and both
checkpoints are signed. Unregistered keys, identity substitutions, equivocation,
future votes and invalid links are rejected; normal duplicates/stale deliveries
are ignored without disconnecting the peer.

Signed votes enter through `Node.BroadcastNativeVote` and the opt-in `ncs/2`
research protocol. `Node.NativeConsensusDomain` returns the configuration-bound
signing domain. `Node.NativeConsensusStatus` reports observed slot, signature
and research finality state. These observations **do not change execution
forkchoice**. The node starts and joins the observer, follows authoritative CL
reorganizations/finalized advances, and seeds it from the persisted CL checkpoint
on restart. Observer votes are not a persistent standalone consensus state.
Validator signing keys, proposal/signing duties, full CL fork choice and native
consensus authority are outside this integration. The earlier authority switch
was removed during review because it exceeded the Track 3 wiring design.

## Peer transport

The native node now uses standard RLPx authentication, canonical RLP Hello,
Snappy, devp2p's reserved message range, and only jointly negotiated capabilities
(highest shared version per name). Its identity is retained in `nodekey` with
0600 permissions. `eth/68` uses its correct message range and canonical typed
transaction/receipt envelopes; timestamp forks are included in ForkID.
Old native plaintext peers require upgrading. Plaintext framing is retained only
for explicit low-level test/server configurations.

## Verification and boundaries

`TestNodeCLDrivenSyncAndSnapRestart` starts two real nodes, submits an
authenticated Engine HTTP request before a peer is available, checks the
`eth_syncing` transition, negotiates `eth` and `snap` over encrypted RLPx/TCP, verifies stored
code/storage and post-pivot withdrawal execution, and restarts both full and
snap receivers. `TestNodeNativeConsensusWireLifecycle` exchanges real signed
PQ votes over RLPx, verifies observer finality without changing execution
finality, then verifies CL selection, shutdown and checkpoint recovery. Separate tests cover
range gaps, wrong roots, false empty replies, cancellation, target replacement,
incomplete snapshots and authenticated account/storage trie healing.

This implementation uses **snap/1 with a fixed finalized pivot per download**. Snapshot
assembly and serving currently materialize state in memory; moving-pivot
range reuse, mainnet-scale resource qualification, and snap/2 BAL-based sync
are not implemented. The native FileDB also keeps its full key/value index in
memory. Snapshot validation and node generation use a streaming trie with a
sorted hash list, and nodes and snapshot records are written in bounded batches;
the downloaded snapshot itself and its sorted hash list still scale with state
size. A completed snapshot can survive a finalized advance, but this does not
retarget an in-progress range download. If a peer ages out the pivot, the
attempt fails and the worker retries the current CL target. This is a functional bootstrap path,
not completed production/mainnet snap synchronization. The research observer
is not an autonomous CL or a mainnet consensus-fork activation.

Sync testing exposed two pre-existing codec defects: generic header/body
serialization lost optional-field and typed-transaction semantics, and header
RLP omitted the EIP-7928 BAL commitment. Both are corrected. **Amsterdam block
hashes now commit to the BAL.** Old development-chain data produced without
that commitment requires a fresh chain or resynchronization; it is not silently
rewritten. Pre-Amsterdam header encoding remains unchanged.

## Specification sources

- `refs/EIPs/EIPS/eip-3675.md`: CL authority, validated ancestry, atomic forkchoice.
- `refs/execution-apis/src/engine/paris.md`: SYNCING, payload validation and forkchoice.
- `refs/EIPs/EIPS/eip-7928.md`: mandatory BAL header commitment.
- [Pinned snap/1 protocol](https://github.com/ethereum/devp2p/blob/e0091d1140bddce2a9805047b94110d3ad439e6e/caps/snap.md): ranges, proofs, slim accounts, bytecodes and trie-node paths.
- `refs/EIPs/EIPS/eip-7782.md`: configurable slot timing; no fork schedule is activated here.
- `refs/research/3sf-mini/consensus.py`: justification eligibility and source finalization; the implementation requires at least two thirds of registered validators.

## Adversarial review

The review of `fc1f70f` and the corrective evidence are recorded in
[Track 3 review](track3-review.md). FCU validation/publication is synchronous for
known heads; candidate payloads are staged; validated-block markers distinguish
execution acceptance from stored data. Recovery preserves the saved target on
failure. FileDB commits WAL transactions before materializing files and fences
failed writes until reopen. These properties are covered by failure-injection,
branch-execution and mutation tests, in addition to normal end-to-end tests.
