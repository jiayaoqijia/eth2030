// PQ blob integrity layer providing real post-quantum signing of blob
// commitments. Integrates ML-DSA-65 (real lattice signer), Falcon-512
// (NTRU lattice polynomial with NTT, q=12289), and SPHINCS+ (WOTS+ Merkle
// OTS with SHA-256) for the DAS post-quantum security roadmap (L+ era).
package pqblob

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/crypto/pqc"
)

// PQ blob integrity algorithm identifiers (distinct from pq_blob_signer.go).
const (
	IntegrityAlgMLDSA   uint8 = 10
	IntegrityAlgFalcon  uint8 = 11
	IntegrityAlgSPHINCS uint8 = 12
)

const integrityDomainTag = "pq-blob-integrity-v2"

// PQ blob integrity errors.
var (
	ErrIntegrityNilCommitment = errors.New("pq_integrity: nil commitment")
	ErrIntegrityEmptyData     = errors.New("pq_integrity: empty data")
	ErrIntegrityNilSigner     = errors.New("pq_integrity: nil signer")
	ErrIntegrityNilKey        = errors.New("pq_integrity: nil key pair")
	ErrIntegrityBadSignature  = errors.New("pq_integrity: invalid signature")
	ErrIntegrityBatchEmpty    = errors.New("pq_integrity: empty batch")
	ErrIntegrityBatchMismatch = errors.New("pq_integrity: batch length mismatch")
)

// PQBlobIntegritySig holds a PQ signature over a blob commitment.
type PQBlobIntegritySig struct {
	Algorithm        uint8
	SignatureBytes   []byte
	PublicKey        []byte
	CommitmentDigest [PQCommitmentSize]byte
	Timestamp        int64
}

// PQBlobIntegritySigner defines the interface for signing blob commitments.
type PQBlobIntegritySigner interface {
	SignCommitment(commitment *PQBlobCommitment, data []byte) (*PQBlobIntegritySig, error)
	VerifyIntegrity(sig *PQBlobIntegritySig, commitment *PQBlobCommitment) bool
	AlgorithmID() uint8
}

// --- ML-DSA Blob Integrity Signer ---

// MLDSABlobIntegritySigner uses the real ML-DSA-65 lattice signer.
type MLDSABlobIntegritySigner struct {
	mu      sync.RWMutex
	signer  *pqc.MLDSASigner
	keyPair *pqc.MLDSAKeyPair
}

func NewMLDSABlobIntegritySigner() (*MLDSABlobIntegritySigner, error) {
	signer := pqc.NewMLDSASigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("pq_integrity: mldsa keygen: %w", err)
	}
	return &MLDSABlobIntegritySigner{signer: signer, keyPair: kp}, nil
}

func (m *MLDSABlobIntegritySigner) AlgorithmID() uint8 { return IntegrityAlgMLDSA }

func (m *MLDSABlobIntegritySigner) SignCommitment(commitment *PQBlobCommitment, data []byte) (*PQBlobIntegritySig, error) {
	if commitment == nil {
		return nil, ErrIntegrityNilCommitment
	}
	if len(data) == 0 {
		return nil, ErrIntegrityEmptyData
	}
	m.mu.RLock()
	kp, signer := m.keyPair, m.signer
	m.mu.RUnlock()
	if kp == nil {
		return nil, ErrIntegrityNilKey
	}
	msg := integrityCommitMessage(commitment.Digest[:])
	sig, err := signer.Sign(kp, msg)
	if err != nil {
		return nil, fmt.Errorf("pq_integrity: mldsa sign: %w", err)
	}
	result := &PQBlobIntegritySig{
		Algorithm: IntegrityAlgMLDSA, SignatureBytes: sig,
		PublicKey: make([]byte, len(kp.PublicKey)), Timestamp: time.Now().Unix(),
	}
	copy(result.PublicKey, kp.PublicKey)
	copy(result.CommitmentDigest[:], commitment.Digest[:])
	return result, nil
}

