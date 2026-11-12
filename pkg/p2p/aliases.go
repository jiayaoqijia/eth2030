package p2p

// aliases.go re-exports sub-package types into the root p2p namespace so that
// internal files and package-level tests can reference them without explicit
// import paths.

import (
	"net"
	"time"

	"github.com/eth2030/eth2030/p2p/broadcast"
	"github.com/eth2030/eth2030/p2p/ethproto"
	"github.com/eth2030/eth2030/p2p/gossip"
	"github.com/eth2030/eth2030/p2p/peermgr"
	"github.com/eth2030/eth2030/p2p/scoring"
	"github.com/eth2030/eth2030/p2p/transport"
	"github.com/eth2030/eth2030/p2p/wire"
)

// wire type aliases.
type (
	Msg                = wire.Msg
	Message            = wire.Message
	Transport          = wire.Transport
	ConnTransport      = wire.ConnTransport
	Dialer             = wire.Dialer
	Listener           = wire.Listener
	MsgPipeEnd         = wire.MsgPipeEnd
	FrameTransport     = wire.FrameTransport
	FrameConnTransport = wire.FrameConnTransport
	TCPDialer          = wire.TCPDialer
	TCPListener        = wire.TCPListener
	Cap                = wire.Cap
	HelloPacket        = wire.HelloPacket
	RLPxTransport      = wire.RLPxTransport
	DisconnectReason   = wire.DisconnectReason
)

// wire disconnect reason constants.
const (
	DiscRequested        = wire.DiscRequested
	DiscNetworkError     = wire.DiscNetworkError
	DiscProtocolError    = wire.DiscProtocolError
	DiscUselessPeer      = wire.DiscUselessPeer
	DiscTooManyPeers     = wire.DiscTooManyPeers
	DiscAlreadyConnected = wire.DiscAlreadyConnected
	DiscSubprotocolError = wire.DiscSubprotocolError
)

// wire errors and constants.
var (
	ErrDecode              = wire.ErrDecode
	ErrInvalidMsgCode      = wire.ErrInvalidMsgCode
	ErrMessageTooLarge     = wire.ErrMessageTooLarge
	ErrNoMatchingCaps      = wire.ErrNoMatchingCaps
	ErrIncompatibleVersion = wire.ErrIncompatibleVersion
	MaxMessageSize         = wire.MaxMessageSize
)

const DisconnectMsg = wire.DisconnectMsg

// wire function wrappers.
func Send(t Transport, code uint64, data []byte) error       { return wire.Send(t, code, data) }
func MsgPipe() (*MsgPipeEnd, *MsgPipeEnd)                    { return wire.MsgPipe() }
func EncodeMessage(code uint64, v any) (wire.Message, error) { return wire.EncodeMessage(code, v) }
func DecodeMessage(msg wire.Message, v any) error            { return wire.DecodeMessage(msg, v) }
func EncodeHello(h *HelloPacket) []byte                      { return wire.EncodeHello(h) }
func DecodeHello(data []byte) (*HelloPacket, error)          { return wire.DecodeHello(data) }
func NewFrameConnTransport(conn net.Conn) *FrameConnTransport {
	return wire.NewFrameConnTransport(conn)
}
func PerformHandshake(tr Transport, local *HelloPacket) (*HelloPacket, error) {
	return wire.PerformHandshake(tr, local)
}
func NewRLPxTransport(conn net.Conn) *RLPxTransport   { return wire.NewRLPxTransport(conn) }
func NewFrameTransport(conn net.Conn) *FrameTransport { return wire.NewFrameTransport(conn) }
func MatchingCaps(local, remote []Cap) []Cap          { return wire.MatchingCaps(local, remote) }
func NewTCPListener(ln net.Listener) *wire.TCPListener {
	return wire.NewTCPListener(ln)
}

