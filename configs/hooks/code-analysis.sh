#!/usr/bin/env bash
# code-analysis.sh - Consolidated PostToolUse hook for code analysis after edits
#
# Combines functionality from:
#   - complexity-warning.sh: Analyze for high-complexity functions
#   - impact-analysis.sh: LSP-based analysis of external dependencies
#
# This hook is OPTIONAL - can be slow due to LSP calls (~30s first time).
# Disable with AGENTCTL_CODE_ANALYSIS_DISABLED=1
#
# Environment:
#   AGENTCTL_CODE_ANALYSIS_DISABLED - Set to 1 to disable all
#   AGENTCTL_COMPLEXITY_DISABLED - Set to 1 to skip complexity check
#   AGENTCTL_IMPACT_DISABLED - Set to 1 to skip impact analysis
#   AGENTCTL_BIN - Path to agentctl binary
#   AGENTCTL_COMPLEXITY_THRESHOLD - Complexity threshold (default: 15)
#   AGENTCTL_IMPACT_MAX_SYMBOLS - Max symbols to analyze (default: 3)
#   AGENTCTL_IMPACT_TIMEOUT - LSP timeout in seconds (default: 45)

set -euo pipefail

# Ensure child processes are killed when this script is terminated
trap 'kill $(jobs -p) 2>/dev/null || true' SIGTERM SIGINT EXIT

# Check if all analysis is disabled
if [[ "${AGENTCTL_CODE_ANALYSIS_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
COMPLEXITY_THRESHOLD="${AGENTCTL_COMPLEXITY_THRESHOLD:-15}"
MAX_SYMBOLS="${AGENTCTL_IMPACT_MAX_SYMBOLS:-3}"
MAX_REFS="${AGENTCTL_IMPACT_MAX_REFS:-5}"
LSP_TIMEOUT="${AGENTCTL_IMPACT_TIMEOUT:-45}"

# Read hook input from stdin
INPUT=$(cat)

# Extract file path from tool_input
file_path=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

# Skip if no file path
if [[ -z "$file_path" || "$file_path" == "null" ]]; then
  echo '{}'
  exit 0
fi

# Only analyze code files
case "$file_path" in
  *.go|*.py|*.js|*.ts|*.tsx|*.jsx|*.java|*.c|*.cpp|*.rs)
    ;;
  *)
    echo '{}'
    exit 0
    ;;
esac

# Skip test files
case "$file_path" in
  *_test.go|*_test.py|*.test.ts|*.test.js|*.spec.ts|*.spec.js|*__test__*)
    echo '{}'
    exit 0
    ;;
esac

context_parts=()

# =============================================================================
# 1. COMPLEXITY ANALYSIS
# =============================================================================