func (m *MLDSABlobIntegritySigner) VerifyIntegrity(sig *PQBlobIntegritySig, commitment *PQBlobCommitment) bool {
	if !integrityPrecheck(sig, commitment, IntegrityAlgMLDSA) {
		return false
	}
	m.mu.RLock()
	signer := m.signer
	m.mu.RUnlock()
	return signer.Verify(sig.PublicKey, integrityCommitMessage(commitment.Digest[:]), sig.SignatureBytes)
}

// --- Falcon Blob Integrity Signer ---

// FalconBlobIntegritySigner uses NTRU-lattice polynomial arithmetic modulo
// q=12289 with NTT-based multiplication for signing blob commitments.
type FalconBlobIntegritySigner struct {
	mu      sync.RWMutex
	keyPair *pqc.PQKeyPair
}

func NewFalconBlobIntegritySigner() (*FalconBlobIntegritySigner, error) {
	fs := &pqc.FalconSigner{}
	kp, err := fs.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("pq_integrity: falcon keygen: %w", err)
	}
	return &FalconBlobIntegritySigner{keyPair: kp}, nil
}

func NewFalconBlobIntegritySignerWithKey(kp *pqc.PQKeyPair) (*FalconBlobIntegritySigner, error) {
	if kp == nil {
		return nil, ErrIntegrityNilKey
	}
	return &FalconBlobIntegritySigner{keyPair: kp}, nil
}

func (f *FalconBlobIntegritySigner) AlgorithmID() uint8 { return IntegrityAlgFalcon }

func (f *FalconBlobIntegritySigner) SignCommitment(commitment *PQBlobCommitment, data []byte) (*PQBlobIntegritySig, error) {
	if commitment == nil {
		return nil, ErrIntegrityNilCommitment
	}
	if len(data) == 0 {
		return nil, ErrIntegrityEmptyData
	}
	f.mu.RLock()
	kp := f.keyPair
	f.mu.RUnlock()
	if kp == nil {
		return nil, ErrIntegrityNilKey
	}
	msg := integrityCommitMessage(commitment.Digest[:])
	fs := &pqc.FalconSigner{}
	sig, err := fs.Sign(kp.SecretKey, msg)
	if err != nil {
		return nil, fmt.Errorf("pq_integrity: falcon sign: %w", err)
	}
	result := &PQBlobIntegritySig{
		Algorithm: IntegrityAlgFalcon, SignatureBytes: sig,
		PublicKey: make([]byte, len(kp.PublicKey)), Timestamp: time.Now().Unix(),
	}
	copy(result.PublicKey, kp.PublicKey)
	copy(result.CommitmentDigest[:], commitment.Digest[:])
	return result, nil
}

func (f *FalconBlobIntegritySigner) VerifyIntegrity(sig *PQBlobIntegritySig, commitment *PQBlobCommitment) bool {
	if !integrityPrecheck(sig, commitment, IntegrityAlgFalcon) {
		return false
	}
	fs := &pqc.FalconSigner{}
	return fs.Verify(sig.PublicKey, integrityCommitMessage(commitment.Digest[:]), sig.SignatureBytes)
}

// --- SPHINCS+ Blob Integrity Signer ---

// SPHINCSBlobIntegritySigner uses Merkle tree WOTS+ one-time signatures with
// SHA-256 for hash-based blob commitment signing.
type SPHINCSBlobIntegritySigner struct {
	mu      sync.RWMutex
	signer  *pqc.SPHINCSSigner
	keyPair *pqc.PQKeyPair
}

func NewSPHINCSBlobIntegritySigner() (*SPHINCSBlobIntegritySigner, error) {
	signer := pqc.NewSPHINCSSigner()
	kp, err := signer.GenerateKey()
	if err != nil {
		return nil, fmt.Errorf("pq_integrity: sphincs keygen: %w", err)
	}
	return &SPHINCSBlobIntegritySigner{signer: signer, keyPair: kp}, nil
}

