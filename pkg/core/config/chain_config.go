package config

import (
	"math/big"

	"github.com/eth2030/eth2030/core/types"
)

// ChainConfig holds chain-level configuration for fork scheduling.
// Pre-merge forks are activated by block number, post-merge by timestamp.
type ChainConfig struct {
	ChainID *big.Int

	// Block-number based forks (pre-merge)
	HomesteadBlock      *big.Int
	EIP150Block         *big.Int
	EIP155Block         *big.Int
	EIP158Block         *big.Int
	ByzantiumBlock      *big.Int
	ConstantinopleBlock *big.Int
	PetersburgBlock     *big.Int
	IstanbulBlock       *big.Int
	MuirGlacierBlock    *big.Int
	BerlinBlock         *big.Int
	LondonBlock         *big.Int
	ArrowGlacierBlock   *big.Int
	GrayGlacierBlock    *big.Int

	// TerminalTotalDifficulty triggers the merge consensus upgrade.
	TerminalTotalDifficulty *big.Int

	// Timestamp-based forks (post-merge)
	ShanghaiTime    *uint64
	CancunTime      *uint64
	PragueTime      *uint64
	OsakaTime       *uint64
	AmsterdamTime   *uint64
	GlamsterdanTime *uint64
	HogotaTime      *uint64
	IPlusTime       *uint64

	// DepositContractAddress selects the EIP-6110 event emitter. Zero uses
	// the mainnet deposit contract address.
	DepositContractAddress types.Address
	// RequireRequestPredeploys enforces the EIP-7002/7251 code requirement.
	// Minimal private chains may omit the queue contracts.
	RequireRequestPredeploys bool

	// LegacyNativeBLSUntil preserves the experimental native precompile
	// layout before the given timestamp. Nil selects the Ethereum layout.
	LegacyNativeBLSUntil *uint64 `json:",omitempty"`

	// BPO (Blob Parameter Optimization) fork timestamps for blob schedule upgrades.
	BPO1Time *uint64
	BPO2Time *uint64

	// EIP7864FinalHashTime is the timestamp at which the binary trie switches
	// from SHA-256 to BLAKE3-256 as the canonical hash function (BL-1.3).
	// Nil means the fork is not yet activated; SHA-256 remains default.
	EIP7864FinalHashTime *uint64

	// BinaryTreeHashFunc overrides the hash function used by the binary trie.
	// Accepted values: "sha256" (default), "blake3".
	// This field is derived from EIP7864FinalHashTime but can be set directly
	// for testing.
	BinaryTreeHashFunc string
}

// Block-number fork checks

func isBlockForked(forkBlock, head *big.Int) bool {
	if forkBlock == nil || head == nil {
		return false
	}
	return forkBlock.Cmp(head) <= 0
}

// IsHomestead returns whether the given block number is at or past Homestead.
func (c *ChainConfig) IsHomestead(num *big.Int) bool {
	return isBlockForked(c.HomesteadBlock, num)
}

// IsEIP150 returns whether the given block number is at or past EIP-150.
func (c *ChainConfig) IsEIP150(num *big.Int) bool {
	return isBlockForked(c.EIP150Block, num)
}

// IsEIP155 returns whether the given block number is at or past EIP-155.
func (c *ChainConfig) IsEIP155(num *big.Int) bool {
	return isBlockForked(c.EIP155Block, num)
}

// IsEIP158 returns whether the given block number is at or past EIP-158.
func (c *ChainConfig) IsEIP158(num *big.Int) bool {
	return isBlockForked(c.EIP158Block, num)
}

// IsByzantium returns whether the given block number is at or past Byzantium.
func (c *ChainConfig) IsByzantium(num *big.Int) bool {
	return isBlockForked(c.ByzantiumBlock, num)
}

// IsConstantinople returns whether the given block number is at or past Constantinople.
func (c *ChainConfig) IsConstantinople(num *big.Int) bool {
	return isBlockForked(c.ConstantinopleBlock, num)
}

// IsPetersburg returns whether the given block number is at or past Petersburg.
// Petersburg is a fix-fork for Constantinople; if PetersburgBlock is nil,
// it activates at the same block as Constantinople.
func (c *ChainConfig) IsPetersburg(num *big.Int) bool {
	return isBlockForked(c.PetersburgBlock, num) || c.PetersburgBlock == nil && isBlockForked(c.ConstantinopleBlock, num)
}

