package types

import (
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"github.com/cloudflare/circl/sign/slhdsa"
	"github.com/pornin/go-fn-dsa/fndsa"
)

func TestPQTxValidatorCreation(t *testing.T) {
	v := NewPQTxValidator()
	if v == nil {
		t.Fatal("validator is nil")
	}

	// All three algorithms should be registered.
	for _, alg := range PQSupportedAlgorithms() {
		if !v.IsWhitelisted(alg) {
			t.Fatalf("algorithm %d should be whitelisted by default", alg)
		}
	}
}

func TestPQTxValidatorAlgorithmNames(t *testing.T) {
	v := NewPQTxValidator()

	tests := []struct {
		alg  PQAlgorithm
		name string
	}{
		{PQ_MLDSA65, "ML-DSA-65"},
		{PQ_FALCON512, "Falcon-512"},
		{PQ_SPHINCS_SHA256, "SPHINCS+-SHA256"},
	}

	for _, tt := range tests {
		got := v.AlgorithmName(tt.alg)
		if got != tt.name {
			t.Fatalf("algorithm %d name = %q, want %q", tt.alg, got, tt.name)
		}
	}

	// Unknown algorithm.
	if v.AlgorithmName(PQAlgorithm(255)) != "unknown" {
		t.Fatal("unknown algorithm should return 'unknown'")
	}
}

func TestPQTxValidatorValidateNilTx(t *testing.T) {
	v := NewPQTxValidator()
	if err := v.ValidatePQTransaction(nil); err == nil {
		t.Fatal("expected error for nil transaction")
	}
}

func TestPQTxValidatorValidateNoSignature(t *testing.T) {
	v := NewPQTxValidator()
	tx := makePQValidationTestTx(PQSigDilithium)
	tx.PQSignature = nil

	if err := v.ValidatePQTransaction(tx); err == nil {
		t.Fatal("expected error for missing signature")
	}
}

func TestPQTxValidatorValidateNoPubKey(t *testing.T) {
	v := NewPQTxValidator()
	tx := makePQValidationTestTx(PQSigDilithium)
	tx.PQPublicKey = nil

	if err := v.ValidatePQTransaction(tx); err == nil {
		t.Fatal("expected error for missing public key")
	}
}

func TestPQTxValidatorValidateUnknownAlgorithm(t *testing.T) {
	v := NewPQTxValidator()
	tx := makePQValidationTestTx(PQSigDilithium)
	tx.PQSignatureType = 99 // unknown

	if err := v.ValidatePQTransaction(tx); err == nil {
		t.Fatal("expected error for unknown algorithm")
	}
}

func TestPQTxValidatorWhitelist(t *testing.T) {
	v := NewPQTxValidator()

	// Restrict to Falcon only.
	v.SetWhitelist([]PQAlgorithm{PQ_FALCON512})

	if v.IsWhitelisted(PQ_MLDSA65) {
		t.Fatal("MLDSA should not be whitelisted")
	}
	if !v.IsWhitelisted(PQ_FALCON512) {
		t.Fatal("Falcon should be whitelisted")
	}
	if v.IsWhitelisted(PQ_SPHINCS_SHA256) {
		t.Fatal("SPHINCS should not be whitelisted")
	}

	// Validate a Dilithium tx should fail.
	tx := makePQValidationTestTx(PQSigDilithium)
	if err := v.ValidatePQTransaction(tx); err == nil {
		t.Fatal("expected error for non-whitelisted algorithm")
	}
}

func TestPQTxValidatorSizeMismatch(t *testing.T) {
	v := NewPQTxValidator()

	// Wrong signature size for Dilithium.
	tx := makePQValidationTestTx(PQSigDilithium)
	tx.PQSignature = make([]byte, 100) // wrong size
	tx.PQSignature[0] = 0xFF

	if err := v.ValidatePQTransaction(tx); err == nil {
		t.Fatal("expected error for wrong signature size")
	}
}

