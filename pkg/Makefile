.DEFAULT_GOAL := all

.PHONY: all build eth2030 eth2030-geth docker devnet devnet-down clean contracts eest

SOLC := solc

# contracts: compile the eiptest scenario Solidity contracts to runtime
# bytecode and regenerate eiptest/contracts/gen.go (plan T4.1). Requires
# solc on PATH; the committed gen.go keeps normal builds solc-free.
contracts:
	@command -v $(SOLC) >/dev/null 2>&1 || { echo "solc not found — install https://docs.soliditylang.org/en/latest/installing-solidity.html"; exit 1; }
	cd eiptest/contracts && set -eu; for f in *.sol; do \
		$(SOLC) --bin-runtime --overwrite -o . "$$f" >/dev/null; \
	done
	cd eiptest/contracts && set -eu; for f in *.yul; do \
		output="$$($(SOLC) --strict-assembly --bin "$$f")"; \
		bin="$$(printf '%s\n' "$$output" | awk '/Binary representation:/ {getline; print}')"; \
		test -n "$$bin"; printf '%s' "$$bin" > "$${f%.yul}.bin-runtime"; \
	done
	go run ./tools/gen-contracts eiptest/contracts
	go build ./eiptest/...

# EEST's fill command now ships with the pinned execution-specs workspace.
# Its default transition tool supports Amsterdam. EVM_BIN is optional for
# fork-compatible Geth builds, and "make bin/evm" builds the pinned Geth tool.
EEST_DIR ?= ../refs/execution-specs
EEST_PYTHON ?= 3.11
EEST_FORK ?= Amsterdam
EEST_TARGET ?= $(abspath eiptest/testdata/eest-fill/test_amsterdam_state.py)
EEST_SELECT ?= state_test and not from_state_test
EEST_OUT ?= eiptest/testdata/eest-generated
EEST_ENV ?= $(abspath bin/eest-venv)
EVM_BIN ?=

bin/evm:
	@mkdir -p bin
	cd ../refs/go-ethereum && go build -o "$(abspath bin/evm)" ./cmd/evm

eest:
	@command -v uv >/dev/null 2>&1 || { echo "uv is required"; exit 1; }
	@set -eu; \
		tmp=$$(mktemp -d); trap 'rm -rf "$$tmp"' EXIT; \
		UV_PROJECT_ENVIRONMENT="$(EEST_ENV)" uv run --project "$(abspath $(EEST_DIR))" --python "$(EEST_PYTHON)" --frozen --package ethereum-execution-testing fill "$(EEST_TARGET)" --fork "$(EEST_FORK)" -k "$(EEST_SELECT)" $(if $(EVM_BIN),--evm-bin="$(abspath $(EVM_BIN))") --output "$$tmp"; \
		EIPTEST_EEST_DIR="$$tmp/state_tests" EIPTEST_EEST_FORK="$(EEST_FORK)" go test -count=1 ./eiptest -run '^TestEESTVectorPipeline$$'; \
		mkdir -p "$(EEST_OUT)"; cp -R "$$tmp"/. "$(EEST_OUT)"/


BIN_DIR := ./bin

# Build tags: goethkzg enables production KZG backend for EIP-4844/EIP-7594.
BUILD_TAGS := goethkzg

all: build

build: eth2030 eth2030-geth

eth2030:
	@mkdir -p $(BIN_DIR)
	go build -tags $(BUILD_TAGS) -o $(BIN_DIR)/eth2030 ./cmd/eth2030

eth2030-geth:
	@mkdir -p $(BIN_DIR)
	go build -tags $(BUILD_TAGS) -o $(BIN_DIR)/eth2030-geth ./cmd/eth2030-geth

DOCKER_IMAGE := eth2030
DOCKER_TAG := latest
COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")

docker:
	docker build --build-arg COMMIT=$(COMMIT) --build-arg VERSION=$(VERSION) -t $(DOCKER_IMAGE):$(DOCKER_TAG) .

devnet:
	docker compose -f devnet/docker-compose.yml up --build -d

devnet-down:
	docker compose -f devnet/docker-compose.yml down

clean:
	rm -rf $(BIN_DIR)
