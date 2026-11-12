package consensus

import (
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/pqc"
)

func TestNativeRuntimeThreeSlotPQFinality(t *testing.T) {
	cfg := NativeConfig{SlotMillis: 6000, SlotsPerEpoch: 32, Validators: make(map[uint64][]byte)}
	keys := make([]*pqc.DilithiumKeyPair, 3)
	for i := range keys {
		var err error
		keys[i], err = pqc.GenerateDilithiumKey()
		if err != nil {
			t.Fatal(err)
		}
		cfg.Validators[uint64(i)] = keys[i].PublicKey
	}
	genesis := types.BytesToHash([]byte{1})
	first := types.BytesToHash([]byte{2})
	second := types.BytesToHash([]byte{3})
	finalized := types.Hash{}
	runtime, err := NewNodeRuntime(cfg, genesis, time.Now().Add(-12*time.Second), genesis, 0, nil, func(root, safe types.Hash) error { finalized = root; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.ObserveBlock(first, genesis, 1); err != nil {
		t.Fatal(err)
	}
	if err := runtime.ObserveBlock(second, first, 2); err != nil {
		t.Fatal(err)
	}
	runtime.Start()
	defer runtime.Stop()
	bad, err := SignNativeVote(genesis, genesis, first, 0, 1, 1, 0, keys[0])
	if err != nil {
		t.Fatal(err)
	}
	bad.Proof.ValidatorIndex = 1
	if err := runtime.Submit(bad); err == nil {
		t.Fatal("validator identity substitution accepted")
	}
	crossChain, _ := SignNativeVote(second, genesis, first, 0, 1, 1, 0, keys[0])
	if err := runtime.Submit(crossChain); err == nil {
		t.Fatal("cross-chain signature accepted")
	}
	for i := 0; i < 2; i++ {
		vote, _ := SignNativeVote(genesis, genesis, first, 0, 1, 1, uint64(i), keys[i])
		if err := runtime.Submit(vote); err != nil {
			t.Fatal(err)
		}
		if i == 0 && runtime.Status().JustifiedRoot != genesis {
			t.Fatal("minority justified target")
		}
	}
	if runtime.Status().JustifiedRoot != first || runtime.Status().FinalizedRoot != genesis {
		t.Fatal("first quorum must justify target without finalizing it")
	}
	for i := 0; i < 2; i++ {
		vote, _ := SignNativeVote(genesis, first, second, 1, 2, 2, uint64(i), keys[i])
		if err := runtime.Submit(vote); err != nil {
			t.Fatal(err)
		}
	}
	if finalized != first || runtime.Status().FinalizedSlot != 1 {
		t.Fatal("consecutive quorum did not finalize source")
	}
	runtime.Stop()
	vote, _ := SignNativeVote(genesis, first, second, 1, 2, 2, 2, keys[2])
	if err := runtime.Submit(vote); err == nil {
		t.Fatal("stopped runtime accepted vote")
	}
}
