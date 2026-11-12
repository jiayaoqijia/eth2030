package ethproto

import (
	"fmt"

	"github.com/eth2030/eth2030/core/types"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
)

type wireHeaders struct {
	ID      uint64
	Headers []gethrlp.RawValue
}

func (p BlockHeadersPacket) EncodeRLP() ([]byte, error) {
	out := wireHeaders{ID: p.RequestID}
	for _, header := range p.Headers {
		if header == nil {
			return nil, fmt.Errorf("eth: nil header")
		}
		raw, err := header.EncodeRLP()
		if err != nil {
			return nil, err
		}
		out.Headers = append(out.Headers, raw)
	}
	return gethrlp.EncodeToBytes(out)
}
func (p *BlockHeadersPacket) DecodeRLP(data []byte) error {
	var in wireHeaders
	if err := gethrlp.DecodeBytes(data, &in); err != nil {
		return err
	}
	p.RequestID = in.ID
	p.Headers = nil
	for _, raw := range in.Headers {
		header, err := types.DecodeHeaderRLP(raw)
		if err != nil {
			return err
		}
		p.Headers = append(p.Headers, header)
	}
	return nil
}

type wireHeaderRequest struct {
	ID    uint64
	Query struct {
		Origin  gethrlp.RawValue
		Amount  uint64
		Skip    uint64
		Reverse bool
	}
}

func (p GetBlockHeadersPacket) EncodeRLP() ([]byte, error) {
	var out wireHeaderRequest
	out.ID = p.RequestID
	var err error
	if p.Request.Origin.Hash != (types.Hash{}) {
		out.Query.Origin, err = gethrlp.EncodeToBytes(p.Request.Origin.Hash)
	} else {
		out.Query.Origin, err = gethrlp.EncodeToBytes(p.Request.Origin.Number)
	}
	if err != nil {
		return nil, err
	}
	out.Query.Amount, out.Query.Skip, out.Query.Reverse = p.Request.Amount, p.Request.Skip, p.Request.Reverse
	return gethrlp.EncodeToBytes(out)
}
func (p *GetBlockHeadersPacket) DecodeRLP(data []byte) error {
	var in wireHeaderRequest
	if err := gethrlp.DecodeBytes(data, &in); err != nil {
		return err
	}
	p.RequestID = in.ID
	p.Request = GetBlockHeadersRequest{Amount: in.Query.Amount, Skip: in.Query.Skip, Reverse: in.Query.Reverse}
	_, content, _, err := gethrlp.Split(in.Query.Origin)
	if err != nil {
		return err
	}
	if len(content) == 32 {
		return gethrlp.DecodeBytes(in.Query.Origin, &p.Request.Origin.Hash)
	}
	return gethrlp.DecodeBytes(in.Query.Origin, &p.Request.Origin.Number)
}

type wireBody struct {
	Transactions []gethrlp.RawValue
	Uncles       []gethrlp.RawValue
	Withdrawals  []*types.Withdrawal `rlp:"optional"`
}
type wireBodies struct {
	ID     uint64
	Bodies []wireBody
}

func (p BlockBodiesPacket) EncodeRLP() ([]byte, error) {
	out := wireBodies{ID: p.RequestID}
	for _, body := range p.Bodies {
		if body == nil {
			return nil, fmt.Errorf("eth: nil body")
		}
		wb := wireBody{Withdrawals: body.Withdrawals}
		for _, tx := range body.Transactions {
			raw, err := tx.EncodeRLP()
			if err != nil {
				return nil, err
			}
			if tx.Type() != types.LegacyTxType {
				raw, err = gethrlp.EncodeToBytes(raw)
				if err != nil {
					return nil, err
				}
			}
			wb.Transactions = append(wb.Transactions, raw)
		}
		for _, uncle := range body.Uncles {
			raw, err := uncle.EncodeRLP()
			if err != nil {
				return nil, err
			}
			wb.Uncles = append(wb.Uncles, raw)
		}
		out.Bodies = append(out.Bodies, wb)
	}
	return gethrlp.EncodeToBytes(out)
}
func (p *BlockBodiesPacket) DecodeRLP(data []byte) error {
	var in wireBodies
	if err := gethrlp.DecodeBytes(data, &in); err != nil {
		return err
	}
	p.RequestID = in.ID
	p.Bodies = nil
	for _, body := range in.Bodies {
		result := &BlockBody{Withdrawals: body.Withdrawals}
		for _, raw := range body.Transactions {
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
			result.Transactions = append(result.Transactions, tx)
		}
		for _, raw := range body.Uncles {
			uncle, err := types.DecodeHeaderRLP(raw)
			if err != nil {
				return err
			}
			result.Uncles = append(result.Uncles, uncle)
		}
		p.Bodies = append(p.Bodies, result)
	}
	return nil
}
