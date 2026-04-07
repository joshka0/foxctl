#!/usr/bin/env bash
set -euo pipefail

# Idempotent environment setup for room-sandbox mission
# Runs at the start of each worker session

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
cd "$REPO_ROOT"

# Install Go dependencies (idempotent)
echo "Ensuring Go dependencies..."
go mod download

# Verify essential tools
for tool in git tmux zellij; do
    if ! command -v "$tool" &>/dev/null; then
        echo "WARNING: $tool not found in PATH. Some features may not work."
    fi
done

echo "Environment ready."
