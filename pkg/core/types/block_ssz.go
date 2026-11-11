package types

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/ssz"
)

// ExecutionGasAmounts and ExecutionBaseFees encode the regular and blob
// dimensions of the published EIP-7807 ExecutionPayload.
type ExecutionGasAmounts struct {
	Regular uint64
	Blob    uint64
}

type ExecutionBaseFees struct {
	Regular *big.Int
	Blob    *big.Int
}

// ExecutionSSZFields are shared by the payload and its header summary.
type ExecutionSSZFields struct {
	ParentHash            Hash
	Miner                 Address
	StateRoot             Hash
	ReceiptsRoot          Hash
	Number                uint64
	GasLimits             ExecutionGasAmounts
	GasUsed               ExecutionGasAmounts
	Timestamp             uint64
	ExtraData             []byte
	MixHash               Hash
	BaseFeesPerGas        ExecutionBaseFees
	ExcessGas             ExecutionGasAmounts
	ParentBeaconBlockRoot Hash
	RequestsHash          Hash
	SlotNumber            uint64
}

// ExecutionPayloadSSZ is the published EIP-7807 payload. Transactions and
// withdrawals retain their existing EIP-2718 and RLP element encodings.
type ExecutionPayloadSSZ struct {
	ExecutionSSZFields
	Transactions    [][]byte
	Withdrawals     [][]byte
	BlockAccessList []byte
}

// ExecutionHeaderSSZ is the SSZ summary of an ExecutionPayloadSSZ.
type ExecutionHeaderSSZ struct {
	ExecutionSSZFields
	TransactionsRoot    Hash
	WithdrawalsRoot     Hash
	BlockAccessListRoot Hash
}

// ExecutionSSZContext supplies the EIP-7807 inputs unavailable in an RLP block.
type ExecutionSSZContext struct {
	BlobGasLimit     uint64
	BlobBaseFee      *big.Int
	ExcessRegularGas uint64
	Requests         Requests
	BlockAccessList  []byte
	SlotNumber       uint64
}

// NewExecutionPayloadSSZ projects a block and its execution outputs into the
// EIP-7807 schema. RLP block roots and block IDs remain in their native format.
func NewExecutionPayloadSSZ(block *Block, receipts []*Receipt, ctx ExecutionSSZContext) (*ExecutionPayloadSSZ, error) {
	if block == nil {
		return nil, fmt.Errorf("EIP-7807: missing block")
	}
	h := block.Header()
	if h.Number == nil || !h.Number.IsUint64() || h.BaseFee == nil ||
		h.BlobGasUsed == nil || h.ExcessBlobGas == nil || h.ParentBeaconRoot == nil ||
		ctx.BlobBaseFee == nil || len(ctx.BlockAccessList) == 0 || len(block.Uncles()) != 0 {
		return nil, fmt.Errorf("EIP-7807: missing post-merge execution payload fields")
	}
	if len(receipts) != len(block.Transactions()) {
		return nil, fmt.Errorf("EIP-7807: receipt count %d differs from transaction count %d", len(receipts), len(block.Transactions()))
	}
	receiptsRoot, err := ExecutionReceiptsSSZRoot(receipts)
	if err != nil {
		return nil, err
	}
	requestsRoot, err := ExecutionRequestsSSZRoot(ctx.Requests)
	if err != nil {
		return nil, err
	}
	txs := make([][]byte, len(block.Transactions()))
	for i, tx := range block.Transactions() {
		if tx == nil {
			return nil, fmt.Errorf("EIP-7807: nil transaction %d", i)
		}
		txs[i], err = tx.EncodeRLP()
		if err != nil {
			return nil, err
		}
	}
	withdrawals := make([][]byte, len(block.Withdrawals()))
	for i, w := range block.Withdrawals() {
		if w == nil {
			return nil, fmt.Errorf("EIP-7807: nil withdrawal %d", i)
		}
		withdrawals[i] = EncodeWithdrawal(w)
	}
	return &ExecutionPayloadSSZ{
		ExecutionSSZFields: ExecutionSSZFields{
			ParentHash: h.ParentHash, Miner: h.Coinbase, StateRoot: h.Root, ReceiptsRoot: receiptsRoot,
			Number: h.Number.Uint64(), GasLimits: ExecutionGasAmounts{h.GasLimit, ctx.BlobGasLimit},
			GasUsed: ExecutionGasAmounts{h.GasUsed, *h.BlobGasUsed}, Timestamp: h.Time,
			ExtraData: h.Extra, MixHash: h.MixDigest,
			BaseFeesPerGas:        ExecutionBaseFees{h.BaseFee, new(big.Int).Set(ctx.BlobBaseFee)},
			ExcessGas:             ExecutionGasAmounts{ctx.ExcessRegularGas, *h.ExcessBlobGas},
			ParentBeaconBlockRoot: *h.ParentBeaconRoot, RequestsHash: requestsRoot, SlotNumber: ctx.SlotNumber,
		},
		Transactions: txs, Withdrawals: withdrawals, BlockAccessList: bytes.Clone(ctx.BlockAccessList),
	}, nil
}

