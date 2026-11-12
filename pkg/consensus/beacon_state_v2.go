// beacon_state_v2.go implements a modernized beacon state following the phase0
// spec. Includes validator management, epoch processing, proposer index
// computation, and SSZ hash tree root. Thread-safe.
package consensus

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math"
	gosync "sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/ssz"
)

// Spec constants.
const (
	SlotsPerHistoricalRoot    = 8192
	EpochsPerHistoricalVector = 65536
	EpochsPerSlashingsVector  = 8192
	HistoricalRootsLimitV2    = 16777216
	ValidatorRegistryLimit    = 1 << 40
	JustificationBitsLengthV2 = 4
	BaseRewardFactor          = 64
	BaseRewardsPerEpoch       = 4
	MinPerEpochChurnLimit     = 4
	ChurnLimitQuotient        = 65536
	ProportionalSlashingMul   = 1
	MaxSeedLookahead          = 4
	MinValidatorWithdrawDelay = 256
	EjectionBalance           = 16_000_000_000
	ShuffleRoundCount         = 90
	MaxEffectiveBalanceV2     = 32_000_000_000
)

var (
	ErrV2NoValidators    = errors.New("beacon_state_v2: no active validators")
	ErrV2IndexOutOfRange = errors.New("beacon_state_v2: validator index out of range")
)

// ForkV2 represents a beacon chain fork version pair.
type ForkV2 struct {
	PreviousVersion, CurrentVersion [4]byte
	Epoch                           Epoch
}

// Eth1DataV2 represents ETH1 deposit data reference.
type Eth1DataV2 struct {
	DepositRoot, BlockHash [32]byte
	DepositCount           uint64
}

// BeaconBlockHeaderV2 represents a beacon block header.
type BeaconBlockHeaderV2 struct {
	Slot, ProposerIndex             uint64
	ParentRoot, StateRoot, BodyRoot [32]byte
}

// ValidatorV2 holds the full spec validator fields from phase0.
type ValidatorV2 struct {
	Pubkey                                                                    [48]byte
	WithdrawalCredentials                                                     [32]byte
	EffectiveBalance                                                          uint64
	Slashed                                                                   bool
	ActivationEligibilityEpoch, ActivationEpoch, ExitEpoch, WithdrawableEpoch Epoch
}

func (v *ValidatorV2) IsActiveV2(epoch Epoch) bool {
	return v.ActivationEpoch <= epoch && epoch < v.ExitEpoch
}
func (v *ValidatorV2) IsSlashableV2(epoch Epoch) bool {
	return !v.Slashed && v.ActivationEpoch <= epoch && epoch < v.WithdrawableEpoch
}

// HashTreeRoot computes the SSZ hash tree root of a ValidatorV2.
func (v *ValidatorV2) HashTreeRoot() [32]byte {
	var pkC [2][32]byte
	copy(pkC[0][:], v.Pubkey[:32])
	copy(pkC[1][:16], v.Pubkey[32:])
	return ssz.HashTreeRootContainer([][32]byte{
		ssz.Merkleize(pkC[:], 0), v.WithdrawalCredentials,
		ssz.HashTreeRootUint64(v.EffectiveBalance), ssz.HashTreeRootBool(v.Slashed),
		ssz.HashTreeRootUint64(uint64(v.ActivationEligibilityEpoch)),
		ssz.HashTreeRootUint64(uint64(v.ActivationEpoch)),
		ssz.HashTreeRootUint64(uint64(v.ExitEpoch)),
		ssz.HashTreeRootUint64(uint64(v.WithdrawableEpoch)),
	})
}

// CheckpointV2 is a finality checkpoint.
type CheckpointV2 struct {
	Epoch Epoch
	Root  [32]byte
}

