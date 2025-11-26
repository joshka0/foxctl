# Knowledge Registry Specification

**Status:** Draft\
**Last Updated:** 2025-11-26

---

## 1. Overview

The **knowledge registry** is a SQLite-backed store that indexes Claude-facing
knowledge packs, agents, and commands. It enables:

- **Unified discovery** of all knowledge objects.
- **Rule-based matching** (keywords, intent patterns, path patterns).
- **Embedding-based retrieval** (optional, for semantic similarity).
- **Threshold-gated surfacing** via `hooks/knowledge_router`.

This is distinct from **agentctl skills**, which are executable Go/WASI/exec
plugins managed via `agentctl skills ...`.

---

## 2. Terminology

| Term               | Definition                                                                                      |
| ------------------ | ----------------------------------------------------------------------------------------------- |
| **Knowledge pack** | A markdown + resources bundle in `docs/knowledge/<name>/` providing domain guidance for Claude. |
| **Agent**          | A prompt profile in `.claude/agents/<name>.md` for multi-step autonomous work.                  |
| **Command**        | A prompt template in `.claude/commands/<name>.md` for structured workflows.                     |
| **Knowledge item** | A row in the registry representing a pack, agent, or command.                                   |
| **Trigger**        | A rule (keyword, regex, path pattern) that activates a knowledge item.                          |
| **Document**       | A text chunk (main file or resource) associated with a knowledge item.                          |
| **Embedding**      | A vector representation of a document for semantic search.                                      |

---

## 3. Storage Layout

### 3.1 Filesystem (authoring)

```
agentctl/
├── docs/knowledge/
│   ├── backend-dev-guidelines/
│   │   ├── SKILL.md              # Main knowledge pack doc
│   │   └── resources/            # Supporting files
│   ├── frontend-dev-guidelines/
│   ├── skill-rules.json          # Trigger definitions (design spec)
│   └── README.md
├── .claude/
│   ├── agents/
│   │   ├── code-architecture-reviewer.md
│   │   └── ...
│   ├── commands/
│   │   ├── dev-docs.md
│   │   └── ...
│   ├── hooks/
│   │   └── task-guard.sh
│   └── settings.json
```

### 3.2 SQLite (runtime)

Database: `~/.agentctl/storage/knowledge.db`

#### Tables

```sql
-- Knowledge items (packs, agents, commands)
CREATE TABLE knowledge_items (
    id            TEXT PRIMARY KEY,  -- ULID
    name          TEXT NOT NULL UNIQUE,
    kind          TEXT NOT NULL,     -- 'pack' | 'agent' | 'command'
    description   TEXT,
    source_path   TEXT NOT NULL,     -- Relative to workspace
    priority      TEXT DEFAULT 'medium',
    created_at    TEXT NOT NULL,     -- RFC3339Nano
    updated_at    TEXT NOT NULL
);

-- Triggers for rule-based matching
CREATE TABLE knowledge_triggers (
    id            TEXT PRIMARY KEY,
    item_id       TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
    trigger_kind  TEXT NOT NULL,     -- 'keyword' | 'intent' | 'path' | 'content'
    pattern       TEXT NOT NULL
);
CREATE INDEX idx_triggers_item ON knowledge_triggers(item_id);
CREATE INDEX idx_triggers_kind ON knowledge_triggers(trigger_kind);

-- Documents (text chunks for embedding/search)
CREATE TABLE knowledge_documents (
    id            TEXT PRIMARY KEY,
    item_id       TEXT NOT NULL REFERENCES knowledge_items(id) ON DELETE CASCADE,
    title         TEXT,
    source_path   TEXT,              -- Relative path to file
    body          TEXT,              -- Full text (or CAS digest if large)
    body_digest   TEXT,              -- sha256:<hex> if stored in CAS
    created_at    TEXT NOT NULL
);
CREATE INDEX idx_documents_item ON knowledge_documents(item_id);

-- Embeddings (optional, for semantic search)
CREATE TABLE knowledge_embeddings (
    document_id   TEXT PRIMARY KEY REFERENCES knowledge_documents(id) ON DELETE CASCADE,
    model         TEXT NOT NULL,     -- e.g., 'text-embedding-3-small'
    dim           INTEGER NOT NULL,
    vector        BLOB NOT NULL      -- float32 array
);
```

---

## 4. CLI: `agentctl knowledge`

### 4.1 `agentctl knowledge sync`

Indexes knowledge from the filesystem into SQLite.

```bash
agentctl knowledge sync [--embed] [--model <model>] [--workspace <path>]
```

**Flags:**

- `--embed` — Compute embeddings for all documents.
- `--model` — Embedding model to use (default: configured in `config.yaml`).
- `--workspace` — Workspace root (default: current directory).

**Behavior:**

1. Walk `docs/knowledge/`, `.claude/agents/`, `.claude/commands/`.
2. Parse `skill-rules.json` for trigger definitions.
3. Upsert `knowledge_items`, `knowledge_triggers`, `knowledge_documents`.
4. If `--embed`, compute and store embeddings.