// peermgr type aliases.
type (
	Peer              = peermgr.Peer
	PeerSet           = peermgr.PeerSet
	ManagedPeerSet    = peermgr.ManagedPeerSet
	PeerManagerConfig = peermgr.PeerManagerConfig
	AdvPeerManager    = peermgr.AdvPeerManager
	AdvPeerInfo       = peermgr.AdvPeerInfo
	MsgReadWriter     = peermgr.MsgReadWriter
	PeerHandler       = peermgr.PeerHandler
	PeerHandlerFunc   = peermgr.PeerHandlerFunc
	PeerInfo          = peermgr.PeerInfo
	PeerSetReader     = peermgr.PeerSetReader
)

// peermgr errors.
var (
	ErrPeerAlreadyRegistered = peermgr.ErrPeerAlreadyRegistered
	ErrPeerNotRegistered     = peermgr.ErrPeerNotRegistered
	ErrPeerSetClosed         = peermgr.ErrPeerSetClosed
	ErrPeerExists            = peermgr.ErrPeerExists
	ErrMaxPeers              = peermgr.ErrMaxPeers
	ErrTooManyInbound        = peermgr.ErrTooManyInbound
	ErrTooManyOutbound       = peermgr.ErrTooManyOutbound
	ErrPeerBanned            = peermgr.ErrPeerBanned
)

// peermgr constructors.
func NewPeer(id, remoteAddr string, caps []Cap) *Peer {
	return peermgr.NewPeer(id, remoteAddr, caps)
}
func NewPeerSet() *PeerSet                      { return peermgr.NewPeerSet() }
func NewManagedPeerSet(max int) *ManagedPeerSet { return peermgr.NewManagedPeerSet(max) }
func NewAdvPeerManager(cfg PeerManagerConfig) *AdvPeerManager {
	return peermgr.NewAdvPeerManager(cfg)
}

// scoring type aliases.
type (
	PeerScore       = scoring.PeerScore
	PeerScorer      = scoring.PeerScorer
	PeerScoreConfig = scoring.PeerScoreConfig
	ScoreMap        = scoring.ScoreMap
)

// scoring constructors.
func NewPeerScore() *PeerScore                      { return scoring.NewPeerScore() }
func NewPeerScorer(cfg PeerScoreConfig) *PeerScorer { return scoring.NewPeerScorer(cfg) }
func DefaultPeerScoreConfig() PeerScoreConfig       { return scoring.DefaultPeerScoreConfig() }
func NewScoreMap() *ScoreMap                        { return scoring.NewScoreMap() }

// ethproto version constants.
const ETH68 = ethproto.ETH68

// ethproto type aliases.
type (
	ForkID = ethproto.ForkID
)

// ethproto message code constants.
const (
	StatusMsg                     = ethproto.StatusMsg
	NewBlockHashesMsg             = ethproto.NewBlockHashesMsg
	TransactionsMsg               = ethproto.TransactionsMsg
	GetBlockHeadersMsg            = ethproto.GetBlockHeadersMsg
	BlockHeadersMsg               = ethproto.BlockHeadersMsg
	GetBlockBodiesMsg             = ethproto.GetBlockBodiesMsg
	BlockBodiesMsg                = ethproto.BlockBodiesMsg
	NewBlockMsg                   = ethproto.NewBlockMsg
	NewPooledTransactionHashesMsg = ethproto.NewPooledTransactionHashesMsg
	GetPooledTransactionsMsg      = ethproto.GetPooledTransactionsMsg
	PooledTransactionsMsg         = ethproto.PooledTransactionsMsg
	GetReceiptsMsg                = ethproto.GetReceiptsMsg
	ReceiptsMsg                   = ethproto.ReceiptsMsg
	GetBlockAccessListsMsg        = ethproto.GetBlockAccessListsMsg
	BlockAccessListsMsg           = ethproto.BlockAccessListsMsg
	InclusionList                 = gossip.InclusionList
)

