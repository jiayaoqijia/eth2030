package node

import (
	"os"
	"path/filepath"
	"testing"

	nodebackend "github.com/eth2030/eth2030/node/backend"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("default config should be valid: %v", err)
	}
	if cfg.P2PPort != 30303 {
		t.Errorf("expected P2P port 30303, got %d", cfg.P2PPort)
	}
	if cfg.RPCPort != 8545 {
		t.Errorf("expected RPC port 8545, got %d", cfg.RPCPort)
	}
	if cfg.EnginePort != 8551 {
		t.Errorf("expected Engine port 8551, got %d", cfg.EnginePort)
	}
	if cfg.Network != "mainnet" {
		t.Errorf("expected network mainnet, got %s", cfg.Network)
	}
	if cfg.NetworkID != 1 {
		t.Errorf("expected network id 1, got %d", cfg.NetworkID)
	}
	if cfg.SyncMode != "snap" {
		t.Errorf("expected sync mode snap, got %s", cfg.SyncMode)
	}
	if cfg.MaxPeers != 50 {
		t.Errorf("expected max peers 50, got %d", cfg.MaxPeers)
	}
	if cfg.Verbosity != 3 {
		t.Errorf("expected verbosity 3, got %d", cfg.Verbosity)
	}
	if cfg.Metrics {
		t.Error("expected metrics false by default")
	}

	// DataDir should point to ~/.ETH2030.
	home, err := os.UserHomeDir()
	if err == nil {
		want := filepath.Join(home, ".ETH2030")
		if cfg.DataDir != want {
			t.Errorf("expected DataDir %q, got %q", want, cfg.DataDir)
		}
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{
			name:    "valid default",
			modify:  func(c *Config) {},
			wantErr: false,
		},
		{
			name:    "empty datadir",
			modify:  func(c *Config) { c.DataDir = "" },
			wantErr: true,
		},
		{
			name:    "invalid network",
			modify:  func(c *Config) { c.Network = "foonet" },
			wantErr: true,
		},
		{
			name:    "invalid sync mode",
			modify:  func(c *Config) { c.SyncMode = "turbo" },
			wantErr: true,
		},
		{
			name:    "invalid port",
			modify:  func(c *Config) { c.P2PPort = -1 },
			wantErr: true,
		},
		{
			name:    "invalid log level",
			modify:  func(c *Config) { c.LogLevel = "verbose" },
			wantErr: true,
		},
		{
			name:    "sepolia network",
			modify:  func(c *Config) { c.Network = "sepolia" },
			wantErr: false,
		},
		{
			name:    "holesky network",
			modify:  func(c *Config) { c.Network = "holesky" },
			wantErr: false,
		},
		{
			name:    "verbosity too low",
			modify:  func(c *Config) { c.Verbosity = -1 },
			wantErr: true,
		},
		{
			name:    "verbosity too high",
			modify:  func(c *Config) { c.Verbosity = 6 },
			wantErr: true,
		},
		{
			name:    "verbosity zero",
			modify:  func(c *Config) { c.Verbosity = 0 },
			wantErr: false,
		},
		{
			name:    "verbosity five",
			modify:  func(c *Config) { c.Verbosity = 5 },
			wantErr: false,
		},
		{
			name:    "sync mode full",
			modify:  func(c *Config) { c.SyncMode = "full" },
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.modify(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfigAddrs(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.P2PAddr() != ":30303" {
		t.Errorf("P2PAddr() = %s, want :30303", cfg.P2PAddr())
	}
	if cfg.RPCAddr() != "0.0.0.0:8545" {
		t.Errorf("RPCAddr() = %s, want 0.0.0.0:8545", cfg.RPCAddr())
	}
	if cfg.EngineAddr() != "0.0.0.0:8551" {
		t.Errorf("EngineAddr() = %s, want 0.0.0.0:8551", cfg.EngineAddr())
	}
}

func TestNewNode(t *testing.T) {
	// Use ephemeral ports to avoid conflicts.
	cfg := makeTestConfig(t)
	n := newTestNode(t, &cfg)
	if n.Blockchain() == nil {
		t.Error("blockchain should not be nil")
	}
	if n.TxPool() == nil {
		t.Error("txpool should not be nil")
	}
	if n.Config().Network != "mainnet" {
		t.Errorf("expected mainnet, got %s", n.Config().Network)
	}

	// Genesis block should exist.
	genesis := n.Blockchain().Genesis()
	if genesis == nil {
		t.Fatal("genesis block should not be nil")
	}
	if genesis.NumberU64() != 0 {
		t.Errorf("genesis number = %d, want 0", genesis.NumberU64())
	}
}

func TestNewNode_NilConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	// Passing nil should use default config.
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
		t.Errorf("expected mainnet, got %s", n.Config().Network)
	}
}

func TestNewNode_InvalidConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Network = "badnet"
	_, err := New(&cfg)
	if err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestNode_StartStop(t *testing.T) {
	cfg := makeTestConfig(t)
	n := newTestNode(t, &cfg)

	if err := n.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Double start should error.
	if err := n.Start(); err == nil {
		t.Error("expected error on double start")
	}

	if err := n.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}
}

func TestNode_StopWithoutStart(t *testing.T) {
	cfg := makeTestConfig(t)
	n := newTestNode(t, &cfg)

	// Stop on a node that was never started should be a no-op.
	if err := n.Stop(); err != nil {
		t.Fatalf("Stop() on non-started node should not error: %v", err)
	}
}

