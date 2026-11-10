package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP7918_BlobBaseFeeFloor verifies the blob base fee reserve-price
// floor: when excess blob gas is very low, the computed fee falls below the
// execution-linked reserve price and the floor must win.
func TestEIP7918_BlobBaseFeeFloor(t *testing.T) {
	s := eiptest.New(t)

	// Fresh chain: excess blob gas = 0 → raw fake-exponential fee = 1 wei,
	// far below any execution-linked reserve price.
	h := s.CurrentBlock().Header()
	fee := gas.CalcBlobBaseFeeV2(0, h.BaseFee)
	if fee == nil || fee.Sign() <= 0 {
		t.Fatalf("blob base fee = %v, want positive", fee)
	}

	// EIP-7918 reserve price = BLOB_BASE_COST(8192) * baseFee / GAS_PER_BLOB.
	// With excess=0 the raw fee is minimal; the reserve must dominate and be
	// exactly 8192 * baseFee / 131072.
	baseFee := big.NewInt(1_000_000_000)
	floored := gas.CalcBlobBaseFeeV2WithFraction(0, baseFee, 1_000_000)
	wantReserve := new(big.Int).Mul(big.NewInt(gas.BlobBaseCost), baseFee)
	wantReserve.Div(wantReserve, big.NewInt(gas.GasPerBlob))
	if floored.Cmp(wantReserve) != 0 {
		t.Fatalf("floored fee = %v, want reserve price %v (EIP-7918 floor not applied)", floored, wantReserve)
	}
	// Without an execution base fee the raw fake-exponential fee stands.
	rawOnly := gas.CalcBlobBaseFeeV2WithFraction(0, nil, 1_000_000)
	if rawOnly.Cmp(big.NewInt(gas.MinBaseFeePerBlobGas)) != 0 {
		t.Fatalf("raw blob fee with zero excess = %v, want MinBaseFeePerBlobGas %d", rawOnly, gas.MinBaseFeePerBlobGas)
	}
}
