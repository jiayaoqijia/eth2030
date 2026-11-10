package p2p

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/eth2030/eth2030/p2p/peermgr"
	"github.com/eth2030/eth2030/p2p/wire"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/rlpx"
)

func TestRLPxTransportInteroperatesWithGeth(t *testing.T) {
	clientKey, err := gethcrypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	serverKey, err := gethcrypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	left, right := net.Pipe()
	t.Cleanup(func() { left.Close(); right.Close() })
	server := wire.NewRLPxTransport(right)
	client := rlpx.NewConn(left, &serverKey.PublicKey)
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 1)
	go func() {
		_, err := client.Handshake(clientKey)
		if err == nil {
			_, err = client.Write(0x12, []byte("upstream RLPx"))
		}
		errs <- err
	}()
	remote, err := server.Handshake(serverKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !remote.Equal(&clientKey.PublicKey) {
		t.Fatal("RLPx returned the wrong peer key")
	}
	got, err := server.ReadMsg()
	if err != nil || got.Code != 0x12 || !bytes.Equal(got.Payload, []byte("upstream RLPx")) {
		t.Fatalf("RLPx frame = %+v, %v", got, err)
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
}

func TestRLPxServersNegotiatePeerIdentity(t *testing.T) {
	done := make(chan struct{})
	protocol := Protocol{Name: "test", Version: 1, Run: func(*peermgr.Peer, wire.Transport) error {
		<-done
		return nil
	}}
	cfg := Config{ListenAddr: "127.0.0.1:0", EnableRLPx: true, Protocols: []Protocol{protocol}}
	a, b := NewServer(cfg), NewServer(cfg)
	if err := a.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Stop)
	if err := b.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(b.Stop)
	t.Cleanup(func() { close(done) })
	if err := a.AddPeer(fmt.Sprintf("enode://%s@%s", b.LocalID(), b.ListenAddr())); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if a.peers.Get(b.LocalID()) != nil && b.peers.Get(a.LocalID()) != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("servers did not register the RLPx identities")
}
