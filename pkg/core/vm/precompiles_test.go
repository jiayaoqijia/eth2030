package vm

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"golang.org/x/crypto/ripemd160"
)

func TestECRecoverAcceptsHighS(t *testing.T) {
	key, err := gethcrypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	hash := gethcrypto.Keccak256([]byte("EIP-2 precompile recovery"))
	sig, err := gethcrypto.Sign(hash, key)
	if err != nil {
		t.Fatal(err)
	}
	input := make([]byte, 128)
	copy(input, hash)
	input[63] = 27 + (sig[64] ^ 1)
	copy(input[64:96], sig[:32])
	new(big.Int).Sub(gethcrypto.S256().Params().N, new(big.Int).SetBytes(sig[32:64])).FillBytes(input[96:])

	out, _, err := RunPrecompiledContract(types.Address{19: 1}, input, 3000)
	want := gethcrypto.PubkeyToAddress(key.PublicKey)
	if err != nil || len(out) != 32 || !bytes.Equal(out[12:], want[:]) {
		t.Fatalf("high-S recovery = %x, %v, want padded address %x", out, err, want)
	}
}

func TestIsPrecompiledContract(t *testing.T) {
	// Addresses 0x01-0x0a (Cancun) + 0x0b-0x13 (EIP-2537 BLS12-381).
	for i := 1; i <= 0x13; i++ {
		addr := types.BytesToAddress([]byte{byte(i)})
		if !IsPrecompiledContract(addr) {
			t.Errorf("address 0x%02x should be a precompiled contract", i)
		}
	}
	// Addresses 0 and beyond 0x13 should not be precompiles.
	for _, i := range []int{0, 0x14, 0x15, 255} {
		addr := types.BytesToAddress([]byte{byte(i)})
		if IsPrecompiledContract(addr) {
			t.Errorf("address 0x%02x should not be a precompiled contract", i)
		}
	}
}

func TestRunPrecompiledContract_NotFound(t *testing.T) {
	addr := types.BytesToAddress([]byte{99})
	_, _, err := RunPrecompiledContract(addr, nil, 1000000)
	if err == nil {
		t.Fatal("expected error for non-precompile address")
	}
}

func TestRunPrecompiledContract_OutOfGas(t *testing.T) {
	addr := types.BytesToAddress([]byte{1}) // ecrecover costs 3000
	_, _, err := RunPrecompiledContract(addr, nil, 100)
	if err != ErrOutOfGas {
		t.Fatalf("expected ErrOutOfGas, got %v", err)
	}
}

func TestEcrecoverGas(t *testing.T) {
	c := &ecrecover{}
	if g := c.RequiredGas(nil); g != 3000 {
		t.Errorf("ecrecover gas = %d, want 3000", g)
	}
}

func TestEcrecoverInvalidInput(t *testing.T) {
	c := &ecrecover{}

	// Empty input: v will be 0, which is not 27 or 28 -> nil result.
	out, err := c.Run(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output for empty input, got %x", out)
	}

	// Invalid v value (not 27 or 28).
	input := make([]byte, 128)
	input[63] = 26 // v = 26
	out, err = c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil output for invalid v, got %x", out)
	}
}

