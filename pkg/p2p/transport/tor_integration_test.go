//go:build integration

package transport

import (
	"os"
	"testing"
	"time"
)

// TestTorTransport_RealTor requires a live Tor SOCKS5 daemon at 127.0.0.1:9050.
// Run with: go test -tags=integration ./p2p/... -run TestTorTransport_RealTor
func TestTorTransport_RealTor(t *testing.T) {
	endpoint := os.Getenv("ETH2030_TOR_TEST_RPC")
	if endpoint == "" {
		t.Skip("set ETH2030_TOR_TEST_RPC to an endpoint reachable through Tor")
	}

	cfg := DefaultTorConfig()
	cfg.RPCEndpoint = endpoint
	if !probeProxy(cfg.ProxyAddr, 500*time.Millisecond) {
		t.Fatalf("Tor SOCKS5 not available at %s", cfg.ProxyAddr)
	}

	tr := NewTorTransport(cfg)
	if err := tr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Stop()

	payload := []byte{0xde, 0xad, 0xbe, 0xef}
	if err := tr.SendViaExternalMixnet(payload, cfg.RPCEndpoint); err != nil {
		t.Fatalf("SendViaExternalMixnet via Tor: %v", err)
	}
}

// TestNymTransport_RealNym requires a live Nym SOCKS5 client at 127.0.0.1:1080.
// Run with: go test -tags=integration ./p2p/... -run TestNymTransport_RealNym
func TestNymTransport_RealNym(t *testing.T) {
	endpoint := os.Getenv("ETH2030_NYM_TEST_RPC")
	if endpoint == "" {
		t.Skip("set ETH2030_NYM_TEST_RPC to an endpoint reachable through Nym")
	}

	cfg := DefaultNymConfig()
	cfg.RPCEndpoint = endpoint
	if !probeProxy(cfg.ProxyAddr, 500*time.Millisecond) {
		t.Fatalf("Nym SOCKS5 not available at %s", cfg.ProxyAddr)
	}

	tr := NewNymTransport(cfg)
	if err := tr.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer tr.Stop()

	payload := []byte{0xca, 0xfe, 0xba, 0xbe}
	if err := tr.SendViaExternalMixnet(payload, cfg.RPCEndpoint); err != nil {
		t.Fatalf("SendViaExternalMixnet via Nym: %v", err)
	}
}
