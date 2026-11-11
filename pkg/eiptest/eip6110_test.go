package eiptest_test

import (
	"bytes"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/eiptest/contracts"
	"github.com/eth2030/eth2030/ssz"
)

func TestEIP6110_DepositRequests(t *testing.T) {
	s := eiptest.New(t, eiptest.WithRequestContracts())
	first, firstRecord := nativeDeposit(s, 0, 0, 0x11)
	second, secondRecord := nativeDeposit(s, 1, 1, 0x22)
	result := s.Commit(first, second)
	eiptest.ExpectSuccess(t, result, 0)
	eiptest.ExpectSuccess(t, result, 1)
	if len(result.Requests) != 1 || result.Requests[0].Type != types.DepositRequestType ||
		!bytes.Equal(result.Requests[0].Data, append(firstRecord, secondRecord...)) {
		t.Fatalf("deposit requests do not match receipt order: %+v", result.Requests)
	}

	empty := s.Commit()
	if len(empty.Requests) != 0 || *empty.Block.Header().RequestsHash != types.EmptyRequestsHash {
		t.Fatal("deposit requests were repeated in the next block")
	}
}

func nativeDeposit(s *eiptest.Sim, nonce, index uint64, tag byte) (*types.Transaction, []byte) {
	deposit := types.DepositRequest{
		Pubkey: [48]byte{tag}, WithdrawalCredentials: types.Hash{1, tag},
		Amount: 32_000_000_000, Signature: [96]byte{tag, 0x42}, Index: index,
	}
	fields := [][32]byte{
		ssz.HashTreeRootBytes48(deposit.Pubkey), deposit.WithdrawalCredentials,
		ssz.HashTreeRootUint64(deposit.Amount), ssz.HashTreeRootBytes96(deposit.Signature),
	}
	root := ssz.HashTreeRootContainer(fields)
	data := contracts.ABISelector("deposit(bytes,bytes,bytes,bytes32)")
	for _, offset := range []uint64{128, 224, 288} {
		data = append(data, contracts.ABIEncodeUint256(offset)...)
	}
	data = append(data, root[:]...)
	for _, field := range [][]byte{deposit.Pubkey[:], deposit.WithdrawalCredentials[:], deposit.Signature[:]} {
		data = append(data, contracts.ABIEncodeUint256(uint64(len(field)))...)
		data = append(data, field...)
		data = append(data, make([]byte, (32-len(field)%32)%32)...)
	}
	value := new(big.Int).Mul(eiptest.Wei(32), eiptest.Ether)
	tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), nonce, eiptest.ToPtr(types.DepositContractAddress), value, data, 1_000_000, bigFeeCap, bigTipCap)
	return tx, deposit.Encode()
}