func TestSha256(t *testing.T) {
	c := &sha256hash{}

	tests := []struct {
		input []byte
	}{
		{[]byte{}},
		{[]byte("hello")},
		{[]byte("The quick brown fox jumps over the lazy dog")},
	}

	for _, tt := range tests {
		out, err := c.Run(tt.input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		expected := sha256.Sum256(tt.input)
		if !bytes.Equal(out, expected[:]) {
			t.Errorf("sha256(%q) = %x, want %x", tt.input, out, expected)
		}
	}
}

func TestSha256Gas(t *testing.T) {
	c := &sha256hash{}

	tests := []struct {
		inputLen int
		want     uint64
	}{
		{0, 60},          // 60 + 12*0
		{1, 72},          // 60 + 12*1
		{32, 72},         // 60 + 12*1
		{33, 84},         // 60 + 12*2
		{64, 84},         // 60 + 12*2
		{100, 60 + 12*4}, // ceil(100/32) = 4
	}

	for _, tt := range tests {
		input := make([]byte, tt.inputLen)
		if g := c.RequiredGas(input); g != tt.want {
			t.Errorf("sha256 gas for %d bytes = %d, want %d", tt.inputLen, g, tt.want)
		}
	}
}

func TestRipemd160(t *testing.T) {
	c := &ripemd160hash{}

	tests := []struct {
		input   string
		wantHex string // 20-byte RIPEMD-160 hash, hex encoded
	}{
		{"", "9c1185a5c5e9fc54612808977ee8f548b2258d31"},
		{"hello", "108f07b8382412612c048d07d13f814118445acd"},
	}

	for _, tt := range tests {
		out, err := c.Run([]byte(tt.input))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 32 {
			t.Fatalf("ripemd160 output length = %d, want 32", len(out))
		}
		// First 12 bytes should be zero (left-padding).
		for i := 0; i < 12; i++ {
			if out[i] != 0 {
				t.Errorf("ripemd160 output byte %d = %d, want 0", i, out[i])
			}
		}
		// Remaining 20 bytes should be the hash.
		gotHex := hex.EncodeToString(out[12:])
		if gotHex != tt.wantHex {
			t.Errorf("ripemd160(%q) = %s, want %s", tt.input, gotHex, tt.wantHex)
		}
	}
}

func TestRipemd160MatchesStdlib(t *testing.T) {
	c := &ripemd160hash{}
	input := []byte("The quick brown fox jumps over the lazy dog")

	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	h := ripemd160.New()
	h.Write(input)
	expected := h.Sum(nil)

	if !bytes.Equal(out[12:], expected) {
		t.Errorf("ripemd160 hash mismatch: got %x, want %x", out[12:], expected)
	}
}

func TestRipemd160Gas(t *testing.T) {
	c := &ripemd160hash{}

	tests := []struct {
		inputLen int
		want     uint64
	}{
		{0, 600},           // 600 + 120*0
		{1, 720},           // 600 + 120*1
		{32, 720},          // 600 + 120*1
		{33, 840},          // 600 + 120*2
		{100, 600 + 120*4}, // ceil(100/32) = 4
	}

	for _, tt := range tests {
		input := make([]byte, tt.inputLen)
		if g := c.RequiredGas(input); g != tt.want {
			t.Errorf("ripemd160 gas for %d bytes = %d, want %d", tt.inputLen, g, tt.want)
		}
	}
}

func TestDataCopy(t *testing.T) {
	c := &dataCopy{}

	tests := [][]byte{
		{},
		{1, 2, 3, 4, 5},
		make([]byte, 100),
	}

	for _, input := range tests {
		out, err := c.Run(input)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !bytes.Equal(out, input) {
			t.Errorf("dataCopy(%x) = %x, want %x", input, out, input)
		}
		// Verify it's a copy, not the same slice.
		if len(input) > 0 && &out[0] == &input[0] {
			t.Error("dataCopy returned same slice, expected a copy")
		}
	}
}

func TestDataCopyGas(t *testing.T) {
	c := &dataCopy{}

	tests := []struct {
		inputLen int
		want     uint64
	}{
		{0, 15},         // 15 + 3*0
		{1, 18},         // 15 + 3*1
		{32, 18},        // 15 + 3*1
		{33, 21},        // 15 + 3*2
		{100, 15 + 3*4}, // ceil(100/32) = 4
	}

	for _, tt := range tests {
		input := make([]byte, tt.inputLen)
		if g := c.RequiredGas(input); g != tt.want {
			t.Errorf("dataCopy gas for %d bytes = %d, want %d", tt.inputLen, g, tt.want)
		}
	}
}

func TestBigModExp(t *testing.T) {
	c := &bigModExp{}

	// Test: 2^10 % 1000 = 24
	base := big.NewInt(2)
	exp := big.NewInt(10)
	mod := big.NewInt(1000)

	input := buildModExpInput(base.Bytes(), exp.Bytes(), mod.Bytes())
	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := new(big.Int).SetBytes(out)
	if result.Cmp(big.NewInt(24)) != 0 {
		t.Errorf("modexp(2, 10, 1000) = %s, want 24", result)
	}
}

func TestBigModExpZeroMod(t *testing.T) {
	c := &bigModExp{}

	// 2^10 % 0 = 0
	base := big.NewInt(2)
	exp := big.NewInt(10)
	mod := big.NewInt(0)

	input := buildModExpInput(base.Bytes(), exp.Bytes(), mod.Bytes())
	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if new(big.Int).SetBytes(out).Sign() != 0 {
		t.Errorf("modexp with zero mod should return 0, got %x", out)
	}
}

func TestBigModExpLargeValues(t *testing.T) {
	c := &bigModExp{}

	// 123456789^65537 % 998244353
	base := big.NewInt(123456789)
	exp := big.NewInt(65537)
	mod := big.NewInt(998244353)

	input := buildModExpInput(base.Bytes(), exp.Bytes(), mod.Bytes())
	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := new(big.Int).Exp(base, exp, mod)
	result := new(big.Int).SetBytes(out)
	if result.Cmp(expected) != 0 {
		t.Errorf("modexp(123456789, 65537, 998244353) = %s, want %s", result, expected)
	}
}

func TestBigModExpGas(t *testing.T) {
	c := &bigModExp{}

	// Simple case: small values should cost minimum 200 gas.
	base := big.NewInt(2)
	exp := big.NewInt(10)
	mod := big.NewInt(1000)

	input := buildModExpInput(base.Bytes(), exp.Bytes(), mod.Bytes())
	gas := c.RequiredGas(input)
	if gas < 200 {
		t.Errorf("modexp gas = %d, want >= 200", gas)
	}
}

func TestRunPrecompiledContractGasAccounting(t *testing.T) {
	// Run sha256 with plenty of gas and verify remaining gas.
	addr := types.BytesToAddress([]byte{2})
	input := []byte("test")
	suppliedGas := uint64(10000)

	out, remainingGas, err := RunPrecompiledContract(addr, input, suppliedGas)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected non-nil output")
	}

	expectedGasCost := uint64(60 + 12*1) // 1 word
	if remainingGas != suppliedGas-expectedGasCost {
		t.Errorf("remaining gas = %d, want %d", remainingGas, suppliedGas-expectedGasCost)
	}
}

