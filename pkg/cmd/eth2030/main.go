// Command eth2030 is the main entry point for the eth2030 Ethereum client.
//
// Usage:
//
//	eth2030 [flags]
//
// Flags match eth2030-geth for Kurtosis devnet compatibility.
package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	coretypes "github.com/eth2030/eth2030/core/types"
	ethlog "github.com/eth2030/eth2030/log"
	"github.com/eth2030/eth2030/node"
)

// Build-time version info, overridable with ldflags.
var (
	version = "v0.1.0-dev"
	commit  = "unknown"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run is the actual entry point, returning an exit code.
func run(args []string) int {
	cfg, exit, code := parseFlags(args)
	if exit {
		return code
	}

	cfg.LogLevel = node.VerbosityToLogLevel(cfg.Verbosity)
	setupLogging(cfg.Verbosity)

	slog.Info("eth2030 starting", "version", version,
		"network", cfg.Network,
		"datadir", cfg.DataDir,
		"syncmode", cfg.SyncMode,
	)
	slog.Info("HTTP-RPC",
		"addr", cfg.HTTPListenAddr(),
		"vhosts", cfg.HTTPVhosts,
		"modules", cfg.HTTPModules,
	)
	slog.Info("Engine API",
		"addr", cfg.AuthListenAddr(),
		"vhosts", cfg.AuthVhosts,
		"jwt", cfg.JWTSecretPath(),
	)
	slog.Info("P2P",
		"port", cfg.P2PPort,
		"maxpeers", cfg.MaxPeers,
		"bootnodes", cfg.Bootnodes,
	)
	if cfg.Metrics {
		slog.Info("Metrics", "addr", cfg.MetricsListenAddr())
	}
	if cfg.WSEnabled {
		slog.Info("WebSocket RPC", "addr", cfg.WSListenAddr())
	}

	// Log security configuration.
	slog.Info("Security",
		"rpc_auth", cfg.RPCAuthSecret != "",
		"rpc_rate", cfg.RPCRateLimitPerSec,
		"rpc_body", cfg.RPCMaxRequestSize,
		"rpc_batch", cfg.RPCMaxBatchSize,
		"engine_body", cfg.EngineMaxRequestSize,
		"engine_auth", cfg.EngineAuthToken != "" || cfg.EngineAuthTokenPath != "",
	)

	// Log any fork override timestamps so they appear in the startup banner.
	logForkOverrides(&cfg)

	if err := cfg.Validate(); err != nil {
		slog.Error("invalid configuration", "err", err)
		return 1
	}

	if err := cfg.InitDataDir(); err != nil {
		slog.Error("failed to initialize datadir", "err", err)
		return 1
	}

	n, err := node.New(&cfg)
	if err != nil {
		slog.Error("failed to create node", "err", err)
		return 1
	}

	if err := n.Start(); err != nil {
		slog.Error("failed to start node", "err", err)
		return 1
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	slog.Info("received signal, shutting down", "signal", sig)

	if err := n.Stop(); err != nil {
		slog.Error("error during shutdown", "err", err)
		return 1
	}

	slog.Info("shutdown complete")
	return 0
}

// parseFlags parses CLI arguments into a node.Config.
// Returns (config, shouldExit, exitCode).
func parseFlags(args []string) (node.Config, bool, int) {
	cfg := node.DefaultConfig()
	fs := newFlagSet(&cfg)

	showVersion := fs.Bool("version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return cfg, true, 2
	}

	if *showVersion {
		fmt.Printf("eth2030 %s (commit %s)\n", version, commit)
		return cfg, true, 0
	}

	// Handle --override.*=0 special case: flag.Uint64 treats 0 as "not set"
	// because it is the zero value. We scan raw args so an explicit =0 activates
	// the fork at genesis.
	applyZeroForkOverrides(args, &cfg)

	return cfg, false, 0
}

// newFlagSet creates a flagSet binding all CLI flags to cfg.
func newFlagSet(cfg *node.Config) *flagSet {
	fs := newCustomFlagSet("eth2030")

	// --- Node ---
	fs.StringVar(&cfg.DataDir, "datadir", cfg.DataDir, "data directory path")
	fs.StringVar(&cfg.Network, "network", cfg.Network, "network to join (mainnet, sepolia, holesky)")
	fs.IntVar(&cfg.P2PPort, "port", cfg.P2PPort, "P2P listening port")
	fs.IntVar(&cfg.MaxPeers, "maxpeers", cfg.MaxPeers, "maximum number of P2P peers")
	fs.StringVar(&cfg.SyncMode, "syncmode", cfg.SyncMode, "sync mode (full, snap)")
	fs.StringVar(&cfg.NativeConsensusFile, "native-consensus", cfg.NativeConsensusFile, "research 3SF/PQ observer configuration file (CL retains forkchoice authority)")
	fs.StringVar(&cfg.GCMode, "gcmode", cfg.GCMode, "GC mode: archive (no pruning) or full")
	fs.Uint64Var(&cfg.NetworkID, "networkid", cfg.NetworkID, "network ID override (0 = use genesis chain ID)")
	fs.IntVar(&cfg.Verbosity, "verbosity", cfg.Verbosity, "log level 0-5 (0=silent, 5=trace)")

	// --- HTTP-RPC ---
	fs.Bool("http", false, "enable HTTP-RPC server (always on; kept for compatibility)")
	fs.StringVar(&cfg.HTTPAddr, "http.addr", cfg.HTTPAddr, "HTTP-RPC listen address")
	fs.IntVar(&cfg.RPCPort, "http.port", cfg.RPCPort, "HTTP-RPC port")
	fs.StringSliceVar(&cfg.HTTPVhosts, "http.vhosts", cfg.HTTPVhosts, "comma-separated virtual hostnames for HTTP-RPC")
	fs.StringSliceVar(&cfg.HTTPCORSDomain, "http.corsdomain", cfg.HTTPCORSDomain, "comma-separated CORS domains")
	fs.StringSliceVar(&cfg.HTTPModules, "http.api", cfg.HTTPModules, "comma-separated HTTP-RPC API modules")
	fs.StringVar(&cfg.RPCAuthSecret, "rpc.auth_secret", cfg.RPCAuthSecret, "Bearer token required by HTTP-RPC (empty = disabled)")
	fs.IntVar(&cfg.RPCRateLimitPerSec, "rpc.rate_limit", cfg.RPCRateLimitPerSec, "HTTP-RPC request rate limit (req/s, 0 = unlimited)")
	fs.Int64Var(&cfg.RPCMaxRequestSize, "rpc.max_request_size", cfg.RPCMaxRequestSize, "HTTP-RPC max request body size (bytes)")
	fs.IntVar(&cfg.RPCMaxBatchSize, "rpc.max_batch_size", cfg.RPCMaxBatchSize, "HTTP-RPC max batch size")
	fs.StringVar(&cfg.RPCCORSOrigins, "rpc.cors_origin", cfg.RPCCORSOrigins, "HTTP-RPC allowed CORS origins (comma-separated, * = all)")

	// --- Auth / Engine API ---
	fs.StringVar(&cfg.AuthAddr, "authrpc.addr", cfg.AuthAddr, "Engine API listen address")
	fs.IntVar(&cfg.EnginePort, "authrpc.port", cfg.EnginePort, "Engine API port")
	fs.StringSliceVar(&cfg.AuthVhosts, "authrpc.vhosts", cfg.AuthVhosts, "comma-separated virtual hostnames for Engine API")
	fs.StringVar(&cfg.JWTSecret, "authrpc.jwtsecret", cfg.JWTSecret, "path to JWT secret for Engine API auth")
	fs.StringVar(&cfg.AuthAddr, "engine.addr", cfg.AuthAddr, "Engine API bind address (alias for --authrpc.addr)")
	fs.Int64Var(&cfg.EngineMaxRequestSize, "engine.max_request_size", cfg.EngineMaxRequestSize, "Engine API max request body size (bytes)")
	fs.StringVar(&cfg.EngineAuthToken, "engine.auth_secret", cfg.EngineAuthToken, "Engine API bearer token (empty = disabled)")
	fs.StringVar(&cfg.EngineAuthTokenPath, "engine.auth_secret_file", cfg.EngineAuthTokenPath, "Engine API bearer token file path")

	// --- WebSocket ---
	fs.BoolVar(&cfg.WSEnabled, "ws", cfg.WSEnabled, "enable WebSocket RPC server")
	fs.StringVar(&cfg.WSAddr, "ws.addr", cfg.WSAddr, "WebSocket RPC listen address")
	fs.IntVar(&cfg.WSPort, "ws.port", cfg.WSPort, "WebSocket RPC port")
	fs.StringSliceVar(&cfg.WSModules, "ws.api", cfg.WSModules, "comma-separated WebSocket API modules")
	fs.StringSliceVar(&cfg.WSOrigins, "ws.origins", cfg.WSOrigins, "comma-separated allowed WebSocket origins")

	// --- P2P ---
	fs.StringVar(&cfg.Bootnodes, "bootnodes", cfg.Bootnodes, "comma-separated enode URLs for bootstrap")
	fs.IntVar(&cfg.DiscoveryPort, "discovery.port", cfg.DiscoveryPort, "UDP port for node discovery")
	fs.StringVar(&cfg.NAT, "nat", cfg.NAT, "NAT traversal method (e.g. extip:1.2.3.4)")

	// --- Metrics ---
	fs.BoolVar(&cfg.Metrics, "metrics", cfg.Metrics, "enable metrics collection")
	fs.StringVar(&cfg.MetricsAddr, "metrics.addr", cfg.MetricsAddr, "metrics HTTP server address")
	fs.IntVar(&cfg.MetricsPort, "metrics.port", cfg.MetricsPort, "metrics HTTP server port")

	// --- Genesis / fork overrides ---
	fs.StringVar(&cfg.GenesisPath, "override.genesis", cfg.GenesisPath, "path to custom genesis.json (for Kurtosis devnets)")
	fs.Uint64PtrVar(&cfg.GlamsterdamOverride, "override.glamsterdam", "override Glamsterdam fork timestamp")
	fs.Uint64PtrVar(&cfg.HogotaOverride, "override.hogota", "override Hogota fork timestamp")
	fs.Uint64PtrVar(&cfg.IPlusOverride, "override.iplus", "override I+ fork timestamp (enables NTT/NII precompiles)")

	// --- Misc ---
	fs.BoolVar(&cfg.AllowUnprotectedTxs, "rpc.allow-unprotected-txs", cfg.AllowUnprotectedTxs, "allow non-EIP155 transactions over RPC")
	fs.Uint64Var(&cfg.MinerGasPrice, "miner.gasprice", cfg.MinerGasPrice, "minimum gas price for mining (wei)")
	fs.Uint64Var(&cfg.MinerGasLimit, "miner.gaslimit", cfg.MinerGasLimit, "target gas ceiling for mined blocks")

	// --- EIP-8141 Frame Mempool (AA-2.3) ---
	fs.StringVar(&cfg.FrameMempoolTier, "frame-mempool", cfg.FrameMempoolTier, "frame tx mempool ruleset: conservative (50K VERIFY gas cap) or aggressive (200K with staked paymaster)")

	// --- EP-3 Post-Quantum ---
	fs.BoolVar(&cfg.LeanAvailableChainMode, "lean-available-chain", cfg.LeanAvailableChainMode, "enable lean available chain mode (PQ attestation for a validator subset)")
	fs.IntVar(&cfg.LeanAvailableChainValidators, "lean-available-validators", cfg.LeanAvailableChainValidators, "per-slot PQ attestor count [256-1024]")
	fs.BoolVar(&cfg.StarkValidationFrames, "stark-validation-frames", cfg.StarkValidationFrames, "enable STARK proof sealing for VERIFY frame transactions")

	// --- EP-4 Lean Consensus ---
	fs.StringVar(&cfg.SlotDuration, "slot-duration", cfg.SlotDuration, "slot duration: 4s or 6s (default 6s, LEAN-1.1)")

	// --- EP-5 Roadmap Gaps ---
	fs.IntVar(&cfg.AttesterSampleSize, "attester-sample-size", cfg.AttesterSampleSize, "per-slot attester sample: 0=full committee, 256, 512, or 1024 (GAP-3.4)")
	fs.StringVar(&cfg.FinalityMode, "finality-mode", cfg.FinalityMode, "finality engine: ssf (default) or minimmit (GAP-5.2)")
	fs.StringVar(&cfg.BLSBackend, "bls-backend", cfg.BLSBackend, "BLS backend: blst (default) or pure-go (GAP-7.2)")

	// --- EP-6 Block Building Pipeline ---
	fs.StringVar(&cfg.MixnetMode, "mixnet", cfg.MixnetMode, "anonymous tx transport: simulated (default) | tor | nym (BB-1.1)")
	fs.BoolVar(&cfg.ExperimentalLocalTx, "experimental-local-tx", cfg.ExperimentalLocalTx, "enable type-0x08 local tx (proof-of-concept; enforces ScopeHint BAL check, BB-2.2)")
	fs.BoolVar(&cfg.AllowAATx, "txpool.allow-aa", cfg.AllowAATx, "accept EIP-7701 AA transactions (type 0x05) in the txpool; default true on Glamsterdam+ networks")
	fs.IntVar(&cfg.TxpoolPriceHistory, "txpool.price-history", cfg.TxpoolPriceHistory, "number of recent blocks the txpool gas-price suggestor uses for fee estimates (default 20)")

	// --- Trie Migration ---
	fs.IntVar(&cfg.MigrateEveryBlocks, "trie.migrate-every", cfg.MigrateEveryBlocks, "advance MPT→BinaryTrie migration one batch every N blocks (0 = disabled, I+ only)")

	// --- Snapshot ---
	fs.IntVar(&cfg.SnapshotCapDepth, "snapshot.cap-depth", cfg.SnapshotCapDepth, "max in-memory snapshot diff layers before flushing to disk (0 = no flush, default 128)")

	// --- Memory cache sizes ---
	fs.IntVar(&cfg.CacheBlockSize, "cache.block", cfg.CacheBlockSize, "max blocks in the in-memory block cache (default 256)")
	fs.IntVar(&cfg.CacheReceiptSize, "cache.receipts", cfg.CacheReceiptSize, "max receipt sets in the in-memory receipt cache (default 128)")
	fs.IntVar(&cfg.CacheStateSize, "cache.state-snapshots", cfg.CacheStateSize, "max MemoryStateDB snapshots for fast reorg/payload-building (default 4)")
	fs.BoolVar(&coretypes.EIP7706HashFields, "eip7706.hash-fields", coretypes.EIP7706HashFields, "include EIP-7706 CalldataGasUsed/CalldataExcessGas in the canonical block hash RLP; disable for compatibility with Lighthouse/go-ethereum (default false)")
	fs.IntVar(&cfg.CacheEnginePayloads, "cache.engine-payloads", cfg.CacheEnginePayloads, "max pending built payloads retained in the engine backend (default 32)")
	fs.IntVar(&cfg.CacheEngineILs, "cache.engine-ils", cfg.CacheEngineILs, "max inclusion lists stored by the engine backend (default 256)")
	fs.IntVar(&cfg.CacheFCPruneBuffer, "cache.fc-prune-buffer", cfg.CacheFCPruneBuffer, "blocks behind finalized head to keep in forkchoice state manager (default 128)")
	fs.IntVar(&cfg.CacheFCHistory, "cache.fc-history", cfg.CacheFCHistory, "FCU debug history ring-buffer size in forkchoice tracker (default 256)")
	fs.IntVar(&cfg.CacheFCReorgHistory, "cache.fc-reorg-history", cfg.CacheFCReorgHistory, "max reorg events retained by forkchoice tracker (default 128)")
	fs.IntVar(&cfg.CacheAllocatedIDs, "cache.alloc-ids", cfg.CacheAllocatedIDs, "cap on payload ID allocator in-memory map (default 512)")

	return fs
}

// applyZeroForkOverrides scans raw args for --override.*=0 patterns so that
// an explicit zero value activates the fork at genesis (0 is otherwise
// indistinguishable from "flag not set" for Uint64Ptr flags).
func applyZeroForkOverrides(args []string, cfg *node.Config) {
	zero := uint64(0)
	for _, arg := range args {
		switch arg {
		case "--override.glamsterdam=0", "-override.glamsterdam=0":
			cfg.GlamsterdamOverride = &zero
		case "--override.hogota=0", "-override.hogota=0":
			cfg.HogotaOverride = &zero
		case "--override.iplus=0", "-override.iplus=0":
			cfg.IPlusOverride = &zero
		}
	}
}

// defaultDataDir returns the default data directory path.
func defaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".eth2030"
	}
	return filepath.Join(home, ".eth2030")
}

// setupLogging configures both the slog default and the pkg/log default based
// on verbosity so that all subsystem loggers (blockchainLog, txpoolLog, …)
// respect the -verbosity flag.
func setupLogging(verbosity int) {
	var level slog.Level
	switch {
	case verbosity <= 1:
		level = slog.LevelError
	case verbosity == 2:
		level = slog.LevelWarn
	case verbosity == 3:
		level = slog.LevelInfo
	default:
		level = slog.LevelDebug
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))
	// Also update the pkg/log default so module-scoped loggers (blockchainLog,
	// txpoolLog, rpcLog, …) inherit the chosen level.
	ethlog.SetLevel(level)
}
