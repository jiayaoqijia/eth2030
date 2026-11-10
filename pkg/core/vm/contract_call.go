package vm

// contract_call.go implements the contract call mechanics for the EVM:
// CALL, STATICCALL, DELEGATECALL, and CALLCODE value transfer; gas stipend
// calculation (EIP-150 63/64 rule); depth limit checking; and precompile
// detection and routing.

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// Call operation errors.
var (
	ErrCallDepthExceeded       = errors.New("call: max depth exceeded")
	ErrCallInsufficientBalance = errors.New("call: insufficient balance for value transfer")
	ErrCallReadOnlyValue       = errors.New("call: value transfer in static context")
	ErrCallGasOverflow         = errors.New("call: gas calculation overflow")
	ErrCallInputBoundsCheck    = errors.New("call: input bounds exceed memory")
)

// CallKind identifies the type of call operation.
type CallKind uint8

const (
	// CallKindCall is a normal CALL.
	CallKindCall CallKind = iota
	// CallKindCallCode is a CALLCODE (runs callee code in caller context).
	CallKindCallCode
	// CallKindDelegateCall is a DELEGATECALL (preserves sender and value).
	CallKindDelegateCall
	// CallKindStaticCall is a STATICCALL (read-only, no state changes).
	CallKindStaticCall
)

// String returns a human-readable name for the call kind.
func (ck CallKind) String() string {
	switch ck {
	case CallKindCall:
		return "CALL"
	case CallKindCallCode:
		return "CALLCODE"
	case CallKindDelegateCall:
		return "DELEGATECALL"
	case CallKindStaticCall:
		return "STATICCALL"
	default:
		return fmt.Sprintf("CallKind(%d)", ck)
	}
}

// CallParams encapsulates the parameters for a CALL-family operation.
type CallParams struct {
	Kind         CallKind
	Caller       types.Address // address of the calling contract
	Target       types.Address // address being called
	Value        *big.Int      // ETH value transferred (nil for no transfer)
	GasProvided  uint64        // gas explicitly requested on the stack
	GasAvailable uint64        // total gas remaining in the caller
	InputOffset  uint64        // memory offset for call input data
	InputSize    uint64        // size of call input data
	RetOffset    uint64        // memory offset to write return data
	RetSize      uint64        // size of return data buffer
	IsStatic     bool          // true if execution is read-only
}

// CallResult holds the outcome of a CALL-family operation.
type CallResult struct {
	Success    bool   // true if the call succeeded (did not revert or error)
	ReturnData []byte // data returned by the callee
	GasUsed    uint64 // gas consumed by the call
	GasLeft    uint64 // gas remaining after the call
}

// CallGasCalculator computes the gas available for a child call per the
// EIP-150 63/64 rule and handles gas stipend for value-bearing calls.
type CallGasCalculator struct {
	// GasStipend is the bonus gas given to the callee when value is
	// transferred (2300 gas per Ethereum spec).
	GasStipend uint64
	// GasFraction is the denominator for the 63/64 rule (64).
	GasFraction uint64
}

// NewCallGasCalculator returns a CallGasCalculator with standard parameters.
func NewCallGasCalculator() *CallGasCalculator {
	return &CallGasCalculator{
		GasStipend:  CallStipend,     // 2300
		GasFraction: CallGasFraction, // 64
	}
}

// ChildGas computes the gas to forward to a child call.
//
// Per EIP-150, the caller retains at least 1/64 of its remaining gas:
//
//	maxChild = available - floor(available / 64)
//
// The actual child gas is min(requested, maxChild).
// If value is being transferred, a gas stipend of 2300 is added to the child.
func (cgc *CallGasCalculator) ChildGas(available, requested uint64, transfersValue bool) uint64 {
	// EIP-150: cap at 63/64 of available gas.
	retained := available / cgc.GasFraction
	maxChild := available - retained
	if requested > maxChild {
		requested = maxChild
	}

	// Stipend: when value is transferred, the callee receives an additional
	// 2300 gas that is not deducted from the caller.
	if transfersValue {
		requested = safeAddU64(requested, cgc.GasStipend)
	}
	return requested
}

// CallerCost returns the gas that must be deducted from the caller for
// forwarding childGas to the child. When a stipend was added, it is not
// charged to the caller.
func (cgc *CallGasCalculator) CallerCost(childGas uint64, transfersValue bool) uint64 {
	if transfersValue && childGas >= cgc.GasStipend {
		return childGas - cgc.GasStipend
	}
	return childGas
}

// safeAddU64 returns a + b, capped at math.MaxUint64.
func safeAddU64(a, b uint64) uint64 {
	if a > math.MaxUint64-b {
		return math.MaxUint64
	}
	return a + b
}

// CallDepthChecker validates the EVM call depth. The Ethereum spec limits
// call depth to 1024. Each CALL/CREATE increments the depth; exceeding
// the limit causes the call to fail (returning 0 on the stack) rather than
// consuming all gas.
type CallDepthChecker struct {
	MaxDepth int
}

// NewCallDepthChecker creates a CallDepthChecker with the standard 1024 limit.
func NewCallDepthChecker() *CallDepthChecker {
	return &CallDepthChecker{MaxDepth: MaxCallDepth}
}

// Check returns an error if the current depth would exceed the maximum
// when entering a new call frame.
func (cdc *CallDepthChecker) Check(currentDepth int) error {
	if currentDepth >= cdc.MaxDepth {
		return fmt.Errorf("%w: depth %d >= max %d",
			ErrCallDepthExceeded, currentDepth, cdc.MaxDepth)
	}
	return nil
}

// PrecompileRouter detects precompiled contracts and routes calls to them.
// Precompiles are special addresses (typically 0x01 through 0x13) with
// native implementations that bypass the EVM interpreter.
type PrecompileRouter struct {
	precompiles map[types.Address]PrecompiledContract
}

