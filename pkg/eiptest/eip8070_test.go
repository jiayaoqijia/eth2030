package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP8070_SparseBlobpoolAdmission verifies EIP-8070 admission semantics
// observable at the pool surface: blob transactions with well-formed
// version-0x01 hashes are admitted; malformed version bytes are rejected.
func TestEIP8070_SparseBlobpoolAdmission(t *testing.T) {
	s := eiptest.New(t)
	p := s.Pool()
	alice := s.Alice()

	// Well-formed blob hash (0x01 version) → admitted.
	good := eiptest.UnsignedBlob(
		alice.Address, s.Config().ChainID, s.Nonce(alice.Address),
		s.Bob().Address, big.NewInt(1), 100_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000), big.NewInt(1_000_000_000),
		[]types.Hash{eiptest.BlobHash(1)},
	)
	p.ExpectAccepted(t, good)

	// Malformed blob hash version (0x02) → rejected at the pool boundary.
	badHash := eiptest.BlobHash(1)
	badHash[0] = 0x02
	bad := eiptest.UnsignedBlob(
		alice.Address, s.Config().ChainID, s.Nonce(alice.Address),
		s.Bob().Address, big.NewInt(1), 100_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000), big.NewInt(1_000_000_000),
		[]types.Hash{badHash},
	)
	p.ExpectRejected(t, bad, "")
}

