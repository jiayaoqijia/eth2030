package p2p

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/eth2030/eth2030/p2p/peermgr"
	"github.com/eth2030/eth2030/p2p/scoring"
	"github.com/eth2030/eth2030/p2p/wire"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// Config holds the configuration for a P2P Server.
type Config struct {
	// ListenAddr is the TCP address to listen on (e.g., ":30303").
	ListenAddr string

	// MaxPeers is the maximum number of connected peers.
	MaxPeers int

	// Protocols is the list of supported sub-protocols.
	Protocols []Protocol

	// StaticNodes is an initial list of enode URLs to always connect to.
	StaticNodes []string

	// EnableRLPx enables the RLPx encrypted transport (default: plaintext framing).
	EnableRLPx     bool
	RLPxPrivateKey *ecdsa.PrivateKey

	// Name is the client identity string sent in the hello handshake.
	// Defaults to "ETH2030" if empty.
	Name string

	// NodeID is the local node identifier sent during handshake.
	// If empty, a random ID is generated at start.
	NodeID string

	// ListenPort is the advertised TCP listening port (0 = auto-detect).
	ListenPort uint64

	// Dialer is the interface used for outbound connections.
	// If nil, a TCPDialer is used.
	Dialer wire.Dialer

	// Listener is the interface for accepting inbound connections.
	// If nil, a TCPListener is created from ListenAddr.
	Listener wire.Listener

	// DisableHandshake disables the devp2p hello handshake, for backward
	// compatibility with tests that connect raw TCP clients without
	// performing a handshake exchange.
	DisableHandshake bool

	// DiscoveryPort is the UDP port used for node discovery.
	// When 0, the same port as ListenAddr is used.
	DiscoveryPort int

	// NAT is the NAT traversal method string (e.g. "extip:1.2.3.4").
	// An empty string disables NAT traversal.
	NAT string

	// ExternalIP is the resolved external IP to advertise in the enode URL.
	// Populated by the node from the NAT configuration before passing to Server.
	ExternalIP net.IP

	// BootstrapNodes is a comma-separated list of enode URLs used for initial
	// peer discovery. When non-empty, these are dialed before StaticNodes.
	BootstrapNodes string
}

// baseProtocolVersion is the devp2p hello protocol version.
const baseProtocolVersion = 5

// Protocol represents a sub-protocol that runs on top of the devp2p connection.
type Protocol struct {
	Name    string
	Version uint
	Length  uint64 // Number of message codes used by this protocol.

	// Run is called for each peer that supports this protocol.
	// It should read/write messages and return when done.
	Run func(peer *peermgr.Peer, t wire.Transport) error
}

// Server manages TCP connections and peer lifecycle.
type Server struct {
	config   Config
	listener wire.Listener
	dialer   wire.Dialer
	peers    *peermgr.ManagedPeerSet
	nodes    *NodeTable
	scores   *scoring.ScoreMap
	localID  string // Node ID used in handshake.

	mu      sync.Mutex
	running bool
	quit    chan struct{}
	wg      sync.WaitGroup
}

// NewServer creates a new P2P server with the given configuration.
func NewServer(cfg Config) *Server {
	if cfg.MaxPeers <= 0 {
		cfg.MaxPeers = 25
	}
	if cfg.Name == "" {
		cfg.Name = "ETH2030"
	}
	localID := cfg.NodeID
	if localID == "" {
		localID = randomID()
	}
	return &Server{
		config:  cfg,
		dialer:  cfg.Dialer,
		peers:   peermgr.NewManagedPeerSet(cfg.MaxPeers),
		nodes:   NewNodeTable(),
		scores:  scoring.NewScoreMap(),
		localID: localID,
		quit:    make(chan struct{}),
	}
}

// dialRetryInterval is how long to wait before re-dialing a failed peer.
const dialRetryInterval = 30 * time.Second

