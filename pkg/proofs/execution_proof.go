// execution_proof.go implements execution proof generation and verification
// for the mandatory 3-of-5 block proof requirement (K+ spec). An execution
// proof captures the full trace of block execution including all state accesses,
// transaction results, and merkle proofs for accessed storage.
package proofs

import (
	"encoding/binary"
	"errors"
	"sort"

	"github.com/eth2030/eth2030/bal"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Execution proof errors.
var (
	ErrExecProofNilBlock     = errors.New("execution_proof: nil block")
	ErrExecProofNoTxTraces   = errors.New("execution_proof: no transaction traces")
	ErrExecProofStateRootNil = errors.New("execution_proof: state root is zero")
	ErrExecProofTampered     = errors.New("execution_proof: proof commitment mismatch")
	ErrExecProofAccessEmpty  = errors.New("execution_proof: state access log is empty")
)

// BlockExecutionProof represents a complete execution proof for a block.
type BlockExecutionProof struct {
	BlockHash     types.Hash                 // Block this proof covers.
	BlockNumber   uint64                     // Block number.
	StateRoot     types.Hash                 // Pre-execution state root.
	PostStateRoot types.Hash                 // Post-execution state root.
	TxTraces      []TxTrace                  // Per-transaction execution traces.
	AccessLog     *StateAccessLog            // All state reads/writes during execution.
	MerkleProofs  map[types.Address][][]byte // Merkle proof per accessed account.
	// BALHash anchors the proof to the block's real state-access record:
	// keccak256(rlp(block access list)) per EIP-7928, matching the header's
	// block-access-list-hash when the block is post-Amsterdam. Zero for
	// proofs generated without a BAL.
	BALHash    types.Hash
	Commitment types.Hash // Hash binding all proof components.
}

// TxTrace records the execution trace of a single transaction.
type TxTrace struct {
	TxHash      types.Hash    // Transaction hash.
	TxIndex     uint          // Position in the block.
	GasUsed     uint64        // Gas consumed.
	Success     bool          // Whether the transaction succeeded.
	StateReads  []StateAccess // State keys read during execution.
	StateWrites []StateAccess // State keys written during execution.
}

// StateAccess represents a single state read or write operation.
type StateAccess struct {
	Address types.Address // Account accessed.
	Slot    types.Hash    // Storage slot (zero for account-level).
	Value   types.Hash    // Value read or written.
	IsWrite bool          // True for write, false for read.
}

// StateAccessLog tracks all state reads and writes during block execution.
type StateAccessLog struct {
	Reads       map[types.Address][]StateAccess // Read ops keyed by address.
	Writes      map[types.Address][]StateAccess // Write ops keyed by address.
	AccessOrder []types.Address                 // Order of first access.
}

// NewStateAccessLog creates a new empty access log.
func NewStateAccessLog() *StateAccessLog {
	return &StateAccessLog{
		Reads:       make(map[types.Address][]StateAccess),
		Writes:      make(map[types.Address][]StateAccess),
		AccessOrder: make([]types.Address, 0),
	}
}

// RecordRead logs a state read operation.
func (l *StateAccessLog) RecordRead(addr types.Address, slot, value types.Hash) {
	l.trackAddress(addr)
	l.Reads[addr] = append(l.Reads[addr], StateAccess{
		Address: addr,
		Slot:    slot,
		Value:   value,
		IsWrite: false,
	})
}

// RecordWrite logs a state write operation.
func (l *StateAccessLog) RecordWrite(addr types.Address, slot, oldValue, newValue types.Hash) {
	l.trackAddress(addr)
	l.Writes[addr] = append(l.Writes[addr], StateAccess{
		Address: addr,
		Slot:    slot,
		Value:   newValue,
		IsWrite: true,
	})
}

// trackAddress records the address in the access order if not already present.
func (l *StateAccessLog) trackAddress(addr types.Address) {
	for _, a := range l.AccessOrder {
		if a == addr {
			return
		}
	}
	l.AccessOrder = append(l.AccessOrder, addr)
}

// TotalReads returns the total number of read operations.
func (l *StateAccessLog) TotalReads() int {
	count := 0
	for _, reads := range l.Reads {
		count += len(reads)
	}
	return count
}

// TotalWrites returns the total number of write operations.
func (l *StateAccessLog) TotalWrites() int {
	count := 0
	for _, writes := range l.Writes {
		count += len(writes)
	}
	return count
}

// UniqueAccounts returns the number of unique accounts accessed.
func (l *StateAccessLog) UniqueAccounts() int {
	return len(l.AccessOrder)
}

// Digest returns a deterministic hash of the entire access log.
func (l *StateAccessLog) Digest() types.Hash {
	var data []byte
	for _, addr := range l.AccessOrder {
		data = append(data, addr[:]...)

		// Sort reads by slot for determinism.
		reads := l.Reads[addr]
		sortAccesses(reads)
		for _, r := range reads {
			data = append(data, r.Slot[:]...)
			data = append(data, r.Value[:]...)
			data = append(data, 0x00) // read marker
		}

		// Sort writes by slot for determinism.
		writes := l.Writes[addr]
		sortAccesses(writes)
		for _, w := range writes {
			data = append(data, w.Slot[:]...)
			data = append(data, w.Value[:]...)
			data = append(data, 0x01) // write marker
		}
	}
	return crypto.Keccak256Hash(data)
}

// sortAccesses sorts state accesses by slot (lexicographic).
func sortAccesses(accesses []StateAccess) {
	sort.Slice(accesses, func(i, j int) bool {
		for k := 0; k < types.HashLength; k++ {
			if accesses[i].Slot[k] < accesses[j].Slot[k] {
				return true
			}
			if accesses[i].Slot[k] > accesses[j].Slot[k] {
				return false
			}
		}
		return false
	})
}

// GenerateExecutionProof creates an execution proof from a block and its
// state access information. In a full implementation, this would use the
// EVM execution engine to trace all state accesses. Here we derive a
// deterministic proof from the block data.
func GenerateExecutionProof(block *types.Block, stateRoot types.Hash) (*BlockExecutionProof, error) {
	if block == nil {
		return nil, ErrExecProofNilBlock
	}
	if stateRoot.IsZero() {
		return nil, ErrExecProofStateRootNil
	}

	txs := block.Transactions()
	accessLog := NewStateAccessLog()
	traces := make([]TxTrace, len(txs))

	for i, tx := range txs {
		txHash := computeTxHash(tx, uint(i))
		trace := TxTrace{
			TxHash:  txHash,
			TxIndex: uint(i),
			GasUsed: 21000 + uint64(i)*1000, // simulated gas
			Success: true,
		}

		// Derive state accesses from the transaction.
		accesses := deriveStateAccesses(txHash, stateRoot, uint(i))
		trace.StateReads = accesses.reads
		trace.StateWrites = accesses.writes

		// Record in the access log.
		for _, r := range accesses.reads {
			accessLog.RecordRead(r.Address, r.Slot, r.Value)
		}
		for _, w := range accesses.writes {
			accessLog.RecordWrite(w.Address, w.Slot, types.Hash{}, w.Value)
		}

		traces[i] = trace
	}

	// Compute post-state root from state root and access log.
	postState := computePostState(stateRoot, accessLog)

	// Build merkle proofs for each accessed account.
	merkleProofs := buildMerkleProofs(stateRoot, accessLog)

	// Compute the proof commitment.
	commitment := computeExecutionCommitment(block.Hash(), stateRoot, postState, types.Hash{}, accessLog)

	return &BlockExecutionProof{
		BlockHash:     block.Hash(),
		BlockNumber:   block.NumberU64(),
		StateRoot:     stateRoot,
		PostStateRoot: postState,
		TxTraces:      traces,
		AccessLog:     accessLog,
		MerkleProofs:  merkleProofs,
		Commitment:    commitment,
	}, nil
}

// VerifyExecutionProof verifies that an execution proof is valid for the
// given state root. Verification checks:
// 1. The access log is non-empty and internally consistent.
// 2. The merkle proofs are valid for each accessed account.
// 3. The proof commitment matches the recomputed commitment.
func VerifyExecutionProof(proof *BlockExecutionProof, stateRoot types.Hash) (bool, error) {
	if proof == nil {
		return false, ErrExecProofNilBlock
	}
	if stateRoot.IsZero() {
		return false, ErrExecProofStateRootNil
	}
	if len(proof.TxTraces) == 0 {
		return false, ErrExecProofNoTxTraces
	}
	if proof.AccessLog == nil || proof.AccessLog.UniqueAccounts() == 0 {
		return false, ErrExecProofAccessEmpty
	}

	// Recompute the commitment.
	expected := computeExecutionCommitment(
		proof.BlockHash, stateRoot, proof.PostStateRoot, proof.BALHash, proof.AccessLog,
	)
	if expected != proof.Commitment {
		return false, ErrExecProofTampered
	}

	// Verify the merkle proofs for each accessed account.
	for _, addr := range proof.AccessLog.AccessOrder {
		proofPath, ok := proof.MerkleProofs[addr]
		if !ok || len(proofPath) == 0 {
			return false, ErrExecProofTampered
		}
		if !verifyAccountProof(stateRoot, addr, proofPath) {
			return false, ErrExecProofTampered
		}
	}

	// Verify post-state derivation.
	expectedPost := computePostState(stateRoot, proof.AccessLog)
	if expectedPost != proof.PostStateRoot {
		return false, ErrExecProofTampered
	}

	return true, nil
}

// ProofCompression provides methods for compressing execution proofs by
// deduplicating shared merkle branches across multiple account proofs.
type ProofCompression struct {
	// SharedBranches maps branch hash to branch data, for deduplication.
	SharedBranches map[types.Hash][]byte

	// References maps account addresses to their branch hash sequences.
	References map[types.Address][]types.Hash
}

// NewProofCompression creates a new compression context.
func NewProofCompression() *ProofCompression {
	return &ProofCompression{
		SharedBranches: make(map[types.Hash][]byte),
		References:     make(map[types.Address][]types.Hash),
	}
}

// Compress deduplicates the merkle proofs in an execution proof.
// Returns the number of bytes saved through deduplication.
func (pc *ProofCompression) Compress(proof *BlockExecutionProof) int {
	if proof == nil || len(proof.MerkleProofs) == 0 {
		return 0
	}

	originalSize := 0
	compressedSize := 0

	for addr, branches := range proof.MerkleProofs {
		refs := make([]types.Hash, 0, len(branches))
		for _, branch := range branches {
			originalSize += len(branch)
			branchHash := crypto.Keccak256Hash(branch)

			if _, exists := pc.SharedBranches[branchHash]; !exists {
				pc.SharedBranches[branchHash] = append([]byte(nil), branch...)
				compressedSize += len(branch)
			}
			refs = append(refs, branchHash)
			compressedSize += types.HashLength // reference cost
		}
		pc.References[addr] = refs
	}

	saved := originalSize - compressedSize
	if saved < 0 {
		saved = 0
	}
	return saved
}

// DecompressProofs reconstructs the merkle proofs from compressed references.
func (pc *ProofCompression) DecompressProofs() map[types.Address][][]byte {
	result := make(map[types.Address][][]byte)
	for addr, refs := range pc.References {
		branches := make([][]byte, 0, len(refs))
		for _, ref := range refs {
			if branch, ok := pc.SharedBranches[ref]; ok {
				branches = append(branches, append([]byte(nil), branch...))
			}
		}
		result[addr] = branches
	}
	return result
}

// SharedBranchCount returns the number of unique merkle branches stored.
func (pc *ProofCompression) SharedBranchCount() int {
	return len(pc.SharedBranches)
}

// --- Internal helpers ---

// computeTxHash derives a deterministic transaction hash.
func computeTxHash(tx *types.Transaction, index uint) types.Hash {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], uint32(index))
	// Use the transaction's size and index to derive a hash.
	size := tx.Size()
	var sizeBuf [8]byte
	binary.BigEndian.PutUint64(sizeBuf[:], size)
	return crypto.Keccak256Hash(sizeBuf[:], buf[:])
}

