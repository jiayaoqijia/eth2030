package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/eiptest"
)

// TestRPCProbe_BlockQueries exercises the RPC probe surface (plan T2.4):
// the production JSON-RPC server over the simulated chain answers the core
// query methods with correctly encoded fields. This demonstrates the pattern
// that replaces ad-hoc httptest wiring in the legacy pkg-root E2E files
// (e.g. e2e_wave10_test.go's TestRPCE2E_*).
func TestRPCProbe_BlockQueries(t *testing.T) {
	s := eiptest.New(t)
	rpc := s.RPC()

	// One transfer so the head block carries a transaction.
	tx := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(s.Bob().Address), big.NewInt(21), nil,
		100_000, bigFeeCap, bigTipCap,
	)
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)
	headHash := r.Block.Hash().Hex()
	headNum := r.Block.NumberU64()

	// eth_blockNumber: hex-encoded quantity (bare JSON string, not object).
	res := rpc.Call(t, "eth_blockNumber", nil)
	var bn string
	mustUnmarshal(t, res, &bn)
	if bn != "0x1" {
		t.Fatalf("eth_blockNumber = %s, want 0x1", bn)
	}

	// eth_chainId: matches the sim config.
	res = rpc.Call(t, "eth_chainId", nil)
	mustUnmarshal(t, res, &bn)
	if want := "0x539"; bn != want { // 1337
		t.Fatalf("eth_chainId = %s, want %s", bn, want)
	}

	// eth_getBlockByNumber: hash field + transactions as a HASH array (the
	// block carries one tx; empty must stay [] not null — the de58dd9
	// regression).
	params := []interface{}{"0x" + hexUint64(headNum), false}
	res = rpc.Call(t, "eth_getBlockByNumber", params)
	rpc.ExpectField(t, res, "hash", `"`+headHash+`"`)
	var txHashes []string
	mustUnmarshal(t, rpc.Value(res, "transactions"), &txHashes)
	if len(txHashes) != 1 {
		t.Fatalf("block transactions = %v, want exactly the one transfer", txHashes)
	}

	// An empty block must report [] (not null).
	s.Commit()
	res = rpc.Call(t, "eth_getBlockByNumber", []interface{}{"latest", false})
	rpc.ExpectField(t, res, "transactions", `[]`)

	// eth_getBalance: RPC view matches the live sim state.
	res = rpc.Call(t, "eth_getBalance", []interface{}{s.Bob().Address.Hex(), "latest"})
	mustUnmarshal(t, res, &bn)
	want := "0x" + s.Balance(s.Bob().Address).Text(16)
	if bn != want {
		t.Fatalf("eth_getBalance = %s, want %s", bn, want)
	}
}

func hexUint64(n uint64) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = "0123456789abcdef"[n%16]
		n /= 16
	}
	return string(buf[i:])
}
