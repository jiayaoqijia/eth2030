package types

import (
	"fmt"
	"math/big"

	"github.com/eth2030/eth2030/rlp"
	"golang.org/x/crypto/sha3"
)

// emptyRLP is the RLP encoding of an empty/nil optional field (0x80).
var emptyRLP = []byte{0x80}

// EIP7706HashFields controls whether CalldataGasUsed and CalldataExcessGas
// (EIP-7706) are included in the canonical block-header RLP and therefore
// in the block hash.
//
// Default false: hash is compatible with Lighthouse and go-ethereum, which
// do not yet implement EIP-7706 and therefore do not encode these fields.
// Set to true only when running a fully EIP-7706-aware network where all
// peers (CL and EL) agree to include the fields.
var EIP7706HashFields = false

// EncodeRLP returns the RLP encoding of the header in Yellow Paper field order:
// [ParentHash, UncleHash, Coinbase, Root, TxHash, ReceiptHash, Bloom,
//
//	Difficulty, Number, GasLimit, GasUsed, Time, Extra, MixDigest, Nonce,
//	BaseFee, WithdrawalsHash, BlobGasUsed, ExcessBlobGas, ParentBeaconRoot,
//	RequestsHash, CalldataGasUsed, CalldataExcessGas]
//
// Optional fields are appended only when needed.  When a later optional field
// is non-nil, earlier nil optional fields are written as 0x80 (empty string)
// to preserve positional integrity, matching go-ethereum's gen_header_rlp.go.
func (h *Header) EncodeRLP() ([]byte, error) {
	var payload []byte

	// appendItem RLP-encodes v and appends to payload.
	appendItem := func(v interface{}) error {
		enc, err := rlp.EncodeToBytes(v)
		if err != nil {
			return err
		}
		payload = append(payload, enc...)
		return nil
	}
	// appendEmpty appends an RLP empty string (0x80) as a nil-field placeholder.
	appendEmpty := func() {
		payload = append(payload, emptyRLP...)
	}

	// 15 mandatory base fields.
	if err := appendItem(h.ParentHash); err != nil {
		return nil, err
	}
	if err := appendItem(h.UncleHash); err != nil {
		return nil, err
	}
	if err := appendItem(h.Coinbase); err != nil {
		return nil, err
	}
	if err := appendItem(h.Root); err != nil {
		return nil, err
	}
	if err := appendItem(h.TxHash); err != nil {
		return nil, err
	}
	if err := appendItem(h.ReceiptHash); err != nil {
		return nil, err
	}
	if err := appendItem(h.Bloom); err != nil {
		return nil, err
	}
	if err := appendItem(bigIntOrZero(h.Difficulty)); err != nil {
		return nil, err
	}
	if err := appendItem(bigIntOrZero(h.Number)); err != nil {
		return nil, err
	}
	if err := appendItem(h.GasLimit); err != nil {
		return nil, err
	}
	if err := appendItem(h.GasUsed); err != nil {
		return nil, err
	}
	if err := appendItem(h.Time); err != nil {
		return nil, err
	}
	if err := appendItem(h.Extra); err != nil {
		return nil, err
	}
	if err := appendItem(h.MixDigest); err != nil {
		return nil, err
	}
	if err := appendItem(h.Nonce); err != nil {
		return nil, err
	}

	// Determine which optional fields are present.
	hasBaseFee := h.BaseFee != nil
	hasWithdrawals := h.WithdrawalsHash != nil
	hasBlobGasUsed := h.BlobGasUsed != nil
	hasExcessBlobGas := h.ExcessBlobGas != nil
	hasBeaconRoot := h.ParentBeaconRoot != nil
	hasRequestsHash := h.RequestsHash != nil
	hasBALHash := h.BlockAccessListHash != nil
	hasCalldataUsed := EIP7706HashFields && h.CalldataGasUsed != nil
	hasCalldataExcess := EIP7706HashFields && h.CalldataExcessGas != nil

	// anyFrom[i] is true when any optional field at or after position i is set.
	// This mirrors go-ethereum's generated encoder logic.
	anyFromCalldataExcess := hasCalldataExcess
	anyFromCalldataUsed := hasCalldataUsed || anyFromCalldataExcess
	anyFromBAL := hasBALHash || anyFromCalldataUsed
	anyFromRequests := hasRequestsHash || anyFromBAL
	anyFromBeacon := hasBeaconRoot || anyFromRequests
	anyFromExcessBlob := hasExcessBlobGas || anyFromBeacon
	anyFromBlobUsed := hasBlobGasUsed || anyFromExcessBlob
	anyFromWithdrawals := hasWithdrawals || anyFromBlobUsed
	anyFromBaseFee := hasBaseFee || anyFromWithdrawals

	if !anyFromBaseFee {
		return rlp.WrapList(payload), nil
	}

	// EIP-1559: BaseFee
	if hasBaseFee {
		if err := appendItem(h.BaseFee); err != nil {
			return nil, err
		}
	} else {
		appendEmpty()
	}
	if !anyFromWithdrawals {
		return rlp.WrapList(payload), nil
	}

	// EIP-4895: WithdrawalsHash
	if hasWithdrawals {
		if err := appendItem(*h.WithdrawalsHash); err != nil {
			return nil, err
		}
	} else {
		appendEmpty()
	}
	if !anyFromBlobUsed {
		return rlp.WrapList(payload), nil
	}

	// EIP-4844: BlobGasUsed
	if hasBlobGasUsed {
		if err := appendItem(*h.BlobGasUsed); err != nil {
			return nil, err
		}
	} else {
		appendEmpty()
	}
	if !anyFromExcessBlob {
		return rlp.WrapList(payload), nil
	}

	// EIP-4844: ExcessBlobGas
	if hasExcessBlobGas {
		if err := appendItem(*h.ExcessBlobGas); err != nil {
			return nil, err
		}
	} else {
		appendEmpty()
	}
	if !anyFromBeacon {
		return rlp.WrapList(payload), nil
	}

	// EIP-4788: ParentBeaconBlockRoot
	if hasBeaconRoot {
		if err := appendItem(*h.ParentBeaconRoot); err != nil {
			return nil, err
		}
	} else {
		appendEmpty()
	}
	if !anyFromRequests {
		return rlp.WrapList(payload), nil
	}

	// EIP-7685: RequestsHash
	if hasRequestsHash {
		if err := appendItem(*h.RequestsHash); err != nil {
			return nil, err
		}
	} else {
		appendEmpty()
	}
	if !anyFromBAL {
		return rlp.WrapList(payload), nil
	}
	// EIP-7928: block_access_list_hash follows requests_hash.
	if hasBALHash {
		if err := appendItem(*h.BlockAccessListHash); err != nil {
			return nil, err
		}
	} else {
		appendEmpty()
	}
	if !anyFromCalldataUsed {
		return rlp.WrapList(payload), nil
	}

	// EIP-7706: CalldataGasUsed (only when EIP7706HashFields is enabled)
	if hasCalldataUsed {
		if err := appendItem(*h.CalldataGasUsed); err != nil {
			return nil, err
		}
	} else {
		appendEmpty()
	}
	if !anyFromCalldataExcess {
		return rlp.WrapList(payload), nil
	}

	// EIP-7706: CalldataExcessGas (only when EIP7706HashFields is enabled)
	if hasCalldataExcess {
		if err := appendItem(*h.CalldataExcessGas); err != nil {
			return nil, err
		}
	} else {
		appendEmpty()
	}
	return rlp.WrapList(payload), nil
}

