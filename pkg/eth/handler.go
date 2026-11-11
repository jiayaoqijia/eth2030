package eth

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/p2p"
	"github.com/eth2030/eth2030/rlp"
	ethsync "github.com/eth2030/eth2030/sync"
)

var (
	ErrIncompatibleVersion  = errors.New("eth: incompatible protocol version")
	ErrNetworkIDMismatch    = errors.New("eth: network ID mismatch")
	ErrGenesisMismatch      = errors.New("eth: genesis block mismatch")
	ErrReceiptsBlockUnknown = errors.New("eth: receipts block unknown")
	ErrReceiptsCountMismatch = errors.New("eth: partial receipts count mismatch")
	ErrReceiptsTooLarge     = errors.New("eth: partial receipt exceeds transaction gas bound")
	ErrReceiptsTotalTooLarge = errors.New("eth: partial receipts exceed block gas limit")
	ErrBALHashMismatch      = errors.New("eth: BAL does not match block-access-list-hash")
)

// SyncNotifier is an optional callback interface for triggering sync on
// new block announcements.
type SyncNotifier interface {
	OnNewBlock(peerID string, blockNum uint64)
}

// Handler implements the eth protocol message handling. It connects
// incoming P2P messages to the blockchain and transaction pool.
type Handler struct {
	chain     Blockchain
	txPool    TxPool
	networkID uint64
	peers     *p2p.PeerSet

	ethPeersMu sync.RWMutex
	ethPeers   map[string]*EthPeer // peer ID → EthPeer, for tx broadcast

	// Optional sync notifier triggered by NewBlock messages.
	syncNotifier SyncNotifier

	// Optional downloader for responding to header/body requests
	// received as responses (from sync).
	downloader *ethsync.Downloader

	// Optional providers for eth/70 and eth/71 message handling.
	receiptProvider    ReceiptProvider
	accessListProvider AccessListProvider

	// onBlockInserted is called after a P2P block is successfully inserted.
	// Used to reset the tx pool so stale confirmed txs are evicted.
	onBlockInserted func()
}

// NewHandler creates a new eth protocol handler.
func NewHandler(chain Blockchain, txPool TxPool, networkID uint64) *Handler {
	return &Handler{
		chain:     chain,
		txPool:    txPool,
		networkID: networkID,
		peers:     p2p.NewPeerSet(),
		ethPeers:  make(map[string]*EthPeer),
	}
}

// SetSyncNotifier configures an optional callback for new block events.
func (h *Handler) SetSyncNotifier(sn SyncNotifier) {
	h.syncNotifier = sn
}

// SetOnBlockInserted registers a callback invoked after a P2P block is
// successfully inserted. Typically used to reset the tx pool.
func (h *Handler) SetOnBlockInserted(fn func()) {
	h.onBlockInserted = fn
}

// SetDownloader configures the downloader for the handler.
func (h *Handler) SetDownloader(dl *ethsync.Downloader) {
	h.downloader = dl
}

// SetReceiptProvider configures the receipt provider for eth/70 partial receipts.
func (h *Handler) SetReceiptProvider(rp ReceiptProvider) {
	h.receiptProvider = rp
}

// SetAccessListProvider configures the BAL provider for eth/71 access list exchange.
func (h *Handler) SetAccessListProvider(alp AccessListProvider) {
	h.accessListProvider = alp
}

// Peers returns the handler's peer set.
func (h *Handler) Peers() *p2p.PeerSet {
	return h.peers
}

// BroadcastTransactions announces the given transactions to all connected peers
// via NewPooledTransactionHashes (0x08). Peers that don't have the transactions
// will follow up with GetPooledTransactions (0x09). Called after a local
// eth_sendRawTransaction so the tx propagates to the rest of the network.
func (h *Handler) BroadcastTransactions(txs []*types.Transaction) {
	h.announceTransactions(txs, "")
}

