// Package zkvm provides a framework for zkVM guest execution and proof
// verification, supporting EIP-8079 native rollup proof-carrying transactions.
//
// circuit_builder.go implements R1CS (Rank-1 Constraint System) circuit
// construction for ZK state transition proofs. Circuits built here can
// represent Ethereum execution steps provably, supporting the K+ era
// roadmap for mandatory 3-of-5 proofs and canonical guest verification.
package zkvm

import (
	"errors"
	"math/big"

	"github.com/consensys/gnark-crypto/ecc/bn254/fr"
	"github.com/eth2030/eth2030/zkvm/poseidon"
)

var bn254ScalarField = fr.Modulus()

// Circuit builder errors.
var (
	ErrCircuitFinalized = errors.New("circuit: already finalized")
	ErrCircuitNotReady  = errors.New("circuit: not finalized")
	ErrInvalidVariable  = errors.New("circuit: invalid variable index")
	ErrConstraintFailed = errors.New("circuit: constraint not satisfied")
	ErrVerificationFail = errors.New("circuit: proof verification failed")
	ErrNilValue         = errors.New("circuit: nil value")
	ErrDuplicatePublic  = errors.New("circuit: duplicate public input")
	ErrProofTooShort    = errors.New("circuit: proof data too short")
	ErrPublicInputCount = errors.New("circuit: public input count mismatch")
)

// Variable represents a wire in the R1CS circuit.
// Index 0 is reserved for the constant "one" wire.
type Variable uint32

const (
	// OneVar is the constant-one wire, always assigned value 1.
	OneVar Variable = 0
)

// R1CSConstraint represents a single R1CS constraint: a * b = c
// where a, b, c are linear combinations of variables.
type R1CSConstraint struct {
	A LinearCombination
	B LinearCombination
	C LinearCombination
}

// LinearCombination is a sum of (coefficient, variable) pairs.
type LinearCombination []Term

// Term is a single (coefficient, variable) pair in a linear combination.
type Term struct {
	Coeff *big.Int
	Var   Variable
}

// Circuit holds the R1CS constraint system for ZK proof generation.
type Circuit struct {
	// constraints is the list of R1CS constraints.
	constraints []R1CSConstraint

	// witness holds variable assignments (index -> value).
	witness map[Variable]*big.Int

	// publicVars tracks which variables are public inputs.
	publicVars map[Variable]bool

	// nextVar is the next available variable index.
	nextVar Variable

	// field is the prime field modulus.
	field *big.Int

	// finalized indicates whether Synthesize has been called.
	finalized bool
}

// NewCircuit creates a new R1CS circuit over the BN254 scalar field.
func NewCircuit() *Circuit {
	c := &Circuit{
		witness:    make(map[Variable]*big.Int),
		publicVars: make(map[Variable]bool),
		nextVar:    1, // 0 is reserved for OneVar
		field:      new(big.Int).Set(bn254ScalarField),
	}
	// Assign the constant-one wire.
	c.witness[OneVar] = big.NewInt(1)
	c.publicVars[OneVar] = true
	return c
}

// NewCircuitWithField creates a circuit over a custom prime field.
func NewCircuitWithField(field *big.Int) *Circuit {
	c := NewCircuit()
	if field != nil && field.Sign() > 0 {
		c.field = new(big.Int).Set(field)
	}
	return c
}

// Field returns the prime field modulus.
func (c *Circuit) Field() *big.Int {
	return new(big.Int).Set(c.field)
}

// AllocatePublic creates a public input variable with the given value.
func (c *Circuit) AllocatePublic(value *big.Int) (Variable, error) {
	if c.finalized {
		return 0, ErrCircuitFinalized
	}
	if value == nil {
		return 0, ErrNilValue
	}
	v := c.nextVar
	c.nextVar++
	val := new(big.Int).Set(value)
	val.Mod(val, c.field)
	c.witness[v] = val
	c.publicVars[v] = true
	return v, nil
}

// AllocatePrivate creates a private witness variable with the given value.
func (c *Circuit) AllocatePrivate(value *big.Int) (Variable, error) {
	if c.finalized {
		return 0, ErrCircuitFinalized
	}
	if value == nil {
		return 0, ErrNilValue
	}
	v := c.nextVar
	c.nextVar++
	val := new(big.Int).Set(value)
	val.Mod(val, c.field)
	c.witness[v] = val
	return v, nil
}

