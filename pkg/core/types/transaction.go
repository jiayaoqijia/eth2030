package types

import (
	"math/big"
	"sync/atomic"
	"unsafe"
)

// Transaction type constants.
const (
	LegacyTxType     = 0x00
	AccessListTxType = 0x01
	DynamicFeeTxType = 0x02
	BlobTxType       = 0x03
	SetCodeTxType    = 0x04
	// FrameTxType is defined in tx_frame.go as 0x06.
	// PQTransactionType is defined in pq_transaction.go as 0x07.
)

// BlobSidecarData holds the raw blob data, KZG commitments, and proofs for a
// blob transaction (EIP-4844). Stored out-of-band alongside the tx so it does
// not affect the signing hash or on-chain RLP encoding.
type BlobSidecarData struct {
	Blobs       [][]byte
	Commitments [][]byte
	Proofs      [][]byte
}

// Transaction represents an Ethereum transaction.
type Transaction struct {
	inner   TxData
	hash    atomic.Pointer[Hash]
	size    atomic.Uint64
	from    atomic.Pointer[Address] // cached sender address
	sidecar *BlobSidecarData        // EIP-4844: blob sidecar (nil for non-blob txs)
}

// SetBlobSidecar attaches KZG blob sidecar data to the transaction.
// Only meaningful for blob transactions (type 0x03).
func (tx *Transaction) SetBlobSidecar(s *BlobSidecarData) { tx.sidecar = s }

// BlobSidecar returns the attached blob sidecar, or nil if absent.
func (tx *Transaction) BlobSidecar() *BlobSidecarData { return tx.sidecar }

// SetSender caches the sender address on the transaction.
func (tx *Transaction) SetSender(addr Address) {
	a := addr
	tx.from.Store(&a)
}

// Sender returns the cached sender address, or nil if not yet set.
func (tx *Transaction) Sender() *Address {
	return tx.from.Load()
}

// TxData is the underlying data of a transaction.
type TxData interface {
	txType() byte
	chainID() *big.Int
	accessList() AccessList
	data() []byte
	gas() uint64
	gasPrice() *big.Int
	gasTipCap() *big.Int
	gasFeeCap() *big.Int
	value() *big.Int
	nonce() uint64
	to() *Address

	copy() TxData
}

// AccessList is a list of address-slot pairs accessed by a transaction.
type AccessList []AccessTuple

// AccessTuple is a single address and its accessed storage slots.
type AccessTuple struct {
	Address     Address
	StorageKeys []Hash
}

// Authorization is an EIP-7702 authorization entry for SetCodeTx.
type Authorization struct {
	ChainID *big.Int
	Address Address
	Nonce   uint64
	V       *big.Int
	R       *big.Int
	S       *big.Int
}

// LegacyTx represents a legacy (type 0x00) Ethereum transaction.
type LegacyTx struct {
	Nonce    uint64
	GasPrice *big.Int
	Gas      uint64
	To       *Address
	Value    *big.Int
	Data     []byte
	V, R, S  *big.Int
}

func (tx *LegacyTx) txType() byte           { return LegacyTxType }
func (tx *LegacyTx) chainID() *big.Int      { return deriveChainID(tx.V) }
func (tx *LegacyTx) accessList() AccessList { return nil }
func (tx *LegacyTx) data() []byte           { return tx.Data }
func (tx *LegacyTx) gas() uint64            { return tx.Gas }
func (tx *LegacyTx) gasPrice() *big.Int     { return tx.GasPrice }
func (tx *LegacyTx) gasTipCap() *big.Int    { return tx.GasPrice }
func (tx *LegacyTx) gasFeeCap() *big.Int    { return tx.GasPrice }
func (tx *LegacyTx) value() *big.Int        { return tx.Value }
func (tx *LegacyTx) nonce() uint64          { return tx.Nonce }
func (tx *LegacyTx) to() *Address           { return tx.To }
func (tx *LegacyTx) copy() TxData {
	cpy := &LegacyTx{
		Nonce: tx.Nonce,
		Gas:   tx.Gas,
		To:    copyAddressPtr(tx.To),
		Data:  copyBytes(tx.Data),
	}
	if tx.GasPrice != nil {
		cpy.GasPrice = new(big.Int).Set(tx.GasPrice)
	}
	if tx.Value != nil {
		cpy.Value = new(big.Int).Set(tx.Value)
	}
	if tx.V != nil {
		cpy.V = new(big.Int).Set(tx.V)
	}
	if tx.R != nil {
		cpy.R = new(big.Int).Set(tx.R)
	}
	if tx.S != nil {
		cpy.S = new(big.Int).Set(tx.S)
	}
	return cpy
}

