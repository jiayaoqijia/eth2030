package main

import (
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"testing"

	"github.com/ethereum/go-ethereum/eth/ethconfig"

	"github.com/eth2030/eth2030/geth"
)

func TestMapEthConfigMainnet(t *testing.T) {
	cfg := mapEthConfig(ethConfigParams{network: "mainnet", syncMode: "snap"})
	if cfg.Genesis == nil {
		t.Fatal("expected mainnet genesis to be non-nil")
	}
	if cfg.NetworkId != 1 {
		t.Fatalf("expected network id 1, got %d", cfg.NetworkId)
	}
	if cfg.SyncMode != ethconfig.SnapSync {
		t.Fatalf("expected snap sync, got %v", cfg.SyncMode)
	}
}

func TestMapEthConfigSepolia(t *testing.T) {
	cfg := mapEthConfig(ethConfigParams{network: "sepolia", syncMode: "full"})
	if cfg.Genesis == nil {
		t.Fatal("expected sepolia genesis to be non-nil")
	}
	if cfg.NetworkId != 11155111 {
		t.Fatalf("expected network id 11155111, got %d", cfg.NetworkId)
	}
	if cfg.SyncMode != ethconfig.FullSync {
		t.Fatalf("expected full sync, got %v", cfg.SyncMode)
	}
}

func TestMapEthConfigHolesky(t *testing.T) {
	// go-ethereum v1.17.x removed the Holesky built-in genesis (superseded by
	// Hoodi); the holesky network alias now maps to Hoodi.
	cfg := mapEthConfig(ethConfigParams{network: "holesky", syncMode: "snap"})
	if cfg.Genesis == nil {
		t.Fatal("expected holesky (mapped to hoodi) genesis to be non-nil")
	}
	if cfg.NetworkId != 560048 {
		t.Fatalf("expected network id 560048, got %d", cfg.NetworkId)
	}
}

func TestMapNodeConfig(t *testing.T) {
	cfg := mapNodeConfig(nodeConfigParams{
		datadir: "/tmp/test", name: "test-node", network: "mainnet",
		p2pPort: 30303, httpAddr: "127.0.0.1", httpPort: 8545,
		authAddr: "127.0.0.1", authPort: 8551, authVhosts: "localhost",
		maxPeers: 25, jwtSecret: "/tmp/jwt",
		httpVhosts: "localhost", httpCORSDomain: "*", httpAPI: "eth,web3",
		discoveryPort: 30303,
	})
	if cfg.DataDir != "/tmp/test" {
		t.Fatalf("expected datadir /tmp/test, got %s", cfg.DataDir)
	}
	if cfg.HTTPPort != 8545 {
		t.Fatalf("expected http port 8545, got %d", cfg.HTTPPort)
	}
	if cfg.AuthPort != 8551 {
		t.Fatalf("expected auth port 8551, got %d", cfg.AuthPort)
	}
	if cfg.P2P.MaxPeers != 25 {
		t.Fatalf("expected max peers 25, got %d", cfg.P2P.MaxPeers)
	}
	if cfg.P2P.ListenAddr != ":30303" {
		t.Fatalf("expected listen addr :30303, got %s", cfg.P2P.ListenAddr)
	}
	if len(cfg.AuthVirtualHosts) != 1 || cfg.AuthVirtualHosts[0] != "localhost" {
		t.Fatalf("expected auth vhosts [localhost], got %v", cfg.AuthVirtualHosts)
	}
}

func TestMapNodeConfigAuthVhostsWildcard(t *testing.T) {
	cfg := mapNodeConfig(nodeConfigParams{
		datadir: "/tmp/test", name: "test-node", network: "mainnet",
		p2pPort: 30303, httpAddr: "0.0.0.0", httpPort: 8545,
		authAddr: "0.0.0.0", authPort: 8551, authVhosts: "*",
		maxPeers: 25, jwtSecret: "/tmp/jwt",
		httpVhosts: "*", httpCORSDomain: "*", httpAPI: "eth",
		discoveryPort: 30303,
	})
	if len(cfg.AuthVirtualHosts) != 1 || cfg.AuthVirtualHosts[0] != "*" {
		t.Fatalf("expected auth vhosts [*], got %v", cfg.AuthVirtualHosts)
	}
}

