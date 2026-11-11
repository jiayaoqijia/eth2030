// eiptest-scaffold generates an EIP integration test skeleton for the
// eiptest framework, plus (optionally) a devnet feature config + verify
// script stub for network-level surfaces (plan T2.8 / T5.2).
//
// Usage:
//
//	go run ./tools/eiptest-scaffold -eip 8200 -name "My Feature" -fork glamsterdan
//	go run ./tools/eiptest-scaffold -eip 8200 -devnet   # also emit devnet files
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var (
	eipNum   = flag.Int("eip", 0, "EIP number (required)")
	name     = flag.String("name", "", "short EIP title (default: \"EIP-<n>\")")
	fork     = flag.String("fork", "", "fork the EIP activates at (osaka|amsterdam|glamsterdan|hogota|iplus|bpo1|bpo2)")
	devnet   = flag.Bool("devnet", false, "also generate a devnet feature config + verify script stub")
	outDir   = flag.String("out", "eiptest", "output directory for the test file (relative to pkg/)")
	tier     = flag.String("tier", "A", "coverage tier A, B, C, D, or E")
	surfaces = flag.String("surfaces", "stf", "comma-separated covered surfaces")
	exempt   = flag.String("exempt", "", "documented exemption reason, creates only the registry entry")
	dryRun   = flag.Bool("dry-run", false, "print the generated files to stdout instead of writing")
)

func main() {
	flag.Parse()
	if err := generate(); err != nil {
		fmt.Fprintf(os.Stderr, "eiptest-scaffold: %v\n", err)
		os.Exit(1)
	}
}

func generate() error {
	if *eipNum <= 0 {
		return fmt.Errorf("-eip must be positive")
	}
	title := strings.TrimSpace(*name)
	if title == "" {
		title = fmt.Sprintf("EIP-%d", *eipNum)
	}
	if strings.ContainsAny(title, "\r\n") {
		return fmt.Errorf("title must fit on one line")
	}
	forkName := forkConst(*fork)
	if *fork != "" && forkName == "" {
		return fmt.Errorf("unknown fork %q", *fork)
	}
	if !strings.Contains("ABCDE", *tier) || len(*tier) != 1 {
		return fmt.Errorf("unknown tier %q", *tier)
	}
	reason := strings.TrimSpace(*exempt)
	if (*tier == "D" || *tier == "E") && reason == "" {
		return fmt.Errorf("tier %s requires -exempt", *tier)
	}

	testFile := fmt.Sprintf("eip%d_test.go", *eipNum)
	list, err := registrySurfaces(*surfaces)
	if err != nil {
		return err
	}
	if reason != "" {
		testFile, list = "", "nil"
	}
	entry := fmt.Sprintf("{%d, %q, Tier%s, %s, %q, %q},", *eipNum, title, *tier, list, testFile, reason)
	out := filepath.Clean(*outDir)
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	if !filepath.IsAbs(out) {
		out = filepath.Join(root, out)
	}
	type generated struct {
		path string
		data []byte
		mode os.FileMode
	}
	var outputs []generated
	if testFile != "" {
		src, err := format.Source([]byte(genTest(*eipNum, title, forkName)))
		if err != nil {
			return err
		}
		outputs = append(outputs, generated{filepath.Join(out, testFile), src, 0o644})
	}
	if *devnet {
		slug := fmt.Sprintf("eip%d", *eipNum)
		dir := filepath.Join(root, "devnet", "kurtosis")
		outputs = append(outputs,
			generated{filepath.Join(dir, "configs", "features", slug+".yaml"), []byte(genDevnetConfig(slug)), 0o644},
			generated{filepath.Join(dir, "scripts", "features", "verify-"+slug+".sh"), []byte(genVerifyScript(slug, *eipNum)), 0o755},
		)
	}
	registry := filepath.Join(out, "registry.go")
	if *dryRun {
		for _, item := range outputs {
			fmt.Printf("// %s\n%s\n", item.path, item.data)
		}
		fmt.Printf("// %s\n%s\n", registry, entry)
		return nil
	}
	lockPath := registry + ".lock"
	lock, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("lock registry for generation: %w", err)
	}
	lock.Close()
	defer os.Remove(lockPath)
	old, err := os.ReadFile(registry)
	if err != nil {
		return err
	}
	updated, err := insertRegistryEntry(old, *eipNum, entry)
	if err != nil {
		return err
	}
	for _, item := range outputs {
		if _, err := os.Lstat(item.path); !os.IsNotExist(err) {
			return fmt.Errorf("cannot create %s: already exists or inaccessible", item.path)
		}
	}
	var created []string
	committed := false
	defer func() {
		if !committed {
			for _, path := range created {
				_ = os.Remove(path)
			}
		}
	}()
	for _, item := range outputs {
		f, err := os.OpenFile(item.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, item.mode)
		if err != nil {
			return err
		}
		created = append(created, item.path)
		_, err = f.Write(item.data)
		closeErr := f.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}
	tmp, err := os.CreateTemp(out, ".registry-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	_, err = tmp.Write(updated)
	closeErr := tmp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(tmp.Name(), registry); err != nil {
		return err
	}
	committed = true
	for _, item := range outputs {
		fmt.Printf("wrote %s\n", item.path)
	}
	fmt.Printf("updated %s\n", registry)
	return nil
}

func moduleRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := cwd; ; dir = filepath.Dir(dir) {
		for _, candidate := range []string{dir, filepath.Join(dir, "pkg")} {
			if _, err := os.Stat(filepath.Join(candidate, "eiptest", "registry.go")); err == nil {
				return candidate, nil
			}
		}
		if filepath.Dir(dir) == dir {
			return "", fmt.Errorf("cannot locate the ETH2030 Go module from %s", cwd)
		}
	}
}

func registrySurfaces(value string) (string, error) {
	allowed := map[string]string{
		"stf": "STF", "pool": "Pool", "fork": "Fork", "encoding": "Encoding",
		"rpc": "RPC", "engine": "Engine", "diff": "Diff", "devnet": "Devnet",
	}
	var values []string
	seen := map[string]bool{}
	for _, key := range strings.Split(value, ",") {
		key = strings.TrimSpace(key)
		name, ok := allowed[key]
		if !ok || seen[key] {
			return "", fmt.Errorf("invalid or repeated surface %q", key)
		}
		seen[key] = true
		values = append(values, "Surface"+name)
	}
	return "[]Surface{" + strings.Join(values, ", ") + "}", nil
}

func insertRegistryEntry(src []byte, id int, entry string) ([]byte, error) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "registry.go", src, 0)
	if err != nil {
		return nil, err
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok || len(value.Names) != 1 || value.Names[0].Name != "Registry" || len(value.Values) != 1 {
				continue
			}
			list, ok := value.Values[0].(*ast.CompositeLit)
			if !ok {
				break
			}
			position := list.Rbrace
			for _, row := range list.Elts {
				item, ok := row.(*ast.CompositeLit)
				if !ok || len(item.Elts) == 0 {
					return nil, fmt.Errorf("unrecognized registry entry")
				}
				number, ok := item.Elts[0].(*ast.BasicLit)
				if !ok {
					return nil, fmt.Errorf("registry number must be a literal")
				}
				n, err := strconv.Atoi(number.Value)
				if err != nil {
					return nil, err
				}
				if n == id {
					return nil, fmt.Errorf("EIP-%d already registered", id)
				}
				if n > id && position == list.Rbrace {
					position = row.Pos()
				}
			}
			offset := files.Position(position).Offset
			return format.Source([]byte(string(src[:offset]) + entry + "\n" + string(src[offset:])))
		}
	}
	return nil, fmt.Errorf("could not locate Registry in registry.go")
}

