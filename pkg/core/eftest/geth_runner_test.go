package eftest

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	gethcommon "github.com/ethereum/go-ethereum/common"

	"github.com/eth2030/eth2030/geth"
)

// testdataDir returns the path to go-ethereum's state test fixtures.
func testdataDir() string {
	return filepath.Join("..", "..", "..", "refs", "go-ethereum", "tests", "testdata", "GeneralStateTests")
}

func TestGethRunnerSmoke(t *testing.T) {
	// Run a single known fixture to verify the geth runner works.
	fixturePath := filepath.Join(testdataDir(), "stExample", "add11.json")
	if _, err := os.Stat(fixturePath); os.IsNotExist(err) {
		t.Skip("go-ethereum test fixtures not available")
	}

	tests, err := LoadGethTests(fixturePath)
	if err != nil {
		t.Fatalf("LoadGethTests: %v", err)
	}

	var passed, failed, skipped int
	for _, test := range tests {
		for _, sub := range test.Subtests() {
			if !geth.EFTestForkSupported(sub.Fork) {
				skipped++
				continue
			}
			result := test.RunWithGeth(sub)
			if result.Passed {
				passed++
			} else {
				failed++
				if result.Error != nil {
					t.Logf("FAIL %s/%s[%d]: %v", test.Name, sub.Fork, sub.Index, result.Error)
				}
			}
		}
	}
	t.Logf("smoke: %d passed, %d failed, %d skipped", passed, failed, skipped)
	if passed == 0 || failed != 0 {
		t.Errorf("expected successful fixture execution, got %d passed and %d failed", passed, failed)
	}
}

// go-ethereum v1.17.10 excludes these legacy storage-collision fixtures in
// tests/state_test.go. Keep executing the pinned Cancun and Prague vectors and
// track their exact observed roots so a different result fails the gate.
var legacyGethRoots = map[string][]string{
	"stCreate2/RevertInCreateInInitCreate2Paris.json": {
		"0x66d387d97b50ce1cf5dfe0d00f7b69a963091c5a2e4b7e8c83d204c5a3e517d2",
	},
	"stCreate2/create2collisionStorageParis.json": {
		"0xb7f1cac2f16e94f72881f6edeee333c1a9db8d5cf3aa2454c2d53e590d806c75",
		"0xf80c59c964196be76ffed56b99247e463d54dd64002716f276216810aded685a",
		"0xa696e84d1092671f7cad3c860ef9537f5b7bf0ea0190b66dac736dbd4885a184",
	},
	"stExtCodeHash/dynamicAccountOverwriteEmpty_Paris.json": {
		"0xa5eb59c308d0473198980b46664066a1475ae1e24dea4b3508c224fefba5ec2a",
	},
	"stRevertTest/RevertInCreateInInit_Paris.json": {
		"0x906380cad079c25e5652a2ecdee8fec499cbd5e838303352d8057990b9e98913",
	},
	"stSStoreTest/InitCollisionParis.json": {
		"0x0c0a220de34e681280506c92702e74a5714d7f6206210cbe3569348250506972",
		"0x2d7c7ea8ebe327a7b12f5b9d39a23ada92458c28737edae01db72208da2afe4d",
		"0x0f83a79b57db64f5eb2fb56499540c7d48f5931aa02fb299f9317bacb1db4c91",
		"0x5a1214486bc8a618dc512cd435706dffeebb25d2c1bf785c00ccdc6ebe851f2c",
	},
}

