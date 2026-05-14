#!/usr/bin/env bash
# Thin wrapper for hook-ready next prepared proposal-merge packets.

set -euo pipefail

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
if ! command -v "$FOXCTL_BIN" >/dev/null 2>&1; then
  echo '{}'
  exit 0
fi

workspace="${FOXCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
payload="$(cat)"
vault_path="${FOXCTL_CONTEXTWIKI_VAULT_PATH:-${FOXCTL_OBSIDIAN_VAULT_PATH:-$(printf '%s' "$payload" | jq -r '.vault_path // ""' 2>/dev/null || echo "")}}"

args=(hooks proposal-next-merge --workspace "$workspace")
if [[ -n "$vault_path" ]]; then
  args+=(--vault-path "$vault_path")
fi
args+=(--claim)

if ! output="$(printf '%s' "$payload" | "$FOXCTL_BIN" "${args[@]}" 2>/dev/null)"; then
  echo '{}'
  exit 0
fi

context="$(printf '%s' "$output" | jq -r '.data.response.context // ""' 2>/dev/null || echo "")"
metadata="$(printf '%s' "$output" | jq -c '.data.response.metadata // {}' 2>/dev/null || echo '{}')"

if [[ -n "$context" && "$context" != "null" ]]; then
  jq -nc --arg ctx "$context" --argjson meta "$metadata" '{
    hookSpecificOutput: {
      hookEventName: "UserPromptSubmit",
      additionalContext: $ctx,
      metadata: $meta
    }
  }'
else
  echo '{}'
fi
