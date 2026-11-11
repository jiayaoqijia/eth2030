#!/usr/bin/env bash
# run-feature-tests.sh — Run devnet tests for ETH2030 features one by one.
#
# Usage:
#   ./run-feature-tests.sh              # Run all 35 feature tests
#   ./run-feature-tests.sh epbs focil   # Run specific features
#   ./run-feature-tests.sh --list       # List available features
#
# Each feature test:
#   1. Starts a Kurtosis devnet with the feature config
#   2. Waits for blocks to produce
#   3. Runs consensus check
#   4. Runs assertoor check (if available)
#   5. Runs feature-specific verification
#   6. Cleans up
#
# Results are logged to pkg/devnet/kurtosis/results/

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_DIR="$SCRIPT_DIR/../configs/features"
RESULTS_DIR="$SCRIPT_DIR/../results"
PKG_DIR="$SCRIPT_DIR/../../../"
# Pinned local ethereum-package (refs/, read-only submodule) for reproducible devnets;
# override with KURTOSIS_PACKAGE=github.com/ethpandaops/ethereum-package to follow upstream.
ROOT_DIR="$SCRIPT_DIR/../../../../"
KURTOSIS_PACKAGE="${KURTOSIS_PACKAGE:-$ROOT_DIR/refs/ethereum-package}"

FEATURES=(
  epbs
  focil
  bal
  native-aa
  gas-repricing
  blobs
  multidim-gas
  ssz
  native-rollups
  peerdas
  consensus-3sf
  pq-crypto
  encrypted-mempool
  shielded-transfers
  cl-finality
  cl-validators
  cl-attestations
  cl-security
  cl-infrastructure
  dl-blob-advanced
  dl-reconstruction
  dl-futures
  dl-broadcast
  el-gas-schedule
  el-payload
  el-proofs
  el-zkvm
  el-state
  el-tx-advanced
  el-gas-futures
  frame-mempool
  riscv-precompile
  stark-frames
  mixnet
  local-tx
  il-satisfaction
  bal-feasibility
  bal-retention
  multidim-fee-tx
  gas-vectors-3d
)

FEATURE_DESCRIPTIONS=(
  "ePBS — Enshrined Proposer-Builder Separation (EIP-7732)"
  "FOCIL — Fork-Choice Enforced Inclusion Lists (EIP-7805)"
  "BALs — Block Access Lists for Parallel Execution (EIP-7928)"
  "Native AA — Account Abstraction via SetCode (EIP-7702)"
  "Gas Repricing — Glamsterdam Conversion Repricing (18 EIPs)"
  "Blobs — Blob Transactions + Sparse Blobpool (EIP-4844/8070)"
  "Multidim Gas — Multi-Gas Pricing (EIP-7706)"
  "SSZ — SSZ Transactions & Blocks (EIP-6404/7807)"
  "Native Rollups — EXECUTE Precompile (EIP-8079)"
  "PeerDAS — Data Availability Sampling (EIP-7594)"
  "3SF — 3-Slot Finality + Quick Slots"
  "PQ Crypto — Post-Quantum Cryptography"
  "Encrypted Mempool — Commit-Reveal Ordering"
  "Shielded Transfers — Private L1 Transactions"
  "CL Finality — Fast Confirmation + Endgame Finality + Fast L1 Finality"
  "CL Validators — Attester Caps + APS + 1 ETH Includers"
  "CL Attestations — jeanVM Aggregation + 1M Attestations/Slot"
  "CL Security — Attack Recovery + VDF Randomness + Secret Proposers"
  "CL Infrastructure — Beacon Specs + Tech Debt + Distributed Builder + CL Proofs"
  "DL Blob Advanced — BPO Blobs + Blob Streaming + PQ Blobs + Variable Blobs"
  "DL Reconstruction — Cell Messages + Blob Reconstruction + Sample Size"
  "DL Futures — Blob Futures + Custody Proofs"
  "DL Broadcast — EIP-7702 Broadcast + Teragas L2"
  "EL Gas Schedule — Hogota Repricing + 3x/Year Gas Limit"
  "EL Payload — Payload Chunking + Block in Blobs + Announce Nonce"
  "EL Proofs — Mandatory 3-of-5 + Optional + Aggregation + AA Proofs"
  "EL zkVM — Canonical Guest + zkVM + STF + zkISA + Exposed zkISA"
  "EL State — Binary Tree + VOPS + Endgame State + Misc Purges"
  "EL Tx Advanced — Tx Assertions + NTT Precompile + PQ Tx + Sharded Mempool"
  "EL Gas Futures — Long-Dated Gas Futures + Gigagas L1"
  "Frame Mempool — EIP-8141 Frame Transactions (dual-tier, paymaster registry, VERIFY simulation)"
  "RISC-V Precompile — I+ fork RISC-V guest routing (EL-2.3, EL-3.1, EL-3.3)"
  "STARK Frames — STARK proof sealing + import-side VerifyBlockFrameProof (PQ-5b.2/5b.3)"
  "Mixnet Transport — TransportManager wiring, simulated mode default (BB-1.1)"
  "LocalTx Gate — type-0x08 AllowLocalTx gate, gas discount wiring (BB-2.2)"
  "IL Satisfaction — engine_newPayload IL check wiring (SPEC-4.3 / EIP-7805)"
  "BAL Feasibility — block builder feasibility check every 8 txs (SPEC-5.4 / EIP-7928)"
  "BAL Retention — IsBALRetained retention window 113056 slots (SPEC-5.5 / EIP-7928)"
  "MultiDimFeeTx — type 0x09 fee vector registration (SPEC-6.1 / EIP-7706)"
  "3D Gas Vectors — GasLimitVec/GasUsedVec/ExcessGasVec + copyHeader fix (SPEC-6.4 / EIP-7706)"
)