// AccessListTx represents an EIP-2930 (type 0x01) transaction.
type AccessListTx struct {
	ChainID    *big.Int
	Nonce      uint64
	GasPrice   *big.Int
	Gas        uint64
	To         *Address
	Value      *big.Int
	Data       []byte
	AccessList AccessList
	V, R, S    *big.Int
}

func (tx *AccessListTx) txType() byte           { return AccessListTxType }
func (tx *AccessListTx) chainID() *big.Int      { return tx.ChainID }
func (tx *AccessListTx) accessList() AccessList { return tx.AccessList }
func (tx *AccessListTx) data() []byte           { return tx.Data }
func (tx *AccessListTx) gas() uint64            { return tx.Gas }
func (tx *AccessListTx) gasPrice() *big.Int     { return tx.GasPrice }
func (tx *AccessListTx) gasTipCap() *big.Int    { return tx.GasPrice }
func (tx *AccessListTx) gasFeeCap() *big.Int    { return tx.GasPrice }
func (tx *AccessListTx) value() *big.Int        { return tx.Value }
func (tx *AccessListTx) nonce() uint64          { return tx.Nonce }
func (tx *AccessListTx) to() *Address           { return tx.To }
func (tx *AccessListTx) copy() TxData {
	cpy := &AccessListTx{
		Nonce: tx.Nonce,
		Gas:   tx.Gas,
		To:    copyAddressPtr(tx.To),
		Data:  copyBytes(tx.Data),
	}
	if tx.ChainID != nil {
		cpy.ChainID = new(big.Int).Set(tx.ChainID)
	}
	if tx.GasPrice != nil {
		cpy.GasPrice = new(big.Int).Set(tx.GasPrice)
	}
	if tx.Value != nil {
		cpy.Value = new(big.Int).Set(tx.Value)
	}
	if tx.AccessList != nil {
		cpy.AccessList = copyAccessList(tx.AccessList)
	}
	if tx.V != nil {
		cpy.V = new(big.Int).Set(tx.V)
	}
	if tx.R != nil {
		cpy.R = new(big.Int).Set(tx.R)
	}
	if tx.S != nil {
		cpy.S = new(big.Int).Set(tx.S)
	}
	return cpy
}

// DynamicFeeTx represents an EIP-1559 (type 0x02) transaction.
type DynamicFeeTx struct {
	ChainID    *big.Int
	Nonce      uint64
	GasTipCap  *big.Int // maxPriorityFeePerGas
	GasFeeCap  *big.Int // maxFeePerGas
	Gas        uint64
	To         *Address
	Value      *big.Int
	Data       []byte
	AccessList AccessList
	V, R, S    *big.Int
}

