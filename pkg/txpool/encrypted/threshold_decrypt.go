// Package encrypted implements threshold decryption for the encrypted mempool.
// This file provides the ThresholdDecryptor, which collects decryption shares
// from validators and combines them once the t-of-n threshold is reached.
// Part of the EL Cryptography roadmap: encrypted mempool.
package encrypted

import (
	"crypto/aes"
	"crypto/cipher"
	"errors"
	"math/big"
	"sync"

	ethcrypto "github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/crypto/threshold"
)

var (
	ErrThresholdInvalid    = errors.New("threshold_decrypt: t must be >= 1 and <= n")
	ErrShareAlreadyAdded   = errors.New("threshold_decrypt: share already added for this validator")
	ErrInvalidShareData    = errors.New("threshold_decrypt: share data is nil or empty")
	ErrThresholdNotMet     = errors.New("threshold_decrypt: insufficient shares for decryption")
	ErrNoCiphertext        = errors.New("threshold_decrypt: no ciphertext set")
	ErrDecryptFailed       = errors.New("threshold_decrypt: decryption failed")
	ErrInvalidCommitment   = errors.New("threshold_decrypt: invalid commitment")
	ErrShareVerifyFailed   = errors.New("threshold_decrypt: share verification failed")
	ErrEpochMismatch       = errors.New("threshold_decrypt: share epoch does not match current epoch")
	ErrInvalidValidatorIdx = errors.New("threshold_decrypt: validator index must be >= 0")
)

// DecryptionShare represents a single validator's contribution to the
// threshold decryption process. Each share is bound to a specific epoch.
type DecryptionShare struct {
	ValidatorIndex int
	ShareBytes     []byte
	Epoch          uint64
}

// ThresholdDecryptor manages the collection and combination of decryption
// shares for the encrypted mempool. It requires t-of-n shares before
// decryption can proceed.
type ThresholdDecryptor struct {
	mu         sync.RWMutex
	threshold  int // minimum shares needed (t)
	totalParts int // total validators (n)
	epoch      uint64
	shares     map[int]*DecryptionShare // validatorIndex -> share
	ciphertext []byte                   // encrypted transaction data
	nonce      []byte                   // AES-GCM nonce
}

// NewThresholdDecryptor creates a new decryptor with the given t-of-n
// threshold parameters.
func NewThresholdDecryptor(threshold, totalParts int) (*ThresholdDecryptor, error) {
	if threshold < 1 || threshold > totalParts {
		return nil, ErrThresholdInvalid
	}
	return &ThresholdDecryptor{
		threshold:  threshold,
		totalParts: totalParts,
		shares:     make(map[int]*DecryptionShare),
	}, nil
}

// SetCiphertext sets the encrypted data to be decrypted once threshold is met.
func (td *ThresholdDecryptor) SetCiphertext(ciphertext, nonce []byte) {
	td.mu.Lock()
	defer td.mu.Unlock()
	td.ciphertext = make([]byte, len(ciphertext))
	copy(td.ciphertext, ciphertext)
	td.nonce = make([]byte, len(nonce))
	copy(td.nonce, nonce)
}

// SetEpoch sets the current epoch for the decryptor.
func (td *ThresholdDecryptor) SetEpoch(epoch uint64) {
	td.mu.Lock()
	defer td.mu.Unlock()
	td.epoch = epoch
}

// AddShare adds a decryption share from a validator. Returns true when the
// threshold is reached (enough shares collected to attempt decryption).
func (td *ThresholdDecryptor) AddShare(share *DecryptionShare) (bool, error) {
	if share == nil || len(share.ShareBytes) == 0 {
		return false, ErrInvalidShareData
	}
	if share.ValidatorIndex < 0 {
		return false, ErrInvalidValidatorIdx
	}

	td.mu.Lock()
	defer td.mu.Unlock()

	if share.Epoch != td.epoch {
		return false, ErrEpochMismatch
	}

	if _, exists := td.shares[share.ValidatorIndex]; exists {
		return false, ErrShareAlreadyAdded
	}

	td.shares[share.ValidatorIndex] = share
	return len(td.shares) >= td.threshold, nil
}

// TryDecrypt attempts to decrypt the ciphertext if enough shares have been
// collected. It combines the shares into a decryption key and uses it to
// decrypt the AES-GCM ciphertext.
func (td *ThresholdDecryptor) TryDecrypt() ([]byte, error) {
	td.mu.RLock()
	defer td.mu.RUnlock()

	if len(td.shares) < td.threshold {
		return nil, ErrThresholdNotMet
	}
	if td.ciphertext == nil {
		return nil, ErrNoCiphertext
	}

	// Collect shares for key derivation.
	shares := make([]DecryptionShare, 0, len(td.shares))
	for _, s := range td.shares {
		shares = append(shares, *s)
	}

	key := ComputeDecryptionKey(shares)

	// Decrypt with AES-GCM.
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, ErrDecryptFailed
	}

	plaintext, err := aesGCM.Open(nil, td.nonce, td.ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return plaintext, nil
}

