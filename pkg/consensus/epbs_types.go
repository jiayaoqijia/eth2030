// epbs_types.go defines the Gloas (EIP-7732) consensus-layer containers:
// staked builders, execution payload bids, payload envelopes, and payload
// timeliness committee (PTC) attestation types.
//
// Shapes follow refs/consensus-specs/specs/gloas/beacon-chain.md.
package consensus

import (
	"errors"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
)

// ePBS domain types per the Gloas spec.
var (
	// DomainBeaconBuilder is DOMAIN_BEACON_BUILDER = 0x0B000000.
	DomainBeaconBuilder = [4]byte{0x0B, 0x00, 0x00, 0x00}
	// DomainPTCAttester is DOMAIN_PTC_ATTESTER = 0x0C000000.
	DomainPTCAttester = [4]byte{0x0C, 0x00, 0x00, 0x00}
	// DomainProposerPreferences is DOMAIN_PROPOSER_PREFERENCES = 0x0D000000.
	DomainProposerPreferences = [4]byte{0x0D, 0x00, 0x00, 0x00}
)

// ePBS constants per the Gloas spec.
const (
	// PTCSize is PTC_SIZE = 2**9 (512) PTC members per slot.
	PTCSize = 512

	// MaxPayloadAttestations is MAX_PAYLOAD_ATTESTATIONS per beacon block.
	MaxPayloadAttestations = 4

	// BuilderRegistryLimit is BUILDER_REGISTRY_LIMIT = 2**40.
	BuilderRegistryLimit = uint64(1) << 40

	// BuilderPendingWithdrawalsLimit is BUILDER_PENDING_WITHDRAWALS_LIMIT = 2**20.
	BuilderPendingWithdrawalsLimit = uint64(1) << 20

	// BuilderIndexFlag is BUILDER_INDEX_FLAG = 2**40. Validator indices at or
	// above this flag are treated as builder indices.
	BuilderIndexFlag = uint64(1) << 40

	// BuilderIndexSelfBuild is BUILDER_INDEX_SELF_BUILD = UINT64_MAX.
	BuilderIndexSelfBuild = ^uint64(0)

	// BuilderPaymentThresholdNumerator is BUILDER_PAYMENT_THRESHOLD_NUMERATOR = 6.
	BuilderPaymentThresholdNumerator = uint64(6)

	// BuilderPaymentThresholdDenominator is BUILDER_PAYMENT_THRESHOLD_DENOMINATOR = 10.
	BuilderPaymentThresholdDenominator = uint64(10)

	// BuilderWithdrawalPrefix is BUILDER_WITHDRAWAL_PREFIX = 0x03.
	BuilderWithdrawalPrefix = byte(0x03)

	// MinBuilderWithdrawabilityDelay is MIN_BUILDER_WITHDRAWABILITY_DELAY = 64 epochs.
	MinBuilderWithdrawabilityDelay = Epoch(64)

	// MaxBuildersPerWithdrawalsSweep is MAX_BUILDERS_PER_WITHDRAWALS_SWEEP = 2**14.
	MaxBuildersPerWithdrawalsSweep = uint64(1) << 14

	// PayloadTimelyThreshold is PAYLOAD_TIMELY_THRESHOLD = PTC_SIZE // 2.
	PayloadTimelyThreshold = PTCSize / 2

	// DataAvailabilityTimelyThreshold is DATA_AVAILABILITY_TIMELY_THRESHOLD = PTC_SIZE // 2.
	DataAvailabilityTimelyThreshold = PTCSize / 2
)

// Fork-choice payload statuses per the Gloas fork-choice spec.
const (
	PayloadStatusEmpty   uint8 = 0
	PayloadStatusFull    uint8 = 1
	PayloadStatusPending uint8 = 2
)

