package types

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/ssz"
)

// SSZ encoding errors.
var (
	ErrSSZUnknownTxType   = errors.New("ssz: unknown transaction type")
	ErrSSZTooShort        = errors.New("ssz: data too short")
	ErrSSZInvalidOffset   = errors.New("ssz: invalid offset")
	ErrSSZLegacySignature = errors.New("ssz: legacy signature v exceeds the one-byte field")
)

// Maximum lengths for SSZ list limits (used in Merkleization).
const (
	sszMaxTxData       = 1 << 24 // 16 MiB max calldata
	sszMaxAccessList   = 1 << 20
	sszMaxBlobHashes   = 1 << 12
	sszMaxAuthList     = 1 << 12
	sszMaxLogs         = 1 << 16
	sszMaxLogData      = 1 << 24
	sszMaxLogTopics    = 4
	sszMaxTransactions = 1 << 20
)

// bigIntTo32 converts a *big.Int to a 32-byte little-endian slice.
func bigIntTo32(v *big.Int) []byte {
	buf := make([]byte, 32)
	if v == nil || v.Sign() == 0 {
		return buf
	}
	// big.Int.Bytes() returns big-endian; reverse into little-endian.
	b := v.Bytes()
	for i, j := 0, len(b)-1; i <= j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	copy(buf, b)
	return buf
}

// bigIntFrom32LE reads a 32-byte little-endian slice into a *big.Int.
func bigIntFrom32LE(data []byte) *big.Int {
	if len(data) < 32 {
		return new(big.Int)
	}
	// Reverse to big-endian for big.Int.SetBytes.
	be := make([]byte, 32)
	for i := 0; i < 32; i++ {
		be[31-i] = data[i]
	}
	return new(big.Int).SetBytes(be)
}

// TransactionToSSZ encodes a transaction into SSZ format.
// Format: type_byte(1) || ssz_payload for typed txs; ssz_payload for legacy.
func TransactionToSSZ(tx *Transaction) ([]byte, error) {
	switch inner := tx.inner.(type) {
	case *LegacyTx:
		return encodeLegacySSZ(inner)
	case *AccessListTx:
		payload, err := encodeAccessListTxSSZ(inner)
		if err != nil {
			return nil, err
		}
		return append([]byte{AccessListTxType}, payload...), nil
	case *DynamicFeeTx:
		payload, err := encodeDynamicFeeTxSSZ(inner)
		if err != nil {
			return nil, err
		}
		return append([]byte{DynamicFeeTxType}, payload...), nil
	case *BlobTx:
		payload, err := encodeBlobTxSSZ(inner)
		if err != nil {
			return nil, err
		}
		return append([]byte{BlobTxType}, payload...), nil
	case *SetCodeTx:
		payload, err := encodeSetCodeTxSSZ(inner)
		if err != nil {
			return nil, err
		}
		return append([]byte{SetCodeTxType}, payload...), nil
	default:
		return nil, ErrSSZUnknownTxType
	}
}

// encodeLegacySSZ encodes a legacy tx as SSZ container.
// Fixed fields: nonce(8), gasPrice(32), gasLimit(8), to_flag(1), to(20), value(32), v(1), r(32), s(32)
// Variable fields: data
func encodeLegacySSZ(tx *LegacyTx) ([]byte, error) {
	if tx.V != nil && (tx.V.Sign() < 0 || tx.V.BitLen() > 8) {
		return nil, ErrSSZLegacySignature
	}
	// Fixed parts.
	nonce := ssz.MarshalUint64(tx.Nonce)
	gasPrice := bigIntTo32(tx.GasPrice)
	gasLimit := ssz.MarshalUint64(tx.Gas)

	// Optional "to": 1-byte flag + 20-byte address if present.
	var toField []byte
	if tx.To != nil {
		toField = make([]byte, 21)
		toField[0] = 1
		copy(toField[1:], tx.To[:])
	} else {
		toField = make([]byte, 21) // flag=0, zero address
	}

	value := bigIntTo32(tx.Value)

	// Signature: v as 1 byte, r and s as 32 bytes each.
	var vByte byte
	if tx.V != nil && tx.V.Sign() != 0 {
		vByte = byte(tx.V.Uint64())
	}

	r := bigIntTo32(tx.R)
	s := bigIntTo32(tx.S)

	// Variable part: data.
	data := tx.Data
	if data == nil {
		data = []byte{}
	}

	// Build container: fixed parts first, with offset for variable field (data).
	// Field order: nonce(8) | gasPrice(32) | gasLimit(8) | to(21) | value(32) | v(1) | r(32) | s(32) | data_offset(4)
	fixedParts := [][]byte{nonce, gasPrice, gasLimit, toField, value, {vByte}, r, s, nil}
	variableParts := [][]byte{data}
	variableIndices := []int{8}

	return ssz.MarshalVariableContainer(fixedParts, variableParts, variableIndices), nil
}

