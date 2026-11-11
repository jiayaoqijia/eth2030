// VDF chain and beacon randomness (K+ / M+ roadmap).
//
// Extends the VDF subsystem with:
//   - VDFChain: chains multiple sequential VDF evaluations, where each step's
//     output feeds into the next step's input.
//   - VDFBeacon: uses VDF chains to produce unbiasable, unpredictable randomness
//     for epoch-level beacon duties (proposer selection, committee assignment).
//   - Thread-safe caching of verified chains to avoid redundant re-verification.
//
// This targets CL Cryptography: "VDF, secure prequorum" milestone.
package vdf

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"
)

// Errors for VDF beacon operations.
var (
	errVDFBeaconNilSeed      = errors.New("vdf-beacon: nil seed")
	errVDFBeaconEmptySeed    = errors.New("vdf-beacon: empty seed")
	errVDFBeaconZeroChain    = errors.New("vdf-beacon: zero chain length")
	errVDFBeaconZeroEpoch    = errors.New("vdf-beacon: zero epoch")
	errVDFBeaconChainTooLong = errors.New("vdf-beacon: chain length exceeds maximum")
	errVDFBeaconMismatch     = errors.New("vdf-beacon: chain link output/input mismatch")
)

// MaxChainLength caps chain length to prevent excessive computation.
const MaxChainLength = 256

// ChainedVDFProof holds the result of a chained VDF evaluation. Each link's
// output is the input to the next link, forming a sequential delay chain.
type ChainedVDFProof struct {
	Proofs      []VDFv2Result // individual step results (link 0 .. chainLength-1)
	ChainLength uint64        // number of links in the chain
	Seed        []byte        // original seed (input to the first link)
	FinalOutput []byte        // output of the last link
}

// BeaconOutput holds the randomness produced by a VDF beacon for one epoch.
type BeaconOutput struct {
	Epoch      uint64 // consensus epoch this randomness applies to
	Randomness []byte // 32-byte unbiasable random value
	VDFProof   []byte // compact proof derived from the chain
	Timestamp  uint64 // unix seconds when the beacon was produced
}

// VDFChain evaluates and verifies multi-step VDF chains.
// It wraps a VDFv2 instance and adds caching of verified chain hashes.
type VDFChain struct {
	mu             sync.RWMutex
	vdf            *VDFv2
	itersPerStep   uint64
	verifiedChains map[string]bool // keys bind the complete ordered link proofs
}

// NewVDFChain creates a chain evaluator using the given VDFv2 and per-step
// iteration count. itersPerStep must be >= 1; values < 1 are clamped to 1.
func NewVDFChain(vdf *VDFv2, itersPerStep uint64) *VDFChain {
	if itersPerStep < 1 {
		itersPerStep = 1
	}
	return &VDFChain{
		vdf:            vdf,
		itersPerStep:   itersPerStep,
		verifiedChains: make(map[string]bool),
	}
}

// EvaluateChain runs chainLength sequential VDF evaluations, feeding each
// output into the next step. Returns a ChainedVDFProof capturing all links.
func (c *VDFChain) EvaluateChain(seed []byte, chainLength uint64) (*ChainedVDFProof, error) {
	if seed == nil {
		return nil, errVDFBeaconNilSeed
	}
	if len(seed) == 0 {
		return nil, errVDFBeaconEmptySeed
	}
	if chainLength == 0 {
		return nil, errVDFBeaconZeroChain
	}
	if chainLength > MaxChainLength {
		return nil, errVDFBeaconChainTooLong
	}

	proofs := make([]VDFv2Result, chainLength)
	currentInput := make([]byte, len(seed))
	copy(currentInput, seed)

	for i := uint64(0); i < chainLength; i++ {
		result, err := c.vdf.Evaluate(currentInput, c.itersPerStep)
		if err != nil {
			return nil, err
		}
		proofs[i] = *result
		// Next link's input is this link's output.
		currentInput = make([]byte, len(result.Output))
		copy(currentInput, result.Output)
	}

	finalOutput := make([]byte, len(proofs[chainLength-1].Output))
	copy(finalOutput, proofs[chainLength-1].Output)

	return &ChainedVDFProof{
		Proofs:      proofs,
		ChainLength: chainLength,
		Seed:        copyBytes(seed),
		FinalOutput: finalOutput,
	}, nil
}

// VerifyChain verifies every link in a ChainedVDFProof: checks each individual
// VDF proof and checks that consecutive links are properly chained (output_i == input_{i+1}).
func (c *VDFChain) VerifyChain(proof *ChainedVDFProof) bool {
	if proof == nil || proof.ChainLength == 0 || len(proof.Proofs) == 0 {
		return false
	}
	if uint64(len(proof.Proofs)) != proof.ChainLength {
		return false
	}
	if len(proof.Seed) == 0 || len(proof.FinalOutput) == 0 {
		return false
	}

	// Verify first link's input matches the seed.
	if !bytesEqual(proof.Proofs[0].Input, proof.Seed) {
		return false
	}

	// Verify chaining: output[i] == input[i+1].
	for i := uint64(0); i+1 < proof.ChainLength; i++ {
		if !bytesEqual(proof.Proofs[i].Output, proof.Proofs[i+1].Input) {
			return false
		}
	}

	// Verify final output matches.
	lastIdx := proof.ChainLength - 1
	if !bytesEqual(proof.Proofs[lastIdx].Output, proof.FinalOutput) {
		return false
	}

	links := make([]*VDFv2Result, len(proof.Proofs))
	for i := range proof.Proofs {
		link := &proof.Proofs[i]
		if link.Iterations != c.itersPerStep || len(link.Output) != formSize || len(link.Proof) != formSize {
			return false
		}
		links[i] = link
	}
	cacheKey := string(aggregateDigest(links))
	c.mu.RLock()
	cached := c.verifiedChains[cacheKey]
	c.mu.RUnlock()
	if cached {
		return true
	}
	for _, link := range links {
		if !c.vdf.Verify(link) {
			return false
		}
	}

	// Cache the successful verification.
	c.mu.Lock()
	c.verifiedChains[cacheKey] = true
	c.mu.Unlock()

	return true
}