// BeaconStateV2 is a comprehensive beacon state implementing the phase0 spec.
type BeaconStateV2 struct {
	mu                          gosync.RWMutex
	GenesisTime                 uint64
	GenesisValidatorsRoot       [32]byte
	Slot                        uint64
	Fork                        ForkV2
	LatestBlockHeader           BeaconBlockHeaderV2
	BlockRoots                  [SlotsPerHistoricalRoot][32]byte
	StateRoots                  [SlotsPerHistoricalRoot][32]byte
	HistoricalRoots             [][32]byte
	Eth1Data                    Eth1DataV2
	Validators                  []*ValidatorV2
	Balances                    []uint64
	RandaoMixes                 [EpochsPerHistoricalVector][32]byte
	Slashings                   [EpochsPerSlashingsVector]uint64
	JustificationBitsV2         [JustificationBitsLengthV2]bool
	PreviousJustifiedCheckpoint CheckpointV2
	CurrentJustifiedCheckpoint  CheckpointV2
	FinalizedCheckpoint         CheckpointV2
	SlotsPerEpoch               uint64
	// ePBS (Gloas/EIP-7732) state. Zero values are safe: lazy initializers
	// in the epbs accessors size the lists on first use.
	Builders                     []*Builder
	NextWithdrawalBuilderIndex   uint64
	ExecutionPayloadAvailability []bool
	BuilderPendingPayments       []BuilderPendingPayment
	BuilderPendingWithdrawals    []BuilderPendingWithdrawal
	LatestBlockHash              types.Hash
	LatestExecutionPayloadBid    *ExecutionPayloadBid
	PayloadExpectedWithdrawals   []ExpectedWithdrawal
}

// NewBeaconStateV2 creates a new empty BeaconStateV2.
func NewBeaconStateV2(spe uint64) *BeaconStateV2 {
	if spe == 0 {
		spe = 32
	}
	return &BeaconStateV2{Validators: make([]*ValidatorV2, 0), Balances: make([]uint64, 0),
		HistoricalRoots: make([][32]byte, 0), SlotsPerEpoch: spe,
		ExecutionPayloadAvailability: make([]bool, SlotsPerHistoricalRoot),
		BuilderPendingPayments:       make([]BuilderPendingPayment, 2*spe)}
}

func (s *BeaconStateV2) GetCurrentEpoch() Epoch { return Epoch(s.Slot / s.SlotsPerEpoch) }
func (s *BeaconStateV2) GetPreviousEpoch() Epoch {
	if c := s.GetCurrentEpoch(); c > 0 {
		return c - 1
	}
	return 0
}

// AddValidatorV2 appends a validator and returns its index. Thread-safe.
func (s *BeaconStateV2) AddValidatorV2(v *ValidatorV2, balance uint64) uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := uint64(len(s.Validators))
	s.Validators = append(s.Validators, v)
	s.Balances = append(s.Balances, balance)
	return idx
}

// GetActiveValidatorIndices returns active validator indices. Thread-safe.
func (s *BeaconStateV2) GetActiveValidatorIndices(epoch Epoch) []uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeIndices(epoch)
}
func (s *BeaconStateV2) activeIndices(epoch Epoch) []uint64 {
	var out []uint64
	for i, v := range s.Validators {
		if v.IsActiveV2(epoch) {
			out = append(out, uint64(i))
		}
	}
	return out
}

// GetTotalActiveBalance returns total effective balance (min 1 ETH). Thread-safe.
func (s *BeaconStateV2) GetTotalActiveBalance(epoch Epoch) uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalBal(epoch)
}
func (s *BeaconStateV2) totalBal(epoch Epoch) uint64 {
	var t uint64
	for _, v := range s.Validators {
		if v.IsActiveV2(epoch) {
			t += v.EffectiveBalance
		}
	}
	if t < EffectiveBalanceIncrement {
		return EffectiveBalanceIncrement
	}
	return t
}

func integerSquareRoot(n uint64) uint64 {
	if n == math.MaxUint64 {
		return 4294967295
	}
	x, y := n, (n+1)/2
	for y < x {
		x = y
		y = (x + n/x) / 2
	}
	return x
}

func (s *BeaconStateV2) getBaseReward(idx uint64) uint64 {
	sq := integerSquareRoot(s.totalBal(s.GetCurrentEpoch()))
	if sq == 0 {
		return 0
	}
	return s.Validators[idx].EffectiveBalance * BaseRewardFactor / sq / BaseRewardsPerEpoch
}

// GetBeaconProposerIndex computes the proposer for the current slot. Thread-safe.
func (s *BeaconStateV2) GetBeaconProposerIndex() (uint64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.proposerIdx()
}
func (s *BeaconStateV2) proposerIdx() (uint64, error) {
	indices := s.activeIndices(s.GetCurrentEpoch())
	if len(indices) == 0 {
		return 0, ErrV2NoValidators
	}
	mix := s.RandaoMixes[uint64(s.GetCurrentEpoch())%EpochsPerHistoricalVector]
	var buf [40]byte
	copy(buf[:32], mix[:])
	binary.LittleEndian.PutUint64(buf[32:], s.Slot)
	seed := sha256.Sum256(buf[:])
	total := uint64(len(indices))
	for i := uint64(0); i < total*100; i++ {
		cand := indices[computeShuffledIndex(i%total, total, seed)]
		copy(buf[:32], seed[:])
		binary.LittleEndian.PutUint64(buf[32:], i/32)
		rh := sha256.Sum256(buf[:])
		if s.Validators[cand].EffectiveBalance*255 >= MaxEffectiveBalanceV2*uint64(rh[i%32]) {
			return cand, nil
		}
	}
	return indices[0], nil
}

