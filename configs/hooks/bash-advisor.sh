#!/usr/bin/env bash
# bash-advisor.sh - Claude Code PreToolUse hook for tool advisory
# Detects tool usage (Bash, Grep, Glob, Read) and suggests agentctl
# skill alternatives that provide additional capabilities.
#
# This is advisory only - tools are always approved but with helpful
# context suggesting more powerful alternatives.
#
# Supported tools:
#   - Bash: Suggests Claude tools (Glob, Grep, Read) or agentctl skills
#   - Grep: Suggests code/semantic_search for "where is X" queries (vector search)
#   - Glob: Suggests fs/find for metadata, code/semantic_search for concepts
#   - Read: Suggests code/symbols for structure-first reading
#
# code/semantic_search provides vector search across:
#   - symbols: Code symbols (functions, types) with embeddings
#   - sessions: Past Claude Code sessions with summaries
#   - memories: Named memories with embeddings (Turso vector search)
#
# Environment:
#   AGENTCTL_TOOL_ADVISOR_DISABLED - Set to 1 to disable this hook

set -euo pipefail

# Allow disabling via environment
if [[ "${AGENTCTL_TOOL_ADVISOR_DISABLED:-}" == "1" ]]; then
  echo '{}'
  exit 0
fi

# Read hook input from stdin
INPUT=$(cat)

# Extract tool name and inputs
tool_name=$(echo "$INPUT" | jq -r '.tool_name // ""')

# Build suggestion based on tool type
suggestion=""

case "$tool_name" in
  Bash)
    # Extract the command from tool_input
    command=$(echo "$INPUT" | jq -r '.tool_input.command // ""')

    # Skip if no command
    if [[ -z "$command" || "$command" == "null" ]]; then
      echo '{}'
      exit 0
    fi

    # Get the first word (the actual command being run)
    first_word=$(echo "$command" | awk '{print $1}')

    # Build suggestion based on detected bash command
    case "$first_word" in
      find)
        suggestion="Use **Glob** tool, \`fs/find\` for metadata, or \`code/semantic_search\` for \"where is X\" queries
\`\`\`bash
# Fast pattern match
Glob pattern=\"**/*.go\" path=\".\"
# Semantic search across code, sessions & memories
agentctl run code/semantic_search --input '{\"query\": \"where is auth handler\", \"scope\": [\"symbols\", \"sessions\", \"memories\"]}'
\`\`\`"
        ;;

      grep|rg|ripgrep)
        suggestion="Use **Grep** tool, or semantic search for understanding code:
\`\`\`bash
# Pattern match
Grep pattern=\"myFunction\" path=\".\" output_mode=\"content\"
# Semantic search (uses vector search across symbols, sessions, memories)
agentctl run code/semantic_search --input '{\"query\": \"how does auth work\", \"scope\": [\"symbols\", \"sessions\", \"memories\"]}'
\`\`\`"
        ;;

      cat)
        if echo "$command" | grep -qE '^cat\s+[^|<>]+$'; then
          suggestion="Use **Read** tool (line numbers, offset/limit support)
\`\`\`
Read file_path=\"/path/to/file\"
\`\`\`"
        fi
        ;;

      head|tail)
        suggestion="Use **Read** tool with offset/limit
\`\`\`
Read file_path=\"/path/to/file\" limit=50
\`\`\`"
        ;;

      sed|awk)
        if echo "$command" | grep -qE "(sed\s+-i|sed\s+.*>\s*\S)"; then
          suggestion="Use **Edit** tool or \`fs/apply_edit\` / \`text/replace\` skills
\`\`\`
Edit file_path=\"file.go\" old_string=\"old\" new_string=\"new\"
\`\`\`"
        fi
        ;;

      ls)
        if echo "$command" | grep -qE 'ls\s+(-la?|.*\*|.*\.\w+$)'; then
          suggestion="Use **Glob** tool for pattern matching
\`\`\`
Glob pattern=\"*.go\" path=\"./internal\"
\`\`\`"
        fi
        ;;

      xargs)
        if echo "$command" | grep -qE 'xargs.*(grep|rg)'; then
          suggestion="**Grep** tool searches files directly (no xargs needed)
\`\`\`
Grep pattern=\"myFunction\" glob=\"*.go\" output_mode=\"content\"
\`\`\`"
        fi
        ;;
    esac
    detected="Bash: \`$first_word\`"
    ;;  # End Bash case

  Grep)
    # Suggest semantic search alternatives for Grep
    pattern=$(echo "$INPUT" | jq -r '.tool_input.pattern // ""')

    # Skip very short patterns
    if [[ ${#pattern} -lt 4 ]]; then
      echo '{}'
      exit 0
    fi

    suggestion="For semantic \"where is X\" queries: \`code/semantic_search\` (vector search across code, sessions & memories)
\`\`\`bash
agentctl run code/semantic_search --input '{\"query\": \"${pattern}\", \"scope\": [\"symbols\", \"sessions\", \"memories\"]}'
\`\`\`"
    detected="Grep: \`${pattern}\`"
    ;;

  Glob)
    # Suggest fs/find for richer metadata, code/semantic_search for conceptual queries
    pattern=$(echo "$INPUT" | jq -r '.tool_input.pattern // ""')

    suggestion="For metadata: \`fs/find\`. For conceptual \"where is X\": \`code/semantic_search\`
\`\`\`bash
# File metadata (size, mtime)
agentctl run fs/find --input '{\"pattern\": \"${pattern}\", \"path\": \".\", \"sort_by\": \"modified\"}'
# Semantic search (vector search)
agentctl run code/semantic_search --input '{\"query\": \"where is ${pattern}\", \"scope\": [\"symbols\", \"memories\"]}'
\`\`\`"
    detected="Glob: \`${pattern}\`"
    ;;

  Read)
    # Suggest code/symbols for structure-first reading
    file_path=$(echo "$INPUT" | jq -r '.tool_input.file_path // ""')

    # Only suggest for code files
    case "$file_path" in
      *.go|*.py|*.js|*.ts|*.tsx|*.jsx|*.java|*.c|*.cpp|*.rs|*.rb)
        suggestion="For structure-first reading: \`code/symbols\` (get functions/types with line numbers)
\`\`\`bash
agentctl run code/symbols --input '{\"path\": \"${file_path}\"}'
\`\`\`"
        detected="Read: \`$(basename "${file_path}")\`"
        ;;
      *)
        echo '{}'
        exit 0
        ;;
    esac
    ;;

  *)
    # Unknown tool, skip
    echo '{}'
    exit 0
    ;;
esac

# If no suggestion, approve silently
if [[ -z "$suggestion" ]]; then
  echo '{}'
  exit 0
fi

# Build context with the suggestion (concise format)
context="**$detected** $suggestion"

# Return approve with context (never blocks)
jq -n --arg ctx "$context" '{
  decision: "approve",
  context: $ctx
}'
