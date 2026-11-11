// execution_requests.go implements EIP-7685 execution layer triggered requests.
//
// EIP-7685 introduces a general framework for the execution layer to trigger
// requests to the consensus layer. Each request is prefixed by a type byte:
//   - Type 0x00: Deposit requests (EIP-6110)
//   - Type 0x01: Withdrawal requests (EIP-7002)
//   - Type 0x02: Consolidation requests (EIP-7251)
//
// Requests are encoded as type || data and must appear in ascending type order
// within a block. The consensus layer validates the content; the execution layer
// validates structure and ordering.
package requests

import (
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
)

// Execution request type constants per EIP-7685 (re-exported for execution_requests module).
const (
	// ExecReqDepositType is the type byte for deposit requests (EIP-6110).
	ExecReqDepositType byte = 0x00

	// ExecReqWithdrawalType is the type byte for withdrawal requests (EIP-7002).
	ExecReqWithdrawalType byte = 0x01

	// ExecReqConsolidationType is the type byte for consolidation requests (EIP-7251).
	ExecReqConsolidationType byte = 0x02

	// ExecReqMaxType is the highest valid request type.
	ExecReqMaxType byte = 0x02

	// DepositRequestDataLen is the expected data length for a deposit request.
	// pubkey(48) + withdrawal_credentials(32) + amount(8) + signature(96) + index(8) = 192
	DepositRequestDataLen = 192

	// WithdrawalRequestDataLen is the expected data length for a withdrawal request.
	// source_address(20) + validator_pubkey(48) + amount(8) = 76
	WithdrawalRequestDataLen = 76

	// ConsolidationRequestDataLen is the expected data length for a consolidation request.
	// source_address(20) + source_pubkey(48) + target_pubkey(48) = 116
	ConsolidationRequestDataLen = 116
)

// Execution request errors.
var (
	ErrExecReqNil               = errors.New("execution_requests: nil request list")
	ErrExecReqEmpty             = errors.New("execution_requests: empty request (no data)")
	ErrExecReqTooShort          = errors.New("execution_requests: request too short (needs type + data)")
	ErrExecReqUnknownType       = errors.New("execution_requests: unknown request type")
	ErrExecReqTypesNotAscending = errors.New("execution_requests: request types not in ascending order")
	ErrExecReqDuplicateType     = errors.New("execution_requests: duplicate request type")
	ErrExecReqInvalidDataLen    = errors.New("execution_requests: invalid data length for request type")
	ErrExecReqTooMany           = errors.New("execution_requests: too many requests of a single type")
)

// ExecutionRequest represents a single execution layer request per EIP-7685.
// The first byte is the request type, followed by type-specific data.
type ExecutionRequest struct {
	// Type is the request type byte (0x00=deposit, 0x01=withdrawal, 0x02=consolidation).
	Type byte

	// Data is the type-specific request payload (excludes the type byte).
	Data []byte
}

// Encode serializes the execution request as type || data.
func (r *ExecutionRequest) Encode() []byte {
	out := make([]byte, 1+len(r.Data))
	out[0] = r.Type
	copy(out[1:], r.Data)
	return out
}

// ParseExecutionRequests decodes a list of raw execution requests from a block.
// Each raw request is formatted as type_byte || request_data. Returns the
// parsed ExecutionRequest structs.
func ParseExecutionRequests(raw [][]byte) ([]*ExecutionRequest, error) {
	if raw == nil {
		return nil, ErrExecReqNil
	}

	requests := make([]*ExecutionRequest, 0, len(raw))
	for i, r := range raw {
		if len(r) == 0 {
			return nil, fmt.Errorf("%w at index %d", ErrExecReqEmpty, i)
		}
		if len(r) < 2 {
			return nil, fmt.Errorf("%w at index %d: length %d", ErrExecReqTooShort, i, len(r))
		}

		reqType := r[0]
		reqData := make([]byte, len(r)-1)
		copy(reqData, r[1:])

		requests = append(requests, &ExecutionRequest{
			Type: reqType,
			Data: reqData,
		})
	}
	return requests, nil
}