// bigIntOrZero returns v if non-nil, otherwise a zero big.Int.
func bigIntOrZero(v *big.Int) *big.Int {
	if v == nil {
		return new(big.Int)
	}
	return v
}

// DecodeHeaderRLP decodes an RLP-encoded header.
func DecodeHeaderRLP(data []byte) (*Header, error) {
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, err
	}

	h := &Header{}

	// 15 base fields
	if err := decodeHash(s, &h.ParentHash); err != nil {
		return nil, err
	}
	if err := decodeHash(s, &h.UncleHash); err != nil {
		return nil, err
	}
	if err := decodeAddress(s, &h.Coinbase); err != nil {
		return nil, err
	}
	if err := decodeHash(s, &h.Root); err != nil {
		return nil, err
	}
	if err := decodeHash(s, &h.TxHash); err != nil {
		return nil, err
	}
	if err := decodeHash(s, &h.ReceiptHash); err != nil {
		return nil, err
	}
	if err := decodeBloom(s, &h.Bloom); err != nil {
		return nil, err
	}

	h.Difficulty, err = s.BigInt()
	if err != nil {
		return nil, err
	}
	h.Number, err = s.BigInt()
	if err != nil {
		return nil, err
	}
	h.GasLimit, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	h.GasUsed, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	h.Time, err = s.Uint64()
	if err != nil {
		return nil, err
	}
	h.Extra, err = s.Bytes()
	if err != nil {
		return nil, err
	}
	if err := decodeHash(s, &h.MixDigest); err != nil {
		return nil, err
	}
	if err := decodeBlockNonce(s, &h.Nonce); err != nil {
		return nil, err
	}

	// Optional fields: read each in sequence; stop at list end.
	// A field encoded as 0x80 (empty string / nil placeholder) has len 0.

	// EIP-1559: BaseFee (*big.Int)
	// If this position is reached, the field is present (0x80 = value 0, not nil).
	if s.AtListEnd() {
		return finishHeader(s, h)
	}
	h.BaseFee, err = s.BigInt()
	if err != nil {
		return nil, err
	}

	// EIP-4895: WithdrawalsHash (*Hash)
	if s.AtListEnd() {
		return finishHeader(s, h)
	}
	var whBytes []byte
	whBytes, err = s.Bytes()
	if err != nil {
		return nil, err
	}
	if len(whBytes) == HashLength {
		wh := BytesToHash(whBytes)
		h.WithdrawalsHash = &wh
	}

	// EIP-4844: BlobGasUsed (*uint64)
	// If this position is reached, field is present. 0x80 = value 0 (not nil).
	if s.AtListEnd() {
		return finishHeader(s, h)
	}
	bgu, err := s.Uint64()
	if err != nil {
		return nil, err
	}
	h.BlobGasUsed = &bgu

	// EIP-4844: ExcessBlobGas (*uint64)
	if s.AtListEnd() {
		return finishHeader(s, h)
	}
	ebg, err := s.Uint64()
	if err != nil {
		return nil, err
	}
	h.ExcessBlobGas = &ebg

	// EIP-4788: ParentBeaconBlockRoot (*Hash)
	if s.AtListEnd() {
		return finishHeader(s, h)
	}
	var pbrBytes []byte
	pbrBytes, err = s.Bytes()
	if err != nil {
		return nil, err
	}
	if len(pbrBytes) == HashLength {
		pbr := BytesToHash(pbrBytes)
		h.ParentBeaconRoot = &pbr
	}

	// EIP-7685: RequestsHash (*Hash)
	if s.AtListEnd() {
		return finishHeader(s, h)
	}
	var rhBytes []byte
	rhBytes, err = s.Bytes()
	if err != nil {
		return nil, err
	}
	if len(rhBytes) == HashLength {
		rh := BytesToHash(rhBytes)
		h.RequestsHash = &rh
	}

	if s.AtListEnd() {
		return finishHeader(s, h)
	}
	balBytes, err := s.Bytes()
	if err != nil {
		return nil, err
	}
	if len(balBytes) == HashLength {
		hash := BytesToHash(balBytes)
		h.BlockAccessListHash = &hash
	} else if len(balBytes) != 0 {
		return nil, fmt.Errorf("header: invalid BAL hash length %d", len(balBytes))
	}

	// EIP-7706: CalldataGasUsed / CalldataExcessGas — only decoded when the
	// EIP7706HashFields flag is enabled (off by default for Lighthouse compat).
	if !EIP7706HashFields || s.AtListEnd() {
		return finishHeader(s, h)
	}
	cgu, err := s.Uint64()
	if err != nil {
		return nil, err
	}
	h.CalldataGasUsed = &cgu

	if s.AtListEnd() {
		return finishHeader(s, h)
	}
	ceg, err := s.Uint64()
	if err != nil {
		return nil, err
	}
	h.CalldataExcessGas = &ceg

	return finishHeader(s, h)
}

