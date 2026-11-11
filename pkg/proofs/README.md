# proofs

Proof aggregation framework supporting ZK-SNARK, ZK-STARK, IPA, and KZG proof systems, with mandatory 3-of-5 block proving and AA validation circuits.

## Overview

The `proofs` package implements the proof infrastructure required for ETH2030's zkVM and execution verification roadmap. It provides a unified registry and aggregation layer over four cryptographic proof systems (ZK-SNARK, ZK-STARK, IPA, KZG), enabling modular composition of provers and verifiers across the node.

The package enforces a mandatory 3-of-5 proof requirement per block, where five provers are deterministically assigned to each block hash and at least three must submit and pass verification within the deadline window. This design aligns with the K+ roadmap milestone for mandatory block proofs with prover penalties for non-compliance.

The package also provides zero-knowledge AA (account abstraction) proof circuits for ERC-4337-style user operations. These circuits enforce three constraints (signature binding, gas limit, nonce) and produce compact proofs that can be batch-verified or compressed for efficient transmission.

## Table of Contents

- [Functionality](#functionality)
- [Subpackages](#subpackages)
- [Usage](#usage)
- [Documentation References](#documentation-references)

## Functionality

### Proof Types

Four proof systems are supported, identified by the `ProofType` constant:

| Constant | Value | Description |
|----------|-------|-------------|
| `ZKSNARK` | 0 | ZK-SNARK (Groth16 / BLS12-381) |
| `ZKSTARK` | 1 | ZK-STARK (FRI-based, algebraic) |
| `IPA`     | 2 | Inner-product argument (Pedersen) |
| `KZG`     | 3 | KZG polynomial commitment |

### Aggregation

`ProofAggregator` is the core interface:

```go
type ProofAggregator interface {
    Aggregate(proofs []ExecutionProof) (*AggregatedProof, error)
    Verify(proof *AggregatedProof) (bool, error)
}
```

Two aggregator implementations are provided:

- `SimpleAggregator` — computes a Merkle root over SHA-256 leaf hashes of each proof using SSZ merkleization.
- `STARKAggregator` — converts proofs into an algebraic execution trace and produces a STARK proof over the trace, using `STARKProver`.

### Mandatory 3-of-5 Proof System

`MandatoryProofSystem` enforces block-level proof coverage:

- `RegisterProver(proverID Hash, proofTypes []string)` — adds a prover to the pool.
- `AssignProvers(blockHash Hash) ([]Hash, error)` — deterministically selects `TotalProvers` provers via `Keccak256(blockHash || proverID)` scoring; result is cached per block.
- `SubmitProof(submission *ProofSubmission) error` — records a proof from an assigned prover.
- `VerifyProof(submission *ProofSubmission) bool` — dispatches to type-specific verification: Groth16 for 192-byte ZK-SNARK proofs; binding commitment check for all others.
- `CheckRequirement(blockHash Hash) *ProofRequirementStatus` — returns submitted/verified counts and satisfaction flag.
- `PenalizeLatePoof(proverID, blockHash Hash) uint64` — returns penalty in Gwei: full penalty for no submission, half for unverified submission.

Default configuration (`DefaultMandatoryProofConfig`): 3 required proofs of 5 total, 32-slot deadline, 1000 Gwei base penalty.

### AA Proof Circuits

`AAProofGenerator` produces zero-knowledge proofs for ERC-4337 user operations:

- `GenerateValidationProof(userOp *UserOperation, entryPoint Address) (*AAProof, error)` — enforces signature binding, gas limit, and nonce constraints; produces a domain-separated Keccak256 commitment chain.
- `VerifyValidationProof(proof *AAProof) bool` — recomputes and compares the proof data commitment.
- `BatchVerifyAAProofs(proofs []*AAProof) (valid, invalid int)` — bulk verification.
- `CompressProof(proof *AAProof) ([]byte, error)` / `DecompressProof(data []byte) (*AAProof, error)` — compact binary serialization: `[version:1][commitment:32][validationHash:32][entryPoint:20][gasUsed:8][dataLen:4][proofData:N]`.

### Proof Registry

`ProverRegistry` manages named `ProofAggregator` instances:

- `Register(name string, agg ProofAggregator) error`
- `Get(name string) (ProofAggregator, error)`
- `Names() []string`

### Execution Proofs

`ExecutionProof` carries a single proof from one prover:

```go
type ExecutionProof struct {
    StateRoot types.Hash
    BlockHash types.Hash
    ProofData []byte
    ProverID  string
    Type      ProofType
}
```

`AggregatedProof` holds the output of aggregation, including the merkle/STARK aggregate root and the validity flag.

### Groth16 / KZG

The `groth16` and `kzg` subpackages provide BLS12-381 Groth16 deserialization and KZG polynomial commitment verification used by `VerifyProof` for ZK-SNARK proofs.

### Recursive and Optional Proofs

`RecursiveAggregator` and `OptionalProofSystem` extend the core model for nested proof verification and optional proof submission policies.

## Subpackages

| Subpackage | Description |
|------------|-------------|
| [`kzg/`](./kzg/) | KZG polynomial commitment verifier |
| [`prover/`](./prover/) | Prover backend compatibility shims |
| [`queue/`](./queue/) | Proof submission queue compatibility shims |

## Usage

```go
// Create a mandatory proof system.
sys := proofs.NewMandatoryProofSystem(proofs.DefaultMandatoryProofConfig())

// Register provers.
sys.RegisterProver(proverID, []string{"ZK-SNARK", "KZG"})

// Assign provers to a block and submit proofs.
assigned, _ := sys.AssignProvers(blockHash)
sys.SubmitProof(&proofs.ProofSubmission{
    ProverID:  assigned[0],
    ProofType: "ZK-SNARK",
    ProofData: proofBytes,
    BlockHash: blockHash,
})
sys.VerifyProof(&proofs.ProofSubmission{ /* same fields */ })

status := sys.CheckRequirement(blockHash)
// status.IsSatisfied == true when Verified >= RequiredProofs

// Aggregate execution proofs.
agg := proofs.NewSimpleAggregator()
aggregated, _ := agg.Aggregate(executionProofs)
valid, _ := agg.Verify(aggregated)

// Generate an AA validation proof.
gen := proofs.NewAAProofGenerator(proofs.DefaultAAProofConfig())
proof, _ := gen.GenerateValidationProof(userOp, entryPointAddr)
compressed, _ := proofs.CompressProof(proof)
```

## Documentation References

- [Roadmap](../../docs/ROADMAP.md)
- [GAP Analysis](../../docs/GAP_ANALYSIS.md)
- EIP-8025: Execution witness proofs
- K+ roadmap: mandatory 3-of-5 proofs
- M+ roadmap: canonical zkVM, proof aggregation
