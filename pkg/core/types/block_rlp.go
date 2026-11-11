package types

import (
	"fmt"

	"github.com/eth2030/eth2030/rlp"
)

// EncodeRLP returns the RLP encoding of the block:
// [header, [tx...], [uncle...]] for pre-Shanghai blocks
// [header, [tx...], [uncle...], [withdrawal...]] for post-Shanghai blocks
//
// Legacy transactions are encoded as RLP lists directly; typed transactions
// are wrapped as RLP byte strings (type_byte || RLP_payload), matching geth.
func (b *Block) EncodeRLP() ([]byte, error) {
	headerEnc, err := b.header.EncodeRLP()
	if err != nil {
		return nil, fmt.Errorf("encoding header: %w", err)
	}

	// Encode transactions list.
	// Legacy txs are RLP lists; typed txs are byte strings (type || payload).
	var txsPayload []byte
	for i, tx := range b.body.Transactions {
		txEnc, err := tx.EncodeRLP()
		if err != nil {
			return nil, fmt.Errorf("encoding tx %d: %w", i, err)
		}
		if tx.Type() == LegacyTxType {
			// Legacy tx: txEnc is already an RLP list; append directly.
			txsPayload = append(txsPayload, txEnc...)
		} else {
			// Typed tx: wrap as RLP byte string (type_byte || RLP_payload).
			wrapped, err := rlp.EncodeToBytes(txEnc)
			if err != nil {
				return nil, fmt.Errorf("wrapping tx %d: %w", i, err)
			}
			txsPayload = append(txsPayload, wrapped...)
		}
	}

	// Encode uncles list.
	var unclesPayload []byte
	for _, uncle := range b.body.Uncles {
		uncleEnc, err := uncle.EncodeRLP()
		if err != nil {
			return nil, fmt.Errorf("encoding uncle: %w", err)
		}
		unclesPayload = append(unclesPayload, uncleEnc...)
	}

	var blockPayload []byte
	blockPayload = append(blockPayload, headerEnc...)
	blockPayload = append(blockPayload, rlp.WrapList(txsPayload)...)
	blockPayload = append(blockPayload, rlp.WrapList(unclesPayload)...)

	// Withdrawals list (post-Shanghai, rlp:"optional").
	// A non-nil slice (even empty) indicates post-Shanghai.
	if b.body.Withdrawals != nil {
		var wPayload []byte
		for _, w := range b.body.Withdrawals {
			wEnc := EncodeWithdrawal(w)
			wPayload = append(wPayload, wEnc...)
		}
		blockPayload = append(blockPayload, rlp.WrapList(wPayload)...)
	}

	return rlp.WrapList(blockPayload), nil
}

// RLPEncodedSize returns the exact length of EncodeRLP without assembling the block.
func (b *Block) RLPEncodedSize() (int, error) {
	header, err := b.header.EncodeRLP()
	if err != nil {
		return 0, err
	}
	var txBytes, uncleBytes, withdrawalBytes int
	for _, tx := range b.body.Transactions {
		encoded, err := tx.EncodeRLP()
		if err != nil {
			return 0, err
		}
		size := len(encoded)
		if tx.Type() != LegacyTxType {
			size = rlp.EstimateStringSize(size)
		}
		txBytes += size
	}
	for _, uncle := range b.body.Uncles {
		encoded, err := uncle.EncodeRLP()
		if err != nil {
			return 0, err
		}
		uncleBytes += len(encoded)
	}
	size := len(header) + rlp.EstimateListSize(txBytes) + rlp.EstimateListSize(uncleBytes)
	if b.body.Withdrawals != nil {
		for _, w := range b.body.Withdrawals {
			withdrawalBytes += len(EncodeWithdrawal(w))
		}
		size += rlp.EstimateListSize(withdrawalBytes)
	}
	return rlp.EstimateListSize(size), nil
}

// DecodeBlockRLP decodes an RLP-encoded block.
func DecodeBlockRLP(data []byte) (*Block, error) {
	s := rlp.NewStreamFromBytes(data)
	_, err := s.List()
	if err != nil {
		return nil, fmt.Errorf("opening block list: %w", err)
	}

	// Decode header.
	headerBytes, err := s.RawItem()
	if err != nil {
		return nil, fmt.Errorf("reading header: %w", err)
	}
	header, err := DecodeHeaderRLP(headerBytes)
	if err != nil {
		return nil, fmt.Errorf("decoding header: %w", err)
	}

	// Decode transactions list.
	_, err = s.List()
	if err != nil {
		return nil, fmt.Errorf("opening txs list: %w", err)
	}
	var txs []*Transaction
	for !s.AtListEnd() {
		// Legacy txs appear as RLP lists; typed txs as byte strings.
		kind, _, err := s.Kind()
		if err != nil {
			return nil, fmt.Errorf("peeking tx kind: %w", err)
		}
		var txData []byte
		if kind == rlp.List {
			// Legacy transaction: read entire RLP list item.
			txData, err = s.RawItem()
		} else {
			// Typed transaction: read byte string (type_byte || RLP_payload).
			txData, err = s.Bytes()
		}
		if err != nil {
			return nil, fmt.Errorf("reading tx: %w", err)
		}
		tx, err := DecodeTxRLP(txData)
		if err != nil {
			return nil, fmt.Errorf("decoding tx: %w", err)
		}
		txs = append(txs, tx)
	}
	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing txs list: %w", err)
	}

	// Decode uncles list.
	_, err = s.List()
	if err != nil {
		return nil, fmt.Errorf("opening uncles list: %w", err)
	}
	var uncles []*Header
	for !s.AtListEnd() {
		uncleBytes, err := s.RawItem()
		if err != nil {
			return nil, fmt.Errorf("reading uncle: %w", err)
		}
		uncle, err := DecodeHeaderRLP(uncleBytes)
		if err != nil {
			return nil, fmt.Errorf("decoding uncle: %w", err)
		}
		uncles = append(uncles, uncle)
	}
	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing uncles list: %w", err)
	}

	block := &Block{header: header}
	block.body.Transactions = txs
	block.body.Uncles = uncles

	// Decode withdrawals list (optional, post-Shanghai).
	if !s.AtListEnd() {
		_, err = s.List()
		if err != nil {
			return nil, fmt.Errorf("opening withdrawals list: %w", err)
		}
		withdrawals := make([]*Withdrawal, 0)
		for !s.AtListEnd() {
			wBytes, err := s.RawItem()
			if err != nil {
				return nil, fmt.Errorf("reading withdrawal: %w", err)
			}
			w, err := DecodeWithdrawal(wBytes)
			if err != nil {
				return nil, fmt.Errorf("decoding withdrawal: %w", err)
			}
			withdrawals = append(withdrawals, w)
		}
		if err := s.ListEnd(); err != nil {
			return nil, fmt.Errorf("closing withdrawals list: %w", err)
		}
		block.body.Withdrawals = withdrawals
	}

	if err := s.ListEnd(); err != nil {
		return nil, fmt.Errorf("closing block list: %w", err)
	}

	return block, nil
}