func TestPQTxValidatorValidSignature(t *testing.T) {
	v := NewPQTxValidator()

	// Create a valid Dilithium transaction with correct sizes.
	tx := makePQValidationTestTx(PQSigDilithium)

	signPQValidationTestTx(t, tx)
	err := v.ValidatePQTransaction(tx)
	if err != nil {
		t.Fatalf("valid transaction rejected: %v", err)
	}
}

func TestPQTxValidatorFalconValidation(t *testing.T) {
	v := NewPQTxValidator()
	tx := makePQValidationTestTx(PQSigFalcon)
	tx.PQSignatureType = PQSigFalcon

	signPQValidationTestTx(t, tx)
	err := v.ValidatePQTransaction(tx)
	if err != nil {
		t.Fatalf("valid Falcon transaction rejected: %v", err)
	}
}

func TestPQTxValidatorSPHINCSValidation(t *testing.T) {
	v := NewPQTxValidator()
	tx := makePQValidationTestTx(PQSigSPHINCS)
	tx.PQSignatureType = PQSigSPHINCS

	signPQValidationTestTx(t, tx)
	err := v.ValidatePQTransaction(tx)
	if err != nil {
		t.Fatalf("valid SPHINCS transaction rejected: %v", err)
	}
}

func TestEstimatePQGas(t *testing.T) {
	v := NewPQTxValidator()

	tests := []struct {
		alg      PQAlgorithm
		expected uint64
	}{
		{PQ_MLDSA65, PQBaseGas + PQGasMLDSA65},
		{PQ_FALCON512, PQBaseGas + PQGasFalcon},
		{PQ_SPHINCS_SHA256, PQBaseGas + PQGasSPHINCS},
	}

	for _, tt := range tests {
		gas, err := v.EstimatePQGas(tt.alg)
		if err != nil {
			t.Fatalf("EstimatePQGas(%d) error: %v", tt.alg, err)
		}
		if gas != tt.expected {
			t.Fatalf("EstimatePQGas(%d) = %d, want %d", tt.alg, gas, tt.expected)
		}
	}

	// Unknown algorithm.
	_, err := v.EstimatePQGas(PQAlgorithm(99))
	if err == nil {
		t.Fatal("expected error for unknown algorithm")
	}
}

func TestEstimatePQGasStatic(t *testing.T) {
	if EstimatePQGasStatic(PQ_MLDSA65) != PQBaseGas+PQGasMLDSA65 {
		t.Fatal("static gas estimate wrong for MLDSA")
	}
	if EstimatePQGasStatic(PQ_FALCON512) != PQBaseGas+PQGasFalcon {
		t.Fatal("static gas estimate wrong for Falcon")
	}
	if EstimatePQGasStatic(PQAlgorithm(99)) != 0 {
		t.Fatal("static gas estimate for unknown should be 0")
	}
}

func TestPQTxPool(t *testing.T) {
	v := NewPQTxValidator()
	pool := NewPQTxPool(v, 10)

	if pool.PendingCount() != 0 {
		t.Fatal("pool should start empty")
	}

	// Accept a valid transaction.
	tx := makePQValidationTestTx(PQSigDilithium)

	tx.Gas = 100000

	signPQValidationTestTx(t, tx)
	err := pool.AcceptPQTx(tx)
	if err != nil {
		t.Fatalf("AcceptPQTx failed: %v", err)
	}
	if pool.PendingCount() != 1 {
		t.Fatalf("pending count = %d, want 1", pool.PendingCount())
	}

	// Duplicate should fail.
	err = pool.AcceptPQTx(tx)
	if err == nil {
		t.Fatal("expected error for duplicate transaction")
	}

	// Remove transaction.
	txHash := tx.Hash()
	pool.RemovePQTx(txHash)
	if pool.PendingCount() != 0 {
		t.Fatal("pool should be empty after removal")
	}
}

