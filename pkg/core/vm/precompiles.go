package vm

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/ethereum/go-ethereum/crypto/kzg4844"
	"golang.org/x/crypto/ripemd160"
)

// EIP-4844 point evaluation precompile constants.
var (
	fieldElementsPerBlob = big.NewInt(4096)
	blsModulus, _        = new(big.Int).SetString("52435875175126190479447740508185965837690552500527637822603658699938581184513", 10)
)

// PrecompiledContract is the interface for native precompiled contracts.
type PrecompiledContract interface {
	RequiredGas(input []byte) uint64
	Run(input []byte) ([]byte, error)
}

// PrecompiledContractsCancun contains the default set of pre-compiled contracts.
var PrecompiledContractsCancun = map[types.Address]PrecompiledContract{
	types.BytesToAddress([]byte{1}):    &ecrecover{},
	types.BytesToAddress([]byte{2}):    &sha256hash{},
	types.BytesToAddress([]byte{3}):    &ripemd160hash{},
	types.BytesToAddress([]byte{4}):    &dataCopy{},
	types.BytesToAddress([]byte{5}):    &bigModExp{},
	types.BytesToAddress([]byte{6}):    &bn256Add{},
	types.BytesToAddress([]byte{7}):    &bn256ScalarMul{},
	types.BytesToAddress([]byte{8}):    &bn256Pairing{},
	types.BytesToAddress([]byte{9}):    &blake2F{},
	types.BytesToAddress([]byte{0x0a}): &kzgPointEvaluation{},
	// EIP-2537: BLS12-381 precompiles (0x0b - 0x13).
	types.BytesToAddress([]byte{0x0b}): &bls12G1Add{},
	types.BytesToAddress([]byte{0x0c}): &bls12G1Mul{},
	types.BytesToAddress([]byte{0x0d}): &bls12G1MSM{},
	types.BytesToAddress([]byte{0x0e}): &bls12G2Add{},
	types.BytesToAddress([]byte{0x0f}): &bls12G2Mul{},
	types.BytesToAddress([]byte{0x10}): &bls12G2MSM{},
	types.BytesToAddress([]byte{0x11}): &bls12Pairing{},
	types.BytesToAddress([]byte{0x12}): &bls12MapFpToG1{},
	types.BytesToAddress([]byte{0x13}): &bls12MapFp2ToG2{},
	// EIP-7212: P256VERIFY precompile.
	types.BytesToAddress([]byte{0x01, 0x00}): &p256Verify{},
}

// IsPrecompiledContract checks if the given address is a precompiled contract.
func IsPrecompiledContract(addr types.Address) bool {
	_, ok := PrecompiledContractsCancun[addr]
	return ok
}

// RunPrecompiledContract executes a precompiled contract and returns the output,
// remaining gas, and any error.
func RunPrecompiledContract(addr types.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	p, ok := PrecompiledContractsCancun[addr]
	if !ok {
		return nil, gas, errors.New("not a precompiled contract")
	}
	gasCost := p.RequiredGas(input)
	if gas < gasCost {
		return nil, 0, ErrOutOfGas
	}
	output, err := p.Run(input)
	return output, gas - gasCost, err
}

// --- ecrecover (address 0x01) ---

type ecrecover struct{}

func (c *ecrecover) RequiredGas(input []byte) uint64 {
	return 3000
}

func (c *ecrecover) Run(input []byte) ([]byte, error) {
	// Pad input to 128 bytes.
	input = padRight(input, 128)

	// Extract hash, v, r, s.
	hash := input[0:32]
	v := new(big.Int).SetBytes(input[32:64])
	r := new(big.Int).SetBytes(input[64:96])
	s := new(big.Int).SetBytes(input[96:128])

	// v must be 27 or 28 (Ethereum convention).
	if v.BitLen() > 8 {
		return nil, nil
	}
	vByte := byte(v.Uint64())
	if vByte != 27 && vByte != 28 {
		return nil, nil
	}

	// Validate r and s.
	if !crypto.ValidateSignatureValues(vByte-27, r, s, true) {
		return nil, nil
	}

	// Build 65-byte signature [R || S || V].
	sig := make([]byte, 65)
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	copy(sig[32-len(rBytes):32], rBytes)
	copy(sig[64-len(sBytes):64], sBytes)
	sig[64] = vByte - 27

	// Recover the public key.
	pub, err := crypto.Ecrecover(hash, sig)
	if err != nil {
		return nil, nil
	}

	// Derive address: Keccak256(pubkey[1:])[12:]
	addr := crypto.Keccak256(pub[1:])

	// Return 32-byte left-padded address.
	result := make([]byte, 32)
	copy(result[12:], addr[12:])
	return result, nil
}

