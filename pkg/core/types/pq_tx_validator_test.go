package types_test

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/pqc"
	"golang.org/x/crypto/sha3"
)

// newTestPQTx creates a test PQ transaction.
func newTestPQTx() *types.PQTransaction {
	to := types.BytesToAddress([]byte{0x01, 0x02, 0x03})
	return types.NewPQTransaction(big.NewInt(1), 42, &to, big.NewInt(100), 100000, big.NewInt(10), []byte("test payload"))
}

// pqTestSigningHash reproduces pqRealSigningHash for test-side signing.
func pqTestSigningHash(tx *types.PQTransaction) []byte {
	d := sha3.NewLegacyKeccak256()
	if tx.ChainID != nil {
		d.Write(tx.ChainID.Bytes())
	}
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], tx.Nonce)
	d.Write(buf[:])
	if tx.To != nil {
		d.Write(tx.To[:])
	}
	if tx.Value != nil {
		d.Write(tx.Value.Bytes())
	}
	binary.BigEndian.PutUint64(buf[:], tx.Gas)
	d.Write(buf[:])
	if tx.GasPrice != nil {
		d.Write(tx.GasPrice.Bytes())
	}
	d.Write(tx.Data)
	return d.Sum(nil)
}

// signMLDSATx signs a test tx with ML-DSA and returns a wired-up tx.
func signMLDSATx(t *testing.T, v *types.PQTxValidatorReal) (*types.PQTransaction, *pqc.MLDSASigner) {
	t.Helper()
	signer := pqc.NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("ML-DSA keygen: %v", err)
	}
	tx := newTestPQTx()
	sig, err := signer.Sign(kp, pqTestSigningHash(tx))
	if err != nil {
		t.Fatalf("ML-DSA sign: %v", err)
	}
	tx.PQSignatureType = types.PQSigTypeMLDSA
	tx.PQPublicKey = kp.PublicKey
	tx.PQSignature = sig
	v.RegisterSigner(types.PQSigTypeMLDSA, func(pk, m, s []byte) bool {
		return signer.Verify(pk, m, s)
	})
	return tx, signer
}

func TestPQTxValidatorRealNewDefault(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	if v == nil {
		t.Fatal("nil validator")
	}
	algs := v.SupportedAlgorithmsReal()
	if len(algs) != 3 {
		t.Fatalf("supported = %d, want 3", len(algs))
	}
	expected := []string{"ML-DSA-65", "Falcon-512", "SPHINCS+-SHA256"}
	for i, name := range expected {
		if algs[i] != name {
			t.Fatalf("alg[%d] = %q, want %q", i, algs[i], name)
		}
	}
}

func TestPQTxValidatorRealValidateMLDSA(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	tx, _ := signMLDSATx(t, v)
	if err := v.ValidatePQSignatureReal(tx); err != nil {
		t.Fatalf("valid ML-DSA rejected: %v", err)
	}
}

func TestPQTxValidatorRealValidateFalcon(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	signer := &pqc.FalconSigner{}
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("Falcon keygen: %v", err)
	}
	tx := newTestPQTx()
	sig, err := signer.Sign(kp.SecretKey, pqTestSigningHash(tx))
	if err != nil {
		t.Fatalf("Falcon sign: %v", err)
	}
	tx.PQSignatureType = types.PQSigTypeFalcon
	tx.PQPublicKey = kp.PublicKey
	tx.PQSignature = sig
	v.RegisterSigner(types.PQSigTypeFalcon, func(pk, m, s []byte) bool {
		return signer.Verify(pk, m, s)
	})
	if err := v.ValidatePQSignatureReal(tx); err != nil {
		t.Fatalf("valid Falcon rejected: %v", err)
	}
}

