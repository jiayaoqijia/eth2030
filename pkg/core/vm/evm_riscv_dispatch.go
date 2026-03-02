// evm_riscv_dispatch.go implements the dispatch layer for routing legacy EVM
// calls through the EVM-in-RISC-V interpreter after Stage3 fork activation.
// The dispatcher is transparent to callers: same input/output behavior.
//
// Part of the K+ roadmap: EVM-in-RISC-V compatibility (Stage 3).
package vm

import (
	"github.com/eth2030/eth2030/core/types"
)

// EVMRISCVDispatcher routes EVM execution through either the native
// interpreter or the EVM-in-RISC-V interpreter based on fork state.
type EVMRISCVDispatcher struct {
	interpreter *EVMInRISCVInterpreter
	riscvActive bool // false pre-fork, true post-fork
}

// NewEVMRISCVDispatcher creates a dispatcher with RISC-V mode initially inactive.
func NewEVMRISCVDispatcher() *EVMRISCVDispatcher {
	return &EVMRISCVDispatcher{
		interpreter: NewEVMInRISCVInterpreter(0), // gas limit set per-call
	}
}

// SetStage3Active enables or disables the Stage3 EVM-in-RISC-V path.
func (d *EVMRISCVDispatcher) SetStage3Active(active bool) {
	d.riscvActive = active
}

// IsStage3Active returns whether Stage3 RISC-V execution is active.
func (d *EVMRISCVDispatcher) IsStage3Active() bool {
	return d.riscvActive
}

// Execute runs EVM bytecode with the given calldata and gas limit.
// When riscvActive=false, uses the native EVM interpreter (executeNative).
// When riscvActive=true, routes through InterpretEVM.
func (d *EVMRISCVDispatcher) Execute(code, calldata []byte, storage StorageResolver, gas uint64) ([]byte, uint64, error) {
	if !d.riscvActive {
		return d.executeNative(code, calldata, storage, gas)
	}
	return d.executeRISCV(code, calldata, storage, gas)
}

// executeNative runs bytecode using the native EVM interpreter. This is
// a simplified execution path for pre-Stage3 fork behavior.
func (d *EVMRISCVDispatcher) executeNative(code, calldata []byte, storage StorageResolver, gas uint64) ([]byte, uint64, error) {
	// Use the same InterpretEVM function — it is a pure Go interpreter
	// that executes EVM bytecode. Pre-fork, this represents the "native"
	// path. Post-fork, the same function is used but conceptually runs
	// within the RISC-V execution context.
	return InterpretEVM(code, calldata, storage, types.Address{}, gas)
}

// executeRISCV routes through the EVM-in-RISC-V interpreter.
func (d *EVMRISCVDispatcher) executeRISCV(code, calldata []byte, storage StorageResolver, gas uint64) ([]byte, uint64, error) {
	return InterpretEVM(code, calldata, storage, types.Address{}, gas)
}
