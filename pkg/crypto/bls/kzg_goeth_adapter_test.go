//go:build goethkzg

package bls

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// testBackend is a package-level backend initialized once to avoid the ~2-5s
// startup cost of NewContext4096Secure() per test function.
var testBackend *GoEthKZGBackend

func init() {
	var err error
	testBackend, err = NewGoEthKZGBackend()
	if err != nil {
		panic("failed to initialize GoEthKZGBackend for tests: " + err.Error())
	}
}

// makeTestBlob creates a valid test blob with small field element values.
// Each 32-byte element has its value in the last 8 bytes (big-endian uint64),
// ensuring all values are well below BLS_MODULUS.
func makeTestBlob(seed byte) []byte {
	blob := make([]byte, KZGBytesPerBlob)
	for i := 0; i < KZGFieldElementsPerBlob; i++ {
		offset := i*KZGBytesPerFieldElement + KZGBytesPerFieldElement - 8
		val := uint64(i+1) * uint64(seed+1)
		binary.BigEndian.PutUint64(blob[offset:], val)
	}
	return blob
}

// makeZeroBlob creates a blob with all zero field elements.
func makeZeroBlob() []byte {
	return make([]byte, KZGBytesPerBlob)
}

func TestGoEthKZGBackendName(t *testing.T) {
	name := testBackend.Name()
	if name != "go-eth-kzg" {
		t.Fatalf("expected name 'go-eth-kzg', got %q", name)
	}
}

func TestGoEthKZGBackendInit(t *testing.T) {
	// Verify the backend was initialized successfully.
	if testBackend == nil {
		t.Fatal("testBackend is nil")
	}
	if testBackend.ctx == nil {
		t.Fatal("testBackend.ctx is nil")
	}
}

func TestGoEthKZGBlobToCommitment(t *testing.T) {
	blob := makeZeroBlob()
	comm, err := testBackend.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("BlobToCommitment failed: %v", err)
	}

	// The commitment to a zero blob should be the point at infinity,
	// which in compressed form is 0xc0 followed by 47 zero bytes.
	expectedFirst := byte(0xc0)
	if comm[0] != expectedFirst {
		t.Errorf("expected first byte 0x%02x for zero blob commitment, got 0x%02x", expectedFirst, comm[0])
	}

	// Verify remaining bytes are zero (point at infinity).
	for i := 1; i < KZGBytesPerCommitment; i++ {
		if comm[i] != 0 {
			t.Errorf("expected byte %d to be 0 for zero blob commitment, got 0x%02x", i, comm[i])
			break
		}
	}
}

func TestGoEthKZGBlobToCommitmentDeterministic(t *testing.T) {
	blob := makeTestBlob(1)
	comm1, err := testBackend.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("first BlobToCommitment failed: %v", err)
	}

	comm2, err := testBackend.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("second BlobToCommitment failed: %v", err)
	}

	if comm1 != comm2 {
		t.Fatal("BlobToCommitment is not deterministic: same blob produced different commitments")
	}
}

func TestGoEthKZGBlobToCommitmentDifferentBlobs(t *testing.T) {
	blob1 := makeTestBlob(1)
	blob2 := makeTestBlob(2)

	comm1, err := testBackend.BlobToCommitment(blob1)
	if err != nil {
		t.Fatalf("BlobToCommitment for blob1 failed: %v", err)
	}

	comm2, err := testBackend.BlobToCommitment(blob2)
	if err != nil {
		t.Fatalf("BlobToCommitment for blob2 failed: %v", err)
	}

	if comm1 == comm2 {
		t.Fatal("different blobs produced identical commitments")
	}
}

func TestGoEthKZGComputeAndVerifyProof(t *testing.T) {
	blob := makeTestBlob(3)

	// Compute commitment.
	comm, err := testBackend.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("BlobToCommitment failed: %v", err)
	}

	// Compute proof.
	proof, err := testBackend.ComputeBlobProof(blob, comm)
	if err != nil {
		t.Fatalf("ComputeBlobProof failed: %v", err)
	}

	// Verify proof.
	valid, err := testBackend.VerifyBlobProof(blob, comm[:], proof[:])
	if err != nil {
		t.Fatalf("VerifyBlobProof failed: %v", err)
	}
	if !valid {
		t.Fatal("VerifyBlobProof returned false for a valid proof")
	}
}