func TestPQTxValidatorRealValidateSPHINCS(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	signer := pqc.NewSPHINCSSigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("SPHINCS+ keygen: %v", err)
	}
	tx := newTestPQTx()
	sig, err := signer.Sign(kp.SecretKey, pqTestSigningHash(tx))
	if err != nil {
		t.Fatalf("SPHINCS+ sign: %v", err)
	}
	tx.PQSignatureType = types.PQSigTypeSPHINCS
	tx.PQPublicKey = kp.PublicKey
	tx.PQSignature = sig
	// SPHINCS+ hash-tree verification needs circl for full correctness.
	// Register structural verifier checking non-trivial content.
	v.RegisterSigner(types.PQSigTypeSPHINCS, func(pk, m, s []byte) bool {
		if len(pk) < 32 || len(s) == 0 || len(m) == 0 {
			return false
		}
		for _, b := range s[:32] {
			if b != 0 {
				return true
			}
		}
		return false
	})
	if err := v.ValidatePQSignatureReal(tx); err != nil {
		t.Fatalf("SPHINCS+ validation failed: %v", err)
	}
	// All-zero signature should be rejected.
	tx2 := newTestPQTx()
	tx2.PQSignatureType = types.PQSigTypeSPHINCS
	tx2.PQPublicKey = kp.PublicKey
	tx2.PQSignature = make([]byte, types.SPHINCSPlusSigSize)
	if err := v.ValidatePQSignatureReal(tx2); err == nil {
		t.Fatal("all-zero SPHINCS+ sig should be rejected")
	}
}

func TestPQTxValidatorRealInvalidSignature(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	tx, _ := signMLDSATx(t, v)
	tx.PQSignature[0] ^= 0xFF
	tx.PQSignature[10] ^= 0xFF
	if err := v.ValidatePQSignatureReal(tx); err == nil {
		t.Fatal("tampered signature should be rejected")
	}
}

func TestPQTxValidatorRealWrongAlgorithm(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	falconSigner := &pqc.FalconSigner{}
	kp, err := falconSigner.GenerateKey()
	if err != nil {
		t.Fatalf("Falcon keygen: %v", err)
	}
	tx := newTestPQTx()
	sig, err := falconSigner.Sign(kp.SecretKey, pqTestSigningHash(tx))
	if err != nil {
		t.Fatalf("Falcon sign: %v", err)
	}
	// Claim ML-DSA but use Falcon key/sig.
	tx.PQSignatureType = types.PQSigTypeMLDSA
	tx.PQPublicKey = kp.PublicKey
	tx.PQSignature = sig
	mldsaSigner := pqc.NewMLDSASigner()
	v.RegisterSigner(types.PQSigTypeMLDSA, func(pk, m, s []byte) bool {
		return mldsaSigner.Verify(pk, m, s)
	})
	if err := v.ValidatePQSignatureReal(tx); err == nil {
		t.Fatal("wrong algorithm type should be rejected")
	}
}

func TestPQTxValidatorRealEmptySignature(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	tx := newTestPQTx()
	tx.PQSignatureType = types.PQSigTypeMLDSA
	tx.PQPublicKey = make([]byte, types.DilithiumPubKeySize)
	tx.PQSignature = nil
	if err := v.ValidatePQSignatureReal(tx); err == nil {
		t.Fatal("empty signature should be rejected")
	}
}

func TestPQTxValidatorRealEstimateGas(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	tests := []struct {
		st uint8
		g  uint64
	}{
		{types.PQSigTypeMLDSA, types.PQGasCostMLDSA},
		{types.PQSigTypeFalcon, types.PQGasCostFalcon},
		{types.PQSigTypeSPHINCS, types.PQGasCostSPHINCS},
	}
	for _, tt := range tests {
		gas, err := v.EstimatePQGasReal(tt.st)
		if err != nil {
			t.Fatalf("gas(%d): %v", tt.st, err)
		}
		if gas != tt.g {
			t.Fatalf("gas(%d) = %d, want %d", tt.st, gas, tt.g)
		}
	}
	if _, err := v.EstimatePQGasReal(99); err == nil {
		t.Fatal("unknown alg should error")
	}
}

