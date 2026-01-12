# Knowledge–Factory Bridge Specification

**Status:** Draft\
**Last Updated:** 2025-11-27

---

## 1. Overview

This document defines how **Factory AI / DROID** assets ("droids", orchestrator
configuration, and documentation) are represented inside the **agentctl
knowledge registry**.

Goals:

- **Unify discovery** of Factory droids and orchestrator docs with existing
  knowledge packs, agents, and commands.
- **Avoid per-repo copies** of Factory assets; instead, treat them as **builtin
  knowledge** available to all workspaces via `agentctl knowledge`.
- **Align concepts** so that Factory _droids_ and Claude _agents_ share a common
  representation (`kind: "agent"`), enabling joint recommendation and routing.

This spec builds on the `docs/spec/knowledge_registry.md` schema and terminology
without changing the JSON envelope wire contract.

---

## 2. Relationship to the Knowledge Registry

The knowledge registry spec defines:

- `knowledge_items` with fields:
  - `name` (unique string)
  - `kind` (`'pack' | 'agent' | 'command'`)
  - `description`
  - `source_path` (relative path within workspace)
- `knowledge_triggers` for rule-based matching.
- `knowledge_documents` and optional embeddings.

The Factory bridge does **not** introduce new wire-level fields. Instead, it
uses **naming conventions**, **tags**, and **synthetic source paths** to
represent Factory knowledge within the existing schema:

- Factory items use a **name prefix**: `factory/<category>/<slug>`.
- Factory items use **synthetic source paths** with a `builtin://` scheme to
  indicate that the original documents are embedded, not located in the current
  workspace.
- Factory-specific metadata (e.g., "droid" vs. "orchestrator") is represented
  via tags and naming rather than new DB columns.

If we later extend the DB schema (e.g., adding a `provider` column), this spec
will be updated accordingly, but initial integration relies only on conventions.

---

## 3. Factory Source Assets

Factory assets are authored and maintained outside of agentctl, typically under:

- `~/.factory/droids/*.md` – role definitions and workflows ("droids").
- `~/.factory/orchestrator/*` – orchestrator configuration and docs.
- Additional docs under `~/.factory/docs/`.

For development of this bridge, a subset of these assets is vendored in this
repo under `Droid-CLI-Orchestrator/`. At runtime, agentctl treats Factory
content as **builtin knowledge**, not as files in the user workspace.

### 3.1 Embedded Subset

To avoid embedding the entire Factory tree and to keep agentctl focused, we only
embed a **curated subset** of Factory content:

- **Orchestrator overview** (markdown):
  - High-level description of how the orchestrator works.
- **Representative droids**:
  - `backend-architect`
  - `frontend-developer`
  - `code-reviewer` (optional but useful)
- **Key orchestrator config artifacts**:
  - `orchestrator-config.json` (for reference only)
  - `task-patterns.json` (for reference only)

The embedded subset can be expanded in future versions, but the schema and
naming conventions defined here must remain stable.

---

## 4. Mapping Factory Assets to Knowledge Items

### 4.1 Naming Conventions

Each Factory asset is mapped to a `knowledge_items.name` as follows:

- **Droids → Agents**

  ```text
  factory/droid/<slug>
  ```

  Examples:

  - `factory/droid/backend-architect`
  - `factory/droid/frontend-developer`

- **Orchestrator docs → Packs or docs**

  ```text
  factory/orchestrator/<slug>
  ```

  Examples:

  - `factory/orchestrator/overview`
  - `factory/orchestrator/task-patterns`

This prefixing makes it trivial to filter Factory-related knowledge items via
CLI or hooks (`WHERE name LIKE 'factory/%'`).

### 4.2 Kinds

The `kind` field in `knowledge_items` is used as follows:

- **Droids**: `kind = 'agent'`
  - Semantically equivalent to Claude agents stored under `.claude/agents/`.
- **Orchestrator docs**: `kind = 'pack'` or `'command'` depending on usage:
  - High-level orchestrator guides → `kind = 'pack'`.
  - If we later expose orchestrator as a pre-defined workflow template, it may
    also be represented as a `kind = 'command'` pointing to the same documents.

### 4.3 Source Paths

Because Factory content is treated as builtin, not authored inside the current
workspace, we use a **synthetic source path scheme** in `knowledge_items` and
`knowledge_documents`:

- Droids:

  ```text
  source_path = "builtin://factory/droids/<slug>.md"
  ```

- Orchestrator docs:

  ```text
  source_path = "builtin://factory/orchestrator/<slug>.md"
  ```

