package state

import (
	"math/big"
	"sync"
	"testing"

	"github.com/eth2030/eth2030/core/types"
)

// setupAdvancer creates a StateAdvancer with a registered parent state.
func setupAdvancer(t *testing.T) (*StateAdvancer, types.Hash) {
	t.Helper()
	sa := NewStateAdvancer(DefaultAdvancerConfig())

	db := NewMemoryStateDB()
	addr := types.HexToAddress("0xaabb")
	db.CreateAccount(addr)
	db.AddBalance(addr, big.NewInt(1e18))
	db.SetNonce(addr, 10)

	root, err := db.Commit()
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	sa.RegisterState(root, db)
	return sa, root
}

func TestNewStateAdvancer(t *testing.T) {
	sa := NewStateAdvancer(AdvancerConfig{})
	if sa == nil {
		t.Fatal("NewStateAdvancer returned nil")
	}
	if sa.config.MaxSpeculations != DefaultAdvancerConfig().MaxSpeculations {
		t.Errorf("expected default MaxSpeculations %d, got %d",
			DefaultAdvancerConfig().MaxSpeculations, sa.config.MaxSpeculations)
	}
}

func TestNewStateAdvancerCustomConfig(t *testing.T) {
	cfg := AdvancerConfig{
		MaxSpeculations:      10,
		MaxTxsPerSpeculation: 50,
		CacheSize:            32,
		SpeculationDepth:     2,
	}
	sa := NewStateAdvancer(cfg)
	if sa.config.MaxSpeculations != 10 {
		t.Errorf("MaxSpeculations: want 10, got %d", sa.config.MaxSpeculations)
	}
	if sa.config.MaxTxsPerSpeculation != 50 {
		t.Errorf("MaxTxsPerSpeculation: want 50, got %d", sa.config.MaxTxsPerSpeculation)
	}
}

func TestSpeculateBlock(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	txs := [][]byte{
		{0x01, 0x02, 0x03},
		{0x04, 0x05, 0x06},
	}

	spec, err := sa.SpeculateBlock(parentRoot, txs)
	if err != nil {
		t.Fatalf("SpeculateBlock: %v", err)
	}
	if spec == nil {
		t.Fatal("expected non-nil speculative state")
	}
	if spec.Root.IsZero() {
		t.Error("speculative root should not be zero")
	}
	if spec.ParentRoot != parentRoot {
		t.Error("parent root mismatch")
	}
	if spec.GasUsed != 42000 { // 2 txs * 21000
		t.Errorf("gas used: want 42000, got %d", spec.GasUsed)
	}
	if len(spec.Receipts) != 2 {
		t.Errorf("receipts: want 2, got %d", len(spec.Receipts))
	}
	if spec.IsValid {
		t.Error("speculation should not be valid until validated")
	}
}

func TestSpeculateBlockNoParent(t *testing.T) {
	sa := NewStateAdvancer(DefaultAdvancerConfig())

	_, err := sa.SpeculateBlock(types.HexToHash("0xdead"), [][]byte{{0x01}})
	if err != ErrAdvancerNoParent {
		t.Errorf("expected ErrAdvancerNoParent, got %v", err)
	}
}

func TestSpeculateBlockTooManyTxs(t *testing.T) {
	cfg := AdvancerConfig{MaxTxsPerSpeculation: 2}
	sa := NewStateAdvancer(cfg)

	db := NewMemoryStateDB()
	root, _ := db.Commit()
	sa.RegisterState(root, db)

	txs := [][]byte{{0x01}, {0x02}, {0x03}}
	_, err := sa.SpeculateBlock(root, txs)
	if err != ErrAdvancerTooManyTxs {
		t.Errorf("expected ErrAdvancerTooManyTxs, got %v", err)
	}
}

func TestSpeculateBlockEmptyTx(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	_, err := sa.SpeculateBlock(parentRoot, [][]byte{{}})
	if err != ErrAdvancerInvalidTx {
		t.Errorf("expected ErrAdvancerInvalidTx, got %v", err)
	}
}

