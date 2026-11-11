// deposit.go implements EIP-6110 deposit types and deposit root calculation.
// The DepositRequest struct is defined in request.go; this file provides
// deposit contract log parsing, deposit tree Merkleization, and validation.
//
// EIP-6110 moves validator deposit processing in-protocol by reading
// deposit events from the deposit contract's transaction logs instead of
// relying on the consensus layer's deposit vote mechanism.
package types

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/eth2030/eth2030/ssz"
)

// Deposit contract event constants.
// The deposit contract emits a DepositEvent with the following signature:
//
//	event DepositEvent(bytes pubkey, bytes withdrawal_credentials, bytes amount,
//	                   bytes signature, bytes index)
//
// The event topic0 is keccak256("DepositEvent(bytes,bytes,bytes,bytes,bytes)").
var (
	// DepositEventTopic is the topic0 of the DepositEvent log emitted by the
	// beacon chain deposit contract.
	DepositEventTopic = keccak256Hash([]byte("DepositEvent(bytes,bytes,bytes,bytes,bytes)"))
)

// Deposit tree constants per consensus specs.
const (
	// DepositContractTreeDepth is the depth of the deposit Merkle tree.
	DepositContractTreeDepth = 32

	// MaxDepositsPerPayload is the maximum number of deposit requests
	// per execution payload (EIP-6110).
	MaxDepositsPerPayload = 8192

	// DepositRequestSSZSize is the fixed SSZ size of a DepositRequest.
	// pubkey(48) + withdrawal_credentials(32) + amount(8) + signature(96) + index(8) = 192
	DepositRequestSSZSize = 192
)

// Deposit-specific errors.
var (
	ErrDepositLogInvalid   = errors.New("deposit: invalid log format")
	ErrDepositLogTopic     = errors.New("deposit: wrong event topic")
	ErrDepositDataTooShort = errors.New("deposit: log data too short")
	ErrDepositSSZSize      = errors.New("deposit: invalid SSZ data size")
	ErrTooManyDeposits     = errors.New("deposit: too many deposits in payload")
)

// ParseDepositLog extracts a deposit emitted by the mainnet deposit contract.
func ParseDepositLog(log *Log) (*DepositRequest, error) {
	return parseDepositLog(log, DepositContractAddress)
}

func parseDepositLog(log *Log, address Address) (*DepositRequest, error) {
	if log == nil || log.Address != address {
		return nil, ErrDepositLogInvalid
	}
	if len(log.Topics) == 0 || log.Topics[0] != DepositEventTopic {
		return nil, ErrDepositLogTopic
	}
	if len(log.Data) < 576 {
		return nil, fmt.Errorf("%w: got %d bytes, want 576", ErrDepositDataTooShort, len(log.Data))
	}
	if len(log.Data) != 576 {
		return nil, fmt.Errorf("%w: got %d bytes, want 576", ErrDepositLogInvalid, len(log.Data))
	}

	// EIP-6110 requires the exact DepositEvent ABI layout, including the
	// complete 256-bit offset and length words for each dynamic field.
	offsets := [...]int{160, 256, 320, 384, 512}
	sizes := [...]int{48, 32, 8, 96, 8}
	for i, offset := range offsets {
		var word [32]byte
		binary.BigEndian.PutUint64(word[24:], uint64(offset))
		if !bytes.Equal(log.Data[i*32:(i+1)*32], word[:]) {
			return nil, fmt.Errorf("%w: field %d offset", ErrDepositLogInvalid, i)
		}
		binary.BigEndian.PutUint64(word[24:], uint64(sizes[i]))
		if !bytes.Equal(log.Data[offset:offset+32], word[:]) {
			return nil, fmt.Errorf("%w: field %d length", ErrDepositLogInvalid, i)
		}
	}
	d := &DepositRequest{
		Amount: binary.LittleEndian.Uint64(log.Data[352:360]),
		Index:  binary.LittleEndian.Uint64(log.Data[544:552]),
	}
	copy(d.Pubkey[:], log.Data[192:240])
	copy(d.WithdrawalCredentials[:], log.Data[288:320])
	copy(d.Signature[:], log.Data[416:512])
	return d, nil
}

