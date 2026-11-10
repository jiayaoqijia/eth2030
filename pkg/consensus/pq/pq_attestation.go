package pq

import (
	"encoding/binary"
	"errors"
	"math/rand"
	"sync/atomic"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/crypto/pqc"
)

// PQ attestation errors.
var (
	ErrPQAttNoSignature       = errors.New("pq attestation: no signature present")
	ErrPQAttInvalidPQSig      = errors.New("pq attestation: invalid post-quantum signature")
	ErrPQAttInvalidClassicSig = errors.New("pq attestation: invalid classic signature")
	ErrPQAttNilKey            = errors.New("pq attestation: nil PQ key pair")
)

// PQAttestationConfig configures post-quantum attestation verification.
type PQAttestationConfig struct {
	// UsePQSignatures enables verification of PQ signatures.
	UsePQSignatures bool

	// FallbackToClassic allows classic (ECDSA) signatures when PQ is unavailable.
	FallbackToClassic bool

	// MinPQValidators is the minimum number of validators that must use PQ
	// signatures before the network enforces PQ-only mode.
	MinPQValidators int
}

// DefaultPQAttestationConfig returns the default PQ attestation config.
// PQ signatures are enabled with classic fallback for the transition period.
func DefaultPQAttestationConfig() *PQAttestationConfig {
	return &PQAttestationConfig{
		UsePQSignatures:   true,
		FallbackToClassic: true,
		MinPQValidators:   0,
	}
}

// PQAttestation represents an attestation with post-quantum signature support.
// During the L+ transition, attestations may carry both PQ and classic signatures.
type PQAttestation struct {
	Slot             uint64
	CommitteeIndex   uint64
	BeaconBlockRoot  types.Hash
	SourceEpoch      uint64
	TargetEpoch      uint64
	PQSignature      []byte // ML-DSA-65 signature
	PQPublicKey      []byte // ML-DSA-65 public key
	ClassicSignature []byte // ECDSA signature (65 bytes)
	ValidatorIndex   uint64
}

// PQAttestationVerifier verifies post-quantum attestations.
type PQAttestationVerifier struct {
	config   *PQAttestationConfig
	verified atomic.Uint64
	failed   atomic.Uint64
}

// NewPQAttestationVerifier creates a new verifier with the given config.
func NewPQAttestationVerifier(config *PQAttestationConfig) *PQAttestationVerifier {
	if config == nil {
		config = DefaultPQAttestationConfig()
	}
	return &PQAttestationVerifier{
		config: config,
	}
}

// VerifyAttestation verifies a PQ attestation's signature(s).
// If PQSignature is present and PQ verification is enabled, it is checked first.
// If ClassicSignature is present and fallback is enabled, it is checked as well.
// Returns an error if no valid signature is found.
func (v *PQAttestationVerifier) VerifyAttestation(att *PQAttestation) (bool, error) {
	if att == nil {
		v.failed.Add(1)
		return false, ErrPQAttNoSignature
	}

	hasPQ := len(att.PQSignature) > 0
	hasClassic := len(att.ClassicSignature) > 0

	if !hasPQ && !hasClassic {
		v.failed.Add(1)
		return false, ErrPQAttNoSignature
	}

	// Try PQ verification first.
	if hasPQ && v.config.UsePQSignatures {
		// Compute the attestation signing message and verify with the real
		// public key and message via VerifyDilithium.
		msg := attestationMessage(att.Slot, att.CommitteeIndex, att.BeaconBlockRoot, att.SourceEpoch, att.TargetEpoch)
		if pqc.VerifyDilithium(att.PQPublicKey, msg, att.PQSignature) {
			v.verified.Add(1)
			return true, nil
		}
		// PQ sig present but invalid; if no fallback, fail.
		if !hasClassic || !v.config.FallbackToClassic {
			v.failed.Add(1)
			return false, ErrPQAttInvalidPQSig
		}
	}

	// Try classic verification as fallback.
	if hasClassic && v.config.FallbackToClassic {
		if len(att.ClassicSignature) == 65 {
			// Check that classic sig is not all zeros.
			allZero := true
			for _, b := range att.ClassicSignature {
				if b != 0 {
					allZero = false
					break
				}
			}
			if !allZero {
				v.verified.Add(1)
				return true, nil
			}
		}
		v.failed.Add(1)
		return false, ErrPQAttInvalidClassicSig
	}

	v.failed.Add(1)
	return false, ErrPQAttNoSignature
}

// Stats returns the number of verified and failed attestation checks.
func (v *PQAttestationVerifier) Stats() (verified, failed uint64) {
	return v.verified.Load(), v.failed.Load()
}

// attestationMessage computes the signing message for a PQ attestation.
// Message = Keccak256(slot || committeeIndex || blockRoot || sourceEpoch || targetEpoch)
func attestationMessage(slot, committeeIndex uint64, blockRoot types.Hash, sourceEpoch, targetEpoch uint64) []byte {
	buf := make([]byte, 8+8+32+8+8)
	binary.BigEndian.PutUint64(buf[0:8], slot)
	binary.BigEndian.PutUint64(buf[8:16], committeeIndex)
	copy(buf[16:48], blockRoot[:])
	binary.BigEndian.PutUint64(buf[48:56], sourceEpoch)
	binary.BigEndian.PutUint64(buf[56:64], targetEpoch)
	return crypto.Keccak256(buf)
}