func encodeAccessListTxSSZ(tx *AccessListTx) ([]byte, error) {
	chainID := bigIntTo32(tx.ChainID)
	nonce := ssz.MarshalUint64(tx.Nonce)
	gasPrice := bigIntTo32(tx.GasPrice)
	gasLimit := ssz.MarshalUint64(tx.Gas)

	var toField []byte
	if tx.To != nil {
		toField = make([]byte, 21)
		toField[0] = 1
		copy(toField[1:], tx.To[:])
	} else {
		toField = make([]byte, 21)
	}

	value := bigIntTo32(tx.Value)

	var vByte byte
	if tx.V != nil {
		vByte = byte(tx.V.Uint64())
	}
	r := bigIntTo32(tx.R)
	s := bigIntTo32(tx.S)

	data := tx.Data
	if data == nil {
		data = []byte{}
	}
	al := encodeAccessListSSZ(tx.AccessList)

	// Field order: chainID(32) | nonce(8) | gasPrice(32) | gasLimit(8) | to(21) | value(32) | v(1) | r(32) | s(32) | data_offset(4) | accessList_offset(4)
	fixedParts := [][]byte{chainID, nonce, gasPrice, gasLimit, toField, value, {vByte}, r, s, nil, nil}
	variableParts := [][]byte{data, al}
	variableIndices := []int{9, 10}

	return ssz.MarshalVariableContainer(fixedParts, variableParts, variableIndices), nil
}

func encodeDynamicFeeTxSSZ(tx *DynamicFeeTx) ([]byte, error) {
	chainID := bigIntTo32(tx.ChainID)
	nonce := ssz.MarshalUint64(tx.Nonce)
	gasTipCap := bigIntTo32(tx.GasTipCap)
	gasFeeCap := bigIntTo32(tx.GasFeeCap)
	gasLimit := ssz.MarshalUint64(tx.Gas)

	var toField []byte
	if tx.To != nil {
		toField = make([]byte, 21)
		toField[0] = 1
		copy(toField[1:], tx.To[:])
	} else {
		toField = make([]byte, 21)
	}

	value := bigIntTo32(tx.Value)

	var vByte byte
	if tx.V != nil {
		vByte = byte(tx.V.Uint64())
	}
	r := bigIntTo32(tx.R)
	s := bigIntTo32(tx.S)

	data := tx.Data
	if data == nil {
		data = []byte{}
	}
	al := encodeAccessListSSZ(tx.AccessList)

	// chainID(32)|nonce(8)|gasTipCap(32)|gasFeeCap(32)|gasLimit(8)|to(21)|value(32)|v(1)|r(32)|s(32)|data|accessList
	fixedParts := [][]byte{chainID, nonce, gasTipCap, gasFeeCap, gasLimit, toField, value, {vByte}, r, s, nil, nil}
	variableParts := [][]byte{data, al}
	variableIndices := []int{10, 11}

	return ssz.MarshalVariableContainer(fixedParts, variableParts, variableIndices), nil
}

