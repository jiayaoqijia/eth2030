package config

import (
	"math/big"
	"testing"
)

func TestForkScheduleLength(t *testing.T) {
	cfg := MainnetConfig
	schedule := cfg.ForkSchedule()

	// Mainnet should have 16 forks (Homestead through Hogota).
	if len(schedule) != 16 {
		t.Fatalf("expected 16 forks in schedule, got %d", len(schedule))
	}

	// First fork should be Homestead.
	if schedule[0].Name != "Homestead" {
		t.Fatalf("expected first fork Homestead, got %s", schedule[0].Name)
	}

	// Last fork should be Hogota.
	if schedule[len(schedule)-1].Name != "Hogota" {
		t.Fatalf("expected last fork Hogota, got %s", schedule[len(schedule)-1].Name)
	}
}

func TestForkIDIsActive(t *testing.T) {
	tests := []struct {
		name     string
		fork     ForkID
		num      *big.Int
		time     uint64
		expected bool
	}{
		{
			name:     "block fork active",
			fork:     ForkID{Name: "London", Block: big.NewInt(100)},
			num:      big.NewInt(100),
			time:     0,
			expected: true,
		},
		{
			name:     "block fork not yet active",
			fork:     ForkID{Name: "London", Block: big.NewInt(100)},
			num:      big.NewInt(99),
			time:     0,
			expected: false,
		},
		{
			name:     "timestamp fork active",
			fork:     ForkID{Name: "Shanghai", Timestamp: newUint64(1000)},
			num:      big.NewInt(0),
			time:     1000,
			expected: true,
		},
		{
			name:     "timestamp fork not yet active",
			fork:     ForkID{Name: "Shanghai", Timestamp: newUint64(1000)},
			num:      big.NewInt(0),
			time:     999,
			expected: false,
		},
		{
			name:     "unscheduled fork",
			fork:     ForkID{Name: "Future"},
			num:      big.NewInt(1000000),
			time:     9999999,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fork.IsActive(tt.num, tt.time)
			if got != tt.expected {
				t.Fatalf("IsActive=%v, want %v", got, tt.expected)
			}
		})
	}
}

func TestForkIDString(t *testing.T) {
	tests := []struct {
		fork ForkID
		want string
	}{
		{
			fork: ForkID{Name: "London", Block: big.NewInt(12965000)},
			want: "London@block:12965000",
		},
		{
			fork: ForkID{Name: "Shanghai", Timestamp: newUint64(1681338455)},
			want: "Shanghai@time:1681338455",
		},
		{
			fork: ForkID{Name: "Hogota"},
			want: "Hogota@pending",
		},
	}

	for _, tt := range tests {
		got := tt.fork.String()
		if got != tt.want {
			t.Fatalf("String()=%q, want %q", got, tt.want)
		}
	}
}

func TestActiveForks(t *testing.T) {
	cfg := TestConfig // all pre-merge forks at block 0, timestamp forks at 0

	// At block 0, timestamp 0, all scheduled forks should be active.
	active := cfg.ActiveForks(big.NewInt(0), 0)

	// Count forks that have activation points in TestConfig.
	expected := 0
	for _, f := range cfg.ForkSchedule() {
		if f.Block != nil || f.Timestamp != nil {
			expected++
		}
	}

	if len(active) != expected {
		t.Fatalf("expected %d active forks, got %d", expected, len(active))
	}
}

func TestPendingForks(t *testing.T) {
	cfg := MainnetConfig

	// At a high block number but timestamp 0, timestamp forks should be pending.
	pending := cfg.PendingForks(big.NewInt(20_000_000), 0)

	hasShanghaiPending := false
	for _, f := range pending {
		if f.Name == "Shanghai" {
			hasShanghaiPending = true
		}
	}
	if !hasShanghaiPending {
		t.Fatal("expected Shanghai to be pending at timestamp 0")
	}
}

func TestUnscheduledForks(t *testing.T) {
	cfg := MainnetConfig

	unscheduled := cfg.UnscheduledForks()

	// The native roadmap configuration still leaves later forks unscheduled.
	hasHogota := false
	for _, f := range unscheduled {
		if f.Name == "Hogota" {
			hasHogota = true
		}
	}
	if !hasHogota {
		t.Fatal("expected Hogota to be unscheduled on mainnet")
	}
}

func TestConfigDiff(t *testing.T) {
	local := &ChainConfig{
		ChainID:        big.NewInt(1),
		HomesteadBlock: big.NewInt(100),
		LondonBlock:    big.NewInt(200),
	}
	remote := &ChainConfig{
		ChainID:        big.NewInt(1),
		HomesteadBlock: big.NewInt(100),
		LondonBlock:    big.NewInt(300), // different
	}

	diffs := ConfigDiff(local, remote)

	if len(diffs) != 1 {
		t.Fatalf("expected 1 diff, got %d", len(diffs))
	}
	if diffs[0].ForkName != "London" {
		t.Fatalf("expected London diff, got %s", diffs[0].ForkName)
	}
	if diffs[0].Local != "block:200" {
		t.Fatalf("expected local block:200, got %s", diffs[0].Local)
	}
	if diffs[0].Remote != "block:300" {
		t.Fatalf("expected remote block:300, got %s", diffs[0].Remote)
	}
}

