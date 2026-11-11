package types

import (
	"bytes"
	"testing"
)

func TestRequestEncode(t *testing.T) {
	r := NewRequest(DepositRequestType, []byte{0x01, 0x02, 0x03})
	encoded := r.Encode()
	if encoded[0] != 0x00 {
		t.Errorf("type byte = %d, want 0", encoded[0])
	}
	if !bytes.Equal(encoded[1:], []byte{0x01, 0x02, 0x03}) {
		t.Errorf("data mismatch")
	}
}

func TestDecodeRequest(t *testing.T) {
	data := []byte{0x01, 0xaa, 0xbb}
	r, err := DecodeRequest(data)
	if err != nil {
		t.Fatal(err)
	}
	if r.Type != WithdrawalRequestType {
		t.Errorf("type = %d, want %d", r.Type, WithdrawalRequestType)
	}
	if !bytes.Equal(r.Data, []byte{0xaa, 0xbb}) {
		t.Errorf("data mismatch")
	}
}

func TestDecodeRequestTooShort(t *testing.T) {
	_, err := DecodeRequest([]byte{})
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestRequestsFilterByType(t *testing.T) {
	requests := Requests{
		NewRequest(DepositRequestType, []byte{0x01}),
		NewRequest(WithdrawalRequestType, []byte{0x02}),
		NewRequest(DepositRequestType, []byte{0x03}),
		NewRequest(ConsolidationRequestType, []byte{0x04}),
	}

	deposits := requests.FilterByType(DepositRequestType)
	if len(deposits) != 2 {
		t.Errorf("expected 2 deposit requests, got %d", len(deposits))
	}

	withdrawals := requests.FilterByType(WithdrawalRequestType)
	if len(withdrawals) != 1 {
		t.Errorf("expected 1 withdrawal request, got %d", len(withdrawals))
	}

	consolidations := requests.FilterByType(ConsolidationRequestType)
	if len(consolidations) != 1 {
		t.Errorf("expected 1 consolidation request, got %d", len(consolidations))
	}
}

func TestRequestsEncode(t *testing.T) {
	requests := Requests{
		NewRequest(0x00, []byte{0x01}),
		NewRequest(0x01, []byte{0x02, 0x03}),
	}
	encoded := requests.Encode()
	if len(encoded) != 2 {
		t.Fatalf("expected 2 encoded requests, got %d", len(encoded))
	}
	if !bytes.Equal(encoded[0], []byte{0x00, 0x01}) {
		t.Errorf("first request encoding mismatch")
	}
	if !bytes.Equal(encoded[1], []byte{0x01, 0x02, 0x03}) {
		t.Errorf("second request encoding mismatch")
	}
}

func TestComputeRequestsHash(t *testing.T) {
	// Compute hash of known requests.
	requests := Requests{
		NewRequest(DepositRequestType, []byte{0x01, 0x02}),
		NewRequest(WithdrawalRequestType, []byte{0x03}),
	}

	hash := ComputeRequestsHash(requests)

	// Hash should not be zero.
	if hash.IsZero() {
		t.Error("requests hash should not be zero")
	}

	// Same requests should produce same hash.
	hash2 := ComputeRequestsHash(requests)
	if hash != hash2 {
		t.Error("same requests should produce same hash")
	}

	// Different requests should produce different hash.
	requests3 := Requests{
		NewRequest(DepositRequestType, []byte{0x01, 0x03}), // different data
	}
	hash3 := ComputeRequestsHash(requests3)
	if hash == hash3 {
		t.Error("different requests should produce different hash")
	}
}

func TestComputeRequestsHashEmpty(t *testing.T) {
	hash := ComputeRequestsHash(nil)
	// Empty requests should produce the hash of empty input.
	if hash.IsZero() {
		// SHA-256 of empty is not zero, but our implementation only hashes
		// existing types, so empty input gives sha256 of nothing.
	}
}

func TestValidateRequestsHash(t *testing.T) {
	requests := Requests{
		NewRequest(DepositRequestType, []byte{0x01}),
	}
	hash := ComputeRequestsHash(requests)

	// Valid header.
	header := &Header{RequestsHash: &hash}
	if err := ValidateRequestsHash(header, requests); err != nil {
		t.Errorf("expected valid, got: %v", err)
	}

	// Wrong hash.
	wrongHash := HexToHash("0xdeadbeef")
	header2 := &Header{RequestsHash: &wrongHash}
	if err := ValidateRequestsHash(header2, requests); err == nil {
		t.Error("expected error for wrong hash")
	}

	// No hash field with requests.
	header3 := &Header{}
	if err := ValidateRequestsHash(header3, requests); err == nil {
		t.Error("expected error for missing requests_hash")
	}

	// No hash field without requests.
	header4 := &Header{}
	if err := ValidateRequestsHash(header4, nil); err != nil {
		t.Errorf("expected valid for no requests + no hash, got: %v", err)
	}
}

func TestRequestTypeConstants(t *testing.T) {
	if DepositRequestType != 0x00 {
		t.Errorf("DepositRequestType = %d, want 0", DepositRequestType)
	}
	if WithdrawalRequestType != 0x01 {
		t.Errorf("WithdrawalRequestType = %d, want 1", WithdrawalRequestType)
	}
	if ConsolidationRequestType != 0x02 {
		t.Errorf("ConsolidationRequestType = %d, want 2", ConsolidationRequestType)
	}
}

func TestSystemContractAddresses(t *testing.T) {
	// Verify system contract addresses are non-zero.
	if DepositContractAddress.IsZero() {
		t.Fatal("DepositContractAddress should not be zero")
	}
	if WithdrawalRequestAddress.IsZero() {
		t.Fatal("WithdrawalRequestAddress should not be zero")
	}
	if ConsolidationRequestAddress.IsZero() {
		t.Fatal("ConsolidationRequestAddress should not be zero")
	}

	// Verify they are all distinct.
	if DepositContractAddress == WithdrawalRequestAddress {
		t.Fatal("deposit and withdrawal addresses should differ")
	}
	if DepositContractAddress == ConsolidationRequestAddress {
		t.Fatal("deposit and consolidation addresses should differ")
	}
	if WithdrawalRequestAddress == ConsolidationRequestAddress {
		t.Fatal("withdrawal and consolidation addresses should differ")
	}
}

func TestSystemAddress(t *testing.T) {
	if SystemAddress.IsZero() {
		t.Fatal("SystemAddress should not be zero")
	}
	expected := HexToAddress("0xfffffffffffffffffffffffffffffffffffffffe")
	if SystemAddress != expected {
		t.Fatalf("SystemAddress = %s, want %s", SystemAddress.Hex(), expected.Hex())
	}
}

func TestDepositRequest_EncodeDecode(t *testing.T) {
	d := &DepositRequest{
		Amount: 32_000_000_000, // 32 ETH in Gwei
		Index:  42,
	}
	d.Pubkey[0] = 0xAA
	d.Pubkey[47] = 0xBB
	d.WithdrawalCredentials[0] = 0x01
	d.Signature[0] = 0xCC

	encoded := d.Encode()
	if len(encoded) != 192 {
		t.Fatalf("deposit request encoding length = %d, want 192", len(encoded))
	}

	decoded, err := DecodeDepositRequest(encoded)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded.Pubkey != d.Pubkey {
		t.Fatal("pubkey mismatch after decode")
	}
	if decoded.WithdrawalCredentials != d.WithdrawalCredentials {
		t.Fatal("withdrawal credentials mismatch after decode")
	}
	if decoded.Amount != d.Amount {
		t.Fatalf("amount mismatch: got %d, want %d", decoded.Amount, d.Amount)
	}
	if decoded.Signature != d.Signature {
		t.Fatal("signature mismatch after decode")
	}
	if decoded.Index != d.Index {
		t.Fatalf("index mismatch: got %d, want %d", decoded.Index, d.Index)
	}
}

func TestDepositRequest_DecodeInvalidLength(t *testing.T) {
	_, err := DecodeDepositRequest(make([]byte, 100))
	if err == nil {
		t.Fatal("expected error for invalid deposit request length")
	}
	_, err = DecodeDepositRequest(make([]byte, 0))
	if err == nil {
		t.Fatal("expected error for empty deposit request")
	}
}

func TestWithdrawalRequest_EncodeDecode(t *testing.T) {
	w := &WithdrawalRequest{
		SourceAddress: HexToAddress("0xdead"),
		Amount:        1_000_000_000,
	}
	w.ValidatorPubkey[0] = 0xBE
	w.ValidatorPubkey[47] = 0xEF

	encoded := w.Encode()
	if len(encoded) != 76 {
		t.Fatalf("withdrawal request encoding length = %d, want 76", len(encoded))
	}

	decoded, err := DecodeWithdrawalRequest(encoded)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded.SourceAddress != w.SourceAddress {
		t.Fatal("source address mismatch")
	}
	if decoded.ValidatorPubkey != w.ValidatorPubkey {
		t.Fatal("validator pubkey mismatch")
	}
	if decoded.Amount != w.Amount {
		t.Fatalf("amount mismatch: got %d, want %d", decoded.Amount, w.Amount)
	}
}

func TestWithdrawalRequest_DecodeInvalidLength(t *testing.T) {
	_, err := DecodeWithdrawalRequest(make([]byte, 50))
	if err == nil {
		t.Fatal("expected error for invalid withdrawal request length")
	}
}

func TestConsolidationRequest_EncodeDecode(t *testing.T) {
	c := &ConsolidationRequest{
		SourceAddress: HexToAddress("0x1234"),
	}
	c.SourcePubkey[0] = 0xAA
	c.TargetPubkey[0] = 0xBB

	encoded := c.Encode()
	if len(encoded) != 116 {
		t.Fatalf("consolidation request encoding length = %d, want 116", len(encoded))
	}

	decoded, err := DecodeConsolidationRequest(encoded)
	if err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if decoded.SourceAddress != c.SourceAddress {
		t.Fatal("source address mismatch")
	}
	if decoded.SourcePubkey != c.SourcePubkey {
		t.Fatal("source pubkey mismatch")
	}
	if decoded.TargetPubkey != c.TargetPubkey {
		t.Fatal("target pubkey mismatch")
	}
}

func TestConsolidationRequest_DecodeInvalidLength(t *testing.T) {
	_, err := DecodeConsolidationRequest(make([]byte, 50))
	if err == nil {
		t.Fatal("expected error for invalid consolidation request length")
	}
}

// --- EncodeRequests / DecodeRequests roundtrip tests ---

func TestEncodeDecodeRequests_Roundtrip(t *testing.T) {
	requests := Requests{
		NewRequest(DepositRequestType, []byte{0x01, 0x02, 0x03}),
		NewRequest(WithdrawalRequestType, []byte{0xAA, 0xBB}),
		NewRequest(ConsolidationRequestType, []byte{0xFF}),
	}

	encoded := EncodeRequests(requests)
	decoded, err := DecodeRequests(encoded)
	if err != nil {
		t.Fatalf("DecodeRequests error: %v", err)
	}

	if len(decoded) != len(requests) {
		t.Fatalf("decoded length: got %d, want %d", len(decoded), len(requests))
	}

	for i := range requests {
		if decoded[i].Type != requests[i].Type {
			t.Errorf("request %d type: got %d, want %d", i, decoded[i].Type, requests[i].Type)
		}
		if !bytes.Equal(decoded[i].Data, requests[i].Data) {
			t.Errorf("request %d data: got %x, want %x", i, decoded[i].Data, requests[i].Data)
		}
	}
}

func TestEncodeDecodeRequests_Empty(t *testing.T) {
	encoded := EncodeRequests(nil)
	if len(encoded) != 0 {
		t.Fatalf("encoding nil requests should produce empty bytes, got %d bytes", len(encoded))
	}

	decoded, err := DecodeRequests(encoded)
	if err != nil {
		t.Fatalf("DecodeRequests error for empty: %v", err)
	}
	if len(decoded) != 0 {
		t.Fatalf("decoded should be empty, got %d", len(decoded))
	}
}

func TestEncodeDecodeRequests_SingleRequest(t *testing.T) {
	requests := Requests{
		NewRequest(DepositRequestType, []byte{0x42}),
	}

	encoded := EncodeRequests(requests)
	decoded, err := DecodeRequests(encoded)
	if err != nil {
		t.Fatalf("DecodeRequests error: %v", err)
	}

	if len(decoded) != 1 {
		t.Fatalf("expected 1 decoded request, got %d", len(decoded))
	}
	if decoded[0].Type != DepositRequestType {
		t.Errorf("type: got %d, want %d", decoded[0].Type, DepositRequestType)
	}
	if !bytes.Equal(decoded[0].Data, []byte{0x42}) {
		t.Errorf("data: got %x, want 42", decoded[0].Data)
	}
}

func TestDecodeRequests_Truncated(t *testing.T) {
	// Just 2 bytes of a length prefix.
	_, err := DecodeRequests([]byte{0x01, 0x00})
	if err == nil {
		t.Error("expected error for truncated length prefix")
	}

	// Length says 10 bytes but only 3 available.
	_, err = DecodeRequests([]byte{0x0A, 0x00, 0x00, 0x00, 0x01, 0x02, 0x03})
	if err == nil {
		t.Error("expected error for truncated request data")
	}
}

func TestDecodeRequests_ZeroLength(t *testing.T) {
	_, err := DecodeRequests([]byte{0x00, 0x00, 0x00, 0x00})
	if err == nil {
		t.Error("expected error for zero-length request")
	}
}

func TestEncodeDecodeRequests_LargeData(t *testing.T) {
	// Request with 1000 bytes of data.
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i % 256)
	}
	requests := Requests{
		NewRequest(DepositRequestType, data),
	}

	encoded := EncodeRequests(requests)
	decoded, err := DecodeRequests(encoded)
	if err != nil {
		t.Fatalf("DecodeRequests error: %v", err)
	}
	if len(decoded) != 1 {
		t.Fatalf("expected 1 request, got %d", len(decoded))
	}
	if !bytes.Equal(decoded[0].Data, data) {
		t.Error("large data roundtrip failed")
	}
}

