package engine

import (
	"encoding/json"
	"testing"
)

// Sample mirrors lighthouse JsonPayloadAttributesV5 (snake_case, hex ints).
const lighthouseAttrsJSON = `{
	"timestamp": "0x67fbf0a0",
	"prev_randao": "0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
	"suggested_fee_recipient": "0xaa00000000000000000000000000000000000001",
	"withdrawals": [
		{"index": "0x1", "validator_index": "0x2a", "address": "0xbb00000000000000000000000000000000000002", "amount": "0x5f5e100"}
	],
	"parent_beacon_block_root": "0x0303030303030303030303030303030303030303030303030303030303030303",
	"slot_number": "0x20",
	"target_gas_limit": "0x1c9c380",
	"inclusion_list_transactions": "0x00"
}`

func TestParseLighthousePayloadAttrs(t *testing.T) {
	attrs, err := parsePayloadAttributes(json.RawMessage(lighthouseAttrsJSON))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if attrs.Timestamp != 0x67fbf0a0 {
		t.Errorf("timestamp = %d, want %d", attrs.Timestamp, 0x67fbf0a0)
	}
	if len(attrs.Withdrawals) != 1 {
		t.Fatalf("withdrawals len = %d, want 1", len(attrs.Withdrawals))
	}
	w := attrs.Withdrawals[0]
	if w.Index != 1 || w.ValidatorIndex != 0x2a || w.Amount != 0x5f5e100 {
		t.Errorf("withdrawal = %+v, want index 1, validator 42, amount 100000000", w)
	}
	if attrs.SlotNumber != 0x20 {
		t.Errorf("slot = %d, want 32", attrs.SlotNumber)
	}
	if attrs.ParentBeaconBlockRoot == (attrs.PrevRandao) {
		t.Errorf("parent beacon root and prev randao should differ")
	}
	if attrs.InclusionListTransactions != nil {
		t.Errorf("IL txs should be nil until progressive-list decode lands")
	}
}

func TestParsePayloadAttributesForkShape(t *testing.T) {
	// Fork-era CL shape: camelCase, decimal integers, IL txs inline.
	raw := json.RawMessage(`{
		"timestamp": 1000,
		"prevRandao": "0x0102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f20",
		"suggestedFeeRecipient": "0xaa00000000000000000000000000000000000001",
		"withdrawals": [],
		"parentBeaconBlockRoot": "0x0303030303030303030303030303030303030303030303030303030303030303",
		"slotNumber": 32
	}`)
	attrs, err := parsePayloadAttributes(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if attrs.Timestamp != 1000 || attrs.SlotNumber != 32 {
		t.Errorf("attrs = %+v, want timestamp 1000 slot 32", attrs)
	}
}

func TestParsePayloadAttributesNull(t *testing.T) {
	attrs, err := parsePayloadAttributes(json.RawMessage("null"))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if attrs != nil {
		t.Errorf("expected nil attrs for null, got %+v", attrs)
	}
}

func TestValidateCustodyColumns(t *testing.T) {
	cases := []struct {
		raw   string
		fails bool
	}{
		{"null", false},
		{`"0x01"`, false},
		{`"0x00ff00"`, false},
		{`"nothex"`, true},
		{`42`, true},
	}
	for _, c := range cases {
		err := validateCustodyColumns(json.RawMessage(c.raw))
		if (err != nil) != c.fails {
			t.Errorf("validateCustodyColumns(%s): err=%v, wantFail=%v", c.raw, err, c.fails)
		}
	}
}
