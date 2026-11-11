// Unified PQ signing pipeline for Ethereum transactions. Routes signing and
// verification to the correct post-quantum algorithm (ML-DSA-65, Falcon-512,
// SPHINCS+-SHA256) based on algorithm identifier. Thread-safe for concurrent use.
package pqc

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Pipeline algorithm identifiers.
const (
	PipelineAlgMLDSA65   uint8 = 20
	PipelineAlgFalcon512 uint8 = 21
	PipelineAlgSPHINCS   uint8 = 22
)

// Signing modes.
const (
	SignModeHashThenSign uint8 = 0
	SignModeDirect       uint8 = 1
)

// Gas costs per EIP-7932.
const (
	PipelineGasMLDSA65   uint64 = 4500
	PipelineGasFalcon512 uint64 = 3200
	PipelineGasSPHINCS   uint64 = 8000
)

const pipelineDomainTag = "eth2030-pq-tx-sign-v2"

var (
	ErrPipelineNilTx        = errors.New("pq_pipeline: nil transaction")
	ErrPipelineEmptyKey     = errors.New("pq_pipeline: empty key bytes")
	ErrPipelineUnknownAlg   = errors.New("pq_pipeline: unknown algorithm")
	ErrPipelineAlgNotFound  = errors.New("pq_pipeline: algorithm not registered")
	ErrPipelineAlgExists    = errors.New("pq_pipeline: algorithm already registered")
	ErrPipelineVerifyFailed = errors.New("pq_pipeline: verification failed")
	ErrPipelineInvalidSig   = errors.New("pq_pipeline: invalid signature format")
	ErrPipelineEmptyTxData  = errors.New("pq_pipeline: empty transaction data")
)

// PipelineSignerEntry holds a signer with metadata for the pipeline.
type PipelineSignerEntry struct {
	AlgID      uint8
	Name       string
	GasCost    uint64
	SigSize    int
	PubKeySize int
	SecKeySize int
	SignFn     func(sk, msg []byte) ([]byte, error)
	VerifyFn   func(pk, msg, sig []byte) bool
	KeyGenFn   func() (pk, sk []byte, err error)
}

// PQSigningPipeline manages multiple PQ signers and routes operations.
type PQSigningPipeline struct {
	mu      sync.RWMutex
	signers map[uint8]*PipelineSignerEntry
	mode    uint8
}

// NewPQSigningPipeline creates a pipeline with default signers registered.
func NewPQSigningPipeline() *PQSigningPipeline {
	p := &PQSigningPipeline{signers: make(map[uint8]*PipelineSignerEntry), mode: SignModeHashThenSign}
	p.registerDefaults()
	return p
}

// NewPQSigningPipelineEmpty creates an empty pipeline.
func NewPQSigningPipelineEmpty() *PQSigningPipeline {
	return &PQSigningPipeline{signers: make(map[uint8]*PipelineSignerEntry), mode: SignModeHashThenSign}
}

func (p *PQSigningPipeline) SetMode(mode uint8) {
	p.mu.Lock()
	p.mode = mode
	p.mu.Unlock()
}

