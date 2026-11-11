package types

import (
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/ssz"
)

// SSZ encoding errors.
var (
	ErrSSZUnknownTxType = errors.New("ssz: unknown transaction type")
	ErrSSZTooShort      = errors.New("ssz: data too short")
	ErrSSZInvalidOffset = errors.New("ssz: invalid offset")
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

func readUint64LE(data []byte, offset int) (uint64, int) {
	return binary.LittleEndian.Uint64(data[offset : offset+8]), offset + 8
}

func readUint32LE(data []byte, offset int) (uint32, int) {
	return binary.LittleEndian.Uint32(data[offset : offset+4]), offset + 4
}

func readBigInt32LE(data []byte, offset int) (*big.Int, int) {
	return bigIntFrom32LE(data[offset : offset+32]), offset + 32
}

// ReceiptToSSZ encodes the repository’s experimental receipt format.
// EIP-7807 uses the existing receipt RLP envelope.
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
