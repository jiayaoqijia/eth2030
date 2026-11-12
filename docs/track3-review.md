# Track 3 Adversarial Review

Review target: `fc1f70f`, on `feat/track3-node`. Date: 2026-11-12.

The initial passing suite did not establish correct forkchoice authority,
crash-safe publication, external protocol interoperability, or complete
production snap synchronization. This review follows the production consumers
and corrects both implementation defects and the completion claims.

## Findings and corrections

| Area | Reproduced defect or missing invariant | Correction and evidence |
|---|---|---|
| Engine forkchoice | A known head with an unknown checkpoint returned `VALID`; checkpoint updates ran asynchronously and could outlive a newer FCU. | Known-head validation/publication is synchronous and serialized with sync-worker publication. `TestKnownHeadFCURejectsUnknownCheckpointSynchronously`. |
| Payload authority | `newPayload` and payload construction could select a canonical head before FCU. | Both stage execution-validated candidates. The Engine test DSL now performs an explicit FCU after accepting a payload. Legacy PoS block announcements cannot select the node's head. |
| Validation provenance | Headers or cached/stored bodies were treated as validated blocks; a missing body was synthesized as an empty body. | Durable validation markers, complete-body readback, and checked persistence. Header-only snap history remains separately retrievable. |
| Forkchoice state | A corrupt cached state could be published; rewinds retained stale safe/header/transaction indices. | Check the head state root before publication, invalidate corrupt cache entries, and update/remove canonical indices atomically. `TestForkchoiceRejectsCorruptCachedState`, `TestForkchoiceRewindClearsSafeAndHeaderIndices`. |
| Recovery | Replaying each block rewrote the saved head; a later failure could erase the original recovery target. Wrong genesis could overwrite an existing database. | Replay as validated side data, publish only after the saved target verifies, reject genesis mismatch and malformed ancestry, release locks on construction failure. `TestRestoreFailurePreservesDurableHead`. |
| Durable atomicity | FileDB changed data files before the WAL commit. An injected second-file failure recovered `head=new, finalized=old`. | Commit the WAL before file materialization, flush data before journal truncation, and fence failed writes until reopen. `TestFileDBBatchFailureCannotRecoverMixedForkchoice`. |
| Branch execution | `BLOCKHASH` could read the mutable canonical branch while building or replaying another branch. | Capture the candidate parent's own header ancestry. A signed transaction stores the expected side-branch hash, then caches are evicted and readback is checked: `TestNonCanonicalBlockhashExecutionAndReadback`. |
| Transaction state | Two storage-writing transactions without an intermediate disk commit reused the first transaction's original storage; the second transaction was undercharged by 2,800 gas. | Advance original-storage state at every transaction boundary. `TestSnapMultiTransactionOriginalStorage`. |
| State rollback and reuse | Implicit account creation was not journalled; finalized deletion could remain readable; reverted recreation could purge storage; a cached trie root could survive a later mutation. | Journal implicit creation, retain reversible deletion tombstones, recompute recreation from the live overlay, flush adopted committed slots, and invalidate root caches. `TestStateBackendsRollbackDeletionAndRecreation` covers memory, trie and snap states; `TestTrieCommitInvalidatesPreviouslyReadRoot` pins commit behavior. |
| Differential oracle | The local Geth adapter also omitted Geth's per-transaction `Finalise`, so the old oracle repeated the same gas defect. | Apply the real Geth transaction boundary and replay a complete genesis/deployment/two-transaction sequence. `TestDiffProbe_MultipleStorageWritesInOneBlock` compares receipts and roots. |
| Proof consumers | `eth_getProof` rejected snap/trie-backed state; storage values were interpreted as encoded RLP bytes rather than decoded integers. | Build proofs from complete hashed snapshots; decode storage integers. The TCP sync test validates account and storage proofs with Geth's independent verifier using a value of `0x80`. Snap state no longer promotes incomplete memory-only helper APIs. |
| Body commitments | A trusted pivot's withdrawal list was not checked against `withdrawals_root`. | Enforce EIP-4895 body commitment/presence checks before adoption or execution. |
| Peer interoperability | Native-node transport was plaintext; Hello used private encoding/message numbers; reserved devp2p offsets and shared-capability selection were missing. | Standard encrypted RLPx, canonical RLP Hello, Snappy, persistent 0600 `nodekey`, reserved base messages, highest common capability versions, and correct `eth/68` range. `TestRLPxNegotiationAgainstIndependentCodec` uses Geth's RLPx/codec directly. |
| Fork negotiation | Status advertised no useful fork ID. | EIP-2124/6122 block/time fork checksums and peer filtering, compared with Geth: `TestTimestampForkIDsMatchGeth`. |
| ETH packet formats | Pooled transaction messages omitted request IDs; receipts serialized local derived fields. | Canonical `eth/68` transaction/receipt envelopes and request IDs; `eth/70` uses EIP-7642's flat bloom-free receipts with bloom reconstruction. Receipt pagination tests compare consensus bytes instead of the non-wire `TxHash` field. |
| Peer cancellation | Cancelling a response wait disconnected a healthy multiplexed peer; request IDs could collide between connection generations. | Close only a blocked write, correlate by peer session, and wake waits on disconnect. Cancellation regression checks that the connection survives. |
| Pagination | A storage range capped by its explicit limit could omit its required boundary proof; short hash bounds were compared incorrectly. | Normalize bounds, track truncation, and propagate proof errors. Verified adapters bind account/storage requests to one pivot. |
| Research consensus | Wiring components was expanded into a replacement CL without a complete consensus design; votes for different sources could share a quorum, and failed callbacks consumed deciding votes. | Remove execution authority. The opt-in observer uses source-target link quorums, a chain/timing/registry signing domain, harmless duplicate handling, rollback on callback failure and CL re-anchoring. |
| Cache and lifecycle | A one-entry block cache could fail to evict genesis; backend work and constructors were not consistently joined/cleaned up. | Track genesis/pivot eviction order, join actual backend work, remove unused worker/cache machinery, and close partially initialized nodes. |
| Broader race checks | Discovery tests raced their counters; gossip and anonymous transport statistics mutated under read locks. | Correct synchronization and release gossip locks before application callbacks. Entire affected P2P race suites are included in verification. |