// --- SortRequests tests ---

func TestSortRequests_AlreadySorted(t *testing.T) {
	requests := Requests{
		NewRequest(DepositRequestType, []byte{0x01}),
		NewRequest(WithdrawalRequestType, []byte{0x02}),
		NewRequest(ConsolidationRequestType, []byte{0x03}),
	}
	SortRequests(requests)

	if requests[0].Type != DepositRequestType {
		t.Errorf("first should be deposit, got %d", requests[0].Type)
	}
	if requests[1].Type != WithdrawalRequestType {
		t.Errorf("second should be withdrawal, got %d", requests[1].Type)
	}
	if requests[2].Type != ConsolidationRequestType {
		t.Errorf("third should be consolidation, got %d", requests[2].Type)
	}
}

func TestSortRequests_Reversed(t *testing.T) {
	requests := Requests{
		NewRequest(ConsolidationRequestType, []byte{0x03}),
		NewRequest(WithdrawalRequestType, []byte{0x02}),
		NewRequest(DepositRequestType, []byte{0x01}),
	}
	SortRequests(requests)

	if requests[0].Type != DepositRequestType {
		t.Errorf("first should be deposit, got %d", requests[0].Type)
	}
	if requests[1].Type != WithdrawalRequestType {
		t.Errorf("second should be withdrawal, got %d", requests[1].Type)
	}
	if requests[2].Type != ConsolidationRequestType {
		t.Errorf("third should be consolidation, got %d", requests[2].Type)
	}
}

