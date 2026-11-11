package ssz

import "encoding/binary"

// --- Basic type decoding ---

// UnmarshalBool decodes a boolean from a single byte.
func UnmarshalBool(data []byte) (bool, error) {
	if len(data) != 1 {
		return false, ErrSize
	}
	switch data[0] {
	case 0:
		return false, nil
	case 1:
		return true, nil
	default:
		return false, ErrInvalidBool
	}
}

// UnmarshalUint8 decodes a uint8 from a single byte.
func UnmarshalUint8(data []byte) (uint8, error) {
	if len(data) != 1 {
		return 0, ErrSize
	}
	return data[0], nil
}

// UnmarshalUint16 decodes a uint16 from 2 bytes little-endian.
func UnmarshalUint16(data []byte) (uint16, error) {
	if len(data) != 2 {
		return 0, ErrSize
	}
	return binary.LittleEndian.Uint16(data), nil
}

// UnmarshalUint32 decodes a uint32 from 4 bytes little-endian.
func UnmarshalUint32(data []byte) (uint32, error) {
	if len(data) != 4 {
		return 0, ErrSize
	}
	return binary.LittleEndian.Uint32(data), nil
}

// UnmarshalUint64 decodes a uint64 from 8 bytes little-endian.
func UnmarshalUint64(data []byte) (uint64, error) {
	if len(data) != 8 {
		return 0, ErrSize
	}
	return binary.LittleEndian.Uint64(data), nil
}

// UnmarshalUint128 decodes a 128-bit unsigned integer from 16 bytes
// little-endian, returning (lo, hi) limbs.
func UnmarshalUint128(data []byte) (lo, hi uint64, err error) {
	if len(data) != 16 {
		return 0, 0, ErrSize
	}
	lo = binary.LittleEndian.Uint64(data[0:8])
	hi = binary.LittleEndian.Uint64(data[8:16])
	return lo, hi, nil
}

// UnmarshalUint256 decodes a 256-bit unsigned integer from 32 bytes
// little-endian, returning [4]uint64 limbs.
func UnmarshalUint256(data []byte) ([4]uint64, error) {
	if len(data) != 32 {
		return [4]uint64{}, ErrSize
	}
	return [4]uint64{
		binary.LittleEndian.Uint64(data[0:8]),
		binary.LittleEndian.Uint64(data[8:16]),
		binary.LittleEndian.Uint64(data[16:24]),
		binary.LittleEndian.Uint64(data[24:32]),
	}, nil
}

// --- Composite type decoding ---

// UnmarshalVector decodes a vector of n fixed-size elements, each elemSize
// bytes long.
func UnmarshalVector(data []byte, n, elemSize int) ([][]byte, error) {
	if len(data) != n*elemSize {
		return nil, ErrSize
	}
	elements := make([][]byte, n)
	for i := 0; i < n; i++ {
		elem := make([]byte, elemSize)
		copy(elem, data[i*elemSize:(i+1)*elemSize])
		elements[i] = elem
	}
	return elements, nil
}

// UnmarshalList decodes a list of fixed-size elements, each elemSize bytes
// long. Returns the decoded elements.
func UnmarshalList(data []byte, elemSize int) ([][]byte, error) {
	if elemSize == 0 {
		return nil, ErrSize
	}
	if len(data)%elemSize != 0 {
		return nil, ErrSize
	}
	n := len(data) / elemSize
	return UnmarshalVector(data, n, elemSize)
}

// UnmarshalVariableContainer decodes a container with both fixed and variable
// fields. fixedSizes maps field index to its fixed byte size (0 for variable
// fields). numFields is the total number of fields.
// Returns a slice of decoded field data.
func UnmarshalVariableContainer(data []byte, numFields int, fixedSizes []int) ([][]byte, error) {
	if len(fixedSizes) != numFields {
		return nil, ErrSize
	}

	fields := make([][]byte, numFields)
	offsets := make([]uint32, 0)
	offsetFieldIndices := make([]int, 0)

	pos := 0
	for i := 0; i < numFields; i++ {
		if fixedSizes[i] > 0 {
			// Fixed-size field.
			end := pos + fixedSizes[i]
			if end > len(data) {
				return nil, ErrBufferTooSmall
			}
			fields[i] = make([]byte, fixedSizes[i])
			copy(fields[i], data[pos:end])
			pos = end
		} else {
			// Variable-size field: read a 4-byte offset.
			if pos+BytesPerLengthOffset > len(data) {
				return nil, ErrBufferTooSmall
			}
			offset := binary.LittleEndian.Uint32(data[pos : pos+BytesPerLengthOffset])
			offsets = append(offsets, offset)
			offsetFieldIndices = append(offsetFieldIndices, i)
			pos += BytesPerLengthOffset
		}
	}

	if len(offsets) == 0 && pos != len(data) {
		return nil, ErrSize
	}
	if len(offsets) != 0 && uint64(offsets[0]) != uint64(pos) {
		return nil, ErrOffset
	}

	// Decode variable-size fields using the offsets.
	for i, idx := range offsetFieldIndices {
		start := uint64(offsets[i])
		end := uint64(len(data))
		if i+1 < len(offsets) {
			end = uint64(offsets[i+1])
		}
		if start > end || end > uint64(len(data)) {
			return nil, ErrOffset
		}
		fields[idx] = make([]byte, end-start)
		copy(fields[idx], data[start:end])
	}
	return fields, nil
}

// --- Bitfield decoding ---

// UnmarshalBitvector decodes a bitvector of exactly n bits.
func UnmarshalBitvector(data []byte, n int) ([]bool, error) {
	numBytes := (n + 7) / 8
	if len(data) != numBytes {
		return nil, ErrSize
	}
	bits := make([]bool, n)
	for i := 0; i < n; i++ {
		bits[i] = (data[i/8]>>(uint(i)%8))&1 == 1
	}
	return bits, nil
}

// UnmarshalBitlist decodes a bitlist, which includes a sentinel bit to mark
// the boundary. Returns the data bits (without the sentinel).
func UnmarshalBitlist(data []byte) ([]bool, error) {
	if len(data) == 0 {
		return nil, ErrSize
	}

	// Find the sentinel bit: the highest set bit in the last byte.
	lastByte := data[len(data)-1]
	if lastByte == 0 {
		return nil, ErrSize // no sentinel bit found
	}
	sentinelBit := 7
	for (lastByte>>uint(sentinelBit))&1 == 0 {
		sentinelBit--
	}

	// Total number of data bits = (len(data)-1)*8 + sentinelBit.
	n := (len(data)-1)*8 + sentinelBit
	bits := make([]bool, n)
	for i := 0; i < n; i++ {
		bits[i] = (data[i/8]>>(uint(i)%8))&1 == 1
	}
	return bits, nil
}