func (tx *DynamicFeeTx) txType() byte           { return DynamicFeeTxType }
func (tx *DynamicFeeTx) chainID() *big.Int      { return tx.ChainID }
func (tx *DynamicFeeTx) accessList() AccessList { return tx.AccessList }
func (tx *DynamicFeeTx) data() []byte           { return tx.Data }
func (tx *DynamicFeeTx) gas() uint64            { return tx.Gas }
func (tx *DynamicFeeTx) gasPrice() *big.Int     { return tx.GasFeeCap }
func (tx *DynamicFeeTx) gasTipCap() *big.Int    { return tx.GasTipCap }
func (tx *DynamicFeeTx) gasFeeCap() *big.Int    { return tx.GasFeeCap }
func (tx *DynamicFeeTx) value() *big.Int        { return tx.Value }
func (tx *DynamicFeeTx) nonce() uint64          { return tx.Nonce }
func (tx *DynamicFeeTx) to() *Address           { return tx.To }
func (tx *DynamicFeeTx) copy() TxData {
	cpy := &DynamicFeeTx{
		Nonce: tx.Nonce,
		Gas:   tx.Gas,
		To:    copyAddressPtr(tx.To),
		Data:  copyBytes(tx.Data),
	}
	if tx.ChainID != nil {
		cpy.ChainID = new(big.Int).Set(tx.ChainID)
	}
	if tx.GasTipCap != nil {
		cpy.GasTipCap = new(big.Int).Set(tx.GasTipCap)
	}
	if tx.GasFeeCap != nil {
		cpy.GasFeeCap = new(big.Int).Set(tx.GasFeeCap)
	}
	if tx.Value != nil {
		cpy.Value = new(big.Int).Set(tx.Value)
	}
	if tx.AccessList != nil {
		cpy.AccessList = copyAccessList(tx.AccessList)
	}
	if tx.V != nil {
		cpy.V = new(big.Int).Set(tx.V)
	}
	if tx.R != nil {
		cpy.R = new(big.Int).Set(tx.R)
	}
	if tx.S != nil {
		cpy.S = new(big.Int).Set(tx.S)
	}
	return cpy
}

// BlobTx represents an EIP-4844 (type 0x03) blob transaction.
type BlobTx struct {
	ChainID    *big.Int
	Nonce      uint64
	GasTipCap  *big.Int
	GasFeeCap  *big.Int
	Gas        uint64
	To         Address
	Value      *big.Int
	Data       []byte
	AccessList AccessList
	BlobFeeCap *big.Int
	BlobHashes []Hash
	V, R, S    *big.Int
}

func (tx *BlobTx) txType() byte           { return BlobTxType }
func (tx *BlobTx) chainID() *big.Int      { return tx.ChainID }
func (tx *BlobTx) accessList() AccessList { return tx.AccessList }
func (tx *BlobTx) data() []byte           { return tx.Data }
func (tx *BlobTx) gas() uint64            { return tx.Gas }
func (tx *BlobTx) gasPrice() *big.Int     { return tx.GasFeeCap }
func (tx *BlobTx) gasTipCap() *big.Int    { return tx.GasTipCap }
func (tx *BlobTx) gasFeeCap() *big.Int    { return tx.GasFeeCap }
func (tx *BlobTx) value() *big.Int        { return tx.Value }
func (tx *BlobTx) nonce() uint64          { return tx.Nonce }
func (tx *BlobTx) to() *Address           { addr := tx.To; return &addr }
func (tx *BlobTx) copy() TxData {
	cpy := &BlobTx{
		Nonce: tx.Nonce,
		Gas:   tx.Gas,
		To:    tx.To,
		Data:  copyBytes(tx.Data),
	}
	if tx.ChainID != nil {
		cpy.ChainID = new(big.Int).Set(tx.ChainID)
	}
	if tx.GasTipCap != nil {
		cpy.GasTipCap = new(big.Int).Set(tx.GasTipCap)
	}
	if tx.GasFeeCap != nil {
		cpy.GasFeeCap = new(big.Int).Set(tx.GasFeeCap)
	}
	if tx.Value != nil {
		cpy.Value = new(big.Int).Set(tx.Value)
	}
	if tx.BlobFeeCap != nil {
		cpy.BlobFeeCap = new(big.Int).Set(tx.BlobFeeCap)
	}
	if tx.AccessList != nil {
		cpy.AccessList = copyAccessList(tx.AccessList)
	}
	if tx.BlobHashes != nil {
		cpy.BlobHashes = make([]Hash, len(tx.BlobHashes))
		copy(cpy.BlobHashes, tx.BlobHashes)
	}
	if tx.V != nil {
		cpy.V = new(big.Int).Set(tx.V)
	}
	if tx.R != nil {
		cpy.R = new(big.Int).Set(tx.R)
	}
	if tx.S != nil {
		cpy.S = new(big.Int).Set(tx.S)
	}
	return cpy
}