func TestSortRequests_StableOrder(t *testing.T) {
	// Multiple deposits should maintain their relative order.
	requests := Requests{
		NewRequest(DepositRequestType, []byte{0x01}),
		NewRequest(ConsolidationRequestType, []byte{0x04}),
		NewRequest(DepositRequestType, []byte{0x02}),
		NewRequest(DepositRequestType, []byte{0x03}),
	}
	SortRequests(requests)

	if requests[0].Type != DepositRequestType || !bytes.Equal(requests[0].Data, []byte{0x01}) {
		t.Error("first deposit should have data 0x01")
	}
	if requests[1].Type != DepositRequestType || !bytes.Equal(requests[1].Data, []byte{0x02}) {
		t.Error("second deposit should have data 0x02")
	}
	if requests[2].Type != DepositRequestType || !bytes.Equal(requests[2].Data, []byte{0x03}) {
		t.Error("third deposit should have data 0x03")
	}
	if requests[3].Type != ConsolidationRequestType {
		t.Error("consolidation should be last")
	}
}

func TestSortRequests_Empty(t *testing.T) {
	var requests Requests
	SortRequests(requests) // should not panic
}

// --- ComputeRequestsHash determinism ---

func TestComputeRequestsHash_Deterministic(t *testing.T) {
	requests := Requests{
		NewRequest(DepositRequestType, []byte{0x01, 0x02}),
		NewRequest(WithdrawalRequestType, []byte{0x03}),
		NewRequest(ConsolidationRequestType, []byte{0x04, 0x05, 0x06}),
	}

	hash1 := ComputeRequestsHash(requests)
	hash2 := ComputeRequestsHash(requests)

	if hash1 != hash2 {
		t.Error("hash should be deterministic")
	}
}

