package stark

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	gethrlp "github.com/ethereum/go-ethereum/rlp"
	"io"
	"math/big"
	"sync"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/proofs"
	"github.com/eth2030/eth2030/txpool/peertick"
)

// STARK mempool aggregation errors.
var (
	ErrAggNotRunning     = errors.New("stark_aggregation: aggregator not running")
	ErrAggAlreadyRunning = errors.New("stark_aggregation: aggregator already running")
	ErrAggNoTransactions = errors.New("stark_aggregation: no validated transactions")
	ErrAggTickFailed     = errors.New("stark_aggregation: tick generation failed")
	ErrAggInvalidTick    = errors.New("stark_aggregation: invalid tick data")
	ErrAggMergeFailed    = errors.New("stark_aggregation: merge failed")
	ErrAggTickTooLarge   = errors.New("stark_aggregation: tick exceeds 128KB bandwidth limit")
)

// Default aggregation parameters.
const (
	DefaultTickInterval = 500 * time.Millisecond
	MaxTickTransactions = 10000
	// MaxTickSize is the maximum serialized size of a mempool tick (128KB per ethresear.ch).
	MaxTickSize = 128 * 1024
)

// ValidatedTx represents a transaction that has been validated with a proof.
type ValidatedTx struct {
	TxHash          types.Hash
	ValidationProof []byte // proof of tx validity
	ValidatedAt     time.Time
	GasUsed         uint64
	RemoteProven    bool // true if proven by a remote peer's STARK tick
}

// MempoolAggregationTick represents a single aggregation cycle result.
type MempoolAggregationTick struct {
	// Timestamp is when this tick was generated.
	Timestamp time.Time
	// ValidTxHashes are the transaction hashes included in this tick.
	ValidTxHashes []types.Hash
	// AggregateProof is the STARK proving all tx validations are valid.
	AggregateProof *proofs.STARKProofData
	// DiscardList contains txs invalidated since the last tick.
	DiscardList []types.Hash
	// TickInterval is the duration between ticks.
	TickInterval time.Duration
	// PeerID identifies the node that generated this tick.
	PeerID string
	// TickNumber is the sequential tick counter.
	TickNumber uint64
	// ValidBitfield is a compact bitfield where bit i indicates tx i is valid.
	ValidBitfield []byte
	// TxMerkleRoot is the Merkle root of the valid transaction hashes.
	TxMerkleRoot types.Hash
}

const tickProofVersion = 1

// The extension follows the original trace commitment field. A legacy
// decoder can still parse the envelope and ignore the extension.
type tickProofWire struct {
	FRICommitments           [][32]byte
	QueryResponses           []proofs.FRIQueryResponse
	TraceLength              uint64
	BlowupFactor             uint8
	NumQueries               uint8
	FieldModulus             *big.Int
	ConstraintCount          uint64
	ConstraintEvalCommitment [32]byte
}

