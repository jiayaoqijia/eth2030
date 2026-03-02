package vm

import (
	"encoding/binary"
	"testing"
)

// buildVecInput constructs input: opcode(1) || width(1) || count(4 BE) || data
func buildVecInput(opcode, width byte, count uint32, data []byte) []byte {
	buf := make([]byte, 6+len(data))
	buf[0] = opcode
	buf[1] = width
	binary.BigEndian.PutUint32(buf[2:6], count)
	copy(buf[6:], data)
	return buf
}

func encodeU32s(vals ...uint32) []byte {
	out := make([]byte, len(vals)*4)
	for i, v := range vals {
		binary.BigEndian.PutUint32(out[i*4:], v)
	}
	return out
}

func encodeU64s(vals ...uint64) []byte {
	out := make([]byte, len(vals)*8)
	for i, v := range vals {
		binary.BigEndian.PutUint64(out[i*8:], v)
	}
	return out
}

func TestVectorizedVADD32(t *testing.T) {
	// [1,2,3] + [4,5,6] = [5,7,9]
	data := encodeU32s(1, 2, 3, 4, 5, 6)
	input := buildVecInput(VOpADD, VWidth32, 3, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VADD32: %v", err)
	}

	expected := encodeU32s(5, 7, 9)
	if len(out) != len(expected) {
		t.Fatalf("output length %d, want %d", len(out), len(expected))
	}
	for i := 0; i < 3; i++ {
		got := binary.BigEndian.Uint32(out[i*4:])
		want := binary.BigEndian.Uint32(expected[i*4:])
		if got != want {
			t.Errorf("element %d: got %d, want %d", i, got, want)
		}
	}
}

func TestVectorizedVMUL64(t *testing.T) {
	// [2,3] * [4,5] = [8,15]
	data := encodeU64s(2, 3, 4, 5)
	input := buildVecInput(VOpMUL, VWidth64, 2, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VMUL64: %v", err)
	}

	expected := encodeU64s(8, 15)
	if len(out) != len(expected) {
		t.Fatalf("output length %d, want %d", len(out), len(expected))
	}
	for i := 0; i < 2; i++ {
		got := binary.BigEndian.Uint64(out[i*8:])
		want := binary.BigEndian.Uint64(expected[i*8:])
		if got != want {
			t.Errorf("element %d: got %d, want %d", i, got, want)
		}
	}
}

func TestVectorizedVREDUCE32(t *testing.T) {
	// sum([1,2,3,4]) = 10
	data := encodeU32s(1, 2, 3, 4)
	input := buildVecInput(VOpREDUCE, VWidth32, 4, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VREDUCE: %v", err)
	}

	if len(out) != 4 {
		t.Fatalf("output length %d, want 4", len(out))
	}
	got := binary.BigEndian.Uint32(out)
	if got != 10 {
		t.Errorf("VREDUCE: got %d, want 10", got)
	}
}

func TestVectorizedVDOT32(t *testing.T) {
	// [1,2,3] . [4,5,6] = 4+10+18 = 32
	data := encodeU32s(1, 2, 3, 4, 5, 6)
	input := buildVecInput(VOpDOT, VWidth32, 3, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VDOT: %v", err)
	}

	if len(out) != 4 {
		t.Fatalf("output length %d, want 4", len(out))
	}
	got := binary.BigEndian.Uint32(out)
	if got != 32 {
		t.Errorf("VDOT: got %d, want 32", got)
	}
}

func TestVectorizedOverflow32(t *testing.T) {
	// 0xFFFFFFFF + 1 should wrap to 0
	data := encodeU32s(0xFFFFFFFF, 1)
	input := buildVecInput(VOpADD, VWidth32, 1, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("overflow: %v", err)
	}

	got := binary.BigEndian.Uint32(out)
	if got != 0 {
		t.Errorf("expected wrap to 0, got %d", got)
	}
}

