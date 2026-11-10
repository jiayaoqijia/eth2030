// Groth16 verification layer for the mandatory proof system (K+ milestone).
// Implements BLS12-381 Groth16 verification: e(A,B) = e(Alpha,Beta) * e(IC,Gamma) * e(C,Delta).

package proofs

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"

	bls381 "github.com/consensys/gnark-crypto/ecc/bls12-381"
	"github.com/consensys/gnark-crypto/ecc/bls12-381/fr"
	gnarkgroth16 "github.com/consensys/gnark/backend/groth16/bls12-381"
	"github.com/eth2030/eth2030/crypto"
)

var (
	ErrGroth16NilProof      = errors.New("groth16: nil proof")
	ErrGroth16NilVK         = errors.New("groth16: nil verifying key")
	ErrGroth16InvalidA      = errors.New("groth16: invalid A (G1)")
	ErrGroth16InvalidB      = errors.New("groth16: invalid B (G2)")
	ErrGroth16InvalidC      = errors.New("groth16: invalid C (G1)")
	ErrGroth16InvalidAlpha  = errors.New("groth16: invalid Alpha (G1)")
	ErrGroth16InvalidBeta   = errors.New("groth16: invalid Beta (G2)")
	ErrGroth16InvalidGamma  = errors.New("groth16: invalid Gamma (G2)")
	ErrGroth16InvalidDelta  = errors.New("groth16: invalid Delta (G2)")
	ErrGroth16NoIC          = errors.New("groth16: no IC points")
	ErrGroth16ICMismatch    = errors.New("groth16: IC length mismatch")
	ErrGroth16PairingFailed = errors.New("groth16: pairing failed")
)

// BLSGroth16Proof: three BLS12-381 group elements (A in G1, B in G2, C in G1).
type BLSGroth16Proof struct {
	A *crypto.BlsG1Point
	B *crypto.BlsG2Point
	C *crypto.BlsG1Point
}

// BLSGroth16VerifyingKey holds the BLS12-381 verification key.
type BLSGroth16VerifyingKey struct {
	Alpha *crypto.BlsG1Point
	Beta  *crypto.BlsG2Point
	Gamma *crypto.BlsG2Point
	Delta *crypto.BlsG2Point
	IC    []*crypto.BlsG1Point // IC[0] constant + IC[1..n] per public input
}

// BLSGroth16Backend defines pluggable BLS12-381 Groth16 verification.
type BLSGroth16Backend interface {
	Verify(vk *BLSGroth16VerifyingKey, proof *BLSGroth16Proof, publicInputs [][]byte) (bool, error)
	Name() string
}

var (
	groth16BackendMu      sync.RWMutex
	activeGroth16Backend  BLSGroth16Backend
	defaultGroth16Backend = &GnarkGroth16Backend{}
)

func DefaultGroth16Backend() BLSGroth16Backend {
	groth16BackendMu.RLock()
	defer groth16BackendMu.RUnlock()
	if activeGroth16Backend != nil {
		return activeGroth16Backend
	}
	return defaultGroth16Backend
}

func SetGroth16Backend(b BLSGroth16Backend) {
	groth16BackendMu.Lock()
	defer groth16BackendMu.Unlock()
	activeGroth16Backend = b
}

func Groth16IntegrationStatus() string { return DefaultGroth16Backend().Name() }

// --- Validation ---

func ValidateGroth16Proof(proof *BLSGroth16Proof) error {
	if proof == nil {
		return ErrGroth16NilProof
	}
	if proof.A == nil {
		return ErrGroth16InvalidA
	}
	if proof.B == nil {
		return ErrGroth16InvalidB
	}
	if proof.C == nil {
		return ErrGroth16InvalidC
	}
	return nil
}

func ValidateVerifyingKey(vk *BLSGroth16VerifyingKey) error {
	if vk == nil {
		return ErrGroth16NilVK
	}
	if vk.Alpha == nil {
		return ErrGroth16InvalidAlpha
	}
	if vk.Beta == nil {
		return ErrGroth16InvalidBeta
	}
	if vk.Gamma == nil {
		return ErrGroth16InvalidGamma
	}
	if vk.Delta == nil {
		return ErrGroth16InvalidDelta
	}
	if len(vk.IC) == 0 {
		return ErrGroth16NoIC
	}
	return nil
}

// GnarkGroth16Backend verifies the legacy A/B/C proof encoding with gnark.
type GnarkGroth16Backend struct{}

func (*GnarkGroth16Backend) Name() string { return "gnark-groth16-bls12-381" }