// MarshalBinary encodes a MempoolAggregationTick for P2P transmission.
func (t *MempoolAggregationTick) MarshalBinary() ([]byte, error) {
	if len(t.PeerID) > 65535 || len(t.ValidTxHashes) > MaxTickSize/32 ||
		len(t.DiscardList) > MaxTickSize/32 || len(t.ValidBitfield) > MaxTickSize {
		return nil, ErrAggTickTooLarge
	}
	var buf bytes.Buffer

	// Write tick number (8 bytes).
	if err := binary.Write(&buf, binary.BigEndian, t.TickNumber); err != nil {
		return nil, err
	}

	// Write timestamp (8 bytes as UnixNano).
	if err := binary.Write(&buf, binary.BigEndian, t.Timestamp.UnixNano()); err != nil {
		return nil, err
	}

	// Write peer ID length + peer ID.
	peerIDBytes := []byte(t.PeerID)
	if err := binary.Write(&buf, binary.BigEndian, uint16(len(peerIDBytes))); err != nil {
		return nil, err
	}
	buf.Write(peerIDBytes)

	// Write valid tx count + hashes.
	if err := binary.Write(&buf, binary.BigEndian, uint32(len(t.ValidTxHashes))); err != nil {
		return nil, err
	}
	for _, h := range t.ValidTxHashes {
		buf.Write(h[:])
	}

	// Write discard count + hashes.
	if err := binary.Write(&buf, binary.BigEndian, uint32(len(t.DiscardList))); err != nil {
		return nil, err
	}
	for _, h := range t.DiscardList {
		buf.Write(h[:])
	}

	// Write bitfield.
	if err := binary.Write(&buf, binary.BigEndian, uint32(len(t.ValidBitfield))); err != nil {
		return nil, err
	}
	buf.Write(t.ValidBitfield)

	// Write Merkle root.
	buf.Write(t.TxMerkleRoot[:])

	// Write aggregate proof presence + trace commitment.
	if t.AggregateProof != nil {
		buf.WriteByte(1) // has proof
		proof := t.AggregateProof
		buf.Write(proof.TraceCommitment[:])
		if proof.TraceLength != 0 {
			if proof.ConstraintCount < 0 {
				return nil, ErrAggInvalidTick
			}
			buf.WriteByte(tickProofVersion)
			if err := gethrlp.Encode(&buf, tickProofWire{
				proof.FRICommitments, proof.QueryResponses, proof.TraceLength,
				proof.BlowupFactor, proof.NumQueries, proof.FieldModulus,
				uint64(proof.ConstraintCount), proof.ConstraintEvalCommitment,
			}); err != nil {
				return nil, err
			}
		}
	} else {
		buf.WriteByte(0) // no proof
	}

	if buf.Len() > MaxTickSize {
		return nil, ErrAggTickTooLarge
	}
	return buf.Bytes(), nil
}

// UnmarshalBinary decodes a MempoolAggregationTick from P2P data.
func (t *MempoolAggregationTick) UnmarshalBinary(data []byte) error {
	if len(data) > MaxTickSize {
		return ErrAggTickTooLarge
	}
	if len(data) < 18 { // minimum: tick(8) + timestamp(8) + peerID_len(2)
		return errors.New("stark_aggregation: tick data too short")
	}

	r := bytes.NewReader(data)

	// Read tick number.
	if err := binary.Read(r, binary.BigEndian, &t.TickNumber); err != nil {
		return err
	}

	// Read timestamp.
	var tsNano int64
	if err := binary.Read(r, binary.BigEndian, &tsNano); err != nil {
		return err
	}
	t.Timestamp = time.Unix(0, tsNano)

	// Read peer ID.
	var peerIDLen uint16
	if err := binary.Read(r, binary.BigEndian, &peerIDLen); err != nil {
		return err
	}
	peerIDBytes := make([]byte, peerIDLen)
	if _, err := io.ReadFull(r, peerIDBytes); err != nil {
		return err
	}
	t.PeerID = string(peerIDBytes)

	// Read valid tx hashes.
	var txCount uint32
	if err := binary.Read(r, binary.BigEndian, &txCount); err != nil {
		return err
	}
	if uint64(txCount)*32 > uint64(r.Len()) {
		return io.ErrUnexpectedEOF
	}
	t.ValidTxHashes = make([]types.Hash, txCount)
	for i := uint32(0); i < txCount; i++ {
		if _, err := io.ReadFull(r, t.ValidTxHashes[i][:]); err != nil {
			return err
		}
	}

	// Read discard list.
	var discardCount uint32
	if err := binary.Read(r, binary.BigEndian, &discardCount); err != nil {
		return err
	}
	if uint64(discardCount)*32 > uint64(r.Len()) {
		return io.ErrUnexpectedEOF
	}
	t.DiscardList = make([]types.Hash, discardCount)
	for i := uint32(0); i < discardCount; i++ {
		if _, err := io.ReadFull(r, t.DiscardList[i][:]); err != nil {
			return err
		}
	}

	// Read bitfield.
	var bfLen uint32
	if err := binary.Read(r, binary.BigEndian, &bfLen); err != nil {
		return err
	}
	if uint64(bfLen)*1 > uint64(r.Len()) {
		return io.ErrUnexpectedEOF
	}
	t.ValidBitfield = make([]byte, bfLen)
	if _, err := io.ReadFull(r, t.ValidBitfield); err != nil {
		return err
	}

	// Read Merkle root.
	if _, err := io.ReadFull(r, t.TxMerkleRoot[:]); err != nil {
		return err
	}

	// Read aggregate proof presence.
	var hasProof byte
	if err := binary.Read(r, binary.BigEndian, &hasProof); err != nil {
		return err
	}
	t.AggregateProof = nil
	switch hasProof {
	case 0:
		if r.Len() != 0 {
			return ErrAggInvalidTick
		}
	case 1:
		var proof proofs.STARKProofData
		if _, err := io.ReadFull(r, proof.TraceCommitment[:]); err != nil {
			return err
		}
		if r.Len() != 0 {
			version, err := r.ReadByte()
			if err != nil || version != tickProofVersion {
				return ErrAggInvalidTick
			}
			var wire tickProofWire
			if err := gethrlp.NewStream(r, uint64(r.Len())).Decode(&wire); err != nil || r.Len() != 0 ||
				wire.ConstraintCount > uint64(MaxTickSize) || wire.TraceLength > uint64(MaxTickSize/32) {
				return ErrAggInvalidTick
			}
			proof.FRICommitments = wire.FRICommitments
			proof.QueryResponses = wire.QueryResponses
			proof.TraceLength = wire.TraceLength
			proof.BlowupFactor = wire.BlowupFactor
			proof.NumQueries = wire.NumQueries
			proof.FieldModulus = wire.FieldModulus
			proof.ConstraintCount = int(wire.ConstraintCount)
			proof.ConstraintEvalCommitment = wire.ConstraintEvalCommitment
		}
		t.AggregateProof = &proof
	default:
		return ErrAggInvalidTick
	}

	return nil
}

