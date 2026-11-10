package vm

// precompile_router.go implements a fork-aware precompile dispatch router.
// ForkPrecompileRouter selects the active set of precompiled contracts based
// on the current fork rules and dispatches calls to them. Unlike the simpler
// PrecompileRouter in contract_call.go (which uses a static map), this router
// dynamically resolves precompiles per fork at query time.

import (
	"errors"
	"sort"

	"github.com/eth2030/eth2030/core/types"
)

// Errors returned by ForkPrecompileRouter.
var (
	ErrRouterNoPrecompile = errors.New("fork precompile router: no precompile at address")
	ErrRouterForkUnknown  = errors.New("fork precompile router: unknown fork configuration")
)

// ForkPrecompileRouter dispatches precompile calls based on the active fork
// rules. It lazily selects the correct precompile map when queried.
type ForkPrecompileRouter struct {
	// Per-fork precompile maps indexed by fork identifier.
	forkSets map[string]map[types.Address]PrecompiledContract
}

// NewForkPrecompileRouter creates a router pre-populated with the standard
// Ethereum fork precompile sets (Cancun and Glamsterdan).
func NewForkPrecompileRouter() *ForkPrecompileRouter {
	r := &ForkPrecompileRouter{
		forkSets: make(map[string]map[types.Address]PrecompiledContract),
	}
	r.forkSets[ForkCancun] = copyPrecompileMap(precompiledContractsCancunStandardBLS)
	r.forkSets[ForkPrague] = copyPrecompileMap(PrecompiledContractsPrague)
	r.forkSets[ForkGlamsterdan] = copyPrecompileMap(precompiledContractsGlamsterdanStandardBLS)
	return r
}

// RegisterFork adds or replaces the precompile set for a named fork.
func (r *ForkPrecompileRouter) RegisterFork(fork string, contracts map[types.Address]PrecompiledContract) {
	r.forkSets[fork] = copyPrecompileMap(contracts)
}

// GetPrecompile returns the precompile contract at addr for the given fork
// rules. Returns nil and false if the address is not an active precompile.
func (r *ForkPrecompileRouter) GetPrecompile(addr types.Address, rules ForkRules) (PrecompiledContract, bool) {
	m := r.selectMap(rules)
	if m == nil {
		return nil, false
	}
	p, ok := m[addr]
	return p, ok
}

// ActivePrecompiles returns the sorted list of precompile addresses active
// for the given fork rules.
func (r *ForkPrecompileRouter) ActivePrecompiles(rules ForkRules) []types.Address {
	m := r.selectMap(rules)
	if m == nil {
		return nil
	}
	addrs := make([]types.Address, 0, len(m))
	for a := range m {
		addrs = append(addrs, a)
	}
	sort.Slice(addrs, func(i, j int) bool {
		return routerAddrLess(addrs[i], addrs[j])
	})
	return addrs
}

// IsActive returns true if addr is an active precompile for the given fork.
func (r *ForkPrecompileRouter) IsActive(addr types.Address, rules ForkRules) bool {
	_, ok := r.GetPrecompile(addr, rules)
	return ok
}

// Execute runs the precompile at addr with the given input and gas. Returns
// the output, remaining gas, and any error.
func (r *ForkPrecompileRouter) Execute(addr types.Address, rules ForkRules, input []byte, gas uint64) ([]byte, uint64, error) {
	p, ok := r.GetPrecompile(addr, rules)
	if !ok {
		return nil, gas, ErrRouterNoPrecompile
	}
	return runPrecompile(p, input, gas)
}

// GasCost returns the gas cost for the precompile at addr with the given
// input under the specified fork rules.
func (r *ForkPrecompileRouter) GasCost(addr types.Address, rules ForkRules, input []byte) (uint64, error) {
	p, ok := r.GetPrecompile(addr, rules)
	if !ok {
		return 0, ErrRouterNoPrecompile
	}
	return p.RequiredGas(input), nil
}

// Count returns the number of active precompiles for the given fork rules.
func (r *ForkPrecompileRouter) Count(rules ForkRules) int {
	m := r.selectMap(rules)
	if m == nil {
		return 0
	}
	return len(m)
}

// selectMap resolves standard and historical native fork configurations.
func (r *ForkPrecompileRouter) selectMap(rules ForkRules) map[types.Address]PrecompiledContract {
	switch {
	case rules.LegacyNativeBLS || rules.IsIPlus:
		return SelectPrecompiles(rules)
	case rules.IsGlamsterdan:
		return r.forkSets[ForkGlamsterdan]
	case rules.IsPrague:
		return r.forkSets[ForkPrague]
	default:
		return r.forkSets[ForkCancun]
	}
}

// copyPrecompileMap creates a shallow copy of a precompile address map.
func copyPrecompileMap(src map[types.Address]PrecompiledContract) map[types.Address]PrecompiledContract {
	if src == nil {
		return nil
	}
	dst := make(map[types.Address]PrecompiledContract, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// routerAddrLess compares two addresses byte-by-byte for sorting.
func routerAddrLess(a, b types.Address) bool {
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
