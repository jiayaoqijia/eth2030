package vm

import (
	"math/big"
	"math/bits"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

func TestNTTMatchesLegacyEvaluationOrder(t *testing.T) {
	// NTT of f(x) = x enumerates the roots in the deployed evaluation order.
	for _, op := range []byte{NTTOpForward, NTTOpGoldilocks} {
		field, generator := bn254ScalarField, int64(5)
		if op == NTTOpGoldilocks {
			field, generator = goldilocksField, 7
		}
		input := make([]byte, 1+8*32)
		input[0], input[64] = op, 1
		output, err := (&nttPrecompile{}).Run(input)
		if err != nil {
			t.Fatal(err)
		}
		exponent := new(big.Int).Sub(field, big.NewInt(1))
		exponent.Rsh(exponent, 3)
		root := new(big.Int).Exp(big.NewInt(generator), exponent, field)
		for i := range 8 {
			want := new(big.Int).Exp(root, big.NewInt(int64(i)), field)
			got := new(big.Int).SetBytes(output[i*32 : (i+1)*32])
			if got.Cmp(want) != 0 {
				t.Fatalf("op %d evaluation %d = %s, want %s", op, i, got, want)
			}
		}
	}
}

func TestNTTForwardInverseRoundtrip(t *testing.T) {
	// [1, 2, 3, 4] -> forward NTT -> inverse NTT -> [1, 2, 3, 4]
	coeffs := []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4)}
	fwd := bn254NTT(coeffs, false)
	inv := bn254NTT(fwd, true)

	for i, v := range inv {
		if v.Cmp(coeffs[i]) != 0 {
			t.Errorf("roundtrip[%d]: got %v, want %v", i, v, coeffs[i])
		}
	}
}

func TestNTTPolynomialMultiplication(t *testing.T) {
	// Multiply polynomials a(x) = 1 + 2x and b(x) = 3 + 4x
	// Product: 3 + 10x + 8x^2
	// Use size 4 (next power of two for length 3 result).
	a := []*big.Int{big.NewInt(1), big.NewInt(2), big.NewInt(0), big.NewInt(0)}
	b := []*big.Int{big.NewInt(3), big.NewInt(4), big.NewInt(0), big.NewInt(0)}

	fa := bn254NTT(a, false)
	fb := bn254NTT(b, false)

	// Pointwise multiply.
	fc := make([]*big.Int, 4)
	for i := range fc {
		fc[i] = new(big.Int).Mul(fa[i], fb[i])
		fc[i].Mod(fc[i], bn254ScalarField)
	}

	result := bn254NTT(fc, true)

	expected := []*big.Int{big.NewInt(3), big.NewInt(10), big.NewInt(8), big.NewInt(0)}
	for i, v := range result {
		if v.Cmp(expected[i]) != 0 {
			t.Errorf("convolution[%d]: got %v, want %v", i, v, expected[i])
		}
	}
}

