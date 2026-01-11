#!/usr/bin/env bash
# context-detect.sh - Quick code context gathering via /context
#
# Claude Code UserPromptSubmit hook:
# - When user includes "/context <query>", runs code/smart_read to gather
#   relevant code snippets without LLM analysis
# - Returns formatted evidence as context injection
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary (default: agentctl)
#   AGENTCTL_CONTEXT_MAX_FILES - Max files to process (default: 5)
#   AGENTCTL_CONTEXT_MODE - Extraction mode: general, structure (default: general)

set -euo pipefail

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
MAX_FILES="${AGENTCTL_CONTEXT_MAX_FILES:-5}"
MODE="${AGENTCTL_CONTEXT_MODE:-general}"

payload="$(cat)"
workspace_root="${CLAUDE_PROJECT_DIR:-$(pwd)}"
prompt="$(printf '%s' "$payload" | jq -r '.prompt // ""' 2>/dev/null || true)"

if [[ -z "$prompt" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Match /context <query>
if ! printf '%s' "$prompt" | grep -qiE '(^|\s)/context(\s|$)'; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Extract query after /context
query="$(printf '%s' "$prompt" | sed -E 's/.*\/context[[:space:]]+//' | sed -E 's/[[:space:]]*$//')"

if [[ -z "$query" || ${#query} -lt 2 ]]; then
  jq -nc '{decision:"approve", context:"**Usage:** `/context <query>`\n\nExample: `/context database connection handling`"}'
  exit 0
fi

# Build input for code/smart_read
input_json=$(jq -nc \
  --arg query "$query" \
  --arg mode "$MODE" \
  --argjson max_files "$MAX_FILES" \
  '{
    query: $query,
    auto_files: true,
    mode: $mode,
    max_files: $max_files
  }')

# Run code/smart_read skill
result=$("$AGENTCTL_BIN" run code/smart_read --ephemeral --workspace "$workspace_root" --input "$input_json" 2>/dev/null) || {
  jq -nc '{decision:"approve", context:"**Context:** Failed to gather code context."}'
  exit 0
}

# Check for errors
if echo "$result" | jq -e '.status == "error"' >/dev/null 2>&1; then
  error_msg=$(echo "$result" | jq -r '.error // "Unknown error"')
  jq -nc --arg err "$error_msg" '{decision:"approve", context:("**Context Error:** " + $err)}'
  exit 0
fi

# Format evidence as markdown
context=$(echo "$result" | jq -r '
  def format_snippet:
    "#### " + .file + ":" + (.start_line | tostring) + "-" + (.end_line | tostring) + "\n" +
    "```" + (.language // "") + "\n" +
    .text + "\n```";

  def format_candidate:
    "`" + .path + "` (" + ((.score * 100) | floor | tostring) + "%)";

  "## Code Context: " + (.data.stats.query // "query") + "\n\n" +
  "**Files:** " + ((.data.stats.files_processed // 0) | tostring) +
  " | **Snippets:** " + ((.data.stats.snippets_found // 0) | tostring) +
  " | **Method:** " + (.data.stats.selection_method // "auto") +
  "\n\n" +
  (if .data.candidates and (.data.candidates | length) > 0 then
    "**Candidates:** " + ([.data.candidates[] | format_candidate] | join(", ")) + "\n\n"
  else "" end) +
  (if .data.evidence.snippets and (.data.evidence.snippets | length) > 0 then
    (.data.evidence.snippets[:10] | map(format_snippet) | join("\n\n"))
  else
    "_No relevant snippets found_"
  end) +
  "\n\n---\n*Use line numbers to read more context.*"
')

jq -nc --arg ctx "$context" '{decision:"approve", context:$ctx}'
