#!/usr/bin/env bash
set -euo pipefail

# Idempotent environment setup for the TUI mission.
# Runs at the start of each worker session.

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

# Go dependencies (idempotent)
echo "[init] go mod download ..."
go mod download

# Verify pinned framework is still on the expected line.
if ! grep -q "github.com/grindlemire/go-tui v0.11.0" go.mod; then
    echo "[init] WARNING: github.com/grindlemire/go-tui is not pinned to v0.11.0 in go.mod."
    echo "[init]          VAL-CROSS-005 asserts this pin is unchanged at mission end."
fi

# Verify required tools are on PATH.
for tool in git go gofumpt golangci-lint make; do
    if ! command -v "$tool" &>/dev/null; then
        echo "[init] WARNING: $tool not found in PATH."
    fi
done

# Ensure bin/ exists for builds.
mkdir -p "$REPO_ROOT/bin"

echo "[init] Environment ready."
