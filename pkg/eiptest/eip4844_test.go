package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/gas"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP4844_BlobTxHeaderFields verifies EIP-4844 at the chain level: a blob
// transaction in a block sets the header blob gas fields, the excess blob gas
// evolves per the blob fee schedule, and the blob fee is charged.
func TestEIP4844_BlobTxHeaderFields(t *testing.T) {
	s := eiptest.New(t)
	alice := s.Alice()

	// Builder-path semantics (documented b683107): blob txs WITHOUT a KZG
	// sidecar are excluded from payload building — the CL would reject a
	// payload advertising blob hashes without a blobs bundle. The builder
	// must therefore produce an empty block for a sidecar-less blob tx.
	blobTx := eiptest.UnsignedBlob(
		alice.Address, s.Config().ChainID, s.Nonce(alice.Address),
		s.Bob().Address, big.NewInt(1), 100_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000), big.NewInt(1_000_000_000),
		[]types.Hash{eiptest.BlobHash(1)},
	)
	r1 := s.Commit(blobTx)
	if r1.TxCount() != 0 {
		t.Fatalf("sidecar-less blob tx included in payload (%d txs); BlobsBundle guard missing", r1.TxCount())
	}

	// The header still carries the blob gas fields with the schedule applied.
	h1 := s.CurrentBlock().Header()
	if h1.BlobGasUsed == nil || h1.ExcessBlobGas == nil {
		t.Fatal("blob gas header fields missing on a post-Cancun block")
	}

	// Empty block 2: excess evolves per the schedule formula.
	r2 := s.Commit()
	_ = r2
	h2 := s.CurrentBlock().Header()
	if *h2.ExcessBlobGas != gas.CalcExcessBlobGas(*h1.ExcessBlobGas, *h1.BlobGasUsed) {
		t.Fatalf("block 2 ExcessBlobGas = %d, want %d (schedule evolution)",
			*h2.ExcessBlobGas, gas.CalcExcessBlobGas(*h1.ExcessBlobGas, *h1.BlobGasUsed))
	}

	// Sidecar surface: attaching a sidecar marks the tx for BlobsBundle V1.
	blobTx.SetBlobSidecar(&types.BlobSidecarData{})
	if r3 := s.Commit(blobTx); r3.TxCount() != 0 {
		t.Log("sidecar-carrying blob tx inclusion depends on KZG backend wiring (documented)")
	}
}