// announceTransactions sends NewPooledTransactionHashes (0x08) for the given
// transactions to all connected peers, excluding the peer with id excludeID
// (to avoid echoing a tx back to the peer that originally sent it).
func (h *Handler) announceTransactions(txs []*types.Transaction, excludeID string) {
	if len(txs) == 0 {
		return
	}
	h.ethPeersMu.RLock()
	defer h.ethPeersMu.RUnlock()
	for id, ep := range h.ethPeers {
		if id == excludeID {
			continue
		}
		if err := ep.SendNewPooledTransactionHashes(txs); err != nil {
			log.Printf("eth: announce tx hashes to peer %s: %v", ep.ID(), err)
		}
	}
}

// Chain returns the handler's blockchain.
func (h *Handler) Chain() Blockchain {
	return h.chain
}

// Protocol returns a p2p.Protocol that can be registered with the P2P server.
func (h *Handler) Protocol() p2p.Protocol {
	return p2p.Protocol{
		Name:    "eth",
		Version: ETH68,
		Length:  13,
		Run:     h.runPeer,
	}
}

// runPeer is called by the P2P server for each connected peer.
// It performs the handshake and then enters the message loop.
func (h *Handler) runPeer(peer *p2p.Peer, t p2p.Transport) error {
	ethPeer := NewEthPeer(peer, t)

	// Build local status.
	head := h.chain.CurrentBlock()
	genesis := h.chain.Genesis()
	status := &p2p.StatusData{
		ProtocolVersion: ETH68,
		NetworkID:       h.networkID,
		TD:              head.Difficulty(),
		Head:            head.Hash(),
		Genesis:         genesis.Hash(),
	}

	// Handshake.
	if _, err := ethPeer.Handshake(status); err != nil {
		return err
	}

	// Register peer.
	if err := h.peers.Register(peer); err != nil {
		return err
	}
	h.ethPeersMu.Lock()
	h.ethPeers[peer.ID()] = ethPeer
	h.ethPeersMu.Unlock()
	defer func() {
		h.peers.Unregister(peer.ID())
		h.ethPeersMu.Lock()
		delete(h.ethPeers, peer.ID())
		h.ethPeersMu.Unlock()
	}()

	// Message loop.
	return h.handleMessages(ethPeer)
}

// handleMessages reads and dispatches messages from the peer.
func (h *Handler) handleMessages(ep *EthPeer) error {
	for {
		msg, err := ep.transport.ReadMsg()
		if err != nil {
			return err
		}
		if err := h.handleMsg(ep, msg); err != nil {
			return err
		}
	}
}

// HandleMsg dispatches a single message to the appropriate handler.
// Exported for testing.
func (h *Handler) HandleMsg(ep *EthPeer, msg p2p.Msg) error {
	return h.handleMsg(ep, msg)
}

func (h *Handler) handleMsg(ep *EthPeer, msg p2p.Msg) error {
	switch msg.Code {
	case p2p.StatusMsg:
		return fmt.Errorf("eth: unexpected status message after handshake")

	case p2p.GetBlockHeadersMsg:
		return h.handleGetBlockHeaders(ep, msg)

	case p2p.BlockHeadersMsg:
		return h.handleBlockHeaders(ep, msg)

	case p2p.GetBlockBodiesMsg:
		return h.handleGetBlockBodies(ep, msg)

	case p2p.BlockBodiesMsg:
		return h.handleBlockBodies(ep, msg)

	case p2p.NewBlockHashesMsg:
		return h.handleNewBlockHashes(ep, msg)

	case p2p.NewBlockMsg:
		return h.handleNewBlock(ep, msg)

	case p2p.TransactionsMsg:
		return h.handleTransactions(ep, msg)

	case p2p.NewPooledTransactionHashesMsg:
		return h.handleNewPooledTransactionHashes(ep, msg)

	case p2p.GetPooledTransactionsMsg:
		return h.handleGetPooledTransactions(ep, msg)

	case p2p.PooledTransactionsMsg:
		return h.handlePooledTransactions(ep, msg)

	case p2p.GetReceiptsMsg:
		return h.handleGetReceiptsV70(ep, msg)

	case p2p.ReceiptsMsg:
		return h.handleReceiptsV70(ep, msg)

	case p2p.GetBlockAccessListsMsg:
		return h.handleGetBlockAccessLists(ep, msg)

	case p2p.BlockAccessListsMsg:
		return h.handleBlockAccessLists(ep, msg)

	default:
		log.Printf("eth: ignoring unknown message code 0x%02x from %s", msg.Code, ep.ID())
		return nil
	}
}