// Start begins listening for incoming connections and dials static/bootstrap nodes.
func (srv *Server) Start() error {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	if srv.running {
		return errors.New("p2p: server already running")
	}

	if srv.config.EnableRLPx {
		key := srv.config.RLPxPrivateKey
		if key == nil {
			var err error
			key, err = gethcrypto.GenerateKey()
			if err != nil {
				return err
			}
		} else {
			if key.D == nil || key.D.Sign() <= 0 || key.D.BitLen() > 256 {
				return errors.New("p2p: invalid RLPx private key")
			}
			derived, err := gethcrypto.ToECDSA(key.D.FillBytes(make([]byte, 32)))
			if err != nil || key.Curve != gethcrypto.S256() ||
				key.X == nil || key.Y == nil || !key.PublicKey.Equal(&derived.PublicKey) {
				return errors.New("p2p: invalid RLPx private key")
			}
			key = derived
		}
		srv.config.RLPxPrivateKey = key
		srv.localID = hex.EncodeToString(gethcrypto.FromECDSAPub(&key.PublicKey)[1:])
	}

	// Set up the dialer.
	if srv.dialer == nil {
		srv.dialer = &wire.TCPDialer{}
	}

	// Set up the listener.
	if srv.config.Listener != nil {
		srv.listener = srv.config.Listener
	} else {
		ln, err := net.Listen("tcp", srv.config.ListenAddr)
		if err != nil {
			return fmt.Errorf("p2p: listen error: %w", err)
		}
		srv.listener = wire.NewTCPListener(ln)
	}

	srv.running = true

	// Load static nodes into the node table.
	for _, rawurl := range srv.config.StaticNodes {
		if node, err := ParseEnode(rawurl); err == nil {
			srv.nodes.AddStatic(node)
		}
	}

	// Load bootstrap nodes as static peers so the dial loop reaches them.
	for _, rawurl := range strings.Split(srv.config.BootstrapNodes, ",") {
		rawurl = strings.TrimSpace(rawurl)
		if rawurl == "" {
			continue
		}
		if node, err := ParseEnode(rawurl); err == nil {
			srv.nodes.AddStatic(node)
		}
	}

	srv.wg.Add(2)
	go srv.listenLoop()
	go srv.dialLoop()
	return nil
}

// dialLoop periodically dials all static nodes that are not currently connected.
// It retries on the dialRetryInterval until the server stops.
func (srv *Server) dialLoop() {
	defer srv.wg.Done()

	ticker := time.NewTicker(dialRetryInterval)
	defer ticker.Stop()

	// Dial immediately on start, then on each tick.
	srv.dialStaticNodes()
	for {
		select {
		case <-ticker.C:
			srv.dialStaticNodes()
		case <-srv.quit:
			return
		}
	}
}

// dialStaticNodes dials any static nodes that are not already connected.
func (srv *Server) dialStaticNodes() {
	nodes := srv.nodes.StaticNodes()
	for _, node := range nodes {
		if srv.peers.Get(string(node.ID)) != nil {
			continue
		}
		addr := node.Addr()
		srv.wg.Add(1)
		go func(addr string, id NodeID) {
			defer srv.wg.Done()
			var remote *ecdsa.PublicKey
			if srv.config.EnableRLPx {
				var err error
				remote, err = parseRLPxNodeKey(id)
				if err != nil {
					return
				}
			}
			ct, err := srv.dialer.Dial(addr)
			if err != nil {
				return
			}
			srv.setupConn(ct, remote)
		}(addr, node.ID)
	}
}

// Stop shuts down the server and disconnects all peers.
func (srv *Server) Stop() {
	srv.mu.Lock()
	if !srv.running {
		srv.mu.Unlock()
		return
	}
	srv.running = false
	close(srv.quit)
	srv.listener.Close()
	srv.mu.Unlock()

	srv.wg.Wait()
	srv.peers.Close()
}

// ListenAddr returns the actual listen address (useful when using ":0").
func (srv *Server) ListenAddr() net.Addr {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.listener == nil {
		return nil
	}
	return srv.listener.Addr()
}

// AddPeer dials the given address and adds the connection as a peer.
func (srv *Server) AddPeer(addr string) error {
	var remote *ecdsa.PublicKey
	if strings.HasPrefix(addr, "enode://") || srv.config.EnableRLPx {
		node, err := ParseEnode(addr)
		if err != nil {
			return err
		}
		addr = node.Addr()
		if srv.config.EnableRLPx {
			remote, err = parseRLPxNodeKey(node.ID)
			if err != nil {
				return err
			}
		}
	}
	ct, err := srv.dialer.Dial(addr)
	if err != nil {
		return err
	}

	srv.wg.Add(1)
	go func() {
		defer srv.wg.Done()
		srv.setupConn(ct, remote)
	}()
	return nil
}

