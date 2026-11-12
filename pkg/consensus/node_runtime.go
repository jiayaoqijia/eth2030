package consensus

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/eth2030/eth2030/consensus/pq"
	"github.com/eth2030/eth2030/consensus/slot"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/crypto/pqc"
	"github.com/eth2030/eth2030/rlp"
)

// NativeConfig explicitly enables a research 3SF/PQ observer. It is
// not a mainnet fork schedule. Slot timing is configurable as in EIP-7782.
type NativeConfig struct {
	SlotMillis    uint64            `json:"slot_millis"`
	SlotsPerEpoch uint64            `json:"slots_per_epoch"`
	Validators    map[uint64][]byte `json:"validators"`
}

type NativeVote struct {
	Source, Target         types.Hash
	SourceSlot, TargetSlot uint64
	Proof                  pq.PQAttestation
}

func nativeVoteRoot(domain types.Hash, v NativeVote) types.Hash {
	encoded, _ := rlp.EncodeToBytes([]any{[]byte("eth2030/native-3sf/v1"), domain, v.Source, v.Target, v.SourceSlot, v.TargetSlot, v.Proof.Slot, v.Proof.ValidatorIndex})
	return crypto.Keccak256Hash(encoded)
}

// SignNativeVote binds chain, validator identity, source and target into the
// existing PQ attestation signing path. No classic fallback is permitted.
func SignNativeVote(domain types.Hash, source, target types.Hash, sourceSlot, targetSlot, voteSlot, validator uint64, key *pqc.DilithiumKeyPair) (NativeVote, error) {
	vote := NativeVote{Source: source, Target: target, SourceSlot: sourceSlot, TargetSlot: targetSlot, Proof: pq.PQAttestation{Slot: voteSlot, ValidatorIndex: validator}}
	proof, err := pq.CreatePQAttestation(voteSlot, 0, nativeVoteRoot(domain, vote), sourceSlot, targetSlot, validator, key)
	if err != nil {
		return NativeVote{}, err
	}
	vote.Proof = *proof
	return vote, nil
}

type NativeStatus struct {
	Running                      bool
	Slot                         uint64
	JustifiedSlot, FinalizedSlot uint64
	JustifiedRoot, FinalizedRoot types.Hash
	Verified, Rejected           uint64
}

var ErrNativeDuplicateVote = errors.New("native observer: duplicate vote")
var ErrNativeStaleVote = errors.New("native observer: stale vote")
var ErrNativeUnready = errors.New("native observer: vote cannot yet be evaluated")

type nativeLink struct{ Source, Target types.Hash }

type nativeBlock struct {
	slot   uint64
	parent types.Hash
}

// NodeRuntime combines quick-slot timing, real ML-DSA attestation validation
// and the source/target justification rules in refs/research/3sf-mini. It
// finalizes the source only when a quorum justifies its next eligible target;
// a quorum for an isolated target never finalizes that target immediately.
type NodeRuntime struct {
	mu           sync.Mutex
	config       NativeConfig
	domain       types.Hash
	scheduler    *slot.QuickSlotScheduler
	verifier     *pq.PQAttestationVerifier
	blocks       map[types.Hash]nativeBlock
	justified    map[types.Hash]bool
	votes        map[nativeLink]map[uint64]bool
	seen         map[uint64]map[uint64]types.Hash
	votedTargets map[uint64]map[uint64]types.Hash
	status       NativeStatus
	cancel       context.CancelFunc
	done         chan struct{}
	onSlot       func(uint64)
	onFinalized  func(types.Hash, types.Hash) error
}

