# Progressive Memory System for agentctl

## Scope: Transitional + V2 Target

- Phases 1-4 describe existing/prototyped session-memory behavior.
- Event schema + artifact schema + context-builder API surfaces are the v2
  target, intended for `internal/v2/runtime/*`.

## Overview

A tiered memory system that enables agents to learn from past Claude Code sessions,
recall relevant context on-demand, and generate training data for DSPy.

## Architecture

```
PostCompact Hook
       │
       ▼
┌──────────────────────────────────────────────────────────────┐
│  Phase 1: Extract & Summarize                                │
│  ┌─────────────────┐    ┌─────────────────┐                  │
│  │ Raw JSONL       │───►│ High-Signal     │───► Cerebras     │
│  │ (~/.claude/     │    │ Filter          │    Summarize     │
│  │  projects/)     │    │                 │                  │
│  └─────────────────┘    └─────────────────┘         │        │
│                                                     ▼        │
│                                           ┌─────────────────┐│
│                                           │ sessions.db     ││
│                                           │ (SQLite)        ││
│                                           └─────────────────┘│
└──────────────────────────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────────────────────────┐
│  Phase 2: Embed & Index                                      │
│  ┌─────────────────┐    ┌─────────────────┐                  │
│  │ Summaries       │───►│ Embedding       │───► Vector Index │
│  │ + Tags          │    │ (OpenAI/Local)  │                  │
│  └─────────────────┘    └─────────────────┘                  │
└──────────────────────────────────────────────────────────────┘
       │
       ▼
┌──────────────────────────────────────────────────────────────┐
│  Phase 3: RAG & DSPy Export                                  │
│  ┌─────────────────┐    ┌─────────────────┐                  │
│  │ session/recall  │    │ DSPy Export     │                  │
│  │ (semantic RAG)  │    │ (training data) │                  │
│  └─────────────────┘    └─────────────────┘                  │
└──────────────────────────────────────────────────────────────┘
```

## Progressive Retrieval Tiers

```
Tier 1: Embeddings (fast lookup)
┌─────────────────────────────────────────────────────────────┐
│ "This relates to session X about authentication refactor"   │
└─────────────────────────────────────────────────────────────┘
                           ↓ if relevant
Tier 1.5: Context Windows (sub-session granularity)
┌─────────────────────────────────────────────────────────────┐
│ Window 0 (123K tokens): Initial auth implementation         │
│ Window 1 (89K tokens): Token refresh and race condition fix │
│ Window 2 (45K tokens): Testing and cleanup                  │
└─────────────────────────────────────────────────────────────┘
                           ↓ if need more detail
Tier 2: Summaries (medium detail)
┌─────────────────────────────────────────────────────────────┐
│ Key decisions: Used JWT, stored in httpOnly cookie          │
│ Gotchas: Had to handle token refresh race condition         │
│ Tools: Edit auth.go, Bash(go test), Read middleware.go      │
└─────────────────────────────────────────────────────────────┘
                           ↓ if need specifics
Tier 3: Full conversation (on-demand)
┌─────────────────────────────────────────────────────────────┐
│ Exact tool call that fixed the race condition               │
│ The error message and how it was resolved                   │
└─────────────────────────────────────────────────────────────┘
```

### Temporal Pyramid + Referenceability

To avoid full-history replay, retrieval should prefer coarse-to-fine temporal
views before loading raw chunks:

1. `months` view (long-term pattern summaries)
2. `weeks` view (grouped work themes)
3. `days` view (daily summaries/context windows)
4. `hours` view (dense turn/chunk slices)

Stable references enable deterministic drill-down:

- `session/{session_id}`
- `session/{session_id}/window/{window_index}`
- `session/{session_id}/chunk/{chunk_index}`
- `turn/{turn_id}`
- `turn/{turn_id}#msg:{msg_id}:{start}-{end}`

These refs should be emitted in retrieval responses so callers can progressively
expand context without replaying full JSONL archives.

### Context Windows (Tier 1.5)

Claude Code sessions can span multiple compaction events. When Claude compacts
its context, it emits a `compact_boundary` marker in the JSONL:

```json
{
  "type": "system",
  "subtype": "compact_boundary",
  "content": "Conversation compacted",
  "timestamp": "2026-01-02T20:44:11.358Z",
  "compactMetadata": {
    "trigger": "auto",
    "preTokens": 123433
  }
}
```

