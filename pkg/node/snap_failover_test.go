package node

import (
	"context"
	"errors"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/sync/statesync"
)

type failoverTestPeer struct {
	statesync.SnapPeer
	fail            bool
	storageRequests []types.Hash
}

func (p *failoverTestPeer) RequestStorageRange(req statesync.StorageRangeRequest) (*statesync.StorageRangeResponse, error) {
	p.storageRequests = append(p.storageRequests, req.Origin)
	if p.fail {
		return nil, errors.New("disconnected")
	}
	return &statesync.StorageRangeResponse{}, nil
}

func TestSnapFailoverPreservesRequestAndUsesReplacement(t *testing.T) {
	first := &failoverTestPeer{fail: true}
	second := &failoverTestPeer{}
	peer := &snapFailoverPeer{ctx: context.Background(), peers: []statesync.SnapPeer{first, second}}
	origin := types.BytesToHash([]byte{42})
	for i := 0; i < 2; i++ {
		if _, err := peer.RequestStorageRange(statesync.StorageRangeRequest{Origin: origin}); err != nil {
			t.Fatal(err)
		}
	}
	if len(first.storageRequests) != 1 || len(second.storageRequests) != 2 || second.storageRequests[0] != origin || second.storageRequests[1] != origin {
		t.Fatalf("failover lost request or retried failed peer: first=%v second=%v", first.storageRequests, second.storageRequests)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	peer.ctx = ctx
	if _, err := peer.RequestStorageRange(statesync.StorageRangeRequest{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled failover: %v", err)
	}
}
