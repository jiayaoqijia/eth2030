// KZG trusted setup integration layer for EIP-4844 and EIP-7594.
//
// This file provides a KZGCeremonyBackend interface that abstracts KZG
// polynomial commitment operations, allowing the codebase to switch between
// an injected backend and the production
// go-eth-kzg library (github.com/crate-crypto/go-eth-kzg).
//
// Key EIP-4844 constants (FIELD_ELEMENTS_PER_BLOB=4096, BytesPerBlob=131072)
// and EIP-7594 constants (CellsPerExtBlob=128, FieldElementsPerCell=64) are
// defined here to match the consensus spec and go-eth-kzg.
//
// The BLS_MODULUS constant matches the go-eth-kzg BlsModulus byte array
// exactly, representing the BLS12-381 scalar field order.
//
// ValidateBlob and ValidateCommitment perform format checks that mirror the
// validation done in the consensus spec (blob_to_polynomial, validate_kzg_g1).
package bls

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"sync"

	goethkzg "github.com/crate-crypto/go-eth-kzg"
)

// EIP-4844 constants matching the consensus spec and go-eth-kzg/serialization.go.
const (
	// KZGFieldElementsPerBlob is the number of field elements in a blob.
	// Matches FIELD_ELEMENTS_PER_BLOB in the spec.
	KZGFieldElementsPerBlob = 4096

	// KZGBytesPerFieldElement is the serialized size of a single BLS scalar.
	// Matches BYTES_PER_FIELD_ELEMENT in the spec.
	KZGBytesPerFieldElement = 32

	// KZGBytesPerBlob is the total byte size of a blob.
	// = FIELD_ELEMENTS_PER_BLOB * BYTES_PER_FIELD_ELEMENT = 4096 * 32 = 131072.
	KZGBytesPerBlob = KZGFieldElementsPerBlob * KZGBytesPerFieldElement

	// KZGBytesPerCommitment is the size of a KZG commitment (compressed G1 point).
	KZGBytesPerCommitment = 48

	// KZGBytesPerProof is the size of a KZG proof (compressed G1 point).
	KZGBytesPerProof = 48
)

// EIP-7594 PeerDAS constants matching go-eth-kzg/serialization.go.
const (
	// KZGCellsPerExtBlob is the number of cells in an extended blob.
	// The extended blob uses a 2x expansion factor.
	KZGCellsPerExtBlob = 128

	// KZGFieldElementsPerCell is the number of scalars per cell.
	KZGFieldElementsPerCell = 64

	// KZGBytesPerCell is the byte size of a single cell.
	// = FieldElementsPerCell * BytesPerFieldElement = 64 * 32 = 2048.
	KZGBytesPerCell = KZGFieldElementsPerCell * KZGBytesPerFieldElement

	// KZGExpansionFactor is the factor by which the blob is extended
	// for Reed-Solomon erasure coding.
	KZGExpansionFactor = 2

	// KZGScalarsPerExtBlob is the total number of scalars in an extended blob.
	KZGScalarsPerExtBlob = KZGExpansionFactor * KZGFieldElementsPerBlob
)

// KZGBLSModulus is the BLS12-381 scalar field modulus as a 32-byte big-endian
// array. This matches go-eth-kzg's BlsModulus exactly.
//
// r = 0x73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001
//
// Source: consensus-specs/specs/deneb/polynomial-commitments.md#constants
var KZGBLSModulus = [32]byte{
	0x73, 0xed, 0xa7, 0x53, 0x29, 0x9d, 0x7d, 0x48,
	0x33, 0x39, 0xd8, 0x08, 0x09, 0xa1, 0xd8, 0x05,
	0x53, 0xbd, 0xa4, 0x02, 0xff, 0xfe, 0x5b, 0xfe,
	0xff, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x01,
}

// KZG validation errors.
var (
	ErrKZGInvalidBlobSize         = errors.New("kzg: blob size must be 131072 bytes")
	ErrKZGFieldElementOutOfRange  = errors.New("kzg: field element >= BLS_MODULUS")
	ErrKZGInvalidCommitmentSize   = errors.New("kzg: commitment must be 48 bytes")
	ErrKZGInvalidCommitmentFormat = errors.New("kzg: invalid commitment G1 format")
	ErrKZGInvalidProofSize        = errors.New("kzg: proof must be 48 bytes")
	ErrKZGInvalidCellIndex        = errors.New("kzg: cell index >= CellsPerExtBlob")
	ErrKZGBackendNotImplemented   = errors.New("kzg: backend operation not implemented")
)