// DepositRequestData returns concatenated EIP-6110 records in receipt and log order.
func DepositRequestData(receipts []*Receipt, address Address) ([]byte, error) {
	var data []byte
	for _, receipt := range receipts {
		if receipt == nil {
			continue
		}
		for _, log := range receipt.Logs {
			if log == nil || log.Address != address || len(log.Topics) == 0 || log.Topics[0] != DepositEventTopic {
				continue
			}
			deposit, err := parseDepositLog(log, address)
			if err != nil {
				return nil, err
			}
			data = append(data, deposit.Encode()...)
		}
	}
	return data, nil
}

// DepositRequestHashTreeRoot computes the SSZ hash tree root of a single
// DepositRequest, treating it as a container with 5 fields:
//
//	pubkey: Vector[byte, 48]
//	withdrawal_credentials: Bytes32
//	amount: uint64
//	signature: Vector[byte, 96]
//	index: uint64
func DepositRequestHashTreeRoot(d *DepositRequest) [32]byte {
	fieldRoots := [5][32]byte{
		ssz.HashTreeRootBytes48(d.Pubkey),
		ssz.HashTreeRootBytes32(d.WithdrawalCredentials),
		ssz.HashTreeRootUint64(d.Amount),
		ssz.HashTreeRootBytes96(d.Signature),
		ssz.HashTreeRootUint64(d.Index),
	}
	return ssz.HashTreeRootContainer(fieldRoots[:])
}

// DepositRequestsHashTreeRoot computes the SSZ hash tree root for a list
// of deposit requests, treated as List[DepositRequest, MAX_DEPOSITS_PER_PAYLOAD].
func DepositRequestsHashTreeRoot(deposits []*DepositRequest) [32]byte {
	roots := make([][32]byte, len(deposits))
	for i, d := range deposits {
		roots[i] = DepositRequestHashTreeRoot(d)
	}
	return ssz.HashTreeRootList(roots, MaxDepositsPerPayload)
}

// MarshalDepositRequestSSZ serializes a DepositRequest to its SSZ encoding.
// Layout (fixed-size container, 192 bytes):
//
//	pubkey(48) || withdrawal_credentials(32) || amount(8) || signature(96) || index(8)
func MarshalDepositRequestSSZ(d *DepositRequest) []byte {
	return d.Encode()
}

// UnmarshalDepositRequestSSZ deserializes a DepositRequest from SSZ bytes.
func UnmarshalDepositRequestSSZ(data []byte) (*DepositRequest, error) {
	return DecodeDepositRequest(data)
}

// ValidateDepositRequest validates a deposit request's fields.
func ValidateDepositRequest(d *DepositRequest) error {
	if d == nil {
		return errors.New("deposit: nil request")
	}
	// Pubkey must not be all zeros.
	var zeroPubkey [48]byte
	if d.Pubkey == zeroPubkey {
		return errors.New("deposit: zero pubkey")
	}
	// Amount must be at least 1 Gwei (spec minimum is 1 ETH = 1e9 Gwei,
	// but we only check for zero here; higher-level validation enforces min).
	if d.Amount == 0 {
		return errors.New("deposit: zero amount")
	}
	return nil
}

// FilterDepositLogs scans a list of logs and extracts all valid deposit
// events from the deposit contract. Logs from other contracts or with
// incorrect topics are silently skipped.
func FilterDepositLogs(logs []*Log) []*DepositRequest {
	var deposits []*DepositRequest
	for _, log := range logs {
		if log == nil || log.Address != DepositContractAddress {
			continue
		}
		if len(log.Topics) < 1 || log.Topics[0] != DepositEventTopic {
			continue
		}
		d, err := ParseDepositLog(log)
		if err != nil {
			continue
		}
		deposits = append(deposits, d)
	}
	return deposits
}

// ValidateDepositRequests validates a list of deposit requests for inclusion
// in an execution payload.
func ValidateDepositRequests(deposits []*DepositRequest) error {
	if len(deposits) > MaxDepositsPerPayload {
		return fmt.Errorf("%w: got %d, max %d",
			ErrTooManyDeposits, len(deposits), MaxDepositsPerPayload)
	}
	for i, d := range deposits {
		if err := ValidateDepositRequest(d); err != nil {
			return fmt.Errorf("deposit %d: %w", i, err)
		}
	}
	return nil
}
