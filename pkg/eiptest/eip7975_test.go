package eiptest_test

import (
	"bytes"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
	"github.com/eth2030/eth2030/eth"
	"github.com/eth2030/eth2030/p2p"
	"github.com/eth2030/eth2030/rlp"
)

// stubTxPool satisfies eth.TxPool without pool wiring; the eth/70 and
// eth/71 message paths under test do not touch the pool.
type stubTxPool struct{}

func (stubTxPool) AddRemote(*types.Transaction) error              { return nil }
func (stubTxPool) Get(types.Hash) *types.Transaction               { return nil }
func (stubTxPool) Pending() map[types.Address][]*types.Transaction { return nil }

// simReceiptProvider serves the receipts produced by a Sim over the
// eth/70 ReceiptProvider interface.
type simReceiptProvider struct {
	receipts map[types.Hash][]*types.Receipt
}

func (p *simReceiptProvider) GetReceipts(hash types.Hash) []*types.Receipt {
	return p.receipts[hash]
}

// wireEIP7975Request sends an eth/70 GetReceipts request into the handler
// over a message pipe and returns the decoded Receipts response.
func wireEIP7975Request(t *testing.T, handler *eth.Handler, hashes []types.Hash, firstIndex uint64) *p2p.ReceiptsPacketV70 {
	t.Helper()
	end1, end2 := p2p.MsgPipe()
	defer end1.Close()
	defer end2.Close()

	ep := eth.NewEthPeer(p2p.NewPeer("eip7975-peer", "127.0.0.1:30303", nil), end1)
	req, err := p2p.EncodeMessage(p2p.GetReceiptsMsg, &p2p.GetReceiptsPacketV70{
		RequestID:              7,
		FirstBlockReceiptIndex: firstIndex,
		Hashes:                 hashes,
	})
	if err != nil {
		t.Fatalf("encode GetReceipts: %v", err)
	}
	if err := handler.HandleMsg(ep, p2p.Msg{Code: req.Code, Size: req.Size, Payload: req.Payload}); err != nil {
		t.Fatalf("handle GetReceipts: %v", err)
	}
	resp, err := end2.ReadMsg()
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if resp.Code != p2p.ReceiptsMsg {
		t.Fatalf("response code = 0x%02x, want Receipts (0x%02x)", resp.Code, p2p.ReceiptsMsg)
	}
	var pkt p2p.ReceiptsPacketV70
	if err := p2p.DecodeMessage(p2p.Message{Code: resp.Code, Size: resp.Size, Payload: resp.Payload}, &pkt); err != nil {
		t.Fatalf("decode Receipts: %v", err)
	}
	return &pkt
}

// TestEIP7975_ReceiptsPaginationOverRealChain paginates receipts of a real
// Sim chain through the eth/70 handler: firstBlockReceiptIndex omits the
// head of the first block's list, later blocks are served in full, and the
// complete response is not marked incomplete. The served receipts are the
// chain's own.
func TestEIP7975_ReceiptsPaginationOverRealChain(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))

	provider := &simReceiptProvider{receipts: make(map[types.Hash][]*types.Receipt)}
	var hashes []types.Hash
	nonce := s.Nonce(s.Alice().Address)
	for _, n := range []int{3, 2, 1} {
		var txs []*types.Transaction
		for i := 0; i < n; i++ {
			txs = append(txs, eiptest.DynamicFee(s.Config().ChainID, s.Alice(), nonce+uint64(i),
				eiptest.ToPtr(s.Bob().Address), eiptest.Wei(1), nil, 21_000, bigFeeCap, bigTipCap))
		}
		nonce += uint64(n)
		r := s.Commit(txs...)
		eiptest.ExpectSuccess(t, r, n-1)
		provider.receipts[r.Block.Hash()] = r.Receipts
		hashes = append(hashes, r.Block.Hash())
	}

	handler := eth.NewHandler(s.Chain(), stubTxPool{}, 1)
	handler.SetReceiptProvider(provider)

	// Page from receipt index 2 of the first block: the first page carries
	// 3-2=1 receipt, the remaining blocks are complete.
	pkt := wireEIP7975Request(t, handler, hashes, 2)
	if pkt.RequestID != 7 {
		t.Fatalf("request id = %d, want 7", pkt.RequestID)
	}
	if pkt.LastBlockIncomplete {
		t.Fatal("complete response must not be marked incomplete")
	}
	if len(pkt.Receipts) != 3 {
		t.Fatalf("pages = %d, want 3", len(pkt.Receipts))
	}
	wantCounts := []int{1, 2, 1}
	for i, want := range wantCounts {
		if len(pkt.Receipts[i]) != want {
			t.Fatalf("page %d receipts = %d, want %d", i, len(pkt.Receipts[i]), want)
		}
		for j, got := range pkt.Receipts[i] {
			wantReceipt := provider.receipts[hashes[i]][j]
			if i == 0 {
				wantReceipt = provider.receipts[hashes[0]][j+2] // first page starts at index 2
			}
			// TxHash is derived metadata and is not carried by the receipt
			// wire encoding. Compare the consensus encoding instead.
			actual, err := got.EncodeRLP()
			if err != nil {
				t.Fatal(err)
			}
			expected, err := wantReceipt.EncodeRLP()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(actual, expected) {
				t.Fatalf("page %d receipt %d consensus bytes differ", i, j)
			}
		}
	}

	// A request with firstBlockReceiptIndex=0 serves every receipt.
	full := wireEIP7975Request(t, handler, hashes, 0)
	total := 0
	for _, page := range full.Receipts {
		total += len(page)
	}
	if total != 6 {
		t.Fatalf("full response receipts = %d, want 6", total)
	}
	if full.LastBlockIncomplete {
		t.Fatal("full response must not be marked incomplete")
	}
}