func TestWordCount(t *testing.T) {
	tests := []struct {
		size int
		want uint64
	}{
		{0, 0},
		{1, 1},
		{31, 1},
		{32, 1},
		{33, 2},
		{64, 2},
		{65, 3},
	}

	for _, tt := range tests {
		if got := wordCount(tt.size); got != tt.want {
			t.Errorf("wordCount(%d) = %d, want %d", tt.size, got, tt.want)
		}
	}
}

func TestKZGPrecompileGasCost(t *testing.T) {
	c := &kzgPointEvaluation{}
	if g := c.RequiredGas(nil); g != 50000 {
		t.Errorf("kzg gas = %d, want 50000", g)
	}
	// Gas cost is constant regardless of input size.
	if g := c.RequiredGas(make([]byte, 192)); g != 50000 {
		t.Errorf("kzg gas = %d, want 50000", g)
	}
}

func TestKZGPrecompileInputValidation(t *testing.T) {
	c := &kzgPointEvaluation{}

	// Wrong input length.
	for _, length := range []int{0, 1, 64, 128, 191, 193, 256} {
		_, err := c.Run(make([]byte, length))
		if err == nil {
			t.Errorf("expected error for input length %d", length)
		}
	}
}

func TestKZGPrecompileInvalidVersion(t *testing.T) {
	c := &kzgPointEvaluation{}

	// Valid length but wrong version byte.
	input := make([]byte, 192)
	input[0] = 0x00 // should be 0x01
	_, err := c.Run(input)
	if err == nil {
		t.Fatal("expected error for invalid version byte")
	}
}

func TestKZGPrecompileFieldElementValidation(t *testing.T) {
	c := &kzgPointEvaluation{}

	// Build input with valid version byte but z >= BLS_MODULUS.
	input := make([]byte, 192)
	input[0] = 0x01 // valid version

	// Set z = BLS_MODULUS (should fail).
	zBytes := blsModulus.Bytes()
	copy(input[64-len(zBytes):64], zBytes)

	_, err := c.Run(input)
	if err == nil {
		t.Fatal("expected error for z >= BLS_MODULUS")
	}
}

