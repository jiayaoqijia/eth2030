package snap

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/eth2030/eth2030/core/rawdb"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/p2p"
	"github.com/eth2030/eth2030/p2p/peermgr"
	"github.com/eth2030/eth2030/p2p/wire"
	"github.com/eth2030/eth2030/rlp"
)

var (
	// ErrRequestTooLarge is returned when a request exceeds protocol limits.
	ErrRequestTooLarge = errors.New("snap: request exceeds size limits")

	// ErrInvalidRange is returned when the requested range is malformed.
	ErrInvalidRange = errors.New("snap: invalid range (origin > limit)")

	// ErrMissingRoot is returned when the requested state root is unknown.
	ErrMissingRoot = errors.New("snap: unknown state root")
)

// StateBackend provides read access to the world state for serving
// snap protocol requests. It abstracts away the underlying trie and
// database implementation.
type StateBackend interface {
	// AccountIterator returns accounts in hash-sorted order starting at
	// the given origin hash under the given state root. The callback is
	// invoked for each account. Return false from the callback to stop.
	AccountIterator(root types.Hash, origin types.Hash, fn func(hash types.Hash, body []byte) bool) error

	// StorageIterator returns storage slots in hash-sorted order for the
	// given account under the given state root, starting at origin.
	StorageIterator(root types.Hash, account types.Hash, origin []byte, fn func(hash types.Hash, body []byte) bool) error

	// Code returns the bytecode for the given code hash.
	Code(hash types.Hash) ([]byte, error)

	// TrieNode returns the trie node at the given path under the given root.
	TrieNode(root types.Hash, path []byte) ([]byte, error)

	// AccountProof returns a Merkle proof for the given account hash
	// under the state root.
	AccountProof(root types.Hash, hash types.Hash) ([][]byte, error)

	// StorageProof returns a Merkle proof for the given slot hash in
	// the given account under the state root.
	StorageProof(root types.Hash, account types.Hash, slot types.Hash) ([][]byte, error)
}

// ServerHandler implements the Handler interface by serving snap protocol
// requests from a StateBackend.
type ServerHandler struct {
	backend StateBackend
	mu      sync.Mutex
	clients map[string]*Client
}

// NewServerHandler creates a new snap protocol server handler.
func NewServerHandler(backend StateBackend) *ServerHandler {
	return &ServerHandler{backend: backend, clients: make(map[string]*Client)}
}

// HandleGetAccountRange iterates accounts in hash-sorted order within the
// [origin, limit] range and returns up to the soft byte limit of account data,
// plus boundary proofs for the range endpoints.
func (h *ServerHandler) HandleGetAccountRange(req *GetAccountRangePacket) (*AccountRangePacket, error) {
	// Validate the range.
	if bytes.Compare(req.Origin[:], req.Limit[:]) > 0 {
		return nil, ErrInvalidRange
	}

	softLimit := req.Bytes
	if softLimit == 0 || softLimit > SoftResponseLimit {
		softLimit = SoftResponseLimit
	}

	var accounts []AccountData
	var totalSize uint64

	err := h.backend.AccountIterator(req.Root, req.Origin, func(hash types.Hash, body []byte) bool {
		// Stop if past the limit hash.
		if len(accounts) > 0 && bytes.Compare(accounts[len(accounts)-1].Hash[:], req.Limit[:]) >= 0 {
			return false
		}
		// Stop if we hit the account count cap.
		if len(accounts) >= MaxAccountRangeCount {
			return false
		}

		accounts = append(accounts, AccountData{
			Hash: hash,
			Body: append([]byte{}, body...),
		})
		totalSize += uint64(types.HashLength + len(body))

		// Stop if we exceed the soft byte limit (but include at least one).
		if totalSize >= softLimit && len(accounts) > 0 {
			return false
		}
		// Hard cap.
		if totalSize >= HardResponseLimit {
			return false
		}
		return true
	})
	if err != nil {
		if errors.Is(err, ErrMissingRoot) {
			return &AccountRangePacket{ID: req.ID}, nil
		}
		return nil, err
	}

	// Generate boundary proofs if we have accounts.
	var proof [][]byte
	{
		// Proof for the first account (origin side).
		originProof, err := h.backend.AccountProof(req.Root, req.Origin)
		if err != nil {
			return nil, err
		}
		proof = append(proof, originProof...)
		// Proof for the last account (limit side), if different from origin.
		if len(accounts) > 0 {
			lastProof, err := h.backend.AccountProof(req.Root, accounts[len(accounts)-1].Hash)
			if err != nil {
				return nil, err
			}
			proof = append(proof, lastProof...)
		}
	}

	return &AccountRangePacket{
		ID:       req.ID,
		Accounts: accounts,
		Proof:    proof,
	}, nil
}