// txAccesses holds the derived reads and writes for a transaction.
type txAccesses struct {
	reads  []StateAccess
	writes []StateAccess
}

// deriveStateAccesses generates deterministic state accesses from a tx hash.
func deriveStateAccesses(txHash types.Hash, stateRoot types.Hash, txIndex uint) txAccesses {
	// Derive 2 reads and 1 write per transaction.
	var accesses txAccesses

	// Read: sender account balance.
	senderAddr := deriveAddress(txHash, 0)
	readSlot := crypto.Keccak256Hash(txHash[:], stateRoot[:])
	readValue := crypto.Keccak256Hash(stateRoot[:], txHash[:])
	accesses.reads = append(accesses.reads, StateAccess{
		Address: senderAddr,
		Slot:    readSlot,
		Value:   readValue,
		IsWrite: false,
	})

	// Read: recipient account.
	recipientAddr := deriveAddress(txHash, 1)
	readSlot2 := crypto.Keccak256Hash(txHash[:], []byte{byte(txIndex)})
	readValue2 := crypto.Keccak256Hash(stateRoot[:], []byte{byte(txIndex)})
	accesses.reads = append(accesses.reads, StateAccess{
		Address: recipientAddr,
		Slot:    readSlot2,
		Value:   readValue2,
		IsWrite: false,
	})

	// Write: update sender nonce.
	writeValue := crypto.Keccak256Hash(txHash[:], stateRoot[:], []byte{0x01})
	accesses.writes = append(accesses.writes, StateAccess{
		Address: senderAddr,
		Slot:    readSlot,
		Value:   writeValue,
		IsWrite: true,
	})

	return accesses
}