func TestNTTPrecompileForwardInverse(t *testing.T) {
	p := &nttPrecompile{}

	// Build input: op_type(0x00) || 4 coefficients [1, 2, 3, 4] as 32-byte big-endian.
	coeffs := []int64{1, 2, 3, 4}
	input := make([]byte, 1+4*32)
	input[0] = 0x00 // forward
	for i, c := range coeffs {
		b := big.NewInt(c).Bytes()
		copy(input[1+i*32+(32-len(b)):], b)
	}

	fwdOut, err := p.Run(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(fwdOut) != 4*32 {
		t.Fatalf("expected %d bytes output, got %d", 4*32, len(fwdOut))
	}

	// Now inverse: op_type(0x01) || forward output.
	invInput := make([]byte, 1+len(fwdOut))
	invInput[0] = 0x01 // inverse
	copy(invInput[1:], fwdOut)

	invOut, err := p.Run(invInput)
	if err != nil {
		t.Fatal(err)
	}

	// Verify roundtrip.
	for i, c := range coeffs {
		val := new(big.Int).SetBytes(invOut[i*32 : (i+1)*32])
		if val.Cmp(big.NewInt(c)) != 0 {
			t.Errorf("precompile roundtrip[%d]: got %v, want %d", i, val, c)
		}
	}
}

func TestNTTPrecompileGasCost(t *testing.T) {
	p := &nttPrecompile{}

	tests := []struct {
		name    string
		nElems  int
		wantGas uint64
	}{
		{"4 elements", 4, NTTBaseCost + 4*2*NTTPerElementCost},    // log2(4) = 2
		{"8 elements", 8, NTTBaseCost + 8*3*NTTPerElementCost},    // log2(8) = 3
		{"16 elements", 16, NTTBaseCost + 16*4*NTTPerElementCost}, // log2(16) = 4
		{"1 element", 1, NTTBaseCost + 1*1*NTTPerElementCost},     // log2(1) = 0 -> clamped to 1
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := make([]byte, 1+tt.nElems*32)
			input[0] = 0x00
			got := p.RequiredGas(input)
			if got != tt.wantGas {
				t.Errorf("gas for %d elements: got %d, want %d", tt.nElems, got, tt.wantGas)
			}
		})
	}
}

func TestNTTPrecompileMaxSize(t *testing.T) {
	p := &nttPrecompile{}

	// Create input exceeding max degree.
	n := NTTMaxDegree + 1
	input := make([]byte, 1+n*32)
	input[0] = 0x00

	_, err := p.Run(input)
	if err != ErrNTTTooLarge {
		t.Errorf("expected ErrNTTTooLarge, got %v", err)
	}
}

func TestNTTPrecompileNotPowerOfTwo(t *testing.T) {
	p := &nttPrecompile{}

	// 3 elements = not a power of two.
	input := make([]byte, 1+3*32)
	input[0] = 0x00

	_, err := p.Run(input)
	if err != ErrNTTNotPowerOfTwo {
		t.Errorf("expected ErrNTTNotPowerOfTwo, got %v", err)
	}
}

func TestNTTPrecompileInvalidOpType(t *testing.T) {
	p := &nttPrecompile{}

	input := make([]byte, 1+4*32)
	input[0] = 0x04 // invalid (opType 0-3 are valid: BN254 fwd/inv, Goldilocks fwd/inv)

	_, err := p.Run(input)
	if err != ErrNTTInvalidOpType {
		t.Errorf("expected ErrNTTInvalidOpType, got %v", err)
	}
}

func TestNTTPrecompileEmptyInput(t *testing.T) {
	p := &nttPrecompile{}

	_, err := p.Run(nil)
	if err != ErrNTTInvalidInput {
		t.Errorf("expected ErrNTTInvalidInput, got %v", err)
	}

	_, err = p.Run([]byte{0x00})
	if err != ErrNTTInvalidInput {
		t.Errorf("expected ErrNTTInvalidInput for zero coefficients, got %v", err)
	}
}

func TestNTTPrecompileRegisteredIPlus(t *testing.T) {
	// Split NTT precompiles are at 0x0f-0x14.
	addr := types.BytesToAddress([]byte{0x0f})
	p, ok := PrecompiledContractsIPlus[addr]
	if !ok {
		t.Fatal("NTT FW precompile not found at address 0x0f in I+ fork")
	}
	if p == nil {
		t.Fatal("NTT FW precompile is nil")
	}
}

func TestNTTPrecompileViaRunPrecompiled(t *testing.T) {
	// Test through the registry for I+ fork (split FW precompile at 0x0f).
	addr := types.BytesToAddress([]byte{0x0f})
	p, ok := PrecompiledContractsIPlus[addr]
	if !ok {
		t.Skip("NTT FW precompile not in I+ fork")
	}

	// Forward NTT on [1, 0, 0, 0] should give [1, 1, 1, 1].
	// Split precompiles have no opType byte prefix.
	input := make([]byte, 4*32)
	input[31] = 1 // coefficients[0] = 1 (big-endian, last byte of first 32-byte chunk)

	gas := p.RequiredGas(input)
	out, err := p.Run(input)
	if err != nil {
		t.Fatal(err)
	}
	if gas == 0 {
		t.Error("gas should be non-zero")
	}

	// NTT of [1, 0, 0, 0] evaluates the constant polynomial 1 at 4 roots
	// of unity, yielding [1, 1, 1, 1].
	for i := 0; i < 4; i++ {
		val := new(big.Int).SetBytes(out[i*32 : (i+1)*32])
		if val.Cmp(big.NewInt(1)) != 0 {
			t.Errorf("NTT([1,0,0,0])[%d] = %v, want 1", i, val)
		}
	}
}

