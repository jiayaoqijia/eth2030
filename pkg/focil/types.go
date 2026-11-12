// Package focil implements Fork-Choice enforced Inclusion Lists (EIP-7805).
//
// FOCIL ensures censorship resistance by requiring block builders to include
// transactions specified by a committee of validators. Each committee member
// constructs an inclusion list from pending mempool transactions, and the
// block builder must satisfy these constraints or attesters will not vote
// for the block.
package focil

import (
	"encoding/binary"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

// HashUint64LE encodes a uint64 as 8 little-endian bytes.
func HashUint64LE(v uint64) []byte {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	return buf[:]
}

// DomainILCommittee is DOMAIN_IL_COMMITTEE = 0x0C000000 per EIP-7805. The
// same domain byte value is reused by Gloas for DOMAIN_PTC_ATTESTER; the
// two domains live in different fork scopes.
var DomainILCommittee = [4]byte{0x0C, 0x00, 0x00, 0x00}

// Constants from EIP-7805.
const (
	// MAX_TRANSACTIONS_PER_INCLUSION_LIST is the max entries in one IL (2^4 = 16).
	MAX_TRANSACTIONS_PER_INCLUSION_LIST = 16

	// MAX_GAS_PER_INCLUSION_LIST is the max total gas per IL (2^21 = 2097152).
	MAX_GAS_PER_INCLUSION_LIST = 1 << 21

	// MAX_BYTES_PER_INCLUSION_LIST is the max byte size of IL transactions (8 KiB).
	MAX_BYTES_PER_INCLUSION_LIST = 8192

	// IL_COMMITTEE_SIZE is the number of validators in the IL committee.
	IL_COMMITTEE_SIZE = 16
)

// InclusionListEntry represents a single transaction entry in an inclusion list.
type InclusionListEntry struct {
	Transaction []byte `json:"transaction"` // RLP-encoded transaction
	Index       uint64 `json:"index"`       // position in the list
}

// InclusionList represents a set of transactions that MUST be included in
// a subsequent block. Created by an IL committee member for a given slot.
type InclusionList struct {
	Slot          uint64               `json:"slot"`
	ProposerIndex uint64               `json:"proposerIndex"` // IL committee member index
	CommitteeRoot types.Hash           `json:"committeeRoot"` // root of IL committee
	Entries       []InclusionListEntry `json:"entries"`
}

// SignedInclusionList wraps an InclusionList with a BLS signature.
type SignedInclusionList struct {
	Message   InclusionList `json:"message"`
	Signature [96]byte      `json:"signature"`
}

// HashTreeRoot computes the deterministic signing root input of the
// InclusionList message, used for the DOMAIN_IL_COMMITTEE BLS signature.
func (il *InclusionList) HashTreeRoot() types.Hash {
	var data []byte
	data = append(data, HashUint64LE(il.Slot)...)
	data = append(data, HashUint64LE(il.ProposerIndex)...)
	data = append(data, il.CommitteeRoot[:]...)
	var count [8]byte
	binary.LittleEndian.PutUint64(count[:], uint64(len(il.Entries)))
	data = append(data, count[:]...)
	for _, entry := range il.Entries {
		data = append(data, HashUint64LE(entry.Index)...)
		data = append(data, entry.Transaction...)
	}
	return crypto.Keccak256Hash(data)
}

// MarshalBinary RLP-encodes the signed inclusion list for gossip transport.
func (s *SignedInclusionList) MarshalBinary() ([]byte, error) {
	msg := struct {
		Slot          uint64
		ProposerIndex uint64
		CommitteeRoot types.Hash
		Entries       []InclusionListEntry
		Signature     [96]byte
	}{
		Slot:          s.Message.Slot,
		ProposerIndex: s.Message.ProposerIndex,
		CommitteeRoot: s.Message.CommitteeRoot,
		Entries:       s.Message.Entries,
		Signature:     s.Signature,
	}
	return rlp.EncodeToBytes(msg)
}

// UnmarshalBinary decodes a gossip-transported signed inclusion list.
func (s *SignedInclusionList) UnmarshalBinary(data []byte) error {
	var msg struct {
		Slot          uint64
		ProposerIndex uint64
		CommitteeRoot types.Hash
		Entries       []InclusionListEntry
		Signature     [96]byte
	}
	if err := rlp.DecodeBytes(data, &msg); err != nil {
		return err
	}
	s.Message = InclusionList{
		Slot:          msg.Slot,
		ProposerIndex: msg.ProposerIndex,
		CommitteeRoot: msg.CommitteeRoot,
		Entries:       msg.Entries,
	}
	s.Signature = msg.Signature
	return nil
}

// TotalGas returns the total gas of all transactions in the inclusion list.
func (il *InclusionList) TotalGas() uint64 {
	var total uint64
	for _, entry := range il.Entries {
		tx, err := types.DecodeTxRLP(entry.Transaction)
		if err != nil {
			continue
		}
		total += tx.Gas()
	}
	return total
}

// TotalBytes returns the total byte size of all transactions in the IL.
func (il *InclusionList) TotalBytes() int {
	total := 0
	for _, entry := range il.Entries {
		total += len(entry.Transaction)
	}
	return total
}

// TransactionHashes returns the keccak256 hashes of all transactions in the IL.
func (il *InclusionList) TransactionHashes() []types.Hash {
	var hashes []types.Hash
	for _, entry := range il.Entries {
		tx, err := types.DecodeTxRLP(entry.Transaction)
		if err != nil {
			continue
		}
		hashes = append(hashes, tx.Hash())
	}
	return hashes
}