// P2PBroadcaster broadcasts mempool ticks to network peers.
// Interface avoids circular import between txpool and p2p.
type P2PBroadcaster interface {
	GossipMempoolStarkTick(data []byte) error
}

// STARKAggregator implements Vitalik's recursive STARK mempool proposal.
// Every tick interval (default 500ms), it creates a STARK proving validity
// of all known validated transactions.
type STARKAggregator struct {
	mu           sync.RWMutex
	validTxs     map[types.Hash]*ValidatedTx
	discardList  []types.Hash
	prover       *proofs.STARKProver
	tickInterval time.Duration
	peerID       string
	tickNumber   uint64
	running      bool
	stopCh       chan struct{}
	tickCh       chan *MempoolAggregationTick
	broadcaster  P2PBroadcaster
	peerCache    *peertick.PeerTickCache
}

// NewSTARKAggregator creates a new STARK mempool aggregator.
func NewSTARKAggregator(peerID string) *STARKAggregator {
	return &STARKAggregator{
		validTxs:     make(map[types.Hash]*ValidatedTx),
		prover:       proofs.NewSTARKProver(),
		tickInterval: DefaultTickInterval,
		peerID:       peerID,
		stopCh:       make(chan struct{}),
		tickCh:       make(chan *MempoolAggregationTick, 16),
		peerCache:    peertick.NewPeerTickCache(2),
	}
}

// NewSTARKAggregatorWithInterval creates a new aggregator with a custom tick interval.
func NewSTARKAggregatorWithInterval(peerID string, interval time.Duration) *STARKAggregator {
	agg := NewSTARKAggregator(peerID)
	if interval > 0 {
		agg.tickInterval = interval
	}
	return agg
}

// Start begins the periodic aggregation tick loop.
func (sa *STARKAggregator) Start() error {
	sa.mu.Lock()
	if sa.running {
		sa.mu.Unlock()
		return ErrAggAlreadyRunning
	}
	sa.running = true
	sa.mu.Unlock()

	go sa.tickLoop()
	return nil
}

// Stop halts the aggregation tick loop.
func (sa *STARKAggregator) Stop() {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	if !sa.running {
		return
	}
	sa.running = false
	close(sa.stopCh)
}

// IsRunning returns whether the aggregator is currently running.
func (sa *STARKAggregator) IsRunning() bool {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.running
}