func (s *SPHINCSBlobIntegritySigner) AlgorithmID() uint8 { return IntegrityAlgSPHINCS }

func (s *SPHINCSBlobIntegritySigner) SignCommitment(commitment *PQBlobCommitment, data []byte) (*PQBlobIntegritySig, error) {
	if commitment == nil {
		return nil, ErrIntegrityNilCommitment
	}
	if len(data) == 0 {
		return nil, ErrIntegrityEmptyData
	}
	s.mu.RLock()
	signer, kp := s.signer, s.keyPair
	s.mu.RUnlock()
	if kp == nil {
		return nil, ErrIntegrityNilKey
	}
	msg := integrityCommitMessage(commitment.Digest[:])
	sigBytes, err := signer.Sign(kp.SecretKey, msg)
	if err != nil {
		return nil, fmt.Errorf("pq_integrity: sphincs sign: %w", err)
	}
	result := &PQBlobIntegritySig{
		Algorithm: IntegrityAlgSPHINCS, SignatureBytes: sigBytes,
		PublicKey: make([]byte, len(kp.PublicKey)), Timestamp: time.Now().Unix(),
	}
	copy(result.PublicKey, kp.PublicKey)
	copy(result.CommitmentDigest[:], commitment.Digest[:])
	return result, nil
}

func (s *SPHINCSBlobIntegritySigner) VerifyIntegrity(sig *PQBlobIntegritySig, commitment *PQBlobCommitment) bool {
	if !integrityPrecheck(sig, commitment, IntegrityAlgSPHINCS) {
		return false
	}
	signer := pqc.NewSPHINCSSigner()
	return signer.Verify(sig.PublicKey, integrityCommitMessage(commitment.Digest[:]), sig.SignatureBytes)
}

// --- Batch Blob Verifier ---

// BatchBlobIntegrityVerifier verifies multiple blob integrity signatures concurrently.
type BatchBlobIntegrityVerifier struct{ workers int }

func NewBatchBlobIntegrityVerifier(workers int) *BatchBlobIntegrityVerifier {
	if workers < 1 {
		workers = 4
	}
	return &BatchBlobIntegrityVerifier{workers: workers}
}

// VerifyBatch verifies signatures against commitments. Returns (validCount, results, error).
func (v *BatchBlobIntegrityVerifier) VerifyBatch(
	sigs []*PQBlobIntegritySig, commitments []*PQBlobCommitment,
) (int, []bool, error) {
	if len(sigs) == 0 {
		return 0, nil, ErrIntegrityBatchEmpty
	}
	if len(sigs) != len(commitments) {
		return 0, nil, ErrIntegrityBatchMismatch
	}
	n := len(sigs)
	results := make([]bool, n)
	var validCount int64
	if n <= v.workers {
		for i := 0; i < n; i++ {
			ok := dispatchIntegrityVerify(sigs[i], commitments[i])
			results[i] = ok
			if ok {
				validCount++
			}
		}
		return int(validCount), results, nil
	}
	var wg sync.WaitGroup
	chunkSz := (n + v.workers - 1) / v.workers
	for w := 0; w < v.workers; w++ {
		start, end := w*chunkSz, (w+1)*chunkSz
		if end > n {
			end = n
		}
		if start >= n {
			break
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			for i := s; i < e; i++ {
				if dispatchIntegrityVerify(sigs[i], commitments[i]) {
					results[i] = true
					atomic.AddInt64(&validCount, 1)
				}
			}
		}(start, end)
	}
	wg.Wait()
	return int(validCount), results, nil
}