func TestNode_DoubleStop(t *testing.T) {
	cfg := makeTestConfig(t)
	n := newTestNode(t, &cfg)

	if err := n.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if err := n.Stop(); err != nil {
		t.Fatalf("first Stop() error: %v", err)
	}

	// Second stop should be a no-op (not panic on closed channel).
	if err := n.Stop(); err != nil {
		t.Fatalf("second Stop() should not error: %v", err)
	}
}

func TestNode_Running(t *testing.T) {
	cfg := makeTestConfig(t)
	n := newTestNode(t, &cfg)

	if n.Running() {
		t.Error("node should not be running before Start()")
	}

	if err := n.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	if !n.Running() {
		t.Error("node should be running after Start()")
	}

	if err := n.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if n.Running() {
		t.Error("node should not be running after Stop()")
	}
}

func TestNode_Lifecycle(t *testing.T) {
	// Full lifecycle test: create, start, verify subsystems, stop.
	cfg := makeTestConfig(t)
	n := newTestNode(t, &cfg)

	// Verify subsystems are initialized before start.
	if n.Blockchain() == nil {
		t.Fatal("blockchain should be initialized after New()")
	}
	if n.TxPool() == nil {
		t.Fatal("txpool should be initialized after New()")
	}
	if n.Config() == nil {
		t.Fatal("config should be initialized after New()")
	}

	// Genesis should be accessible.
	genesis := n.Blockchain().Genesis()
	if genesis == nil {
		t.Fatal("genesis block should exist")
	}
	if genesis.NumberU64() != 0 {
		t.Errorf("genesis block number = %d, want 0", genesis.NumberU64())
	}

	// Start the node.
	if err := n.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	// Verify it is running.
	if !n.Running() {
		t.Error("node should be running after Start()")
	}

	// Stop the node.
	if err := n.Stop(); err != nil {
		t.Fatalf("Stop() error: %v", err)
	}

	if n.Running() {
		t.Error("node should not be running after Stop()")
	}
}

func TestNode_Backend(t *testing.T) {
	cfg := makeTestConfig(t)
	n := newTestNode(t, &cfg)

	// Test the RPC backend adapter.
	backend := nodebackend.NewRPCBackend(toNodeDeps(n), n.engBackend)

	// ChainID should match mainnet.
	chainID := backend.ChainID()
	if chainID == nil || chainID.Int64() != 1 {
		t.Errorf("ChainID() = %v, want 1", chainID)
	}

	// CurrentHeader should return genesis.
	header := backend.CurrentHeader()
	if header == nil {
		t.Fatal("CurrentHeader() should not be nil")
	}
	if header.Number.Uint64() != 0 {
		t.Errorf("CurrentHeader().Number = %d, want 0", header.Number.Uint64())
	}

	// SuggestGasPrice should return non-nil.
	price := backend.SuggestGasPrice()
	if price == nil || price.Sign() <= 0 {
		t.Error("SuggestGasPrice() should return positive value")
	}
}

func TestVerbosityToLogLevel(t *testing.T) {
	tests := []struct {
		verbosity int
		wantLevel string
	}{
		{0, "error"},
		{1, "error"},
		{2, "warn"},
		{3, "info"},
		{4, "debug"},
		{5, "debug"},
	}
	for _, tt := range tests {
		got := VerbosityToLogLevel(tt.verbosity)
		if got != tt.wantLevel {
			t.Errorf("VerbosityToLogLevel(%d) = %q, want %q", tt.verbosity, got, tt.wantLevel)
		}
	}
}

func TestInitDataDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ETH2030-test")

	cfg := DefaultConfig()
	cfg.DataDir = dir

	if err := cfg.InitDataDir(); err != nil {
		t.Fatalf("InitDataDir() error: %v", err)
	}

	// Verify root directory.
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("datadir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("datadir is not a directory")
	}

	// Verify subdirectories.
	for _, sub := range dataDirSubdirs {
		subpath := filepath.Join(dir, sub)
		info, err := os.Stat(subpath)
		if err != nil {
			t.Errorf("subdir %q not created: %v", sub, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("subdir %q is not a directory", sub)
		}
	}
}

func TestInitDataDir_Idempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ETH2030-test")

	cfg := DefaultConfig()
	cfg.DataDir = dir

	if err := cfg.InitDataDir(); err != nil {
		t.Fatalf("first InitDataDir() error: %v", err)
	}

	// Write a file to verify it is not deleted.
	marker := filepath.Join(dir, "chaindata", "marker")
	if err := os.WriteFile(marker, []byte("test"), 0600); err != nil {
		t.Fatalf("write marker: %v", err)
	}

	if err := cfg.InitDataDir(); err != nil {
		t.Fatalf("second InitDataDir() error: %v", err)
	}

	// Verify marker still exists.
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("marker file removed after second init: %v", err)
	}
}

func TestInitDataDir_EmptyPath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = ""
	if err := cfg.InitDataDir(); err == nil {
		t.Fatal("expected error for empty datadir")
	}
}

func TestConfig_ResolvePath(t *testing.T) {
	cfg := DefaultConfig()
	cfg.DataDir = "/data/ETH2030"

	// Relative path should be resolved under datadir.
	got := cfg.ResolvePath("chaindata")
	want := "/data/ETH2030/chaindata"
	if got != want {
		t.Errorf("ResolvePath(chaindata) = %q, want %q", got, want)
	}

	// Absolute path should be returned as-is.
	got = cfg.ResolvePath("/absolute/path")
	want = "/absolute/path"
	if got != want {
		t.Errorf("ResolvePath(/absolute/path) = %q, want %q", got, want)
	}
}
