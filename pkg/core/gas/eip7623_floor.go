package gas

import (
	"fmt"

	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/core/vm"
)

// EIP-7623 calldata cost floor.
//
// EIP-7623 introduces a minimum gas charge for transactions with significant
// calldata, incentivizing the use of EIP-4844 blob transactions instead. The
// floor cost is calculated using a token-based approach:
//
//   tokens = zero_bytes * 1 + nonzero_bytes * 4
//   floor_gas = TX_BASE_COST + tokens * TOTAL_COST_FLOOR_PER_TOKEN
//
// After execution, the actual gas charged is: max(execution_gas, floor_gas).
// If the floor exceeds execution gas, the difference is added to the final
// gas used. This ensures calldata-heavy transactions pay at least the floor.
//
// EIP-7976 (Glamsterdan) modifies the floor calculation:
//   - All bytes (zero and nonzero) are weighted equally: tokens = total_bytes * 4
//   - TOTAL_COST_FLOOR_PER_TOKEN increases from 10 to 16
//   - TX_BASE_COST uses the Glamsterdan value (4500)
//   - EIP-7981: access list data tokens are included in the floor

// FloorGasResult holds the result of an EIP-7623 calldata floor calculation.
type FloorGasResult struct {
	// FloorGas is the minimum gas the transaction must consume.
	FloorGas uint64

	// Tokens is the number of calldata tokens computed from the data.
	Tokens uint64

	// IsFloorActive indicates whether the floor exceeds the standard
	// execution gas (i.e., the floor actually imposes additional cost).
	IsFloorActive bool

	// EffectiveGas is max(executionGas, floorGas) -- the gas that
	// will actually be charged to the sender.
	EffectiveGas uint64
}

// CalcFloorGas computes the EIP-7623 calldata floor gas for a transaction.
// The floor is based on the calldata byte composition:
//   - Zero bytes contribute 1 token each
//   - Non-zero bytes contribute 4 tokens each
//   - Floor = TxGas + tokens * TotalCostFloorPerToken
func CalcFloorGas(data []byte, isCreate bool) FloorGasResult {
	tokens := calldataTokens(data)
	floor := TxGas + tokens*TotalCostFloorPerToken
	return FloorGasResult{
		FloorGas: floor,
		Tokens:   tokens,
	}
}

// CalcFloorGasGlamst computes the EIP-7976 calldata floor gas for Glamsterdan.
// Under EIP-7976, all calldata bytes are weighted equally:
//   - floor_tokens = total_bytes * 4
//   - EIP-7981: access list bytes are also counted as tokens
//   - Floor = TxBaseGlamsterdam + total_tokens * TotalCostFloorPerTokenGlamst
func CalcFloorGasGlamst(data []byte, accessList types.AccessList, isCreate bool) FloorGasResult {
	calldataFloorTokens := uint64(len(data)) * 4
	alTokens := accessListDataTokens(accessList)
	totalTokens := calldataFloorTokens + alTokens

	floor := vm.TxBaseGlamsterdam + totalTokens*TotalCostFloorPerTokenGlamst
	return FloorGasResult{
		FloorGas: floor,
		Tokens:   totalTokens,
	}
}

// ApplyCalldataFloor applies the EIP-7623 calldata floor to a transaction's
// gas usage. It returns the effective gas (max of execution gas and floor gas)
// and whether the floor was binding.
//
// This is the core accounting integration point: after EVM execution and
// refund calculation, the calldata floor may override the gas used if the
// standard execution path was cheaper than the floor.
//
// Parameters:
//   - executionGas: gas used after EVM execution and refund application
//   - data: the transaction's calldata
//   - isCreate: whether the transaction creates a contract
//
// Returns the effective gas to charge and whether the floor was applied.
func ApplyCalldataFloor(executionGas uint64, data []byte, isCreate bool) (effectiveGas uint64, floorApplied bool) {
	result := CalcFloorGas(data, isCreate)
	if result.FloorGas > executionGas {
		return result.FloorGas, true
	}
	return executionGas, false
}

// ApplyCalldataFloorGlamst applies the EIP-7976 calldata floor for Glamsterdan
// transactions. This is the Glamsterdan variant of ApplyCalldataFloor that
// uses the increased floor cost and equal byte weighting.
func ApplyCalldataFloorGlamst(executionGas uint64, data []byte, accessList types.AccessList, isCreate bool) (effectiveGas uint64, floorApplied bool) {
	result := CalcFloorGasGlamst(data, accessList, isCreate)
	if result.FloorGas > executionGas {
		return result.FloorGas, true
	}
	return executionGas, false
}

