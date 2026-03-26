package node

import (
	"encoding/json"
	"fmt"
	"math/big"
	"os"

	coreconfig "github.com/eth2030/eth2030/core/config"
	"github.com/eth2030/eth2030/core/types"
)

// genesisJSON is the JSON-decodable representation of a genesis.json file
// as produced by Kurtosis devnet tooling (compatible with geth genesis format).
type genesisJSON struct {
	Config     chainConfigJSON             `json:"config"`
	Nonce      uint64JSON                  `json:"nonce"`
	Timestamp  uint64JSON                  `json:"timestamp"`
	ExtraData  hexBytes                    `json:"extraData"`
	GasLimit   uint64JSON                  `json:"gasLimit"`
	Difficulty *bigIntJSON                 `json:"difficulty"`
	MixHash    hashJSON                    `json:"mixHash"`
	Coinbase   addressJSON                 `json:"coinbase"`
	Alloc      map[string]allocAccountJSON `json:"alloc"`
	BaseFee    *bigIntJSON                 `json:"baseFeePerGas"`
}

// chainConfigJSON is the JSON-decodable form of coreconfig.ChainConfig.
type chainConfigJSON struct {
	ChainID *bigIntJSON `json:"chainId"`

	HomesteadBlock      *bigIntJSON `json:"homesteadBlock"`
	EIP150Block         *bigIntJSON `json:"eip150Block"`
	EIP155Block         *bigIntJSON `json:"eip155Block"`
	EIP158Block         *bigIntJSON `json:"eip158Block"`
	ByzantiumBlock      *bigIntJSON `json:"byzantiumBlock"`
	ConstantinopleBlock *bigIntJSON `json:"constantinopleBlock"`
	PetersburgBlock     *bigIntJSON `json:"petersburgBlock"`
	IstanbulBlock       *bigIntJSON `json:"istanbulBlock"`
	BerlinBlock         *bigIntJSON `json:"berlinBlock"`
	LondonBlock         *bigIntJSON `json:"londonBlock"`

	TerminalTotalDifficulty *bigIntJSON `json:"terminalTotalDifficulty"`

	ShanghaiTime         *uint64 `json:"shanghaiTime"`
	CancunTime           *uint64 `json:"cancunTime"`
	PragueTime           *uint64 `json:"pragueTime"`
	AmsterdamTime        *uint64 `json:"amsterdamTime"`
	GlamsterdanTime      *uint64 `json:"glamsterdanTime"`
	HogotaTime           *uint64 `json:"hogotaTime"`
	IPlusTime            *uint64 `json:"iPlusTime"`
	BPO1Time             *uint64 `json:"bpo1Time"`
	BPO2Time             *uint64 `json:"bpo2Time"`
	EIP7864FinalHashTime *uint64 `json:"eip7864FinalHashTime"`
}

// allocAccountJSON is an account in the genesis alloc.
type allocAccountJSON struct {
	Balance string            `json:"balance"`
	Code    hexBytes          `json:"code"`
	Nonce   uint64JSON        `json:"nonce"`
	Storage map[string]string `json:"storage"`
}

// bigIntJSON wraps big.Int for JSON decoding from a decimal or hex string.
type bigIntJSON struct{ *big.Int }

func (b *bigIntJSON) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		// Also handle plain number.
		var n json.Number
		if err2 := json.Unmarshal(data, &n); err2 != nil {
			return fmt.Errorf("bigIntJSON: %w", err)
		}
		b.Int = new(big.Int)
		_, ok := b.Int.SetString(n.String(), 10)
		if !ok {
			return fmt.Errorf("bigIntJSON: invalid number %s", n)
		}
		return nil
	}
	b.Int = new(big.Int)
	if len(s) >= 2 && s[:2] == "0x" {
		_, ok := b.Int.SetString(s[2:], 16)
		if !ok {
			return fmt.Errorf("bigIntJSON: invalid hex %s", s)
		}
	} else {
		_, ok := b.Int.SetString(s, 10)
		if !ok {
			return fmt.Errorf("bigIntJSON: invalid decimal %s", s)
		}
	}
	return nil
}

// uint64JSON decodes a decimal or hex-string uint64.
type uint64JSON uint64

