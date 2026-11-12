// Package p2p implements gossip topic management per the Ethereum consensus
// layer P2P specification. TopicManager tracks subscribed gossip topics,
// handles message validation, deduplication, and per-topic scoring.
package gossip

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// GossipTopic enumerates the consensus-layer gossip sub topics.
type GossipTopic int

const (
	// BeaconBlock is the global topic for propagating signed beacon blocks.
	BeaconBlock GossipTopic = iota
	// BeaconAggregateAndProof is the global topic for aggregated attestations.
	BeaconAggregateAndProof
	// VoluntaryExit propagates signed voluntary validator exits.
	VoluntaryExit
	// ProposerSlashing propagates proposer slashings.
	ProposerSlashing
	// AttesterSlashing propagates attester slashings.
	AttesterSlashing
	// BlobSidecar propagates blob sidecars (EIP-4844+).
	BlobSidecar
	// DataColumnSidecar propagates data column sidecars (EIP-7594/Fulu),
	// one gossipsub topic per subnet: data_column_sidecar_<subnet_id>.
	DataColumnSidecar
	// InclusionList propagates signed inclusion lists (EIP-7805 FOCIL),
	// one global topic per the spec.
	InclusionList
	// SyncCommitteeContribution propagates sync committee contributions.
	SyncCommitteeContribution
	// STARKMempoolTick propagates recursive STARK mempool aggregation ticks.
	// Handler registration: at application startup, the node should call
	// TopicManager.Subscribe(STARKMempoolTick, handler) where the handler
	// deserialises the tick via MempoolAggregationTick.UnmarshalBinary,
	// validates the STARK proof, and calls STARKAggregator.MergeTick.
	STARKMempoolTick
	// PQAggRequest is the topic on which an aggregator broadcasts an
	// AggregateRequest asking validators to submit their XMSS bundles (LEAN-3.3).
	PQAggRequest
	// PQAggResult is the topic on which the aggregator publishes the finished
	// STARKSignatureAggregation so proposers and peers can verify it (LEAN-3.4).
	PQAggResult
	// ProposerPreferences is the gossip topic for EIP-7732 proposer preferences
	// messages using domain DOMAIN_PROPOSER_PREFERENCES (0x0D000000).
	ProposerPreferences
)

// gossipTopicNames maps each GossipTopic to its canonical string name
// per the consensus P2P spec topic naming convention.
var gossipTopicNames = map[GossipTopic]string{
	BeaconBlock:               "beacon_block",
	BeaconAggregateAndProof:   "beacon_aggregate_and_proof",
	VoluntaryExit:             "voluntary_exit",
	ProposerSlashing:          "proposer_slashing",
	AttesterSlashing:          "attester_slashing",
	BlobSidecar:               "blob_sidecar",
	DataColumnSidecar:         "data_column_sidecar",
	InclusionList:             "inclusion_list",
	SyncCommitteeContribution: "sync_committee_contribution",
	STARKMempoolTick:          "stark_mempool_tick",
	PQAggRequest:              "pq_agg_request",
	PQAggResult:               "pq_agg_result",
	ProposerPreferences:       "proposer_preferences",
}

// String returns the spec-defined name of the gossip topic.
func (t GossipTopic) String() string {
	if name, ok := gossipTopicNames[t]; ok {
		return name
	}
	return fmt.Sprintf("unknown_topic(%d)", int(t))
}

// TopicString builds the full topic string in the form:
// /eth2/<fork_digest>/<name>/ssz_snappy
func (t GossipTopic) TopicString(forkDigest string) string {
	return fmt.Sprintf("/eth2/%s/%s/ssz_snappy", forkDigest, t.String())
}

// DataColumnSidecarsSubnetCount is DATA_COLUMN_SIDECARS_SUBNET_COUNT from
// the Fulu preset (EIP-7594).
const DataColumnSidecarsSubnetCount = 64

// ComputeSubnetForDataColumnSidecar returns the gossip subnet for a column
// index, per the Fulu p2p spec: column_index mod DATA_COLUMN_SIDECARS_
// SUBNET_COUNT.
func ComputeSubnetForDataColumnSidecar(columnIndex uint64) uint64 {
	return columnIndex % DataColumnSidecarsSubnetCount
}

// DataColumnSidecarTopicString builds the canonical Fulu topic for a
// data column sidecar subnet:
// /eth2/<fork_digest>/data_column_sidecar_<subnet_id>/ssz_snappy
func DataColumnSidecarTopicString(forkDigest string, subnetID uint64) string {
	return fmt.Sprintf("/eth2/%s/data_column_sidecar_%d/ssz_snappy", forkDigest, subnetID)
}