// allocateInternal creates an internal variable with a computed value.
func (c *Circuit) allocateInternal(value *big.Int) Variable {
	v := c.nextVar
	c.nextVar++
	val := new(big.Int).Set(value)
	val.Mod(val, c.field)
	c.witness[v] = val
	return v
}

// AddConstraint adds a raw R1CS constraint: a * b = c.
// Each of a, b, c is a single variable (coefficient=1).
func (c *Circuit) AddConstraint(a, b, cv Variable) error {
	if c.finalized {
		return ErrCircuitFinalized
	}
	one := big.NewInt(1)
	c.constraints = append(c.constraints, R1CSConstraint{
		A: LinearCombination{{Coeff: one, Var: a}},
		B: LinearCombination{{Coeff: one, Var: b}},
		C: LinearCombination{{Coeff: one, Var: cv}},
	})
	return nil
}

// AddLinearConstraint adds an R1CS constraint with full linear combinations.
func (c *Circuit) AddLinearConstraint(a, b, cv LinearCombination) error {
	if c.finalized {
		return ErrCircuitFinalized
	}
	c.constraints = append(c.constraints, R1CSConstraint{A: a, B: b, C: cv})
	return nil
}

// AssertEqual constrains two variables to be equal: (a - b) * 1 = 0.
func (c *Circuit) AssertEqual(a, b Variable) error {
	if c.finalized {
		return ErrCircuitFinalized
	}
	one := big.NewInt(1)
	negOne := new(big.Int).Sub(c.field, big.NewInt(1))
	c.constraints = append(c.constraints, R1CSConstraint{
		A: LinearCombination{
			{Coeff: one, Var: a},
			{Coeff: negOne, Var: b},
		},
		B: LinearCombination{{Coeff: one, Var: OneVar}},
		C: LinearCombination{}, // zero
	})
	return nil
}

// AssertBool constrains a variable to be 0 or 1: v * (1 - v) = 0.
func (c *Circuit) AssertBool(v Variable) error {
	if c.finalized {
		return ErrCircuitFinalized
	}
	one := big.NewInt(1)
	negOne := new(big.Int).Sub(c.field, big.NewInt(1))
	c.constraints = append(c.constraints, R1CSConstraint{
		A: LinearCombination{{Coeff: one, Var: v}},
		B: LinearCombination{
			{Coeff: one, Var: OneVar},
			{Coeff: negOne, Var: v},
		},
		C: LinearCombination{}, // zero
	})
	return nil
}

// Add creates an addition gate: result = a + b.
// Encoded as: (a + b) * 1 = result.
func (c *Circuit) Add(a, b Variable) (Variable, error) {
	if c.finalized {
		return 0, ErrCircuitFinalized
	}
	aVal := c.getWitness(a)
	bVal := c.getWitness(b)
	sum := new(big.Int).Add(aVal, bVal)
	sum.Mod(sum, c.field)
	result := c.allocateInternal(sum)

	one := big.NewInt(1)
	c.constraints = append(c.constraints, R1CSConstraint{
		A: LinearCombination{
			{Coeff: one, Var: a},
			{Coeff: one, Var: b},
		},
		B: LinearCombination{{Coeff: one, Var: OneVar}},
		C: LinearCombination{{Coeff: one, Var: result}},
	})
	return result, nil
}

// Mul creates a multiplication gate: result = a * b.
// Encoded as: a * b = result.
func (c *Circuit) Mul(a, b Variable) (Variable, error) {
	if c.finalized {
		return 0, ErrCircuitFinalized
	}
	aVal := c.getWitness(a)
	bVal := c.getWitness(b)
	prod := new(big.Int).Mul(aVal, bVal)
	prod.Mod(prod, c.field)
	result := c.allocateInternal(prod)

	one := big.NewInt(1)
	c.constraints = append(c.constraints, R1CSConstraint{
		A: LinearCombination{{Coeff: one, Var: a}},
		B: LinearCombination{{Coeff: one, Var: b}},
		C: LinearCombination{{Coeff: one, Var: result}},
	})
	return result, nil
}