func NewNodeRuntime(cfg NativeConfig, domain types.Hash, genesis time.Time, checkpoint types.Hash, checkpointSlot uint64, onSlot func(uint64), onFinalized func(types.Hash, types.Hash) error) (*NodeRuntime, error) {
	if cfg.SlotMillis == 0 || cfg.SlotMillis > 60000 || cfg.SlotsPerEpoch == 0 || len(cfg.Validators) < 3 {
		return nil, fmt.Errorf("native consensus: invalid timing or validator registry")
	}
	keys := make(map[string]bool)
	registry := make(map[uint64][]byte)
	for id, key := range cfg.Validators {
		if len(key) != pqc.MLDSAPublicKeySize || keys[string(key)] {
			return nil, fmt.Errorf("native consensus: invalid or duplicate ML-DSA-65 key")
		}
		keys[string(key)] = true
		registry[id] = bytes.Clone(key)
	}
	cfg.Validators = registry
	scheduler := slot.NewQuickSlotScheduler(&slot.QuickSlotConfig{SlotDuration: time.Duration(cfg.SlotMillis) * time.Millisecond, SlotsPerEpoch: cfg.SlotsPerEpoch}, genesis)
	return &NodeRuntime{config: cfg, domain: nativeDomain(domain, cfg), scheduler: scheduler, verifier: pq.NewPQAttestationVerifier(&pq.PQAttestationConfig{UsePQSignatures: true}), blocks: map[types.Hash]nativeBlock{checkpoint: {slot: checkpointSlot}}, justified: map[types.Hash]bool{checkpoint: true}, votes: make(map[nativeLink]map[uint64]bool), votedTargets: make(map[uint64]map[uint64]types.Hash), seen: make(map[uint64]map[uint64]types.Hash), status: NativeStatus{JustifiedRoot: checkpoint, FinalizedRoot: checkpoint, JustifiedSlot: checkpointSlot, FinalizedSlot: checkpointSlot}, onSlot: onSlot, onFinalized: onFinalized}, nil
}
func (r *NodeRuntime) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.Running {
		return
	}
	if r.done != nil {
		select {
		case <-r.done:
		default:
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.done = make(chan struct{})
	r.status.Running = true
	go r.loop(ctx, r.done)
}
func (r *NodeRuntime) loop(ctx context.Context, done chan struct{}) {
	defer close(done)
	for {
		current := r.scheduler.CurrentSlot()
		r.mu.Lock()
		r.status.Slot = current
		for slot := range r.seen {
			if current > 2 && slot < current-2 {
				delete(r.seen, slot)
			}
		}
		r.mu.Unlock()
		if r.onSlot != nil {
			r.onSlot(current)
		}
		timer := time.NewTimer(r.scheduler.TimeUntilNextSlot())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
func (r *NodeRuntime) Stop() {
	r.mu.Lock()
	if r.done == nil {
		r.mu.Unlock()
		return
	}
	r.status.Running = false
	r.cancel()
	done := r.done
	r.mu.Unlock()
	<-done
}
func (r *NodeRuntime) Status() NativeStatus      { r.mu.Lock(); defer r.mu.Unlock(); return r.status }
func (r *NodeRuntime) SlotAt(t time.Time) uint64 { return r.scheduler.SlotAt(t) }

// ResetCheckpoint follows the authoritative CL after a reorg or finalized
// advance. Research votes never prevent the execution node following its CL.
func (r *NodeRuntime) ResetCheckpoint(root types.Hash, slot uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.blocks = map[types.Hash]nativeBlock{root: {slot: slot}}
	r.justified = map[types.Hash]bool{root: true}
	r.votes = make(map[nativeLink]map[uint64]bool)
	r.seen = make(map[uint64]map[uint64]types.Hash)
	r.votedTargets = make(map[uint64]map[uint64]types.Hash)
	r.status.FinalizedRoot, r.status.JustifiedRoot = root, root
	r.status.FinalizedSlot, r.status.JustifiedSlot = slot, slot
}
func (r *NodeRuntime) ObserveBlock(root, parent types.Hash, slot uint64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if known, ok := r.blocks[root]; ok {
		if known.slot != slot {
			return fmt.Errorf("native consensus: changed block slot")
		}
		return nil
	}
	prev, ok := r.blocks[parent]
	if !ok || prev.slot >= slot {
		return fmt.Errorf("native consensus: unknown or non-increasing parent")
	}
	if len(r.blocks) >= 8192 {
		return fmt.Errorf("native consensus: unfinalized history limit")
	}
	r.blocks[root] = nativeBlock{slot, parent}
	return nil
}
func (r *NodeRuntime) Submit(v NativeVote) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	reject := func(message string) error { r.status.Rejected++; return fmt.Errorf("native consensus: %s", message) }
	if !r.status.Running {
		return reject("not running")
	}
	now := r.scheduler.CurrentSlot()
	if now > 1 && v.Proof.Slot < now-1 {
		return ErrNativeStaleVote
	}
	if v.Proof.Slot > now {
		return ErrNativeUnready
	}
	if v.TargetSlot > v.Proof.Slot {
		return reject("vote outside slot window")
	}
	key, ok := r.config.Validators[v.Proof.ValidatorIndex]
	if !ok || !bytes.Equal(key, v.Proof.PQPublicKey) {
		return reject("unregistered validator key")
	}
	if v.Proof.BeaconBlockRoot != nativeVoteRoot(r.domain, v) || v.Proof.SourceEpoch != v.SourceSlot || v.Proof.TargetEpoch != v.TargetSlot || v.Proof.CommitteeIndex != 0 {
		return reject("signing fields mismatch")
	}
	valid, err := r.verifier.VerifyAttestation(&v.Proof)
	if err != nil || !valid {
		return reject("invalid PQ signature")
	}
	source, sok := r.blocks[v.Source]
	target, tok := r.blocks[v.Target]
	if !sok || !tok || !r.justified[v.Source] {
		return ErrNativeUnready
	}
	if source.slot != v.SourceSlot || target.slot != v.TargetSlot || v.TargetSlot <= v.SourceSlot || !IsJustifiableSlot(r.status.FinalizedSlot, v.TargetSlot) {
		return reject("invalid justification link")
	}
	ancestor := v.Target
	for r.blocks[ancestor].slot > v.SourceSlot {
		ancestor = r.blocks[ancestor].parent
	}
	if ancestor != v.Source {
		return reject("source is not target ancestor")
	}
	voteRoot := nativeVoteRoot(r.domain, v)
	if previous, exists := r.seen[v.Proof.Slot][v.Proof.ValidatorIndex]; exists {
		if previous == voteRoot {
			return ErrNativeDuplicateVote
		}
		return reject("equivocating attestation slot")
	}
	if previous, exists := r.votedTargets[v.TargetSlot][v.Proof.ValidatorIndex]; exists && previous != v.Target {
		return reject("double vote for target slot")
	}
	link := nativeLink{v.Source, v.Target}
	count := len(r.votes[link])
	if !r.votes[link][v.Proof.ValidatorIndex] {
		count++
	}
	quorum := count*3 >= len(r.config.Validators)*2
	consecutive := true
	if quorum {
		for candidate := v.SourceSlot + 1; candidate < v.TargetSlot; candidate++ {
			if IsJustifiableSlot(r.status.FinalizedSlot, candidate) {
				consecutive = false
				break
			}
		}
	}
	finalize := quorum && consecutive && v.SourceSlot > r.status.FinalizedSlot
	// A failed downstream operation must not consume the vote or advance
	// justification: the same signed message must remain retryable.
	if finalize && r.onFinalized != nil {
		if err := r.onFinalized(v.Source, v.Target); err != nil {
			return err
		}
	}
	if r.seen[v.Proof.Slot] == nil {
		r.seen[v.Proof.Slot] = make(map[uint64]types.Hash)
	}
	r.seen[v.Proof.Slot][v.Proof.ValidatorIndex] = voteRoot
	if r.votedTargets[v.TargetSlot] == nil {
		r.votedTargets[v.TargetSlot] = make(map[uint64]types.Hash)
	}
	r.votedTargets[v.TargetSlot][v.Proof.ValidatorIndex] = v.Target
	if r.votes[link] == nil {
		r.votes[link] = make(map[uint64]bool)
	}
	r.votes[link][v.Proof.ValidatorIndex] = true
	r.status.Verified++
	if !quorum {
		return nil
	}
	r.justified[v.Target] = true
	if v.TargetSlot > r.status.JustifiedSlot {
		r.status.JustifiedSlot, r.status.JustifiedRoot = v.TargetSlot, v.Target
	}
	if finalize {
		r.status.FinalizedSlot, r.status.FinalizedRoot = v.SourceSlot, v.Source
		for root, block := range r.blocks {
			if block.slot < v.SourceSlot {
				delete(r.blocks, root)
				delete(r.justified, root)
			}
		}
		for link := range r.votes {
			if _, ok := r.blocks[link.Source]; !ok {
				delete(r.votes, link)
			}
		}
		for slot := range r.votedTargets {
			if slot < v.SourceSlot {
				delete(r.votedTargets, slot)
			}
		}
	}
	return nil
}

func nativeDomain(genesis types.Hash, cfg NativeConfig) types.Hash {
	ids := make([]uint64, 0, len(cfg.Validators))
	for id := range cfg.Validators {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	type validator struct {
		ID  uint64
		Key []byte
	}
	registry := make([]validator, 0, len(ids))
	for _, id := range ids {
		registry = append(registry, validator{id, cfg.Validators[id]})
	}
	encoded, _ := rlp.EncodeToBytes([]any{[]byte("eth2030/native-observer/v2"), genesis, cfg.SlotMillis, cfg.SlotsPerEpoch, registry})
	return crypto.Keccak256Hash(encoded)
}
func (r *NodeRuntime) Domain() types.Hash { return r.domain }
