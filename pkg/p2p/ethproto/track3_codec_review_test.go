package ethproto

import (
	"github.com/eth2030/eth2030/core/types"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
	"testing"
)

func TestEmptyWithdrawalsRemainPresentOnWire(t *testing.T) {
	for _, present := range []bool{false, true} {
		body := &BlockBody{}
		if present {
			body.Withdrawals = []*types.Withdrawal{}
		}
		encoded, err := (BlockBodiesPacket{RequestID: 1, Bodies: []*BlockBody{body}}).EncodeRLP()
		if err != nil {
			t.Fatal(err)
		}
		var packet struct {
			ID     uint64
			Bodies [][]gethrlp.RawValue
		}
		if err := gethrlp.DecodeBytes(encoded, &packet); err != nil {
			t.Fatal(err)
		}
		want := 2
		if present {
			want = 3
		}
		if len(packet.Bodies[0]) != want {
			t.Fatalf("withdrawals present=%v: body has %d fields, want %d", present, len(packet.Bodies[0]), want)
		}
		var decoded BlockBodiesPacket
		if err := decoded.DecodeRLP(encoded); err != nil {
			t.Fatal(err)
		}
		if (decoded.Bodies[0].Withdrawals != nil) != present {
			t.Fatal("withdrawals presence changed")
		}
	}
}