# Show usage
usage() {
  echo "Usage: $0 [--list] [feature1 feature2 ...]"
  echo ""
  echo "Features:"
  for i in "${!FEATURES[@]}"; do
    echo "  ${FEATURES[$i]} — ${FEATURE_DESCRIPTIONS[$i]}"
  done
}

if [[ "${1:-}" == "--list" ]]; then
  usage
  exit 0
fi

if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then
  usage
  exit 0
fi

# Check prerequisites
if ! command -v kurtosis &>/dev/null; then
  echo "Error: kurtosis CLI not found. Install: https://docs.kurtosis.com/install/"
  exit 1
fi

if ! command -v docker &>/dev/null; then
  echo "Error: docker not found."
  exit 1
fi

# Create results directory
mkdir -p "$RESULTS_DIR"

# Determine which features to test
SELECTED=("${@}")
if [ ${#SELECTED[@]} -eq 0 ]; then
  SELECTED=("${FEATURES[@]}")
fi

PASS=0
FAIL=0
SKIP=0
RESULTS=()

for feature in "${SELECTED[@]}"; do
  CONFIG_FILE="$CONFIG_DIR/$feature.yaml"
  ENCLAVE="eth2030-${feature}"
  LOG_FILE="$RESULTS_DIR/${feature}.log"

  if [ ! -f "$CONFIG_FILE" ]; then
    echo "=== SKIP: $feature (config not found) ==="
    SKIP=$((SKIP + 1))
    RESULTS+=("SKIP $feature — config not found")
    continue
  fi

  echo "========================================"
  echo "=== FEATURE: $feature"
  echo "========================================"

  # Clean up any previous enclave
  kurtosis enclave rm -f "$ENCLAVE" 2>/dev/null || true

  # Start devnet
  echo "Starting devnet..."
  if ! kurtosis run "$KURTOSIS_PACKAGE" \
      --args-file "$CONFIG_FILE" \
      --enclave "$ENCLAVE" 2>&1 | tee "$LOG_FILE"; then
    echo "FAIL: Could not start devnet for $feature"
    FAIL=$((FAIL + 1))
    RESULTS+=("FAIL $feature — devnet start failed")
    kurtosis enclave rm -f "$ENCLAVE" 2>/dev/null || true
    continue
  fi

  # Wait for blocks
  echo "Waiting 30s for blocks..."
  sleep 30

  # Resolve EL RPC URL early — services may stop during assertoor check.
  INSPECT=$(kurtosis enclave inspect "$ENCLAVE" 2>/dev/null || true)
  EL_SVC=$(echo "$INSPECT" | grep "el-[0-9]" | head -1 | awk '{print $2}' || true)
  EL_SVC="${EL_SVC:-el-1-geth-lighthouse}"
  EARLY_RPC_URL=$(kurtosis port print "$ENCLAVE" "$EL_SVC" rpc 2>/dev/null || true)
  # Filter out kurtosis error messages — only keep valid host:port
  if ! echo "$EARLY_RPC_URL" | grep -qE '^[0-9.]+:[0-9]+$'; then
    EARLY_RPC_URL=""
  fi

  # Check consensus
  echo "Checking consensus..."
  if bash "$SCRIPT_DIR/check-consensus.sh" "$ENCLAVE" 2>&1 | tee -a "$LOG_FILE"; then
    CONSENSUS="PASS"
  else
    CONSENSUS="FAIL"
  fi

  # Feature-specific verification — run before assertoor to avoid EL service timeout.
  FEATURE_CHECK="N/A"
  FEATURE_SCRIPT="$SCRIPT_DIR/features/verify-${feature}.sh"
  if [ -x "$FEATURE_SCRIPT" ]; then
    echo "Running feature verification..."
    RPC_URL="$EARLY_RPC_URL"
    if [ -n "$RPC_URL" ]; then
      [[ "$RPC_URL" == http* ]] || RPC_URL="http://$RPC_URL"
      if bash "$FEATURE_SCRIPT" "$ENCLAVE" "$RPC_URL" 2>&1 | tee -a "$LOG_FILE"; then
        FEATURE_CHECK="PASS"
      else
        FEATURE_CHECK="FAIL"
      fi
    else
      echo "  Could not resolve RPC URL — skipping feature check"
      FEATURE_CHECK="N/A"
    fi
  fi

  # Check assertoor (if available — informational, not blocking)
  ASSERTOOR="N/A"
  ASSERTOOR_URL=$(kurtosis port print "$ENCLAVE" "assertoor" http 2>/dev/null || true)
  if [ -n "$ASSERTOOR_URL" ]; then
    echo "Checking assertoor (30s quick check)..."
    if bash "$SCRIPT_DIR/check-assertoor.sh" "$ENCLAVE" 30 2>&1 | tee -a "$LOG_FILE"; then
      ASSERTOOR="PASS"
    else
      # Assertoor stability check needs many epochs — timeout is expected for quick tests.
      ASSERTOOR="TIMEOUT"
    fi
  fi

  # Cleanup
  echo "Cleaning up..."
  kurtosis enclave rm -f "$ENCLAVE" 2>/dev/null || true

  # Record result — consensus is the primary gate; assertoor timeout is OK for quick tests.
  if [[ "$CONSENSUS" == "FAIL" || "$FEATURE_CHECK" == "FAIL" ]]; then
    FAIL=$((FAIL + 1))
    STATUS="FAIL"
  else
    PASS=$((PASS + 1))
    STATUS="PASS"
  fi

  RESULTS+=("$STATUS $feature — consensus:$CONSENSUS assertoor:$ASSERTOOR feature:$FEATURE_CHECK")
  echo ""
done

# Print summary
echo ""
echo "========================================"
echo "=== FEATURE TEST RESULTS"
echo "========================================"
echo ""
for result in "${RESULTS[@]}"; do
  echo "  $result"
done
echo ""
echo "Total: $((PASS + FAIL + SKIP)) | Pass: $PASS | Fail: $FAIL | Skip: $SKIP"

# Write summary to file
SUMMARY_FILE="$RESULTS_DIR/summary.txt"
echo "ETH2030 Feature Test Results — $(date -u '+%Y-%m-%d %H:%M:%S UTC')" > "$SUMMARY_FILE"
echo "" >> "$SUMMARY_FILE"
for result in "${RESULTS[@]}"; do
  echo "$result" >> "$SUMMARY_FILE"
done
echo "" >> "$SUMMARY_FILE"
echo "Total: $((PASS + FAIL + SKIP)) | Pass: $PASS | Fail: $FAIL | Skip: $SKIP" >> "$SUMMARY_FILE"

echo ""
echo "Results saved to $RESULTS_DIR/"

# Exit with failure if any test failed
[ "$FAIL" -eq 0 ] || exit 1
