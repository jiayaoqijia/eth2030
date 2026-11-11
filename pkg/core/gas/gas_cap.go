package gas

import (
	"errors"

	"github.com/eth2030/eth2030/core/config"
)

// EIP-7825: Transaction Gas Limit Cap
//
// Introduces a protocol-level cap on the maximum gas used by a single
// transaction to 16,777,216 (2^24). Transactions exceeding this limit
// are rejected at both the txpool and block validation level.

// MaxTransactionGas is the protocol-level cap on individual transaction
// gas limits, per EIP-7825.
const MaxTransactionGas uint64 = 1 << 24 // 16,777,216

// ErrTxGasLimitExceeded is returned when a transaction's gas limit exceeds
// the EIP-7825 cap.
var ErrTxGasLimitExceeded = errors.New("transaction gas limit exceeds maximum (EIP-7825)")

// ValidateTransactionGasLimit checks whether a transaction's gas limit
// is within the EIP-7825 cap. Returns ErrTxGasLimitExceeded if gasLimit
// exceeds MaxTransactionGas.
func ValidateTransactionGasLimit(gasLimit uint64) error {
	if gasLimit > MaxTransactionGas {
		return ErrTxGasLimitExceeded
	}
	return nil
}

// IsGasLimitCapped returns true if the EIP-7825 transaction gas cap is
// active for the given chain config and block timestamp. The gas cap
// activates with the Osaka fork.
func IsGasLimitCapped(config *config.ChainConfig, time uint64) bool {
	return config != nil && config.IsOsaka(time)
}
