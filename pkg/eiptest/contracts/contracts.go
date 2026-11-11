// Package contracts exposes scenario contract bytecode for eiptest tests.
// The runtime hex is compiled from contracts/*.sol by `make contracts`
// (solc → gen.go). Tests reference eiptest.Contracts.X() to deploy.
package contracts

import (
	"encoding/hex"
	"strings"

	"golang.org/x/crypto/sha3"
)

// Named scenario runtimes.
var named = map[string]string{
	"Storage42":    Storage42Runtime,
	"SelfDestruct": SelfDestructRuntime,
	"Echo":         EchoRuntime,
}

// Runtime returns the scenario runtime bytecode by name; ok=false for
// unknown names.
func Runtime(name string) (code []byte, ok bool) {
	hex, known := named[name]
	if !known {
		return nil, false
	}
	return FromHex(hex), true
}

// Names lists the available scenario contracts.
func Names() []string {
	return []string{"Echo", "SelfDestruct", "Storage42"}
}

// FromHex decodes a hex string (0x optional).
func FromHex(s string) []byte {
	s = strings.TrimPrefix(s, "0x")
	if len(s)%2 == 1 {
		s = "0" + s
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		panic(err)
	}
	return b
}

// ABISelector computes the 4-byte function selector for a canonical
// signature, e.g. ABISelector("set(uint256)").
func ABISelector(sig string) []byte {
	h := sha3.NewLegacyKeccak256()
	h.Write([]byte(sig))
	return h.Sum(nil)[:4]
}

// ABIEncodeUint256 ABI-encodes a single uint256 argument.
func ABIEncodeUint256(v uint64) []byte {
	out := make([]byte, 32)
	out[31] = byte(v)
	out[30] = byte(v >> 8)
	out[29] = byte(v >> 16)
	out[28] = byte(v >> 24)
	out[27] = byte(v >> 32)
	out[26] = byte(v >> 40)
	out[25] = byte(v >> 48)
	out[24] = byte(v >> 56)
	return out
}
