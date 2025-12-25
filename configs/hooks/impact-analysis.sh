#!/usr/bin/env bash
# impact-analysis.sh - Claude Code PostToolUse hook for LSP-based impact analysis
# After editing a file, shows what other code depends on the edited symbols.
# Uses LSP servers for references, incoming calls, and implementations.
#
# Features:
#   - References: Find all usages of edited symbols
#   - Incoming calls: Show functions that call edited functions
#   - Implementations: Show types implementing edited interfaces
#
# Supported languages:
#   - Go (.go) → gopls (direct)
#   - Python (.py) → lsp/pylsp
#   - TypeScript/JavaScript (.ts, .tsx, .js, .jsx) → lsp/tsserver
#
# Environment:
#   AGENTCTL_IMPACT_DISABLED - Set to 1 to disable (enabled by default)
#   AGENTCTL_BIN - Path to agentctl binary (default: agentctl)
#   AGENTCTL_IMPACT_MAX_SYMBOLS - Max symbols to analyze (default: 3)
#   AGENTCTL_IMPACT_MAX_REFS - Max references per symbol (default: 5)
#   AGENTCTL_IMPACT_TIMEOUT - LSP timeout in seconds (default: 45)
#
# Note: First LSP request takes ~30-40s as gopls loads packages.
# Hook runs asynchronously so this doesn't block the user.

set -euo pipefail

# Disable with AGENTCTL_IMPACT_DISABLED=1
if [[ "${AGENTCTL_IMPACT_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
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

# Determine language and LSP skill based on file extension
lsp_skill=""
lang=""
case "$file_path" in
  *.go)
    lsp_skill="lsp/gopls"
    lang="go"
    ;;
  *.py)
    lsp_skill="lsp/pylsp"
    lang="python"
    ;;
  *.ts|*.tsx)
    lsp_skill="lsp/tsserver"
    lang="typescript"
    ;;
  *.js|*.jsx)
    lsp_skill="lsp/tsserver"
    lang="javascript"
    ;;
  *)
    echo '{}'
    exit 0
    ;;
esac

# Skip test files - they don't typically have external dependents
case "$file_path" in
  *_test.go|*_test.py|*.test.ts|*.test.js|*.spec.ts|*.spec.js|*__test__*)
    echo '{}'
    exit 0
    ;;
esac

# Skip if file doesn't exist
if [[ ! -f "$file_path" ]]; then
  echo '{}'
  exit 0
fi

# Function to check if symbol is public/exported based on language
is_public_symbol() {
  local name="$1"
  local sym_type="$2"
  local language="$3"

  case "$language" in
    go)
      # Go: uppercase first letter means exported
      first_char="${name:0:1}"
      [[ "$first_char" =~ [A-Z] ]]
      ;;
    python)
      # Python: no underscore prefix means public
      [[ ! "$name" =~ ^_ ]]
      ;;
    typescript|javascript)
      # TS/JS: assume all named functions/classes are worth checking
      # (export detection would require parsing)
      [[ ! "$name" =~ ^_ ]]
      ;;
  esac
}

# Get symbols from the edited file
input_json=$(jq -nc --arg path "$file_path" --argjson max "$MAX_SYMBOLS" '{
  path: $path,
  include_private: false,
  max_results: $max
}')
symbols_result=$("$AGENTCTL_BIN" run code/symbols --input "$input_json" 2>/dev/null) || {
  echo '{}'
  exit 0
}

# Extract functions and types
symbols=$(echo "$symbols_result" | jq -r '.data.preview // []')
symbol_count=$(echo "$symbols" | jq 'length')

if [[ "$symbol_count" -eq 0 ]]; then
  echo '{}'
  exit 0
fi

# Get filename for display
filename=$(basename "$file_path")

# Collect all external references
all_refs=""
analyzed_count=0

