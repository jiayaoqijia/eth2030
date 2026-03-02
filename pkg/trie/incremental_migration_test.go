package trie

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eth2030/eth2030/crypto"
)

// buildMPT creates an MPT with n accounts keyed by sequential byte values.
func buildMPT(n int) *Trie {
	mpt := New()
	for i := 0; i < n; i++ {
		key := fmt.Appendf(nil, "account_%05d", i)
		val := fmt.Appendf(nil, "balance_%05d", i)
		mpt.Put(key, val)
	}
	return mpt
}

// TestIncrementalMigration_10KAccounts_1KPerBlock migrates 10K accounts in
// batches of 1K and verifies all are migrated in exactly 10 blocks.
func TestIncrementalMigration_10KAccounts_1KPerBlock(t *testing.T) {
	mpt := buildMPT(10000)
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 1000}
	m := NewIncrementalMigration(mpt, bt, config)

	blocks := 0
	totalMigrated := 0
	for !m.IsDone() {
		migrated, done, err := m.MigrateBlock(1000)
		if err != nil {
			t.Fatalf("block %d error: %v", blocks, err)
		}
		totalMigrated += migrated
		blocks++
		if done {
			break
		}
	}

	if blocks != 10 {
		t.Errorf("expected 10 blocks, got %d", blocks)
	}
	if totalMigrated != 10000 {
		t.Errorf("expected 10000 migrated, got %d", totalMigrated)
	}
	if !m.IsDone() {
		t.Error("expected migration to be done")
	}

	// Verify every account is in the binary trie.
	for i := 0; i < 10000; i++ {
		key := fmt.Appendf(nil, "account_%05d", i)
		hk := crypto.Keccak256Hash(key)
		val, err := bt.GetHashed(hk)
		if err != nil {
			t.Fatalf("account %d not found in bt: %v", i, err)
		}
		expected := fmt.Appendf(nil, "balance_%05d", i)
		if !bytesEqual(val, expected) {
			t.Fatalf("account %d value mismatch", i)
		}
	}
}

// TestIncrementalMigration_InterruptAndResume interrupts at block 5 and
// resumes from the persisted cursor.
func TestIncrementalMigration_InterruptAndResume(t *testing.T) {
	mpt := buildMPT(10000)
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 1000}
	m := NewIncrementalMigration(mpt, bt, config)

	// Migrate 5 blocks.
	for i := 0; i < 5; i++ {
		_, _, err := m.MigrateBlock(1000)
		if err != nil {
			t.Fatalf("block %d: %v", i, err)
		}
	}

	migrated1, total1, pct1 := m.Progress()
	if migrated1 != 5000 {
		t.Fatalf("expected 5000 migrated, got %d", migrated1)
	}
	if total1 != 10000 {
		t.Fatalf("expected 10000 total, got %d", total1)
	}
	if pct1 != 50.0 {
		t.Fatalf("expected 50%% progress, got %.1f%%", pct1)
	}

	// Save cursor.
	cursor := m.GetCursor()
	if cursor == nil {
		t.Fatal("cursor should not be nil after 5 blocks")
	}

	// Simulate restart: create a new migration with the same bt.
	m2 := NewIncrementalMigration(mpt, bt, config)
	m2.SetCursor(cursor)

	migrated2, _, _ := m2.Progress()
	if migrated2 != 5000 {
		t.Fatalf("after restore, expected 5000 migrated, got %d", migrated2)
	}

	// Complete migration.
	for !m2.IsDone() {
		_, _, err := m2.MigrateBlock(1000)
		if err != nil {
			t.Fatal(err)
		}
	}

	migrated3, _, pct3 := m2.Progress()
	if migrated3 != 10000 || pct3 != 100.0 {
		t.Fatalf("expected 10000/100%%, got %d/%.1f%%", migrated3, pct3)
	}
}

// TestIncrementalMigration_DualWrite verifies that new accounts written
// after partial migration appear in both tries.
func TestIncrementalMigration_DualWrite(t *testing.T) {
	mpt := buildMPT(100)
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 50, DualWriteEnabled: true}
	m := NewIncrementalMigration(mpt, bt, config)

	// Migrate first batch (50 accounts).
	m.MigrateBlock(50)

	// Create dual-write manager.
	dw := NewDualWriteStateManager(m)

	// Write a new account through dual-write.
	newKey := []byte("new_account")
	newVal := []byte("new_balance")
	if err := dw.Put(newKey, newVal); err != nil {
		t.Fatalf("dual write put: %v", err)
	}

	// Verify the new account is in MPT.
	mptVal, err := mpt.Get(newKey)
	if err != nil {
		t.Fatalf("new account not in mpt: %v", err)
	}
	if !bytesEqual(mptVal, newVal) {
		t.Error("mpt value mismatch for new account")
	}

	// Verify the new account is in binary trie.
	hk := crypto.Keccak256Hash(newKey)
	btVal, err := bt.GetHashed(hk)
	if err != nil {
		t.Fatalf("new account not in bt: %v", err)
	}
	if !bytesEqual(btVal, newVal) {
		t.Error("bt value mismatch for new account")
	}

	// Test reading through dual-write manager.
	val, err := dw.Get(newKey)
	if err != nil {
		t.Fatalf("dual write get: %v", err)
	}
	if !bytesEqual(val, newVal) {
		t.Error("dual write get value mismatch")
	}
}