func dispatchIntegrityVerify(sig *PQBlobIntegritySig, commitment *PQBlobCommitment) bool {
	if sig == nil || commitment == nil {
		return false
	}
	msg := integrityCommitMessage(commitment.Digest[:])
	switch sig.Algorithm {
	case IntegrityAlgMLDSA:
		return pqc.NewMLDSASigner().Verify(sig.PublicKey, msg, sig.SignatureBytes)
	case IntegrityAlgFalcon:
		return (&pqc.FalconSigner{}).Verify(sig.PublicKey, msg, sig.SignatureBytes)
	case IntegrityAlgSPHINCS:
		return pqc.NewSPHINCSSigner().Verify(sig.PublicKey, msg, sig.SignatureBytes)
	default:
		return false
	}
}

// --- PQ Blob Integrity Report ---

// PQBlobIntegrityReport tracks algorithm distribution and verification success rates.
type PQBlobIntegrityReport struct {
	mu              sync.Mutex
	MLDSASigned     int64
	MLDSAVerified   int64
	MLDSAFailed     int64
	FalconSigned    int64
	FalconVerified  int64
	FalconFailed    int64
	SPHINCSSigned   int64
	SPHINCSVerified int64
	SPHINCSFailed   int64
	TotalSigned     int64
	TotalVerified   int64
	TotalFailed     int64
}

func NewPQBlobIntegrityReport() *PQBlobIntegrityReport { return &PQBlobIntegrityReport{} }

func (r *PQBlobIntegrityReport) RecordSign(algID uint8) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.TotalSigned++
	switch algID {
	case IntegrityAlgMLDSA:
		r.MLDSASigned++
	case IntegrityAlgFalcon:
		r.FalconSigned++
	case IntegrityAlgSPHINCS:
		r.SPHINCSSigned++
	}
}

func (r *PQBlobIntegrityReport) RecordVerify(algID uint8, valid bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if valid {
		r.TotalVerified++
		switch algID {
		case IntegrityAlgMLDSA:
			r.MLDSAVerified++
		case IntegrityAlgFalcon:
			r.FalconVerified++
		case IntegrityAlgSPHINCS:
			r.SPHINCSVerified++
		}
	} else {
		r.TotalFailed++
		switch algID {
		case IntegrityAlgMLDSA:
			r.MLDSAFailed++
		case IntegrityAlgFalcon:
			r.FalconFailed++
		case IntegrityAlgSPHINCS:
			r.SPHINCSFailed++
		}
	}
}

func (r *PQBlobIntegrityReport) SuccessRate() float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := r.TotalVerified + r.TotalFailed
	if total == 0 {
		return 0.0
	}
	return float64(r.TotalVerified) / float64(total)
}

func (r *PQBlobIntegrityReport) AlgorithmDistribution() map[uint8]float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	dist := make(map[uint8]float64)
	if r.TotalSigned == 0 {
		return dist
	}
	total := float64(r.TotalSigned)
	dist[IntegrityAlgMLDSA] = float64(r.MLDSASigned) / total
	dist[IntegrityAlgFalcon] = float64(r.FalconSigned) / total
	dist[IntegrityAlgSPHINCS] = float64(r.SPHINCSSigned) / total
	return dist
}

// --- CommitAndSign integration ---

// CommitAndSignBlob creates a PQ blob commitment and signs it in one step.
func CommitAndSignBlob(data []byte, signer PQBlobIntegritySigner) (*PQBlobCommitment, *PQBlobIntegritySig, error) {
	if signer == nil {
		return nil, nil, ErrIntegrityNilSigner
	}
	if len(data) == 0 {
		return nil, nil, ErrIntegrityEmptyData
	}
	commitment, err := CommitBlob(data)
	if err != nil {
		return nil, nil, fmt.Errorf("pq_integrity: commit: %w", err)
	}
	sig, err := signer.SignCommitment(commitment, data)
	if err != nil {
		return nil, nil, fmt.Errorf("pq_integrity: sign: %w", err)
	}
	return commitment, sig, nil
}

// --- Internal helpers ---

