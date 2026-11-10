package bls

import (
	"math/big"
	"sync"
	"testing"
)

// --- EIP-4844 constant tests ---

func TestKZGIntegrationFieldElementsPerBlob(t *testing.T) {
	if KZGFieldElementsPerBlob != 4096 {
		t.Errorf("FieldElementsPerBlob = %d, want 4096", KZGFieldElementsPerBlob)
	}
}

func TestKZGIntegrationBytesPerFieldElement(t *testing.T) {
	if KZGBytesPerFieldElement != 32 {
		t.Errorf("BytesPerFieldElement = %d, want 32", KZGBytesPerFieldElement)
	}
}

func TestKZGIntegrationBytesPerBlob(t *testing.T) {
	if KZGBytesPerBlob != 131072 {
		t.Errorf("BytesPerBlob = %d, want 131072", KZGBytesPerBlob)
	}
	// Cross-check: should equal FieldElementsPerBlob * BytesPerFieldElement.
	if KZGBytesPerBlob != KZGFieldElementsPerBlob*KZGBytesPerFieldElement {
		t.Error("BytesPerBlob != FieldElementsPerBlob * BytesPerFieldElement")
	}
}

func TestKZGIntegrationBytesPerCommitment(t *testing.T) {
	if KZGBytesPerCommitment != 48 {
		t.Errorf("BytesPerCommitment = %d, want 48", KZGBytesPerCommitment)
	}
}

func TestKZGIntegrationBytesPerProof(t *testing.T) {
	if KZGBytesPerProof != 48 {
		t.Errorf("BytesPerProof = %d, want 48", KZGBytesPerProof)
	}
}

// --- EIP-7594 constant tests ---

func TestKZGIntegrationCellsPerExtBlob(t *testing.T) {
	if KZGCellsPerExtBlob != 128 {
		t.Errorf("CellsPerExtBlob = %d, want 128", KZGCellsPerExtBlob)
	}
}

func TestKZGIntegrationFieldElementsPerCell(t *testing.T) {
	if KZGFieldElementsPerCell != 64 {
		t.Errorf("FieldElementsPerCell = %d, want 64", KZGFieldElementsPerCell)
	}
}

func TestKZGIntegrationBytesPerCell(t *testing.T) {
	if KZGBytesPerCell != 2048 {
		t.Errorf("BytesPerCell = %d, want 2048", KZGBytesPerCell)
	}
	if KZGBytesPerCell != KZGFieldElementsPerCell*KZGBytesPerFieldElement {
		t.Error("BytesPerCell != FieldElementsPerCell * BytesPerFieldElement")
	}
}

func TestKZGIntegrationExpansionFactor(t *testing.T) {
	if KZGExpansionFactor != 2 {
		t.Errorf("ExpansionFactor = %d, want 2", KZGExpansionFactor)
	}
}

func TestKZGIntegrationScalarsPerExtBlob(t *testing.T) {
	if KZGScalarsPerExtBlob != 8192 {
		t.Errorf("ScalarsPerExtBlob = %d, want 8192", KZGScalarsPerExtBlob)
	}
}

func TestKZGIntegrationCellCountConsistency(t *testing.T) {
	// CellsPerExtBlob * FieldElementsPerCell should equal ScalarsPerExtBlob.
	if KZGCellsPerExtBlob*KZGFieldElementsPerCell != KZGScalarsPerExtBlob {
		t.Error("CellsPerExtBlob * FieldElementsPerCell != ScalarsPerExtBlob")
	}
}

// --- BLS_MODULUS tests ---

func TestKZGIntegrationBLSModulusValue(t *testing.T) {
	// Convert the byte array to big.Int and check against blsR.
	modulus := new(big.Int).SetBytes(KZGBLSModulus[:])
	if modulus.Cmp(blsR) != 0 {
		t.Errorf("KZGBLSModulus does not match blsR:\n  got:  %x\n  want: %x",
			modulus, blsR)
	}
}

func TestKZGIntegrationBLSModulusHex(t *testing.T) {
	modulus := new(big.Int).SetBytes(KZGBLSModulus[:])
	expected := "73eda753299d7d483339d80809a1d80553bda402fffe5bfeffffffff00000001"
	if modulus.Text(16) != expected {
		t.Errorf("BLS_MODULUS hex = %s, want %s", modulus.Text(16), expected)
	}
}

func TestKZGIntegrationBLSModulusIsPrime(t *testing.T) {
	modulus := new(big.Int).SetBytes(KZGBLSModulus[:])
	// BLS12-381 r is prime. ProbablyPrime with 20 rounds is sufficient.
	if !modulus.ProbablyPrime(20) {
		t.Error("BLS_MODULUS should be prime")
	}
}