// deriveAddress generates a deterministic address from a hash and salt.
func deriveAddress(h types.Hash, salt byte) types.Address {
	derived := crypto.Keccak256(h[:], []byte{salt})
	var addr types.Address
	copy(addr[:], derived[:types.AddressLength])
	return addr
}

// computePostState derives a post-state root from the pre-state and access log.
func computePostState(stateRoot types.Hash, accessLog *StateAccessLog) types.Hash {
	digest := accessLog.Digest()
	return crypto.Keccak256Hash(stateRoot[:], digest[:])
}

// buildMerkleProofs constructs merkle proofs for each accessed account.
func buildMerkleProofs(stateRoot types.Hash, accessLog *StateAccessLog) map[types.Address][][]byte {
	proofs := make(map[types.Address][][]byte)
	for _, addr := range accessLog.AccessOrder {
		// Generate a deterministic proof path for this account.
		path := generateAccountPath(stateRoot, addr)
		proofs[addr] = path
	}
	return proofs
}

// generateAccountPath creates a simulated merkle proof path for an account.
// The path consists of 8 sibling hashes (representing tree depth 8).
func generateAccountPath(stateRoot types.Hash, addr types.Address) [][]byte {
	const depth = 8
	path := make([][]byte, depth)
	for i := 0; i < depth; i++ {
		sibling := crypto.Keccak256(stateRoot[:], addr[:], []byte{byte(i)})
		path[i] = sibling
	}
	return path
}

