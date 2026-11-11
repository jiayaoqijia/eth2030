package eiptest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var eipNumbers = regexp.MustCompile(`EIP-([0-9]+(?:/[0-9]+)*)`)

func implementationList(data []byte) ([]int, error) {
	const heading = "### EIP Implementation Status"
	var inSection bool
	seen := make(map[int]bool)
	var result []int
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if !inSection {
			inSection = line == heading
			continue
		}
		if strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "## ") || strings.HasPrefix(line, "### ") {
			break
		}
		if !strings.HasPrefix(line, "- **Complete**") && !strings.HasPrefix(line, "- **Substantial**") {
			continue
		}
		for _, match := range eipNumbers.FindAllStringSubmatch(line, -1) {
			for _, member := range strings.Split(match[1], "/") {
				n, err := strconv.Atoi(member)
				if err != nil || n < 1 {
					return nil, fmt.Errorf("invalid EIP number %q", member)
				}
				if !seen[n] {
					seen[n] = true
					result = append(result, n)
				}
			}
		}
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("%q has no implementation entries", heading)
	}
	return result, nil
}

func registryCoverage(implemented []int, entries []EIPEntry, files fs.FS) error {
	seen := make(map[int]bool, len(entries))
	for _, entry := range entries {
		n := entry.Number
		if n < 0 || seen[n] {
			return fmt.Errorf("invalid or duplicate registry number %d", n)
		}
		seen[n] = true
		if entry.Title == "" {
			return fmt.Errorf("EIP-%d has no title", n)
		}
		switch entry.Tier {
		case TierA, TierB, TierC, TierD, TierE:
		default:
			return fmt.Errorf("EIP-%d has unknown tier %q", n, entry.Tier)
		}
		if entry.TestFile == "" {
			if strings.TrimSpace(entry.Exempt) == "" {
				return fmt.Errorf("EIP-%d has no test file or exemption reason", n)
			}
			continue
		}
		if entry.Exempt != "" || entry.Tier == TierD || entry.Tier == TierE || len(entry.Surfaces) == 0 {
			return fmt.Errorf("EIP-%d has inconsistent coverage declaration", n)
		}
		for _, surface := range entry.Surfaces {
			switch surface {
			case SurfaceSTF, SurfacePool, SurfaceFork, SurfaceEncoding, SurfaceRPC, SurfaceEngine, SurfaceDiff, SurfaceDevnet:
			default:
				return fmt.Errorf("EIP-%d has unknown surface %q", n, surface)
			}
		}
		if entry.TestFile != fmt.Sprintf("eip%d_test.go", n) {
			return fmt.Errorf("EIP-%d points to unexpected test file %q", n, entry.TestFile)
		}
		src, err := fs.ReadFile(files, entry.TestFile)
		if err != nil {
			return fmt.Errorf("EIP-%d: %w", n, err)
		}
		if err := verifyEIPTest(entry.TestFile, n, src); err != nil {
			return err
		}
	}
	for _, n := range implemented {
		if !seen[n] {
			return fmt.Errorf("implemented EIP-%d is missing from the registry", n)
		}
	}
	return nil
}

func verifyEIPTest(name string, n int, src []byte) error {
	file, err := parser.ParseFile(token.NewFileSet(), name, src, parser.ParseComments)
	if err != nil {
		return err
	}
	for _, group := range file.Comments {
		if group.Pos() >= file.Package {
			break
		}
		for _, comment := range group.List {
			if strings.HasPrefix(comment.Text, "//go:build") || strings.HasPrefix(comment.Text, "// +build") {
				return fmt.Errorf("%s has a build tag", name)
			}
		}
	}
	testName := "TestEIP" + strconv.Itoa(n)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv != nil || fn.Body == nil || (fn.Name.Name != testName && !strings.HasPrefix(fn.Name.Name, testName+"_")) {
			continue
		}
		if hasAssertion(fn.Body) {
			return nil
		}
	}
	return fmt.Errorf("%s has no enabled %s test with an assertion", name, testName)
}

func hasAssertion(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || found {
			return !found
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := selector.Sel.Name
		found = strings.HasPrefix(name, "Expect") || name == "RLPRoundTrip" || name == "SSZRoundTrip" || name == "Fatal" || name == "Fatalf" || name == "Error" || name == "Errorf" || name == "Fail" || name == "FailNow"
		return !found
	})
	return found
}

func TestRegistryCoversIFEIPList(t *testing.T) {
	root := filepath.Join("..", "..")
	data, err := os.ReadFile(filepath.Join(root, "IFLOW.md"))
	if err != nil {
		t.Fatal(err)
	}
	implemented, err := implementationList(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := registryCoverage(implemented, Registry, os.DirFS(".")); err != nil {
		t.Fatal(err)
	}
}
