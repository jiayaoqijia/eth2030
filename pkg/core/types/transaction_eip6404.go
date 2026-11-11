package types

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"math/bits"

	"github.com/eth2030/eth2030/ssz"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

var ErrSSZTransactionSignature = errors.New("ssz: invalid EIP-7932 secp256k1 signature")

// EIP-6404 payload field positions. The selector determines the active
// positions, and inactive positions take no bytes on the wire.
const (
	sszTxType = iota
	sszTxChain
	sszTxNonce
	sszTxMaxFees
	sszTxGas
	sszTxTo
	sszTxValue
	sszTxInput
	sszTxAccess
	sszTxTip
	sszTxBlobHashes
	sszTxAuthorizations
	sszTxFieldCount
)

type transactionSSZLayout struct {
	txType byte
	active uint32
}

var transactionSSZLayouts = [...]transactionSSZLayout{
	{},                        // CompatibleUnion selector 0 is reserved.
	{LegacyTxType, 0x0fd},     // replayable, call
	{LegacyTxType, 0x0dd},     // replayable, create
	{LegacyTxType, 0x0ff},     // EIP-155, call
	{LegacyTxType, 0x0df},     // EIP-155, create
	{AccessListTxType, 0x1ff}, // call
	{AccessListTxType, 0x1df}, // create
	{DynamicFeeTxType, 0x3ff}, // call
	{DynamicFeeTxType, 0x3df}, // create
	{BlobTxType, 0x7ff},
	{SetCodeTxType, 0xbff},
}

func (l transactionSSZLayout) sizes() []int {
	sizes := [...]int{1, 32, 8, 32, 8, 20, 32, 0, 0, 32, 0, 0}
	if l.txType == BlobTxType {
		sizes[sszTxMaxFees] = 64
	}
	var active []int
	for i, size := range sizes {
		if l.active&(1<<i) != 0 {
			active = append(active, size)
		}
	}
	return active
}

func (l transactionSSZLayout) expand(parts [][]byte) [sszTxFieldCount][]byte {
	var fields [sszTxFieldCount][]byte
	for i := range fields {
		if l.active&(1<<i) != 0 {
			fields[i], parts = parts[0], parts[1:]
		}
	}
	return fields
}

// TransactionToSSZ converts an EIP-2718 transaction to the published
// EIP-6404 Transaction container, preserving its original RLP identity.
func TransactionToSSZ(tx *Transaction) ([]byte, error) {
	selector, parts, signature, err := transactionSSZParts(tx)
	if err != nil {
		return nil, err
	}
	payload, err := marshalExecutionSSZFields(parts, transactionSSZLayouts[selector].sizes())
	if err != nil {
		return nil, err
	}
	return marshalExecutionSSZFields([][]byte{append([]byte{selector}, payload...), signature}, []int{0, 0})
}