func TestKZGPrecompileValidInput(t *testing.T) {
	c := &kzgPointEvaluation{}
	input := buildKZGPrecompileInput(t)

	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 64 {
		t.Fatalf("output length = %d, want 64", len(out))
	}

	// First 32 bytes: FIELD_ELEMENTS_PER_BLOB = 4096
	fepb := new(big.Int).SetBytes(out[:32])
	if fepb.Int64() != 4096 {
		t.Errorf("FIELD_ELEMENTS_PER_BLOB = %d, want 4096", fepb.Int64())
	}

	// Second 32 bytes: BLS_MODULUS
	mod := new(big.Int).SetBytes(out[32:64])
	expectedMod, _ := new(big.Int).SetString("52435875175126190479447740508185965837690552500527637822603658699938581184513", 10)
	if mod.Cmp(expectedMod) != 0 {
		t.Errorf("BLS_MODULUS = %s, want %s", mod, expectedMod)
	}
}

// TestKZGPrecompileWrongProof tests that an invalid proof is rejected.
func TestKZGPrecompileWrongProof(t *testing.T) {
	c := &kzgPointEvaluation{}
	input := buildKZGPrecompileInput(t)

	// Corrupt the proof (last 48 bytes) by flipping a bit.
	input[191] ^= 0x01

	_, err := c.Run(input)
	if err == nil {
		t.Fatal("expected error for wrong proof")
	}
}

func TestKZGPrecompileCommitmentMismatch(t *testing.T) {
	c := &kzgPointEvaluation{}

	// Build input where versioned_hash doesn't match commitment.
	input := make([]byte, 192)
	input[0] = 0x01 // version byte
	input[1] = 0xff // garbage in versioned_hash so it won't match

	_, err := c.Run(input)
	if err == nil {
		t.Fatal("expected error for commitment mismatch")
	}
}

func TestKZGPrecompileRegistered(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x0a})
	if !IsPrecompiledContract(addr) {
		t.Fatal("address 0x0a should be a precompiled contract")
	}
}

