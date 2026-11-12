// Package p2p implements the devp2p eth protocol types for peer-to-peer networking.
package ethproto

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// Protocol version constants.
const (
	ETH68 = 68
	ETH70 = 70 // EIP-7975: Partial Block Receipt Lists
	ETH71 = 71 // EIP-8159: Block Access List Exchange
)

// eth/68 protocol message codes.
const (
	StatusMsg                     = 0x00
	NewBlockHashesMsg             = 0x01
	TransactionsMsg               = 0x02
	GetBlockHeadersMsg            = 0x03
	BlockHeadersMsg               = 0x04
	GetBlockBodiesMsg             = 0x05
	BlockBodiesMsg                = 0x06
	NewBlockMsg                   = 0x07
	NewPooledTransactionHashesMsg = 0x08
	GetPooledTransactionsMsg      = 0x09
	PooledTransactionsMsg         = 0x0a
	GetReceiptsMsg                = 0x0f
	ReceiptsMsg                   = 0x10

	// eth/71 message codes (EIP-8159: Block Access List Exchange).
	// Message IDs 0x12/0x13 follow sequentially from BlockRangeUpdate (0x11).
	GetBlockAccessListsMsg = 0x12
	BlockAccessListsMsg    = 0x13
)

// StatusData represents the status message exchanged during the eth handshake.
type StatusData struct {
	ProtocolVersion uint32
	NetworkID       uint64
	TD              *big.Int
	Head            types.Hash
	Genesis         types.Hash
	ForkID          ForkID
}

// NewBlockHashesEntry is a single block hash announcement.
type NewBlockHashesEntry struct {
	Hash   types.Hash
	Number uint64
}

// HashOrNumber is a combined field for requesting a block header either by
// hash or by number. Exactly one must be set.
type HashOrNumber struct {
	Hash   types.Hash // If non-zero, look up by hash.
	Number uint64     // If Hash is zero, look up by number.
}

// IsHash returns true if the request specifies a hash rather than a number.
func (hon *HashOrNumber) IsHash() bool {
	return !hon.Hash.IsZero()
}

// GetBlockHeadersRequest represents a request for block headers.
type GetBlockHeadersRequest struct {
	Origin  HashOrNumber // Block from which to retrieve headers.
	Amount  uint64       // Maximum number of headers to retrieve.
	Skip    uint64       // Blocks to skip between consecutive headers.
	Reverse bool         // Whether to query in reverse direction.
}

// GetBlockHeadersPacket wraps a GetBlockHeadersRequest with a request ID.
type GetBlockHeadersPacket struct {
	RequestID uint64
	Request   GetBlockHeadersRequest
}

// BlockHeadersPacket is the response to GetBlockHeadersRequest.
type BlockHeadersPacket struct {
	RequestID uint64
	Headers   []*types.Header
}

// GetBlockBodiesRequest is a list of block hashes for which to retrieve bodies.
type GetBlockBodiesRequest []types.Hash

// GetBlockBodiesPacket wraps a GetBlockBodiesRequest with a request ID.
type GetBlockBodiesPacket struct {
	RequestID uint64
	Hashes    GetBlockBodiesRequest
}

// BlockBody represents the body of a single block in a response.
type BlockBody struct {
	Transactions []*types.Transaction
	Uncles       []*types.Header
	Withdrawals  []*types.Withdrawal
}

// BlockBodiesPacket is the response to GetBlockBodiesRequest.
type BlockBodiesPacket struct {
	RequestID uint64
	Bodies    []*BlockBody
}

// NewBlockData is the data propagated when a new block is announced.
type NewBlockData struct {
	Block *types.Block
	TD    *big.Int
}

// GetReceiptsRequest is a list of block hashes for which to retrieve receipts.
type GetReceiptsRequest []types.Hash

// GetReceiptsPacket wraps a GetReceiptsRequest with a request ID.
type GetReceiptsPacket struct {
	RequestID uint64
	Hashes    GetReceiptsRequest
}

// ReceiptsPacket is the response to GetReceiptsRequest.
type ReceiptsPacket struct {
	RequestID uint64
	Receipts  [][]*types.Receipt
}

// NewPooledTransactionHashesPacket68 represents the eth/68 announcement of
// transaction hashes along with their types and sizes.
type NewPooledTransactionHashesPacket68 struct {
	Types  []byte
	Sizes  []uint32
	Hashes []types.Hash
}

// GetPooledTransactionsRequest is a list of transaction hashes to retrieve
// from the remote peer's transaction pool.
type GetPooledTransactionsRequest []types.Hash

// GetPooledTransactionsPacket wraps a GetPooledTransactionsRequest with a request ID.
type GetPooledTransactionsPacket struct {
	RequestID uint64
	Hashes    GetPooledTransactionsRequest
}

// PooledTransactionsPacket is the response to GetPooledTransactionsRequest.
type PooledTransactionsPacket struct {
	RequestID    uint64
	Transactions []*types.Transaction
}

// --- eth/70: EIP-7975 Partial Block Receipt Lists ---
//
// eth/70 does not add message codes: it extends the encodings of
// GetReceipts (0x0f) and Receipts (0x10) for paginated receipt transfer.

// GetReceiptsPacketV70 is the eth/70 GetReceipts (0x0f) request:
// [request-id, firstBlockReceiptIndex, [blockhash, ...]]. For the first
// requested block the server omits receipts before firstBlockReceiptIndex.
type GetReceiptsPacketV70 struct {
	RequestID              uint64
	FirstBlockReceiptIndex uint64
	Hashes                 []types.Hash
}

// ReceiptsPacketV70 is the eth/70 Receipts (0x10) response:
// [request-id, lastBlockIncomplete, [[receipt, ...], ...]]. When
// lastBlockIncomplete is 1 the last receipt list is partial and the client
// must request the remainder in a follow-up message.
type ReceiptsPacketV70 struct {
	RequestID           uint64
	LastBlockIncomplete bool
	Receipts            [][]*types.Receipt
}

// --- eth/71: EIP-8159 Block Access List Exchange ---

// GetBlockAccessListsPacket is the eth/71 GetBlockAccessLists (0x12)
// request: [request-id, [blockhash, ...]]. Requests for unavailable BALs
// (pre-activation or pruned) return empty entries.
type GetBlockAccessListsPacket struct {
	RequestID   uint64
	BlockHashes []types.Hash
}

// BlockAccessListsPacket is the eth/71 BlockAccessLists (0x13) response:
// [request-id, [bal, ...]]. Each element is the RLP-encoded block access
// list per EIP-7928, in request order; the RLP empty list (0xc0) marks an
// unavailable BAL. Clients MUST validate each BAL against the
// block-access-list-hash in the corresponding block header.
type BlockAccessListsPacket struct {
	RequestID   uint64
	AccessLists [][]byte
}
