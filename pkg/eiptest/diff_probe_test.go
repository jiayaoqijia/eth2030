package eiptest_test

import (
	"encoding/json"
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

func mustUnmarshal(t *testing.T, data []byte, v interface{}) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("unmarshal %s: %v", data, err)
	}
}

func snapshotStandardAccounts(t *testing.T, s *eiptest.Sim) map[types.Address]*eiptest.PreAccountView {
	t.Helper()
	return s.SnapshotAccounts(t, s.Alice().Address, s.Bob().Address, s.Charlie().Address,
		s.Dave().Address, s.Erin().Address, s.Faythe().Address)
}

func TestDiffProbe_TransferParity(t *testing.T) {
	s := eiptest.New(t)
	pre := snapshotStandardAccounts(t, s)
	p := s.STF()
	p.Transfer(s.Alice(), s.Bob().Address, eiptest.Wei(123))
	p.Transfer(s.Bob(), s.Charlie().Address, eiptest.Wei(7))
	s.Diff().DiffBlocks(t, pre, s.Block(1), s.Block(2))
}

func TestDiffProbe_ContractParity(t *testing.T) {
	s := eiptest.New(t)
	pre := snapshotStandardAccounts(t, s)
	deploy := eiptest.UnsignedDynamicFee(s.Alice().Address, s.Config().ChainID, 0,
		nil, big.NewInt(0), contractStore42Init, 200_000, bigFeeCap, bigTipCap)
	r := s.Commit(deploy)
	contract := eiptest.ExpectSuccess(t, r, 0).ContractAddress()
	slot := types.Hash{}
	eiptest.ExpectStorage(t, s, contract, slot, eiptest.Uint64ToHash(42))
	snapshot := s.SnapshotAccounts(t, contract)
	if snapshot[contract].Storage[slot] != eiptest.Uint64ToHash(42) {
		t.Fatal("snapshot omitted constructor storage")
	}
	s.Diff().DiffBlocks(t, pre, r.Block)
}

func TestDiffProbe_MultipleStorageWritesInOneBlock(t *testing.T) {
	s := eiptest.New(t)
	pre := snapshotStandardAccounts(t, s)
	init := []byte{0x60, 7, 0x60, 12, 0x60, 0, 0x39, 0x60, 7, 0x60, 0, 0xf3, 0x60, 0, 0x35, 0x60, 0, 0x55, 0}
	deploy := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), 0, nil, big.NewInt(0), init, 200000, bigFeeCap, bigTipCap)
	deployment := s.Commit(deploy)
	contract := eiptest.ExpectSuccess(t, deployment, 0).ContractAddress()
	first, second := make([]byte, 32), make([]byte, 32)
	first[31] = 1
	second[31] = 2
	tx1 := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), 1, &contract, big.NewInt(0), first, 100000, bigFeeCap, bigTipCap)
	tx2 := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), 2, &contract, big.NewInt(0), second, 100000, bigFeeCap, bigTipCap)
	result := s.Commit(tx1, tx2)
	eiptest.ExpectSuccess(t, result, 0)
	eiptest.ExpectSuccess(t, result, 1)
	eiptest.ExpectStorage(t, s, contract, types.Hash{}, eiptest.Uint64ToHash(2))
	// Replay from complete genesis state, including deployment fees and all
	// system-contract effects, instead of supplying a partial post-deploy map.
	s.Diff().DiffBlocks(t, pre, deployment.Block, result.Block)
}
