# Project Status

## Track 3 Snap Peer Failover (2026-11-12)

- One fixed-pivot scheduler can now continue across connected snap peers without throwing away ranges already downloaded in the same attempt. Peers share storage roots only after account-range verification against the same pivot; a root mismatch is rejected even if concurrent verification races with pivot initialization.
- Tests cover proof-verified account roots consumed by a second peer, request-preserving transport failover, cancellation, and the existing two-node snap bootstrap. This is not durable resume or moving-pivot range reuse.
- Auditing a possible bounded-memory trie builder revealed that the existing `StackTrie` retains inserted nodes until finalization and discarded node-writer errors. Its documentation now states the actual memory behavior, and `Commit` propagates write failures; it is not yet a bounded-memory replacement for snap assembly.

## Track 3 Sync Scalability Follow-up (2026-11-12)

- A completed snap pivot can now be adopted after finalized advances only after verifying hash-linked, height-contiguous ancestry against the current CL target and checking that the target did not change during verification. Conflicting branches and malformed ancestry fail closed.
- Storage pagination retries preserve the last verified origin, and completed storage tasks are traversed linearly instead of rescanning the entire task list. Regression tests cover interrupted pages, advancing finality, conflicting finality, concurrent target replacement, and invalid heights.
- This is partial progress, not production snap completion: moving-pivot range reuse, bounded-memory state assembly and serving, bounded-memory database storage, and real-client/mainnet-scale validation remain open.

## Forkchoice Missing-Ancestor Follow-up (2026-11-12)

- Rechecked `0084028` and found that old-canonical ancestry traversal could terminate at a missing block body without finding a common ancestor, then publish a reorg without fully removing old transaction indices. `ApplyForkchoice` now rejects that case before committing state or metadata.
- `TestForkchoiceRejectsMissingOldBranchBody` reproduces a missing intermediate canonical body and verifies the proposed reorg fails without changing the head. The test passed with the race detector; affected-package race and local build checks were run separately.
- This follow-up does not close the production snap limitations recorded below: moving-pivot continuation and bounded-memory assembly remain open.

## Track 3 Corrective Review (2026-11-12)

Reviewed `fc1f70f` through specification, production callers, wire messages, persisted state, restart and adversarial tests. The previous Track 3 completion claim was too broad. Detailed findings and regressions: [Track 3 review](docs/track3-review.md).

- Fixed synchronous FCU validation/publication, candidate staging, validation provenance, missing-body handling, canonical index cleanup, branch-specific `BLOCKHASH`, failed-recovery target preservation and genesis mismatch handling. Backend shutdown now joins actual work instead of unused queue loops; partially constructed nodes release their database locks.
- Fixed FileDB's mixed-transaction recovery window by committing WAL before materializing files and fencing failed writes. Added failure injection that previously recovered a new head with an old finalized checkpoint.
- Fixed per-transaction original storage, reversible account creation/deletion/recreation, adopted committed-storage persistence, mutation-invalidated trie roots, and snap/trie RPC proofs. Added a complete multi-transaction comparison with Geth, including the missing per-transaction `Finalise` in the Geth adapter itself. State/proof comparisons include independent verification and values requiring multi-byte RLP encoding.
- Replaced the native node's private plaintext handshake with standard RLPx, canonical Hello, capability negotiation, reserved message offsets, persistent identity and timestamp-aware fork IDs. Corrected eth/68 pooled-transaction/receipt envelopes and eth/70 bloom-free receipts. Added direct interoperability checks against Geth's RLPx, RLP and ForkID implementations.
- Corrected the design overreach in the native lane: `--native-consensus` now enables a research observer and never replaces external CL authority. Votes use a chain/timing/registry domain, per-source-target quorums, duplicate handling, retryable callback failures and CL re-anchoring. Normal future/unavailable observations do not disconnect EL sync peers.
- Broader race checks exposed and fixed gossip/anonymous-transport counter races and discovery test-counter races. Five temporary behavior mutations were detected by their intended regressions.
- Validation passed on Go 1.26.10: full uncached Go suite; affected-package and complete P2P subtree race suites; integration, blst and goethkzg variants; vet; full build; native executable build with goethkzg. Final changed packages were rechecked after the last edits. Pinned Geth-backed EF vectors passed with 36,264 valid outcomes, zero failures/skips and 20 existing legacy outcomes. Five final-source behavior mutations were each detected by their intended regression.
- **Still open:** production moving-pivot continuation and bounded-memory snap assembly. The original real-snap Track 3 checkbox is reopened. The observer is not an autonomous consensus client. No mainnet-scale qualification or complete native-consensus claim is made.


## Track 3 Node Sync and Native Consensus (2026-11-12)

> Historical implementation record for `fc1f70f`; the corrective review above supersedes its authority and completion claims.