// TestEIP7975_ValidatePartialReceipts exercises the EIP-7975 sanity checks
// for lastBlockIncomplete lists against a real Sim block: the delivered
// count must match the transaction count and each receipt must fit within
// gasLimit/8 of its transaction.
func TestEIP7975_ValidatePartialReceipts(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
	txs := []*types.Transaction{
		eiptest.DynamicFee(s.Config().ChainID, s.Alice(), 0,
			eiptest.ToPtr(s.Bob().Address), eiptest.Wei(1), nil, 21_000, bigFeeCap, bigTipCap),
		eiptest.DynamicFee(s.Config().ChainID, s.Alice(), 1,
			eiptest.ToPtr(s.Bob().Address), eiptest.Wei(2), nil, 21_000, bigFeeCap, bigTipCap),
	}
	r := s.Commit(txs...)
	eiptest.ExpectSuccess(t, r, 1)
	block := r.Block

	// Honest partial list passes.
	if err := eth.ValidatePartialReceipts(block, r.Receipts); err != nil {
		t.Fatalf("honest partial receipts rejected: %v", err)
	}

	// Count mismatch: one receipt short.
	if err := eth.ValidatePartialReceipts(block, r.Receipts[:1]); err == nil {
		t.Fatal("count mismatch must be rejected")
	}

	// Receipt larger than gasLimit/8 of its transaction: a single log with
	// 4 KiB of data exceeds the 2625-byte bound of a 21000-gas transaction.
	fat := &types.Receipt{
		Status:            1,
		CumulativeGasUsed: 21000,
		TxHash:            txs[0].Hash(),
		Logs: []*types.Log{{
			Address: s.Bob().Address,
			Data:    make([]byte, 4096),
		}},
	}
	fatEnc, err := rlp.EncodeToBytes(fat)
	if err != nil {
		t.Fatalf("encode fat receipt: %v", err)
	}
	if int64(len(fatEnc)) <= int64(txs[0].Gas())/8 {
		t.Fatalf("test receipt expected oversized, is %d bytes", len(fatEnc))
	}
	if err := eth.ValidatePartialReceipts(block, []*types.Receipt{fat, r.Receipts[1]}); err == nil {
		t.Fatal("oversized receipt must be rejected")
	}

	// Unknown block.
	if err := eth.ValidatePartialReceipts(nil, r.Receipts); err == nil {
		t.Fatal("unknown block must be rejected")
	}
}

// TestEIP7975_LastBlockIncompleteTruncation drives the serving-side
// truncation: once the 10 MiB response budget is exhausted the remaining
// receipts are dropped, the list is truncated, and lastBlockIncomplete is
// set so the client pages the remainder. Real blocks cannot reach the
// budget below the ~83M gas limit (that is the EIP's motivation), so the
// oversized page is synthetic while the chain and handler are real.
func TestEIP7975_LastBlockIncompleteTruncation(t *testing.T) {
	s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))

	provider := &simReceiptProvider{receipts: make(map[types.Hash][]*types.Receipt)}
	fat := make([]*types.Receipt, 0, 4)
	for i := 0; i < 4; i++ {
		fat = append(fat, &types.Receipt{
			Status:            1,
			CumulativeGasUsed: uint64(i+1) * 21000,
			Logs: []*types.Log{{
				Address: s.Bob().Address,
				Data:    make([]byte, 4<<20), // 4 MiB of log data
			}},
		})
	}
	var hashes []types.Hash
	for i := 0; i < 2; i++ {
		r := s.Commit()
		provider.receipts[r.Block.Hash()] = fat
		hashes = append(hashes, r.Block.Hash())
	}

	handler := eth.NewHandler(s.Chain(), stubTxPool{}, 1)
	handler.SetReceiptProvider(provider)

	pkt := wireEIP7975Request(t, handler, hashes, 0)
	if !pkt.LastBlockIncomplete {
		t.Fatal("budget-exceeding response must be marked incomplete")
	}
	// Two 4 MiB receipts fit the budget; the third exceeds it, so the first
	// block's list is truncated and the response stops there.
	if len(pkt.Receipts) != 1 {
		t.Fatalf("pages = %d, want 1 (truncated first block)", len(pkt.Receipts))
	}
	if len(pkt.Receipts[0]) != 2 {
		t.Fatalf("first page receipts = %d, want 2", len(pkt.Receipts[0]))
	}
}