# Process each symbol (functions and types)
while IFS= read -r symbol; do
  name=$(echo "$symbol" | jq -r '.name')
  line=$(echo "$symbol" | jq -r '.line')
  sym_type=$(echo "$symbol" | jq -r '.type')

  # Skip private symbols based on language conventions
  if ! is_public_symbol "$name" "$sym_type" "$lang"; then
    continue
  fi

  # Only analyze functions, methods, types, classes
  case "$sym_type" in
    function|method|type|struct|interface|class)
      ;;
    *)
      continue
      ;;
  esac

  # Find the column where the symbol name appears
  file_line=$(sed -n "${line}p" "$file_path" 2>/dev/null)
  col=$(echo "$file_line" | grep -bo "$name" 2>/dev/null | head -1 | cut -d: -f1)
  col=$((col + 1))  # grep is 0-indexed, LSP expects 1-indexed
  [[ "$col" -le 1 ]] && col=6  # fallback

  abs_file_path=$(cd "$(dirname "$file_path")" && pwd)/$(basename "$file_path")
  workspace_root=$(pwd)

  # Collect impact info for this symbol
  impact_info=""

  # Use agentctl LSP skills (first request slow ~30s, subsequent faster)
  # Use TextDocumentPositionParams-shaped structure with custom params at top level
  lsp_input=$(jq -nc --arg file "$file_path" --argjson line "$line" --argjson col "$col" --argjson max "$MAX_REFS" --argjson timeout "$LSP_TIMEOUT" '{
    textDocument: { uri: $file },
    position: { line: $line, character: $col },
    max_results: $max,
    timeout: $timeout
  }')

  case "$lang" in
    go)
      # For interfaces: get implementations
      if [[ "$sym_type" == "interface" ]]; then
        impl_result=$("$AGENTCTL_BIN" run lsp/gopls --input "$(echo "$lsp_input" | jq '. + {operation: "implementation"}')" 2>/dev/null) || true
        if [[ -n "$impl_result" ]]; then
          impl_list=$(echo "$impl_result" | jq -r --arg self "$abs_file_path" '
            .data.locations // [] | map(select(.file != $self)) | .[:'$MAX_REFS'] |
            map(.file + ":" + (.line | tostring)) | join(", ")
          ')
          if [[ -n "$impl_list" && "$impl_list" != "null" ]]; then
            impact_info+="impls: $impl_list; "
          fi
        fi
      fi

      # Get references
      refs_result=$("$AGENTCTL_BIN" run lsp/gopls --input "$(echo "$lsp_input" | jq '. + {operation: "references"}')" 2>/dev/null) || true
      refs_raw=$(echo "$refs_result" | jq -r '.data.locations // [] | map(.file + ":" + (.line | tostring)) | .[]')
      ;;
    *)
      # For other languages, use their LSP skill
      refs_result=$("$AGENTCTL_BIN" run "$lsp_skill" --input "$(echo "$lsp_input" | jq '. + {operation: "references"}')" 2>/dev/null) || true
      refs_raw=$(echo "$refs_result" | jq -r '.data.locations // [] | map(.file + ":" + (.line | tostring)) | .[]')
      ;;
  esac

  # Count external references
  ref_count=0
  ref_files=""
  while IFS= read -r ref_line; do
    [[ -z "$ref_line" ]] && continue
    ref_file=$(echo "$ref_line" | cut -d: -f1)
    [[ "$ref_file" == "$abs_file_path" ]] && continue
    ref_file_rel="${ref_file#$workspace_root/}"
    ref_files+="$ref_file_rel, "
    ((ref_count++)) || true
    [[ "$ref_count" -ge "$MAX_REFS" ]] && break
  done <<< "$refs_raw"

  if [[ "$ref_count" -gt 0 ]]; then
    impact_info+="$ref_count refs in: ${ref_files%, }"
  fi

  if [[ -n "$impact_info" ]]; then
    all_refs+="- \`$name\` ($sym_type): $impact_info
"
    ((analyzed_count++)) || true
  fi

  # Limit symbols analyzed
  if [[ "$analyzed_count" -ge "$MAX_SYMBOLS" ]]; then
    break
  fi
done < <(echo "$symbols" | jq -c '.[]')

# If no external references found, skip
if [[ -z "$all_refs" ]]; then
  echo '{}'
  exit 0
fi

# Build concise context message
context="**Impact:** \`$filename\` - external dependencies found:
$all_refs"

# Return approve with context (non-blocking advisory)
jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
