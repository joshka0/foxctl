# Spec: Bidirectional Todo Sync (Claude Code ⇄ agentctl) v1

## 1. Overview

This feature provides **bidirectional synchronization** between:

* **Claude Code native todos** (stored as JSON files under `~/.claude/todos/`)
* **agentctl tasks** (the canonical source of truth, enriched, ranked, graph-aware)

Claude's todo UI is treated as a **thin projection**: it can only display `content` + `status`, and strips any extra metadata fields. Therefore, the projection encodes only minimal hints (glyphs + a stable task id tag), while full task context lives in agentctl and is retrievable on-demand.

---

## 2. Goals

### 2.1 Goals

1. **Source of truth:** agentctl task store is canonical.
2. **Bidirectional sync:**
   * Claude → agentctl: TodoWrite changes become agentctl task updates.
   * agentctl → Claude: task changes in agentctl update Claude's todo file.
3. **Stable mapping:** each Claude todo line maps to exactly one agentctl task via an embedded stable tag.
4. **Low-noise UI:** Claude todo `content` remains short; rich context is obtained via agentctl tools (`todo/context`).
5. **Graph-aware ordering:** Claude todo list can be re-ordered from agentctl ranking (pagerank/critical-path/readiness), with a controlled cadence.
6. **Safe + contained:** writing `~/.claude/todos` is privileged (hooks/daemon), not generally available to arbitrary agents/skills unless explicitly allowed.

### 2.2 Non-goals (v1)

* Injecting hidden metadata into Claude todos (Claude strips extra fields).
* Real-time multi-client todo collaboration.

---

## 3. Provider File Layout

### 3.1 Claude Code todos

Location: `~/.claude/todos/<session_id>-agent-<session_id>.json`

Format:
```json
[
  {
    "content": "Task description",
    "status": "pending|in_progress|completed",
    "activeForm": "Present tense description"
  }
]
```

### 3.2 Session detection

Priority order:
1. `CLAUDE_SESSION_ID` env var
2. `AGENTCTL_SESSION_ID` env var
3. Identity file scan in `~/.agentctl/sessions/active/`
4. Newest file in `~/.claude/todos/` matching workspace

---

## 4. Stable Task ID Tag (MUST)

Every projected todo line MUST include an agentctl task ID tag.

### 4.1 Tag format

Use a visually light bracket token at the end:

```
〔T:<task_id>〕
```

Example:
```
▶ Add auth middleware ordering ⛓0〔T:01HF...〕
```

### 4.2 Mapping rules

* If `〔T:<id>〕` exists, it is the **primary key** linking Claude todo ↔ agentctl task.
* If missing, agentctl MUST treat it as "unmapped" and create or match heuristically (see §7).

---

## 5. Minimal Hint Encoding (SHOULD)

Because Claude todos are short, we use glyphs for quick "readiness".

### 5.1 Status glyph (optional but recommended)

| Glyph | Status |
|-------|--------|
| `▶` | in_progress |
| `□` | pending |
| `✓` | completed |

### 5.2 Dependency hint (recommended)

| Glyph | Meaning |
|-------|---------|
| `⛓<n>` | Number of unresolved dependencies |
| `⛔` | Blocked (agentctl status `blocked`; Claude status remains `pending`) |

Example:
```
□ Implement refresh token flow ⛔⛓2〔T:01HF...〕
```

### 5.3 Keep it short

* Projection `content` SHOULD target ≤ 80 characters before the id tag.
* The id tag MUST remain present even if title truncation is required.

---

## 6. Public Operations (Conceptual API)

These are **agentctl-internal services** and optionally surfaced as commands/skills with strict capability gating.

### 6.1 `todo/sync_from_provider` (Claude → agentctl)

Reads provider todos and applies diffs to agentctl tasks.

**Input:**
| Field | Type | Description |
|-------|------|-------------|
| `workspace_id` | string | Workspace path |
| `session_id` | string | Session identifier |
| `provider` | string | `"claude"` |
| `mode` | string | `"merge"` (default) or `"replace"` |
| `dry_run` | bool | Preview only |

**Output:**
* counts: created/updated/completed/reopened/mapped/unmapped
* warnings: duplicates, conflicts
* optional artifact with full diff details

### 6.2 `todo/sync_to_provider` (agentctl → Claude)

Computes projection from agentctl tasks and writes provider todo file.

**Input:**
| Field | Type | Description |
|-------|------|-------------|
| `workspace_id` | string | Workspace path |
| `session_id` | string | Session identifier |
| `provider` | string | `"claude"` |
| `order` | string | `"agentctl_rank"` (default), `"stable"`, or `"off"` |
| `max_items` | int | Optional limit |
| `dry_run` | bool | Preview only |

