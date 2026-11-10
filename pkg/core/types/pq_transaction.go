package types

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/cloudflare/circl/sign/mldsa/mldsa65"
	"github.com/eth2030/eth2030/rlp"
	"golang.org/x/crypto/sha3"
)

// PQTransactionType is the EIP-2718 type byte for post-quantum transactions.
const PQTransactionType byte = 0x07

// PQ signature algorithm identifiers.
const (
	PQSigDilithium uint8 = 0 // CRYSTALS-Dilithium
	PQSigFalcon    uint8 = 1 // Falcon
	PQSigSPHINCS   uint8 = 2 // SPHINCS+
)

// Packed public key and signature sizes for the selected PQ providers.
const (
	DilithiumPubKeySize   = mldsa65.PublicKeySize
	DilithiumSigSize      = mldsa65.SignatureSize
	FalconPubKeySize      = 897
	FalconSigSize         = 666
	SPHINCSPlusPubKeySize = 32
	SPHINCSPlusSigSize    = 17088
)

// PQ transaction errors.
var (
	ErrPQTxDecode     = errors.New("pq tx: decode failed")
	ErrPQTxTypePrefix = errors.New("pq tx: invalid type prefix")
	ErrPQTxShortData  = errors.New("pq tx: data too short")
)

// PQTransaction represents a post-quantum resistant transaction (type 0x07).
// It extends the standard transaction fields with PQ signature data, enabling
// quantum-safe transaction authentication.
type PQTransaction struct {
	ChainID  *big.Int
	Nonce    uint64
	To       *Address
	Value    *big.Int
	Gas      uint64
	GasPrice *big.Int
	Data     []byte

	// PQ signature fields.
	PQSignatureType  uint8 // 0=Dilithium, 1=Falcon, 2=SPHINCS+
	PQSignature      []byte
	PQPublicKey      []byte
	ClassicSignature []byte // optional ECDSA fallback
}

// pqTxRLP is the RLP encoding layout for PQTransaction.
type pqTxRLP struct {
	ChainID          *big.Int
	Nonce            uint64
	To               []byte
	Value            *big.Int
	Gas              uint64
	GasPrice         *big.Int
	Data             []byte
	PQSignatureType  uint8
	PQSignature      []byte
	PQPublicKey      []byte
	ClassicSignature []byte
}

// NewPQTransaction creates a new post-quantum transaction with the given fields.
func NewPQTransaction(chainID *big.Int, nonce uint64, to *Address, value *big.Int, gas uint64, gasPrice *big.Int, data []byte) *PQTransaction {
	tx := &PQTransaction{
		ChainID:  new(big.Int),
		Nonce:    nonce,
		To:       copyAddressPtr(to),
		Value:    new(big.Int),
		Gas:      gas,
		GasPrice: new(big.Int),
		Data:     copyBytes(data),
	}
	if chainID != nil {
		tx.ChainID.Set(chainID)
	}
	if value != nil {
		tx.Value.Set(value)
	}
	if gasPrice != nil {
		tx.GasPrice.Set(gasPrice)
	}
	return tx
}

// SignWithPQ attaches a post-quantum signature to the transaction.
func (tx *PQTransaction) SignWithPQ(sigType uint8, pubKey, signature []byte) {
	tx.PQSignatureType = sigType
	tx.PQPublicKey = make([]byte, len(pubKey))
	copy(tx.PQPublicKey, pubKey)
	tx.PQSignature = make([]byte, len(signature))
	copy(tx.PQSignature, signature)
}