// IsIstanbul returns whether the given block number is at or past Istanbul.
func (c *ChainConfig) IsIstanbul(num *big.Int) bool {
	return isBlockForked(c.IstanbulBlock, num)
}

// IsBerlin returns whether the given block number is at or past Berlin.
func (c *ChainConfig) IsBerlin(num *big.Int) bool {
	return isBlockForked(c.BerlinBlock, num)
}

// IsLondon returns whether the given block number is at or past London.
func (c *ChainConfig) IsLondon(num *big.Int) bool {
	return isBlockForked(c.LondonBlock, num)
}

// Timestamp-based fork checks

func isTimestampForked(forkTime *uint64, blockTime uint64) bool {
	if forkTime == nil {
		return false
	}
	return *forkTime <= blockTime
}

// IsShanghai returns whether the given block time is at or past Shanghai.
func (c *ChainConfig) IsShanghai(time uint64) bool {
	return isTimestampForked(c.ShanghaiTime, time)
}

// IsCancun returns whether the given block time is at or past Cancun.
func (c *ChainConfig) IsCancun(time uint64) bool {
	return isTimestampForked(c.CancunTime, time)
}

// IsPrague returns whether the given block time is at or past Prague.
func (c *ChainConfig) IsPrague(time uint64) bool {
	return isTimestampForked(c.PragueTime, time)
}

// IsOsaka reports whether the configured Osaka timestamp has been reached.
func (c *ChainConfig) IsOsaka(t uint64) bool {
	return isTimestampForked(c.OsakaTime, t)
}

// IsAmsterdam returns whether the given block time is at or past Amsterdam.
func (c *ChainConfig) IsAmsterdam(time uint64) bool {
	return isTimestampForked(c.AmsterdamTime, time)
}

// IsGlamsterdan returns whether the given block time is at or past Glamsterdan.
// Glamsterdan includes EIP-7904 (compute gas cost increase).
func (c *ChainConfig) IsGlamsterdan(time uint64) bool {
	return isTimestampForked(c.GlamsterdanTime, time)
}

// IsHogota returns whether the given block time is at or past Hogota.
// Hogota includes multidimensional gas pricing (EIP-7706/7999).
func (c *ChainConfig) IsHogota(time uint64) bool {
	return isTimestampForked(c.HogotaTime, time)
}

// IsIPlus returns whether the given block time is at or past I+.
// I+ includes NTT precompile (EIP-7885), native rollups, zkVM, proof aggregation.
func (c *ChainConfig) IsIPlus(time uint64) bool {
	return isTimestampForked(c.IPlusTime, time)
}

// IsEIP7864FinalHash returns whether the binary trie BLAKE3 hash function
// fork is active at the given timestamp (BL-1.3). When active, the binary
// trie uses BLAKE3-256 instead of SHA-256 for internal node hashing.
func (c *ChainConfig) IsEIP7864FinalHash(time uint64) bool {
	return isTimestampForked(c.EIP7864FinalHashTime, time)
}

// BinaryTrieHashFunc returns the hash function name to use for the binary trie
// at the given block timestamp. Returns HashFunctionBlake3 when the
// EIP-7864 final-hash fork is active, otherwise HashFunctionSHA256.
func (c *ChainConfig) BinaryTrieHashFuncAt(time uint64) string {
	if c.BinaryTreeHashFunc != "" {
		return c.BinaryTreeHashFunc
	}
	if c.IsEIP7864FinalHash(time) {
		return "blake3"
	}
	return "sha256"
}

// IsBPO1 returns whether the given block time is at or past BPO1.
// BPO1 increases blob target to 10, max to 15.
func (c *ChainConfig) IsBPO1(time uint64) bool {
	return isTimestampForked(c.BPO1Time, time)
}

// IsBPO2 returns whether the given block time is at or past BPO2.
// BPO2 increases blob target to 14, max to 21.
func (c *ChainConfig) IsBPO2(time uint64) bool {
	return isTimestampForked(c.BPO2Time, time)
}

// Merge check

// IsMerge returns whether terminal total difficulty has been set,
// indicating the chain has transitioned to proof-of-stake.
func (c *ChainConfig) IsMerge() bool {
	return c.TerminalTotalDifficulty != nil
}

// EIP-specific convenience checks