func (p *PQSigningPipeline) RegisterSigner(entry *PipelineSignerEntry) error {
	if entry == nil {
		return ErrPipelineEmptyKey
	}
	if entry.SignFn == nil || entry.VerifyFn == nil {
		return fmt.Errorf("%w: sign/verify functions required", ErrPipelineUnknownAlg)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.signers[entry.AlgID]; exists {
		return fmt.Errorf("%w: %d", ErrPipelineAlgExists, entry.AlgID)
	}
	p.signers[entry.AlgID] = entry
	return nil
}

// SignTransaction signs a PQ transaction. Returns (signature, publicKey, error).
func (p *PQSigningPipeline) SignTransaction(tx *types.PQTransaction, algID uint8, privateKey []byte) ([]byte, []byte, error) {
	if tx == nil {
		return nil, nil, ErrPipelineNilTx
	}
	if len(privateKey) == 0 {
		return nil, nil, ErrPipelineEmptyKey
	}
	p.mu.RLock()
	entry, exists := p.signers[algID]
	mode := p.mode
	p.mu.RUnlock()
	if !exists {
		return nil, nil, fmt.Errorf("%w: %d", ErrPipelineAlgNotFound, algID)
	}
	msg := pipelineTxMessage(tx, mode)
	if len(msg) == 0 {
		return nil, nil, ErrPipelineEmptyTxData
	}
	sig, err := entry.SignFn(privateKey, msg)
	if err != nil {
		return nil, nil, fmt.Errorf("pq_pipeline: sign: %w", err)
	}
	pk := pipelineDerivePublicKey(algID, privateKey, entry.PubKeySize)
	return sig, pk, nil
}

// VerifyTransaction verifies a PQ transaction. Returns (valid, signerAddress, error).
func (p *PQSigningPipeline) VerifyTransaction(tx *types.PQTransaction) (bool, types.Address, error) {
	if tx == nil {
		return false, types.Address{}, ErrPipelineNilTx
	}
	if len(tx.PQSignature) == 0 || len(tx.PQPublicKey) == 0 {
		return false, types.Address{}, ErrPipelineInvalidSig
	}
	algID := pqSigTypeToPipelineAlg(tx.PQSignatureType)
	p.mu.RLock()
	entry, exists := p.signers[algID]
	mode := p.mode
	p.mu.RUnlock()
	if !exists {
		return false, types.Address{}, fmt.Errorf("%w: %d", ErrPipelineAlgNotFound, algID)
	}
	msg := pipelineTxMessage(tx, mode)
	if !entry.VerifyFn(tx.PQPublicKey, msg, tx.PQSignature) {
		return false, types.Address{}, ErrPipelineVerifyFailed
	}
	return true, PipelineDeriveAddress(algID, tx.PQPublicKey), nil
}

// PipelineDeriveAddress derives an Ethereum address from a PQ public key
// per EIP-7932: address = Keccak256(algorithm_id || public_key)[12:32].
func PipelineDeriveAddress(algID uint8, pubkey []byte) types.Address {
	input := make([]byte, 1+len(pubkey))
	input[0] = algID
	copy(input[1:], pubkey)
	hash := crypto.Keccak256(input)
	var addr types.Address
	copy(addr[:], hash[12:32])
	return addr
}

func (p *PQSigningPipeline) EstimateGas(algID uint8) (uint64, error) {
	p.mu.RLock()
	entry, exists := p.signers[algID]
	p.mu.RUnlock()
	if !exists {
		return 0, fmt.Errorf("%w: %d", ErrPipelineAlgNotFound, algID)
	}
	return entry.GasCost, nil
}

func (p *PQSigningPipeline) GenerateKey(algID uint8) ([]byte, []byte, error) {
	p.mu.RLock()
	entry, exists := p.signers[algID]
	p.mu.RUnlock()
	if !exists {
		return nil, nil, fmt.Errorf("%w: %d", ErrPipelineAlgNotFound, algID)
	}
	if entry.KeyGenFn != nil {
		return entry.KeyGenFn()
	}
	return nil, nil, fmt.Errorf("%w: no keygen for %d", ErrPipelineUnknownAlg, algID)
}

func (p *PQSigningPipeline) GetSigner(algID uint8) (*PipelineSignerEntry, error) {
	p.mu.RLock()
	entry, exists := p.signers[algID]
	p.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("%w: %d", ErrPipelineAlgNotFound, algID)
	}
	return entry, nil
}

func (p *PQSigningPipeline) RegisteredAlgorithms() []uint8 {
	p.mu.RLock()
	defer p.mu.RUnlock()
	algs := make([]uint8, 0, len(p.signers))
	for id := range p.signers {
		algs = append(algs, id)
	}
	return algs
}

// BatchVerifyTransactions verifies multiple PQ transactions concurrently.
func (p *PQSigningPipeline) BatchVerifyTransactions(txs []*types.PQTransaction) ([]bool, error) {
	if len(txs) == 0 {
		return nil, nil
	}
	results := make([]bool, len(txs))
	var wg sync.WaitGroup
	for i, tx := range txs {
		wg.Add(1)
		go func(idx int, t *types.PQTransaction) {
			defer wg.Done()
			valid, _, err := p.VerifyTransaction(t)
			results[idx] = valid && err == nil
		}(i, tx)
	}
	wg.Wait()
	return results, nil
}

// PipelineAlgInfo holds metadata about a registered algorithm.
type PipelineAlgInfo struct {
	AlgID                           uint8
	Name                            string
	SecurityLevel                   int
	GasCost                         uint64
	SigSize, PubKeySize, SecKeySize int
}

func (p *PQSigningPipeline) AlgorithmInfo(algID uint8) (*PipelineAlgInfo, error) {
	p.mu.RLock()
	entry, exists := p.signers[algID]
	p.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("%w: %d", ErrPipelineAlgNotFound, algID)
	}
	return &PipelineAlgInfo{
		AlgID: algID, Name: entry.Name, SecurityLevel: pipelineSecurityLevel(algID),
		GasCost: entry.GasCost, SigSize: entry.SigSize, PubKeySize: entry.PubKeySize,
		SecKeySize: entry.SecKeySize,
	}, nil
}

// --- Internal helpers ---

