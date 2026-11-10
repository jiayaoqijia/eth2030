package wire

import (
	"bytes"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/p2p/rlpx"
)

// ErrBadHandshake indicates an unsuccessful RLPx session handshake.
var ErrBadHandshake = errors.New("p2p/wire: rlpx handshake failed")

// RLPxTransport adapts the go-ethereum RLPx connection to the local message API.
type RLPxTransport struct {
	raw     net.Conn
	session *rlpx.Conn
	rmu     sync.Mutex
	wmu     sync.Mutex
}

func NewRLPxTransport(conn net.Conn) *RLPxTransport {
	return &RLPxTransport{raw: conn}
}

// Handshake establishes an RLPx session. An outbound connection supplies the
// remote static public key from its enode URL. An inbound connection passes nil.
func (t *RLPxTransport) Handshake(local *ecdsa.PrivateKey, remote *ecdsa.PublicKey) (*ecdsa.PublicKey, error) {
	if local == nil {
		return nil, ErrBadHandshake
	}
	conn := rlpx.NewConn(t.raw, remote)
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, err
	}
	defer conn.SetDeadline(time.Time{})
	peerKey, err := conn.Handshake(local)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadHandshake, err)
	}
	t.session = conn
	return peerKey, nil
}

func (t *RLPxTransport) SetSnappy(enabled bool) {
	t.session.SetSnappy(enabled)
}

func (t *RLPxTransport) ReadMsg() (Msg, error) {
	t.rmu.Lock()
	defer t.rmu.Unlock()
	if t.session == nil {
		return Msg{}, ErrBadHandshake
	}
	code, payload, _, err := t.session.Read()
	if err != nil {
		return Msg{}, err
	}
	return Msg{Code: code, Size: uint32(len(payload)), Payload: bytes.Clone(payload)}, nil
}

func (t *RLPxTransport) WriteMsg(msg Msg) error {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	if t.session == nil {
		return ErrBadHandshake
	}
	_, err := t.session.Write(msg.Code, msg.Payload)
	return err
}

func (t *RLPxTransport) Close() error { return t.raw.Close() }