// KZGCeremonyConfig holds configuration for a KZG trusted setup.
// In production, this would be populated from the Ethereum KZG ceremony
// output (powers-of-tau).
type KZGCeremonyConfig struct {
	// SRSG1Lagrange contains the G1 Lagrange basis points from the ceremony.
	// For EIP-4844: 4096 points. Each point is a 48-byte compressed G1.
	SRSG1Lagrange [][]byte

	// SRSG2 contains the G2 points from the ceremony.
	// Minimum 2 points: [1]G2, [tau]G2.
	SRSG2 [][]byte

	// Modulus is the BLS12-381 scalar field modulus.
	Modulus [32]byte
}

// KZGCeremonyBackend is the interface for KZG polynomial commitment operations.
// The default backend uses the Ethereum ceremony parameters from go-eth-kzg.
type KZGCeremonyBackend interface {
	// BlobToCommitment computes a KZG commitment for a blob.
	// blob must be exactly BytesPerBlob bytes.
	// Returns a 48-byte compressed G1 commitment.
	BlobToCommitment(blob []byte) ([KZGBytesPerCommitment]byte, error)

	// VerifyBlobProof verifies a KZG proof for a blob against a commitment.
	// blob: BytesPerBlob bytes, commitment: 48 bytes, proof: 48 bytes.
	VerifyBlobProof(blob, commitment, proof []byte) (bool, error)

	// ComputeCells computes the extended blob cells for PeerDAS (EIP-7594).
	// blob must be exactly BytesPerBlob bytes.
	// Returns CellsPerExtBlob cells, each BytesPerCell bytes.
	ComputeCells(blob []byte) ([][KZGBytesPerCell]byte, error)

	// VerifyCellProof verifies a KZG proof for a single cell.
	// commitment: 48 bytes, cell: BytesPerCell bytes, proof: 48 bytes.
	VerifyCellProof(commitment, cell, proof []byte, cellIndex uint64) (bool, error)

	// ComputeCellsAndProofs computes both the 128 extended-blob cells and their
	// per-cell KZG proofs for PeerDAS (EIP-7594).
	// blob must be exactly KZGBytesPerBlob bytes.
	// Returns (CellsPerExtBlob cells, CellsPerExtBlob proofs, error).
	ComputeCellsAndProofs(blob []byte) ([][KZGBytesPerCell]byte, [][KZGBytesPerProof]byte, error)

	// Name returns a human-readable name for the backend.
	Name() string
}

// activeKZGBackend is the currently selected KZG backend.
var (
	activeKZGMu      sync.RWMutex
	activeKZGBackend KZGCeremonyBackend
)

// DefaultKZGBackend returns the currently active KZG backend.
func DefaultKZGBackend() KZGCeremonyBackend {
	activeKZGMu.RLock()
	backend := activeKZGBackend
	activeKZGMu.RUnlock()
	if backend != nil {
		return backend
	}
	standard, err := NewGoEthKZGBackend()
	if err != nil {
		return &GoEthKZGBackend{} // operations return an initialization error
	}
	return standard
}

// SetKZGBackend sets the active KZG backend. This is safe for concurrent use.
// Passing nil resets to the Ethereum ceremony backend.
func SetKZGBackend(b KZGCeremonyBackend) {
	activeKZGMu.Lock()
	defer activeKZGMu.Unlock()
	activeKZGBackend = b
}

// KZGIntegrationStatus returns the name of the currently active KZG backend.
func KZGIntegrationStatus() string {
	return DefaultKZGBackend().Name()
}

// ValidateBlob checks that a blob has the correct size and that each
// 32-byte field element is canonical (less than BLS_MODULUS).
//
// This mirrors blob_to_polynomial in the consensus spec.
func ValidateBlob(blob []byte) error {
	if len(blob) != KZGBytesPerBlob {
		return ErrKZGInvalidBlobSize
	}
	modulus := new(big.Int).SetBytes(KZGBLSModulus[:])
	for i := 0; i < KZGFieldElementsPerBlob; i++ {
		offset := i * KZGBytesPerFieldElement
		elem := blob[offset : offset+KZGBytesPerFieldElement]
		val := new(big.Int).SetBytes(elem)
		if val.Cmp(modulus) >= 0 {
			return ErrKZGFieldElementOutOfRange
		}
	}
	return nil
}