// --- sha256hash (address 0x02) ---

type sha256hash struct{}

func (c *sha256hash) RequiredGas(input []byte) uint64 {
	return 60 + 12*wordCount(len(input))
}

func (c *sha256hash) Run(input []byte) ([]byte, error) {
	h := sha256.Sum256(input)
	return h[:], nil
}

// --- ripemd160hash (address 0x03) ---

type ripemd160hash struct{}

func (c *ripemd160hash) RequiredGas(input []byte) uint64 {
	return 600 + 120*wordCount(len(input))
}

func (c *ripemd160hash) Run(input []byte) ([]byte, error) {
	h := ripemd160.New()
	h.Write(input)
	digest := h.Sum(nil) // 20 bytes

	// Return 32-byte left-padded result.
	result := make([]byte, 32)
	copy(result[12:], digest)
	return result, nil
}

// --- dataCopy (address 0x04) ---

type dataCopy struct{}

func (c *dataCopy) RequiredGas(input []byte) uint64 {
	return 15 + 3*wordCount(len(input))
}

func (c *dataCopy) Run(input []byte) ([]byte, error) {
	out := make([]byte, len(input))
	copy(out, input)
	return out, nil
}

// --- bigModExp (address 0x05) ---

type bigModExp struct{}

func (c *bigModExp) RequiredGas(input []byte) uint64 {
	input = padRight(input, 96)

	baseLen := new(big.Int).SetBytes(input[0:32]).Uint64()
	expLen := new(big.Int).SetBytes(input[32:64]).Uint64()
	modLen := new(big.Int).SetBytes(input[64:96]).Uint64()

	// Calculate adjusted exponent length for gas.
	adjExpLen := adjustedExpLen(expLen, baseLen, input[96:])

	// Gas = max(200, floor(mult_complexity * iter_count / 3))
	maxLen := baseLen
	if modLen > maxLen {
		maxLen = modLen
	}
	words := (maxLen + 7) / 8
	multComplexity := words * words

	gas := multComplexity * maxUint64(adjExpLen, 1) / 3
	if gas < 200 {
		gas = 200
	}
	return gas
}

func (c *bigModExp) Run(input []byte) ([]byte, error) {
	input = padRight(input, 96)

	baseLen := new(big.Int).SetBytes(input[0:32])
	expLen := new(big.Int).SetBytes(input[32:64])
	modLen := new(big.Int).SetBytes(input[64:96])

	// Sanity check lengths.
	if baseLen.BitLen() > 32 || expLen.BitLen() > 32 || modLen.BitLen() > 32 {
		return nil, errors.New("modexp: length overflow")
	}
	bLen := baseLen.Uint64()
	eLen := expLen.Uint64()
	mLen := modLen.Uint64()

	// Extract base, exp, mod from input data after the 96-byte header.
	data := input[96:]
	base := getDataSlice(data, 0, bLen)
	exp := getDataSlice(data, bLen, eLen)
	mod := getDataSlice(data, bLen+eLen, mLen)

	// If mod is zero, return zero.
	modVal := new(big.Int).SetBytes(mod)
	if modVal.Sign() == 0 {
		return make([]byte, mLen), nil
	}

	baseVal := new(big.Int).SetBytes(base)
	expVal := new(big.Int).SetBytes(exp)

	result := new(big.Int).Exp(baseVal, expVal, modVal)

	// Left-pad result to modLen bytes.
	out := result.Bytes()
	if uint64(len(out)) < mLen {
		padded := make([]byte, mLen)
		copy(padded[mLen-uint64(len(out)):], out)
		return padded, nil
	}
	return out[:mLen], nil
}