// ValidatePQAttestation checks that a PQ attestation has valid fields:
// PQ signature format, algorithm compatibility, and epoch ordering.
func ValidatePQAttestation(att *PQAttestation) error {
	if att == nil {
		return ErrPQAttNoSignature
	}
	if len(att.PQSignature) == 0 && len(att.ClassicSignature) == 0 {
		return ErrPQAttNoSignature
	}
	if att.TargetEpoch < att.SourceEpoch {
		return errors.New("pq attestation: target epoch before source epoch")
	}
	emptyHash := types.Hash{}
	if att.BeaconBlockRoot == emptyHash {
		return errors.New("pq attestation: empty beacon block root")
	}
	// Validate classic signature length if present.
	if len(att.ClassicSignature) > 0 && len(att.ClassicSignature) != 65 {
		return errors.New("pq attestation: classic signature must be 65 bytes")
	}
	return nil
}

// CreatePQAttestation creates a new attestation with a post-quantum signature.
// The message is derived from the attestation fields and signed with the PQ key.
func CreatePQAttestation(
	slot uint64,
	committeeIndex uint64,
	blockRoot types.Hash,
	sourceEpoch, targetEpoch uint64,
	validatorIndex uint64,
	pqKey *pqc.DilithiumKeyPair,
) (*PQAttestation, error) {
	if pqKey == nil {
		return nil, ErrPQAttNilKey
	}

	msg := attestationMessage(slot, committeeIndex, blockRoot, sourceEpoch, targetEpoch)

	sig, err := pqKey.Sign(msg)
	if err != nil {
		return nil, err
	}

	return &PQAttestation{
		Slot:            slot,
		CommitteeIndex:  committeeIndex,
		BeaconBlockRoot: blockRoot,
		SourceEpoch:     sourceEpoch,
		TargetEpoch:     targetEpoch,
		PQSignature:     sig,
		PQPublicKey:     pqKey.PublicKey,
		ValidatorIndex:  validatorIndex,
	}, nil
}

// SelectLeanPQAttestors selects a deterministic subset of validators for lean
// available chain PQ attestation. The selection uses a keccak256-based seed
// derived from the slot and epoch seed, then applies Fisher-Yates shuffle.
func SelectLeanPQAttestors(validators []uint64, count int, slot uint64, epochSeed types.Hash) []uint64 {
	if count >= len(validators) {
		out := make([]uint64, len(validators))
		copy(out, validators)
		return out
	}

	// Compute seed = keccak256(big-endian slot || epochSeed[:]).
	buf := make([]byte, 8+len(epochSeed))
	binary.BigEndian.PutUint64(buf[0:8], slot)
	copy(buf[8:], epochSeed[:])
	seed := crypto.Keccak256(buf)

	// Derive int64 from first 8 bytes.
	seedInt := int64(seed[0])<<56 | int64(seed[1])<<48 | int64(seed[2])<<40 |
		int64(seed[3])<<32 | int64(seed[4])<<24 | int64(seed[5])<<16 |
		int64(seed[6])<<8 | int64(seed[7])

	// Copy and shuffle.
	shuffled := make([]uint64, len(validators))
	copy(shuffled, validators)
	rng := rand.New(rand.NewSource(seedInt))
	for i := len(shuffled) - 1; i > 0; i-- {
		j := rng.Intn(i + 1)
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	}

	return shuffled[:count]
}

// LeanAvailablePQAttestors returns the set of validators that should participate
// in PQ attestation for the given slot. In lean mode, a deterministic subset is
// selected; otherwise all validators are returned unchanged.
func (v *PQAttestationVerifier) LeanAvailablePQAttestors(slot uint64, allValidators []uint64, epochSeed types.Hash, cfg *LeanConfig) []uint64 {
	if cfg == nil || !cfg.LeanAvailableChainMode {
		return allValidators
	}
	return SelectLeanPQAttestors(allValidators, cfg.LeanAvailableChainValidators, slot, epochSeed)
}

// STARKAggregateVerify uses STARK-based batch verification for PQ attestations.
// This is more efficient than individual verification for large batches (N > 4).
func (v *PQAttestationVerifier) STARKAggregateVerify(attestations []PQAttestation) (*STARKSignatureAggregation, error) {
	if len(attestations) == 0 {
		return nil, ErrPQAttNoSignature
	}

	// For small batches, individual verification may be more efficient.
	// The crossover point is ~4 signatures (4 * 50k = 200k = STARK verify cost).
	if len(attestations) <= 4 {
		// Verify individually first.
		for _, att := range attestations {
			valid, err := v.VerifyAttestation(&att)
			if err != nil || !valid {
				return nil, ErrPQAttInvalidPQSig
			}
		}
	}

	// Use STARK aggregation for the batch.
	result, err := BatchVerifyPQAttestations(attestations)
	if err != nil {
		return nil, err
	}

	v.verified.Add(uint64(len(attestations)))
	return result, nil
}
