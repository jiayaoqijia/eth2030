package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/eth"
	"github.com/eth2030/eth2030/p2p"
)

// simBALProvider serves the RLP-encoded block access lists recorded from a
// Sim over the eth/71 AccessListProvider interface.
type simBALProvider struct {
	bals map[types.Hash][]byte
}

func (p *simBALProvider) GetBlockAccessListRLP(hash types.Hash) ([]byte, bool) {
	bal, ok := p.bals[hash]
	return bal, ok
}

// wireEIP8159Request sends an eth/71 GetBlockAccessLists request into the
// handler and returns the decoded BlockAccessLists response.
func wireEIP8159Request(t *testing.T, handler *eth.Handler, hashes []types.Hash) *p2p.BlockAccessListsPacket {
	t.Helper()
	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	ep := eth.NewEthPeer(p2p.NewPeer("eip8159-peer", "127.0.0.1:30303", nil), end1)
	req, err := p2p.EncodeMessage(p2p.GetBlockAccessListsMsg, &p2p.GetBlockAccessListsPacket{
		RequestID:   9,
		BlockHashes: hashes,
	})
	if err != nil {
		t.Fatalf("encode GetBlockAccessLists: %v", err)
	}
	if err := handler.HandleMsg(ep, p2p.Msg{Code: req.Code, Size: req.Size, Payload: req.Payload}); err != nil {
		t.Fatalf("handle GetBlockAccessLists: %v", err)
	}
	resp, err := end2.ReadMsg()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.Code != p2p.BlockAccessListsMsg {
		t.Fatalf("response code = 0x%02x, want BlockAccessLists (0x%02x)", resp.Code, p2p.BlockAccessListsMsg)
	}
	var pkt p2p.BlockAccessListsPacket
	if err := p2p.DecodeMessage(p2p.Message{Code: resp.Code, Size: resp.Size, Payload: resp.Payload}, &pkt); err != nil {
		t.Fatalf("decode BlockAccessLists: %v", err)
	}
	return &pkt
}

// TestEIP8159_BALEndToEnd verifies the full eth/71 flow over a real Sim
// chain: the block's BAL RLP-encodes to exactly the header's
// block-access-list-hash commitment, the handler serves those bytes in
// request order, serves the empty list for unavailable blocks, and every
// served BAL validates against its header.
func TestEIP8159_BALEndToEnd(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))

	provider := &simBALProvider{bals: make(map[types.Hash][]byte)}
	var hashes []types.Hash
	for i := 0; i < 2; i++ {
		tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), uint64(i),
			eiptest.ToPtr(s.Bob().Address), eiptest.Wei(int64(1+i)), nil, 21_000, bigFeeCap, bigTipCap)
		r := s.Commit(tx)
		eiptest.ExpectSuccess(t, r, 0)
		eiptest.ExpectBAL(t, r)

		// EIP-7928 commitment: keccak256(rlp(bal)) is the header field, so
		// the served wire bytes are exactly what the header commits to.
		encoded, err := r.BAL.EncodeRLP()
		if err != nil {
			t.Fatalf("encode BAL: %v", err)
		}
		if got := crypto.Keccak256Hash(encoded); r.Block.Header().BlockAccessListHash == nil || got != *r.Block.Header().BlockAccessListHash {
			t.Fatalf("block %d: BAL encoding does not hash to the header commitment", i)
		}
		provider.bals[r.Block.Hash()] = encoded
		hashes = append(hashes, r.Block.Hash())
	}
	unknown := types.Hash{0xEE}
	hashes = append(hashes, unknown)

	handler := eth.NewHandler(s.Chain(), stubTxPool{}, 1)
	handler.SetAccessListProvider(provider)

	pkt := wireEIP8159Request(t, handler, hashes)
	if pkt.RequestID != 9 {
		t.Fatalf("request id = %d, want 9", pkt.RequestID)
	}
	if len(pkt.AccessLists) != 3 {
		t.Fatalf("entries = %d, want 3 (one per requested hash, in order)", len(pkt.AccessLists))
	}
	for i := 0; i < 2; i++ {
		header := s.Chain().GetBlock(hashes[i]).Header()
		if err := eth.ValidateBlockAccessListForHeader(pkt.AccessLists[i], header); err != nil {
			t.Fatalf("served BAL %d failed header validation: %v", i, err)
		}
	}
	// Unavailable blocks are the RLP empty list 0xc0 per the spec.
	if len(pkt.AccessLists[2]) != 1 || pkt.AccessLists[2][0] != 0xc0 {
		t.Fatalf("unavailable BAL = %x, want 0xc0", pkt.AccessLists[2])
	}
}

// TestEIP8159_RejectsTamperedBAL verifies the EIP-8159 client-side rule
// that received BALs must hash to the header's block-access-list-hash:
// a flipped byte is rejected, an empty entry is rejected for a header
// that requires a BAL, and a non-empty entry is rejected for a header
// without the field.
func TestEIP8159_RejectsTamperedBAL(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), 0,
		eiptest.ToPtr(s.Bob().Address), eiptest.Wei(3), nil, 21_000, bigFeeCap, bigTipCap)
	r := s.Commit(tx)
	eiptest.ExpectSuccess(t, r, 0)

	header := r.Block.Header()
	encoded, err := r.BAL.EncodeRLP()
	if err != nil {
		t.Fatalf("encode BAL: %v", err)
	}
	if err := eth.ValidateBlockAccessListForHeader(encoded, header); err != nil {
		t.Fatalf("honest BAL rejected: %v", err)
	}

	// Tamper one byte deep in the encoding.
	tampered := append([]byte(nil), encoded...)
	tampered[len(tampered)/2] ^= 0xFF
	if err := eth.ValidateBlockAccessListForHeader(tampered, header); err == nil {
		t.Fatal("tampered BAL must be rejected")
	}

	// Header requires a BAL; the empty marker is not enough.
	if err := eth.ValidateBlockAccessListForHeader([]byte{0xc0}, header); err == nil {
		t.Fatal("empty BAL must be rejected for a header with a BAL hash")
	}

	// Header without a BAL hash rejects non-empty entries.
	pre := &types.Header{BlockAccessListHash: nil}
	if err := eth.ValidateBlockAccessListForHeader(encoded, pre); err == nil {
		t.Fatal("non-empty BAL must be rejected for a header without a BAL hash")
	}
	if err := eth.ValidateBlockAccessListForHeader([]byte{0xc0}, pre); err != nil {
		t.Fatalf("empty BAL for a header without a BAL hash must pass: %v", err)
	}

	// The handler dispatches a response containing a tampered BAL without
	// accepting it: the client-side path logs the rejection.
	bad := &p2p.BlockAccessListsPacket{RequestID: 11, AccessLists: [][]byte{tampered}}
	msg, err := p2p.EncodeMessage(p2p.BlockAccessListsMsg, bad)
	if err != nil {
		t.Fatalf("encode BlockAccessLists: %v", err)
	}
	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()
	ep := eth.NewEthPeer(p2p.NewPeer("eip8159-client", "127.0.0.1:30303", nil), end1)
	handler := eth.NewHandler(s.Chain(), stubTxPool{}, 1)
	if err := handler.HandleMsg(ep, p2p.Msg{Code: msg.Code, Size: msg.Size, Payload: msg.Payload}); err != nil {
		t.Fatalf("client-side BAL handling errored: %v", err)
	}
}