func transactionSSZParts(tx *Transaction) (byte, [][]byte, []byte, error) {
	if tx == nil {
		return 0, nil, nil, ErrSSZUnknownTxType
	}
	v, r, s := tx.RawSignatureValues()
	if v == nil || v.Sign() < 0 {
		return 0, nil, nil, ErrSSZTransactionSignature
	}
	parity := v
	chainID := tx.ChainId()
	var selector byte
	switch tx.Type() {
	case LegacyTxType:
		switch {
		case v.Cmp(big.NewInt(27)) == 0 || v.Cmp(big.NewInt(28)) == 0:
			selector = 1
			parity = new(big.Int).Sub(v, big.NewInt(27))
		case v.Cmp(big.NewInt(35)) >= 0:
			selector = 3
			parity = new(big.Int).SetUint64(uint64(1 - v.Bit(0)))
		default:
			return 0, nil, nil, ErrSSZTransactionSignature
		}
	case AccessListTxType:
		selector = 5
	case DynamicFeeTxType:
		selector = 7
	case BlobTxType:
		selector = 9
	case SetCodeTxType:
		selector = 10
	default:
		return 0, nil, nil, ErrSSZUnknownTxType
	}

	to := tx.To()
	if to == nil {
		if selector >= 9 {
			return 0, nil, nil, fmt.Errorf("ssz: transaction type %d requires a recipient", tx.Type())
		}
		selector++
	}
	signature, err := encodeExecutionSignature(parity, r, s)
	if err != nil {
		return 0, nil, nil, err
	}
	layout := transactionSSZLayouts[selector]
	var f [sszTxFieldCount][]byte
	f[sszTxType] = []byte{tx.Type()}
	if layout.active&(1<<sszTxChain) != 0 {
		f[sszTxChain], err = sszUint256(chainID)
		if err != nil {
			return 0, nil, nil, fmt.Errorf("ssz: chain ID: %w", err)
		}
	}
	f[sszTxNonce] = ssz.MarshalUint64(tx.Nonce())
	f[sszTxMaxFees], err = sszUint256(tx.GasFeeCap())
	if err != nil {
		return 0, nil, nil, fmt.Errorf("ssz: fee cap: %w", err)
	}
	if layout.txType == BlobTxType {
		blobFee, err := sszUint256(tx.BlobGasFeeCap())
		if err != nil {
			return 0, nil, nil, fmt.Errorf("ssz: blob fee cap: %w", err)
		}
		f[sszTxMaxFees] = append(f[sszTxMaxFees], blobFee...)
		for _, h := range tx.BlobHashes() {
			f[sszTxBlobHashes] = append(f[sszTxBlobHashes], h[:]...)
		}
	}
	f[sszTxGas] = ssz.MarshalUint64(tx.Gas())
	if to != nil {
		f[sszTxTo] = to[:]
	}
	f[sszTxValue], err = sszUint256(tx.Value())
	if err != nil {
		return 0, nil, nil, fmt.Errorf("ssz: value: %w", err)
	}
	f[sszTxInput] = tx.Data()
	if layout.active&(1<<sszTxAccess) != 0 {
		f[sszTxAccess], err = encodeNormalizedAccessList(tx.AccessList())
		if err != nil {
			return 0, nil, nil, err
		}
	}
	if layout.active&(1<<sszTxTip) != 0 {
		f[sszTxTip], err = sszUint256(tx.GasTipCap())
		if err != nil {
			return 0, nil, nil, fmt.Errorf("ssz: tip cap: %w", err)
		}
	}
	if layout.active&(1<<sszTxAuthorizations) != 0 {
		f[sszTxAuthorizations], err = encodeNormalizedAuthorizations(tx.AuthorizationList())
		if err != nil {
			return 0, nil, nil, err
		}
	}

	parts := make([][]byte, 0, bits.OnesCount32(layout.active))
	for i := range f {
		if layout.active&(1<<i) != 0 {
			parts = append(parts, f[i])
		}
	}
	return selector, parts, signature, nil
}

// Published EIP-7932 defines secp256k1 as algorithm 0x00.
const executionSecp256k1 = 0x00

func encodeExecutionSignature(parity, r, s *big.Int) ([]byte, error) {
	if parity == nil || parity.Sign() < 0 || parity.BitLen() > 1 ||
		r == nil || s == nil || !gethcrypto.ValidateSignatureValues(byte(parity.Uint64()), r, s, true) {
		return nil, ErrSSZTransactionSignature
	}
	out := make([]byte, 66)
	out[0] = executionSecp256k1
	r.FillBytes(out[1:33])
	s.FillBytes(out[33:65])
	out[65] = byte(parity.Uint64())
	return out, nil
}

