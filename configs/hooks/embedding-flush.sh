#!/usr/bin/env bash
# Thin Stop hook embedding flush wrapper: delegate to Go.

set -euo pipefail

if [[ "${FOXCTL_EMBED_QUEUE:-1}" == "0" ]]; then
  exit 0
fi

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
if ! command -v "$FOXCTL_BIN" >/dev/null 2>&1; then
  exit 0
fi

workspace="${FOXCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
"$FOXCTL_BIN" hooks embedding-flush --workspace "$workspace" >/dev/null 2>&1 || true
exit 0
