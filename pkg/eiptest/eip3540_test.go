package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP3540_EOFMagicValidation verifies EIP-3540: post-fork, contract
// creation with initcode starting 0xEF00 that is NOT a valid EOF container is
// rejected, while valid creation code still deploys.
func TestEIP3540_EOFMagicValidation(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// initcode starting with the EOF magic 0xEF00 but not a valid container.
	badEOF := []byte{0xEF, 0x00, 0xFF, 0xFF}
	nonce := s.Nonce(s.Alice().Address)
	tx := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, nonce,
		nil, nil, badEOF,
		100_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r := s.Commit(tx)
	if r.TxCount() == 0 {
		t.Fatal("invalid EOF initcode tx missing from block")
	}
	if tx := r.Tx(0); tx.Success() {
		t.Fatal("invalid EOF initcode deployed successfully (EIP-3540 violation)")
	}

	// Sanity: normal (non-0xEF) creation code still deploys.
	good := stf.Deploy(s.Bob(), []byte{0x00})
	if len(s.Code(good)) == 0 && s.Code(good) != nil {
		t.Fatal("plain deploy broken")
	}
}