func decodeExecutionSignature(data []byte) (v, r, s *big.Int, err error) {
	if len(data) != 66 || data[0] != executionSecp256k1 {
		return nil, nil, nil, ErrSSZTransactionSignature
	}
	r, s = new(big.Int).SetBytes(data[1:33]), new(big.Int).SetBytes(data[33:65])
	if !gethcrypto.ValidateSignatureValues(data[65], r, s, true) {
		return nil, nil, nil, ErrSSZTransactionSignature
	}
	return new(big.Int).SetUint64(uint64(data[65])), r, s, nil
}

func encodeNormalizedAccessList(list AccessList) ([]byte, error) {
	elements := make([][]byte, len(list))
	for i, tuple := range list {
		var keys []byte
		for _, key := range tuple.StorageKeys {
			keys = append(keys, key[:]...)
		}
		var err error
		elements[i], err = marshalExecutionSSZFields([][]byte{tuple.Address[:], keys}, []int{20, 0})
		if err != nil {
			return nil, err
		}
	}
	return marshalExecutionSSZList(elements)
}

func encodeNormalizedAuthorizations(list []Authorization) ([]byte, error) {
	elements := make([][]byte, len(list))
	for i, auth := range list {
		signature, err := encodeExecutionSignature(auth.V, auth.R, auth.S)
		if err != nil {
			return nil, fmt.Errorf("authorization %d: %w", i, err)
		}
		payload := []byte{1, 5} // selector, SET_CODE_MAGIC
		if auth.ChainID != nil && auth.ChainID.Sign() != 0 {
			chainID, err := sszUint256(auth.ChainID)
			if err != nil {
				return nil, fmt.Errorf("authorization %d chain ID: %w", i, err)
			}
			payload[0] = 2
			payload = append(payload, chainID...)
		}
		payload = append(payload, auth.Address[:]...)
		payload = append(payload, ssz.MarshalUint64(auth.Nonce)...)
		elements[i], err = marshalExecutionSSZFields([][]byte{payload, signature}, []int{0, 0})
		if err != nil {
			return nil, err
		}
	}
	return marshalExecutionSSZList(elements)
}

// SSZToTransaction decodes the EIP-6404 container and reconstructs the
// corresponding EIP-2718 transaction.
func SSZToTransaction(data []byte) (*Transaction, error) {
	signed, err := ssz.UnmarshalVariableContainer(data, 2, []int{0, 0})
	if err != nil {
		return nil, err
	}
	payload, signature := signed[0], signed[1]
	if len(payload) == 0 || payload[0] == 0 || int(payload[0]) >= len(transactionSSZLayouts) {
		return nil, ErrSSZUnknownTxType
	}
	layout := transactionSSZLayouts[payload[0]]
	sizes := layout.sizes()
	parts, err := ssz.UnmarshalVariableContainer(payload[1:], len(sizes), sizes)
	if err != nil {
		return nil, err
	}
	f := layout.expand(parts)
	if f[sszTxType][0] != layout.txType {
		return nil, ErrSSZUnknownTxType
	}
	v, r, s, err := decodeExecutionSignature(signature)
	if err != nil {
		return nil, err
	}
	nonce := binary.LittleEndian.Uint64(f[sszTxNonce])
	gas := binary.LittleEndian.Uint64(f[sszTxGas])
	fee := bigIntFrom32LE(f[sszTxMaxFees][:32])
	value := bigIntFrom32LE(f[sszTxValue])
	input := f[sszTxInput]
	to := bytesToAddressPtr(f[sszTxTo])
	chainID := bigIntFrom32LE(f[sszTxChain])
	if layout.txType == LegacyTxType {
		if f[sszTxChain] != nil {
			v.Add(v, new(big.Int).Lsh(chainID, 1)).Add(v, big.NewInt(35))
		} else {
			v.Add(v, big.NewInt(27))
		}
		return NewTransaction(&LegacyTx{Nonce: nonce, GasPrice: fee, Gas: gas, To: to, Value: value, Data: input, V: v, R: r, S: s}), nil
	}
	access, err := decodeNormalizedAccessList(f[sszTxAccess])
	if err != nil {
		return nil, err
	}
	tip := bigIntFrom32LE(f[sszTxTip])
	switch layout.txType {
	case AccessListTxType:
		return NewTransaction(&AccessListTx{ChainID: chainID, Nonce: nonce, GasPrice: fee, Gas: gas, To: to, Value: value, Data: input, AccessList: access, V: v, R: r, S: s}), nil
	case DynamicFeeTxType:
		return NewTransaction(&DynamicFeeTx{ChainID: chainID, Nonce: nonce, GasFeeCap: fee, GasTipCap: tip, Gas: gas, To: to, Value: value, Data: input, AccessList: access, V: v, R: r, S: s}), nil
	case BlobTxType:
		hashes, err := decodeNormalizedHashes(f[sszTxBlobHashes])
		if err != nil {
			return nil, err
		}
		return NewTransaction(&BlobTx{ChainID: chainID, Nonce: nonce, GasFeeCap: fee, GasTipCap: tip, Gas: gas, To: *to, Value: value, Data: input, AccessList: access, BlobFeeCap: bigIntFrom32LE(f[sszTxMaxFees][32:]), BlobHashes: hashes, V: v, R: r, S: s}), nil
	case SetCodeTxType:
		auth, err := decodeNormalizedAuthorizations(f[sszTxAuthorizations])
		if err != nil {
			return nil, err
		}
		return NewTransaction(&SetCodeTx{ChainID: chainID, Nonce: nonce, GasFeeCap: fee, GasTipCap: tip, Gas: gas, To: *to, Value: value, Data: input, AccessList: access, AuthorizationList: auth, V: v, R: r, S: s}), nil
	default:
		return nil, ErrSSZUnknownTxType
	}
}