// TestIncrementalMigration_RootHashMatchesOneShot verifies that incremental
// migration produces the same root hash as one-shot migration.
func TestIncrementalMigration_RootHashMatchesOneShot(t *testing.T) {
	mpt := buildMPT(500)

	// One-shot migration.
	oneShotBT := MigrateFromMPT(mpt)
	oneShotHash := oneShotBT.Hash()

	// Incremental migration (100 per block).
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 100}
	m := NewIncrementalMigration(mpt, bt, config)
	for !m.IsDone() {
		m.MigrateBlock(100)
	}
	incrementalHash := bt.Hash()

	if oneShotHash != incrementalHash {
		t.Fatalf("root hash mismatch: oneshot=%x, incremental=%x", oneShotHash, incrementalHash)
	}
}

// TestMigrationVerifier_DetectsCorruption injects corruption and verifies
// the verifier catches it.
func TestMigrationVerifier_DetectsCorruption(t *testing.T) {
	mpt := buildMPT(100)
	bt := MigrateFromMPT(mpt)

	v := NewMigrationVerifier()

	// Clean migration should pass.
	errs, err := v.VerifyMigration(mpt, bt)
	if err != nil {
		t.Fatalf("clean verification failed: %v (errors: %v)", err, errs)
	}

	// Inject corruption: overwrite a known key with wrong value.
	corruptKey := []byte("account_00050")
	hk := crypto.Keccak256Hash(corruptKey)
	bt.PutHashed(hk, []byte("CORRUPTED"))

	errs, err = v.VerifyMigration(mpt, bt)
	if err == nil {
		t.Fatal("expected verification to fail after corruption")
	}
	if len(errs) == 0 {
		t.Fatal("expected at least one error description")
	}
}

// TestMigrationVerifier_DetectsExtraKey verifies the verifier detects
// extra keys in the binary trie.
func TestMigrationVerifier_DetectsExtraKey(t *testing.T) {
	mpt := buildMPT(10)
	bt := MigrateFromMPT(mpt)

	// Add an extra key to the binary trie.
	extraHK := crypto.Keccak256Hash([]byte("extra_key"))
	bt.PutHashed(extraHK, []byte("extra_value"))

	v := NewMigrationVerifier()
	errs, err := v.VerifyMigration(mpt, bt)
	if err == nil {
		t.Fatal("expected verification to fail with extra key")
	}
	found := false
	for _, e := range errs {
		if len(e) > 0 {
			found = true
		}
	}
	if !found {
		t.Fatal("expected error description about count mismatch")
	}
}

// TestMigrationVerifier_VerifyAccount tests single-account verification.
func TestMigrationVerifier_VerifyAccount(t *testing.T) {
	mpt := buildMPT(10)
	bt := MigrateFromMPT(mpt)

	v := NewMigrationVerifier()

	// Existing account should verify.
	if err := v.VerifyAccount([]byte("account_00005"), mpt, bt); err != nil {
		t.Fatalf("VerifyAccount for existing key: %v", err)
	}

	// Non-existent key should verify (absent in both).
	if err := v.VerifyAccount([]byte("nonexistent"), mpt, bt); err != nil {
		t.Fatalf("VerifyAccount for nonexistent key: %v", err)
	}
}

// TestMigrationRollback rolls back migration to MPT-only state.
func TestMigrationRollback(t *testing.T) {
	mpt := buildMPT(100)
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 50}
	m := NewIncrementalMigration(mpt, bt, config)

	// Migrate first batch.
	m.MigrateBlock(50)
	if m.Dest().Len() == 0 {
		t.Fatal("expected non-empty binary trie after migration")
	}

	// Rollback.
	v := NewMigrationVerifier()
	if err := v.Rollback(m); err != nil {
		t.Fatalf("rollback error: %v", err)
	}

	// After rollback, dest trie should be empty and cursor nil.
	if m.Dest().Len() != 0 {
		t.Errorf("expected empty binary trie after rollback, got %d", m.Dest().Len())
	}
	if m.GetCursor() != nil {
		t.Error("expected nil cursor after rollback")
	}
	if m.IsDone() {
		t.Error("expected not done after rollback")
	}

	// Can re-migrate after rollback.
	m.MigrateBlock(100)
	if m.Dest().Len() != 100 {
		t.Errorf("expected 100 after re-migration, got %d", m.Dest().Len())
	}
}