func encodeBlobTxSSZ(tx *BlobTx) ([]byte, error) {
	chainID := bigIntTo32(tx.ChainID)
	nonce := ssz.MarshalUint64(tx.Nonce)
	gasTipCap := bigIntTo32(tx.GasTipCap)
	gasFeeCap := bigIntTo32(tx.GasFeeCap)
	gasLimit := ssz.MarshalUint64(tx.Gas)
	// BlobTx.To is not a pointer -- always present.
	toField := make([]byte, 20)
	copy(toField, tx.To[:])
	value := bigIntTo32(tx.Value)
	blobFeeCap := bigIntTo32(tx.BlobFeeCap)

	var vByte byte
	if tx.V != nil {
		vByte = byte(tx.V.Uint64())
	}
	r := bigIntTo32(tx.R)
	s := bigIntTo32(tx.S)

	data := tx.Data
	if data == nil {
		data = []byte{}
	}
	al := encodeAccessListSSZ(tx.AccessList)
	blobHashes := encodeBlobHashesSSZ(tx.BlobHashes)

	// chainID(32)|nonce(8)|gasTipCap(32)|gasFeeCap(32)|gasLimit(8)|to(20)|value(32)|blobFeeCap(32)|v(1)|r(32)|s(32)|data|accessList|blobHashes
	fixedParts := [][]byte{chainID, nonce, gasTipCap, gasFeeCap, gasLimit, toField, value, blobFeeCap, {vByte}, r, s, nil, nil, nil}
	variableParts := [][]byte{data, al, blobHashes}
	variableIndices := []int{11, 12, 13}

	return ssz.MarshalVariableContainer(fixedParts, variableParts, variableIndices), nil
}

func encodeSetCodeTxSSZ(tx *SetCodeTx) ([]byte, error) {
	chainID := bigIntTo32(tx.ChainID)
	nonce := ssz.MarshalUint64(tx.Nonce)
	gasTipCap := bigIntTo32(tx.GasTipCap)
	gasFeeCap := bigIntTo32(tx.GasFeeCap)
	gasLimit := ssz.MarshalUint64(tx.Gas)
	toField := make([]byte, 20)
	copy(toField, tx.To[:])
	value := bigIntTo32(tx.Value)

	var vByte byte
	if tx.V != nil {
		vByte = byte(tx.V.Uint64())
	}
	r := bigIntTo32(tx.R)
	s := bigIntTo32(tx.S)

	data := tx.Data
	if data == nil {
		data = []byte{}
	}
	al := encodeAccessListSSZ(tx.AccessList)
	authList := encodeAuthorizationListSSZ(tx.AuthorizationList)

	// chainID(32)|nonce(8)|gasTipCap(32)|gasFeeCap(32)|gasLimit(8)|to(20)|value(32)|v(1)|r(32)|s(32)|data|accessList|authList
	fixedParts := [][]byte{chainID, nonce, gasTipCap, gasFeeCap, gasLimit, toField, value, {vByte}, r, s, nil, nil, nil}
	variableParts := [][]byte{data, al, authList}
	variableIndices := []int{10, 11, 12}

	return ssz.MarshalVariableContainer(fixedParts, variableParts, variableIndices), nil
}

// encodeAccessListSSZ encodes an access list as concatenated (address(20) || numKeys(4) || keys(32 each)).
func encodeAccessListSSZ(al AccessList) []byte {
	var out []byte
	for _, tuple := range al {
		out = append(out, tuple.Address[:]...)
		numKeys := make([]byte, 4)
		binary.LittleEndian.PutUint32(numKeys, uint32(len(tuple.StorageKeys)))
		out = append(out, numKeys...)
		for _, key := range tuple.StorageKeys {
			out = append(out, key[:]...)
		}
	}
	return out
}

func encodeBlobHashesSSZ(hashes []Hash) []byte {
	out := make([]byte, 0, len(hashes)*32)
	for _, h := range hashes {
		out = append(out, h[:]...)
	}
	return out
}

// encodeAuthorizationListSSZ encodes authorization entries.
// Each entry: chainID(32) | address(20) | nonce(8) | v(1) | r(32) | s(32) = 125 bytes.
func encodeAuthorizationListSSZ(auths []Authorization) []byte {
	const authSize = 32 + 20 + 8 + 1 + 32 + 32 // 125
	out := make([]byte, 0, len(auths)*authSize)
	for _, auth := range auths {
		out = append(out, bigIntTo32(auth.ChainID)...)
		out = append(out, auth.Address[:]...)
		out = append(out, ssz.MarshalUint64(auth.Nonce)...)
		var vByte byte
		if auth.V != nil {
			vByte = byte(auth.V.Uint64())
		}
		out = append(out, vByte)
		out = append(out, bigIntTo32(auth.R)...)
		out = append(out, bigIntTo32(auth.S)...)
	}
	return out
}

