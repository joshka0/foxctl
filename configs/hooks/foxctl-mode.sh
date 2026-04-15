#!/usr/bin/env bash
# foxctl-mode.sh - Consolidated UserPromptSubmit hook for foxctl mode
#
# Combines functionality from:
#   - foxctl-mode-detect.sh: Handle @strict on/off/status commands
#   - foxctl-mode-prompt.sh: Keyword-triggered actions (memory, search, recall, etc.)
#
# Usage in ~/.claude/settings.json:
#   "UserPromptSubmit": [
#     {
#       "matcher": "",
#       "hooks": ["$HOME/.claude/hooks/foxctl/foxctl-mode.sh"]
#     }
#   ]
#
# Commands:
#   @strict on     - Enable foxctl mode (redirect tools to foxctl skills)
#   @strict off    - Disable foxctl mode
#   @strict status - Show current foxctl mode status
#
# Keyword triggers:
#   "remember", "gotcha", "learned" → memory/put
#   "search", "find", "where is" → code/semantic_search
#   "recall", "how did we" → memory/search
#   "trace", "codemap" → codemap/list
#   "ask", "web" → ask
#   "timeline", "what happened", "recent activity" → session/timeline

set -euo pipefail

FOXCTL_BIN="${FOXCTL_BIN:-foxctl}"

# Read hook payload from stdin
payload="$(cat)"

# Extract prompt content
prompt="$(printf '%s' "$payload" | jq -r '.prompt // .input // ""')"
prompt_lower="$(echo "$prompt" | tr '[:upper:]' '[:lower:]')"

# =============================================================================
# 1. @STRICT COMMANDS (check first - explicit mode control)
# =============================================================================

if [[ "$prompt" =~ ^[[:space:]]*@(strict|foxctl) ]]; then
  # Extract workspace for state (per-workspace, not per-session)
  WORKSPACE="$(printf '%s' "$payload" | jq -r '.cwd // ""' 2>/dev/null || true)"
  if [[ -z "$WORKSPACE" || "$WORKSPACE" == "null" ]]; then
    WORKSPACE="${CLAUDE_PROJECT_DIR:-$(pwd)}"
  fi

  # Database path (ensure directory exists under pipefail)
  DB_PATH="${FOXCTL_HOME:-$HOME/.foxctl}/storage/tasks.db"
  mkdir -p "$(dirname "$DB_PATH")" 2>/dev/null || true

  # Escape workspace for SQL safety (single quotes doubled for SQLite)
  # Note: NUL bytes cannot reach here - jq rejects invalid JSON, and bash
  # variables cannot contain NUL bytes anyway
  WORKSPACE_SAFE="${WORKSPACE//\'/\'\'}"


  # Ensure table exists (idempotent)
  sqlite3 "$DB_PATH" "
  CREATE TABLE IF NOT EXISTS workspace_settings (
    workspace_id TEXT NOT NULL,
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (workspace_id, key)
  );
  " 2>/dev/null || true

  # Parse command
  cmd="$(echo "$prompt" | sed -E 's/^[[:space:]]*@(strict|foxctl)[[:space:]]*//' | tr '[:upper:]' '[:lower:]' | xargs)"

  # Helper to get current timestamp
  now_ts() {
    date -u +"%Y-%m-%dT%H:%M:%SZ"
  }

  case "$cmd" in
    on|enable|1|true)
      sqlite3 "$DB_PATH" "
        INSERT OR REPLACE INTO workspace_settings (workspace_id, key, value, updated_at)
        VALUES ('$WORKSPACE_SAFE', 'agentctl_mode', '{\"enabled\":true}', '$(now_ts)');
      " 2>/dev/null || true
      response="**Foxctl Mode: ENABLED**

Tool redirections active:
- Edit/Write/MultiEdit -> fs/apply_edit (dry-run preview)
- Grep -> code/smart_search (semantic + snippet extraction)
- Glob -> code/semantic_search (vector similarity)
- Read (large files) -> symbols outline + navigation
- Bash sed -> fs/apply_edit
- Bash grep/find -> semantic search

