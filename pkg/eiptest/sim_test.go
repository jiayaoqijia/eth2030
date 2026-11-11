package eiptest_test

import (
	"math/big"
	"strings"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

// TestSimDemo is the framework acceptance test (plan T1.1): genesis → transfer,
// deploy, call in 3 blocks with contract state carried across blocks, using
// only framework API (zero setup code).
func TestSimDemo(t *testing.T) {
	s := eiptest.New(t)

	// Block 1: plain value transfer.
	transfer := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(s.Bob().Address), big.NewInt(5), nil,
		100_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r1 := s.Commit(transfer)
	eiptest.ExpectSuccess(t, r1, 0)
	eiptest.ExpectBalance(t, s, s.Bob().Address, new(big.Int).Add(new(big.Int).Mul(big.NewInt(1000), eiptest.Ether), big.NewInt(5)))

	// Block 2: contract deployment (runtime stores 0x2a in slot 0).
	deploy := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		nil, big.NewInt(0), contractStore42Init,
		200_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r2 := s.Commit(deploy)
	tx2 := eiptest.ExpectSuccess(t, r2, 0)
	contract := tx2.ContractAddress()
	if contract == (types.Address{}) {
		t.Fatal("deployment produced empty contract address")
	}

	// Block 3: call the contract; state continuity across blocks is proven by
	// the storage slot being written by the constructor at block 2.
	call := eiptest.UnsignedDynamicFee(
		s.Alice().Address, s.Config().ChainID, s.Nonce(s.Alice().Address),
		eiptest.ToPtr(contract), big.NewInt(0), []byte{},
		100_000, big.NewInt(2_000_000_000), big.NewInt(1_000_000_000),
	)
	r3 := s.Commit(call)
	eiptest.ExpectSuccess(t, r3, 0)
	eiptest.ExpectStorage(t, s, contract, types.Hash{}, eiptest.Uint64ToHash(42))
	for height := uint64(4); height <= 5; height++ {
		if got := s.Commit().Block.NumberU64(); got != height {
			t.Fatalf("head = %d, want %d", got, height)
		}
		eiptest.ExpectStorage(t, s, contract, types.Hash{}, eiptest.Uint64ToHash(42))
	}

}

// contractStore42Init is hand-written initcode: constructor SSTOREs 42 at slot
// 0, then returns the runtime code (which STOPs).
var contractStore42Init = []byte{
	0x60, 0x2a, // PUSH1 42
	0x60, 0x00, // PUSH1 0
	0x55,       // SSTORE
	0x60, 0x01, // PUSH1 1  (runtime size)
	0x60, 0x0c, // PUSH1 12 (runtime offset)
	0x60, 0x00, // PUSH1 0
	0x39,       // CODECOPY
	0x60, 0x01, // PUSH1 1
	0x60, 0x00, // PUSH1 0
	0xf3, // RETURN
	0x00, // runtime: STOP
}

func TestForkProbe_ExactBoundaryAndStableView(t *testing.T) {
	s := eiptest.New(t, eiptest.WithGenesisTime(100), eiptest.ForkAt(eiptest.ForkGlamsterdan, 18),
		eiptest.ForkAt(eiptest.ForkHogota, 37), eiptest.ForkAt(eiptest.ForkBPO2, 63))
	fork := s.Fork(eiptest.ForkGlamsterdan)
	before := fork.Before()
	at := fork.AtBoundary()
	before.ExpectInactive(t)
	at.ExpectActive(t)
	if before.Time() != 117 || at.Time() != 118 || before.Header().CalldataGasUsed != nil || at.Header().CalldataGasUsed == nil {
		t.Fatalf("incorrect boundary: before=%d, at=%d", before.Time(), at.Time())
	}
	hogota := s.Fork(eiptest.ForkHogota).AtBoundary()
	hogota.ExpectActive(t)
	s.Fork(eiptest.ForkBPO2).AtBoundary().ExpectActive(t)
	if hogota.Time() != 137 || s.CurrentBlock().Time() != 163 {
		t.Fatal("later forks crossed at the wrong timestamp")
	}
}

func TestSim_NegativeValidationLeavesHeadUnchanged(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	head := s.CurrentBlock().Hash()
	tx := eiptest.UnsignedDynamicFee(s.Alice().Address, s.Config().ChainID, 5, eiptest.ToPtr(s.Bob().Address), eiptest.Wei(1), nil, 21_000, bigFeeCap, bigTipCap)
	if err := s.CommitInvalid(tx); err == nil || !strings.Contains(err.Error(), "nonce too high") {
		t.Fatalf("invalid nonce: %v", err)
	}
	if err := s.TryBlock(func(h *types.Header, _ *types.Body) { h.BlockAccessListHash = new(types.Hash) }); err == nil || !strings.Contains(err.Error(), "block access list hash") {
		t.Fatalf("invalid BAL: %v", err)
	}
	if s.CurrentBlock().Hash() != head {
		t.Fatal("negative validation changed head")
	}
}

func TestSTF_ReceiptsLogsAndHistoricalCalls(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	p := s.STF()
	log := p.Deploy(s.Alice(), []byte{0x60, 42, 0x5f, 0x5f, 0xa1, 0x00})
	tx := p.Execute(s.Alice(), log, nil).ExpectStatus(t, types.ReceiptStatusSuccessful).ExpectGas(t, 21_000, 25_000)
	result := s.Chain().GetReceipts(s.CurrentBlock().Hash())
	if len(result) != 1 || len(tx.Logs()) != 1 || tx.Logs()[0].Topics[0] != eiptest.Uint64ToHash(42) {
		t.Fatal("missing log topic or persisted receipt")
	}

	clock := p.Deploy(s.Alice(), []byte{0x42, 0x5f, 0x52, 0x60, 32, 0x5f, 0xf3})
	block := s.CurrentBlock()
	s.Commit()
	out, err := p.CallAtBlock(s.Alice().Address, clock, nil, block.NumberU64())
	if err != nil || new(big.Int).SetBytes(out).Uint64() != block.Time() {
		t.Fatalf("historical TIMESTAMP: %x: %v", out, err)
	}

	writer := p.Deploy(s.Alice(), []byte{0x60, 42, 0x5f, 0x55, 0x00})
	if _, err := p.Call(s.Alice().Address, writer, nil); err != nil {
		t.Fatal(err)
	}
	eiptest.ExpectStorage(t, s, writer, types.Hash{}, types.Hash{})
	reverter := p.Deploy(s.Alice(), []byte{0x5f, 0x5f, 0xfd})
	p.Execute(s.Alice(), reverter, nil).ExpectStatus(t, types.ReceiptStatusFailed)
}
