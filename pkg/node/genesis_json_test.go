package node

import (
	"encoding/json"
	"testing"

	coreconfig "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/types"
)

// --- bigIntJSON ---

func TestBigIntJSON_HexString(t *testing.T) {
	var b bigIntJSON
	if err := json.Unmarshal([]byte(`"0xFF"`), &b); err != nil {
		t.Fatalf("unmarshal hex: %v", err)
	}
	if b.Int64() != 255 {
		t.Errorf("got %d, want 255", b.Int64())
	}
}

func TestBigIntJSON_DecimalString(t *testing.T) {
	var b bigIntJSON
	if err := json.Unmarshal([]byte(`"12345"`), &b); err != nil {
		t.Fatalf("unmarshal decimal string: %v", err)
	}
	if b.Int64() != 12345 {
		t.Errorf("got %d, want 12345", b.Int64())
	}
}

func TestBigIntJSON_PlainNumber(t *testing.T) {
	var b bigIntJSON
	if err := json.Unmarshal([]byte(`42`), &b); err != nil {
		t.Fatalf("unmarshal plain number: %v", err)
	}
	if b.Int64() != 42 {
		t.Errorf("got %d, want 42", b.Int64())
	}
}

func TestBigIntJSON_InvalidHex(t *testing.T) {
	var b bigIntJSON
	if err := json.Unmarshal([]byte(`"0xGGGG"`), &b); err == nil {
		t.Error("expected error for invalid hex")
	}
}

func TestBigIntJSON_InvalidDecimal(t *testing.T) {
	var b bigIntJSON
	if err := json.Unmarshal([]byte(`"not-a-number"`), &b); err == nil {
		t.Error("expected error for invalid decimal")
	}
}

func TestBigIntJSON_LargeNumber(t *testing.T) {
	var b bigIntJSON
	if err := json.Unmarshal([]byte(`"1000000000000000000000"`), &b); err != nil {
		t.Fatalf("unmarshal large decimal: %v", err)
	}
	if b.String() != "1000000000000000000000" {
		t.Errorf("got %s, want 1000000000000000000000", b.String())
	}
}

// --- uint64JSON ---

func TestUint64JSON_HexString(t *testing.T) {
	var u uint64JSON
	if err := json.Unmarshal([]byte(`"0x10"`), &u); err != nil {
		t.Fatalf("unmarshal hex: %v", err)
	}
	if uint64(u) != 16 {
		t.Errorf("got %d, want 16", u)
	}
}

func TestUint64JSON_DecimalString(t *testing.T) {
	var u uint64JSON
	if err := json.Unmarshal([]byte(`"100"`), &u); err != nil {
		t.Fatalf("unmarshal decimal string: %v", err)
	}
	if uint64(u) != 100 {
		t.Errorf("got %d, want 100", u)
	}
}

func TestUint64JSON_PlainNumber(t *testing.T) {
	var u uint64JSON
	if err := json.Unmarshal([]byte(`999`), &u); err != nil {
		t.Fatalf("unmarshal plain number: %v", err)
	}
	if uint64(u) != 999 {
		t.Errorf("got %d, want 999", u)
	}
}

func TestUint64JSON_Zero(t *testing.T) {
	var u uint64JSON
	if err := json.Unmarshal([]byte(`"0x0"`), &u); err != nil {
		t.Fatalf("unmarshal zero: %v", err)
	}
	if uint64(u) != 0 {
		t.Errorf("got %d, want 0", u)
	}
}

func TestUint64JSON_BadType(t *testing.T) {
	var u uint64JSON
	// Non-string, non-number type (array).
	if err := json.Unmarshal([]byte(`[1,2]`), &u); err == nil {
		t.Error("expected error for array")
	}
}

// --- addressJSON ---

func TestAddressJSON_WithPrefix(t *testing.T) {
	var a addressJSON
	if err := json.Unmarshal([]byte(`"0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"`), &a); err != nil {
		t.Fatalf("unmarshal address: %v", err)
	}
	if a.Address[0] == 0 && a.Address[19] == 0 {
		t.Error("address should not be all zeros")
	}
}

func TestAddressJSON_WithoutPrefix(t *testing.T) {
	var a addressJSON
	// Some tooling emits addresses without 0x prefix.
	if err := json.Unmarshal([]byte(`"f39Fd6e51aad88F6F4ce6aB8827279cffFb92266"`), &a); err != nil {
		t.Fatalf("unmarshal unprefixed address: %v", err)
	}
	if want := types.HexToAddress("0xf39Fd6e51aad88F6F4ce6aB8827279cffFb92266"); a.Address != want {
		t.Fatalf("decoded address = %s, want %s", a.Address, want)
	}
}

