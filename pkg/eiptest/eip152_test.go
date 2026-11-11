package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP152_Blake2F verifies the BLAKE2F precompile (0x09) against test
// vector 5 of refs/EIPs/EIPS/eip-152.md, plus the vector-3 negative case
// (final block flag 0x02 must be rejected).
func TestEIP152_Blake2F(t *testing.T) {
	s := eiptest.New(t)
	stf := s.STF()
	blake2F := types.BytesToAddress([]byte{0x09})

	input := eiptest.MustHex(t, "0000000c48c9bdf267e6096a3ba7ca8485ae67bb2bf894fe72f36e3cf1361d5f3af54fa5d182e6ad7f520e511f6c3e2b8c68059b6bbd41fbabd9831f79217e1319cde05b61626300000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000300000000000000000000000000000001")
	if len(input) != 213 {
		t.Fatalf("vector input length = %d, want 213", len(input))
	}
	want := eiptest.MustHex(t, "ba80a53f981c4d0d6a2797b69f12f6e94c212f14685ac4b74b12bb6fdbffa2d17d87c5392aab792dc252d5de4533cc9518d38aa8dbf1925ab92386edd4009923")

	ret, err := stf.Call(s.Alice().Address, blake2F, input)
	if err != nil {
		t.Fatalf("blake2f call failed: %v", err)
	}
	if eiptest.Hex(ret) != eiptest.Hex(want) {
		t.Fatalf("blake2f output mismatch:\n got %s\nwant %s", eiptest.Hex(ret), eiptest.Hex(want))
	}

	// Vector 3: final block flag 0x02 must be rejected. Same input with the
	// final byte changed from 0x01 to 0x02.
	bad := make([]byte, len(input))
	copy(bad, input)
	bad[len(bad)-1] = 0x02
	if _, err := stf.Call(s.Alice().Address, blake2F, bad); err == nil {
		t.Fatal("blake2f accepted final flag 0x02 (want rejection)")
	}
}
