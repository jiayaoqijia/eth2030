package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/core/eftest"
	"github.com/eth2030/eth2030/eiptest"
)

// TestEESTVectorPipeline verifies the T4.2 vector pipeline end to end:
// a state-test fixture (the same state_test JSON format execution-spec-tests
// `fill` produces) parses through the framework's vector loader and executes
// through the exported eftest runner.
//
// NOTE on generation: the pinned refs/execution-spec-tests checkout is
// mid-migration to the "weld"/EELS framework and pytest cannot collect tests
// there (rc=4), so `make eest` cannot produce fixtures from it yet. This
// test consumes a real GeneralStateTests fixture (the same format EEST
// emits) committed at testdata/vectors/state_add11.json; when the EEST pin
// advances past the migration, `make eest` output drops into
// testdata/eest/ and the same loading path applies.
func TestEESTVectorPipeline(t *testing.T) {
	const fixture = "testdata/vectors/state_add11.json"

	// 1. Vector loader parses the fixture (framework-facing surface).
	vectors := eiptest.LoadVectors(t, "vectors")
	if len(vectors) == 0 {
		t.Fatal("vector loader parsed no vectors")
	}

	// 2. Execution through the exported eftest runner.
	results, err := eftest.RunSingleFixture(fixture, "Cancun")
	if err != nil {
		t.Fatalf("RunSingleFixture: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("runner produced no results")
	}
	// Non-matching forks appear as skip records (Passed=false, "fork filter"
	// error); assert that at least one subtest actually passed and none
	// failed for a real reason.
	passed := 0
	for _, r := range results {
		if r.Passed {
			passed++
			continue
		}
		if r.Error != nil && !containsStr(r.Error.Error(), "fork filter") && !containsStr(r.Error.Error(), "unsupported fork") {
			t.Fatalf("vector %s (%s) failed: %v", r.Name, r.Fork, r.Error)
		}
	}
	if passed == 0 {
		t.Fatal("no subtest passed under the fork filter")
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
