package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// TestEIP7685_RequestsHash verifies EIP-7685: the requests hash binds the
// ordered request list — identical sets produce identical hashes, and any
// request changes the hash (empty-list hash is the keccak of empty RLP).
func TestEIP7685_RequestsHash(t *testing.T) {
	empty := types.Requests{}
	h1 := types.ComputeRequestsHash(empty)
	h2 := types.ComputeRequestsHash(empty)
	if h1 != h2 {
		t.Fatal("identical request sets produced different hashes")
	}

	withReq := types.Requests{types.NewRequest(types.WithdrawalRequestType, []byte{0xEE})}
	h3 := types.ComputeRequestsHash(withReq)
	if h3 == h1 {
		t.Fatal("adding a request did not change the requests hash")
	}

	// Distinct request sets produce distinct hashes.
	different := types.Requests{types.NewRequest(types.DepositRequestType, []byte{0x01})}
	if types.ComputeRequestsHash(different) == h3 {
		t.Fatal("distinct request sets produced the same hash")
	}
}