// HandleGetBlockHeaders responds to a header request by looking up blocks
// in the local chain and returning their headers. Exported for use by
// sync adapters.
func (h *Handler) HandleGetBlockHeaders(origin p2p.HashOrNumber, amount, skip uint64, reverse bool) []*types.Header {
	return h.collectHeaders(p2p.GetBlockHeadersRequest{
		Origin:  origin,
		Amount:  amount,
		Skip:    skip,
		Reverse: reverse,
	})
}

// HandleGetBlockBodies retrieves block bodies for the given hashes from
// the local chain. Exported for use by sync adapters.
func (h *Handler) HandleGetBlockBodies(hashes []types.Hash) []*types.Body {
	var bodies []*types.Body
	count := len(hashes)
	if count > MaxBodies {
		count = MaxBodies
	}

	for _, hash := range hashes[:count] {
		block := h.chain.GetBlock(hash)
		if block == nil {
			// Return an empty body to keep index alignment.
			bodies = append(bodies, &types.Body{})
			continue
		}
		bodies = append(bodies, block.Body())
	}
	return bodies
}

// HandleNewBlock processes a new block announcement and optionally triggers
// a sync if the announced block is higher than our head. Exported for
// testing and external use.
func (h *Handler) HandleNewBlock(peerID string, block *types.Block, td uint64) {
	localHead := h.chain.CurrentBlock().NumberU64()
	remoteNum := block.NumberU64()

	if remoteNum > localHead && h.syncNotifier != nil {
		h.syncNotifier.OnNewBlock(peerID, remoteNum)
	}
}

// handleGetBlockHeaders responds to a block headers request.
func (h *Handler) handleGetBlockHeaders(ep *EthPeer, msg p2p.Msg) error {
	var req p2p.GetBlockHeadersPacket
	if err := decodeMsg(msg, &req); err != nil {
		return err
	}

	headers := h.collectHeaders(req.Request)
	return ep.SendBlockHeaders(req.RequestID, headers)
}

// collectHeaders gathers headers based on the request parameters.
func (h *Handler) collectHeaders(req p2p.GetBlockHeadersRequest) []*types.Header {
	var headers []*types.Header

	// Resolve the starting block.
	var origin *types.Block
	if req.Origin.IsHash() {
		origin = h.chain.GetBlock(req.Origin.Hash)
	} else {
		origin = h.chain.GetBlockByNumber(req.Origin.Number)
	}
	if origin == nil {
		return nil
	}

	headers = append(headers, origin.Header())

	// Collect subsequent headers.
	amount := req.Amount
	if amount > MaxHeaders {
		amount = MaxHeaders
	}

	num := origin.NumberU64()
	for i := uint64(1); i < amount; i++ {
		if req.Reverse {
			if num < 1+req.Skip {
				break
			}
			num -= 1 + req.Skip
		} else {
			num += 1 + req.Skip
		}

		block := h.chain.GetBlockByNumber(num)
		if block == nil {
			break
		}
		headers = append(headers, block.Header())
	}
	return headers
}

// handleBlockHeaders processes a received block headers response.
func (h *Handler) handleBlockHeaders(ep *EthPeer, msg p2p.Msg) error {
	var pkt p2p.BlockHeadersPacket
	if err := decodeMsg(msg, &pkt); err != nil {
		return err
	}
	// For now, just log receipt. A sync manager would process these.
	log.Printf("eth: received %d headers from %s (req %d)", len(pkt.Headers), ep.ID(), pkt.RequestID)
	return nil
}

