package types

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/rlp"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/sha3"
)

var (
	errInvalidSig         = errors.New("invalid transaction signature")
	errInvalidChainID     = errors.New("invalid chain ID for signer")
	errTxTypeNotSupported = errors.New("transaction type not supported by signer")
	errNoRecovery         = errors.New("public key recovery failed")
)

var secp256k1NCopy = gethcrypto.S256().Params().N

// Signer provides methods for hashing transactions and recovering the sender.
type Signer interface {
	// ChainID returns the chain ID this signer operates on.
	ChainID() uint64

	// Hash returns the signing hash for the given transaction.
	Hash(tx *Transaction) Hash

	// SignatureValues parses a 65-byte raw signature [R || S || V] into
	// its component r, s values and a normalized v byte (0 or 1).
	SignatureValues(sig []byte) (r, s *big.Int, v byte, err error)

	// Sender recovers the sender address from the transaction's signature.
	Sender(tx *Transaction) (Address, error)
}

// EIP155Signer implements Signer for legacy EIP-155 replay-protected txs.
type EIP155Signer struct {
	chainID    uint64
	chainIDBig *big.Int
}

// NewEIP155Signer creates a signer for EIP-155 legacy transactions.
func NewEIP155Signer(chainID uint64) EIP155Signer {
	return EIP155Signer{
		chainID:    chainID,
		chainIDBig: new(big.Int).SetUint64(chainID),
	}
}

// ChainID returns the chain ID.
func (s EIP155Signer) ChainID() uint64 { return s.chainID }

// Hash returns the signing hash for a legacy transaction.
func (s EIP155Signer) Hash(tx *Transaction) Hash {
	if tx.Type() != LegacyTxType {
		return Hash{}
	}
	return tx.SigningHash()
}

// SignatureValues parses a 65-byte [R||S||V] signature.
func (s EIP155Signer) SignatureValues(sig []byte) (r, s2 *big.Int, v byte, err error) {
	return parseSignatureValues(sig)
}

// Sender recovers the sender address from a legacy transaction's signature.
func (s EIP155Signer) Sender(tx *Transaction) (Address, error) {
	if tx.Type() != LegacyTxType {
		return Address{}, errTxTypeNotSupported
	}
	v, r, rs := tx.RawSignatureValues()
	if v == nil || r == nil || rs == nil {
		return Address{}, errInvalidSig
	}

	var recovery byte
	vVal := v.Uint64()
	if vVal == 27 || vVal == 28 {
		recovery = byte(vVal - 27)
	} else {
		// EIP-155: V = chainID*2 + 35 + recoveryID
		recovery = byte(vVal - 35 - 2*s.chainID)
	}
	if recovery > 1 {
		return Address{}, errInvalidSig
	}

	sigHash := tx.SigningHash()
	return RecoverPlain(sigHash, r, rs, recovery)
}

// LondonSigner implements Signer for EIP-1559 dynamic fee transactions
// and also supports legacy, access-list, blob, and set-code txs.
type LondonSigner struct {
	chainID    uint64
	chainIDBig *big.Int
}

// NewLondonSigner creates a signer that supports all tx types.
func NewLondonSigner(chainID uint64) LondonSigner {
	return LondonSigner{
		chainID:    chainID,
		chainIDBig: new(big.Int).SetUint64(chainID),
	}
}

// ChainID returns the chain ID.
func (s LondonSigner) ChainID() uint64 { return s.chainID }

// Hash returns the signing hash for the given transaction.
func (s LondonSigner) Hash(tx *Transaction) Hash {
	return tx.SigningHash()
}

// SignatureValues parses a 65-byte [R||S||V] signature.
func (s LondonSigner) SignatureValues(sig []byte) (r, s2 *big.Int, v byte, err error) {
	return parseSignatureValues(sig)
}

// Sender recovers the sender address from the transaction's signature.
func (s LondonSigner) Sender(tx *Transaction) (Address, error) {
	v, r, rs := tx.RawSignatureValues()
	if r == nil || rs == nil {
		return Address{}, errInvalidSig
	}

	var recovery byte
	switch tx.Type() {
	case LegacyTxType:
		if v == nil {
			return Address{}, errInvalidSig
		}
		vVal := v.Uint64()
		if vVal == 27 || vVal == 28 {
			recovery = byte(vVal - 27)
		} else {
			recovery = byte(vVal - 35 - 2*s.chainID)
		}
	case AccessListTxType, DynamicFeeTxType, BlobTxType, SetCodeTxType:
		if v == nil {
			recovery = 0
		} else {
			recovery = byte(v.Uint64())
		}
		txChainID := tx.ChainId()
		if txChainID != nil && txChainID.Uint64() != s.chainID {
			return Address{}, errInvalidChainID
		}
	default:
		return Address{}, errTxTypeNotSupported
	}

	if recovery > 1 {
		return Address{}, errInvalidSig
	}

	sigHash := tx.SigningHash()
	return RecoverPlain(sigHash, r, rs, recovery)
}