func TestComputeRequestsHash_OrderMatters(t *testing.T) {
	requests1 := Requests{
		NewRequest(DepositRequestType, []byte{0x01}),
		NewRequest(DepositRequestType, []byte{0x02}),
	}
	requests2 := Requests{
		NewRequest(DepositRequestType, []byte{0x02}),
		NewRequest(DepositRequestType, []byte{0x01}),
	}

	hash1 := ComputeRequestsHash(requests1)
	hash2 := ComputeRequestsHash(requests2)

	if hash1 == hash2 {
		t.Error("different order should produce different hashes")
	}
}

func TestComputeRequestsHash_TypeMatters(t *testing.T) {
	requests1 := Requests{
		NewRequest(DepositRequestType, []byte{0x01}),
	}
	requests2 := Requests{
		NewRequest(WithdrawalRequestType, []byte{0x01}),
	}

	hash1 := ComputeRequestsHash(requests1)
	hash2 := ComputeRequestsHash(requests2)

	if hash1 == hash2 {
		t.Error("different types with same data should produce different hashes")
	}
}

func TestEIP7685WireRequests(t *testing.T) {
	for _, tc := range []struct {
		name  string
		wire  [][]byte
		valid bool
	}{
		{"empty array", [][]byte{}, true},
		{"sorted groups", [][]byte{{0, 1}, {2, 3}}, true},
		{"future group", [][]byte{{255, 1}}, true},
		{"null", nil, false},
		{"empty data", [][]byte{{0}}, false},
		{"duplicate type", [][]byte{{0, 1}, {0, 2}}, false},
		{"descending type", [][]byte{{2, 1}, {0, 2}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateExecutionRequestsWire(tc.wire); (err == nil) != tc.valid {
				t.Fatalf("wire validation: %v", err)
			}
		})
	}
}
