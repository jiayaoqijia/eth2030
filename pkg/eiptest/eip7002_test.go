package eiptest_test

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

func TestEIP7002_WithdrawalRequests(t *testing.T) {
	s := eiptest.New(t, eiptest.WithRequestContracts())
	tx, record := nativeWithdrawal(s, 0)
	result := s.Commit(tx)
	eiptest.ExpectSuccess(t, result, 0)
	if len(result.Requests) != 1 || result.Requests[0].Type != types.WithdrawalRequestType ||
		!bytes.Equal(result.Requests[0].Data, record) {
		t.Fatalf("withdrawal request data mismatch: %+v", result.Requests)
	}
	if len(s.Commit().Requests) != 0 {
		t.Fatal("withdrawal queue was not drained")
	}
}

func nativeWithdrawal(s *eiptest.Sim, nonce uint64) (*types.Transaction, []byte) {
	request := types.WithdrawalRequest{
		SourceAddress: s.Alice().Address, ValidatorPubkey: [48]byte{0x55}, Amount: 0x0102030405,
	}
	var input [56]byte
	copy(input[:48], request.ValidatorPubkey[:])
	binary.BigEndian.PutUint64(input[48:], request.Amount)
	tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), nonce, eiptest.ToPtr(types.WithdrawalRequestAddress), eiptest.Wei(1), input[:], 200_000, bigFeeCap, bigTipCap)
	return tx, request.Encode()
}