Context windows subdivide sessions at these boundaries, enabling:
- **Granular retrieval**: Find specific work spans within long sessions
- **Token-aware context**: Know how much context was used per window
- **Better RAG**: Search at window level instead of whole-session level

## Data Source

Claude Code stores full conversations in:
```
~/.claude/projects/<encoded-workspace-path>/<session-uuid>.jsonl
```

Each JSONL contains message types:
- `user` - User prompts with metadata
- `assistant` - Model responses with thinking, tool_use, text
- `summary` - Compaction summaries
- `system` - System messages
- `file-history-snapshot` - File state snapshots

Key fields per message:
- `uuid` / `parentUuid` - Message threading
- `sessionId` - Session identifier
- `timestamp` - ISO-8601
- `message.content` - Actual content (text, tool calls, thinking)
- `message.usage` - Token usage stats

---

## Phase 1: Extract & Summarize (MVP)

### Goal
Capture high-signal conversation data and generate structured summaries.

### Skills
| Skill | Description |
|-------|-------------|
| `session/capture` | Parse Claude Code JSONL, filter noise, store in SQLite |
| `session/summarize` | Send filtered content to Cerebras, get structured summary |

### SQLite Schema

```sql
-- ~/.agentctl/storage/sessions.db

CREATE TABLE sessions (
    id TEXT PRIMARY KEY,              -- session_id from Claude Code
    workspace_path TEXT NOT NULL,
    project_name TEXT,                -- extracted from path
    git_branch TEXT,
    started_at TIMESTAMP,
    ended_at TIMESTAMP,
    claude_version TEXT,              -- e.g., "2.0.76"

    -- Cerebras-generated structured summary
    summary TEXT,                     -- Human-readable narrative
    accomplished JSON,                -- ["Implemented auth", "Fixed bug"]
    decisions JSON,                   -- ["Used JWT over sessions"]
    gotchas JSON,                     -- ["Race condition in refresh"]
    tags JSON,                        -- ["auth", "refactor", "bugfix"]
    key_files JSON,                   -- ["auth.go", "middleware.go"]
    tools_pattern TEXT,               -- "Read→Edit→Bash(test)"

    -- Metrics
    message_count INTEGER,
    user_turns INTEGER,
    tool_invocations INTEGER,
    total_tokens INTEGER,

    -- Raw data reference (don't duplicate large JSONL)
    raw_jsonl_path TEXT,

    -- Phase 2: Embedding (nullable initially)
    embedding BLOB,
    embedding_model TEXT,

    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_sessions_workspace ON sessions(workspace_path);
CREATE INDEX idx_sessions_tags ON sessions(tags);
CREATE INDEX idx_sessions_started ON sessions(started_at DESC);
CREATE INDEX idx_sessions_project ON sessions(project_name);
```

### High-Signal Filter

**Keep (high signal):**
- User intent/request (full text)
- Assistant decisions and text responses (truncated if huge)
- Tool names + success/failure (not full outputs)
- Errors encountered → resolutions
- Compaction summaries

**Drop (noise):**
- Full file contents from Read tool
- Verbose grep/glob results
- Thinking blocks (too verbose)
- Token usage stats
- Thinking signatures

```go
type FilteredMessage struct {
    Role       string   `json:"role"`
    Content    string   `json:"content"`
    ToolsUsed  []string `json:"tools_used,omitempty"`
    Error      string   `json:"error,omitempty"`
    Resolution string   `json:"resolution,omitempty"`
}

func FilterForSummary(messages []RawMessage) []FilteredMessage {
    // Implementation filters out noise, keeps high-signal content
}
```

### Cerebras Summarization Prompt

```
You are summarizing a coding session between a developer and an AI assistant.
Extract the following in JSON format:

{
  "summary": "2-3 sentence narrative of what happened",
  "accomplished": ["list of things completed", "2-5 items"],
  "decisions": ["key technical decisions made", "2-5 items"],
  "gotchas": ["problems encountered and solutions", "0-5 items"],
  "tags": ["topic", "tags", "3-7 items"],
  "key_files": ["important/files/modified.go", "up to 10"],
  "tools_pattern": "Common sequence like Read→Edit→Bash(test)"
}

<conversation>
{filtered_messages_json}
</conversation>
```

