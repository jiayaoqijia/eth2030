// Package eiptest provides an in-process integration test framework for
// ETH2030. It wires the real production modules (blockchain, block builder,
// state processor, txpool, engine backend, RPC server) behind a small probe
// API so that per-EIP integration tests contain only feature-specific
// assertions. See docs/plans/eiptest-framework-plan.md.
package eiptest

import (
	"fmt"
	"math"
	"math/big"

	"github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/txpool"
)

// Fork names understood by the framework. The Shanghai/Cancun/Prague forks are
// always active from genesis (they are historical by now); the scheduled forks
// below map to the corresponding *Time fields on config.ChainConfig.
type Fork string

const (
	ForkOsaka       Fork = "Osaka"
	ForkAmsterdam   Fork = "Amsterdam"
	ForkGlamsterdan Fork = "Glamsterdan"
	ForkHogota      Fork = "Hogota"
	ForkIPlus       Fork = "IPlus"
	ForkBPO1        Fork = "BPO1"
	ForkBPO2        Fork = "BPO2"
)

// Never deactivates a fork (activation time set to nil in the chain config).
const Never uint64 = ^uint64(0)

// newUint64p returns a pointer to v (local helper; config keeps this private).
func newUint64p(v uint64) *uint64 { return &v }

// forkTimeField returns a mutable pointer to the scheduled-fork timestamp
// field of cfg for the given fork name.
func forkTimeField(cfg *config.ChainConfig, fork Fork) (**uint64, error) {
	switch fork {
	case ForkOsaka:
		return &cfg.OsakaTime, nil
	case ForkAmsterdam:
		return &cfg.AmsterdamTime, nil
	case ForkGlamsterdan:
		return &cfg.GlamsterdanTime, nil
	case ForkHogota:
		return &cfg.HogotaTime, nil
	case ForkIPlus:
		return &cfg.IPlusTime, nil
	case ForkBPO1:
		return &cfg.BPO1Time, nil
	case ForkBPO2:
		return &cfg.BPO2Time, nil
	default:
		return nil, fmt.Errorf("eiptest: unknown fork %q", fork)
	}
}

// Defaults for a simulation chain.
const (
	DefaultChainID     uint64 = 1337
	DefaultGasLimit    uint64 = 30_000_000
	DefaultBaseFeeGwei uint64 = 1
)

// SimOption configures a Sim at construction time.
type SimOption func(*simOptions) error

type simOptions struct {
	chainID     uint64
	genesisTime uint64
	gasLimit    uint64
	baseFee     *big.Int
	forkTimes   map[Fork]uint64 // Never = deactivated
	alloc       config.GenesisAlloc
	poolConfig  *txpool.Config

	requireRequestPredeploys bool
}

func defaultSimOptions() *simOptions {
	return &simOptions{
		chainID:   DefaultChainID,
		gasLimit:  DefaultGasLimit,
		forkTimes: map[Fork]uint64{},
		alloc:     config.GenesisAlloc{},
	}
}

// ForkAt schedules the given fork to activate at the given genesis-relative
// timestamp. The activation time is absolute: genesis.Timestamp + offset.
func ForkAt(fork Fork, offset uint64) SimOption {
	return func(o *simOptions) error {
		o.forkTimes[fork] = offset
		return nil
	}
}

// Fund prefunds an account in the genesis alloc.
func Fund(addr types.Address, wei *big.Int) SimOption {
	return func(o *simOptions) error {
		o.alloc[addr] = config.GenesisAccount{Balance: wei}
		return nil
	}
}

// FundCode prefunds an account with balance, code and nonce in genesis.
func FundCode(addr types.Address, wei *big.Int, code []byte, nonce uint64) SimOption {
	return func(o *simOptions) error {
		o.alloc[addr] = config.GenesisAccount{Balance: wei, Code: code, Nonce: nonce}
		return nil
	}
}

// WithRequestContracts deploys the Prague request contracts in the genesis state.
func WithRequestContracts() SimOption {
	return func(o *simOptions) error {
		for address, account := range config.ExecutionRequestContractAlloc() {
			o.alloc[address] = account
		}
		o.requireRequestPredeploys = true
		return nil
	}
}

// WithGasLimit overrides the genesis gas limit.
func WithGasLimit(limit uint64) SimOption {
	return func(o *simOptions) error {
		o.gasLimit = limit
		return nil
	}
}

// WithBaseFee overrides the genesis base fee (wei per gas).
func WithBaseFee(wei *big.Int) SimOption {
	return func(o *simOptions) error {
		o.baseFee = wei
		return nil
	}
}

// WithChainID overrides the chain ID used for signing.
func WithChainID(id uint64) SimOption {
	return func(o *simOptions) error {
		o.chainID = id
		return nil
	}
}

// WithGenesisTime sets the genesis timestamp. ForkAt offsets stay relative to it.
func WithGenesisTime(timestamp uint64) SimOption {
	return func(o *simOptions) error {
		o.genesisTime = timestamp
		return nil
	}
}

// WithPoolConfig configures limits and admission policy for the simulation pool.
func WithPoolConfig(cfg txpool.Config) SimOption {
	return func(o *simOptions) error {
		o.poolConfig = &cfg
		return nil
	}
}

// buildChainConfig assembles the chain config from the options. Shanghai,
// Cancun and Prague are active from genesis; scheduled forks follow the
// ForkAt options (unset forks stay inactive).
func (o *simOptions) buildChainConfig() (*config.ChainConfig, error) {
	cfg := &config.ChainConfig{
		RequireRequestPredeploys: o.requireRequestPredeploys,

		ChainID:                 new(big.Int).SetUint64(o.chainID),
		HomesteadBlock:          big.NewInt(0),
		EIP150Block:             big.NewInt(0),
		EIP155Block:             big.NewInt(0),
		EIP158Block:             big.NewInt(0),
		ByzantiumBlock:          big.NewInt(0),
		ConstantinopleBlock:     big.NewInt(0),
		PetersburgBlock:         big.NewInt(0),
		IstanbulBlock:           big.NewInt(0),
		MuirGlacierBlock:        big.NewInt(0),
		BerlinBlock:             big.NewInt(0),
		LondonBlock:             big.NewInt(0),
		TerminalTotalDifficulty: big.NewInt(0),
		ShanghaiTime:            newUint64p(0),
		CancunTime:              newUint64p(0),
		PragueTime:              newUint64p(0),
	}
	for fork, offset := range o.forkTimes {
		field, err := forkTimeField(cfg, fork)
		if err != nil {
			return nil, err
		}
		if offset == Never {
			*field = nil
		} else {
			if offset > math.MaxUint64-o.genesisTime {
				return nil, fmt.Errorf("%s fork timestamp overflows uint64", fork)
			}
			*field = newUint64p(o.genesisTime + offset)
		}
	}
	return cfg, nil
}
