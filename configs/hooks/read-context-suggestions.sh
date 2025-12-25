#!/usr/bin/env bash
# read-context-suggestions.sh - PostToolUse hook for Read that suggests context exploration
# After reading a code file, suggests commands to explore related context:
# - Usages of key exported symbols (functions, types) via context_ripgrep (full function bodies)
# - Related files via imports
# - Editing symbols via code/smart_write
#
# Environment:
#   AGENTCTL_BIN - Path to agentctl binary (default: agentctl)
#   AGENTCTL_READ_SUGGESTIONS_DISABLED - Set to 1 to disable
#   AGENTCTL_READ_SUGGESTIONS_MAX_SYMBOLS - Max symbols to suggest (default: 5)

set -euo pipefail

if [[ "${AGENTCTL_READ_SUGGESTIONS_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

AGENTCTL_BIN="${AGENTCTL_BIN:-agentctl}"
MAX_SYMBOLS="${AGENTCTL_READ_SUGGESTIONS_MAX_SYMBOLS:-5}"

INPUT=$(cat)
file_path=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')
tool_result=$(echo "$INPUT" | jq -r '.tool_result // ""')

# Skip if no file path or file doesn't exist
if [[ -z "$file_path" || "$file_path" == "null" || ! -f "$file_path" ]]; then
  echo '{}'
  exit 0
fi

# Resolve to absolute path for agentctl
abs_path=$(cd "$(dirname "$file_path")" && pwd)/$(basename "$file_path")

# Only analyze code files
case "$file_path" in
  *.go|*.py|*.js|*.ts|*.tsx|*.jsx|*.java|*.c|*.cpp|*.h|*.hpp|*.rs|*.rb|*.php)
    ;;
  *)
    echo '{}'
    exit 0
    ;;
esac

# Skip very large files (>50KB) - they're likely generated
file_size=$(stat -f%z "$file_path" 2>/dev/null || stat -c%s "$file_path" 2>/dev/null || echo "0")
if [[ "$file_size" -gt 51200 ]]; then
  echo '{}'
  exit 0
fi

filename=$(basename "$file_path")
dir=$(dirname "$file_path")

# Extract language-specific info
lang=""
case "$file_path" in
  *.go) lang="go" ;;
  *.py) lang="python" ;;
  *.ts|*.tsx) lang="typescript" ;;
  *.js|*.jsx) lang="javascript" ;;
  *.java) lang="java" ;;
  *.rs) lang="rust" ;;
  *.rb) lang="ruby" ;;
  *.c|*.h) lang="c" ;;
  *.cpp|*.hpp) lang="cpp" ;;
  *.php) lang="php" ;;
  *) lang="unknown" ;;
esac

# Run symbols extraction for exported/public symbols (use absolute path)
input_json=$(jq -nc --arg path "$abs_path" '{
  path: $path,
  include_private: false,
  max_results: 20
}')
symbols_result=$("$AGENTCTL_BIN" run code/symbols --input "$input_json" 2>/dev/null) || symbols_result="{}"

# Extract key symbols (functions, types, structs)
key_functions=$(echo "$symbols_result" | jq -r --argjson max "$MAX_SYMBOLS" '
  [.data.preview // [] | .[] | select(.type == "function" or .type == "method") | .name] | unique | .[:$max] | .[]
' 2>/dev/null) || key_functions=""

key_types=$(echo "$symbols_result" | jq -r --argjson max "$MAX_SYMBOLS" '
  [.data.preview // [] | .[] | select(.type == "struct" or .type == "class" or .type == "interface" or .type == "type") | .name] | unique | .[:$max] | .[]
' 2>/dev/null) || key_types=""

# Extract imports based on language
imports=""
case "$lang" in
  go)
    # Go imports: extract package names from import statements
    imports=$(grep -E '^\s*"[^"]+"|^\s*[a-z]+ "[^"]+"' "$file_path" 2>/dev/null | \
      grep -oE '"[^"]+"' | tr -d '"' | \
      xargs -I{} basename {} | \
      sort -u | head -5) || imports=""
    ;;
  python)
    # Python imports: from X import Y, import X
    imports=$(grep -E '^(from|import)\s+' "$file_path" 2>/dev/null | \
      sed -E 's/^from\s+([a-zA-Z0-9_.]+).*/\1/; s/^import\s+([a-zA-Z0-9_.]+).*/\1/' | \
      cut -d. -f1 | sort -u | head -5) || imports=""
    ;;
  typescript|javascript)
    # JS/TS imports: import X from 'Y', require('Y')
    imports=$(grep -E "(import.*from|require\()" "$file_path" 2>/dev/null | \
      grep -oE "['\"][^'\"]+['\"]" | tr -d "'\"\`" | \
      grep -v "^\." | head -5) || imports=""
    ;;
esac

# Build suggestions
suggestions=""
suggestion_count=0

# Map language to glob patterns
case "$lang" in
  go) glob="*.go" ;;
  python) glob="*.py" ;;
  typescript) glob="*.ts,*.tsx" ;;
  javascript) glob="*.js,*.jsx" ;;
  java) glob="*.java" ;;
  rust) glob="*.rs" ;;
  ruby) glob="*.rb" ;;
  c) glob="*.c,*.h" ;;
  cpp) glob="*.cpp,*.hpp" ;;
  php) glob="*.php" ;;
  *) glob="*" ;;
esac

# Suggest context_ripgrep for key functions (returns full function bodies)
for func in $key_functions; do
  if [[ $suggestion_count -ge $MAX_SYMBOLS ]]; then break; fi
  suggestions+="- Find usages of \`$func\` (full context): \`agentctl run code/context_ripgrep --input '{\"pattern\": \"$func\", \"glob\": [\"$glob\"]}'\`
"
  suggestion_count=$((suggestion_count + 1))
done

# Suggest context_ripgrep for key types
for type in $key_types; do
  if [[ $suggestion_count -ge $((MAX_SYMBOLS * 2)) ]]; then break; fi
  suggestions+="- Find usages of \`$type\` (full context): \`agentctl run code/context_ripgrep --input '{\"pattern\": \"$type\", \"glob\": [\"$glob\"]}'\`
"
  suggestion_count=$((suggestion_count + 1))
done

# Suggest exploring related imports
if [[ -n "$imports" ]]; then
  import_list=$(echo "$imports" | head -3 | tr '\n' ', ' | sed 's/,$//')
  suggestions+="
**Related packages:** $import_list
"
fi

# If no suggestions, skip
if [[ -z "$suggestions" ]]; then
  echo '{}'
  exit 0
fi

# Build context message
context="## Context Suggestions for \`$filename\`

$suggestions
---
*Use \`code/context_ripgrep\` for full function context. Use \`code/smart_write\` to edit by symbol name with dry-run diff preview.*"

jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