func (*GnarkGroth16Backend) Verify(vk *BLSGroth16VerifyingKey, proof *BLSGroth16Proof, publicInputs [][]byte) (bool, error) {
	if err := ValidateGroth16Proof(proof); err != nil {
		return false, err
	}
	if err := ValidateVerifyingKey(vk); err != nil {
		return false, err
	}
	if len(vk.IC) != len(publicInputs)+1 {
		return false, fmt.Errorf("%w: got %d inputs, need %d", ErrGroth16ICMismatch, len(publicInputs), len(vk.IC)-1)
	}
	var gnarkProof gnarkgroth16.Proof
	if err := decodeGroth16G1(&gnarkProof.Ar, proof.A); err != nil {
		return false, fmt.Errorf("%w: %v", ErrGroth16InvalidA, err)
	}
	if err := decodeGroth16G2(&gnarkProof.Bs, proof.B); err != nil {
		return false, fmt.Errorf("%w: %v", ErrGroth16InvalidB, err)
	}
	if err := decodeGroth16G1(&gnarkProof.Krs, proof.C); err != nil {
		return false, fmt.Errorf("%w: %v", ErrGroth16InvalidC, err)
	}
	var key gnarkgroth16.VerifyingKey
	if err := decodeGroth16G1(&key.G1.Alpha, vk.Alpha); err != nil {
		return false, fmt.Errorf("%w: %v", ErrGroth16InvalidAlpha, err)
	}
	if err := decodeGroth16G2(&key.G2.Beta, vk.Beta); err != nil {
		return false, fmt.Errorf("%w: %v", ErrGroth16InvalidBeta, err)
	}
	if err := decodeGroth16G2(&key.G2.Gamma, vk.Gamma); err != nil {
		return false, fmt.Errorf("%w: %v", ErrGroth16InvalidGamma, err)
	}
	if err := decodeGroth16G2(&key.G2.Delta, vk.Delta); err != nil {
		return false, fmt.Errorf("%w: %v", ErrGroth16InvalidDelta, err)
	}
	key.G1.K = make([]bls381.G1Affine, len(vk.IC))
	for i, point := range vk.IC {
		if point == nil {
			return false, fmt.Errorf("groth16: nil IC point at index %d", i)
		}
		if err := decodeGroth16G1(&key.G1.K[i], point); err != nil {
			return false, fmt.Errorf("groth16: invalid IC point at index %d: %w", i, err)
		}
	}
	if err := key.Precompute(); err != nil {
		return false, fmt.Errorf("%w: %v", ErrGroth16PairingFailed, err)
	}
	witness := make(fr.Vector, len(publicInputs))
	for i, input := range publicInputs {
		witness[i].SetBytes(input)
	}
	return gnarkgroth16.Verify(&gnarkProof, &key, witness) == nil, nil
}

func decodeGroth16G1(dst *bls381.G1Affine, point *crypto.BlsG1Point) error {
	encoded := crypto.SerializeG1(point)
	_, err := dst.SetBytes(encoded[:])
	return err
}

func decodeGroth16G2(dst *bls381.G2Affine, point *crypto.BlsG2Point) error {
	encoded := crypto.SerializeG2(point)
	_, err := dst.SetBytes(encoded[:])
	return err
}

// --- Gas estimation ---

func EstimateGroth16VerifyGas(numPublicInputs int) uint64 {
	if numPublicInputs < 0 {
		numPublicInputs = 0
	}
	return 21000 + 113000*4 + 12500*uint64(numPublicInputs)
}

// --- Serialization ---

func SerializeBLSGroth16Proof(proof *BLSGroth16Proof) ([]byte, error) {
	if err := ValidateGroth16Proof(proof); err != nil {
		return nil, err
	}
	a := crypto.SerializeG1(proof.A)
	b := crypto.SerializeG2(proof.B)
	c := crypto.SerializeG1(proof.C)
	out := make([]byte, 0, 192)
	out = append(out, a[:]...)
	out = append(out, b[:]...)
	out = append(out, c[:]...)
	return out, nil
}

func DeserializeBLSGroth16Proof(data []byte) (*BLSGroth16Proof, error) {
	if len(data) != 192 {
		return nil, fmt.Errorf("groth16: invalid length %d, want 192", len(data))
	}
	var ab [48]byte
	copy(ab[:], data[:48])
	a := crypto.DeserializeG1(ab)
	if a == nil {
		return nil, ErrGroth16InvalidA
	}
	var bb [96]byte
	copy(bb[:], data[48:144])
	b := crypto.DeserializeG2(bb)
	if b == nil {
		return nil, ErrGroth16InvalidB
	}
	var cb [48]byte
	copy(cb[:], data[144:])
	c := crypto.DeserializeG1(cb)
	if c == nil {
		return nil, ErrGroth16InvalidC
	}
	return &BLSGroth16Proof{A: a, B: b, C: c}, nil
}

func BLSGroth16ProofFingerprint(proof *BLSGroth16Proof) ([32]byte, error) {
	data, err := SerializeBLSGroth16Proof(proof)
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(data), nil
}

func EncodePublicInput(v int64) []byte {
	var buf [32]byte
	binary.BigEndian.PutUint64(buf[24:], uint64(v))
	return buf[:]
}
