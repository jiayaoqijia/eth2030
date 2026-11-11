package eiptest_test

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
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

	delegate := stf.Deploy(s.Alice(), s.Contracts().DelegateCaller())
	args := append(make([]byte, 12), addr[:]...)
	args = append(args, contracts.ABIEncodeUint256(11)...)
	stf.Tx(s.Alice(), delegate, append(contracts.ABISelector("setThrough(address,uint256)"), args...))
	eiptest.ExpectStorage(t, s, delegate, types.Hash{}, eiptest.Uint64ToHash(11))
	eiptest.ExpectStorage(t, s, addr, types.Hash{}, eiptest.Uint64ToHash(7))

	entry := stf.Deploy(s.Alice(), s.Contracts().AAEntrypoint())
	stf.Tx(s.Alice(), entry, contracts.ABISelector("execute()"))
	eiptest.ExpectStorage(t, s, entry, types.Hash{}, eiptest.Uint64ToHash(1))

	anchor := stf.Deploy(s.Alice(), s.Contracts().RollupAnchor())
	root := types.Hash{0x42}
	stf.Tx(s.Alice(), anchor, append(contracts.ABISelector("record(bytes32)"), root[:]...))
	eiptest.ExpectStorage(t, s, anchor, types.Hash{}, root)

	// Named accessor parity.
	if len(s.Contracts().Storage42()) != len(storage) {
		t.Fatal("Named accessor diverges from Runtime() lookup")
	}
	// Names list is stable.
	if len(contracts.Names()) != 7 {
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
