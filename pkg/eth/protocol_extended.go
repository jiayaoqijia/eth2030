package eth

import (
	"errors"
	"fmt"
	"sort"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/p2p"
)

// Extended protocol version constants (ETH/68 through ETH/72).
const (
	ETH69    = 69 // EIP-7642: eth/69 - Remove pre-merge fields
	ExtETH72 = 72 // EIP-XXXX: Execution Witness Exchange
)

// ETH/70 message codes (partial receipts).
const (
	MsgGetPartialReceipts uint64 = 0x0d
	MsgPartialReceipts    uint64 = 0x0e
)

// ETH/71 message codes (block access list exchange).
const (
	MsgGetBlockAccessLists uint64 = 0x0f
	MsgBlockAccessLists    uint64 = 0x10
)

// ETH/72 message codes (execution witness exchange).
const (
	MsgGetExecutionWitness uint64 = 0x11
	MsgExecutionWitness    uint64 = 0x12
)

// Protocol limits.
const (
	// MaxReceiptsServe is the max receipt sets in a single response.
	MaxReceiptsServe = 256

	// MaxExecutionWitness is the max witness data size in bytes.
	MaxExecutionWitness = 1 << 20 // 1 MiB

	// MaxMessageSize is the maximum ETH protocol message size in bytes.
	MaxMessageSize = 10 * 1024 * 1024 // 10 MiB
)

// Extended protocol errors.
var (
	ErrUnsupportedMessage = errors.New("eth: unsupported message for protocol version")
	ErrVersionNegotiation = errors.New("eth: version negotiation failed")
	ErrMessageTooLarge    = errors.New("eth: message exceeds maximum size")
	ErrInvalidMessageCode = errors.New("eth: invalid message code for version")
)

// ProtocolCapability represents a single protocol version with its name
// and the range of message IDs it supports.
type ProtocolCapability struct {
	Version    uint
	Name       string
	Length     uint64 // number of message IDs consumed
	MessageIDs []uint64
}

// AllCapabilities returns the full set of supported ETH protocol capabilities,
// from ETH/68 through ETH/72.
func AllCapabilities() []ProtocolCapability {
	return []ProtocolCapability{
		{
			Version: ETH68,
			Name:    "eth/68",
			Length:  17,
			MessageIDs: []uint64{
				MsgStatus, MsgNewBlockHashes, MsgTransactions,
				MsgGetBlockHeaders, MsgBlockHeaders,
				MsgGetBlockBodies, MsgBlockBodies,
				MsgNewBlock, MsgNewPooledTransactionHashes,
				MsgGetPooledTransactions, MsgPooledTransactions,
			},
		},
		{
			Version: uint(ETH69),
			Name:    "eth/69",
			Length:  17,
			MessageIDs: []uint64{
				MsgStatus, MsgNewBlockHashes, MsgTransactions,
				MsgGetBlockHeaders, MsgBlockHeaders,
				MsgGetBlockBodies, MsgBlockBodies,
				MsgNewBlock, MsgNewPooledTransactionHashes,
				MsgGetPooledTransactions, MsgPooledTransactions,
			},
		},
		{
			Version: uint(ETH70),
			Name:    "eth/70",
			Length:  19,
			MessageIDs: []uint64{
				MsgStatus, MsgNewBlockHashes, MsgTransactions,
				MsgGetBlockHeaders, MsgBlockHeaders,
				MsgGetBlockBodies, MsgBlockBodies,
				MsgNewBlock, MsgNewPooledTransactionHashes,
				MsgGetPooledTransactions, MsgPooledTransactions,
				MsgGetPartialReceipts, MsgPartialReceipts,
			},
		},
		{
			Version: uint(ETH71),
			Name:    "eth/71",
			Length:  21,
			MessageIDs: []uint64{
				MsgStatus, MsgNewBlockHashes, MsgTransactions,
				MsgGetBlockHeaders, MsgBlockHeaders,
				MsgGetBlockBodies, MsgBlockBodies,
				MsgNewBlock, MsgNewPooledTransactionHashes,
				MsgGetPooledTransactions, MsgPooledTransactions,
				MsgGetPartialReceipts, MsgPartialReceipts,
				MsgGetBlockAccessLists, MsgBlockAccessLists,
			},
		},
		{
			Version: uint(ExtETH72),
			Name:    "eth/72",
			Length:  23,
			MessageIDs: []uint64{
				MsgStatus, MsgNewBlockHashes, MsgTransactions,
				MsgGetBlockHeaders, MsgBlockHeaders,
				MsgGetBlockBodies, MsgBlockBodies,
				MsgNewBlock, MsgNewPooledTransactionHashes,
				MsgGetPooledTransactions, MsgPooledTransactions,
				MsgGetPartialReceipts, MsgPartialReceipts,
				MsgGetBlockAccessLists, MsgBlockAccessLists,
				MsgGetExecutionWitness, MsgExecutionWitness,
			},
		},
	}
}

// CapabilityByVersion returns the capability for the given version, or nil.
func CapabilityByVersion(version uint) *ProtocolCapability {
	for _, cap := range AllCapabilities() {
		if cap.Version == version {
			return &cap
		}
	}
	return nil
}

