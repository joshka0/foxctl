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
bin/agentctl run session/recall --input '{
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
bin/agentctl run session/recall --input '{"query": "database connection pool timeout"}'
```

### Find Similar Work

"Show me sessions where I worked on API authentication"

```bash
bin/agentctl run session/recall --input '{"query": "API authentication middleware"}'
```

### Learn from Mistakes

"What gotchas did I encounter with React hooks?"

```bash
bin/agentctl run session/recall --input '{"query": "React hooks gotchas errors"}'
```

### Cross-Project Learning

Find solutions from other projects:

```bash
bin/agentctl run session/recall --input '{
  "query": "GraphQL schema validation",
  "limit": 10
}'
```

## How It Works

1. **Capture**: Sessions are automatically captured when Claude Code compacts
2. **Summarize**: An LLM generates structured summaries (accomplished, decisions, gotchas)
3. **Embed**: Summaries are embedded using Gemini text-embedding-004
4. **Recall**: Natural language queries are embedded and matched via cosine similarity

## Configuration

Sessions are stored in `~/.agentctl/storage/sessions.db`

Requires `GEMINI_API_KEY` for embedding generation and semantic search.

## Integration with Session Continuity

The session system integrates with:
- **PreCompact hook**: Saves session state before compaction
- **SessionStart hook**: Restores context after compaction/resume
- **Stop hook**: Captures session when Claude Code exits

Sessions capture:
- Active task and todos
- Active plan from `~/.claude/plans/`
- Gotchas and learnings
- User insights and preferences
