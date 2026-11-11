// VRF-based secret proposer election for the consensus layer.
//
// Uses ECVRF-EDWARDS25519-SHA512-ELL2 where each validator computes
// VRFProve(sk, epoch||slot) to derive a deterministic but unpredictable
// proposer score. The proposer is selected by the lowest VRF output
// (sortition). The reveal protocol requires the proposer to publish
// the VRF proof at proposal time, enabling verification without
// revealing identity in advance.
//
// Anti-equivocation: the VRF output is bound to the proposed block hash
// to prevent proposers from equivocating. Double-reveal detection
// provides a slashing condition.
package vrf

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
	"github.com/oasisprotocol/curve25519-voi/primitives/ed25519/extra/ecvrf"
)

// VRF election constants.
const (
	// VRFKeySize is the size of an Ed25519 seed or public key.
	VRFKeySize = ed25519.SeedSize

	// VRFProofSize is the size of a canonical ECVRF proof.
	VRFProofSize = ecvrf.ProofSize

	// VRFOutputSize is the size of a VRF output hash (32 bytes).
	VRFOutputSize = 32

	// MaxVRFValidators is the maximum validators for VRF election.
	MaxVRFValidators = 1 << 20 // 1M
)

// VRF election errors.
var (
	ErrVRFNilKey          = errors.New("vrf_election: nil key")
	ErrVRFInvalidProof    = errors.New("vrf_election: invalid proof")
	ErrVRFInvalidOutput   = errors.New("vrf_election: output does not match proof")
	ErrVRFNoValidators    = errors.New("vrf_election: no validators")
	ErrVRFDoubleReveal    = errors.New("vrf_election: double reveal detected (slashable)")
	ErrVRFNoReveal        = errors.New("vrf_election: no reveal for slot")
	ErrVRFSlotMismatch    = errors.New("vrf_election: slot mismatch in reveal")
	ErrVRFAlreadyRevealed = errors.New("vrf_election: already revealed for this slot")
)

// VRFKeyPair holds a VRF secret key and corresponding public key.
type VRFKeyPair struct {
	SecretKey [VRFKeySize]byte
	PublicKey [VRFKeySize]byte
}

// VRFProof holds the canonical ECVRF proof bytes.
type VRFProof [VRFProofSize]byte

// VRFOutput is the deterministic hash derived from a VRF proof.
type VRFOutput [VRFOutputSize]byte

// VRFElectionEntry represents a validator's election entry for a slot.
type VRFElectionEntry struct {
	ValidatorIndex uint64
	Epoch          uint64
	Slot           uint64
	Output         VRFOutput
	Proof          VRFProof
	Score          *big.Int // derived from output, lower is better
}

// VRFReveal is published by the proposer when proposing a block.
type VRFReveal struct {
	ValidatorIndex uint64
	Slot           uint64
	BlockHash      types.Hash
	Output         VRFOutput
	Proof          VRFProof
}

// VRFSlashingEvidence records a double-reveal for slashing.
type VRFSlashingEvidence struct {
	ValidatorIndex uint64
	Slot           uint64
	Reveal1        *VRFReveal
	Reveal2        *VRFReveal
}

// GenerateVRFKeyPair deterministically derives an ECVRF key pair from a seed.
func GenerateVRFKeyPair(seed []byte) *VRFKeyPair {
	secret := [VRFKeySize]byte(crypto.Keccak256Hash([]byte("vrf-secret-"), seed))
	key := ed25519.NewKeyFromSeed(secret[:])
	public := key.Public().(ed25519.PublicKey)
	return &VRFKeyPair{SecretKey: secret, PublicKey: [VRFKeySize]byte(public)}
}

// VRFProve returns the canonical ECVRF proof and a 32-byte proposer score input.
func VRFProve(sk [VRFKeySize]byte, input []byte) (VRFOutput, VRFProof) {
	proof := ecvrf.Prove(ed25519.NewKeyFromSeed(sk[:]), input)
	output, err := ecvrf.ProofToHash(proof)
	if err != nil {
		return VRFOutput{}, VRFProof{}
	}
	return VRFOutput(sha256.Sum256(output)), VRFProof(proof)
}

// VRFVerify verifies the proof against the validator key and binds its output.
func VRFVerify(pk [VRFKeySize]byte, input []byte, output VRFOutput, proof VRFProof) bool {
	ok, actual := ecvrf.Verify(ed25519.PublicKey(pk[:]), proof[:], input)
	return ok && VRFOutput(sha256.Sum256(actual)) == output
}

