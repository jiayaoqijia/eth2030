package wire

import (
	"bytes"
	"fmt"
	"time"

	gethrlp "github.com/ethereum/go-ethereum/rlp"
)

type rlpxHello struct {
	Version    uint64
	Name       string
	Caps       []Cap
	ListenPort uint64
	ID         []byte
	Rest       []gethrlp.RawValue `rlp:"tail"`
}

const rlpxHelloMsg uint64 = 0x00

// PerformRLPxHandshake uses the standard RLP Hello (not the legacy plaintext
// test framing). Extra fields and p2p version differences are tolerated per
// EIP-8; capability versions determine which subprotocols can run.
func PerformRLPxHandshake(tr *RLPxTransport, local *HelloPacket) (*HelloPacket, error) {
	if len(local.ID) != 64 {
		return nil, fmt.Errorf("p2p: invalid local node identity")
	}
	if err := tr.raw.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, err
	}
	defer tr.raw.SetDeadline(time.Time{})
	encoded, err := gethrlp.EncodeToBytes(rlpxHello{Version: local.Version, Name: local.Name, Caps: local.Caps, ListenPort: local.ListenPort, ID: []byte(local.ID)})
	if err != nil {
		return nil, err
	}
	sent := make(chan error, 1)
	go func() { sent <- tr.WriteMsg(Msg{Code: rlpxHelloMsg, Size: uint32(len(encoded)), Payload: encoded}) }()
	message, readErr := tr.ReadMsg()
	if readErr != nil {
		_ = tr.Close()
		<-sent
		return nil, readErr
	}
	if err := <-sent; err != nil {
		return nil, err
	}
	if message.Code != rlpxHelloMsg || len(message.Payload) > 64*1024 {
		return nil, fmt.Errorf("p2p: invalid hello message")
	}
	var remote rlpxHello
	if err := gethrlp.DecodeBytes(message.Payload, &remote); err != nil {
		return nil, err
	}
	if len(remote.ID) != 64 || bytes.Equal(remote.ID, []byte(local.ID)) {
		return nil, fmt.Errorf("p2p: invalid remote identity")
	}
	if !hasMatchingCap(local.Caps, remote.Caps) {
		return nil, ErrNoMatchingCaps
	}
	return &HelloPacket{Version: remote.Version, Name: remote.Name, Caps: remote.Caps, ListenPort: remote.ListenPort, ID: string(remote.ID)}, nil
}
