package vm

// evm_call_handlers.go implements CALL/DELEGATECALL/STATICCALL handler logic
// with value transfer, 63/64 gas forwarding (EIP-150), call depth limits
// (1024), precompile detection and dispatch, cold/warm access gas (EIP-2929),
// and return data handling.

import (
	"errors"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// CallHandler orchestrates CALL-family opcode execution. It wraps the
// depth checking, gas computation, precompile routing, value transfer,
// and state snapshot/revert logic into a single reusable component.
type CallHandler struct {
	evm          *EVM
	maxCallDepth int
}

// NewCallHandler creates a CallHandler bound to the given EVM instance.
func NewCallHandler(evm *EVM) *CallHandler {
	maxDepth := evm.Config.MaxCallDepth
	if maxDepth == 0 {
		maxDepth = MaxCallDepth
	}
	return &CallHandler{
		evm:          evm,
		maxCallDepth: maxDepth,
	}
}

// CallHandlerParams holds validated parameters for a call operation.
type CallHandlerParams struct {
	Kind     CallKind
	Caller   types.Address
	Target   types.Address
	Value    *big.Int
	Input    []byte
	Gas      uint64
	IsStatic bool
}

// CallHandlerResult holds the outcome of a call handler execution.
type CallHandlerResult struct {
	ReturnData []byte
	GasUsed    uint64
	GasLeft    uint64
	Success    bool
	Err        error
}

// HandleCall executes a CALL-family operation with full lifecycle management.
// It checks depth limits, detects precompiles, snapshots state, transfers
// value (for CALL), executes code, and handles revert/error cleanup.
func (ch *CallHandler) HandleCall(params *CallHandlerParams) *CallHandlerResult {
	result := &CallHandlerResult{GasLeft: params.Gas}

	// Depth check.
	if ch.evm.depth > ch.maxCallDepth {
		result.Err = ErrMaxCallDepthExceeded
		return result
	}

	// Static context check: disallow value transfers.
	if params.IsStatic && params.Value != nil && params.Value.Sign() > 0 {
		result.Err = ErrWriteProtection
		return result
	}

	// Check for precompiled contract.
	if p, ok := ch.evm.precompile(params.Target); ok {
		return ch.runPrecompile(p, params)
	}

	if ch.evm.StateDB == nil {
		result.Err = errors.New("call handler: no state database")
		return result
	}

	// Snapshot for revert on failure.
	snapshot := ch.evm.StateDB.Snapshot()

	// For CALL: create target account if needed, transfer value.
	if params.Kind == CallKindCall {
		if !ch.evm.StateDB.Exist(params.Target) {
			ch.evm.StateDB.CreateAccount(params.Target)
		}
		if params.Value != nil && params.Value.Sign() > 0 {
			callerBal := ch.evm.StateDB.GetBalance(params.Caller)
			if callerBal.Cmp(params.Value) < 0 {
				result.Err = errors.New("call handler: insufficient balance")
				return result
			}
			ch.evm.StateDB.SubBalance(params.Caller, params.Value)
			ch.evm.StateDB.AddBalance(params.Target, params.Value)

			if ch.evm.forkRules.IsEIP7708 && params.Caller != params.Target {
				EmitTransferLog(ch.evm.StateDB, params.Caller, params.Target, params.Value)
			}
		}
	}

	// Get the code to execute.
	var codeAddr types.Address
	var executionAddr types.Address
	switch params.Kind {
	case CallKindCall, CallKindStaticCall:
		codeAddr = params.Target
		executionAddr = params.Target
	case CallKindCallCode:
		codeAddr = params.Target
		executionAddr = params.Caller
	case CallKindDelegateCall:
		codeAddr = params.Target
		executionAddr = params.Caller
	}

	code := ch.evm.StateDB.GetCode(codeAddr)
	if len(code) == 0 {
		result.Success = true
		return result
	}

	// Build the contract.
	var contractValue *big.Int
	switch params.Kind {
	case CallKindCall:
		contractValue = params.Value
	case CallKindCallCode:
		contractValue = params.Value
	case CallKindDelegateCall:
		contractValue = nil // inherits from parent
	case CallKindStaticCall:
		contractValue = new(big.Int)
	}

	contract := NewContract(params.Caller, executionAddr, contractValue, params.Gas)
	contract.Code = code
	contract.CodeHash = ch.evm.StateDB.GetCodeHash(codeAddr)

	// Set read-only mode for STATICCALL.
	prevReadOnly := ch.evm.readOnly
	if params.Kind == CallKindStaticCall {
		ch.evm.readOnly = true
	}

	// Execute.
	ch.evm.depth++
	ret, err := ch.evm.Run(contract, params.Input)
	ch.evm.depth--

	// Restore read-only mode.
	ch.evm.readOnly = prevReadOnly

	gasLeft := contract.Gas

	if err != nil && !errors.Is(err, ErrExecutionReverted) {
		// Non-revert error: revert state, consume all gas.
		ch.evm.StateDB.RevertToSnapshot(snapshot)
		gasLeft = 0
	} else if errors.Is(err, ErrExecutionReverted) {
		// Revert: revert state, return remaining gas.
		ch.evm.StateDB.RevertToSnapshot(snapshot)
	}

	result.ReturnData = ret
	result.GasLeft = gasLeft
	result.GasUsed = params.Gas - gasLeft
	result.Success = err == nil
	result.Err = err
	return result
}

// runPrecompile handles calling a precompiled contract with gas accounting.
func (ch *CallHandler) runPrecompile(p PrecompiledContract, params *CallHandlerParams) *CallHandlerResult {
	result := &CallHandlerResult{GasLeft: params.Gas}

	output, gasLeft, err := runPrecompile(p, params.Input, params.Gas)
	result.ReturnData = output
	result.GasLeft = gasLeft
	result.GasUsed = params.Gas - gasLeft
	result.Success = err == nil
	result.Err = err
	return result
}

// GasForCall computes the gas to forward to a child call using the EIP-150
// 63/64 rule. If transfersValue is true, the 2300 gas stipend is added to
// the child gas (but not deducted from the caller).
func GasForCall(available, requested uint64, transfersValue bool) (childGas, callerDeduction uint64) {
	// EIP-150: cap at 63/64 of available gas.
	maxGas := available - available/CallGasFraction
	if requested > maxGas {
		requested = maxGas
	}

	callerDeduction = requested

	// When value is transferred, callee receives a 2300 gas stipend for free.
	if transfersValue {
		childGas = safeAddU64(requested, CallStipend)
	} else {
		childGas = requested
	}
	return childGas, callerDeduction
}

// ReturnGasFromCall computes how much gas to credit back to the caller after
// a child call completes. The stipend gas is subtracted from the returned gas
// since it was never charged to the caller.
func ReturnGasFromCall(returnGas uint64, transfersValue bool) uint64 {
	if transfersValue {
		if returnGas >= CallStipend {
			return returnGas - CallStipend
		}
		return 0
	}
	return returnGas
}

// ColdAccessGasForCall computes the EIP-2929 cold/warm access gas cost for
// a CALL-family opcode targeting addr. If the address is cold, it is warmed
// and the extra gas (ColdAccountAccessCost - WarmStorageReadCost) is returned.
// If warm, returns 0.
func ColdAccessGasForCall(evm *EVM, addr types.Address) uint64 {
	return gasEIP2929AccountCheck(evm, addr)
}

// CallMemoryGas computes the memory expansion gas for a CALL-family opcode
// given the input and output memory regions.
func CallMemoryGas(mem *Memory, inOffset, inSize, retOffset, retSize uint64) (uint64, bool) {
	// Determine the maximum memory extent required.
	var maxEnd uint64
	if inSize > 0 {
		inEnd := inOffset + inSize
		if inEnd < inOffset {
			return 0, false // overflow
		}
		if inEnd > maxEnd {
			maxEnd = inEnd
		}
	}
	if retSize > 0 {
		retEnd := retOffset + retSize
		if retEnd < retOffset {
			return 0, false // overflow
		}
		if retEnd > maxEnd {
			maxEnd = retEnd
		}
	}
	if maxEnd == 0 {
		return 0, true
	}
	return MemoryCost(uint64(mem.Len()), maxEnd)
}

// CopyReturnData copies return data into the caller's memory, truncating
// if the return data is shorter than the output buffer.
func CopyReturnData(mem *Memory, retOffset, retSize uint64, returnData []byte) {
	if retSize == 0 || len(returnData) == 0 {
		return
	}
	copyLen := retSize
	if uint64(len(returnData)) < copyLen {
		copyLen = uint64(len(returnData))
	}
	mem.Set(retOffset, copyLen, returnData[:copyLen])
}

// IsValueTransfer returns true if val is non-nil and positive.
func IsValueTransfer(val *big.Int) bool {
	return val != nil && val.Sign() > 0
}

// CallValueGasCost returns the gas surcharge for a CALL that transfers value.
// Returns CallValueTransferGas (9000) for value-bearing calls, plus
// CallNewAccountGas (25000) if the recipient does not exist.
func CallValueGasCost(stateDB StateDB, target types.Address, value *big.Int) uint64 {
	if value == nil || value.Sign() == 0 {
		return 0
	}
	gas := uint64(CallValueTransferGas)
	if stateDB != nil && !stateDB.Exist(target) {
		gas = safeAdd(gas, CallNewAccountGas)
	}
	return gas
}

// WarmTarget ensures the target address is warm in the access list and
// returns whether it was already warm. This is used by CALL-family opcodes
// under EIP-2929 to determine cold/warm gas costs.
func WarmTarget(evm *EVM, addr types.Address) bool {
	if evm.StateDB == nil {
		return true
	}
	wasWarm := evm.StateDB.AddressInAccessList(addr)
	if !wasWarm {
		evm.StateDB.AddAddressToAccessList(addr)
	}
	return wasWarm
}
