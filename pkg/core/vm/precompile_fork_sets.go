// Package vm implements the Ethereum Virtual Machine.
//
// precompile_fork_sets.go manages fork-aware precompile activation, providing
// canonical precompile sets for each Ethereum hard fork, address range mapping,
// gas cost lookups by fork, and a unified execution router that selects the
// correct precompile implementation based on the active fork.
package vm

import (
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/eth2030/eth2030/core/types"
)

// Fork name constants used as keys for precompile set selection.
const (
	ForkHomestead   = "Homestead"
	ForkByzantium   = "Byzantium"
	ForkIstanbul    = "Istanbul"
	ForkCancun      = "Cancun"
	ForkPrague      = "Prague"
	ForkGlamsterdan = "Glamsterdan"
	ForkHogotaName  = "Hogota"
)

// Precompile address ranges.
const (
	// StandardPrecompileStart is the first standard precompile address (ecRecover).
	StandardPrecompileStart byte = 0x01
	// StandardPrecompileEnd is the last standard pre-Cancun precompile (pointEval).
	StandardPrecompileEnd byte = 0x0a
	// BLSPrecompileStart is the first BLS12-381 precompile address.
	BLSPrecompileStart byte = 0x0b
	// BLSPrecompileEnd is the last BLS12-381 precompile address.
	BLSPrecompileEnd byte = 0x13
)

// Errors for precompile fork set operations.
var (
	ErrForkNotFound       = errors.New("precompile fork set: unknown fork")
	ErrPrecompileNotFound = errors.New("precompile fork set: address not active in fork")
)

// PrecompileActivation records when a precompile was activated and its
// canonical gas cost function.
type PrecompileActivation struct {
	Address  types.Address
	Name     string
	Fork     string
	Contract PrecompiledContract
}

// ForkPrecompileSet holds the complete set of precompiled contracts active at
// a particular fork. It provides O(1) lookup and gas cost computation.
type ForkPrecompileSet struct {
	fork      string
	contracts map[types.Address]PrecompiledContract
	names     map[types.Address]string
	addresses []types.Address // sorted for deterministic iteration
}

// newForkPrecompileSet creates a ForkPrecompileSet from a slice of activations.
func newForkPrecompileSet(fork string, activations []PrecompileActivation) *ForkPrecompileSet {
	fps := &ForkPrecompileSet{
		fork:      fork,
		contracts: make(map[types.Address]PrecompiledContract, len(activations)),
		names:     make(map[types.Address]string, len(activations)),
	}
	for _, a := range activations {
		fps.contracts[a.Address] = a.Contract
		fps.names[a.Address] = a.Name
		fps.addresses = append(fps.addresses, a.Address)
	}
	sort.Slice(fps.addresses, func(i, j int) bool {
		return addressLessBytes(fps.addresses[i], fps.addresses[j])
	})
	return fps
}

// Fork returns the name of the fork this set represents.
func (fps *ForkPrecompileSet) Fork() string {
	return fps.fork
}

// IsActive returns true if the given address has an active precompile in this fork.
func (fps *ForkPrecompileSet) IsActive(addr types.Address) bool {
	_, ok := fps.contracts[addr]
	return ok
}

// Get returns the precompiled contract at the given address, or nil.
func (fps *ForkPrecompileSet) Get(addr types.Address) PrecompiledContract {
	return fps.contracts[addr]
}

// GasCost computes the gas cost for the precompile at addr with the given input.
// Returns ErrPrecompileNotFound if the address is not active.
func (fps *ForkPrecompileSet) GasCost(addr types.Address, input []byte) (uint64, error) {
	p, ok := fps.contracts[addr]
	if !ok {
		return 0, fmt.Errorf("%w: %s in fork %s", ErrPrecompileNotFound, addr.Hex(), fps.fork)
	}
	return p.RequiredGas(input), nil
}