**Output:**
* counts: written/updated/unchanged
* warnings: merge conflicts, missing provider file
* optional artifact with before/after preview

### 6.3 `todo/context` (on-demand)

Returns rich task context from agentctl (story, deps, files, symbols, snippets).

This is how Claude gets full context without bloating the todo list.

### 6.4 `todo/enrich` (async or sync)

Generates richer story/AC/subtasks/files/symbols using:
* semantic_search + symbols + swe_grep
* optional Cerebras synthesis pass

Persists results to agentctl task context storage.

---

## 7. Inbound Sync Rules (Claude → agentctl)

### 7.1 Trigger points

Inbound sync SHOULD run on:
* Claude Code `PostToolUse` when tool is `TodoWrite`
* SessionStart/resume (to reconcile stale states)
* Optional: file watcher (daemon) if you want manual edits to be captured

### 7.2 Merge semantics

For each provider todo item:

1. If it contains `〔T:<id>〕`:
   * Upsert that exact task.
2. If it has no tag:
   * Try to match by normalized title against existing tasks in the same `(workspace_id, session_id)` scope.
   * If no match: create a new agentctl task and assign an id, then schedule outbound sync to insert the tag.

### 7.3 Status mapping

Provider → agentctl:

| Provider Status | agentctl Status |
|-----------------|-----------------|
| `pending` | `pending` |
| `in_progress` | `in_progress` (also set active task) |
| `completed` | `completed` |

If provider marks completed but agentctl task is blocked/in_progress, agentctl SHOULD accept provider completion as authoritative *unless configured otherwise*.

---

## 8. Outbound Sync Rules (agentctl → Claude)

### 8.1 Trigger points

Outbound sync SHOULD run when agentctl changes tasks via:
* `todo/manage add|update|complete|set_active|block|unblock`
* `todo/enrich` completion
* optional: after pagerank/graph update (rate-limited)

### 8.2 Projection computation

Projection list is derived from agentctl tasks filtered by:
* workspace_id
* session_id (if using session-scoped todos) OR "workspace global" mode (configurable)

Each projected item's `content` MUST include `〔T:<id>〕`.

### 8.3 Ordering

Default ordering SHOULD be:

1. `in_progress` first
2. ready `pending` (deps satisfied)
3. blocked `pending` (deps unmet)
4. completed last (optional; or omit completed from projection)

Within groups:
* sort by agentctl rank score (pagerank + critical path + recency + signals)

**Config knobs:**
* `AGENTCTL_TODO_PROJECTION_ORDER=agentctl_rank|stable|off`
* `AGENTCTL_TODO_PROJECTION_REORDER_CADENCE=on_todowrite|session_start|manual|N_seconds`

---

## 9. Conflict Resolution (MUST be deterministic)

Conflicts happen when:
* Claude writes todos (TodoWrite)
* agentctl writes projection
* user manually edits `~/.claude/todos/*.json`

### 9.1 Writer strategy

Outbound sync MUST write atomically:
1. write to temp file in same dir
2. `fsync`
3. rename into place

### 9.2 Merge strategy (recommended)

Maintain a "last-written hash" in agentctl:
* Store `{provider_file_path, last_projection_sha256, updated_at}` in agentctl storage (sqlite)

When writing outbound:
* If provider file hash == last_projection_sha256: safe overwrite
* Else: perform a merge:
  * preserve any items that agentctl cannot map (no tag) as "unmapped" section
  * re-apply projection items by tag (id wins)
  * keep provider ordering if `order=stable`

---

## 10. Security / Capability Boundary (IMPORTANT)

Writing `~/.claude/todos` is **outside workspace**, so it must be privileged.

### 10.1 Boundary rule

* The provider todo file IO MUST live in an internal integration layer (daemon / hooks service), not as a general "filesystem" skill callable by arbitrary agents.
* If exposed as a skill, it MUST require an explicit capability gate (env flag set by hooks), e.g.:
  * `AGENTCTL_ALLOW_PROVIDER_STATE=1`

### 10.2 Path policy

Either:
* extend PathValidator allowed roots to include `~/.claude/` (only for the privileged component), **or**
* bypass PathValidator inside a restricted internal package that never accepts arbitrary paths (it only touches known provider directories).

---

## 11. Observability (MUST)

Each sync run SHOULD emit a wide event:

**Operation names:**
* `todo.sync_in` (provider → agentctl)
* `todo.sync_out` (agentctl → provider)
* `todo.enrich` (enrichment lifecycle)