func TestValidateSpeculation(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	spec, err := sa.SpeculateBlock(parentRoot, [][]byte{{0x01}})
	if err != nil {
		t.Fatalf("SpeculateBlock: %v", err)
	}

	// Correct validation.
	if !sa.ValidateSpeculation(spec, spec.Root) {
		t.Error("speculation should validate against its own root")
	}
	if !spec.IsValid {
		t.Error("spec.IsValid should be true after successful validation")
	}

	// Wrong root.
	spec2, _ := sa.SpeculateBlock(parentRoot, [][]byte{{0x02}})
	if sa.ValidateSpeculation(spec2, types.HexToHash("0xbad")) {
		t.Error("speculation should not validate against wrong root")
	}
	if spec2.IsValid {
		t.Error("spec.IsValid should remain false after failed validation")
	}
}

func TestValidateSpeculationNil(t *testing.T) {
	sa := NewStateAdvancer(DefaultAdvancerConfig())
	if sa.ValidateSpeculation(nil, types.Hash{}) {
		t.Error("nil speculation should not validate")
	}
}

func TestPrecomputeState(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	txs := make([][]byte, 8)
	for i := range txs {
		txs[i] = []byte{byte(i + 1)}
	}

	results, err := sa.PrecomputeState(parentRoot, txs)
	if err != nil {
		t.Fatalf("PrecomputeState: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one speculative state")
	}

	// Each result should have increasing gas (more txs).
	for i := 1; i < len(results); i++ {
		if results[i].GasUsed < results[i-1].GasUsed {
			t.Errorf("results[%d].GasUsed (%d) < results[%d].GasUsed (%d)",
				i, results[i].GasUsed, i-1, results[i-1].GasUsed)
		}
	}
}

func TestPrecomputeStateEmpty(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	results, err := sa.PrecomputeState(parentRoot, nil)
	if err != nil {
		t.Fatalf("PrecomputeState: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil for empty pending txs, got %d results", len(results))
	}
}

func TestGetBestSpeculation(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	// Create speculations with different tx counts.
	sa.SpeculateBlock(parentRoot, [][]byte{{0x01}})
	sa.SpeculateBlock(parentRoot, [][]byte{{0x01}, {0x02}, {0x03}})
	sa.SpeculateBlock(parentRoot, [][]byte{{0x01}, {0x02}})

	best, ok := sa.GetBestSpeculation(parentRoot)
	if !ok {
		t.Fatal("expected to find best speculation")
	}
	// Best should be the one with 3 txs (63000 gas).
	if best.GasUsed != 63000 {
		t.Errorf("best gas: want 63000, got %d", best.GasUsed)
	}
}

func TestGetBestSpeculationMiss(t *testing.T) {
	sa := NewStateAdvancer(DefaultAdvancerConfig())

	_, ok := sa.GetBestSpeculation(types.HexToHash("0xdead"))
	if ok {
		t.Error("expected miss for unknown parent root")
	}
}

func TestPurgeSpeculations(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	// Create a speculation with low gas (21000).
	sa.SpeculateBlock(parentRoot, [][]byte{{0x01}})
	if sa.ActiveSpeculations() == 0 {
		t.Fatal("expected at least 1 active speculation")
	}

	// Purge everything with gas < 50000.
	sa.PurgeSpeculations(50000)

	// The speculation (21000 gas) should be gone.
	_, ok := sa.GetBestSpeculation(parentRoot)
	if ok {
		t.Error("expected speculation to be purged")
	}
}

func TestPurgeSpeculationsKeepsHighGas(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	// Create a speculation with higher gas (3 txs = 63000).
	sa.SpeculateBlock(parentRoot, [][]byte{{0x01}, {0x02}, {0x03}})

	// Purge entries with gas < 50000; this should keep our 63000 entry.
	sa.PurgeSpeculations(50000)

	_, ok := sa.GetBestSpeculation(parentRoot)
	if !ok {
		t.Error("speculation with sufficient gas should not be purged")
	}
}