// Run executes the precompile at addr, deducting gas. Returns output, remaining
// gas, and any error.
func (fps *ForkPrecompileSet) Run(addr types.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	p, ok := fps.contracts[addr]
	if !ok {
		return nil, gas, fmt.Errorf("%w: %s", ErrPrecompileNotFound, addr.Hex())
	}
	return runPrecompile(p, input, gas)
}

// Addresses returns the sorted list of active precompile addresses.
func (fps *ForkPrecompileSet) Addresses() []types.Address {
	result := make([]types.Address, len(fps.addresses))
	copy(result, fps.addresses)
	return result
}

// ContractMap returns a copy of the precompile contract map, suitable for
// passing to NewPrecompileRouter or the EVM constructor.
func (fps *ForkPrecompileSet) ContractMap() map[types.Address]PrecompiledContract {
	m := make(map[types.Address]PrecompiledContract, len(fps.contracts))
	for addr, c := range fps.contracts {
		m[addr] = c
	}
	return m
}

// Count returns the number of active precompiles.
func (fps *ForkPrecompileSet) Count() int {
	return len(fps.contracts)
}

// Name returns the human-readable name for the precompile at addr, or "".
func (fps *ForkPrecompileSet) Name(addr types.Address) string {
	return fps.names[addr]
}

// PrecompileForkManager maintains precompile sets for all known forks and
// provides fork-aware lookup. Thread-safe.
type PrecompileForkManager struct {
	mu   sync.RWMutex
	sets map[string]*ForkPrecompileSet
}

// NewPrecompileForkManager creates a manager pre-populated with the standard
// Ethereum fork precompile sets: Cancun, Prague, and Glamsterdan.
func NewPrecompileForkManager() *PrecompileForkManager {
	m := &PrecompileForkManager{
		sets: make(map[string]*ForkPrecompileSet),
	}
	m.registerDefaults()
	return m
}

// GetForkSet returns the precompile set for the named fork, or an error if
// the fork is unknown.
func (m *PrecompileForkManager) GetForkSet(fork string) (*ForkPrecompileSet, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	fps, ok := m.sets[fork]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrForkNotFound, fork)
	}
	return fps, nil
}

// RegisterForkSet adds or replaces the precompile set for a fork.
func (m *PrecompileForkManager) RegisterForkSet(fork string, activations []PrecompileActivation) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sets[fork] = newForkPrecompileSet(fork, activations)
}

// Forks returns the list of registered fork names, sorted alphabetically.
func (m *PrecompileForkManager) Forks() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.sets))
	for name := range m.sets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// IsPrecompileAt returns true if addr is active in the specified fork.
func (m *PrecompileForkManager) IsPrecompileAt(addr types.Address, fork string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	fps, ok := m.sets[fork]
	if !ok {
		return false
	}
	return fps.IsActive(addr)
}

// GasCostAt returns the gas cost for calling a precompile at addr in a
// specific fork.
func (m *PrecompileForkManager) GasCostAt(addr types.Address, input []byte, fork string) (uint64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	fps, ok := m.sets[fork]
	if !ok {
		return 0, fmt.Errorf("%w: %s", ErrForkNotFound, fork)
	}
	return fps.GasCost(addr, input)
}

// addressLessBytes compares two addresses in byte-lexicographic order.
func addressLessBytes(a, b types.Address) bool {
	for i := range a {
		if a[i] < b[i] {
			return true
		}
		if a[i] > b[i] {
			return false
		}
	}
	return false
}

// addr is a helper that converts a single byte to a precompile address.
func addr(b byte) types.Address {
	return types.BytesToAddress([]byte{b})
}

// addr2 is a helper that converts two bytes to a precompile address.
func addr2(hi, lo byte) types.Address {
	return types.BytesToAddress([]byte{hi, lo})
}