### Hook Integration

```bash
# .claude/hooks/session-capture.sh (PostCompact event)
#!/bin/bash
set -euo pipefail

# Only run on compact trigger
TRIGGER=$(echo "$1" | jq -r '.trigger // "unknown"')
if [[ "$TRIGGER" != "compact" ]]; then
    echo '{"decision": "none"}'
    exit 0
fi

WORKSPACE=$(echo "$1" | jq -r '.workspace // ""')
SESSION_ID=$(echo "$1" | jq -r '.session_id // ""')

agentctl run session/capture --input "{
  \"workspace\": \"$WORKSPACE\",
  \"session_id\": \"$SESSION_ID\",
  \"summarize\": true
}"
```

### CLI Commands

```bash
# List recent sessions
agentctl sessions list [--workspace <path>] [--limit 20] [--tags auth,refactor]

# Show session details
agentctl sessions show <session-id>

# Search sessions by text
agentctl sessions search "authentication JWT"

# Manual capture (for testing)
agentctl sessions capture --workspace /path/to/project --session-id <uuid>
```

---

## Phase 2: Embed & Index

### Goal
Enable semantic search over past sessions.

### Schema Addition
```sql
-- Embedding added to sessions table (already in Phase 1 schema)
-- embedding BLOB
-- embedding_model TEXT
```

### Skills
| Skill | Description |
|-------|-------------|
| `session/embed` | Generate embedding for a session's summary |
| `session/recall` | Semantic search over session embeddings |

### Embedding Strategy

Combine structured fields for embedding:
```go
func BuildEmbeddingText(s Session) string {
    return fmt.Sprintf(
        "Project: %s\nSummary: %s\nAccomplished: %s\nDecisions: %s\nGotchas: %s\nTags: %s",
        s.ProjectName,
        s.Summary,
        strings.Join(s.Accomplished, "; "),
        strings.Join(s.Decisions, "; "),
        strings.Join(s.Gotchas, "; "),
        strings.Join(s.Tags, ", "),
    )
}
```

### Embedding Model Configuration

```yaml
# ~/.agentctl/config.yaml
sessions:
  embedding:
    provider: openai          # openai | voyage | local
    model: text-embedding-3-small
    dimensions: 1536

    # For local (Ollama):
    # provider: local
    # model: nomic-embed-text
    # ollama_url: http://localhost:11434
```

**Model Options:**
| Model | Provider | Cost | Dimensions | Notes |
|-------|----------|------|------------|-------|
| text-embedding-3-small | OpenAI | $0.02/1M | 1536 | Good baseline |
| text-embedding-3-large | OpenAI | $0.13/1M | 3072 | Higher quality |
| voyage-3-large | Voyage | $0.06/1M | 1024 | SOTA on MTEB |
| voyage-code-3 | Voyage | $0.06/1M | 1024 | Best for code |
| nomic-embed-text | Local | Free | 768 | Privacy, no API |

### Recall Skill

```bash
agentctl run session/recall --input '{
  "query": "authentication with JWT",
  "limit": 5,
  "threshold": 0.7,
  "workspace": "optional-filter"
}'
```

Output:
```json
{
  "matches": [
    {
      "session_id": "abc123",
      "project_name": "praze-api",
      "similarity": 0.89,
      "summary": "Implemented JWT auth with refresh tokens",
      "gotchas": ["Race condition in token refresh - used mutex"],
      "started_at": "2024-12-15T10:00:00Z"
    }
  ]
}
```

### Claude Skill Integration

```bash
# Agent can invoke during conversation
/agentctl-sessions recall "similar auth implementations"
/agentctl-sessions show <session-id>
```

---

## Phase 3: Full RAG + DSPy Export

### Goal
- Fine-grained retrieval (per-turn when needed)
- Training data export for agent improvement

### Schema Addition

```sql
CREATE TABLE session_turns (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    turn_index INTEGER NOT NULL,
    role TEXT NOT NULL,               -- user, assistant
    intent TEXT,                      -- Distilled intent
    content_preview TEXT,             -- First 500 chars
    tools_used JSON,
    files_touched JSON,
    error TEXT,                       -- Error if occurred
    resolution TEXT,                  -- How error was fixed
    timestamp TIMESTAMP,

    -- Optional per-turn embedding for fine-grained RAG
    embedding BLOB,

    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX idx_turns_session ON session_turns(session_id);
CREATE INDEX idx_turns_error ON session_turns(session_id) WHERE error IS NOT NULL;
CREATE INDEX idx_turns_tools ON session_turns(tools_used);
```