// ePBS errors.
var (
	ErrEPBSNilState           = errors.New("epbs: nil beacon state")
	ErrEPBSNilBid             = errors.New("epbs: nil execution payload bid")
	ErrEPBSNilBody            = errors.New("epbs: nil beacon block body")
	ErrEPBSNilEnvelope        = errors.New("epbs: nil execution payload envelope")
	ErrEPBSNilAttestation     = errors.New("epbs: nil payload attestation")
	ErrEPBSUnknownBuilder     = errors.New("epbs: builder index not in registry")
	ErrEPBSInactiveBuilder    = errors.New("epbs: builder is not active")
	ErrEPBSInsufficientFunds  = errors.New("epbs: builder cannot cover bid")
	ErrEPBSInvalidBidSig      = errors.New("epbs: invalid bid signature")
	ErrEPBSSelfBuildAmount    = errors.New("epbs: self-build bid must carry zero value")
	ErrEPBSSelfBuildSig       = errors.New("epbs: self-build bid signature must be G2_POINT_AT_INFINITY")
	ErrEPBSBlobLimit          = errors.New("epbs: too many blob KZG commitments in bid")
	ErrEPBSBidSlotMismatch    = errors.New("epbs: bid is not for the current slot")
	ErrEPBSBidParentHash      = errors.New("epbs: bid parent block hash mismatch")
	ErrEPBSBidParentRoot      = errors.New("epbs: bid parent block root mismatch")
	ErrEPBSBidPrevRandao      = errors.New("epbs: bid prev_randao mismatch")
	ErrEPBSAttestationRoot    = errors.New("epbs: payload attestation is not for the parent block")
	ErrEPBSAttestationSlot    = errors.New("epbs: payload attestation is not for the previous slot")
	ErrEPBSAttestationBits    = errors.New("epbs: payload attestation aggregation bits size mismatch")
	ErrEPBSAttestationSig     = errors.New("epbs: invalid payload attestation signature")
	ErrEPBSEnvelopeBuilder    = errors.New("epbs: envelope builder index mismatch")
	ErrEPBSEnvelopePrevRandao = errors.New("epbs: envelope prev_randao mismatch")
	ErrEPBSEnvelopeBlockRoot  = errors.New("epbs: envelope beacon block root mismatch")
	ErrEPBSEnvelopeSlot       = errors.New("epbs: envelope slot mismatch")
	ErrEPBSEnvelopeGasLimit   = errors.New("epbs: envelope gas limit mismatch")
	ErrEPBSEnvelopeBlockHash  = errors.New("epbs: envelope block hash mismatch")
	ErrEPBSEnvelopeParent     = errors.New("epbs: envelope parent hash mismatch")
	ErrEPBSEnvelopeStateRoot  = errors.New("epbs: envelope state root mismatch")
	ErrEPBSNoWithdrawalCred   = errors.New("epbs: withdrawal credentials lack builder prefix")
)

// Builder is a staked in-protocol block builder per the Gloas spec.
type Builder struct {
	Pubkey            [48]byte
	Version           uint8
	ExecutionAddress  [20]byte
	Balance           uint64 // Gwei
	DepositEpoch      Epoch
	WithdrawableEpoch Epoch
}

// BuilderPendingWithdrawal is a builder payment queued for the execution layer.
type BuilderPendingWithdrawal struct {
	FeeRecipient [20]byte
	Amount       uint64 // Gwei
	BuilderIndex uint64
}

// BuilderPendingPayment tracks a builder payment until its payload is
// processed (or the epoch window closes).
type BuilderPendingPayment struct {
	Weight     uint64 // Gwei of PTC/attestation weight
	Withdrawal BuilderPendingWithdrawal
}

// ExecutionPayloadBid is the builder's commitment per the Gloas spec.
type ExecutionPayloadBid struct {
	ParentBlockHash    types.Hash
	ParentBlockRoot    types.Hash
	BlockHash          types.Hash
	PrevRandao         types.Hash
	FeeRecipient       types.Address
	GasLimit           uint64
	BuilderIndex       uint64
	Slot               uint64
	Value              uint64 // payment to the beacon proposer, Gwei
	ExecutionPayment   uint64 // EL-side payment, Gwei
	BlobKZGCommitments [][48]byte
}

// SignedExecutionPayloadBid wraps a bid with the builder's BLS signature.
type SignedExecutionPayloadBid struct {
	Message   ExecutionPayloadBid
	Signature [96]byte
}

// ExecutionPayloadEnvelope is the consensus-side projection of a revealed
// execution payload per the Gloas spec. The full payload stays in the
// execution layer; the consensus state transition needs only the fields
// committed against the bid and the beacon state.
type ExecutionPayloadEnvelope struct {
	BuilderIndex    uint64
	BeaconBlockRoot types.Hash
	Slot            uint64
	StateRoot       types.Hash
	PrevRandao      types.Hash
	BlockHash       types.Hash
	ParentHash      types.Hash
	GasLimit        uint64
}

