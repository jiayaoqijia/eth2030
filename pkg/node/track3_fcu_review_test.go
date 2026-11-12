package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	coreblock "github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/engine"
	"github.com/eth2030/eth2030/engine/payload"
	"github.com/eth2030/eth2030/p2p"
)

func TestKnownHeadFCURejectsUnknownCheckpointSynchronously(t *testing.T) {
	cfg := makeTestConfig(t)
	cfg.GenesisPath = filepath.Join(t.TempDir(), "genesis.json")
	if err := os.WriteFile(cfg.GenesisPath, minimalGenesisJSON(1337), 0600); err != nil {
		t.Fatal(err)
	}
	n := newTestNode(t, &cfg)
	parent := n.blockchain.Genesis()
	root := types.Hash{}
	builder := coreblock.NewBlockBuilder(n.blockchain.Config(), n.blockchain, nil)
	block, _, err := builder.BuildBlock(parent.Header(), &coreblock.BuildBlockAttributes{Timestamp: 12, GasLimit: 30_000_000, BeaconRoot: &root, Withdrawals: []*types.Withdrawal{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.blockchain.InsertSyncedBlock(block); err != nil {
		t.Fatal(err)
	}
	headers := n.ethHandler.HandleGetBlockHeaders(p2p.HashOrNumber{Hash: block.Hash()}, 1, 0, false)
	if len(headers) != 1 || headers[0].Hash() != block.Hash() {
		t.Fatal("hash-origin request did not return the noncanonical block")
	}
	parentState, err := n.blockchain.StateAtBlock(block)
	if err != nil {
		t.Fatal(err)
	}
	builder.SetState(parentState)
	child, _, err := builder.BuildBlock(block.Header(), &coreblock.BuildBlockAttributes{Timestamp: 24, GasLimit: 30_000_000, BeaconRoot: &root, Withdrawals: []*types.Withdrawal{}})
	if err != nil {
		t.Fatal(err)
	}
	if err := n.blockchain.InsertSyncedBlock(child); err != nil {
		t.Fatal(err)
	}
	reverse := n.ethHandler.HandleGetBlockHeaders(p2p.HashOrNumber{Hash: child.Hash()}, 2, 0, true)
	if len(reverse) != 2 || reverse[1].Hash() != block.Hash() {
		t.Fatal("reverse hash-origin request switched to canonical ancestry")
	}
	fc := payload.ForkchoiceStateV1{HeadBlockHash: block.Hash(), SafeBlockHash: types.BytesToHash([]byte{0xff})}
	result, err := n.engBackend.ForkchoiceUpdated(fc, nil)
	if !errors.Is(err, engine.ErrInvalidForkchoiceState) {
		t.Fatalf("invalid FCU returned %+v, %v", result, err)
	}
	if n.blockchain.CurrentBlock().Hash() != parent.Hash() {
		t.Fatal("invalid FCU changed canonical head")
	}
}