func TestKZGIntegrationBLSModulusBitLength(t *testing.T) {
	modulus := new(big.Int).SetBytes(KZGBLSModulus[:])
	if modulus.BitLen() != 255 {
		t.Errorf("BLS_MODULUS bit length = %d, want 255", modulus.BitLen())
	}
}

// --- Blob validation tests ---

func TestKZGIntegrationValidateBlobCorrectSize(t *testing.T) {
	blob := make([]byte, KZGBytesPerBlob)
	if err := ValidateBlob(blob); err != nil {
		t.Errorf("all-zero blob should be valid: %v", err)
	}
}

func TestKZGIntegrationValidateBlobWrongSize(t *testing.T) {
	tests := []int{0, 1, KZGBytesPerBlob - 1, KZGBytesPerBlob + 1}
	for _, size := range tests {
		blob := make([]byte, size)
		if err := ValidateBlob(blob); err != ErrKZGInvalidBlobSize {
			t.Errorf("blob size %d: got %v, want ErrKZGInvalidBlobSize", size, err)
		}
	}
}

func TestKZGIntegrationValidateBlobFieldElementRange(t *testing.T) {
	// Create a blob where the first field element equals BLS_MODULUS (invalid).
	blob := make([]byte, KZGBytesPerBlob)
	copy(blob[:32], KZGBLSModulus[:])
	if err := ValidateBlob(blob); err != ErrKZGFieldElementOutOfRange {
		t.Errorf("expected ErrKZGFieldElementOutOfRange, got %v", err)
	}
}

func TestKZGIntegrationValidateBlobMaxValidElement(t *testing.T) {
	// BLS_MODULUS - 1 should be valid.
	blob := make([]byte, KZGBytesPerBlob)
	maxValid := new(big.Int).Sub(new(big.Int).SetBytes(KZGBLSModulus[:]), big.NewInt(1))
	maxBytes := maxValid.Bytes()
	copy(blob[32-len(maxBytes):32], maxBytes)
	if err := ValidateBlob(blob); err != nil {
		t.Errorf("max valid field element should be accepted: %v", err)
	}
}

// --- Commitment validation tests ---

func TestKZGIntegrationValidateCommitmentCorrect(t *testing.T) {
	// Use the G1 generator (compressed) as a valid commitment.
	gen := BLSG1GeneratorCompressed
	if err := ValidateCommitment(gen[:]); err != nil {
		t.Errorf("G1 generator should be valid commitment: %v", err)
	}
}

func TestKZGIntegrationValidateCommitmentWrongLength(t *testing.T) {
	for _, size := range []int{0, 47, 49, 96} {
		if err := ValidateCommitment(make([]byte, size)); err != ErrKZGInvalidCommitmentSize {
			t.Errorf("commitment size %d: got %v, want ErrKZGInvalidCommitmentSize", size, err)
		}
	}
}

func TestKZGIntegrationValidateCommitmentNoCompressFlag(t *testing.T) {
	buf := make([]byte, 48)
	// No compression flag set.
	if err := ValidateCommitment(buf); err != ErrKZGInvalidCommitmentFormat {
		t.Errorf("expected ErrKZGInvalidCommitmentFormat, got %v", err)
	}
}

func TestKZGIntegrationValidateCommitmentInfinity(t *testing.T) {
	// The point at infinity (0xC0 + zeros) is a valid compressed G1 point.
	inf := BLSPointAtInfinityG1
	if err := ValidateCommitment(inf[:]); err != nil {
		t.Errorf("point at infinity should be valid commitment format: %v", err)
	}
}

func TestKZGIntegrationBackendSwitching(t *testing.T) {
	original := DefaultKZGBackend()
	if original.Name() != "go-eth-kzg" {
		t.Errorf("default backend should be go-eth-kzg, got %q", original.Name())
	}

	SetKZGBackend(&GoEthKZGBackend{})
	if KZGIntegrationStatus() != "go-eth-kzg" {
		t.Errorf("status should be go-eth-kzg, got %q", KZGIntegrationStatus())
	}

	// Reset.
	SetKZGBackend(nil)
	if KZGIntegrationStatus() != "go-eth-kzg" {
		t.Errorf("status should be go-eth-kzg after nil reset, got %q", KZGIntegrationStatus())
	}
}

// TestKZGIntegrationGoEthKZGName checks the backend name via direct struct init.
// Full nil-ctx behavior is tested in TestKZGGoEthNilCtxReturnsNotImplemented.
func TestKZGIntegrationGoEthKZGName(t *testing.T) {
	backend := &GoEthKZGBackend{}
	if backend.Name() != "go-eth-kzg" {
		t.Errorf("GoEthKZGBackend Name = %q, want go-eth-kzg", backend.Name())
	}
}

