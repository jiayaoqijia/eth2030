package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/eiptest"
)

func TestEngineProbe_ValidatesTwoBlocks(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	p := s.Engine()
	parent := p.HeadHash()
	for nonce := uint64(0); nonce < 2; nonce++ {
		tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), nonce, eiptest.ToPtr(s.Bob().Address), eiptest.Wei(1), nil, 21_000, bigFeeCap, bigTipCap)
		p.Submit(tx)
		payload, status, err := p.BuildAndProcess()
		if err != nil {
			t.Fatal(err)
		}
		p.ExpectStatusValid(t, status)
		p.ExpectPayloadTxCount(t, payload, 1)
		if payload.BlockNumber != nonce+1 || payload.ParentHash != parent || p.HeadHash() != payload.BlockHash {
			t.Fatalf("unexpected block %d with parent %s", payload.BlockNumber, payload.ParentHash.Hex())
		}
		parent = payload.BlockHash
	}
	eiptest.ExpectNonce(t, s, s.Alice().Address, 2)
}
