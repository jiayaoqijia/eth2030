package eiptest_test

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/eiptest"
)

func TestProbe7805Payload(t *testing.T) {
	s := eiptest.New(t)
	ep := s.Engine()
	tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), s.Nonce(s.Alice().Address),
		eiptest.ToPtr(s.Bob().Address), big.NewInt(3), nil, 100_000,
		big.NewInt(2_000_000_000), big.NewInt(1_000_000_000))
	ep.Submit(tx)
	id, err := ep.FCU()
	fmt.Printf("P fcu id=%x err=%v\n", id, err)
	resp, err := ep.GetPayload(id)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("P payload txs=%d\n", len(resp.ExecutionPayload.Transactions))
	status, err := ep.Process(resp.ExecutionPayload)
	fmt.Printf("P status=%s err=%v valErr=%v\n", status.Status, err, status.ValidationError)
}
