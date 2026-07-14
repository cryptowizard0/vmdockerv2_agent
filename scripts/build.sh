#!/usr/bin/env bash
# Build the vmdockerv2_agent /vmm adapter entrypoint binary that vmdockerv2 bakes
# into module images (via VMDOCKER_AGENT_BIN). Produces a static linux binary.
#
# Usage: scripts/build.sh [GOARCH]   (default: $GOARCH or the host arch)
set -euo pipefail
export GOPRIVATE="${GOPRIVATE:-github.com/hymatrix,github.com/xingj404-lab}"
cd "$(dirname "$0")/.."
arch="${1:-${GOARCH:-$(go env GOARCH)}}"
out="build/vmdocker-agent"
mkdir -p build
GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -o "$out" .
echo "built $out (linux/$arch)"