func parseRLPxNodeKey(id NodeID) (*ecdsa.PublicKey, error) {
	key, err := hex.DecodeString(string(id))
	if err != nil || len(key) != 64 {
		return nil, fmt.Errorf("%w: RLPx requires a 64-byte enode public key", ErrInvalidEnode)
	}
	return gethcrypto.UnmarshalPubkey(append([]byte{4}, key...))
}

// PeerCount returns the number of connected peers.
func (srv *Server) PeerCount() int {
	return srv.peers.Len()
}

// PeersList returns a snapshot of connected peers.
func (srv *Server) PeersList() []*peermgr.Peer {
	return srv.peers.Peers()
}

// NodeTable returns the server's node discovery table.
func (srv *Server) NodeTable() *NodeTable {
	return srv.nodes
}

// Scores returns the server's peer score map.
func (srv *Server) Scores() *scoring.ScoreMap {
	return srv.scores
}

// PeerScore returns the score tracker for a connected peer.
func (srv *Server) PeerScore(id string) *scoring.PeerScore {
	return srv.scores.Get(id)
}

// Running returns whether the server is currently running.
func (srv *Server) Running() bool {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.running
}

// LocalID returns the local node identifier used in the devp2p handshake.
func (srv *Server) LocalID() string {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.localID
}

// ExternalIP returns the configured external IP for this node, if any.
// Used by admin_nodeInfo to build a reachable enode URL.
func (srv *Server) ExternalIP() net.IP {
	return srv.config.ExternalIP
}

func (srv *Server) listenLoop() {
	defer srv.wg.Done()

	for {
		ct, err := srv.listener.Accept()
		if err != nil {
			select {
			case <-srv.quit:
				return
			default:
				log.Printf("p2p: accept error: %v", err)
				continue
			}
		}

		srv.wg.Add(1)
		go func() {
			defer srv.wg.Done()
			srv.setupConn(ct, nil)
		}()
	}
}

// localHello builds the local hello packet from the server's configuration.
func (srv *Server) localHello() *wire.HelloPacket {
	caps := make([]wire.Cap, len(srv.config.Protocols))
	for i, p := range srv.config.Protocols {
		caps[i] = wire.Cap{Name: p.Name, Version: p.Version}
	}
	id := srv.LocalID()
	if srv.config.EnableRLPx {
		id = string(gethcrypto.FromECDSAPub(&srv.config.RLPxPrivateKey.PublicKey)[1:])
	}
	return &wire.HelloPacket{
		Version:    baseProtocolVersion,
		Name:       srv.config.Name,
		Caps:       caps,
		ListenPort: srv.config.ListenPort,
		ID:         id,
	}
}

