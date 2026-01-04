#!/usr/bin/env bash
# install-mail.sh - Install agentctl-mail binary with symlink
#
# This script:
# 1. Builds the agentctl-mail binary
# 2. Installs it to ~/.agentctl/bin/
# 3. Creates a symlink in a PATH-accessible location
#
# Usage:
#   ./scripts/install-mail.sh
#   ./scripts/install-mail.sh --link-to /usr/local/bin
#   ./scripts/install-mail.sh --link-to ~/.local/bin

set -euo pipefail

# Parse arguments
LINK_DIR="${HOME}/.local/bin"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --link-to)
      LINK_DIR="$2"
      shift 2
      ;;
    *)
      echo "Unknown option: $1" >&2
      exit 1
      ;;
  esac
done

# Resolve script directory and repo root
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

# Ensure AGENTCTL_HOME is set
AGENTCTL_HOME="${AGENTCTL_HOME:-$HOME/.agentctl}"
AGENTCTL_BIN_DIR="$AGENTCTL_HOME/bin"

echo "Building agentctl-mail..."
cd "$REPO_ROOT"
CGO_ENABLED=0 go build -o "$AGENTCTL_BIN_DIR/agentctl-mail" ./cmd/agentctl-mail

echo "Installed to: $AGENTCTL_BIN_DIR/agentctl-mail"

# Create symlink directory if needed
mkdir -p "$LINK_DIR"

# Create symlink
SYMLINK_PATH="$LINK_DIR/agentctl-mail"
if [[ -e "$SYMLINK_PATH" ]]; then
  if [[ -L "$SYMLINK_PATH" ]]; then
    rm "$SYMLINK_PATH" || { echo "Failed to remove existing symlink: $SYMLINK_PATH"; exit 1; }
  else
    rm -f "$SYMLINK_PATH" || { echo "Failed to remove existing file: $SYMLINK_PATH"; exit 1; }
  fi
fi

ln -s "$AGENTCTL_BIN_DIR/agentctl-mail" "$SYMLINK_PATH" || { echo "Failed to create symlink: $SYMLINK_PATH"; exit 1; }
echo "Symlinked: $SYMLINK_PATH -> $AGENTCTL_BIN_DIR/agentctl-mail"

# Verify it's in PATH
if command -v agentctl-mail &>/dev/null; then
  echo "✓ agentctl-mail is in PATH"
else
  echo ""
  echo "⚠️  agentctl-mail is not in PATH. Add this to your shell profile:"
  echo "    export PATH=\"\$PATH:$LINK_DIR\""
fi

echo ""
echo "Usage:"
echo "  agentctl-mail \"Subject\" \"Body\""
echo "  agentctl-mail -p 1 \"URGENT\" \"Stop and review\""
echo "  agentctl-mail --ack \"Review needed\" \"Check the API changes\""