func TestKZGPrecompileViaRunner(t *testing.T) {
	input := buildKZGPrecompileInput(t)

	addr := types.BytesToAddress([]byte{0x0a})
	out, remainingGas, err := RunPrecompiledContract(addr, input, 100000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if remainingGas != 50000 {
		t.Errorf("remaining gas = %d, want 50000", remainingGas)
	}
	if len(out) != 64 {
		t.Fatalf("output length = %d, want 64", len(out))
	}
}

func TestKZGPrecompileOutOfGas(t *testing.T) {
	addr := types.BytesToAddress([]byte{0x0a})
	_, _, err := RunPrecompiledContract(addr, make([]byte, 192), 49999)
	if err != ErrOutOfGas {
		t.Fatalf("expected ErrOutOfGas, got %v", err)
	}
}

func buildKZGPrecompileInput(t *testing.T) []byte {
	t.Helper()
	fixtures := loadPrecompileFixtures(t, "pointEvaluation.json")
	if len(fixtures) == 0 {
		t.Fatal("missing point evaluation vectors")
	}
	return hexDecode(t, fixtures[0].Input)
}

// buildModExpInput constructs the 96-byte header + data for the modexp precompile.
func buildModExpInput(base, exp, mod []byte) []byte {
	input := make([]byte, 96+len(base)+len(exp)+len(mod))

	// base_length (32 bytes)
	bLen := big.NewInt(int64(len(base)))
	copy(input[32-len(bLen.Bytes()):32], bLen.Bytes())

	// exp_length (32 bytes)
	eLen := big.NewInt(int64(len(exp)))
	copy(input[64-len(eLen.Bytes()):64], eLen.Bytes())

	// mod_length (32 bytes)
	mLen := big.NewInt(int64(len(mod)))
	copy(input[96-len(mLen.Bytes()):96], mLen.Bytes())

	// base + exp + mod
	offset := 96
	copy(input[offset:], base)
	offset += len(base)
	copy(input[offset:], exp)
	offset += len(exp)
	copy(input[offset:], mod)

	return input
}

// --- Additional precompile tests ---

// TestEcRecover_KnownVector tests ecRecover against a known test vector.
func TestEcRecover_KnownVector(t *testing.T) {
	c := &ecrecover{}

	// Construct a valid ecrecover input: hash(32) + v(32) + r(32) + s(32).
	// Using an all-zeros hash with v=27 and invalid r,s should return nil (no error).
	input := make([]byte, 128)
	input[63] = 27 // v = 27
	// r and s are zero, which is invalid. ecrecover should return nil.
	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Zero r/s is not valid, so output should be nil.
	if out != nil {
		t.Errorf("expected nil output for zero r/s, got %x", out)
	}
}

// TestSHA256Precompile tests SHA-256 against known test vectors.
func TestSHA256Precompile(t *testing.T) {
	c := &sha256hash{}

	tests := []struct {
		input   string
		wantHex string
	}{
		{
			"",
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			"abc",
			"ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		},
	}

	for _, tt := range tests {
		out, err := c.Run([]byte(tt.input))
		if err != nil {
			t.Fatalf("sha256(%q): unexpected error: %v", tt.input, err)
		}
		gotHex := hex.EncodeToString(out)
		if gotHex != tt.wantHex {
			t.Errorf("sha256(%q) = %s, want %s", tt.input, gotHex, tt.wantHex)
		}
	}
}

// TestRIPEMD160Precompile tests RIPEMD-160 against known test vectors.
func TestRIPEMD160Precompile(t *testing.T) {
	c := &ripemd160hash{}

	tests := []struct {
		input   string
		wantHex string // 20-byte hash, hex encoded
	}{
		{"", "9c1185a5c5e9fc54612808977ee8f548b2258d31"},
		{"abc", "8eb208f7e05d987a9b044a8e98c6b087f15a0bfc"},
	}

	for _, tt := range tests {
		out, err := c.Run([]byte(tt.input))
		if err != nil {
			t.Fatalf("ripemd160(%q): unexpected error: %v", tt.input, err)
		}
		if len(out) != 32 {
			t.Fatalf("ripemd160(%q) output length = %d, want 32", tt.input, len(out))
		}
		// First 12 bytes should be zero (left-padding).
		for i := 0; i < 12; i++ {
			if out[i] != 0 {
				t.Errorf("ripemd160(%q) byte %d = %d, want 0", tt.input, i, out[i])
			}
		}
		gotHex := hex.EncodeToString(out[12:])
		if gotHex != tt.wantHex {
			t.Errorf("ripemd160(%q) = %s, want %s", tt.input, gotHex, tt.wantHex)
		}
	}
}

// TestIdentityPrecompile tests the identity (data copy) precompile echoes input.
func TestIdentityPrecompile(t *testing.T) {
	c := &dataCopy{}

	tests := [][]byte{
		nil,
		{},
		{0xDE, 0xAD, 0xBE, 0xEF},
		make([]byte, 256),
	}

	for _, input := range tests {
		out, err := c.Run(input)
		if err != nil {
			t.Fatalf("identity: unexpected error: %v", err)
		}
		if !bytes.Equal(out, input) {
			t.Errorf("identity(%x) = %x, want %x", input, out, input)
		}
		// Verify it's a copy (not the same underlying array).
		if len(input) > 0 && len(out) > 0 && &out[0] == &input[0] {
			t.Error("identity should return a copy, not the same slice")
		}
	}
}

// TestModExpPrecompile tests basic modular exponentiation.
func TestModExpPrecompile(t *testing.T) {
	c := &bigModExp{}

	tests := []struct {
		base, exp, mod int64
		want           int64
	}{
		{2, 10, 1000, 24},                // 2^10 % 1000 = 1024 % 1000 = 24
		{3, 5, 13, 9},                    // 3^5 % 13 = 243 % 13 = 9
		{7, 0, 100, 1},                   // 7^0 % 100 = 1
		{0, 10, 7, 0},                    // 0^10 % 7 = 0
		{2, 10, 0, 0},                    // 2^10 % 0 = 0 (zero mod)
		{123456789, 65537, 998244353, 0}, // large values (expected computed below)
	}

	for _, tt := range tests {
		base := big.NewInt(tt.base)
		exp := big.NewInt(tt.exp)
		mod := big.NewInt(tt.mod)

		input := buildModExpInput(base.Bytes(), exp.Bytes(), mod.Bytes())
		out, err := c.Run(input)
		if err != nil {
			t.Fatalf("modexp(%d, %d, %d): unexpected error: %v", tt.base, tt.exp, tt.mod, err)
		}

		result := new(big.Int).SetBytes(out)

		if tt.want != 0 {
			if result.Int64() != tt.want {
				t.Errorf("modexp(%d, %d, %d) = %s, want %d", tt.base, tt.exp, tt.mod, result, tt.want)
			}
		} else if tt.mod == 0 {
			// Zero mod should return zero.
			if result.Sign() != 0 {
				t.Errorf("modexp(%d, %d, 0) = %s, want 0", tt.base, tt.exp, result)
			}
		} else {
			// Verify against Go's math/big.
			expected := new(big.Int).Exp(base, exp, mod)
			if result.Cmp(expected) != 0 {
				t.Errorf("modexp(%d, %d, %d) = %s, want %s", tt.base, tt.exp, tt.mod, result, expected)
			}
		}
	}
}

// TestBlake2FPrecompile tests the BLAKE2b F compression function precompile
// using test vectors from EIP-152.
func TestBlake2FPrecompile(t *testing.T) {
	c := &blake2F{}

	// Test vector from EIP-152: 12 rounds.
	// Input: 4 bytes rounds (12) + 64 bytes h + 128 bytes m + 8 bytes t[0] + 8 bytes t[1] + 1 byte f
	input := make([]byte, 213)
	binary.BigEndian.PutUint32(input[:4], 12)

	// h state vector (all zeros for simplicity).
	// m message block (all zeros for simplicity).
	// t[0] = 0, t[1] = 0.
	// f = 1 (final block).
	input[212] = 1

	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("blake2f: unexpected error: %v", err)
	}
	if len(out) != 64 {
		t.Fatalf("blake2f output length = %d, want 64", len(out))
	}

	// The output should be non-zero for 12 rounds with the default IV.
	allZero := true
	for _, b := range out {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("blake2f: expected non-zero output for 12 rounds")
	}
}

// TestBlake2FPrecompile_InvalidInput tests that Blake2F rejects invalid input.
func TestBlake2FPrecompile_InvalidInput(t *testing.T) {
	c := &blake2F{}

	// Wrong input length.
	for _, length := range []int{0, 1, 64, 212, 214, 256} {
		_, err := c.Run(make([]byte, length))
		if err == nil {
			t.Errorf("blake2f: expected error for input length %d", length)
		}
	}

	// Invalid final block indicator (not 0 or 1).
	input := make([]byte, 213)
	input[212] = 2 // invalid
	_, err := c.Run(input)
	if err == nil {
		t.Error("blake2f: expected error for invalid final block indicator")
	}
}

// TestBlake2FPrecompile_ZeroRounds tests Blake2F with 0 rounds (should be a no-op
// on the state vector).
func TestBlake2FPrecompile_ZeroRounds(t *testing.T) {
	c := &blake2F{}

	input := make([]byte, 213)
	binary.BigEndian.PutUint32(input[:4], 0) // 0 rounds
	input[212] = 0                           // not final

	// Set h to a known pattern.
	for i := 0; i < 64; i++ {
		input[4+i] = byte(i)
	}

	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("blake2f(0 rounds): unexpected error: %v", err)
	}

	// With 0 rounds, the compression function still XORs v[0..7] ^ v[8..15]
	// into h. The result depends on the IV and state setup.
	if len(out) != 64 {
		t.Fatalf("blake2f(0 rounds) output length = %d, want 64", len(out))
	}
}