// ValidateCommitment checks that a KZG commitment has the correct size
// and valid compressed G1 format (compression flag set).
//
// This mirrors validate_kzg_g1 in the consensus spec.
func ValidateCommitment(commitment []byte) error {
	if len(commitment) != KZGBytesPerCommitment {
		return ErrKZGInvalidCommitmentSize
	}
	// Compression flag (bit 7) must be set.
	if commitment[0]&0x80 == 0 {
		return ErrKZGInvalidCommitmentFormat
	}
	return nil
}

// ValidateProof checks that a KZG proof has the correct size.
func ValidateProof(proof []byte) error {
	if len(proof) != KZGBytesPerProof {
		return ErrKZGInvalidProofSize
	}
	// Compression flag must be set.
	if proof[0]&0x80 == 0 {
		return ErrKZGInvalidCommitmentFormat
	}
	return nil
}

// --- GoEthKZGBackend ---

// GoEthKZGBackend implements KZGCeremonyBackend using the production
// go-eth-kzg library (github.com/crate-crypto/go-eth-kzg) with the
// Ethereum KZG ceremony SRS for blob commitments and PeerDAS cell proofs.
//
// Initialization loads the official Ethereum trusted setup (~2-5 seconds),
// so the context is created lazily on first use via globalGoEthKZGContext.
type GoEthKZGBackend struct {
	ctx *goethkzg.Context
}

// globalGoEthKZGContext caches the lazily-initialized production context.
var (
	globalGoEthKZGOnce    sync.Once
	globalGoEthKZGContext *goethkzg.Context
	globalGoEthKZGErr     error
)

// NewGoEthKZGBackend creates a GoEthKZGBackend using the production
// Ethereum KZG ceremony trusted setup. This is the preferred way to
// instantiate the backend; it loads the SRS only once per process.
func NewGoEthKZGBackend() (*GoEthKZGBackend, error) {
	globalGoEthKZGOnce.Do(func() {
		globalGoEthKZGContext, globalGoEthKZGErr = goethkzg.NewContext4096Secure()
	})
	if globalGoEthKZGErr != nil {
		return nil, fmt.Errorf("kzg: init production context: %w", globalGoEthKZGErr)
	}
	return &GoEthKZGBackend{ctx: globalGoEthKZGContext}, nil
}

// VerifyKZGPointProof verifies an EIP-4844 point opening with the same
// Ethereum ceremony context used for blob and PeerDAS operations.
func VerifyKZGPointProof(commitment [KZGBytesPerCommitment]byte,
	point, claim [KZGBytesPerFieldElement]byte, proof [KZGBytesPerProof]byte,
) error {
	backend, err := NewGoEthKZGBackend()
	if err != nil {
		return err
	}
	return backend.ctx.VerifyKZGProof(commitment, point, claim, proof)
}

func (b *GoEthKZGBackend) Name() string { return "go-eth-kzg" }

// BlobToCommitment computes the KZG commitment for a blob using the
// production Ethereum trusted setup.
func (b *GoEthKZGBackend) BlobToCommitment(blob []byte) ([KZGBytesPerCommitment]byte, error) {
	if b.ctx == nil {
		return [KZGBytesPerCommitment]byte{}, ErrKZGBackendNotImplemented
	}
	if len(blob) != KZGBytesPerBlob {
		return [KZGBytesPerCommitment]byte{}, ErrKZGInvalidBlobSize
	}
	var blobArr goethkzg.Blob
	copy(blobArr[:], blob)
	comm, err := b.ctx.BlobToKZGCommitment(&blobArr, 0)
	if err != nil {
		return [KZGBytesPerCommitment]byte{}, fmt.Errorf("kzg: blob to commitment: %w", err)
	}
	return [KZGBytesPerCommitment]byte(comm), nil
}