// SignedExecutionPayloadEnvelope wraps an envelope with the builder's BLS
// signature.
type SignedExecutionPayloadEnvelope struct {
	Message   ExecutionPayloadEnvelope
	Signature [96]byte
}

// PayloadAttestationData is the data attested to by PTC members per the
// Gloas spec (payload presence and blob data availability booleans).
type PayloadAttestationData struct {
	BeaconBlockRoot   types.Hash
	Slot              uint64
	PayloadPresent    bool
	BlobDataAvailable bool
}

// PayloadAttestation is an aggregated PTC attestation.
type PayloadAttestation struct {
	AggregationBits [PTCSize / 8]byte
	Data            PayloadAttestationData
	Signature       [96]byte
}

// PayloadAttestationMessage is a single PTC member's attestation message.
type PayloadAttestationMessage struct {
	ValidatorIndex uint64
	Data           PayloadAttestationData
	Signature      [96]byte
}

// IndexedPayloadAttestation pairs attesting PTC indices with the data and
// aggregate signature, for verification.
type IndexedPayloadAttestation struct {
	AttestingIndices []uint64
	Data             PayloadAttestationData
	Signature        [96]byte
}

// IsBuilderIndex reports whether a validator index is in the builder range
// (BUILDER_INDEX_FLAG set).
func IsBuilderIndex(index uint64) bool {
	return index >= BuilderIndexFlag
}

// ConvertBuilderIndexToValidatorIndex maps a builder index into the
// validator index space by setting the builder flag.
func ConvertBuilderIndexToValidatorIndex(builderIndex uint64) uint64 {
	return builderIndex | BuilderIndexFlag
}

// ConvertValidatorIndexToBuilderIndex maps a validator index carrying the
// builder flag back to a builder index.
func ConvertValidatorIndexToBuilderIndex(index uint64) uint64 {
	return index &^ BuilderIndexFlag
}

// IsBuilderWithdrawalCredential reports whether withdrawal credentials use
// the builder prefix 0x03.
func IsBuilderWithdrawalCredential(credentials [32]byte) bool {
	return credentials[0] == BuilderWithdrawalPrefix
}

// epbsUint64Bytes encodes a uint64 as minimal big-endian bytes, matching the
// epbs EL-side BidHash encoding.
func epbsUint64Bytes(v uint64) []byte {
	if v == 0 {
		return []byte{0x00}
	}
	var buf [8]byte
	n := 0
	for x := v; x > 0; x >>= 8 {
		buf[7-n] = byte(x)
		n++
	}
	return buf[8-n:]
}

// BidHash returns a deterministic hash of the bid message for signing and
// verification. Follows the epbs.EL-side BidHash field order so bids sign
// identically across the CL/EL boundary.
func (bid *ExecutionPayloadBid) BidHash() types.Hash {
	var data []byte
	data = append(data, bid.ParentBlockHash[:]...)
	data = append(data, bid.ParentBlockRoot[:]...)
	data = append(data, bid.BlockHash[:]...)
	data = append(data, bid.PrevRandao[:]...)
	data = append(data, bid.FeeRecipient[:]...)
	data = append(data, epbsUint64Bytes(bid.GasLimit)...)
	data = append(data, epbsUint64Bytes(bid.BuilderIndex)...)
	data = append(data, epbsUint64Bytes(bid.Slot)...)
	data = append(data, epbsUint64Bytes(bid.Value)...)
	return crypto.Keccak256Hash(data)
}

// HashTreeRoot computes the SSZ-style root of the bid message.
func (bid *ExecutionPayloadBid) HashTreeRoot() [32]byte {
	return bid.BidHash()
}

// HashTreeRoot computes the SSZ-style root of the payload attestation data.
func (d *PayloadAttestationData) HashTreeRoot() [32]byte {
	var data []byte
	data = append(data, d.BeaconBlockRoot[:]...)
	data = append(data, epbsUint64Bytes(d.Slot)...)
	if d.PayloadPresent {
		data = append(data, 0x01)
	} else {
		data = append(data, 0x00)
	}
	if d.BlobDataAvailable {
		data = append(data, 0x01)
	} else {
		data = append(data, 0x00)
	}
	return crypto.Keccak256Hash(data)
}
