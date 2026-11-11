package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP6780_SelfdestructRestriction verifies EIP-6780: SELFDESTRUCT in a
// transaction that did NOT create the contract only moves the balance — the
// code and storage survive.
func TestEIP6780_SelfdestructRestriction(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// Runtime: with empty calldata → STOP (allows funding). With any
	// calldata → SSTORE(0,1) then SELFDESTRUCT to Bob. The jump target is
	// computed from the actual layout to keep the JUMPDEST aligned.
	destroyTo := s.Bob().Address
	head := []byte{
		0x60, 0x00, // PUSH1 0
		0x35,       // CALLDATALOAD(0)
		0x60, 0x00, // PUSH1 0
		0x14,       // EQ
		0x60, 0x00, // PUSH1 <dest> — patched below
		0x57,       // JUMPI
		0x60, 0x01, // PUSH1 1
		0x60, 0x00, // PUSH1 0
		0x55, // SSTORE(0, 1)
	}
	code := append([]byte{}, head...)
	code = append(code, 0x73)
	code = append(code, destroyTo[:]...)
	code = append(code, 0xff)          // SELFDESTRUCT
	code = append(code, 0x5b)          // JUMPDEST (target)
	code = append(code, 0x00)          // STOP
	code[7] = byte(len(head) + 21 + 1) // jump target = after beneficiary+SELFDESTRUCT
	addr := stf.Deploy(s.Alice(), code)

	// Fund the contract so the destruct moves real value.
	fund := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(addr), new(big.Int).Mul(big.NewInt(3), eiptest.Ether), nil,
		100_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	eiptest.ExpectSuccess(t, s.Commit(fund), 0)

	bobBefore := s.Balance(destroyTo)
	// Call the contract: same-tx rule means code/storage survive.
	r := stf.Tx(s.Alice(), addr, []byte{0x01})
	_ = r

	if stored := s.StorageAt(addr, types.Hash{}); stored != eiptest.Uint64ToHash(1) {
		t.Fatalf("post-destruct storage slot 0 = %s, want 1 (EIP-6780 keeps storage)", stored.Hex())
	}
	if len(s.Code(addr)) == 0 {
		t.Fatal("post-destruct code is empty (EIP-6780 keeps code for pre-existing contracts)")
	}
	if s.Balance(addr).Sign() != 0 {
		t.Fatalf("post-destruct balance = %s, want 0 (balance still moves)", s.Balance(addr))
	}
	if got := s.Balance(destroyTo); got.Cmp(bobBefore) <= 0 {
		t.Fatalf("beneficiary balance did not increase: %s <= %s", got, bobBefore)
	}
	_ = types.Hash{}
}