// CalcEffectiveGas computes the final gas to charge for a transaction,
// taking into account EVM execution, refunds, and the calldata floor.
//
// The gas accounting flow is:
//  1. Compute intrinsic gas (base + calldata + access list + auth costs)
//  2. Execute EVM with (gas_limit - intrinsic_gas) available
//  3. Compute execution_gas = intrinsic + (available - remaining)
//  4. Apply EIP-3529 refund: execution_gas -= min(refund, execution_gas/5)
//  5. Apply calldata floor: effective_gas = max(execution_gas, floor_gas)
//  6. Refund (gas_limit - effective_gas) to the sender
//
// This function implements step 5.
func CalcEffectiveGas(config *config.ChainConfig, headerTime uint64, executionGas uint64, data []byte, accessList types.AccessList, isCreate bool) (effectiveGas uint64, floorApplied bool) {
	if config == nil {
		return executionGas, false
	}

	// EIP-7623 is activated with Prague.
	if !config.IsPrague(headerTime) {
		return executionGas, false
	}

	if config.IsGlamsterdan(headerTime) {
		return ApplyCalldataFloorGlamst(executionGas, data, accessList, isCreate)
	}
	return ApplyCalldataFloor(executionGas, data, isCreate)
}

// CalcFloorGasForTx computes the calldata floor gas for a transaction,
// automatically selecting the correct calculation based on the active fork.
func CalcFloorGasForTx(config *config.ChainConfig, headerTime uint64, tx *types.Transaction) FloorGasResult {
	data := tx.Data()
	isCreate := tx.To() == nil

	if config != nil && config.IsGlamsterdan(headerTime) {
		return CalcFloorGasGlamst(data, tx.AccessList(), isCreate)
	}
	return CalcFloorGas(data, isCreate)
}

// FloorGasExcess computes how much additional gas the calldata floor
// imposes beyond what the standard execution path would charge. Returns
// zero if the floor is not binding.
//
// This is useful for transaction pool validation: a transaction whose
// gas limit is sufficient for standard execution but insufficient for
// the floor should be rejected early.
func FloorGasExcess(config *config.ChainConfig, headerTime uint64, tx *types.Transaction, standardIntrinsicGas uint64) uint64 {
	result := CalcFloorGasForTx(config, headerTime, tx)
	if result.FloorGas > standardIntrinsicGas {
		return result.FloorGas - standardIntrinsicGas
	}
	return 0
}

// ValidateGasLimitCoversFloor checks that a transaction's gas limit is
// sufficient to cover both the standard intrinsic gas and the calldata floor.
// This should be called during transaction validation (e.g., in the txpool)
// to reject transactions that would inevitably fail the floor check.
func ValidateGasLimitCoversFloor(config *config.ChainConfig, headerTime uint64, tx *types.Transaction) error {
	if config == nil || !config.IsPrague(headerTime) {
		return nil
	}

	result := CalcFloorGasForTx(config, headerTime, tx)
	if tx.Gas() < result.FloorGas {
		return fmt.Errorf("%w: gas_limit=%d, floor=%d (tokens=%d)",
			ErrIntrinsicGasTooLow, tx.Gas(), result.FloorGas, result.Tokens)
	}
	return nil
}

// RefundWithFloor applies the EIP-3529 refund cap and then the calldata
// floor in the correct order. This encapsulates the full post-execution
// gas adjustment logic.
//
// Parameters:
//   - gasUsed: gas consumed by intrinsic cost + EVM execution
//   - refund: accumulated SSTORE/SELFDESTRUCT refund counter
//   - data: transaction calldata
//   - accessList: transaction access list
//   - isCreate: whether the transaction is a contract creation
//   - config: chain configuration
//   - headerTime: block timestamp for fork detection
//
// Returns:
//   - finalGas: the gas amount to charge the sender
//   - refundApplied: how much refund was actually applied
//   - floorApplied: whether the calldata floor overrode the refunded gas
func RefundWithFloor(
	gasUsed uint64,
	refund uint64,
	data []byte,
	accessList types.AccessList,
	isCreate bool,
	config *config.ChainConfig,
	headerTime uint64,
) (finalGas uint64, refundApplied uint64, floorApplied bool) {
	// Step 1: Apply EIP-3529 refund cap (max refund = gasUsed / 5).
	maxRefund := gasUsed / 5
	if refund > maxRefund {
		refund = maxRefund
	}
	refundApplied = refund
	afterRefund := gasUsed - refund

	// Step 2: Apply calldata floor.
	finalGas, floorApplied = CalcEffectiveGas(config, headerTime, afterRefund, data, accessList, isCreate)
	return finalGas, refundApplied, floorApplied
}
