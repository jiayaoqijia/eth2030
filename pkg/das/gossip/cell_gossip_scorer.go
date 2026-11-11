// cell_gossip_scorer.go implements gossip scoring for PeerDAS data column
// subnets, DataColumnSidecar creation from block data, custody column
// verification at the gossip layer, and data column reconstruction triggers.
//
// Gossip scoring follows the GossipSub v1.1 topic scoring model adapted for
// DAS subnets: each subnet topic has its own score parameters, and peers are
// penalized for invalid messages, late deliveries, or failing custody duties.
// Reconstruction triggers monitor received columns and initiate recovery when
// enough columns are available.
package gossip

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/eth2030/eth2030/das/cell"
	"github.com/eth2030/eth2030/das/dastypes"
	"github.com/eth2030/eth2030/das/sampling"
	"golang.org/x/crypto/sha3"
)

// Gossip scorer errors.
var (
	ErrGossipScoreNilPeer      = errors.New("das/gossip: nil peer ID")
	ErrGossipScorePeerNotFound = errors.New("das/gossip: peer not found")
	ErrSidecarBuildNoCells     = errors.New("das/gossip: no cells provided for sidecar")
	ErrSidecarBuildMismatch    = errors.New("das/gossip: cells/commitments/proofs length mismatch")
	ErrReconstructNotNeeded    = errors.New("das/gossip: reconstruction not needed")
	ErrColumnAlreadyReceived   = errors.New("das/gossip: column already received")
)

// GossipScoreConfig configures the gossip scoring parameters per subnet topic.
type GossipScoreConfig struct {
	// MaxScore is the highest achievable score for a peer.
	MaxScore float64

	// MinScore is the threshold below which a peer is considered misbehaving.
	MinScore float64

	// InvalidMessagePenalty is the score reduction for delivering an invalid message.
	InvalidMessagePenalty float64

	// ValidMessageReward is the score increase for delivering a valid message.
	ValidMessageReward float64

	// LateDeliveryPenalty penalizes peers that deliver messages after the expected window.
	LateDeliveryPenalty float64

	// DecayInterval is how often scores decay toward zero.
	DecayInterval time.Duration

	// DecayFactor is the multiplicative decay applied each interval (0.0 to 1.0).
	DecayFactor float64
}

// DefaultGossipScoreConfig returns sensible defaults for DAS subnet gossip scoring.
func DefaultGossipScoreConfig() GossipScoreConfig {
	return GossipScoreConfig{
		MaxScore:              100.0,
		MinScore:              -100.0,
		InvalidMessagePenalty: -10.0,
		ValidMessageReward:    1.0,
		LateDeliveryPenalty:   -2.0,
		DecayInterval:         12 * time.Second,
		DecayFactor:           0.9,
	}
}

// gossipPeerEntry tracks a single peer's gossip score across DAS subnets.
type gossipPeerEntry struct {
	score           float64
	validMessages   uint64
	invalidMessages uint64
	lateMessages    uint64
	lastMessageTime time.Time
	lastDecayTime   time.Time
}

// GossipScorer tracks gossip quality scores for peers across DAS subnets.
// It supports per-subnet scoring, automatic time-based decay, and provides
// peer ranking for sample requests. Thread-safe.
type GossipScorer struct {
	mu     sync.RWMutex
	config GossipScoreConfig

	// peers maps peer ID -> subnet ID -> score entry.
	peers map[[32]byte]map[uint64]*gossipPeerEntry
}

// NewGossipScorer creates a new gossip scorer with the given configuration.
func NewGossipScorer(config GossipScoreConfig) *GossipScorer {
	return &GossipScorer{
		config: config,
		peers:  make(map[[32]byte]map[uint64]*gossipPeerEntry),
	}
}

// RecordValidMessage records that a peer delivered a valid message on a subnet.
func (gs *GossipScorer) RecordValidMessage(peerID [32]byte, subnetID uint64) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	entry := gs.getOrCreateEntry(peerID, subnetID)
	entry.validMessages++
	entry.score += gs.config.ValidMessageReward
	if entry.score > gs.config.MaxScore {
		entry.score = gs.config.MaxScore
	}
	entry.lastMessageTime = time.Now()
}

