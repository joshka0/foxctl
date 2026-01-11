#!/usr/bin/env bash
# memory-capture.sh - Capture edit context to agentctl memory
#
# PostToolUse hook for Edit/Write that stores change context to memory.
# Enabled by default. Set AGENTCTL_MEMORY_CAPTURE=0 to disable.

set -euo pipefail

# Check if memory capture is disabled
if [[ "${AGENTCTL_MEMORY_CAPTURE:-1}" == "0" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Find agentctl binary
if [[ -n "${AGENTCTL_BIN:-}" ]]; then
  : # Use provided path
elif command -v agentctl &>/dev/null; then
  AGENTCTL_BIN="agentctl"
elif [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/agentctl" ]]; then
  AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/agentctl"
else
  echo '{"decision":"approve"}'
  exit 0
fi

# Read hook input
payload="$(cat)"

# Extract tool info
tool_name=$(printf '%s' "$payload" | jq -r '.tool_name // ""')
file_path=$(printf '%s' "$payload" | jq -r '.tool_input.file_path // .tool_input.path // ""')

# Skip if no file path
if [[ -z "$file_path" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Get workspace and relative path
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"
rel_path="${file_path#"$workspace"/}"

# Extract change context (old_string/new_string for Edit, content preview for Write)
change_type=""
change_summary=""

case "$tool_name" in
  Edit)
    old_str=$(printf '%s' "$payload" | jq -r '.tool_input.old_string // ""' | head -c 100)
    new_str=$(printf '%s' "$payload" | jq -r '.tool_input.new_string // ""' | head -c 100)
    change_type="edit"
    change_summary="replaced '${old_str:0:50}...' with '${new_str:0:50}...'"
    ;;
  Write)
    # Extract full content to compute actual length
    content_full=$(printf '%s' "$payload" | jq -r '.tool_input.content // ""')
    content_len=${#content_full}
    change_type="write"
    change_summary="wrote ${content_len} chars"
    ;;
  *)
    change_type="$tool_name"
    change_summary="modified"
    ;;
esac

# Build memory envelope
# Note: Using deterministic name (no timestamp) so memory-embed.sh can find it
# This means repeated edits to same file update the existing memory entry
memory_name="edit:${rel_path}"
memory_summary="$change_type $rel_path: $change_summary"

memory_envelope=$(jq -nc \
  --arg file "$rel_path" \
  --arg type "$change_type" \
  --arg summary "$change_summary" \
  --arg ts "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  '{
    version: 1,
    status: "ok",
    command: "memory/edit",
    data: {
      file: $file,
      change_type: $type,
      summary: $summary,
      timestamp: $ts
    }
  }')

# Store to named memory (fire and forget)
printf '%s' "$memory_envelope" | "$AGENTCTL_BIN" memory put \
  --name "$memory_name" \
  --type "edit" \
  --summary "$memory_summary" \
  --workspace "$workspace" \
  --file - &>/dev/null || true

echo '{"decision":"approve"}'