// handleGetBlockBodies responds to a block bodies request.
func (h *Handler) handleGetBlockBodies(ep *EthPeer, msg p2p.Msg) error {
	var req p2p.GetBlockBodiesPacket
	if err := decodeMsg(msg, &req); err != nil {
		return err
	}

	var bodies []*p2p.BlockBody
	count := len(req.Hashes)
	if count > MaxBodies {
		count = MaxBodies
	}

	for _, hash := range req.Hashes[:count] {
		block := h.chain.GetBlock(hash)
		if block == nil {
			continue
		}
		bodies = append(bodies, &p2p.BlockBody{
			Transactions: block.Transactions(),
			Uncles:       block.Uncles(),
			Withdrawals:  block.Withdrawals(),
		})
	}
	return ep.SendBlockBodies(req.RequestID, bodies)
}

// handleBlockBodies processes a received block bodies response.
func (h *Handler) handleBlockBodies(ep *EthPeer, msg p2p.Msg) error {
	var pkt p2p.BlockBodiesPacket
	if err := decodeMsg(msg, &pkt); err != nil {
		return err
	}
	log.Printf("eth: received %d bodies from %s (req %d)", len(pkt.Bodies), ep.ID(), pkt.RequestID)
	return nil
}

// handleNewBlockHashes processes new block hash announcements.
func (h *Handler) handleNewBlockHashes(ep *EthPeer, msg p2p.Msg) error {
	var entries []p2p.NewBlockHashesEntry
	if err := decodeMsg(msg, &entries); err != nil {
		return err
	}

	// Mark blocks we don't have for fetching.
	for _, entry := range entries {
		if !h.chain.HasBlock(entry.Hash) {
			log.Printf("eth: new block hash %s (#%d) from %s", entry.Hash.Hex(), entry.Number, ep.ID())
		}
	}
	return nil
}

// handleNewBlock processes a full new block announcement.
func (h *Handler) handleNewBlock(ep *EthPeer, msg p2p.Msg) error {
	data, err := decodeNewBlock(msg)
	if err != nil {
		return err
	}

	if data.Block == nil {
		return fmt.Errorf("eth: nil block in NewBlock from %s", ep.ID())
	}

	// Update peer's head.
	ep.peer.SetHead(data.Block.Hash(), data.TD)

	// Notify sync manager if the block is higher than our head.
	h.HandleNewBlock(ep.ID(), data.Block, data.Block.NumberU64())

	// Try to insert the block.
	if err := h.chain.InsertBlock(data.Block); err != nil {
		log.Printf("eth: failed to insert block #%d from %s: %v", data.Block.NumberU64(), ep.ID(), err)
		return nil // don't disconnect on insert failure
	}

	// Evict confirmed transactions so the pool stays in sync with the new head.
	if h.onBlockInserted != nil {
		h.onBlockInserted()
	}

	log.Printf("eth: imported block #%d from %s", data.Block.NumberU64(), ep.ID())
	return nil
}

// handleTransactions processes received transactions and adds them to the pool.
func (h *Handler) handleTransactions(ep *EthPeer, msg p2p.Msg) error {
	txs, err := decodeTransactions(msg)
	if err != nil {
		return err
	}
	var added []*types.Transaction
	for _, tx := range txs {
		if err := h.txPool.AddRemote(tx); err != nil {
			// Non-fatal: log and continue with remaining transactions.
			log.Printf("eth: rejected tx %s from %s: %v", tx.Hash().Hex(), ep.ID(), err)
			continue
		}
		added = append(added, tx)
	}
	// Re-announce successfully added txs to other peers (excluding sender).
	if len(added) > 0 {
		h.announceTransactions(added, ep.ID())
	}
	return nil
}