const executionPayloadMask = 1<<18 - 1

var executionPayloadSizes = [...]int{32, 20, 32, 0, 32, 8, 16, 16, 8, 0, 32, 64, 0, 16, 32, 32, 0, 8}

func executionHeaderSizes() [18]int {
	sizes := executionPayloadSizes
	sizes[3], sizes[12], sizes[16] = 32, 32, 32
	return sizes
}

func encodeExecutionGas(gas ExecutionGasAmounts) []byte {
	return append(ssz.MarshalUint64(gas.Regular), ssz.MarshalUint64(gas.Blob)...)
}

func decodeExecutionGas(data []byte) ExecutionGasAmounts {
	return ExecutionGasAmounts{binary.LittleEndian.Uint64(data), binary.LittleEndian.Uint64(data[8:])}
}

func (f ExecutionSSZFields) parts() ([18][]byte, error) {
	if len(f.ExtraData) > 32 {
		return [18][]byte{}, fmt.Errorf("EIP-7807: extra data exceeds 32 bytes")
	}
	regular, err := sszUint256(f.BaseFeesPerGas.Regular)
	if err != nil {
		return [18][]byte{}, err
	}
	blob, err := sszUint256(f.BaseFeesPerGas.Blob)
	if err != nil {
		return [18][]byte{}, err
	}
	return [18][]byte{
		f.ParentHash[:], f.Miner[:], f.StateRoot[:], nil, f.ReceiptsRoot[:],
		ssz.MarshalUint64(f.Number), encodeExecutionGas(f.GasLimits), encodeExecutionGas(f.GasUsed),
		ssz.MarshalUint64(f.Timestamp), f.ExtraData, f.MixHash[:], append(regular, blob...), nil,
		encodeExecutionGas(f.ExcessGas), f.ParentBeaconBlockRoot[:], f.RequestsHash[:], nil,
		ssz.MarshalUint64(f.SlotNumber),
	}, nil
}

func decodeExecutionSSZFields(f [][]byte) (ExecutionSSZFields, error) {
	if len(f[9]) > 32 {
		return ExecutionSSZFields{}, fmt.Errorf("EIP-7807: extra data exceeds 32 bytes")
	}
	return ExecutionSSZFields{
		ParentHash: BytesToHash(f[0]), Miner: BytesToAddress(f[1]), StateRoot: BytesToHash(f[2]),
		ReceiptsRoot: BytesToHash(f[4]), Number: binary.LittleEndian.Uint64(f[5]),
		GasLimits: decodeExecutionGas(f[6]), GasUsed: decodeExecutionGas(f[7]),
		Timestamp: binary.LittleEndian.Uint64(f[8]), ExtraData: f[9], MixHash: BytesToHash(f[10]),
		BaseFeesPerGas: ExecutionBaseFees{bigIntFrom32LE(f[11][:32]), bigIntFrom32LE(f[11][32:])},
		ExcessGas:      decodeExecutionGas(f[13]), ParentBeaconBlockRoot: BytesToHash(f[14]),
		RequestsHash: BytesToHash(f[15]), SlotNumber: binary.LittleEndian.Uint64(f[17]),
	}, nil
}

func (p ExecutionPayloadSSZ) MarshalSSZ() ([]byte, error) {
	f, err := p.ExecutionSSZFields.parts()
	if err != nil {
		return nil, err
	}
	f[3], err = marshalExecutionSSZList(p.Transactions)
	if err != nil {
		return nil, err
	}
	f[12], err = marshalExecutionSSZList(p.Withdrawals)
	if err != nil {
		return nil, err
	}
	f[16] = p.BlockAccessList
	return marshalExecutionSSZFields(f[:], executionPayloadSizes[:])
}

func (p *ExecutionPayloadSSZ) UnmarshalSSZ(data []byte) error {
	parts, err := ssz.UnmarshalVariableContainer(data, len(executionPayloadSizes), executionPayloadSizes[:])
	if err != nil {
		return err
	}
	fields, err := decodeExecutionSSZFields(parts)
	if err != nil {
		return err
	}
	txs, err := unmarshalExecutionSSZList(parts[3])
	if err != nil {
		return err
	}
	withdrawals, err := unmarshalExecutionSSZList(parts[12])
	if err != nil {
		return err
	}
	*p = ExecutionPayloadSSZ{ExecutionSSZFields: fields, Transactions: txs, Withdrawals: withdrawals, BlockAccessList: parts[16]}
	return nil
}

// Header summarizes the payload by replacing the transaction, withdrawal and
// block access list fields with their tree roots.
func (p ExecutionPayloadSSZ) Header() ExecutionHeaderSSZ {
	return ExecutionHeaderSSZ{
		ExecutionSSZFields:  p.ExecutionSSZFields,
		TransactionsRoot:    executionEnvelopeRoot(p.Transactions),
		WithdrawalsRoot:     executionEnvelopeRoot(p.Withdrawals),
		BlockAccessListRoot: Hash(progressiveBytesRoot(p.BlockAccessList)),
	}
}