// IsEIP1559 returns whether EIP-1559 (base fee) is active. Activated with London.
func (c *ChainConfig) IsEIP1559(num *big.Int) bool {
	return c.IsLondon(num)
}

// IsEIP2929 returns whether EIP-2929 (gas cost increases for state access) is active. Activated with Berlin.
func (c *ChainConfig) IsEIP2929(num *big.Int) bool {
	return c.IsBerlin(num)
}

// IsEIP3529 returns whether EIP-3529 (reduction in refunds) is active. Activated with London.
func (c *ChainConfig) IsEIP3529(num *big.Int) bool {
	return c.IsLondon(num)
}

// IsEIP4844 returns whether EIP-4844 (blob transactions) is active. Activated with Cancun.
func (c *ChainConfig) IsEIP4844(time uint64) bool {
	return c.IsCancun(time)
}

// IsEIP7702 returns whether EIP-7702 (set code tx) is active. Activated with Prague.
func (c *ChainConfig) IsEIP7702(time uint64) bool {
	return c.IsPrague(time)
}

// IsEIP7706 returns whether EIP-7706 (separate calldata gas) is active. Activated with Glamsterdan.
func (c *ChainConfig) IsEIP7706(time uint64) bool {
	return c.IsGlamsterdan(time)
}

// IsEIP7778 returns whether EIP-7778 (block gas accounting without refunds) is active.
func (c *ChainConfig) IsEIP7778(time uint64) bool {
	return c.IsGlamsterdan(time)
}

// IsEIP2780 returns whether EIP-2780 (reduced intrinsic transaction gas) is active.
func (c *ChainConfig) IsEIP2780(time uint64) bool {
	return c.IsGlamsterdan(time)
}

// IsEIP7976 returns whether EIP-7976 (increased calldata floor cost) is active.
func (c *ChainConfig) IsEIP7976(time uint64) bool {
	return c.IsGlamsterdan(time)
}

// IsEIP7981 returns whether EIP-7981 (access list floor cost) is active.
func (c *ChainConfig) IsEIP7981(time uint64) bool {
	return c.IsGlamsterdan(time)
}

// IsEIP8037 returns whether EIP-8037 (state creation gas increase) is active.
func (c *ChainConfig) IsEIP8037(time uint64) bool {
	return c.IsGlamsterdan(time)
}

// IsEIP8038 returns whether EIP-8038 (state access gas increase) is active.
func (c *ChainConfig) IsEIP8038(time uint64) bool {
	return c.IsGlamsterdan(time)
}

// IsEIP7999 returns whether EIP-7999 (unified multidimensional fee market) is active. Activated with Hogota.
func (c *ChainConfig) IsEIP7999(time uint64) bool {
	return c.IsHogota(time)
}

// IsEIP7885 returns whether EIP-7885 (NTT precompile) is active. Activated with I+.
func (c *ChainConfig) IsEIP7885(time uint64) bool {
	return c.IsIPlus(time)
}

// Rules returns a Rules struct for the given block number and timestamp,
// providing boolean flags for quick fork checks.
func (c *ChainConfig) Rules(num *big.Int, isMerge bool, timestamp uint64) Rules {
	// Disallow setting merge out of order.
	isMerge = isMerge && c.IsLondon(num)
	return Rules{
		ChainID:          new(big.Int).Set(c.ChainID),
		IsHomestead:      c.IsHomestead(num),
		IsEIP150:         c.IsEIP150(num),
		IsEIP155:         c.IsEIP155(num),
		IsEIP158:         c.IsEIP158(num),
		IsByzantium:      c.IsByzantium(num),
		IsConstantinople: c.IsConstantinople(num),
		IsPetersburg:     c.IsPetersburg(num),
		IsIstanbul:       c.IsIstanbul(num),
		IsBerlin:         c.IsBerlin(num),
		IsEIP2929:        c.IsBerlin(num),
		IsLondon:         c.IsLondon(num),
		IsEIP1559:        c.IsLondon(num),
		IsEIP3529:        c.IsLondon(num),
		IsMerge:          isMerge,
		IsShanghai:       isMerge && c.IsShanghai(timestamp),
		IsCancun:         isMerge && c.IsCancun(timestamp),
		IsEIP4844:        isMerge && c.IsCancun(timestamp),
		IsPrague:         isMerge && c.IsPrague(timestamp),
		IsEIP7702:        isMerge && c.IsPrague(timestamp),
		IsAmsterdam:      isMerge && c.IsAmsterdam(timestamp),
		IsGlamsterdan:    isMerge && c.IsGlamsterdan(timestamp),
		IsEIP7904:        isMerge && c.IsGlamsterdan(timestamp),
		IsEIP7706:        isMerge && c.IsGlamsterdan(timestamp),
		IsEIP7708:        isMerge && c.IsGlamsterdan(timestamp),
		IsEIP7954:        isMerge && c.IsGlamsterdan(timestamp),
		IsEIP7778:        isMerge && c.IsGlamsterdan(timestamp),
		IsEIP2780:        isMerge && c.IsGlamsterdan(timestamp),
		IsEIP7976:        isMerge && c.IsGlamsterdan(timestamp),
		IsEIP7981:        isMerge && c.IsGlamsterdan(timestamp),
		IsEIP8037:        isMerge && c.IsGlamsterdan(timestamp),
		IsEIP8038:        isMerge && c.IsGlamsterdan(timestamp),
		IsHogota:         isMerge && c.IsHogota(timestamp),
		IsEIP7999:        isMerge && c.IsHogota(timestamp),
		IsIPlus:          isMerge && c.IsIPlus(timestamp),
		LegacyNativeBLS:  isMerge && c.LegacyNativeBLSUntil != nil && timestamp < *c.LegacyNativeBLSUntil,
		IsEIP7885:        isMerge && c.IsIPlus(timestamp),
	}
}