// SetCodeTx represents an EIP-7702 (type 0x04) set-code transaction.
type SetCodeTx struct {
	ChainID           *big.Int
	Nonce             uint64
	GasTipCap         *big.Int
	GasFeeCap         *big.Int
	Gas               uint64
	To                Address
	Value             *big.Int
	Data              []byte
	AccessList        AccessList
	AuthorizationList []Authorization
	V, R, S           *big.Int
}

func (tx *SetCodeTx) txType() byte           { return SetCodeTxType }
func (tx *SetCodeTx) chainID() *big.Int      { return tx.ChainID }
func (tx *SetCodeTx) accessList() AccessList { return tx.AccessList }
func (tx *SetCodeTx) data() []byte           { return tx.Data }
func (tx *SetCodeTx) gas() uint64            { return tx.Gas }
func (tx *SetCodeTx) gasPrice() *big.Int     { return tx.GasFeeCap }
func (tx *SetCodeTx) gasTipCap() *big.Int    { return tx.GasTipCap }
func (tx *SetCodeTx) gasFeeCap() *big.Int    { return tx.GasFeeCap }
func (tx *SetCodeTx) value() *big.Int        { return tx.Value }
func (tx *SetCodeTx) nonce() uint64          { return tx.Nonce }
func (tx *SetCodeTx) to() *Address           { addr := tx.To; return &addr }
func (tx *SetCodeTx) copy() TxData {
	cpy := &SetCodeTx{
		Nonce: tx.Nonce,
		Gas:   tx.Gas,
		To:    tx.To,
		Data:  copyBytes(tx.Data),
	}
	if tx.ChainID != nil {
		cpy.ChainID = new(big.Int).Set(tx.ChainID)
	}
	if tx.GasTipCap != nil {
		cpy.GasTipCap = new(big.Int).Set(tx.GasTipCap)
	}
	if tx.GasFeeCap != nil {
		cpy.GasFeeCap = new(big.Int).Set(tx.GasFeeCap)
	}
	if tx.Value != nil {
		cpy.Value = new(big.Int).Set(tx.Value)
	}
	if tx.AccessList != nil {
		cpy.AccessList = copyAccessList(tx.AccessList)
	}
	if tx.AuthorizationList != nil {
		cpy.AuthorizationList = make([]Authorization, len(tx.AuthorizationList))
		for i, auth := range tx.AuthorizationList {
			cpy.AuthorizationList[i] = Authorization{
				Address: auth.Address,
				Nonce:   auth.Nonce,
			}
			if auth.ChainID != nil {
				cpy.AuthorizationList[i].ChainID = new(big.Int).Set(auth.ChainID)
			}
			if auth.V != nil {
				cpy.AuthorizationList[i].V = new(big.Int).Set(auth.V)
			}
			if auth.R != nil {
				cpy.AuthorizationList[i].R = new(big.Int).Set(auth.R)
			}
			if auth.S != nil {
				cpy.AuthorizationList[i].S = new(big.Int).Set(auth.S)
			}
		}
	}
	if tx.V != nil {
		cpy.V = new(big.Int).Set(tx.V)
	}
	if tx.R != nil {
		cpy.R = new(big.Int).Set(tx.R)
	}
	if tx.S != nil {
		cpy.S = new(big.Int).Set(tx.S)
	}
	return cpy
}

// NewTransaction creates a new transaction with the given inner data.
func NewTransaction(inner TxData) *Transaction {
	tx := &Transaction{inner: inner.copy()}
	return tx
}

