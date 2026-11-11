package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP7939_CLZ verifies the count-leading-zeros precompile-free opcode
// (0x1e) against explicit vectors, driven through deployed bytecode.
func TestEIP7939_CLZ(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkGlamsterdan, 0))
	stf := s.STF()

	// runtime: CLZ(calldata word) → return.
	// CALLDATALOAD(0) → CLZ → MSTORE(0) → RETURN(0, 32).
	clz := []byte{
		0x60, 0x00, 0x35, // CALLDATALOAD(0)
		0x1e,             // CLZ
		0x60, 0x00, 0x52, // MSTORE(0, result)
		0x60, 0x20, 0x60, 0x00, 0xf3, // RETURN(0, 32)
	}
	addr := stf.Deploy(s.Alice(), clz)

	highBit := [32]byte{}
	highBit[0] = 0x80 // 2^255
	cases := []struct {
		input [32]byte
		want  uint64
	}{
		{word(0), 256},
		{word(1), 255},
		{word(2), 254},
		{highBit, 0},
		{word(1 << 8), 247},
	}
	for i, tc := range cases {
		ret, err := stf.Call(s.Alice().Address, addr, tc.input[:])
		if err != nil {
			t.Fatalf("case %d: CLZ call failed: %v", i, err)
		}
		if len(ret) != 32 {
			t.Fatalf("case %d: ret len %d", i, len(ret))
		}
		var got [32]byte
		copy(got[:], ret)
		if eiptest.Uint64ToHash(tc.want) != types.Hash(got) {
			t.Fatalf("case %d: CLZ(%x) = %s, want %d", i, tc.input, types.Hash(got).Hex(), tc.want)
		}
	}
}

func word(v uint64) [32]byte {
	var w [32]byte
	w[24] = byte(v >> 56)
	w[25] = byte(v >> 48)
	w[26] = byte(v >> 40)
	w[27] = byte(v >> 32)
	w[28] = byte(v >> 24)
	w[29] = byte(v >> 16)
	w[30] = byte(v >> 8)
	w[31] = byte(v)
	return w
}