func TestNTTSize8(t *testing.T) {
	// Test with a larger size (8 elements).
	n := 8
	coeffs := make([]*big.Int, n)
	for i := range coeffs {
		coeffs[i] = big.NewInt(int64(i + 1))
	}

	fwd := bn254NTT(coeffs, false)
	inv := bn254NTT(fwd, true)

	for i, v := range inv {
		if v.Cmp(coeffs[i]) != 0 {
			t.Errorf("roundtrip[%d]: got %v, want %v", i, v, coeffs[i])
		}
	}
}

func TestBitReverse(t *testing.T) {
	tests := []struct {
		v, numBits, want int
	}{
		{0, 2, 0},
		{1, 2, 2},
		{2, 2, 1},
		{3, 2, 3},
		{0, 3, 0},
		{1, 3, 4},
		{4, 3, 1},
		{7, 3, 7},
	}
	for _, tt := range tests {
		got := bitReverse(tt.v, tt.numBits)
		if got != tt.want {
			t.Errorf("bitReverse(%d, %d) = %d, want %d", tt.v, tt.numBits, got, tt.want)
		}
	}
}

func TestNTTPrecompileSize1(t *testing.T) {
	// n=1: forward NTT of a single element should return that element.
	p := &nttPrecompile{}
	input := make([]byte, 1+1*32)
	input[0] = 0x00
	input[32] = 42 // last byte of 32-byte big-endian

	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("Run(n=1): %v", err)
	}
	val := new(big.Int).SetBytes(out[0:32])
	if val.Cmp(big.NewInt(42)) != 0 {
		t.Errorf("NTT([42]) = %v, want 42", val)
	}
}

func TestNTTPrecompileZeroInputs(t *testing.T) {
	// All-zero input should produce all-zero output.
	p := &nttPrecompile{}
	input := make([]byte, 1+4*32)
	input[0] = 0x00 // forward

	out, err := p.Run(input)
	if err != nil {
		t.Fatalf("Run(zeros): %v", err)
	}
	for i := 0; i < 4; i++ {
		val := new(big.Int).SetBytes(out[i*32 : (i+1)*32])
		if val.Sign() != 0 {
			t.Errorf("NTT(zeros)[%d] = %v, want 0", i, val)
		}
	}
}

func TestNTTRequiredGas_OverflowProtection(t *testing.T) {
	p := &nttPrecompile{}

	// The gas function should handle large inputs gracefully (no panic).
	// RequiredGas with very large input should not underflow.
	input := make([]byte, 1) // just 1 byte, no coefficients
	gas := p.RequiredGas(input)
	if gas != NTTBaseCost {
		t.Errorf("gas for 0 coeff: got %d, want %d", gas, NTTBaseCost)
	}
}

func TestNTTGasCalculation(t *testing.T) {
	p := &nttPrecompile{}

	// Verify empty input.
	if g := p.RequiredGas(nil); g != 0 {
		t.Errorf("gas for nil input: got %d, want 0", g)
	}

	// Verify single byte (no coefficients).
	if g := p.RequiredGas([]byte{0x00}); g != NTTBaseCost {
		t.Errorf("gas for 0 elements: got %d, want %d", g, NTTBaseCost)
	}

	// Verify scaling: 256 elements.
	n := 256
	input := make([]byte, 1+n*32)
	log2n := uint64(bits.Len(uint(n)) - 1)
	expected := NTTBaseCost + uint64(n)*log2n*NTTPerElementCost
	if g := p.RequiredGas(input); g != expected {
		t.Errorf("gas for %d elements: got %d, want %d", n, g, expected)
	}
}