// VerifyShare validates a decryption share against a commitment.
// The commitment is a hash of the expected share bytes for the validator.
// Returns true if the share matches the commitment.
func VerifyShare(share *DecryptionShare, commitment []byte) (bool, error) {
	if share == nil || len(share.ShareBytes) == 0 {
		return false, ErrInvalidShareData
	}
	if len(commitment) == 0 {
		return false, ErrInvalidCommitment
	}

	// Hash the share bytes with the validator index as domain separator.
	indexByte := byte(share.ValidatorIndex & 0xFF)
	data := append([]byte{indexByte}, share.ShareBytes...)
	hash := ethcrypto.Keccak256(data)

	if len(hash) != len(commitment) {
		return false, nil
	}
	for i := range hash {
		if hash[i] != commitment[i] {
			return false, nil
		}
	}
	return true, nil
}

// MakeCommitment creates a commitment for a given share that can later be
// used with VerifyShare.
func MakeCommitment(share *DecryptionShare) ([]byte, error) {
	if share == nil || len(share.ShareBytes) == 0 {
		return nil, ErrInvalidShareData
	}
	indexByte := byte(share.ValidatorIndex & 0xFF)
	data := append([]byte{indexByte}, share.ShareBytes...)
	return ethcrypto.Keccak256(data), nil
}

// ResetEpoch clears all collected shares and sets a new epoch.
// This is called at the start of each new epoch to prepare for
// a fresh round of threshold decryption.
func (td *ThresholdDecryptor) ResetEpoch(epoch uint64) {
	td.mu.Lock()
	defer td.mu.Unlock()
	td.epoch = epoch
	td.shares = make(map[int]*DecryptionShare)
	td.ciphertext = nil
	td.nonce = nil
}

// PendingShareCount returns the number of shares currently collected.
func (td *ThresholdDecryptor) PendingShareCount() int {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return len(td.shares)
}

// ThresholdMet returns true if enough shares have been collected to
// attempt decryption.
func (td *ThresholdDecryptor) ThresholdMet() bool {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return len(td.shares) >= td.threshold
}

// Threshold returns the configured threshold (t).
func (td *ThresholdDecryptor) Threshold() int {
	return td.threshold
}

// TotalParts returns the configured total parties (n).
func (td *ThresholdDecryptor) TotalParts() int {
	return td.totalParts
}

// CurrentEpoch returns the current epoch.
func (td *ThresholdDecryptor) CurrentEpoch() uint64 {
	td.mu.RLock()
	defer td.mu.RUnlock()
	return td.epoch
}

// ComputeDecryptionKey combines multiple decryption shares into a single
// 32-byte AES key using Lagrange interpolation over the share indices.
// Each share's bytes are interpreted as a big.Int coefficient, and the
// interpolated value at x=0 (the secret) is hashed with Keccak256 to
// produce a uniform 32-byte key. This mirrors the Shamir SSS reconstruction
// from pkg/crypto/threshold.go.
func ComputeDecryptionKey(shares []DecryptionShare) []byte {
	if len(shares) == 0 {
		return ethcrypto.Keccak256(nil)
	}

	// Convert to crypto.Share format for Lagrange interpolation.
	cryptoShares := make([]threshold.Share, len(shares))
	for i, s := range shares {
		val := new(big.Int).SetBytes(s.ShareBytes)
		// Use 1-based index (Lagrange interpolation requires non-zero indices).
		cryptoShares[i] = threshold.Share{
			Index: s.ValidatorIndex + 1,
			Value: val,
		}
	}

	// Attempt Lagrange interpolation to recover f(0).
	secret, err := threshold.LagrangeInterpolate(cryptoShares)
	if err == nil && secret != nil {
		return ethcrypto.Keccak256(secret.Bytes())
	}

	// Fallback: XOR-based combination if interpolation fails.
	maxLen := 0
	for _, s := range shares {
		if len(s.ShareBytes) > maxLen {
			maxLen = len(s.ShareBytes)
		}
	}
	combined := make([]byte, maxLen)
	for _, s := range shares {
		for j, b := range s.ShareBytes {
			combined[j] ^= b
		}
	}
	return ethcrypto.Keccak256(combined)
}

// SharesForValidators returns the validator indices that have submitted shares.
func (td *ThresholdDecryptor) SharesForValidators() []int {
	td.mu.RLock()
	defer td.mu.RUnlock()
	indices := make([]int, 0, len(td.shares))
	for idx := range td.shares {
		indices = append(indices, idx)
	}
	return indices
}

// RemainingShares returns how many more shares are needed to reach threshold.
// Returns 0 if threshold is already met.
func (td *ThresholdDecryptor) RemainingShares() int {
	td.mu.RLock()
	defer td.mu.RUnlock()
	remaining := td.threshold - len(td.shares)
	if remaining < 0 {
		return 0
	}
	return remaining
}