// SSZToTransaction decodes an SSZ-encoded transaction.
func SSZToTransaction(data []byte) (*Transaction, error) {
	if len(data) == 0 {
		return nil, ErrSSZTooShort
	}

	// Legacy has no prefix. Its low nonce byte can match a typed prefix,
	// so use the legacy schema if the typed schema does not decode.
	var typed TxData
	var typedErr error
	switch data[0] {
	case AccessListTxType:
		typed, typedErr = decodeAccessListTxSSZ(data[1:])
	case DynamicFeeTxType:
		typed, typedErr = decodeDynamicFeeTxSSZ(data[1:])
	case BlobTxType:
		typed, typedErr = decodeBlobTxSSZ(data[1:])
	case SetCodeTxType:
		typed, typedErr = decodeSetCodeTxSSZ(data[1:])
	}
	if typed != nil && typedErr == nil {
		return NewTransaction(typed), nil
	}
	legacy, err := decodeLegacySSZ(data)
	if err != nil && typedErr != nil {
		return nil, typedErr
	}
	if err != nil {
		return nil, err
	}
	return NewTransaction(legacy), nil
}

func readUint64LE(data []byte, offset int) (uint64, int) {
	return binary.LittleEndian.Uint64(data[offset : offset+8]), offset + 8
}

func readUint32LE(data []byte, offset int) (uint32, int) {
	return binary.LittleEndian.Uint32(data[offset : offset+4]), offset + 4
}

func readBigInt32LE(data []byte, offset int) (*big.Int, int) {
	return bigIntFrom32LE(data[offset : offset+32]), offset + 32
}

func readToField(data []byte, offset int) (*Address, int) {
	flag := data[offset]
	offset++
	if flag == 0 {
		return nil, offset + 20
	}
	var addr Address
	copy(addr[:], data[offset:offset+20])
	return &addr, offset + 20
}

func readToFieldDirect(data []byte, offset int) (Address, int) {
	var addr Address
	copy(addr[:], data[offset:offset+20])
	return addr, offset + 20
}

// decodeLegacySSZ decodes a legacy tx from SSZ.
// Fixed: nonce(8)|gasPrice(32)|gasLimit(8)|to(21)|value(32)|v(1)|r(32)|s(32)|data_offset(4) = 170
func decodeLegacySSZ(data []byte) (*LegacyTx, error) {
	const fixedSize = 8 + 32 + 8 + 21 + 32 + 1 + 32 + 32 + 4 // 170
	if len(data) < fixedSize {
		return nil, ErrSSZTooShort
	}
	tx := &LegacyTx{}
	off := 0
	tx.Nonce, off = readUint64LE(data, off)
	tx.GasPrice, off = readBigInt32LE(data, off)
	tx.Gas, off = readUint64LE(data, off)
	tx.To, off = readToField(data, off)
	tx.Value, off = readBigInt32LE(data, off)
	vByte := data[off]
	off++
	tx.V = new(big.Int).SetUint64(uint64(vByte))
	tx.R, off = readBigInt32LE(data, off)
	tx.S, off = readBigInt32LE(data, off)

	var dataOffset uint32
	dataOffset, off = readUint32LE(data, off)
	if int(dataOffset) != fixedSize {
		return nil, ErrSSZInvalidOffset
	}
	tx.Data = make([]byte, len(data)-int(dataOffset))
	copy(tx.Data, data[dataOffset:])
	return tx, nil
}

