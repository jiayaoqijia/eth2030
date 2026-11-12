// il_store.go maintains per-validator-per-slot inclusion list state with the
// EIP-7805 CL gossip semantics: identical rebroadcasts are deduplicated, a
// second distinct IL from the same committee member records equivocation
// evidence, and any further ILs from an equivocator are ignored.
package focil

import (
	"bytes"
	"errors"
	"sync"
)

// IL gossip store errors.
var (
	ErrILEquivocation   = errors.New("focil: committee member equivocated with conflicting inclusion lists")
	ErrILKnownEquivoker = errors.New("focil: inclusion list from a known equivocator")
	ErrILTooManyILs     = errors.New("focil: more than two inclusion lists received from this committee member")
)

// ILEquivocationEvidence records the two conflicting inclusion lists a
// committee member signed for the same slot, per the EIP-7805 validator
// duty to "record any evidence of IL equivocation".
type ILEquivocationEvidence struct {
	ValidatorIndex uint64
	Slot           uint64
	First          *InclusionList
	Second         *InclusionList
}

// memberSlotState tracks the ILs observed from one committee member for one
// slot: distinct message count and the first two distinct lists (evidence).
type memberSlotState struct {
	count  int            // distinct IL messages received
	first  *InclusionList // first distinct IL
	second *InclusionList // second distinct IL (set when equivocated)
}

// ILStore maintains per-validator-per-slot inclusion list state and detects
// equivocations per EIP-7805.
type ILStore struct {
	mu sync.Mutex
	// store: validatorIdx -> slotNum -> first IL received
	store map[uint64]map[uint64]*InclusionList
	// equivocators: validatorIdx -> set of slots where they equivocated
	equivocators map[uint64]map[uint64]bool
	// seen: validatorIdx -> slot -> distinct-message state (gossip rule 3)
	seen map[uint64]map[uint64]*memberSlotState
	// evidence: slot -> recorded equivocation evidence
	evidence map[uint64][]ILEquivocationEvidence
}

// NewILStore creates a new ILStore.
func NewILStore() *ILStore {
	return &ILStore{
		store:        make(map[uint64]map[uint64]*InclusionList),
		equivocators: make(map[uint64]map[uint64]bool),
		seen:         make(map[uint64]map[uint64]*memberSlotState),
		evidence:     make(map[uint64][]ILEquivocationEvidence),
	}
}

// AddIL adds an inclusion list from a validator for a slot. Returns true if
// accepted, false if dropped (equivocator's subsequent ILs are silently dropped).
// If the validator already submitted a different IL for the same slot, marks them
// as an equivocator.
func (s *ILStore) AddIL(validatorIdx, slot uint64, il *InclusionList) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Drop ILs from known equivocators.
	if slots, ok := s.equivocators[validatorIdx]; ok {
		if slots[slot] {
			return false
		}
	}

	if _, ok := s.store[validatorIdx]; !ok {
		s.store[validatorIdx] = make(map[uint64]*InclusionList)
	}

	existing, exists := s.store[validatorIdx][slot]
	if !exists {
		s.store[validatorIdx][slot] = il
		return true
	}

	// Check if the new IL differs from the existing one.
	if !ilsEqual(existing, il) {
		// Mark as equivocator.
		if _, ok := s.equivocators[validatorIdx]; !ok {
			s.equivocators[validatorIdx] = make(map[uint64]bool)
		}
		s.equivocators[validatorIdx][slot] = true
	}
	return true
}

