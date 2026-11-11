package types

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
)

// Request types defined by EIP-7685.
// Each request type is identified by a single byte prefix.
const (
	DepositRequestType       byte = 0x00 // EIP-6110: supply validator deposits
	WithdrawalRequestType    byte = 0x01 // EIP-7002: trigger validator withdrawals
	ConsolidationRequestType byte = 0x02 // EIP-7251: validator consolidations

	WithdrawalRequestSSZSize           = 76
	ConsolidationRequestSSZSize        = 116
	MaxWithdrawalRequestsPerPayload    = 16
	MaxConsolidationRequestsPerPayload = 2
)

// System contract addresses for EIP-7685 request processing.
// These contracts are called after all user transactions to collect
// execution layer requests.
var (
	// DepositContractAddress is the beacon chain deposit contract (EIP-6110).
	DepositContractAddress = HexToAddress("0x00000000219ab540356cBB839Cbe05303d7705Fa")

	// WithdrawalRequestAddress is the system contract for triggering
	// validator withdrawals (EIP-7002).
	WithdrawalRequestAddress = HexToAddress("0x00000961Ef480Eb55e80D19ad83579A64c007002")

	// ConsolidationRequestAddress is the system contract for validator
	// consolidations (EIP-7251).
	ConsolidationRequestAddress = HexToAddress("0x0000BBdDc7CE488642fb579F8B00f3a590007251")
)

// SystemAddress is the caller address used for system contract calls.
// Per EIP-7685, system calls use a special "system" address as the caller.
var SystemAddress = HexToAddress("0xfffffffffffffffffffffffffffffffffffffffe")

// DepositRequest represents a validator deposit parsed from the deposit contract.
// Per EIP-6110, deposits are read from the deposit contract's storage/logs.
type DepositRequest struct {
	Pubkey                [48]byte
	WithdrawalCredentials [32]byte
	Amount                uint64
	Signature             [96]byte
	Index                 uint64
}

// Encode serializes a deposit request to bytes.
// Layout: pubkey(48) || withdrawal_credentials(32) || amount(8) || signature(96) || index(8)
func (d *DepositRequest) Encode() []byte {
	buf := make([]byte, 192)
	copy(buf[0:48], d.Pubkey[:])
	copy(buf[48:80], d.WithdrawalCredentials[:])
	binary.LittleEndian.PutUint64(buf[80:88], d.Amount)
	copy(buf[88:184], d.Signature[:])
	binary.LittleEndian.PutUint64(buf[184:192], d.Index)
	return buf
}

// DecodeDepositRequest deserializes a deposit request from bytes.
func DecodeDepositRequest(data []byte) (*DepositRequest, error) {
	if len(data) != 192 {
		return nil, fmt.Errorf("invalid deposit request length: %d, want 192", len(data))
	}
	d := &DepositRequest{}
	copy(d.Pubkey[:], data[0:48])
	copy(d.WithdrawalCredentials[:], data[48:80])
	d.Amount = binary.LittleEndian.Uint64(data[80:88])
	copy(d.Signature[:], data[88:184])
	d.Index = binary.LittleEndian.Uint64(data[184:192])
	return d, nil
}

// WithdrawalRequest represents a validator withdrawal request from the
// system contract (EIP-7002).
type WithdrawalRequest struct {
	SourceAddress   Address
	ValidatorPubkey [48]byte
	Amount          uint64
}

// Encode serializes a withdrawal request to bytes.
// Layout: source_address(20) || validator_pubkey(48) || amount(8)
func (w *WithdrawalRequest) Encode() []byte {
	buf := make([]byte, 76)
	copy(buf[0:20], w.SourceAddress[:])
	copy(buf[20:68], w.ValidatorPubkey[:])
	binary.LittleEndian.PutUint64(buf[68:76], w.Amount)
	return buf
}

// DecodeWithdrawalRequest deserializes a withdrawal request from bytes.
func DecodeWithdrawalRequest(data []byte) (*WithdrawalRequest, error) {
	if len(data) != 76 {
		return nil, fmt.Errorf("invalid withdrawal request length: %d, want 76", len(data))
	}
	w := &WithdrawalRequest{}
	copy(w.SourceAddress[:], data[0:20])
	copy(w.ValidatorPubkey[:], data[20:68])
	w.Amount = binary.LittleEndian.Uint64(data[68:76])
	return w, nil
}

// ConsolidationRequest represents a validator consolidation request from
// the system contract (EIP-7251).
type ConsolidationRequest struct {
	SourceAddress Address
	SourcePubkey  [48]byte
	TargetPubkey  [48]byte
}

// Encode serializes a consolidation request to bytes.
// Layout: source_address(20) || source_pubkey(48) || target_pubkey(48)
func (c *ConsolidationRequest) Encode() []byte {
	buf := make([]byte, 116)
	copy(buf[0:20], c.SourceAddress[:])
	copy(buf[20:68], c.SourcePubkey[:])
	copy(buf[68:116], c.TargetPubkey[:])
	return buf
}

// DecodeConsolidationRequest deserializes a consolidation request from bytes.
func DecodeConsolidationRequest(data []byte) (*ConsolidationRequest, error) {
	if len(data) != 116 {
		return nil, fmt.Errorf("invalid consolidation request length: %d, want 116", len(data))
	}
	c := &ConsolidationRequest{}
	copy(c.SourceAddress[:], data[0:20])
	copy(c.SourcePubkey[:], data[20:68])
	copy(c.TargetPubkey[:], data[68:116])
	return c, nil
}