// decodeAccessListTxSSZ decodes an access list tx from SSZ payload (after type byte).
func decodeAccessListTxSSZ(data []byte) (*AccessListTx, error) {
	// chainID(32)|nonce(8)|gasPrice(32)|gasLimit(8)|to(21)|value(32)|v(1)|r(32)|s(32)|data_off(4)|al_off(4) = 206
	const fixedSize = 32 + 8 + 32 + 8 + 21 + 32 + 1 + 32 + 32 + 4 + 4 // 206
	if len(data) < fixedSize {
		return nil, ErrSSZTooShort
	}
	tx := &AccessListTx{}
	off := 0
	tx.ChainID, off = readBigInt32LE(data, off)
	tx.Nonce, off = readUint64LE(data, off)
	tx.GasPrice, off = readBigInt32LE(data, off)
	tx.Gas, off = readUint64LE(data, off)
	tx.To, off = readToField(data, off)
	tx.Value, off = readBigInt32LE(data, off)
	vByte := data[off]
	off++
	tx.V = new(big.Int).SetUint64(uint64(vByte))
	tx.R, off = readBigInt32LE(data, off)
	tx.S, off = readBigInt32LE(data, off)

	var dataOff, alOff uint32
	dataOff, off = readUint32LE(data, off)
	alOff, _ = readUint32LE(data, off)

	tx.Data = make([]byte, int(alOff)-int(dataOff))
	copy(tx.Data, data[dataOff:alOff])
	tx.AccessList = decodeAccessListSSZ(data[alOff:])
	return tx, nil
}

func decodeDynamicFeeTxSSZ(data []byte) (*DynamicFeeTx, error) {
	// chainID(32)|nonce(8)|gasTipCap(32)|gasFeeCap(32)|gasLimit(8)|to(21)|value(32)|v(1)|r(32)|s(32)|data_off(4)|al_off(4) = 238
	const fixedSize = 32 + 8 + 32 + 32 + 8 + 21 + 32 + 1 + 32 + 32 + 4 + 4
	if len(data) < fixedSize {
		return nil, ErrSSZTooShort
	}
	tx := &DynamicFeeTx{}
	off := 0
	tx.ChainID, off = readBigInt32LE(data, off)
	tx.Nonce, off = readUint64LE(data, off)
	tx.GasTipCap, off = readBigInt32LE(data, off)
	tx.GasFeeCap, off = readBigInt32LE(data, off)
	tx.Gas, off = readUint64LE(data, off)
	tx.To, off = readToField(data, off)
	tx.Value, off = readBigInt32LE(data, off)
	vByte := data[off]
	off++
	tx.V = new(big.Int).SetUint64(uint64(vByte))
	tx.R, off = readBigInt32LE(data, off)
	tx.S, off = readBigInt32LE(data, off)

	var dataOff, alOff uint32
	dataOff, off = readUint32LE(data, off)
	alOff, _ = readUint32LE(data, off)

	tx.Data = make([]byte, int(alOff)-int(dataOff))
	copy(tx.Data, data[dataOff:alOff])
	tx.AccessList = decodeAccessListSSZ(data[alOff:])
	return tx, nil
}

func decodeBlobTxSSZ(data []byte) (*BlobTx, error) {
	// chainID(32)|nonce(8)|gasTipCap(32)|gasFeeCap(32)|gasLimit(8)|to(20)|value(32)|blobFeeCap(32)|v(1)|r(32)|s(32)|data_off(4)|al_off(4)|bh_off(4) = 273
	const fixedSize = 32 + 8 + 32 + 32 + 8 + 20 + 32 + 32 + 1 + 32 + 32 + 4 + 4 + 4
	if len(data) < fixedSize {
		return nil, ErrSSZTooShort
	}
	tx := &BlobTx{}
	off := 0
	tx.ChainID, off = readBigInt32LE(data, off)
	tx.Nonce, off = readUint64LE(data, off)
	tx.GasTipCap, off = readBigInt32LE(data, off)
	tx.GasFeeCap, off = readBigInt32LE(data, off)
	tx.Gas, off = readUint64LE(data, off)
	tx.To, off = readToFieldDirect(data, off)
	tx.Value, off = readBigInt32LE(data, off)
	tx.BlobFeeCap, off = readBigInt32LE(data, off)
	vByte := data[off]
	off++
	tx.V = new(big.Int).SetUint64(uint64(vByte))
	tx.R, off = readBigInt32LE(data, off)
	tx.S, off = readBigInt32LE(data, off)

	var dataOff, alOff, bhOff uint32
	dataOff, off = readUint32LE(data, off)
	alOff, off = readUint32LE(data, off)
	bhOff, _ = readUint32LE(data, off)

	tx.Data = make([]byte, int(alOff)-int(dataOff))
	copy(tx.Data, data[dataOff:alOff])
	tx.AccessList = decodeAccessListSSZ(data[alOff:bhOff])
	tx.BlobHashes = decodeBlobHashesSSZ(data[bhOff:])
	return tx, nil
}