// RecordGossipIL applies the CL gossip semantics of EIP-7805 rule 3 to a
// received signed IL: identical rebroadcasts are deduplicated, the second
// distinct IL from the same member for the same slot records equivocation
// evidence, and any further distinct IL beyond two is rejected outright.
// Returns the member's trusted IL (the first distinct one) or an error.
func (s *ILStore) RecordGossipIL(validatorIdx, slot uint64, il *InclusionList) (*InclusionList, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if slots, ok := s.equivocators[validatorIdx]; ok && slots[slot] {
		return nil, ErrILKnownEquivoker
	}

	states, ok := s.seen[validatorIdx]
	if !ok {
		states = make(map[uint64]*memberSlotState)
		s.seen[validatorIdx] = states
	}
	state, ok := states[slot]
	if !ok {
		state = &memberSlotState{}
		states[slot] = state
	}

	// Deduplicate identical rebroadcasts against both known distinct ILs.
	if state.first != nil && ilsEqual(state.first, il) {
		return state.first, nil
	}
	if state.second != nil && ilsEqual(state.second, il) {
		return state.second, nil
	}

	state.count++
	switch state.count {
	case 1:
		state.first = il
		if s.store[validatorIdx] == nil {
			s.store[validatorIdx] = make(map[uint64]*InclusionList)
		}
		s.store[validatorIdx][slot] = il
		return il, nil
	case 2:
		// Equivocation: record evidence. The member's ILs are no longer
		// trusted; both conflicting lists are kept as evidence.
		state.second = il
		if s.equivocators[validatorIdx] == nil {
			s.equivocators[validatorIdx] = make(map[uint64]bool)
		}
		s.equivocators[validatorIdx][slot] = true
		s.evidence[slot] = append(s.evidence[slot], ILEquivocationEvidence{
			ValidatorIndex: validatorIdx,
			Slot:           slot,
			First:          state.first,
			Second:         state.second,
		})
		return nil, ErrILEquivocation
	default:
		return nil, ErrILTooManyILs
	}
}

// IsEquivocator returns true if the validator equivocated for the given slot.
func (s *ILStore) IsEquivocator(validatorIdx, slot uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slots, ok := s.equivocators[validatorIdx]; ok {
		return slots[slot]
	}
	return false
}

// EquivocatorCount returns the number of equivocators for a given slot.
func (s *ILStore) EquivocatorCount(slot uint64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	count := 0
	for _, slots := range s.equivocators {
		if slots[slot] {
			count++
		}
	}
	return count
}

// EquivocationEvidence returns the recorded equivocation evidence for a slot.
func (s *ILStore) EquivocationEvidence(slot uint64) []ILEquivocationEvidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ILEquivocationEvidence(nil), s.evidence[slot]...)
}

// GetIL returns the first accepted IL from a committee member for a slot,
// or nil. ILs from equivocators are not returned (attesters ignore them).
func (s *ILStore) GetIL(validatorIdx, slot uint64) *InclusionList {
	s.mu.Lock()
	defer s.mu.Unlock()
	if slots, ok := s.equivocators[validatorIdx]; ok && slots[slot] {
		return nil
	}
	if bySlot, ok := s.store[validatorIdx]; ok {
		return bySlot[slot]
	}
	return nil
}

// ILs returns all non-equivocated ILs stored for a slot, keyed by member.
func (s *ILStore) ILs(slot uint64) map[uint64]*InclusionList {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[uint64]*InclusionList)
	for validatorIdx, bySlot := range s.store {
		if slots, eq := s.equivocators[validatorIdx]; eq && slots[slot] {
			continue
		}
		if il, ok := bySlot[slot]; ok {
			out[validatorIdx] = il
		}
	}
	return out
}

// PruneBefore drops all state for slots strictly below keepFromSlot and
// returns the number of pruned member entries.
func (s *ILStore) PruneBefore(keepFromSlot uint64) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	pruned := 0
	for validatorIdx, bySlot := range s.store {
		for slot := range bySlot {
			if slot < keepFromSlot {
				delete(bySlot, slot)
				pruned++
			}
		}
		if len(bySlot) == 0 {
			delete(s.store, validatorIdx)
		}
	}
	for validatorIdx, bySlot := range s.seen {
		for slot := range bySlot {
			if slot < keepFromSlot {
				delete(bySlot, slot)
			}
		}
		if len(bySlot) == 0 {
			delete(s.seen, validatorIdx)
		}
	}
	for validatorIdx, bySlot := range s.equivocators {
		for slot := range bySlot {
			if slot < keepFromSlot {
				delete(bySlot, slot)
			}
		}
		if len(bySlot) == 0 {
			delete(s.equivocators, validatorIdx)
		}
	}
	for slot := range s.evidence {
		if slot < keepFromSlot {
			delete(s.evidence, slot)
		}
	}
	return pruned
}

// ilsEqual compares two ILs for equality by comparing their entries.
func ilsEqual(a, b *InclusionList) bool {
	if a.Slot != b.Slot || a.ProposerIndex != b.ProposerIndex {
		return false
	}
	if len(a.Entries) != len(b.Entries) {
		return false
	}
	for i := range a.Entries {
		if !bytes.Equal(a.Entries[i].Transaction, b.Entries[i].Transaction) {
			return false
		}
	}
	return true
}
