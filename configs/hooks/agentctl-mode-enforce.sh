#!/usr/bin/env bash
# strict-mode-enforce.sh - Enforce tool redirections when strict mode is enabled
#
# This hook triggers on PreToolUse for Edit|Write|MultiEdit|Grep|Glob|Read.
# When strict mode is enabled, it runs the corresponding agentctl skill and
# injects the result as additional context.
#
# Tool mappings:
#   Edit|Write|MultiEdit -> fs/apply_edit (dry-run preview)
#   Grep -> code/smart_search (semantic search + extraction)
#   Glob -> code/semantic_search (vector similarity)
#   Read -> code/context_grep (function-level expansion)

set -euo pipefail

DEBUG="${AGENTCTL_STRICT_DEBUG:-}"

# Debug: log invocations only when DEBUG is enabled
[[ -n "$DEBUG" ]] && echo "$(date): $0 invoked" >> /tmp/strict-mode-debug.log

# Read hook payload from stdin
payload="$(cat)"

# Extract tool name
TOOL="$(printf '%s' "$payload" | jq -r '.tool_name // ""')"

# Extract workspace for state file lookup (per-workspace, not per-session)
WORKSPACE="$(printf '%s' "$payload" | jq -r '.cwd // ""' 2>/dev/null || true)"
if [[ -z "$WORKSPACE" || "$WORKSPACE" == "null" ]]; then
  WORKSPACE="${CLAUDE_PROJECT_DIR:-$(pwd)}"
fi

# Database path for settings
DB_PATH="${AGENTCTL_HOME:-$HOME/.agentctl}/storage/tasks.db"

# Escape single quotes for SQL safety
WORKSPACE_SAFE="${WORKSPACE//\'/\'\'}"

# Check if agentctl mode is enabled (read from database)
enabled="false"
if [[ -f "$DB_PATH" ]]; then
    row=$(sqlite3 "$DB_PATH" "SELECT value FROM workspace_settings WHERE workspace_id = '$WORKSPACE_SAFE' AND key = 'agentctl_mode'" 2>/dev/null || echo "")
  if [[ -n "$row" ]]; then
    enabled=$(echo "$row" | jq -r '.enabled // false' 2>/dev/null || echo "false")
  fi
fi

if [[ "$enabled" != "true" ]]; then
    echo '{"decision":"approve"}'
    exit 0
fi
[[ -n "$DEBUG" ]] && echo "DEBUG: strict mode enabled, processing $TOOL" >&2

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

# Extract tool input
tool_input="$(printf '%s' "$payload" | jq -c '.tool_input // {}')"

# Self-protection: block edits to hook files and settings while agentctl mode is enabled
if [[ "$TOOL" =~ ^(Edit|Write|MultiEdit)$ ]]; then
  EDIT_PATH="$(printf '%s' "$tool_input" | jq -r '.file_path // ""')"
  if [[ "$EDIT_PATH" == *"/.claude/hooks/"* ]]; then
    jq -nc '{"decision": "block", "reason": "**[Agentctl Mode] Cannot edit hook files while enabled.**\n\nUse `@strict off` first to modify hooks."}'
    exit 0
  fi
  if [[ "$EDIT_PATH" == *"/.claude/settings.json"* || "$EDIT_PATH" == *"/configs/claude-settings.json"* ]]; then
    jq -nc '{"decision": "block", "reason": "**[Agentctl Mode] Cannot edit settings.json while enabled.**\n\nUse `@strict off` first to modify settings."}'
    exit 0
  fi
fi

# Build skill invocation based on tool
case "$TOOL" in
  Edit|Write|MultiEdit)
    FILE_PATH="$(printf '%s' "$tool_input" | jq -r '.file_path // ""')"
    OLD_STRING="$(printf '%s' "$tool_input" | jq -r '.old_string // ""')"
    NEW_STRING="$(printf '%s' "$tool_input" | jq -r '.new_string // ""')"

    if [[ -z "$FILE_PATH" ]]; then
      echo '{"decision":"approve"}'
      exit 0
    fi

    SKILL="fs/apply_edit"
    SKILL_INPUT=$(jq -nc \
      --arg path "$FILE_PATH" \
      --arg search "$OLD_STRING" \
      --arg replace "$NEW_STRING" \
      '{path: $path, edits: [{search: $search, replace: $replace}], dry_run: true}')
    PREFIX="**[Agentctl Mode] Use search/replace via fs/apply_edit**

