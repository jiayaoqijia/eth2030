package geth

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/params"

	"github.com/eth2030/eth2030/core/config"
)

// ToGethChainConfig converts an ETH2030 ChainConfig to a go-ethereum ChainConfig.
// ETH2030-specific forks (Glamsterdan, Hogota, etc.) are ignored since
// go-ethereum doesn't know about them.
func ToGethChainConfig(c *config.ChainConfig) *params.ChainConfig {
	if c == nil {
		return nil
	}
	gc := &params.ChainConfig{
		ChainID: c.ChainID,

		// Block-number forks (pre-merge).
		HomesteadBlock:      c.HomesteadBlock,
		EIP150Block:         c.EIP150Block,
		EIP155Block:         c.EIP155Block,
		EIP158Block:         c.EIP158Block,
		ByzantiumBlock:      c.ByzantiumBlock,
		ConstantinopleBlock: c.ConstantinopleBlock,
		PetersburgBlock:     c.PetersburgBlock,
		IstanbulBlock:       c.IstanbulBlock,
		BerlinBlock:         c.BerlinBlock,
		LondonBlock:         c.LondonBlock,

		// Merge.
		TerminalTotalDifficulty: c.TerminalTotalDifficulty,

		// Timestamp forks (post-merge).
		ShanghaiTime: c.ShanghaiTime,
		CancunTime:   c.CancunTime,
		PragueTime:   c.PragueTime,
		OsakaTime:    c.OsakaTime,
	}
	return gc
}

// efForkLevel maps fork names to numeric ordering for building cumulative configs.
var efForkLevel = map[string]int{
	"Frontier":          0,
	"Homestead":         1,
	"EIP150":            2,
	"EIP158":            3,
	"SpuriousDragon":    3,
	"TangerineWhistle":  2,
	"Byzantium":         4,
	"Constantinople":    5,
	"ConstantinopleFix": 5,
	"Istanbul":          6,
	"Berlin":            7,
	"London":            8,
	"Merge":             9,
	"Paris":             9,
	"Shanghai":          10,
	"Cancun":            11,
	"Prague":            12,
}

// EFTestChainConfig returns a go-ethereum ChainConfig for a named EF test fork.
func EFTestChainConfig(fork string) (*params.ChainConfig, error) {
	level, ok := efForkLevel[fork]
	if !ok {
		return nil, fmt.Errorf("unsupported fork: %s", fork)
	}

	zero := big.NewInt(0)
	ts := uint64(0)
	c := &params.ChainConfig{ChainID: big.NewInt(1)}

	if level >= 1 {
		c.HomesteadBlock = zero
	}
	if level >= 2 {
		c.EIP150Block = zero
	}
	if level >= 3 {
		c.EIP155Block = zero
		c.EIP158Block = zero
	}
	if level >= 4 {
		c.ByzantiumBlock = zero
	}
	if level >= 5 {
		c.ConstantinopleBlock = zero
		c.PetersburgBlock = zero
	}
	if level >= 6 {
		c.IstanbulBlock = zero
	}
	if level >= 7 {
		c.BerlinBlock = zero
	}
	if level >= 8 {
		c.LondonBlock = zero
	}
	if level >= 9 {
		c.TerminalTotalDifficulty = zero
	}
	if level >= 10 {
		c.ShanghaiTime = &ts
	}
	if level >= 11 {
		c.CancunTime = &ts
	}
	if level >= 12 {
		c.PragueTime = &ts
	}

	return c, nil
}

// AmsterdamFixtureConfig activates the standard Ethereum forks through Amsterdam.
func AmsterdamFixtureConfig() *params.ChainConfig {
	cfg, _ := EFTestChainConfig("Prague")
	zero := uint64(0)
	cfg.OsakaTime, cfg.BPO1Time, cfg.BPO2Time, cfg.AmsterdamTime = &zero, &zero, &zero, &zero
	cfg.BlobScheduleConfig = &params.BlobScheduleConfig{
		Cancun: params.DefaultCancunBlobConfig, Prague: params.DefaultPragueBlobConfig,
		BPO1: params.DefaultBPO1BlobConfig, BPO2: params.DefaultBPO2BlobConfig,
	}
	return cfg
}

// EFTestForkSupported returns whether a fork name is supported for EF tests.
func EFTestForkSupported(fork string) bool {
	_, ok := efForkLevel[fork]
	return ok
}
