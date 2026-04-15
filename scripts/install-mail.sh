#!/usr/bin/env bash
# install-mail.sh - Install foxctl-mail binary with symlink
#
# This script:
# 1. Builds the foxctl-mail binary
# 2. Installs it to ~/.foxctl/bin/
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

# Ensure FOXCTL_HOME is set
FOXCTL_HOME="${FOXCTL_HOME:-$HOME/.foxctl}"
FOXCTL_BIN_DIR="$FOXCTL_HOME/bin"

echo "Building foxctl-mail..."
cd "$REPO_ROOT"
CGO_ENABLED=0 go build -o "$FOXCTL_BIN_DIR/foxctl-mail" ./cmd/foxctl-mail

echo "Installed to: $FOXCTL_BIN_DIR/foxctl-mail"

# Create symlink directory if needed
mkdir -p "$LINK_DIR"

# Create symlink
SYMLINK_PATH="$LINK_DIR/foxctl-mail"
if [[ -e "$SYMLINK_PATH" ]]; then
  if [[ -L "$SYMLINK_PATH" ]]; then
    rm "$SYMLINK_PATH" || { echo "Failed to remove existing symlink: $SYMLINK_PATH"; exit 1; }
  else
    rm -f "$SYMLINK_PATH" || { echo "Failed to remove existing file: $SYMLINK_PATH"; exit 1; }
  fi
fi

ln -s "$FOXCTL_BIN_DIR/foxctl-mail" "$SYMLINK_PATH" || { echo "Failed to create symlink: $SYMLINK_PATH"; exit 1; }
echo "Symlinked: $SYMLINK_PATH -> $FOXCTL_BIN_DIR/foxctl-mail"

# Verify it's in PATH
if command -v foxctl-mail &>/dev/null; then
  echo "✓ foxctl-mail is in PATH"
else
  echo ""
  echo "⚠️  foxctl-mail is not in PATH. Add this to your shell profile:"
  echo "    export PATH=\"\$PATH:$LINK_DIR\""
fi

echo ""
echo "Usage:"
echo "  foxctl-mail \"Subject\" \"Body\""
echo "  foxctl-mail -p 1 \"URGENT\" \"Stop and review\""
echo "  foxctl-mail --ack \"Review needed\" \"Check the API changes\""
