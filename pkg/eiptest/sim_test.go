package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestSimDemo is the framework acceptance test (plan T1.1): genesis → transfer,
// deploy, call in 3 blocks with contract state carried across blocks, using
// only framework API (zero setup code).
func TestSimDemo(t *testing.T) {
	s := eiptest.New(t)

	// Block 1: plain value transfer.
	transfer := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(s.Bob().Address), big.NewInt(5), nil,
		100_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r1 := s.Commit(transfer)
	eiptest.ExpectSuccess(t, r1, 0)
	eiptest.ExpectBalance(t, s, s.Bob().Address, new(big.Int).Add(new(big.Int).Mul(big.NewInt(1000), eiptest.Ether), big.NewInt(5)))

	// Block 2: contract deployment (runtime stores 0x2a in slot 0).
	deploy := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		nil, big.NewInt(0), contractStore42Init,
		200_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r2 := s.Commit(deploy)
	tx2 := eiptest.ExpectSuccess(t, r2, 0)
	contract := tx2.ContractAddress()
	if contract == (types.Address{}) {
		t.Fatal("deployment produced empty contract address")
	}

	// Block 3: call the contract; state continuity across blocks is proven by
	// the storage slot being written by the constructor at block 2.
	call := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(contract), big.NewInt(0), []byte{},
		100_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r3 := s.Commit(call)
	eiptest.ExpectSuccess(t, r3, 0)
	eiptest.ExpectStorage(t, s, contract, types.Hash{}, eiptest.Uint64ToHash(42))
}

// contractStore42Init is hand-written initcode: constructor SSTOREs 42 at slot
// 0, then returns the runtime code (which STOPs).
var contractStore42Init = []byte{
	0x60, 0x2a, // PUSH1 42
	0x60, 0x00, // PUSH1 0
	0x55, // SSTORE
	0x60, 0x01, // PUSH1 1  (runtime size)
	0x60, 0x0c, // PUSH1 12 (runtime offset)
	0x60, 0x00, // PUSH1 0
	0x39, // CODECOPY
	0x60, 0x01, // PUSH1 1
	0x60, 0x00, // PUSH1 0
	0xf3, // RETURN
	0x00, // runtime: STOP
}