func (p *PQSigningPipeline) registerDefaults() {
	mldsa := NewMLDSASigner()
	p.signers[PipelineAlgMLDSA65] = &PipelineSignerEntry{
		AlgID: PipelineAlgMLDSA65, Name: "ML-DSA-65", GasCost: PipelineGasMLDSA65,
		SigSize: MLDSASignatureSize, PubKeySize: MLDSAPublicKeySize, SecKeySize: MLDSAPrivateKeySize,
		SignFn: func(sk, msg []byte) ([]byte, error) {
			kp, err := pipelineMLDSAKeyFromSK(sk)
			if err != nil {
				return nil, err
			}
			return mldsa.Sign(kp, msg)
		},
		VerifyFn: func(pk, msg, sig []byte) bool { return mldsa.Verify(pk, msg, sig) },
		KeyGenFn: func() ([]byte, []byte, error) {
			kp, err := mldsa.GenerateKey()
			if err != nil {
				return nil, nil, err
			}
			return kp.PublicKey, kp.SecretKey, nil
		},
	}
	falcon := &FalconSigner{}
	p.signers[PipelineAlgFalcon512] = &PipelineSignerEntry{
		AlgID: PipelineAlgFalcon512, Name: "Falcon-512", GasCost: PipelineGasFalcon512,
		SigSize: Falcon512SigSize, PubKeySize: Falcon512PubKeySize, SecKeySize: Falcon512SecKeySize,
		SignFn:   func(sk, msg []byte) ([]byte, error) { return falcon.Sign(sk, msg) },
		VerifyFn: func(pk, msg, sig []byte) bool { return falcon.Verify(pk, msg, sig) },
		KeyGenFn: func() ([]byte, []byte, error) {
			kp, err := falcon.GenerateKey()
			if err != nil {
				return nil, nil, err
			}
			return kp.PublicKey, kp.SecretKey, nil
		},
	}
	sphincs := NewSPHINCSSigner()
	p.signers[PipelineAlgSPHINCS] = &PipelineSignerEntry{
		AlgID: PipelineAlgSPHINCS, Name: "SPHINCS+-SHA256", GasCost: PipelineGasSPHINCS,
		SigSize: SPHINCSSha256SigSize, PubKeySize: SPHINCSSha256PubKeySize, SecKeySize: SPHINCSSha256SecKeySize,
		SignFn:   func(sk, msg []byte) ([]byte, error) { return sphincs.Sign(sk, msg) },
		VerifyFn: func(pk, msg, sig []byte) bool { return sphincs.Verify(pk, msg, sig) },
		KeyGenFn: func() ([]byte, []byte, error) {
			kp, err := sphincs.GenerateKey()
			if err != nil {
				return nil, nil, err
			}
			return kp.PublicKey, kp.SecretKey, nil
		},
	}
}

func pipelineTxMessage(tx *types.PQTransaction, mode uint8) []byte {
	if tx == nil {
		return nil
	}
	var payload []byte
	payload = append(payload, []byte(pipelineDomainTag)...)
	if tx.ChainID != nil {
		payload = append(payload, tx.ChainID.Bytes()...)
	}
	var nonceBuf [8]byte
	binary.BigEndian.PutUint64(nonceBuf[:], tx.Nonce)
	payload = append(payload, nonceBuf[:]...)
	if tx.To != nil {
		payload = append(payload, tx.To[:]...)
	}
	if tx.Value != nil {
		payload = append(payload, tx.Value.Bytes()...)
	}
	var gasBuf [8]byte
	binary.BigEndian.PutUint64(gasBuf[:], tx.Gas)
	payload = append(payload, gasBuf[:]...)
	if tx.GasPrice != nil {
		payload = append(payload, tx.GasPrice.Bytes()...)
	}
	payload = append(payload, tx.Data...)
	if mode == SignModeDirect {
		return payload
	}
	h := sha256.Sum256(payload)
	return h[:]
}

func pipelineDerivePublicKey(algID uint8, sk []byte, pkSize int) []byte {
	input := make([]byte, 1+len(sk))
	input[0] = algID
	copy(input[1:], sk)
	seed := crypto.Keccak256(input)
	pk := make([]byte, pkSize)
	offset, current := 0, seed
	for offset < len(pk) {
		n := copy(pk[offset:], current)
		offset += n
		current = crypto.Keccak256(current)
	}
	return pk
}

func pipelineMLDSAKeyFromSK(sk []byte) (*MLDSAKeyPair, error) {
	if len(sk) != MLDSAPrivateKeySize {
		return nil, ErrInvalidKeySize
	}
	return &MLDSAKeyPair{SecretKey: sk}, nil
}

func pqSigTypeToPipelineAlg(sigType uint8) uint8 {
	switch sigType {
	case types.PQSigDilithium:
		return PipelineAlgMLDSA65
	case types.PQSigFalcon:
		return PipelineAlgFalcon512
	case types.PQSigSPHINCS:
		return PipelineAlgSPHINCS
	default:
		return sigType
	}
}

func pipelineSecurityLevel(algID uint8) int {
	switch algID {
	case PipelineAlgMLDSA65:
		return 3
	case PipelineAlgFalcon512, PipelineAlgSPHINCS:
		return 1
	default:
		return 0
	}
}

// PipelineAlgorithmName returns the name for a pipeline algorithm.
func PipelineAlgorithmName(algID uint8) string {
	switch algID {
	case PipelineAlgMLDSA65:
		return "ML-DSA-65"
	case PipelineAlgFalcon512:
		return "Falcon-512"
	case PipelineAlgSPHINCS:
		return "SPHINCS+-SHA256"
	default:
		return "unknown"
	}
}