// ParseDataColumnSidecarTopic parses a data_column_sidecar_<subnet_id>
// topic string and returns the subnet ID.
func ParseDataColumnSidecarTopic(topic string) (uint64, error) {
	// /eth2/<fork_digest>/data_column_sidecar_<subnet_id>/ssz_snappy
	parts := splitTopicComponents(topic)
	if len(parts) != 4 || len(parts[2]) <= len("data_column_sidecar_") ||
		parts[2][:len("data_column_sidecar_")] != "data_column_sidecar_" {
		return 0, fmt.Errorf("gossip: not a data_column_sidecar topic %q", topic)
	}
	subnet := 0
	for _, c := range parts[2][len("data_column_sidecar_"):] {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("gossip: invalid subnet id in topic %q", topic)
		}
		subnet = subnet*10 + int(c-'0')
	}
	return uint64(subnet), nil
}

// splitTopicComponents splits an /eth2/<digest>/<name>/ssz_snappy topic
// into its three inner components.
func splitTopicComponents(topic string) []string {
	trimmed := strings.Trim(topic, "/")
	return strings.Split(trimmed, "/")
}

// ParseGossipTopic converts a topic name string to a GossipTopic.
// Returns an error if the name is not recognized.
func ParseGossipTopic(name string) (GossipTopic, error) {
	for topic, n := range gossipTopicNames {
		if n == name {
			return topic, nil
		}
	}
	// data_column_sidecar_<subnet_id> maps to the DataColumnSidecar topic.
	if _, err := ParseDataColumnSidecarTopic("/eth2/x/" + name + "/ssz_snappy"); err == nil {
		return DataColumnSidecar, nil
	}
	return 0, fmt.Errorf("gossip: unknown topic name %q", name)
}

// Message domains for gossip message-id computation per the spec.
var (
	MessageDomainValidSnappy   = [4]byte{0x01, 0x00, 0x00, 0x00}
	MessageDomainInvalidSnappy = [4]byte{0x00, 0x00, 0x00, 0x00}
)

// MessageIDSize is the size of a gossip message ID (20 bytes per spec).
const MessageIDSize = 20

// MessageID is a 20-byte gossip message identifier.
type MessageID [MessageIDSize]byte

// ComputeMessageID computes the gossip message ID per the consensus spec:
// SHA256(MESSAGE_DOMAIN_VALID_SNAPPY + snappy_decompressed_data)[:20]
// For simplicity, the caller passes the decompressed data directly.
// If decompression failed, use ComputeInvalidMessageID instead.
func ComputeMessageID(decompressedData []byte) MessageID {
	h := sha256.New()
	h.Write(MessageDomainValidSnappy[:])
	h.Write(decompressedData)
	sum := h.Sum(nil)
	var id MessageID
	copy(id[:], sum[:MessageIDSize])
	return id
}

// ComputeInvalidMessageID computes the message ID for data that failed
// snappy decompression: SHA256(MESSAGE_DOMAIN_INVALID_SNAPPY + raw_data)[:20]
func ComputeInvalidMessageID(rawData []byte) MessageID {
	h := sha256.New()
	h.Write(MessageDomainInvalidSnappy[:])
	h.Write(rawData)
	sum := h.Sum(nil)
	var id MessageID
	copy(id[:], sum[:MessageIDSize])
	return id
}

// TopicParams holds the gossipsub mesh parameters per the spec.
type TopicParams struct {
	// MeshD is the target number of peers in the mesh (D=8 per spec).
	MeshD int
	// MeshDlo is the low watermark for mesh peers (D_low=6).
	MeshDlo int
	// MeshDhi is the high watermark for mesh peers (D_high=12).
	MeshDhi int
	// HeartbeatInterval is the gossipsub heartbeat frequency.
	HeartbeatInterval time.Duration
	// HistoryLength is the number of heartbeat windows to retain message IDs.
	HistoryLength int
	// HistoryGossip is the number of windows to gossip about (mcache_gossip=3).
	HistoryGossip int
	// FanoutTTL is the TTL for fanout maps (60s per spec).
	FanoutTTL time.Duration
	// SeenTTL is the expiry time for the seen message cache.
	SeenTTL time.Duration
}

// DefaultTopicParams returns the gossipsub parameters from the consensus spec.
func DefaultTopicParams() TopicParams {
	return TopicParams{
		MeshD:             8,
		MeshDlo:           6,
		MeshDhi:           12,
		HeartbeatInterval: 700 * time.Millisecond,
		HistoryLength:     6,
		HistoryGossip:     3,
		FanoutTTL:         60 * time.Second,
		SeenTTL:           384 * time.Second, // SECONDS_PER_SLOT(12) * SLOTS_PER_EPOCH(32) = 384
	}
}

