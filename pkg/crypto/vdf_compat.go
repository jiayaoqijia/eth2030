package crypto

// vdf_compat.go provides backward-compatible re-exports from crypto/vdf.
// Existing code that imports "github.com/eth2030/eth2030/crypto" and uses
// VDF types continues to work unchanged. New code should import
// "github.com/eth2030/eth2030/crypto/vdf" directly.

import (
	"github.com/eth2030/eth2030/crypto/vdf"
)

// VDF type aliases — keep crypto.XXX working for all existing importers.
type (
	VDFv2Config        = vdf.VDFv2Config
	VDFv2Result        = vdf.VDFv2Result
	AggregatedVDFProof = vdf.AggregatedVDFProof
	VDFv2              = vdf.VDFv2
	ChainedVDFProof    = vdf.ChainedVDFProof
	BeaconOutput       = vdf.BeaconOutput
	VDFChain           = vdf.VDFChain
	VDFBeacon          = vdf.VDFBeacon
)

// MaxChainLength re-exports the maximum chain length constant.
const MaxChainLength = vdf.MaxChainLength

// VDF function wrappers — delegate to crypto/vdf implementations.

// DefaultVDFv2Config returns sensible defaults for the enhanced VDF.
func DefaultVDFv2Config() VDFv2Config { return vdf.DefaultVDFv2Config() }

// NewVDFv2 creates a new enhanced VDF with the given configuration.
func NewVDFv2(config VDFv2Config) *VDFv2 { return vdf.NewVDFv2(config) }

// NewVDFChain creates a chain evaluator using the given VDFv2 and per-step
// iteration count.
func NewVDFChain(v *VDFv2, itersPerStep uint64) *VDFChain { return vdf.NewVDFChain(v, itersPerStep) }

// NewVDFBeacon creates a beacon with the given chain evaluator and chain length.
func NewVDFBeacon(chain *VDFChain, chainLen uint64) *VDFBeacon {
	return vdf.NewVDFBeacon(chain, chainLen)
}
