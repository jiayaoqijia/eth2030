# Package wire

Low-level devp2p connection layer: length-prefixed message framing, RLPx ECIES encryption, ECIES handshake, capability negotiation, protocol multiplexing, and TCP dial/listen abstractions.

## Overview

The `wire` package implements the transport substrate that all devp2p protocols run on. The central abstraction is the `Transport` interface (`ReadMsg` / `WriteMsg` / `Close`). `FrameTransport` wraps a `net.Conn` with plaintext 4-byte-length-prefixed framing (format: `length[4] || code[1] || payload`). `RLPxTransport` (in `rlpx.go`) uses the go-ethereum RLPx handshake and encrypted frame transport. `Multiplexer` (in `multiplexer.go`) dispatches messages across multiple protocol handlers registered by code offset. `HelloPacket` and `Cap` define the devp2p hello/disconnect message types used during capability negotiation.

## Functionality

### Transport interface

```go
type Transport interface {
    ReadMsg() (Msg, error)
    WriteMsg(msg Msg) error
    Close() error
}
```

### Core types

- `Msg` — `{Code uint64, Size uint32, Payload []byte}`
- `Cap` — `{Name string, Version uint}` (protocol capability)
- `FrameTransport` — plaintext framing over `net.Conn`
  - `NewFrameTransport(conn net.Conn) *FrameTransport`
- `FrameConnTransport` — extends `FrameTransport` with `RemoteAddr() string`
  - `NewFrameConnTransport(conn net.Conn) *FrameConnTransport`

### TCP networking

- `TCPDialer` — `Dial(addr string) (ConnTransport, error)`
- `TCPListener` — `NewTCPListener(ln net.Listener) *TCPListener`, `Accept()`, `Close()`, `Addr()`

### Interfaces

- `ConnTransport` — `Transport` + `RemoteAddr() string`
- `Dialer` — `Dial(addr string) (ConnTransport, error)`
- `Listener` — `Accept() (ConnTransport, error)`, `Close()`, `Addr()`

### Errors and limits

`ErrTransportClosed`, `ErrFrameTooLarge`, `ErrMessageTooLarge`, `ErrInvalidMsgCode`, `ErrDecode`

`MaxMessageSize = 16 MiB`

## Usage

```go
dialer := &wire.TCPDialer{}
conn, err := dialer.Dial("192.168.1.2:30303")
if err != nil {
    log.Fatal(err)
}
conn.WriteMsg(wire.Msg{Code: 0x00, Payload: helloPayload})
msg, err := conn.ReadMsg()
conn.Close()
```

[← p2p](../README.md)