// RecordInvalidMessage records that a peer delivered an invalid message.
func (gs *GossipScorer) RecordInvalidMessage(peerID [32]byte, subnetID uint64) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	entry := gs.getOrCreateEntry(peerID, subnetID)
	entry.invalidMessages++
	entry.score += gs.config.InvalidMessagePenalty
	if entry.score < gs.config.MinScore {
		entry.score = gs.config.MinScore
	}
}

// RecordLateDelivery records that a peer delivered a message late.
func (gs *GossipScorer) RecordLateDelivery(peerID [32]byte, subnetID uint64) {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	entry := gs.getOrCreateEntry(peerID, subnetID)
	entry.lateMessages++
	entry.score += gs.config.LateDeliveryPenalty
	if entry.score < gs.config.MinScore {
		entry.score = gs.config.MinScore
	}
}

// PeerSubnetScore returns the score for a peer on a specific subnet.
func (gs *GossipScorer) PeerSubnetScore(peerID [32]byte, subnetID uint64) (float64, bool) {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	subnets, ok := gs.peers[peerID]
	if !ok {
		return 0, false
	}
	entry, ok := subnets[subnetID]
	if !ok {
		return 0, false
	}
	return entry.score, true
}

// PeerAggregateScore returns the total score across all subnets for a peer.
func (gs *GossipScorer) PeerAggregateScore(peerID [32]byte) float64 {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	subnets, ok := gs.peers[peerID]
	if !ok {
		return 0
	}
	var total float64
	for _, entry := range subnets {
		total += entry.score
	}
	return total
}

// IsBelowThreshold returns true if the peer's aggregate score is below the
// minimum threshold, indicating the peer should be disconnected or avoided.
func (gs *GossipScorer) IsBelowThreshold(peerID [32]byte) bool {
	return gs.PeerAggregateScore(peerID) < gs.config.MinScore
}

// RankPeersForSubnet returns peer IDs sorted by descending score for a subnet.
func (gs *GossipScorer) RankPeersForSubnet(subnetID uint64) [][32]byte {
	gs.mu.RLock()
	defer gs.mu.RUnlock()

	type peerWithScore struct {
		id    [32]byte
		score float64
	}

	var ranked []peerWithScore
	for peerID, subnets := range gs.peers {
		if entry, ok := subnets[subnetID]; ok {
			ranked = append(ranked, peerWithScore{id: peerID, score: entry.score})
		}
	}

	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	result := make([][32]byte, len(ranked))
	for i, r := range ranked {
		result[i] = r.id
	}
	return result
}

// DecayScores applies time-based decay to all peer scores. Should be called
// periodically (e.g., once per slot).
func (gs *GossipScorer) DecayScores() {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	now := time.Now()
	for _, subnets := range gs.peers {
		for _, entry := range subnets {
			elapsed := now.Sub(entry.lastDecayTime)
			if elapsed >= gs.config.DecayInterval {
				entry.score *= gs.config.DecayFactor
				entry.lastDecayTime = now
			}
		}
	}
}

// PeerCount returns the number of tracked peers.
func (gs *GossipScorer) PeerCount() int {
	gs.mu.RLock()
	defer gs.mu.RUnlock()
	return len(gs.peers)
}

// getOrCreateEntry returns (or creates) the score entry for a peer on a subnet.
// Caller must hold gs.mu write lock.
func (gs *GossipScorer) getOrCreateEntry(peerID [32]byte, subnetID uint64) *gossipPeerEntry {
	subnets, ok := gs.peers[peerID]
	if !ok {
		subnets = make(map[uint64]*gossipPeerEntry)
		gs.peers[peerID] = subnets
	}
	entry, ok := subnets[subnetID]
	if !ok {
		entry = &gossipPeerEntry{
			lastDecayTime: time.Now(),
		}
		subnets[subnetID] = entry
	}
	return entry
}

// --- DataColumnSidecar creation ---

// BuildDataColumnSidecar constructs a DataColumnSidecar from raw block data.
// It takes the column index, per-blob cells for that column, the per-blob
// KZG commitments, and per-blob KZG proofs.
func BuildDataColumnSidecar(
	columnIndex dastypes.ColumnIndex,
	cells []dastypes.Cell,
	commitments []dastypes.KZGCommitment,
	proofs []dastypes.KZGProof,
) (*dastypes.DataColumnSidecar, error) {
	return BuildDataColumnSidecarWithHeader(columnIndex, cells, commitments, proofs, nil)
}

