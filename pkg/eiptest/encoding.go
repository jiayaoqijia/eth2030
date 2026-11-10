package eiptest

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// Encoding is the encoding probe: RLP/SSZ round-trip checks and table-driven
// vector files for tx and header types.
type Encoding struct{ s *Sim }

// Encoding returns the encoding probe.
func (s *Sim) Encoding() *Encoding { return &Encoding{s: s} }

// RLPRoundTrip encodes tx to RLP and decodes it back, asserting equality of
// hash, sender, type and calldata. Fails t on any mismatch.
func (e *Encoding) RLPRoundTrip(t *testing.T, tx *types.Transaction) *types.Transaction {
	t.Helper()
	// Use the Transaction's own EncodeRLP (typed envelope with type prefix);
	// the generic rlp.EncodeToBytes does not route through it.
	enc, err := tx.EncodeRLP()
	if err != nil {
		t.Fatalf("rlp encode: %v", err)
	}
	dec, err := types.DecodeTxRLP(enc)
	if err != nil {
		t.Fatalf("rlp decode: %v\nencoded: %x", err, enc)
	}
	if dec.Hash() != tx.Hash() {
		t.Fatalf("rlp roundtrip: hash mismatch: got %s want %s", dec.Hash().Hex(), tx.Hash().Hex())
	}
	if dec.Type() != tx.Type() {
		t.Fatalf("rlp roundtrip: type mismatch: got %d want %d", dec.Type(), tx.Type())
	}
	if tx.Sender() != nil && dec.Sender() != nil && *dec.Sender() != *tx.Sender() {
		t.Fatalf("rlp roundtrip: sender mismatch: got %s want %s", dec.Sender().Hex(), tx.Sender().Hex())
	}
	return dec
}

// SSZRoundTrip encodes tx to SSZ (EIP-6404) twice and asserts determinism
// plus non-empty output. (The types package currently exposes SSZ encoding;
// decode round-trips are covered by its own unit tests.)
func (e *Encoding) SSZRoundTrip(t *testing.T, tx *types.Transaction) {
	t.Helper()
	enc1, err := types.TransactionToSSZ(tx)
	if err != nil {
		t.Fatalf("ssz encode: %v", err)
	}
	enc2, err := types.TransactionToSSZ(tx)
	if err != nil {
		t.Fatalf("ssz encode (2nd): %v", err)
	}
	if len(enc1) == 0 {
		t.Fatalf("ssz encode produced empty output for tx type %d", tx.Type())
	}
	if string(enc1) != string(enc2) {
		t.Fatalf("ssz encode not deterministic for tx type %d", tx.Type())
	}
}

// Vector is one row of a JSON vector file. Keys are file-defined; Values holds
// the raw decoded JSON object.
type Vector struct {
	Name   string
	Values map[string]json.RawMessage
}

// LoadVectors reads all *.json files under dir (relative to the eiptest
// package directory, e.g. "testdata/eip7939") as vector tables. Each JSON
// file must contain either a single object or an array of objects.
func LoadVectors(t *testing.T, dir string) []Vector {
	t.Helper()
	abs := filepath.Join("testdata", dir)
	entries, err := os.ReadDir(abs)
	if err != nil {
		t.Fatalf("vectors: read dir %s: %v", abs, err)
	}
	var out []Vector
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(abs, ent.Name()))
		if err != nil {
			t.Fatalf("vectors: read %s: %v", ent.Name(), err)
		}
		var arr []map[string]json.RawMessage
		if err := json.Unmarshal(data, &arr); err != nil {
			// Single object fallback.
			var one map[string]json.RawMessage
			if err2 := json.Unmarshal(data, &one); err2 != nil {
				t.Fatalf("vectors: parse %s: %v", ent.Name(), err)
			}
			arr = []map[string]json.RawMessage{one}
		}
		for i, obj := range arr {
			name := fmt.Sprintf("%s#%d", ent.Name(), i)
			if n, ok := obj["name"]; ok {
				var s string
				if json.Unmarshal(n, &s) == nil {
					name = s
				}
			}
			out = append(out, Vector{Name: name, Values: obj})
		}
	}
	if len(out) == 0 {
		t.Fatalf("vectors: no vectors found in %s", abs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// VectorString decodes a required string field from the vector.
func (v Vector) VectorString(t *testing.T, key string) string {
	t.Helper()
	raw, ok := v.Values[key]
	if !ok {
		t.Fatalf("vector %s: missing field %q", v.Name, key)
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatalf("vector %s: field %q: %v", v.Name, key, err)
	}
	return s
}

// VectorBytes decodes a required hex field (0x-prefixed) from the vector.
func (v Vector) VectorBytes(t *testing.T, key string) []byte {
	t.Helper()
	s := v.VectorString(t, key)
	b, err := DecodeHex(s)
	if err != nil {
		t.Fatalf("vector %s: field %q: %v", v.Name, key, err)
	}
	return b
}

// VectorUint decodes a required numeric field from the vector.
func (v Vector) VectorUint(t *testing.T, key string) uint64 {
	t.Helper()
	raw, ok := v.Values[key]
	if !ok {
		t.Fatalf("vector %s: missing field %q", v.Name, key)
	}
	var n uint64
	if err := json.Unmarshal(raw, &n); err != nil {
		t.Fatalf("vector %s: field %q: %v", v.Name, key, err)
	}
	return n
}

// DecodeHex parses a 0x-prefixed hex string.
func DecodeHex(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	return hex.DecodeString(s)
}

// MustHex parses a 0x-prefixed hex string, failing t on error.
func MustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := DecodeHex(s)
	if err != nil {
		t.Fatalf("hex %q: %v", s, err)
	}
	return b
}

// Hex renders bytes as a 0x-prefixed hex string.
func Hex(b []byte) string { return "0x" + hex.EncodeToString(b) }

// BlockToSSZ and SSZToBlock expose the types package block SSZ codec to
// external test packages.
func BlockToSSZ(b *types.Block) ([]byte, error) { return types.BlockToSSZ(b) }

func SSZToBlock(data []byte) (*types.Block, error) { return types.SSZToBlock(data) }