func TestAddressJSON_NotString(t *testing.T) {
	var a addressJSON
	if err := json.Unmarshal([]byte(`123`), &a); err == nil {
		t.Error("expected error for non-string address")
	}
}

// --- hashJSON ---

func TestHashJSON_Valid(t *testing.T) {
	var h hashJSON
	input := `"0x0000000000000000000000000000000000000000000000000000000000000001"`
	if err := json.Unmarshal([]byte(input), &h); err != nil {
		t.Fatalf("unmarshal hash: %v", err)
	}
	if h.Hash[31] != 1 {
		t.Errorf("last byte = %d, want 1", h.Hash[31])
	}
}

func TestHashJSON_AllZero(t *testing.T) {
	var h hashJSON
	input := `"0x0000000000000000000000000000000000000000000000000000000000000000"`
	if err := json.Unmarshal([]byte(input), &h); err != nil {
		t.Fatalf("unmarshal zero hash: %v", err)
	}
	for i, b := range h.Hash {
		if b != 0 {
			t.Errorf("hash[%d] = %d, want 0", i, b)
		}
	}
}

func TestHashJSON_NotString(t *testing.T) {
	var h hashJSON
	if err := json.Unmarshal([]byte(`42`), &h); err == nil {
		t.Error("expected error for non-string hash")
	}
}

// --- hexBytes ---

func TestHexBytes_Valid(t *testing.T) {
	var hb hexBytes
	if err := json.Unmarshal([]byte(`"0xdeadbeef"`), &hb); err != nil {
		t.Fatalf("unmarshal hexBytes: %v", err)
	}
	if len(hb) != 4 {
		t.Errorf("len = %d, want 4", len(hb))
	}
	if hb[0] != 0xde || hb[1] != 0xad || hb[2] != 0xbe || hb[3] != 0xef {
		t.Errorf("got %x, want deadbeef", hb)
	}
}

func TestHexBytes_Empty(t *testing.T) {
	var hb hexBytes
	// "0x" with empty payload: Sscanf on empty string fails, falls back to raw string bytes.
	if err := json.Unmarshal([]byte(`"0x"`), &hb); err != nil {
		t.Fatalf("unmarshal '0x': %v", err)
	}
	// Result is the fallback raw string "0x".
	if string(hb) != "0x" {
		t.Errorf("got %q, want \"0x\" (fallback)", string(hb))
	}
}

func TestHexBytes_EmptyString(t *testing.T) {
	var hb hexBytes
	if err := json.Unmarshal([]byte(`""`), &hb); err != nil {
		t.Fatalf("unmarshal empty string: %v", err)
	}
	if hb != nil {
		t.Errorf("expected nil, got %v", hb)
	}
}

func TestHexBytes_PlainString(t *testing.T) {
	var hb hexBytes
	// Without 0x prefix, falls back to raw string.
	if err := json.Unmarshal([]byte(`"hello"`), &hb); err != nil {
		t.Fatalf("unmarshal plain string: %v", err)
	}
	if string(hb) != "hello" {
		t.Errorf("got %q, want hello", hb)
	}
}

// --- applyForkOverrides ---

func TestApplyForkOverrides_All(t *testing.T) {
	cfg := DefaultConfig()
	ts1 := uint64(1000)
	ts2 := uint64(2000)
	ts3 := uint64(3000)
	cfg.GlamsterdamOverride = &ts1
	cfg.HogotaOverride = &ts2
	cfg.IPlusOverride = &ts3

	cc := &coreconfig.ChainConfig{}
	applyForkOverrides(cc, &cfg)

	if cc.GlamsterdanTime == nil || *cc.GlamsterdanTime != ts1 {
		t.Errorf("GlamsterdanTime = %v, want %d", cc.GlamsterdanTime, ts1)
	}
	if cc.HogotaTime == nil || *cc.HogotaTime != ts2 {
		t.Errorf("HogotaTime = %v, want %d", cc.HogotaTime, ts2)
	}
	if cc.IPlusTime == nil || *cc.IPlusTime != ts3 {
		t.Errorf("IPlusTime = %v, want %d", cc.IPlusTime, ts3)
	}
}

func TestApplyForkOverrides_None(t *testing.T) {
	cfg := DefaultConfig()
	// No overrides set.
	cc := &coreconfig.ChainConfig{}
	cc.HogotaTime = nil
	applyForkOverrides(cc, &cfg)
	// Nothing should be set since cfg has no overrides.
	if cc.HogotaTime != nil {
		t.Errorf("HogotaTime should remain nil, got %v", cc.HogotaTime)
	}
}