// TickChannel returns the channel that receives completed aggregation ticks.
func (sa *STARKAggregator) TickChannel() <-chan *MempoolAggregationTick {
	return sa.tickCh
}

// AddValidatedTx adds a validated transaction to the aggregation set.
func (sa *STARKAggregator) AddValidatedTx(txHash types.Hash, validationProof []byte, gasUsed uint64) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	sa.validTxs[txHash] = &ValidatedTx{
		TxHash:          txHash,
		ValidationProof: append([]byte(nil), validationProof...),
		ValidatedAt:     time.Now(),
		GasUsed:         gasUsed,
	}
}

// RemoveTx removes a transaction from the aggregation set and adds it to the discard list.
func (sa *STARKAggregator) RemoveTx(txHash types.Hash) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	if _, exists := sa.validTxs[txHash]; exists {
		delete(sa.validTxs, txHash)
		sa.discardList = append(sa.discardList, txHash)
	}
}

// PendingCount returns the number of validated transactions pending aggregation.
func (sa *STARKAggregator) PendingCount() int {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return len(sa.validTxs)
}

// GenerateTick creates an aggregate STARK proof for the current validated tx set.
func (sa *STARKAggregator) GenerateTick() (*MempoolAggregationTick, error) {
	sa.mu.Lock()
	defer sa.mu.Unlock()

	if len(sa.validTxs) == 0 {
		return nil, ErrAggNoTransactions
	}

	// Collect tx hashes and build execution trace.
	txHashes := make([]types.Hash, 0, len(sa.validTxs))
	trace := make([][]proofs.FieldElement, 0, len(sa.validTxs))

	for hash, vtx := range sa.validTxs {
		txHashes = append(txHashes, hash)
		// Each tx becomes a trace row: [hash_hi, hash_lo, gas_used]
		hi := new(big.Int).SetBytes(hash[:16])
		lo := new(big.Int).SetBytes(hash[16:])
		trace = append(trace, []proofs.FieldElement{
			{Value: hi},
			{Value: lo},
			proofs.NewFieldElement(int64(vtx.GasUsed)),
		})
	}

	// Constraint 1 (hash consistency): sums hash_hi + hash_lo (non-zero for real tx hashes).
	// Constraint 2 (gas bounds): extracts gas_used column.
	constraints := []proofs.STARKConstraint{
		{Degree: 1, Coefficients: []proofs.FieldElement{proofs.NewFieldElement(1), proofs.NewFieldElement(1)}},
		{Degree: 1, Coefficients: []proofs.FieldElement{proofs.NewFieldElement(0), proofs.NewFieldElement(0), proofs.NewFieldElement(1)}},
	}

	starkProof, err := sa.prover.GenerateSTARKProof(trace, constraints)
	if err != nil {
		return nil, ErrAggTickFailed
	}

	// Build bitfield: all transactions in the tick are valid, so all bits are set.
	bitfieldLen := (len(txHashes) + 7) / 8
	bitfield := make([]byte, bitfieldLen)
	for i := range txHashes {
		bitfield[i/8] |= 1 << uint(i%8)
	}

	// Compute Merkle root of valid tx hashes.
	txMerkleRoot := computeTxMerkleRoot(txHashes)

	// Capture and reset discard list.
	discards := sa.discardList
	sa.discardList = nil
	sa.tickNumber++

	return &MempoolAggregationTick{
		Timestamp:      time.Now(),
		ValidTxHashes:  txHashes,
		AggregateProof: starkProof,
		DiscardList:    discards,
		TickInterval:   sa.tickInterval,
		PeerID:         sa.peerID,
		TickNumber:     sa.tickNumber,
		ValidBitfield:  bitfield,
		TxMerkleRoot:   txMerkleRoot,
	}, nil
}