func TestVectorizedInvalidOpcode(t *testing.T) {
	data := encodeU32s(1, 2)
	input := buildVecInput(0xFF, VWidth32, 1, data)

	p := &vectorizedPrecompile{}
	_, err := p.Run(input)
	if err == nil {
		t.Fatal("expected error for invalid opcode")
	}
}

func TestVectorizedInvalidWidth(t *testing.T) {
	data := encodeU32s(1, 2)
	input := buildVecInput(VOpADD, 3, 1, data)

	p := &vectorizedPrecompile{}
	_, err := p.Run(input)
	if err == nil {
		t.Fatal("expected error for invalid width")
	}
}

func TestVectorizedZeroCount(t *testing.T) {
	input := buildVecInput(VOpADD, VWidth32, 0, nil)

	p := &vectorizedPrecompile{}
	_, err := p.Run(input)
	if err != ErrVecZeroCount {
		t.Fatalf("expected ErrVecZeroCount, got %v", err)
	}
}

func TestVectorizedCountTooLarge(t *testing.T) {
	input := buildVecInput(VOpADD, VWidth32, vecMaxCount+1, nil)

	p := &vectorizedPrecompile{}
	_, err := p.Run(input)
	if err != ErrVecCountTooLarge {
		t.Fatalf("expected ErrVecCountTooLarge, got %v", err)
	}
}

func TestVectorizedGasCalculation(t *testing.T) {
	// 1000-element VADD: base(100) + 1000*3 = 3100
	data := make([]byte, 1000*4*2) // 1000 elements * 4 bytes * 2 vectors
	input := buildVecInput(VOpADD, VWidth32, 1000, data)

	p := &vectorizedPrecompile{}
	gas := p.RequiredGas(input)
	if gas != 3100 {
		t.Errorf("gas = %d, want 3100", gas)
	}
}

func TestVectorizedVSUB32(t *testing.T) {
	// [10,20,30] - [1,2,3] = [9,18,27]
	data := encodeU32s(10, 20, 30, 1, 2, 3)
	input := buildVecInput(VOpSUB, VWidth32, 3, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VSUB: %v", err)
	}

	for i, want := range []uint32{9, 18, 27} {
		got := binary.BigEndian.Uint32(out[i*4:])
		if got != want {
			t.Errorf("element %d: got %d, want %d", i, got, want)
		}
	}
}

func TestVectorizedVAND32(t *testing.T) {
	data := encodeU32s(0xFF, 0x0F, 0xFF, 0x0F)
	input := buildVecInput(VOpAND, VWidth32, 2, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VAND: %v", err)
	}

	for i, want := range []uint32{0xFF & 0xFF, 0x0F & 0x0F} {
		got := binary.BigEndian.Uint32(out[i*4:])
		if got != want {
			t.Errorf("element %d: got 0x%x, want 0x%x", i, got, want)
		}
	}
}

func TestVectorizedVOR32(t *testing.T) {
	data := encodeU32s(0xF0, 0x0F)
	input := buildVecInput(VOpOR, VWidth32, 1, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VOR: %v", err)
	}

	got := binary.BigEndian.Uint32(out)
	if got != 0xFF {
		t.Errorf("VOR: got 0x%x, want 0xff", got)
	}
}

func TestVectorizedVXOR32(t *testing.T) {
	data := encodeU32s(0xFF, 0xFF)
	input := buildVecInput(VOpXOR, VWidth32, 1, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VXOR: %v", err)
	}

	got := binary.BigEndian.Uint32(out)
	if got != 0 {
		t.Errorf("VXOR: got 0x%x, want 0", got)
	}
}

func TestVectorizedVSHL32(t *testing.T) {
	data := encodeU32s(1, 4) // 1 << 4 = 16
	input := buildVecInput(VOpSHL, VWidth32, 1, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VSHL: %v", err)
	}

	got := binary.BigEndian.Uint32(out)
	if got != 16 {
		t.Errorf("VSHL: got %d, want 16", got)
	}
}