func decodeSetCodeTxSSZ(data []byte) (*SetCodeTx, error) {
	// chainID(32)|nonce(8)|gasTipCap(32)|gasFeeCap(32)|gasLimit(8)|to(20)|value(32)|v(1)|r(32)|s(32)|data_off(4)|al_off(4)|auth_off(4) = 241
	const fixedSize = 32 + 8 + 32 + 32 + 8 + 20 + 32 + 1 + 32 + 32 + 4 + 4 + 4
	if len(data) < fixedSize {
		return nil, ErrSSZTooShort
	}
	tx := &SetCodeTx{}
	off := 0
	tx.ChainID, off = readBigInt32LE(data, off)
	tx.Nonce, off = readUint64LE(data, off)
	tx.GasTipCap, off = readBigInt32LE(data, off)
	tx.GasFeeCap, off = readBigInt32LE(data, off)
	tx.Gas, off = readUint64LE(data, off)
	tx.To, off = readToFieldDirect(data, off)
	tx.Value, off = readBigInt32LE(data, off)
	vByte := data[off]
	off++
	tx.V = new(big.Int).SetUint64(uint64(vByte))
	tx.R, off = readBigInt32LE(data, off)
	tx.S, off = readBigInt32LE(data, off)

	var dataOff, alOff, authOff uint32
	dataOff, off = readUint32LE(data, off)
	alOff, off = readUint32LE(data, off)
	authOff, _ = readUint32LE(data, off)

	tx.Data = make([]byte, int(alOff)-int(dataOff))
	copy(tx.Data, data[dataOff:alOff])
	tx.AccessList = decodeAccessListSSZ(data[alOff:authOff])
	tx.AuthorizationList = decodeAuthorizationListSSZ(data[authOff:])
	return tx, nil
}

func decodeAccessListSSZ(data []byte) AccessList {
	var al AccessList
	off := 0
	for off < len(data) {
		if off+24 > len(data) {
			break
		}
		var tuple AccessTuple
		copy(tuple.Address[:], data[off:off+20])
		off += 20
		numKeys := binary.LittleEndian.Uint32(data[off : off+4])
		off += 4
		tuple.StorageKeys = make([]Hash, numKeys)
		for i := uint32(0); i < numKeys; i++ {
			if off+32 > len(data) {
				break
			}
			copy(tuple.StorageKeys[i][:], data[off:off+32])
			off += 32
		}
		al = append(al, tuple)
	}
	return al
}

func decodeBlobHashesSSZ(data []byte) []Hash {
	n := len(data) / 32
	hashes := make([]Hash, n)
	for i := 0; i < n; i++ {
		copy(hashes[i][:], data[i*32:(i+1)*32])
	}
	return hashes
}

func decodeAuthorizationListSSZ(data []byte) []Authorization {
	const authSize = 32 + 20 + 8 + 1 + 32 + 32 // 125
	n := len(data) / authSize
	auths := make([]Authorization, n)
	for i := 0; i < n; i++ {
		off := i * authSize
		auths[i].ChainID = bigIntFrom32LE(data[off : off+32])
		off += 32
		copy(auths[i].Address[:], data[off:off+20])
		off += 20
		auths[i].Nonce = binary.LittleEndian.Uint64(data[off : off+8])
		off += 8
		auths[i].V = new(big.Int).SetUint64(uint64(data[off]))
		off++
		auths[i].R = bigIntFrom32LE(data[off : off+32])
		off += 32
		auths[i].S = bigIntFrom32LE(data[off : off+32])
	}
	return auths
}

