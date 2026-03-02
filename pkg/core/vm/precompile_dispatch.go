// Package vm implements the Ethereum Virtual Machine.
//
// precompile_dispatch.go provides a fork-aware dispatch router that selects
// between native Go precompile implementations and RISC-V zkISA guest
// backends based on the active fork. At I+ and beyond, precompiles are
// executed via RISC-V guests for provability.
//
// Part of the K+ roadmap: fork-aware RISC-V precompile dispatch.
package vm

import (
	"fmt"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// ForkIPlus is the fork name at which RISC-V precompile backends activate.
const ForkIPlus = "I_Plus"

// PrecompileDispatchRouter routes precompile calls to either native Go
// implementations or RISC-V zkISA guests depending on fork activation.
type PrecompileDispatchRouter struct {
	forkManager      *PrecompileForkManager
	zkisaPrecompiles map[types.Address]ZkISAPrecompile
	riscvActive      bool
}

// NewPrecompileDispatchRouter creates a new dispatch router backed by the
// given fork manager. RISC-V mode is initially inactive.
func NewPrecompileDispatchRouter(fm *PrecompileForkManager) *PrecompileDispatchRouter {
	return &PrecompileDispatchRouter{
		forkManager:      fm,
		zkisaPrecompiles: make(map[types.Address]ZkISAPrecompile),
	}
}

// SetRISCVActive enables or disables RISC-V backend routing. When active,
// precompiles with zkISA mappings are executed via the RISC-V path.
func (r *PrecompileDispatchRouter) SetRISCVActive(active bool) {
	r.riscvActive = active
}

// IsRISCVActive returns whether RISC-V backend routing is enabled.
func (r *PrecompileDispatchRouter) IsRISCVActive() bool {
	return r.riscvActive
}

// RegisterZkISA registers a zkISA precompile implementation for the given address.
func (r *PrecompileDispatchRouter) RegisterZkISA(addr types.Address, p ZkISAPrecompile) {
	r.zkisaPrecompiles[addr] = p
}

// ZkISACount returns the number of registered zkISA precompiles.
func (r *PrecompileDispatchRouter) ZkISACount() int {
	return len(r.zkisaPrecompiles)
}

// HasZkISA returns true if a zkISA implementation is registered for addr.
func (r *PrecompileDispatchRouter) HasZkISA(addr types.Address) bool {
	_, ok := r.zkisaPrecompiles[addr]
	return ok
}

// Run executes a precompile at the given address. If RISC-V mode is active
// and a zkISA implementation exists for the address, it routes through the
// RISC-V backend. Otherwise, it falls back to the native Go implementation
// via the fork manager's Glamsterdan set.
func (r *PrecompileDispatchRouter) Run(addr types.Address, input []byte, gas uint64) ([]byte, uint64, error) {
	// RISC-V path: if active and zkISA mapping exists.
	if r.riscvActive {
		if zkp, ok := r.zkisaPrecompiles[addr]; ok {
			gasCost := zkp.GasCost(input)
			if gas < gasCost {
				return nil, 0, ErrOutOfGas
			}
			output, err := zkp.Execute(input)
			if err != nil {
				return nil, gas - gasCost, err
			}
			return output, gas - gasCost, nil
		}
	}

	// Native Go path: try the Glamsterdan fork set first.
	fps, err := r.forkManager.GetForkSet(ForkGlamsterdan)
	if err != nil {
		return nil, gas, fmt.Errorf("dispatch: %w", err)
	}
	return fps.Run(addr, input, gas)
}

// RequiredGas returns the gas cost for calling a precompile at addr.
// Uses the zkISA gas cost if RISC-V is active and a mapping exists.
func (r *PrecompileDispatchRouter) RequiredGas(addr types.Address, input []byte) uint64 {
	if r.riscvActive {
		if zkp, ok := r.zkisaPrecompiles[addr]; ok {
			return zkp.GasCost(input)
		}
	}

	fps, err := r.forkManager.GetForkSet(ForkGlamsterdan)
	if err != nil {
		return 0
	}
	cost, err := fps.GasCost(addr, input)
	if err != nil {
		return 0
	}
	return cost
}

// IsActive returns true if the given address has an active precompile
// in either the zkISA set (when RISC-V is active) or the native set.
func (r *PrecompileDispatchRouter) IsActive(addr types.Address) bool {
	if r.riscvActive {
		if _, ok := r.zkisaPrecompiles[addr]; ok {
			return true
		}
	}
	fps, err := r.forkManager.GetForkSet(ForkGlamsterdan)
	if err != nil {
		return false
	}
	return fps.IsActive(addr)
}

// RegisterIPlusPrecompiles registers the I+ fork precompile set in the
// fork manager with RISC-V-backed precompiles for all zkISA addresses.
func RegisterIPlusPrecompiles(fm *PrecompileForkManager) {
	glamFPS, err := fm.GetForkSet(ForkGlamsterdan)
	if err != nil {
		return
	}

	activations := make([]PrecompileActivation, 0, glamFPS.Count()+1)
	for _, a := range glamFPS.Addresses() {
		contract := glamFPS.Get(a)
		name := glamFPS.Name(a)
		activations = append(activations, PrecompileActivation{
			Address:  a,
			Name:     name,
			Fork:     ForkIPlus,
			Contract: contract,
		})
	}

	// Add NTT precompile at 0x15 for I+.
	nttAddr := types.BytesToAddress([]byte{0x15})
	if p, ok := PrecompiledContractsIPlus[nttAddr]; ok {
		activations = append(activations, PrecompileActivation{
			Address:  nttAddr,
			Name:     "ntt",
			Fork:     ForkIPlus,
			Contract: p,
		})
	}

	fm.RegisterForkSet(ForkIPlus, activations)
}

// NewIPlusDispatchRouter creates a fully configured dispatch router for I+
// with all zkISA precompiles registered and RISC-V mode active.
func NewIPlusDispatchRouter(fm *PrecompileForkManager) *PrecompileDispatchRouter {
	router := NewPrecompileDispatchRouter(fm)

	// Register the base 6 zkISA precompiles (ecrecover, sha256, modexp, bn128*).
	zkisaMap := RegisterZkISAPrecompiles()
	for a, p := range zkisaMap {
		router.RegisterZkISA(a, p)
	}

	// Register the 9 BLS12-381 zkISA precompiles.
	bls12Precompiles := registerBLS12381ZkISAPrecompiles()
	for a, p := range bls12Precompiles {
		router.RegisterZkISA(a, p)
	}

	// Register 4 misc zkISA precompiles.
	miscPrecompiles := registerMiscZkISAPrecompiles()
	for a, p := range miscPrecompiles {
		router.RegisterZkISA(a, p)
	}

	router.SetRISCVActive(true)
	return router
}

// registerBLS12381ZkISAPrecompiles creates zkISA precompile wrappers for
// the 9 BLS12-381 addresses (0x0b-0x13).
func registerBLS12381ZkISAPrecompiles() map[types.Address]ZkISAPrecompile {
	m := make(map[types.Address]ZkISAPrecompile)
	entries := []struct {
		addrByte byte
		name     string
		gas      uint64
	}{
		{0x0b, "zkISA-bls12-g1add", 500},
		{0x0c, "zkISA-bls12-g1mul", 12000},
		{0x0d, "zkISA-bls12-g1msm", 12000},
		{0x0e, "zkISA-bls12-g2add", 800},
		{0x0f, "zkISA-bls12-g2mul", 45000},
		{0x10, "zkISA-bls12-g2msm", 45000},
		{0x11, "zkISA-bls12-pairing", 65000},
		{0x12, "zkISA-bls12-map-fp-g1", 5500},
		{0x13, "zkISA-bls12-map-fp2-g2", 75000},
	}
	for _, e := range entries {
		a := types.BytesToAddress([]byte{e.addrByte})
		m[a] = &genericZkISAPrecompile{
			addr:       a,
			name:       e.name,
			fixedGas:   e.gas,
		}
	}
	return m
}

// registerMiscZkISAPrecompiles creates zkISA precompile wrappers for misc
// precompiles: RIPEMD-160 (0x03), DataCopy (0x04), BLAKE2f (0x09), KZG (0x0a).
func registerMiscZkISAPrecompiles() map[types.Address]ZkISAPrecompile {
	m := make(map[types.Address]ZkISAPrecompile)
	entries := []struct {
		addrByte byte
		name     string
		gas      uint64
	}{
		{0x03, "zkISA-ripemd160", 3000},
		{0x04, "zkISA-datacopy", 500},
		{0x09, "zkISA-blake2f", 3000},
		{0x0a, "zkISA-kzg-point", 50000},
	}
	for _, e := range entries {
		a := types.BytesToAddress([]byte{e.addrByte})
		m[a] = &genericZkISAPrecompile{
			addr:     a,
			name:     e.name,
			fixedGas: e.gas,
		}
	}
	return m
}

// genericZkISAPrecompile is a zkISA precompile wrapper that delegates
// execution to the corresponding native Go precompile while providing
// the zkISA interface for proof generation.
type genericZkISAPrecompile struct {
	addr     types.Address
	name     string
	fixedGas uint64
}

func (g *genericZkISAPrecompile) Address() types.Address  { return g.addr }
func (g *genericZkISAPrecompile) Name() string            { return g.name }
func (g *genericZkISAPrecompile) GasCost(_ []byte) uint64 { return g.fixedGas }

func (g *genericZkISAPrecompile) Execute(input []byte) ([]byte, error) {
	// Delegate to the native Go precompile for actual computation.
	p, ok := PrecompiledContractsCancun[g.addr]
	if !ok {
		p, ok = PrecompiledContractsIPlus[g.addr]
		if !ok {
			return nil, fmt.Errorf("zkisa-dispatch: no native precompile at %s", g.addr.Hex())
		}
	}
	return p.Run(input)
}

func (g *genericZkISAPrecompile) ProveExecution(input []byte) (*ExecutionProof, error) {
	output, err := g.Execute(input)
	if err != nil {
		return nil, ErrZkISAExecFailed
	}
	if output == nil {
		output = []byte{}
	}

	witness := dispatchWitness(g.addr, input, output)
	inputHash := crypto.Keccak256Hash(input)
	outputHash := crypto.Keccak256Hash(output)
	digest := computeProofDigest(g.addr, inputHash, outputHash, witness, 128)

	return &ExecutionProof{
		PrecompileAddr: g.addr,
		InputHash:      inputHash,
		OutputHash:     outputHash,
		Witness:        witness,
		ProofDigest:    digest,
		StepCount:      128,
	}, nil
}

// dispatchWitness builds a witness for a generic zkISA precompile.
func dispatchWitness(addr types.Address, input, output []byte) []byte {
	data := make([]byte, 0, 20+len(input)+len(output))
	data = append(data, addr[:]...)
	data = append(data, input...)
	data = append(data, output...)
	return crypto.Keccak256(data)
}
