// column_builder.go implements a DataColumn builder for PeerDAS (EIP-7594).
// It constructs data columns from blob data, assigns custody responsibilities
// based on node ID, computes column commitments using KZG, and supports
// column-level gossip messages with validation and deduplication.
//
// Reference: consensus-specs/specs/fulu/das-core.md
package gossip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/crypto/bls"
	"github.com/eth2030/eth2030/das/dastypes"
	"github.com/eth2030/eth2030/das/sampling"
	"golang.org/x/crypto/sha3"
)

// Column builder errors.
var (
	ErrBuilderNilBlobs       = errors.New("das/colbuilder: nil blob data")
	ErrBuilderEmptyBlobs     = errors.New("das/colbuilder: empty blob data")
	ErrBuilderBlobTooLarge   = errors.New("das/colbuilder: blob exceeds maximum size")
	ErrBuilderColumnOOB      = errors.New("das/colbuilder: column index out of range")
	ErrBuilderDuplicateCol   = errors.New("das/colbuilder: duplicate column in batch")
	ErrBuilderInvalidCell    = errors.New("das/colbuilder: cell data size mismatch")
	ErrBuilderMsgNil         = errors.New("das/colbuilder: nil gossip message")
	ErrBuilderMsgColOOB      = errors.New("das/colbuilder: gossip message column out of range")
	ErrBuilderMsgBlobOOB     = errors.New("das/colbuilder: gossip message blob index out of range")
	ErrBuilderMsgDataInvalid = errors.New("das/colbuilder: gossip message data invalid")
	ErrBuilderAlreadySeen    = errors.New("das/colbuilder: message already seen (duplicate)")
)

// ColumnBuilderConfig configures the column builder.
type ColumnBuilderConfig struct {
	// NumColumns is the total columns in the extended data matrix.
	NumColumns int

	// MaxBlobs is the maximum number of blobs per block.
	MaxBlobs int

	// CellSize is the limit for legacy short gossip messages. KZG cells
	// always use the standard size.
	CellSize int

	// MaxDedup is the capacity of the deduplication set.
	MaxDedup int
}

// DefaultColumnBuilderConfig returns production defaults from the Fulu spec.
func DefaultColumnBuilderConfig() ColumnBuilderConfig {
	return ColumnBuilderConfig{
		NumColumns: dastypes.NumberOfColumns,
		MaxBlobs:   dastypes.MaxBlobCommitmentsPerBlock,
		CellSize:   dastypes.BytesPerCell,
		MaxDedup:   8192,
	}
}

// BuiltColumn represents a fully constructed data column with its computed
// commitment and associated metadata.
type BuiltColumn struct {
	// Index is the column index in [0, NumColumns).
	Index dastypes.ColumnIndex

	// Cells contains one cell per blob in the block.
	Cells []dastypes.Cell

	// Commitment is the keccak256 commitment over all cells in the column.
	Commitment [32]byte

	// Proofs contains one proof per cell (blob) in the column.
	Proofs []dastypes.KZGProof

	// BlobCount is the number of blobs this column spans.
	BlobCount int
}

// ColumnGossipMessage is a network gossip message carrying a single column
// with validation metadata.
type ColumnGossipMessage struct {
	// ColumnIndex identifies which column this message carries.
	ColumnIndex dastypes.ColumnIndex

	// Slot is the beacon slot this column belongs to.
	Slot uint64

	// BlobIndex identifies which blob this cell comes from.
	BlobIndex uint64

	// CellData is the raw cell data.
	CellData []byte

	// Proof is the KZG proof for this cell.
	Proof dastypes.KZGProof

	// MessageHash is the deduplication hash for this message.
	MessageHash [32]byte
}

// CustodyAssignmentResult describes which columns a node should build and
// serve for custody purposes.
type CustodyAssignmentResult struct {
	NodeID        [32]byte
	ColumnIndices []dastypes.ColumnIndex
	GroupIndices  []dastypes.CustodyGroup
}

// ColumnBuilder constructs data columns from blob data, computes commitments,
// manages custody assignments, and handles column gossip deduplication.
// All public methods are safe for concurrent use.
type ColumnBuilder struct {
	mu     sync.RWMutex
	config ColumnBuilderConfig

	// builtColumns caches columns built in the current round.
	builtColumns map[dastypes.ColumnIndex]*BuiltColumn

	// dedupSet tracks seen gossip message hashes for deduplication.
	dedupSet map[[32]byte]bool

	// dedupOrder maintains insertion order for LRU eviction.
	dedupOrder [][32]byte
}

// NewColumnBuilder creates a new column builder with the given configuration.
func NewColumnBuilder(config ColumnBuilderConfig) *ColumnBuilder {
	if config.NumColumns <= 0 {
		config.NumColumns = dastypes.NumberOfColumns
	}
	if config.MaxBlobs <= 0 {
		config.MaxBlobs = dastypes.MaxBlobCommitmentsPerBlock
	}
	if config.CellSize <= 0 {
		config.CellSize = dastypes.BytesPerCell
	}
	if config.MaxDedup <= 0 {
		config.MaxDedup = 8192
	}
	return &ColumnBuilder{
		config:       config,
		builtColumns: make(map[dastypes.ColumnIndex]*BuiltColumn),
		dedupSet:     make(map[[32]byte]bool),
	}
}

