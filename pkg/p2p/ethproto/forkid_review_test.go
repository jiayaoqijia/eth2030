package ethproto

import (
	"math/big"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	gethforkid "github.com/ethereum/go-ethereum/core/forkid"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

type forkOracle struct {
	cfg     *params.ChainConfig
	genesis *gethtypes.Block
	head    *gethtypes.Header
}

func (o forkOracle) Config() *params.ChainConfig      { return o.cfg }
func (o forkOracle) Genesis() *gethtypes.Block        { return o.genesis }
func (o forkOracle) CurrentHeader() *gethtypes.Header { return o.head }

func TestTimestampForkIDsMatchGeth(t *testing.T) {
	shanghai, cancun := uint64(120), uint64(150)
	cfg := &params.ChainConfig{HomesteadBlock: big.NewInt(5), EIP150Block: big.NewInt(10), EIP155Block: big.NewInt(10), ShanghaiTime: &shanghai, CancunTime: &cancun}
	genesis := gethtypes.NewBlockWithHeader(&gethtypes.Header{Number: big.NewInt(0), Time: 100})
	checkpoints := [][2]uint64{{0, 100}, {7, 110}, {10, 119}, {20, 121}, {30, 151}}
	for _, head := range checkpoints {
		want := gethforkid.NewID(cfg, genesis, head[0], head[1])
		got := CalcForkIDWithTime(types.Hash(genesis.Hash()), 100, head[0], head[1], []uint64{5, 10, 10}, []uint64{0, 100, 120, 150})
		if got.Hash != want.Hash || got.Next != want.Next {
			t.Fatalf("head %v: got %+v want %+v", head, got, want)
		}
		oracle := gethforkid.NewFilter(forkOracle{cfg, genesis, &gethtypes.Header{Number: new(big.Int).SetUint64(head[0]), Time: head[1]}})
		for _, remote := range checkpoints {
			id := gethforkid.NewID(cfg, genesis, remote[0], remote[1])
			expected := oracle(id)
			actual := ValidateForkID(ForkID{Hash: id.Hash, Next: id.Next}, types.Hash(genesis.Hash()), 100, head[0], head[1], []uint64{5, 10, 10}, []uint64{0, 100, 120, 150})
			if (actual == nil) != (expected == nil) {
				t.Fatalf("filter diverged at local %v remote %v: %v / %v", head, remote, actual, expected)
			}
		}
	}
}