// VerifyBlobProof verifies a KZG proof for a blob against a commitment
// using the production trusted setup.
func (b *GoEthKZGBackend) VerifyBlobProof(blob, commitment, proof []byte) (bool, error) {
	if b.ctx == nil {
		return false, ErrKZGBackendNotImplemented
	}
	if len(blob) != KZGBytesPerBlob {
		return false, ErrKZGInvalidBlobSize
	}
	if err := ValidateCommitment(commitment); err != nil {
		return false, err
	}
	if err := ValidateProof(proof); err != nil {
		return false, err
	}
	var blobArr goethkzg.Blob
	copy(blobArr[:], blob)
	var comm goethkzg.KZGCommitment
	copy(comm[:], commitment)
	var p goethkzg.KZGProof
	copy(p[:], proof)
	err := b.ctx.VerifyBlobKZGProof(&blobArr, comm, p)
	return err == nil, err
}

// ComputeCells computes the 128 extended-blob cells for PeerDAS (EIP-7594)
// using Reed-Solomon erasure coding via the production trusted setup.
func (b *GoEthKZGBackend) ComputeCells(blob []byte) ([][KZGBytesPerCell]byte, error) {
	if b.ctx == nil {
		return nil, ErrKZGBackendNotImplemented
	}
	if len(blob) != KZGBytesPerBlob {
		return nil, ErrKZGInvalidBlobSize
	}
	var blobArr goethkzg.Blob
	copy(blobArr[:], blob)
	cellPtrs, err := b.ctx.ComputeCells(&blobArr, 0)
	if err != nil {
		return nil, fmt.Errorf("kzg: compute cells: %w", err)
	}
	cells := make([][KZGBytesPerCell]byte, len(cellPtrs))
	for i, c := range cellPtrs {
		if c != nil {
			cells[i] = [KZGBytesPerCell]byte(*c)
		}
	}
	return cells, nil
}

// VerifyCellProof verifies a single KZG cell proof against a commitment
// using VerifyCellKZGProofBatch (EIP-7594 PeerDAS path, EL-4.2).
func (b *GoEthKZGBackend) VerifyCellProof(commitment, cell, proof []byte, cellIndex uint64) (bool, error) {
	if b.ctx == nil {
		return false, ErrKZGBackendNotImplemented
	}
	if cellIndex >= KZGCellsPerExtBlob {
		return false, ErrKZGInvalidCellIndex
	}
	if err := ValidateCommitment(commitment); err != nil {
		return false, err
	}
	if len(cell) != KZGBytesPerCell {
		return false, errors.New("kzg: invalid cell size")
	}
	if err := ValidateProof(proof); err != nil {
		return false, err
	}
	var comm goethkzg.KZGCommitment
	copy(comm[:], commitment)
	var c goethkzg.Cell
	copy(c[:], cell)
	var p goethkzg.KZGProof
	copy(p[:], proof)
	err := b.ctx.VerifyCellKZGProofBatch(
		[]goethkzg.KZGCommitment{comm},
		[]uint64{cellIndex},
		[]*goethkzg.Cell{&c},
		[]goethkzg.KZGProof{p},
	)
	return err == nil, err
}

// ComputeCellsAndProofs computes both the extended cells and per-cell proofs
// for PeerDAS using the production trusted setup.
func (b *GoEthKZGBackend) ComputeCellsAndProofs(blob []byte) ([][KZGBytesPerCell]byte, [][KZGBytesPerProof]byte, error) {
	if b.ctx == nil {
		return nil, nil, ErrKZGBackendNotImplemented
	}
	if len(blob) != KZGBytesPerBlob {
		return nil, nil, ErrKZGInvalidBlobSize
	}
	var blobArr goethkzg.Blob
	copy(blobArr[:], blob)
	cellPtrs, kzgProofs, err := b.ctx.ComputeCellsAndKZGProofs(&blobArr, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("kzg: compute cells and proofs: %w", err)
	}
	cells := make([][KZGBytesPerCell]byte, len(cellPtrs))
	for i, c := range cellPtrs {
		if c != nil {
			cells[i] = [KZGBytesPerCell]byte(*c)
		}
	}
	proofs := make([][KZGBytesPerProof]byte, len(kzgProofs))
	for i, p := range kzgProofs {
		proofs[i] = [KZGBytesPerProof]byte(p)
	}
	return cells, proofs, nil
}

// --- Helpers ---