// Request represents a typed execution layer request per EIP-7685.
// Format: request_type || request_data
type Request struct {
	Type byte
	Data []byte
}

// NewRequest creates a new request with the given type and data.
func NewRequest(reqType byte, data []byte) *Request {
	return &Request{Type: reqType, Data: data}
}

// Encode serializes the request to its wire format: type || data.
func (r *Request) Encode() []byte {
	out := make([]byte, 1+len(r.Data))
	out[0] = r.Type
	copy(out[1:], r.Data)
	return out
}

// DecodeRequest deserializes a request from its wire format.
func DecodeRequest(data []byte) (*Request, error) {
	if len(data) < 1 {
		return nil, errors.New("request too short")
	}
	return &Request{
		Type: data[0],
		Data: data[1:],
	}, nil
}

// Requests is a list of EL requests.
type Requests []*Request

// FilterByType returns all requests of the given type.
func (rs Requests) FilterByType(reqType byte) Requests {
	var result Requests
	for _, r := range rs {
		if r.Type == reqType {
			result = append(result, r)
		}
	}
	return result
}

// Encode serializes all requests to their wire formats.
func (rs Requests) Encode() [][]byte {
	result := make([][]byte, len(rs))
	for i, r := range rs {
		result[i] = r.Encode()
	}
	return result
}

// SortRequests sorts requests by type in ascending order, maintaining the
// relative order of requests of the same type (stable sort).
func SortRequests(requests Requests) {
	sort.SliceStable(requests, func(i, j int) bool {
		return requests[i].Type < requests[j].Type
	})
}

// EncodeRequests serializes a list of requests to a flat byte slice.
// Format: for each request, 4 bytes little-endian length prefix + type + data.
func EncodeRequests(requests Requests) []byte {
	var buf []byte
	for _, r := range requests {
		encoded := r.Encode() // type || data
		length := uint32(len(encoded))
		lenBuf := make([]byte, 4)
		binary.LittleEndian.PutUint32(lenBuf, length)
		buf = append(buf, lenBuf...)
		buf = append(buf, encoded...)
	}
	return buf
}

// DecodeRequests deserializes a flat byte slice into a list of requests.
// The format matches EncodeRequests: 4 bytes LE length + type + data per request.
func DecodeRequests(data []byte) (Requests, error) {
	var requests Requests
	offset := 0
	for offset < len(data) {
		if offset+4 > len(data) {
			return nil, errors.New("truncated request length prefix")
		}
		length := binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4
		if length == 0 {
			return nil, errors.New("zero-length request")
		}
		end := offset + int(length)
		if end > len(data) {
			return nil, fmt.Errorf("request data truncated: need %d bytes at offset %d, have %d", length, offset, len(data)-offset)
		}
		r, err := DecodeRequest(data[offset:end])
		if err != nil {
			return nil, err
		}
		requests = append(requests, r)
		offset = end
	}
	return requests, nil
}

// ComputeRequestsHash computes the SHA-256 commitment over all requests.
// Per EIP-7685: sha256(sha256(requests_0) ++ sha256(requests_1) ++ ...)
// where each requests_i is the concatenation of all request data of type i.
func ComputeRequestsHash(requests Requests) Hash {
	// Combine nonempty data for each type in production order.
	var groups [256][]byte
	for _, r := range requests {
		if r == nil || len(r.Data) == 0 {
			continue
		}
		i := r.Type
		if len(groups[i]) == 0 {
			groups[i] = append(groups[i], i)
		}
		groups[i] = append(groups[i], r.Data...)
	}
	return ComputeWireRequestsHash(groups[:])
}

// ComputeWireRequestsHash commits to the EIP-7685 groups in wire order.
func ComputeWireRequestsHash(requests [][]byte) Hash {
	h := sha256.New()
	for _, group := range requests {
		if len(group) <= 1 {
			continue
		}
		digest := sha256.Sum256(group)
		h.Write(digest[:])
	}
	var result Hash
	copy(result[:], h.Sum(nil))
	return result
}

// ValidateExecutionRequestsWire checks the Prague Engine API request array.
func ValidateExecutionRequestsWire(requests [][]byte) error {
	if requests == nil {
		return errors.New("execution requests must be an array")
	}
	for i, request := range requests {
		if len(request) < 2 {
			return fmt.Errorf("execution request %d has empty data", i)
		}
		if i > 0 && request[0] <= requests[i-1][0] {
			return fmt.Errorf("execution request %d type must be strictly ascending", i)
		}
	}
	return nil
}

// ValidateRequestsHash verifies the requests_hash field in a block header.
func ValidateRequestsHash(header *Header, requests Requests) error {
	if header.RequestsHash == nil {
		if len(requests) == 0 {
			return nil
		}
		return fmt.Errorf("header has no requests_hash but block has %d requests", len(requests))
	}
	computed := ComputeRequestsHash(requests)
	if *header.RequestsHash != computed {
		return fmt.Errorf("requests hash mismatch: header=%s computed=%s", header.RequestsHash.Hex(), computed.Hex())
	}
	return nil
}