// --- helpers ---

// wordCount returns ceil(size / 32), i.e., the number of 32-byte words.
func wordCount(size int) uint64 {
	if size == 0 {
		return 0
	}
	return uint64((size + 31) / 32)
}

// padRight pads data with zeros on the right to reach at least minLen.
func padRight(data []byte, minLen int) []byte {
	if len(data) >= minLen {
		return data
	}
	padded := make([]byte, minLen)
	copy(padded, data)
	return padded
}

// getDataSlice extracts a slice from data starting at offset with given length,
// zero-padding if data is too short.
func getDataSlice(data []byte, offset, length uint64) []byte {
	if length == 0 {
		return nil
	}
	result := make([]byte, length)
	if offset >= uint64(len(data)) {
		return result
	}
	end := offset + length
	if end > uint64(len(data)) {
		end = uint64(len(data))
	}
	copy(result, data[offset:end])
	return result
}

// adjustedExpLen calculates the adjusted exponent length for modexp gas.
func adjustedExpLen(expLen, baseLen uint64, data []byte) uint64 {
	if expLen <= 32 {
		expData := getDataSlice(data, baseLen, expLen)
		exp := new(big.Int).SetBytes(expData)
		if exp.Sign() == 0 {
			return 0
		}
		return uint64(exp.BitLen() - 1)
	}
	// For expLen > 32, use the first 32 bytes of the exponent.
	firstExpData := getDataSlice(data, baseLen, 32)
	firstExp := new(big.Int).SetBytes(firstExpData)
	adj := uint64(0)
	if firstExp.Sign() > 0 {
		adj = uint64(firstExp.BitLen() - 1)
	}
	return adj + 8*(expLen-32)
}

// maxUint64 returns the larger of a and b.
func maxUint64(a, b uint64) uint64 {
	if a > b {
		return a
	}
	return b
}

// --- bn256Add (address 0x06) - EIP-196 ---
// BN254 (alt_bn128) elliptic curve point addition.

type bn256Add struct{}

func (c *bn256Add) RequiredGas(input []byte) uint64 {
	return 150 // EIP-1108 gas cost
}

func (c *bn256Add) Run(input []byte) ([]byte, error) {
	return crypto.BN254Add(input)
}

// --- bn256ScalarMul (address 0x07) - EIP-196 ---
// BN254 (alt_bn128) elliptic curve scalar multiplication.

type bn256ScalarMul struct{}

func (c *bn256ScalarMul) RequiredGas(input []byte) uint64 {
	return 6000 // EIP-1108 gas cost
}

func (c *bn256ScalarMul) Run(input []byte) ([]byte, error) {
	return crypto.BN254ScalarMul(input)
}

// --- bn256Pairing (address 0x08) - EIP-197 ---
// BN254 (alt_bn128) elliptic curve pairing check.

type bn256Pairing struct{}

func (c *bn256Pairing) RequiredGas(input []byte) uint64 {
	// EIP-1108: 45000 + 34000 * k, where k is the number of pairings.
	// Each pairing element is 192 bytes (64 bytes G1 point + 128 bytes G2 point).
	k := uint64(len(input)) / 192
	return 45000 + 34000*k
}

func (c *bn256Pairing) Run(input []byte) ([]byte, error) {
	return crypto.BN254PairingCheck(input)
}

// --- blake2F (address 0x09) - EIP-152 ---
// BLAKE2b F compression function.

type blake2F struct{}

func (c *blake2F) RequiredGas(input []byte) uint64 {
	// Gas cost = rounds (first 4 bytes of input, big-endian uint32).
	if len(input) < 4 {
		return 0
	}
	return uint64(binary.BigEndian.Uint32(input[:4]))
}

