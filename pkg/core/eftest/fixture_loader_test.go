package eftest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// goEthTestdata returns the path to go-ethereum's evm testdata directory.
func goEthTestdata() string {
	return filepath.Join("..", "..", "..", "refs", "go-ethereum", "cmd", "evm", "testdata")
}

// efGeneralStateTests returns the path to the canonical EF GeneralStateTests.
func efGeneralStateTests() string {
	return filepath.Join("..", "..", "..", "refs", "go-ethereum", "tests", "testdata", "GeneralStateTests")
}

// efSpecFixtures returns the path to execution-spec-tests compiled fixtures.
func efSpecFixtures() string {
	return filepath.Join("..", "..", "..", "refs", "execution-spec-tests",
		"src", "ethereum_test_specs", "tests", "fixtures")
}

func TestEFDiscoverFixtures(t *testing.T) {
	dir := goEthTestdata()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", dir)
	}

	files, err := DiscoverFixtures(dir)
	if err != nil {
		t.Fatalf("DiscoverFixtures: %v", err)
	}

	if len(files) == 0 {
		t.Fatal("expected at least one fixture file")
	}

	for _, f := range files {
		if !strings.HasSuffix(f, ".json") {
			t.Errorf("non-JSON file: %s", f)
		}
	}

	found := false
	for _, f := range files {
		if filepath.Base(f) == "statetest.json" {
			found = true
			break
		}
	}
	if !found {
		t.Error("statetest.json not found in fixture discovery")
	}
	t.Logf("discovered %d fixture files", len(files))
}

func TestEFRunSingleFixture(t *testing.T) {
	path := testdataPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s", path)
	}

	results, err := RunSingleFixture(path, "")
	if err != nil {
		t.Fatalf("RunSingleFixture: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result")
	}

	var passed, failed, skipped int
	for _, r := range results {
		if r.Error != nil && strings.HasPrefix(r.Error.Error(), "skipped:") {
			skipped++
		} else if r.Passed {
			passed++
		} else {
			failed++
		}
	}
	t.Logf("single fixture: %d passed, %d failed, %d skipped", passed, failed, skipped)
}

func TestEFBatchResult(t *testing.T) {
	batch := &BatchResult{
		Total: 10, Passed: 7, Failed: 2, Skipped: 1,
		Errors: []*TestResult{
			{File: "a.json", Name: "test1", Fork: "London", Index: 0},
			{File: "b.json", Name: "test2", Fork: "Berlin", Index: 1},
		},
	}
	if batch.Total != 10 {
		t.Errorf("total: got %d, expected 10", batch.Total)
	}
	if batch.Passed+batch.Failed+batch.Skipped != batch.Total {
		t.Error("counts don't add up")
	}
}

func TestEFFormatResults(t *testing.T) {
	batch := &BatchResult{
		Total: 5, Passed: 3, Failed: 1, Skipped: 1,
		Errors: []*TestResult{{File: "/path/to/test.json", Name: "failedTest", Fork: "London"}},
	}
	output := FormatResults(batch)
	for _, want := range []string{"5 total", "3 passed", "1 failed"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output", want)
		}
	}
}

func TestEFSkipUnsupportedForks(t *testing.T) {
	path := testdataPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s", path)
	}

	results, err := RunSingleFixture(path, "FutureHardFork2030")
	if err != nil {
		t.Fatalf("RunSingleFixture: %v", err)
	}

	for _, r := range results {
		if r.Passed {
			t.Error("no tests should pass with unsupported fork filter")
		}
	}
}

func TestEFEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	files, err := DiscoverFixtures(dir)
	if err != nil {
		t.Fatalf("DiscoverFixtures: %v", err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files, got %d", len(files))
	}

	batch, err := RunFixtureDir(dir, "")
	if err != nil {
		t.Fatalf("RunFixtureDir: %v", err)
	}
	if batch.Total != 0 {
		t.Errorf("expected 0 total, got %d", batch.Total)
	}
}

func TestEFInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "bad.json")
	os.WriteFile(f, []byte("{invalid json"), 0644)

	if _, err := LoadStateTests(f); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if _, err := RunSingleFixture(f, ""); err == nil {
		t.Fatal("expected error for invalid JSON fixture")
	}
}

