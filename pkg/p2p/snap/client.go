package snap

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/p2p/wire"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/sync/statesync"
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
	gethtrie "github.com/ethereum/go-ethereum/trie"
)

type pendingResponse struct {
	code uint64
	ch   chan wire.Msg
}

// Client is a snap/1 peer with response correlation. Verification is bound to
// the requested pivot root, not to an untrusted server completion flag.
type Client struct {
	id        string
	transport wire.Transport
	seq       atomic.Uint64
	mu        sync.Mutex
	pending   map[uint64]pendingResponse
	done      chan struct{}
}

func (c *Client) ID() string { return c.id }

// WithContext creates a request-scoped adapter sharing the peer connection.
func (c *Client) WithContext(ctx context.Context) *VerifiedPeer {
	return &VerifiedPeer{Client: c, ctx: ctx, storageRoots: make(map[types.Hash]types.Hash)}
}

type VerifiedPeer struct {
	*Client
	ctx          context.Context
	storageRoots map[types.Hash]types.Hash
	stateRoot    types.Hash
	rootSet      bool
}

func (c *Client) request(ctx context.Context, code uint64, packet func(uint64) any, out any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	stopClose := context.AfterFunc(ctx, func() { _ = c.transport.Close() })
	defer stopClose()
	id := c.seq.Add(1)
	ch := make(chan wire.Msg, 1)
	c.mu.Lock()
	c.pending[id] = pendingResponse{code + 1, ch}
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	err := sendSnapMsg(c.transport, code, packet(id))
	stopClose()
	if err != nil {
		return err
	}
	select {
	case <-c.done:
		return fmt.Errorf("snap: peer disconnected")
	case <-ctx.Done():
		return ctx.Err()
	case msg := <-ch:
		return rlp.DecodeBytes(msg.Payload, out)
	}
}

func (c *Client) deliver(msg wire.Msg) error {
	if len(msg.Payload) > HardResponseLimit {
		return ErrRequestTooLarge
	}
	// Every response starts with its request ID.
	content, _, err := gethrlp.SplitList(msg.Payload)
	if err != nil {
		return err
	}
	id, _, err := gethrlp.SplitUint64(content)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if req, ok := c.pending[id]; ok && req.code == msg.Code {
		select {
		case req.ch <- msg:
		default:
		}
	}
	return nil
}

func verifyRange(root, origin types.Hash, keys, values, proof [][]byte) (bool, error) {
	if root == types.EmptyRootHash && len(keys) == 0 && len(proof) == 0 {
		return false, nil
	}
	if len(proof) == 0 {
		return gethtrie.VerifyRangeProof(gethcommon.Hash(root), origin[:], keys, values, nil)
	}
	db := memorydb.New()
	for _, node := range proof {
		if err := db.Put(crypto.Keccak256(node), node); err != nil {
			return false, err
		}
	}
	return gethtrie.VerifyRangeProof(gethcommon.Hash(root), origin[:], keys, values, db)
}

func (p *VerifiedPeer) RequestAccountRange(req statesync.AccountRangeRequest) (*statesync.AccountRangeResponse, error) {
	p.mu.Lock()
	mismatch := p.rootSet && p.stateRoot != req.Root
	p.mu.Unlock()
	if mismatch {
		return nil, fmt.Errorf("snap: peer adapter pivot changed")
	}
	var response AccountRangePacket
	if err := p.request(p.ctx, GetAccountRangeMsg, func(id uint64) any { return &GetAccountRangePacket{id, req.Root, req.Origin, req.Limit, req.Bytes} }, &response); err != nil {
		return nil, err
	}
	out := &statesync.AccountRangeResponse{ID: req.ID, Proof: response.Proof}
	var keys, values [][]byte
	roots := make(map[types.Hash]types.Hash)
	for _, item := range response.Accounts {
		full, err := gethtypes.FullAccountRLP(item.Body)
		if err != nil {
			return nil, err
		}
		acc, err := gethtypes.FullAccount(item.Body)
		if err != nil {
			return nil, err
		}
		keys = append(keys, item.Hash.Bytes())
		values = append(values, full)
		out.Accounts = append(out.Accounts, statesync.AccountData{Hash: item.Hash, Nonce: acc.Nonce, Balance: acc.Balance.ToBig(), Root: types.Hash(acc.Root), CodeHash: types.BytesToHash(acc.CodeHash)})
		roots[item.Hash] = types.Hash(acc.Root)
	}
	more, err := verifyRange(req.Root, req.Origin, keys, values, response.Proof)
	if err != nil {
		return nil, fmt.Errorf("snap: account range proof: %w", err)
	}
	p.mu.Lock()
	p.stateRoot, p.rootSet = req.Root, true
	for key, root := range roots {
		p.storageRoots[key] = root
	}
	p.mu.Unlock()
	out.More = more && (len(keys) == 0 || bytes.Compare(keys[len(keys)-1], req.Limit[:]) < 0)
	return out, nil
}