func decodeNormalizedHashes(data []byte) ([]Hash, error) {
	if len(data)%32 != 0 {
		return nil, ssz.ErrSize
	}
	out := make([]Hash, len(data)/32)
	for i := range out {
		copy(out[i][:], data[i*32:(i+1)*32])
	}
	return out, nil
}

func decodeNormalizedAccessList(data []byte) (AccessList, error) {
	elements, err := unmarshalExecutionSSZList(data)
	if err != nil {
		return nil, err
	}
	list := make(AccessList, len(elements))
	for i, element := range elements {
		f, err := ssz.UnmarshalVariableContainer(element, 2, []int{20, 0})
		if err != nil {
			return nil, err
		}
		list[i].Address = BytesToAddress(f[0])
		list[i].StorageKeys, err = decodeNormalizedHashes(f[1])
		if err != nil {
			return nil, err
		}
	}
	return list, nil
}

func decodeNormalizedAuthorizations(data []byte) ([]Authorization, error) {
	elements, err := unmarshalExecutionSSZList(data)
	if err != nil {
		return nil, err
	}
	out := make([]Authorization, len(elements))
	for i, element := range elements {
		parts, err := ssz.UnmarshalVariableContainer(element, 2, []int{0, 0})
		if err != nil {
			return nil, err
		}
		p := parts[0]
		if len(p) < 2 || p[1] != 5 {
			return nil, fmt.Errorf("ssz: invalid authorization type")
		}
		offset := 2
		chainID := new(big.Int)
		switch {
		case p[0] == 1 && len(p) == 30:
		case p[0] == 2 && len(p) == 62:
			chainID = bigIntFrom32LE(p[2:34])
			if chainID.Sign() == 0 {
				return nil, fmt.Errorf("ssz: protected authorization has zero chain ID")
			}
			offset = 34
		default:
			return nil, fmt.Errorf("ssz: invalid authorization selector or length")
		}
		v, r, s, err := decodeExecutionSignature(parts[1])
		if err != nil {
			return nil, err
		}
		out[i] = Authorization{ChainID: chainID, Address: BytesToAddress(p[offset : offset+20]), Nonce: binary.LittleEndian.Uint64(p[offset+20:]), V: v, R: r, S: s}
	}
	return out, nil
}

