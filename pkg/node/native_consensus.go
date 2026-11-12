package node

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/eth2030/eth2030/consensus"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/p2p"
	"github.com/eth2030/eth2030/p2p/peermgr"
	"github.com/eth2030/eth2030/p2p/wire"
)

type nativeConsensusPeers struct {
	mu    sync.Mutex
	peers map[string]wire.Transport
}

func (n *Node) initNativeConsensus() error {
	if n.config.NativeConsensusFile == "" {
		return nil
	}
	data, err := os.ReadFile(n.config.NativeConsensusFile)
	if err != nil {
		return err
	}
	cfg := consensus.NativeConfig{SlotMillis: 6000, SlotsPerEpoch: 32}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}
	if cfg.SlotMillis == 0 {
		return fmt.Errorf("native consensus: zero slot duration")
	}
	genesis := n.blockchain.Genesis()
	checkpoint := n.blockchain.CurrentFinalBlock()
	if checkpoint == nil {
		checkpoint = genesis
	}
	checkpointSlot := (checkpoint.Time() - genesis.Time()) * 1000 / cfg.SlotMillis
	runtime, err := consensus.NewNodeRuntime(cfg, genesis.Hash(), time.Unix(int64(genesis.Time()), 0), checkpoint.Hash(), checkpointSlot, func(uint64) { _ = n.observeNativeHead() }, n.applyNativeFinality)
	if err != nil {
		return err
	}
	n.nativeConsensus = runtime
	n.nativePeers = &nativeConsensusPeers{peers: make(map[string]wire.Transport)}
	return n.observeNativeHead()
}

func (n *Node) observeNativeHead() error {
	if n.nativeConsensus == nil {
		return nil
	}
	finalized := n.nativeConsensus.Status().FinalizedRoot
	var chain []*types.Block
	for block := n.blockchain.CurrentBlock(); block != nil && block.Hash() != finalized; block = n.blockchain.GetBlock(block.ParentHash()) {
		chain = append(chain, block)
		if len(chain) > 8192 {
			return fmt.Errorf("native consensus: history limit")
		}
	}
	for i := len(chain) - 1; i >= 0; i-- {
		block := chain[i]
		if err := n.nativeConsensus.ObserveBlock(block.Hash(), block.ParentHash(), n.nativeConsensus.SlotAt(time.Unix(int64(block.Time()), 0))); err != nil {
			return err
		}
	}
	return nil
}
func (n *Node) applyNativeFinality(finalized, justified types.Hash) error {
	return n.blockchain.ApplyForkchoice(n.blockchain.CurrentBlock().Hash(), justified, finalized)
}
func (a *nodeDepsAdapter) ExternalForkchoiceAllowed() bool { return a.n.nativeConsensus == nil }

// NativeConsensusStatus exposes actual slot, signature and finality state.
func (n *Node) NativeConsensusStatus() (consensus.NativeStatus, bool) {
	if n.nativeConsensus == nil {
		return consensus.NativeStatus{}, false
	}
	return n.nativeConsensus.Status(), true
}

func (n *Node) SubmitNativeVote(vote consensus.NativeVote) error {
	if n.nativeConsensus == nil {
		return fmt.Errorf("native consensus is disabled")
	}
	if err := n.observeNativeHead(); err != nil {
		return err
	}
	return n.nativeConsensus.Submit(vote)
}

// BroadcastNativeVote is the validator-facing ingress. Keys remain outside
// the node; only already signed, registered-validator votes are accepted.
func (n *Node) BroadcastNativeVote(ctx context.Context, vote consensus.NativeVote) error {
	if err := n.SubmitNativeVote(vote); err != nil {
		return err
	}
	data, err := json.Marshal(vote)
	if err != nil {
		return err
	}
	n.nativePeers.mu.Lock()
	peers := make([]wire.Transport, 0, len(n.nativePeers.peers))
	for _, peer := range n.nativePeers.peers {
		peers = append(peers, peer)
	}
	n.nativePeers.mu.Unlock()
	for _, peer := range peers {
		sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		stop := context.AfterFunc(sendCtx, func() { _ = peer.Close() })
		err := peer.WriteMsg(wire.Msg{Code: 0, Size: uint32(len(data)), Payload: data})
		stop()
		cancel()
		if err != nil {
			return err
		}
	}
	return nil
}
func (n *Node) nativeProtocol() p2p.Protocol {
	return p2p.Protocol{Name: "ncs", Version: 1, Length: 1, Run: func(peer *peermgr.Peer, transport wire.Transport) error {
		n.nativePeers.mu.Lock()
		n.nativePeers.peers[peer.ID()] = transport
		n.nativePeers.mu.Unlock()
		defer func() { n.nativePeers.mu.Lock(); delete(n.nativePeers.peers, peer.ID()); n.nativePeers.mu.Unlock() }()
		for {
			msg, err := transport.ReadMsg()
			if err != nil {
				return err
			}
			if msg.Code != 0 || len(msg.Payload) > 16*1024 {
				return fmt.Errorf("native consensus: invalid message")
			}
			var vote consensus.NativeVote
			if err := json.Unmarshal(msg.Payload, &vote); err != nil {
				return err
			}
			if err := n.SubmitNativeVote(vote); err != nil {
				return err
			}
		}
	}}
}