Use \`@strict off\` to disable."
      ;;

    off|disable|0|false)
      sqlite3 "$DB_PATH" "
        INSERT OR REPLACE INTO workspace_settings (workspace_id, key, value, updated_at)
        VALUES ('$WORKSPACE_SAFE', 'agentctl_mode', '{\"enabled\":false}', '$(now_ts)');
      " 2>/dev/null || true
      response="**Foxctl Mode: DISABLED**

Default tool behavior restored. Use \`@strict on\` to re-enable."
      ;;

    status|"")
      row=$(sqlite3 "$DB_PATH" "
        SELECT value, updated_at FROM workspace_settings
        WHERE workspace_id = '$WORKSPACE_SAFE' AND key = 'agentctl_mode';
      " 2>/dev/null || echo "")

      if [[ -n "$row" ]]; then
        value=$(echo "$row" | cut -d'|' -f1)
        updated=$(echo "$row" | cut -d'|' -f2)
        enabled=$(echo "$value" | jq -r '.enabled // false' 2>/dev/null || echo "false")

        if [[ "$enabled" == "true" ]]; then
          response="**Foxctl Mode: ENABLED** (since $updated)

Tool redirections:
- Edit/Write/MultiEdit -> fs/apply_edit
- Grep -> code/smart_search
- Glob -> code/semantic_search
- Read (large files) -> symbols outline
- Bash sed/grep/find -> foxctl equivalents"
        else
          response="**Foxctl Mode: DISABLED** (since $updated)"
        fi
      else
        response="**Foxctl Mode: DISABLED** (never enabled for this workspace)"
      fi
      ;;

    *)
      response="**Unknown command:** @strict $cmd

Usage:
- \`@strict on\` - Enable foxctl mode
- \`@strict off\` - Disable foxctl mode
- \`@strict status\` - Show current status"
      ;;
  esac

  jq -nc --arg msg "$response" '{
    "decision": "approve",
    "context": ("[SYSTEM: @strict command processed]\n\n" + $msg + "\n\nThe user ran a @strict command. Acknowledge the status change briefly and continue.")
  }'
  exit 0
fi

# =============================================================================
# 2. RECALL PATTERNS (check early - most specific)
# =============================================================================

if [[ "$prompt_lower" =~ (how\ did\ (we|i)|where\ did\ (we|i)|what\ was\ the|when\ did\ (we|i)) ]] \
|| [[ "$prompt_lower" =~ (recall|remember\ when|previously|earlier\ we|last\ time) ]] \
|| [[ "$prompt_lower" =~ (didn.?t\ (we|i)\ already|like\ before|as\ we\ discussed) ]]; then

  # Extract query from prompt (remove common prefixes)
  query=$(echo "$prompt" | sed -E 's/^(how did (we|i)|what was|recall|remember when|where did (we|i))//i' | xargs)

  if [[ -n "$query" ]]; then
    # Search memories
    mem_result=$($FOXCTL_BIN run memory/search --input "$(jq -nc --arg q "$query" '{query: $q, limit: 5}')" 2>/dev/null || echo '{}')

    mem_formatted=$(echo "$mem_result" | jq -r '
      .data.results // [] | .[0:5] |
      map("  - [" + (.type // "memory") + "] " + (.summary // "")[0:80])
      | join("\n")
    ' 2>/dev/null)

    if [[ -n "$mem_formatted" ]]; then
      context="**Recall results for:** $query"$'\n\n'"**Memories:**"$'\n'"$mem_formatted"
    else
      context="**Recall:** No matching memories found for \"$query\". Try \`foxctl run session/recall\` for session history."
    fi

    jq -nc --arg ctx "$context" '{
      decision: "approve",
      context: $ctx
    }'
    exit 0
  fi
fi

# =============================================================================
# 3. MEMORY SAVE PATTERNS
# =============================================================================

if [[ "$prompt_lower" =~ ^(remember|note|gotcha|learned|important|decision):? ]] \
|| [[ "$prompt_lower" =~ (remember\ this|note\ this|save\ this|don.?t\ forget) ]] \
|| [[ "$prompt_lower" =~ (the\ trick\ is|the\ key\ is|turns\ out|watch\ out) ]]; then

  # Determine memory type
  mem_type="context"
  if [[ "$prompt_lower" =~ ^gotcha ]] || [[ "$prompt_lower" =~ (trick|watch\ out|careful) ]]; then
    mem_type="gotcha"
  elif [[ "$prompt_lower" =~ ^learned ]] || [[ "$prompt_lower" =~ (turns\ out|realized) ]]; then
    mem_type="learning"
  elif [[ "$prompt_lower" =~ ^decision ]]; then
    mem_type="decision"
  fi

  # Extract the content (remove trigger words)
  content=$(echo "$prompt" | sed -E 's/^(remember|note|gotcha|learned|important|decision):?//i' | xargs)

  if [[ -n "$content" && ${#content} -gt 10 ]]; then
    # Auto-save to memory
    result=$($FOXCTL_BIN run memory/put --input "$(jq -nc \
      --arg summary "$content" \
      --arg type "$mem_type" \
      '{summary: $summary, type: $type}')" 2>/dev/null || echo '{}')

    mem_id=$(echo "$result" | jq -r '.data.id // "unknown"')

    jq -nc --arg type "$mem_type" --arg id "$mem_id" --arg content "$content" '{
      decision: "approve",
      context: ("**Memory saved** [" + $type + "]: " + $content[0:100] + "\nID: " + $id)
    }'
    exit 0
  else
    # Content too short, just hint
    jq -nc --arg type "$mem_type" '{
      decision: "approve",
      context: ("**Memory hint:** Detected " + $type + " pattern. Add more detail to auto-save.")
    }'
    exit 0
  fi
fi

# =============================================================================
# 4. SEARCH PATTERNS
# =============================================================================

if [[ "$prompt_lower" =~ ^(search|find|query|where\ is|look\ for) ]] \
|| [[ "$prompt_lower" =~ (search\ for|find\ the|locate) ]]; then

  # Extract search query
  query=$(echo "$prompt" | sed -E 's/^(search|find|query|where is|look for|search for|find the|locate)//i' | xargs)

  if [[ -n "$query" && ${#query} -gt 3 ]]; then
    result=$($FOXCTL_BIN run code/semantic_search --input "$(jq -nc \
      --arg q "$query" \
      '{query: $q, limit: 8}')" 2>/dev/null || echo '{}')

    matches=$(echo "$result" | jq -r '
      .data.results // [] | .[0:8] |
      map("  - " + .path + ":" + (.line|tostring // "?") + " — " + ((.snippet // .symbol // "")[0:50]))
      | join("\n")
    ' 2>/dev/null || echo "  (no results)")

    jq -nc --arg q "$query" --arg matches "$matches" '{
      decision: "approve",
      context: ("**Semantic search for:** " + $q + "\n\n" + $matches + "\n\n*Use `code/snippet_extract` to get full context.*")
    }'
    exit 0
  fi
fi

# =============================================================================
# 5. CODEMAP PATTERNS
# =============================================================================

if [[ "$prompt_lower" =~ (trace|codemap|how\ does.*connect|flow\ of|architecture\ of) ]]; then

  query=$(echo "$prompt" | sed -E 's/^(trace|codemap|how does|show me the)//i' | xargs)

  if [[ -n "$query" ]]; then
    # List relevant codemaps
    result=$($FOXCTL_BIN run codemap/list --input '{"limit": 5}' 2>/dev/null || echo '{}')

    maps=$(echo "$result" | jq -r '
      .data.codemaps // [] | .[0:5] |
      map("  - `" + .id + "`: " + .query[0:60])
      | join("\n")
    ' 2>/dev/null || echo "  (no codemaps)")

    jq -nc --arg q "$query" --arg maps "$maps" '{
      decision: "approve",
      context: ("**Codemap search for:** " + $q + "\n\n**Existing codemaps:**\n" + $maps + "\n\n*Generate new: `foxctl run codemap/generate --input '\''{\"query\": \"...\"}'\''`*")
    }'
    exit 0
  fi