**Output:** JSON envelope with sync summary.

### 4.2 `agentctl knowledge list`

List all knowledge items.

```bash
agentctl knowledge list [--kind <pack|agent|command>]
```

### 4.3 `agentctl knowledge search`

Search knowledge by query (rule-based + optional embedding).

```bash
agentctl knowledge search --query "frontend component patterns" [--threshold 0.7]
```

**Output:** Ranked list of matching items with scores.

---

## 5. Hook: `hooks/knowledge_router`

### 5.1 Event

- **Primary:** `UserPromptSubmit`
- **Optional:** `PreToolUse` (for file-context hints)

### 5.2 Input

```json
{
	"event": "UserPromptSubmit",
	"workspace_root": "/path/to/project",
	"session_id": "...",
	"prompt": "Create a new React component with MUI Grid",
	"file_path": "src/components/MyComponent.tsx"
}
```

### 5.3 Algorithm

1. **Rule pass:**
   - Match prompt against `knowledge_triggers` (keywords, intent regex).
   - Match file path against path patterns.
   - Collect candidate items with rule scores.

2. **Embedding pass** (if embeddings exist):
   - Compute embedding for prompt.
   - Retrieve top-K nearest documents.
   - Score candidates by cosine similarity.

3. **Combine scores:**
   - Weighted combination of rule score + embedding score.
   - Rank candidates.

4. **Threshold check:**
   - If `max(score) < threshold` → emit `DecisionNone`, no context.
   - If `≥ threshold` → emit `DecisionNone` with context hint.

### 5.4 Output

```json
{
	"decision": "none",
	"context": "Recommended: frontend-dev-guidelines (high confidence).",
	"meta": {
		"recommended": [
			{
				"name": "frontend-dev-guidelines",
				"kind": "pack",
				"score": 0.87
			},
			{ "name": "error-tracking", "kind": "pack", "score": 0.62 }
		],
		"threshold": 0.7
	}
}
```

### 5.5 Configuration

Via `.claude/knobs/knowledge.json` (optional):

```json
{
	"threshold": 0.7,
	"max_recommendations": 3,
	"use_embeddings": true,
	"embedding_weight": 0.6,
	"rule_weight": 0.4
}
```

---

## 6. Ingestion Details

### 6.1 Knowledge Packs

- **Source:** `docs/knowledge/<name>/SKILL.md` (or `README.md`)
- **Triggers:** From `docs/knowledge/skill-rules.json` under `skills.<name>`
- **Documents:** Main file + `resources/*.md`

### 6.2 Agents

- **Source:** `.claude/agents/<name>.md`
- **Triggers:** Extracted from YAML frontmatter `description` field (keywords)
- **Documents:** Full agent file

### 6.3 Commands

- **Source:** `.claude/commands/<name>.md`
- **Triggers:** Extracted from YAML frontmatter `description` field
- **Documents:** Full command file

---

## 7. Embedding Strategy

### 7.1 Provider

Embeddings are computed via an **exec-based helper** (not WASI, since network is
required). The helper is configured in `config.yaml`:

```yaml
knowledge:
    embedding:
        provider: openai # or 'local', 'ollama', etc.
        model: text-embedding-3-small
        dimensions: 1536
```

### 7.2 Chunking

- Documents > 8K tokens are chunked with overlap.
- Each chunk gets its own embedding row.
- Search returns document-level scores (max of chunk scores).

### 7.3 In-Memory Search

For small N (< 10K documents), cosine similarity is computed in Go:

```go
func cosineSimilarity(a, b []float32) float32 {
    var dot, normA, normB float32
    for i := range a {
        dot += a[i] * b[i]
        normA += a[i] * a[i]
        normB += b[i] * b[i]
    }
    return dot / (sqrt(normA) * sqrt(normB))
}
```

No SQLite vector extension required (stays CGO-free).

---

## 8. Migration Path

### Phase 1: Rule-based only

- Implement `agentctl knowledge sync` (no embeddings).
- Implement `hooks/knowledge_router` with rule matching only.

### Phase 2: Add embeddings

- Add `--embed` flag to sync.
- Add embedding pass to router.
- Configure threshold in knobs.

### Phase 3: Task integration

- Track which knowledge items were consulted per task.
- Use for analytics and improved recommendations.

---

## 9. Open Questions

1. **Embedding provider:** Should we default to OpenAI, or require explicit
   configuration?
2. **Threshold tuning:** What's a good default? Should it be per-item?
3. **Context injection:** Should the hook inject full doc content, or just names
   for Claude to fetch?
4. **Caching:** Should embeddings be cached in CAS for reproducibility?

---

## 10. References

- [Core Profile v1](core_profile_v1.md) — Envelope and storage contracts
- [Task Hooks Memory](task_hooks_memory.md) — Hook architecture
- [skill-rules.json](../../docs/knowledge/skill-rules.json) — Trigger
  definitions
