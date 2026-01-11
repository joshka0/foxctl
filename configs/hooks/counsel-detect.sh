#!/usr/bin/env bash
# counsel-detect.sh - Run code/counsel multi-perspective analysis via /counsel
#
# Claude Code UserPromptSubmit hook:
# - When user includes "/counsel <question>", runs code/counsel with LLM analysis
# - Returns formatted findings as context injection
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary (default: agentctl)
#   AGENTCTL_COUNSEL_PERSPECTIVES - Comma-separated perspectives (default: security,correctness)
#   AGENTCTL_COUNSEL_MAX_FILES - Max files to analyze (default: 8)

set -euo pipefail

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
PERSPECTIVES="${AGENTCTL_COUNSEL_PERSPECTIVES:-security,correctness}"
MAX_FILES="${AGENTCTL_COUNSEL_MAX_FILES:-8}"

payload="$(cat)"
workspace_root="${CLAUDE_PROJECT_DIR:-$(pwd)}"
prompt="$(printf '%s' "$payload" | jq -r '.prompt // ""' 2>/dev/null || true)"

if [[ -z "$prompt" ]]; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Match /counsel <question>
if ! printf '%s' "$prompt" | grep -qiE '(^|\s)/counsel(\s|$)'; then
  echo '{"decision":"approve"}'
  exit 0
fi

# Extract question after /counsel
question="$(printf '%s' "$prompt" | sed -E 's/.*\/counsel[[:space:]]+//' | sed -E 's/[[:space:]]*$//')"

if [[ -z "$question" || ${#question} -lt 3 ]]; then
  jq -nc '{decision:"approve", context:"**Usage:** `/counsel <question>`\n\nExample: `/counsel review auth flow for security issues`"}'
  exit 0
fi

# Convert perspectives to JSON array
IFS=',' read -ra persp_arr <<< "$PERSPECTIVES"
persp_json=$(printf '%s\n' "${persp_arr[@]}" | jq -R . | jq -s .)

# Build input for code/counsel
input_json=$(jq -nc \
  --arg query "$question" \
  --argjson perspectives "$persp_json" \
  --argjson max_files "$MAX_FILES" \
  '{
    query: $query,
    auto_files: true,
    perspectives: $perspectives,
    max_files: $max_files
  }')

# Run code/counsel skill (may take time due to LLM calls)
result=$("$AGENTCTL_BIN" run code/counsel --ephemeral --workspace "$workspace_root" --input "$input_json" 2>/dev/null) || {
  jq -nc '{decision:"approve", context:"**Counsel:** Analysis failed. Check API keys (ANTHROPIC_API_KEY, OPENAI_API_KEY, or CEREBRAS_API_KEY)."}'
  exit 0
}

# Check for errors
if echo "$result" | jq -e '.status == "error"' >/dev/null 2>&1; then
  error_msg=$(echo "$result" | jq -r '.error // "Unknown error"')
  jq -nc --arg err "$error_msg" '{decision:"approve", context:("**Counsel Error:** " + $err)}'
  exit 0
fi

# Format findings as markdown
context=$(echo "$result" | jq -r '
  def format_finding:
    "- **" + .title + "** (" + (.severity // "info") + ")\n  " + .description +
    (if .location then "\n  Location: `" + .location + "`" else "" end);

  def format_analysis:
    "### " + (.perspective | ascii_upcase) + "\n\n" +
    (if .findings and (.findings | length) > 0
     then (.findings | map(format_finding) | join("\n"))
     else "_No issues found_" end) +
    "\n\n**Summary:** " + (.summary // "No summary");

  "## Code Counsel Analysis\n\n**Query:** " + (.data.query // "unknown") + "\n\n" +
  (if .data.analyses then
    (.data.analyses | map(format_analysis) | join("\n\n---\n\n"))
  else
    "_No analysis results_"
  end) +
  "\n\n---\n**Files analyzed:** " + ((.data.stats.files_analyzed // 0) | tostring) +
  " | **Latency:** " + ((.data.stats.latency_ms // 0) | tostring) + "ms" +
  " | **Provider:** " + (.data.stats.provider // "unknown")
')

jq -nc --arg ctx "$context" '{decision:"approve", context:$ctx}'
