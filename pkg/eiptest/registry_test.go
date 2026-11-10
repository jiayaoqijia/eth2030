package eiptest

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// eipNumRe extracts EIP numbers from the IFLOW.md implementation status
// lines, including compound forms like "EIP-196/197".
var eipNumRe = regexp.MustCompile(`EIP-(\d+)(?:/(\d+))?`)

// parseIFEIPNumbers reads the repo-root IFLOW.md and returns the sorted set
// of EIP numbers listed under "EIP Implementation Status".
func parseIFEIPNumbers(t *testing.T) map[int]bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "IFLOW.md"))
	if err != nil {
		t.Skipf("IFLOW.md not available: %v", err)
	}
	inSection := false
	found := map[int]bool{}
	for _, line := range splitLines(string(data)) {
		if !inSection {
			if contains(line, "EIP Implementation Status") {
				inSection = true
			}
			continue
		}
		// Stop at the next top-level heading.
		if len(line) > 2 && line[0] == '#' && line[1] == ' ' {
			break
		}
		if contains(line, "**Complete**") || contains(line, "**Substantial**") {
			for _, m := range eipNumRe.FindAllStringSubmatch(line, -1) {
				var n int
				fmt.Sscanf(m[1], "%d", &n)
				found[n] = true
				if m[2] != "" {
					var n2 int
					fmt.Sscanf(m[2], "%d", &n2)
					found[n2] = true
				}
			}
		}
	}
	if len(found) == 0 {
		t.Skip("no EIP numbers found under EIP Implementation Status in IFLOW.md")
	}
	return found
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// TestRegistryCoversIFEIPList is the coverage gate (plan T1.4): every EIP in
// the IFLOW.md implementation status must have a registry entry, and every
// non-exempt entry must point to an existing test file in this package.
func TestRegistryCoversIFEIPList(t *testing.T) {
	ifEIPs := parseIFEIPNumbers(t)

	// 1. Every IFLOW EIP has a registry entry.
	var missing []int
	for n := range ifEIPs {
		if _, ok := Lookup(n); !ok {
			missing = append(missing, n)
		}
	}
	if len(missing) > 0 {
		sort.Ints(missing)
		t.Fatalf("EIPs in IFLOW.md missing from eiptest registry: %v\nadd eip%d_test.go and a Registry entry, or register a documented exemption", missing, missing[0])
	}

	// 2. Non-exempt entries have an existing test file.
	for _, e := range Registry {
		if e.Number != 0 {
			if !ifEIPs[e.Number] {
				t.Errorf("registry entry EIP-%d is not in the IFLOW.md implementation list (stale entry?)", e.Number)
			}
		}
		if e.TestFile == "" && e.Exempt == "" {
			t.Errorf("EIP-%d: entry has neither TestFile nor Exempt reason", e.Number)
		}
		if e.TestFile != "" && e.Exempt != "" {
			t.Errorf("EIP-%d: entry has both TestFile and Exempt", e.Number)
		}
		if e.TestFile != "" {
			if _, err := os.Stat(filepath.Join(".", e.TestFile)); err != nil {
				t.Errorf("EIP-%d: test file %s does not exist", e.Number, e.TestFile)
			}
		}
	}

	// 3. Tier D entries must be exempt (they cannot have in-process files).
	for _, e := range Registry {
		if e.Tier == TierD && e.TestFile != "" {
			t.Errorf("EIP-%d: tier D entries are devnet-only and must not claim a test file", e.Number)
		}
	}
}

// TestRegistryNumbersValid sanity-checks the registry itself.
func TestRegistryNumbersValid(t *testing.T) {
	seen := map[int]bool{}
	for _, e := range Registry {
		if e.Number < 0 || e.Number > 9999 {
			t.Errorf("invalid EIP number %d", e.Number)
		}
		if seen[e.Number] {
			t.Errorf("duplicate registry entry for EIP-%d", e.Number)
		}
		seen[e.Number] = true
		switch e.Tier {
		case TierA, TierB, TierC, TierD, TierE:
		default:
			t.Errorf("EIP-%d: unknown tier %q", e.Number, e.Tier)
		}
	}
}
