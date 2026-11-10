# ETH2030 AGENTS

Ethereum EL (Execution Layer) client targeting the L1 Strawmap roadmap.

## Directory Structure

```
eth2030/
├── pkg/              # Main Go module (EL implementation)
│   ├── cmd/          # CLI entrypoints (eth2030-geth)
│   ├── core/         # Blockchain core, state processor
│   ├── eth/          # Ethereum backend, APIs
│   ├── rpc/          # JSON-RPC server
│   ├── engine/       # Engine API (CL <-> EL communication)
│   ├── node/         # Node lifecycle management
│   ├── p2p/          # DevP2P networking
│   ├── trie/         # Merkle Patricia Trie
│   ├── txpool/       # Transaction pool
│   ├── devnet/       # Kurtosis devnet scripts
│   ├── focil/        # FOCIL (EIP-7805) implementation
│   ├── epbs/         # ePBS (Enshrined PBS) implementation
│   ├── bal/          # Block Access Lists (EIP-7928)
│   ├── zkvm/         # zkVM framework
│   └── proofs/       # Cryptographic proofs
├── refs/             # EIP/spec submodules (read-only reference)
├── docs/             # Documentation
├── formal/           # Formal specifications
├── specs/            # Ethereum spec references
└── tools/            # Development tools
```

NOTE: `refs/` is a read-only reference to EIPs and other specs, not for editing.

REMEMBER: every change MUST be written based on EIPs, u can find the EIP docs in `eth2030/refs/EIPs` and `eth2030/refs/consensus-specs`, read them line by line.

## Sibling Repositories

Each is an independent git repo; commit separately:
- `../dora/` - Block explorer with FOCIL support
- `../lighthouse/` - CL fork with ePBS/FOCIL support
- `../spamoor/` - Transaction spammer for devnet

## Devnet Quick Start

Use `devnet-debug` skill for debugging stalled devnets.

### 1. Build Local Lighthouse CL

```bash
cd ../lighthouse && docker build -t eth2030cl:local .
```

### 2. Build ETH2030 EL

```bash
cd pkg && docker build -t eth2030:local .
```

### 3. Start Devnet

```bash
cd pkg/devnet/kurtosis
./scripts/cleanup.sh eth2030-devnet
./scripts/run-devnet.sh full-feature
```

### 4. Verify Chain

```bash
# Check block number
cast bn -r http://$(kurtosis port print eth2030-devnet el-1-geth-lighthouse rpc)

# Check CL logs
kurtosis service logs eth2030-devnet cl-1-lighthouse-geth 2>&1 | grep -E "verified|ERROR" | tail -20

# Check EL logs
kurtosis service logs eth2030-devnet el-1-geth-lighthouse 2>&1 | grep -E "WARN|ERROR" | tail -20
```

## Working Rules

- Write all project documentation in English, including new documents and updates to existing documents.
- Always `git add` and commit after creating or editing files
- Verify builds locally before claiming completion
- Update `STATUS.md` for task tracking
- Keep going without pausing for confirmation; only ask when blocked
- Never stage/commit files you did not edit
- Other agents may work in the same repo; mind your own business
- Before big jobs, create checklist in `CHECK.md`
- Add unit tests for new functionality