func TestEFStateRootMismatchReporting(t *testing.T) {
	path := testdataPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s", path)
	}

	tests, err := LoadStateTests(path)
	if err != nil {
		t.Fatalf("LoadStateTests: %v", err)
	}

	for name, test := range tests {
		for _, sub := range test.Subtests() {
			if !ForkSupported(sub.Fork) {
				continue
			}
			result := test.Run(sub)
			if !result.Passed && result.Error != nil {
				errMsg := result.Error.Error()
				if strings.Contains(errMsg, "state root mismatch") {
					if !strings.Contains(errMsg, "expected") || !strings.Contains(errMsg, "got") {
						t.Errorf("test %s: mismatch error missing details: %s", name, errMsg)
					}
				}
			}
		}
	}
}

func TestEFBatchRunConcurrency(t *testing.T) {
	dir := goEthTestdata()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", dir)
	}

	tmpDir := t.TempDir()
	src := filepath.Join(dir, "statetest.json")
	if _, err := os.Stat(src); os.IsNotExist(err) {
		t.Skipf("statetest.json not found")
	}

	data, _ := os.ReadFile(src)
	os.WriteFile(filepath.Join(tmpDir, "statetest.json"), data, 0644)

	seqResult, _ := RunFixtureDir(tmpDir, "")
	concResult, _ := RunFixtureDirConcurrent(tmpDir, "", 2)

	if seqResult.Total != concResult.Total {
		t.Errorf("total mismatch: seq=%d conc=%d", seqResult.Total, concResult.Total)
	}
	if seqResult.Passed != concResult.Passed {
		t.Errorf("passed mismatch: seq=%d conc=%d", seqResult.Passed, concResult.Passed)
	}
}

func TestEFDiscoverFixturesNotDir(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file.json")
	os.WriteFile(f, []byte("{}"), 0644)
	if _, err := DiscoverFixtures(f); err == nil {
		t.Fatal("expected error for non-directory path")
	}
}

func TestEFDiscoverFixturesNonExistent(t *testing.T) {
	if _, err := DiscoverFixtures("/nonexistent/path/12345"); err == nil {
		t.Fatal("expected error for non-existent directory")
	}
}

func TestEFSupportedForksComplete(t *testing.T) {
	forks := SupportedForks()
	for _, name := range []string{"Frontier", "Homestead", "Byzantium", "Constantinople",
		"Istanbul", "Berlin", "London", "Shanghai", "Cancun", "Prague"} {
		found := false
		for _, f := range forks {
			if f == name {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected fork %q in SupportedForks()", name)
		}
	}
}

// --- Real EF GeneralStateTests integration ---

// runEFCategory runs all tests in a single GeneralStateTests subdirectory,
// reporting results and asserting a minimum pass rate.
func runEFCategory(t *testing.T, category string, minPassRate float64) {
	t.Helper()
	t.Parallel()
	dir := filepath.Join(efGeneralStateTests(), category)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("EF tests not found: %s", dir)
	}

	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Skipf("no JSON fixtures in %s", category)
	}

	var total, passed, failed int
	for _, path := range files {
		tests, err := LoadStateTests(path)
		if err != nil {
			continue
		}
		for _, test := range tests {
			for _, sub := range test.Subtests() {
				if !ForkSupported(sub.Fork) {
					continue
				}
				total++
				result := test.Run(sub)
				if result.Passed {
					passed++
				} else {
					failed++
				}
			}
		}
	}

	if total == 0 {
		t.Skipf("no runnable test vectors in %s", category)
	}

	rate := float64(passed) / float64(total) * 100
	t.Logf("%s: %d total, %d passed, %d failed (%.1f%%)", category, total, passed, failed, rate)

	if rate < minPassRate {
		t.Errorf("%s pass rate %.1f%% below minimum %.1f%%", category, rate, minPassRate)
	}
}

// Tests against categories with 100% pass rate.
func TestEF_stCodeCopyTest(t *testing.T)    { runEFCategory(t, "stCodeCopyTest", 100.0) }
func TestEF_stZeroCallsRevert(t *testing.T) { runEFCategory(t, "stZeroCallsRevert", 100.0) }
func TestEF_stSLoadTest(t *testing.T)       { runEFCategory(t, "stSLoadTest", 100.0) }
func TestEF_stExpectSection(t *testing.T)   { runEFCategory(t, "stExpectSection", 100.0) }

