package p2p

import (
	"net"
	"testing"
	"time"

	"github.com/eth2030/eth2030/p2p/peermgr"
	"github.com/eth2030/eth2030/p2p/wire"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/p2p/rlpx"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
)

func TestRLPxNegotiationAgainstIndependentCodec(t *testing.T) {
	serverKey, err := gethcrypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	wrong := make(chan struct{}, 2)
	protocol := func(name string, version uint, length, code uint64) Protocol {
		return Protocol{Name: name, Version: version, Length: length, Run: func(_ *peermgr.Peer, tr wire.Transport) error {
			if name == "aaa" || version == 67 {
				wrong <- struct{}{}
				return nil
			}
			data, _ := gethrlp.EncodeToBytes(name)
			if err := tr.WriteMsg(wire.Msg{Code: code, Size: uint32(len(data)), Payload: data}); err != nil {
				return err
			}
			_, err := tr.ReadMsg()
			return err
		}}
	}
	server := NewServer(Config{ListenAddr: "127.0.0.1:0", EnableRLPx: true, RLPxPrivateKey: serverKey, Protocols: []Protocol{protocol("aaa", 1, 9, 0), protocol("eth", 67, 17, 0), protocol("eth", 68, 17, 0), protocol("snap", 1, 8, 1)}})
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	defer server.Stop()
	conn, err := net.Dial("tcp", server.ListenAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	remote := rlpx.NewConn(conn, &serverKey.PublicKey)
	_ = remote.SetDeadline(time.Now().Add(5 * time.Second))
	clientKey, _ := gethcrypto.GenerateKey()
	if _, err := remote.Handshake(clientKey); err != nil {
		t.Fatal(err)
	}
	type cap struct {
		Name    string
		Version uint
	}
	hello := struct {
		Version uint64
		Name    string
		Caps    []cap
		Port    uint64
		ID      []byte
		Extra   []byte
	}{5, "independent", []cap{{"eth", 67}, {"eth", 68}, {"snap", 1}}, 0, gethcrypto.FromECDSAPub(&clientKey.PublicKey)[1:], []byte("future field")}
	encoded, err := gethrlp.EncodeToBytes(hello)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := remote.Write(0, encoded); err != nil {
		t.Fatal(err)
	}
	code, body, _, err := remote.Read()
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 {
		t.Fatalf("hello code %x", code)
	}
	var decoded struct {
		Version uint64
		Name    string
		Caps    []cap
		Port    uint64
		ID      []byte
	}
	if err := gethrlp.DecodeBytes(body, &decoded); err != nil {
		t.Fatalf("noncanonical Hello: %v", err)
	}
	remote.SetSnappy(true)
	seen := map[uint64]bool{}
	for i := 0; i < 2; i++ {
		code, _, _, err := remote.Read()
		if err != nil {
			t.Fatal(err)
		}
		seen[code] = true
	}
	if !seen[0x10] || !seen[0x22] {
		t.Fatalf("incorrect negotiated offsets: %v", seen)
	}
	if _, err := remote.Write(2, []byte{0xc0}); err != nil {
		t.Fatal(err)
	}
	code, _, _, err = remote.Read()
	if err != nil || code != 3 {
		t.Fatalf("base ping/pong failed: %x %v", code, err)
	}
	select {
	case <-wrong:
		t.Fatal("unshared/lower capability was started")
	default:
	}
}