// LatestSigner returns the most feature-complete signer for the given chain ID.
func LatestSigner(chainID uint64) Signer {
	return NewLondonSigner(chainID)
}

// MakeSigner returns the appropriate signer for a given chain ID and tx type.
func MakeSigner(chainID uint64, txType uint8) Signer {
	switch txType {
	case LegacyTxType:
		return NewEIP155Signer(chainID)
	default:
		return NewLondonSigner(chainID)
	}
}

// SigningHash computes the EIP-155 signing hash for a legacy transaction
// from its individual fields:
// Keccak256(RLP([nonce, gasPrice, gas, to, value, data, chainID, 0, 0]))
func SigningHash(chainID uint64, nonce uint64, to *Address, value *big.Int,
	gas uint64, data []byte) Hash {

	toBytes := make([]byte, 0)
	if to != nil {
		toBytes = to[:]
	}

	var items [][]byte
	enc := func(v interface{}) {
		b, _ := rlp.EncodeToBytes(v)
		items = append(items, b)
	}

	enc(nonce)
	enc(bigOrZero(value))
	enc(gas)
	enc(toBytes)
	enc(bigOrZero(value))
	enc(data)

	chainBig := new(big.Int).SetUint64(chainID)
	if chainBig.Sign() > 0 {
		enc(chainBig)
		enc(uint(0))
		enc(uint(0))
	}

	var payload []byte
	for _, item := range items {
		payload = append(payload, item...)
	}
	encoded := rlp.WrapList(payload)

	d := sha3.NewLegacyKeccak256()
	d.Write(encoded)
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// RecoverPlain recovers the sender address from an ECDSA signature.
// sighash is the 32-byte message hash, r and s are the signature values,
// and v is the recovery ID (0 or 1).
func RecoverPlain(sighash Hash, r, s *big.Int, v byte) (Address, error) {
	if r == nil || s == nil || v > 1 {
		return Address{}, errInvalidSig
	}
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return Address{}, errInvalidSig
	}
	if r.Cmp(secp256k1NCopy) >= 0 || s.Cmp(secp256k1NCopy) >= 0 {
		return Address{}, errInvalidSig
	}

	pub, err := recoverPubkey(sighash[:], r, s, v)
	if err != nil {
		return Address{}, err
	}

	// Address = Keccak256(pub[1:])[12:] where pub is 65-byte uncompressed.
	d := sha3.NewLegacyKeccak256()
	d.Write(pub[1:])
	hash := d.Sum(nil)
	return BytesToAddress(hash[12:]), nil
}

// parseSignatureValues validates and parses a 65-byte [R||S||V] signature.
func parseSignatureValues(sig []byte) (*big.Int, *big.Int, byte, error) {
	if len(sig) != 65 {
		return nil, nil, 0, errInvalidSig
	}
	r := new(big.Int).SetBytes(sig[0:32])
	s := new(big.Int).SetBytes(sig[32:64])
	v := sig[64]
	if v > 1 {
		return nil, nil, 0, errInvalidSig
	}
	if r.Sign() <= 0 || s.Sign() <= 0 {
		return nil, nil, 0, errInvalidSig
	}
	if r.Cmp(secp256k1NCopy) >= 0 || s.Cmp(secp256k1NCopy) >= 0 {
		return nil, nil, 0, errInvalidSig
	}
	return r, s, v, nil
}

// recoverPubkey adapts transaction signature components to go-ethereum's API.
func recoverPubkey(hash []byte, r, s *big.Int, v byte) ([]byte, error) {
	if r == nil || s == nil || !gethcrypto.ValidateSignatureValues(v, r, s, false) {
		return nil, errInvalidSig
	}
	var signature [gethcrypto.SignatureLength]byte
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:64])
	signature[gethcrypto.RecoveryIDOffset] = v
	pub, err := gethcrypto.Ecrecover(hash, signature[:])
	if err != nil {
		return nil, errNoRecovery
	}
	return pub, nil
}

// Ensure EIP155Signer and LondonSigner satisfy the Signer interface.
var (
	_ Signer = EIP155Signer{}
	_ Signer = LondonSigner{}
)
