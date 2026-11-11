package types

import (
	"encoding/binary"
	"math"
	"math/big"

	"github.com/eth2030/eth2030/ssz"
)

func sszUint256(v *big.Int) ([]byte, error) {
	if v != nil && (v.Sign() < 0 || v.BitLen() > 256) {
		return nil, ssz.ErrSize
	}
	return bigIntTo32(v), nil
}

// A size of zero denotes a variable-length SSZ field.
func marshalExecutionSSZFields(parts [][]byte, sizes []int) ([]byte, error) {
	if len(parts) != len(sizes) {
		return nil, ssz.ErrSize
	}
	fixed := make([][]byte, len(parts))
	var variable [][]byte
	var indices []int
	var total uint64
	for i, size := range sizes {
		total += uint64(len(parts[i]))
		if size == 0 {
			total += 4
			indices = append(indices, i)
			variable = append(variable, parts[i])
		} else if len(parts[i]) != size {
			return nil, ssz.ErrSize
		} else {
			fixed[i] = parts[i]
		}
	}
	if total > math.MaxUint32 {
		return nil, ssz.ErrSize
	}
	return ssz.MarshalVariableContainer(fixed, variable, indices), nil
}

func marshalExecutionSSZList(elements [][]byte) ([]byte, error) {
	size := uint64(len(elements)) * 4
	for _, element := range elements {
		size += uint64(len(element))
	}
	if size > math.MaxUint32 {
		return nil, ssz.ErrSize
	}
	out := make([]byte, len(elements)*4, int(size))
	for i, element := range elements {
		binary.LittleEndian.PutUint32(out[i*4:], uint32(len(out)))
		out = append(out, element...)
	}
	return out, nil
}

func unmarshalExecutionSSZList(data []byte) ([][]byte, error) {
	if len(data) == 0 {
		return nil, nil
	}
	if len(data) < 4 {
		return nil, ssz.ErrBufferTooSmall
	}
	first := uint64(binary.LittleEndian.Uint32(data))
	if first < 4 || first%4 != 0 || first > uint64(len(data)) {
		return nil, ssz.ErrOffset
	}
	out := make([][]byte, int(first)/4)
	start := first
	for i := range out {
		end := uint64(len(data))
		if i+1 < len(out) {
			end = uint64(binary.LittleEndian.Uint32(data[(i+1)*4:]))
		}
		if end < start || end > uint64(len(data)) {
			return nil, ssz.ErrOffset
		}
		out[i] = data[int(start):int(end)]
		start = end
	}
	return out, nil
}

func executionProgressiveRoot(fields [][32]byte, mask uint32) [32]byte {
	var active [32]byte
	binary.LittleEndian.PutUint32(active[:], mask)
	return ssz.HashTreeRootProgressiveContainer(fields, active)
}

func progressiveBytesRoot(data []byte) [32]byte {
	return ssz.HashTreeRootProgressiveBasicList(data, len(data))
}
