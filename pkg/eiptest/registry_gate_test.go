package eiptest

import (
	"strings"
	"testing"
	"testing/fstest"
)

func TestRegistryRejectsMissingOrPlaceholderTest(t *testing.T) {
	ids, err := implementationList([]byte("### EIP Implementation Status\n- **Complete** (3): EIP-196/197/198\n### Other\n- **Complete**: EIP-9999"))
	if err != nil || len(ids) != 3 || ids[2] != 198 {
		t.Fatalf("implementation IDs %v: %v", ids, err)
	}
	if _, err := implementationList([]byte("no section")); err == nil {
		t.Fatal("missing section accepted")
	}
	files := fstest.MapFS{}
	if err := registryCoverage([]int{8200}, nil, files); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing EIP accepted: %v", err)
	}
	entry := EIPEntry{8200, "Example", TierA, []Surface{SurfaceSTF}, "eip8200_test.go", ""}
	for _, input := range []string{
		"package eiptest\nfunc TestEIP8200(t *testing.T) { _ = t }",
		"//go:build integration\n\npackage eiptest\nfunc TestEIP8200(t *testing.T) { t.Fatal(0) }",
	} {
		files[entry.TestFile] = &fstest.MapFile{Data: []byte(input)}
		if err := registryCoverage([]int{8200}, []EIPEntry{entry}, files); err == nil {
			t.Fatalf("accepted inadequate test %s", input)
		}
	}
	files[entry.TestFile] = &fstest.MapFile{Data: []byte("package eiptest\nfunc TestEIP8200(t *testing.T) { t.Fatal(0) }")}
	if err := registryCoverage([]int{8200}, []EIPEntry{entry}, files); err != nil {
		t.Fatal(err)
	}
}
