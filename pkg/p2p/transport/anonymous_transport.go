package transport

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
)

// MixnetTransportMode selects which external mixnet to use for anonymous submission.
// CLI flag: --mixnet=simulated|tor|nym
type MixnetTransportMode int

const (
	// ModeSimulated uses the in-process simulated mixnet (default).
	ModeSimulated MixnetTransportMode = iota
	// ModeTorSocks5 routes via a Tor SOCKS5 proxy at TorProxyAddr.
	ModeTorSocks5
	// ModeNymSocks5 routes via a Nym SOCKS5 proxy at NymProxyAddr.
	ModeNymSocks5
)

// ParseMixnetMode parses a --mixnet=simulated|tor|nym string into MixnetTransportMode.
func ParseMixnetMode(s string) (MixnetTransportMode, error) {
	switch s {
	case "simulated", "":
		return ModeSimulated, nil
	case "tor":
		return ModeTorSocks5, nil
	case "nym":
		return ModeNymSocks5, nil
	default:
		return ModeSimulated, fmt.Errorf("unknown mixnet mode %q: use simulated|tor|nym", s)
	}
}

// String returns the CLI string for a MixnetTransportMode.
func (m MixnetTransportMode) String() string {
	switch m {
	case ModeTorSocks5:
		return "tor"
	case ModeNymSocks5:
		return "nym"
	default:
		return "simulated"
	}
}

// ExternalMixnetTransport extends AnonymousTransport with a raw-bytes send method
// for transports that route via an external anonymizing network (Tor, Nym).
type ExternalMixnetTransport interface {
	AnonymousTransport
	// SendViaExternalMixnet sends raw transaction bytes to a JSON-RPC endpoint
	// via the external mixnet (SOCKS5 tunnel). endpoint is an HTTP URL.
	SendViaExternalMixnet(tx []byte, endpoint string) error
}

// TransportConfig configures transport selection for TransportManager.
type TransportConfig struct {
	// Mode specifies which mixnet to use (--mixnet=simulated|tor|nym).
	Mode MixnetTransportMode

	// TorProxyAddr is the Tor SOCKS5 proxy address (default: 127.0.0.1:9050).
	TorProxyAddr string

	// NymProxyAddr is the Nym SOCKS5 proxy address (default: 127.0.0.1:1080).
	NymProxyAddr string

	// RPCEndpoint is the node's own JSON-RPC endpoint for tx submission.
	RPCEndpoint string

	// DialTimeout is the timeout for probing proxy availability (default: 500ms).
	DialTimeout time.Duration

	// KohakuCompatible enables Kohaku wire format for control messages.
	// TODO: update once the Kohaku spec is finalized (@ncsgy repo).
	KohakuCompatible bool
}

// DefaultTransportConfig returns sensible defaults.
func DefaultTransportConfig() TransportConfig {
	return TransportConfig{
		Mode:         ModeSimulated,
		TorProxyAddr: "127.0.0.1:9050",
		NymProxyAddr: "127.0.0.1:1080",
		RPCEndpoint:  "http://127.0.0.1:8545",
		DialTimeout:  500 * time.Millisecond,
	}
}

// controlMsg is the JSON envelope for non-Kohaku control messages.
type controlMsg struct {
	Type string `json:"type"`
	Msg  string `json:"msg"`
}

// FormatControlMessage formats a transport control message.
// When kohaku is true, uses Kohaku wire format: 4-byte big-endian length + UTF-8 payload.
// When kohaku is false, uses JSON: {"type":"control","msg":"..."}.
func FormatControlMessage(msg string, kohaku bool) []byte {
	if kohaku {
		// Kohaku wire format: [4-byte big-endian length][payload bytes]
		// TODO: replace with real Kohaku spec encoding once spec is published.
		payload := []byte(msg)
		out := make([]byte, 4+len(payload))
		binary.BigEndian.PutUint32(out[:4], uint32(len(payload)))
		copy(out[4:], payload)
		return out
	}
	// JSON format.
	b, _ := json.Marshal(controlMsg{Type: "control", Msg: msg})
	return b
}

// ProbeProxy attempts a TCP dial to addr within timeout, returning true if
// the connection succeeds. Used for transport fallback selection.
func ProbeProxy(addr string, timeout time.Duration) bool {
	return probeProxy(addr, timeout)
}