if [[ "${AGENTCTL_COMPLEXITY_DISABLED:-}" != "1" ]]; then
  input_json=$(jq -nc --arg path "$file_path" --argjson threshold "$COMPLEXITY_THRESHOLD" '{
    path: $path,
    threshold: $threshold,
    analysis_mode: "hotspots",
    max_results: 5
  }')

  result=$("$AGENTCTL_BIN" run --daemon code/complexity --input "$input_json" 2>/dev/null) || true

  if [[ -n "$result" ]]; then
    results=$(echo "$result" | jq -r '.data.results // []')
    high_risk=$(echo "$results" | jq '[.[] | select(.risk_level == "high" or .risk_level == "medium")]')
    risk_count=$(echo "$high_risk" | jq 'length')

    if [[ "$risk_count" -gt 0 ]]; then
      complexity_msg="**Complexity:** $risk_count function(s) with elevated complexity"

      # Add details for top functions
      details=$(echo "$high_risk" | jq -r '
        .[:2] |
        map("- `" + .function + "` (line " + (.line | tostring) + "): cyclomatic=" + (.cyclomatic_complexity | tostring)) |
        join("\n")
      ')

      if [[ -n "$details" ]]; then
        complexity_msg+=$'\n'"$details"
      fi

      context_parts+=("$complexity_msg")
    fi
  fi
fi

# =============================================================================
# 2. IMPACT ANALYSIS (LSP-based)
# =============================================================================

if [[ "${AGENTCTL_IMPACT_DISABLED:-}" != "1" ]]; then
  # Determine LSP skill based on file extension
  lsp_skill=""
  lang=""
  case "$file_path" in
    *.go) lsp_skill="lsp/gopls"; lang="go" ;;
    *.py) lsp_skill="lsp/pylsp"; lang="python" ;;
    *.ts|*.tsx) lsp_skill="lsp/tsserver"; lang="typescript" ;;
    *.js|*.jsx) lsp_skill="lsp/tsserver"; lang="javascript" ;;
  esac

  if [[ -n "$lsp_skill" && -f "$file_path" ]]; then
    # Get symbols from the edited file
    input_json=$(jq -nc --arg path "$file_path" --argjson max "$MAX_SYMBOLS" '{
      path: $path,
      include_private: false,
      max_results: $max
    }')

    symbols_result=$("$AGENTCTL_BIN" run --daemon code/symbols --ephemeral --input "$input_json" 2>/dev/null) || true

    if [[ -n "$symbols_result" ]]; then
      symbols=$(echo "$symbols_result" | jq -r '.data.preview // []')
      symbol_count=$(echo "$symbols" | jq 'length')

      if [[ "$symbol_count" -gt 0 ]]; then
        filename=$(basename "$file_path")
        abs_file_path=$(cd "$(dirname "$file_path")" 2>/dev/null && pwd)/$(basename "$file_path") || abs_file_path="$file_path"
        workspace_root=$(pwd)

        all_refs=""
        analyzed_count=0

        # Process each symbol
        while IFS= read -r symbol; do
          name=$(echo "$symbol" | jq -r '.name')
          line=$(echo "$symbol" | jq -r '.line')
          sym_type=$(echo "$symbol" | jq -r '.type')

          # Skip private symbols
          case "$lang" in
            go) [[ ! "${name:0:1}" =~ [A-Z] ]] && continue ;;
            *) [[ "$name" =~ ^_ ]] && continue ;;
          esac

          # Only analyze functions, methods, types
          case "$sym_type" in
            function|method|type|struct|interface|class) ;;
            *) continue ;;
          esac

          # Find column
          col=6
          if [[ -f "$file_path" ]]; then
            file_line=$(sed -n "${line}p" "$file_path" 2>/dev/null || true)
            if [[ -n "$file_line" ]]; then
              col_raw=$(echo "$file_line" | grep -bo "$name" 2>/dev/null | head -1 | cut -d: -f1 || echo "5")
              col=$((col_raw + 1))
              [[ "$col" -le 1 ]] && col=6
            fi
          fi

          # LSP references query
          lsp_input=$(jq -nc --arg file "$file_path" --argjson line "$line" --argjson col "$col" --argjson max "$MAX_REFS" --argjson timeout "$LSP_TIMEOUT" '{
            textDocument: { uri: $file },
            position: { line: $line, character: $col },
            max_results: $max,
            timeout: $timeout,
            operation: "references"
          }')

          refs_result=$("$AGENTCTL_BIN" run --daemon "$lsp_skill" --ephemeral --input "$lsp_input" 2>/dev/null) || true

          if [[ -n "$refs_result" ]]; then
            ref_count=$(echo "$refs_result" | jq -r --arg self "$abs_file_path" '
              [.data.locations // [] | .[] | select(.file != $self)] | length
            ')

            if [[ "$ref_count" -gt 0 ]]; then
              all_refs+="- \`$name\`: $ref_count external refs"$'\n'
              ((analyzed_count++)) || true
            fi
          fi

          [[ "$analyzed_count" -ge "$MAX_SYMBOLS" ]] && break
        done < <(echo "$symbols" | jq -c '.[]')

        if [[ -n "$all_refs" ]]; then
          context_parts+=("**Impact:** \`$filename\` - external dependencies:"$'\n'"$all_refs")
        fi
      fi
    fi
  fi
fi

# =============================================================================
# OUTPUT
# =============================================================================

if [[ ${#context_parts[@]} -gt 0 ]]; then
  context=$(printf '%s\n\n' "${context_parts[@]}")
  jq -n --arg ctx "$context" '{decision: "approve", context: $ctx}'
else
  echo '{}'
fi