func TestConfigDiffNilConfigs(t *testing.T) {
	diffs := ConfigDiff(nil, MainnetConfig)
	if diffs != nil {
		t.Fatal("expected nil diffs for nil local config")
	}

	diffs = ConfigDiff(MainnetConfig, nil)
	if diffs != nil {
		t.Fatal("expected nil diffs for nil remote config")
	}
}

func TestConfigDiffIdentical(t *testing.T) {
	diffs := ConfigDiff(MainnetConfig, MainnetConfig)
	if len(diffs) != 0 {
		t.Fatalf("expected 0 diffs for identical configs, got %d", len(diffs))
	}
}

func TestCheckConfigCompatible(t *testing.T) {
	local := &ChainConfig{
		ChainID:        big.NewInt(1),
		HomesteadBlock: big.NewInt(100),
		LondonBlock:    big.NewInt(200),
	}
	remote := &ChainConfig{
		ChainID:        big.NewInt(1),
		HomesteadBlock: big.NewInt(100),
		LondonBlock:    big.NewInt(300), // different
	}

	// At head block 199, London is not active; should be compatible.
	err := CheckConfigCompatible(local, remote, 199, 0)
	if err != nil {
		t.Fatalf("expected compatible at block 199, got: %v", err)
	}

	// At head block 200, London is active; should be incompatible.
	err = CheckConfigCompatible(local, remote, 200, 0)
	if err == nil {
		t.Fatal("expected incompatible at block 200")
	}
	if err.ForkName != "London" {
		t.Fatalf("expected London incompatibility, got %s", err.ForkName)
	}
}

func TestCheckConfigCompatibleTimestampFork(t *testing.T) {
	shanghaiTime1 := uint64(1000)
	shanghaiTime2 := uint64(2000)
	local := &ChainConfig{
		ChainID:      big.NewInt(1),
		LondonBlock:  big.NewInt(0),
		ShanghaiTime: &shanghaiTime1,
	}
	remote := &ChainConfig{
		ChainID:      big.NewInt(1),
		LondonBlock:  big.NewInt(0),
		ShanghaiTime: &shanghaiTime2,
	}

	// Before Shanghai activation, should be compatible.
	err := CheckConfigCompatible(local, remote, 500, 999)
	if err != nil {
		t.Fatalf("expected compatible before Shanghai, got: %v", err)
	}

	// After Shanghai activation (local), should be incompatible.
	err = CheckConfigCompatible(local, remote, 500, 1000)
	if err == nil {
		t.Fatal("expected incompatible after Shanghai activation")
	}
}

func TestConfigCompatErrorString(t *testing.T) {
	err := &ConfigCompatError{
		ForkName:  "London",
		LocalVal:  "block:200",
		RemoteVal: "block:300",
		HeadBlock: 200,
		HeadTime:  0,
	}
	got := err.Error()
	expected := `incompatible fork "London": local=block:200 remote=block:300 (head block=200 time=0)`
	if got != expected {
		t.Fatalf("unexpected error string:\n  got:  %s\n  want: %s", got, expected)
	}
}

func TestNextForkAfter(t *testing.T) {
	shanghaiTime := uint64(1000)
	cancunTime := uint64(2000)
	cfg := &ChainConfig{
		ChainID:        big.NewInt(1),
		HomesteadBlock: big.NewInt(0),
		LondonBlock:    big.NewInt(0),
		ShanghaiTime:   &shanghaiTime,
		CancunTime:     &cancunTime,
	}

	// Before Shanghai, next fork should be Shanghai.
	next := cfg.NextForkAfter(big.NewInt(0), 500)
	if next.Name != "Shanghai" {
		t.Fatalf("expected Shanghai as next fork, got %s", next.Name)
	}

	// After Shanghai but before Cancun, next fork should be Cancun.
	next = cfg.NextForkAfter(big.NewInt(0), 1500)
	if next.Name != "Cancun" {
		t.Fatalf("expected Cancun as next fork, got %s", next.Name)
	}

	// After all forks, next fork should be empty.
	next = cfg.NextForkAfter(big.NewInt(0), 5000)
	if next.Name != "" {
		t.Fatalf("expected empty fork after all forks, got %s", next.Name)
	}
}

func TestLegacyNativeBLSActivationBoundary(t *testing.T) {
	cfg := *MainnetConfig
	cutover := uint64(1_800_000_000)
	cfg.LegacyNativeBLSUntil = &cutover
	block := big.NewInt(24_000_000)
	before := cfg.Rules(block, true, cutover-1)
	at := cfg.Rules(block, true, cutover)
	if !before.LegacyNativeBLS || at.LegacyNativeBLS || !at.IsPrague {
		t.Fatalf("legacy BLS before=%t at=%t Prague=%t", before.LegacyNativeBLS, at.LegacyNativeBLS, at.IsPrague)
	}
}
