package config

import (
	"errors"
	"fmt"
	"math/big"
)

// ForkOrder lists all Ethereum hard forks in chronological activation order.
// Block-number forks come first, then timestamp-based forks.
var ForkOrder = []string{
	"Homestead",
	"EIP150",
	"EIP155",
	"EIP158",
	"Byzantium",
	"Constantinople",
	"Petersburg",
	"Istanbul",
	"MuirGlacier",
	"Berlin",
	"London",
	"ArrowGlacier",
	"GrayGlacier",
	"Paris", // The Merge
	"Shanghai",
	"Cancun",
	"Prague",
	"Amsterdam",
	"Glamsterdan",
	"Hogota",
	"IPlus",
}

// Validate checks that the chain configuration is internally consistent.
// It verifies that:
//   - ChainID is set and positive
//   - Block-number forks are in ascending order
//   - Timestamp forks are in ascending order
//   - Post-merge forks require TerminalTotalDifficulty to be set
func (c *ChainConfig) Validate() error {
	if c.ChainID == nil || c.ChainID.Sign() <= 0 {
		return errors.New("invalid chain ID: must be positive")
	}

	// Validate block-number fork ordering.
	blockForks := []struct {
		name  string
		block *big.Int
	}{
		{"Homestead", c.HomesteadBlock},
		{"EIP150", c.EIP150Block},
		{"EIP155", c.EIP155Block},
		{"EIP158", c.EIP158Block},
		{"Byzantium", c.ByzantiumBlock},
		{"Constantinople", c.ConstantinopleBlock},
		{"Petersburg", c.PetersburgBlock},
		{"Istanbul", c.IstanbulBlock},
		{"Berlin", c.BerlinBlock},
		{"London", c.LondonBlock},
	}
	var lastBlock *big.Int
	var lastName string
	for _, f := range blockForks {
		if f.block == nil {
			continue
		}
		if f.block.Sign() < 0 {
			return fmt.Errorf("invalid %s fork block: must be >= 0", f.name)
		}
		if lastBlock != nil && f.block.Cmp(lastBlock) < 0 {
			return fmt.Errorf("fork ordering: %s (block %s) must be >= %s (block %s)",
				f.name, f.block, lastName, lastBlock)
		}
		lastBlock = f.block
		lastName = f.name
	}

	// Validate timestamp fork ordering.
	timeForks := []struct {
		name string
		time *uint64
	}{
		{"Shanghai", c.ShanghaiTime},
		{"Cancun", c.CancunTime},
		{"Prague", c.PragueTime},
		{"Amsterdam", c.AmsterdamTime},
		{"Glamsterdan", c.GlamsterdanTime},
		{"Hogota", c.HogotaTime},
		{"IPlus", c.IPlusTime},
		{"SSZBlocks", c.SSZBlockTime},
	}
	var lastTime *uint64
	var lastTimeName string
	for _, f := range timeForks {
		if f.time == nil {
			continue
		}
		if lastTime != nil && *f.time < *lastTime {
			return fmt.Errorf("fork ordering: %s (time %d) must be >= %s (time %d)",
				f.name, *f.time, lastTimeName, *lastTime)
		}
		lastTime = f.time
		lastTimeName = f.name
	}

	// Post-merge timestamp forks require TerminalTotalDifficulty.
	if c.ShanghaiTime != nil && c.TerminalTotalDifficulty == nil {
		return errors.New("Shanghai requires TerminalTotalDifficulty to be set")
	}

	return nil
}

// ActiveFork returns the name of the most recent active fork at the given
// timestamp. It assumes a post-merge chain (all block-number forks passed).
// Returns "London" if no timestamp forks are active yet.
func (c *ChainConfig) ActiveFork(time uint64) string {
	// Check timestamp forks from newest to oldest.
	if c.IsIPlus(time) {
		return "IPlus"
	}
	if c.IsHogota(time) {
		return "Hogota"
	}
	if c.IsGlamsterdan(time) {
		return "Glamsterdan"
	}
	if c.IsAmsterdam(time) {
		return "Amsterdam"
	}
	if c.IsPrague(time) {
		return "Prague"
	}
	if c.IsCancun(time) {
		return "Cancun"
	}
	if c.IsShanghai(time) {
		return "Shanghai"
	}
	if c.IsMerge() {
		return "Paris"
	}
	return "London"
}

// GetRules returns a Rules struct with fork activation flags for the given
// block number and timestamp. This is a convenience wrapper around Rules()
// that automatically determines the merge status.
func (c *ChainConfig) GetRules(blockNum uint64, time uint64) *Rules {
	num := new(big.Int).SetUint64(blockNum)
	isMerge := c.IsMerge()
	r := c.Rules(num, isMerge, time)
	return &r
}

// MainnetConfig returns the mainnet chain configuration.
func MainnetConfigFunc() *ChainConfig {
	// Return a copy to prevent mutation of the global.
	cfg := *MainnetConfig
	cfg.ChainID = new(big.Int).Set(MainnetConfig.ChainID)
	if MainnetConfig.TerminalTotalDifficulty != nil {
		cfg.TerminalTotalDifficulty = new(big.Int).Set(MainnetConfig.TerminalTotalDifficulty)
	}
	return &cfg
}

// TestnetConfig returns a testnet (Sepolia-like) chain configuration with
// all pre-merge forks active at genesis and post-merge forks scheduled.
func TestnetConfig() *ChainConfig {
	shanghaiTime := uint64(0)
	cancunTime := uint64(0)
	pragueTime := uint64(1000)
	amsterdamTime := uint64(2000)
	glamsterdanTime := uint64(3000)
	hogotaTime := uint64(4000)
	iPlusTime := uint64(5000)

	return &ChainConfig{
		ChainID:                 big.NewInt(11155111),
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
		ShanghaiTime:            &shanghaiTime,
		CancunTime:              &cancunTime,
		PragueTime:              &pragueTime,
		AmsterdamTime:           &amsterdamTime,
		GlamsterdanTime:         &glamsterdanTime,
		HogotaTime:              &hogotaTime,
		IPlusTime:               &iPlusTime,
	}
}

// DevConfig returns a development/local chain configuration with all known
// forks active at genesis (timestamp 0). Useful for testing and local devnets.
func DevConfig() *ChainConfig {
	zero := uint64(0)
	return &ChainConfig{
		ChainID:                 big.NewInt(1337),
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
		ShanghaiTime:            &zero,
		CancunTime:              &zero,
		PragueTime:              &zero,
		AmsterdamTime:           &zero,
		GlamsterdanTime:         &zero,
		HogotaTime:              &zero,
		IPlusTime:               &zero,
		BPO1Time:                &zero,
		BPO2Time:                &zero,
	}
}