// setupConn handles a new connection: performs handshake, creates a peer,
// and runs all matching protocols via the multiplexer.
func (srv *Server) setupConn(ct wire.ConnTransport, remote *ecdsa.PublicKey) {
	var tr wire.Transport = ct
	var encrypted *wire.RLPxTransport
	var authenticatedID []byte

	// Optionally wrap with RLPx encryption.
	if srv.config.EnableRLPx {
		// Check for shutdown before expensive handshake.
		select {
		case <-srv.quit:
			ct.Close()
			return
		default:
		}

		raw, ok := ct.(interface{ Conn() net.Conn })
		if !ok {
			ct.Close()
			return
		}
		encrypted = wire.NewRLPxTransport(raw.Conn())
		peerKey, err := encrypted.Handshake(srv.config.RLPxPrivateKey, remote)
		if err != nil {
			ct.Close()
			return
		}
		authenticatedID = gethcrypto.FromECDSAPub(peerKey)[1:]
		tr = encrypted
	}

	// Perform devp2p hello handshake (unless disabled).
	var peerID string
	var peerCaps []wire.Cap

	if !srv.config.DisableHandshake {
		// Check for shutdown before handshake.
		select {
		case <-srv.quit:
			tr.Close()
			return
		default:
		}

		var remoteHello *wire.HelloPacket
		var err error
		if encrypted != nil {
			remoteHello, err = wire.PerformRLPxHandshake(encrypted, srv.localHello())
		} else {
			remoteHello, err = wire.PerformHandshake(tr, srv.localHello())
		}
		if err != nil {
			tr.Close()
			return
		}
		peerID = remoteHello.ID
		if encrypted != nil {
			if remoteHello.ID != string(authenticatedID) {
				tr.Close()
				return
			}
			peerID = hex.EncodeToString(authenticatedID)
			encrypted.SetSnappy(remoteHello.Version >= 5)
		}
		peerCaps = remoteHello.Caps
	} else if encrypted != nil {
		peerID = hex.EncodeToString(authenticatedID)
	} else {
		// Legacy mode: generate a random peer ID with no handshake.
		peerID = randomID()
	}

	peer := peermgr.NewPeer(peerID, ct.RemoteAddr(), peerCaps)
	score := srv.scores.Get(peerID)

	if err := srv.peers.Add(peer); err != nil {
		tr.Close()
		return
	}

	// Record successful handshake.
	score.HandshakeOK()

	defer func() {
		srv.peers.Remove(peer.ID())
		srv.scores.Remove(peer.ID())
		tr.Close()
	}()

	protos := srv.config.Protocols
	if !srv.config.DisableHandshake {
		selected := make(map[string]Protocol)
		for _, local := range protos {
			for _, remote := range peerCaps {
				if local.Name == remote.Name && local.Version == remote.Version {
					if prior, ok := selected[local.Name]; !ok || local.Version > prior.Version {
						selected[local.Name] = local
					}
				}
			}
		}
		protos = nil
		for _, proto := range selected {
			protos = append(protos, proto)
		}
	}
	if len(protos) == 0 {
		// No protocol handler; wait until quit.
		<-srv.quit
		return
	}

	// Single protocol: run directly (backwards compatible with existing tests).
	if len(protos) == 1 && encrypted == nil {
		proto := protos[0]
		if proto.Run != nil {
			err := proto.Run(peer, tr)
			if err != nil {
				score.BadResponse()
			} else {
				score.GoodResponse()
			}
		}
		return
	}

	// Multiple protocols: use multiplexer.
	baseOffset := uint64(0)
	if encrypted != nil {
		baseOffset = 0x10
	}
	mux := newMultiplexer(tr, protos, baseOffset)

	// Start the read loop in the background.
	readErr := make(chan error, 1)
	go func() {
		readErr <- mux.ReadLoop()
	}()

	// Run each protocol in its own goroutine.
	var protoWG sync.WaitGroup
	for _, rw := range mux.Protocols() {
		protoWG.Add(1)
		go func(rw *ProtoRW) {
			defer protoWG.Done()
			if rw.proto.Run != nil {
				// Create a multiplexed transport adapter.
				adapter := &muxTransportAdapter{mux: mux, rw: rw}
				if err := rw.proto.Run(peer, adapter); err != nil {
					score.BadResponse()
					mux.Close()
				} else {
					score.GoodResponse()
				}
			}
		}(rw)
	}

	// Wait for the read loop to end (connection closed) or all protocols to finish.
	done := make(chan struct{})
	go func() {
		protoWG.Wait()
		close(done)
	}()

	select {
	case <-done:
		mux.Close()
	case <-readErr:
		mux.Close()
		protoWG.Wait()
	case <-srv.quit:
		mux.Close()
		protoWG.Wait()
	}
}

// muxTransportAdapter wraps the multiplexer to implement the Transport interface
// for a single protocol.
type muxTransportAdapter struct {
	mux *Multiplexer
	rw  *ProtoRW
}

func (a *muxTransportAdapter) ReadMsg() (wire.Msg, error) {
	return a.rw.ReadMsg()
}

func (a *muxTransportAdapter) WriteMsg(msg wire.Msg) error {
	return a.mux.WriteMsg(a.rw, msg)
}

func (a *muxTransportAdapter) Close() error {
	a.mux.Close()
	return nil
}

// randomID generates a random 32-byte hex-encoded peer ID.
func randomID() string {
	var b [32]byte
	rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