func TestPrecompileInjectorForkLevels(t *testing.T) {
	glamTime := uint64(1700000000)
	hogTime := uint64(1800000000)
	iPlusTime := uint64(1900000000)
	pi := newPrecompileInjector(&glamTime, &hogTime, &iPlusTime)

	if level := pi.forkLevelAtTime(1600000000); level != geth.ForkLevelPrague {
		t.Fatalf("expected Prague, got %v", level)
	}
	if level := pi.forkLevelAtTime(1700000000); level != geth.ForkLevelGlamsterdam {
		t.Fatalf("expected Glamsterdam, got %v", level)
	}
	if level := pi.forkLevelAtTime(1800000000); level != geth.ForkLevelHogota {
		t.Fatalf("expected Hogota, got %v", level)
	}
	if level := pi.forkLevelAtTime(1900000000); level != geth.ForkLevelIPlus {
		t.Fatalf("expected IPlus, got %v", level)
	}
}

func TestPrecompileInjectorNoForks(t *testing.T) {
	pi := newPrecompileInjector(nil, nil, nil)
	addrs := pi.CustomAddresses(9999999999)
	if len(addrs) != 0 {
		t.Fatalf("expected no custom addresses, got %d", len(addrs))
	}
}

func TestPrecompileInjectorGlamsterdamAddresses(t *testing.T) {
	glamTime := uint64(1000)
	pi := newPrecompileInjector(&glamTime, nil, nil)

	// Before fork.
	addrs := pi.CustomAddresses(999)
	if len(addrs) != 0 {
		t.Fatalf("expected no addresses before fork, got %d", len(addrs))
	}

	// At fork activation.
	addrs = pi.CustomAddresses(1000)
	if len(addrs) != 4 {
		t.Fatalf("expected 4 Glamsterdam addresses, got %d", len(addrs))
	}
}

func TestVersionFlag(t *testing.T) {
	code := run([]string{"--version"})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
}

func writeTestGenesis(t *testing.T, dir string, chainID int64, gasLimit uint64) string {
	t.Helper()
	genesisFile := filepath.Join(dir, "genesis.json")
	// Write valid genesis JSON with all required fields.
	genesisJSON := fmt.Sprintf(`{
		"config": {"chainId": %d},
		"gasLimit": "0x%x",
		"difficulty": "0x1",
		"alloc": {}
	}`, chainID, gasLimit)
	if err := os.WriteFile(genesisFile, []byte(genesisJSON), 0644); err != nil {
		t.Fatal(err)
	}
	return genesisFile
}

func TestGenesisOverride(t *testing.T) {
	dir := t.TempDir()
	genesisFile := writeTestGenesis(t, dir, 32382, 30_000_000)

	cfg := mapEthConfig(ethConfigParams{
		network:     "mainnet",
		syncMode:    "full",
		genesisPath: genesisFile,
	})
	if cfg.Genesis == nil {
		t.Fatal("expected genesis to be non-nil")
	}
	if cfg.Genesis.GasLimit != 30_000_000 {
		t.Fatalf("expected gas limit 30000000, got %d", cfg.Genesis.GasLimit)
	}
	// Chain ID should be used as network ID.
	if cfg.NetworkId != 32382 {
		t.Fatalf("expected network id 32382, got %d", cfg.NetworkId)
	}
}

func TestGenesisOverrideWithNetworkID(t *testing.T) {
	dir := t.TempDir()
	genesisFile := writeTestGenesis(t, dir, 32382, 30_000_000)

	cfg := mapEthConfig(ethConfigParams{
		network:     "mainnet",
		syncMode:    "snap",
		genesisPath: genesisFile,
		networkID:   99999,
	})
	// Explicit network ID overrides genesis chain ID.
	if cfg.NetworkId != 99999 {
		t.Fatalf("expected network id 99999, got %d", cfg.NetworkId)
	}
}

func TestWSConfig(t *testing.T) {
	cfg := mapNodeConfig(nodeConfigParams{
		datadir: "/tmp/test", name: "test-node", network: "mainnet",
		p2pPort: 30303, httpAddr: "127.0.0.1", httpPort: 8545,
		authAddr: "127.0.0.1", authPort: 8551, authVhosts: "localhost",
		maxPeers: 25, jwtSecret: "/tmp/jwt",
		httpVhosts: "*", httpCORSDomain: "*", httpAPI: "eth,web3",
		wsEnabled: true, wsAddr: "0.0.0.0", wsPort: 8546,
		wsAPI: "eth,web3,debug", wsOrigins: "*",
		discoveryPort: 30303,
	})
	if cfg.WSHost != "0.0.0.0" {
		t.Fatalf("expected ws host 0.0.0.0, got %s", cfg.WSHost)
	}
	if cfg.WSPort != 8546 {
		t.Fatalf("expected ws port 8546, got %d", cfg.WSPort)
	}
	if len(cfg.WSModules) != 3 {
		t.Fatalf("expected 3 ws modules, got %d: %v", len(cfg.WSModules), cfg.WSModules)
	}
	if len(cfg.WSOrigins) != 1 || cfg.WSOrigins[0] != "*" {
		t.Fatalf("expected ws origins [*], got %v", cfg.WSOrigins)
	}
}