// BuildColumns constructs data columns and cell proofs using the Ethereum KZG setup.
func (cb *ColumnBuilder) BuildColumns(blobData [][]byte) ([]*BuiltColumn, error) {
	indices := make([]dastypes.ColumnIndex, cb.config.NumColumns)
	for i := range indices {
		indices[i] = dastypes.ColumnIndex(i)
	}
	return cb.BuildColumnSubset(blobData, indices)
}

// BuildColumnSubset constructs only the specified column indices from blob data.
// This is useful for building just the custody columns a node is responsible for.
func (cb *ColumnBuilder) BuildColumnSubset(blobData [][]byte, indices []dastypes.ColumnIndex) ([]*BuiltColumn, error) {
	if blobData == nil {
		return nil, ErrBuilderNilBlobs
	}
	if len(blobData) == 0 {
		return nil, ErrBuilderEmptyBlobs
	}

	if len(blobData) > cb.config.MaxBlobs {
		return nil, fmt.Errorf("%w: %d blobs > max %d", ErrBuilderBlobTooLarge, len(blobData), cb.config.MaxBlobs)
	}
	numBlobs := len(blobData)

	// Check for duplicates and OOB.
	seen := make(map[dastypes.ColumnIndex]bool, len(indices))
	for _, idx := range indices {
		if uint64(idx) >= uint64(cb.config.NumColumns) || uint64(idx) >= bls.KZGCellsPerExtBlob {
			return nil, fmt.Errorf("%w: %d >= %d", ErrBuilderColumnOOB, idx, cb.config.NumColumns)
		}
		if seen[idx] {
			return nil, fmt.Errorf("%w: column %d", ErrBuilderDuplicateCol, idx)
		}
		seen[idx] = true
	}

	columns := make([]*BuiltColumn, len(indices))
	for i, idx := range indices {
		columns[i] = &BuiltColumn{
			Index: idx, BlobCount: numBlobs,
			Cells:  make([]dastypes.Cell, numBlobs),
			Proofs: make([]dastypes.KZGProof, numBlobs),
		}
	}
	kzg := bls.DefaultKZGBackend()
	for i, blob := range blobData {
		cells, proofs, err := kzg.ComputeCellsAndProofs(blob)
		if err != nil {
			return nil, fmt.Errorf("blob %d: %w", i, err)
		}
		if len(cells) != bls.KZGCellsPerExtBlob || len(proofs) != bls.KZGCellsPerExtBlob {
			return nil, fmt.Errorf("blob %d: invalid KZG cell count", i)
		}
		for _, col := range columns {
			col.Cells[i] = dastypes.Cell(cells[col.Index])
			col.Proofs[i] = dastypes.KZGProof(proofs[col.Index])
		}
	}
	for _, col := range columns {
		col.Commitment = computeColumnCommitment(col)
	}

	// Sort by index.
	sort.Slice(columns, func(i, j int) bool {
		return columns[i].Index < columns[j].Index
	})

	cb.mu.Lock()
	for _, col := range columns {
		cb.builtColumns[col.Index] = col
	}
	cb.mu.Unlock()

	return columns, nil
}

// GetBuiltColumn retrieves a previously built column by index.
func (cb *ColumnBuilder) GetBuiltColumn(idx dastypes.ColumnIndex) *BuiltColumn {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.builtColumns[idx]
}

// BuiltColumnCount returns how many columns are cached.
func (cb *ColumnBuilder) BuiltColumnCount() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return len(cb.builtColumns)
}

// AssignCustodyColumns computes the custody column indices for a given node.
func (cb *ColumnBuilder) AssignCustodyColumns(nodeID [32]byte, custodyGroupCount uint64) (*CustodyAssignmentResult, error) {
	groups, err := sampling.GetCustodyGroups(nodeID, custodyGroupCount)
	if err != nil {
		return nil, fmt.Errorf("das/colbuilder: custody groups: %w", err)
	}

	var columnIndices []dastypes.ColumnIndex
	for _, g := range groups {
		cols, err := sampling.ComputeColumnsForCustodyGroup(g)
		if err != nil {
			continue
		}
		columnIndices = append(columnIndices, cols...)
	}
	sort.Slice(columnIndices, func(i, j int) bool {
		return columnIndices[i] < columnIndices[j]
	})

	return &CustodyAssignmentResult{
		NodeID:        nodeID,
		ColumnIndices: columnIndices,
		GroupIndices:  groups,
	}, nil
}

