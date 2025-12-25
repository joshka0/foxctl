#!/usr/bin/env bash
# lsp-diagnostics.sh - PostToolUse hook for LSP diagnostic errors
# Shows compile errors, unused imports, type errors immediately after editing.
#
# Supported:
#   - Go (.go) → gopls check
#   - TypeScript (.ts, .tsx) → tsc --noEmit
#   - JavaScript (.js, .jsx) → eslint (if available)
#   - Python (.py) → pyright (if available)
#
# Environment:
#   AGENTCTL_LSP_DIAG_DISABLED=1 - Disable this hook

set -euo pipefail

if [[ "${AGENTCTL_LSP_DIAG_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

INPUT=$(cat)
file_path=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

if [[ -z "$file_path" || "$file_path" == "null" || ! -f "$file_path" ]]; then
  echo '{}'
  exit 0
fi

# Get absolute path and directory
abs_path=$(cd "$(dirname "$file_path")" && pwd)/$(basename "$file_path")
dir=$(dirname "$abs_path")

diagnostics=""

case "$file_path" in
  *.go)
    # Use gopls check for Go files
    if command -v gopls &>/dev/null; then
      # gopls check outputs diagnostics to stderr
      output=$(gopls check "$abs_path" 2>&1) || true
      if [[ -n "$output" ]]; then
        # Format: file:line:col-endcol: message (e.g., file.go:725:14-16: undefined: os)
        diagnostics=$(echo "$output" | head -10 | while IFS= read -r line; do
          # Extract line:col and message (col can be col-endcol range)
          if [[ "$line" =~ :([0-9]+):([0-9]+)(-[0-9]+)?:\ (.+)$ ]]; then
            echo "Error [${BASH_REMATCH[1]}:${BASH_REMATCH[2]}] ${BASH_REMATCH[4]}"
          fi
        done)
      fi
    fi
    ;;

  *.ts|*.tsx|*.js|*.jsx)
    # Priority: quick-lint-js > biome > eslint (speed order)
    if command -v quick-lint-js &>/dev/null; then
      # quick-lint-js: fastest JS/TS linter (~170x faster than ESLint)
      output=$(quick-lint-js --output-format=gnu-like "$abs_path" 2>&1 | head -10) || true
      if [[ -n "$output" ]]; then
        diagnostics=$(echo "$output" | while IFS= read -r line; do
          # Format: file:line:col: error: message
          if [[ "$line" =~ :([0-9]+):([0-9]+):\ (error|warning):\ (.+)$ ]]; then
            level="${BASH_REMATCH[3]^}"
            echo "$level [${BASH_REMATCH[1]}:${BASH_REMATCH[2]}] ${BASH_REMATCH[4]}"
          fi
        done)
      fi
    elif command -v biome &>/dev/null; then
      # biome: 15-25x faster than ESLint, Rust-based
      output=$(biome check --max-diagnostics=10 "$abs_path" 2>&1) || true
      if [[ -n "$output" && "$output" != *"No diagnostics"* ]]; then
        diagnostics=$(echo "$output" | grep -E "^\s*[0-9]+.*│" | head -10 | while IFS= read -r line; do
          # Extract line number and message from biome output
          if [[ "$line" =~ ^[[:space:]]*([0-9]+)[[:space:]]*│ ]]; then
            echo "Error [${BASH_REMATCH[1]}] $line"
          fi
        done)
      fi
    elif command -v eslint &>/dev/null; then
      # eslint: slowest but most mature
      output=$(eslint --format compact "$abs_path" 2>&1 | head -10) || true
      if [[ -n "$output" && "$output" != *"0 problems"* ]]; then
        diagnostics=$(echo "$output" | grep -E ":[0-9]+:[0-9]+:" | while IFS= read -r line; do
          if [[ "$line" =~ :([0-9]+):([0-9]+):\ (Error|Warning)\ -\ (.+)$ ]]; then
            echo "${BASH_REMATCH[3]} [${BASH_REMATCH[1]}:${BASH_REMATCH[2]}] ${BASH_REMATCH[4]}"
          fi
        done)
      fi
    fi
    ;;

  *.py)
    # Priority: ruff > pyright (ruff is 10-100x faster)
    if command -v ruff &>/dev/null; then
      # ruff: extremely fast Python linter, Rust-based
      output=$(ruff check --output-format=concise "$abs_path" 2>&1 | head -10) || true
      if [[ -n "$output" && "$output" != *"All checks passed"* ]]; then
        diagnostics=$(echo "$output" | while IFS= read -r line; do
          # Format: file:line:col: CODE message
          if [[ "$line" =~ :([0-9]+):([0-9]+):\ ([A-Z]+[0-9]+)\ (.+)$ ]]; then
            echo "Error [${BASH_REMATCH[1]}:${BASH_REMATCH[2]}] ${BASH_REMATCH[3]} ${BASH_REMATCH[4]}"
          fi
        done)
      fi
    elif command -v pyright &>/dev/null; then
      # pyright: good for type checking
      output=$(pyright "$abs_path" 2>&1 | grep -E ":[0-9]+:[0-9]+:" | head -10) || true
      if [[ -n "$output" ]]; then
        diagnostics=$(echo "$output" | while IFS= read -r line; do
          if [[ "$line" =~ :([0-9]+):([0-9]+):\ (error|warning):\ (.+)$ ]]; then
            level="${BASH_REMATCH[3]^}"
            echo "$level [${BASH_REMATCH[1]}:${BASH_REMATCH[2]}] ${BASH_REMATCH[4]}"
          fi
        done)
      fi
    fi
    ;;

  *)
    echo '{}'
    exit 0
    ;;
esac

# If no diagnostics, return empty
if [[ -z "$diagnostics" ]]; then
  echo '{}'
  exit 0
fi

filename=$(basename "$file_path")

# Build context message
context="**LSP Diagnostics** for \`$filename\`:
\`\`\`
$diagnostics
\`\`\`"

jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