Implemented on `feat/track3-node`, from `master` at `9fde8aa` (PR #50 merged). See [architecture, operation and scope](docs/track3-node.md).

- Replaced the node's no-op snap writers and local-only download path with real correlated `eth` requests and `snap/1` clients. Account/storage range proofs are checked against the pivot and storage roots; bytecodes and reconstructed state are hash-verified before a persistent completion marker permits adoption. Hashed state supports ordinary address/slot reads without preimages. The healer follows authenticated account/storage trie references and fails exhausted work.
- Authenticated Engine forkchoice updates now schedule a cancellable CL-driven execution sync worker. Snap bootstrap uses only the CL's finalized hash, retains the 256-parent `BLOCKHASH` window, stages subsequent execution, and publishes the complete forkchoice atomically. Advancing heads do not repeatedly cancel the finalized snapshot. `eth_syncing` reflects pending/completed work. Full and snap startup restore the persisted chain by validated replay; node shutdown joins workers.
- Added explicit `--native-consensus` configuration for the research 3SF/PQ lane: quick-slot scheduling, real ML-DSA-65 verification bound to configured validator identities and chain/source/target fields, quorum justification and source finalization, opt-in `ncs/1` TCP vote delivery, execution checkpoint updates, and lifecycle/restart integration. External Engine forkchoice is rejected in native mode; the default remains external CL control. Signing keys and automated proposal/signing duties remain outside this runtime.
- End-to-end tests use real TCP nodes and JWT-authenticated Engine HTTP calls, wait for peers after SYNCING, assert the RPC sync-status transition, compare state roots/code/storage and post-pivot execution, and reopen databases. Native-consensus tests deliver real signed votes between nodes, verify finalized execution checkpoints and restart state, and verify shutdown. Negative tests cover incomplete/gapped state, wrong roots, tampered trie nodes, exhausted retries, cancellation, superseded targets, signature identity substitution and cross-chain replay.
- These tests exposed and fixed pre-existing canonical header/body wire encoding errors, missing EIP-7928 BAL header commitment, and loss of the storage-root cache when copying dirty state. The BAL execution-rejection regression now recomputes the advertised block hash after tampering so it still reaches execution validation. Amsterdam hashes now include the BAL; old development-chain data using the previous hash encoding requires a fresh chain or resynchronization.
- Verification: Go 1.26.10 full uncached suite, affected race suites (node/backend, consensus, chain/state/types, eth, snap/ethproto, beacon/healer/statesync, trie and RPC), integration/blst/goethkzg CI variants, vet and local build passed. The native executable also built with the Docker image's `goethkzg` tag. Pinned EF vectors at `81862e4` passed: 36,264 valid outcomes, zero failures/skips, 20 existing legacy outcomes. The EF run used a temporary Go overlay only to point the test fixture lookup at the existing read-only checkout; production sources and reference repositories were unchanged.
- Boundaries: fixed-pivot snapshot assembly/serving materializes state in memory; moving-pivot range reuse, mainnet-scale resource qualification and snap/2 are not implemented. Native consensus is an opt-in research lane, not a mainnet fork activation. No existing worktree or running devnet was modified.

## PR #50 CI Repair and Master Merge (2026-11-12)

- Merged `origin/master` at `c8fd3a1` (PR #49 merge, including fix `4903084`), importing the EIP-7928 fork-boundary regression fix and formatting corrections. Resolved the `STATUS.md` conflict by retaining both Track 2 and PR #49 records.
- Applied `gofmt` to the seven remaining Track 2 files reported by the CI formatting check. No protocol behavior changed.
- Validation: CI-matching Go 1.26.10 passed formatting, `go build -p 4 ./...`, `go vet -p 4 ./...`, `go test -p 4 -count=1 -timeout=15m ./...`, and `go test -p 4 -count=1 -timeout=15m -tags integration ./core ./p2p/...`. External EF fixtures are absent in the isolated worktree, so fixture-dependent tests skip locally; the dedicated remote CI job validates the pinned vectors.

## Hegotá Track 2 Closure (2026-11-12)

Completed the Track 2 items on `feat/track2-hegota` (base: `feat/track1-glamsterdam`, PR #49).

- **EIP-7805 FOCIL**: the six CL P2P validation rules implemented in spec order in `focil/gossip_validator.go` (current-or-previous slot, IL committee root match, two-ILs-per-member with equivocation evidence, BLS signature over the message root with DOMAIN_IL_COMMITTEE 0x0C000000, 16-member committee check, 8 KiB byte cap) with an optional-hook GossipContext for EL-side use. `ILStore` gained gossip-grade semantics: identical rebroadcasts dedup, the second distinct IL records `ILEquivocationEvidence` and withdraws trust, further ILs rejected; accessors GetIL/ILs/Evidence/PruneBefore. `list_validator.go` slot window corrected from current-or-next to current-or-previous per the spec, byte cap added. `inclusion_list` gossip topic added and the node subscribes, funnelling through the validator into the IL store.
- **EIP-8141**: the sponsored-frame-tx integration test surfaced three real gaps, all fixed: `opApprove` set the approve flag before validation (a failed APPROVE still counted for VERIFY frames — flags now set only after all checks pass); the CALLER==tx.sender checks were unreachable for the canonical entry-point-caller flow (entry-point case now passes, sender authorization stays with frame.target == tx.sender); and the gas payer was never actually charged (frame txs skip upfront payment, the payer transfer excluded FrameTxType while refunds routed to the payer — the total gas cost is now collected from the payer on APPROVE(1)/(2) with refunds routed back). Receipts carry payer and per-frame results as derived fields per the receipt format. New scenario contracts FrameApproveExec (scope 0) and FrameApprovePay (scope 1); the two-VERIFY-frame sponsored flow is covered end-to-end (sender pays nothing, sponsor charged effectiveGasPrice*gasUsed).
- **EIP-7495**: StableContainer HashTreeRoot aligned with the spec form mix_in_active_fields(merkleize_progressive([field roots]), active_fields) — progressive merkleization for stable generalized indices, packed-bits mix-in; capacity clamped at 256. Conformance tests pin the spec's Square/Circle stable-gindex example, absent-vs-zero distinction, progressive growth stability, and illegal configurations.
- **EIP-8025**: `GenerateExecutionProofWithBAL` anchors execution proofs to the block's real state-access record (EIP-7928 BAL grouped by access index) and commits to the BAL hash, so a stateless verifier can check the proof against the header commitment without re-executing; tampered access sets rejected.
- **SSZ fork activation (EIP-6404/7807/7916)**: `SSZBlockTime` chain-config fork (fork schedule, ordering, genesis loader); header-only SSZ projection `SSZBlockHash`; `engine_getPayloadV7` returns the SSZ payload encoding plus its hash tree root when active; `engine_newPayloadV7` validates the SSZ binding (decode, hash match, field cross-check) before executing through the Amsterdam path; capabilities advertised. eiptest drives the full V7 surface on an SSZ-active sim: SSZ binding accepted, tampered binding INVALID.
- Verification: full uncached `go test ./...` green (207 packages), `go vet` clean.
- **Devnet node verification** (`full-feature-eth2030`, 3x EL + 3x Lighthouse 8.4.0-rc.0, rebuilt with the Track 2 code): fulu@slot 32, gloas@slot 64 with Amsterdam BAL enforcement, and heze@slot 96 all activated cleanly — blocks progressed past 115 with spamoor transfers landing; every post-Amsterdam block processed with `amsterdamActive:true` and nonempty `balEntries` (43); zero WARN/ERROR across all six nodes. The FOCIL gossip subscription, 8141 payer billing, 7495/8025 code, and SSZ fork plumbing (inert without a scheduled SSZBlockTime) run in production without interop regressions.

## PR #49 CI Repair (2026-11-12)

- Reproduced both failures from CI run `43086199969`: nine Go files failed `gofmt`, and `TestBAL_HashValidationAcrossFork` still expected post-Amsterdam headers without a BAL commitment to pass.
- Applied `gofmt` to the nine reported files. Aligned the integration test with EIP-7928's mandatory header commitment, covering absent/present hashes immediately before, exactly at, and immediately after activation. Invalid cases assert `ErrInvalidBlockAccessList`; production validation remains unchanged.
- Validation: Go 1.26.10 (matching CI) and Go 1.27.3 passed formatting, `go build -p 4 ./...`, `go vet -p 4 ./...`, `go test -p 4 -count=1 -timeout=15m ./...`, and `go test -p 4 -count=1 -timeout=15m -tags integration ./core ./p2p/...`. The isolated worktree has no external EF fixtures, so those fixture-dependent tests skip locally; the dedicated remote CI job checks the pinned vectors.

## Full-Feature Devnet Interop After BAL Enforcement (2026-11-12)

Ran `full-feature-eth2030` kurtosis devnet (3x eth2030:local EL + 3x eth2030cl:local Lighthouse 8.4.0-rc.0, 15s slots) against the BAL-enforced Track 1 branch.

- Fork schedule confirmed from the devnet genesis (`refs/ethereum-package/el_cl_genesis_data/genesis.json`): prague@0, osaka@+480s, **amsterdam@+960s — coinciding with CL gloas_fork_epoch 2 (slot 64)**, fulu at slot 32, heze at slot 96.
- All three forks activated cleanly: blocks progressed through slot 32 (Fulu/PeerDAS), slot 64 (Gloas ePBS + Amsterdam BAL enforcement) and slot 96 (Heze/FOCIL) with no stall; head passed 131 during monitoring with spamoor EOA transfers landing continuously.
- BAL enforcement interoperates with Lighthouse: every block after Amsterdam processed with `amsterdamActive:true` and nonempty `balEntries` (40-43 per block); `engine_newPayloadV5` responses all `error:false`; two EL nodes independently processed block 131 with identical hash/gasUsed/balEntries (43), confirming deterministic BAL derivation under the strict header-hash rule.
- Lighthouse's ePBS bid flow is live on the devnet ("Bid has no blobs, data is available" debug logs) with no CL ERROR/CRIT and no EL WARN/ERROR (beyond filtered noise) on any of the six nodes.
- Finding recorded as follow-up: the JSON-RPC block object does not expose the `blockAccessListHash` header field (the field exists and is enforced; only the eth_getBlockByNumber serializer omits it). Explorers/CL tooling reading the field from RPC will not see it.
- The devnet was left running (enclave `eth2030-devnet`, dora at http://127.0.0.1:32809, assertoor at http://127.0.0.1:32811); stop with `pkg/devnet/kurtosis/scripts/cleanup.sh eth2030-devnet`.

## EIP Integration Test Coverage for Track 1 (2026-11-12)

Added EIP-standard integration tests for the Glamsterdam Track 1 work on `feat/track1-glamsterdam`.

- **EIP-7732**: `TestEIP7732_BuilderPaymentLifecycle` walks the payment state machine over the consensus state with real BLS-signed bids (bid → pending payment → payload envelope → withdrawal queue; attest-backed quorum flush across two epoch boundaries). `TestEIP7732_PTCAttestationBlockFlow` aggregates 257 real PTC member signatures over a 16384-validator state, validates through `process_payload_attestation`, includes the attestation in a produced block body (MAX_PAYLOAD_ATTESTATIONS cap), and confirms fork-choice timeliness. `TestEIP7732_ForkChoiceHeadRespectsPayloadStatus` drives head eligibility between payload-building and empty-building children.
- **Integration test found a real bug**: `GetIndexedPayloadAttestation` dropped the aggregated `Signature` field, so every aggregated payload-attestation verification checked a zero signature. Fixed in `consensus/epbs_state.go`; the new tests pin the behavior.
- **EIP-7975**: `TestEIP7975_ReceiptsPaginationOverRealChain` paginates real Sim-chain receipts through the eth handler over a message pipe (`firstBlockReceiptIndex` slicing, request-order pages, no false incomplete flag); `TestEIP7975_ValidatePartialReceipts` covers the three spec sanity checks against a real block (count match, receipt size vs gasLimit/8, unknown block) plus the block-gas-limit total check; `TestEIP7975_LastBlockIncompleteTruncation` drives serving-side truncation across the 10 MiB budget with `lastBlockIncomplete` set.
- **EIP-8159**: `TestEIP8159_BALEndToEnd` ties the Sim's BAL RLP encoding to the header `block-access-list-hash` commitment and validates served wire bytes against real headers, with 0xc0 for unavailable blocks; `TestEIP8159_RejectsTamperedBAL` enforces the client-side tamper rejection and the empty/non-empty rules for headers with and without the field.
- **EIP-7594**: canonical `data_column_sidecar_<subnet>` topic round-trips for all 128 columns, custody-manager assignments validated against canonical subnets, sidecar gossip REJECT/IGNORE rules (subnet, future slot, finalized slot) on builder-built sidecars with signed headers, and reconstruction from one column per subnet meeting the 64-column threshold.
- **Registry**: EIP-7975 and EIP-8159 added to the coverage registry (TierB/Encoding) and to the IFLOW.md implementation list (Substantial); EIP-7594 upgraded from the TierD exemption to TierC with in-process coverage. The inverted `integration`-tagged BAL test (`TestValidator_RejectsMissingBALHashPostAmsterdam`) plus a block-gas-limit total-size case keep the tagged CI suite aligned with the strict post-Amsterdam rule.
- Verification: full uncached `go test ./...` green (207 packages), `go vet ./...` clean, integration-tagged core suite green, eiptest suite 68+ tests green in ~2.5 s.

## Glamsterdam Track 1: ePBS, eth/70-71, BAL, PeerDAS (2026-11-12)

Implemented the Track 1 closure items on `feat/track1-glamsterdam` (base: `master` at `137006e`, PR #48 merged; merged branch `feat/eiptest-framework` deleted locally).

- **EIP-7732 ePBS (Gloas)** per `refs/consensus-specs/specs/gloas`: new `consensus` epbs layer with the Builder registry container, `ExecutionPayloadBid`, payload envelope, and PTC attestation types; `get_ptc` with balance-weighted selection over concatenated slot committees; `process_execution_payload_bid` (self-build infinity-signature rule, active/funded builder checks, parent/randao/slot commitments, pending-payment recording), `process_payload_attestation`, `process_builder_pending_payments` (quorum flush plus window slide), the consensus side of `process_execution_payload` (payment queueing, payload availability, latest block hash), builder withdrawal plumbing (`get_builder_withdrawals`, builders sweep, apply/cursor updates), attestation weight and slashed-proposal payment clearing. `BeaconBlockBody` now carries `SignedExecutionPayloadBid` and `PayloadAttestations` (Gloas shape, replacing the inline payload header). `EPBSForkChoice` wraps ForkChoiceV2 with PTC vote vectors, payload states, FULL/EMPTY/PENDING classification, the payload-status tiebreaker, and child eligibility filtering. `engine_submitBuilderBidV1` now feeds the node's auction engine through a bid-observer hook.
- **EIP-7975 (eth/70)**: receipts pagination moved to the spec encodings — `GetReceipts` (0x0f) with `firstBlockReceiptIndex`, `Receipts` (0x10) with `lastBlockIncomplete` — replacing the invented 0x11/0x12 messages. Serving paginates under a 10 MiB byte budget. `ValidatePartialReceipts` implements the spec sanity checks: delivered count equals the transaction count, each receipt fits `gasLimit/8` of its transaction, and the total stays within the block gas limit.
- **EIP-8159 (eth/71)**: `GetBlockAccessLists`/`BlockAccessLists` moved to message IDs 0x12/0x13 and carry the canonical EIP-7928 RLP BAL encoding. Unavailable BALs are served as the empty list (0xc0). `ValidateBlockAccessListForHeader` enforces `keccak256(rlp(bal)) == block-access-list-hash` and rejects non-empty BALs for headers without the field.
- **EIP-7928 (BAL)**: post-Amsterdam blocks now MUST include `block-access-list-hash` (the CL-compatibility leniency predated mandatory BAL in V5 payloads and lighthouse >= 8.4). The earlier "probe only logs a BAL mismatch" note was stale — both engine backends already rejected mismatches.
- **EIP-7594 (PeerDAS/Fulu)**: canonical `data_column_sidecar_<subnet_id>` gossip topics with `compute_subnet_for_data_column_sidecar` and topic parsing in `p2p/gossip`; `DataColumnSidecar` now carries `SignedBeaconBlockHeader` per the Fulu container; gossip-rule validators implement the subnet REJECT rule and the slot IGNORE rules (future slot, not newer than finalized); sidecar builders gained header-carrying variants.
- Verification: `go vet ./...` clean, `go build ./...` clean, full uncached `go test ./...` green across 207 packages including the pinned EF fixtures, plus focused suites for `consensus`, `engine`, `epbs/...`, `node`, `eth`, `p2p/...`, `das/...`, `bal`, `eiptest`. New unit tests cover PTC election determinism, bid validation paths, payment quorum/slide, envelope processing, PTC wire-message validation, fork-choice payload status filtering, eth/70 pagination and partial-receipt validation, eth/71 BAL round-trip and header validation, and sidecar subnet/slot rules.

## Native Execution Requests and SSZ Integration (2026-11-11)

- Merged upstream master at `677bbb8` for PR preparation and retained both sets of status and checklist entries.
- Replaced the 32-byte storage fixture path with canonical native execution. EIP-6110 deposits come from matching receipt logs with the exact ABI layout and produce 192-byte records in log order. The EIP-7002 and EIP-7251 system calls execute the canonical queue contracts and return 76-byte withdrawal and 116-byte consolidation records. The native public-network configurations require queue code. `WithRequestContracts` and `ExecutionRequestContractAlloc` provide the deployed runtimes for new private and test chains. The deposit runtime comes from execution-specs at `c708ab6075973b6b928d17ec073dd43a7fe161b6`.
- Block builders, replayers, and validators share post-transaction finalization. Header commitments use the requests derived from execution, and the post-execution BAL records queue writes. Engine payload builders return the canonical request groups, including through the asynchronous builder. Prague and Amsterdam payload validation rejects malformed wire ordering and mismatches between supplied and executed requests.
- Verified two real deposits in one block and all three request types through the native Amsterdam Engine HTTP path, an independent receiver, block replay, and the EIP-7807 projection. A payload with altered requests and a matching advertised header hash was rejected by execution validation. The valid payload produced the expected typed consensus requests root, and the next block emitted no repeated requests.
- The default Go suite with required EF fixtures passed in 130.82 seconds. Affected race checks, the tagged integration suite, formatting, vet and build passed. The native request integration tests passed 20 repetitions. Four isolated mutations that dropped deposits, withdrawals or async build output, or skipped request-commitment validation, each failed its intended regression. Local verification used Go 1.27.3.

## Execution SSZ and Framework Test Effectiveness Review (2026-11-11)

- Reviewed the pushed `b3972f0` branch with the CI-shaped default Go suite and required EF fixtures. After the test updates, 19,424 top-level Go tests passed and seven existing top-level skips were reported. All 68 framework tests passed with zero skips. The pinned EF results were 36,264 passes, zero failures or skips, and 20 known legacy outcomes. The default command took 130.74 seconds using Go 1.27.3.
- Ran 18 isolated behavior mutations using Go overlays. Initially, 11 failed a targeted assertion and seven passed the selected tests. After adding assertions, the same 18 mutations each failed the intended tests. This covered EIP-6404 signature encoding and decoding, full-width chain IDs, compatible-union roots, the empty progressive list, canonical offsets, EIP-7807 payload construction and roots, Engine BAL and hash validation, pool selection, Geth receipt comparison, and the registry gate.
- Added specific rejection assertions for high-S encoding and decoding and for a container with padding between otherwise valid fields. Regenerated the independent EIP-7807 fixture with two transactions and two receipts, then verified that the block projection reproduces its bytes and root with nonempty requests and withdrawals and distinct blob gas limit and usage. The Engine BAL case now verifies the execution validation reason and the unchanged receiver head.
- Ran the targeted suites 20 times. Formatting, vet, build, the integration, blst and goethkzg tagged suites, CI and extended race coverage, and the EEST generation-to-execution smoke test passed. CI race coverage now includes `ssz`, `core/types` and `eiptest`.
- The EIP-7807 projection regression uses valid nonempty reference requests. A separate probe confirmed that the current native EIP-6110 storage queue fixture emits 32-byte deposit request data, which the consensus request root rejects. The test coverage therefore establishes the projection contract for valid requests and the current queue behavior. Native production of canonical nonempty requests and SSZ fork activation remain open.

## Execution SSZ Specification Review (2026-11-11)

- Replaced the experimental transaction byte layout with the ten EIP-6404 compatible-union variants and typed Merkleization. The current EIP-7932 draft uses secp256k1 selector `0x00`. EIP-155 chain IDs and signature parity round-trip independently of the size of the original RLP `v`, preserving the original bytes and signing hash.
- Replaced the block serializer with the published EIP-7807 payload and header summary. It retains EIP-2718 transaction and receipt envelopes and RLP withdrawals, and derives the typed Electra requests root. Both SSZ objects commit to the same root. The codec is available to callers with explicit execution inputs. Consensus activation, Engine API versioning and RPC migration remain separate fork work.
- Corrected the empty EIP-7916 progressive basic list root and tightened canonical container offsets. Reference vectors use the published EIP schemas at `bd93d240b72702450920eeeadc2ce593626bfd8a`, pinned Electra definitions and `ethereum/remerkleable` at `2f0baeef0082d4278acaef7d822deb7009d7db7e`. They cover byte-for-byte encoding and roots for all ten transaction variants, an EIP-155 signed example, a large chain ID, both authorization variants, and an EIP-7807 payload with nonempty transaction, withdrawal and execution request data.
- Verified deterministic vector regeneration, the uncached default Go suite (138.46 seconds), race checks for `ssz`, `core/types` and `eiptest`, the tagged integration suite, formatting, vet and build. Local verification used Go 1.27.3.

## EIP Test Framework Completion (2026-11-11)

- The simulator returns the execution results from canonical block insertion. Regressions cover five-block state continuity, scheduled genesis JSON, exact fork timestamps and stable phase views, receipt and log assertions, state diagnostics, historical calls, and negative validation that preserves the head. Pool tests exercise admission, replacement, eviction, nonce ordering, branch rollback, and finalization.
- The Engine probe uses the node's HTTP endpoint and an independent validating chain. Tests cover consecutive signed payloads, hash and head consistency, inclusion-list attributes, canonical EIP-7928 BAL encoding and rejection, and ePBS bid ordering. The RPC probe checks typed transactions and receipts. The standalone Engine backend also checks V5 payload hash and BAL consistency.
- The differential probe compares state roots and full encoded receipts across a sequence. Native and Geth now agree on the previously divergent contract creation at 75,572 gas. The calldata floor determines the minimum transaction gas limit and clamps final gas usage. Intrinsic gas alone is charged before the EVM runs. The committed calldata, beacon root, blob, Frame and DELEGATECALL regressions assert observable execution outcomes.
- The transaction matrix verifies RLP round trips and recovery for signed envelopes. The pinned EIP-155 vector is checked independently. EIP-7825 is exercised through pool admission, block building and block validation at Osaka. The EIP-7934 scaffold demonstrated a red/green cycle: the original block path reached an intrinsic-gas error, then the Osaka RLP-size check rejected the same candidate.
- The coverage gate detects missing implementation entries and assertion-free test files. Both implementation list paths trigger CI. Verified the scaffold in an isolated directory from generation to compilation, expected gate failure, and passing behavior assertion. It also generated a shell-valid devnet config and verifier pair. Optional legacy test migration remains incremental.
- `make contracts` generates seven embedded runtimes from Solidity and Yul. `make eest` uses the pinned EELS workspace to fill a fresh Amsterdam fixture and immediately executes it through Geth. The smoke fixture tests calldata-derived storage with zero gas price across the two pinned revisions. `make bin/evm` builds the older pinned Geth tool for forks it supports.
- Local verification uses Go 1.27.3. The framework ran 68 top-level tests with no skips in 1.57 seconds of package time. The uncached default suite passed in 130.84 seconds wall time, including the pinned EF fixtures. Build, vet, formatting, integration and goethkzg tagged suites, and race checks for the framework, Engine and node backend passed. Contract generation was repeatable with a clean artifact comparison.
- Compatibility boundaries remain in the underlying protocols. The current SSZ legacy signature field is one byte, so large EIP-155 `v` values produce an explicit encoding error. EIP-8079's draft address and ABI are unspecified; coverage exercises the rollup component. The Amsterdam fixture verifies a shared state-transition case across pinned EELS and Geth revisions. The optional devnet scaffold was verified locally without starting a network.

## EIP Test Framework Acceptance Review (2026-11-11)

Reviewed `feat/eiptest-framework` at `4610eac` against the [framework plan](docs/plans/eiptest-framework-plan.md). The framework runs in the default suite. The v1 acceptance criteria remain open.

- Local Go 1.27.3 verification passed for `go test -count=1 ./eiptest/...` (57 top-level passes and two EIP-8141 skips, 0.795 s package time), `go test -count=1 ./...` (127.22 s wall time, including the EF suite), `go test -race -count=1 ./eiptest/...`, `go test -count=1 -tags integration ./core ./p2p/...`, `go vet ./...`, `go build ./...`, and the CI formatting check. The Go 1.26.8 toolchain download timed out locally. `make contracts` regenerated three contracts with a clean diff. The EIP-7702 selection took 0.711 s of package time and 1.59 s wall time.
- T0.2 state continuity passed an independent five-block test that incremented the same storage slot across three blocks. T0.1/T2.2 still need the genesis JSON round trip and the planned fork transition. With activation at second 18, the probe built its boundary block at second 24 and the retained before-phase view moved to the new head.
- T1.1/T1.3 and T2.1 are incomplete. `BlockResult` lacks block logs, BAL and requests, and the diagnostic dump is unused. `CommitInvalid` returned the same unrelated gas-limit validation error with and without a nonce-invalid transaction. The STF call uses `STATICCALL` and rejects a storage write that succeeds in a simulated RPC `eth_call`. T2.6 wires ordinary insert feedback, but an empty `Commit()` consumes pending pool transactions and the probe has no finalize/reorg wiring.
- T1.2/T2.3 are incomplete. An EIP-155 legacy transaction signed for Alice recovered another address after RLP decode, while the helper passed because it skipped sender comparison on a missing sender cache. Several planned signed builders and the EOF builder are absent. The transaction SSZ helper checks repeated encodings and never decodes them.
- T1.4/T2.8/T2.9 need stronger enforcement and a working walkthrough. A synthetic implemented EIP failed the gate while absent from the registry. Adding its generated TODO-only test and a manual registry row made the gate pass. A missing `IFLOW.md` skipped the gate. The CI path filters omit both `IFLOW.md` and its tracked target `CLAUDE.md`. The documented scaffold command fails from the repository root and the generator prints the registry row for manual entry. T3.1/T3.2 have no demonstrated test-first red/green workflow for a new feature or resulting feedback round.
- T2.4 verifies live block RPC responses, with typed-transaction and receipt assertions still due. T2.5 passes the first injected transaction, but two successive Engine cycles both returned block 1 with `VALID`; the Engine head remained at genesis and the latest-valid hash differed from the advertised payload hash. The probe uses the standalone Engine backend and leaves inclusion-list attributes and node-level HTTP dispatch uncovered. Its BAL mismatch path only logs the mismatch, which conflicts with the EIP-7928 validation requirement. T2.7 compares one transfer's receipt status and gas. It lacks multi-block root parity and storage snapshots. A direct comparison reproduced contract creation at 75,930 native gas and 75,570 Geth gas. The committed contract-parity test logs a fixed Geth value without running Geth.
- The EIP-8141 skip is explained by the existing STOP-only frames omitting payer approval. A separately run funded sender with an `APPROVE` contract and Glamsterdam enabled successfully executed a VERIFY frame and advanced its nonce. The calldata exclusion at 1,500 and 2,000 bytes was explained by the EIP-7976 floor: both were included at their respective minimum limits of 100,500 and 132,500 gas.
- T4.1 still lacks the delegate caller, AA entrypoint, rollup anchor and frame approval scenario contracts. T4.2 has two distinct blockers. `make eest` invokes `go build` for Geth from outside its module. Building the pinned Geth binary inside its module succeeded, but the direct EEST fill exited 5 without running any tests or producing Amsterdam vectors. The default test executes the committed Cancun fixture at a fixed path. T4.3 depends on the first test-first feature. T5.2 emits a devnet stub with no feature validation. T5.3 is already covered by the tagged CI job.

## eiptest Framework Implementation (2026-11-11)

- Implemented the full `eiptest` framework on `feat/eiptest-framework` per the [task plan](docs/plans/eiptest-framework-plan.md): `Sim` chain simulator over the production blockchain/builder/processor path with scheduled-fork configs and genesis-alloc funding; ten-type tx builder DSL; assertion library; seven surface probes (STF, pool, fork, encoding, RPC over the production RPCBackend, Engine payload flow with transaction injection, geth differential); EIP coverage registry gate wired into the default suite; scaffold generator with optional devnet config emission (`tools/eiptest-scaffold`).
- Shipped 45 EIP integration test files (60 test functions) covering the implementation list (tiers A–C; network/CL tiers documented as exempt). Full suite runs in ~0.75s with no build tags; the registry gate fails the default suite when an implemented EIP lacks a test file.
- Contract pipeline (`make contracts`): three solc scenario contracts compiled to runtime bytecode with a Go generator (`tools/gen-contracts`) and an end-to-end deploy/ABI test. Vector pipeline (`make eest`): wired uv + geth `evm` fill; the pinned EEST checkout is mid weld-migration (pytest cannot collect), so consumption is verified with a real GeneralStateTests fixture through `eftest.RunSingleFixture`.
- Production fixes the framework surfaced during development: EIP-7702 delegated-EOA calls now resolve the 0xef0100 designator in `core/vm` (Call + call handlers — previously calls into delegated EOAs executed the designator bytes and reverted); Engine API `ProcessBlockV5` now decodes the lighthouse ≥ 8.4 hex-string BAL form it itself emits; `SetTxPool` now feeds payload building (async builder + the sync FCU path built with a nil pool) instead of only inclusion-list generation.
- Recorded tracked findings for follow-up: ~360-gas contract-creation divergence between native and geth backends; post-Glamsterdan builder drops calldata-carrying txs between 1–2 KB; EIP-8141 frame execution reverts across probed variants while envelope/pool surfaces work; geth differential adapter applies no post-block system contracts (EIP-4788/2935), so the Diff probe compares receipts not roots.
- Corrected the plan's record: the `integration` build tag is not orphaned — CI's `test-variants` matrix runs it and the suite is green locally (core tagged suite verified; `core/eftest` under the tag runs the full EF pass at ~115s).
- Verification: `go vet ./...` clean, `go build ./...` clean, `go test ./eiptest/...` green (58 tests, ~0.75s), `make contracts` idempotent, scaffold generate→compile→test round-trip verified with a throwaway EIP.

## EIP Integration Test Framework Plan (2026-11-11)

- Authored the `eiptest` framework design and phased task plan at [docs/plans/eiptest-framework-plan.md](docs/plans/eiptest-framework-plan.md): an in-process integration test base for future EIP development, with seven surface probes (STF, pool, fork gating, encoding, RPC, engine payload flow, geth differential), an EIP coverage registry gate, and a test scaffold generator.
- Plan targets: single-EIP test iteration under 1 second, full framework suite under 2 minutes, no build tags, default `go test ./...` inclusion. Devnet stays responsible for multi-node surfaces only.
- Baselines recorded on the dev host: `-short` full suite 37.3s, `go vet` 5.7s, single-client devnet boot to block 10 ~50s; the git history review (288 fix commits) shows engine/RPC/txpool surfaces carry the most recurring fixes, which sets the probe priorities.
## PR #47 Merge Resolution

- Merged upstream `master` at `d610385` into the audit follow-up branch and retained both sets of status entries.
- Preserved the refreshed Strawmap and linked its seven milestone cards to the corresponding EIP tracker sections.
- Passed the Go module build and focused BAL, ePBS, FOCIL, core EIP, PQ, and rollup tests. The site build, static export assertions, and EIP tracker file checks also passed.

## Cryptography Regression Fixes (2026-11-10)

- Authenticated native PQ execution and transaction admission with a shared typed signing digest and the existing transaction wire format.
- Matched the pinned Geth Prague activation and EIP-2537 contract layout. Historical native BLS replay uses an explicit chain configuration cutoff.
- Corrected RLPx identity handling, PeerDAS cell-size consistency, empty ECIES messages, and proof batch deadlines. STARK gossip now transmits the complete proof and remains advisory.
- The clean staged snapshot passed 206 Go test packages, builds, vet, tagged tests, Go-only tests, and targeted race checks. The BLS tests passed 914 success and failure vectors. The pinned EF gate reported 36,264 passes and 20 known outcomes with no unexpected failures or skips.

## Ethereum Client Cryptography Follow-Up (2026-11-10)

- Traced Geth's secp256k1, EVM curves, KZG, Engine JWT, and keystore providers and the separate Lighthouse BLS and Verkle paths.
- Switched IPA openings, Poseidon, VDF, VRF, and prime-field share reconstruction to public libraries. Retired unused local signers and the old threshold prototype.
- Added real opening verification for IPA and KZG batches and required registered verifiers for SNARK and STARK statements. The PQ attestation codec carries the full ML-DSA key and signature.
- The clean staged source passed all 206 test-bearing Go packages, build, vet, tagged tests, Go-only tests, and targeted race checks. The pinned EF gate recorded 36,264 passes and 20 known outcomes with zero unexpected failures or skips.
- A Geth provider probe selected Go KZG and C KZG in turn, generated and verified an opening with each, and compared their commitment bytes.
- Recorded the format changes and outstanding STARK, lattice and aggregation work in the [codebase audit](docs/CODEBASE_AUDIT.md#vii-ethereum-client-references-and-follow-up-migration).

## Public Cryptography Library Migration (2026-11-10)

- Moved secp256k1, BN254 precompiles, BLAKE2b compression, ECIES, keystore and RLPx transport to go-ethereum. Engine JWT authentication uses golang-jwt.
- Moved BLS12-381 arithmetic and encodings, BN254 point operations, Bandersnatch operations, EVM transforms, and Groth16 verification to gnark libraries. Blob and PeerDAS operations share the Ethereum KZG context.
- Replaced the project-local PQ signing and KEM implementations with CIRCL and go-fn-dsa. Earlier PQ and ECIES artifacts require regeneration in the public library formats.
- The staged Go snapshot passed builds, vet, the full fixture-backed suite, and tagged tests. The geth-backed EF gate recorded 36,264 passes and 20 known outcomes with zero unexpected failures or skips.
- Recorded the remaining protocol migration work and compatibility boundaries in the [codebase audit](docs/CODEBASE_AUDIT.md#vi-public-cryptography-library-migration).

## Unused Package Cleanup (2026-11-10)

- Removed six packages with no in-repository importers in the [first cleanup batch](docs/CODEBASE_AUDIT.md#v-follow-up-results).
- Verified the clean snapshot with Go builds, vet, module tidy, default and tagged tests, and the website path check. Both executables retained the same production dependency sets in the compared configurations.

## Codebase Audit Follow-Up (2026-11-10)

- Completed the [audit follow-up](docs/CODEBASE_AUDIT.md#v-follow-up-results) on `fix/codebase-audit-follow-up`.
- Native point evaluation verifies the tracked upstream vector using the shared Ethereum KZG ceremony context. Docker and Makefile binary builds use `goethkzg`.
- The geth-backed EF gate executed 36,284 pinned vectors locally. It reported 36,264 passes and 20 exact legacy outcomes, with zero unexpected failures or skips. The missing-fixture probe failed as intended.
- Gnark v0.16.5 passed BN254 AA proving and verification. A compatibility probe also loaded the previous serialized R1CS and keys and verified a prior proof.
- The fresh source snapshot passed formatting, vet, build, and all 213 test-bearing Go packages. The tagged and race suites passed locally. The container started both binary entry points and reported source commit `7e05845`. The site build and EIP path check passed.
- Removed unused internal wrappers and moved ignored artifacts out of the worktree. Tor/Nym live probes require configured services, and the workflow results recorded here are local.

## Codebase Audit Evidence Review (2026-11-10)

- Updated [the codebase audit](docs/CODEBASE_AUDIT.md) with verified production import paths, the native KZG fixture result, tagged test outcomes, EF vector totals and exit behavior, and the gnark advisory.
- Revised the follow-up to match the evidence and added commands for reproducing the results at the audited revisions.
- Verified report links, the website verifier on the documented source snapshot, the Go build, and the website build.

## English Documentation Review (2026-11-10)

- Manually translated `docs/CODEBASE_AUDIT.md` into English, retaining the reported findings and reproduction commands.
- Added the English documentation requirement to `AGENTS.md`.
- Found no remaining Chinese text in the project-owned text files. Extracted text from the five tracked PDFs and visually checked pages without a text layer.
- Verified the Go module with `go build ./...` and the website with `npm run build`.

## Update Plan Phases 2+3: Dependency Upgrades + Lighthouse Sync (2026-11-06)

### Current Status: COMPLETE (with recorded Gloas/Heze devnet dependency)

**Scope:** Execute UPDATE_PLAN.md phases 2 (Go dependency upgrades) and 3 (lighthouse upstream sync), per the "upgrade as far as possible" directive.

**Completed Work:**

Phase 2 — eth2030 Go deps (each group committed separately):
- go-ethereum v1.17.1 -> v1.17.10: GasPool AddGas -> NewGasPool, StateDB.Commit(rules, block), SetTxContext blockAccessIndex, Message gas fields -> *uint256.Int, Holesky removed upstream (mapped to Hoodi 560048, added hoodi network). EF state tests 36,126 passing. (f20cf72)
- gnark v0.14.0 -> v0.16.4, gnark-crypto v0.19.2 -> v0.22.0: 65 packages green (eb20de1)
- x/crypto v0.48.0 -> v0.58.0 (+net/sys/text/sync): 43 packages green (732e844)
- blst v0.3.16 -> v0.3.17: 55 packages green (1934f0b)
- Indirect refresh via go get -u: pebble v2 2.1.7, otel 1.47, grpc 1.85 etc.; full suite 213 packages, 0 cached (da9c207)
- Dockerfile builder -> golang:1.27-alpine (go directive bumped to 1.26.8 by -u) (02a8789)
- CLAUDE.md geth version refs updated (e711263)

Phase 3 — lighthouse:
- Verified all 69 fork commits (feat/fix-native-aa) are superseded by upstream unstable (ForkName::Heze + full IL infra, payload envelopes, PTC). Backup: backup/feat-fix-native-aa-20261106.
- New branch integrate/unstable-202611 = sigp/lighthouse unstable 25b9b754 (8.4.0-rc.0, rust 1.91 image) + IFLOW symlink. (4df5a7bcf)
- cargo update batch reverted: parking_lot trio now requires rustc >= 1.95, breaking the rust:1.91 Docker build; upstream lockfile is the MSRV-compatible set. (2c468be9c)
- cargo check + make lint (clippy) clean; eth2030cl:local rebuilt.

Phase 3e — devnet cross-validation (eth2030cl:local + forks enabled):
- Sanity (forks disabled, upgraded deps): blocks advancing, CL verified, peers=2.
- Fulu transition (PeerDAS): passed on both rounds.
- Engine API compat fixes for lighthouse >= 8.4 (in pkg/engine, with tests):
  - fcuV4/V5 third custodyColumns param accepted; attributes shape sniffing (lighthouse snake_case/hex vs fork-era camelCase); engine_forkchoiceUpdatedV5 routed (1d56d58)
  - fcuV5 advertised in exchangeCapabilities; getBlobsV3 custody bitarray param (d673210)
  - Gloas getPayloadV5/V6 responses carry blockAccessList (0x-hex, RLP bytes; EIP-7928 SSZ wire encoding TODO) + slotNumber from payload attributes; e2e test drives real backend (77287d4, 1991580)

**Recorded dependency (blocks Gloas/Heze devnet acceptance):**
The devnet EL runs eth2030-geth (ethereum-package's geth launcher hardcodes the `geth` command), so the Engine API is go-ethereum's built-in server — go-ethereum v1.17.10 does not emit the Gloas payload shape (blockAccessList/slotNumber), and the CL correctly rejects it (NoViablePayloadBid -> missing field blockAccessList). The pkg/engine fixes serve the self-built binary path (verified by e2e test against the real backend). Full Gloas/Heze devnet bring-up requires either: (a) devnet EL switched to the self-built binary (needs an ethereum-package launcher override or fork — refs are read-only), or (b) Gloas support upstream in go-ethereum. Local manual verification confirmed fcuV5 + lighthouse-shaped attributes parse and build correctly on the self-built EngineAPI.

**Verification:**
- `cd pkg && go build ./... && go test -timeout 240m ./...` (213 packages green incl. EF state tests)
- `cd ../lighthouse && cargo check --workspace && make lint` (clean)
- `cd pkg && go test -run TestGetPayloadV6ResponseCarriesGloasFields ./engine/` (Gloas wire shape)
- Devnet sanity + fulu: `./scripts/run-devnet.sh full-feature-eth2030` -> fulu boundary passed (blocks past slot 32)

## Update Plan Phase 0/1 Baseline (2026-11-06)

### Current Status: COMPLETE

**Scope:** Baseline re-validation after ~6.5 months of maintenance downtime, plus devnet infrastructure pinning per workspace docs/UPDATE_PLAN.md (phases 0 and 1).

**Completed Work:**
- Toolchain: Go 1.25.2 -> 1.27.3 (gvm default), rust/cargo 1.99.1
- spamoor: pushed 3 unpushed commits (4d4b439..4908a05 on feat/support-native-aa)
- eth2030: `go build ./...`, `go vet ./...`, `go test ./...` all green on go1.27.3 (213 packages, 0 cached results, 0 FAIL; includes core/eftest EF state tests, 156s)
- lighthouse: `cargo check --workspace` clean (2m17s, 0 errors, 22 upstream future-incompat warnings)
- Devnet infra pinned: run-devnet.sh / run-feature-tests.sh / run-geth-testnet.sh default KURTOSIS_PACKAGE to local refs/ethereum-package instead of remote master; submodule advanced 234fb546 (2026-02-12) -> 6.1.0 c4a8e15d (2026-04-02) because configs use heze_fork_epoch (upstream renamed eip7805_fork_epoch -> heze on 2026-02-26, eb8c590a)
- Devnet smoke (full-feature, pinned package): blocks advancing, CL "Synced (verified)" with peers=2, EL net_peerCount=2, no WARN/ERROR in EL logs

**Verification:**
- `cd pkg && go build ./... && go vet ./... && go test -timeout 240m ./...`
- `cd ../lighthouse && cargo check --workspace`
- `cd pkg/devnet/kurtosis && ./scripts/cleanup.sh && ./scripts/run-devnet.sh full-feature`
- `cast bn -r http://$(kurtosis port print eth2030-devnet el-1-geth-lighthouse rpc)` -> advancing

## Website Strawmap Refresh (2026-11-05)

### Current Status: LOCAL UPDATE COMPLETE; FURTHER AUDIT AND DEPLOYMENT PENDING

Updated the site against the EF Strawmap drawing dated 2026-11-02, separating proposed Ethereum milestones from selected ETH2030 code evidence. Replaced unsupported completion claims with links to code, tests, specifications, and known limits. Reviewed selected EIPs and implementation paths at repository revision `14497b5`; this is not a full EIP-by-EIP conformance or cross-client interoperability audit. In particular, PQ registration verification uses a stub and ePBS bid validation has a zero-signature testing path.

Verification: `cd site && npm run build && node scripts/check-export.mjs`; desktop and mobile browser previews; `cd pkg && env -u GOROOT go test ./bal ./epbs ./focil ./core/eips ./crypto/pqc ./rollup/...` (run as two commands). Publishing remains pending; the deployment workflow still names `eth2030.com`, while GitHub Pages currently binds `eth2030.altllm.ai`.


## Website Domain Recovery (2026-11-05)

### Current Status: BLOCKED ON REPOSITORY ADMIN

GitHub Pages is configured for GitHub Actions but currently binds `eth2030.altllm.ai`, while the website and deployment workflow specify `eth2030.com`. The latter resolves to GitHub Pages and returns 404. A repository administrator must change the Pages custom domain to `eth2030.com` and verify DNS and HTTPS; this replaces the existing custom-domain binding. The privacy page now identifies GitHub Pages as the hosting provider.

## Full-Feature Devnet Bring-Up (2026-03-25)

### Current Status: IN PROGRESS

**Scope:** Build the local Lighthouse and ETH2030 images as needed, launch the Kurtosis `full-feature` devnet in the `eth2030-devnet` enclave, and verify block production plus basic EL/CL health.

**Planned Verification:**
- `cd ../lighthouse && docker build -t eth2030cl:local .`
- `cd pkg && docker build -t eth2030:local .`
- `cd pkg/devnet/kurtosis && ./scripts/cleanup.sh eth2030-devnet`
- `cd pkg/devnet/kurtosis && ./scripts/run-devnet.sh full-feature`
- `cast bn -r http://$(kurtosis port print eth2030-devnet el-1-geth-lighthouse rpc)`
- `kurtosis service logs eth2030-devnet cl-1-lighthouse-geth 2>&1 | grep -E "verified|ERROR" | tail -20`
- `kurtosis service logs eth2030-devnet el-1-geth-lighthouse 2>&1 | grep -E "WARN|ERROR" | tail -20`

## Inclusion List JSON Refactor (2026-03-21)

### Current Status: COMPLETE

**Scope:** Simplify the local inclusion-list JSON parsing helpers in `pkg/engine/inclusion_list.go` without changing the wire format or validation behavior.

**Completed Work:**
- Extract small helper functions for repeated hex-prefix stripping and byte-slice conversion.
- Reuse the helpers from the custom JSON marshal/unmarshal paths.
- Add focused parsing tests and rerun the relevant engine test suites.

**Verification:**
- `cd pkg && go test ./engine/...`

## Blob Bundle Refactor (2026-03-21)

### Current Status: COMPLETE

**Scope:** Deduplicate the cell-proof expansion logic shared by the V2 and V6 blob-bundle builders in `pkg/engine/backend.go` without changing output formats.

**Completed Work:**
- Extract shared blob sidecar collection and cell-proof expansion logic.
- Reuse the helper from the V2 and V6 blob-bundle builders.
- Verify the relevant engine test suites after the refactor.

**Verification:**
- `cd pkg && go test ./engine/...`

## Payload Retrieval Refactor (2026-03-21)

### Current Status: COMPLETE

**Scope:** Deduplicate the repeated payload retrieval paths in `pkg/engine/backend.go` without changing API-specific response contents or async eviction behavior.

**Completed Work:**
- Extract shared helpers for async payload waiting and mutex-protected payload lookup.
- Reuse the helpers from `GetPayloadByID`, `GetPayloadV4ByID`, `GetPayloadV5`, and `GetPayloadV6ByID`.
- Verify the relevant engine test suites after the refactor.

**Verification:**
- `cd pkg && go test ./engine/...`

## Inclusion List Selection Refactor (2026-03-21)

### Current Status: COMPLETE

**Scope:** Deduplicate inclusion-list transaction selection across engine backends and make oversized pending transactions skippable instead of aborting the whole selection pass.

**Completed Work:**
- Extract shared inclusion-list transaction selection into a helper in `pkg/core/eips/inclusion_list.go`.
- Use the helper from both `pkg/engine/backend.go` and `pkg/node/backend/engine_backend.go`.
- Add a focused test for oversized-transaction skipping.
- Verify the relevant Go test suites after the refactor.

**Verification:**
- `cd pkg && go test ./core/eips ./engine/... ./node/backend/...`

## Engine Review Follow-Up (2026-03-21)

### Current Status: COMPLETE

**Scope:** Recheck branch review findings, confirm whether `engine_getPayloadV5` JSON encoding is already fixed, and address the remaining safe bugs in inclusion-list handling.

**Completed Work:**
- Verified that the `engine_getPayloadV5` response encoding issue was already fixed on this branch by `fdd0820`.
- Reject oversized hex input in `flexibleUint64` instead of truncating it.
- Make the node backend return an explicit error for `ProcessInclusionList` until real storage/wiring exists.
- Add tests for the inclusion-list fixes and rerun the relevant engine/node test suites.

**Verification:**
- `cd pkg && go test ./engine/... ./node/backend/...`

## Engine Lock Wrapper Refactor (2026-03-19)

### Current Status: COMPLETE

**Scope:** Refactor `pkg/engine/backend.go` so accesses guarded by `stateMu`, `blocksMu`, `payloadMu`, and `ilMu` go through helper functions instead of open-coded lock/unlock pairs.

**Completed Work:**
- Add backend accessor methods that encapsulate `stateMu`, `blocksMu`, `payloadMu`, and `ilMu` reads/writes.
- Migrate existing call sites in `pkg/engine/backend.go` to the accessors without changing lock ordering or behavior.
- Follow up on the accessor refactor by removing remaining unsynchronized forkchoice field reads in `ForkchoiceUpdated`.
- Extend the accessor refactor into `pkg/engine/backend_bodies.go` to remove direct mutex usage there as well.
- Verify the `pkg/engine` build and tests after the refactor.

**Verification:**
- `cd pkg && go test ./engine/...`

## Go Test Repair (2026-03-19)

### Current Status: COMPLETE

**Scope:** Reproduce and fix failing or hanging `go test ./...` runs from the Go module root at `pkg/`.

**Resolved Issues:**
- `pkg/core/vm` JSON-vector and precompile fixture tests no longer fail when the optional external fixture corpus is absent from the checkout; they now skip cleanly on missing files.
- `pkg/engine` no longer deadlocks while storing processed blocks: `ProcessBlock` and `ProcessBlockV5` were calling `evictOldBlocks` with `blocksMu` already held, while the helper tried to lock `blocksMu` again.

**Verification:**
- `cd pkg && go test ./...`

## Devnet Testing (2026-03-16)

### Current Status: WORKING

**Devnet Configuration:** `full-feature-prysm.yaml` with PeerDAS/Fulu

**Test Results:**
- Slot: 367+ (continuing to advance)
- Finalized Epoch: 9+ (continuing to finalize)
- Block Processing Time: 15-200ms (normal)
- Data Availability Check: ~5 microseconds (instant)

### Issues Fixed

1. **engine_getPayloadV5 "build timed out" (CRITICAL)**
   - Root Cause: `buildMu` mutex held during `StateAtBlock` call which could take tens of seconds on cache miss
   - Death Spiral: state cache miss → state rebuild blocks buildMu → all builds timeout → chain stalls
   - Fix: Pre-fetch state BEFORE acquiring `buildMu`, use singleflight for deduplication
   - File: `pkg/node/backend.go`
   - Status: ✅ RESOLVED

2. **Race condition in computeBlobsV2**
   - Root Cause: `len(b.blobCache)` accessed after `blobCacheMu.RUnlock()`
   - Fix: Capture cache size before releasing lock
   - Status: ✅ RESOLVED

### Known Non-Critical Issues

1. **engine_getBlobsV2 "JSON-RPC response has no result"**
   - This is expected behavior in PeerDAS
   - Blobs are propagated via gossip (data column sidecars), not through Engine API
   - Prysm CL attempts to reconstruct data columns from EL as a fallback
   - EL doesn't have blobs for blocks built by other nodes
   - **Impact: NONE** - Chain continues to advance and finalize normally
   - These errors are just log noise from Prysm's gossip message handling

### Architecture Notes

**PeerDAS Data Flow:**
1. Block proposer (CL-2) broadcasts data column sidecars (128 columns per block)
2. Other CL nodes receive data columns via gossip
3. Data availability is verified from gossip-received columns
4. CL may attempt to reconstruct from EL as fallback (fails but non-fatal)
5. Block processing succeeds regardless

**State Cache Behavior:**
- `StateAtBlock` uses singleflight for deduplication
- Hot cache (`sc`) and permanent cache (`memSC`) provide state caching
- Re-execution needed when cache misses (can take tens of seconds)

---

## Formal Verification

- Formal Lean workspace is present under `formal/lean`.
- Initial EVM/VM model (`Lean2030/VM`) and richer `Lean2030/EVM` model are now linked in `Lean2030/Lean2030.lean`.
- EVM model now includes:
  - arithmetic and bitwise ops,
  - DUP/SWAP,
  - POP/JUMP/JUMPI/JUMPDEST,
  - compiler and theorem suites.
- Current status: still a semantic subset only (toy control-flow, no memory/tracing, no Go cross-checking).
- Added bytecode-offset execution module `formal/lean/Lean2030/EVM/Bytecode.lean` with `decodeAt`/`decodePush`/`runBytecode`.
- Added executable mismatch lemmas showing current op-index `run` diverges from byte-offset EVM behavior for `PUSH`-preceded jumps.
- Lean 4 test driver is now configured in `formal/lean/lakefile.lean` with `Lean2030/Tests.lean`.
- Added `.gitignore` entries for Lean outputs (`.lake`, `*.olean`, `*.ilean`) and linked the Lean VM strategy doc in README.
- Next major deliverable: full `compile`→`runBytecode` refinement theorem and stronger instruction/stack invariants.
