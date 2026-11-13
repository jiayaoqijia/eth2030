# Contributing to ETH2030

Thank you for your interest in contributing to ETH2030! We welcome contributions
from anyone and are grateful for even the smallest improvements.

## Development Setup

Install Go 1.26.8 or later.

```bash
# Clone the repository
git clone https://github.com/alt-research/eth2030.git
cd eth2030

# Build all packages
cd pkg && go build ./...

# Run all package tests
go test ./...

# Run tests for a specific package
go test ./core/types/...
go test ./consensus/...

# Run with verbose output
go test -v ./core/vm/...

# Run fuzz tests
go test -fuzz=FuzzDecode ./rlp/ -fuzztime=30s
```

## Contribution Categories

### 1. EIP Implementations

ETH2030 implements 58+ EIPs across the [L1 Strawmap](https://strawmap.org/). To add or improve
an EIP implementation:

1. Check `docs/EIP_SPEC_IMPL.md` for current implementation status
2. Reference the spec in `refs/EIPs/EIPS/eip-XXXX.md`
3. Implement in the appropriate package (see package structure in README)
4. Add comprehensive tests (table-driven preferred)
5. Update `docs/EIP_SPEC_IMPL.md` with your implementation details

### 2. Bug Fixes

- Follow the [Quick Start](README.md#quick-start) to fetch the pinned fixtures and run the EF state test suite.
- Check `docs/GAP_ANALYSIS.md` for known gaps and PARTIAL items
- Write a regression test before fixing the bug

### 3. New Packages

When creating a new package:

- Place it under `pkg/` following existing naming conventions
- Keep files under ~500 LOC
- Include `*_test.go` files matching each source file
- Export only what other packages need

### 4. Documentation

- `docs/EIP_SPEC_IMPL.md` — EIP traceability (specs, implementations, tests)
- `docs/GAP_ANALYSIS.md` — Roadmap coverage audit (65 items)
- `docs/PROGRESS.md` — Package completion and statistics
- `docs/DESIGN.md` — Architecture and implementation design

## Coding Guidelines

- Code must use `gofmt` formatting
- Code must be documented following Go [commentary guidelines](https://golang.org/doc/effective_go.html#commentary)
- Prefer strict typing; avoid loose types
- Add brief comments for tricky or non-obvious logic
- Keep files concise; aim for under ~500 LOC
- Pull requests must be based on and opened against the `master` branch
- Commit messages should be prefixed with the package they modify:
  - `core/vm: add EOF DATALOAD opcode`
  - `consensus: fix attestation aggregation`
  - `docs: update EIP traceability`

## Pull Request Process

1. Fork the repository and create your branch from `master`
2. Make your changes following the coding guidelines above
3. Add or update tests as needed
4. Ensure all tests pass: `cd pkg && go test ./...`
5. Ensure code is formatted: `gofmt -l .` (should produce no output)
6. Submit your PR with a clear description of the changes

## Testing Requirements

All contributions must:

- Pass the full test suite (`go test ./...`)
- Include tests for new functionality
- Not decrease test coverage for modified packages
- Not introduce any `go vet` warnings

## Module Structure

The Go module root is `pkg/` (not the project root). This avoids conflicts with
the `refs/` submodules which contain their own `go.mod` files.

```bash
# All Go commands should run from pkg/
cd pkg
go build ./...
go test ./...
```

## Questions?

Open an issue with the `question` label or reach out to the maintainers.
