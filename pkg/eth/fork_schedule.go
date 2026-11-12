package eth

import (
	"github.com/eth2030/eth2030/core/config"
	"math/big"
)

func (h *Handler) forkSchedule() ([]uint64, []uint64, bool) {
	chain, ok := h.chain.(interface{ Config() *config.ChainConfig })
	if !ok || chain.Config() == nil {
		return nil, nil, false
	}
	cfg := chain.Config()
	var blocks, times []uint64
	for _, fork := range []*big.Int{cfg.HomesteadBlock, cfg.EIP150Block, cfg.EIP155Block, cfg.EIP158Block, cfg.ByzantiumBlock, cfg.ConstantinopleBlock, cfg.PetersburgBlock, cfg.IstanbulBlock, cfg.MuirGlacierBlock, cfg.BerlinBlock, cfg.LondonBlock, cfg.ArrowGlacierBlock, cfg.GrayGlacierBlock} {
		if fork != nil && fork.IsUint64() {
			blocks = append(blocks, fork.Uint64())
		}
	}
	for _, fork := range []*uint64{cfg.ShanghaiTime, cfg.CancunTime, cfg.PragueTime, cfg.OsakaTime, cfg.AmsterdamTime, cfg.GlamsterdanTime, cfg.HogotaTime, cfg.IPlusTime, cfg.SSZBlockTime, cfg.BPO1Time, cfg.BPO2Time, cfg.EIP7864FinalHashTime} {
		if fork != nil {
			times = append(times, *fork)
		}
	}
	return blocks, times, true
}
