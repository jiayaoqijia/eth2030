package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/eiptest/contracts"
)

func TestEIP8141_ApprovedFrameAndReplay(t *testing.T) {
	sender := types.Address{0x70}
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkGlamsterdan, 0),
		eiptest.FundCode(sender, eiptest.Ether, contracts.Named{}.FrameApprove(), 0))
	pool := s.Pool()
	for seq := uint64(0); seq < 2; seq++ {
		tx := eiptest.UnsignedFrame(sender, s.Config().ChainID, new(big.Int).SetUint64(seq), 200_000,
			bigFeeCap, bigTipCap, []types.Frame{{Mode: types.ModeVerify, GasLimit: 100_000}})
		decoded := s.Encoding().RLPRoundTrip(t, tx)
		if decoded.FrameSender() != sender || len(decoded.Frames()) != 1 || decoded.Frames()[0].Mode != types.ModeVerify {
			t.Fatal("frame envelope lost its sender or VERIFY frame")
		}
		pool.ExpectAccepted(t, decoded)
		result := pool.CommitPending()
		eiptest.ExpectSuccess(t, result, 0)
		eiptest.ExpectNonce(t, s, sender, seq+1)
		pool.ExpectRejected(t, decoded, "nonce too low")
	}
}
