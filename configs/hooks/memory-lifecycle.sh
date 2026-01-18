#!/usr/bin/env bash
# memory-lifecycle.sh - Consolidated PostToolUse hook for memory operations
# Last tested: 2026-01-14
#
# Combines functionality from:
#   - memory-capture.sh: Capture edit context to agentctl memory
#   - memory-embed.sh: Generate embeddings for memory updates
#   - memory-prompt.sh: Prompt to save memories when completing tasks
#
# Triggers:
#   - Edit/Write/MultiEdit/NotebookEdit: Capture changes and embed
#   - TodoWrite: Prompt to save learnings from completed tasks
#   - MCP memory tools: Embed on set/append operations
#
# Environment:
#   AGENTCTL_MEMORY_CAPTURE=0 - Disable edit capture
#   AGENTCTL_MEMORY_EMBED=0 - Disable embedding
#   AGENTCTL_MEMORY_PROMPT_DISABLED=1 - Disable completion prompts

set -euo pipefail

# Find agentctl binary
AGENTCTL_BIN="${AGENTCTL_BIN:-}"
if [[ -z "$AGENTCTL_BIN" ]]; then
  if command -v agentctl &>/dev/null; then
    AGENTCTL_BIN="agentctl"
  elif [[ -x "${CLAUDE_PROJECT_DIR:-}/bin/agentctl" ]]; then
    AGENTCTL_BIN="${CLAUDE_PROJECT_DIR}/bin/agentctl"
  else
    echo '{"decision":"approve"}'
    exit 0
  fi
fi

# Read hook input
payload="$(cat)"

# Extract tool info
tool_name=$(printf '%s' "$payload" | jq -r '.tool_name // ""')
workspace="${CLAUDE_PROJECT_DIR:-$(pwd)}"

# =============================================================================
# TodoWrite: Prompt to save memories when completing tasks
# =============================================================================

if [[ "$tool_name" == "TodoWrite" ]]; then
  # Check if disabled
  if [[ "${AGENTCTL_MEMORY_PROMPT_DISABLED:-}" == "1" ]]; then
    echo '{"decision":"approve"}'
    exit 0
  fi

  # Extract todos
  todos=$(printf '%s' "$payload" | jq -c '.tool_input.todos // []')

  if [[ "$todos" == "[]" || "$todos" == "null" ]]; then
    echo '{"decision":"approve"}'
    exit 0
  fi

  # Count completed tasks
  completed_count=$(printf '%s' "$todos" | jq '[.[] | select(.status == "completed")] | length')

  if [[ "$completed_count" -eq 0 ]]; then
    echo '{"decision":"approve"}'
    exit 0
  fi

  # Get the names of completed tasks
  completed_tasks=$(printf '%s' "$todos" | jq -r '[.[] | select(.status == "completed") | .content] | join(", ")')

  # Build the reminder message
  if [[ "$completed_count" -eq 1 ]]; then
    hint="**Memory prompt:** Task completed: \"$completed_tasks\"

If you learned something useful or encountered a gotcha, save it:
\`agentctl memory put --name \"gotcha-<topic>\" --type gotcha --summary \"<learning>\"\`"
  else
    hint="**Memory prompt:** Completed $completed_count tasks.

If you learned something useful or encountered gotchas, save them:
\`agentctl memory put --name \"gotcha-<topic>\" --type gotcha --summary \"<learning>\"\`"
  fi

  jq -nc --arg hint "$hint" '{decision: "approve", context: $hint}'
  exit 0
fi

# =============================================================================
# Edit/Write: Capture changes and trigger embedding
# =============================================================================

case "$tool_name" in
  Edit|Write|MultiEdit|NotebookEdit)
    file_path=$(printf '%s' "$payload" | jq -r '.tool_input.file_path // .tool_input.path // ""')

    if [[ -z "$file_path" ]]; then
      echo '{"decision":"approve"}'
      exit 0
    fi

    rel_path="${file_path#"$workspace"/}"

    # --- Memory Capture ---
    if [[ "${AGENTCTL_MEMORY_CAPTURE:-1}" != "0" ]]; then
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

      # Store to named memory (fire and forget)
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

      printf '%s' "$memory_envelope" | "$AGENTCTL_BIN" memory put \
        --name "$memory_name" \
        --type "edit" \
        --summary "$memory_summary" \
        --workspace "$workspace" \
        --file - &>/dev/null || true
    fi

    # --- Memory Embedding ---
    # Auto-enable when VOYAGE_API_KEY or GEMINI_API_KEY is set
    if [[ "${AGENTCTL_MEMORY_EMBED:-1}" != "0" ]]; then
      if [[ -n "${VOYAGE_API_KEY:-}" || -n "${GEMINI_API_KEY:-}" ]]; then
        memory_name="edit:${rel_path}"

        # Small delay to ensure capture has stored the entry, then embed
        (
          sleep 1
          "$AGENTCTL_BIN" run embedding/refresh --input "$(jq -nc \
            --arg scope "memory" \
            --arg name "$memory_name" \
            --arg workspace "$workspace" \
            '{scope: $scope, name: $name, workspace: $workspace}'
          )" &>/dev/null
        ) &
      fi
    fi
    ;;

  # --- MCP Memory Tools ---
  *memory*|*Memory*)
    if [[ "${AGENTCTL_MEMORY_EMBED:-1}" != "0" ]]; then
      if [[ -n "${VOYAGE_API_KEY:-}" || -n "${GEMINI_API_KEY:-}" ]]; then
        operation=$(printf '%s' "$payload" | jq -r '.tool_input.operation // ""')

        if [[ "$operation" == "set" || "$operation" == "append" ]]; then
          name=$(printf '%s' "$payload" | jq -r '.tool_input.name // ""')

          if [[ -n "$name" ]]; then
            # Fire and forget - don't block the tool
            "$AGENTCTL_BIN" run embedding/refresh --input "$(jq -nc \
              --arg scope "memory" \
              --arg name "$name" \
              --arg workspace "$workspace" \
              '{scope: $scope, name: $name, workspace: $workspace}'
            )" &>/dev/null &
          fi
        fi
      fi
    fi
    ;;
esac

echo '{"decision":"approve"}'