// NewPrecompileRouter creates a PrecompileRouter with the given precompile map.
func NewPrecompileRouter(precompiles map[types.Address]PrecompiledContract) *PrecompileRouter {
	return &PrecompileRouter{precompiles: precompiles}
}

// IsPrecompile returns true if the address is a precompiled contract.
func (pr *PrecompileRouter) IsPrecompile(addr types.Address) bool {
	if pr.precompiles == nil {
		return false
	}
	_, ok := pr.precompiles[addr]
	return ok
}

// RunPrecompile executes a precompiled contract. Returns the output, gas
// remaining, and any error. Returns ErrOutOfGas if the supplied gas is
// insufficient for the precompile's required gas.
func (pr *PrecompileRouter) RunPrecompile(addr types.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	p, ok := pr.precompiles[addr]
	if !ok {
		return nil, gas, fmt.Errorf("no precompile at address %s", addr.Hex())
	}
	return runPrecompile(p, input, gas)
}

// CallContext holds the contextual information needed to execute a CALL-family
// operation. It combines the gas calculator, depth checker, and precompile
// router into a single facade.
type CallContext struct {
	GasCalc    *CallGasCalculator
	DepthCheck *CallDepthChecker
	Router     *PrecompileRouter
}

// NewCallContext creates a CallContext with standard parameters and the given
// precompile map.
func NewCallContext(precompiles map[types.Address]PrecompiledContract) *CallContext {
	return &CallContext{
		GasCalc:    NewCallGasCalculator(),
		DepthCheck: NewCallDepthChecker(),
		Router:     NewPrecompileRouter(precompiles),
	}
}

// PrepareCall validates and prepares a CALL-family operation. It checks depth
// limits, validates value transfers, computes child gas, and detects precompiles.
// Returns the gas to send to the child and whether the target is a precompile.
func (cc *CallContext) PrepareCall(params *CallParams, currentDepth int) (childGas uint64, isPrecompile bool, err error) {
	// Depth check.
	if err := cc.DepthCheck.Check(currentDepth); err != nil {
		return 0, false, err
	}

	// Static context: disallow value transfers.
	if params.IsStatic && params.Value != nil && params.Value.Sign() > 0 {
		return 0, false, ErrCallReadOnlyValue
	}

	// Determine if value is being transferred.
	transfersValue := params.Value != nil && params.Value.Sign() > 0

	// Compute child gas per EIP-150 63/64 rule.
	childGas = cc.GasCalc.ChildGas(params.GasAvailable, params.GasProvided, transfersValue)

	// Detect precompile.
	isPrecompile = cc.Router.IsPrecompile(params.Target)

	return childGas, isPrecompile, nil
}

// CallValueTransfer handles the ETH value transfer for a CALL operation.
// It checks the caller's balance, debits the caller, and credits the callee.
// For CALLCODE and DELEGATECALL, no actual transfer occurs (handled differently
// by the opcodes). Returns an error if the balance is insufficient.
func CallValueTransfer(stateDB StateDB, caller, recipient types.Address, value *big.Int) error {
	if value == nil || value.Sign() == 0 {
		return nil
	}
	if stateDB == nil {
		return errors.New("call: no state database for value transfer")
	}
	balance := stateDB.GetBalance(caller)
	if balance.Cmp(value) < 0 {
		return fmt.Errorf("%w: caller %s has %s, needs %s",
			ErrCallInsufficientBalance, caller.Hex(), balance, value)
	}
	stateDB.SubBalance(caller, value)
	stateDB.AddBalance(recipient, value)
	return nil
}

// CallValueCost returns the gas cost for a value transfer.
// Standard: 9000 gas for nonzero value, 0 for zero value.
// If the recipient does not exist and value > 0, an additional 25000 gas
// for new account creation is added.
func CallValueCost(stateDB StateDB, recipient types.Address, value *big.Int, isGlamsterdan bool) uint64 {
	if value == nil || value.Sign() == 0 {
		return 0
	}

	var transferGas, newAccountGas uint64
	if isGlamsterdan {
		transferGas = CallValueTransferGlamst // 2000 (EIP-2780)
		newAccountGas = CallNewAccountGlamst  // 26000
	} else {
		transferGas = CallValueTransferGas // 9000
		newAccountGas = CallNewAccountGas  // 25000
	}

	gas := transferGas
	if stateDB != nil && !stateDB.Exist(recipient) {
		gas = safeAddU64(gas, newAccountGas)
	}
	return gas
}

// EffectiveCallAddress returns the address that will be used for storage
// operations depending on the call kind:
//   - CALL: the target address (code and storage belong to callee)
//   - CALLCODE: the caller address (code from callee, storage from caller)
//   - DELEGATECALL: the caller address (preserves everything from caller)
//   - STATICCALL: the target address (read-only)
func EffectiveCallAddress(kind CallKind, caller, target types.Address) types.Address {
	switch kind {
	case CallKindCallCode, CallKindDelegateCall:
		return caller
	default:
		return target
	}
}

// EffectiveCallValue returns the value that the callee sees via CALLVALUE:
//   - CALL/CALLCODE: the value from the stack
//   - DELEGATECALL: the value from the parent context (not from the stack)
//   - STATICCALL: always zero
func EffectiveCallValue(kind CallKind, stackValue, parentValue *big.Int) *big.Int {
	switch kind {
	case CallKindDelegateCall:
		if parentValue != nil {
			return parentValue
		}
		return new(big.Int)
	case CallKindStaticCall:
		return new(big.Int)
	default:
		if stackValue != nil {
			return stackValue
		}
		return new(big.Int)
	}
}