### Skills
| Skill | Description |
|-------|-------------|
| `session/expand` | Get detailed turns for a session |
| `session/turns` | Query specific turn patterns |
| `session/export-dspy` | Export training data |

### DSPy Export

```bash
agentctl run session/export-dspy --input '{
  "filter": {
    "has_tool_use": true,
    "min_turns": 5,
    "tags": ["refactor", "bugfix"]
  },
  "include_thinking": false,
  "format": "jsonl",
  "output_path": "./training_data.jsonl"
}'
```

Output format:
```jsonl
{"input": "Find auth-related files", "output": "Using Glob to search for auth patterns...", "trace": ["Glob(**/*auth*)", "Read(auth/handler.go)"], "success": true}
{"input": "Fix the race condition in token refresh", "output": "Adding mutex to TokenStore...", "trace": ["Read(token.go)", "Edit(token.go)"], "success": true, "error_resolved": "concurrent map write"}
```

### Training Data Filters

```go
type DSPyExportFilter struct {
    HasToolUse      bool     `json:"has_tool_use"`
    SuccessOnly     bool     `json:"success_only"`
    MinTurns        int      `json:"min_turns"`
    Tags            []string `json:"tags"`
    ToolPatterns    []string `json:"tool_patterns"`  // ["Edit", "Bash"]
    IncludeThinking bool     `json:"include_thinking"`
    Projects        []string `json:"projects"`       // Filter by project
}
```

---

## Implementation Order

```
Phase 1.1: session/capture skill ✅
    ├── Parse ~/.claude/projects/<workspace>/<session>.jsonl
    ├── High-signal filter implementation
    └── Store in sessions.db

Phase 1.2: session/summarize skill ✅
    ├── OpenRouter multi-model integration (free tier optimized)
    ├── Structured output parsing (JSON with user_insights)
    └── Update sessions.db with summary

Phase 1.3: Stop hook ✅
    └── .claude/hooks/session-capture.sh

Phase 1.4: CLI commands ✅
    ├── agentctl sessions list
    ├── agentctl sessions show
    ├── agentctl sessions search
    ├── agentctl sessions stats
    └── agentctl sessions delete

---

Phase 2.1: Embedding generation ✅
    ├── Gemini text-embedding-004 (768 dimensions)
    ├── Integrated into session/summarize skill
    └── Stored as binary float32 in sessions.db

Phase 2.2: session/recall skill ✅
    ├── Pure Go cosine similarity (no CGO)
    ├── Natural language query → embedding → search
    └── Filters: workspace, project, min_similarity

Phase 2.3: /agentctl-sessions Claude skill ✅
    ├── Invoke session/recall from Claude Code
    ├── Display relevant past sessions inline
    └── Link to full conversation on demand

---

Phase 3.1: session_turns table + extraction ✅
    ├── Per-turn high-signal extraction
    ├── Error → resolution tracking
    └── Tool call sequences

Phase 3.2: session/expand, session/turns skills ✅
    ├── Fine-grained turn-level retrieval
    └── "Show me how X error was fixed"

Phase 3.3: session/export-dspy skill ✅
    ├── Training data generation
    ├── Configurable filters (tags, tools, success)
    └── JSONL output format

---

Phase 4: JSONL Archive & Deep Retrieval ✅
    ├── Copy JSONL to ~/.agentctl/sessions/<session-id>.jsonl
    ├── Compress with gzip for storage efficiency
    ├── session/deep-dive skill for full conversation access
    ├── Chunk-level embeddings for precise retrieval
    └── Lazy loading: Tier 1 (summary) → Tier 2 (turns) → Tier 3 (full JSONL)

Phase 4.1: Context Windows (Tier 1.5) ✅
    ├── Detect compact_boundary markers in JSONL
    ├── session_context_windows table with token counts
    ├── session/archive skill extracts windows during parsing
    ├── Chunks tagged with context_window_index
    ├── agentctl sessions windows <id> CLI command
    └── Enables sub-session granularity retrieval
```