func TestGoEthKZGVerifyProofWrongCommitment(t *testing.T) {
	blob := makeTestBlob(4)

	comm, err := testBackend.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("BlobToCommitment failed: %v", err)
	}

	proof, err := testBackend.ComputeBlobProof(blob, comm)
	if err != nil {
		t.Fatalf("ComputeBlobProof failed: %v", err)
	}

	// Use a different blob's commitment.
	blob2 := makeTestBlob(5)
	wrongComm, err := testBackend.BlobToCommitment(blob2)
	if err != nil {
		t.Fatalf("BlobToCommitment for wrong blob failed: %v", err)
	}

	valid, err := testBackend.VerifyBlobProof(blob, wrongComm[:], proof[:])
	if valid {
		t.Fatal("VerifyBlobProof should fail with wrong commitment")
	}
	if err == nil {
		t.Fatal("VerifyBlobProof should return error with wrong commitment")
	}
}

func TestGoEthKZGVerifyProofWrongBlob(t *testing.T) {
	blob := makeTestBlob(6)

	comm, err := testBackend.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("BlobToCommitment failed: %v", err)
	}

	proof, err := testBackend.ComputeBlobProof(blob, comm)
	if err != nil {
		t.Fatalf("ComputeBlobProof failed: %v", err)
	}

	// Use a different blob for verification.
	wrongBlob := makeTestBlob(7)
	valid, err := testBackend.VerifyBlobProof(wrongBlob, comm[:], proof[:])
	if valid {
		t.Fatal("VerifyBlobProof should fail with wrong blob")
	}
	if err == nil {
		t.Fatal("VerifyBlobProof should return error with wrong blob")
	}
}

func TestGoEthKZGComputeCells(t *testing.T) {
	blob := makeTestBlob(8)

	cells, err := testBackend.ComputeCells(blob)
	if err != nil {
		t.Fatalf("ComputeCells failed: %v", err)
	}

	if len(cells) != KZGCellsPerExtBlob {
		t.Fatalf("expected %d cells, got %d", KZGCellsPerExtBlob, len(cells))
	}

	// Each cell should be KZGBytesPerCell bytes.
	for i, cell := range cells {
		if len(cell) != KZGBytesPerCell {
			t.Fatalf("cell %d has size %d, expected %d", i, len(cell), KZGBytesPerCell)
		}
	}

	// At least some cells should be non-zero for a non-zero blob.
	allZero := true
	for _, cell := range cells {
		for _, b := range cell {
			if b != 0 {
				allZero = false
				break
			}
		}
		if !allZero {
			break
		}
	}
	if allZero {
		t.Fatal("all cells are zero for a non-zero blob")
	}
}

func TestGoEthKZGComputeCellsAndProofs(t *testing.T) {
	blob := makeTestBlob(9)

	cells, proofs, err := testBackend.ComputeCellsAndProofs(blob)
	if err != nil {
		t.Fatalf("ComputeCellsAndProofs failed: %v", err)
	}

	if len(cells) != KZGCellsPerExtBlob {
		t.Fatalf("expected %d cells, got %d", KZGCellsPerExtBlob, len(cells))
	}
	if len(proofs) != KZGCellsPerExtBlob {
		t.Fatalf("expected %d proofs, got %d", KZGCellsPerExtBlob, len(proofs))
	}

	// Each proof should be KZGBytesPerProof bytes.
	for i, proof := range proofs {
		if len(proof) != KZGBytesPerProof {
			t.Fatalf("proof %d has size %d, expected %d", i, len(proof), KZGBytesPerProof)
		}
	}
}

func TestGoEthKZGVerifyCellProof(t *testing.T) {
	blob := makeTestBlob(10)

	// Compute commitment.
	comm, err := testBackend.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("BlobToCommitment failed: %v", err)
	}

	// Compute cells and proofs.
	cells, proofs, err := testBackend.ComputeCellsAndProofs(blob)
	if err != nil {
		t.Fatalf("ComputeCellsAndProofs failed: %v", err)
	}

	// Verify cell 0.
	valid, err := testBackend.VerifyCellProof(comm[:], cells[0][:], proofs[0][:], 0)
	if err != nil {
		t.Fatalf("VerifyCellProof failed: %v", err)
	}
	if !valid {
		t.Fatal("VerifyCellProof returned false for valid cell proof")
	}

	// Verify a cell in the middle of the extended blob (cell 64, in the parity region).
	valid, err = testBackend.VerifyCellProof(comm[:], cells[64][:], proofs[64][:], 64)
	if err != nil {
		t.Fatalf("VerifyCellProof failed for cell 64: %v", err)
	}
	if !valid {
		t.Fatal("VerifyCellProof returned false for valid cell 64 proof")
	}
}

