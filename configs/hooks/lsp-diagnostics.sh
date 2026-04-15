#!/usr/bin/env bash
# Thin PostToolUse diagnostics wrapper: delegate to Go.

set -euo pipefail

if [[ "${FOXCTL_LSP_DIAG_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
if ! command -v "$FOXCTL_BIN" >/dev/null 2>&1; then
  echo '{}'
  exit 0
fi

workspace="${FOXCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"

if ! output="$(printf '%s' "$payload" | "$FOXCTL_BIN" hooks lsp-diagnostics --workspace "$workspace" 2>/dev/null)"; then
  echo '{}'
  exit 0
fi

context="$(printf '%s' "$output" | jq -r '.data.response.context // ""' 2>/dev/null || echo "")"
if [[ -n "$context" && "$context" != "null" ]]; then
  jq -nc --arg ctx "$context" '{decision:"approve", context:$ctx}'
else
  echo '{}'
fi
