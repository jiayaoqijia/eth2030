package eiptest_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

func TestProbeDelegateCall(t *testing.T) {
	s := eiptest.New(t)
	alice, bob := s.Alice(), s.Bob()
	chainID := s.Config().ChainID
	delegate := s.STF().Deploy(alice, []byte{0x33, 0x60, 0x00, 0x55, 0x00})
	auth := eiptest.Auth(chainID, alice, delegate, s.Nonce(alice.Address)+1)
	tx := eiptest.SetCode(chainID, alice, s.Nonce(alice.Address), bob.Address, 500_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000), []types.Authorization{auth})
	r := s.Commit(tx)
	fmt.Printf("PD setcode: status=%d code=%x\n", r.Tx(0).Status(), s.Code(alice.Address))

	call := eiptest.UnsignedDynamicFee(
		bob.Address, chainID, s.Nonce(bob.Address),
		eiptest.ToPtr(alice.Address), nil, nil,
		200_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r2 := s.Commit(call)
	fmt.Printf("PD call: status=%d gas=%d storage=%s\n", r2.Tx(0).Status(), r2.Tx(0).GasUsed(), s.StorageAt(alice.Address, types.Hash{}))
}