// registerDefaults populates the manager with canonical fork precompile sets.
func (m *PrecompileForkManager) registerDefaults() {
	// Cancun set includes the pre-Prague contracts.
	cancunActivations := []PrecompileActivation{
		{addr(0x01), "ecRecover", ForkHomestead, &ecrecover{}},
		{addr(0x02), "sha256", ForkHomestead, &sha256hash{}},
		{addr(0x03), "ripemd160", ForkHomestead, &ripemd160hash{}},
		{addr(0x04), "identity", ForkHomestead, &dataCopy{}},
		{addr(0x05), "modexp", ForkByzantium, &bigModExp{}},
		{addr(0x06), "ecAdd", ForkByzantium, &bn256Add{}},
		{addr(0x07), "ecMul", ForkByzantium, &bn256ScalarMul{}},
		{addr(0x08), "ecPairing", ForkByzantium, &bn256Pairing{}},
		{addr(0x09), "blake2f", ForkIstanbul, &blake2F{}},
		{addr(0x0a), "pointEval", ForkCancun, &kzgPointEvaluation{}},

		{addr2(0x01, 0x00), "p256Verify", ForkCancun, &p256Verify{}},
	}
	m.sets[ForkCancun] = newForkPrecompileSet(ForkCancun, cancunActivations)

	blsActivations := []PrecompileActivation{
		{addr(0x0b), "bls12G1Add", ForkPrague, PrecompiledContractsPrague[addr(0x0b)]},
		{addr(0x0c), "bls12G1MSM", ForkPrague, PrecompiledContractsPrague[addr(0x0c)]},
		{addr(0x0d), "bls12G2Add", ForkPrague, PrecompiledContractsPrague[addr(0x0d)]},
		{addr(0x0e), "bls12G2MSM", ForkPrague, PrecompiledContractsPrague[addr(0x0e)]},
		{addr(0x0f), "bls12Pairing", ForkPrague, PrecompiledContractsPrague[addr(0x0f)]},
		{addr(0x10), "bls12MapFpToG1", ForkPrague, PrecompiledContractsPrague[addr(0x10)]},
		{addr(0x11), "bls12MapFp2ToG2", ForkPrague, PrecompiledContractsPrague[addr(0x11)]},
	}
	pragueActivations := append(append([]PrecompileActivation(nil), cancunActivations...), blsActivations...)
	m.sets[ForkPrague] = newForkPrecompileSet(ForkPrague, pragueActivations)

	// Glamsterdan set: same addresses but with repriced gas for ecAdd, blake2f,
	// pointEval, and ecPairing. Also removes 0x12 (moved to system contract
	// per EIP-7997).
	glamActivations := []PrecompileActivation{
		{addr(0x01), "ecRecover", ForkHomestead, &ecrecover{}},
		{addr(0x02), "sha256", ForkHomestead, &sha256hash{}},
		{addr(0x03), "ripemd160", ForkHomestead, &ripemd160hash{}},
		{addr(0x04), "identity", ForkHomestead, &dataCopy{}},
		{addr(0x05), "modexp", ForkByzantium, &bigModExp{}},
		{addr(0x06), "ecAdd", ForkGlamsterdan, &bn256AddGlamsterdan{}},
		{addr(0x07), "ecMul", ForkByzantium, &bn256ScalarMul{}},
		{addr(0x08), "ecPairing", ForkGlamsterdan, &bn256PairingGlamsterdan{}},
		{addr(0x09), "blake2f", ForkGlamsterdan, &blake2FGlamsterdan{}},
		{addr(0x0a), "pointEval", ForkGlamsterdan, &kzgPointEvaluationGlamsterdan{}},

		// 0x12 removed: EIP-7997 deterministic CREATE2 factory (system contract).

		{addr2(0x01, 0x00), "p256Verify", ForkCancun, &p256Verify{}},
	}
	m.sets[ForkGlamsterdan] = newForkPrecompileSet(ForkGlamsterdan, append(glamActivations, blsActivations...))
}