func forkConst(f string) string {
	switch strings.ToLower(f) {
	case "osaka":
		return "eiptest.ForkOsaka"
	case "amsterdam":
		return "eiptest.ForkAmsterdam"
	case "glamsterdan":
		return "eiptest.ForkGlamsterdan"
	case "hogota":
		return "eiptest.ForkHogota"
	case "iplus":
		return "eiptest.ForkIPlus"
	case "bpo1":
		return "eiptest.ForkBPO1"
	case "bpo2":
		return "eiptest.ForkBPO2"
	default:
		return "" // no fork scheduling
	}
}

func genTest(eip int, title, forkConst string) string {
	var b strings.Builder
	fmt.Fprintf(&b, `package eiptest_test

import (
	"testing"

	"github.com/eth2030/eth2030/eiptest"
)

// TestEIP%d_%s verifies %s through the eiptest framework.
// Generated by tools/eiptest-scaffold; fill in each surface section.
func TestEIP%d(t *testing.T) {
	s := eiptest.New(t%s)
	_ = s

	// --- STF surface: state-transition behavior ---
	// stf := s.STF()
	// TODO: deploy/tx/call expectations for the new opcode, precompile or
	// tx-type semantics. See eip7702_test.go for a full example.

	// --- Pool surface: admission / replacement / eviction rules ---
	// p := s.Pool()
	// TODO: p.ExpectAccepted / p.ExpectRejected for the new tx validation.

	// --- Fork surface: activation gating ---
	// fp := s.Fork(eiptest.Fork%s)
	// TODO: fp.Before().ExpectInactive(t); fp.AtBoundary().ExpectActive(t)
	//       and boundary header-field assertions.

	// --- Encoding surface: RLP/SSZ roundtrip + vectors ---
	// enc := s.Encoding()
	// TODO: enc.RLPRoundTrip(t, tx); eiptest.LoadVectors(t, "eip%d")

	// --- RPC surface: new fields/methods exposed correctly ---
	// r := s.RPC()
	// TODO: r.ExpectField(t, r.Call(t, "eth_getBlockByNumber", ...), "field", ...)

	// --- Engine surface: payload carries the new feature ---
	// ep := s.Engine()
	// TODO: ep.Submit(tx); payload, status, err := ep.BuildAndProcess()

	// --- Differential surface: compare a contiguous block sequence ---
	// TODO: s.Diff().DiffBlocks(t, initialAccounts, firstBlock, nextBlock)
}
`, eip, sanitize(title), title, eip, forkOption(forkConst), forkIdent(forkConst), eip)
	return b.String()
}

func forkOption(fc string) string {
	if fc == "" {
		return ""
	}
	return fmt.Sprintf(",\n\t\teiptest.ForkAt(%s, 100)", fc)
}

func forkIdent(fc string) string {
	if fc == "" {
		return "Amsterdam"
	}
	return strings.TrimPrefix(fc, "eiptest.Fork")
}

func sanitize(s string) string {
	out := strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, s)
	if out == "" {
		return "Feature"
	}
	return out
}

func genDevnetConfig(slug string) string {
	return fmt.Sprintf(`# Devnet feature config for %s (generated by tools/eiptest-scaffold).
# Participants mirror the standard 2 EL + 2 CL feature layout with a 2s slot
# so the feature smoke completes within the standard feature-test window.
participants:
  - el_type: geth
    el_image: eth2030:local
    el_log_level: info
    cl_type: lighthouse
    cl_log_level: info
    cl_image: sigp/lighthouse:latest
    count: 2

network_params:
  network: kurtosis
  seconds_per_slot: 2
  genesis_delay: 10

additional_services:
  - dora
  - spamoor
  - assertoor

dora_params:
  image: "eth2030/dora:v0.0.1"
`, slug)
}

func genVerifyScript(slug string, eip int) string {
	return fmt.Sprintf(`#!/usr/bin/env bash
# Usage: ./scripts/run-feature-tests.sh %[1]s
set -euo pipefail

ENCLAVE="${1:?enclave required}"
RPC="${2:?rpc url required}"

cast bn -r "$RPC"
# Add an EIP-%[2]d assertion against the devnet before removing this error.
echo "EIP-%[2]d feature verification is incomplete for $ENCLAVE" >&2
exit 1
`, slug, eip)
}