func TestPQTxPoolCapacity(t *testing.T) {
	v := NewPQTxValidator()
	pool := NewPQTxPool(v, 2) // very small pool

	for i := 0; i < 2; i++ {
		tx := makePQValidationTestTx(PQSigFalcon)
		tx.PQSignatureType = PQSigFalcon
		tx.Nonce = uint64(i)

		tx.Gas = 100000

		signPQValidationTestTx(t, tx)
		err := pool.AcceptPQTx(tx)
		if err != nil {
			t.Fatalf("AcceptPQTx(%d) failed: %v", i, err)
		}
	}

	// Third transaction should fail (pool full).
	tx := makePQValidationTestTx(PQSigFalcon)
	tx.PQSignatureType = PQSigFalcon
	tx.Nonce = 99

	tx.Gas = 100000

	err := pool.AcceptPQTx(tx)
	if err == nil {
		t.Fatal("expected error for full pool")
	}
}

func TestPQTxPoolInsufficientGas(t *testing.T) {
	v := NewPQTxValidator()
	pool := NewPQTxPool(v, 100)

	tx := makePQValidationTestTx(PQSigDilithium)

	tx.Gas = 100 // too low

	signPQValidationTestTx(t, tx)
	err := pool.AcceptPQTx(tx)
	if err == nil {
		t.Fatal("expected error for insufficient gas")
	}
}

func TestPQAlgorithmString(t *testing.T) {
	if PQAlgorithmString(PQ_MLDSA65) != "ML-DSA-65" {
		t.Fatal("wrong string for MLDSA")
	}
	if PQAlgorithmString(PQ_FALCON512) != "Falcon-512" {
		t.Fatal("wrong string for Falcon")
	}
	if PQAlgorithmString(PQ_SPHINCS_SHA256) != "SPHINCS+-SHA256" {
		t.Fatal("wrong string for SPHINCS")
	}
	if PQAlgorithmString(PQAlgorithm(99)) != "unknown(99)" {
		t.Fatal("wrong string for unknown")
	}
}

func TestPQSupportedAlgorithms(t *testing.T) {
	algs := PQSupportedAlgorithms()
	if len(algs) != 3 {
		t.Fatalf("supported algorithms count = %d, want 3", len(algs))
	}
}

// --- Test helpers ---

func makePQValidationTestTx(sigType uint8) *PQTransaction {
	to := Address{0x01, 0x02, 0x03}
	tx := NewPQTransaction(
		big.NewInt(1),
		0,
		&to,
		big.NewInt(1000),
		21000,
		big.NewInt(1),
		nil,
	)
	tx.PQSignatureType = sigType
	return tx
}

func makePQTestSig(size int) []byte {
	sig := make([]byte, size)
	for i := range sig {
		sig[i] = byte(i%251 + 1) // non-zero content
	}
	return sig
}

func makePQTestPK(size int) []byte {
	pk := make([]byte, size)
	for i := range pk {
		pk[i] = byte(i%239 + 1)
	}
	return pk
}

func signPQValidationTestTx(t *testing.T, tx *PQTransaction) {
	t.Helper()
	h := tx.SigningHash()
	switch tx.PQSignatureType {
	case PQSigDilithium:
		public, private, err := mldsa65.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		tx.PQPublicKey, err = public.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		tx.PQSignature = make([]byte, DilithiumSigSize)
		if err := mldsa65.SignTo(private, h[:], nil, false, tx.PQSignature); err != nil {
			t.Fatal(err)
		}
	case PQSigFalcon:
		private, public, err := fndsa.KeyGen(9, nil)
		if err != nil {
			t.Fatal(err)
		}
		tx.PQPublicKey = public
		tx.PQSignature, err = fndsa.Sign(nil, private, nil, 0, h[:])
		if err != nil {
			t.Fatal(err)
		}
	case PQSigSPHINCS:
		public, private, err := slhdsa.GenerateKey(rand.Reader, slhdsa.SHA2_128f)
		if err != nil {
			t.Fatal(err)
		}
		tx.PQPublicKey, err = public.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		tx.PQSignature, err = slhdsa.SignDeterministic(&private, slhdsa.NewMessage(h[:]), nil)
		if err != nil {
			t.Fatal(err)
		}
	}
}