// HandleGetStorageRanges iterates storage slots for the requested accounts
// within the [origin, limit] range.
func (h *ServerHandler) HandleGetStorageRanges(req *GetStorageRangesPacket) (*StorageRangesPacket, error) {
	softLimit := req.Bytes
	if softLimit == 0 || softLimit > SoftResponseLimit {
		softLimit = SoftResponseLimit
	}

	var allSlots [][]StorageData
	var totalSize uint64
	var proof [][]byte

	// Determine origin/limit for the storage hash range.
	if len(req.Origin) > 32 || len(req.Limit) > 32 {
		return nil, ErrInvalidRange
	}
	origin := types.BytesToHash(req.Origin).Bytes()
	limit := types.BytesToHash(req.Limit).Bytes()
	if len(req.Limit) == 0 {
		// Default to full range.
		limit = bytes.Repeat([]byte{0xff}, types.HashLength)
	}
	if bytes.Compare(origin, limit) > 0 {
		return nil, ErrInvalidRange
	}

	for i, account := range req.Accounts {
		if i >= MaxStorageRangeCount {
			break
		}
		if totalSize >= HardResponseLimit {
			break
		}

		var slots []StorageData
		truncated := false

		iterOrigin := origin
		// Only the first account uses the specified origin; subsequent accounts
		// start from the beginning of their storage trie.
		if i > 0 {
			iterOrigin = nil
		}

		err := h.backend.StorageIterator(req.Root, account, iterOrigin, func(hash types.Hash, body []byte) bool {
			// Stop if we are past the limit hash.
			if len(limit) >= types.HashLength && bytes.Compare(hash[:], limit[:types.HashLength]) > 0 {
				truncated = true
				return false
			}
			if len(slots) >= MaxStorageRangeCount {
				truncated = true
				return false
			}

			slots = append(slots, StorageData{
				Hash: hash,
				Body: append([]byte{}, body...),
			})
			totalSize += uint64(types.HashLength + len(body))

			if totalSize >= softLimit {
				return false
			}
			if totalSize >= HardResponseLimit {
				return false
			}
			return true
		})
		if err != nil {
			if errors.Is(err, ErrMissingRoot) {
				return &StorageRangesPacket{ID: req.ID}, nil
			}
			return nil, err
		}

		allSlots = append(allSlots, slots)

		// Prove both the requested origin (including absence) and the last
		// slot. A count-limited response needs the same proof as a byte cap.
		if truncated || totalSize >= softLimit || len(slots) >= MaxStorageRangeCount || len(req.Origin) > 0 {
			originProof, err := h.backend.StorageProof(req.Root, account, types.BytesToHash(iterOrigin))
			if err != nil {
				return nil, err
			}
			proof = append(proof, originProof...)
			if len(slots) > 0 {
				lastSlot := slots[len(slots)-1].Hash
				slotProof, err := h.backend.StorageProof(req.Root, account, lastSlot)
				if err != nil {
					return nil, err
				}
				proof = append(proof, slotProof...)
			}
			break
		}
	}

	return &StorageRangesPacket{
		ID:    req.ID,
		Slots: allSlots,
		Proof: proof,
	}, nil
}

// HandleGetByteCodes retrieves contract bytecodes by code hash, up to
// the soft response size limit.
func (h *ServerHandler) HandleGetByteCodes(req *GetByteCodesPacket) (*ByteCodesPacket, error) {
	softLimit := req.Bytes
	if softLimit == 0 || softLimit > SoftResponseLimit {
		softLimit = SoftResponseLimit
	}

	var codes [][]byte
	var totalSize uint64

	for _, hash := range req.Hashes {
		if len(codes) >= MaxByteCodeCount {
			break
		}
		if totalSize >= HardResponseLimit {
			break
		}

		code, err := h.backend.Code(hash)
		if err != nil {
			// Skip missing bytecodes rather than failing the entire request.
			if errors.Is(err, rawdb.ErrNotFound) {
				continue
			}
			return nil, err
		}

		// Verify the code hash matches.
		computed := crypto.Keccak256Hash(code)
		if computed != hash {
			continue
		}

		codes = append(codes, code)
		totalSize += uint64(len(code))

		if totalSize >= softLimit {
			break
		}
	}

	return &ByteCodesPacket{
		ID:    req.ID,
		Codes: codes,
	}, nil
}

// HandleGetTrieNodes retrieves trie nodes by path under the given state root,
// up to the soft response size limit.
func (h *ServerHandler) HandleGetTrieNodes(req *GetTrieNodesPacket) (*TrieNodesPacket, error) {
	softLimit := req.Bytes
	if softLimit == 0 || softLimit > SoftResponseLimit {
		softLimit = SoftResponseLimit
	}

	var nodes [][]byte
	var totalSize uint64

	for _, pathSet := range req.Paths {
		if len(pathSet) == 0 {
			return nil, ErrInvalidRange
		}
		paths := pathSet
		if len(pathSet) > 1 {
			if len(pathSet[0]) != types.HashLength {
				return nil, ErrInvalidRange
			}
			paths = make([][]byte, 0, len(pathSet)-1)
			for _, storagePath := range pathSet[1:] {
				paths = append(paths, append(append([]byte{}, pathSet[0]...), storagePath...))
			}
		}
		for _, path := range paths {
			if len(nodes) >= MaxTrieNodeCount || totalSize >= softLimit {
				return &TrieNodesPacket{ID: req.ID, Nodes: nodes}, nil
			}
			data, err := h.backend.TrieNode(req.Root, path)
			if err != nil || len(data) == 0 {
				return &TrieNodesPacket{ID: req.ID}, nil
			}
			nodes = append(nodes, data)
			totalSize += uint64(len(data))
		}
	}

	return &TrieNodesPacket{
		ID:    req.ID,
		Nodes: nodes,
	}, nil
}

