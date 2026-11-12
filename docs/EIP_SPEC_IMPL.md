# EIP Specification & Implementation Traceability

> Last updated: 2026-02-22

Complete traceability matrix mapping every EIP to its specification, implementation, tests, and roadmap position. ETH2030 implements **94+ EIPs** across all upgrade phases.

---

## Quick Summary

| Category | Count |
|----------|-------|
| EIPs Complete | 58 |
| EIPs Substantial | 5 |
| EIPs Referenced/Partial | 30+ |
| Total unique EIPs | 94+ |
| EF State Tests | 36,126/36,126 (100%) |

---

## Glamsterdam (2026, ~99% complete)

### Core EIPs

| EIP | Title | Status | Spec | Implementation | Tests |
|-----|-------|--------|------|----------------|-------|
| 7928 | Block Access Lists (BALs) | Complete | [EIP-7928](https://eips.ethereum.org/EIPS/eip-7928) | `bal/types.go`, `bal/scheduler_pipeline.go`, `bal/conflict_detector_advanced.go`, `core/state/bals_engine.go` | `core/bal_integration_test.go`, `bal/*_test.go` |
| 7702 | Set Code Authorization | Complete | [EIP-7702](https://eips.ethereum.org/EIPS/eip-7702) | `core/eip7702.go`, `core/types/tx_setcode.go`, `core/vm/precompile_7702.go`, `p2p/setcode_broadcast.go` | `core/eip7702_test.go`, `core/vm/precompile_7702_test.go` |
| 7701 | Native Account Abstraction | Complete | [EIP-7701](https://eips.ethereum.org/EIPS/eip-7701) | `core/types/tx_aa.go`, `core/vm/eip7701_opcodes.go`, `core/vm/aa_executor.go`, `core/aa_entrypoint.go` | `core/vm/eip7701_opcodes_test.go` |
| 8070 | Sparse Blob Pool | Complete | [EIP-8070](https://eips.ethereum.org/EIPS/eip-8070) | `txpool/blobpool.go`, `txpool/sparse_blobpool.go`, `das/sparse_blobpool.go` | `txpool/blobpool_test.go`, `txpool/sparse_blobpool_test.go` |
| 8141 | Frame Transactions | Complete | [EIP-8141](https://eips.ethereum.org/EIPS/eip-8141) | `core/types/tx_frame.go`, `core/vm/eip8141_opcodes.go`, `core/frame_execution.go` | `core/vm/eip8141_opcodes_test.go` |
| 7685 | Execution Layer Requests | Complete | [EIP-7685](https://eips.ethereum.org/EIPS/eip-7685) | `core/types/request.go` | `core/withdrawal_test.go` |
| 7708 | ETH Transfers Emit Log | Complete | [EIP-7708](https://eips.ethereum.org/EIPS/eip-7708) | `core/vm/eip7708.go`, `core/vm/eip7708_deep.go` | `core/vm/eip7708_test.go` |
| 7918 | Blob Base Fee Floor | Complete | [EIP-7918](https://eips.ethereum.org/EIPS/eip-7918) | `core/blob_gas.go` | `core/blob_gas_test.go` |

### Glamsterdam Repricing (18 EIPs)

| EIP | Title | Status | Spec | Implementation | Tests |
|-----|-------|--------|------|----------------|-------|
| 7904 | Precompile Gas Repricing | Complete | [EIP-7904](https://eips.ethereum.org/EIPS/eip-7904) | `core/vm/precompiles.go` (lines 582-641), `core/vm/gas.go` (lines 72-79) | `core/vm/glamsterdan_gas_test.go` |
| 7778 | Remove SSTORE Gas Refunds | Complete | [EIP-7778](https://eips.ethereum.org/EIPS/eip-7778) | `core/vm/instructions.go` (opSstoreGlamst), `core/vm/gas_table.go` | `core/eip7778_test.go` |
| 2780 | Reduce Intrinsic Gas | Complete | [EIP-2780](https://eips.ethereum.org/EIPS/eip-2780) | `core/vm/gas_table.go`, `core/glamsterdam_repricing.go` | `core/eip2780_test.go` |
| 8037 | State Creation Gas Increase | Complete | [EIP-8037](https://eips.ethereum.org/EIPS/eip-8037) | `core/vm/gas_table.go`, `core/vm/repricing.go` | `core/eip8037_test.go` |
| 8038 | State Access Gas Increase | Complete | [EIP-8038](https://eips.ethereum.org/EIPS/eip-8038) | `core/vm/gas_table.go`, `core/vm/repricing.go` | `core/eip8038_test.go` |
| 7623 | Calldata Floor Pricing | Complete | [EIP-7623](https://eips.ethereum.org/EIPS/eip-7623) | `core/eip7623_floor.go` | `core/eip7623_test.go` |
| 7976 | Calldata Floor Revision | Complete | [EIP-7976](https://eips.ethereum.org/EIPS/eip-7976) | `core/eip7623_floor.go` (CalcFloorGasGlamst) | `core/eip7976_test.go` |
| 7981 | Access List Floor Cost | Complete | Draft | `core/processor.go`, `core/chain_config.go` | via processor tests |
| 7997 | Deterministic CREATE2 Factory | Complete | [EIP-7997](https://eips.ethereum.org/EIPS/eip-7997) | `core/eip7997.go` | `core/eip7997_test.go` |
| 7610 | Revert Creation Non-Empty Storage | Complete | [EIP-7610](https://eips.ethereum.org/EIPS/eip-7610) | `core/vm/eip7610.go` | `core/vm/eip7610_test.go` |
| 7954 | Increased Max Code Size | Complete | [EIP-7954](https://eips.ethereum.org/EIPS/eip-7954) | `core/vm/eip7954.go`, `core/vm/eip7954_deep.go` | `core/vm/eip7954_test.go`, `core/vm/eip7954_deep_test.go` |

### Glamsterdam Opcodes

| EIP | Title | Status | Spec | Implementation | Tests |
|-----|-------|--------|------|----------------|-------|
| 7939 | CLZ Opcode | Complete | [EIP-7939](https://eips.ethereum.org/EIPS/eip-7939) | `core/vm/opcodes.go` (CLZ=0x1e), `core/vm/instructions.go` (opCLZ) | `core/vm/new_opcodes_test.go` |
| 8024 | DUPN/SWAPN/EXCHANGE | Complete | [EIP-8024](https://eips.ethereum.org/EIPS/eip-8024) | `core/vm/opcodes.go`, `core/vm/instructions.go` | `core/vm/new_opcodes_test.go` |
| 7843 | SLOTNUM Opcode | Complete | Draft | `core/vm/opcodes.go` (SLOTNUM=0x4b) | `core/vm/new_opcodes_test.go` |

### Glamsterdam P2P

| EIP | Title | Status | Spec | Implementation | Tests |
|-----|-------|--------|------|----------------|-------|
| 7975 | Partial Block Receipt Lists (ETH/70) | Substantial | Draft | `eth/protocol.go`, `eth/handler.go`, `p2p/ethproto/protocol.go` | `eiptest/eip7975_test.go`, `eth/handler_test.go` |
| 8159 | Block Access List Exchange (ETH/71) | Substantial | Draft | `eth/protocol_extended.go`, `p2p/ethproto/protocol.go` | `eiptest/eip8159_test.go` |

---

## Hegotá (2026-2027, ~85% complete)

| EIP | Title | Status | Spec | Implementation | Tests |
|-----|-------|--------|------|----------------|-------|
| 7742 | Uncoupled Blob Count | Complete | [EIP-7742](https://eips.ethereum.org/EIPS/eip-7742) | `core/types/header.go` (TargetBlobsPerBlock), `core/blob_gas.go`, `engine/payload_attributes_v4.go` | `core/blob_schedule_test.go` |
| 7898 | Uncoupled Execution Payload | Complete | [EIP-7898](https://eips.ethereum.org/EIPS/eip-7898) | `engine/engine_uncoupled.go` | `engine/engine_glamsterdam_test.go` |
| 7706 | Separate Calldata Gas Dimension | Complete | [EIP-7706](https://eips.ethereum.org/EIPS/eip-7706) | `core/calldata_gas.go`, `core/multidim.go`, `core/multidim_gas.go` | `core/multidim_test.go`, `core/calldata_gas_test.go` |
| 7999 | Multidimensional Gas Extension | Complete | Draft | `core/multidim.go` | `core/multidim_test.go` |
| 6404 | SSZ Transactions | Substantial | [EIP-6404](https://eips.ethereum.org/EIPS/eip-6404) | `core/types/tx_ssz.go`, `core/types/block_ssz.go` | `core/types/tx_ssz_test.go`, `core/types/block_ssz_test.go` |
| 7807 | SSZ Blocks | Substantial | [EIP-7807](https://eips.ethereum.org/EIPS/eip-7807) | `core/types/block_ssz.go` | `core/types/block_ssz_test.go` |
| 7745 | Log Index | Complete | [EIP-7745](https://eips.ethereum.org/EIPS/eip-7745) | `core/types/log_index.go` | `core/types/log_index_test.go` |
| 7916 | SSZ ProgressiveList | Complete | [EIP-7916](https://eips.ethereum.org/EIPS/eip-7916) | `ssz/progressive_list.go`, `ssz/progressive_encoder.go` | `ssz/progressive_list_test.go`, `ssz/progressive_encoder_test.go` |
| 8077 | Announce Nonce (ETH/72) | Complete | [EIP-8077](https://eips.ethereum.org/EIPS/eip-8077) | `eth/announce_nonce.go`, `p2p/announce_nonce.go` | via eth tests |
| 7691 | Blob Throughput Increase | Complete | [EIP-7691](https://eips.ethereum.org/EIPS/eip-7691) | `core/blob_schedule.go`, `core/blob_gas.go` | `core/eip7691_test.go`, `core/blob_schedule_test.go` |
| 7495 | SSZ StableContainer | Functional | [EIP-7495](https://eips.ethereum.org/EIPS/eip-7495) | `ssz/stable_container.go` | `ssz/stable_container_test.go` |

---

## I+ (2027, ~80% complete)

| EIP | Title | Status | Spec | Implementation | Tests |
|-----|-------|--------|------|----------------|-------|
| 8079 | Native Rollups | Substantial | [EIP-8079](https://eips.ethereum.org/EIPS/eip-8079) | `rollup/execute.go`, `rollup/anchor.go`, `rollup/native_rollup.go`, `rollup/bridge.go` | `rollup/native_rollup_test.go`, `rollup/execute_test.go` |
| 7251 | Max Effective Balance (2048 ETH) | Complete | [EIP-7251](https://eips.ethereum.org/EIPS/eip-7251) | `consensus/validator.go`, `consensus/consolidation.go`, `consensus/consolidation_manager.go` | `consensus/consolidation_test.go`, `consensus/validator_test.go` |
| 7549 | Attestation Committee Index | Complete | [EIP-7549](https://eips.ethereum.org/EIPS/eip-7549) | `consensus/eip7549.go`, `consensus/attestation.go`, `consensus/attestation_aggregator.go` | via consensus tests |
| 4762 | Verkle Gas Cost Changes | Complete | [EIP-4762](https://eips.ethereum.org/EIPS/eip-4762) | `core/vm/eip4762_gas.go`, `core/state/access_events.go` | `core/vm/eip4762_gas_test.go` |
| 8025 | Execution Proofs | Substantial | [EIP-8025](https://eips.ethereum.org/EIPS/eip-8025) | `witness/block_witness.go`, `witness/producer.go`, `witness/verifier.go` | `witness/verifier_test.go` |
| 7864 | Binary Merkle Trie | Substantial | [EIP-7864](https://eips.ethereum.org/EIPS/eip-7864) | `trie/bintrie/bintrie.go` | `trie/bintrie/*_test.go` |
| 7594 | PeerDAS | Complete | [EIP-7594](https://eips.ethereum.org/EIPS/eip-7594) | `das/types.go`, `das/custody_manager.go`, `das/column_builder.go`, `das/column_sampling.go` | `das/*_test.go` |

---

## J+ (2027-2028, ~75% complete)

| EIP | Title | Status | Spec | Implementation | Tests |
|-----|-------|--------|------|----------------|-------|
| 7732 | Enshrined PBS (ePBS) | Substantial | [EIP-7732](https://eips.ethereum.org/EIPS/eip-7732) | `epbs/types.go`, `epbs/bid_escrow.go`, `epbs/commitment_reveal.go`, `engine/engine_epbs.go` | `epbs/*_test.go` |
| 7805 | FOCIL (Inclusion Lists) | Substantial | [EIP-7805](https://eips.ethereum.org/EIPS/eip-7805) | `focil/types.go`, `focil/builder.go`, `focil/validation.go`, `focil/compliance_tracker.go` | `focil/*_test.go` |
| 7547 | Inclusion Lists | Complete | [EIP-7547](https://eips.ethereum.org/EIPS/eip-7547) | `core/types/inclusion_list.go`, `core/inclusion_list.go` | via focil tests |

---

## K+ (2028, ~80% complete)

| Feature | Related EIPs | Status | Implementation | Tests |
|---------|-------------|--------|----------------|-------|
| Quick Slots (6-sec) | Framework-level | Functional | `consensus/quick_slots.go` | `consensus/quick_slots_test.go` |
| SSF (Single-Slot Finality) | Framework-level | Functional | `consensus/finality.go` | `consensus/finality_test.go` |
| 4-Slot Epochs | Framework-level | Functional | `consensus/quick_slots.go` (SlotsPerEpoch=4) | `consensus/quick_slots_test.go` |
| 1-Epoch Finality | Framework-level | Functional | `consensus/finality.go` (singleEpochMode) | `consensus/finality_test.go` |
| 1M Attestations/Slot | Framework-level | Functional | `consensus/attestation_scaler.go`, `consensus/parallel_bls.go`, `consensus/committee_subnet.go`, `consensus/batch_verifier.go` | `consensus/attestation_scaler_test.go`, `consensus/batch_verifier_test.go` |
| Mandatory 3-of-5 Proofs | Framework-level | Functional | `proofs/mandatory_proofs.go`, `proofs/mandatory.go` | `proofs/mandatory_test.go` |
| Canonical Guest | Framework-level | Functional | `zkvm/canonical.go`, `zkvm/canonical_executor.go` | `zkvm/canonical_test.go` |
| CL Proof Circuits | Framework-level | Functional | `consensus/cl_proof_circuits.go`, `light/cl_proofs.go` | `consensus/cl_proof_circuits_test.go` |

---

## L+ (2029, ~80% complete)

| Feature | Related EIPs | Status | Implementation | Tests |
|---------|-------------|--------|----------------|-------|
| Endgame Finality | Framework-level | Functional | `consensus/endgame_finality.go`, `consensus/finality_bls_adapter.go` | `consensus/endgame_finality_test.go` |
| PQ Attestations | PQC suite | Functional | `consensus/pq_attestation.go`, `consensus/pq_chain_security.go` | `consensus/pq_attestation_test.go` |
| Attester Stake Cap | Framework-level | Functional | `consensus/attester_cap.go`, `consensus/attester_cap_extended.go` | `consensus/attester_cap_test.go` |
| APS Committee Selection | Framework-level | Functional | `consensus/aps.go` | `consensus/aps_test.go` |
| Blob Streaming | Framework-level | Functional | `das/streaming.go`, `das/streaming_proto.go` | `das/streaming_test.go` |
| Custody Proofs | Framework-level | Functional | `das/custody_proof.go`, `das/custody_verify.go` | `das/custody_proof_test.go` |
| 1 ETH Includers | Framework-level | Functional | `consensus/one_eth_includers.go` | `consensus/one_eth_includers_test.go` |
| jeanVM Aggregation | Framework-level | Functional | `consensus/jeanvm_aggregation.go` | `consensus/jeanvm_aggregation_test.go` |
| Secret Proposers | Framework-level | Functional | `consensus/secret_proposer.go`, `consensus/vrf_election.go` | `consensus/secret_proposer_test.go` |
| Distributed Block Building | Framework-level | Functional | `consensus/dist_builder.go`, `engine/distributed_builder.go` | `engine/distributed_builder_test.go` |

---

## M+ (2029+, ~75% complete)

| Feature | Related EIPs | Status | Implementation | Tests |
|---------|-------------|--------|----------------|-------|
| Gigagas L1 (1 Ggas/sec) | Framework-level | Functional | `core/gigagas.go`, `core/gigagas_integration.go` | `core/gigagas_test.go` |
| Canonical zkVM | Framework-level | Functional | `zkvm/riscv_cpu.go`, `zkvm/canonical_executor.go`, `zkvm/proof_backend.go` | `zkvm/riscv_cpu_test.go` |
| Gas Futures Market | Framework-level | Functional | `core/vm/gas_futures_long.go`, `core/gas_market.go` | `core/gas_market_test.go` |
| PQ Transactions | 7932, 8051 | Partial | `core/types/pq_transaction.go`, `crypto/pqc/pq_tx_signer.go` | `core/types/pq_transaction_test.go` |
| Sharded Mempool | Framework-level | Functional | `txpool/sharding.go`, `txpool/shared/` | `txpool/sharding_test.go` |
| VDF Randomness | Framework-level | Functional | `crypto/vdf.go`, `consensus/vdf_consensus.go` | `crypto/vdf_test.go` |
| Private L1 Shielded | Framework-level | Functional | `crypto/shielded_circuit.go`, `crypto/nullifier_set.go` | `crypto/shielded_circuit_test.go` |

---

## Pre-Glamsterdam Foundational EIPs (All Complete)

### Frontier-Constantinople

| EIP | Title | Spec | Implementation | Tests |
|-----|-------|------|----------------|-------|
| 150 | Gas Cost Changes | [EIP-150](https://eips.ethereum.org/EIPS/eip-150) | `core/vm/gas_table.go` | `core/vm/gas_table_test.go` |
| 152 | BLAKE2b Precompile | [EIP-152](https://eips.ethereum.org/EIPS/eip-152) | `core/vm/precompiles.go` (blake2F) | `core/vm/precompiles_test.go` |
| 170 | Contract Size Limit | [EIP-170](https://eips.ethereum.org/EIPS/eip-170) | `core/vm/gas_table.go` (MaxCodeSize) | via vm tests |
| 196 | BN256 Point Addition | [EIP-196](https://eips.ethereum.org/EIPS/eip-196) | `core/vm/precompiles.go` (bn256Add) | `core/vm/precompiles_test.go` |
| 197 | BN256 Pairing Check | [EIP-197](https://eips.ethereum.org/EIPS/eip-197) | `core/vm/precompiles.go` (bn256Pairing) | `core/vm/precompiles_test.go` |
| 1108 | Istanbul BN256 Repricing | [EIP-1108](https://eips.ethereum.org/EIPS/eip-1108) | `core/vm/precompiles.go` | via vm tests |

### Istanbul-London

| EIP | Title | Spec | Implementation | Tests |
|-----|-------|------|----------------|-------|
| 1153 | Transient Storage | [EIP-1153](https://eips.ethereum.org/EIPS/eip-1153) | `core/vm/instructions.go` (opTload, opTstore) | `core/vm/instructions_test.go` |
| 1559 | EIP-1559 Fee Market | [EIP-1559](https://eips.ethereum.org/EIPS/eip-1559) | `core/types/transaction.go` (DynamicFeeTx), `core/fee.go` | `core/vm/gas_accounting_test.go` |
| 2200 | SSTORE Gas Metering | [EIP-2200](https://eips.ethereum.org/EIPS/eip-2200) | `core/vm/evm_storage_ops.go`, `core/vm/gas_table_ext.go` | `core/vm/gas_table_test.go` |
| 2537 | BLS12-381 Precompiles (9x) | [EIP-2537](https://eips.ethereum.org/EIPS/eip-2537) | `core/vm/precompiles_bls.go`, `crypto/bls12381.go` | `core/vm/precompiles_bls_test.go` |
| 2718 | Typed Transaction Envelope | [EIP-2718](https://eips.ethereum.org/EIPS/eip-2718) | `core/types/transaction.go`, `core/types/transaction_rlp.go` | `core/types/fuzz_test.go` |
| 2929 | Cold/Warm Storage Gas | [EIP-2929](https://eips.ethereum.org/EIPS/eip-2929) | `core/vm/gas_eip2929.go`, `core/state/access_list.go` | `core/vm/gas_table_test.go` |
| 2930 | Access List Transactions | [EIP-2930](https://eips.ethereum.org/EIPS/eip-2930) | `core/types/transaction.go` (AccessListTx) | via types tests |
| 2935 | Historical Block Hashes | [EIP-2935](https://eips.ethereum.org/EIPS/eip-2935) | `core/eip2935.go` | `core/eip2935_test.go` |
| 3529 | Reduced Gas Refunds | [EIP-3529](https://eips.ethereum.org/EIPS/eip-3529) | `core/vm/gas_table.go`, `core/vm/evm_storage_ops.go` | `core/vm/gas_table_test.go` |
| 3540 | EOF Container Format | [EIP-3540](https://eips.ethereum.org/EIPS/eip-3540) | `core/vm/eof.go` | `core/vm/eof_opcodes_test.go` |

### Shanghai-Cancun

| EIP | Title | Spec | Implementation | Tests |
|-----|-------|------|----------------|-------|
| 3855 | PUSH0 Opcode | [EIP-3855](https://eips.ethereum.org/EIPS/eip-3855) | `core/vm/evm_stack_ops.go` | via vm tests |
| 3860 | Limit/Meter Init Code | [EIP-3860](https://eips.ethereum.org/EIPS/eip-3860) | `core/vm/gas_table.go`, `core/vm/evm_create.go` | via vm tests |
| 4444 | History Pruning | [EIP-4444](https://eips.ethereum.org/EIPS/eip-4444) | `core/rawdb/history.go`, `p2p/portal/history.go` | `p2p/portal/history_test.go` |
| 4788 | Beacon Root in EVM | [EIP-4788](https://eips.ethereum.org/EIPS/eip-4788) | `core/beacon_root.go` | `core/beacon_root_test.go` |
| 4844 | Blob Transactions + KZG | [EIP-4844](https://eips.ethereum.org/EIPS/eip-4844) | `core/types/blob_tx.go`, `core/vm/precompiles.go` (kzgPointEval), `crypto/kzg.go` | `core/types/blob_tx_test.go`, `crypto/kzg_integration_test.go` |
| 4895 | Beacon Chain Withdrawals | [EIP-4895](https://eips.ethereum.org/EIPS/eip-4895) | `core/types/withdrawal.go`, `engine/withdrawal_processor.go` | `core/withdrawal_test.go` |
| 5656 | MCOPY Opcode | [EIP-5656](https://eips.ethereum.org/EIPS/eip-5656) | `core/vm/instructions.go` (opMcopy) | `core/vm/instructions_test.go` |
| 6110 | On-Chain Deposits | [EIP-6110](https://eips.ethereum.org/EIPS/eip-6110) | `core/eip6110.go`, `core/types/deposit.go` | `core/eip6110_test.go` |
| 6780 | SELFDESTRUCT Restriction | [EIP-6780](https://eips.ethereum.org/EIPS/eip-6780) | `core/vm/purges.go`, `core/vm/instructions.go` | `core/vm/purges_test.go` |

### Prague

| EIP | Title | Spec | Implementation | Tests |
|-----|-------|------|----------------|-------|
| 7002 | Withdrawal Requests via EL | [EIP-7002](https://eips.ethereum.org/EIPS/eip-7002) | `core/eip7002.go` | `core/eip7002_test.go` |
| 7069 | Revamped CALL (EXTCALL etc.) | [EIP-7069](https://eips.ethereum.org/EIPS/eip-7069) | `core/vm/eof_opcodes.go`, `core/vm/evm_call_handlers.go` | `core/vm/eof_opcodes_test.go` |
| 7212 | P256VERIFY Precompile | [EIP-7212](https://eips.ethereum.org/EIPS/eip-7212) | `core/vm/precompiles.go` (p256Verify), `crypto/p256.go` | `core/vm/precompiles_test.go` |
| 7480 | EOF Data Section Access | [EIP-7480](https://eips.ethereum.org/EIPS/eip-7480) | `core/vm/eof_opcodes.go` | `core/vm/eof_opcodes_test.go` |
| 7516 | BLOBBASEFEE Opcode | [EIP-7516](https://eips.ethereum.org/EIPS/eip-7516) | `core/vm/instructions.go` (opBlobBaseFee) | `core/vm/instructions_test.go` |
| 7620 | EOF EOFCREATE/RETURNCONTRACT | [EIP-7620](https://eips.ethereum.org/EIPS/eip-7620) | `core/vm/eof_opcodes.go` | `core/vm/eof_opcodes_test.go` |
| 7698 | EOF Creation Transaction | [EIP-7698](https://eips.ethereum.org/EIPS/eip-7698) | `core/types/tx_eof.go` | via types tests |
| 7825 | Transaction Gas Limit Cap | [EIP-7825](https://eips.ethereum.org/EIPS/eip-7825) | `core/gas_cap.go` | `core/gas_cap_extended_test.go` |

---

## Post-Quantum Cryptography (PQC Suite)

| EIP | Title | Status | Spec | Implementation | Tests |
|-----|-------|--------|------|----------------|-------|
| 7932 | PQ Algorithm Registry | Referenced | [EIP-7932](https://eips.ethereum.org/EIPS/eip-7932) | `crypto/pqc/pq_algorithm_registry.go` | `crypto/pqc/pq_algorithm_registry_test.go` |
| 8051 | PQ Signature Gas Costs | Referenced | [EIP-8051](https://eips.ethereum.org/EIPS/eip-8051) | `core/types/pq_tx_validation.go` | via pqc tests |
| - | ML-DSA-65 (FIPS 204) | Real impl | FIPS 204 | `crypto/pqc/mldsa_signer.go` | `crypto/pqc/mldsa_signer_test.go` |
| - | Dilithium3 | Stub | CRYSTALS-Dilithium | `crypto/pqc/dilithium.go` | `crypto/pqc/dilithium_test.go` |
| - | Falcon-512 | Stub (Sign) | FALCON | `crypto/pqc/falcon.go` | `crypto/pqc/falcon_test.go` |
| - | SPHINCS+-SHA256 | Stub (Sign) | SPHINCS+ | `crypto/pqc/sphincs.go` | `crypto/pqc/sphincs_test.go` |

---

## Infrastructure EIPs

| EIP | Title | Status | Implementation | Tests |
|-----|-------|--------|----------------|-------|
| 778 | Ethereum Node Records | Complete | `p2p/enr/enr.go` | `p2p/enr/enr_test.go` |
| 1186 | eth_getProof | Complete | `rpc/api_proof.go`, `rpc/eth_api_state.go` | via rpc tests |
| 1459 | DNS-Based Discovery | Complete | `p2p/dnsdisc/client.go` | `p2p/dnsdisc/client_test.go` |
| 2124 | Fork ID | Complete | `p2p/forkid.go` | `p2p/forkid_test.go` |
| 4337 | AA Entry Point (predecessor) | Referenced | `core/aa_entrypoint.go` | via core tests |

---

## go-ethereum Integration

The `pkg/geth/` adapter package bridges ETH2030 to go-ethereum v1.17.0:

| Component | File | Description |
|-----------|------|-------------|
| Type conversion | `geth/types.go` | Address, Hash, uint256, AccessList, Log conversion |
| Chain config | `geth/config.go` | Fork-aware ChainConfig mapping (Frontier-Prague) |
| Pre-state | `geth/prestate.go` | Create go-ethereum StateDB from ETH2030 accounts |
| State transition | `geth/transition.go` | ApplyMessage, MakeBlockContext, EffectiveGasPrice |
| Block processor | `geth/processor.go` | GethBlockProcessor with custom precompile injection |
| Custom precompiles | `geth/extensions.go` | 13 ETH2030 precompiles injected via SetPrecompiles() |
| Precompile adapters | `core/vm/precompile_adapters.go` | Exported wrappers for unexported precompile types |
| EF test runner | `core/eftest/geth_runner.go` | 36,126/36,126 tests passing (100%) |

### Custom Precompile Injection

| Category | Count | Addresses | Fork |
|----------|-------|-----------|------|
| Glamsterdam repricing | 4 | 0x06, 0x08, 0x09, 0x0a | Glamsterdam |
| NTT | 1 | 0x15 | I+ |
| NII (Number-Theoretic) | 4 | 0x0201-0x0204 | I+ |
| Field arithmetic | 4 | 0x0205-0x0208 | I+ |

**Note**: go-ethereum's `operation` struct is unexported, so 26 custom opcodes (CLZ, DUPN, APPROVE, TXPARAM*, EOF, AA) remain ETH2030-native-EVM-only.

---

## Roadmap Feature Coverage Matrix

All 65 roadmap items mapped to their EIP and implementation status:

### Consensus Layer (22 items)

| # | Feature | EIP(s) | Status | Key Implementation |
|---|---------|--------|--------|-------------------|
| 1 | Fast confirmation | - | Functional | `consensus/fast_confirm.go` |
| 2 | Quick slots (6-sec) | - | Functional | `consensus/quick_slots.go` |
| 3 | 1-epoch finality | - | Functional | `consensus/finality.go` |
| 4 | 4-slot epochs | - | Functional | `consensus/quick_slots.go` |
| 5 | Endgame finality | - | Functional | `consensus/endgame_finality.go` |
| 6 | Fast L1 finality (sec) | - | **Partial** | `consensus/endgame_engine.go` |
| 7 | Modernized beacon specs | - | Functional | `consensus/unified_beacon_state.go` |
| 8 | Attester stake cap | - | Functional | `consensus/attester_cap.go` |
| 9 | 128K attester cap | - | Functional | `consensus/attester_cap.go` |
| 10 | APS | - | Functional | `consensus/aps.go` |
| 11 | 1 ETH includers | - | Functional | `consensus/one_eth_includers.go` |
| 12 | Tech debt reset | - | **Partial** | `consensus/tech_debt_reset.go` |
| 13 | PQ attestations | 7932 | Functional | `consensus/pq_attestation.go` |
| 14 | jeanVM aggregation | - | Functional | `consensus/jeanvm_aggregation.go` |
| 15 | 1M attestations/slot | - | Functional | `consensus/attestation_scaler.go` |
| 16 | 51% attack recovery | - | Functional | `consensus/attack_recovery.go` |
| 17 | Distributed building | - | Functional | `consensus/dist_builder.go` |
| 18 | VDF randomness | - | Functional | `crypto/vdf.go` |
| 19 | Secret proposers | - | Functional | `consensus/secret_proposer.go` |
| 20 | PQ pubkey registry | 7932 | Functional | `crypto/pqc/pubkey_registry.go` |
| 21 | PQ available chain | - | Functional | `consensus/pq_chain_security.go` |
| 22 | Real-time CL proofs | 8025 | Functional | `consensus/cl_proof_circuits.go` |

### Data Layer (12 items)

| # | Feature | EIP(s) | Status | Key Implementation |
|---|---------|--------|--------|-------------------|
| 23 | Sparse blobpool | 8070 | **Complete** | `das/sparse_blobpool.go` |
| 24 | Cell-level messages | 7594 | Functional | `das/cell_messages.go` |
| 25 | EIP-7702 broadcast | 7702 | Functional | `p2p/setcode_broadcast.go` |
| 26 | BPO blobs increase | 7691 | Functional | `das/rpo_slots.go` |
| 27 | Local blob reconstruction | - | Functional | `das/blob_reconstruct.go` |
| 28 | Decrease sample size | 7594 | Functional | `das/sample_optimize.go` |
| 29 | Blob streaming | - | Functional | `das/streaming.go` |
| 30 | Short-dated blob futures | - | Functional | `das/blob_futures.go` |
| 31 | PQ blobs | - | **Partial** | `das/pq_blobs.go` |
| 32 | Variable-size blobs | - | Functional | `das/varblob.go` |
| 33 | Proofs of custody | - | Functional | `das/custody_proof.go` |
| 34 | Teragas L2 | - | **Partial** | `das/teradata.go` |

### Execution Layer (31 items)

| # | Feature | EIP(s) | Status | Key Implementation |
|---|---------|--------|--------|-------------------|
| 35 | Glamsterdam repricing | 7904,7778,2780,8037,8038,7623,7976 | **Complete** | `core/glamsterdam_repricing.go` |
| 36 | Optional proofs | 8025 | Functional | `proofs/optional.go` |
| 37 | Hegotá repricing | - | Functional | `core/hogota_repricing.go` |
| 38 | 3x/year gas limit | - | Functional | `core/gas_limit.go` |
| 39 | Multidim pricing | 7706,7999 | Functional | `core/multidim_gas.go` |
| 40 | Payload chunking | - | Functional | `core/payload_chunking.go` |
| 41 | Block in blobs | - | Functional | `core/block_in_blobs.go` |
| 42 | Announce nonce | 8077 | Functional | `eth/announce_nonce.go` |
| 43 | Mandatory proofs | - | Functional | `proofs/mandatory_proofs.go` |
| 44 | Canonical guest | - | Functional | `zkvm/canonical.go` |
| 45 | Canonical zkVM | - | Functional | `zkvm/riscv_cpu.go` |
| 46 | Long-dated gas futures | - | Functional | `core/vm/gas_futures_long.go` |
| 47 | Sharded mempool | - | Functional | `txpool/sharding.go` |
| 48 | Gigagas L1 | - | Functional | `core/gigagas.go` |
| 49 | BALs | 7928 | **Complete** | `bal/` |
| 50 | Binary tree | 7864 | **Complete** | `trie/bintrie/` |
| 51 | Validity-only state | - | Functional | `core/vops/` |
| 52 | Endgame state | - | Functional | `core/state/endgame_state.go` |
| 53 | Native AA | 7701,7702 | Functional | `core/vm/aa_executor.go` |
| 54 | Misc purges | 6780 | Functional | `core/state/misc_purges.go` |
| 55 | Tx assertions | - | Functional | `core/tx_assertions.go` |
| 56 | NTT precompile | - | Functional | `core/vm/precompile_ntt.go` |
| 57 | Precompiles in zkISA | - | Functional | `core/vm/zkisa_precompiles.go` |
| 58 | STF in zkISA | - | Functional | `zkvm/stf.go` |
| 59 | Native rollups | 8079 | Functional | `rollup/` |
| 60 | Proof aggregation | - | Functional | `proofs/aggregation.go` |
| 61 | Exposed zkISA | - | Functional | `zkvm/zkisa_bridge.go` |
| 62 | AA proofs | - | Functional | `proofs/aa_proof_circuits.go` |
| 63 | Encrypted mempool | - | Functional | `txpool/encrypted/` |
| 64 | PQ transactions | 7932,8051 | **Partial** | `core/types/pq_transaction.go` |
| 65 | Private L1 shielded | - | Functional | `crypto/shielded_circuit.go` |

---

## Coverage Summary

| Metric | Value |
|--------|-------|
| Roadmap items: COMPLETE | 12/65 |
| Roadmap items: FUNCTIONAL | 48/65 |
| Roadmap items: PARTIAL | 5/65 |
| Roadmap items: MISSING | 0/65 |
| EIPs: Complete | 58 |
| EIPs: Substantial | 5 |
| Total unique EIPs referenced | 94+ |
| EF State Tests (go-ethereum backend) | 36,126/36,126 (100%) |
| Custom precompiles (geth-injected) | 13 |
| Test functions | 18,066 |
| Source files | 986 |
| Test files | 916 |
| Total LOC | ~702K |
