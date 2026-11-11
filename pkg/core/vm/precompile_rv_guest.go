// precompile_riscv.go wires RISC-V guest programs for the three highest-frequency
// EVM precompiles (ECRECOVER/0x01, SHA-256/0x02) at the I+ fork.
//
// When IsIPlus is active, calls to these addresses execute via the RISC-V CPU
// emulator (pkg/zkvm RunPrecompileGuest) instead of the Go native path.
// All other precompiles remain on the Go path.
//
// The KECCAK256 opcode (0x20) is not a precompile; it stays in the jump table.
//
// Part of US-EL-2.3 (I+ precompile router).
package vm

import (
	"fmt"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/zkvm"
)

// rvPrecompileGasBase is the base gas budget given to a RISC-V guest when
// invoked via the precompile wrapper. The RISC-V CPU charges 1 gas/instruction;
// for hash precompiles the programs are 5 instructions, so this budget is
// intentionally generous to avoid spurious out-of-gas on malformed inputs.
const rvPrecompileGasBase = 200_000

// rvPrecompile wraps a RISC-V guest program as a PrecompiledContract.
// It delegates gas estimation to the underlying native contract and execution
// to RunPrecompileGuest.
type rvPrecompile struct {
	ecallCode uint32
	native    PrecompiledContract // fallback for gas cost
}

func (r *rvPrecompile) RequiredGas(input []byte) uint64 {
	return r.native.RequiredGas(input)
}

func (r *rvPrecompile) Run(input []byte) ([]byte, error) {
	out, err := zkvm.RunPrecompileGuest(r.ecallCode, input, rvPrecompileGasBase)
	if err != nil {
		return nil, fmt.Errorf("riscv precompile: %w", err)
	}
	return out, nil
}

// init patches PrecompiledContractsIPlus to replace ECRECOVER (0x01) and
// SHA-256 (0x02) with their RISC-V equivalents.
func init() {
	ecrecoverAddr := types.BytesToAddress([]byte{1})
	sha256Addr := types.BytesToAddress([]byte{2})

	for _, table := range []map[types.Address]PrecompiledContract{PrecompiledContractsIPlus, precompiledContractsIPlusStandardBLS} {
		if native, ok := table[ecrecoverAddr]; ok {
			table[ecrecoverAddr] = &rvPrecompile{
				ecallCode: zkvm.RVEcallECRecover,
				native:    native,
			}
		}
		if native, ok := table[sha256Addr]; ok {
			table[sha256Addr] = &rvPrecompile{
				ecallCode: zkvm.RVEcallSHA256,
				native:    native,
			}
		}
	}
}

// IsPrecompileRISCV reports whether the given address is routed through the
// RISC-V execution path at the I+ fork.
func IsPrecompileRISCV(addr types.Address) bool {
	addr1 := types.BytesToAddress([]byte{1})
	addr2 := types.BytesToAddress([]byte{2})
	return addr == addr1 || addr == addr2
}