func (p *VerifiedPeer) RequestStorageRange(req statesync.StorageRangeRequest) (*statesync.StorageRangeResponse, error) {
	if len(req.Accounts) != 1 {
		return nil, fmt.Errorf("snap: verified storage requests require one account")
	}
	account := req.Accounts[0]
	p.mu.Lock()
	root, ok := p.storageRoots[account]
	ok = ok && p.rootSet && p.stateRoot == req.Root
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("snap: storage account not verified")
	}
	var response StorageRangesPacket
	if err := p.request(p.ctx, GetStorageRangesMsg, func(id uint64) any {
		return &GetStorageRangesPacket{id, req.Root, req.Accounts, req.Origin[:], req.Limit[:], req.Bytes}
	}, &response); err != nil {
		return nil, err
	}
	if len(response.Slots) != 1 {
		return nil, fmt.Errorf("snap: missing storage range")
	}
	out := &statesync.StorageRangeResponse{ID: req.ID, Proof: response.Proof}
	var keys, values [][]byte
	for _, slot := range response.Slots[0] {
		keys = append(keys, slot.Hash.Bytes())
		values = append(values, slot.Body)
		out.Slots = append(out.Slots, statesync.StorageData{AccountHash: account, SlotHash: slot.Hash, Value: slot.Body})
	}
	more, err := verifyRange(root, req.Origin, keys, values, response.Proof)
	if err != nil {
		return nil, fmt.Errorf("snap: storage range proof: %w", err)
	}
	out.More = more && (len(keys) == 0 || bytes.Compare(keys[len(keys)-1], req.Limit[:]) < 0)
	return out, nil
}

func (p *VerifiedPeer) RequestBytecodes(req statesync.BytecodeRequest) (*statesync.BytecodeResponse, error) {
	var response ByteCodesPacket
	if err := p.request(p.ctx, GetByteCodesMsg, func(id uint64) any { return &GetByteCodesPacket{id, req.Hashes, SoftResponseLimit} }, &response); err != nil {
		return nil, err
	}
	allowed := make(map[types.Hash]int)
	for i, hash := range req.Hashes {
		allowed[hash] = i
	}
	previous := -1
	out := &statesync.BytecodeResponse{ID: req.ID}
	for _, code := range response.Codes {
		hash := crypto.Keccak256Hash(code)
		index, ok := allowed[hash]
		if !ok || index <= previous {
			return nil, fmt.Errorf("snap: unrequested bytecode")
		}
		delete(allowed, hash)
		previous = index
		out.Codes = append(out.Codes, statesync.BytecodeData{Hash: hash, Code: code})
	}
	return out, nil
}

func (p *VerifiedPeer) RequestTrieNodes(root types.Hash, paths [][]byte) ([][]byte, error) {
	sets := make([]TrieNodePathSet, len(paths))
	for i, path := range paths {
		if len(path) > 32 {
			sets[i] = TrieNodePathSet{path[:32], path[32:]}
		} else {
			sets[i] = TrieNodePathSet{path}
		}
	}
	var response TrieNodesPacket
	err := p.request(p.ctx, GetTrieNodesMsg, func(id uint64) any { return &GetTrieNodesPacket{id, root, sets, SoftResponseLimit} }, &response)
	return response.Nodes, err
}

func (h *ServerHandler) Clients() []*Client {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]*Client, 0, len(h.clients))
	for _, client := range h.clients {
		out = append(out, client)
	}
	return out
}