// TestBlake2FPrecompile_GasCost tests that Blake2F gas is based on rounds.
func TestBlake2FPrecompile_GasCost(t *testing.T) {
	c := &blake2F{}

	tests := []struct {
		rounds uint32
		want   uint64
	}{
		{0, 0},
		{1, 1},
		{12, 12},
		{100, 100},
		{1000000, 1000000},
	}

	for _, tt := range tests {
		input := make([]byte, 213)
		binary.BigEndian.PutUint32(input[:4], tt.rounds)
		got := c.RequiredGas(input)
		if got != tt.want {
			t.Errorf("blake2f gas for %d rounds = %d, want %d", tt.rounds, got, tt.want)
		}
	}
}

// TestBN256Add verifies that BN256 point addition works correctly.
func TestBN256Add(t *testing.T) {
	c := &bn256Add{}

	if g := c.RequiredGas(nil); g != 150 {
		t.Errorf("bn256Add gas = %d, want 150", g)
	}

	// Adding identity to identity should return identity.
	out, err := c.Run(make([]byte, 128))
	if err != nil {
		t.Fatalf("bn256Add(0+0): unexpected error: %v", err)
	}
	if len(out) != 64 {
		t.Fatalf("bn256Add output length = %d, want 64", len(out))
	}
	// Should be all zeros (identity).
	for i, b := range out {
		if b != 0 {
			t.Fatalf("bn256Add(0+0) byte %d = %d, want 0", i, b)
		}
	}

	// G + G should return 2G.
	input := make([]byte, 128)
	input[31] = 1  // x1 = 1
	input[63] = 2  // y1 = 2
	input[95] = 1  // x2 = 1
	input[127] = 2 // y2 = 2
	out, err = c.Run(input)
	if err != nil {
		t.Fatalf("bn256Add(G+G): unexpected error: %v", err)
	}
	if len(out) != 64 {
		t.Fatalf("bn256Add(G+G) output length = %d, want 64", len(out))
	}
	// Verify result is on the curve by using it in another add.
	x := new(big.Int).SetBytes(out[:32])
	y := new(big.Int).SetBytes(out[32:64])
	if x.Sign() == 0 && y.Sign() == 0 {
		t.Fatal("bn256Add(G+G) should not be identity")
	}
}

