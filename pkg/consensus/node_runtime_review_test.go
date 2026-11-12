package consensus

import (
	"errors"
	"testing"
	"time"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/crypto/pqc"
)

func reviewObserver(t *testing.T, callback func(types.Hash, types.Hash) error) (*NodeRuntime, []*pqc.DilithiumKeyPair, []types.Hash) {
	t.Helper()
	cfg := NativeConfig{SlotMillis: 60000, SlotsPerEpoch: 32, Validators: make(map[uint64][]byte)}
	keys := make([]*pqc.DilithiumKeyPair, 3)
	for i := range keys {
		var err error
		keys[i], err = pqc.GenerateDilithiumKey()
		if err != nil {
			t.Fatal(err)
		}
		cfg.Validators[uint64(i)] = keys[i].PublicKey
	}
	roots := []types.Hash{types.BytesToHash([]byte{1}), types.BytesToHash([]byte{2}), types.BytesToHash([]byte{3})}
	runtime, err := NewNodeRuntime(cfg, roots[0], time.Now().Add(-120*time.Second), roots[0], 0, nil, callback)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(roots); i++ {
		if err := runtime.ObserveBlock(roots[i], roots[i-1], uint64(i)); err != nil {
			t.Fatal(err)
		}
	}
	runtime.Start()
	t.Cleanup(runtime.Stop)
	return runtime, keys, roots
}

func TestNativeObserverDoesNotMixSourceQuorums(t *testing.T) {
	runtime, keys, roots := reviewObserver(t, nil)
	vote := func(source, target, id int, slot uint64) {
		t.Helper()
		v, err := SignNativeVote(runtime.Domain(), roots[source], roots[target], uint64(source), uint64(target), slot, uint64(id), keys[id])
		if err != nil {
			t.Fatal(err)
		}
		if err := runtime.Submit(v); err != nil {
			t.Fatal(err)
		}
	}
	vote(0, 1, 0, 1)
	vote(0, 1, 1, 1)
	vote(0, 2, 0, 2)
	vote(0, 2, 1, 2)
	vote(1, 2, 2, 2)
	if runtime.Status().FinalizedRoot != roots[0] {
		t.Fatal("a minority source link reused another link's quorum to finalize")
	}
}

func TestNativeObserverCallbackFailureCanRetry(t *testing.T) {
	injected := errors.New("publication failed")
	calls := 0
	runtime, keys, roots := reviewObserver(t, func(types.Hash, types.Hash) error {
		calls++
		if calls == 1 {
			return injected
		}
		return nil
	})
	for id := 0; id < 2; id++ {
		v, _ := SignNativeVote(runtime.Domain(), roots[0], roots[1], 0, 1, 1, uint64(id), keys[id])
		if err := runtime.Submit(v); err != nil {
			t.Fatal(err)
		}
	}
	first, _ := SignNativeVote(runtime.Domain(), roots[1], roots[2], 1, 2, 2, 0, keys[0])
	if err := runtime.Submit(first); err != nil {
		t.Fatal(err)
	}
	deciding, _ := SignNativeVote(runtime.Domain(), roots[1], roots[2], 1, 2, 2, 1, keys[1])
	if err := runtime.Submit(deciding); !errors.Is(err, injected) {
		t.Fatalf("want injected failure, got %v", err)
	}
	if runtime.Status().Verified != 3 || runtime.Status().JustifiedRoot != roots[1] {
		t.Fatal("failed callback consumed deciding vote")
	}
	if err := runtime.Submit(deciding); err != nil {
		t.Fatal(err)
	}
	if runtime.Status().FinalizedRoot != roots[1] || calls != 2 {
		t.Fatal("retry failed to finalize")
	}
	if err := runtime.Submit(deciding); !errors.Is(err, ErrNativeDuplicateVote) {
		t.Fatalf("duplicate should be identifiable and harmless: %v", err)
	}
}