---

## Phase 4: JSONL Archive & Deep Retrieval (NEW)

### Goal
Enable "Tier 3" deep retrieval by archiving full JSONL files and providing
chunk-level access for precise RAG.

### Motivation
Current system stores summaries and embeddings but loses access to:
- Exact error messages and stack traces
- Full tool outputs (grep results, file contents)
- Thinking blocks and reasoning chains
- Precise code diffs from Edit operations

### Architecture

```
~/.agentctl/sessions/
├── index.db                    # Session metadata (existing sessions.db)
└── archives/
    ├── <session-id>.jsonl.gz   # Compressed full conversation
    └── <session-id>.chunks.db  # Chunk embeddings for deep search
```

### Schema: session_chunks

```sql
CREATE TABLE session_chunks (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT NOT NULL,
    chunk_index INTEGER NOT NULL,       -- Position in conversation
    chunk_type TEXT NOT NULL,           -- 'user', 'assistant', 'tool_result', 'error'
    content_hash TEXT NOT NULL,         -- SHA256 for dedup
    content_preview TEXT,               -- First 500 chars
    byte_offset INTEGER,                -- Offset in JSONL for lazy loading
    byte_length INTEGER,                -- Length for extraction
    turn_ref TEXT,                      -- Optional: turn/<id> reference
    chunk_ref TEXT,                     -- Optional: session/<id>/chunk/<idx> reference

    -- Metadata
    tools_used JSON,                    -- ["Edit", "Bash"]
    files_touched JSON,                 -- ["auth.go"]
    has_error BOOLEAN DEFAULT FALSE,
    error_type TEXT,                    -- "TypeError", "CompileError", etc.
    trace_id TEXT,                      -- Trace lineage for observability
    span_id TEXT,
    parent_span_id TEXT,

    -- Embedding for chunk-level search
    embedding BLOB,

    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
);

CREATE INDEX idx_chunks_session ON session_chunks(session_id);
CREATE INDEX idx_chunks_error ON session_chunks(session_id) WHERE has_error = TRUE;
CREATE INDEX idx_chunks_files ON session_chunks(files_touched);
CREATE INDEX idx_chunks_turn_ref ON session_chunks(turn_ref);
```

### Schema: session_context_windows

```sql
CREATE TABLE session_context_windows (
    id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    window_index INTEGER NOT NULL,           -- 0, 1, 2... per session
    started_at TEXT,                         -- First message timestamp in window
    ended_at TEXT,                           -- compact_boundary timestamp
    pre_compact_tokens INTEGER,              -- From compactMetadata.preTokens
    trigger TEXT,                            -- 'auto' or 'manual'
    chunk_start INTEGER,                     -- First chunk_index in window
    chunk_end INTEGER,                       -- Last chunk_index in window
    message_count INTEGER,                   -- Messages in this window
    summary TEXT,                            -- Per-window summary (optional)
    embedding BLOB,                          -- Per-window embedding (optional)
    embedding_model TEXT,
    created_at TEXT DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE,
    UNIQUE(session_id, window_index)
);

CREATE INDEX idx_context_windows_session ON session_context_windows(session_id);
CREATE INDEX idx_context_windows_ended ON session_context_windows(ended_at DESC);
```

Context windows are detected and created by the `session/archive` skill when it
parses JSONL files. Each `compact_boundary` marker triggers a new window.
Sessions without any compaction events are treated as a single window (index 0).

### Skills

| Skill | Description |
|-------|-------------|
| `session/archive` | Copy JSONL to archive, extract chunks, generate embeddings |
| `session/deep-dive` | Retrieve specific chunks or full conversation sections |
| `session/search-chunks` | Semantic search at chunk level |

### CLI Commands

```bash
# Archive a session (copies JSONL, indexes chunks, detects context windows)
agentctl sessions archive <session-id>

# Archive all unarchived sessions
agentctl sessions archive --all

# List context windows for a session
agentctl sessions windows <session-id>
agentctl sessions windows <session-id> --show-chunks

# Deep search across all sessions
agentctl sessions deep-search "race condition mutex"

# Get specific chunk from session
agentctl sessions chunk <session-id> --index 42

# Extract section around an error
agentctl sessions extract <session-id> --error --context 5
```

### Retrieval Flow