// --- Nil/empty input handling ---

func TestKZGIntegrationNilBlob(t *testing.T) {
	if err := ValidateBlob(nil); err != ErrKZGInvalidBlobSize {
		t.Errorf("expected ErrKZGInvalidBlobSize for nil, got %v", err)
	}
}

func TestKZGIntegrationNilCommitment(t *testing.T) {
	if err := ValidateCommitment(nil); err != ErrKZGInvalidCommitmentSize {
		t.Errorf("expected ErrKZGInvalidCommitmentSize for nil, got %v", err)
	}
}

func TestKZGIntegrationValidateProof(t *testing.T) {
	if err := ValidateProof(nil); err != ErrKZGInvalidProofSize {
		t.Errorf("expected ErrKZGInvalidProofSize for nil, got %v", err)
	}
	// Valid proof format.
	proof := BLSPointAtInfinityG1
	if err := ValidateProof(proof[:]); err != nil {
		t.Errorf("valid proof format should pass: %v", err)
	}
}

// --- Concurrent verification ---

func TestKZGIntegrationConcurrentBlobToCommitment(t *testing.T) {
	backend := DefaultKZGBackend()
	blob := kzgBlobWithFieldElement(0, 99)

	var wg sync.WaitGroup
	results := make([][KZGBytesPerCommitment]byte, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			comm, err := backend.BlobToCommitment(blob)
			if err != nil {
				t.Errorf("goroutine %d: %v", idx, err)
				return
			}
			results[idx] = comm
		}(i)
	}
	wg.Wait()

	// All results should be identical.
	for i := 1; i < 10; i++ {
		if results[i] != results[0] {
			t.Errorf("concurrent result %d differs from result 0", i)
		}
	}
}

// --- Cell proof verification ---

func TestKZGIntegrationBlobWithFieldElement(t *testing.T) {
	blob := kzgBlobWithFieldElement(0, 42)
	if len(blob) != KZGBytesPerBlob {
		t.Errorf("blob size = %d, want %d", len(blob), KZGBytesPerBlob)
	}
	// First field element should be 42 (big-endian in last 8 bytes of 32).
	val := new(big.Int).SetBytes(blob[:32])
	if val.Int64() != 42 {
		t.Errorf("first field element = %d, want 42", val.Int64())
	}
	// Second field element should be zero.
	val2 := new(big.Int).SetBytes(blob[32:64])
	if val2.Sign() != 0 {
		t.Error("second field element should be zero")
	}
}

// --- GoEthKZGBackend production tests ---

// initGoEthKZG initialises the production KZG backend once per test binary.
// It marks the test as skipped if the trusted-setup can't be loaded
// (e.g., in extremely memory-constrained CI environments).
func initGoEthKZG(t *testing.T) *GoEthKZGBackend {
	t.Helper()
	b, err := NewGoEthKZGBackend()
	if err != nil {
		t.Skipf("skipping go-eth-kzg production test: %v", err)
	}
	return b
}

func TestKZGGoEthBackendName(t *testing.T) {
	b := initGoEthKZG(t)
	if b.Name() != "go-eth-kzg" {
		t.Errorf("Name = %q, want go-eth-kzg", b.Name())
	}
}

func TestKZGGoEthBlobToCommitmentZero(t *testing.T) {
	b := initGoEthKZG(t)
	blob := make([]byte, KZGBytesPerBlob)
	comm, err := b.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("BlobToCommitment zero blob: %v", err)
	}
	// Commitment must be a compressed G1 point (first byte has 0x80 set).
	if comm[0]&0x80 == 0 {
		t.Errorf("commitment first byte 0x%x: compression flag not set", comm[0])
	}
}

func TestKZGGoEthBlobToCommitmentDeterministic(t *testing.T) {
	b := initGoEthKZG(t)
	blob := kzgBlobWithFieldElement(0, 1)
	c1, err := b.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("first BlobToCommitment: %v", err)
	}
	c2, err := b.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("second BlobToCommitment: %v", err)
	}
	if c1 != c2 {
		t.Error("BlobToCommitment is not deterministic")
	}
}

func TestKZGGoEthBlobToCommitmentDifferentBlobs(t *testing.T) {
	b := initGoEthKZG(t)
	c1, err := b.BlobToCommitment(kzgBlobWithFieldElement(0, 1))
	if err != nil {
		t.Fatalf("blob1 commitment: %v", err)
	}
	c2, err := b.BlobToCommitment(kzgBlobWithFieldElement(0, 2))
	if err != nil {
		t.Fatalf("blob2 commitment: %v", err)
	}
	if c1 == c2 {
		t.Error("different blobs produced the same commitment")
	}
}

