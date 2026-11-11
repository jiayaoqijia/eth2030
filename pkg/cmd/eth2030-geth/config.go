// Package main implements the eth2030-geth binary, which embeds go-ethereum
// as a library for full Ethereum node operation with ETH2030 custom precompiles.
package main

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strings"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/metrics"
	gethnode "github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/nat"
	"github.com/ethereum/go-ethereum/params"
)

// eth2030GethConfig holds the go-ethereum node and eth service configuration.
type eth2030GethConfig struct {
	Node gethnode.Config
	Eth  ethconfig.Config
}

// nodeConfigParams groups all CLI parameters for node configuration.
type nodeConfigParams struct {
	datadir, name, network              string
	p2pPort                             int
	httpAddr                            string
	httpPort                            int
	authAddr                            string
	authPort                            int
	authVhosts                          string
	maxPeers                            int
	jwtSecret                           string
	httpVhosts, httpCORSDomain, httpAPI string
	wsEnabled                           bool
	wsAddr                              string
	wsPort                              int
	wsAPI, wsOrigins                    string
	discoveryPort                       int
	bootnodes, natAddr                  string
	metricsEnabled                      bool
	metricsAddr                         string
	metricsPort                         int
	allowUnprotectedTxs                 bool
}

// mapNodeConfig creates a go-ethereum node.Config from CLI parameters.
func mapNodeConfig(p nodeConfigParams) gethnode.Config {
	// Determine bootnodes: use custom if provided, else network defaults.
	var bootstrapNodes, bootstrapNodesV5 []*enode.Node
	if p.bootnodes != "" {
		bootstrapNodes = parseCustomBootnodes(p.bootnodes)
		bootstrapNodesV5 = bootstrapNodes
	} else {
		bootstrapNodes = parseBootnodes(p.network)
		bootstrapNodesV5 = bootstrapNodes
	}

	// Parse NAT configuration.
	var natInterface nat.Interface
	if p.natAddr != "" {
		var err error
		natInterface, err = nat.Parse(p.natAddr)
		if err != nil {
			natInterface = nil // Fall back to no NAT on parse error.
		}
	}

	// Discovery address.
	var discAddr string
	if p.discoveryPort != p.p2pPort {
		discAddr = fmt.Sprintf(":%d", p.discoveryPort)
	}

	// Enable metrics if requested.
	if p.metricsEnabled {
		metrics.Enable()
	}

	cfg := gethnode.Config{
		Name:                p.name,
		Version:             version,
		DataDir:             p.datadir,
		HTTPHost:            p.httpAddr,
		HTTPPort:            p.httpPort,
		HTTPModules:         splitAndTrim(p.httpAPI),
		HTTPVirtualHosts:    splitAndTrim(p.httpVhosts),
		HTTPCors:            splitAndTrim(p.httpCORSDomain),
		AuthAddr:            p.authAddr,
		AuthPort:            p.authPort,
		AuthVirtualHosts:    splitAndTrim(p.authVhosts),
		JWTSecret:           p.jwtSecret,
		AllowUnprotectedTxs: p.allowUnprotectedTxs,
		P2P: p2p.Config{
			ListenAddr:       fmt.Sprintf(":%d", p.p2pPort),
			DiscAddr:         discAddr,
			MaxPeers:         p.maxPeers,
			MaxPendingPeers:  50,
			NAT:              natInterface,
			BootstrapNodes:   bootstrapNodes,
			BootstrapNodesV5: bootstrapNodesV5,
			DiscoveryV5:      true,
		},
	}

	// Wire WebSocket if enabled.
	if p.wsEnabled {
		cfg.WSHost = p.wsAddr
		cfg.WSPort = p.wsPort
		cfg.WSModules = splitAndTrim(p.wsAPI)
		cfg.WSOrigins = splitAndTrim(p.wsOrigins)
	}

	return cfg
}