func computeShuffledIndex(index, count uint64, seed [32]byte) uint64 {
	if count <= 1 {
		return index
	}
	for r := uint64(0); r < ShuffleRoundCount; r++ {
		var pi [33]byte
		copy(pi[:32], seed[:])
		pi[32] = byte(r)
		ph := sha256.Sum256(pi[:])
		pivot := binary.LittleEndian.Uint64(ph[:8]) % count
		flip := (pivot + count - index) % count
		pos := flip
		if index > flip {
			pos = index
		}
		var si [37]byte
		copy(si[:32], seed[:])
		si[32] = byte(r)
		binary.LittleEndian.PutUint32(si[33:], uint32(pos/256))
		src := sha256.Sum256(si[:])
		if (src[(pos%256)/8]>>(pos%8))&1 != 0 {
			index = flip
		}
	}
	return index
}

// ProcessEpoch runs the epoch transition. Thread-safe.
func (s *BeaconStateV2) ProcessEpoch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.processJustFinalize()
	s.processRewards()
	s.processRegistry()
	s.processSlash()
	s.processEffBal()
	ne := s.GetCurrentEpoch() + 1
	s.Slashings[uint64(ne)%EpochsPerSlashingsVector] = 0
	s.RandaoMixes[uint64(ne)%EpochsPerHistoricalVector] =
		s.RandaoMixes[uint64(s.GetCurrentEpoch())%EpochsPerHistoricalVector]
}

func (s *BeaconStateV2) processJustFinalize() {
	ce := s.GetCurrentEpoch()
	if ce <= 1 {
		return
	}
	pe := s.GetPreviousEpoch()
	oldPJ, oldCJ := s.PreviousJustifiedCheckpoint, s.CurrentJustifiedCheckpoint
	s.PreviousJustifiedCheckpoint = s.CurrentJustifiedCheckpoint
	for i := JustificationBitsLengthV2 - 1; i > 0; i-- {
		s.JustificationBitsV2[i] = s.JustificationBitsV2[i-1]
	}
	s.JustificationBitsV2[0] = false
	tab := s.totalBal(ce)
	if s.totalBal(pe)*3 >= tab*2 {
		s.CurrentJustifiedCheckpoint = CheckpointV2{pe,
			s.BlockRoots[uint64(EpochStartSlot(pe, s.SlotsPerEpoch))%SlotsPerHistoricalRoot]}
		s.JustificationBitsV2[1] = true
	}
	if s.totalBal(ce)*3 >= tab*2 {
		s.CurrentJustifiedCheckpoint = CheckpointV2{ce,
			s.BlockRoots[uint64(EpochStartSlot(ce, s.SlotsPerEpoch))%SlotsPerHistoricalRoot]}
		s.JustificationBitsV2[0] = true
	}
	b := s.JustificationBitsV2
	if b[1] && b[2] && b[3] && oldPJ.Epoch+3 == ce {
		s.FinalizedCheckpoint = oldPJ
	}
	if b[1] && b[2] && oldPJ.Epoch+2 == ce {
		s.FinalizedCheckpoint = oldPJ
	}
	if b[0] && b[1] && b[2] && oldCJ.Epoch+2 == ce {
		s.FinalizedCheckpoint = oldCJ
	}
	if b[0] && b[1] && oldCJ.Epoch+1 == ce {
		s.FinalizedCheckpoint = oldCJ
	}
}

func (s *BeaconStateV2) processRewards() {
	if s.GetCurrentEpoch() == 0 {
		return
	}
	for i := range s.Validators {
		if !s.Validators[i].IsActiveV2(s.GetPreviousEpoch()) {
			continue
		}
		r := s.getBaseReward(uint64(i))
		if s.Validators[i].Slashed {
			if s.Balances[i] >= r {
				s.Balances[i] -= r
			} else {
				s.Balances[i] = 0
			}
		} else {
			s.Balances[i] += r
		}
	}
}

