# crypto

Cryptographic primitives for the ETH2030 Ethereum client.

## Overview

The root package and its subpackages provide classical signing, Ethereum curve and KZG operations, Verkle polynomial openings, post-quantum signing and KEM, prime-field share reconstruction, and class-group verifiable delay functions.

## Table of Contents

- [Functionality](#functionality)
- [Subpackages](#subpackages)
- [Usage](#usage)
- [Documentation References](#documentation-references)

## Functionality

### Keccak-256

`Keccak256(data ...[]byte) []byte` and `Keccak256Hash(data ...[]byte) types.Hash`
compute the Ethereum-standard Keccak-256 hash backed by `golang.org/x/crypto/sha3`.
Extended variants in `keccak_extended.go` support domain separation and incremental
hashing.

### ECDSA / secp256k1 (via `secp256k1/`)

The `secp256k1` subpackage provides:

- `GenerateKey()` — generate a secp256k1 private key.
- `Sign(hash, prv)` / `SigToPub(hash, sig)` / `Ecrecover(hash, sig)` — standard
  Ethereum signing and public key recovery.
- `ValidateSignatureValues` — EIP-2 (Homestead) lower-S normalization check.
- `PubkeyToAddress` — derive an Ethereum address from a public key.
- ECIES (`ECIESEncrypt` / `ECIESDecrypt`) for peer-to-peer message encryption.
- P256 (`P256GenerateKey`, `P256Sign`, `P256Verify`, etc.) for TLS/QUIC use.

`SigRecover` (root package) provides compact 65-byte signature parsing, EIP-155
replay-protected recovery, and batch verification for transaction pool use.

`SignatureCacheLRU` (`signature_cache_lru.go`) is an LRU cache for recovered sender
addresses, avoiding redundant ECDSA operations during block import.

`Keystore` (`keystore.go`) manages encrypted private keys using scrypt
(N=262144, r=8, p=1) with version-3 keystore format.

### BLS12-381 (via `bls/`)

The `bls` subpackage implements all EIP-2537 BLS12-381 precompile operations using
gnark-crypto (`github.com/consensys/gnark-crypto/ecc/bls12-381`) as the backend:

- **G1 operations**: `BLS12G1Add`, `BLS12G1Mul`, `BLS12G1MSM` (Pippenger multi-scalar
  multiplication via gnark-crypto `G1Affine.MultiExp`).
- **G2 operations**: `BLS12G2Add`, `BLS12G2Mul`, `BLS12G2MSM`.
- **Pairing**: `BLS12Pairing`.
- **Map-to-curve**: `BLS12MapFpToG1`, `BLS12MapFp2ToG2`, `HashToCurveG1`.
- **High-level**: `BLSSign`, `BLSVerify`, `AggregatePublicKeys`,
  `AggregateSignatures`, `FastAggregateVerify`, `VerifyAggregate`.
- **KZG**: `KZGCommit`, `KZGComputeProof`, `KZGVerifyFromBytes`,
  `KZGCeremonyBackend` interface and `DefaultKZGBackend`.
- **blst adapter** (`bls_blst_adapter.go`): thin adapter over the `blst` CGO library
  for production BLS performance when CGO is available.
- **Batch aggregation** (`bls_aggregate_batch.go`, `bls_aggregate_extended.go`):
  batch BLS verification for transaction pools and attestation processing.

### BN254 (via `bn254/`)

The `bn254` subpackage provides EVM precompile-compatible BN254 operations:

- `BN254Add` — point addition (EIP-196 alt_bn128_add).
- `BN254ScalarMul` — scalar multiplication (EIP-196 alt_bn128_mul).
- `BN254PairingCheck` — pairing check (EIP-197 alt_bn128_pairing).

BN254 is also used for shielded transfers: Pedersen commitments (`v*G + r*H`) and
nullifier set operations live in `shielded_circuit.go` (root package).

### IPA / Banderwagon (via `ipa/`)

The `ipa` subpackage implements Inner Product Arguments over the Banderwagon group
(used for Verkle/IPA proofs):

- `BanderPoint` — Banderwagon group element with Add, Double, Neg, ScalarMul, MSM,
  Serialize/Deserialize.
- `PedersenCommit(values)` — multi-scalar Pedersen commitment.
- `IPAProve(generators, a, b, commitment)` / `IPAVerify(...)` — IPA prover and
  verifier.
- `GoIPABackend` and `PureGoIPABackend` for pluggable backends.

### VDF and threshold reconstruction

The [VDF package](vdf/README.md) uses Harmony's class-group Wesolowski proof library for the experimental epoch reveal, encrypted-mempool timer and beacon. The [threshold package](threshold/README.md) reconstructs existing encrypted-mempool shares with TNO Shamir.

### Post-Quantum Cryptography (via `pqc/`)

The [PQ package](pqc/README.md) uses CIRCL for ML-DSA, SLH-DSA and ML-KEM, and go-fn-dsa for FN-DSA-512.

- **Hybrid signer** (`hybrid.go`, `hybrid_threshold.go`): ECDSA + PQ hybrid signing
  for gradual migration.
- **PQ algorithm registry** (`pq_algorithm_registry.go`): runtime-selectable
  algorithm registry mapping `PQAlgorithm` identifiers to signers.
- **PQ transaction signer** (`pq_tx_signer.go`): PQ-signed Ethereum transaction
  wrapper.
- **PQ signing pipeline** (`pq_signing_pipeline.go`): batch signing pipeline for
  high-throughput PQ attestations.
- **Lattice-based blob commitments** (`lattice_commit.go`): MLWE-based blob
  commitment scheme for PQ blob security (L+ roadmap).
- **Pubkey registry** (`pubkey_registry.go`): on-chain PQ pubkey registration
  (post-quantum pubkey registry roadmap item).
- **Signature shares** (`signature_share.go`): threshold PQ signature shares for
  distributed signing.
- **KZG/blob batch verifier** (`batch_blob_verify.go`): batched KZG proof
  verification for blob transactions.

### Merkle Trees (via `merkle/`)

The `merkle` subpackage provides SHA-256-based Merkle tree construction for beacon
chain state roots and block roots, including multi-proof generation and verification.

### Signature Cache

`SignatureCacheLRU` provides an LRU-based cache for ECDSA sender recovery, keyed on
`(txHash, signature)`. The standard `SignatureCache` is a fixed-size concurrent
cache used by the transaction pool.

## Subpackages

| Subpackage | Description |
|------------|-------------|
| [`bls/`](./bls/) | BLS12-381: G1/G2 ops, pairing, MSM, aggregation, KZG (EIP-2537, EIP-4844) |
| [`bn254/`](./bn254/) | BN254: add, scalar-mul, pairing check (EIP-196/197 precompiles) |
| [`ipa/`](./ipa/) | Verkle polynomial openings via go-ipa |
| [`merkle/`](./merkle/) | SHA-256 Merkle tree, proofs, multi-proofs (beacon state roots) |
| [`pqc/`](./pqc/) | Post-quantum: ML-DSA, FN-DSA, SLH-DSA, ML-KEM, and hybrid signing |
| [`secp256k1/`](./secp256k1/) | secp256k1 ECDSA, ECIES, P256, address derivation |
| [`threshold/`](./threshold/) | Prime-field share reconstruction for the encrypted mempool |
| [`vdf/`](./vdf/) | Wesolowski VDF, VDF chain, randomness beacon (M+ roadmap) |

## Usage

```go
import "github.com/eth2030/eth2030/crypto"

// Keccak-256 hashing.
hash := crypto.Keccak256Hash(data)

// secp256k1 key generation and signing.
prv, _ := crypto.GenerateKey()
sig, _ := crypto.Sign(msgHash, prv)
pubkey, _ := crypto.SigToPub(msgHash, sig)
addr := crypto.PubkeyToAddress(*pubkey)

// EIP-2537 BLS12-381 G1 addition.
result, err := crypto.BLS12G1Add(input)

// BLS aggregate verification.
ok := crypto.FastAggregateVerify(pubkeys, msg, aggregateSig)

// BN254 pairing check (EIP-197).
result, err := crypto.BN254PairingCheck(input)

// Post-quantum signing (Dilithium3).
import "github.com/eth2030/eth2030/crypto/pqc"
signer := pqc.GetSigner(pqc.DILITHIUM3)
kp, _ := signer.GenerateKey()
sig, _ := signer.Sign(kp.SecretKey, msg)
ok := signer.Verify(kp.PublicKey, msg, sig)

// Wesolowski VDF.
import "github.com/eth2030/eth2030/crypto/vdf"
result, _ := vdf.Evaluate(seed, 100)
output, ok := vdf.VerifySolution(seed, 100, result.EncodeSolution())
```

## Documentation References

- [Design Doc](../../docs/DESIGN.md)
- [PQ Implementation Report](../../docs/PQ_IMPLEMENTATION_REPORT.md)
- [PQ Roadmap Report](../../docs/PQ_ROADMAP_REPORT.md)
- [Roadmap](../../docs/ROADMAP.md)
- [EIP-196: BN254 add/mul](https://eips.ethereum.org/EIPS/eip-196)
- [EIP-197: BN254 pairing](https://eips.ethereum.org/EIPS/eip-197)
- [EIP-2537: BLS12-381 precompiles](https://eips.ethereum.org/EIPS/eip-2537)
- [FIPS 204: ML-DSA (Module-Lattice-Based DSA)](https://csrc.nist.gov/pubs/fips/204/final)
