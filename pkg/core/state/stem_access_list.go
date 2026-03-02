package state

import (
	"crypto/sha256"

	"github.com/eth2030/eth2030/core/types"
)

// Gas constants for binary trie stem-based warm/cold access tracking.
// Accessing a new stem is cold (expensive); subsequent accesses to the
// same stem within a transaction are warm (cheap), because slots 0-63,
// account basic data, and code chunks 0-127 share the same stem.
const (
	StemColdAccessGas = 2600 // first access to a new stem
	StemWarmAccessGas = 100  // subsequent access to same stem
)

// StemAccessList tracks which binary trie stems have been accessed during
// a transaction. A "stem" is the first 31 bytes of the binary trie key.
// Account basic data (slot 0), storage slots 0-63, and code chunks 0-127
// all share the same stem for a given address, enabling gas savings on
// colocated accesses.
type StemAccessList struct {
	stems map[[31]byte]struct{}
}

// NewStemAccessList creates an empty StemAccessList.
func NewStemAccessList() *StemAccessList {
	return &StemAccessList{
		stems: make(map[[31]byte]struct{}),
	}
}

// IsStemWarm returns true if the stem has already been accessed.
func (s *StemAccessList) IsStemWarm(stem [31]byte) bool {
	_, ok := s.stems[stem]
	return ok
}

// AddStem marks a stem as accessed. Returns true if the stem was already warm.
func (s *StemAccessList) AddStem(stem [31]byte) bool {
	if _, ok := s.stems[stem]; ok {
		return true
	}
	s.stems[stem] = struct{}{}
	return false
}

// StemForAddress returns the stem (first 31 bytes of the binary trie key)
// for an address's basic data. This is the same stem used for storage
// slots 0-63 and code chunks 0-127.
func (s *StemAccessList) StemForAddress(addr types.Address) [31]byte {
	return computeAddressStem(addr)
}

// StemForSlot returns the stem for a given storage slot. For slots 0-63
// (header storage), the stem matches the address stem (colocated). For
// larger slots, the stem differs.
func (s *StemAccessList) StemForSlot(addr types.Address, slot uint64) [31]byte {
	if slot < 64 {
		return computeAddressStem(addr)
	}
	return computeSlotStem(addr, slot)
}

// Copy returns a deep copy of the StemAccessList.
func (s *StemAccessList) Copy() *StemAccessList {
	cp := &StemAccessList{
		stems: make(map[[31]byte]struct{}, len(s.stems)),
	}
	for k := range s.stems {
		cp.stems[k] = struct{}{}
	}
	return cp
}

// Reset clears all tracked stems.
func (s *StemAccessList) Reset() {
	s.stems = make(map[[31]byte]struct{})
}

// computeAddressStem computes the stem for an address's basic data.
// Mirrors bintrie.GetBinaryTreeKey(addr, [32]byte{0})[:31] using
// SHA-256 to avoid an import cycle with the bintrie package.
func computeAddressStem(addr types.Address) [31]byte {
	// SHA256(zeroHash[:12] || addr || key[:31] || 0x00)
	// For basic data, key is all zeros.
	var buf [64]byte
	copy(buf[12:32], addr[:])
	h := sha256.Sum256(buf[:])
	var stem [31]byte
	copy(stem[:], h[:31])
	return stem
}

// computeSlotStem computes the stem for a main storage slot (>= 64).
// Main storage uses key[0]=1 in the binary trie key derivation.
func computeSlotStem(addr types.Address, slot uint64) [31]byte {
	var key [32]byte
	key[0] = 1
	key[24] = byte(slot >> 56)
	key[25] = byte(slot >> 48)
	key[26] = byte(slot >> 40)
	key[27] = byte(slot >> 32)
	key[28] = byte(slot >> 24)
	key[29] = byte(slot >> 16)
	key[30] = byte(slot >> 8)
	key[31] = byte(slot)

	var buf [64]byte
	copy(buf[12:32], addr[:])
	copy(buf[32:63], key[:31])

	h := sha256.Sum256(buf[:])
	var stem [31]byte
	copy(stem[:], h[:31])
	return stem
}
