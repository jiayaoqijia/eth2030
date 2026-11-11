// Package dastypes provides fundamental PeerDAS data types per EIP-7594.
package dastypes

import (
	"errors"
	"fmt"
)

// Sidecar validation errors.
var (
	ErrSidecarNil      = errors.New("dastypes: nil data column sidecar")
	ErrSidecarNoHeader = errors.New("dastypes: sidecar missing signed block header")
)

// PeerDAS constants from the Fulu consensus spec.
const (
	// NumberOfColumns is the number of columns in the extended data matrix.
	// Equal to CELLS_PER_EXT_BLOB.
	NumberOfColumns = 128

	// NumberOfCustodyGroups is the number of custody groups available for
	// nodes to custody.
	NumberOfCustodyGroups = 128

	// CustodyRequirement is the minimum number of custody groups an honest
	// node custodies and serves samples from.
	CustodyRequirement = 4

	// SamplesPerSlot is the minimum number of samples for an honest node.
	SamplesPerSlot = 8

	// DataColumnSidecarSubnetCount is the number of subnets used to gossip
	// data column sidecars.
	DataColumnSidecarSubnetCount = 64

	// FieldElementsPerBlob is the number of field elements in a blob (4096).
	FieldElementsPerBlob = 4096

	// FieldElementsPerExtBlob is the number of field elements in an extended blob.
	FieldElementsPerExtBlob = 2 * FieldElementsPerBlob

	// FieldElementsPerCell is the number of field elements in a single cell.
	FieldElementsPerCell = 64

	// BytesPerFieldElement is the byte size of a BLS scalar field element.
	BytesPerFieldElement = 32

	// BytesPerCell is the byte size of a single cell.
	BytesPerCell = FieldElementsPerCell * BytesPerFieldElement // 2048

	// CellsPerExtBlob is the number of cells in an extended blob.
	CellsPerExtBlob = FieldElementsPerExtBlob / FieldElementsPerCell // 128

	// MaxBlobCommitmentsPerBlock is the maximum number of blob commitments
	// per block (EIP-7691 increase).
	MaxBlobCommitmentsPerBlock = 9

	// ReconstructionThreshold is the minimum fraction of columns needed
	// for reconstruction (50%).
	ReconstructionThreshold = NumberOfColumns / 2
)

// SubnetID identifies a data column sidecar gossip subnet.
type SubnetID uint64

// CustodyGroup identifies a custody group.
type CustodyGroup uint64

// ColumnIndex identifies a column in the extended data matrix.
type ColumnIndex uint64

// RowIndex identifies a row (blob) in the extended data matrix.
type RowIndex uint64

// Cell is the smallest unit of blob data that can come with its own KZG proof.
// It contains FIELD_ELEMENTS_PER_CELL field elements.
type Cell [BytesPerCell]byte

// KZGCommitment is a 48-byte compressed BLS12-381 G1 point representing a
// polynomial commitment.
type KZGCommitment [48]byte

// KZGProof is a 48-byte compressed BLS12-381 G1 point representing a KZG proof.
type KZGProof [48]byte

// DataColumn holds the cells for a single column index across all blobs in a block.
type DataColumn struct {
	Index     ColumnIndex
	Cells     []Cell
	KZGProofs []KZGProof
}

// DataColumnSidecar is the network-level container for a data column, including
// the KZG commitments and an inclusion proof as defined in the Fulu DAS spec.
type DataColumnSidecar struct {
	// Index is the column index in [0, NUMBER_OF_COLUMNS).
	Index ColumnIndex

	// Column contains one cell per blob in the block.
	Column []Cell

	// KZGCommitments contains one commitment per blob in the block.
	KZGCommitments []KZGCommitment

	// KZGProofs contains one proof per blob in the block.
	KZGProofs []KZGProof

	// SignedBlockHeader is the signed beacon block header the sidecar
	// belongs to, per the Fulu DataColumnSidecar container. Gossip
	// validation uses its slot for clock-disparity and finalization
	// checks; signature verification stays in the consensus layer.
	SignedBlockHeader *SignedBeaconBlockHeader

	// InclusionProof is the Merkle proof for KZG commitments inclusion.
	InclusionProof [][32]byte
}

// SignedBeaconBlockHeader is the consensus signed beacon block header as
// referenced by DataColumnSidecar.SignedBlockHeader.
type SignedBeaconBlockHeader struct {
	Slot       uint64
	ProposerIndex uint64
	ParentRoot [32]byte
	StateRoot  [32]byte
	BodyRoot   [32]byte
	Signature  [96]byte
}

// ComputeSubnetForDataColumnSidecar returns the gossip subnet for a column,
// per the Fulu p2p spec: column_index mod DATA_COLUMN_SIDECARS_SUBNET_COUNT.
func ComputeSubnetForDataColumnSidecar(index ColumnIndex) uint64 {
	return uint64(index) % DataColumnSidecarSubnetCount
}

// ValidateSidecarSubnet enforces the Fulu gossip REJECT rule: the sidecar
// must be published on compute_subnet_for_data_column_sidecar(sidecar.index).
func ValidateSidecarSubnet(sidecar *DataColumnSidecar, subnetID uint64) error {
	if sidecar == nil {
		return ErrSidecarNil
	}
	if got := ComputeSubnetForDataColumnSidecar(sidecar.Index); got != subnetID {
		return fmt.Errorf("sidecar column %d belongs to subnet %d, got %d",
			sidecar.Index, got, subnetID)
	}
	return nil
}

// ValidateSidecarSlot enforces the Fulu gossip IGNORE rules on slots: the
// sidecar must not be from a future slot (MAXIMUM_GOSSIP_CLOCK_DISPARITY
// handled by the caller's currentSlot) and must be newer than the latest
// finalized slot.
func ValidateSidecarSlot(sidecar *DataColumnSidecar, currentSlot, finalizedSlot uint64) error {
	if sidecar == nil {
		return ErrSidecarNil
	}
	if sidecar.SignedBlockHeader == nil {
		return ErrSidecarNoHeader
	}
	if sidecar.SignedBlockHeader.Slot > currentSlot {
		return fmt.Errorf("sidecar slot %d is in the future (current %d)",
			sidecar.SignedBlockHeader.Slot, currentSlot)
	}
	if sidecar.SignedBlockHeader.Slot <= finalizedSlot {
		return fmt.Errorf("sidecar slot %d not newer than finalized slot %d",
			sidecar.SignedBlockHeader.Slot, finalizedSlot)
	}
	return nil
}

// MatrixEntry represents a single cell in the extended data matrix along with
// its proof and position.
type MatrixEntry struct {
	Cell        Cell
	KZGProof    KZGProof
	ColumnIndex ColumnIndex
	RowIndex    RowIndex
}

// STARKCommitment represents a STARK-based data availability commitment.
// This provides post-quantum security for blob commitments, replacing
// KZG commitments when UseSTARKDA is enabled.
type STARKCommitment struct {
	// Root is the Merkle root of the committed data.
	Root [32]byte
	// ProofSize is the size of the STARK proof in bytes.
	ProofSize uint32
	// BlowupFactor is the Reed-Solomon expansion factor.
	BlowupFactor uint8
}
