package types

import (
	"errors"
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/rlp"
	"golang.org/x/crypto/sha3"
)

// EIP-8141 Frame Transaction constants.
const (
	FrameTxType          byte   = 0x06
	FrameTxIntrinsicCost uint64 = 15000
	MaxFrames            int    = 1000

	// Frame modes.
	ModeDefault uint8 = 0
	ModeVerify  uint8 = 1
	ModeSender  uint8 = 2
)

// EntryPointAddress is the canonical caller for DEFAULT and VERIFY frames.
var EntryPointAddress = HexToAddress("0x00000000000000000000000000000000000000aa")

// Frame represents a single execution frame within a Frame transaction.
type Frame struct {
	Mode     uint8
	Target   *Address
	GasLimit uint64
	Data     []byte
}

// FrameTx represents an EIP-8141 (type 0x06) frame transaction.
// Per EIP-8141: chain_id < 2**256, nonce < 2**64.
// We use uint64 for nonce (per spec) and *big.Int for ChainID (for chain compatibility).
type FrameTx struct {
	ChainID              *big.Int
	Nonce                uint64 // EIP-8141: nonce < 2**64
	Sender               Address
	Frames               []Frame
	MaxPriorityFeePerGas *big.Int
	MaxFeePerGas         *big.Int
	MaxFeePerBlobGas     *big.Int
	BlobVersionedHashes  []Hash
}

// TxData interface implementation for FrameTx.
func (tx *FrameTx) txType() byte           { return FrameTxType }
func (tx *FrameTx) chainID() *big.Int      { return tx.ChainID }
func (tx *FrameTx) accessList() AccessList { return nil }
func (tx *FrameTx) data() []byte           { return nil }
func (tx *FrameTx) gas() uint64            { return CalcFrameTxGas(tx) }
func (tx *FrameTx) gasPrice() *big.Int     { return tx.MaxFeePerGas }
func (tx *FrameTx) gasTipCap() *big.Int    { return tx.MaxPriorityFeePerGas }
func (tx *FrameTx) gasFeeCap() *big.Int    { return tx.MaxFeePerGas }
func (tx *FrameTx) value() *big.Int        { return new(big.Int) }
func (tx *FrameTx) nonce() uint64 {
	return tx.Nonce
}
func (tx *FrameTx) to() *Address { return nil }

func (tx *FrameTx) copy() TxData {
	cpy := &FrameTx{
		Nonce:  tx.Nonce,
		Sender: tx.Sender,
	}
	if tx.ChainID != nil {
		cpy.ChainID = new(big.Int).Set(tx.ChainID)
	}
	if tx.MaxPriorityFeePerGas != nil {
		cpy.MaxPriorityFeePerGas = new(big.Int).Set(tx.MaxPriorityFeePerGas)
	}
	if tx.MaxFeePerGas != nil {
		cpy.MaxFeePerGas = new(big.Int).Set(tx.MaxFeePerGas)
	}
	if tx.MaxFeePerBlobGas != nil {
		cpy.MaxFeePerBlobGas = new(big.Int).Set(tx.MaxFeePerBlobGas)
	}
	if tx.Frames != nil {
		cpy.Frames = make([]Frame, len(tx.Frames))
		for i, f := range tx.Frames {
			cpy.Frames[i] = Frame{
				Mode:     f.Mode,
				Target:   copyAddressPtr(f.Target),
				GasLimit: f.GasLimit,
				Data:     copyBytes(f.Data),
			}
		}
	}
	if tx.BlobVersionedHashes != nil {
		cpy.BlobVersionedHashes = make([]Hash, len(tx.BlobVersionedHashes))
		copy(cpy.BlobVersionedHashes, tx.BlobVersionedHashes)
	}
	return cpy
}

// --- RLP encoding/decoding ---

// frameRLP is the RLP encoding layout for a single frame.
// Fields: [mode, target, gas_limit, data]
type frameRLP struct {
	Mode     uint8
	Target   []byte // empty for nil (defaults to sender), 20 bytes otherwise
	GasLimit uint64
	Data     []byte
}

// frameTxRLP is the RLP encoding layout for FrameTx.
// Fields: [chain_id, nonce, sender, frames, max_priority_fee_per_gas, max_fee_per_gas, max_fee_per_blob_gas, blob_versioned_hashes]
// Per EIP-8141: nonce < 2**64, so we use uint64 for RLP encoding.
type frameTxRLP struct {
	ChainID              *big.Int
	Nonce                uint64
	Sender               Address
	Frames               []frameRLP
	MaxPriorityFeePerGas *big.Int
	MaxFeePerGas         *big.Int
	MaxFeePerBlobGas     *big.Int
	BlobVersionedHashes  []Hash
}

// EncodeFrameTx encodes a FrameTx as a typed transaction envelope: 0x06 || RLP([...]).
func EncodeFrameTx(tx *FrameTx) ([]byte, error) {
	enc := frameTxRLP{
		ChainID:              bigOrZero(tx.ChainID),
		Nonce:                tx.Nonce,
		Sender:               tx.Sender,
		Frames:               encodeFrames(tx.Frames),
		MaxPriorityFeePerGas: bigOrZero(tx.MaxPriorityFeePerGas),
		MaxFeePerGas:         bigOrZero(tx.MaxFeePerGas),
		MaxFeePerBlobGas:     bigOrZero(tx.MaxFeePerBlobGas),
		BlobVersionedHashes:  tx.BlobVersionedHashes,
	}
	if enc.BlobVersionedHashes == nil {
		enc.BlobVersionedHashes = []Hash{}
	}
	payload, err := rlp.EncodeToBytes(enc)
	if err != nil {
		return nil, err
	}
	result := make([]byte, 1+len(payload))
	result[0] = FrameTxType
	copy(result[1:], payload)
	return result, nil
}