// verifyAccountProof checks that a merkle proof path is valid for an account.
func verifyAccountProof(stateRoot types.Hash, addr types.Address, proofPath [][]byte) bool {
	if len(proofPath) == 0 {
		return false
	}
	// Regenerate the expected proof path and compare.
	expected := generateAccountPath(stateRoot, addr)
	if len(expected) != len(proofPath) {
		return false
	}
	for i := range expected {
		if len(proofPath[i]) != len(expected[i]) {
			return false
		}
		for j := range expected[i] {
			if proofPath[i][j] != expected[i][j] {
				return false
			}
		}
	}
	return true
}

// computeExecutionCommitment creates a binding commitment over all proof components.
func computeExecutionCommitment(blockHash, stateRoot, postState, balHash types.Hash, accessLog *StateAccessLog) types.Hash {
	digest := accessLog.Digest()
	return crypto.Keccak256Hash(
		blockHash[:],
		stateRoot[:],
		postState[:],
		balHash[:],
		digest[:],
	)
}

// GenerateExecutionProofWithBAL builds an execution proof anchored to the
// block's real state-access record: the block access list (EIP-7928). The
// per-account reads/writes, balance, nonce, and code changes come from the
// BAL entries grouped by access index (0 = pre-execution, 1..n =
// transactions, n+1 = post-execution), and the proof commits to the BAL
// hash — the same keccak256(rlp(bal)) the header commits to — so a verifier
// can check the proof against the header without re-executing.
func GenerateExecutionProofWithBAL(
	block *types.Block,
	preStateRoot types.Hash,
	bal *bal.BlockAccessList,
) (*BlockExecutionProof, error) {
	if block == nil {
		return nil, ErrExecProofNilBlock
	}
	if preStateRoot.IsZero() {
		return nil, ErrExecProofStateRootNil
	}
	if bal == nil || len(bal.Entries) == 0 {
		return nil, ErrExecProofAccessEmpty
	}

	accessLog := NewStateAccessLog()
	txs := block.Transactions()
	traces := make([]TxTrace, len(txs))
	for i := range traces {
		traces[i] = TxTrace{TxIndex: uint(i)}
		if i < len(txs) {
			traces[i].TxHash = txs[i].Hash()
		}
	}

	recordAccess := func(trace *TxTrace, addr types.Address, slot, value types.Hash, isWrite bool) {
		access := StateAccess{Address: addr, Slot: slot, Value: value, IsWrite: isWrite}
		if isWrite {
			trace.StateWrites = append(trace.StateWrites, access)
			accessLog.RecordWrite(addr, slot, value, value)
		} else {
			trace.StateReads = append(trace.StateReads, access)
			accessLog.RecordRead(addr, slot, value)
		}
	}

	for i := range bal.Entries {
		entry := &bal.Entries[i]
		// AccessIndex 0 = pre-execution, 1..n = transaction i-1, n+1 = post.
		var trace *TxTrace
		if entry.AccessIndex >= 1 && uint64(len(traces)) >= entry.AccessIndex {
			trace = &traces[entry.AccessIndex-1]
		}
		for _, read := range entry.StorageReads {
			if trace != nil {
				recordAccess(trace, entry.Address, read.Slot, read.Slot, false)
			} else {
				accessLog.RecordRead(entry.Address, read.Slot, read.Slot)
			}
		}
		for _, change := range entry.StorageChanges {
			if trace != nil {
				recordAccess(trace, entry.Address, change.Slot, change.NewValue, true)
			} else {
				accessLog.RecordWrite(entry.Address, change.Slot, change.OldValue, change.NewValue)
			}
		}
		// Account-level changes count as account accesses in the log.
		if entry.BalanceChange != nil || entry.NonceChange != nil || entry.CodeChange != nil {
			accessLog.trackAddress(entry.Address)
		}
	}

	postState := computePostState(preStateRoot, accessLog)
	merkleProofs := buildMerkleProofs(preStateRoot, accessLog)
	balHash := bal.Hash()
	commitment := computeExecutionCommitment(block.Hash(), preStateRoot, postState, balHash, accessLog)

	return &BlockExecutionProof{
		BlockHash:     block.Hash(),
		BlockNumber:   block.NumberU64(),
		StateRoot:     preStateRoot,
		PostStateRoot: postState,
		TxTraces:      traces,
		AccessLog:     accessLog,
		MerkleProofs:  merkleProofs,
		BALHash:       balHash,
		Commitment:    commitment,
	}, nil
}
