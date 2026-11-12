package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Legacy plaintext test-transport control codes. Standard RLPx uses 0x00-0x03
// and the canonical RLP Hello in rlpx_hello.go; these codes never go on RLPx.
const (
	HelloMsg      = 0x80 // Capability handshake.
	DisconnectMsg = 0x81 // Graceful disconnect with reason.
	PingMsg       = 0x82
	PongMsg       = 0x83
)

// devp2p base protocol version. We implement v5 which is used by all modern
// Ethereum clients since the Constantinople fork.
const baseProtocolVersion = 5

// Handshake errors.
var (
	ErrHandshakeTimeout    = errors.New("p2p/wire: handshake timeout")
	ErrIncompatibleVersion = errors.New("p2p/wire: incompatible protocol version")
	ErrNoMatchingCaps      = errors.New("p2p/wire: no matching capabilities")
)

// HelloPacket is the devp2p hello message exchanged during the capability
// handshake. Each side advertises its client identity and supported
// sub-protocol capabilities.
type HelloPacket struct {
	Version    uint64 // devp2p base protocol version (5).
	Name       string // Client identity string (e.g. "ETH2030/v0.1.0").
	Caps       []Cap  // Supported sub-protocol capabilities.
	ListenPort uint64 // TCP listening port (0 if not listening).
	ID         string // Node ID (hex-encoded public key or random).
}

// EncodeHello serializes a HelloPacket into a wire-format byte slice.
func EncodeHello(h *HelloPacket) []byte {
	// Pre-calculate size.
	size := 8 + 2 + len(h.Name) // version + nameLen + name
	size += 2                   // capCount
	for _, c := range h.Caps {
		size += 1 + len(c.Name) + 4 // capNameLen + capName + capVersion
	}
	size += 8             // listenPort
	size += 2 + len(h.ID) // idLen + id

	buf := make([]byte, 0, size)

	// Version (8 bytes).
	var tmp [8]byte
	binary.BigEndian.PutUint64(tmp[:], h.Version)
	buf = append(buf, tmp[:]...)

	// Name (2-byte length prefix + string).
	var lenBuf [2]byte
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(h.Name)))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, []byte(h.Name)...)

	// Caps (2-byte count, then each: 1-byte name len + name + 4-byte version).
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(h.Caps)))
	buf = append(buf, lenBuf[:]...)
	for _, c := range h.Caps {
		buf = append(buf, byte(len(c.Name)))
		buf = append(buf, []byte(c.Name)...)
		var vbuf [4]byte
		binary.BigEndian.PutUint32(vbuf[:], uint32(c.Version))
		buf = append(buf, vbuf[:]...)
	}

	// ListenPort (8 bytes).
	binary.BigEndian.PutUint64(tmp[:], h.ListenPort)
	buf = append(buf, tmp[:]...)

	// ID (2-byte length prefix + string).
	binary.BigEndian.PutUint16(lenBuf[:], uint16(len(h.ID)))
	buf = append(buf, lenBuf[:]...)
	buf = append(buf, []byte(h.ID)...)

	return buf
}

// DecodeHello deserializes a HelloPacket from wire-format bytes.
func DecodeHello(data []byte) (*HelloPacket, error) {
	if len(data) < 8+2 {
		return nil, fmt.Errorf("p2p/wire: hello packet too short")
	}
	h := &HelloPacket{}
	off := 0

	// Version.
	h.Version = binary.BigEndian.Uint64(data[off:])
	off += 8

	// Name.
	if off+2 > len(data) {
		return nil, fmt.Errorf("p2p/wire: hello packet truncated at name length")
	}
	nameLen := int(binary.BigEndian.Uint16(data[off:]))
	off += 2
	if off+nameLen > len(data) {
		return nil, fmt.Errorf("p2p/wire: hello packet truncated at name")
	}
	h.Name = string(data[off : off+nameLen])
	off += nameLen

	// Caps.
	if off+2 > len(data) {
		return nil, fmt.Errorf("p2p/wire: hello packet truncated at cap count")
	}
	capCount := int(binary.BigEndian.Uint16(data[off:]))
	off += 2
	h.Caps = make([]Cap, 0, capCount)
	for i := 0; i < capCount; i++ {
		if off+1 > len(data) {
			return nil, fmt.Errorf("p2p/wire: hello packet truncated at cap %d name length", i)
		}
		cnLen := int(data[off])
		off++
		if off+cnLen+4 > len(data) {
			return nil, fmt.Errorf("p2p/wire: hello packet truncated at cap %d", i)
		}
		name := string(data[off : off+cnLen])
		off += cnLen
		ver := binary.BigEndian.Uint32(data[off:])
		off += 4
		h.Caps = append(h.Caps, Cap{Name: name, Version: uint(ver)})
	}

	// ListenPort.
	if off+8 > len(data) {
		return nil, fmt.Errorf("p2p/wire: hello packet truncated at listen port")
	}
	h.ListenPort = binary.BigEndian.Uint64(data[off:])
	off += 8

	// ID.
	if off+2 > len(data) {
		return nil, fmt.Errorf("p2p/wire: hello packet truncated at id length")
	}
	idLen := int(binary.BigEndian.Uint16(data[off:]))
	off += 2
	if off+idLen > len(data) {
		return nil, fmt.Errorf("p2p/wire: hello packet truncated at id")
	}
	h.ID = string(data[off : off+idLen])

	return h, nil
}