// DecodeFrameTx decodes the RLP payload (without the type byte) into a FrameTx.
func DecodeFrameTx(data []byte) (*FrameTx, error) {
	var dec frameTxRLP
	if err := rlp.DecodeBytes(data, &dec); err != nil {
		return nil, fmt.Errorf("decode frame tx: %w", err)
	}
	tx := &FrameTx{
		ChainID:              dec.ChainID,
		Nonce:                dec.Nonce,
		Sender:               dec.Sender,
		Frames:               decodeFrames(dec.Frames),
		MaxPriorityFeePerGas: dec.MaxPriorityFeePerGas,
		MaxFeePerGas:         dec.MaxFeePerGas,
		MaxFeePerBlobGas:     dec.MaxFeePerBlobGas,
		BlobVersionedHashes:  dec.BlobVersionedHashes,
	}
	return tx, nil
}

func encodeFrames(frames []Frame) []frameRLP {
	if frames == nil {
		return nil
	}
	out := make([]frameRLP, len(frames))
	for i, f := range frames {
		out[i] = frameRLP{
			Mode:     f.Mode,
			Target:   addressPtrToBytes(f.Target),
			GasLimit: f.GasLimit,
			Data:     f.Data,
		}
	}
	return out
}

func decodeFrames(frames []frameRLP) []Frame {
	if frames == nil {
		return nil
	}
	out := make([]Frame, len(frames))
	for i, f := range frames {
		out[i] = Frame{
			Mode:     f.Mode,
			Target:   bytesToAddressPtr(f.Target),
			GasLimit: f.GasLimit,
			Data:     f.Data,
		}
	}
	return out
}

// --- Signature hash ---

// ComputeFrameSigHash computes the canonical signature hash for a FrameTx.
// VERIFY frames have their data elided (set to empty) before hashing.
// Result: keccak256(0x06 || rlp(tx_with_elided_verify_data))
func ComputeFrameSigHash(tx *FrameTx) Hash {
	// Build a copy with VERIFY frame data elided.
	frames := make([]frameRLP, len(tx.Frames))
	for i, f := range tx.Frames {
		frames[i] = frameRLP{
			Mode:     f.Mode,
			Target:   addressPtrToBytes(f.Target),
			GasLimit: f.GasLimit,
		}
		if f.Mode == ModeVerify {
			frames[i].Data = []byte{}
		} else {
			frames[i].Data = f.Data
		}
	}
	enc := frameTxRLP{
		ChainID:              bigOrZero(tx.ChainID),
		Nonce:                tx.Nonce,
		Sender:               tx.Sender,
		Frames:               frames,
		MaxPriorityFeePerGas: bigOrZero(tx.MaxPriorityFeePerGas),
		MaxFeePerGas:         bigOrZero(tx.MaxFeePerGas),
		MaxFeePerBlobGas:     bigOrZero(tx.MaxFeePerBlobGas),
		BlobVersionedHashes:  tx.BlobVersionedHashes,
	}
	if enc.BlobVersionedHashes == nil {
		enc.BlobVersionedHashes = []Hash{}
	}
	payload, err := rlp.EncodeToBytes(enc)
	if err != nil {
		return Hash{}
	}
	d := sha3.NewLegacyKeccak256()
	d.Write([]byte{FrameTxType})
	d.Write(payload)
	var h Hash
	copy(h[:], d.Sum(nil))
	return h
}

// --- Validation ---

// ValidateFrameTx performs static validity checks on a FrameTx per EIP-8141 constraints.
func ValidateFrameTx(tx *FrameTx) error {
	if len(tx.Frames) == 0 {
		return errors.New("frame tx: must have at least one frame")
	}
	if len(tx.Frames) > MaxFrames {
		return fmt.Errorf("frame tx: too many frames (%d > %d)", len(tx.Frames), MaxFrames)
	}
	if tx.ChainID != nil && tx.ChainID.Sign() < 0 {
		return errors.New("frame tx: negative chain ID")
	}
	// Nonce is uint64, always >= 0, no validation needed per EIP-8141 (nonce < 2**64)
	for i, f := range tx.Frames {
		if f.Mode > 2 {
			return fmt.Errorf("frame %d: invalid mode %d", i, f.Mode)
		}
		if f.Target != nil && len(f.Target) != AddressLength {
			return fmt.Errorf("frame %d: invalid target length %d", i, len(f.Target))
		}
	}
	// If no blobs, max_fee_per_blob_gas must be zero and blob_versioned_hashes must be empty.
	if len(tx.BlobVersionedHashes) == 0 {
		if tx.MaxFeePerBlobGas != nil && tx.MaxFeePerBlobGas.Sign() > 0 {
			return errors.New("frame tx: max_fee_per_blob_gas must be 0 when no blobs")
		}
	}
	return nil
}

// --- Gas calculation ---

// CalcFrameTxGas calculates the total gas limit of a frame transaction per EIP-8141:
// tx_gas_limit = FRAME_TX_INTRINSIC_COST + calldata_cost(rlp(tx.frames)) + sum(frame.gas_limit)
func CalcFrameTxGas(tx *FrameTx) uint64 {
	gas := FrameTxIntrinsicCost

	// Encode frames for calldata cost calculation.
	framesRLP := encodeFrames(tx.Frames)
	if framesRLP != nil {
		if encoded, err := rlp.EncodeToBytes(framesRLP); err == nil {
			gas += CalldataTokenGas(encoded)
		}
	}

	// Sum per-frame gas limits.
	for _, f := range tx.Frames {
		gas += f.GasLimit
	}
	return gas
}
