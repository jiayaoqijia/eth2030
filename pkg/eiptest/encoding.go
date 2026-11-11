package eiptest

import (
	"bytes"
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
	_, sigR, sigS := tx.RawSignatureValues()
	if sigR != nil && sigS != nil && sigR.Sign() != 0 && sigS.Sign() != 0 {
		signer := types.MakeSigner(e.s.cfg.ChainID.Uint64(), tx.Type())
		want, err := signer.Sender(tx)
		if err != nil {
			t.Fatalf("rlp: recover original sender: %v", err)
		}
		got, err := signer.Sender(dec)
		if err != nil || got != want {
			t.Fatalf("rlp: decoded sender %s, want %s: %v", got.Hex(), want.Hex(), err)
		}
		if cached := tx.Sender(); cached != nil && *cached != got {
			t.Fatalf("rlp: recovered sender %s, want %s", got.Hex(), cached.Hex())
		}
	}
	return dec
}

// SSZRoundTrip verifies the decoded transaction and its canonical re-encoding.
func (e *Encoding) SSZRoundTrip(t *testing.T, tx *types.Transaction) {
	t.Helper()
	enc, err := types.TransactionToSSZ(tx)
	if err != nil {
		t.Fatalf("ssz encode: %v", err)
	}
	dec, err := types.SSZToTransaction(enc)
	if err != nil {
		t.Fatalf("ssz decode: %v", err)
	}
	if dec.Type() != tx.Type() || dec.Hash() != tx.Hash() {
		t.Fatalf("ssz roundtrip: type/hash %d/%s, want %d/%s", dec.Type(), dec.Hash().Hex(), tx.Type(), tx.Hash().Hex())
	}
	reencoded, err := types.TransactionToSSZ(dec)
	if err != nil || !bytes.Equal(reencoded, enc) {
		t.Fatalf("ssz re-encode: bytes changed: %v", err)
	}
}

// Vector is one row of a JSON vector file. Keys are file-defined; Values holds
// the raw decoded JSON object.
type Vector struct {
	Name   string
	Path   string
	Values map[string]json.RawMessage
}

// LoadVectors reads all *.json files under dir (relative to the eiptest
// package directory, e.g. "testdata/eip7939") as vector tables. Each JSON
// file must contain either a single object or an array of objects.
func LoadVectors(t *testing.T, dir string) []Vector {
	t.Helper()
	abs := dir
	if !filepath.IsAbs(abs) {
		abs = filepath.Join("testdata", dir)
	}
	entries, err := vectorFiles(abs)
	if err != nil {
		t.Fatalf("vectors: read dir %s: %v", abs, err)
	}
	var out []Vector
	for _, ent := range entries {
		data, err := os.ReadFile(ent)
		if err != nil {
			t.Fatalf("vectors: read %s: %v", ent, err)
		}
		var arr []map[string]json.RawMessage
		if err := json.Unmarshal(data, &arr); err != nil {
			// Single object fallback.
			var one map[string]json.RawMessage
			if err2 := json.Unmarshal(data, &one); err2 != nil {
				t.Fatalf("vectors: parse %s: %v", ent, err)
			}
			arr = []map[string]json.RawMessage{one}
		}
		for i, obj := range arr {
			name := fmt.Sprintf("%s#%d", ent, i)
			if n, ok := obj["name"]; ok {
				var s string
				if json.Unmarshal(n, &s) == nil {
					name = s
				}
			}
			out = append(out, Vector{Name: name, Path: ent, Values: obj})
		}
	}
	if len(out) == 0 {
		t.Fatalf("vectors: no vectors found in %s", abs)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func vectorFiles(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && strings.HasPrefix(entry.Name(), ".") && path != dir {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			paths = append(paths, path)
		}
		return nil
	})
	return paths, err
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