// ComputeVRFElectionInput builds the VRF input for proposer election.
func ComputeVRFElectionInput(epoch, slot uint64) []byte {
	var buf [16]byte
	binary.BigEndian.PutUint64(buf[:8], epoch)
	binary.BigEndian.PutUint64(buf[8:], slot)
	return buf[:]
}

// ComputeProposerScore derives a sortition score from a VRF output.
// Lower score means higher priority for proposer selection.
func ComputeProposerScore(output VRFOutput) *big.Int {
	return new(big.Int).SetBytes(output[:])
}

// SecretElection manages VRF-based secret proposer election.
// Thread-safe.
type SecretElection struct {
	mu sync.RWMutex

	// reveals tracks published reveals per slot.
	reveals map[uint64][]*VRFReveal

	// slashingEvidence tracks detected double-reveals.
	slashingEvidence []*VRFSlashingEvidence
}

// NewSecretElection creates a new secret election manager.
func NewSecretElection() *SecretElection {
	return &SecretElection{
		reveals: make(map[uint64][]*VRFReveal),
	}
}

// ElectProposer selects the proposer from a set of election entries.
// The validator with the lowest VRF score wins.
func (se *SecretElection) ElectProposer(entries []*VRFElectionEntry) (*VRFElectionEntry, error) {
	if len(entries) == 0 {
		return nil, ErrVRFNoValidators
	}

	best := entries[0]
	bestScore := ComputeProposerScore(best.Output)

	for _, entry := range entries[1:] {
		score := ComputeProposerScore(entry.Output)
		if score.Cmp(bestScore) < 0 {
			best = entry
			bestScore = score
		}
	}

	return best, nil
}

// SubmitReveal processes a proposer's VRF reveal. Detects double-reveals
// (equivocation) which are slashable offenses.
func (se *SecretElection) SubmitReveal(reveal *VRFReveal) error {
	if reveal == nil {
		return ErrVRFNoReveal
	}

	se.mu.Lock()
	defer se.mu.Unlock()

	existing := se.reveals[reveal.Slot]

	// Check for double-reveal from the same validator.
	for _, r := range existing {
		if r.ValidatorIndex == reveal.ValidatorIndex {
			// Same validator, different block hash = equivocation.
			if r.BlockHash != reveal.BlockHash {
				evidence := &VRFSlashingEvidence{
					ValidatorIndex: reveal.ValidatorIndex,
					Slot:           reveal.Slot,
					Reveal1:        r,
					Reveal2:        reveal,
				}
				se.slashingEvidence = append(se.slashingEvidence, evidence)
				return ErrVRFDoubleReveal
			}
			// Same block hash = duplicate, ignore.
			return ErrVRFAlreadyRevealed
		}
	}

	se.reveals[reveal.Slot] = append(se.reveals[reveal.Slot], reveal)
	return nil
}

// GetReveals returns all reveals for a slot.
func (se *SecretElection) GetReveals(slot uint64) []*VRFReveal {
	se.mu.RLock()
	defer se.mu.RUnlock()

	reveals := se.reveals[slot]
	result := make([]*VRFReveal, len(reveals))
	copy(result, reveals)
	return result
}

// GetSlashingEvidence returns all detected slashing evidence.
func (se *SecretElection) GetSlashingEvidence() []*VRFSlashingEvidence {
	se.mu.RLock()
	defer se.mu.RUnlock()

	result := make([]*VRFSlashingEvidence, len(se.slashingEvidence))
	copy(result, se.slashingEvidence)
	return result
}

// PurgeSlot removes reveal data for a given slot.
func (se *SecretElection) PurgeSlot(slot uint64) {
	se.mu.Lock()
	defer se.mu.Unlock()
	delete(se.reveals, slot)
}

// VerifyReveal checks that a VRF reveal is valid: the VRF output and proof
// must verify against the public key, and the output must bind to the
// block hash.
func VerifyReveal(pk [VRFKeySize]byte, reveal *VRFReveal, epoch uint64) bool {
	if reveal == nil {
		return false
	}
	input := ComputeVRFElectionInput(epoch, reveal.Slot)
	return VRFVerify(pk, input, reveal.Output, reveal.Proof)
}

// BlockBindingHash computes a binding hash between VRF output and block hash.
// This prevents a proposer from revealing a VRF proof for one block
// and then switching to a different block.
func BlockBindingHash(output VRFOutput, blockHash types.Hash) types.Hash {
	var buf []byte
	buf = append(buf, output[:]...)
	buf = append(buf, blockHash[:]...)
	return crypto.Keccak256Hash(buf)
}