func (s *BeaconStateV2) processRegistry() {
	ce := s.GetCurrentEpoch()
	for i, v := range s.Validators {
		if v.ActivationEligibilityEpoch == FarFutureEpoch && v.EffectiveBalance >= MaxEffectiveBalanceV2 {
			s.Validators[i].ActivationEligibilityEpoch = ce + 1
		}
		if v.IsActiveV2(ce) && v.EffectiveBalance <= EjectionBalance && v.ExitEpoch == FarFutureEpoch {
			ex := ce + 1 + Epoch(MaxSeedLookahead)
			s.Validators[i].ExitEpoch = ex
			s.Validators[i].WithdrawableEpoch = ex + MinValidatorWithdrawDelay
		}
	}
	churn := uint64(len(s.activeIndices(ce))) / ChurnLimitQuotient
	if churn < MinPerEpochChurnLimit {
		churn = MinPerEpochChurnLimit
	}
	var n uint64
	for i, v := range s.Validators {
		if n >= churn {
			break
		}
		if v.ActivationEligibilityEpoch <= s.FinalizedCheckpoint.Epoch && v.ActivationEpoch == FarFutureEpoch {
			s.Validators[i].ActivationEpoch = ce + 1 + Epoch(MaxSeedLookahead)
			n++
		}
	}
}

func (s *BeaconStateV2) processSlash() {
	ep, tb := s.GetCurrentEpoch(), s.totalBal(s.GetCurrentEpoch())
	var ts uint64
	for _, sl := range s.Slashings {
		ts += sl
	}
	if adj := ts * ProportionalSlashingMul; adj > tb {
		ts = tb
	} else {
		ts = adj
	}
	for i, v := range s.Validators {
		if v.Slashed && ep+EpochsPerSlashingsVector/2 == v.WithdrawableEpoch {
			pen := v.EffectiveBalance / EffectiveBalanceIncrement * ts / tb * EffectiveBalanceIncrement
			if s.Balances[i] >= pen {
				s.Balances[i] -= pen
			} else {
				s.Balances[i] = 0
			}
		}
	}
}

func (s *BeaconStateV2) processEffBal() {
	for i, v := range s.Validators {
		if i >= len(s.Balances) {
			break
		}
		eb := ComputeEffectiveBalance(s.Balances[i], v.EffectiveBalance)
		if eb > MaxEffectiveBalanceV2 {
			eb = MaxEffectiveBalanceV2
		}
		s.Validators[i].EffectiveBalance = eb
	}
}

// IncreaseBalance / DecreaseBalance manage validator balances. Thread-safe.
func (s *BeaconStateV2) IncreaseBalance(index, delta uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if int(index) >= len(s.Balances) {
		return ErrV2IndexOutOfRange
	}
	s.Balances[index] += delta
	return nil
}
func (s *BeaconStateV2) DecreaseBalance(index, delta uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if int(index) >= len(s.Balances) {
		return ErrV2IndexOutOfRange
	}
	if delta > s.Balances[index] {
		s.Balances[index] = 0
	} else {
		s.Balances[index] -= delta
	}
	return nil
}

// ValidatorCount returns the total number of validators. Thread-safe.
func (s *BeaconStateV2) ValidatorCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.Validators)
}