func (c *blake2F) Run(input []byte) ([]byte, error) {
	// Input: [4 bytes rounds][64 bytes h][128 bytes m][8 bytes t0][8 bytes t1][1 byte f]
	// Total: 213 bytes.
	if len(input) != 213 {
		return nil, errors.New("blake2f: invalid input length (expected 213 bytes)")
	}

	rounds := binary.BigEndian.Uint32(input[:4])

	// Final block indicator: must be 0 or 1.
	finalByte := input[212]
	if finalByte != 0 && finalByte != 1 {
		return nil, errors.New("blake2f: invalid final block indicator")
	}
	final := finalByte == 1

	// Parse h (8 x uint64, little-endian).
	var h [8]uint64
	for i := 0; i < 8; i++ {
		h[i] = binary.LittleEndian.Uint64(input[4+i*8 : 4+(i+1)*8])
	}

	// Parse m (16 x uint64, little-endian).
	var m [16]uint64
	for i := 0; i < 16; i++ {
		m[i] = binary.LittleEndian.Uint64(input[68+i*8 : 68+(i+1)*8])
	}

	// Parse t (2 x uint64, little-endian).
	t0 := binary.LittleEndian.Uint64(input[196:204])
	t1 := binary.LittleEndian.Uint64(input[204:212])

	// Execute the BLAKE2b F compression function.
	blake2bF(&h, m, [2]uint64{t0, t1}, final, rounds)

	// Encode h back to little-endian bytes.
	result := make([]byte, 64)
	for i := 0; i < 8; i++ {
		binary.LittleEndian.PutUint64(result[i*8:(i+1)*8], h[i])
	}
	return result, nil
}

// blake2bF is the BLAKE2b compression function F.
// It modifies h in-place after `rounds` rounds of mixing.
func blake2bF(h *[8]uint64, m [16]uint64, t [2]uint64, final bool, rounds uint32) {
	// BLAKE2b IV.
	var iv = [8]uint64{
		0x6a09e667f3bcc908, 0xbb67ae8584caa73b,
		0x3c6ef372fe94f82b, 0xa54ff53a5f1d36f1,
		0x510e527fade682d1, 0x9b05688c2b3e6c1f,
		0x1f83d9abfb41bd6b, 0x5be0cd19137e2179,
	}

	// Sigma permutation table for BLAKE2b.
	var sigma = [12][16]byte{
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		{14, 10, 4, 8, 9, 15, 13, 6, 1, 12, 0, 2, 11, 7, 5, 3},
		{11, 8, 12, 0, 5, 2, 15, 13, 10, 14, 3, 6, 7, 1, 9, 4},
		{7, 9, 3, 1, 13, 12, 11, 14, 2, 6, 5, 10, 4, 0, 15, 8},
		{9, 0, 5, 7, 2, 4, 10, 15, 14, 1, 11, 12, 6, 8, 3, 13},
		{2, 12, 6, 10, 0, 11, 8, 3, 4, 13, 7, 5, 15, 14, 1, 9},
		{12, 5, 1, 15, 14, 13, 4, 10, 0, 7, 6, 3, 9, 2, 8, 11},
		{13, 11, 7, 14, 12, 1, 3, 9, 5, 0, 15, 4, 8, 6, 2, 10},
		{6, 15, 14, 9, 11, 3, 0, 8, 12, 2, 13, 7, 1, 4, 10, 5},
		{10, 2, 8, 4, 7, 6, 1, 5, 15, 11, 9, 14, 3, 12, 13, 0},
		{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15},
		{14, 10, 4, 8, 9, 15, 13, 6, 1, 12, 0, 2, 11, 7, 5, 3},
	}

	// Initialize the work vector v.
	var v [16]uint64
	copy(v[:8], h[:])
	copy(v[8:], iv[:])
	v[12] ^= t[0]
	v[13] ^= t[1]
	if final {
		v[14] = ^v[14]
	}

	// G mixing function.
	g := func(a, b, c, d int, x, y uint64) {
		v[a] = v[a] + v[b] + x
		v[d] = bits64RotateRight(v[d]^v[a], 32)
		v[c] = v[c] + v[d]
		v[b] = bits64RotateRight(v[b]^v[c], 24)
		v[a] = v[a] + v[b] + y
		v[d] = bits64RotateRight(v[d]^v[a], 16)
		v[c] = v[c] + v[d]
		v[b] = bits64RotateRight(v[b]^v[c], 63)
	}

	for i := uint32(0); i < rounds; i++ {
		s := sigma[i%10]
		g(0, 4, 8, 12, m[s[0]], m[s[1]])
		g(1, 5, 9, 13, m[s[2]], m[s[3]])
		g(2, 6, 10, 14, m[s[4]], m[s[5]])
		g(3, 7, 11, 15, m[s[6]], m[s[7]])
		g(0, 5, 10, 15, m[s[8]], m[s[9]])
		g(1, 6, 11, 12, m[s[10]], m[s[11]])
		g(2, 7, 8, 13, m[s[12]], m[s[13]])
		g(3, 4, 9, 14, m[s[14]], m[s[15]])
	}

	for i := 0; i < 8; i++ {
		h[i] ^= v[i] ^ v[i+8]
	}
}