func TestGoEthKZGVerifyCellProofWrongCell(t *testing.T) {
	blob := makeTestBlob(11)

	comm, err := testBackend.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("BlobToCommitment failed: %v", err)
	}

	cells, proofs, err := testBackend.ComputeCellsAndProofs(blob)
	if err != nil {
		t.Fatalf("ComputeCellsAndProofs failed: %v", err)
	}

	// Tamper with cell data.
	tamperedCell := cells[0]
	tamperedCell[100] ^= 0xFF // Flip some bits.

	// Use the tampered cell with the original proof for cell 0.
	valid, err := testBackend.VerifyCellProof(comm[:], tamperedCell[:], proofs[0][:], 0)
	if valid {
		t.Fatal("VerifyCellProof should fail with tampered cell data")
	}
	if err == nil {
		t.Fatal("VerifyCellProof should return error with tampered cell data")
	}
}

func TestGoEthKZGRecoverCells(t *testing.T) {
	blob := makeTestBlob(12)

	// Compute all cells.
	allCells, err := testBackend.ComputeCells(blob)
	if err != nil {
		t.Fatalf("ComputeCells failed: %v", err)
	}

	// Use only the first 64 cells (50% of 128) for recovery.
	// Cell IDs must be in ascending order.
	numRecovery := KZGCellsPerExtBlob / KZGExpansionFactor // 64
	cellIDs := make([]uint64, numRecovery)
	partialCells := make([][KZGBytesPerCell]byte, numRecovery)
	for i := 0; i < numRecovery; i++ {
		cellIDs[i] = uint64(i)
		partialCells[i] = allCells[i]
	}

	recovered, err := testBackend.RecoverCells(cellIDs, partialCells)
	if err != nil {
		t.Fatalf("RecoverCells failed: %v", err)
	}

	if len(recovered) != KZGCellsPerExtBlob {
		t.Fatalf("expected %d recovered cells, got %d", KZGCellsPerExtBlob, len(recovered))
	}

	// Verify recovered cells match the originals.
	for i := 0; i < KZGCellsPerExtBlob; i++ {
		if !bytes.Equal(recovered[i][:], allCells[i][:]) {
			t.Fatalf("recovered cell %d does not match original", i)
		}
	}
}

func TestGoEthKZGRecoverCellsInsufficient(t *testing.T) {
	blob := makeTestBlob(13)

	allCells, err := testBackend.ComputeCells(blob)
	if err != nil {
		t.Fatalf("ComputeCells failed: %v", err)
	}

	// Provide only 63 cells (less than the required 64).
	numCells := KZGCellsPerExtBlob/KZGExpansionFactor - 1 // 63
	cellIDs := make([]uint64, numCells)
	partialCells := make([][KZGBytesPerCell]byte, numCells)
	for i := 0; i < numCells; i++ {
		cellIDs[i] = uint64(i)
		partialCells[i] = allCells[i]
	}

	_, err = testBackend.RecoverCells(cellIDs, partialCells)
	if err == nil {
		t.Fatal("RecoverCells should fail with insufficient cells")
	}
}

func TestGoEthKZGBatchVerify(t *testing.T) {
	const batchSize = 3
	blobs := make([][]byte, batchSize)
	commitments := make([][KZGBytesPerCommitment]byte, batchSize)
	proofs := make([][KZGBytesPerProof]byte, batchSize)

	for i := 0; i < batchSize; i++ {
		blobs[i] = makeTestBlob(byte(20 + i))

		var err error
		commitments[i], err = testBackend.BlobToCommitment(blobs[i])
		if err != nil {
			t.Fatalf("BlobToCommitment for blob %d failed: %v", i, err)
		}

		proofs[i], err = testBackend.ComputeBlobProof(blobs[i], commitments[i])
		if err != nil {
			t.Fatalf("ComputeBlobProof for blob %d failed: %v", i, err)
		}
	}

	valid, err := testBackend.VerifyBlobProofBatch(blobs, commitments, proofs)
	if err != nil {
		t.Fatalf("VerifyBlobProofBatch failed: %v", err)
	}
	if !valid {
		t.Fatal("VerifyBlobProofBatch returned false for valid batch")
	}
}

