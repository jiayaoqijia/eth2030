// Package eth implements the eth/68 wire protocol handler connecting P2P
// networking to the blockchain and transaction pool.
package eth

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/p2p"
)

// Protocol version constants.
const (
	ETH68 = 68
	ETH70 = 70 // EIP-7975: Partial Block Receipt Lists
	ETH71 = 71 // EIP-8159: Block Access List Exchange
)

// MaxHeaders is the maximum number of headers returned in a single response.
const MaxHeaders = 1024

// MaxBodies is the maximum number of block bodies returned in a single response.
const MaxBodies = 512

// MaxReceiptsResponseSize is the soft byte budget for one Receipts (0x10)
// response (EIP-7975 pagination). Blocks exceeding the budget are served
// across messages with lastBlockIncomplete set.
const MaxReceiptsResponseSize = 10 << 20

// MaxAccessLists is the maximum number of block access lists returned in a single response.
const MaxAccessLists = 64

// Blockchain defines the interface for blockchain operations needed by the handler.
type Blockchain interface {
	CurrentBlock() *types.Block
	GetBlock(hash types.Hash) *types.Block
	GetBlockByNumber(number uint64) *types.Block
	HasBlock(hash types.Hash) bool
	InsertBlock(block *types.Block) error
	Genesis() *types.Block
}

// TxPool defines the interface for transaction pool operations needed by the handler.
type TxPool interface {
	AddRemote(tx *types.Transaction) error
	Get(hash types.Hash) *types.Transaction
	Pending() map[types.Address][]*types.Transaction
}

// ReceiptProvider provides receipt data for eth/70 receipt requests
// (EIP-7975). Implementations back the paginated Receipts (0x10) response.
type ReceiptProvider interface {
	GetReceipts(blockHash types.Hash) []*types.Receipt
}

// AccessListProvider provides RLP-encoded block access list data for
// eth/71 BAL exchange (EIP-8159). The returned bytes are the canonical
// EIP-7928 BAL encoding; ok is false when the BAL is unavailable
// (pre-activation or pruned), in which case the handler serves the empty
// list per the spec.
type AccessListProvider interface {
	GetBlockAccessListRLP(blockHash types.Hash) (bal []byte, ok bool)
}

// StatusInfo holds the local chain status for handshake exchange.
type StatusInfo struct {
	ProtocolVersion uint32
	NetworkID       uint64
	TD              *big.Int
	Head            types.Hash
	Genesis         types.Hash
	ForkID          p2p.ForkID

	// EIP-4444: advertise available history range so peers know what data
	// this node can serve. OldestBlock is the lowest block number for which
	// bodies and receipts are available. Zero means no pruning has occurred.
	OldestBlock uint64
}
