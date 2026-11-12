package types

import (
	"math/big"
	"testing"
)

func TestHeaderRLPCommitsBAL(t *testing.T) {
	header := &Header{Difficulty: new(big.Int), Number: big.NewInt(1), BaseFee: big.NewInt(1)}
	before := header.Hash()
	header = copyHeader(header)
	bal := HexToHash("0x1234")
	header.BlockAccessListHash = &bal
	encoded, err := header.EncodeRLP()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeHeaderRLP(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.BlockAccessListHash == nil || *decoded.BlockAccessListHash != bal || decoded.Hash() != header.Hash() {
		t.Fatal("BAL commitment lost in header RLP")
	}
	if header.Hash() == before {
		t.Fatal("BAL does not affect block hash")
	}
	changed := HexToHash("0x5678")
	decoded = copyHeader(decoded)
	decoded.BlockAccessListHash = &changed
	if decoded.Hash() == header.Hash() {
		t.Fatal("tampered BAL retains the same block hash")
	}
}