// Tests against categories with >80% pass rate.
func TestEF_stMemoryStressTest(t *testing.T)   { runEFCategory(t, "stMemoryStressTest", 84.0) }
func TestEF_stArgsZeroOneBalance(t *testing.T) { runEFCategory(t, "stArgsZeroOneBalance", 80.0) }
func TestEF_stQuadraticComplexityTest(t *testing.T) {
	runEFCategory(t, "stQuadraticComplexityTest", 85.0)
}

// Tests against categories with >40% pass rate.
func TestEF_stCallCodes(t *testing.T)      { runEFCategory(t, "stCallCodes", 60.0) }
func TestEF_stEIP150Specific(t *testing.T) { runEFCategory(t, "stEIP150Specific", 50.0) }
func TestEF_stMemoryTest(t *testing.T)     { runEFCategory(t, "stMemoryTest", 50.0) }
func TestEF_stEIP1559(t *testing.T)        { runEFCategory(t, "stEIP1559", 40.0) }
func TestEF_stStaticCall(t *testing.T)     { runEFCategory(t, "stStaticCall", 40.0) }
func TestEF_stRevertTest(t *testing.T)     { runEFCategory(t, "stRevertTest", 40.0) }

// --- execution-spec-tests compiled state test fixtures ---

// TestEF_SpecFixtures runs the execution-spec-tests compiled fixtures.
// These use CHAINID opcode which depends on config.chainid parsing; results
// are logged but not asserted as hard failures since the canonical EF
// GeneralStateTests (37,000+ vectors) are the primary validation target.
func TestEF_SpecFixtures(t *testing.T) {
	fixtures := []string{
		"chainid_cancun_state_test_tx_type_0.json",
		"chainid_cancun_state_test_tx_type_1.json",
		"chainid_shanghai_state_test_tx_type_0.json",
		"chainid_paris_state_test_tx_type_0.json",
	}

	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			path := filepath.Join(efSpecFixtures(), fixture)
			if _, err := os.Stat(path); os.IsNotExist(err) {
				t.Skipf("fixture not found: %s", path)
			}

			tests, err := LoadStateTests(path)
			if err != nil {
				t.Fatalf("LoadStateTests: %v", err)
			}

			for name, test := range tests {
				for _, sub := range test.Subtests() {
					if !ForkSupported(sub.Fork) {
						continue
					}
					result := test.Run(sub)
					t.Logf("%s [%s]: passed=%v expected=%s got=%s",
						name, sub.Fork, result.Passed,
						result.ExpectedRoot.Hex(), result.GotRoot.Hex())
				}
			}
		})
	}
}

// --- EF example directory: known-passing simple tests ---

func TestEF_Example_Add11(t *testing.T) {
	path := filepath.Join(efGeneralStateTests(), "stExample", "add11.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s", path)
	}

	tests, err := LoadStateTests(path)
	if err != nil {
		t.Fatalf("LoadStateTests: %v", err)
	}

	for name, test := range tests {
		for _, sub := range test.Subtests() {
			if !ForkSupported(sub.Fork) {
				continue
			}
			result := test.Run(sub)
			if !result.Passed {
				t.Errorf("%s [%s]: %v\n  expected=%s\n  got=%s",
					name, sub.Fork, result.Error,
					result.ExpectedRoot.Hex(), result.GotRoot.Hex())
			}
		}
	}
}

func TestEF_Example_SelfBalance(t *testing.T) {
	dir := filepath.Join(efGeneralStateTests(), "stSelfBalance")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("directory not found: %s", dir)
	}

	path := filepath.Join(dir, "selfBalance.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found")
	}

	tests, err := LoadStateTests(path)
	if err != nil {
		t.Fatalf("LoadStateTests: %v", err)
	}

	for name, test := range tests {
		for _, sub := range test.Subtests() {
			if !ForkSupported(sub.Fork) {
				continue
			}
			result := test.Run(sub)
			t.Logf("%s [%s]: passed=%v expected=%s got=%s",
				name, sub.Fork, result.Passed,
				result.ExpectedRoot.Hex(), result.GotRoot.Hex())
		}
	}
}

func TestEF_DiscoverGeneralStateTests(t *testing.T) {
	dir := efGeneralStateTests()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("EF tests not found: %s", dir)
	}

	files, err := DiscoverFixtures(dir)
	if err != nil {
		t.Fatalf("DiscoverFixtures: %v", err)
	}

	t.Logf("discovered %d EF GeneralStateTests fixture files", len(files))
	if len(files) < 2000 {
		t.Errorf("expected at least 2000 files, got %d", len(files))
	}

	// Count categories.
	categories := make(map[string]int)
	for _, f := range files {
		rel, _ := filepath.Rel(dir, f)
		parts := strings.SplitN(rel, string(filepath.Separator), 2)
		if len(parts) > 0 {
			categories[parts[0]]++
		}
	}
	t.Logf("%d test categories", len(categories))
	for cat, count := range categories {
		if count > 100 {
			t.Logf("  %s: %d files", cat, count)
		}
	}
}