## Behavior-mutation checks

Temporary Go overlays reintroduced five defects without changing the checkout:

1. Retain a stale safe block on a zero-safe FCU.
2. Use canonical `BLOCKHASH` lookup for a side branch.
3. Count different source links in one target quorum.
4. Accept an account whose required bytecode is missing.
5. Omit the native transaction-boundary original-storage update.

Each mutation failed its intended regression. The fifth is checked against the
corrected Geth execution adapter, not just a second native implementation.

## Scope and acceptance

The functional fixed-pivot path is verified over authenticated Engine HTTP and
standard RLPx, through execution, RPC proofs and restart readback. This does
**not** close production snap synchronization: moving-pivot continuation and
bounded-memory assembly remain open. The original Track 3 snap checkbox is
therefore reopened. The research observer is not an autonomous CL, proposer or
validator, and cannot replace the external CL's forkchoice authority.

The full Go suite, affected race suites, integration/tag variants, vet, local
build, and pinned Geth-backed EF vectors are recorded in `STATUS.md`. Geth-backed
EF results are a compatibility baseline; they are not presented as exhaustive
validation of the new native sync or observer paths.

## Normative references

- Local EIPs 161, 3675, 6780, 7928, 2200, 4895, 1186, 6122, 7642 and 7975; Engine Paris.
- [Pinned RLPx specification](https://github.com/ethereum/devp2p/blob/e0091d1140bddce2a9805047b94110d3ad439e6e/rlpx.md).
- [Pinned snap/1 specification](https://github.com/ethereum/devp2p/blob/e0091d1140bddce2a9805047b94110d3ad439e6e/caps/snap.md).
- `refs/research/3sf-mini/consensus.py` is research input, not authority to activate a new mainnet consensus protocol.