func TestGoEthKZGInterface(t *testing.T) {
	// Verify that GoEthKZGBackend satisfies KZGCeremonyBackend.
	var _ KZGCeremonyBackend = testBackend
}

func TestGoEthKZGBlobToCommitmentInvalidSize(t *testing.T) {
	_, err := testBackend.BlobToCommitment([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for invalid blob size")
	}
}

func TestGoEthKZGComputeCellsInvalidSize(t *testing.T) {
	_, err := testBackend.ComputeCells([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for invalid blob size")
	}
}

func TestGoEthKZGVerifyBlobProofInvalidSizes(t *testing.T) {
	// Invalid blob size.
	_, err := testBackend.VerifyBlobProof([]byte{1}, make([]byte, 48), make([]byte, 48))
	if err == nil {
		t.Fatal("expected error for invalid blob size")
	}

	// Invalid commitment size.
	_, err = testBackend.VerifyBlobProof(make([]byte, KZGBytesPerBlob), []byte{1}, make([]byte, 48))
	if err == nil {
		t.Fatal("expected error for invalid commitment size")
	}

	// Invalid proof size.
	_, err = testBackend.VerifyBlobProof(make([]byte, KZGBytesPerBlob), make([]byte, 48), []byte{1})
	if err == nil {
		t.Fatal("expected error for invalid proof size")
	}
}

func TestGoEthKZGVerifyCellProofInvalidIndex(t *testing.T) {
	_, err := testBackend.VerifyCellProof(
		make([]byte, KZGBytesPerCommitment),
		make([]byte, KZGBytesPerCell),
		make([]byte, KZGBytesPerProof),
		KZGCellsPerExtBlob, // Out of range.
	)
	if err == nil {
		t.Fatal("expected error for invalid cell index")
	}
}

func TestGoEthKZGRecoverCellsEvenSpaced(t *testing.T) {
	blob := makeTestBlob(14)

	allCells, err := testBackend.ComputeCells(blob)
	if err != nil {
		t.Fatalf("ComputeCells failed: %v", err)
	}

	// Use every other cell (even indices: 0, 2, 4, ..., 126) = 64 cells.
	numRecovery := KZGCellsPerExtBlob / KZGExpansionFactor
	cellIDs := make([]uint64, numRecovery)
	partialCells := make([][KZGBytesPerCell]byte, numRecovery)
	for i := 0; i < numRecovery; i++ {
		cellIDs[i] = uint64(i * 2)
		partialCells[i] = allCells[i*2]
	}

	recovered, err := testBackend.RecoverCells(cellIDs, partialCells)
	if err != nil {
		t.Fatalf("RecoverCells (even-spaced) failed: %v", err)
	}

	for i := 0; i < KZGCellsPerExtBlob; i++ {
		if !bytes.Equal(recovered[i][:], allCells[i][:]) {
			t.Fatalf("recovered cell %d does not match original (even-spaced recovery)", i)
		}
	}
}

// BenchmarkGoEthKZGBlobToCommitment measures the time to compute a KZG
// commitment for a single blob.
func BenchmarkGoEthKZGBlobToCommitment(b *testing.B) {
	blob := makeTestBlob(30)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := testBackend.BlobToCommitment(blob)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGoEthKZGComputeCells measures the time to compute all 128 cells
// for a single blob.
func BenchmarkGoEthKZGComputeCells(b *testing.B) {
	blob := makeTestBlob(31)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := testBackend.ComputeCells(blob)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGoEthKZGComputeCellsAndProofs measures the time to compute
// all 128 cells and their proofs for a single blob.
func BenchmarkGoEthKZGComputeCellsAndProofs(b *testing.B) {
	blob := makeTestBlob(32)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, err := testBackend.ComputeCellsAndProofs(blob)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGoEthKZGVerifyBlobProof measures the time to verify a blob proof.
func BenchmarkGoEthKZGVerifyBlobProof(b *testing.B) {
	blob := makeTestBlob(33)
	comm, err := testBackend.BlobToCommitment(blob)
	if err != nil {
		b.Fatal(err)
	}
	proof, err := testBackend.ComputeBlobProof(blob, comm)
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := testBackend.VerifyBlobProof(blob, comm[:], proof[:])
		if err != nil {
			b.Fatal(err)
		}
	}
}
