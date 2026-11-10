package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP7825_TransactionGasCap verifies EIP-7825 through the gas-cap
// validation API: transactions above MaxTransactionGas (2^24) are rejected,
// at-cap transactions pass, and the fork config activates the cap post-Prague.
func TestEIP7825_TransactionGasCap(t *testing.T) {
	s := eiptest.New(t)

	cfg := gas.GasCapConfigForFork(s.Config(), s.Now())
	if cfg.MaxTxGas != gas.MaxTransactionGas {
		t.Fatalf("MaxTxGas = %d, want %d (2^24)", cfg.MaxTxGas, gas.MaxTransactionGas)
	}

	overCap := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(s.Bob().Address), nil, nil,
		gas.MaxTransactionGas+1,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	if err := gas.ValidateTransactionGasCaps(overCap, cfg); err == nil {
		t.Fatal("over-cap tx accepted by gas cap validation (EIP-7825 violation)")
	}

	atCap := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(s.Bob().Address), nil, nil,
		gas.MaxTransactionGas,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	if err := gas.ValidateTransactionGasCaps(atCap, cfg); err != nil {
		t.Fatalf("at-cap tx rejected: %v", err)
	}

	// Blob count per tx also capped by the schedule.
	blobTx := eiptest.UnsignedBlob(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		s.Bob().Address, nil, 100_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000), big.NewInt(1_000_000_000),
		[]types.Hash{eiptest.BlobHash(1)},
	)
	if err := gas.ValidateTransactionGasCaps(blobTx, cfg); err != nil {
		t.Fatalf("single-blob tx rejected: %v", err)
	}
}