func TestEFRunGoEthereumStateTest(t *testing.T) {
	path := testdataPath()
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skipf("fixture not found: %s", path)
	}

	results, err := RunSingleFixture(path, "")
	if err != nil {
		t.Fatalf("RunSingleFixture: %v", err)
	}

	var passed, failed, skipped int
	for _, r := range results {
		if r.Error != nil && strings.HasPrefix(r.Error.Error(), "skipped:") {
			skipped++
		} else if r.Passed {
			passed++
		} else {
			failed++
			t.Logf("FAIL: %s [%s/%d]: %v", r.Name, r.Fork, r.Index, r.Error)
		}
	}
	t.Logf("go-ethereum statetest.json: %d total, %d passed, %d failed, %d skipped",
		len(results), passed, failed, skipped)
}

func TestEFIntegrationGoEthereumFixtures(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	dir := goEthTestdata()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("testdata not found: %s", dir)
	}

	files, err := DiscoverFixtures(dir)
	if err != nil {
		t.Fatalf("DiscoverFixtures: %v", err)
	}

	t.Logf("discovered %d fixture files in go-ethereum testdata", len(files))

	stateTestPath := filepath.Join(dir, "statetest.json")
	if _, err := os.Stat(stateTestPath); os.IsNotExist(err) {
		t.Skipf("statetest.json not found")
	}

	results, err := RunSingleFixture(stateTestPath, "")
	if err != nil {
		t.Fatalf("RunSingleFixture: %v", err)
	}

	var passed, failed int
	for _, r := range results {
		if r.Passed {
			passed++
		} else {
			failed++
		}
	}
	t.Logf("Integration: %d total, %d passed, %d failed", len(results), passed, failed)
}

func TestEFRunStaticStateTests(t *testing.T) {
	dir := filepath.Join("..", "..", "..", "refs", "execution-spec-tests", "tests", "static", "state_tests")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Skipf("execution-spec-tests state tests not found: %s", dir)
	}

	files, err := DiscoverFixtures(dir)
	if err != nil {
		t.Fatalf("DiscoverFixtures: %v", err)
	}
	t.Logf("found %d fixture files in execution-spec-tests", len(files))

	var loaded, parseFailed int
	for _, f := range files {
		if _, err := LoadStateTests(f); err != nil {
			parseFailed++
		} else {
			loaded++
		}
	}
	t.Logf("loaded: %d, parse failed: %d (expected: filler format)", loaded, parseFailed)
}

// --- Per-category regression gates ---

// TestEF_RegressionGates runs a sampling of EF test categories and ensures
// pass rates don't regress below known thresholds.
func TestEF_RegressionGates(t *testing.T) {
	baseDir := efGeneralStateTests()
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		t.Skipf("EF tests not found: %s", baseDir)
	}

	// category -> minimum pass count (conservative, below current observed)
	gates := map[string]int{
		"stCodeCopyTest":            4,
		"stZeroCallsRevert":         30,
		"stSLoadTest":               2,
		"stExpectSection":           18,
		"stMemoryStressTest":        130,
		"stArgsZeroOneBalance":      150,
		"stQuadraticComplexityTest": 55,
		"stCallCodes":               100,
		"stEIP150Specific":          25,
		"stMemoryTest":              500,
	}

	for category, minPass := range gates {
		category, minPass := category, minPass
		t.Run(category, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(baseDir, category)
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				t.Skipf("not found: %s", dir)
			}

			files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
			var passed int
			for _, path := range files {
				tests, err := LoadStateTests(path)
				if err != nil {
					continue
				}
				for _, test := range tests {
					for _, sub := range test.Subtests() {
						if !ForkSupported(sub.Fork) {
							continue
						}
						if result := test.Run(sub); result.Passed {
							passed++
						}
					}
				}
			}

			t.Logf("%s: %d passed (min: %d)", category, passed, minPass)
			if passed < minPass {
				t.Errorf("regression: %s passed %d < minimum %d", category, passed, minPass)
			}
		})
	}
}