// TopicHandler is a callback invoked when a validated message is received
// on a subscribed topic.
type TopicHandler func(topic GossipTopic, msgID MessageID, data []byte)

// TopicScoreSnapshot holds per-topic scoring metrics.
type TopicScoreSnapshot struct {
	// MessagesReceived is the total number of messages received on this topic.
	MessagesReceived uint64
	// InvalidMessages is the count of messages that failed validation.
	InvalidMessages uint64
	// MeshDeliveries is the count of messages delivered via the mesh.
	MeshDeliveries uint64
	// FirstDeliveries is the count of messages received for the first time.
	FirstDeliveries uint64
}

// Errors for the TopicManager.
var (
	ErrTopicNotSubscribed     = errors.New("gossip_topics: topic not subscribed")
	ErrTopicAlreadySubscribed = errors.New("gossip_topics: topic already subscribed")
	ErrTopicManagerClosed     = errors.New("gossip_topics: manager is closed")
	ErrTopicNilHandler        = errors.New("gossip_topics: nil handler")
	ErrTopicEmptyData         = errors.New("gossip_topics: empty data")
	ErrTopicDuplicateMessage  = errors.New("gossip_topics: duplicate message")
	ErrTopicDataTooLarge      = errors.New("gossip_topics: data exceeds max payload size")
)

// MaxPayloadSize is the maximum uncompressed payload size (10 MiB per spec).
const MaxPayloadSize = 10 * 1024 * 1024

// TopicMessageSizeLimit defines per-topic maximum message sizes.
// Topics not in this map use the global MaxPayloadSize limit.
var TopicMessageSizeLimit = map[GossipTopic]int{
	STARKMempoolTick: 128 * 1024, // 128KB per ethresear.ch
}

// ErrTopicMsgTooLarge is returned when a message exceeds its per-topic size limit.
var ErrTopicMsgTooLarge = errors.New("gossip_topics: message exceeds per-topic size limit")

// topicState tracks per-topic subscription state and scoring.
type topicState struct {
	handler TopicHandler
	score   TopicScoreSnapshot
	peers   map[string]float64 // peer ID -> per-topic score
}

// TopicManager manages gossip sub topics per the consensus P2P spec.
// It tracks subscribed topics, message handlers, message deduplication,
// and per-topic scoring. All methods are safe for concurrent use.
type TopicManager struct {
	mu     sync.RWMutex
	params TopicParams
	closed bool

	// Subscribed topics with their handlers and state.
	topics map[GossipTopic]*topicState

	// Global message deduplication: messageID -> receive time.
	seen   map[MessageID]time.Time
	seenMu sync.Mutex
}

// NewTopicManager creates a new TopicManager with the given parameters.
func NewTopicManager(params TopicParams) *TopicManager {
	return &TopicManager{
		params: params,
		topics: make(map[GossipTopic]*topicState),
		seen:   make(map[MessageID]time.Time),
	}
}

// Subscribe registers a handler for the given gossip topic.
// Returns an error if the topic is already subscribed or the handler is nil.
func (tm *TopicManager) Subscribe(topic GossipTopic, handler TopicHandler) error {
	if handler == nil {
		return ErrTopicNilHandler
	}

	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.closed {
		return ErrTopicManagerClosed
	}

	if _, exists := tm.topics[topic]; exists {
		return ErrTopicAlreadySubscribed
	}

	tm.topics[topic] = &topicState{
		handler: handler,
		peers:   make(map[string]float64),
	}
	return nil
}

// Unsubscribe removes the handler for the given gossip topic.
func (tm *TopicManager) Unsubscribe(topic GossipTopic) error {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	if tm.closed {
		return ErrTopicManagerClosed
	}

	if _, exists := tm.topics[topic]; !exists {
		return ErrTopicNotSubscribed
	}

	delete(tm.topics, topic)
	return nil
}

// IsSubscribed returns whether the given topic is currently subscribed.
func (tm *TopicManager) IsSubscribed(topic GossipTopic) bool {
	tm.mu.RLock()
	defer tm.mu.RUnlock()
	_, exists := tm.topics[topic]
	return exists
}

// SubscribedTopics returns a list of all currently subscribed topics.
func (tm *TopicManager) SubscribedTopics() []GossipTopic {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	topics := make([]GossipTopic, 0, len(tm.topics))
	for t := range tm.topics {
		topics = append(topics, t)
	}
	return topics
}