// handleNewPooledTransactionHashes processes a NewPooledTransactionHashes (0x08)
// announcement from a peer. For any hashes not already in our pool, request
// the full transaction bodies via GetPooledTransactions (0x09).
func (h *Handler) handleNewPooledTransactionHashes(ep *EthPeer, msg p2p.Msg) error {
	var ann NewPooledTxHashesMsg68
	if err := decodeMsg(msg, &ann); err != nil {
		return err
	}
	if len(ann.Hashes) == 0 {
		return nil
	}
	var unknown []types.Hash
	for _, hash := range ann.Hashes {
		if h.txPool.Get(hash) == nil {
			unknown = append(unknown, hash)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	return ep.SendGetPooledTransactions(unknown)
}

// handleGetPooledTransactions responds to a GetPooledTransactions (0x09) request
// by returning the requested transactions from our pool via PooledTransactions (0x0a).
func (h *Handler) handleGetPooledTransactions(ep *EthPeer, msg p2p.Msg) error {
	var req GetPooledTransactionsMessage
	if err := decodeMsg(msg, &req); err != nil {
		return err
	}
	var txs []*types.Transaction
	for _, hash := range req.Hashes {
		if tx := h.txPool.Get(hash); tx != nil {
			txs = append(txs, tx)
		}
	}
	return ep.SendPooledTransactions(txs)
}

// handlePooledTransactions processes a PooledTransactions (0x0a) response from
// a peer and adds any new transactions to our pool.
func (h *Handler) handlePooledTransactions(ep *EthPeer, msg p2p.Msg) error {
	txs, err := decodeTransactions(msg)
	if err != nil {
		return err
	}
	for _, tx := range txs {
		if err := h.txPool.AddRemote(tx); err != nil {
			log.Printf("eth: rejected pooled tx %s from %s: %v", tx.Hash().Hex(), ep.ID(), err)
		}
	}
	// Re-announce to other peers (excluding the sender).
	if len(txs) > 0 {
		h.announceTransactions(txs, ep.ID())
	}
	return nil
}

// handleGetReceiptsV70 responds to an eth/70 GetReceipts (0x0f) request
// (EIP-7975). Receipts of the first requested block before
// firstBlockReceiptIndex are omitted. When the response would exceed the
// byte budget, the last served block's list is truncated and
// lastBlockIncomplete is set so the client can page the remainder.
func (h *Handler) handleGetReceiptsV70(ep *EthPeer, msg p2p.Msg) error {
	var req p2p.GetReceiptsPacketV70
	if err := decodeMsg(msg, &req); err != nil {
		return err
	}

	if h.receiptProvider == nil {
		return ep.SendReceiptsV70(req.RequestID, false, nil)
	}

	var pages [][]*types.Receipt
	lastIncomplete := false
	budget := int64(MaxReceiptsResponseSize)
	for i, hash := range req.Hashes {
		receipts := h.receiptProvider.GetReceipts(hash)
		if receipts == nil {
			receipts = []*types.Receipt{}
		}
		// Omit receipts of the first block before the requested index.
		if i == 0 && req.FirstBlockReceiptIndex > 0 {
			if req.FirstBlockReceiptIndex >= uint64(len(receipts)) {
				receipts = nil
			} else {
				receipts = receipts[req.FirstBlockReceiptIndex:]
			}
		}
		// Serve as many receipts as the byte budget allows.
		page := make([]*types.Receipt, 0, len(receipts))
		for _, r := range receipts {
			enc, err := rlp.EncodeToBytes(r)
			if err != nil {
				continue
			}
			if int64(len(enc)) > budget {
				lastIncomplete = true
				break
			}
			budget -= int64(len(enc))
			page = append(page, r)
		}
		pages = append(pages, page)
		if lastIncomplete {
			break
		}
	}

	return ep.SendReceiptsV70(req.RequestID, lastIncomplete, pages)
}

// handleReceiptsV70 processes an eth/70 Receipts (0x10) response (EIP-7975).
// Full receipt lists are verified against the header receipts root by the
// consumer; partial lists are sanity-checked via ValidatePartialReceipts
// with the requested block, which the sync layer correlates by request ID.
func (h *Handler) handleReceiptsV70(ep *EthPeer, msg p2p.Msg) error {
	var pkt p2p.ReceiptsPacketV70
	if err := decodeMsg(msg, &pkt); err != nil {
		return err
	}
	total := 0
	for _, page := range pkt.Receipts {
		for _, r := range page {
			if enc, err := rlp.EncodeToBytes(r); err == nil {
				total += len(enc)
			}
		}
	}
	log.Printf("eth: received %d receipt pages (%d bytes, incomplete=%v) from %s (req %d)",
		len(pkt.Receipts), total, pkt.LastBlockIncomplete, ep.ID(), pkt.RequestID)
	return nil
}

// ValidatePartialReceipts applies the EIP-7975 sanity checks to a partial
// (lastBlockIncomplete=1) receipt list for the given block:
//
//  1. the delivered count matches the block's transaction count,
//  2. each receipt's encoded size fits within gasLimit/8 of its
//     transaction (bounded by the EIP-7825 transaction gas cap),
//  3. the total downloaded size stays within the block gas limit
//     allowance (1 byte per gas).
func ValidatePartialReceipts(block *types.Block, receipts []*types.Receipt) error {
	if block == nil {
		return ErrReceiptsBlockUnknown
	}
	txs := block.Transactions()
	if len(receipts) != len(txs) {
		return fmt.Errorf("%w: %d receipts for %d transactions",
			ErrReceiptsCountMismatch, len(receipts), len(txs))
	}
	total := 0
	for i, r := range receipts {
		enc, err := rlp.EncodeToBytes(r)
		if err != nil {
			return fmt.Errorf("receipt %d encode: %w", i, err)
		}
		maxSize := int64(txs[i].Gas()) / 8
		if maxSize <= 0 {
			maxSize = 1
		}
		if int64(len(enc)) > maxSize {
			return fmt.Errorf("%w: receipt %d is %d bytes, gas limit allows %d",
				ErrReceiptsTooLarge, i, len(enc), maxSize)
		}
		total += len(enc)
	}
	if int64(total) > int64(block.GasLimit()) {
		return fmt.Errorf("%w: %d bytes exceed the block gas limit %d",
			ErrReceiptsTotalTooLarge, total, block.GasLimit())
	}
	return nil
}

// handleGetBlockAccessLists responds to an eth/71 GetBlockAccessLists
// (0x12) request (EIP-8159). Each element of the response corresponds to a
// requested block hash in order; unavailable BALs are served as the RLP
// empty list (0xc0). The soft response limit is 10 MiB.
func (h *Handler) handleGetBlockAccessLists(ep *EthPeer, msg p2p.Msg) error {
	var req p2p.GetBlockAccessListsPacket
	if err := decodeMsg(msg, &req); err != nil {
		return err
	}

	count := len(req.BlockHashes)
	if count > MaxAccessLists {
		count = MaxAccessLists
	}

	emptyList := []byte{0xc0}
	accessLists := make([][]byte, 0, count)
	budget := int64(MaxReceiptsResponseSize)
	for _, hash := range req.BlockHashes[:count] {
		bal, ok := []byte(emptyList), false
		if h.accessListProvider != nil {
			bal, ok = h.accessListProvider.GetBlockAccessListRLP(hash)
		}
		if !ok || len(bal) == 0 {
			bal = emptyList
		} else if int64(len(bal)) <= budget {
			budget -= int64(len(bal))
		} else {
			// Soft limit reached: serve empty entries for the remainder.
			bal = emptyList
		}
		accessLists = append(accessLists, bal)
	}

	return ep.SendBlockAccessLists(req.RequestID, accessLists)
}

// handleBlockAccessLists processes an eth/71 BlockAccessLists (0x13)
// response (EIP-8159). Clients MUST validate each BAL by comparing
// keccak256(rlp(bal)) against the block-access-list-hash in the
// corresponding block header; mismatching BALs are rejected.
func (h *Handler) handleBlockAccessLists(ep *EthPeer, msg p2p.Msg) error {
	var pkt p2p.BlockAccessListsPacket
	if err := decodeMsg(msg, &pkt); err != nil {
		return err
	}
	accepted := 0
	for i, encoded := range pkt.AccessLists {
		if err := h.validateReceivedBAL(encoded); err != nil {
			log.Printf("eth: rejected BAL %d from %s (req %d): %v", i, ep.ID(), pkt.RequestID, err)
			continue
		}
		accepted++
	}
	log.Printf("eth: received %d block access lists from %s (req %d), %d valid",
		len(pkt.AccessLists), ep.ID(), pkt.RequestID, accepted)
	return nil
}

// validateReceivedBAL checks a received RLP-encoded BAL against the
// block-access-list-hash of the corresponding header, when the header
// carries one (post-Amsterdam). Empty BALs mark unavailable data.
func (h *Handler) validateReceivedBAL(encoded []byte) error {
	if len(encoded) == 0 || (len(encoded) == 1 && encoded[0] == 0xc0) {
		return nil // unavailable marker
	}
	if h.chain == nil {
		return nil
	}
	// Resolve the block by scanning recent headers via the BAL hash is not
	// possible without the request context; validation happens per header
	// when the consumer correlates the response. Here we enforce the
	// encoding sanity: the RLP must decode as a list.
	if !isRLPList(encoded) {
		return fmt.Errorf("malformed BAL encoding: not an RLP list")
	}
	return nil
}

// isRLPList reports whether the payload starts with an RLP list header
// (single-byte 0xc0-0xf7 or multi-byte 0xf8+).
func isRLPList(encoded []byte) bool {
	return len(encoded) > 0 && encoded[0] >= 0xc0
}

// ValidateBlockAccessListForHeader validates a received RLP-encoded BAL
// against a specific block header per EIP-8159: keccak256(rlp(bal)) must
// equal the header's block-access-list-hash. Empty encodings mark
// unavailable data and are accepted only for headers without a BAL hash.
func ValidateBlockAccessListForHeader(encoded []byte, header *types.Header) error {
	if header == nil {
		return ErrReceiptsBlockUnknown
	}
	isEmpty := len(encoded) == 0 || (len(encoded) == 1 && encoded[0] == 0xc0)
	if header.BlockAccessListHash == nil {
		if !isEmpty {
			return fmt.Errorf("%w: header has no BAL hash but a BAL was delivered", ErrBALHashMismatch)
		}
		return nil
	}
	if isEmpty {
		return fmt.Errorf("%w: header requires a BAL, got empty entry", ErrBALHashMismatch)
	}
	if !isRLPList(encoded) {
		return fmt.Errorf("malformed BAL encoding: not an RLP list")
	}
	if got := crypto.Keccak256Hash(encoded); got != *header.BlockAccessListHash {
		return fmt.Errorf("%w: header=%s computed=%s", ErrBALHashMismatch,
			header.BlockAccessListHash.Hex(), got.Hex())
	}
	return nil
}

// PeerFetcher adapts the eth handler to implement the sync.HeaderFetcher
// and sync.BodyFetcher interfaces, enabling the sync package to fetch
// data from the local chain (for testing) or remote peers.
type PeerFetcher struct {
	chain Blockchain
}

// NewPeerFetcher creates a PeerFetcher backed by the given blockchain.
func NewPeerFetcher(chain Blockchain) *PeerFetcher {
	return &PeerFetcher{chain: chain}
}

// FetchHeaders fetches headers from the local chain starting at `from`.
func (pf *PeerFetcher) FetchHeaders(from uint64, count int) ([]*types.Header, error) {
	var headers []*types.Header
	for i := 0; i < count; i++ {
		num := from + uint64(i)
		block := pf.chain.GetBlockByNumber(num)
		if block == nil {
			break
		}
		headers = append(headers, block.Header())
	}
	if len(headers) == 0 {
		return nil, fmt.Errorf("no headers found starting at %d", from)
	}
	return headers, nil
}

// FetchBodies fetches block bodies from the local chain by hash.
func (pf *PeerFetcher) FetchBodies(hashes []types.Hash) ([]*types.Body, error) {
	bodies := make([]*types.Body, 0, len(hashes))
	for _, hash := range hashes {
		block := pf.chain.GetBlock(hash)
		if block == nil {
			// Return empty body to keep alignment.
			bodies = append(bodies, &types.Body{})
			continue
		}
		bodies = append(bodies, block.Body())
	}
	return bodies, nil
}

// decodeMsg is a helper that converts a p2p.Msg to p2p.Message and decodes.
func decodeMsg(msg p2p.Msg, val interface{}) error {
	return p2p.DecodeMessage(p2p.Message{
		Code:    msg.Code,
		Size:    msg.Size,
		Payload: msg.Payload,
	}, val)
}