// ValidateExecutionRequestList performs structural validation of execution
// requests per EIP-7685. It checks:
//   - Request types are in ascending order (no duplicates).
//   - Each request has valid minimum length.
//   - Request types are known (0x00, 0x01, 0x02).
//   - Data length is a valid multiple for the given type.
func ValidateExecutionRequestList(requests []*ExecutionRequest) error {
	if requests == nil {
		return ErrExecReqNil
	}

	var lastType byte

	for i, req := range requests {
		if req == nil {
			return fmt.Errorf("%w at index %d", ErrExecReqEmpty, i)
		}

		// Check ascending type order.
		if i > 0 {
			if req.Type < lastType {
				return fmt.Errorf("%w at index %d: type 0x%02x after 0x%02x",
					ErrExecReqTypesNotAscending, i, req.Type, lastType)
			}
			if req.Type == lastType {
				return fmt.Errorf("%w at index %d: type 0x%02x",
					ErrExecReqDuplicateType, i, req.Type)
			}
		}
		lastType = req.Type

		// Check known type.
		if req.Type > ExecReqMaxType {
			return fmt.Errorf("%w at index %d: type 0x%02x",
				ErrExecReqUnknownType, i, req.Type)
		}

		// Validate data length per type.
		if err := validateRequestDataLength(req.Type, req.Data, i); err != nil {
			return err
		}

	}

	return nil
}

// validateRequestDataLength checks that the data length is valid for the type.
// Deposit, withdrawal, and consolidation requests have fixed per-item sizes;
// the total data must be a multiple of that size.
func validateRequestDataLength(reqType byte, data []byte, index int) error {
	var expectedItemLen, limit int
	switch reqType {
	case ExecReqDepositType:
		expectedItemLen, limit = DepositRequestDataLen, types.MaxDepositsPerPayload
	case ExecReqWithdrawalType:
		expectedItemLen, limit = WithdrawalRequestDataLen, types.MaxWithdrawalRequestsPerPayload
	case ExecReqConsolidationType:
		expectedItemLen, limit = ConsolidationRequestDataLen, types.MaxConsolidationRequestsPerPayload
	default:
		return nil // unknown types pass length check
	}

	if len(data) == 0 {
		return fmt.Errorf("%w at index %d: empty data for type 0x%02x",
			ErrExecReqInvalidDataLen, index, reqType)
	}
	if len(data)%expectedItemLen != 0 {
		return fmt.Errorf("%w at index %d: length %d not a multiple of %d for type 0x%02x",
			ErrExecReqInvalidDataLen, index, len(data), expectedItemLen, reqType)
	}

	itemCount := len(data) / expectedItemLen
	if itemCount > limit {
		return fmt.Errorf("%w at index %d: %d items exceeds max %d for type 0x%02x",
			ErrExecReqTooMany, index, itemCount, limit, reqType)
	}

	return nil
}

// ComputeExecutionRequestsHash computes the EIP-7685 wire commitment.
func ComputeExecutionRequestsHash(requests [][]byte) types.Hash {
	return types.ComputeWireRequestsHash(requests)
}

// SplitRequestsByType groups parsed execution requests by their type byte.
// Returns a map from type byte to the list of requests of that type.
func SplitRequestsByType(requests []*ExecutionRequest) map[byte][]*ExecutionRequest {
	result := make(map[byte][]*ExecutionRequest)
	for _, req := range requests {
		if req == nil {
			continue
		}
		result[req.Type] = append(result[req.Type], req)
	}
	return result
}

// CountDepositRequests returns the number of deposit request items contained
// in the given raw request data for type 0x00.
func CountDepositRequests(data []byte) int {
	if DepositRequestDataLen == 0 || len(data) == 0 {
		return 0
	}
	return len(data) / DepositRequestDataLen
}

// CountWithdrawalRequests returns the number of withdrawal request items
// contained in the given raw request data for type 0x01.
func CountWithdrawalRequests(data []byte) int {
	if WithdrawalRequestDataLen == 0 || len(data) == 0 {
		return 0
	}
	return len(data) / WithdrawalRequestDataLen
}

// CountConsolidationRequests returns the number of consolidation request items
// contained in the given raw request data for type 0x02.
func CountConsolidationRequests(data []byte) int {
	if ConsolidationRequestDataLen == 0 || len(data) == 0 {
		return 0
	}
	return len(data) / ConsolidationRequestDataLen
}