// HashTreeRootV2 computes the SSZ hash tree root of the beacon state. Thread-safe.
func (s *BeaconStateV2) HashTreeRootV2() types.Hash {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cHTR := func(fields ...[32]byte) [32]byte { return ssz.HashTreeRootContainer(fields) }
	cpH := func(cp CheckpointV2) [32]byte {
		return cHTR(ssz.HashTreeRootUint64(uint64(cp.Epoch)), cp.Root)
	}
	f := make([][32]byte, 21)
	f[0] = ssz.HashTreeRootUint64(s.GenesisTime)
	f[1] = s.GenesisValidatorsRoot
	f[2] = ssz.HashTreeRootUint64(s.Slot)
	f[3] = cHTR(ssz.HashTreeRootBasicVector(s.Fork.PreviousVersion[:]),
		ssz.HashTreeRootBasicVector(s.Fork.CurrentVersion[:]),
		ssz.HashTreeRootUint64(uint64(s.Fork.Epoch)))
	f[4] = cHTR(ssz.HashTreeRootUint64(s.LatestBlockHeader.Slot),
		ssz.HashTreeRootUint64(s.LatestBlockHeader.ProposerIndex),
		s.LatestBlockHeader.ParentRoot, s.LatestBlockHeader.StateRoot,
		s.LatestBlockHeader.BodyRoot)
	f[5] = ssz.HashTreeRootVector(s.BlockRoots[:])
	f[6] = ssz.HashTreeRootVector(s.StateRoots[:])
	f[7] = virtualHTRList(s.HistoricalRoots, HistoricalRootsLimitV2)
	f[8] = cHTR(s.Eth1Data.DepositRoot,
		ssz.HashTreeRootUint64(s.Eth1Data.DepositCount), s.Eth1Data.BlockHash)
	f[9] = virtualHTRList(nil, 2048)  // eth1_data_votes placeholder
	f[10] = ssz.HashTreeRootUint64(0) // eth1_deposit_index
	vr := make([][32]byte, len(s.Validators))
	for i, v := range s.Validators {
		vr[i] = v.HashTreeRoot()
	}
	f[11] = virtualHTRList(vr, ValidatorRegistryLimit)
	bs := make([]byte, len(s.Balances)*8)
	for i, b := range s.Balances {
		binary.LittleEndian.PutUint64(bs[i*8:], b)
	}
	f[12] = virtualHTRBasicList(bs, len(s.Balances), 8, ValidatorRegistryLimit)
	f[13] = ssz.HashTreeRootVector(s.RandaoMixes[:])
	ss := make([]byte, EpochsPerSlashingsVector*8)
	for i, sl := range s.Slashings {
		binary.LittleEndian.PutUint64(ss[i*8:], sl)
	}
	f[14] = ssz.HashTreeRootBasicVector(ss)
	f[15] = virtualHTRList(nil, 4096) // previous_epoch_attestations
	f[16] = virtualHTRList(nil, 4096) // current_epoch_attestations
	f[17] = ssz.HashTreeRootBitvector(s.JustificationBitsV2[:])
	f[18] = cpH(s.PreviousJustifiedCheckpoint)
	f[19] = cpH(s.CurrentJustifiedCheckpoint)
	f[20] = cpH(s.FinalizedCheckpoint)
	var h types.Hash
	r := ssz.HashTreeRootContainer(f)
	copy(h[:], r[:])
	return h
}

// virtualHTRList merkleizes a list with large max capacity without
// materializing the full tree.
func virtualHTRList(roots [][32]byte, maxLen uint64) [32]byte {
	depth, p := uint64(0), uint64(1)
	for n := maxLen; p < n; {
		p <<= 1
		depth++
	}
	if maxLen == 0 {
		depth = 0
	}
	zh := make([][32]byte, depth+1)
	for i := uint64(1); i <= depth; i++ {
		zh[i] = sha256Pair(zh[i-1], zh[i-1])
	}
	return ssz.MixInLength(merkVirtual(roots, depth, zh), uint64(len(roots)))
}

func virtualHTRBasicList(ser []byte, count, elemSz int, maxLen uint64) [32]byte {
	chunks := ssz.Pack(ser)
	mc := (maxLen*uint64(elemSz) + 31) / 32
	depth, p := uint64(0), uint64(1)
	for n := mc; p < n; {
		p <<= 1
		depth++
	}
	if mc == 0 {
		depth = 0
	}
	zh := make([][32]byte, depth+1)
	for i := uint64(1); i <= depth; i++ {
		zh[i] = sha256Pair(zh[i-1], zh[i-1])
	}
	return ssz.MixInLength(merkVirtual(chunks, depth, zh), uint64(count))
}

func merkVirtual(chunks [][32]byte, depth uint64, zh [][32]byte) [32]byte {
	if depth == 0 {
		if len(chunks) > 0 {
			return chunks[0]
		}
		return zh[0]
	}
	if len(chunks) == 0 {
		return zh[depth]
	}
	layer := make([][32]byte, len(chunks))
	copy(layer, chunks)
	for d := uint64(0); d < depth; d++ {
		ns := (len(layer) + 1) / 2
		nl := make([][32]byte, ns)
		for i := 0; i < ns; i++ {
			r := zh[d]
			if 2*i+1 < len(layer) {
				r = layer[2*i+1]
			}
			nl[i] = sha256Pair(layer[2*i], r)
		}
		layer = nl
	}
	return layer[0]
}

func sha256Pair(a, b [32]byte) [32]byte {
	var c [64]byte
	copy(c[:32], a[:])
	copy(c[32:], b[:])
	return sha256.Sum256(c[:])
}
