package eth

import (
	"context"
	"fmt"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/p2p"
)

type syncRequestKey struct {
	peer string
	id   uint64
}

type syncResponse struct {
	headers []*types.Header
	bodies  []*p2p.BlockBody
}

type syncRequest struct {
	code     uint64
	response chan syncResponse
}

// deliverSyncResponse correlates both the peer and request ID. Unsolicited or
// late responses cannot satisfy another peer's outstanding request.
func (h *Handler) deliverSyncResponse(peer string, id, code uint64, response syncResponse) {
	h.syncMu.Lock()
	defer h.syncMu.Unlock()
	if req := h.syncRequests[syncRequestKey{peer, id}]; req != nil && req.code == code {
		select {
		case req.response <- response:
		default:
		}
	}
}

func (h *Handler) syncRequest(ctx context.Context, ep *EthPeer, code uint64, makePacket func(uint64) any) (syncResponse, error) {
	stopClose := context.AfterFunc(ctx, func() { _ = ep.transport.Close() })
	defer stopClose()
	id := ep.nextRequestID()
	key := syncRequestKey{ep.ID(), id}
	req := &syncRequest{code: code + 1, response: make(chan syncResponse, 1)}
	h.syncMu.Lock()
	if h.syncRequests == nil {
		h.syncRequests = make(map[syncRequestKey]*syncRequest)
	}
	h.syncRequests[key] = req
	h.syncMu.Unlock()
	defer func() { h.syncMu.Lock(); delete(h.syncRequests, key); h.syncMu.Unlock() }()
	if err := ep.sendMessage(code, makePacket(id)); err != nil {
		return syncResponse{}, err
	}
	select {
	case <-ctx.Done():
		return syncResponse{}, ctx.Err()
	case response := <-req.response:
		return response, nil
	}
}

// FetchBlock obtains the requested hash over eth, never from a peer's claimed
// block number. The caller must still execute/validate the returned block.
func (h *Handler) FetchBlock(ctx context.Context, hash types.Hash) (*types.Block, error) {
	h.ethPeersMu.RLock()
	peers := make([]*EthPeer, 0, len(h.ethPeers))
	for _, peer := range h.ethPeers {
		peers = append(peers, peer)
	}
	h.ethPeersMu.RUnlock()
	lastErr := fmt.Errorf("eth: no peers for block %s", hash)
	for _, ep := range peers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		requestCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		blk, err := h.fetchPeerBlock(requestCtx, ep, hash)
		cancel()
		if err == nil {
			return blk, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

func (h *Handler) fetchPeerBlock(ctx context.Context, ep *EthPeer, hash types.Hash) (*types.Block, error) {
	headers, err := h.syncRequest(ctx, ep, p2p.GetBlockHeadersMsg, func(id uint64) any {
		return &p2p.GetBlockHeadersPacket{RequestID: id, Request: p2p.GetBlockHeadersRequest{Origin: p2p.HashOrNumber{Hash: hash}, Amount: 1}}
	})
	if err != nil {
		return nil, err
	}
	if len(headers.headers) != 1 || headers.headers[0] == nil || headers.headers[0].Number == nil || !headers.headers[0].Number.IsUint64() || headers.headers[0].Hash() != hash {
		return nil, fmt.Errorf("eth: incorrect header for %s", hash)
	}
	bodies, err := h.syncRequest(ctx, ep, p2p.GetBlockBodiesMsg, func(id uint64) any {
		return &p2p.GetBlockBodiesPacket{RequestID: id, Hashes: p2p.GetBlockBodiesRequest{hash}}
	})
	if err != nil {
		return nil, err
	}
	if len(bodies.bodies) != 1 || bodies.bodies[0] == nil {
		return nil, fmt.Errorf("eth: missing body for %s", hash)
	}
	body := bodies.bodies[0]
	return types.NewBlock(headers.headers[0], &types.Body{Transactions: body.Transactions, Uncles: body.Uncles, Withdrawals: body.Withdrawals}), nil
}