// kzgBlobWithFieldElement creates a test blob with a single non-zero field
// element at the given index. All other elements are zero.
func kzgBlobWithFieldElement(index int, value uint64) []byte {
	blob := make([]byte, KZGBytesPerBlob)
	if index >= 0 && index < KZGFieldElementsPerBlob {
		offset := index*KZGBytesPerFieldElement + KZGBytesPerFieldElement - 8
		binary.BigEndian.PutUint64(blob[offset:], value)
	}
	return blob
}

// ComputeBlobProof computes a KZG proof for a blob given its commitment.
// This is the EIP-4844 blob proof computation used for on-chain verification.
func (b *GoEthKZGBackend) ComputeBlobProof(blob []byte, commitment [KZGBytesPerCommitment]byte) ([KZGBytesPerProof]byte, error) {
	var out [KZGBytesPerProof]byte
	if b.ctx == nil {
		return out, ErrKZGBackendNotImplemented
	}
	if len(blob) != KZGBytesPerBlob {
		return out, ErrKZGInvalidBlobSize
	}

	var blobArr goethkzg.Blob
	copy(blobArr[:], blob)

	comm := goethkzg.KZGCommitment(commitment)

	proof, err := b.ctx.ComputeBlobKZGProof(&blobArr, comm, 0)
	if err != nil {
		return out, fmt.Errorf("kzg: ComputeBlobKZGProof failed: %w", err)
	}

	out = [KZGBytesPerProof]byte(proof)
	return out, nil
}

// VerifyBlobProofBatch verifies a batch of blob proofs against their
// commitments. This is more efficient than verifying each proof individually.
func (b *GoEthKZGBackend) VerifyBlobProofBatch(blobs [][]byte, commitments, proofs [][KZGBytesPerCommitment]byte) (bool, error) {
	if b.ctx == nil {
		return false, ErrKZGBackendNotImplemented
	}
	n := len(blobs)
	if n != len(commitments) || n != len(proofs) {
		return false, errors.New("kzg: batch length mismatch")
	}

	blobPtrs := make([]*goethkzg.Blob, n)
	comms := make([]goethkzg.KZGCommitment, n)
	kzgProofs := make([]goethkzg.KZGProof, n)

	for i := 0; i < n; i++ {
		if len(blobs[i]) != KZGBytesPerBlob {
			return false, fmt.Errorf("kzg: blob %d has invalid size %d", i, len(blobs[i]))
		}
		blobPtrs[i] = new(goethkzg.Blob)
		copy(blobPtrs[i][:], blobs[i])
		comms[i] = goethkzg.KZGCommitment(commitments[i])
		kzgProofs[i] = goethkzg.KZGProof(proofs[i])
	}

	err := b.ctx.VerifyBlobKZGProofBatch(blobPtrs, comms, kzgProofs)
	if err != nil {
		return false, err
	}
	return true, nil
}

// RecoverCells recovers the full set of extended blob cells from a subset
// of at least 64 cells (50% of CellsPerExtBlob=128). The cellIDs must be
// in ascending order and each must be < CellsPerExtBlob.
func (b *GoEthKZGBackend) RecoverCells(cellIDs []uint64, cells [][KZGBytesPerCell]byte) ([][KZGBytesPerCell]byte, error) {
	if b.ctx == nil {
		return nil, ErrKZGBackendNotImplemented
	}
	if len(cellIDs) != len(cells) {
		return nil, errors.New("kzg: cellIDs and cells length mismatch")
	}

	// Convert [][KZGBytesPerCell]byte to []*goethkzg.Cell.
	cellPtrs := make([]*goethkzg.Cell, len(cells))
	for i := range cells {
		c := goethkzg.Cell(cells[i])
		cellPtrs[i] = &c
	}

	recoveredPtrs, err := b.ctx.RecoverCells(cellIDs, cellPtrs, 0)
	if err != nil {
		return nil, fmt.Errorf("kzg: RecoverCells failed: %w", err)
	}

	result := make([][KZGBytesPerCell]byte, goethkzg.CellsPerExtBlob)
	for i, c := range recoveredPtrs {
		if c == nil {
			return nil, fmt.Errorf("kzg: RecoverCells returned nil cell at index %d", i)
		}
		result[i] = [KZGBytesPerCell]byte(*c)
	}
	return result, nil
}