// Rules contains boolean flags for quick fork activation checks.
type Rules struct {
	ChainID                                                 *big.Int
	IsHomestead, IsEIP150, IsEIP155, IsEIP158               bool
	IsByzantium, IsConstantinople, IsPetersburg, IsIstanbul bool
	IsBerlin, IsEIP2929                                     bool
	IsLondon, IsEIP1559, IsEIP3529                          bool
	IsMerge                                                 bool
	IsShanghai                                              bool
	IsCancun, IsEIP4844                                     bool
	IsPrague, IsEIP7702                                     bool
	IsAmsterdam                                             bool
	IsGlamsterdan, IsEIP7904, IsEIP7706, IsEIP7708          bool
	IsEIP7954                                               bool
	IsEIP7778, IsEIP2780, IsEIP7976, IsEIP7981              bool
	IsEIP8037, IsEIP8038                                    bool
	IsHogota, IsEIP7999                                     bool
	IsIPlus, IsEIP7885                                      bool
	LegacyNativeBLS                                         bool
}

func newUint64(v uint64) *uint64 { return &v }

// Mainnet TTD: 58,750,000,000,000,000,000,000
var MainnetTerminalTotalDifficulty, _ = new(big.Int).SetString("58750000000000000000000", 10)

// MainnetConfig is the chain config for Ethereum mainnet.
var MainnetConfig = &ChainConfig{
	DepositContractAddress:   types.DepositContractAddress,
	RequireRequestPredeploys: true,

	ChainID:                 big.NewInt(1),
	HomesteadBlock:          big.NewInt(1_150_000),
	EIP150Block:             big.NewInt(2_463_000),
	EIP155Block:             big.NewInt(2_675_000),
	EIP158Block:             big.NewInt(2_675_000),
	ByzantiumBlock:          big.NewInt(4_370_000),
	ConstantinopleBlock:     big.NewInt(7_280_000),
	PetersburgBlock:         big.NewInt(7_280_000),
	IstanbulBlock:           big.NewInt(9_069_000),
	MuirGlacierBlock:        big.NewInt(9_200_000),
	BerlinBlock:             big.NewInt(12_244_000),
	LondonBlock:             big.NewInt(12_965_000),
	ArrowGlacierBlock:       big.NewInt(13_773_000),
	GrayGlacierBlock:        big.NewInt(15_050_000),
	TerminalTotalDifficulty: MainnetTerminalTotalDifficulty,
	ShanghaiTime:            newUint64(1681338455),
	CancunTime:              newUint64(1710338135),
	PragueTime:              newUint64(1746612311),
	OsakaTime:               newUint64(1764798551),
	AmsterdamTime:           nil, // not yet scheduled
	GlamsterdanTime:         nil, // not yet scheduled
	HogotaTime:              nil, // not yet scheduled
	IPlusTime:               nil, // not yet scheduled
	BPO1Time:                nil, // not yet scheduled
	BPO2Time:                nil, // not yet scheduled
}

