package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/eth2030/eth2030/consensus"
	coreblock "github.com/eth2030/eth2030/core/block"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/pqc"
	"github.com/eth2030/eth2030/engine/payload"
)

func TestNodeNativeConsensusWireLifecycle(t *testing.T) {
	cfg := consensus.NativeConfig{SlotMillis: 60000, SlotsPerEpoch: 32, Validators: make(map[uint64][]byte)}
	keys := make([]*pqc.DilithiumKeyPair, 3)
	for i := range keys {
		var err error
		keys[i], err = pqc.GenerateDilithiumKey()
		if err != nil {
			t.Fatal(err)
		}
		cfg.Validators[uint64(i)] = keys[i].PublicKey
	}
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "native.json")
	data, _ := json.Marshal(cfg)
	if err := os.WriteFile(cfgPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	var genesis map[string]any
	_ = json.Unmarshal(minimalGenesisJSON(1337), &genesis)
	genesisTime := uint64(time.Now().Unix() - 120)
	genesis["timestamp"] = fmt.Sprintf("0x%x", genesisTime)
	data, _ = json.Marshal(genesis)
	genesisPath := filepath.Join(dir, "genesis.json")
	if err := os.WriteFile(genesisPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	aCfg := makeTestConfig(t)
	aCfg.GenesisPath = genesisPath
	aCfg.NativeConsensusFile = cfgPath
	a := newTestNode(t, &aCfg)
	bCfg := makeTestConfig(t)
	bCfg.GenesisPath = genesisPath
	bCfg.NativeConsensusFile = cfgPath
	b := newTestNode(t, &bCfg)
	blocks := []*types.Block{a.blockchain.Genesis()}
	for i := 1; i <= 2; i++ {
		builder := coreblock.NewBlockBuilder(a.blockchain.Config(), a.blockchain, nil)
		beaconRoot := types.Hash{}
		block, _, err := builder.BuildBlock(blocks[len(blocks)-1].Header(), &coreblock.BuildBlockAttributes{Timestamp: genesisTime + uint64(i*60), GasLimit: 30_000_000, BeaconRoot: &beaconRoot, Withdrawals: []*types.Withdrawal{}})
		if err != nil {
			t.Fatal(err)
		}
		if err := a.blockchain.InsertBlock(block); err != nil {
			t.Fatal(err)
		}
		if err := b.blockchain.InsertBlock(block); err != nil {
			t.Fatal(err)
		}
		blocks = append(blocks, block)
	}
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	connectSyncNodes(t, a, b)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		a.nativePeers.mu.Lock()
		ready := len(a.nativePeers.peers) > 0
		a.nativePeers.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := a.engBackend.ForkchoiceUpdated(payload.ForkchoiceStateV1{HeadBlockHash: blocks[2].Hash()}, nil); err == nil {
		t.Fatal("external CL overrode native mode")
	}
	for target := 1; target <= 2; target++ {
		for id := 0; id < 2; id++ {
			vote, err := consensus.SignNativeVote(blocks[0].Hash(), blocks[target-1].Hash(), blocks[target].Hash(), uint64(target-1), uint64(target), uint64(target), uint64(id), keys[id])
			if err != nil {
				t.Fatal(err)
			}
			if err := a.BroadcastNativeVote(context.Background(), vote); err != nil {
				t.Fatal(err)
			}
		}
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, _ := b.NativeConsensusStatus()
		if status.FinalizedRoot == blocks[1].Hash() {
			break
		}
		time.Sleep(time.Millisecond)
	}
	status, enabled := b.NativeConsensusStatus()
	if !enabled || status.FinalizedRoot != blocks[1].Hash() || status.Verified != 4 {
		t.Fatalf("peer votes did not drive native finality: %+v", status)
	}
	if b.blockchain.CurrentFinalBlock().Hash() != blocks[1].Hash() {
		t.Fatal("native finality did not reach execution chain")
	}
	if err := b.Stop(); err != nil {
		t.Fatal(err)
	}
	status, _ = b.NativeConsensusStatus()
	if status.Running {
		t.Fatal("native runtime survived Node.Stop")
	}
	reopened := newTestNode(t, &bCfg)
	restored, _ := reopened.NativeConsensusStatus()
	if restored.FinalizedRoot != blocks[1].Hash() || reopened.blockchain.CurrentBlock().Hash() != blocks[2].Hash() {
		t.Fatal("native restart lost finalized checkpoint or execution head")
	}
}