func TestVectorizedVSHR32(t *testing.T) {
	data := encodeU32s(256, 4) // 256 >> 4 = 16
	input := buildVecInput(VOpSHR, VWidth32, 1, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VSHR: %v", err)
	}

	got := binary.BigEndian.Uint32(out)
	if got != 16 {
		t.Errorf("VSHR: got %d, want 16", got)
	}
}

func TestVectorizedVMOD32(t *testing.T) {
	data := encodeU32s(10, 3) // 10 % 3 = 1
	input := buildVecInput(VOpMOD, VWidth32, 1, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VMOD: %v", err)
	}

	got := binary.BigEndian.Uint32(out)
	if got != 1 {
		t.Errorf("VMOD: got %d, want 1", got)
	}
}

func TestVectorizedVMOD32_DivByZero(t *testing.T) {
	data := encodeU32s(10, 0) // 10 % 0 = 0 (safe)
	input := buildVecInput(VOpMOD, VWidth32, 1, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VMOD div-by-zero: %v", err)
	}

	got := binary.BigEndian.Uint32(out)
	if got != 0 {
		t.Errorf("VMOD div-by-zero: got %d, want 0", got)
	}
}

func TestVectorizedInputTooShort(t *testing.T) {
	p := &vectorizedPrecompile{}
	_, err := p.Run([]byte{0x01, 0x04})
	if err != ErrVecInputTooShort {
		t.Fatalf("expected ErrVecInputTooShort, got %v", err)
	}
}

func TestVectorizedDataTooShort(t *testing.T) {
	// Count=2 but only provide 1 element pair
	data := encodeU32s(1, 2)
	input := buildVecInput(VOpADD, VWidth32, 2, data)

	p := &vectorizedPrecompile{}
	_, err := p.Run(input)
	if err != ErrVecDataTooShort {
		t.Fatalf("expected ErrVecDataTooShort, got %v", err)
	}
}

func TestVectorizedRegistration(t *testing.T) {
	_, ok := PrecompiledContractsKPlus[VectorizedPrecompileAddr]
	if !ok {
		t.Fatal("vectorized precompile not registered in K+ fork set")
	}
}

func TestVectorizedGasOpcodes(t *testing.T) {
	tests := []struct {
		opcode  byte
		count   uint32
		wantGas uint64
	}{
		{VOpADD, 100, 100 + 100*3},
		{VOpMUL, 100, 100 + 100*5},
		{VOpSUB, 100, 100 + 100*3},
		{VOpAND, 100, 100 + 100*3},
		{VOpMOD, 100, 100 + 100*8},
		{VOpDOT, 100, 100 + 100*10},
		{VOpREDUCE, 100, 100 + 100*3},
	}
	p := &vectorizedPrecompile{}
	for _, tt := range tests {
		data := make([]byte, int(tt.count)*4*2)
		input := buildVecInput(tt.opcode, VWidth32, tt.count, data)
		gas := p.RequiredGas(input)
		if gas != tt.wantGas {
			t.Errorf("opcode 0x%02x count=%d: gas=%d, want=%d", tt.opcode, tt.count, gas, tt.wantGas)
		}
	}
}

func TestVectorizedVREDUCE64(t *testing.T) {
	data := encodeU64s(100, 200, 300)
	input := buildVecInput(VOpREDUCE, VWidth64, 3, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VREDUCE64: %v", err)
	}

	got := binary.BigEndian.Uint64(out)
	if got != 600 {
		t.Errorf("VREDUCE64: got %d, want 600", got)
	}
}

func TestVectorizedVDOT64(t *testing.T) {
	// [1,2,3] . [4,5,6] = 4+10+18 = 32
	data := encodeU64s(1, 2, 3, 4, 5, 6)
	input := buildVecInput(VOpDOT, VWidth64, 3, data)

	p := &vectorizedPrecompile{}
	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("VDOT64: %v", err)
	}

	got := binary.BigEndian.Uint64(out)
	if got != 32 {
		t.Errorf("VDOT64: got %d, want 32", got)
	}
}