// ClearCache removes all entries from the verified-chains cache.
func (c *VDFChain) ClearCache() {
	c.mu.Lock()
	c.verifiedChains = make(map[string]bool)
	c.mu.Unlock()
}

// CacheSize returns the number of verified chains in the cache.
func (c *VDFChain) CacheSize() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.verifiedChains)
}

// VDFBeacon uses a VDFChain to produce epoch-level unbiasable randomness.
// The beacon takes a per-epoch seed (typically derived from the RANDAO mix)
// and runs it through a VDF chain whose output cannot be biased by any
// single party because the VDF delay prevents last-revealer manipulation.
type VDFBeacon struct {
	mu    sync.RWMutex
	chain *VDFChain
	// chainLen is the number of VDF steps per beacon evaluation.
	chainLen uint64
	// cache maps epoch -> verified BeaconOutput.
	cache map[uint64]*BeaconOutput
}

// NewVDFBeacon creates a beacon with the given chain evaluator and chain length.
// chainLen must be >= 1; values below are clamped.
func NewVDFBeacon(chain *VDFChain, chainLen uint64) *VDFBeacon {
	if chainLen < 1 {
		chainLen = 1
	}
	if chainLen > MaxChainLength {
		chainLen = MaxChainLength
	}
	return &VDFBeacon{
		chain:    chain,
		chainLen: chainLen,
		cache:    make(map[uint64]*BeaconOutput),
	}
}

// ProduceBeaconRandomness evaluates a VDF chain for the given epoch and seed,
// then derives a 32-byte randomness value from the chain's final output.
func (b *VDFBeacon) ProduceBeaconRandomness(epoch uint64, seed []byte) (*BeaconOutput, error) {
	if epoch == 0 {
		return nil, errVDFBeaconZeroEpoch
	}
	if seed == nil {
		return nil, errVDFBeaconNilSeed
	}
	if len(seed) == 0 {
		return nil, errVDFBeaconEmptySeed
	}

	// Domain-separate the seed with the epoch number.
	domainSeed := beaconDomainSeed(epoch, seed)

	chainProof, err := b.chain.EvaluateChain(domainSeed, b.chainLen)
	if err != nil {
		return nil, err
	}

	// Derive the 32-byte randomness from the final output.
	randomness := keccak256(chainProof.FinalOutput)

	// Build compact proof: hash all individual proofs together.
	compactProof := beaconCompactProof(chainProof)

	now := uint64(time.Now().Unix())

	output := &BeaconOutput{
		Epoch:      epoch,
		Randomness: randomness,
		VDFProof:   compactProof,
		Timestamp:  now,
	}

	// Cache the output for this epoch.
	b.mu.Lock()
	b.cache[epoch] = output
	b.mu.Unlock()

	return output, nil
}

// VerifyBeaconRandomness verifies that a BeaconOutput was correctly derived
// from the given seed for the specified epoch by re-evaluating the VDF chain.
func (b *VDFBeacon) VerifyBeaconRandomness(beacon *BeaconOutput, seed []byte) bool {
	if beacon == nil {
		return false
	}
	if beacon.Epoch == 0 || len(beacon.Randomness) == 0 || len(beacon.VDFProof) == 0 {
		return false
	}
	if seed == nil || len(seed) == 0 {
		return false
	}

	domainSeed := beaconDomainSeed(beacon.Epoch, seed)

	chainProof, err := b.chain.EvaluateChain(domainSeed, b.chainLen)
	if err != nil {
		return false
	}

	// Check randomness matches.
	expectedRandomness := keccak256(chainProof.FinalOutput)
	if !bytesEqual(expectedRandomness, beacon.Randomness) {
		return false
	}

	// Check compact proof matches.
	expectedProof := beaconCompactProof(chainProof)
	return bytesEqual(expectedProof, beacon.VDFProof)
}

// GetCachedBeacon returns a cached BeaconOutput for the given epoch, or nil.
func (b *VDFBeacon) GetCachedBeacon(epoch uint64) *BeaconOutput {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.cache[epoch]
}

// ClearBeaconCache removes all cached beacon outputs.
func (b *VDFBeacon) ClearBeaconCache() {
	b.mu.Lock()
	b.cache = make(map[uint64]*BeaconOutput)
	b.mu.Unlock()
}

// beaconDomainSeed creates a domain-separated seed by hashing the epoch number
// together with the raw seed. This ensures that different epochs always produce
// different VDF chain inputs even with the same underlying RANDAO mix.
func beaconDomainSeed(epoch uint64, seed []byte) []byte {
	var epochBuf [8]byte
	binary.BigEndian.PutUint64(epochBuf[:], epoch)
	return keccak256(epochBuf[:], seed)
}

// beaconCompactProof commits to the complete sequence of VDF results.
func beaconCompactProof(chain *ChainedVDFProof) []byte {
	links := make([]*VDFv2Result, len(chain.Proofs))
	for i := range chain.Proofs {
		links[i] = &chain.Proofs[i]
	}
	return aggregateDigest(links)
}

// suppress unused error variable
var _ = errVDFBeaconMismatch