func TestWSDisabled(t *testing.T) {
	cfg := mapNodeConfig(nodeConfigParams{
		datadir: "/tmp/test", name: "test-node", network: "mainnet",
		p2pPort: 30303, httpAddr: "127.0.0.1", httpPort: 8545,
		authAddr: "127.0.0.1", authPort: 8551, authVhosts: "localhost",
		maxPeers: 25, jwtSecret: "/tmp/jwt",
		httpVhosts: "*", httpCORSDomain: "*", httpAPI: "eth",
		wsEnabled: false, wsAddr: "0.0.0.0", wsPort: 8546,
		discoveryPort: 30303,
	})
	if cfg.WSHost != "" {
		t.Fatalf("expected empty ws host when ws disabled, got %s", cfg.WSHost)
	}
}

func TestCustomBootnodes(t *testing.T) {
	// Use a valid mainnet bootnode enode URL.
	enodeURL := "enode://d860a01f9722d78051619d1e2351aba3f43f943f6f00718d1b9baa4101932a1f5011f16bb2b1bb35db20d6fe28fa0bf09636d26a87d31de9ec6203eeedb1f666@18.138.108.67:30303"

	cfg := mapNodeConfig(nodeConfigParams{
		datadir: "/tmp/test", name: "test-node", network: "mainnet",
		p2pPort: 30303, httpAddr: "127.0.0.1", httpPort: 8545,
		authAddr: "127.0.0.1", authPort: 8551, authVhosts: "localhost",
		maxPeers: 25, jwtSecret: "/tmp/jwt",
		httpVhosts: "*", httpCORSDomain: "*", httpAPI: "eth",
		bootnodes:     enodeURL,
		discoveryPort: 30303,
	})
	if len(cfg.P2P.BootstrapNodes) != 1 {
		t.Fatalf("expected 1 custom bootnode, got %d", len(cfg.P2P.BootstrapNodes))
	}
}

func TestCustomBootnodesEmpty(t *testing.T) {
	// Empty bootnodes should fall back to network defaults.
	cfg := mapNodeConfig(nodeConfigParams{
		datadir: "/tmp/test", name: "test-node", network: "mainnet",
		p2pPort: 30303, httpAddr: "127.0.0.1", httpPort: 8545,
		authAddr: "127.0.0.1", authPort: 8551, authVhosts: "localhost",
		maxPeers: 25, jwtSecret: "/tmp/jwt",
		httpVhosts: "*", httpCORSDomain: "*", httpAPI: "eth",
		discoveryPort: 30303,
	})
	if len(cfg.P2P.BootstrapNodes) == 0 {
		t.Fatal("expected default bootnodes when bootnodes flag is empty")
	}
}

func TestHTTPVhostsAndCORS(t *testing.T) {
	cfg := mapNodeConfig(nodeConfigParams{
		datadir: "/tmp/test", name: "test-node", network: "mainnet",
		p2pPort: 30303, httpAddr: "0.0.0.0", httpPort: 8545,
		authAddr: "0.0.0.0", authPort: 8551, authVhosts: "*",
		maxPeers: 25, jwtSecret: "/tmp/jwt",
		httpVhosts: "example.com,localhost", httpCORSDomain: "http://example.com,http://localhost",
		httpAPI:       "eth,web3,net",
		discoveryPort: 30303,
	})
	if len(cfg.HTTPVirtualHosts) != 2 {
		t.Fatalf("expected 2 vhosts, got %d: %v", len(cfg.HTTPVirtualHosts), cfg.HTTPVirtualHosts)
	}
	if len(cfg.HTTPCors) != 2 {
		t.Fatalf("expected 2 cors domains, got %d: %v", len(cfg.HTTPCors), cfg.HTTPCors)
	}
	if len(cfg.HTTPModules) != 3 {
		t.Fatalf("expected 3 http modules, got %d: %v", len(cfg.HTTPModules), cfg.HTTPModules)
	}
}

func TestGCModeArchive(t *testing.T) {
	cfg := mapEthConfig(ethConfigParams{
		network:  "mainnet",
		syncMode: "snap",
		gcMode:   "archive",
	})
	if !cfg.NoPruning {
		t.Fatal("expected NoPruning=true for gcmode=archive")
	}
	if cfg.SyncMode != ethconfig.FullSync {
		t.Fatalf("expected full sync for archive mode, got %v", cfg.SyncMode)
	}
}