// BuildDataColumnSidecarWithHeader constructs a DataColumnSidecar carrying
// the signed beacon block header per EIP-7594.
func BuildDataColumnSidecarWithHeader(
	columnIndex dastypes.ColumnIndex,
	cells []dastypes.Cell,
	commitments []dastypes.KZGCommitment,
	proofs []dastypes.KZGProof,
	header *dastypes.SignedBeaconBlockHeader,
) (*dastypes.DataColumnSidecar, error) {
	if len(cells) == 0 {
		return nil, ErrSidecarBuildNoCells
	}
	if len(cells) != len(commitments) || len(cells) != len(proofs) {
		return nil, fmt.Errorf("%w: cells=%d, commitments=%d, proofs=%d",
			ErrSidecarBuildMismatch, len(cells), len(commitments), len(proofs))
	}
	if uint64(columnIndex) >= dastypes.NumberOfColumns {
		return nil, fmt.Errorf("%w: %d >= %d", sampling.ErrInvalidColumnIndex, columnIndex, dastypes.NumberOfColumns)
	}

	// Build the inclusion proof (simplified Merkle proof over commitments).
	inclusionProof := buildCommitmentInclusionProof(commitments, uint64(columnIndex))

	return &dastypes.DataColumnSidecar{
		Index:             columnIndex,
		Column:            cells,
		KZGCommitments:    commitments,
		KZGProofs:         proofs,
		SignedBlockHeader: header,
		InclusionProof:    inclusionProof,
	}, nil
}

// buildCommitmentInclusionProof builds a binary Merkle inclusion proof for a
// column index over the set of commitments using SHA-256 hashing. The proof
// consists of sibling hashes along the path from the leaf to the root.
func buildCommitmentInclusionProof(commitments []dastypes.KZGCommitment, index uint64) [][32]byte {
	n := len(commitments)
	if n == 0 {
		return nil
	}

	// Build leaf hashes using SHA-256 with domain separation.
	leaves := make([][32]byte, n)
	for i, c := range commitments {
		leaves[i] = merkleHashLeaf(c[:])
	}

	// Collect sibling hashes along the Merkle path.
	var proof [][32]byte
	current := leaves
	idx := index % uint64(n)

	for len(current) > 1 {
		// Pad to even by duplicating last element.
		if len(current)%2 != 0 {
			current = append(current, current[len(current)-1])
		}

		siblingIdx := idx ^ 1
		if siblingIdx < uint64(len(current)) {
			proof = append(proof, current[siblingIdx])
		}

		// Compute next level using SHA-256 node hashing.
		next := make([][32]byte, len(current)/2)
		for i := 0; i < len(current); i += 2 {
			next[i/2] = merkleHashNode(current[i], current[i+1])
		}
		current = next
		idx /= 2
	}

	return proof
}

