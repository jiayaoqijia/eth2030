package eiptest

import (
	"encoding/json"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// This file provides expectation helpers with readable failure output. Every
// helper takes *testing.T and fails the test with context on violation.

// ExpectSuccess asserts the tx at index i succeeded.
func ExpectSuccess(t *testing.T, r *BlockResult, i int) *TxResult {
	t.Helper()
	return r.Tx(i).ExpectStatus(t, types.ReceiptStatusSuccessful)
}

// ExpectRevert asserts the tx at index i reverted.
func ExpectRevert(t *testing.T, r *BlockResult, i int) *TxResult {
	t.Helper()
	return r.Tx(i).ExpectStatus(t, types.ReceiptStatusFailed)
}

// ExpectGasUsed asserts the exact gas used by a successful transaction.
func ExpectGasUsed(t *testing.T, r *BlockResult, i int, want uint64) *TxResult {
	t.Helper()
	return ExpectSuccess(t, r, i).ExpectGas(t, want, want)
}

// ExpectGasBetween asserts a successful transaction's gas used is in [lo, hi].
func ExpectGasBetween(t *testing.T, r *BlockResult, i int, lo, hi uint64) *TxResult {
	t.Helper()
	return ExpectSuccess(t, r, i).ExpectGas(t, lo, hi)
}

// ExpectLogCount asserts the tx emitted exactly n logs.
func ExpectLogCount(t *testing.T, r *BlockResult, i int, n int) *TxResult {
	t.Helper()
	tx := ExpectSuccess(t, r, i)
	if len(tx.Logs()) != n {
		failBlock(t, r, "tx %d: log count = %d, want %d (%v)", i, len(tx.Logs()), n, tx.Logs())
	}
	return tx
}

// ExpectLog asserts the tx emitted a log with the given address and first
// topic at position logIdx.
func ExpectLog(t *testing.T, r *BlockResult, i, logIdx int, addr types.Address, topic types.Hash) *types.Log {
	t.Helper()
	tx := ExpectSuccess(t, r, i)
	logs := tx.Logs()
	if logIdx < 0 || logIdx >= len(logs) {
		failBlock(t, r, "tx %d: log %d missing (have %d logs)", i, logIdx, len(logs))
	}
	log := logs[logIdx]
	if log.Address != addr {
		failBlock(t, r, "tx %d log %d: emitter = %s, want %s", i, logIdx, log.Address.Hex(), addr.Hex())
	}
	if len(log.Topics) == 0 || log.Topics[0] != topic {
		failBlock(t, r, "tx %d log %d: first topic = %v, want %s", i, logIdx, log.Topics, topic.Hex())
	}
	return log
}

// ExpectBalance asserts the account balance equals want wei.
func ExpectBalance(t *testing.T, s *Sim, addr types.Address, want *big.Int) {
	t.Helper()
	got := s.Balance(addr)
	if got.Cmp(want) != 0 {
		t.Fatalf("balance(%s) = %s wei, want %s wei", addr.Hex(), got, want)
	}
}

// ExpectBalanceDelta asserts the account balance changed by exactly delta
// (positive or negative) relative to want baseline.
func ExpectBalanceDelta(t *testing.T, s *Sim, addr types.Address, before, delta *big.Int) {
	t.Helper()
	want := new(big.Int).Add(before, delta)
	ExpectBalance(t, s, addr, want)
}

// ExpectNonce asserts the account nonce.
func ExpectNonce(t *testing.T, s *Sim, addr types.Address, want uint64) {
	t.Helper()
	if got := s.Nonce(addr); got != want {
		t.Fatalf("nonce(%s) = %d, want %d", addr.Hex(), got, want)
	}
}

// ExpectCode asserts the account code (empty allowed).
func ExpectCode(t *testing.T, s *Sim, addr types.Address, want []byte) {
	t.Helper()
	got := s.Code(addr)
	if len(got) != len(want) {
		t.Fatalf("code(%s) len = %d, want %d; got %x want %x", addr.Hex(), len(got), len(want), got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("code(%s) mismatch at %d: got %x want %x", addr.Hex(), i, got, want)
		}
	}
}

// ExpectStorage asserts a storage slot value.
func ExpectStorage(t *testing.T, s *Sim, addr types.Address, slot, want types.Hash) {
	t.Helper()
	if got := s.StorageAt(addr, slot); got != want {
		t.Fatalf("storage(%s, %s) = %s, want %s", addr.Hex(), slot.Hex(), got.Hex(), want.Hex())
	}
}

// ExpectDelegateAt asserts the EIP-7702 delegate designation indicator
// (0xef0100 || address) on the account code.
func ExpectDelegateAt(t *testing.T, s *Sim, addr types.Address, delegate types.Address) {
	t.Helper()
	want := append([]byte{0xef, 0x01, 0x00}, delegate[:]...)
	ExpectCode(t, s, addr, want)
}

// ExpectRequests asserts the block carries a requests hash (EIP-7685).
func ExpectRequests(t *testing.T, s *Sim, n uint64) {
	t.Helper()
	blk := s.Block(n)
	if blk.Header().RequestsHash == nil {
		t.Fatalf("block %d: RequestsHash is nil", n)
	}
}

// ExpectHeaderField is a generic header field assertion via accessor.
func ExpectHeaderField(t *testing.T, s *Sim, n uint64, name string, get func(*types.Header) bool, want bool) {
	t.Helper()
	if got := get(s.Block(n).Header()); got != want {
		t.Fatalf("block %d: header %s = %v, want %v", n, name, got, want)
	}
}

// ExpectBaseFeeAtLeast asserts the block base fee is >= min.
func ExpectBaseFeeAtLeast(t *testing.T, s *Sim, n uint64, min *big.Int) {
	t.Helper()
	got := s.Block(n).Header().BaseFee
	if got == nil || got.Cmp(min) < 0 {
		t.Fatalf("block %d: base fee = %v, want >= %s", n, got, min)
	}
}

// dumpReceipts produces a readable dump of block receipts for failure context.
func dumpReceipts(r *BlockResult) string {
	var out strings.Builder
	fmt.Fprintf(&out, "block %d (%d txs, gasUsed %d):\n", r.Block.NumberU64(), r.TxCount(), r.GasUsed())
	for i, tx := range r.Txs() {
		fmt.Fprintf(&out, "  tx[%d] status=%d gas=%d logs=%d\n", i, tx.Status(), tx.GasUsed(), len(tx.Logs()))
		for j, l := range tx.Logs() {
			fmt.Fprintf(&out, "    log[%d] addr=%s topics=%x data=%x\n", j, l.Address.Hex(), l.Topics, l.Data)
		}
	}
	return out.String()
}

// ExpectBAL asserts that the returned execution BAL matches the block header.
func ExpectBAL(t *testing.T, r *BlockResult) {
	t.Helper()
	h := r.Block.Header()
	if r.BAL == nil || h.BlockAccessListHash == nil || r.BAL.Hash() != *h.BlockAccessListHash {
		failBlock(t, r, "BAL missing or inconsistent with header")
	}
}

// ExpectRequestCount asserts the number of execution requests in the result.
func ExpectRequestCount(t *testing.T, r *BlockResult, want int) {
	t.Helper()
	if len(r.Requests) != want {
		failBlock(t, r, "requests: got %d, want %d", len(r.Requests), want)
	}
}

func failBlock(t *testing.T, r *BlockResult, format string, args ...any) {
	t.Helper()
	if r == nil {
		t.Fatalf(format, args...)
	}
	t.Fatalf("%s\n%s", fmt.Sprintf(format, args...), blockDiagnostics(r))
}

func blockDiagnostics(r *BlockResult) string {
	var out strings.Builder
	out.WriteString(dumpReceipts(r))
	balJSON, _ := json.Marshal(r.BAL)
	fmt.Fprintf(&out, "BAL: %s\nrequests: %x\n", balJSON, types.EncodeRequests(r.Requests))
	if r.Sim != nil {
		parent := r.Sim.bc.GetBlock(r.Block.ParentHash())
		if parent != nil {
			fmt.Fprintf(&out, "state roots: %s -> %s\n", parent.Header().Root.Hex(), r.Block.Header().Root.Hex())
			before, beforeErr := r.Sim.bc.StateAtBlock(parent)
			after, afterErr := r.Sim.bc.StateAtBlock(r.Block)
			if beforeErr == nil && afterErr == nil {
				addresses := map[types.Address]bool{r.Block.Header().Coinbase: true}
				for _, tx := range r.Block.Transactions() {
					if sender := tx.Sender(); sender != nil {
						addresses[*sender] = true
					}
					if to := tx.To(); to != nil {
						addresses[*to] = true
					}
				}
				if r.BAL != nil {
					for _, entry := range r.BAL.Entries {
						addresses[entry.Address] = true
					}
				}
				ordered := make([]types.Address, 0, len(addresses))
				for addr := range addresses {
					ordered = append(ordered, addr)
				}
				sort.Slice(ordered, func(i, j int) bool { return ordered[i].Hex() < ordered[j].Hex() })
				for _, addr := range ordered {
					oldBalance, newBalance := before.GetBalance(addr), after.GetBalance(addr)
					oldNonce, newNonce := before.GetNonce(addr), after.GetNonce(addr)
					if oldBalance.Cmp(newBalance) != 0 || oldNonce != newNonce {
						fmt.Fprintf(&out, "state delta %s: balance %s -> %s, nonce %d -> %d\n", addr.Hex(), oldBalance, newBalance, oldNonce, newNonce)
					}
				}
			}
		}
	}
	return out.String()
}
