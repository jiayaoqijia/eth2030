package eiptest_test

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/eth2030/eth2030/rollup"
)

// The draft leaves the address and ABI open. Exercise the repository's
// rollup execution component until the EVM precompile contract is specified.
func TestEIP8079_ExecuteComponent(t *testing.T) {
	execute := new(rollup.ExecutePrecompile)
	if _, err := execute.Run(nil); !errors.Is(err, rollup.ErrInputTooShort) {
		t.Fatalf("empty input: %v", err)
	}
	input := make([]byte, 53)
	binary.BigEndian.PutUint32(input[40:44], 1)
	input[52] = 0xc0 // empty rollup transaction list
	result, err := execute.Run(input)
	if err != nil || len(result) != 81 || result[80] != 0 {
		t.Fatalf("zero chain ID produced %x: %v", result, err)
	}
	binary.BigEndian.PutUint64(input[0:8], 1)
	result, err = execute.Run(input)
	if err != nil || len(result) != 81 || result[80] != 1 {
		t.Fatalf("valid empty rollup block produced %x: %v", result, err)
	}
}
