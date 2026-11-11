.PHONY: all build eth2030 eth2030-geth docker devnet devnet-down clean contracts eest

SOLC := solc

# contracts: compile the eiptest scenario Solidity contracts to runtime
# bytecode and regenerate eiptest/contracts/gen.go (plan T4.1). Requires
# solc on PATH; the committed gen.go keeps normal builds solc-free.
contracts:
	@command -v $(SOLC) >/dev/null 2>&1 || { echo "solc not found — install https://docs.soliditylang.org/en/latest/installing-solidity.html"; exit 1; }
	cd eiptest/contracts && for f in *.sol; do \
		$(SOLC) --bin-runtime --overwrite -o . "$$f" >/dev/null; \
	done
	go run ./tools/gen-contracts eiptest/contracts
	go build ./eiptest/...

# eest: run execution-spec-tests `fill` for the configured fork to produce
# state-test vectors consumed by pkg/core/eftest (plan T4.2). Requires uv
# (python) and the geth evm binary; EEST_DIR points at the refs checkout.
EEST_DIR ?= ../refs/execution-spec-tests
EVM_BIN ?= $(BIN_DIR)/evm
EEST_FORK ?= amsterdam
EEST_OUT ?= eiptest/testdata/eest

$(EVM_BIN):
	@mkdir -p $(BIN_DIR)
	go build -o $(EVM_BIN) ../refs/go-ethereum/cmd/evm

eest: $(EVM_BIN)
	@command -v uv >/dev/null 2>&1 || { echo "uv not found — install https://docs.astral.sh/uv/"; exit 1; }
	cd $(EEST_DIR) && uv run fill "tests/$(EEST_FORK)" --evm-bin=../../$(EVM_BIN) --output ../../$(EEST_OUT)
	@echo "EEST fixtures written to $(EEST_OUT)"

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
