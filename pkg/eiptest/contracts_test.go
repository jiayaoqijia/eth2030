package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/eiptest/contracts"
)

// TestContractsPipeline verifies T4.1: solc-compiled scenario contracts
// deploy and execute through the framework (the .bin-runtime → gen.go path).
func TestContractsPipeline(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// Storage42: constructor arg via ABI encoding.
	storage, ok := contracts.Runtime("Storage42")
	if !ok || len(storage) == 0 {
		t.Fatal("Storage42 runtime missing from the generated contract set")
	}
	// Storage42's constructor takes (uint256 v) — deploy via a wrapper:
	// the solc runtime has no constructor, so exercise set()/get() instead.
	addr := stf.Deploy(s.Alice(), storage)

	// set(7): selector = keccak("set(uint256)")[:4], arg = 7.
	calldata := append(contracts.ABISelector("set(uint256)"), contracts.ABIEncodeUint256(7)...)
	stf.Tx(s.Alice(), addr, calldata)

	// get(): selector only; read via eth_call.
	ret, err := stf.Call(s.Alice().Address, addr, contracts.ABISelector("get()"))
	if err != nil {
		t.Fatalf("get() call failed: %v", err)
	}
	got := new(big.Int).SetBytes(ret)
	if got.Int64() != 7 {
		t.Fatalf("get() = %d, want 7 (solc-compiled contract roundtrip)", got)
	}

	// Echo: fallback returns calldata verbatim.
	echoAddr := stf.Deploy(s.Alice(), s.Contracts().Echo())
	payload := []byte{0xde, 0xad, 0xbe, 0xef}
	ret2, err := stf.Call(s.Alice().Address, echoAddr, payload)
	if err != nil {
		t.Fatalf("echo call failed: %v", err)
	}
	if !equalBytes(ret2, payload) {
		t.Fatalf("echo returned %x, want %x", ret2, payload)
	}

	// Named accessor parity.
	if len(s.Contracts().Storage42()) != len(storage) {
		t.Fatal("Named accessor diverges from Runtime() lookup")
	}
	// Names list is stable.
	if len(contracts.Names()) != 3 {
		t.Fatalf("scenario contract count = %d, want 3", len(contracts.Names()))
	}
}

func equalBytes(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var _ = eiptest.GweiWei