func TestCacheHitRate(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	// Initially zero.
	if rate := sa.CacheHitRate(); rate != 0 {
		t.Errorf("initial hit rate: want 0, got %f", rate)
	}

	sa.SpeculateBlock(parentRoot, [][]byte{{0x01}})

	// One hit.
	sa.GetBestSpeculation(parentRoot)
	// One miss.
	sa.GetBestSpeculation(types.HexToHash("0xdead"))

	rate := sa.CacheHitRate()
	if rate < 0.49 || rate > 0.51 {
		t.Errorf("hit rate: want ~0.5, got %f", rate)
	}
}

func TestActiveSpeculations(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	if n := sa.ActiveSpeculations(); n != 0 {
		t.Errorf("initial active: want 0, got %d", n)
	}

	sa.SpeculateBlock(parentRoot, [][]byte{{0x01}})
	if n := sa.ActiveSpeculations(); n != 1 {
		t.Errorf("after 1 speculation: want 1, got %d", n)
	}

	// Different parent root counts as same cache entry.
	sa.SpeculateBlock(parentRoot, [][]byte{{0x02}})
	if n := sa.ActiveSpeculations(); n != 1 {
		t.Errorf("after 2nd speculation with same parent: want 1, got %d", n)
	}
}

func TestChainedSpeculations(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	// First speculation.
	spec1, err := sa.SpeculateBlock(parentRoot, [][]byte{{0x01}})
	if err != nil {
		t.Fatalf("spec 1: %v", err)
	}

	// Chain: speculate on top of the first speculation's root.
	spec2, err := sa.SpeculateBlock(spec1.Root, [][]byte{{0x02}})
	if err != nil {
		t.Fatalf("spec 2: %v", err)
	}
	if spec2.ParentRoot != spec1.Root {
		t.Error("chained speculation should reference first spec's root")
	}
	if spec2.Root == spec1.Root {
		t.Error("chained speculation should produce a different root")
	}
}

func TestMaxSpeculationsReached(t *testing.T) {
	cfg := AdvancerConfig{
		MaxSpeculations:      2,
		MaxTxsPerSpeculation: 100,
		CacheSize:            2,
	}
	sa := NewStateAdvancer(cfg)

	db := NewMemoryStateDB()
	root, _ := db.Commit()
	sa.RegisterState(root, db)

	// Fill cache.
	_, err := sa.SpeculateBlock(root, [][]byte{{0x01}})
	if err != nil {
		t.Fatal(err)
	}

	// Second unique parent.
	db2 := NewMemoryStateDB()
	db2.CreateAccount(types.HexToAddress("0x01"))
	root2, _ := db2.Commit()
	sa.RegisterState(root2, db2)

	_, err = sa.SpeculateBlock(root2, [][]byte{{0x02}})
	if err != nil {
		t.Fatal(err)
	}

	// Third should fail.
	db3 := NewMemoryStateDB()
	db3.CreateAccount(types.HexToAddress("0x02"))
	root3, _ := db3.Commit()
	sa.RegisterState(root3, db3)

	_, err = sa.SpeculateBlock(root3, [][]byte{{0x03}})
	if err != ErrAdvancerMaxReached {
		t.Errorf("expected ErrAdvancerMaxReached, got %v", err)
	}
}

func TestSpeculationCacheLRU(t *testing.T) {
	cache := newSpeculationCache(2)

	key1 := types.HexToHash("0x01")
	key2 := types.HexToHash("0x02")
	key3 := types.HexToHash("0x03")

	spec := []*SpeculativeState{{GasUsed: 100}}

	cache.put(key1, spec)
	cache.put(key2, spec)

	// Access key1 to make it more recent.
	cache.get(key1)

	// Insert key3 which should evict key2 (least recently used).
	cache.put(key3, spec)

	if _, ok := cache.get(key2); ok {
		t.Error("key2 should have been evicted")
	}
	if _, ok := cache.get(key1); !ok {
		t.Error("key1 should still be in cache")
	}
	if _, ok := cache.get(key3); !ok {
		t.Error("key3 should be in cache")
	}
}

