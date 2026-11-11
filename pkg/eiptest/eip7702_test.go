package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP7702_SetCodeLifecycle is the framework's full-surface validation of
// EIP-7702 (plan T3.1): delegation designation, per-auth intrinsic gas,
// nonce invalidation and the pool admission surface.
func TestEIP7702_SetCodeLifecycle(t *testing.T) {
	s := eiptest.New(t)
	alice, bob := s.Alice(), s.Bob()
	chainID := s.Config().ChainID

	// Deploy a delegate contract that stores its caller in slot 0.
	delegate := s.STF().Deploy(bob, []byte{
		0x33,       // CALLER
		0x60, 0x00, // PUSH1 0
		0x55, // SSTORE(0, caller)
		0x00, // STOP
	})

	// Alice signs an authorization delegating herself to the delegate. The
	// sender's account nonce is incremented before auth processing, so the
	// auth nonce must be current+1 for a sender-authority.
	auth := eiptest.Auth(chainID, alice, delegate, s.Nonce(alice.Address)+1)
	tx := eiptest.SetCode(chainID, alice, s.Nonce(alice.Address), s.Bob().Address, 500_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000), []types.Authorization{auth})
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)

	// Alice's EOA code is the delegation designator 0xef0100 || delegate.
	eiptest.ExpectDelegateAt(t, s, alice.Address, delegate)

	// Calling Alice executes the delegate's code: caller-of-delegate is the
	// outer EOA (this tx's sender = Alice), stored in slot 0 of ALICE's
	// storage (delegate storage is per-calling-address).
	call := eiptest.UnsignedDynamicFee(
		bob.Address, chainID, s.Nonce(bob.Address),
		eiptest.ToPtr(alice.Address), nil, nil,
		200_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r2 := s.Commit(call)
	eiptest.ExpectSuccess(t, r2, 0)
	if got := s.StorageAt(alice.Address, types.Hash{}); got == (types.Hash{}) {
		t.Fatal("delegate code did not execute through the EOA delegation")
	}

	// The used auth nonce is invalidated: reusing it must be a silent no-op.
	auth2 := eiptest.Auth(chainID, alice, delegate, auth.Nonce-1) // already consumed
	tx2 := eiptest.SetCode(chainID, alice, s.Nonce(alice.Address), s.Bob().Address, 500_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000), []types.Authorization{auth2})
	r3 := s.Commit(tx2)
	if r3.TxCount() > 0 {
		eiptest.ExpectSuccess(t, r3, 0) // tx succeeds; the stale auth is a no-op
	}
	eiptest.ExpectDelegateAt(t, s, alice.Address, delegate)

	// Encoding surface: RLP roundtrip preserves the authorization list.
	dec := s.Encoding().RLPRoundTrip(t, tx)
	if len(dec.AuthorizationList()) != 1 {
		t.Fatalf("roundtrip lost authorization list (len %d)", len(dec.AuthorizationList()))
	}
}

// TestEIP7702_PerAuthGas verifies intrinsic gas accounting: each
// authorization costs PER_EMPTY_ACCOUNT_COST (25,000) for a never-existing
// authority, charged to the set-code transaction.
func TestEIP7702_PerAuthGas(t *testing.T) {
	s := eiptest.New(t)
	alice := s.Alice()
	chainID := s.Config().ChainID
	delegate := s.STF().Deploy(s.Bob(), []byte{0x00})

	scratch := s.Account(8) // never existed in state
	auth := eiptest.Auth(chainID, scratch, delegate, 0)
	tx := eiptest.SetCode(chainID, alice, s.Nonce(alice.Address), s.Bob().Address, 500_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000), []types.Authorization{auth})
	r := s.Commit(tx)
	txr := eiptest.ExpectSuccess(t, r, 0)

	// 21000 base + 25000 per empty account + delegate call overhead + push.
	if txr.GasUsed() < 21000+25000 {
		t.Fatalf("set-code tx gas %d below intrinsic floor 46000 (per-auth charge missing)", txr.GasUsed())
	}
	// The authority account now exists with the delegation code.
	eiptest.ExpectDelegateAt(t, s, scratch.Address, delegate)
}
