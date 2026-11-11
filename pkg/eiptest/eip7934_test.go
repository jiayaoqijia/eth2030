package eiptest_test

import (
	"strings"
	"testing"

	"github.com/eth2030/eth2030/eiptest"
)

func TestEIP7934(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkOsaka, 100))
	fork := s.Fork(eiptest.ForkOsaka)
	fork.Before().ExpectInactive(t)
	fork.AtBoundary().ExpectActive(t)

	// EIP-7934 caps the entire RLP block at 8 MiB, including its header.
	tx := eiptest.UnsignedDynamicFee(s.Alice().Address, s.Config().ChainID, 0,
		eiptest.ToPtr(s.Bob().Address), nil, make([]byte, 8<<20), 100_000, bigFeeCap, bigTipCap)
	err := s.CommitInvalid(tx)
	if err == nil || !strings.Contains(err.Error(), "RLP block size") {
		t.Fatalf("want RLP block size rejection, got %v", err)
	}
	if got := s.Commit().TxCount(); got != 0 {
		t.Fatalf("ordinary empty block contains %d transactions", got)
	}
}