// merkleHashLeaf computes SHA-256(0x00 || data) for a leaf node.
// The 0x00 prefix provides domain separation from internal nodes.
func merkleHashLeaf(data []byte) [32]byte {
	h := sha256.New()
	h.Write([]byte{0x00})
	h.Write(data)
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// merkleHashNode computes SHA-256(0x01 || left || right) for an internal node.
// The 0x01 prefix provides domain separation from leaf nodes.
func merkleHashNode(left, right [32]byte) [32]byte {
	h := sha256.New()
	h.Write([]byte{0x01})
	h.Write(left[:])
	h.Write(right[:])
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out
}

// --- Reconstruction trigger ---

// ReconstructionTrigger monitors received columns for a slot and triggers
// blob reconstruction when enough columns have been received. Thread-safe.
type ReconstructionTrigger struct {
	mu sync.Mutex

	// receivedColumns tracks which columns have been received per blob.
	// Maps blobIndex -> set of column indices.
	receivedColumns map[uint64]map[dastypes.ColumnIndex]bool

	// reconstructed tracks which blobs have already been reconstructed.
	reconstructed map[uint64]bool

	// threshold is the minimum columns needed (default: ReconstructionThreshold).
	threshold int
}

// NewReconstructionTrigger creates a new reconstruction trigger.
func NewReconstructionTrigger() *ReconstructionTrigger {
	return &ReconstructionTrigger{
		receivedColumns: make(map[uint64]map[dastypes.ColumnIndex]bool),
		reconstructed:   make(map[uint64]bool),
		threshold:       dastypes.ReconstructionThreshold,
	}
}

// RecordColumn records that a column has been received for a blob.
// Returns true if the blob now has enough columns for reconstruction
// and reconstruction has not yet been triggered for this blob.
func (rt *ReconstructionTrigger) RecordColumn(blobIndex uint64, colIndex dastypes.ColumnIndex) (bool, error) {
	if uint64(colIndex) >= dastypes.NumberOfColumns {
		return false, fmt.Errorf("%w: %d >= %d", sampling.ErrInvalidColumnIndex, colIndex, dastypes.NumberOfColumns)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	if rt.reconstructed[blobIndex] {
		return false, nil
	}

	cols, ok := rt.receivedColumns[blobIndex]
	if !ok {
		cols = make(map[dastypes.ColumnIndex]bool)
		rt.receivedColumns[blobIndex] = cols
	}
	cols[colIndex] = true

	return len(cols) >= rt.threshold && !rt.reconstructed[blobIndex], nil
}

// MarkReconstructed marks a blob as having been reconstructed, preventing
// further reconstruction triggers.
func (rt *ReconstructionTrigger) MarkReconstructed(blobIndex uint64) {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.reconstructed[blobIndex] = true
}

// ReceivedColumnCount returns the number of columns received for a blob.
func (rt *ReconstructionTrigger) ReceivedColumnCount(blobIndex uint64) int {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return len(rt.receivedColumns[blobIndex])
}

// ReadyBlobs returns the blob indices that have enough columns for
// reconstruction but have not yet been reconstructed.
func (rt *ReconstructionTrigger) ReadyBlobs() []uint64 {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	var ready []uint64
	for blobIdx, cols := range rt.receivedColumns {
		if len(cols) >= rt.threshold && !rt.reconstructed[blobIdx] {
			ready = append(ready, blobIdx)
		}
	}
	sort.Slice(ready, func(i, j int) bool { return ready[i] < ready[j] })
	return ready
}

// Reset clears all tracked state for a new slot/block.
func (rt *ReconstructionTrigger) Reset() {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	rt.receivedColumns = make(map[uint64]map[dastypes.ColumnIndex]bool)
	rt.reconstructed = make(map[uint64]bool)
}

// --- Custody column verification at gossip layer ---

// VerifyGossipColumn validates a DataColumnSidecar received via gossip against
// the node's custody assignment. It checks that:
//   - The sidecar passes structural validation
//   - The column index maps to a subnet the node should be subscribed to
//   - The column is in the node's custody set (if custodyColumns is provided)
//
// Returns the subnet ID that this column belongs to on success.
func VerifyGossipColumn(
	sidecar *dastypes.DataColumnSidecar,
	custodyColumns []dastypes.ColumnIndex,
) (dastypes.SubnetID, error) {
	// Structural validation.
	if err := sampling.VerifyDataColumnSidecar(sidecar); err != nil {
		return 0, err
	}

	// Compute the subnet for this column.
	subnet := sampling.ColumnSubnet(sidecar.Index)

	// If custody columns are provided, verify membership.
	if len(custodyColumns) > 0 {
		if !sampling.ShouldCustodyColumn(sidecar.Index, custodyColumns) {
			return 0, fmt.Errorf("%w: column %d not in custody set",
				cell.ErrCellNotInCustody, sidecar.Index)
		}
	}

	return subnet, nil
}

// ComputeSidecarHash computes a unique identifier for a DataColumnSidecar
// based on its column index, cells, and commitments. Used for deduplication
// in the gossip layer.
func ComputeSidecarHash(sidecar *dastypes.DataColumnSidecar) [32]byte {
	h := sha3.NewLegacyKeccak256()

	// Write column index.
	var indexBuf [8]byte
	binary.LittleEndian.PutUint64(indexBuf[:], uint64(sidecar.Index))
	h.Write(indexBuf[:])

	// Write cells.
	for _, c := range sidecar.Column {
		h.Write(c[:])
	}

	// Write commitments.
	for _, c := range sidecar.KZGCommitments {
		h.Write(c[:])
	}

	var result [32]byte
	h.Sum(result[:0])
	return result
}