// TransactionSSZRoot computes the SSZ hash tree root of a transaction.
func TransactionSSZRoot(tx *Transaction) (Hash, error) {
	encoded, err := TransactionToSSZ(tx)
	if err != nil {
		return Hash{}, err
	}
	chunks := ssz.Pack(encoded)
	root := ssz.Merkleize(chunks, 0)
	var h Hash
	copy(h[:], root[:])
	return h, nil
}

// TransactionsSSZRoot computes the SSZ merkle root over a list of transactions.
func TransactionsSSZRoot(txs []*Transaction) (Hash, error) {
	roots := make([][32]byte, len(txs))
	for i, tx := range txs {
		r, err := TransactionSSZRoot(tx)
		if err != nil {
			return Hash{}, err
		}
		roots[i] = r
	}
	root := ssz.HashTreeRootList(roots, sszMaxTransactions)
	var h Hash
	copy(h[:], root[:])
	return h, nil
}

// ReceiptToSSZ encodes a receipt into SSZ format.
// Fixed fields: status(1) | cumulativeGasUsed(8) | logsBloom(256)
// Variable: logs
func ReceiptToSSZ(receipt *Receipt) ([]byte, error) {
	status := []byte{byte(receipt.Status)}
	cumulativeGas := ssz.MarshalUint64(receipt.CumulativeGasUsed)
	bloom := make([]byte, BloomLength)
	copy(bloom, receipt.Bloom[:])

	logsData := encodeLogsSSZ(receipt.Logs)

	fixedParts := [][]byte{status, cumulativeGas, bloom, nil}
	variableParts := [][]byte{logsData}
	variableIndices := []int{3}

	return ssz.MarshalVariableContainer(fixedParts, variableParts, variableIndices), nil
}

func encodeLogsSSZ(logs []*Log) []byte {
	var out []byte
	for _, log := range logs {
		out = append(out, log.Address[:]...)
		numTopics := make([]byte, 4)
		binary.LittleEndian.PutUint32(numTopics, uint32(len(log.Topics)))
		out = append(out, numTopics...)
		for _, topic := range log.Topics {
			out = append(out, topic[:]...)
		}
		dataLen := make([]byte, 4)
		binary.LittleEndian.PutUint32(dataLen, uint32(len(log.Data)))
		out = append(out, dataLen...)
		out = append(out, log.Data...)
	}
	return out
}

// SSZToReceipt decodes an SSZ-encoded receipt.
func SSZToReceipt(data []byte) (*Receipt, error) {
	// Fixed: status(1) | cumulativeGasUsed(8) | bloom(256) | logs_offset(4) = 269
	const fixedSize = 1 + 8 + 256 + 4
	if len(data) < fixedSize {
		return nil, ErrSSZTooShort
	}
	r := &Receipt{}
	off := 0
	r.Status = uint64(data[off])
	off++
	r.CumulativeGasUsed, off = readUint64LE(data, off)
	copy(r.Bloom[:], data[off:off+BloomLength])
	off += BloomLength

	var logsOffset uint32
	logsOffset, _ = readUint32LE(data, off)

	r.Logs = decodeLogsSSZ(data[logsOffset:])
	return r, nil
}

func decodeLogsSSZ(data []byte) []*Log {
	var logs []*Log
	off := 0
	for off < len(data) {
		if off+24 > len(data) {
			break
		}
		log := &Log{}
		copy(log.Address[:], data[off:off+20])
		off += 20
		numTopics := binary.LittleEndian.Uint32(data[off : off+4])
		off += 4
		log.Topics = make([]Hash, numTopics)
		for i := uint32(0); i < numTopics; i++ {
			if off+32 > len(data) {
				break
			}
			copy(log.Topics[i][:], data[off:off+32])
			off += 32
		}
		if off+4 > len(data) {
			break
		}
		dataLen := binary.LittleEndian.Uint32(data[off : off+4])
		off += 4
		if off+int(dataLen) > len(data) {
			break
		}
		log.Data = make([]byte, dataLen)
		copy(log.Data, data[off:off+int(dataLen)])
		off += int(dataLen)
		logs = append(logs, log)
	}
	return logs
}
