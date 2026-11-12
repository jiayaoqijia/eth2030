package ethproto

import (
	"fmt"
	"github.com/eth2030/eth2030/core/types"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
)

type wireReceipts struct {
	ID    uint64
	Pages [][]gethrlp.RawValue
}
type wireReceiptLog struct {
	Address types.Address
	Topics  []types.Hash
	Data    []byte
}
type wireReceipt70 struct {
	Type          uint8
	State         gethrlp.RawValue
	CumulativeGas uint64
	Logs          []wireReceiptLog
}
type wireReceipts70 struct {
	ID         uint64
	Incomplete bool
	Pages      [][]wireReceipt70
}

func encodeReceiptPages(pages [][]*types.Receipt) ([][]gethrlp.RawValue, error) {
	out := make([][]gethrlp.RawValue, len(pages))
	for i, page := range pages {
		for _, receipt := range page {
			raw, err := receipt.EncodeRLP()
			if err != nil {
				return nil, err
			}
			if receipt.Type != 0 {
				raw, err = gethrlp.EncodeToBytes(raw)
				if err != nil {
					return nil, err
				}
			}
			out[i] = append(out[i], raw)
		}
	}
	return out, nil
}
func decodeReceiptPages(pages [][]gethrlp.RawValue) ([][]*types.Receipt, error) {
	out := make([][]*types.Receipt, len(pages))
	for i, page := range pages {
		for _, raw := range page {
			kind, _, _, err := gethrlp.Split(raw)
			if err != nil {
				return nil, err
			}
			encoded := []byte(raw)
			if kind != gethrlp.List {
				if err := gethrlp.DecodeBytes(raw, &encoded); err != nil {
					return nil, err
				}
			}
			receipt, err := types.DecodeReceiptRLP(encoded)
			if err != nil {
				return nil, err
			}
			out[i] = append(out[i], receipt)
		}
	}
	return out, nil
}
func (p ReceiptsPacket) EncodeRLP() ([]byte, error) {
	pages, err := encodeReceiptPages(p.Receipts)
	if err != nil {
		return nil, err
	}
	return gethrlp.EncodeToBytes(wireReceipts{p.RequestID, pages})
}
func (p *ReceiptsPacket) DecodeRLP(data []byte) error {
	var packet wireReceipts
	if err := gethrlp.DecodeBytes(data, &packet); err != nil {
		return err
	}
	pages, err := decodeReceiptPages(packet.Pages)
	if err != nil {
		return err
	}
	p.RequestID, p.Receipts = packet.ID, pages
	return nil
}
func (p ReceiptsPacketV70) EncodeRLP() ([]byte, error) {
	packet := wireReceipts70{ID: p.RequestID, Incomplete: p.LastBlockIncomplete, Pages: make([][]wireReceipt70, len(p.Receipts))}
	for i, page := range p.Receipts {
		for _, receipt := range page {
			if receipt == nil {
				return nil, fmt.Errorf("nil receipt")
			}
			item := wireReceipt70{Type: receipt.Type, CumulativeGas: receipt.CumulativeGasUsed}
			var err error
			if len(receipt.PostState) > 0 {
				item.State, err = gethrlp.EncodeToBytes(receipt.PostState)
			} else {
				item.State, err = gethrlp.EncodeToBytes(receipt.Status)
			}
			if err != nil {
				return nil, err
			}
			for _, log := range receipt.Logs {
				if log == nil {
					return nil, fmt.Errorf("nil log")
				}
				item.Logs = append(item.Logs, wireReceiptLog{log.Address, log.Topics, log.Data})
			}
			packet.Pages[i] = append(packet.Pages[i], item)
		}
	}
	return gethrlp.EncodeToBytes(packet)
}
func (p *ReceiptsPacketV70) DecodeRLP(data []byte) error {
	var packet wireReceipts70
	if err := gethrlp.DecodeBytes(data, &packet); err != nil {
		return err
	}
	pages := make([][]*types.Receipt, len(packet.Pages))
	for i, page := range packet.Pages {
		for _, item := range page {
			receipt := &types.Receipt{Type: item.Type, CumulativeGasUsed: item.CumulativeGas}
			var status []byte
			if err := gethrlp.DecodeBytes(item.State, &status); err != nil {
				return err
			}
			switch {
			case len(status) == 0:
			case len(status) == 1 && status[0] == 1:
				receipt.Status = 1
			case len(status) == 32:
				receipt.PostState = status
			default:
				return fmt.Errorf("invalid receipt status")
			}
			for _, log := range item.Logs {
				receipt.Logs = append(receipt.Logs, &types.Log{Address: log.Address, Topics: log.Topics, Data: log.Data})
			}
			receipt.Bloom = types.CreateBloom([]*types.Receipt{receipt})
			pages[i] = append(pages[i], receipt)
		}
	}
	p.RequestID, p.LastBlockIncomplete, p.Receipts = packet.ID, packet.Incomplete, pages
	return nil
}

type wirePooledTransactions struct {
	ID           uint64
	Transactions []gethrlp.RawValue
}

func (p PooledTransactionsPacket) EncodeRLP() ([]byte, error) {
	packet := wirePooledTransactions{ID: p.RequestID}
	for _, tx := range p.Transactions {
		raw, err := tx.EncodeRLP()
		if err != nil {
			return nil, err
		}
		if tx.Type() != 0 {
			raw, err = gethrlp.EncodeToBytes(raw)
			if err != nil {
				return nil, err
			}
		}
		packet.Transactions = append(packet.Transactions, raw)
	}
	return gethrlp.EncodeToBytes(packet)
}
func (p *PooledTransactionsPacket) DecodeRLP(data []byte) error {
	var packet wirePooledTransactions
	if err := gethrlp.DecodeBytes(data, &packet); err != nil {
		return err
	}
	p.RequestID = packet.ID
	p.Transactions = nil
	for _, raw := range packet.Transactions {
		kind, _, _, err := gethrlp.Split(raw)
		if err != nil {
			return err
		}
		encoded := []byte(raw)
		if kind != gethrlp.List {
			if err := gethrlp.DecodeBytes(raw, &encoded); err != nil {
				return err
			}
		}
		tx, err := types.DecodeTxRLP(encoded)
		if err != nil {
			return err
		}
		p.Transactions = append(p.Transactions, tx)
	}
	return nil
}
