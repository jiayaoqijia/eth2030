package payload

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/eth2030/eth2030/bal"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// DecodeBlockAccessList validates the Engine API DATA field and returns its
// EIP-7928 commitment to the canonical RLP bytes.
func DecodeBlockAccessList(raw json.RawMessage) (types.Hash, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil || !strings.HasPrefix(value, "0x") {
		return types.Hash{}, fmt.Errorf("blockAccessList must be 0x-prefixed hex DATA")
	}
	encoded, err := hex.DecodeString(value[2:])
	if err != nil {
		return types.Hash{}, fmt.Errorf("blockAccessList: %w", err)
	}
	if len(encoded) == 0 {
		return types.Hash{}, fmt.Errorf("blockAccessList RLP is empty")
	}
	// The processor validates canonical RLP by matching this commitment
	// against the BAL reconstructed from execution.
	return crypto.Keccak256Hash(encoded), nil
}

// PayloadToHeader converts an ExecutionPayloadV4 to a block Header.
func PayloadToHeader(p *ExecutionPayloadV4) *types.Header {
	header := &types.Header{
		ParentHash:    p.ParentHash,
		Coinbase:      p.FeeRecipient,
		Root:          p.StateRoot,
		ReceiptHash:   p.ReceiptsRoot,
		Bloom:         p.LogsBloom,
		MixDigest:     p.PrevRandao,
		Number:        new(big.Int).SetUint64(p.BlockNumber),
		GasLimit:      p.GasLimit,
		GasUsed:       p.GasUsed,
		Time:          p.Timestamp,
		Extra:         p.ExtraData,
		BaseFee:       p.BaseFeePerGas,
		BlobGasUsed:   &p.BlobGasUsed,
		ExcessBlobGas: &p.ExcessBlobGas,
	}
	// Post-merge: difficulty is always 0, uncle hash is empty.
	header.Difficulty = new(big.Int)
	header.UncleHash = types.EmptyUncleHash
	return header
}

// HeaderToPayloadFields extracts common payload fields from a Header.
func HeaderToPayloadFields(header *types.Header) ExecutionPayloadV1 {
	return ExecutionPayloadV1{
		ParentHash:    header.ParentHash,
		FeeRecipient:  header.Coinbase,
		StateRoot:     header.Root,
		ReceiptsRoot:  header.ReceiptHash,
		LogsBloom:     header.Bloom,
		PrevRandao:    header.MixDigest,
		BlockNumber:   header.Number.Uint64(),
		GasLimit:      header.GasLimit,
		GasUsed:       header.GasUsed,
		Timestamp:     header.Time,
		ExtraData:     header.Extra,
		BaseFeePerGas: header.BaseFee,
	}
}

// WithdrawalsToEngine converts core Withdrawal types to payload Withdrawal types.
func WithdrawalsToEngine(ws []*types.Withdrawal) []*Withdrawal {
	result := make([]*Withdrawal, len(ws))
	for i, w := range ws {
		result[i] = &Withdrawal{
			Index:          w.Index,
			ValidatorIndex: w.ValidatorIndex,
			Address:        w.Address,
			Amount:         w.Amount,
		}
	}
	return result
}

// WithdrawalsToCore converts payload Withdrawal types to core Withdrawal types.
func WithdrawalsToCore(ws []*Withdrawal) []*types.Withdrawal {
	result := make([]*types.Withdrawal, len(ws))
	for i, w := range ws {
		result[i] = &types.Withdrawal{
			Index:          w.Index,
			ValidatorIndex: w.ValidatorIndex,
			Address:        w.Address,
			Amount:         w.Amount,
		}
	}
	return result
}

// BlockToPayload converts a types.Block to an ExecutionPayloadV4.
func BlockToPayload(block *types.Block, prevRandao types.Hash, withdrawals []*Withdrawal) *ExecutionPayloadV4 {
	header := block.Header()

	// Encode transactions.
	encodedTxs := make([][]byte, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		enc, err := tx.EncodeRLP()
		if err != nil {
			continue
		}
		encodedTxs[i] = enc
	}

	// Blob gas fields.
	var blobGasUsed, excessBlobGas uint64
	if header.BlobGasUsed != nil {
		blobGasUsed = *header.BlobGasUsed
	}
	if header.ExcessBlobGas != nil {
		excessBlobGas = *header.ExcessBlobGas
	}

	if withdrawals == nil {
		withdrawals = []*Withdrawal{}
	}

	return &ExecutionPayloadV4{
		ExecutionPayloadV3: ExecutionPayloadV3{
			ExecutionPayloadV2: ExecutionPayloadV2{
				ExecutionPayloadV1: ExecutionPayloadV1{
					ParentHash:    header.ParentHash,
					FeeRecipient:  header.Coinbase,
					StateRoot:     header.Root,
					ReceiptsRoot:  header.ReceiptHash,
					LogsBloom:     header.Bloom,
					PrevRandao:    prevRandao,
					BlockNumber:   header.Number.Uint64(),
					GasLimit:      header.GasLimit,
					GasUsed:       header.GasUsed,
					Timestamp:     header.Time,
					ExtraData:     header.Extra,
					BaseFeePerGas: header.BaseFee,
					BlockHash:     block.Hash(),
					Transactions:  encodedTxs,
				},
				Withdrawals: withdrawals,
			},
			BlobGasUsed:   blobGasUsed,
			ExcessBlobGas: excessBlobGas,
		},
	}
}

// BlockToPayloadV5 converts a built block to an ExecutionPayloadV5 with BAL.
func BlockToPayloadV5(block *types.Block, prevRandao types.Hash, withdrawals []*Withdrawal, blockBAL *bal.BlockAccessList, slotNumber uint64) *ExecutionPayloadV5 {
	ep4 := BlockToPayload(block, prevRandao, withdrawals)

	// lighthouse >= 8.4 deserializes the Gloas payload's blockAccessList as a
	// 0x-prefixed hex string (hex_prog_var_list); contents are passed through
	// unvalidated to the payload envelope. Encode the RLP BAL bytes as hex;
	// an absent BAL serializes as the empty string "0x".
	balData := json.RawMessage(`"0x"`)
	if blockBAL != nil {
		if encoded, err := blockBAL.EncodeRLP(); err == nil {
			if quoted, err := json.Marshal("0x" + hex.EncodeToString(encoded)); err == nil {
				balData = json.RawMessage(quoted)
			}
		}
	}

	return &ExecutionPayloadV5{
		ExecutionPayloadV4: *ep4,
		BlockAccessList:    balData,
		SlotNumber:         fmt.Sprintf("0x%x", slotNumber),
	}
}