// bits64RotateRight rotates x right by k bits.
func bits64RotateRight(x uint64, k uint) uint64 {
	return (x >> k) | (x << (64 - k))
}

// --- kzgPointEvaluation (address 0x0a) - EIP-4844 ---

const pointEvaluationGas = 50000

type kzgPointEvaluation struct{}

func (c *kzgPointEvaluation) RequiredGas(input []byte) uint64 {
	return pointEvaluationGas
}

func (c *kzgPointEvaluation) Run(input []byte) ([]byte, error) {
	if len(input) != 192 {
		return nil, errors.New("kzg: invalid input length")
	}

	// Parse input: versioned_hash(32) | z(32) | y(32) | commitment(48) | proof(48)
	versionedHash := input[:32]
	z := new(big.Int).SetBytes(input[32:64])
	y := new(big.Int).SetBytes(input[64:96])

	// Validate that versioned_hash starts with KZG version byte.
	if versionedHash[0] != types.VersionedHashVersionKZG {
		return nil, errors.New("kzg: invalid versioned hash version")
	}

	// Validate that z and y are valid field elements (< BLS_MODULUS).
	if z.Cmp(blsModulus) >= 0 {
		return nil, errors.New("kzg: z is not a valid field element")
	}
	if y.Cmp(blsModulus) >= 0 {
		return nil, errors.New("kzg: y is not a valid field element")
	}

	// Verify commitment matches versioned_hash: sha256(commitment) with version prefix.
	commitment := input[96:144]
	commitHash := sha256.Sum256(commitment)
	commitHash[0] = types.VersionedHashVersionKZG
	if !bytesEqual(versionedHash, commitHash[:]) {
		return nil, errors.New("kzg: commitment does not match versioned hash")
	}

	// Verify against the Ethereum ceremony setup.
	if err := kzg4844.VerifyProof(kzg4844.Commitment(commitment), kzg4844.Point(input[32:64]),
		kzg4844.Claim(input[64:96]), kzg4844.Proof(input[144:192])); err != nil {
		return nil, errors.New("kzg: proof verification failed")
	}

	// Return FIELD_ELEMENTS_PER_BLOB and BLS_MODULUS as 32-byte big-endian values.
	result := make([]byte, 64)
	fieldBytes := fieldElementsPerBlob.Bytes()
	copy(result[32-len(fieldBytes):32], fieldBytes)
	modBytes := blsModulus.Bytes()
	copy(result[64-len(modBytes):64], modBytes)
	return result, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- p256Verify (address 0x0100) - EIP-7212 ---
// P-256 (secp256r1) ECDSA signature verification, enabling passkey/WebAuthn support.

const p256VerifyGas = 6900

type p256Verify struct{}

func (c *p256Verify) RequiredGas(input []byte) uint64 {
	return p256VerifyGas
}

func (c *p256Verify) Run(input []byte) ([]byte, error) {
	const p256VerifyInputLength = 160
	if len(input) != p256VerifyInputLength {
		return nil, nil
	}

	// Extract hash, r, s, x, y from the input.
	hash := input[0:32]
	r := new(big.Int).SetBytes(input[32:64])
	s := new(big.Int).SetBytes(input[64:96])
	x := new(big.Int).SetBytes(input[96:128])
	y := new(big.Int).SetBytes(input[128:160])

	// Verify the signature using P-256 curve.
	if crypto.P256Verify(hash, r, s, x, y) {
		// Return 1 as 32-byte big-endian.
		result := make([]byte, 32)
		result[31] = 1
		return result, nil
	}
	return nil, nil
}

// --- EIP-7904: Glamsterdan precompile gas repricing ---

// bn256AddGlamsterdan wraps bn256Add with the updated gas cost.
type bn256AddGlamsterdan struct{ bn256Add }

func (c *bn256AddGlamsterdan) RequiredGas(input []byte) uint64 {
	return GasECADDGlamsterdan // 314 (was 150)
}

// blake2FGlamsterdan wraps blake2F with the updated gas cost.
type blake2FGlamsterdan struct{ blake2F }

func (c *blake2FGlamsterdan) RequiredGas(input []byte) uint64 {
	if len(input) < 4 {
		return 0
	}
	rounds := uint64(binary.BigEndian.Uint32(input[:4]))
	return GasBlake2fConstGlamsterdan + rounds*GasBlake2fPerRoundGlamsterdan
}

// kzgPointEvaluationGlamsterdan wraps kzgPointEvaluation with the updated gas cost.
type kzgPointEvaluationGlamsterdan struct{ kzgPointEvaluation }

func (c *kzgPointEvaluationGlamsterdan) RequiredGas(input []byte) uint64 {
	return GasPointEvalGlamsterdan // 89363 (was 50000)
}

// bn256PairingGlamsterdan wraps bn256Pairing with the updated gas cost.
type bn256PairingGlamsterdan struct{ bn256Pairing }

func (c *bn256PairingGlamsterdan) RequiredGas(input []byte) uint64 {
	k := uint64(len(input)) / 192
	return GasECPairingConstGlamsterdan + GasECPairingPerPairGlamsterdan*k
}

// PrecompiledContractsGlamsterdan contains the precompiled contracts with
// EIP-7904 gas repricing applied.
var PrecompiledContractsGlamsterdan = map[types.Address]PrecompiledContract{
	types.BytesToAddress([]byte{1}):    &ecrecover{},
	types.BytesToAddress([]byte{2}):    &sha256hash{},
	types.BytesToAddress([]byte{3}):    &ripemd160hash{},
	types.BytesToAddress([]byte{4}):    &dataCopy{},
	types.BytesToAddress([]byte{5}):    &bigModExp{},
	types.BytesToAddress([]byte{6}):    &bn256AddGlamsterdan{},
	types.BytesToAddress([]byte{7}):    &bn256ScalarMul{},
	types.BytesToAddress([]byte{8}):    &bn256PairingGlamsterdan{},
	types.BytesToAddress([]byte{9}):    &blake2FGlamsterdan{},
	types.BytesToAddress([]byte{0x0a}): &kzgPointEvaluationGlamsterdan{},
	// EIP-2537: BLS12-381 precompiles.
	types.BytesToAddress([]byte{0x0b}): &bls12G1Add{},
	types.BytesToAddress([]byte{0x0c}): &bls12G1Mul{},
	types.BytesToAddress([]byte{0x0d}): &bls12G1MSM{},
	types.BytesToAddress([]byte{0x0e}): &bls12G2Add{},
	types.BytesToAddress([]byte{0x0f}): &bls12G2Mul{},
	types.BytesToAddress([]byte{0x10}): &bls12G2MSM{},
	types.BytesToAddress([]byte{0x11}): &bls12Pairing{},
	// EIP-7997: address 0x12 is now the deterministic CREATE2 factory (system contract).
	// bls12MapFpToG1 removed from precompile set; calls to 0x12 run stored EVM bytecode.
	types.BytesToAddress([]byte{0x13}): &bls12MapFp2ToG2{},
	// EIP-7212: P256VERIFY precompile.
	types.BytesToAddress([]byte{0x01, 0x00}): &p256Verify{},
}
