package eiptest_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/eth2030/eth2030/core/types"
	"github.com/eth2030/eth2030/eiptest"
)

func TestAssertionDiagnostics(t *testing.T) {
	const child = "EIPTEST_ASSERTION_FAILURE"
	if os.Getenv(child) == "1" {
		s := eiptest.New(t, eiptest.ForkAt(eiptest.ForkAmsterdam, 0))
		tx := eiptest.DynamicFee(s.Config().ChainID, s.Alice(), 0, eiptest.ToPtr(s.Bob().Address), eiptest.Wei(1), nil, 21_000, bigFeeCap, bigTipCap)
		s.Commit(tx).Tx(0).ExpectStatus(t, types.ReceiptStatusFailed)
		t.Fatal("missing assertion failure")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestAssertionDiagnostics$")
	cmd.Env = append(os.Environ(), child+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected the child assertion to fail: %s", out)
	}
	for _, want := range []string{"want status 0, got status 1", "tx[0] status=1 gas=21000", "BAL:", "requests:", "state roots:", "state delta"} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("diagnostic missing %q: %s", want, out)
		}
	}
}
