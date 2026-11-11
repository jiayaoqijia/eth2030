# vrf

VRF-based secret proposer election with anti-equivocation protection (L+ roadmap).

## Overview

The experimental proposer election uses RFC 9381 ECVRF-EDWARDS25519-SHA512-ELL2 through curve25519-voi. The proposer score input is the SHA-256 digest of the provider's 64-byte VRF output. This format replaces the former hash-chain proof. Ethereum beacon randomness uses BLS-verified RANDAO reveals.

`ElectProposer` applies sortition: the validator with the **lowest** VRF score
(derived from the output bytes as a big-endian integer) wins. Proposers reveal
their VRF proof at block proposal time; `SubmitReveal` detects double-reveals
(same validator, different block hash in same slot) and emits
`VRFSlashingEvidence`. `BlockBindingHash` binds the VRF output to the specific
block hash, preventing post-election payload switching.

## Functionality

### Types

| Name | Description |
|------|-------------|
| `SecretElection` | Thread-safe reveal manager with equivocation detection |
| `VRFKeyPair` | 32-byte secret key and public key |
| `VRFProof` | 80-byte canonical ECVRF proof |
| `VRFOutput` | 32-byte VRF output hash |
| `VRFElectionEntry` | Validator index, epoch, slot, output, proof, sortition score |
| `VRFReveal` | Validator index, slot, block hash, output, proof |
| `VRFSlashingEvidence` | Double-reveal: two `VRFReveal` values for the same validator+slot |

### Constants

| Name | Value |
|------|-------|
| `VRFKeySize` | 32 bytes |
| `VRFProofSize` | 64 bytes (Gamma + Challenge) |
| `VRFOutputSize` | 32 bytes |
| `MaxVRFValidators` | 1,048,576 (1M) |

### Functions / methods

| Name | Description |
|------|-------------|
| `GenerateVRFKeyPair(seed) *VRFKeyPair` | Derive keypair from seed using Keccak-256 |
| `VRFProve(sk, input) (VRFOutput, VRFProof)` | Compute VRF output and proof |
| `VRFVerify(pk, input, output, proof) bool` | Verify the hash-chain binding |
| `ComputeVRFElectionInput(epoch, slot) []byte` | Standard 16-byte input encoding |
| `ComputeProposerScore(output) *big.Int` | Sortition score (lower = higher priority) |
| `NewSecretElection() *SecretElection` | Create election manager |
| `(*SecretElection).ElectProposer(entries) (*VRFElectionEntry, error)` | Select lowest-score validator |
| `(*SecretElection).SubmitReveal(reveal) error` | Record reveal; detect equivocation |
| `(*SecretElection).GetSlashingEvidence() []*VRFSlashingEvidence` | All detected double-reveals |
| `VerifyReveal(pk, reveal, epoch) bool` | Validate a reveal against a known public key |
| `BlockBindingHash(output, blockHash) types.Hash` | Bind output to block to prevent switching |

## Usage

```go
import "github.com/eth2030/eth2030/consensus/vrf"

kp := vrf.GenerateVRFKeyPair(seed)
input := vrf.ComputeVRFElectionInput(epoch, slot)
output, proof := vrf.VRFProve(kp.SecretKey, input)

election := vrf.NewSecretElection()
election.SubmitReveal(&vrf.VRFReveal{
    ValidatorIndex: idx, Slot: slot,
    BlockHash: blockHash, Output: output, Proof: proof,
})

winner, _ := election.ElectProposer(entries)
```

[← consensus](../README.md)