func (u *uint64JSON) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		// Plain number.
		var n uint64
		if err2 := json.Unmarshal(data, &n); err2 != nil {
			return fmt.Errorf("uint64JSON: %w", err)
		}
		*u = uint64JSON(n)
		return nil
	}
	if len(s) >= 2 && s[:2] == "0x" {
		n := new(big.Int)
		n.SetString(s[2:], 16)
		*u = uint64JSON(n.Uint64())
	} else {
		n := new(big.Int)
		n.SetString(s, 10)
		*u = uint64JSON(n.Uint64())
	}
	return nil
}

// addressJSON decodes a 0x-prefixed 20-byte hex string into types.Address.
type addressJSON struct{ types.Address }

func (a *addressJSON) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("addressJSON: %w", err)
	}
	if len(s) >= 2 && s[:2] == "0x" {
		s = s[2:]
	}
	b := make([]byte, 20)
	if _, err := fmt.Sscanf(s, "%x", &b); err != nil {
		return fmt.Errorf("addressJSON: invalid address %q", s)
	}
	copy(a.Address[:], b)
	return nil
}

// hashJSON decodes a 0x-prefixed 32-byte hex string into types.Hash.
type hashJSON struct{ types.Hash }

func (h *hashJSON) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("hashJSON: %w", err)
	}
	if len(s) >= 2 && s[:2] == "0x" {
		s = s[2:]
	}
	b := make([]byte, 32)
	if _, err := fmt.Sscanf(s, "%x", &b); err != nil {
		return fmt.Errorf("hashJSON: invalid hash %q", s)
	}
	copy(h.Hash[:], b)
	return nil
}

// hexBytes decodes a 0x-prefixed hex string into []byte.
type hexBytes []byte

func (h *hexBytes) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	if s == "" {
		return nil
	}
	if len(s) < 2 || s[:2] != "0x" {
		*h = []byte(s)
		return nil
	}
	b := make([]byte, (len(s)-2)/2)
	if _, err := fmt.Sscanf(s[2:], "%x", &b); err != nil {
		// fallback: just use the string
		*h = []byte(s)
		return nil
	}
	*h = b
	return nil
}