// ethproto type aliases.
type (
	StatusData                         = ethproto.StatusData
	NewBlockHashesEntry                = ethproto.NewBlockHashesEntry
	HashOrNumber                       = ethproto.HashOrNumber
	GetBlockHeadersRequest             = ethproto.GetBlockHeadersRequest
	GetBlockHeadersPacket              = ethproto.GetBlockHeadersPacket
	BlockHeadersPacket                 = ethproto.BlockHeadersPacket
	GetBlockBodiesRequest              = ethproto.GetBlockBodiesRequest
	GetBlockBodiesPacket               = ethproto.GetBlockBodiesPacket
	BlockBody                          = ethproto.BlockBody
	BlockBodiesPacket                  = ethproto.BlockBodiesPacket
	NewBlockData                       = ethproto.NewBlockData
	NewPooledTransactionHashesPacket68 = ethproto.NewPooledTransactionHashesPacket68
	GetPooledTransactionsRequest       = ethproto.GetPooledTransactionsRequest
	GetPooledTransactionsPacket        = ethproto.GetPooledTransactionsPacket
	PooledTransactionsPacket           = ethproto.PooledTransactionsPacket
	GetReceiptsPacket                  = ethproto.GetReceiptsPacket
	ReceiptsPacket                     = ethproto.ReceiptsPacket
	GetReceiptsPacketV70               = ethproto.GetReceiptsPacketV70
	ReceiptsPacketV70                  = ethproto.ReceiptsPacketV70
	GetBlockAccessListsPacket          = ethproto.GetBlockAccessListsPacket
	BlockAccessListsPacket             = ethproto.BlockAccessListsPacket
)

// ethproto function wrappers.
func MessageName(code uint64) string        { return ethproto.MessageName(code) }
func ValidateMessageCode(code uint64) error { return ethproto.ValidateMessageCode(code) }

// transport type aliases.
type (
	MixnetTransportMode = transport.MixnetTransportMode
	TransportConfig     = transport.TransportConfig
	TransportManager    = transport.TransportManager
	AnonymousTransport  = transport.AnonymousTransport
	TransportStats      = transport.TransportStats
)

// transport mode constants.
const (
	ModeSimulated = transport.ModeSimulated
	ModeTorSocks5 = transport.ModeTorSocks5
	ModeNymSocks5 = transport.ModeNymSocks5
)

// transport function wrappers.
func DefaultTransportConfig() TransportConfig { return transport.DefaultTransportConfig() }
func NewTransportManagerWithConfig(cfg TransportConfig) *TransportManager {
	return transport.NewTransportManagerWithConfig(cfg)
}
func ParseMixnetMode(s string) (MixnetTransportMode, error) { return transport.ParseMixnetMode(s) }
func ProbeProxy(addr string, timeout time.Duration) bool    { return transport.ProbeProxy(addr, timeout) }

// scoring constants.
const ScoreHandshakeOK = scoring.ScoreHandshakeOK

// gossip type aliases.
type (
	GossipTopic  = gossip.GossipTopic
	MessageID    = gossip.MessageID
	TopicManager = gossip.TopicManager
	TopicParams  = gossip.TopicParams
	TopicHandler = gossip.TopicHandler
)

// gossip topic constants.
const (
	STARKMempoolTick = gossip.STARKMempoolTick
)

// gossip function wrappers.
func NewTopicManager(params TopicParams) *TopicManager { return gossip.NewTopicManager(params) }
func DefaultTopicParams() TopicParams                  { return gossip.DefaultTopicParams() }

// broadcast type aliases.
type MempoolBroadcaster = broadcast.MempoolBroadcaster

// broadcast function wrappers.
func NewMempoolBroadcaster(tm *TopicManager) *MempoolBroadcaster {
	return broadcast.NewMempoolBroadcaster(tm)
}

// transport extra types.
type (
	TorTransport    = transport.TorTransport
	TorConfig       = transport.TorConfig
	NymTransport    = transport.NymTransport
	NymConfig       = transport.NymConfig
	MixnetTransport = transport.MixnetTransport
	MixnetConfig    = transport.MixnetConfig
)

// transport constructors.
func NewTorTransport(cfg *TorConfig) *TorTransport          { return transport.NewTorTransport(cfg) }
func NewNymTransport(cfg *NymConfig) *NymTransport          { return transport.NewNymTransport(cfg) }
func NewMixnetTransport(cfg *MixnetConfig) *MixnetTransport { return transport.NewMixnetTransport(cfg) }