func finishHeader(s *rlp.Stream, h *Header) (*Header, error) {
	if err := s.ListEnd(); err != nil {
		return nil, err
	}
	return h, nil
}

// decodeHash reads an RLP string into a Hash.
func decodeHash(s *rlp.Stream, h *Hash) error {
	b, err := s.Bytes()
	if err != nil {
		return err
	}
	copy(h[HashLength-len(b):], b)
	return nil
}

// decodeAddress reads an RLP string into an Address.
func decodeAddress(s *rlp.Stream, a *Address) error {
	b, err := s.Bytes()
	if err != nil {
		return err
	}
	copy(a[AddressLength-len(b):], b)
	return nil
}

// decodeBloom reads an RLP string into a Bloom.
func decodeBloom(s *rlp.Stream, bl *Bloom) error {
	b, err := s.Bytes()
	if err != nil {
		return err
	}
	copy(bl[BloomLength-len(b):], b)
	return nil
}

// decodeBlockNonce reads an RLP string into a BlockNonce.
func decodeBlockNonce(s *rlp.Stream, n *BlockNonce) error {
	b, err := s.Bytes()
	if err != nil {
		return err
	}
	copy(n[NonceLength-len(b):], b)
	return nil
}

// computeHeaderHash computes the Keccak-256 hash of the RLP-encoded header.
func computeHeaderHash(h *Header) Hash {
	enc, err := h.EncodeRLP()
	if err != nil {
		return Hash{}
	}
	d := sha3.NewLegacyKeccak256()
	d.Write(enc)
	var hash Hash
	copy(hash[:], d.Sum(nil))
	return hash
}
