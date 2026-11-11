// block_producer.go implements beacon block production for the consensus layer.
// Produces BeaconBlockV2 by assembling attestations, slashings, deposits,
// voluntary exits, sync aggregates, and execution payload headers from pools.
package consensus

import (
	"encoding/binary"
	"errors"
	"sync"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// Block producer constants.
const (
	// GraffitiLength is the fixed length of the graffiti field (32 bytes).
	GraffitiLength = 32

	// MaxProposerSlashings is the maximum proposer slashings per block.
	MaxProposerSlashings = 16

	// MaxAttesterSlashings is the maximum attester slashings per block.
	MaxAttesterSlashings = 2

	// MaxDepositsPerBlock is the maximum deposits per block.
	MaxDepositsPerBlock = 16

	// MaxVoluntaryExits is the maximum voluntary exits per block.
	MaxVoluntaryExits = 16

	// LogsBloomLength is the byte length of the logs bloom filter.
	LogsBloomLength = 256
)

// Block producer errors.
var (
	ErrBPNilState         = errors.New("block_producer: nil beacon state")
	ErrBPSlotRegression   = errors.New("block_producer: slot must be greater than state slot")
	ErrBPInvalidProposer  = errors.New("block_producer: proposer index out of range")
	ErrBPEmptyRandao      = errors.New("block_producer: empty RANDAO reveal")
	ErrBPInvalidBlockSlot = errors.New("block_producer: produced block has invalid slot")
	ErrBPInvalidParent    = errors.New("block_producer: produced block has empty parent root")
	ErrBPInvalidBody      = errors.New("block_producer: produced block has nil body")
)

// ProposerSlashing represents a proposer slashing operation.
type ProposerSlashing struct {
	ProposerIndex    ValidatorIndex
	Header1, Header2 SignedBeaconBlockHeader
}

// SignedBeaconBlockHeader is a signed beacon block header.
type SignedBeaconBlockHeader struct {
	Slot       Slot
	ParentRoot types.Hash
	StateRoot  types.Hash
	BodyRoot   types.Hash
	Signature  [96]byte
}

// AttesterSlashing represents an attester slashing operation.
type AttesterSlashing struct {
	Attestation1, Attestation2 BlockIndexedAttestation
}

// BlockIndexedAttestation is an attestation with attesting validator indices for block inclusion.
type BlockIndexedAttestation struct {
	AttestingIndices []ValidatorIndex
	Data             AttestationData
	Signature        [96]byte
}

// Deposit represents a validator deposit.
type Deposit struct {
	Pubkey                [48]byte
	WithdrawalCredentials [32]byte
	Amount                uint64
	Signature             [96]byte
}

// VoluntaryExit represents a signed voluntary exit.
type VoluntaryExit struct {
	Epoch          Epoch
	ValidatorIndex ValidatorIndex
	Signature      [96]byte
}

// Eth1Data represents an ETH1 deposit data reference for block production.
type Eth1Data struct {
	DepositRoot  types.Hash
	DepositCount uint64
	BlockHash    types.Hash
}

// BeaconBlockBody contains the body of a beacon block, in the Gloas
// (EIP-7732) shape: the execution payload is committed to via a signed
// execution payload bid and PTC payload attestations reference the
// previous slot's payload, replacing the former inline execution payload
// header.
type BeaconBlockBody struct {
	RandaoReveal             [96]byte
	Eth1Data                 Eth1Data
	Graffiti                 [GraffitiLength]byte
	ProposerSlashings        []ProposerSlashing
	AttesterSlashings        []AttesterSlashing
	Attestations             []*PoolAttestation
	Deposits                 []Deposit
	VoluntaryExits           []VoluntaryExit
	SyncAggregate            *SyncAggregate
	SignedExecutionPayloadBid *SignedExecutionPayloadBid
	PayloadAttestations      []*PayloadAttestation
}

// BeaconBlockV2 represents a full beacon block for production.
type BeaconBlockV2 struct {
	Slot          Slot
	ProposerIndex ValidatorIndex
	ParentRoot    types.Hash
	StateRoot     types.Hash
	Body          *BeaconBlockBody
	Signature     [96]byte // placeholder for block signature
}

// BlockRoot2 computes the hash tree root of a BeaconBlockV2.
// Uses Keccak256 over deterministic encoding of block fields.
func BlockRoot2(block *BeaconBlockV2) types.Hash {
	var buf [8 + 8 + 32 + 32]byte
	binary.LittleEndian.PutUint64(buf[0:8], uint64(block.Slot))
	binary.LittleEndian.PutUint64(buf[8:16], uint64(block.ProposerIndex))
	copy(buf[16:48], block.ParentRoot[:])
	copy(buf[48:80], block.StateRoot[:])

	bodyRoot := computeBodyRoot(block.Body)
	return crypto.Keccak256Hash(buf[:], bodyRoot[:])
}

// computeBodyRoot computes a deterministic hash of the block body.
func computeBodyRoot(body *BeaconBlockBody) types.Hash {
	if body == nil {
		return types.Hash{}
	}
	var data []byte
	data = append(data, body.RandaoReveal[:]...)
	data = append(data, body.Eth1Data.DepositRoot[:]...)
	data = append(data, body.Graffiti[:]...)

	// Include attestation count as a signal.
	var countBuf [8]byte
	binary.LittleEndian.PutUint64(countBuf[:], uint64(len(body.Attestations)))
	data = append(data, countBuf[:]...)

	// Include the committed execution payload bid (Gloas/EIP-7732).
	if body.SignedExecutionPayloadBid != nil {
		bidRoot := body.SignedExecutionPayloadBid.Message.HashTreeRoot()
		data = append(data, bidRoot[:]...)
	}

	// Include PTC payload attestation data roots.
	var ptcCountBuf [8]byte
	binary.LittleEndian.PutUint64(ptcCountBuf[:], uint64(len(body.PayloadAttestations)))
	data = append(data, ptcCountBuf[:]...)
	for _, att := range body.PayloadAttestations {
		if att == nil {
			continue
		}
		dataRoot := att.Data.HashTreeRoot()
		data = append(data, dataRoot[:]...)
		data = append(data, att.AggregationBits[:]...)
	}

	return crypto.Keccak256Hash(data)
}

// OperationPools aggregates the pools from which the block producer draws
// operations for block assembly.
type OperationPools struct {
	Attestations        *AttestationPool
	ProposerSlashings   []ProposerSlashing
	AttesterSlashings   []AttesterSlashing
	Deposits            []Deposit
	VoluntaryExits      []VoluntaryExit
	SyncAggregate       *SyncAggregate
	PayloadAttestations []*PayloadAttestation
}

// BlockProducerConfig configures the block producer.
type BlockProducerConfig struct {
	Graffiti            [GraffitiLength]byte
	DefaultFeeRecipient types.Address
	// DefaultBid is the signed execution payload bid to commit to. When nil
	// the producer emits a zero-value self-build bid (BUILDER_INDEX_SELF_BUILD).
	DefaultBid *SignedExecutionPayloadBid
	Eth1Data   Eth1Data
}

// BlockProducer assembles beacon blocks for a given slot. Thread-safe.
type BlockProducer struct {
	mu     sync.Mutex
	config *BlockProducerConfig
	pools  *OperationPools
}

// NewBlockProducer creates a new block producer with the given config and pools.
func NewBlockProducer(cfg *BlockProducerConfig, pools *OperationPools) *BlockProducer {
	if cfg == nil {
		cfg = &BlockProducerConfig{}
	}
	if pools == nil {
		pools = &OperationPools{}
	}
	return &BlockProducer{
		config: cfg,
		pools:  pools,
	}
}

// SetGraffiti updates the graffiti message for future blocks.
func (bp *BlockProducer) SetGraffiti(graffiti [GraffitiLength]byte) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	bp.config.Graffiti = graffiti
}