**Workflow:**
1. **Get exact text**: \`agentctl run code/context_grep --input '{\"mode\": \"line\", \"file_path\": \"$FILE_PATH\", \"line_start\": N, \"line_end\": M}'\`
2. **Preview change**: \`agentctl run fs/apply_edit --input '{\"path\": \"$FILE_PATH\", \"edits\": [{\"search\": \"exact old text\", \"replace\": \"new text\"}], \"dry_run\": true}'\`
3. **Apply**: Set \`dry_run: false\`

**Tips:**
- \`search\` must match exactly (copy from context_grep output)
- Multiple edits: \`\"edits\": [{...}, {...}]\`
- Use \`--input-file\` for complex edits"
    ;;

  Grep)
    PATTERN="$(printf '%s' "$tool_input" | jq -r '.pattern // ""')"

    if [[ -z "$PATTERN" ]]; then
      echo '{"decision":"approve"}'
      exit 0
    fi

    SKILL="code/smart_search"
    SKILL_INPUT=$(jq -nc \
      --arg question "$PATTERN" \
      --arg workspace "$WORKSPACE" \
      '{question: $question, workspace_id: $workspace, limits: {max_candidates: 20, max_snippets: 10}}')
    PREFIX="**[Agentctl Mode] Smart Search via code/smart_search**
> Direct: \`agentctl run code/smart_search --input '{\"question\": \"your query\"}'\`
> Params: \`limits.max_candidates\`, \`limits.max_snippets\` to control output size."
    ;;

  Glob)
    PATTERN="$(printf '%s' "$tool_input" | jq -r '.pattern // ""')"

    if [[ -z "$PATTERN" ]]; then
      echo '{"decision":"approve"}'
      exit 0
    fi

    SKILL="code/semantic_search"
    SKILL_INPUT=$(jq -nc \
      --arg query "$PATTERN" \
      --arg workspace "$WORKSPACE" \
      '{query: $query, scope: ["symbols"], workspace: $workspace, limit: 20}')
    PREFIX="**[Agentctl Mode] Semantic Search via code/semantic_search**
> Direct: \`agentctl run code/semantic_search --input '{\"query\": \"concept\", \"scope\": [\"symbols\"], \"limit\": 10}'\`
> Scopes: \`symbols\`, \`memory\`, \`codemaps\`. Uses vector embeddings."
    ;;

  Read)
    FILE_PATH="$(printf '%s' "$tool_input" | jq -r '.file_path // ""')"
    OFFSET="$(printf '%s' "$tool_input" | jq -r '.offset // 0')"
    LIMIT="$(printf '%s' "$tool_input" | jq -r '.limit // 0')"

    if [[ -z "$FILE_PATH" ]]; then
      echo '{"decision":"approve"}'
      exit 0
    fi

    # Get file line count (ensure numeric fallback for edge cases)
    LINE_COUNT=0
    if [[ -f "$FILE_PATH" ]]; then
      LINE_COUNT=$(wc -l < "$FILE_PATH" 2>/dev/null | tr -d ' ')
      # Ensure LINE_COUNT is a valid non-negative integer
      [[ "$LINE_COUNT" =~ ^[0-9]+$ ]] || LINE_COUNT=0
    fi

    # Small files (<300 lines) or specific line ranges: approve
    if [[ "$LINE_COUNT" -lt 300 ]] || [[ "$OFFSET" -gt 0 ]] || [[ "$LIMIT" -gt 0 ]]; then
      echo '{"decision":"approve"}'
      exit 0
    fi

    # Large file: show symbols outline using pattern matching
    file_basename=$(basename "$FILE_PATH")
    
    # Get symbols via pattern matching (works for most languages)
    symbols_result=$($AGENTCTL_BIN run code/context_grep --input "$(jq -nc \
      --arg path "$FILE_PATH" \
      '{mode: "ripgrep", path: $path, pattern: "^(func|type|const|var|class|def|function|async|export|interface|struct)\\s+\\w+", max_blocks: 30}')" 2>/dev/null || echo '{}')
    
    # Format symbols as compact list
    symbols_formatted=$(echo "$symbols_result" | jq -r '
      .data.blocks // []
      | map("  - " + (.header_line | split("(")[0] | gsub("\\s+$"; "")) + " (line " + (.start_line|tostring) + ")")
      | join("\n")
    ' 2>/dev/null || echo "  (no symbols found)")
    
    [[ -z "$symbols_formatted" ]] && symbols_formatted="  (no symbols found)"

    # Check for relevant gotchas
    gotchas_result=$($AGENTCTL_BIN run code/semantic_search --input "$(jq -nc --arg q "$file_basename gotchas" '{query: $q, scope: ["memory"], limit: 3}')" 2>/dev/null || echo '{}')
    gotchas_formatted=$(echo "$gotchas_result" | jq -r '.data.results[:2] | map("- **" + .name + "**: " + (.snippet // .summary // ""  | split("\n")[0])) | join("\n")' 2>/dev/null || echo "")
    
    GOTCHAS_SECTION=""
    [[ -n "$gotchas_formatted" ]] && GOTCHAS_SECTION="

**Relevant Gotchas:**
$gotchas_formatted"

    # Build block reason
    REASON="**[Agentctl Mode] Large File: ${file_basename} (${LINE_COUNT} lines)**

**Symbols:**
${symbols_formatted}${GOTCHAS_SECTION}

**To read specific lines:**
  \`agentctl run code/context_grep --input '{\"mode\": \"line\", \"file_path\": \"${FILE_PATH}\", \"line_start\": N, \"line_end\": M}'\`

**To search by concept:**
  \`agentctl run code/semantic_search --input '{\"query\": \"your concept here\"}'\`

**To read full file anyway (${LINE_COUNT} lines):**
  \`agentctl run fs/read --input '{\"path\": \"${FILE_PATH}\"}'\`"

    jq -nc --arg reason "$REASON" '{"decision": "block", "reason": $reason}'
    exit 0
    ;;

  WebSearch|WebFetch)
    # Redirect web search/fetch to agentctl web tools for CAS limits and curated results
    QUERY="$(printf '%s' "$tool_input" | jq -r '.query // .url // ""')"
    URL="$(printf '%s' "$tool_input" | jq -r '.url // ""')"
    PROMPT="$(printf '%s' "$tool_input" | jq -r '.prompt // ""')"

    if [[ -z "$QUERY" && -z "$URL" ]]; then
      echo '{"decision":"approve"}'
      exit 0
    fi

    if [[ "$TOOL" == "WebFetch" && -n "$URL" ]]; then
      # WebFetch -> web/extract with optional prompt for snippet extraction
      jq -nc --arg url "$URL" --arg prompt "$PROMPT" '{
        "decision": "block",
        "reason": ("**[Agentctl Mode] Use agentctl web extract instead of WebFetch**\n\n**Extract content:**\n```bash\nagentctl run web/extract --input '\''{ \"urls\": [\"" + $url + "\"]" + (if $prompt != "" then ", \"query\": \"" + $prompt + "\"" else "" end) + " }'\''\n```\n\n**Benefits:**\n- CAS storage for large responses\n- Query-based snippet extraction\n- Rate limiting and caching")
      }'
      exit 0
    fi

    # WebSearch -> web/search
    jq -nc --arg query "$QUERY" '{
      "decision": "block",
      "reason": ("**[Agentctl Mode] Use agentctl web search instead of WebSearch**\n\n**Search:**\n```bash\nagentctl run web/search --input '\''{ \"query\": \"" + $query + "\" }'\''\n```\n\n**With extraction (recommended):**\n```bash\nagentctl run web/search --input '\''{ \"query\": \"" + $query + "\", \"extract\": true, \"max_results\": 5 }'\''\n```\n\n**Benefits:**\n- CAS storage for large results\n- Exa/Tavily backends for curated results\n- Optional snippet extraction from top results")
    }'
    exit 0
    ;;

  Bash)
    # Parse bash command for specific patterns to redirect
    COMMAND="$(printf '%s' "$tool_input" | jq -r '.command // ""')"

    if [[ -z "$COMMAND" ]]; then
      echo '{"decision":"approve"}'
      exit 0
    fi

    # Block attempts to bypass agentctl mode via state manipulation
    if [[ "$COMMAND" =~ (workspace-modes) ]]; then
      jq -nc '{
        "decision": "block",
        "reason": "**[Agentctl Mode] Cannot modify mode state via Bash.**\n\nUse `@strict off` to disable."
      }'
      exit 0
    fi

    # Block sqlite3 modifications to workspace_settings
    # Case-insensitive check for sqlite3 targeting tasks.db with write operations
    # Also detect pipe/redirect bypass attempts (echo "INSERT..." | sqlite3)
    COMMAND_LOWER="${COMMAND,,}"
    if [[ "$COMMAND_LOWER" =~ sqlite3.*tasks\.db.*(insert|update|delete|drop).*workspace_settings ]] || \
       [[ "$COMMAND_LOWER" =~ sqlite3.*tasks\.db.*(insert|update|delete).*agentctl_mode ]] || \
       [[ "$COMMAND_LOWER" =~ (\||'<').*sqlite3.*tasks\.db ]]; then
      jq -nc '{
        "decision": "block",
        "reason": "**[Agentctl Mode] Cannot modify mode settings via sqlite3.**\n\nUse `@strict off` to disable."
      }'
      exit 0
    fi

    # Block go test commands - use make test-* instead for consistency
    if [[ "$COMMAND" =~ ^go[[:space:]]+test ]]; then
      jq -nc '{
        "decision": "block",
        "reason": "**[Agentctl Mode] Use make test-* instead of go test**\n\n**Quick tests:** `make test-short`\n**CGO tests:** `make test-cgo-short`\n**Full tests:** `make test`\n**Race tests:** `make test-race`"
      }'
      exit 0
    fi

    # Block gh pr/api/checks commands - use agentctl ci instead
    if [[ "$COMMAND" =~ ^gh[[:space:]]+(pr|api|checks) ]]; then
      jq -nc '{
        "decision": "block",
        "reason": "**[Agentctl Mode] Use agentctl ci instead of gh pr/api/checks**\n\n**CI Checks:** `agentctl ci checks --pr <num>` (mirrors gh pr checks)\n**PR Comments:** `agentctl ci prcomments --pr <num>`\n\nOr via MCP: `agentctl run ci/checks --input '\''{ \"pr\": \"<num>\" }'\''`"
      }'
      exit 0
    fi

    # Extract the base command (first word, handling pipes)
    BASE_CMD="$(echo "$COMMAND" | sed 's/|.*//' | awk '{print $1}' | xargs)"

    case "$BASE_CMD" in
      grep|rg|ripgrep)
        # Extract pattern from grep/rg command
        # Try to get the pattern (usually second arg or after -e)
        PATTERN="$(echo "$COMMAND" | grep -oE '(grep|rg)[[:space:]]+(-[^[:space:]]+[[:space:]]+)*[^-][^[:space:]]+' | awk '{print $NF}' || echo "")"
        if [[ -z "$PATTERN" || "$PATTERN" == "grep" || "$PATTERN" == "rg" ]]; then
          echo '{"decision":"approve"}'
          exit 0
        fi
        SKILL="code/smart_search"
        SKILL_INPUT=$(jq -nc \
          --arg question "$PATTERN" \
          --arg workspace "$WORKSPACE" \
          '{question: $question, workspace_id: $workspace, limits: {max_candidates: 15, max_snippets: 8}}')
        PREFIX="**[Agentctl Mode] Bash grep → Smart Search:**"
        ;;

      find|fd)
        # Extract path and pattern from find/fd command
        # find <path> -name "<pattern>" OR fd "<pattern>" <path>
        FIND_PATH="."
        FIND_PATTERN="*"

        if [[ "$BASE_CMD" == "find" ]]; then
          # Parse: find <path> -name "<pattern>"
          FIND_PATH="$(echo "$COMMAND" | awk '{print $2}' || echo ".")"
          FIND_PATTERN="$(echo "$COMMAND" | grep -oE '\-name[[:space:]]+["\047]?[^"\047]+["\047]?' | sed 's/-name[[:space:]]*//' | tr -d "\"'" || echo "*")"
        else
          # Parse: fd "<pattern>" <path>
          FIND_PATTERN="$(echo "$COMMAND" | awk '{print $2}' | tr -d "\"'" || echo "*")"
          FIND_PATH="$(echo "$COMMAND" | awk '{print $3}' || echo ".")"
        fi

        [[ -z "$FIND_PATH" ]] && FIND_PATH="."
        [[ -z "$FIND_PATTERN" ]] && FIND_PATTERN="*"

        SKILL="fs/find"
        SKILL_INPUT=$(jq -nc \
          --arg pattern "$FIND_PATTERN" \
          --arg path "$FIND_PATH" \
          '{pattern: $pattern, path: $path, limit: 50}')
        PREFIX="**[Agentctl Mode] Bash find → fs/find:**"
        ;;

      cat|head|tail|less|more)
        # Extract file path from cat/head/tail
        FILE_PATH="$(echo "$COMMAND" | grep -oE '(cat|head|tail|less|more)[[:space:]]+(-[^[:space:]]+[[:space:]]+)*[^|]+' | awk '{print $NF}' | xargs || echo "")"
        if [[ -z "$FILE_PATH" || ! -f "$FILE_PATH" ]]; then
          # Try relative to workspace
          if [[ -f "$WORKSPACE/$FILE_PATH" ]]; then
            FILE_PATH="$WORKSPACE/$FILE_PATH"
          else
            echo '{"decision":"approve"}'
            exit 0
          fi
        fi
        SKILL="code/context_grep"
        SKILL_INPUT=$(jq -nc \
          --arg path "$FILE_PATH" \
          '{mode: "ripgrep", path: $path, pattern: "^(func|def|class|function|async|export|type|interface|struct)", max_blocks: 15}')
        PREFIX="**[Agentctl Mode] Bash $BASE_CMD → Context Grep:**"
        ;;

      sed)
        # BLOCK sed - always use fs/apply_edit instead
        jq -nc '{
          "decision": "block",
          "reason": "**[Agentctl Mode] sed is blocked — use fs/apply_edit**\n\n**Search/replace workflow:**\n1. Find text: `agentctl run code/smart_search --input '\''{ \"question\": \"what to change\" }'\''`\n2. Preview: `agentctl run fs/apply_edit --input '\''{ \"path\": \"file\", \"edits\": [{ \"search\": \"old\", \"replace\": \"new\" }], \"dry_run\": true }'\''`\n3. Apply: Set `dry_run: false` to commit\n\n**Tips:**\n- `search` must match exactly (not regex)\n- Multiple edits: `\"edits\": [{...}, {...}]`\n- Use `code/context_grep` to get exact text to search for"
        }'
        exit 0
        ;;

      gh)
        # GitHub CLI commands
        if [[ "$COMMAND" =~ (pr[[:space:]]+(view|status|checks|comment|review)) ]]; then
          # Extract PR number - look for number after 'pr' keyword or '#' prefix
          # Avoids capturing unrelated numbers like --limit 10
          PR_NUM=""
          if [[ "$COMMAND" =~ pr[[:space:]]+(view|status|checks|comment|review)[[:space:]]+([0-9]+) ]]; then
            PR_NUM="${BASH_REMATCH[2]}"
          elif [[ "$COMMAND" =~ \#([0-9]+) ]]; then
            PR_NUM="${BASH_REMATCH[1]}"
          fi
          if [[ -n "$PR_NUM" && "$PR_NUM" =~ ^[0-9]+$ ]]; then
            SKILL="ci/prcomments"
            SKILL_INPUT=$(jq -nc --argjson pr "$PR_NUM" '{pr: $pr, include_merge_status: true}')
            PREFIX="**[Agentctl Mode] Bash gh → PR Comments:**"
          else
            echo '{"decision":"approve"}'
            exit 0
          fi
        elif [[ "$COMMAND" =~ (run[[:space:]]+(list|view)|workflow) ]]; then
          # Extract run ID - look for number after 'view' or 'run'
          PR_NUM=""
          if [[ "$COMMAND" =~ (view|run)[[:space:]]+([0-9]+) ]]; then
            PR_NUM="${BASH_REMATCH[2]}"
          fi
          SKILL="ci/checks"
          SKILL_INPUT=$(jq -nc --argjson pr "${PR_NUM:-0}" '{pr: $pr}')
          PREFIX="**[Agentctl Mode] Bash gh → CI Status:**"
        else
          echo '{"decision":"approve"}'
          exit 0
        fi
        ;;

      *)
        # Unknown bash command - let it through
        echo '{"decision":"approve"}'
        exit 0
        ;;
    esac
    ;;

  *)
    echo '{"decision":"approve"}'
    exit 0
    ;;
