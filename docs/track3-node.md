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
The authenticated trie healer follows parent hash references, includes storage
tries, and rejects unrequested or mismatched node bytes. Exhausted downloads
and healing tasks fail rather than reporting completion.

The node joins its sync worker on shutdown. A committed pivot and subsequent
canonical blocks survive restart; the latter are replayed through ordinary
execution validation. Full-mode restart also replays the saved canonical head.
Interrupted, unpublished snapshots are never adopted. A finalized snapshot
download is allowed to finish while the CL head advances, then the worker
catches up to the latest target.

## Explicit native consensus mode

`eth2030 --native-consensus /path/to/native.json` enables the research 3SF/PQ
lane. The file contains only timing and validator public keys:

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
eligible slot after that source. Validator identity, chain domain and both
checkpoints are signed. Unregistered keys, identity substitutions, duplicate
votes, stale/future votes and invalid links are rejected.

Signed votes enter through `Node.BroadcastNativeVote` and the opt-in `ncs/1`
peer protocol. `Node.NativeConsensusStatus` reports actual slot, signature and
finality state. Finality updates reach the execution chain. The node starts and
joins the runtime with its lifecycle, and seeds it from the persisted finalized
checkpoint on restart. External Engine forkchoice updates are rejected in this
mode so they cannot compete with native finality. Validator signing keys and
automatic proposal/signing duties remain outside this node runtime.

## Verification and boundaries

`TestNodeCLDrivenSyncAndSnapRestart` starts two real nodes, submits an
authenticated Engine HTTP request before a peer is available, checks the
`eth_syncing` transition, negotiates `eth` and `snap` over TCP, verifies stored
code/storage and post-pivot withdrawal execution, and restarts both full and
snap receivers. `TestNodeNativeConsensusWireLifecycle` exchanges real signed
PQ votes over TCP, verifies execution finalization, rejects external
forkchoice, and checks shutdown and checkpoint recovery. Separate tests cover
range gaps, wrong roots, false empty replies, cancellation, target replacement,
incomplete snapshots and authenticated account/storage trie healing.

This implementation uses **snap/1 with a fixed finalized pivot**. Snapshot
assembly and serving currently materialize state in memory; moving-pivot
range reuse, mainnet-scale resource qualification, and snap/2 BAL-based sync
are not implemented. If a peer ages out the pivot, the attempt fails and the
worker retries the current CL target. Native consensus is an explicit research
mode over locally validated blocks, not a mainnet consensus-fork activation.

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
