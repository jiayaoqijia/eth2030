package snap

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/state"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/p2p/wire"
	"github.com/eth2030/eth2030/rlp"
	"github.com/eth2030/eth2030/sync/statesync"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
)

func TestVerifiedSnapPeerRejectsGapsAndTampering(t *testing.T) {
	source := state.NewMemoryStateDB()
	for i := byte(1); i <= 3; i++ {
		source.AddBalance(types.BytesToAddress([]byte{i}), big.NewInt(int64(i)))
	}
	snapshot, err := state.ExportStateSnapshot(source)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]types.Hash, 0, 3)
	for k := range snapshot.Accounts {
		keys = append(keys, k)
	}
	// Small explicit sort keeps the fixture independent of server iteration.
	for i := range keys {
		for j := i + 1; j < len(keys); j++ {
			if keys[j].Hex() < keys[i].Hex() {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	proof, _ := snapshot.AccountProof(types.Hash{})
	lastProof, _ := snapshot.AccountProof(keys[2])
	proof = append(proof, lastProof...)
	for _, test := range []string{"valid", "gap", "tampered", "false-empty", "wrong-root"} {
		t.Run(test, func(t *testing.T) {
			left, right := wire.MsgPipe()
			defer left.Close()
			client := &Client{id: "test", transport: left, pending: make(map[uint64]pendingResponse)}
			readerDone := make(chan struct{})
			go func() {
				defer close(readerDone)
				for {
					msg, err := left.ReadMsg()
					if err != nil {
						return
					}
					_ = client.deliver(msg)
				}
			}()
			defer func() { _ = left.Close(); <-readerDone }()
			serverDone := make(chan struct{})
			go func() {
				defer close(serverDone)
				msg, err := right.ReadMsg()
				if err != nil {
					return
				}
				var request GetAccountRangePacket
				if rlp.DecodeBytes(msg.Payload, &request) != nil {
					return
				}
				response := AccountRangePacket{ID: request.ID, Proof: proof}
				for i, key := range keys {
					if test == "gap" && i == 1 {
						continue
					}
					var account gethtypes.StateAccount
					_ = gethrlp.DecodeBytes(snapshot.Accounts[key], &account)
					response.Accounts = append(response.Accounts, AccountData{key, gethtypes.SlimAccountRLP(account)})
				}
				if test == "tampered" {
					response.Accounts[0].Body[len(response.Accounts[0].Body)-1] ^= 1
				}
				if test == "false-empty" {
					response.Accounts = nil
					response.Proof = nil
				}
				_ = sendSnapMsg(right, AccountRangeMsg, &response)
			}()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			root := source.GetRoot()
			if test == "wrong-root" {
				root[0] ^= 1
			}
			response, err := client.WithContext(ctx).RequestAccountRange(statesync.AccountRangeRequest{Root: root, Limit: types.Hash{0xff}, Bytes: SoftResponseLimit})
			<-serverDone
			if test == "valid" {
				if err != nil || len(response.Accounts) != 3 || response.More {
					t.Fatalf("valid complete range: %+v %v", response, err)
				}
			} else if err == nil {
				t.Fatal("unproven range accepted")
			}
		})
	}
}

func TestSnapRequestCancellation(t *testing.T) {
	left, right := wire.MsgPipe()
	defer left.Close()
	defer right.Close()
	client := &Client{id: "silent", transport: left, pending: make(map[uint64]pendingResponse)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := client.WithContext(ctx).RequestBytecodes(statesync.BytecodeRequest{}); done <- err }()
	if _, err := right.ReadMsg(); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled request succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("request did not cancel")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.pending) != 0 {
		t.Fatal("cancelled request leaked correlation entry")
	}
	// The request was already sent. A superseded CL target must cancel its
	// response wait without tearing down the healthy multiplexed connection.
	if err := right.WriteMsg(wire.Msg{Code: ByteCodesMsg, Payload: []byte{0xc0}}); err != nil {
		t.Fatalf("cancellation disconnected healthy peer: %v", err)
	}
}