// CreateGossipMessage creates a column gossip message for broadcasting a cell.
func (cb *ColumnBuilder) CreateGossipMessage(slot uint64, blobIdx uint64, colIdx dastypes.ColumnIndex, cellData []byte, proof dastypes.KZGProof) (*ColumnGossipMessage, error) {
	if uint64(colIdx) >= uint64(cb.config.NumColumns) {
		return nil, fmt.Errorf("%w: %d", ErrBuilderMsgColOOB, colIdx)
	}
	if blobIdx >= uint64(cb.config.MaxBlobs) {
		return nil, fmt.Errorf("%w: %d", ErrBuilderMsgBlobOOB, blobIdx)
	}
	if !cb.validCellSize(len(cellData)) {
		return nil, fmt.Errorf("%w: size %d", ErrBuilderMsgDataInvalid, len(cellData))
	}

	msg := &ColumnGossipMessage{
		ColumnIndex: colIdx,
		Slot:        slot,
		BlobIndex:   blobIdx,
		CellData:    append([]byte(nil), cellData...),
		Proof:       proof,
	}
	msg.MessageHash = computeGossipMessageHash(msg)
	return msg, nil
}

// ValidateGossipMessage performs structural validation on a gossip message.
func (cb *ColumnBuilder) ValidateGossipMessage(msg *ColumnGossipMessage) error {
	if msg == nil {
		return ErrBuilderMsgNil
	}
	if uint64(msg.ColumnIndex) >= uint64(cb.config.NumColumns) {
		return fmt.Errorf("%w: column %d >= %d", ErrBuilderMsgColOOB, msg.ColumnIndex, cb.config.NumColumns)
	}
	if msg.BlobIndex >= uint64(cb.config.MaxBlobs) {
		return fmt.Errorf("%w: blob %d >= %d", ErrBuilderMsgBlobOOB, msg.BlobIndex, cb.config.MaxBlobs)
	}
	if !cb.validCellSize(len(msg.CellData)) {
		return fmt.Errorf("%w: size %d", ErrBuilderMsgDataInvalid, len(msg.CellData))
	}
	return nil
}

func (cb *ColumnBuilder) validCellSize(size int) bool {
	return size == dastypes.BytesPerCell || size > 0 && size < dastypes.BytesPerCell && size <= cb.config.CellSize
}

// CheckDuplicate checks if a gossip message has already been seen.
// Returns ErrBuilderAlreadySeen if the message is a duplicate, nil otherwise.
// Non-duplicate messages are added to the dedup set.
func (cb *ColumnBuilder) CheckDuplicate(msg *ColumnGossipMessage) error {
	if msg == nil {
		return ErrBuilderMsgNil
	}

	hash := msg.MessageHash
	if hash == [32]byte{} {
		hash = computeGossipMessageHash(msg)
	}

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if cb.dedupSet[hash] {
		return ErrBuilderAlreadySeen
	}

	// Evict oldest if at capacity.
	if len(cb.dedupOrder) >= cb.config.MaxDedup {
		oldest := cb.dedupOrder[0]
		cb.dedupOrder = cb.dedupOrder[1:]
		delete(cb.dedupSet, oldest)
	}

	cb.dedupSet[hash] = true
	cb.dedupOrder = append(cb.dedupOrder, hash)
	return nil
}

// DedupSize returns the current size of the deduplication set.
func (cb *ColumnBuilder) DedupSize() int {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return len(cb.dedupSet)
}

// ClearBuilt clears the built column cache.
func (cb *ColumnBuilder) ClearBuilt() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.builtColumns = make(map[dastypes.ColumnIndex]*BuiltColumn)
}

// ClearDedup clears the deduplication set.
func (cb *ColumnBuilder) ClearDedup() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.dedupSet = make(map[[32]byte]bool)
	cb.dedupOrder = nil
}

// ToDataColumnSidecar converts a BuiltColumn into a DataColumnSidecar
// suitable for network transmission.
func (cb *ColumnBuilder) ToDataColumnSidecar(col *BuiltColumn, commitments []dastypes.KZGCommitment) *dastypes.DataColumnSidecar {
	sidecar := &dastypes.DataColumnSidecar{
		Index:          col.Index,
		Column:         make([]dastypes.Cell, len(col.Cells)),
		KZGCommitments: make([]dastypes.KZGCommitment, len(commitments)),
		KZGProofs:      make([]dastypes.KZGProof, len(col.Proofs)),
	}
	copy(sidecar.Column, col.Cells)
	copy(sidecar.KZGCommitments, commitments)
	copy(sidecar.KZGProofs, col.Proofs)
	return sidecar
}

// --- internal helpers ---

// computeColumnCommitment binds the column index and cells for the local cache.
func computeColumnCommitment(col *BuiltColumn) [32]byte {
	h := sha3.NewLegacyKeccak256()
	var index [8]byte
	binary.LittleEndian.PutUint64(index[:], uint64(col.Index))
	h.Write(index[:])
	for _, cell := range col.Cells {
		h.Write(cell[:])
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

func computeGossipMessageHash(msg *ColumnGossipMessage) [32]byte {
	h := sha3.NewLegacyKeccak256()
	var buf [24]byte
	binary.LittleEndian.PutUint64(buf[:8], msg.Slot)
	binary.LittleEndian.PutUint64(buf[8:16], msg.BlobIndex)
	binary.LittleEndian.PutUint64(buf[16:], uint64(msg.ColumnIndex))
	h.Write(buf[:])
	h.Write(msg.CellData)
	var hash [32]byte
	copy(hash[:], h.Sum(nil))
	return hash
}