// Hash computes the Keccak-256 hash of the transaction.
func (tx *PQTransaction) Hash() Hash {
	d := sha3.NewLegacyKeccak256()

	// Hash all fields in deterministic order.
	if tx.ChainID != nil {
		d.Write(tx.ChainID.Bytes())
	}
	var nonceBuf [8]byte
	binary.BigEndian.PutUint64(nonceBuf[:], tx.Nonce)
	d.Write(nonceBuf[:])

	if tx.To != nil {
		d.Write(tx.To[:])
	}
	if tx.Value != nil {
		d.Write(tx.Value.Bytes())
	}
	var gasBuf [8]byte
	binary.BigEndian.PutUint64(gasBuf[:], tx.Gas)
	d.Write(gasBuf[:])

	if tx.GasPrice != nil {
		d.Write(tx.GasPrice.Bytes())
	}
	d.Write(tx.Data)
	d.Write([]byte{tx.PQSignatureType})
	d.Write(tx.PQSignature)
	d.Write(tx.PQPublicKey)
	d.Write(tx.ClassicSignature)

	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// Type returns the PQ transaction type byte.
func (tx *PQTransaction) Type() byte {
	return PQTransactionType
}

// EncodePQ encodes the transaction as type_byte || RLP(fields).
func (tx *PQTransaction) EncodePQ() ([]byte, error) {
	enc := pqTxRLP{
		ChainID:          bigOrZero(tx.ChainID),
		Nonce:            tx.Nonce,
		To:               addressPtrToBytes(tx.To),
		Value:            bigOrZero(tx.Value),
		Gas:              tx.Gas,
		GasPrice:         bigOrZero(tx.GasPrice),
		Data:             tx.Data,
		PQSignatureType:  tx.PQSignatureType,
		PQSignature:      tx.PQSignature,
		PQPublicKey:      tx.PQPublicKey,
		ClassicSignature: tx.ClassicSignature,
	}
	payload, err := rlp.EncodeToBytes(enc)
	if err != nil {
		return nil, err
	}
	result := make([]byte, 1+len(payload))
	result[0] = PQTransactionType
	copy(result[1:], payload)
	return result, nil
}

// DecodePQTransaction decodes a PQ transaction from type_byte || RLP(fields).
func DecodePQTransaction(data []byte) (*PQTransaction, error) {
	if len(data) < 2 {
		return nil, ErrPQTxShortData
	}
	if data[0] != PQTransactionType {
		return nil, ErrPQTxTypePrefix
	}
	var dec pqTxRLP
	if err := rlp.DecodeBytes(data[1:], &dec); err != nil {
		return nil, ErrPQTxDecode
	}
	tx := &PQTransaction{
		ChainID:          dec.ChainID,
		Nonce:            dec.Nonce,
		To:               bytesToAddressPtr(dec.To),
		Value:            dec.Value,
		Gas:              dec.Gas,
		GasPrice:         dec.GasPrice,
		Data:             dec.Data,
		PQSignatureType:  dec.PQSignatureType,
		PQSignature:      dec.PQSignature,
		PQPublicKey:      dec.PQPublicKey,
		ClassicSignature: dec.ClassicSignature,
	}
	return tx, nil
}

// VerifyPQSignature checks that the PQ signature is well-formed: non-empty
// and the correct size for the declared signature type.
func (tx *PQTransaction) VerifyPQSignature() bool {
	if len(tx.PQSignature) == 0 || len(tx.PQPublicKey) == 0 {
		return false
	}
	switch tx.PQSignatureType {
	case PQSigDilithium:
		return len(tx.PQSignature) == DilithiumSigSize
	case PQSigFalcon:
		return len(tx.PQSignature) == FalconSigSize
	case PQSigSPHINCS:
		return len(tx.PQSignature) == SPHINCSPlusSigSize
	default:
		return false
	}
}

// TxData interface implementation for PQTransaction.
func (pq *PQTransaction) txType() byte           { return PQTransactionType }
func (pq *PQTransaction) chainID() *big.Int      { return pq.ChainID }
func (pq *PQTransaction) accessList() AccessList { return nil }
func (pq *PQTransaction) data() []byte           { return pq.Data }
func (pq *PQTransaction) gas() uint64            { return pq.Gas }
func (pq *PQTransaction) gasPrice() *big.Int     { return pq.GasPrice }
func (pq *PQTransaction) gasTipCap() *big.Int    { return pq.GasPrice }
func (pq *PQTransaction) gasFeeCap() *big.Int    { return pq.GasPrice }
func (pq *PQTransaction) value() *big.Int        { return pq.Value }
func (pq *PQTransaction) nonce() uint64          { return pq.Nonce }
func (pq *PQTransaction) to() *Address           { return pq.To }

func (pq *PQTransaction) copy() TxData {
	cpy := &PQTransaction{
		Nonce:           pq.Nonce,
		Gas:             pq.Gas,
		PQSignatureType: pq.PQSignatureType,
	}
	if pq.ChainID != nil {
		cpy.ChainID = new(big.Int).Set(pq.ChainID)
	}
	cpy.To = copyAddressPtr(pq.To)
	if pq.Value != nil {
		cpy.Value = new(big.Int).Set(pq.Value)
	}
	if pq.GasPrice != nil {
		cpy.GasPrice = new(big.Int).Set(pq.GasPrice)
	}
	cpy.Data = copyBytes(pq.Data)
	cpy.PQSignature = copyBytes(pq.PQSignature)
	cpy.PQPublicKey = copyBytes(pq.PQPublicKey)
	cpy.ClassicSignature = copyBytes(pq.ClassicSignature)
	return cpy
}