fi

# =============================================================================
# 6. WEB/ASK PATTERNS
# =============================================================================

if [[ "$prompt_lower" =~ ^(ask|exa|web|perplexity) ]] \
|| [[ "$prompt_lower" =~ (search\ the\ web|look\ up|google) ]]; then

  query=$(echo "$prompt" | sed -E 's/^(ask|exa|web|perplexity|search the web|look up|google)//i' | xargs)

  if [[ -n "$query" && ${#query} -gt 5 ]]; then
    result=$($FOXCTL_BIN run ask --input "$(jq -nc --arg q "$query" '{question: $q}')" 2>/dev/null || echo '{}')

    answer=$(echo "$result" | jq -r '.data.answer // .data.summary // "No answer found"' 2>/dev/null | head -20)

    jq -nc --arg q "$query" --arg answer "$answer" '{
      decision: "approve",
      context: ("**Web answer for:** " + $q + "\n\n" + $answer)
    }'
    exit 0
  fi
fi

# =============================================================================
# 7. TIMELINE PATTERNS
# =============================================================================

if [[ "$prompt_lower" =~ (timeline|what\ happened|recent\ activity|what\ did\ we|session\ history|earlier\ in\ this\ session) ]]; then

  # Get current session ID from environment (check multiple sources for consistency with other hooks)
  session_id="${FOXCTL_SESSION_ID:-${CLAUDE_SESSION_ID:-${OPENCODE_SESSION_ID:-}}}"
  if [[ -z "$session_id" ]]; then
    # Try to get the most recent active session for this workspace
    # Guard against non-JSON output by providing empty JSON fallback (use {} grouping for pipefail safety)
    session_id=$({ $FOXCTL_BIN run session/list --input '{"limit": 1, "status": "running"}' 2>/dev/null || echo '{}'; } | jq -r '.data.sessions[0].id // empty')
  fi

  if [[ -n "$session_id" ]]; then
    # Fetch timeline with max_windows=5 and since=2h for focused recent context
    result=$($FOXCTL_BIN run session/timeline --input "$(jq -nc --arg sid "$session_id" '{
      session_id: $sid,
      max_windows: 5,
      since: "2h",
      include_rollup: true,
      include_list: true
    }')" 2>/dev/null || echo '{}')

    # Format the timeline output
    rollup=$(echo "$result" | jq -r '
      .data.rollup // {} |
      "**Recent Activity Summary:**\n" +
      (if .summary_lines then (.summary_lines | join("\n")) else "No summaries available" end) +
      (if .decisions and (.decisions | length > 0) then "\n\n**Decisions:** " + (.decisions | join(", ")) else "" end) +
      (if .gotchas and (.gotchas | length > 0) then "\n\n**Gotchas:** " + (.gotchas | join(", ")) else "" end)
    ' 2>/dev/null)

    if [[ -n "$rollup" && "$rollup" != "null" ]]; then
      jq -nc --arg timeline "$rollup" '{
        decision: "approve",
        context: $timeline
      }'
      exit 0
    fi
  fi

  # No session or no timeline - hint
  jq -nc '{
    decision: "approve",
    context: "**Timeline:** No active session timeline available. Use `session/timeline` with a session_id."
  }'
  exit 0
fi

# =============================================================================
# NO PATTERN MATCHED - approve silently
# =============================================================================

echo '{"decision":"approve"}'