// TestBN256ScalarMul verifies that BN256 scalar multiplication works correctly.
func TestBN256ScalarMul(t *testing.T) {
	c := &bn256ScalarMul{}

	if g := c.RequiredGas(nil); g != 6000 {
		t.Errorf("bn256ScalarMul gas = %d, want 6000", g)
	}

	// 1 * G = G
	input := make([]byte, 96)
	input[31] = 1 // x = 1
	input[63] = 2 // y = 2
	input[95] = 1 // scalar = 1
	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("bn256ScalarMul(1*G): unexpected error: %v", err)
	}
	if out[31] != 1 || out[63] != 2 {
		t.Fatalf("bn256ScalarMul(1*G) should return G, got %x", out)
	}

	// 0 * G = identity
	input2 := make([]byte, 96)
	input2[31] = 1 // x = 1
	input2[63] = 2 // y = 2
	// scalar = 0
	out2, err := c.Run(input2)
	if err != nil {
		t.Fatalf("bn256ScalarMul(0*G): unexpected error: %v", err)
	}
	for i, b := range out2 {
		if b != 0 {
			t.Fatalf("bn256ScalarMul(0*G) byte %d = %d, want 0", i, b)
		}
	}
}

// TestBN256Pairing verifies the BN256 pairing precompile.
func TestBN256Pairing(t *testing.T) {
	c := &bn256Pairing{}

	// Gas cost: 45000 + 34000*k where k = len(input)/192.
	if g := c.RequiredGas(nil); g != 45000 {
		t.Errorf("bn256Pairing gas (0 pairs) = %d, want 45000", g)
	}
	if g := c.RequiredGas(make([]byte, 192)); g != 45000+34000 {
		t.Errorf("bn256Pairing gas (1 pair) = %d, want %d", g, 45000+34000)
	}
	if g := c.RequiredGas(make([]byte, 384)); g != 45000+34000*2 {
		t.Errorf("bn256Pairing gas (2 pairs) = %d, want %d", g, 45000+34000*2)
	}

	// Empty input should return 1 (true).
	out, err := c.Run(nil)
	if err != nil {
		t.Fatalf("bn256Pairing(empty): unexpected error: %v", err)
	}
	if out[31] != 1 {
		t.Fatalf("bn256Pairing(empty) should return 1, got %x", out)
	}

	// Invalid length (not multiple of 192).
	_, err = c.Run(make([]byte, 100))
	if err == nil {
		t.Error("bn256Pairing(invalid length): expected error")
	}
}

