package consensus

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/eth2030/eth2030/consensus/pq"
	"github.com/eth2030/eth2030/consensus/slot"
	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto"
	"github.com/eth2030/eth2030/crypto/pqc"
	"github.com/eth2030/eth2030/rlp"
)

// NativeConfig explicitly enables the research 3SF/PQ consensus lane. It is
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
type nativeBlock struct {
	slot   uint64
	parent types.Hash
}

// NodeRuntime combines quick-slot timing, real ML-DSA attestation validation
// and the source/target justification rules in refs/research/3sf-mini. It
// finalizes the source only when a quorum justifies its next eligible target;
// a quorum for an isolated target never finalizes that target immediately.
type NodeRuntime struct {
	mu          sync.Mutex
	config      NativeConfig
	domain      types.Hash
	scheduler   *slot.QuickSlotScheduler
	verifier    *pq.PQAttestationVerifier
	blocks      map[types.Hash]nativeBlock
	justified   map[types.Hash]bool
	votes       map[types.Hash]map[uint64]bool
	seen        map[uint64]map[uint64]types.Hash
	status      NativeStatus
	cancel      context.CancelFunc
	done        chan struct{}
	onSlot      func(uint64)
	onFinalized func(types.Hash, types.Hash) error
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
	return &NodeRuntime{config: cfg, domain: domain, scheduler: scheduler, verifier: pq.NewPQAttestationVerifier(&pq.PQAttestationConfig{UsePQSignatures: true}), blocks: map[types.Hash]nativeBlock{checkpoint: {slot: checkpointSlot}}, justified: map[types.Hash]bool{checkpoint: true}, votes: make(map[types.Hash]map[uint64]bool), seen: make(map[uint64]map[uint64]types.Hash), status: NativeStatus{JustifiedRoot: checkpoint, FinalizedRoot: checkpoint, JustifiedSlot: checkpointSlot, FinalizedSlot: checkpointSlot}, onSlot: onSlot, onFinalized: onFinalized}, nil
}
func (r *NodeRuntime) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.status.Running {
		return
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
			if slot+2 < current {
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
	if !r.status.Running {
		r.mu.Unlock()
		return
	}
	r.cancel()
	done := r.done
	r.mu.Unlock()
	<-done
	r.mu.Lock()
	r.status.Running = false
	r.mu.Unlock()
}
func (r *NodeRuntime) Status() NativeStatus      { r.mu.Lock(); defer r.mu.Unlock(); return r.status }
func (r *NodeRuntime) SlotAt(t time.Time) uint64 { return r.scheduler.SlotAt(t) }
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
	if v.Proof.Slot > now || (now > 1 && v.Proof.Slot < now-1) || v.TargetSlot > v.Proof.Slot {
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
	if !sok || !tok || source.slot != v.SourceSlot || target.slot != v.TargetSlot || !r.justified[v.Source] || v.TargetSlot <= v.SourceSlot || !IsJustifiableSlot(r.status.FinalizedSlot, v.TargetSlot) {
		return reject("invalid justification link")
	}
	ancestor := v.Target
	for r.blocks[ancestor].slot > v.SourceSlot {
		ancestor = r.blocks[ancestor].parent
	}
	if ancestor != v.Source {
		return reject("source is not target ancestor")
	}
	if r.seen[v.Proof.Slot] == nil {
		r.seen[v.Proof.Slot] = make(map[uint64]types.Hash)
	}
	if _, exists := r.seen[v.Proof.Slot][v.Proof.ValidatorIndex]; exists {
		return reject("duplicate or equivocating vote")
	}
	r.seen[v.Proof.Slot][v.Proof.ValidatorIndex] = v.Target
	r.status.Verified++
	if r.votes[v.Target] == nil {
		r.votes[v.Target] = make(map[uint64]bool)
	}
	r.votes[v.Target][v.Proof.ValidatorIndex] = true
	if len(r.votes[v.Target])*3 < len(r.config.Validators)*2 {
		return nil
	}
	r.justified[v.Target] = true
	if v.TargetSlot > r.status.JustifiedSlot {
		r.status.JustifiedSlot, r.status.JustifiedRoot = v.TargetSlot, v.Target
	}
	consecutive := true
	for candidate := v.SourceSlot + 1; candidate < v.TargetSlot; candidate++ {
		if IsJustifiableSlot(r.status.FinalizedSlot, candidate) {
			consecutive = false
			break
		}
	}
	if consecutive && v.SourceSlot > r.status.FinalizedSlot {
		if r.onFinalized != nil {
			if err := r.onFinalized(v.Source, v.Target); err != nil {
				return err
			}
		}
		r.status.FinalizedSlot, r.status.FinalizedRoot = v.SourceSlot, v.Source
		for root, block := range r.blocks {
			if block.slot < v.SourceSlot {
				delete(r.blocks, root)
				delete(r.justified, root)
				delete(r.votes, root)
			}
		}
	}
	return nil
}
