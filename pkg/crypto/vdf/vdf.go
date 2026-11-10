package vdf

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math"
	"time"

	harmonyvdf "github.com/harmony-one/vdf/src/vdf_go"
	"golang.org/x/crypto/sha3"
)

const (
	discriminantBits = 1024
	formSize         = 2 * ((discriminantBits + 16) >> 4)
	SolutionSize     = 2 * formSize
)

var (
	errVDFIterations = errors.New("vdf: invalid iteration count")
	errVDFEvaluation = errors.New("vdf: library did not produce a complete proof")
)

func vdfSeed(input []byte) [32]byte {
	h := sha256.New()
	h.Write([]byte("eth2030/classgroup-vdf/v1"))
	h.Write(input)
	return [32]byte(h.Sum(nil))
}

// Evaluate computes the class-group Wesolowski output and proof using Harmony's
// library. Output and Proof use its fixed-width binary quadratic form encoding.
func Evaluate(input []byte, iterations uint64) (*VDFv2Result, error) {
	if input == nil {
		return nil, errVDFv2NilInput
	}
	if len(input) == 0 {
		return nil, errVDFv2EmptyInput
	}
	if iterations == 0 {
		return nil, errVDFv2ZeroIterations
	}
	if iterations > uint64(math.MaxInt/2) {
		return nil, errVDFIterations
	}
	seed := vdfSeed(input)
	start := time.Now()
	output, proof := harmonyvdf.GenerateVDF(seed[:], int(iterations), discriminantBits)
	if len(output) != formSize || len(proof) != formSize {
		return nil, errVDFEvaluation
	}
	return &VDFv2Result{
		Input: bytes.Clone(input), Output: output, Proof: proof,
		Iterations: iterations, Duration: time.Since(start),
	}, nil
}

// EncodeSolution returns the canonical output followed by its proof.
func (r *VDFv2Result) EncodeSolution() []byte {
	return append(bytes.Clone(r.Output), r.Proof...)
}

// VerifySolution checks the encoded output and proof against the seed and
// iteration count. It returns a copy of the verified output.
func VerifySolution(input []byte, iterations uint64, solution []byte) ([]byte, bool) {
	if len(solution) != SolutionSize {
		return nil, false
	}
	if !verify(input, iterations, solution[:formSize], solution[formSize:]) {
		return nil, false
	}
	return bytes.Clone(solution[:formSize]), true
}

func verify(input []byte, iterations uint64, output, proof []byte) (valid bool) {
	if len(input) == 0 || iterations == 0 || iterations > uint64(math.MaxInt/2) ||
		len(output) != formSize || len(proof) != formSize {
		return false
	}
	// The form's first coefficient must be positive. The upstream decoder
	// divides by it while reconstructing the third coefficient.
	half := formSize / 2
	zero := make([]byte, half)
	for _, form := range [][]byte{output, proof} {
		if form[0] != 0 || bytes.Equal(form[:half], zero) {
			return false
		}
	}
	defer func() {
		// The upstream verifier does not return errors for malformed forms.
		if recover() != nil {
			valid = false
		}
	}()
	seed := vdfSeed(input)
	discriminant := harmonyvdf.CreateDiscriminant(seed[:], discriminantBits)
	for _, encodedForm := range [][]byte{output, proof} {
		form, ok := harmonyvdf.NewClassGroupFromBytesDiscriminant(bytes.Clone(encodedForm), discriminant)
		if !ok || !bytes.Equal(form.Serialize(), encodedForm) {
			return false
		}
	}
	encoded := append(bytes.Clone(output), proof...)
	return harmonyvdf.VerifyVDF(seed[:], encoded, int(iterations), discriminantBits)
}

func keccak256(data ...[]byte) []byte {
	h := sha3.NewLegacyKeccak256()
	for _, part := range data {
		h.Write(part)
	}
	return h.Sum(nil)
}