// Protocol returns a p2p.Protocol descriptor for snap/1. Register it with the
// P2P server's Protocols list to enable snap-sync serving for connected peers.
func (h *ServerHandler) Protocol() p2p.Protocol {
	return p2p.Protocol{
		Name:    ProtocolName,
		Version: ProtocolVersion,
		Length:  8, // message codes 0x00-0x07
		Run:     h.runPeer,
	}
}

// runPeer is the snap/1 message loop invoked by the P2P server for each peer
// that negotiates the snap sub-protocol. It dispatches Get* requests to the
// appropriate Handle* method and writes the response back. The loop exits when
// the transport is closed or an unrecoverable error occurs.
func (h *ServerHandler) runPeer(peer *peermgr.Peer, t wire.Transport) error {
	client := &Client{id: peer.ID(), transport: t, pending: make(map[uint64]pendingResponse), done: make(chan struct{})}
	h.mu.Lock()
	h.clients[peer.ID()] = client
	h.mu.Unlock()
	defer func() {
		close(client.done)
		h.mu.Lock()
		if h.clients[peer.ID()] == client {
			delete(h.clients, peer.ID())
		}
		h.mu.Unlock()
	}()
	for {
		msg, err := t.ReadMsg()
		if err != nil {
			return err
		}
		if msg.Code%2 == 1 && msg.Code <= TrieNodesMsg {
			if err := client.deliver(msg); err != nil {
				return err
			}
			continue
		}
		if err := h.handleSnapMsg(t, msg); err != nil {
			return err
		}
	}
}

// handleSnapMsg decodes a single snap/1 message and writes the response.
func (h *ServerHandler) handleSnapMsg(t wire.Transport, msg wire.Msg) error {
	if len(msg.Payload) > HardResponseLimit {
		return ErrRequestTooLarge
	}
	switch msg.Code {
	case GetAccountRangeMsg:
		var req GetAccountRangePacket
		if err := rlp.DecodeBytes(msg.Payload, &req); err != nil {
			return fmt.Errorf("snap: decode GetAccountRange: %w", err)
		}
		resp, err := h.HandleGetAccountRange(&req)
		if err != nil {
			return fmt.Errorf("snap: GetAccountRange: %w", err)
		}
		return sendSnapMsg(t, AccountRangeMsg, resp)

	case GetStorageRangesMsg:
		var req GetStorageRangesPacket
		if err := rlp.DecodeBytes(msg.Payload, &req); err != nil {
			return fmt.Errorf("snap: decode GetStorageRanges: %w", err)
		}
		resp, err := h.HandleGetStorageRanges(&req)
		if err != nil {
			return fmt.Errorf("snap: GetStorageRanges: %w", err)
		}
		return sendSnapMsg(t, StorageRangesMsg, resp)

	case GetByteCodesMsg:
		var req GetByteCodesPacket
		if err := rlp.DecodeBytes(msg.Payload, &req); err != nil {
			return fmt.Errorf("snap: decode GetByteCodes: %w", err)
		}
		resp, err := h.HandleGetByteCodes(&req)
		if err != nil {
			return fmt.Errorf("snap: GetByteCodes: %w", err)
		}
		return sendSnapMsg(t, ByteCodesMsg, resp)

	case GetTrieNodesMsg:
		var req GetTrieNodesPacket
		if err := rlp.DecodeBytes(msg.Payload, &req); err != nil {
			return fmt.Errorf("snap: decode GetTrieNodes: %w", err)
		}
		resp, err := h.HandleGetTrieNodes(&req)
		if err != nil {
			return fmt.Errorf("snap: GetTrieNodes: %w", err)
		}
		return sendSnapMsg(t, TrieNodesMsg, resp)

	default:
		// Response messages (AccountRangeMsg etc.) or unknown codes: skip.
		return nil
	}
}

// sendSnapMsg RLP-encodes val and writes it as a snap/1 message with the given code.
func sendSnapMsg(t wire.Transport, code uint64, val interface{}) error {
	payload, err := rlp.EncodeToBytes(val)
	if err != nil {
		return fmt.Errorf("snap: encode message 0x%02x: %w", code, err)
	}
	return t.WriteMsg(wire.Msg{
		Code:    code,
		Size:    uint32(len(payload)),
		Payload: payload,
	})
}
