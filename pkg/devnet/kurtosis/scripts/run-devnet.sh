#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
CONFIG_DIR="$SCRIPT_DIR/../configs"
PKG_DIR="$SCRIPT_DIR/../../../"
# Workspace root (eth2030/), for the pinned ethereum-package in refs/
ROOT_DIR="$SCRIPT_DIR/../../../../"

CONFIG="${1:-single-client}"
CONFIG_FILE="$CONFIG_DIR/$CONFIG.yaml"

if [ ! -f "$CONFIG_FILE" ]; then
    echo "Error: Config not found: $CONFIG_FILE"
    echo "Available configs:"
    ls "$CONFIG_DIR"/*.yaml 2>/dev/null | xargs -n1 basename | sed 's/.yaml//' | sed 's/^/  /'
    exit 1
fi

if ! command -v kurtosis &>/dev/null; then
    echo "Error: kurtosis CLI not found."
    echo "Install: https://docs.kurtosis.com/install/"
    exit 1
fi

ENCLAVE="${2:-eth2030-devnet}"
# Default to the pinned local ethereum-package (refs/, read-only submodule) for
# reproducible devnets. Override with KURTOSIS_PACKAGE=github.com/ethpandaops/ethereum-package
# to follow upstream master, or point to any local package path.
KURTOSIS_PACKAGE="${KURTOSIS_PACKAGE:-$ROOT_DIR/refs/ethereum-package}"

if [ "${SKIP_EL_IMAGE_BUILD:-0}" = "1" ]; then
    echo "=== Reusing existing eth2030:local Docker image ==="
else
    echo "=== Building eth2030-geth Docker image ==="
    docker build -t eth2030:local "$PKG_DIR"
fi

echo ""
echo "=== Launching devnet: $CONFIG ==="
echo "=== Kurtosis package: $KURTOSIS_PACKAGE ==="
kurtosis run "$KURTOSIS_PACKAGE" \
    --args-file "$CONFIG_FILE" \
    --enclave "$ENCLAVE"

echo ""
echo "=== Devnet running in enclave: $ENCLAVE ==="
echo ""
echo "Inspect:  kurtosis enclave inspect $ENCLAVE"
echo "Logs:     kurtosis service logs $ENCLAVE <service>"
echo ""

# Print tool URLs
for svc in dora assertoor grafana blobscan forky tracoor forkmon; do
    URL=$(kurtosis port print "$ENCLAVE" "$svc" http 2>/dev/null || true)
    if [ -n "$URL" ]; then
        echo "$svc: $URL"
    fi
done