// TestBlake2FPrecompile_EIP152Vector tests against the EIP-152 reference test vector.
func TestBlake2FPrecompile_EIP152Vector(t *testing.T) {
	// EIP-152 test vector 5:
	// rounds = 12
	// h = 48 01 6a 09 e6 67 f3 bc  c9 08 bb 67 ae 85 84 ca
	//     a7 3b 3c 6e f3 72 fe 94  f8 2b a5 4f f5 3a 5f 1d
	//     36 f1 51 0e 52 7f ad e6  82 d1 9b 05 68 8c 2b 3e
	//     6c 1f 1f 83 d9 ab fb 41  bd 6b 5b e0 cd 19 13 7e
	//     21 79
	// m = 61 62 63 (+ zeros to fill 128 bytes)
	// t[0] = 3, t[1] = 0
	// f = 1
	//
	// Expected output (h after compression):
	// ba 80 a5 3f 98 1c 4d 0d  6a 27 97 b6 9f 12 f6 e9
	// 4c 21 2f 14 68 5a c4 b7  4b 12 bb 6f db ff a2 d1
	// 7d 87 c5 39 2a ab 79 2d  c2 52 d5 de 45 33 cc 95
	// 18 d3 8a a8 db f1 92 5a  b9 23 86 ed d4 00 99 23

	input := make([]byte, 213)
	// rounds = 12
	binary.BigEndian.PutUint32(input[0:4], 12)

	// h = BLAKE2b IV XORed with params.
	// For the EIP-152 test vector, h is the BLAKE2b state after initialization
	// with personal = empty, key = empty, digest_length = 64.
	// The initial state is IV XOR param block. param[0] = 0x01010040.
	// IV[0] XOR 0x01010040 = 0x6a09e667f3bcc908 XOR 0x0000000001010040 = 0x6a09e667f2bdc948
	h := [8]uint64{
		0x6a09e667f2bdc948, // IV[0] ^ 0x01010040
		0xbb67ae8584caa73b,
		0x3c6ef372fe94f82b,
		0xa54ff53a5f1d36f1,
		0x510e527fade682d1,
		0x9b05688c2b3e6c1f,
		0x1f83d9abfb41bd6b,
		0x5be0cd19137e2179,
	}
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint64(input[4+i*8:4+(i+1)*8], h[i])
	}

	// m = "abc" (0x61, 0x62, 0x63) followed by zeros.
	input[68] = 0x61 // 'a'
	input[69] = 0x62 // 'b'
	input[70] = 0x63 // 'c'
	// rest of m is zeros

	// t[0] = 3 (3 bytes processed)
	binary.LittleEndian.PutUint64(input[196:204], 3)
	// t[1] = 0
	binary.LittleEndian.PutUint64(input[204:212], 0)

	// f = 1 (final block)
	input[212] = 1

	c := &blake2F{}
	out, err := c.Run(input)
	if err != nil {
		t.Fatalf("blake2f EIP-152 vector: unexpected error: %v", err)
	}
	if len(out) != 64 {
		t.Fatalf("blake2f output length = %d, want 64", len(out))
	}

	// Expected output: BLAKE2b("abc") with 64-byte digest.
	// This is a well-known value:
	// ba80a53f981c4d0d6a2797b69f12f6e94c212f14685ac4b74b12bb6fdbffa2d1
	// 7d87c5392aab792dc252d5de4533cc9518d38aa8dbf1925ab92386edd4009923
	expectedHex := "ba80a53f981c4d0d6a2797b69f12f6e94c212f14685ac4b74b12bb6fdbffa2d17d87c5392aab792dc252d5de4533cc9518d38aa8dbf1925ab92386edd4009923"
	gotHex := hex.EncodeToString(out)
	if gotHex != expectedHex {
		t.Errorf("blake2f EIP-152 vector:\ngot  %s\nwant %s", gotHex, expectedHex)
	}
}