func TestSpeculationCacheRemove(t *testing.T) {
	cache := newSpeculationCache(10)
	key := types.HexToHash("0x01")
	cache.put(key, []*SpeculativeState{{GasUsed: 1}})
	cache.remove(key)
	if _, ok := cache.get(key); ok {
		t.Error("key should be removed")
	}
}

func TestConcurrentSpeculateBlock(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tx := []byte{byte(i + 1)}
			_, err := sa.SpeculateBlock(parentRoot, [][]byte{tx})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent speculation failed: %v", err)
		}
	}
	if specs, ok := sa.cache.get(parentRoot); !ok || len(specs) != 20 {
		t.Fatalf("concurrent cache updates lost speculations: %d", len(specs))
	}

	// Should have some speculations cached.
	if sa.ActiveSpeculations() == 0 {
		t.Error("expected at least one active speculation after concurrent calls")
	}
}

func TestConcurrentValidation(t *testing.T) {
	sa, parentRoot := setupAdvancer(t)

	specs := make([]*SpeculativeState, 10)
	for i := 0; i < 10; i++ {
		s, err := sa.SpeculateBlock(parentRoot, [][]byte{[]byte{byte(i + 1)}})
		if err != nil {
			t.Fatalf("spec %d: %v", i, err)
		}
		specs[i] = s
	}

	var wg sync.WaitGroup
	for _, spec := range specs {
		wg.Add(1)
		go func(s *SpeculativeState) {
			defer wg.Done()
			sa.ValidateSpeculation(s, s.Root)
		}(spec)
	}
	wg.Wait()
}

func TestRegisterState(t *testing.T) {
	sa := NewStateAdvancer(DefaultAdvancerConfig())

	db := NewMemoryStateDB()
	db.CreateAccount(types.HexToAddress("0x01"))
	root, _ := db.Commit()

	sa.RegisterState(root, db)

	// Should be able to speculate on this root now.
	spec, err := sa.SpeculateBlock(root, [][]byte{{0xAA}})
	if err != nil {
		t.Fatalf("SpeculateBlock: %v", err)
	}
	if spec.ParentRoot != root {
		t.Error("parent root mismatch")
	}
}

func TestDeterministicSpeculation(t *testing.T) {
	// Two identical speculations should produce the same root.
	sa1, root1 := setupAdvancer(t)
	sa2, root2 := setupAdvancer(t)

	if root1 != root2 {
		t.Fatal("setup roots differ; test is invalid")
	}

	tx := [][]byte{{0x01, 0x02}}
	spec1, err := sa1.SpeculateBlock(root1, tx)
	if err != nil {
		t.Fatal(err)
	}
	spec2, err := sa2.SpeculateBlock(root2, tx)
	if err != nil {
		t.Fatal(err)
	}

	if spec1.Root != spec2.Root {
		t.Errorf("deterministic speculation failed: %s != %s", spec1.Root, spec2.Root)
	}
	if spec1.GasUsed != spec2.GasUsed {
		t.Errorf("gas mismatch: %d != %d", spec1.GasUsed, spec2.GasUsed)
	}
}

func TestPrecomputeStateSingleTx(t *testing.T) {
	sa, root := setupAdvancer(t)
	results, err := sa.PrecomputeState(root, [][]byte{{0x01}})
	if err != nil {
		t.Fatalf("PrecomputeState: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}
	for _, r := range results {
		if r.GasUsed != 21000 {
			t.Errorf("expected 21000 gas, got %d", r.GasUsed)
		}
	}
}