// loadGenesisFile reads a genesis.json file and converts it to a coreconfig.Genesis.
// Fork override timestamps from config are applied on top of the file values.
func loadGenesisFile(cfg *Config) (*coreconfig.Genesis, error) {
	data, err := os.ReadFile(cfg.GenesisPath)
	if err != nil {
		return nil, fmt.Errorf("read genesis file %s: %w", cfg.GenesisPath, err)
	}

	var gj genesisJSON
	if err := json.Unmarshal(data, &gj); err != nil {
		return nil, fmt.Errorf("decode genesis file: %w", err)
	}

	chainCfg := &coreconfig.ChainConfig{}
	if gj.Config.ChainID != nil {
		chainCfg.ChainID = gj.Config.ChainID.Int
	}
	if gj.Config.HomesteadBlock != nil {
		chainCfg.HomesteadBlock = gj.Config.HomesteadBlock.Int
	}
	if gj.Config.EIP150Block != nil {
		chainCfg.EIP150Block = gj.Config.EIP150Block.Int
	}
	if gj.Config.EIP155Block != nil {
		chainCfg.EIP155Block = gj.Config.EIP155Block.Int
	}
	if gj.Config.EIP158Block != nil {
		chainCfg.EIP158Block = gj.Config.EIP158Block.Int
	}
	if gj.Config.ByzantiumBlock != nil {
		chainCfg.ByzantiumBlock = gj.Config.ByzantiumBlock.Int
	}
	if gj.Config.ConstantinopleBlock != nil {
		chainCfg.ConstantinopleBlock = gj.Config.ConstantinopleBlock.Int
	}
	if gj.Config.PetersburgBlock != nil {
		chainCfg.PetersburgBlock = gj.Config.PetersburgBlock.Int
	}
	if gj.Config.IstanbulBlock != nil {
		chainCfg.IstanbulBlock = gj.Config.IstanbulBlock.Int
	}
	if gj.Config.BerlinBlock != nil {
		chainCfg.BerlinBlock = gj.Config.BerlinBlock.Int
	}
	if gj.Config.LondonBlock != nil {
		chainCfg.LondonBlock = gj.Config.LondonBlock.Int
	}
	if gj.Config.TerminalTotalDifficulty != nil {
		chainCfg.TerminalTotalDifficulty = gj.Config.TerminalTotalDifficulty.Int
	}
	chainCfg.ShanghaiTime = gj.Config.ShanghaiTime
	chainCfg.CancunTime = gj.Config.CancunTime
	chainCfg.PragueTime = gj.Config.PragueTime
	chainCfg.AmsterdamTime = gj.Config.AmsterdamTime
	chainCfg.GlamsterdanTime = gj.Config.GlamsterdanTime
	chainCfg.HogotaTime = gj.Config.HogotaTime
	chainCfg.IPlusTime = gj.Config.IPlusTime
	chainCfg.BPO1Time = gj.Config.BPO1Time
	chainCfg.BPO2Time = gj.Config.BPO2Time
	chainCfg.EIP7864FinalHashTime = gj.Config.EIP7864FinalHashTime

	// Apply fork override timestamps from CLI flags.
	applyForkOverrides(chainCfg, cfg)

	// Resolve network ID.
	networkID := cfg.NetworkID
	if networkID == 0 && chainCfg.ChainID != nil {
		networkID = chainCfg.ChainID.Uint64()
	}
	_ = networkID // passed to node later; stored in Config

	// Parse genesis alloc.
	alloc := make(coreconfig.GenesisAlloc, len(gj.Alloc))
	for addrStr, acc := range gj.Alloc {
		var addr types.Address
		if len(addrStr) >= 2 && addrStr[:2] == "0x" {
			addrStr = addrStr[2:]
		}
		addrBytes := make([]byte, 20)
		if _, err := fmt.Sscanf(addrStr, "%x", &addrBytes); err == nil {
			copy(addr[:], addrBytes)
		}
		bal := parseBalance(acc.Balance)

		// Parse storage slots (hex string -> hex string).
		var storage map[types.Hash]types.Hash
		if len(acc.Storage) > 0 {
			storage = make(map[types.Hash]types.Hash, len(acc.Storage))
			for slotStr, valStr := range acc.Storage {
				slot := types.HexToHash(slotStr)
				val := types.HexToHash(valStr)
				storage[slot] = val
			}
		}

		alloc[addr] = coreconfig.GenesisAccount{
			Balance: bal,
			Code:    acc.Code,
			Nonce:   uint64(acc.Nonce),
			Storage: storage,
		}
	}

	difficulty := new(big.Int)
	if gj.Difficulty != nil {
		difficulty = gj.Difficulty.Int
	}

	var baseFee *big.Int
	if gj.BaseFee != nil {
		baseFee = gj.BaseFee.Int
	}

	genesis := &coreconfig.Genesis{
		Config:     chainCfg,
		Nonce:      uint64(gj.Nonce),
		Timestamp:  uint64(gj.Timestamp),
		ExtraData:  gj.ExtraData,
		GasLimit:   uint64(gj.GasLimit),
		Difficulty: difficulty,
		MixHash:    gj.MixHash.Hash,
		Coinbase:   gj.Coinbase.Address,
		Alloc:      alloc,
		BaseFee:    baseFee,
	}
	return genesis, nil
}

// parseBalance parses a balance string that may be decimal ("12345") or
// 0x-prefixed hex ("0x2FAF080"). Returns zero for empty or invalid input.
func parseBalance(s string) *big.Int {
	n := new(big.Int)
	if s == "" {
		return n
	}
	if len(s) >= 2 && (s[:2] == "0x" || s[:2] == "0X") {
		n.SetString(s[2:], 16)
	} else {
		n.SetString(s, 10)
	}
	return n
}

// applyForkOverrides overwrites ChainConfig fork timestamps with CLI override
// values when set.
func applyForkOverrides(c *coreconfig.ChainConfig, cfg *Config) {
	if cfg.GlamsterdamOverride != nil {
		c.GlamsterdanTime = cfg.GlamsterdamOverride
	}
	if cfg.HogotaOverride != nil {
		c.HogotaTime = cfg.HogotaOverride
	}
	if cfg.IPlusOverride != nil {
		c.IPlusTime = cfg.IPlusOverride
	}
}
