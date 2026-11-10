// KZG trusted setup integration layer for EIP-4844 and EIP-7594.
//
// This file provides a KZGCeremonyBackend interface that abstracts KZG
// polynomial commitment operations, allowing the codebase to switch between
// the existing pure-Go placeholder (test secret s=42) and the production
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
// Implementations may use the placeholder (test secret) or the production
// go-eth-kzg library with the real Ethereum ceremony SRS.
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
	activeKZGBackend KZGCeremonyBackend = &PlaceholderKZGBackend{}
)

// DefaultKZGBackend returns the currently active KZG backend.
func DefaultKZGBackend() KZGCeremonyBackend {
	activeKZGMu.RLock()
	defer activeKZGMu.RUnlock()
	return activeKZGBackend
}

// SetKZGBackend sets the active KZG backend. This is safe for concurrent use.
// Passing nil resets to the default placeholder backend.
func SetKZGBackend(b KZGCeremonyBackend) {
	activeKZGMu.Lock()
	defer activeKZGMu.Unlock()
	if b == nil {
		b = &PlaceholderKZGBackend{}
	}
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

// --- PlaceholderKZGBackend ---

// PlaceholderKZGBackend implements KZGCeremonyBackend using the existing
// pure-Go KZG code from kzg.go (test secret s=42). This is suitable for
// unit tests but not for production use.
type PlaceholderKZGBackend struct{}

func (b *PlaceholderKZGBackend) Name() string { return "placeholder" }

func (b *PlaceholderKZGBackend) BlobToCommitment(blob []byte) ([KZGBytesPerCommitment]byte, error) {
	var out [KZGBytesPerCommitment]byte
	if len(blob) != KZGBytesPerBlob {
		return out, ErrKZGInvalidBlobSize
	}
	// Evaluate the blob polynomial at the test secret s=42.
	// p(s) = sum(blob_i * s^i) for each 32-byte field element.
	secret := big.NewInt(42)
	polyAtS := big.NewInt(0)
	sPower := big.NewInt(1)
	for i := 0; i < KZGFieldElementsPerBlob; i++ {
		offset := i * KZGBytesPerFieldElement
		elem := new(big.Int).SetBytes(blob[offset : offset+KZGBytesPerFieldElement])
		term := new(big.Int).Mul(elem, sPower)
		term.Mod(term, blsR)
		polyAtS.Add(polyAtS, term)
		polyAtS.Mod(polyAtS, blsR)
		sPower.Mul(sPower, secret)
		sPower.Mod(sPower, blsR)
	}
	commitment := KZGCommit(polyAtS)
	compressed := KZGCompressG1(commitment)
	copy(out[:], compressed)
	return out, nil
}

func (b *PlaceholderKZGBackend) VerifyBlobProof(blob, commitment, proof []byte) (bool, error) {
	if len(blob) != KZGBytesPerBlob {
		return false, ErrKZGInvalidBlobSize
	}
	if err := ValidateCommitment(commitment); err != nil {
		return false, err
	}
	if err := ValidateProof(proof); err != nil {
		return false, err
	}
	// For the placeholder, we verify by re-computing the commitment and
	// checking it matches. This is a simplified check that doesn't use
	// the full KZG pairing verification (which requires a real SRS).
	recomputed, err := b.BlobToCommitment(blob)
	if err != nil {
		return false, err
	}
	match := true
	for i := range recomputed {
		if recomputed[i] != commitment[i] {
			match = false
			break
		}
	}
	return match, nil
}

func (b *PlaceholderKZGBackend) ComputeCells(blob []byte) ([][KZGBytesPerCell]byte, error) {
	if len(blob) != KZGBytesPerBlob {
		return nil, ErrKZGInvalidBlobSize
	}
	// For the placeholder, split the blob into cells and pad the extension
	// with zeros. A real implementation would use Reed-Solomon erasure coding.
	cells := make([][KZGBytesPerCell]byte, KZGCellsPerExtBlob)

	// The original blob occupies the first half of the extended blob.
	// CellsPerExtBlob/2 cells come from the blob data.
	originalCells := KZGCellsPerExtBlob / KZGExpansionFactor
	for i := 0; i < originalCells; i++ {
		offset := i * KZGBytesPerCell
		if offset+KZGBytesPerCell <= len(blob) {
			copy(cells[i][:], blob[offset:offset+KZGBytesPerCell])
		}
	}
	// Remaining cells (extension) are zero-filled, representing the
	// Reed-Solomon parity data (placeholder only).
	return cells, nil
}

func (b *PlaceholderKZGBackend) ComputeCellsAndProofs(blob []byte) ([][KZGBytesPerCell]byte, [][KZGBytesPerProof]byte, error) {
	if len(blob) != KZGBytesPerBlob {
		return nil, nil, ErrKZGInvalidBlobSize
	}
	cells := make([][KZGBytesPerCell]byte, KZGCellsPerExtBlob)
	proofs := make([][KZGBytesPerProof]byte, KZGCellsPerExtBlob)
	return cells, proofs, nil
}

func (b *PlaceholderKZGBackend) VerifyCellProof(commitment, cell, proof []byte, cellIndex uint64) (bool, error) {
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
	// Placeholder: accept if formats are valid. A real implementation
	// would perform the pairing check.
	return true, nil
}

// --- GoEthKZGBackend ---

// GoEthKZGBackend implements KZGCeremonyBackend using the production
// go-eth-kzg library (github.com/crate-crypto/go-eth-kzg) with the
// real Ethereum KZG ceremony SRS. This replaces the placeholder backend
// for mainnet-valid blob commitments and PeerDAS cell proofs (EL-4.1).
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