// DisconnectReason is a devp2p disconnect reason code.
type DisconnectReason uint8

const (
	DiscRequested        DisconnectReason = 0x00 // Peer requested disconnect.
	DiscNetworkError     DisconnectReason = 0x01 // Network error.
	DiscProtocolError    DisconnectReason = 0x02 // Protocol breach.
	DiscUselessPeer      DisconnectReason = 0x03 // No matching capabilities.
	DiscTooManyPeers     DisconnectReason = 0x04 // Too many peers.
	DiscAlreadyConnected DisconnectReason = 0x05 // Already connected.
	DiscSubprotocolError DisconnectReason = 0x10 // Sub-protocol error.
)

// String returns a human-readable disconnect reason.
func (r DisconnectReason) String() string {
	switch r {
	case DiscRequested:
		return "requested"
	case DiscNetworkError:
		return "network error"
	case DiscProtocolError:
		return "protocol error"
	case DiscUselessPeer:
		return "useless peer"
	case DiscTooManyPeers:
		return "too many peers"
	case DiscAlreadyConnected:
		return "already connected"
	case DiscSubprotocolError:
		return "sub-protocol error"
	default:
		return fmt.Sprintf("unknown(%d)", r)
	}
}

// PerformHandshake exchanges hello messages with the remote peer over the
// given transport. It sends our hello and reads the remote hello concurrently.
// On success, it returns the remote HelloPacket.
func PerformHandshake(tr Transport, local *HelloPacket) (*HelloPacket, error) {
	type result struct {
		hello *HelloPacket
		err   error
	}
	recvCh := make(chan result, 1)
	sendCh := make(chan error, 1)

	go func() {
		payload := EncodeHello(local)
		err := tr.WriteMsg(Msg{
			Code:    HelloMsg,
			Size:    uint32(len(payload)),
			Payload: payload,
		})
		sendCh <- err
	}()

	go func() {
		msg, err := tr.ReadMsg()
		if err != nil {
			recvCh <- result{nil, fmt.Errorf("p2p/wire: handshake read: %w", err)}
			return
		}
		if msg.Code == DisconnectMsg {
			reason := DisconnectReason(0xFF)
			if len(msg.Payload) > 0 {
				reason = DisconnectReason(msg.Payload[0])
			}
			recvCh <- result{nil, fmt.Errorf("p2p/wire: remote disconnected during handshake: %s", reason)}
			return
		}
		if msg.Code != HelloMsg {
			recvCh <- result{nil, fmt.Errorf("p2p/wire: expected hello (0x%02x), got 0x%02x", HelloMsg, msg.Code)}
			return
		}
		remote, err := DecodeHello(msg.Payload)
		if err != nil {
			recvCh <- result{nil, err}
			return
		}
		recvCh <- result{remote, nil}
	}()

	if err := <-sendCh; err != nil {
		return nil, fmt.Errorf("p2p/wire: handshake write: %w", err)
	}

	res := <-recvCh
	if res.err != nil {
		return nil, res.err
	}

	if res.hello.Version < baseProtocolVersion {
		sendDisconnect(tr, DiscProtocolError)
		return nil, fmt.Errorf("%w: remote=%d, local=%d", ErrIncompatibleVersion, res.hello.Version, baseProtocolVersion)
	}

	if !hasMatchingCap(local.Caps, res.hello.Caps) {
		sendDisconnect(tr, DiscUselessPeer)
		return nil, ErrNoMatchingCaps
	}

	return res.hello, nil
}

// sendDisconnect sends a disconnect message with the given reason.
func sendDisconnect(tr Transport, reason DisconnectReason) {
	go func() {
		_ = tr.WriteMsg(Msg{
			Code:    DisconnectMsg,
			Size:    1,
			Payload: []byte{byte(reason)},
		})
	}()
}

// hasMatchingCap returns true if local and remote share at least one capability.
func hasMatchingCap(local, remote []Cap) bool {
	for _, lc := range local {
		for _, rc := range remote {
			if lc.Name == rc.Name && lc.Version == rc.Version {
				return true
			}
		}
	}
	return false
}

// MatchingCaps returns the list of capabilities shared between local and remote.
func MatchingCaps(local, remote []Cap) []Cap {
	var matched []Cap
	for _, lc := range local {
		for _, rc := range remote {
			if lc.Name == rc.Name && lc.Version == rc.Version {
				matched = append(matched, lc)
			}
		}
	}
	return matched
}