func integrityCommitMessage(commitDigest []byte) []byte {
	h := sha256.New()
	h.Write([]byte(integrityDomainTag))
	h.Write(commitDigest)
	return h.Sum(nil)
}

func integrityPrecheck(sig *PQBlobIntegritySig, commitment *PQBlobCommitment, expectedAlg uint8) bool {
	if sig == nil || commitment == nil {
		return false
	}
	if sig.Algorithm != expectedAlg {
		return false
	}
	if sig.CommitmentDigest != commitment.Digest {
		return false
	}
	return len(sig.PublicKey) > 0 && len(sig.SignatureBytes) > 0
}

// IntegrityAlgorithmName returns the human-readable name for an algorithm ID.
func IntegrityAlgorithmName(algID uint8) string {
	switch algID {
	case IntegrityAlgMLDSA:
		return "ML-DSA-65"
	case IntegrityAlgFalcon:
		return "Falcon-512"
	case IntegrityAlgSPHINCS:
		return "SPHINCS+-SHA256"
	default:
		return "unknown"
	}
}

// IntegritySignatureSize returns the expected signature byte size per algorithm.
func IntegritySignatureSize(algID uint8) int {
	switch algID {
	case IntegrityAlgMLDSA:
		return pqc.MLDSASignatureSize
	case IntegrityAlgFalcon:
		return pqc.Falcon512SigSize
	case IntegrityAlgSPHINCS:
		return pqc.SPHINCSSha256SigSize
	default:
		return 0
	}
}

// EncodeIntegritySig serializes a PQBlobIntegritySig to bytes.
func EncodeIntegritySig(sig *PQBlobIntegritySig) []byte {
	if sig == nil {
		return nil
	}
	buf := make([]byte, 1+8+PQCommitmentSize+2+len(sig.PublicKey)+2+len(sig.SignatureBytes))
	o := 0
	buf[o] = sig.Algorithm
	o++
	for i := 0; i < 8; i++ {
		buf[o+i] = byte(uint64(sig.Timestamp) >> (56 - 8*uint(i)))
	}
	o += 8
	copy(buf[o:], sig.CommitmentDigest[:])
	o += PQCommitmentSize
	buf[o], buf[o+1] = byte(len(sig.PublicKey)>>8), byte(len(sig.PublicKey))
	o += 2
	copy(buf[o:], sig.PublicKey)
	o += len(sig.PublicKey)
	buf[o], buf[o+1] = byte(len(sig.SignatureBytes)>>8), byte(len(sig.SignatureBytes))
	o += 2
	copy(buf[o:], sig.SignatureBytes)
	return buf
}

// DecodeIntegritySig deserializes a PQBlobIntegritySig from bytes.
func DecodeIntegritySig(data []byte) (*PQBlobIntegritySig, error) {
	if len(data) < 1+8+PQCommitmentSize+4 {
		return nil, ErrIntegrityBadSignature
	}
	sig := &PQBlobIntegritySig{Algorithm: data[0]}
	o := 1
	var ts uint64
	for i := 0; i < 8; i++ {
		ts = (ts << 8) | uint64(data[o+i])
	}
	sig.Timestamp = int64(ts)
	o += 8
	copy(sig.CommitmentDigest[:], data[o:o+PQCommitmentSize])
	o += PQCommitmentSize
	pkLen := int(data[o])<<8 | int(data[o+1])
	o += 2
	if o+pkLen+2 > len(data) {
		return nil, ErrIntegrityBadSignature
	}
	sig.PublicKey = make([]byte, pkLen)
	copy(sig.PublicKey, data[o:o+pkLen])
	o += pkLen
	sigLen := int(data[o])<<8 | int(data[o+1])
	o += 2
	if o+sigLen > len(data) {
		return nil, ErrIntegrityBadSignature
	}
	sig.SignatureBytes = make([]byte, sigLen)
	copy(sig.SignatureBytes, data[o:o+sigLen])
	return sig, nil
}