func (p ExecutionPayloadSSZ) HashTreeRoot() (Hash, error) {
	return p.Header().HashTreeRoot()
}

func (h ExecutionHeaderSSZ) parts() ([18][]byte, error) {
	f, err := h.ExecutionSSZFields.parts()
	f[3], f[12], f[16] = h.TransactionsRoot[:], h.WithdrawalsRoot[:], h.BlockAccessListRoot[:]
	return f, err
}

func (h ExecutionHeaderSSZ) MarshalSSZ() ([]byte, error) {
	fields, err := h.parts()
	if err != nil {
		return nil, err
	}
	sizes := executionHeaderSizes()
	return marshalExecutionSSZFields(fields[:], sizes[:])
}

func (h *ExecutionHeaderSSZ) UnmarshalSSZ(data []byte) error {
	sizes := executionHeaderSizes()
	parts, err := ssz.UnmarshalVariableContainer(data, len(sizes), sizes[:])
	if err != nil {
		return err
	}
	fields, err := decodeExecutionSSZFields(parts)
	if err != nil {
		return err
	}
	*h = ExecutionHeaderSSZ{fields, BytesToHash(parts[3]), BytesToHash(parts[12]), BytesToHash(parts[16])}
	return nil
}

func (h ExecutionHeaderSSZ) HashTreeRoot() (Hash, error) {
	f, err := h.parts()
	if err != nil {
		return Hash{}, err
	}
	var roots [18][32]byte
	for i := range roots {
		switch i {
		case 6, 7, 13:
			amounts := decodeExecutionGas(f[i])
			roots[i] = executionProgressiveRoot([][32]byte{ssz.HashTreeRootUint64(amounts.Regular), ssz.HashTreeRootUint64(amounts.Blob)}, 3)
		case 9:
			roots[i] = ssz.HashTreeRootByteList(f[i], 32)
		case 11:
			roots[i] = executionProgressiveRoot(ssz.Pack(f[i]), 3)
		default:
			copy(roots[i][:], f[i])
		}
	}
	return Hash(executionProgressiveRoot(roots[:], executionPayloadMask)), nil
}

func executionEnvelopeRoot(elements [][]byte) Hash {
	roots := make([][32]byte, len(elements))
	for i, raw := range elements {
		roots[i] = progressiveBytesRoot(raw)
	}
	return Hash(ssz.HashTreeRootProgressiveList(roots))
}

// ExecutionReceiptsSSZRoot computes the EIP-7807 root of existing receipt envelopes.
func ExecutionReceiptsSSZRoot(receipts []*Receipt) (Hash, error) {
	elements := make([][]byte, len(receipts))
	for i, receipt := range receipts {
		if receipt == nil {
			return Hash{}, fmt.Errorf("EIP-7807: nil receipt %d", i)
		}
		raw, err := receipt.EncodeRLP()
		if err != nil {
			return Hash{}, err
		}
		elements[i] = raw
	}
	return executionEnvelopeRoot(elements), nil
}

// ExecutionRequestsSSZRoot is the consensus ExecutionRequests root over the
// three Electra request lists, reconstructed from EIP-7685 request groups.
func ExecutionRequestsSSZRoot(requests Requests) (Hash, error) {
	fieldSizes := [3][]int{{48, 32, 8, 96, 8}, {20, 48, 8}, {20, 48, 48}}
	elementSizes := [3]int{DepositRequestSSZSize, WithdrawalRequestSSZSize, ConsolidationRequestSSZSize}
	limits := [3]int{MaxDepositsPerPayload, MaxWithdrawalRequestsPerPayload, MaxConsolidationRequestsPerPayload}
	var lists [3][][32]byte
	for _, request := range requests {
		if request == nil || int(request.Type) >= len(lists) {
			return Hash{}, fmt.Errorf("EIP-7807: unknown execution request type")
		}
		i := int(request.Type)
		size := elementSizes[i]
		if len(request.Data) == 0 || len(request.Data)%size != 0 || len(lists[i])+len(request.Data)/size > limits[i] {
			return Hash{}, fmt.Errorf("EIP-7807: invalid execution request data for type %d", i)
		}
		for at := 0; at < len(request.Data); {
			var fields [][32]byte
			for _, width := range fieldSizes[i] {
				fields = append(fields, ssz.Merkleize(ssz.Pack(request.Data[at:at+width]), 0))
				at += width
			}
			lists[i] = append(lists[i], ssz.HashTreeRootContainer(fields))
		}
	}
	var roots [3][32]byte
	for i := range roots {
		roots[i] = ssz.HashTreeRootList(lists[i], limits[i])
	}
	return Hash(ssz.HashTreeRootContainer(roots[:])), nil
}