// NegotiateCapability selects the highest common protocol version between
// local and remote capabilities. Returns nil if no common version exists.
func NegotiateCapability(local, remote []ProtocolCapability) *ProtocolCapability {
	remoteSet := make(map[uint]ProtocolCapability, len(remote))
	for _, r := range remote {
		remoteSet[r.Version] = r
	}

	// Sort local by version descending (highest first).
	sorted := make([]ProtocolCapability, len(local))
	copy(sorted, local)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Version > sorted[j].Version
	})

	for _, l := range sorted {
		if _, ok := remoteSet[l.Version]; ok {
			result := l
			return &result
		}
	}
	return nil
}

// IsMessageSupported checks whether the given message code is valid for
// the specified protocol version.
func IsMessageSupported(version uint, msgCode uint64) bool {
	cap := CapabilityByVersion(version)
	if cap == nil {
		return false
	}
	for _, id := range cap.MessageIDs {
		if id == msgCode {
			return true
		}
	}
	return false
}

// MessageIDOffset returns the base offset for a protocol version.
// Used for multiplexing multiple sub-protocols on the same p2p connection.
func MessageIDOffset(version uint, baseOffset uint64) uint64 {
	cap := CapabilityByVersion(version)
	if cap == nil {
		return baseOffset
	}
	return baseOffset
}

// ExtMsgCodeName returns a human-readable name for any ETH message code,
// including extended message types for ETH/70-72.
func ExtMsgCodeName(code uint64) string {
	switch code {
	case MsgStatus:
		return "Status"
	case MsgNewBlockHashes:
		return "NewBlockHashes"
	case MsgTransactions:
		return "Transactions"
	case MsgGetBlockHeaders:
		return "GetBlockHeaders"
	case MsgBlockHeaders:
		return "BlockHeaders"
	case MsgGetBlockBodies:
		return "GetBlockBodies"
	case MsgBlockBodies:
		return "BlockBodies"
	case MsgNewBlock:
		return "NewBlock"
	case MsgNewPooledTransactionHashes:
		return "NewPooledTransactionHashes"
	case MsgGetPooledTransactions:
		return "GetPooledTransactions"
	case MsgPooledTransactions:
		return "PooledTransactions"
	case MsgGetPartialReceipts:
		return "GetPartialReceipts"
	case MsgPartialReceipts:
		return "PartialReceipts"
	case MsgGetBlockAccessLists:
		return "GetBlockAccessLists"
	case MsgBlockAccessLists:
		return "BlockAccessLists"
	case MsgGetExecutionWitness:
		return "GetExecutionWitness"
	case MsgExecutionWitness:
		return "ExecutionWitness"
	default:
		return fmt.Sprintf("Unknown(0x%02x)", code)
	}
}

// GetPartialReceiptsMessage requests specific receipt indices for a block.
type GetPartialReceiptsMessage struct {
	BlockHash types.Hash
	Indices   []uint64
}

// PartialReceiptsMessage contains the requested partial receipts.
type PartialReceiptsMessage struct {
	BlockHash types.Hash
	Receipts  []*types.Receipt
}

// GetBlockAccessListsMessage requests block access lists by hash.
type GetBlockAccessListsMessage struct {
	Hashes []types.Hash
}

// BlockAccessListsMessage contains RLP-encoded block access lists
// (EIP-8159); empty entries mark unavailable BALs.
type BlockAccessListsMessage struct {
	Hashes      []types.Hash
	AccessLists [][]byte
}

// GetExecutionWitnessMessage requests an execution witness for a block.
type GetExecutionWitnessMessage struct {
	BlockHash types.Hash
}

// ExecutionWitnessMessage contains an execution witness.
type ExecutionWitnessMessage struct {
	BlockHash   types.Hash
	WitnessData []byte
}

// ExtStatusInfo extends StatusInfo with fields from eth/69+.
type ExtStatusInfo struct {
	StatusInfo
	// eth/69: Remove pre-merge TD field.
	// PostMerge indicates this is a post-merge node (TD is zeroed in eth/69+).
	PostMerge bool
}

// BuildStatusMessage constructs a StatusMessage from chain state.
func BuildStatusMessage(
	protocolVersion uint32,
	networkID uint64,
	chain Blockchain,
	forkID p2p.ForkID,
	oldestBlock uint64,
) *StatusMessage {
	currentBlock := chain.CurrentBlock()
	genesis := chain.Genesis()

	msg := &StatusMessage{
		ProtocolVersion: protocolVersion,
		NetworkID:       networkID,
		BestHash:        currentBlock.Hash(),
		Genesis:         genesis.Hash(),
		ForkID:          forkID,
	}
	return msg
}

// SupportedVersions returns a sorted slice of all supported ETH protocol
// version numbers (from highest to lowest).
func SupportedVersions() []uint {
	caps := AllCapabilities()
	versions := make([]uint, len(caps))
	for i, c := range caps {
		versions[i] = c.Version
	}
	sort.Slice(versions, func(i, j int) bool {
		return versions[i] > versions[j]
	})
	return versions
}
