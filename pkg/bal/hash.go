package bal

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"sort"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/rlp"
)

type indexedChange[T any] struct {
	Index uint64
	Value T
}

type slotChanges struct {
	Slot    *big.Int
	Changes []indexedChange[*big.Int]
}

type accountChanges struct {
	Address        types.Address
	StorageChanges []slotChanges
	StorageReads   []*big.Int
	BalanceChanges []indexedChange[*big.Int]
	NonceChanges   []indexedChange[uint64]
	CodeChanges    []indexedChange[[]byte]
}

// EncodeRLP encodes the EIP-7928 account-grouped wire format. Internal
// entries retain per-phase pre-values and read values for execution tooling.
func (list *BlockAccessList) EncodeRLP() ([]byte, error) {
	entries := append([]AccessEntry(nil), list.Entries...)
	sort.Slice(entries, func(i, j int) bool {
		if cmp := bytes.Compare(entries[i].Address[:], entries[j].Address[:]); cmp != 0 {
			return cmp < 0
		}
		return entries[i].AccessIndex < entries[j].AccessIndex
	})
	if err := ValidateBALOrdering(&BlockAccessList{Entries: entries}); err != nil {
		return nil, err
	}
	accounts := make([]accountChanges, 0)
	for first := 0; first < len(entries); {
		last := first + 1
		for last < len(entries) && entries[last].Address == entries[first].Address {
			last++
		}
		account, err := encodeAccountChanges(entries[first:last])
		if err != nil {
			return nil, err
		}
		accounts = append(accounts, account)
		first = last
	}
	return rlp.EncodeToBytes(accounts)
}

func encodeAccountChanges(entries []AccessEntry) (accountChanges, error) {
	account := accountChanges{Address: entries[0].Address}
	writes := make(map[types.Hash][]indexedChange[*big.Int])
	reads := make(map[types.Hash]bool)
	for _, entry := range entries {
		idx := entry.AccessIndex
		if idx > math.MaxUint16 {
			return account, fmt.Errorf("BAL access index %d exceeds uint16", idx)
		}
		for _, read := range entry.StorageReads {
			reads[read.Slot] = true
		}
		for _, write := range entry.StorageChanges {
			if write.OldValue == write.NewValue {
				reads[write.Slot] = true
				continue
			}
			writes[write.Slot] = append(writes[write.Slot], indexedChange[*big.Int]{idx, new(big.Int).SetBytes(write.NewValue[:])})
		}
		if change := entry.BalanceChange; change != nil {
			account.BalanceChanges = append(account.BalanceChanges, indexedChange[*big.Int]{idx, change.NewValue})
		}
		if change := entry.NonceChange; change != nil {
			account.NonceChanges = append(account.NonceChanges, indexedChange[uint64]{idx, change.NewValue})
		}
		if change := entry.CodeChange; change != nil {
			account.CodeChanges = append(account.CodeChanges, indexedChange[[]byte]{idx, change.NewCode})
		}
	}
	for slot, changes := range writes {
		account.StorageChanges = append(account.StorageChanges, slotChanges{new(big.Int).SetBytes(slot[:]), changes})
		delete(reads, slot)
	}
	for slot := range reads {
		account.StorageReads = append(account.StorageReads, new(big.Int).SetBytes(slot[:]))
	}
	sort.Slice(account.StorageChanges, func(i, j int) bool { return account.StorageChanges[i].Slot.Cmp(account.StorageChanges[j].Slot) < 0 })
	sort.Slice(account.StorageReads, func(i, j int) bool { return account.StorageReads[i].Cmp(account.StorageReads[j]) < 0 })
	return account, nil
}

// Hash computes the Keccak256 hash of the RLP-encoded BlockAccessList.
func (list *BlockAccessList) Hash() types.Hash {
	encoded, err := list.EncodeRLP()
	if err != nil {
		return types.Hash{}
	}
	return crypto.Keccak256Hash(encoded)
}