```
User query: "How did I fix the JWT refresh race condition?"
                    │
                    ▼
┌─────────────────────────────────────────────────────────────┐
│ Tier 1: Session-level search (fast, ~50ms)                  │
│ session/recall → "Session abc123: JWT auth implementation"  │
└─────────────────────────────────────────────────────────────┘
                    │ request temporal context?
                    ▼
┌─────────────────────────────────────────────────────────────┐
│ Temporal Pyramid View                                       │
│ months → weeks → days → hours (with expandable refs)        │
└─────────────────────────────────────────────────────────────┘
                    │ need more detail?
                    ▼
┌─────────────────────────────────────────────────────────────┐
│ Tier 1.5: Context window search (~100ms)                    │
│ session/windows → Window 1 (89K tokens): token refresh work │
│                   chunks 45-120, trigger=auto               │
└─────────────────────────────────────────────────────────────┘
                    │ narrow to specific chunks?
                    ▼
┌─────────────────────────────────────────────────────────────┐
│ Tier 2: Chunk-level search (~200ms)                         │
│ session/search-chunks → Chunks 78-81: mutex implementation  │
└─────────────────────────────────────────────────────────────┘
                    │ need exact code?
                    ▼
┌─────────────────────────────────────────────────────────────┐
│ Tier 3: Full extraction (lazy load from .jsonl.gz)          │
│ session/deep-dive → Full Edit tool call with diff           │
└─────────────────────────────────────────────────────────────┘
```

### Event-Driven Enrichment Hook (PR-10 Alignment, V2 Target)

Turn/chunk indexing should emit events so artifacts can be generated
asynchronously:

- `turn.recorded`
- `chunk.indexed`
- `bucket.updated`
- `artifact.created`
- `artifact.failed`

Artifacts (summaries, embeddings, labels) should use idempotency keys:
`(subject_id, artifact_kind, artifact_version)` so retries are safe and
turn completion remains non-blocking.

### Storage Estimates

| Sessions | JSONL (raw) | Compressed | Chunk DB | Total |
|----------|-------------|------------|----------|-------|
| 100      | ~2 GB       | ~400 MB    | ~50 MB   | ~450 MB |
| 500      | ~10 GB      | ~2 GB      | ~250 MB  | ~2.25 GB |
| 1000     | ~20 GB      | ~4 GB      | ~500 MB  | ~4.5 GB |

### Cleanup Policy

```yaml
# ~/.agentctl/config.yaml
sessions:
  archive:
    enabled: true
    retention_days: 90          # Delete archives older than 90 days
    max_storage_gb: 5           # Cap total archive storage
    compress: true              # gzip compression (default)
    chunk_embeddings: true      # Generate chunk-level embeddings
```

---

## Success Criteria

- [x] Sessions captured automatically on compaction
- [x] Summaries generated via OpenRouter free-tier models
- [x] Semantic recall returns relevant sessions
- [x] Agent can invoke session recall during work (Phase 2.3)
- [x] DSPy export produces valid training data (Phase 3.3)
- [x] Cross-project learnings surface appropriately
- [ ] Works offline with local embedding model (future)
- [x] Full conversation retrieval via JSONL archives (Phase 4)
- [x] Chunk-level semantic search for precise retrieval (Phase 4)
- [x] Context windows detected from compact_boundary markers (Phase 4.1)
- [x] Sub-session granularity via context window queries (Phase 4.1)

---

## Related Systems

| System | Purpose | Integration |
|--------|---------|-------------|
| `memory.db` | Named memories, cache | Sessions can be saved as named memories |
| `trajectory.db` | Agentctl skill execution tracking | Different lifecycle than Claude sessions |
| `tasks.db` | Task management | Sessions can reference active task |
| `plan/sync` | Claude plans | Sessions can reference active plan |

---

## Future Considerations

1. **Auto-injection**: SessionStart hook that injects relevant past sessions based on current context
2. **Feedback loop**: Mark sessions as helpful/not helpful for training data quality
3. **Cross-user**: Anonymized session sharing for team learning
4. **Multimodal**: Handle image/screenshot content in sessions
5. **Incremental sync**: Watch for new JSONL files and auto-archive
6. **Deduplication**: Detect and link related sessions (same bug, same feature)
7. **Session chains**: Track conversation continuations across compactions