// Hash computes a Poseidon hash gate over the input variables.
// Returns a variable holding the hash result.
func (c *Circuit) Hash(inputs ...Variable) (Variable, error) {
	if c.finalized {
		return 0, ErrCircuitFinalized
	}
	// Collect witness values for the inputs.
	vals := make([]*big.Int, len(inputs))
	for i, v := range inputs {
		vals[i] = c.getWitness(v)
	}

	hashVal, err := poseidon.Hash(vals...)
	if err != nil {
		return 0, err
	}
	result := c.allocateInternal(hashVal)

	// The local circuit format records an oracle value for the hash.
	// It does not encode the permutation as R1CS constraints.
	inputCommitment := c.allocateInternal(hashVal)
	one := big.NewInt(1)
	c.constraints = append(c.constraints, R1CSConstraint{
		A: LinearCombination{{Coeff: one, Var: inputCommitment}},
		B: LinearCombination{{Coeff: one, Var: OneVar}},
		C: LinearCombination{{Coeff: one, Var: result}},
	})
	return result, nil
}

// Synthesize finalizes the circuit. Returns the number of constraints
// and total number of variables.
func (c *Circuit) Synthesize() (numConstraints, numVariables int) {
	c.finalized = true
	return len(c.constraints), int(c.nextVar)
}

// NumConstraints returns the current number of constraints.
func (c *Circuit) NumConstraints() int {
	return len(c.constraints)
}

// NumVariables returns the current number of variables.
func (c *Circuit) NumVariables() int {
	return int(c.nextVar)
}

// NumPublicInputs returns the number of public input variables.
func (c *Circuit) NumPublicInputs() int {
	return len(c.publicVars)
}

// IsPublic returns true if the variable is a public input.
func (c *Circuit) IsPublic(v Variable) bool {
	return c.publicVars[v]
}

// GetWitness returns the assigned value of a variable.
func (c *Circuit) GetWitness(v Variable) *big.Int {
	return c.getWitness(v)
}

func (c *Circuit) getWitness(v Variable) *big.Int {
	if val, ok := c.witness[v]; ok {
		return new(big.Int).Set(val)
	}
	return new(big.Int)
}

// CheckConstraints verifies all R1CS constraints are satisfied by the
// current witness assignment. Returns nil if all constraints hold.
func (c *Circuit) CheckConstraints() error {
	for i, con := range c.constraints {
		aVal := c.evalLC(con.A)
		bVal := c.evalLC(con.B)
		cVal := c.evalLC(con.C)

		// Check: a * b = c (mod field).
		ab := new(big.Int).Mul(aVal, bVal)
		ab.Mod(ab, c.field)
		cVal.Mod(cVal, c.field)

		if ab.Cmp(cVal) != 0 {
			return errors.New("circuit: constraint " + big.NewInt(int64(i)).String() + " not satisfied")
		}
	}
	return nil
}

// evalLC evaluates a linear combination using the current witness.
func (c *Circuit) evalLC(lc LinearCombination) *big.Int {
	result := new(big.Int)
	for _, term := range lc {
		val := c.getWitness(term.Var)
		prod := new(big.Int).Mul(term.Coeff, val)
		result.Add(result, prod)
	}
	return result.Mod(result, c.field)
}

// Verify checks a mock proof against the circuit's public inputs.
// In production, this would verify a Groth16/PLONK proof. The mock
// implementation checks that public inputs match and the proof data
// has the expected structure.
func (c *Circuit) Verify(publicInputs map[Variable]*big.Int, proofData []byte) (bool, error) {
	if !c.finalized {
		return false, ErrCircuitNotReady
	}
	if len(proofData) < 32 {
		return false, ErrProofTooShort
	}

	// Verify public inputs match the circuit's witness.
	pubCount := 0
	for v := range c.publicVars {
		if v == OneVar {
			continue
		}
		pubCount++
	}
	if len(publicInputs) != pubCount {
		return false, ErrPublicInputCount
	}

	for v, expected := range publicInputs {
		if !c.publicVars[v] {
			return false, ErrInvalidVariable
		}
		actual := c.getWitness(v)
		expectedMod := new(big.Int).Mod(expected, c.field)
		if actual.Cmp(expectedMod) != 0 {
			return false, ErrVerificationFail
		}
	}

	// Check all constraints are satisfied (mock verification).
	if err := c.CheckConstraints(); err != nil {
		return false, err
	}

	return true, nil
}

// PublicVariables returns all public variable indices (excluding OneVar).
func (c *Circuit) PublicVariables() []Variable {
	vars := make([]Variable, 0, len(c.publicVars)-1)
	for v := range c.publicVars {
		if v == OneVar {
			continue
		}
		vars = append(vars, v)
	}
	return vars
}