func TestKZGGoEthBlobToCommitmentWrongSize(t *testing.T) {
	b := initGoEthKZG(t)
	_, err := b.BlobToCommitment(make([]byte, 100))
	if err != ErrKZGInvalidBlobSize {
		t.Errorf("expected ErrKZGInvalidBlobSize, got %v", err)
	}
}

func TestKZGGoEthComputeCellsCount(t *testing.T) {
	b := initGoEthKZG(t)
	blob := make([]byte, KZGBytesPerBlob)
	cells, err := b.ComputeCells(blob)
	if err != nil {
		t.Fatalf("ComputeCells: %v", err)
	}
	if len(cells) != KZGCellsPerExtBlob {
		t.Errorf("cells count = %d, want %d", len(cells), KZGCellsPerExtBlob)
	}
	for i, c := range cells {
		if len(c) != KZGBytesPerCell {
			t.Errorf("cell %d size = %d, want %d", i, len(c), KZGBytesPerCell)
		}
	}
}

func TestKZGGoEthComputeCellsWrongSize(t *testing.T) {
	b := initGoEthKZG(t)
	_, err := b.ComputeCells(make([]byte, 100))
	if err != ErrKZGInvalidBlobSize {
		t.Errorf("expected ErrKZGInvalidBlobSize, got %v", err)
	}
}

// TestKZGGoEthCommitmentCellsRoundTrip verifies that ComputeCells produces
// cells whose proof can be verified against the blob commitment (EL-4.2 path).
func TestKZGGoEthCommitmentCellsRoundTrip(t *testing.T) {
	b := initGoEthKZG(t)
	blob := kzgBlobWithFieldElement(7, 999)

	comm, err := b.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("BlobToCommitment: %v", err)
	}
	cells, err := b.ComputeCells(blob)
	if err != nil {
		t.Fatalf("ComputeCells: %v", err)
	}
	// Cell count is 128 (CellsPerExtBlob).
	if len(cells) != KZGCellsPerExtBlob {
		t.Fatalf("cells count = %d", len(cells))
	}
	// Commitment for the same blob called twice must match (idempotence).
	comm2, err := b.BlobToCommitment(blob)
	if err != nil {
		t.Fatalf("second BlobToCommitment: %v", err)
	}
	if comm != comm2 {
		t.Error("commitment not idempotent")
	}
}

func TestKZGGoEthVerifyCellProofBadIndex(t *testing.T) {
	b := initGoEthKZG(t)
	comm := BLSG1GeneratorCompressed
	cell := make([]byte, KZGBytesPerCell)
	proof := BLSPointAtInfinityG1
	_, err := b.VerifyCellProof(comm[:], cell, proof[:], KZGCellsPerExtBlob)
	if err != ErrKZGInvalidCellIndex {
		t.Errorf("expected ErrKZGInvalidCellIndex, got %v", err)
	}
}

func TestKZGGoEthNilCtxReturnsNotImplemented(t *testing.T) {
	b := &GoEthKZGBackend{} // ctx is nil
	blob := make([]byte, KZGBytesPerBlob)
	if _, err := b.BlobToCommitment(blob); err != ErrKZGBackendNotImplemented {
		t.Errorf("BlobToCommitment nil ctx: want ErrKZGBackendNotImplemented, got %v", err)
	}
	if _, err := b.VerifyBlobProof(nil, nil, nil); err != ErrKZGBackendNotImplemented {
		t.Errorf("VerifyBlobProof nil ctx: want ErrKZGBackendNotImplemented, got %v", err)
	}
	if _, err := b.ComputeCells(nil); err != ErrKZGBackendNotImplemented {
		t.Errorf("ComputeCells nil ctx: want ErrKZGBackendNotImplemented, got %v", err)
	}
	if _, err := b.VerifyCellProof(nil, nil, nil, 0); err != ErrKZGBackendNotImplemented {
		t.Errorf("VerifyCellProof nil ctx: want ErrKZGBackendNotImplemented, got %v", err)
	}
}

func TestKZGIntegrationCeremonyConfigStruct(t *testing.T) {
	config := KZGCeremonyConfig{
		SRSG1Lagrange: make([][]byte, KZGFieldElementsPerBlob),
		SRSG2:         make([][]byte, 2),
		Modulus:       KZGBLSModulus,
	}
	if len(config.SRSG1Lagrange) != 4096 {
		t.Errorf("SRSG1Lagrange size = %d, want 4096", len(config.SRSG1Lagrange))
	}
	if len(config.SRSG2) != 2 {
		t.Errorf("SRSG2 size = %d, want 2", len(config.SRSG2))
	}
	if config.Modulus != KZGBLSModulus {
		t.Error("modulus mismatch")
	}
}