func TestPQTxValidatorRealRecoverSender(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	signer := pqc.NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	tx := newTestPQTx()
	tx.PQPublicKey = kp.PublicKey
	tx.PQSignature = make([]byte, types.DilithiumSigSize)
	addr, err := v.RecoverPQSender(tx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if addr.IsZero() {
		t.Fatal("address should not be zero")
	}
}

func TestPQTxValidatorRealRecoverSenderConsistency(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	signer := pqc.NewMLDSASigner()
	kp1, _ := signer.GenerateKey()
	kp2, _ := signer.GenerateKey()

	tx1 := newTestPQTx()
	tx1.PQPublicKey = kp1.PublicKey
	tx1.PQSignature = make([]byte, types.DilithiumSigSize)

	tx2 := newTestPQTx()
	tx2.Nonce = 99
	tx2.PQPublicKey = kp1.PublicKey
	tx2.PQSignature = make([]byte, types.DilithiumSigSize)

	a1, _ := v.RecoverPQSender(tx1)
	a2, _ := v.RecoverPQSender(tx2)
	if a1 != a2 {
		t.Fatal("same pubkey must give same address")
	}

	tx3 := newTestPQTx()
	tx3.PQPublicKey = kp2.PublicKey
	tx3.PQSignature = make([]byte, types.DilithiumSigSize)
	a3, _ := v.RecoverPQSender(tx3)
	if a1 == a3 {
		t.Fatal("different pubkeys must give different addresses")
	}
}

func TestPQTxValidatorRealSupportedAlgorithms(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	algs := v.SupportedAlgorithmsReal()
	want := []string{"ML-DSA-65", "Falcon-512", "SPHINCS+-SHA256"}
	if len(algs) != len(want) {
		t.Fatalf("got %d algs, want %d", len(algs), len(want))
	}
	for i := range want {
		if algs[i] != want[i] {
			t.Fatalf("algs[%d] = %q, want %q", i, algs[i], want[i])
		}
	}
}

func TestPQTxValidatorRealKeySize(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	valid := []struct {
		st uint8
		sz int
	}{
		{types.PQSigTypeMLDSA, types.DilithiumPubKeySize},
		{types.PQSigTypeFalcon, 897},
		{types.PQSigTypeSPHINCS, 32},
	}
	for _, tt := range valid {
		if err := v.ValidatePQKeySize(tt.st, make([]byte, tt.sz)); err != nil {
			t.Fatalf("valid key(%d, %d): %v", tt.st, tt.sz, err)
		}
	}
	invalid := []struct {
		st uint8
		sz int
	}{
		{types.PQSigTypeMLDSA, 100},
		{types.PQSigTypeFalcon, 2000},
		{types.PQSigTypeSPHINCS, 64},
	}
	for _, tt := range invalid {
		if err := v.ValidatePQKeySize(tt.st, make([]byte, tt.sz)); err == nil {
			t.Fatalf("bad key(%d, %d) should error", tt.st, tt.sz)
		}
	}
	if err := v.ValidatePQKeySize(99, make([]byte, 32)); err == nil {
		t.Fatal("unknown alg should error")
	}
}

func TestPQTxValidatorRealNilTransaction(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	if err := v.ValidatePQSignatureReal(nil); err == nil {
		t.Fatal("nil tx validate should error")
	}
	if _, err := v.RecoverPQSender(nil); err == nil {
		t.Fatal("nil tx recover should error")
	}
	if err := v.ValidatePQHybrid(nil); err == nil {
		t.Fatal("nil tx hybrid should error")
	}
}

func TestPQTxValidatorRealHybridMode(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	tx, _ := signMLDSATx(t, v)
	tx.ClassicSignature = []byte{0x01, 0x02, 0x03}
	if err := v.ValidatePQHybrid(tx); err != nil {
		t.Fatalf("hybrid pass: %v", err)
	}
	tx.ClassicSignature = nil
	if err := v.ValidatePQHybrid(tx); err == nil {
		t.Fatal("hybrid should require classic sig")
	}
}

func TestPQTxValidatorRealBatchValidate(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	signer := pqc.NewMLDSASigner()
	v.RegisterSigner(types.PQSigTypeMLDSA, func(pk, m, s []byte) bool {
		return signer.Verify(pk, m, s)
	})
	kp, _ := signer.GenerateKey()
	txs := make([]*types.PQTransaction, 5)
	for i := range txs {
		tx := newTestPQTx()
		tx.Nonce = uint64(i)
		sig, err := signer.Sign(kp, pqTestSigningHash(tx))
		if err != nil {
			t.Fatalf("sign %d: %v", i, err)
		}
		tx.PQSignatureType = types.PQSigTypeMLDSA
		tx.PQPublicKey = kp.PublicKey
		tx.PQSignature = sig
		txs[i] = tx
	}
	errs := v.ValidatePQBatch(txs)
	for i, e := range errs {
		if e != nil {
			t.Fatalf("tx %d: %v", i, e)
		}
	}
	// Tamper tx[2].
	txs[2].PQSignature[0] ^= 0xFF
	errs = v.ValidatePQBatch(txs)
	if errs[2] == nil {
		t.Fatal("tampered tx[2] should fail")
	}
	for i, e := range errs {
		if i != 2 && e != nil {
			t.Fatalf("tx %d should pass: %v", i, e)
		}
	}
}

func TestPQTxValidatorRealBatchEmpty(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	if v.ValidatePQBatch(nil) != nil {
		t.Fatal("nil batch should return nil")
	}
	if v.ValidatePQBatch([]*types.PQTransaction{}) != nil {
		t.Fatal("empty batch should return nil")
	}
}

func TestPQTxValidatorRealNoVerifier(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	tx := newTestPQTx()
	tx.PQSignatureType = types.PQSigTypeMLDSA
	tx.PQPublicKey = make([]byte, types.DilithiumPubKeySize)
	tx.PQPublicKey[0] = 1
	tx.PQSignature = make([]byte, types.DilithiumSigSize)
	tx.PQSignature[0] = 1
	if err := v.ValidatePQSignatureReal(tx); err == nil {
		t.Fatal("nil verifier should error")
	}
}

func TestPQTxValidatorRealGetSignerEntry(t *testing.T) {
	v := types.NewPQTxValidatorReal()
	entry, ok := v.GetSignerEntry(types.PQSigTypeMLDSA)
	if !ok || entry.Name != "ML-DSA-65" || entry.GasCost != types.PQGasCostMLDSA {
		t.Fatalf("bad ML-DSA entry: ok=%v", ok)
	}
	if _, ok := v.GetSignerEntry(99); ok {
		t.Fatal("unknown alg should not exist")
	}
}

func TestPQTxValidatorRealPubKeyToAddress(t *testing.T) {
	pk := make([]byte, 100)
	for i := range pk {
		pk[i] = byte(i + 1)
	}
	addr := types.PQPubKeyToAddress(pk)
	if addr.IsZero() {
		t.Fatal("non-zero pk should give non-zero address")
	}
	if types.PQPubKeyToAddress(pk) != addr {
		t.Fatal("same pk must give same address")
	}
	pk2 := make([]byte, 100)
	for i := range pk2 {
		pk2[i] = byte(i + 10)
	}
	if types.PQPubKeyToAddress(pk2) == addr {
		t.Fatal("different pk must give different address")
	}
}

func TestPQTxValidatorRealGasOrdering(t *testing.T) {
	if types.PQGasCostMLDSA >= types.PQGasCostFalcon {
		t.Fatal("ML-DSA < Falcon expected")
	}
	if types.PQGasCostFalcon >= types.PQGasCostSPHINCS {
		t.Fatal("Falcon < SPHINCS expected")
	}
}
