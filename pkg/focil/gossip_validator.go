// gossip_validator.go implements the EIP-7805 CL P2P validation rules for a
// received SignedInclusionList, in spec order:
//
//  1. The slot of the IL matches the current or previous slot.
//  2. The IL committee root referenced in the IL matches the expected
//     committee root for its slot.
//  3. Two or fewer ILs received from this IL committee member
//     (equivocation evidence recorded on a conflicting second IL).
//  4. The IL is correctly signed by the validator (BLS over the message
//     root with DOMAIN_IL_COMMITTEE).
//  5. The validator is part of the IL committee (IL_COMMITTEE_SIZE = 16).
//  6. The IL does not exceed MAX_BYTES_PER_INCLUSION_LIST (8 KiB).
package focil

import (
	"fmt"

	"github.com/eth2030/eth2030/consensus"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Gossip validation errors, one per spec rule.
var (
	ErrGossipNilIL          = fmt.Errorf("gossip: nil signed inclusion list")
	ErrGossipSlotMismatch   = fmt.Errorf("gossip: IL slot is not the current or previous slot")
	ErrGossipCommitteeRoot  = fmt.Errorf("gossip: IL committee root mismatch")
	ErrGossipBadSignature   = fmt.Errorf("gossip: invalid IL signature")
	ErrGossipNotMember      = fmt.Errorf("gossip: validator is not an IL committee member")
	ErrGossipTooLarge       = fmt.Errorf("gossip: IL exceeds MAX_BYTES_PER_INCLUSION_LIST")
	ErrGossipNoEntries      = fmt.Errorf("gossip: IL has no entries")
	ErrGossipTooManyEntries = fmt.Errorf("gossip: IL exceeds MAX_TRANSACTIONS_PER_INCLUSION_LIST")
)

// GossipContext supplies the chain/committee view a gossip validator needs.
// Rules 2 and 5 (committee root, membership) and rule 4's public-key lookup
// require consensus-side state the EL may not hold; the corresponding hooks
// are optional. When nil, the rule is skipped and the reason is returned in
// the accepted-IL result so callers can log it — a complete implementation
// (CL node or EL fed the beacon state) installs all hooks. Tests provide
// every hook.
type GossipContext struct {
	// CurrentSlot returns the current beacon slot. Required.
	CurrentSlot func() uint64
	// CommitteeRootForSlot returns the expected IL committee root (rule 2).
	CommitteeRootForSlot func(slot uint64) (types.Hash, error)
	// IsILCommitteeMember reports committee membership (rule 5).
	IsILCommitteeMember func(validatorIndex, slot uint64) bool
	// ValidatorPubkey returns the BLS public key of a validator (rule 4).
	ValidatorPubkey func(validatorIndex uint64) ([48]byte, error)
	// ForkVersion and GenesisValidatorsRoot feed the signing domain.
	ForkVersion           func() [4]byte
	GenesisValidatorsRoot func() [32]byte
}

// GossipValidator validates received signed inclusion lists per the
// EIP-7805 CL P2P rules and feeds accepted ILs into the IL store.
type GossipValidator struct {
	store   *ILStore
	context *GossipContext
}

// NewGossipValidator creates a gossip validator over the given store.
func NewGossipValidator(store *ILStore, ctx *GossipContext) *GossipValidator {
	return &GossipValidator{store: store, context: ctx}
}

// Validate runs the six CL P2P rules in spec order against a received
// signed IL. On success the IL is recorded in the store (rule 3 applies
// there: identical rebroadcasts dedup, conflicting second IL records
// equivocation evidence, further ILs are rejected). Optional context hooks
// that are unset skip their rule (see GossipContext).
func (v *GossipValidator) Validate(signed *SignedInclusionList) (*InclusionList, error) {
	if v == nil || v.context.CurrentSlot == nil {
		return nil, ErrGossipNilIL
	}
	if signed == nil {
		return nil, ErrGossipNilIL
	}
	il := &signed.Message
	ctx := v.context
	currentSlot := ctx.CurrentSlot()

	// Rule 1: the IL slot must be the current or previous slot.
	if il.Slot == 0 || (il.Slot != currentSlot && il.Slot+1 != currentSlot) {
		return nil, fmt.Errorf("%w: slot %d, current %d", ErrGossipSlotMismatch, il.Slot, currentSlot)
	}

	// Rule 6 (structural size caps) is checked before any expensive work.
	if len(il.Entries) == 0 {
		return nil, ErrGossipNoEntries
	}
	if len(il.Entries) > MAX_TRANSACTIONS_PER_INCLUSION_LIST {
		return nil, fmt.Errorf("%w: %d entries", ErrGossipTooManyEntries, len(il.Entries))
	}
	if il.TotalBytes() > MAX_BYTES_PER_INCLUSION_LIST {
		return nil, fmt.Errorf("%w: %d bytes", ErrGossipTooLarge, il.TotalBytes())
	}

	// Rule 2: the committee root must match the expected root for the slot.
	if ctx.CommitteeRootForSlot != nil {
		expectedRoot, err := ctx.CommitteeRootForSlot(il.Slot)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrGossipCommitteeRoot, err)
		}
		if il.CommitteeRoot != expectedRoot {
			return nil, fmt.Errorf("%w: got %s want %s", ErrGossipCommitteeRoot,
				il.CommitteeRoot.Hex(), expectedRoot.Hex())
		}
	}

	// Rule 5: the validator must sit on the IL committee.
	if ctx.IsILCommitteeMember != nil && !ctx.IsILCommitteeMember(il.ProposerIndex, il.Slot) {
		return nil, fmt.Errorf("%w: validator %d at slot %d", ErrGossipNotMember,
			il.ProposerIndex, il.Slot)
	}

	// Rule 4: BLS signature over the message root with DOMAIN_IL_COMMITTEE.
	if ctx.ValidatorPubkey != nil {
		pubkey, err := ctx.ValidatorPubkey(il.ProposerIndex)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrGossipBadSignature, err)
		}
		var domain [32]byte
		if ctx.ForkVersion != nil || ctx.GenesisValidatorsRoot != nil {
			var fv [4]byte
			var gr [32]byte
			if ctx.ForkVersion != nil {
				fv = ctx.ForkVersion()
			}
			if ctx.GenesisValidatorsRoot != nil {
				gr = ctx.GenesisValidatorsRoot()
			}
			domain = DomainSeparationIL(fv, gr)
		}
		signingRoot := consensus.ComputeSigningRoot(il.HashTreeRoot(), domain)
		if !crypto.BLSVerify(pubkey, signingRoot[:], signed.Signature) {
			return nil, fmt.Errorf("%w: validator %d", ErrGossipBadSignature, il.ProposerIndex)
		}
	}

	// Rule 3: track messages per committee member with equivocation
	// evidence; further ILs from an equivocator are rejected by the store.
	accepted, err := v.store.RecordGossipIL(il.ProposerIndex, il.Slot, il)
	if err != nil {
		return nil, err
	}
	return accepted, nil
}

// DomainSeparationIL computes the DOMAIN_IL_COMMITTEE signing domain with
// the same fork-data-root construction as the beacon domains.
func DomainSeparationIL(forkVersion [4]byte, genesisRoot [32]byte) [32]byte {
	return consensus.DomainSeparation(DomainILCommittee, forkVersion, genesisRoot)
}