// TestMigrationMetrics verifies metrics report non-zero values during migration.
func TestMigrationMetrics(t *testing.T) {
	mpt := buildMPT(100)
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 30}
	m := NewIncrementalMigration(mpt, bt, config)

	collector := NewMigrationMetricsCollector(m)

	// Before migration.
	collector.Collect()
	metrics := collector.Metrics()
	if metrics.AccountsMigrated.Get() != 0 {
		t.Error("expected 0 migrated before start")
	}
	if metrics.EstimatedBlocksRemaining <= 0 {
		t.Error("expected positive estimated blocks before start")
	}

	// Migrate one batch.
	m.MigrateBlock(30)

	collector.Collect()
	metrics = collector.Metrics()
	if metrics.AccountsMigrated.Get() == 0 {
		t.Error("expected non-zero accounts migrated")
	}
	if metrics.MigrationPercentage.Get() <= 0.0 {
		t.Error("expected positive migration percentage")
	}
	if metrics.PhaseDuration <= 0 {
		t.Error("expected positive phase duration")
	}

	// Complete migration.
	for !m.IsDone() {
		m.MigrateBlock(30)
	}

	collector.Collect()
	metrics = collector.Metrics()
	if metrics.MigrationPercentage.Get() != 100.0 {
		t.Errorf("expected 100%% migration, got %.1f%%", metrics.MigrationPercentage.Get())
	}
	if metrics.EstimatedBlocksRemaining != 0 {
		t.Errorf("expected 0 remaining blocks, got %d", metrics.EstimatedBlocksRemaining)
	}
}

// TestMigrationStatusJSON verifies the JSON status endpoint.
func TestMigrationStatusJSON(t *testing.T) {
	mpt := buildMPT(50)
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 25}
	m := NewIncrementalMigration(mpt, bt, config)

	collector := NewMigrationMetricsCollector(m)

	m.MigrateBlock(25)

	data, err := collector.MigrationStatusJSON()
	if err != nil {
		t.Fatalf("MigrationStatusJSON error: %v", err)
	}

	var status map[string]interface{}
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	if status["accounts_migrated"].(float64) != 25 {
		t.Errorf("expected 25 accounts_migrated, got %v", status["accounts_migrated"])
	}
	if status["migration_percentage"].(float64) != 50.0 {
		t.Errorf("expected 50%% migration, got %v", status["migration_percentage"])
	}
	if status["is_done"].(bool) {
		t.Error("expected is_done=false")
	}
}

// TestIncrementalMigration_Progress tracks progress correctly.
func TestIncrementalMigration_Progress(t *testing.T) {
	mpt := buildMPT(200)
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 100}
	m := NewIncrementalMigration(mpt, bt, config)

	migrated, total, pct := m.Progress()
	if migrated != 0 || total != 200 || pct != 0.0 {
		t.Fatalf("initial progress: migrated=%d, total=%d, pct=%.1f", migrated, total, pct)
	}

	m.MigrateBlock(100)
	migrated, total, pct = m.Progress()
	if migrated != 100 || total != 200 || pct != 50.0 {
		t.Fatalf("half progress: migrated=%d, total=%d, pct=%.1f", migrated, total, pct)
	}

	m.MigrateBlock(100)
	migrated, total, pct = m.Progress()
	if migrated != 200 || total != 200 || pct != 100.0 {
		t.Fatalf("full progress: migrated=%d, total=%d, pct=%.1f", migrated, total, pct)
	}
}

// TestIncrementalMigration_EmptyTrie handles an empty source trie.
func TestIncrementalMigration_EmptyTrie(t *testing.T) {
	mpt := New()
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 100}
	m := NewIncrementalMigration(mpt, bt, config)

	migrated, done, err := m.MigrateBlock(100)
	if err != nil {
		t.Fatal(err)
	}
	if migrated != 0 || !done {
		t.Fatalf("expected 0/true for empty trie, got %d/%v", migrated, done)
	}
}