func TestMinerConfig(t *testing.T) {
	cfg := mapEthConfig(ethConfigParams{
		network:       "mainnet",
		syncMode:      "full",
		minerGasPrice: 1,
		minerGasLimit: 60_000_000,
	})
	if cfg.Miner.GasPrice.Cmp(big.NewInt(1)) != 0 {
		t.Fatalf("expected gas price 1, got %s", cfg.Miner.GasPrice)
	}
	if cfg.Miner.GasCeil != 60_000_000 {
		t.Fatalf("expected gas ceil 60000000, got %d", cfg.Miner.GasCeil)
	}
}

func TestDiscoveryPort(t *testing.T) {
	cfg := mapNodeConfig(nodeConfigParams{
		datadir: "/tmp/test", name: "test-node", network: "mainnet",
		p2pPort: 30303, httpAddr: "127.0.0.1", httpPort: 8545,
		authAddr: "127.0.0.1", authPort: 8551, authVhosts: "localhost",
		maxPeers: 25, jwtSecret: "/tmp/jwt",
		httpVhosts: "*", httpCORSDomain: "*", httpAPI: "eth",
		discoveryPort: 30304,
	})
	if cfg.P2P.DiscAddr != ":30304" {
		t.Fatalf("expected disc addr :30304, got %s", cfg.P2P.DiscAddr)
	}
}

func TestDiscoveryPortSameAsP2P(t *testing.T) {
	cfg := mapNodeConfig(nodeConfigParams{
		datadir: "/tmp/test", name: "test-node", network: "mainnet",
		p2pPort: 30303, httpAddr: "127.0.0.1", httpPort: 8545,
		authAddr: "127.0.0.1", authPort: 8551, authVhosts: "localhost",
		maxPeers: 25, jwtSecret: "/tmp/jwt",
		httpVhosts: "*", httpCORSDomain: "*", httpAPI: "eth",
		discoveryPort: 30303,
	})
	// When discovery port equals p2p port, no separate DiscAddr needed.
	if cfg.P2P.DiscAddr != "" {
		t.Fatalf("expected empty disc addr when same as p2p port, got %s", cfg.P2P.DiscAddr)
	}
}

func TestNATConfig(t *testing.T) {
	cfg := mapNodeConfig(nodeConfigParams{
		datadir: "/tmp/test", name: "test-node", network: "mainnet",
		p2pPort: 30303, httpAddr: "127.0.0.1", httpPort: 8545,
		authAddr: "127.0.0.1", authPort: 8551, authVhosts: "localhost",
		maxPeers: 25, jwtSecret: "/tmp/jwt",
		httpVhosts: "*", httpCORSDomain: "*", httpAPI: "eth",
		natAddr:       "extip:1.2.3.4",
		discoveryPort: 30303,
	})
	if cfg.P2P.NAT == nil {
		t.Fatal("expected NAT to be set for extip:1.2.3.4")
	}
}

func TestAllowUnprotectedTxs(t *testing.T) {
	cfg := mapNodeConfig(nodeConfigParams{
		datadir: "/tmp/test", name: "test-node", network: "mainnet",
		p2pPort: 30303, httpAddr: "127.0.0.1", httpPort: 8545,
		authAddr: "127.0.0.1", authPort: 8551, authVhosts: "localhost",
		maxPeers: 25, jwtSecret: "/tmp/jwt",
		httpVhosts: "*", httpCORSDomain: "*", httpAPI: "eth",
		allowUnprotectedTxs: true,
		discoveryPort:       30303,
	})
	if !cfg.AllowUnprotectedTxs {
		t.Fatal("expected AllowUnprotectedTxs=true")
	}
}

func TestNetworkIDOverride(t *testing.T) {
	cfg := mapEthConfig(ethConfigParams{
		network:   "mainnet",
		syncMode:  "snap",
		networkID: 42,
	})
	if cfg.NetworkId != 42 {
		t.Fatalf("expected network id 42, got %d", cfg.NetworkId)
	}
}

func TestLoadGenesisInvalid(t *testing.T) {
	_, err := loadGenesis("/nonexistent/path/genesis.json")
	if err == nil {
		t.Fatal("expected error for nonexistent genesis file")
	}
}

func TestLoadGenesisMalformed(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.json")
	os.WriteFile(f, []byte("not json"), 0644)
	_, err := loadGenesis(f)
	if err == nil {
		t.Fatal("expected error for malformed genesis json")
	}
}