func TestGethCategorySummary(t *testing.T) {
	requireFixtures := os.Getenv("ETH2030_REQUIRE_EF_FIXTURES") == "1"
	if testing.Short() && !requireFixtures {
		t.Skip("skipping full EF test suite in short mode")
	}

	dir := testdataDir()
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		if requireFixtures {
			t.Fatalf("required EF state test fixtures missing: %s", dir)
		}
		t.Skip("go-ethereum test fixtures not available")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}

	type catResult struct {
		passed  int
		failed  int
		skipped int
		legacy  int
	}

	var (
		mu         sync.Mutex
		catMap     = make(map[string]*catResult)
		totPassed  int
		totFailed  int
		totSkipped int
		totLegacy  int
	)

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		catMap[name] = &catResult{}
		names = append(names, name)
	}
	sort.Strings(names)

	// Run each category directory in parallel goroutines so totals are
	// available before we print the summary (t.Run+t.Parallel subtests
	// only run after the parent returns, making result collection impossible).
	var wg sync.WaitGroup
	for _, name := range names {
		name := name
		wg.Add(1)
		go func() {
			defer wg.Done()
			catDir := filepath.Join(dir, name)
			files, err := filepath.Glob(filepath.Join(catDir, "*.json"))
			if err != nil {
				t.Errorf("list state test fixtures in %s: %v", catDir, err)
				return
			}
			if len(files) == 0 {
				// The pinned suite uses direct category fixtures. The corpus also
				// contains grouping directories with only nested suites.
				t.Logf("%s has no direct fixture files", name)
				return
			}
			var passed, failed, skipped, legacy int
			for _, file := range files {
				tests, loadErr := LoadGethTests(file)
				if loadErr != nil || len(tests) == 0 {
					t.Errorf("no state tests loaded from %s: %v", file, loadErr)
					continue
				}
				base := filepath.Base(file)
				roots := legacyGethRoots[name+"/"+base]
				for _, test := range tests {
					for _, sub := range test.Subtests() {
						if !geth.EFTestForkSupported(sub.Fork) {
							skipped++
							continue
						}
						func() {
							defer func() {
								if r := recover(); r != nil {
									failed++
									t.Errorf("%s/%s %s[%d] panicked: %v", name, test.Name, sub.Fork, sub.Index, r)
								}
							}()
							result := test.RunWithGeth(sub)
							known := test.Name == strings.TrimSuffix(base, ".json") &&
								(sub.Fork == "Cancun" || sub.Fork == "Prague") && sub.Index < len(roots)
							switch {
							case known && !result.Passed && result.GotRoot.Hex() == roots[sub.Index]:
								legacy++
							case !known && result.Passed:
								passed++
							default:
								failed++
								t.Errorf("%s/%s %s[%d]: passed=%t, known legacy=%t, error=%v", name, test.Name, sub.Fork, sub.Index, result.Passed, known, result.Error)
							}
						}()
					}
				}
			}
			mu.Lock()
			catMap[name].passed = passed
			catMap[name].failed = failed
			catMap[name].skipped = skipped
			catMap[name].legacy = legacy
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Collect totals.
	mu.Lock()
	for _, n := range names {
		cat := catMap[n]
		totPassed += cat.passed
		totFailed += cat.failed
		totSkipped += cat.skipped
		totLegacy += cat.legacy
	}
	mu.Unlock()

	t.Log("")
	t.Log("=== GETH-BACKED EF STATE TEST RESULTS ===")
	t.Log(strings.Repeat("-", 70))
	t.Logf("%-40s %6s %6s %6s %6s", "CATEGORY", "PASS", "FAIL", "SKIP", "LEGACY")
	t.Log(strings.Repeat("-", 70))

	for _, n := range names {
		cat := catMap[n]
		t.Logf("%-40s %6d %6d %6d %6d", n, cat.passed, cat.failed, cat.skipped, cat.legacy)
	}

	t.Log(strings.Repeat("-", 70))
	t.Logf("%-40s %6d %6d %6d %6d", "TOTAL", totPassed, totFailed, totSkipped, totLegacy)

	if totPassed == 0 {
		t.Error("expected at least some passing tests")
	}
	if requireFixtures && (totPassed != 36264 || totFailed != 0 || totSkipped != 0 || totLegacy != 20) {
		t.Error("pinned suite must report 36,264 passes, zero failures or skips, and 20 known legacy outcomes")
	}
}

func TestGethRunnerSingleCategory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	// Test a single category for quick validation.
	catDir := filepath.Join(testdataDir(), "stExample")
	if _, err := os.Stat(catDir); os.IsNotExist(err) {
		t.Skip("test fixtures not available")
	}

	files, err := filepath.Glob(filepath.Join(catDir, "*.json"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no stExample fixtures: %v", err)
	}
	var passed, failed int
	for _, file := range files {
		tests, err := LoadGethTests(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, test := range tests {
			for _, sub := range test.Subtests() {
				if !geth.EFTestForkSupported(sub.Fork) {
					continue
				}
				result := test.RunWithGeth(sub)
				if result.Passed {
					passed++
				} else {
					failed++
					t.Logf("FAIL %s/%s[%d]: %v", test.Name, sub.Fork, sub.Index, result.Error)
				}
			}
		}
	}
	t.Logf("stExample: %d passed, %d failed", passed, failed)
	if passed == 0 || failed != 0 {
		t.Fatalf("expected successful stExample fixture execution, got %d passed and %d failed", passed, failed)
	}
}

func TestGethForkConfigs(t *testing.T) {
	forks := []string{
		"Frontier", "Homestead", "EIP150", "EIP158",
		"Byzantium", "Constantinople", "Istanbul",
		"Berlin", "London", "Merge", "Shanghai", "Cancun", "Prague",
	}
	for _, fork := range forks {
		config, err := geth.EFTestChainConfig(fork)
		if err != nil {
			t.Errorf("EFTestChainConfig(%s): %v", fork, err)
			continue
		}
		if config == nil {
			t.Errorf("EFTestChainConfig(%s) returned nil", fork)
		}
	}

	_, err := geth.EFTestChainConfig("UnknownFork")
	if err == nil {
		t.Error("expected error for unsupported fork")
	}
}

func TestGethMakePreState(t *testing.T) {
	accounts := map[string]geth.PreAccount{
		"0x1000000000000000000000000000000000000001": {
			Balance: hexToBigInt("0x1000"),
			Nonce:   1,
			Code:    []byte{0x60, 0x00},
		},
	}

	state, err := geth.MakePreState(accounts)
	if err != nil {
		t.Fatalf("MakePreState: %v", err)
	}
	defer state.Close()

	if state.StateDB == nil {
		t.Fatal("StateDB is nil")
	}

	// Verify the account exists.
	addr := gethcommon.HexToAddress("0x1000000000000000000000000000000000000001")
	if !state.StateDB.Exist(addr) {
		t.Error("account does not exist")
	}
	if state.StateDB.GetNonce(addr) != 1 {
		t.Errorf("nonce = %d, want 1", state.StateDB.GetNonce(addr))
	}
}

var _ = fmt.Sprintf // keep fmt imported