// probeProxy attempts a TCP dial to addr within timeout, returning true if
// the connection succeeds. Used for transport fallback selection.
func probeProxy(addr string, timeout time.Duration) bool {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// Anonymous transport errors.
var (
	ErrAnonTransportClosed   = errors.New("anon_transport: transport is closed")
	ErrAnonTransportExists   = errors.New("anon_transport: transport already registered")
	ErrAnonTransportNotFound = errors.New("anon_transport: transport not found")
	ErrAnonTransportNilTx    = errors.New("anon_transport: nil transaction")
)

// AnonymousTransport is the interface for anonymous transaction submission.
// Implementations hide the sender's IP address from the P2P network.
type AnonymousTransport interface {
	// Name returns the transport identifier (e.g., "tor", "mixnet", "flashnet").
	Name() string
	// Submit sends a transaction through the anonymous transport.
	Submit(tx *types.Transaction) error
	// Receive returns a channel of transactions received via this transport.
	Receive() <-chan *types.Transaction
	// Start initializes the transport.
	Start() error
	// Stop shuts down the transport.
	Stop() error
}

// TransportStats holds statistics for an anonymous transport.
type TransportStats struct {
	Name      string
	Submitted uint64
	Received  uint64
	Errors    uint64
	Running   bool
}

// TransportManager manages multiple anonymous transports and provides
// a unified interface for anonymous transaction submission.
type TransportManager struct {
	mu           sync.RWMutex
	transports   map[string]AnonymousTransport
	stats        map[string]*TransportStats
	closed       bool
	config       TransportConfig
	selectedMode MixnetTransportMode
}

// NewTransportManager creates a new transport manager with default config.
func NewTransportManager() *TransportManager {
	return NewTransportManagerWithConfig(DefaultTransportConfig())
}

// NewTransportManagerWithConfig creates a transport manager with the given config.
func NewTransportManagerWithConfig(cfg TransportConfig) *TransportManager {
	return &TransportManager{
		transports:   make(map[string]AnonymousTransport),
		stats:        make(map[string]*TransportStats),
		config:       cfg,
		selectedMode: ModeSimulated,
	}
}

// Config returns the transport configuration.
func (tm *TransportManager) Config() TransportConfig {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.config
}

// SelectedMode returns the currently selected transport mode.
func (tm *TransportManager) SelectedMode() MixnetTransportMode {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.selectedMode
}

// setSelectedMode sets the selected transport mode (used internally and in tests).
func (tm *TransportManager) setSelectedMode(m MixnetTransportMode) {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.selectedMode = m
}

// SelectBestTransport probes Tor and Nym proxy availability and selects the
// best available transport mode using priority: Tor > Nym > Simulated.
// It sets the manager's selectedMode and returns the chosen mode.
// Intended to be called once at startup to log transport selection.
func (tm *TransportManager) SelectBestTransport() MixnetTransportMode {
	cfg := tm.Config()

	if probeProxy(cfg.TorProxyAddr, cfg.DialTimeout) {
		tm.setSelectedMode(ModeTorSocks5)
		slog.Info("anonymous transport selected", "mode", "tor", "proxy", cfg.TorProxyAddr)
		return ModeTorSocks5
	}
	if probeProxy(cfg.NymProxyAddr, cfg.DialTimeout) {
		tm.setSelectedMode(ModeNymSocks5)
		slog.Info("anonymous transport selected", "mode", "nym", "proxy", cfg.NymProxyAddr)
		return ModeNymSocks5
	}
	tm.setSelectedMode(ModeSimulated)
	slog.Info("anonymous transport selected", "mode", "simulated", "reason", "no external proxy reachable")
	return ModeSimulated
}

// RegisterTransport adds an anonymous transport to the manager.
func (tm *TransportManager) RegisterTransport(t AnonymousTransport) error {
	if t == nil {
		return ErrAnonTransportNilTx
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.closed {
		return ErrAnonTransportClosed
	}

	name := t.Name()
	if _, exists := tm.transports[name]; exists {
		return ErrAnonTransportExists
	}

	tm.transports[name] = t
	tm.stats[name] = &TransportStats{Name: name}
	return nil
}

// UnregisterTransport removes a transport from the manager.
func (tm *TransportManager) UnregisterTransport(name string) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	t, exists := tm.transports[name]
	if !exists {
		return ErrAnonTransportNotFound
	}

	_ = t.Stop()
	delete(tm.transports, name)
	delete(tm.stats, name)
	return nil
}

// SubmitAnonymous submits a tx via all registered transports.
// Returns the number of successful submissions and any errors.
func (tm *TransportManager) SubmitAnonymous(tx *types.Transaction) (int, []error) {
	if tx == nil {
		return 0, []error{ErrAnonTransportNilTx}
	}
	tm.mu.RLock()
	if tm.closed {
		tm.mu.RUnlock()
		return 0, []error{ErrAnonTransportClosed}
	}
	type entry struct {
		name      string
		transport AnonymousTransport
		stats     *TransportStats
	}
	entries := make([]entry, 0, len(tm.transports))
	for name, transport := range tm.transports {
		entries = append(entries, entry{name, transport, tm.stats[name]})
	}
	tm.mu.RUnlock()
	submitted := 0
	var errs []error
	for _, entry := range entries {
		err := entry.transport.Submit(tx)
		tm.mu.Lock()
		if current := tm.stats[entry.name]; current != nil && current == entry.stats {
			if err != nil {
				current.Errors++
			} else {
				current.Submitted++
			}
		}
		tm.mu.Unlock()
		if err != nil {
			errs = append(errs, err)
		} else {
			submitted++
		}
	}
	return submitted, errs
}

// StartAll starts all registered transports.
func (tm *TransportManager) StartAll() []error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	var errs []error
	for name, t := range tm.transports {
		if err := t.Start(); err != nil {
			errs = append(errs, err)
		} else if s := tm.stats[name]; s != nil {
			s.Running = true
		}
	}
	return errs
}

// StopAll stops all registered transports.
func (tm *TransportManager) StopAll() []error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	tm.closed = true
	var errs []error
	for name, t := range tm.transports {
		if err := t.Stop(); err != nil {
			errs = append(errs, err)
		}
		if s := tm.stats[name]; s != nil {
			s.Running = false
		}
	}
	return errs
}

// GetStats returns a copy of the stats for all transports.
func (tm *TransportManager) GetStats() []TransportStats {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	var result []TransportStats
	for _, s := range tm.stats {
		result = append(result, *s)
	}
	return result
}

// TransportCount returns the number of registered transports.
func (tm *TransportManager) TransportCount() int {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return len(tm.transports)
}
