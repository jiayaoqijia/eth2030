package state

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// journalEntry is a revertible state change.
type journalEntry interface {
	revert(s *MemoryStateDB)
}

// journal tracks state modifications for snapshot/revert.
type journal struct {
	entries   []journalEntry
	snapshots map[int]int // snapshot ID -> entry index
	nextID    int
}

func newJournal() *journal {
	return &journal{
		snapshots: make(map[int]int),
	}
}

func (j *journal) append(entry journalEntry) {
	j.entries = append(j.entries, entry)
}

func (j *journal) length() int {
	return len(j.entries)
}

func (j *journal) snapshot() int {
	id := j.nextID
	j.nextID++
	j.snapshots[id] = len(j.entries)
	return id
}

func (j *journal) revertToSnapshot(id int, s *MemoryStateDB) {
	idx, ok := j.snapshots[id]
	if !ok {
		return
	}
	// Revert in reverse order.
	for i := len(j.entries) - 1; i >= idx; i-- {
		j.entries[i].revert(s)
	}
	j.entries = j.entries[:idx]

	// Remove invalidated snapshots.
	for sid := range j.snapshots {
		if sid >= id {
			delete(j.snapshots, sid)
		}
	}
}

// --- Concrete journal entries ---

type createAccountChange struct {
	addr types.Address
	prev *stateObject // nil if the account didn't exist before
}

func (ch createAccountChange) revert(s *MemoryStateDB) {
	if ch.prev == nil {
		delete(s.stateObjects, ch.addr)
	} else {
		s.stateObjects[ch.addr] = ch.prev
	}
}

type balanceChange struct {
	addr types.Address
	prev *big.Int
}

func (ch balanceChange) revert(s *MemoryStateDB) {
	if obj := s.getStateObject(ch.addr); obj != nil {
		obj.account.Balance = ch.prev
	}
}

type nonceChange struct {
	addr types.Address
	prev uint64
}

func (ch nonceChange) revert(s *MemoryStateDB) {
	if obj := s.getStateObject(ch.addr); obj != nil {
		obj.account.Nonce = ch.prev
	}
}

type codeChange struct {
	addr     types.Address
	prevCode []byte
	prevHash []byte
}

func (ch codeChange) revert(s *MemoryStateDB) {
	if obj := s.getStateObject(ch.addr); obj != nil {
		obj.code = ch.prevCode
		obj.account.CodeHash = ch.prevHash
	}
}

type storageChange struct {
	addr       types.Address
	key        types.Hash
	prev       types.Hash
	prevExists bool // true if the key was present in dirtyStorage before
}

func (ch storageChange) revert(s *MemoryStateDB) {
	if obj := s.getStateObject(ch.addr); obj != nil {
		if ch.prevExists {
			obj.dirtyStorage[ch.key] = ch.prev
		} else {
			// The slot was not in dirtyStorage before this write;
			// remove it so committed storage is visible again.
			delete(obj.dirtyStorage, ch.key)
		}
	}
}

type selfDestructChange struct {
	addr           types.Address
	prevDestructed bool
	prevDeleted    bool
	prevBalance    *big.Int
}

func (ch selfDestructChange) revert(s *MemoryStateDB) {
	if obj := s.getStateObject(ch.addr); obj != nil {
		obj.selfDestructed = ch.prevDestructed
		obj.deleted = ch.prevDeleted
		obj.account.Balance = ch.prevBalance
	}
}

type accessListAddAccountChange struct {
	addr types.Address
}

func (ch accessListAddAccountChange) revert(s *MemoryStateDB) {
	s.accessList.DeleteAddress(ch.addr)
}

type accessListAddSlotChange struct {
	addr types.Address
	slot types.Hash
}

func (ch accessListAddSlotChange) revert(s *MemoryStateDB) {
	s.accessList.DeleteSlot(ch.addr, ch.slot)
}

type transientStorageChange struct {
	addr types.Address
	key  types.Hash
	prev types.Hash
}

func (ch transientStorageChange) revert(s *MemoryStateDB) {
	if ch.prev == (types.Hash{}) {
		delete(s.transientStorage[ch.addr], ch.key)
		if len(s.transientStorage[ch.addr]) == 0 {
			delete(s.transientStorage, ch.addr)
		}
	} else {
		s.transientStorage[ch.addr][ch.key] = ch.prev
	}
}

type logChange struct {
	txHash  types.Hash
	prevLen int
}

func (ch logChange) revert(s *MemoryStateDB) {
	logs := s.logs[ch.txHash]
	s.logs[ch.txHash] = logs[:ch.prevLen]
	if ch.prevLen == 0 {
		delete(s.logs, ch.txHash)
	}
}

type refundChange struct {
	prev uint64
}

func (ch refundChange) revert(s *MemoryStateDB) {
	s.refund = ch.prev
}