// TransactionSSZRoot returns the EIP-6404 tx_root. The historical RLP hash
// remains available through Transaction.Hash.
func TransactionSSZRoot(tx *Transaction) (Hash, error) {
	selector, parts, signature, err := transactionSSZParts(tx)
	if err != nil {
		return Hash{}, err
	}
	layout := transactionSSZLayouts[selector]
	f := layout.expand(parts)
	roots := make([][32]byte, bits.Len32(layout.active))
	for i := range roots {
		switch i {
		case sszTxMaxFees, sszTxTip:
			if f[i] != nil {
				fees := ssz.Pack(f[i])
				roots[i] = executionProgressiveRoot(fees, uint32(1<<len(fees))-1)
			}
		case sszTxInput:
			roots[i] = progressiveBytesRoot(f[i])
		case sszTxAccess:
			if layout.active&(1<<i) != 0 {
				tuples := tx.AccessList()
				list := make([][32]byte, len(tuples))
				for j, tuple := range tuples {
					var address [32]byte
					copy(address[:], tuple.Address[:])
					keys := hashChunks(tuple.StorageKeys)
					list[j] = ssz.HashTreeRootContainer([][32]byte{address, ssz.HashTreeRootProgressiveList(keys)})
				}
				roots[i] = ssz.HashTreeRootProgressiveList(list)
			}
		case sszTxBlobHashes:
			if layout.active&(1<<i) != 0 {
				roots[i] = ssz.HashTreeRootProgressiveList(hashChunks(tx.BlobHashes()))
			}
		case sszTxAuthorizations:
			if layout.active&(1<<i) != 0 {
				list := tx.AuthorizationList()
				authRoots := make([][32]byte, len(list))
				for j, auth := range list {
					sig, err := encodeExecutionSignature(auth.V, auth.R, auth.S)
					if err != nil {
						return Hash{}, err
					}
					var fields [4][32]byte
					fields[0][0] = 5
					copy(fields[2][:], auth.Address[:])
					fields[3] = ssz.HashTreeRootUint64(auth.Nonce)
					mask, selector := uint32(0xd), byte(1)
					if auth.ChainID != nil && auth.ChainID.Sign() != 0 {
						chainID, err := sszUint256(auth.ChainID)
						if err != nil {
							return Hash{}, err
						}
						copy(fields[1][:], chainID)
						mask, selector = 0xf, 2
					}
					payload := ssz.HashTreeRootCompatibleUnion(executionProgressiveRoot(fields[:], mask), selector)
					authRoots[j] = ssz.HashTreeRootContainer([][32]byte{payload, progressiveBytesRoot(sig)})
				}
				roots[i] = ssz.HashTreeRootProgressiveList(authRoots)
			}
		default:
			copy(roots[i][:], f[i])
		}
	}
	payload := ssz.HashTreeRootCompatibleUnion(executionProgressiveRoot(roots, layout.active), selector)
	return Hash(ssz.HashTreeRootContainer([][32]byte{payload, progressiveBytesRoot(signature)})), nil
}

func hashChunks(hashes []Hash) [][32]byte {
	chunks := make([][32]byte, len(hashes))
	for i, h := range hashes {
		chunks[i] = [32]byte(h)
	}
	return chunks
}

// TransactionsSSZRoot is the root of EIP-6404 ProgressiveList[Transaction].
func TransactionsSSZRoot(txs []*Transaction) (Hash, error) {
	roots := make([][32]byte, len(txs))
	for i, tx := range txs {
		root, err := TransactionSSZRoot(tx)
		if err != nil {
			return Hash{}, fmt.Errorf("transaction %d: %w", i, err)
		}
		roots[i] = root
	}
	return Hash(ssz.HashTreeRootProgressiveList(roots)), nil
}
