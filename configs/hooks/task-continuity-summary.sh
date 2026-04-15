#!/usr/bin/env bash
# Thin wrapper for prompt-ready task continuity summaries.
# Use `foxctl context task-history-summary` directly for Codex/agent/script callers.

set -euo pipefail

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"
if ! command -v "$FOXCTL_BIN" >/dev/null 2>&1; then
  echo '{}'
  exit 0
fi

workspace="${FOXCTL_WORKSPACE:-${CLAUDE_PROJECT_DIR:-$(pwd)}}"
vault_path="${FOXCTL_ACA_VAULT_PATH:-${FOXCTL_OBSIDIAN_VAULT_PATH:-}}"

args=(context task-history-summary --workspace "$workspace")
if [[ -n "${vault_path}" ]]; then
  args+=(--vault-path "$vault_path")
fi
if [[ -n "${FOXCTL_TRANSCRIPT_HISTORY_SCOPE:-}" ]]; then
  args+=(--transcript-history-scope "$FOXCTL_TRANSCRIPT_HISTORY_SCOPE")
fi

if ! output="$("$FOXCTL_BIN" "${args[@]}" 2>/dev/null)"; then
  echo '{}'
  exit 0
fi

rendered="$(printf '%s' "$output" | jq -r '.data.rendered // ""' 2>/dev/null || echo "")"
artifact="$(printf '%s' "$output" | jq -r '.data.artifact // ""' 2>/dev/null || echo "")"

if [[ -n "$rendered" && "$rendered" != "null" ]]; then
  jq -nc --arg ctx "$rendered" --arg artifact "$artifact" '{
    hookSpecificOutput: {
      hookEventName: "UserPromptSubmit",
      additionalContext: $ctx,
      metadata: (
        if $artifact != "" and $artifact != "null" then
          {task_continuity_artifact: $artifact}
        else
          {}
        end
      )
    }
  }'
else
  echo '{}'
fi