- JSON config artifacts (if stored as documents):

  ```text
  source_path = "builtin://factory/orchestrator/orchestrator-config.json"
  source_path = "builtin://factory/orchestrator/task-patterns.json"
  ```

The `builtin://` prefix is reserved for embedded, read-only content that is
shipped with agentctl itself.

### 4.4 Triggers

Triggers for Factory items follow the same `knowledge_triggers` table as other
knowledge:

- **Droids**:
  - `trigger_kind = 'keyword'` patterns for role and domain terms, e.g.:
    - `"backend"`, `"architecture"` for `backend-architect`.
    - `"frontend"`, `"component"`, `"UI"` for `frontend-developer`.
  - Additional `trigger_kind = 'intent'` patterns may be added later.

- **Orchestrator docs**:
  - Keywords like `"orchestrator"`, `"droid"`, `"factory"`, `"task patterns"`.

Factory triggers are defined in code (for builtin items) rather than in
`skill-rules.json`, but they are stored in the same DB tables and surfaced via
`knowledge search` and `hooks/knowledge_router`.

---

## 5. Ingestion and Seeding

### 5.1 Builtin Seeding

Factory knowledge is seeded as **builtin items**. There are two supported
mechanisms:

1. **Automatic bootstrap** (recommended default):

   - On first use of:
     - `agentctl knowledge list` or
     - `agentctl knowledge search` or
     - `hooks/knowledge_router` hook,
   - agentctl ensures that builtin Factory items exist in the `knowledge_items`
     and related tables.

2. **Explicit sync command** (optional):

   ```bash
   agentctl knowledge sync --builtin
   ```

   - Ensures all builtin knowledge sources (including Factory) are present in
     the registry.

In both cases, Factory entries are **read-only** from the perspective of normal
CLI operations. User-created knowledge (packs, agents, commands) is still
ingested from the filesystem as described in `knowledge_registry.md`.

### 5.2 Idempotency and Versioning

- Builtin Factory items are identified by stable `name` values
  (`factory/droid/backend-architect`, etc.).
- Seeding is idempotent: rerunning bootstrap or `knowledge sync --builtin`
  updates descriptions and documents but preserves stable IDs where possible.
- Changes to the embedded Factory subset should bump the **spec last-updated
  date** and be accompanied by a changelog entry.

---

## 6. CLI and Hook Integration

### 6.1 CLI: `agentctl knowledge`

Factory knowledge appears alongside user-authored knowledge:

- List all Factory items:

  ```bash
  agentctl knowledge list --prefix factory/
  ```

- Search with Factory filters:

  ```bash
  agentctl knowledge search --query "orchestrator phases" --prefix factory/
  ```

Exact flags and filtering semantics are defined by the `knowledge` CLI, but this
spec requires that Factory items be distinguishable by name prefix.

### 6.2 Hook: `hooks/knowledge_router`

The `hooks/knowledge_router` implementation SHOULD:

- Include Factory items in its candidate pool when matching:
  - User prompts mentioning "orchestrator", "droid", or related concepts.
  - File paths or contexts that suggest orchestrated, multi-phase workflows.
- Treat Factory items the same as other knowledge items in its scoring and
  threshold logic.
- Keep behavior **advisory only**: Factory knowledge recommendations must not
  block or force specific tools; they only add context hints.

Example hook output when Factory knowledge is relevant:

```json
{
	"decision": "none",
	"context": "Recommended: factory/orchestrator/overview (high confidence).",
	"meta": {
		"recommended": [
			{
				"name": "factory/orchestrator/overview",
				"kind": "pack",
				"score": 0.82
			},
			{
				"name": "factory/droid/backend-architect",
				"kind": "agent",
				"score": 0.74
			}
		],
		"threshold": 0.7
	}
}
```

---

## 7. Future Extensions

Potential future enhancements (out of scope for this draft):

- **Schema-level provider field**
  - Add `provider` column to `knowledge_items` to distinguish:
    - `"claude"`, `"factory"`, `"custom"`, etc.
  - Update ingestion and CLI filters accordingly.

- **Deeper droid–agent integration**
  - Map selected Factory droids directly into `.claude/agents/` wrappers that
    reference the same underlying knowledge item.
  - Allow the knowledge registry to recommend _either_ a Claude agent or a
    Factory droid for a given task, based on context.

- **Task pattern integration**
  - Represent orchestrator `task-patterns.json` as structured knowledge that can
    inform planning skills or future `agentctl` features.

Any such changes must update this spec, preserve backwards compatibility for
existing knowledge entries, and avoid breaking the JSON envelope wire contract.