// SetPools updates the operation pools.
func (bp *BlockProducer) SetPools(pools *OperationPools) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	if pools != nil {
		bp.pools = pools
	}
}

// ProduceBlock assembles a beacon block for the given slot, proposer, and roots.
// It collects pending operations from pools, builds the block body, validates
// the structure, and returns the assembled block.
func (bp *BlockProducer) ProduceBlock(
	slot Slot,
	proposerIndex ValidatorIndex,
	parentRoot types.Hash,
	stateRoot types.Hash,
	randaoReveal [96]byte,
) (*BeaconBlockV2, error) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	// Validate the RANDAO reveal is non-empty.
	emptyReveal := [96]byte{}
	if randaoReveal == emptyReveal {
		return nil, ErrBPEmptyRandao
	}

	// Assemble the block body.
	body := &BeaconBlockBody{
		RandaoReveal: randaoReveal,
		Eth1Data:     bp.config.Eth1Data,
		Graffiti:     bp.config.Graffiti,
	}

	// Collect attestations from pool.
	body.Attestations = bp.collectAttestations(slot)

	// Collect proposer slashings (up to max).
	body.ProposerSlashings = bp.collectProposerSlashings()

	// Collect attester slashings (up to max).
	body.AttesterSlashings = bp.collectAttesterSlashings()

	// Collect deposits (up to max).
	body.Deposits = bp.collectDeposits()

	// Collect voluntary exits (up to max).
	body.VoluntaryExits = bp.collectVoluntaryExits()

	// Include sync aggregate if available.
	if bp.pools.SyncAggregate != nil {
		agg := *bp.pools.SyncAggregate
		body.SyncAggregate = &agg
	} else {
		// Empty sync aggregate (no participation).
		body.SyncAggregate = &SyncAggregate{}
	}

	// Collect PTC payload attestations for the previous slot.
	body.PayloadAttestations = bp.collectPayloadAttestations()

	// Attach the signed execution payload bid. A nil configured bid produces
	// a self-build commitment (zero value, no payment).
	if bp.config.DefaultBid != nil {
		bid := *bp.config.DefaultBid
		body.SignedExecutionPayloadBid = &bid
	} else {
		body.SignedExecutionPayloadBid = &SignedExecutionPayloadBid{
			Message: ExecutionPayloadBid{
				BuilderIndex: BuilderIndexSelfBuild,
				FeeRecipient: bp.config.DefaultFeeRecipient,
				Slot:         uint64(slot),
			},
		}
	}

	block := &BeaconBlockV2{
		Slot:          slot,
		ProposerIndex: proposerIndex,
		ParentRoot:    parentRoot,
		StateRoot:     stateRoot,
		Body:          body,
	}

	// Validate the produced block structure.
	if err := validateBlock(block); err != nil {
		return nil, err
	}

	return block, nil
}

