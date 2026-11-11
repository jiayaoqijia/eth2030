package ethproto

import (
	"fmt"

	"github.com/eth2030/eth2030/p2p/wire"
)

// ForkID is the EIP-2124 fork identifier for chain compatibility checks.
// It consists of a CRC32 checksum of the genesis hash and fork block numbers,
// plus the block number of the next expected fork.
type ForkID struct {
	Hash [4]byte // CRC32 checksum of the genesis hash and passed fork block numbers.
	Next uint64  // Block number of the next expected fork, or 0 if no fork is planned.
}

// ValidateMessageCode returns an error if the message code is not a known
// protocol message (eth/68 through eth/71).
func ValidateMessageCode(code uint64) error {
	switch code {
	case StatusMsg, NewBlockHashesMsg, TransactionsMsg,
		GetBlockHeadersMsg, BlockHeadersMsg,
		GetBlockBodiesMsg, BlockBodiesMsg,
		NewBlockMsg, NewPooledTransactionHashesMsg,
		GetPooledTransactionsMsg, PooledTransactionsMsg,
		GetReceiptsMsg, ReceiptsMsg,
		GetBlockAccessListsMsg, BlockAccessListsMsg:
		return nil
	default:
		return fmt.Errorf("%w: 0x%02x", wire.ErrInvalidMsgCode, code)
	}
}

// MessageName returns a human-readable name for the given message code.
func MessageName(code uint64) string {
	switch code {
	case StatusMsg:
		return "Status"
	case NewBlockHashesMsg:
		return "NewBlockHashes"
	case TransactionsMsg:
		return "Transactions"
	case GetBlockHeadersMsg:
		return "GetBlockHeaders"
	case BlockHeadersMsg:
		return "BlockHeaders"
	case GetBlockBodiesMsg:
		return "GetBlockBodies"
	case BlockBodiesMsg:
		return "BlockBodies"
	case NewBlockMsg:
		return "NewBlock"
	case NewPooledTransactionHashesMsg:
		return "NewPooledTransactionHashes"
	case GetPooledTransactionsMsg:
		return "GetPooledTransactions"
	case PooledTransactionsMsg:
		return "PooledTransactions"
	case GetReceiptsMsg:
		return "GetReceipts"
	case ReceiptsMsg:
		return "Receipts"
	case GetBlockAccessListsMsg:
		return "GetBlockAccessLists"
	case BlockAccessListsMsg:
		return "BlockAccessLists"
	default:
		return fmt.Sprintf("Unknown(0x%02x)", code)
	}
}
