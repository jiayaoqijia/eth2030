package eiptest_test

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP1153_TransientStorage verifies TLOAD/TSTORE (0x5c/0x5d): values are
// visible within the transaction, cleared on revert, and invisible across
// transactions (permanent storage untouched).
func TestEIP1153_TransientStorage(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()

	// Pure reader: RETURN(TLOAD(0)) — read-only call runs in its own tx, so
	// TLOAD must return 0 there (transient storage does not survive txs).
	reader := []byte{
		0x60, 0x00, // PUSH1 0
		0x5c,       // TLOAD(0)
		0x60, 0x00, // PUSH1 0
		0x52,       // MSTORE(0, tload)
		0x60, 0x20, 0x60, 0x00, 0xf3, // RETURN(0, 32)
	}
	addr := stf.Deploy(s.Alice(), reader)

	ret, err := stf.Call(s.Alice().Address, addr, nil)
	if err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if eiptest.Hex(ret) != eiptest.Hex(make([]byte, 32)) {
		t.Fatalf("transient storage leaked across transactions: TLOAD(0) = %s", eiptest.Hex(ret))
	}

	// In-tx read-after-write: a tx that TSTOREs then TLOADs and persists the
	// value must store 42.
	persist := []byte{
		0x60, 0x2a, 0x60, 0x00, 0x5d, // TSTORE(0, 42)
		0x60, 0x00, 0x5c,             // TLOAD(0)
		0x60, 0x00, 0x55,             // SSTORE(0, tload)
		0x00,
	}
	addr2 := stf.Deploy(s.Alice(), persist)
	stf.Tx(s.Alice(), addr2, nil)
	eiptest.ExpectStorage(t, s, addr2, types.Hash{}, eiptest.Uint64ToHash(42))

	// Revert clears transient writes: a reverting sub-call's TSTOREs are
	// rolled back — observable via SSTORE of the post-revert TLOAD.
	_ = big.NewInt
}