// SepoliaConfig is the chain config for the Sepolia test network.
var SepoliaConfig = &ChainConfig{
	DepositContractAddress:   types.HexToAddress("0x7f02c3e3c98b133055b8b348b2ac625669ed295d"),
	RequireRequestPredeploys: true,

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
	TerminalTotalDifficulty: big.NewInt(17_000_000_000_000_000),
	ShanghaiTime:            newUint64(1677557088),
	CancunTime:              newUint64(1706655072),
	PragueTime:              newUint64(1741159776),
	OsakaTime:               newUint64(1760427360),
	AmsterdamTime:           nil,
	GlamsterdanTime:         nil,
	HogotaTime:              nil,
	IPlusTime:               nil,
	BPO1Time:                nil,
	BPO2Time:                nil,
}

// HoleskyConfig is the chain config for the Holesky test network.
var HoleskyConfig = &ChainConfig{
	DepositContractAddress:   types.HexToAddress("0x4242424242424242424242424242424242424242"),
	RequireRequestPredeploys: true,

	ChainID:                 big.NewInt(17000),
	HomesteadBlock:          big.NewInt(0),
	EIP150Block:             big.NewInt(0),
	EIP155Block:             big.NewInt(0),
	EIP158Block:             big.NewInt(0),
	ByzantiumBlock:          big.NewInt(0),
	ConstantinopleBlock:     big.NewInt(0),
	PetersburgBlock:         big.NewInt(0),
	IstanbulBlock:           big.NewInt(0),
	BerlinBlock:             big.NewInt(0),
	LondonBlock:             big.NewInt(0),
	TerminalTotalDifficulty: big.NewInt(0),
	ShanghaiTime:            newUint64(1696000704),
	CancunTime:              newUint64(1707305664),
	PragueTime:              newUint64(1740434112),
	OsakaTime:               newUint64(1759308480),
	AmsterdamTime:           nil,
	GlamsterdanTime:         nil,
	HogotaTime:              nil,
	IPlusTime:               nil,
	BPO1Time:                nil,
	BPO2Time:                nil,
}

// TestConfig is a chain config with forks up to Amsterdam active at genesis.
// Glamsterdam gas repricing is NOT active, preserving pre-Glamsterdam gas semantics.
var TestConfig = &ChainConfig{
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
	ShanghaiTime:            newUint64(0),
	CancunTime:              newUint64(0),
	PragueTime:              newUint64(0),
	AmsterdamTime:           newUint64(0),
	GlamsterdanTime:         nil,
	HogotaTime:              nil,
	IPlusTime:               nil,
	BPO1Time:                nil,
	BPO2Time:                nil,
}

// TestConfigGlamsterdan is a chain config with all forks including Glamsterdam active.
// Use this for tests that specifically exercise Glamsterdam gas repricing.
var TestConfigGlamsterdan = &ChainConfig{
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
	ShanghaiTime:            newUint64(0),
	CancunTime:              newUint64(0),
	PragueTime:              newUint64(0),
	AmsterdamTime:           newUint64(0),
	GlamsterdanTime:         newUint64(0),
	HogotaTime:              nil,
	IPlusTime:               nil,
	BPO1Time:                nil,
	BPO2Time:                nil,
}

// TestConfigHogota is a chain config with all forks including Hogota active.
// Use this for tests that exercise multidimensional gas pricing.
var TestConfigHogota = &ChainConfig{
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
	ShanghaiTime:            newUint64(0),
	CancunTime:              newUint64(0),
	PragueTime:              newUint64(0),
	AmsterdamTime:           newUint64(0),
	GlamsterdanTime:         newUint64(0),
	HogotaTime:              newUint64(0),
	IPlusTime:               nil,
	BPO1Time:                nil,
	BPO2Time:                nil,
}

// TestConfigBPO2 is a chain config with all forks including BPO2 active.
// Use this for tests that exercise blob parameter optimization schedules.
var TestConfigBPO2 = &ChainConfig{
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
	ShanghaiTime:            newUint64(0),
	CancunTime:              newUint64(0),
	PragueTime:              newUint64(0),
	AmsterdamTime:           newUint64(0),
	GlamsterdanTime:         newUint64(0),
	HogotaTime:              newUint64(0),
	IPlusTime:               nil,
	BPO1Time:                newUint64(0),
	BPO2Time:                newUint64(0),
}