// TestIncrementalMigration_AlreadyDone calling MigrateBlock after done is a no-op.
func TestIncrementalMigration_AlreadyDone(t *testing.T) {
	mpt := buildMPT(10)
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 100}
	m := NewIncrementalMigration(mpt, bt, config)

	m.MigrateBlock(100)
	if !m.IsDone() {
		t.Fatal("expected done")
	}

	migrated, done, err := m.MigrateBlock(100)
	if err != nil {
		t.Fatal(err)
	}
	if migrated != 0 || !done {
		t.Fatalf("expected 0/true for already-done, got %d/%v", migrated, done)
	}
}

// TestDualWriteStateManager_IsAccountMigrated tests cursor-based migration check.
func TestDualWriteStateManager_IsAccountMigrated(t *testing.T) {
	mpt := buildMPT(100)
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 50}
	m := NewIncrementalMigration(mpt, bt, config)

	dw := NewDualWriteStateManager(m)

	// Before migration, no accounts migrated.
	hk := crypto.Keccak256Hash([]byte("account_00000"))
	if dw.IsAccountMigrated(hk[:]) {
		t.Error("no accounts should be migrated before start")
	}

	// Migrate first batch.
	m.MigrateBlock(50)

	// After migration, the cursor is set; some accounts should be migrated.
	cursor := m.GetCursor()
	if cursor == nil {
		t.Fatal("cursor should be non-nil after migration")
	}
}

// TestMigrationPlannerWithIncrementalEngine tests ExecutePhase with a real
// source trie attached.
func TestMigrationPlannerWithIncrementalEngine(t *testing.T) {
	mpt := buildMPT(100)
	bt := NewBinaryTrie()

	mp := NewMigrationPlanner(&PlannerConfig{
		BatchSize:         50,
		EstimatedAccounts: 100,
	})
	mp.SetSourceTrie(mpt, bt)

	var root [32]byte
	plan, err := mp.CreatePlan(root)
	if err != nil {
		t.Fatal(err)
	}

	result, err := mp.ExecutePhase(plan.ID, 0)
	if err != nil {
		t.Fatalf("ExecutePhase: %v", err)
	}
	if result.AccountsMigrated == 0 {
		t.Error("expected non-zero accounts migrated with real trie")
	}
}

// TestMigrateFromMPT_BackwardCompat verifies the one-shot function still works.
func TestMigrateFromMPT_BackwardCompat(t *testing.T) {
	mpt := buildMPT(50)
	bt := MigrateFromMPT(mpt)

	if bt.Len() != 50 {
		t.Fatalf("expected 50 entries in binary trie, got %d", bt.Len())
	}

	// Verify a sample key.
	key := []byte("account_00025")
	hk := crypto.Keccak256Hash(key)
	val, err := bt.GetHashed(hk)
	if err != nil {
		t.Fatalf("key not found: %v", err)
	}
	expected := []byte("balance_00025")
	if !bytesEqual(val, expected) {
		t.Fatalf("value mismatch: got %s, want %s", val, expected)
	}
}

// TestCounterAndGauge tests the metric primitives.
func TestCounterAndGauge(t *testing.T) {
	c := &Counter{}
	c.Inc()
	c.Inc()
	c.Add(3)
	if c.Get() != 5 {
		t.Errorf("counter: expected 5, got %d", c.Get())
	}

	g := &Gauge{}
	g.Set(42.5)
	if g.Get() != 42.5 {
		t.Errorf("gauge set: expected 42.5, got %f", g.Get())
	}
	g.Inc()
	if g.Get() != 43.5 {
		t.Errorf("gauge inc: expected 43.5, got %f", g.Get())
	}
}

// TestIncrementalMigration_DefaultBatchSize verifies default batch size.
func TestIncrementalMigration_DefaultBatchSize(t *testing.T) {
	mpt := buildMPT(10)
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{} // zero BatchSize
	m := NewIncrementalMigration(mpt, bt, config)

	if m.config.BatchSize != 1000 {
		t.Errorf("expected default batch size 1000, got %d", m.config.BatchSize)
	}
}

// TestMigrationVerifier_Rollback_ThenRemigrate tests full rollback+remigrate cycle.
func TestMigrationVerifier_Rollback_ThenRemigrate(t *testing.T) {
	mpt := buildMPT(200)
	bt := NewBinaryTrie()
	config := IncrementalMigrationConfig{BatchSize: 100}
	m := NewIncrementalMigration(mpt, bt, config)

	// Partial migration.
	m.MigrateBlock(100)

	// Rollback.
	v := NewMigrationVerifier()
	v.Rollback(m)

	// Full migration from scratch.
	for !m.IsDone() {
		m.MigrateBlock(200)
	}

	// Verify.
	errs, err := v.VerifyMigration(mpt, m.Dest())
	if err != nil {
		t.Fatalf("verification after rollback+remigrate: %v (errors: %v)", err, errs)
	}
}