func TestValidateNTTInput(t *testing.T) {
	// Empty input.
	if err := ValidateNTTInput(nil); err == nil {
		t.Fatal("expected error for empty input")
	}

	// Invalid op type.
	if err := ValidateNTTInput([]byte{0x05}); err == nil {
		t.Fatal("expected error for invalid op type")
	}

	// Valid forward NTT with 4 elements (power of two).
	input := make([]byte, 1+4*32)
	input[0] = 0 // forward
	if err := ValidateNTTInput(input); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Non-power-of-two element count (3).
	input2 := make([]byte, 1+3*32)
	input2[0] = 0
	if err := ValidateNTTInput(input2); err == nil {
		t.Fatal("expected error for non-power-of-two elements")
	}
}

func TestGoldilocksNTT(t *testing.T) {
	// Test with small polynomial: [1, 2, 3, 4]
	coeffs := []*big.Int{
		big.NewInt(1), big.NewInt(2), big.NewInt(3), big.NewInt(4),
	}

	result, err := GoldilocksNTT(coeffs)
	if err != nil {
		t.Fatal(err)
	}

	if len(result) != 4 {
		t.Fatalf("expected 4 results, got %d", len(result))
	}

	// Verify all results are in range [0, p).
	for i, r := range result {
		if r.Sign() < 0 || r.Cmp(goldilocksField) >= 0 {
			t.Errorf("result[%d] = %s out of Goldilocks field range", i, r.String())
		}
	}
}

func TestGoldilocksNTTRoundTrip(t *testing.T) {
	// NTT then INTT should return original values.
	coeffs := []*big.Int{
		big.NewInt(10), big.NewInt(20), big.NewInt(30), big.NewInt(40),
	}

	nttResult, err := GoldilocksNTT(coeffs)
	if err != nil {
		t.Fatal(err)
	}

	recovered, err := GoldilocksINTT(nttResult)
	if err != nil {
		t.Fatal(err)
	}

	if len(recovered) != len(coeffs) {
		t.Fatalf("length mismatch: %d vs %d", len(recovered), len(coeffs))
	}

	for i := range coeffs {
		expected := new(big.Int).Mod(coeffs[i], goldilocksField)
		if recovered[i].Cmp(expected) != 0 {
			t.Errorf("coefficient[%d]: got %s, want %s", i, recovered[i].String(), expected.String())
		}
	}
}

func TestGoldilocksField(t *testing.T) {
	// Verify p = 2^64 - 2^32 + 1
	expected := new(big.Int).SetUint64(18446744069414584321)
	if goldilocksField.Cmp(expected) != 0 {
		t.Errorf("Goldilocks field mismatch: got %s, want %s", goldilocksField.String(), expected.String())
	}

	// Verify p is prime (probabilistic).
	if !goldilocksField.ProbablyPrime(20) {
		t.Error("Goldilocks field modulus should be prime")
	}
}

func TestNTTPrecompileGoldilocksOp(t *testing.T) {
	// Test the precompile with opType 2 (Goldilocks forward NTT).
	c := &nttPrecompile{}

	// Build input: opType(1 byte) + 4 coefficients (4 * 32 bytes).
	input := make([]byte, 1+4*32)
	input[0] = NTTOpGoldilocks
	// Set coefficient 0 = 1.
	input[32] = 1
	// Set coefficient 1 = 2.
	input[64] = 2
	// Set coefficient 2 = 3.
	input[96] = 3
	// Set coefficient 3 = 4.
	input[128] = 4

	result, err := c.Run(input)
	if err != nil {
		t.Fatal(err)
	}

	// Result should be 4 * 32 bytes.
	if len(result) != 4*32 {
		t.Errorf("expected %d bytes, got %d", 4*32, len(result))
	}
}
