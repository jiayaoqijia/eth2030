package node

import (
	"testing"

	nodebackend "github.com/eth2030/eth2030/node/backend"
)

// TestNodeCreate verifies that a Node can be created with default config
// and that all subsystems are initialized.
func TestNodeCreate(t *testing.T) {
	cfg := makeTestConfig(t)
	n := newTestNode(t, &cfg)

	// Blockchain must be initialized.
	if n.Blockchain() == nil {
		t.Fatal("blockchain should be initialized")
	}

	// TxPool must be initialized.
	if n.TxPool() == nil {
		t.Fatal("txpool should be initialized")
	}

	// Config must be set.
	if n.Config() == nil {
		t.Fatal("config should be initialized")
	}
	if n.Config().Network != "mainnet" {
		t.Errorf("network = %s, want mainnet", n.Config().Network)
	}

	// Genesis block must exist.
	genesis := n.Blockchain().Genesis()
	if genesis == nil {
		t.Fatal("genesis block should exist")
	}
	if genesis.NumberU64() != 0 {
		t.Errorf("genesis number = %d, want 0", genesis.NumberU64())
	}

	// Node should not be running before Start.
	if n.Running() {
		t.Error("node should not be running before Start()")
	}
}

// TestNodeConfigValidation verifies that invalid configurations are rejected
// when creating a Node.
func TestNodeConfigValidation(t *testing.T) {
	tests := []struct {
		name   string
		modify func(*Config)
	}{
		{
			name:   "invalid network",
			modify: func(c *Config) { c.Network = "badnet" },
		},
		{
			name:   "empty datadir",
			modify: func(c *Config) { c.DataDir = "" },
		},
		{
			name:   "invalid sync mode",
			modify: func(c *Config) { c.SyncMode = "turbo" },
		},
		{
			name:   "invalid port",
			modify: func(c *Config) { c.P2PPort = -1 },
		},
		{
			name:   "invalid log level",
			modify: func(c *Config) { c.LogLevel = "verbose" },
		},
		{
			name:   "verbosity too high",
			modify: func(c *Config) { c.Verbosity = 6 },
		},
		{
			name:   "verbosity too low",
			modify: func(c *Config) { c.Verbosity = -1 },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := makeTestConfig(t)
			tt.modify(&cfg)

			_, err := New(&cfg)
			if err == nil {
				t.Fatal("expected error for invalid config")
			}
		})
	}
}

// TestNodeCreateWithNilConfig verifies that passing nil config uses defaults.
func TestNodeCreateWithNilConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	n, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil) error: %v", err)
	}
	t.Cleanup(func() {
		if err := n.Stop(); err != nil {
			t.Errorf("Stop() error: %v", err)
		}
	})
	if n.Config().Network != "mainnet" {
		t.Errorf("network = %s, want mainnet", n.Config().Network)
	}
	if n.Config().SyncMode != "snap" {
		t.Errorf("sync mode = %s, want snap", n.Config().SyncMode)
	}
}

// TestNodeStartStopLifecycle verifies the full node lifecycle: create, start,
// verify running state, stop, verify stopped state.
func TestNodeStartStopLifecycle(t *testing.T) {
	cfg := makeTestConfig(t)
	n := newTestNode(t, &cfg)

	// Start.
	if err := n.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}
	if !n.Running() {
		t.Error("node should be running after Start()")
	}

	// Double start should fail.
	if err := n.Start(); err == nil {
		t.Error("expected error on double Start()")
	}

	// Stop.
	if err := n.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
	if n.Running() {
		t.Error("node should not be running after Stop()")
	}
}

// TestNodeSubsystemsAvailable verifies that all subsystems are accessible
// after node creation.
func TestNodeSubsystemsAvailable(t *testing.T) {
	cfg := makeTestConfig(t)
	n := newTestNode(t, &cfg)

	// Blockchain should have genesis as current head.
	head := n.Blockchain().CurrentBlock()
	if head == nil {
		t.Fatal("current block should not be nil")
	}
	if head.NumberU64() != 0 {
		t.Errorf("current block number = %d, want 0 (genesis)", head.NumberU64())
	}

	// TxPool should be initialized and empty.
	pool := n.TxPool()
	if pool == nil {
		t.Fatal("txpool should not be nil")
	}

	// Chain length should be 1 (genesis only).
	if n.Blockchain().ChainLength() != 1 {
		t.Errorf("chain length = %d, want 1", n.Blockchain().ChainLength())
	}
}

// TestNodeNetworkConfigs verifies that nodes can be created with different
// network configurations.
func TestNodeNetworkConfigs(t *testing.T) {
	networks := []string{"mainnet", "sepolia", "holesky"}
	for _, network := range networks {
		t.Run(network, func(t *testing.T) {
			cfg := makeTestConfig(t)
			cfg.Network = network
			n := newTestNode(t, &cfg)
			if n.Config().Network != network {
				t.Errorf("network = %s, want %s", n.Config().Network, network)
			}
			if n.Blockchain() == nil {
				t.Error("blockchain should be initialized")
			}
			if n.Blockchain().Genesis() == nil {
				t.Error("genesis should exist")
			}
		})
	}
}

// TestNodeBackendIntegration verifies the RPC backend adapter provides
// correct chain data from the node's blockchain.
func TestNodeBackendIntegration(t *testing.T) {
	cfg := makeTestConfig(t)
	n := newTestNode(t, &cfg)

	backend := nodebackend.NewRPCBackend(toNodeDeps(n), n.engBackend)

	// ChainID should match mainnet (1).
	chainID := backend.ChainID()
	if chainID == nil || chainID.Int64() != 1 {
		t.Errorf("ChainID() = %v, want 1", chainID)
	}

	// CurrentHeader should return genesis header.
	header := backend.CurrentHeader()
	if header == nil {
		t.Fatal("CurrentHeader() should not be nil")
	}
	if header.Number.Uint64() != 0 {
		t.Errorf("CurrentHeader().Number = %d, want 0", header.Number.Uint64())
	}

	// SuggestGasPrice should return a positive value.
	price := backend.SuggestGasPrice()
	if price == nil || price.Sign() <= 0 {
		t.Error("SuggestGasPrice() should return positive value")
	}
}
