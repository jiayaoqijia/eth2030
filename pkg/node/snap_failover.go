package node

import (
	"context"
	"fmt"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/sync/statesync"
)

// snapFailoverPeer switches transport on errors while preserving the scheduler
// and its verified progress. The scheduler uses it from one goroutine.
type snapFailoverPeer struct {
	ctx   context.Context
	peers []statesync.SnapPeer
	next  int
}

func (p *snapFailoverPeer) ID() string { return "verified-snap-failover" }

func trySnapPeers[T any](p *snapFailoverPeer, request func(statesync.SnapPeer) (T, error)) (T, error) {
	var zero T
	var last error
	for tried := 0; tried < len(p.peers); tried++ {
		if err := p.ctx.Err(); err != nil {
			return zero, err
		}
		index := (p.next + tried) % len(p.peers)
		result, err := request(p.peers[index])
		if err == nil {
			p.next = index
			return result, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("snap: no connected peers")
	}
	return zero, last
}

func (p *snapFailoverPeer) RequestAccountRange(req statesync.AccountRangeRequest) (*statesync.AccountRangeResponse, error) {
	return trySnapPeers(p, func(peer statesync.SnapPeer) (*statesync.AccountRangeResponse, error) {
		return peer.RequestAccountRange(req)
	})
}

func (p *snapFailoverPeer) RequestStorageRange(req statesync.StorageRangeRequest) (*statesync.StorageRangeResponse, error) {
	return trySnapPeers(p, func(peer statesync.SnapPeer) (*statesync.StorageRangeResponse, error) {
		return peer.RequestStorageRange(req)
	})
}

func (p *snapFailoverPeer) RequestBytecodes(req statesync.BytecodeRequest) (*statesync.BytecodeResponse, error) {
	return trySnapPeers(p, func(peer statesync.SnapPeer) (*statesync.BytecodeResponse, error) { return peer.RequestBytecodes(req) })
}

func (p *snapFailoverPeer) RequestTrieNodes(root types.Hash, paths [][]byte) ([][]byte, error) {
	return trySnapPeers(p, func(peer statesync.SnapPeer) ([][]byte, error) { return peer.RequestTrieNodes(root, paths) })
}