// parseBootnodes returns the go-ethereum bootstrap nodes for the given network.
func parseBootnodes(network string) []*enode.Node {
	var urls []string
	switch network {
	case "sepolia":
		urls = params.SepoliaBootnodes
	case "hoodi":
		urls = params.HoodiBootnodes
	case "holesky":
		// Holesky was removed upstream in go-ethereum v1.17.x (superseded by
		// Hoodi); fall back to Hoodi discovery nodes.
		urls = params.HoodiBootnodes
	default:
		urls = params.MainnetBootnodes
	}
	nodes := make([]*enode.Node, 0, len(urls))
	for _, url := range urls {
		n, err := enode.Parse(enode.ValidSchemes, url)
		if err == nil {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// parseCustomBootnodes parses a comma-separated list of enode URLs.
func parseCustomBootnodes(s string) []*enode.Node {
	urls := splitAndTrim(s)
	nodes := make([]*enode.Node, 0, len(urls))
	for _, url := range urls {
		n, err := enode.Parse(enode.ValidSchemes, url)
		if err == nil {
			nodes = append(nodes, n)
		}
	}
	return nodes
}

// splitAndTrim splits a comma-separated string and trims whitespace from each element.
func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			result = append(result, v)
		}
	}
	return result
}

// ethConfigParams groups all CLI parameters for eth service configuration.
type ethConfigParams struct {
	network, syncMode, genesisPath string
	networkID                      uint64
	gcMode                         string
	minerGasPrice, minerGasLimit   uint64
}

// mapEthConfig creates a go-ethereum ethconfig.Config for the selected network.
func mapEthConfig(p ethConfigParams) ethconfig.Config {
	cfg := ethconfig.Defaults

	switch p.syncMode {
	case "full":
		cfg.SyncMode = ethconfig.FullSync
	default:
		cfg.SyncMode = ethconfig.SnapSync
	}

	// Load custom genesis if provided (Kurtosis devnets).
	if p.genesisPath != "" {
		genesis, err := loadGenesis(p.genesisPath)
		if err == nil {
			cfg.Genesis = genesis
			if p.networkID > 0 {
				cfg.NetworkId = p.networkID
			} else if genesis.Config != nil && genesis.Config.ChainID != nil {
				cfg.NetworkId = genesis.Config.ChainID.Uint64()
			}
		}
	} else {
		switch p.network {
		case "sepolia":
			cfg.Genesis = core.DefaultSepoliaGenesisBlock()
			cfg.NetworkId = 11155111
		case "hoodi":
			cfg.Genesis = core.DefaultHoodiGenesisBlock()
			cfg.NetworkId = 560048
		case "holesky":
			// Holesky's built-in genesis was removed upstream in go-ethereum
			// v1.17.x (superseded by Hoodi). Map to Hoodi so existing
			// --network holesky invocations keep booting against a live
			// testnet instead of failing.
			cfg.Genesis = core.DefaultHoodiGenesisBlock()
			cfg.NetworkId = 560048
		default:
			cfg.Genesis = core.DefaultGenesisBlock()
			cfg.NetworkId = 1
		}
	}

	// Override network ID if explicitly set.
	if p.networkID > 0 && p.genesisPath == "" {
		cfg.NetworkId = p.networkID
	}

	// GC mode: archive disables pruning.
	if p.gcMode == "archive" {
		cfg.NoPruning = true
		cfg.SyncMode = ethconfig.FullSync
	}

	// Miner settings.
	if p.minerGasPrice > 0 {
		cfg.Miner.GasPrice = new(big.Int).SetUint64(p.minerGasPrice)
	}
	if p.minerGasLimit > 0 {
		cfg.Miner.GasCeil = p.minerGasLimit
	}

	return cfg
}

// loadGenesis reads and decodes a genesis.json file.
func loadGenesis(path string) (*core.Genesis, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading genesis file: %w", err)
	}
	var genesis core.Genesis
	if err := json.Unmarshal(data, &genesis); err != nil {
		return nil, fmt.Errorf("decoding genesis file: %w", err)
	}
	return &genesis, nil
}