// collectAttestations retrieves pending attestations from the pool.
func (bp *BlockProducer) collectAttestations(slot Slot) []*PoolAttestation {
	if bp.pools.Attestations == nil {
		return nil
	}
	return bp.pools.Attestations.GetForBlock(slot)
}

// collectPayloadAttestations returns up to MaxPayloadAttestations PTC
// payload attestations for inclusion in the block body.
func (bp *BlockProducer) collectPayloadAttestations() []*PayloadAttestation {
	available := bp.pools.PayloadAttestations
	if len(available) == 0 {
		return nil
	}
	count := len(available)
	if count > MaxPayloadAttestations {
		count = MaxPayloadAttestations
	}
	result := make([]*PayloadAttestation, count)
	copy(result, available[:count])
	return result
}

// collectProposerSlashings returns up to MaxProposerSlashings slashings.
func (bp *BlockProducer) collectProposerSlashings() []ProposerSlashing {
	available := bp.pools.ProposerSlashings
	if len(available) == 0 {
		return nil
	}
	count := len(available)
	if count > MaxProposerSlashings {
		count = MaxProposerSlashings
	}
	result := make([]ProposerSlashing, count)
	copy(result, available[:count])
	return result
}

// collectAttesterSlashings returns up to MaxAttesterSlashings slashings.
func (bp *BlockProducer) collectAttesterSlashings() []AttesterSlashing {
	available := bp.pools.AttesterSlashings
	if len(available) == 0 {
		return nil
	}
	count := len(available)
	if count > MaxAttesterSlashings {
		count = MaxAttesterSlashings
	}
	result := make([]AttesterSlashing, count)
	copy(result, available[:count])
	return result
}

// collectDeposits returns up to MaxDepositsPerBlock deposits.
func (bp *BlockProducer) collectDeposits() []Deposit {
	available := bp.pools.Deposits
	if len(available) == 0 {
		return nil
	}
	count := len(available)
	if count > MaxDepositsPerBlock {
		count = MaxDepositsPerBlock
	}
	result := make([]Deposit, count)
	copy(result, available[:count])
	return result
}

// collectVoluntaryExits returns up to MaxVoluntaryExits exits.
func (bp *BlockProducer) collectVoluntaryExits() []VoluntaryExit {
	available := bp.pools.VoluntaryExits
	if len(available) == 0 {
		return nil
	}
	count := len(available)
	if count > MaxVoluntaryExits {
		count = MaxVoluntaryExits
	}
	result := make([]VoluntaryExit, count)
	copy(result, available[:count])
	return result
}

// validateBlock checks the structural validity of a produced block.
func validateBlock(block *BeaconBlockV2) error {
	if block.Slot == 0 {
		return ErrBPInvalidBlockSlot
	}
	emptyHash := types.Hash{}
	if block.ParentRoot == emptyHash {
		return ErrBPInvalidParent
	}
	if block.Body == nil {
		return ErrBPInvalidBody
	}
	return nil
}