**Event data examples:**
| Field | Description |
|-------|-------------|
| `provider` | `"claude"` |
| `workspace_id` | Workspace path |
| `session_id` | Session identifier |
| `items_total` | Total items processed |
| `items_created` | New tasks created |
| `items_updated` | Tasks updated |
| `items_completed` | Tasks completed |
| `conflicts_detected` | Number of conflicts |
| `projection_reordered` | `true`/`false` |
| `duration_ms` | Execution time |
| `warnings_count` | Warning count |
| `error_code` | Error code if failure |

---

## 12. Implementation Structure (Recommended)

### 12.1 Packages

```
internal/
├── todosync/
│   ├── service.go          # Core sync logic, merge rules
│   ├── projection.go       # Glyph + tag formatting
│   └── conflict.go         # Conflict detection/resolution
├── providers/
│   └── claude/
│       └── todos/
│           └── store.go    # TodoStore implementation
└── todoenrich/
    └── worker.go           # Queue + worker (optional)
```

**Reuse:**
* `internal/storage/tasks`
* `internal/storage/graph` + pagerank
* `internal/intelligence/retrieval` + `code/snippet_extract`
* `internal/observability`

### 12.2 Session file locator

Use existing identity/session detection:
1. env: `CLAUDE_SESSION_ID` or `AGENTCTL_SESSION_ID`
2. fallback: identity file (`sessions.NewIdentityManager`)
3. then map to file:
   * preferred: `~/.claude/todos/<sid>-agent-<sid>.json`
   * fallback: scan `~/.claude/todos` for newest file containing sid

---

## 13. Rollout Plan

### Phase 1: Inbound refactor (now)

* Ensure every imported task gets a stable `task_id` stored in agentctl
* Add `todo/sync_from_provider` internal service
* Refactor `todo-sync.sh` hook to call it
* Parse and preserve `〔T:<id>〕` tags from existing content

### Phase 2: Outbound projection + id tags

* Implement `todo/sync_to_provider`
* On any agentctl task mutation, update Claude file
* Add atomic write with temp file + rename
* Store last-written hash for conflict detection

### Phase 3: Ranking + glyphs

* Format `content` with `⛓n`, `⛔`, status glyph
* Reorder according to agentctl rank with cadence controls
* Add config knobs for ordering preferences

### Phase 4: Enrichment pipeline (future)

* Implement `todo/enrich` (sync)
* Queue/daemon worker for async enrichment

---

## 14. Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `AGENTCTL_TODO_BIDIRECTIONAL` | `1` | Enable bidirectional sync |
| `AGENTCTL_TODO_PROJECTION_ORDER` | `agentctl_rank` | Ordering: `agentctl_rank`, `stable`, `off` |
| `AGENTCTL_TODO_PROJECTION_GLYPHS` | `1` | Enable status/dep glyphs |
| `AGENTCTL_TODO_PROJECTION_CADENCE` | `on_todowrite` | When to reorder |
| `AGENTCTL_ALLOW_PROVIDER_STATE` | `0` | Allow provider file writes (hooks only) |

---

## 15. Tag Parsing Regex

```go
var taskIDTagRe = regexp.MustCompile(`〔T:([A-Za-z0-9]+)〕`)

func ParseTaskID(content string) string {
    matches := taskIDTagRe.FindStringSubmatch(content)
    if len(matches) >= 2 {
        return matches[1]
    }
    return ""
}

func StripTaskID(content string) string {
    return strings.TrimSpace(taskIDTagRe.ReplaceAllString(content, ""))
}

func AppendTaskID(content, taskID string) string {
    return fmt.Sprintf("%s 〔T:%s〕", strings.TrimSpace(content), taskID)
}
```

---

## 16. Example Projection

**agentctl tasks:**
```json
[
  {"id": "01HF1234", "title": "Add auth middleware", "status": "in_progress", "depends_on": []},
  {"id": "01HF5678", "title": "Implement refresh token flow", "status": "pending", "depends_on": ["01HF1234", "01HFABCD"]},
  {"id": "01HF9ABC", "title": "Write auth tests", "status": "completed", "depends_on": []}
]
```

**Projected to Claude:**
```json
[
  {"content": "▶ Add auth middleware 〔T:01HF1234〕", "status": "in_progress", "activeForm": "Adding auth middleware"},
  {"content": "□ Implement refresh token flow ⛓2 〔T:01HF5678〕", "status": "pending", "activeForm": "Implementing refresh token flow"},
  {"content": "✓ Write auth tests 〔T:01HF9ABC〕", "status": "completed", "activeForm": "Writing auth tests"}
]
```