esac

[[ -n "$DEBUG" ]] && echo "DEBUG: running $SKILL with input: $SKILL_INPUT" >&2

# Run the skill
skill_stderr=$(mktemp)
result=$(printf '%s' "$SKILL_INPUT" | "$AGENTCTL_BIN" run "$SKILL" --input-file - 2>"$skill_stderr") || {
  error_detail=$(cat "$skill_stderr" 2>/dev/null || echo "unknown error")
  rm -f "$skill_stderr"
  [[ -n "$DEBUG" ]] && echo "DEBUG: skill failed: $error_detail" >&2

  # Skill failed - approve original tool and add warning
  jq -nc --arg err "$error_detail" '{
    "decision": "approve",
    "hookSpecificOutput": {
      "hookEventName": "PreToolUse",
      "additionalContext": ("**[Agentctl Mode] Skill failed:** " + $err + "\n\nFalling back to default tool behavior.")
    }
  }'
  exit 0
}
rm -f "$skill_stderr"

[[ -n "$DEBUG" ]] && echo "DEBUG: skill result: $result" >&2

# Format result for context injection (with line numbers)
formatted=$(printf '%s' "$result" | jq -r '
  if .data then
    if .data.blocks then
      (.data.blocks[:5] | map(
        "### " + .file + " (lines " + (.start_line|tostring) + "-" + (.end_line|tostring) + ")" +
        (if .symbol_name then " — " + .symbol_name + " (" + .symbol_kind + ")" else "" end) +
        "\n```" + .language + "\n" +
        (.start_line as $sl | .source | split("\n") | to_entries | map("  " + (($sl + .key)|tostring) + "│ " + .value) | join("\n")) +
        "\n```"
      ) | join("\n\n"))
    elif .data.snippets_inline then
      (.data.snippets_inline[:5] | map(
        "### " + .file + " (lines " + (.start_line|tostring) + "-" + (.end_line|tostring) + ")\n```\n" +
        (.preview | split("\n") | to_entries | map("  " + ((.key + .start_line)|tostring) + "│ " + .value) | join("\n")) +
        "\n```"
      ) | join("\n\n"))
    elif .data.snippets then
      (.data.snippets[:5] | map(
        "### " + (.path // .file) + " (lines " + (.start_line|tostring) + "-" + (.end_line|tostring) + ")\n```\n" +
        ((.content // .preview) | split("\n") | to_entries | map("  " + ((.key + .start_line)|tostring) + "│ " + .value) | join("\n")) +
        "\n```"
      ) | join("\n\n"))
    elif .data.hits then
      (.data.hits[:10] | map("- **" + .path + "** (score: " + (.score|tostring) + ")") | join("\n"))
    elif .data.diff then
      "```diff\n" + .data.diff + "\n```"
    else
      (. | tostring)
    end
  else
    (. | tostring)
  end
' 2>/dev/null || printf '%s' "$result")

# Block the original tool and return skill result
context="$PREFIX\n\n$formatted"

jq -nc --arg ctx "$context" '{
  "decision": "block",
  "reason": $ctx
}'