// MergeTick merges a remote peer's aggregation tick into the local state.
func (sa *STARKAggregator) MergeTick(remote *MempoolAggregationTick) error {
	if remote == nil {
		return ErrAggInvalidTick
	}
	if remote.AggregateProof == nil {
		return ErrAggInvalidTick
	}

	// Check actual serialized tick size against bandwidth limit.
	serialized, err := remote.MarshalBinary()
	if err != nil {
		return err
	}
	if len(serialized) > MaxTickSize {
		return ErrAggTickTooLarge
	}

	proof := remote.AggregateProof
	if len(remote.ValidTxHashes) == 0 || proof.TraceLength != uint64(len(remote.ValidTxHashes)) ||
		proof.NumQueries == 0 || len(proof.QueryResponses) != int(proof.NumQueries) ||
		remote.TxMerkleRoot != computeTxMerkleRoot(remote.ValidTxHashes) {
		return ErrAggMergeFailed
	}
	valid, err := sa.prover.VerifySTARKProof(proof, nil)
	if err != nil || !valid {
		return ErrAggMergeFailed
	}

	// The current trace proves no execution statement about the hashes.
	// A peer tick cannot add or discard entries in the local validated set.

	return nil
}

// SetBroadcaster sets the P2P broadcaster for gossipping ticks to peers.
func (sa *STARKAggregator) SetBroadcaster(b P2PBroadcaster) {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	sa.broadcaster = b
}

// BroadcastTick broadcasts a tick to peers via the configured P2PBroadcaster.
// Ticks that exceed MaxTickSize are silently dropped.
func (sa *STARKAggregator) BroadcastTick(tick *MempoolAggregationTick) {
	sa.mu.RLock()
	b := sa.broadcaster
	sa.mu.RUnlock()

	if b == nil {
		return
	}
	data, err := tick.MarshalBinary()
	if err != nil {
		return
	}
	if len(data) > MaxTickSize {
		return
	}
	_ = b.GossipMempoolStarkTick(data)
}

// MergeTickAtSlot merges a remote tick and marks its txs in the peer cache.
func (sa *STARKAggregator) MergeTickAtSlot(remote *MempoolAggregationTick, slot uint64) error {
	if err := sa.MergeTick(remote); err != nil {
		return err
	}
	sa.peerCache.AdvanceSlot(slot)
	return nil
}

// PeerCache returns the peer tick cache.
func (sa *STARKAggregator) PeerCache() *peertick.PeerTickCache {
	return sa.peerCache
}

// TickHash computes a deterministic hash of a tick for comparison.
func TickHash(tick *MempoolAggregationTick) types.Hash {
	h := sha256.New()
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], tick.TickNumber)
	h.Write(buf[:])
	for _, txHash := range tick.ValidTxHashes {
		h.Write(txHash[:])
	}
	if tick.AggregateProof != nil {
		h.Write(tick.AggregateProof.TraceCommitment[:])
	}
	var result types.Hash
	copy(result[:], h.Sum(nil))
	return result
}

// computeTxMerkleRoot computes a simple binary Merkle root of transaction hashes.
func computeTxMerkleRoot(hashes []types.Hash) types.Hash {
	if len(hashes) == 0 {
		return types.Hash{}
	}
	if len(hashes) == 1 {
		return hashes[0]
	}

	// Simple binary Merkle tree using SHA-256.
	layer := make([]types.Hash, len(hashes))
	copy(layer, hashes)

	for len(layer) > 1 {
		var next []types.Hash
		for i := 0; i < len(layer); i += 2 {
			h := sha256.New()
			h.Write(layer[i][:])
			if i+1 < len(layer) {
				h.Write(layer[i+1][:])
			} else {
				h.Write(layer[i][:]) // duplicate last if odd
			}
			var hash types.Hash
			copy(hash[:], h.Sum(nil))
			next = append(next, hash)
		}
		layer = next
	}
	return layer[0]
}

// tickLoop runs the periodic aggregation.
func (sa *STARKAggregator) tickLoop() {
	ticker := time.NewTicker(sa.tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-sa.stopCh:
			return
		case <-ticker.C:
			tick, err := sa.GenerateTick()
			if err != nil {
				continue // skip empty ticks
			}
			select {
			case sa.tickCh <- tick:
			default:
				// channel full, drop oldest
			}
			sa.BroadcastTick(tick)
		}
	}
}