// Type returns the transaction type.
func (tx *Transaction) Type() uint8 { return tx.inner.txType() }

// Inner returns the underlying typed transaction data.
func (tx *Transaction) Inner() TxData { return tx.inner }

// ChainId returns the chain ID of the transaction.
func (tx *Transaction) ChainId() *big.Int { return tx.inner.chainID() }

// AccessList returns the access list of the transaction.
func (tx *Transaction) AccessList() AccessList { return tx.inner.accessList() }

// Data returns the input data of the transaction.
func (tx *Transaction) Data() []byte { return tx.inner.data() }

// Gas returns the gas limit of the transaction.
func (tx *Transaction) Gas() uint64 { return tx.inner.gas() }

// GasPrice returns the gas price of the transaction.
func (tx *Transaction) GasPrice() *big.Int { return tx.inner.gasPrice() }

// GasTipCap returns the gas tip cap (maxPriorityFeePerGas) of the transaction.
func (tx *Transaction) GasTipCap() *big.Int { return tx.inner.gasTipCap() }

// GasFeeCap returns the gas fee cap (maxFeePerGas) of the transaction.
func (tx *Transaction) GasFeeCap() *big.Int { return tx.inner.gasFeeCap() }

// Value returns the value transfer amount of the transaction.
func (tx *Transaction) Value() *big.Int { return tx.inner.value() }

// Nonce returns the nonce of the transaction.
func (tx *Transaction) Nonce() uint64 { return tx.inner.nonce() }

// To returns the recipient address, or nil for contract creation.
func (tx *Transaction) To() *Address { return tx.inner.to() }

// AuthorizationList returns the authorization list for EIP-7702 SetCode transactions.
// Returns nil for all other transaction types.
func (tx *Transaction) AuthorizationList() []Authorization {
	if setCode, ok := tx.inner.(*SetCodeTx); ok {
		return setCode.AuthorizationList
	}
	return nil
}

// BlobGasFeeCap returns the blob gas fee cap for EIP-4844 blob transactions.
func (tx *Transaction) BlobGasFeeCap() *big.Int {
	if blob, ok := tx.inner.(*BlobTx); ok {
		return blob.BlobFeeCap
	}
	return nil
}

// BlobHashes returns the versioned hashes for EIP-4844 blob transactions.
func (tx *Transaction) BlobHashes() []Hash {
	if blob, ok := tx.inner.(*BlobTx); ok {
		return blob.BlobHashes
	}
	return nil
}

// Frames returns the frame list for EIP-8141 frame transactions.
// Returns nil for all other transaction types.
func (tx *Transaction) Frames() []Frame {
	if ftx, ok := tx.inner.(*FrameTx); ok {
		return ftx.Frames
	}
	return nil
}

// PQPublicKey returns the post-quantum public key for type-0x07 transactions.
// Returns nil for all other transaction types.
func (tx *Transaction) PQPublicKey() []byte {
	if pq, ok := tx.inner.(*PQTransaction); ok {
		return pq.PQPublicKey
	}
	return nil
}

// PQSigType returns the PQ signature algorithm identifier for type-0x07 transactions.
// Returns 0 for all other transaction types.
func (tx *Transaction) PQSigType() uint8 {
	if pq, ok := tx.inner.(*PQTransaction); ok {
		return pq.PQSignatureType
	}
	return 0
}

// FrameSender returns the sender address for EIP-8141 frame transactions.
// Returns the zero address for all other transaction types.
func (tx *Transaction) FrameSender() Address {
	if ftx, ok := tx.inner.(*FrameTx); ok {
		return ftx.Sender
	}
	return Address{}
}

// BlobGas returns the blob gas used by an EIP-4844 blob transaction.
// Each blob uses 131072 gas (2^17).
func (tx *Transaction) BlobGas() uint64 {
	if blob, ok := tx.inner.(*BlobTx); ok {
		return uint64(len(blob.BlobHashes)) * 131072
	}
	return 0
}