// Publish validates and dispatches data on the given topic.
// It computes the message ID, checks for duplicates, and invokes
// the registered handler. Returns an error if the topic is not
// subscribed, the data is empty/too large, or it is a duplicate.
func (tm *TopicManager) Publish(topic GossipTopic, data []byte) error {
	return tm.Deliver(topic, data, true)
}

// Deliver processes an incoming message from another peer. It validates
// the message, checks deduplication, and dispatches to the handler.
// isValid indicates whether the data successfully decompressed from snappy.
func (tm *TopicManager) Deliver(topic GossipTopic, data []byte, isValid bool) error {
	if len(data) == 0 {
		return ErrTopicEmptyData
	}
	if len(data) > MaxPayloadSize {
		return ErrTopicDataTooLarge
	}
	// Per-topic size limit check.
	if limit, ok := TopicMessageSizeLimit[topic]; ok && len(data) > limit {
		return ErrTopicMsgTooLarge
	}

	var msgID MessageID
	if isValid {
		msgID = ComputeMessageID(data)
	} else {
		msgID = ComputeInvalidMessageID(data)
	}

	tm.mu.Lock()
	if tm.closed {
		tm.mu.Unlock()
		return ErrTopicManagerClosed
	}
	state, exists := tm.topics[topic]
	if !exists {
		tm.mu.Unlock()
		return ErrTopicNotSubscribed
	}
	tm.seenMu.Lock()
	if _, duplicate := tm.seen[msgID]; duplicate {
		tm.seenMu.Unlock()
		tm.mu.Unlock()
		return ErrTopicDuplicateMessage
	}
	tm.seen[msgID] = time.Now()
	tm.seenMu.Unlock()
	state.score.MessagesReceived++
	if !isValid {
		state.score.InvalidMessages++
		tm.mu.Unlock()
		return nil
	}
	state.score.FirstDeliveries++
	state.score.MeshDeliveries++
	handler := state.handler
	tm.mu.Unlock()
	// Application callbacks may publish, unsubscribe or inspect stats.
	handler(topic, msgID, data)
	return nil
}

// RecordInvalidMessage increments the invalid message counter for a topic.
func (tm *TopicManager) RecordInvalidMessage(topic GossipTopic) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	if state, exists := tm.topics[topic]; exists {
		state.score.InvalidMessages++
	}
}

// TopicScore returns the scoring snapshot for a subscribed topic.
func (tm *TopicManager) TopicScore(topic GossipTopic) (TopicScoreSnapshot, bool) {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	state, exists := tm.topics[topic]
	if !exists {
		return TopicScoreSnapshot{}, false
	}
	return state.score, true
}

// UpdatePeerTopicScore adjusts the per-topic score for a peer.
func (tm *TopicManager) UpdatePeerTopicScore(topic GossipTopic, peerID string, delta float64) {
	tm.mu.Lock()
	defer tm.mu.Unlock()

	state, exists := tm.topics[topic]
	if !exists {
		return
	}
	state.peers[peerID] += delta
}

// PeerTopicScore returns the per-topic score for a peer on a given topic.
func (tm *TopicManager) PeerTopicScore(topic GossipTopic, peerID string) float64 {
	tm.mu.RLock()
	defer tm.mu.RUnlock()

	state, exists := tm.topics[topic]
	if !exists {
		return 0
	}
	return state.peers[peerID]
}

// PruneSeenMessages removes seen message entries older than the SeenTTL.
// This should be called periodically to prevent unbounded memory growth.
func (tm *TopicManager) PruneSeenMessages() int {
	cutoff := time.Now().Add(-tm.params.SeenTTL)
	pruned := 0

	tm.seenMu.Lock()
	defer tm.seenMu.Unlock()

	for id, t := range tm.seen {
		if t.Before(cutoff) {
			delete(tm.seen, id)
			pruned++
		}
	}
	return pruned
}

// SeenCount returns the number of message IDs in the seen cache.
func (tm *TopicManager) SeenCount() int {
	tm.seenMu.Lock()
	defer tm.seenMu.Unlock()
	return len(tm.seen)
}

// Params returns the current topic parameters.
func (tm *TopicManager) Params() TopicParams {
	return tm.params
}

// Close shuts down the topic manager. After closing, all methods
// that modify state return ErrTopicManagerClosed.
func (tm *TopicManager) Close() {
	tm.mu.Lock()
	defer tm.mu.Unlock()
	tm.closed = true
	tm.topics = make(map[GossipTopic]*topicState)
}
