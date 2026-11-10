// groth16_gnark.go implements real Groth16 proving and verification using
// github.com/consensys/gnark over BN254 for the AA validation circuit.
//
// PQ-6.1: real gnark/backend/groth16.Verify() integration.
// PQ-6.2: AAValidationGnarkCircuit R1CS compilation via frontend.Compile.
package proofs

import (
	"errors"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/constraint"
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/frontend/cs/r1cs"
)

// Gnark circuit errors.
var (
	ErrGnarkNilCS   = errors.New("gnark: nil constraint system")
	ErrGnarkNilKeys = errors.New("gnark: nil keys")
	ErrGnarkBadArgs = errors.New("gnark: invalid circuit arguments")
)

// AAValidationGnarkCircuit is a gnark-native circuit proving AA operation validity.
// It enforces three verifiable constraints:
//  1. Nonce > 0 (sequential: must be nonzero — checked by solver)
//  2. GasLimit > 0 (gas payment proof)
//  3. Nonce = PrevNonce + 1 (sequential increment)
//
// Nonce and GasLimit are public inputs; PrevNonce is a private witness.
type AAValidationGnarkCircuit struct {
	// Public inputs (exposed in verifying key).
	Nonce    frontend.Variable `gnark:",public"`
	GasLimit frontend.Variable `gnark:",public"`

	// Private witness: previous nonce proving sequential increment.
	PrevNonce frontend.Variable
}

// Define encodes the R1CS constraints for AA validation.
func (c *AAValidationGnarkCircuit) Define(api frontend.API) error {
	// Constraint 1: Nonce must be nonzero (nonce > 0).
	api.AssertIsDifferent(c.Nonce, 0)

	// Constraint 2: GasLimit must be nonzero (gas payment required).
	api.AssertIsDifferent(c.GasLimit, 0)

	// Constraint 3: Nonce = PrevNonce + 1 (sequential increment).
	expected := api.Add(c.PrevNonce, 1)
	api.AssertIsEqual(c.Nonce, expected)

	return nil
}

// GnarkAACircuitCS holds the compiled AA validation circuit.
type GnarkAACircuitCS struct {
	cs constraint.ConstraintSystem
}

// CompileAACircuitGnark compiles AAValidationGnarkCircuit to an R1CS using
// gnark's frontend. This is the PQ-6.2 implementation.
func CompileAACircuitGnark() (*GnarkAACircuitCS, error) {
	var circuit AAValidationGnarkCircuit
	cs, err := frontend.Compile(ecc.BN254.ScalarField(), r1cs.NewBuilder, &circuit)
	if err != nil {
		return nil, err
	}
	return &GnarkAACircuitCS{cs: cs}, nil
}

// ConstraintCount returns the number of R1CS constraints.
func (g *GnarkAACircuitCS) ConstraintCount() int {
	if g.cs == nil {
		return 0
	}
	return g.cs.GetNbConstraints()
}

// GnarkAAProverKeys holds the gnark proving and verifying keys for the AA circuit.
type GnarkAAProverKeys struct {
	ProvingKey   groth16.ProvingKey
	VerifyingKey groth16.VerifyingKey
}

// SetupGnarkAAKeys generates Groth16 proving and verifying keys from the
// compiled AA circuit constraint system. This is the PQ-6.2 SetupKeys implementation.
func SetupGnarkAAKeys(circuit *GnarkAACircuitCS) (*GnarkAAProverKeys, error) {
	if circuit == nil || circuit.cs == nil {
		return nil, ErrGnarkNilCS
	}
	pk, vk, err := groth16.Setup(circuit.cs)
	if err != nil {
		return nil, err
	}
	return &GnarkAAProverKeys{ProvingKey: pk, VerifyingKey: vk}, nil
}

// ProveGnarkAA generates a Groth16 proof for an AA validation operation.
// nonce and gasLimit are the public inputs; prevNonce is the private witness.
func ProveGnarkAA(
	circuit *GnarkAACircuitCS,
	keys *GnarkAAProverKeys,
	nonce, gasLimit, prevNonce uint64,
) (groth16.Proof, error) {
	if circuit == nil || circuit.cs == nil {
		return nil, ErrGnarkNilCS
	}
	if keys == nil {
		return nil, ErrGnarkNilKeys
	}
	if nonce == 0 || gasLimit == 0 {
		return nil, ErrGnarkBadArgs
	}

	assignment := &AAValidationGnarkCircuit{
		Nonce:     nonce,
		GasLimit:  gasLimit,
		PrevNonce: prevNonce,
	}
	witness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField())
	if err != nil {
		return nil, err
	}
	return groth16.Prove(circuit.cs, keys.ProvingKey, witness)
}

// VerifyGnarkAAProof verifies a gnark Groth16 proof against the public inputs.
// Returns true when the proof is valid for the given nonce and gasLimit.
func VerifyGnarkAAProof(
	keys *GnarkAAProverKeys,
	proof groth16.Proof,
	nonce, gasLimit uint64,
) (bool, error) {
	if keys == nil {
		return false, ErrGnarkNilKeys
	}
	if proof == nil {
		return false, errors.New("gnark: nil proof")
	}

	pubAssignment := &AAValidationGnarkCircuit{
		Nonce:    nonce,
		GasLimit: gasLimit,
	}
	publicWitness, err := frontend.NewWitness(
		pubAssignment,
		ecc.BN254.ScalarField(),
		frontend.PublicOnly(),
	)
	if err != nil {
		return false, err
	}

	if err := groth16.Verify(proof, keys.VerifyingKey, publicWitness); err != nil {
		return false, err
	}
	return true, nil
}

// GnarkIntegrationStatus returns the gnark library version info.
func GnarkIntegrationStatus() string {
	return "gnark-groth16-bn254-real"
}

// gnarkBN254ScalarField returns the BN254 scalar field for external use.
func gnarkBN254ScalarField() *big.Int {
	return ecc.BN254.ScalarField()
}
