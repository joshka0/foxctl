---
name: agentctl Sessions
description: Search and recall past Claude Code sessions using semantic search. Find how problems were solved before.
---

# Session Memory with agentctl

Search past coding sessions to recall how problems were solved, what decisions
were made, and what gotchas were encountered.

## Commands

### Recall Similar Sessions (Semantic Search)

Find past sessions relevant to a natural language query:

```bash
agentctl run session/recall --input '{
  "query": "authentication JWT refresh token",
  "limit": 5,
  "min_similarity": 0.5
}'
```

Returns sessions ranked by semantic similarity with:

- Summary of what was accomplished
- Key decisions made
- Gotchas and solutions
- Files modified

### List Recent Sessions

```bash
agentctl sessions list --limit 10
agentctl sessions list --workspace /path/to/project
agentctl sessions list --project myproject
```

### Show Session Details

```bash
agentctl sessions show <session-id>
```

Displays full session metadata:

- Summary and accomplishments
- Decisions and gotchas
- Key files modified
- Tool usage patterns
- Token usage stats

### Search Sessions (Text)

Full-text search across session content:

```bash
agentctl sessions search "race condition mutex"
```

### Session Statistics

```bash
agentctl sessions stats
```

Shows aggregate stats across all captured sessions.

## Use Cases

### Recall Past Solutions

"How did I fix the database connection pooling issue?"

```bash
agentctl run session/recall --input '{"query": "database connection pool timeout"}'
```

### Find Similar Work

"Show me sessions where I worked on API authentication"

```bash
agentctl run session/recall --input '{"query": "API authentication middleware"}'
```

### Learn from Mistakes

"What gotchas did I encounter with React hooks?"

```bash
agentctl run session/recall --input '{"query": "React hooks gotchas errors"}'
```

### Cross-Project Learning

Find solutions from other projects:

```bash
agentctl run session/recall --input '{
  "query": "GraphQL schema validation",
  "limit": 10
}'
```

## How It Works

1. **Capture**: Sessions are automatically captured when Claude Code compacts
2. **Summarize**: An LLM generates structured summaries (accomplished,
   decisions, gotchas)
3. **Embed**: Summaries are embedded using Gemini text-embedding-004
4. **Recall**: Natural language queries are embedded and matched via cosine
   similarity

## Configuration

Sessions are stored in `~/.agentctl/storage/sessions.db`

Requires `GEMINI_API_KEY` for embedding generation and semantic search.

## Integration with Session Continuity

The session system integrates with:

- **PreCompact hook**: Saves session state before compaction
- **SessionStart hook**: Restores context after compaction/resume
- **Stop hook**: Captures session when Claude Code exits
- **Lineage & identity**:
  - Single active session per workspace/agent; start/resume/fork will refuse if
    another is active unless `--force`.
  - Active is determined by status (e.g., `running`), not `ended_at`; terminal
    statuses set `ended_at`, non-terminal clear it.
  - Identity fallback file: `~/.agentctl/sessions/active/<workspace_hash>.json`
    (stores `session_id`, `agent_id`, lineage).
  - Env to skills: `AGENTCTL_SESSION_ID`, `AGENTCTL_AGENT_ID` plus fallbacks
    (`CLAUDE_SESSION_ID`, `OPENCODE_SESSION_ID`, `CURSOR_SESSION_ID`,
    `TERM_SESSION_ID`) are forwarded by exec/WASI runners.
  - View lineage with `agentctl sessions chain --session <id>`; trajectories
    record `session_id` for joins.

Sessions capture:

- Active task and todos
- Active plan from `~/.claude/plans/`
- Gotchas and learnings
- User insights and preferences
