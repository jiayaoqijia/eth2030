// Package payload defines the execution payload types for the Engine API.
package payload

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// PayloadID is the identifier for an execution payload being assembled.
type PayloadID [8]byte

// String returns the hex representation of the PayloadID.
func (id PayloadID) String() string {
	return fmt.Sprintf("0x%x", id[:])
}

// MarshalJSON implements json.Marshaler. PayloadID encodes as a 0x-prefixed hex string.
func (id PayloadID) MarshalJSON() ([]byte, error) {
	return json.Marshal(fmt.Sprintf("0x%x", id[:]))
}

// UnmarshalJSON implements json.Unmarshaler.
func (id *PayloadID) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("PayloadID: %w", err)
	}
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		s = s[2:]
	}
	b, err := hex.DecodeString(s)
	if err != nil {
		return fmt.Errorf("PayloadID: invalid hex: %w", err)
	}
	if len(b) != 8 {
		return fmt.Errorf("PayloadID: expected 8 bytes, got %d", len(b))
	}
	copy(id[:], b)
	return nil
}

// Withdrawal represents a validator withdrawal.
type Withdrawal struct {
	Index          uint64        `json:"index"`
	ValidatorIndex uint64        `json:"validatorIndex"`
	Address        types.Address `json:"address"`
	Amount         uint64        `json:"amount"` // in Gwei
}

// ExecutionPayloadV1 is the Prague V1 execution payload.
type ExecutionPayloadV1 struct {
	ParentHash    types.Hash    `json:"parentHash"`
	FeeRecipient  types.Address `json:"feeRecipient"`
	StateRoot     types.Hash    `json:"stateRoot"`
	ReceiptsRoot  types.Hash    `json:"receiptsRoot"`
	LogsBloom     types.Bloom   `json:"logsBloom"`
	PrevRandao    types.Hash    `json:"prevRandao"`
	BlockNumber   uint64        `json:"blockNumber"`
	GasLimit      uint64        `json:"gasLimit"`
	GasUsed       uint64        `json:"gasUsed"`
	Timestamp     uint64        `json:"timestamp"`
	ExtraData     []byte        `json:"extraData"`
	BaseFeePerGas *big.Int      `json:"baseFeePerGas"`
	BlockHash     types.Hash    `json:"blockHash"`
	Transactions  [][]byte      `json:"transactions"`
}

// ExecutionPayloadV2 extends V1 with withdrawals (Shanghai).
type ExecutionPayloadV2 struct {
	ExecutionPayloadV1
	Withdrawals []*Withdrawal `json:"withdrawals"`
}

// ExecutionPayloadV3 extends V2 with blob gas (Cancun/EIP-4844).
type ExecutionPayloadV3 struct {
	ExecutionPayloadV2
	BlobGasUsed   uint64 `json:"blobGasUsed"`
	ExcessBlobGas uint64 `json:"excessBlobGas"`
}

// ExecutionPayloadV4 extends V3 with execution requests (Prague/EIP-7685).
type ExecutionPayloadV4 struct {
	ExecutionPayloadV3
	ExecutionRequests [][]byte `json:"executionRequests"`
}

// ExecutionPayloadV5 extends V4 with Block Access Lists (Amsterdam/EIP-7928).
type ExecutionPayloadV5 struct {
	ExecutionPayloadV4
	BlockAccessList json.RawMessage `json:"blockAccessList,omitempty"`
	// SlotNumber is the beacon slot (hex-encoded) that requested this payload
	// (Gloas payload field; lighthouse >= 8.4 expects it on Gloas payloads).
	SlotNumber string `json:"slotNumber,omitempty"`
}

// PayloadAttributesV1 contains attributes for building a new payload.
type PayloadAttributesV1 struct {
	Timestamp             uint64        `json:"timestamp"`
	PrevRandao            types.Hash    `json:"prevRandao"`
	SuggestedFeeRecipient types.Address `json:"suggestedFeeRecipient"`
}

// PayloadAttributesV2 extends V1 with withdrawals.
type PayloadAttributesV2 struct {
	PayloadAttributesV1
	Withdrawals []*Withdrawal `json:"withdrawals"`
}

// PayloadAttributesV3 extends V2 with parent beacon block root.
type PayloadAttributesV3 struct {
	PayloadAttributesV2
	ParentBeaconBlockRoot types.Hash `json:"parentBeaconBlockRoot"`
}

// PayloadAttributesV4 extends V3 with slot number and inclusion list (Amsterdam/FOCIL).
type PayloadAttributesV4 struct {
	PayloadAttributesV3
	SlotNumber                uint64   `json:"slotNumber"`
	InclusionListTransactions [][]byte `json:"inclusionListTransactions,omitempty"` // EIP-7805 FOCIL
}

// GetPayloadV3Response is the response for engine_getPayloadV3 (Cancun).
type GetPayloadV3Response struct {
	ExecutionPayload *ExecutionPayloadV3 `json:"executionPayload"`
	BlockValue       *big.Int            `json:"blockValue"`
	BlobsBundle      *BlobsBundleV1      `json:"blobsBundle"`
	Override         bool                `json:"shouldOverrideBuilder"`
}

// GetPayloadV4Response is the response for engine_getPayloadV4 (Prague).
type GetPayloadV4Response struct {
	ExecutionPayload  *ExecutionPayloadV3 `json:"executionPayload"`
	BlockValue        *big.Int            `json:"blockValue"`
	BlobsBundle       *BlobsBundleV1      `json:"blobsBundle"`
	Override          bool                `json:"shouldOverrideBuilder"`
	ExecutionRequests [][]byte            `json:"executionRequests"`
}

// GetPayloadV6Response is the response for engine_getPayloadV5/V6 (Osaka/Amsterdam).
// Per the Osaka spec, BlobsBundle uses V2 format with 128 per-cell KZG proofs.
type GetPayloadV6Response struct {
	ExecutionPayload  *ExecutionPayloadV5 `json:"executionPayload"`
	BlockValue        *big.Int            `json:"blockValue"`
	BlobsBundle       *BlobsBundleV2      `json:"blobsBundle"`
	Override          bool                `json:"shouldOverrideBuilder"`
	ExecutionRequests [][]byte            `json:"executionRequests"`

	// EIP-7807 SSZ activation fields (set when SSZ blocks are active):
	// SSZPayload is the 0x-hex EIP-7807 payload encoding of the block and
	// SSZBlockHash its hash tree root, the SSZ execution block hash.
	SSZPayload   json.RawMessage `json:"sszPayload,omitempty"`
	SSZBlockHash *types.Hash     `json:"sszBlockHash,omitempty"`
}

// BlobsBundleV1 is the blobs bundle returned by engine_getPayload.
type BlobsBundleV1 struct {
	Commitments [][]byte
	Proofs      [][]byte
	Blobs       [][]byte
}

// BlobAndProofV1 is a single blob with its KZG commitment and proof,
// returned by engine_getBlobsV1.
type BlobAndProofV1 struct {
	Blob       []byte
	Commitment []byte
	Proof      []byte
}

// GetPayloadResponse is the combined response for engine_getPayload.
type GetPayloadResponse struct {
	ExecutionPayload *ExecutionPayloadV4 `json:"executionPayload"`
	BlockValue       *big.Int            `json:"blockValue"`
	BlobsBundle      *BlobsBundleV1      `json:"blobsBundle,omitempty"`
	Override         bool                `json:"shouldOverrideBuilder"`
}