// CalldataGas returns the EIP-7706 calldata gas for this transaction.
// tokens = zero_bytes * 1 + nonzero_bytes * TOKENS_PER_NONZERO_BYTE
// calldata_gas = tokens * CALLDATA_GAS_PER_TOKEN
func (tx *Transaction) CalldataGas() uint64 {
	return CalldataTokenGas(tx.inner.data())
}

// CalldataTokenGas computes the EIP-7706 calldata gas for raw data bytes.
// tokens = zero_bytes + nonzero_bytes * 4
// gas = tokens * 4
func CalldataTokenGas(data []byte) uint64 {
	var tokens uint64
	for _, b := range data {
		if b == 0 {
			tokens++
		} else {
			tokens += CalldataTokensPerNonZeroByte
		}
	}
	return tokens * CalldataGasPerToken
}

// EIP-7706 calldata gas constants.
const (
	// CalldataGasPerToken is the gas cost per calldata token.
	CalldataGasPerToken uint64 = 4
	// CalldataTokensPerNonZeroByte is the number of tokens per nonzero calldata byte.
	CalldataTokensPerNonZeroByte uint64 = 4
	// CalldataGasLimitRatio is the divisor for deriving calldata gas limit from execution gas limit.
	CalldataGasLimitRatio uint64 = 4
)

// RawSignatureValues returns the V, R, S signature values of the transaction.
func (tx *Transaction) RawSignatureValues() (v, r, s *big.Int) {
	switch t := tx.inner.(type) {
	case *LegacyTx:
		return t.V, t.R, t.S
	case *AccessListTx:
		return t.V, t.R, t.S
	case *DynamicFeeTx:
		return t.V, t.R, t.S
	case *BlobTx:
		return t.V, t.R, t.S
	case *SetCodeTx:
		return t.V, t.R, t.S
	case *MultiDimFeeTx:
		return t.rawSignatureValues()
	case *FrameTx:
		// Frame transactions have no traditional signature fields.
		return nil, nil, nil
	default:
		return nil, nil, nil
	}
}

// Hash returns the transaction hash (Keccak-256 of RLP encoding), caching on first call.
func (tx *Transaction) Hash() Hash {
	if h := tx.hash.Load(); h != nil {
		return *h
	}
	h := tx.hashRLP()
	tx.hash.Store(&h)
	return h
}

// Size returns the approximate memory footprint of the transaction.
func (tx *Transaction) Size() uint64 {
	if cached := tx.size.Load(); cached != 0 {
		return cached
	}
	size := uint64(unsafe.Sizeof(*tx))
	tx.size.Store(size)
	return size
}

// Helpers

func copyAddressPtr(a *Address) *Address {
	if a == nil {
		return nil
	}
	cpy := *a
	return &cpy
}

func copyBytes(b []byte) []byte {
	if b == nil {
		return nil
	}
	cpy := make([]byte, len(b))
	copy(cpy, b)
	return cpy
}

func copyAccessList(al AccessList) AccessList {
	if al == nil {
		return nil
	}
	cpy := make(AccessList, len(al))
	for i, tuple := range al {
		cpy[i] = AccessTuple{
			Address:     tuple.Address,
			StorageKeys: make([]Hash, len(tuple.StorageKeys)),
		}
		copy(cpy[i].StorageKeys, tuple.StorageKeys)
	}
	return cpy
}

// deriveChainID derives the chain ID from a legacy V value.
func deriveChainID(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	// EIP-155: v = chainID * 2 + 35 or v = chainID * 2 + 36
	if v.BitLen() <= 8 {
		val := v.Uint64()
		if val == 27 || val == 28 {
			return new(big.Int)
		}
	}
	// v = chainID * 2 + 35 => chainID = (v - 35) / 2
	chainID := new(big.Int).Sub(v, big.NewInt(35))
	chainID.Div(chainID, big.NewInt(2))
	return chainID
}
