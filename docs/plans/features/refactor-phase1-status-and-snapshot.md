# Refactor Phase 1 Spec: Status and Snapshot

Status: active plan

Owner: agentctl

Last updated: 2026-04-01

Parent plan:

- [Refactor Intelligence Substrate Plan](./refactor-intelligence-substrate.md)

## Goal

Define the first concrete refactor intelligence primitives:

- `agentctl refactor status`
- `agentctl refactor snapshot`

These commands establish the baseline questions that the current refactor
workflow cannot answer cleanly today:

- is this run parser-only or index-backed
- is the repo graph fresh enough to trust
- what exact scope am I analyzing
- can I freeze that scope into a stable artifact for later diffing and evidence
  reuse

## Decision

Phase 1 will be:

- CLI-first under `agentctl refactor ...`
- implemented in shared internal packages, not bespoke command-local logic
- compatible with current `refactor scout`
- single-language by default, matching current scout invariants

It will **not** introduce a new standalone skill in the first pass.

Reason:

- these commands are control-plane substrate around repoindex, git state, scope
  resolution, and CAS-backed snapshot persistence
- keeping them in the CLI first is simpler and makes the contract easier to
  stabilize before exposing them as skills

## Scope

This phase includes:

- shared scope resolution for refactor commands
- repoindex freshness/status evaluation
- deterministic scope snapshot creation
- snapshot metadata persistence
- CAS artifact persistence for full snapshot payloads
- `refactor scout` output enrichment with `index_mode`

This phase does **not** include:

- dependency expansion
- change cursors
- hot/churn ranking
- full evidence packs
- tree/outline/search UX commands

## Shared Resolution Rules

`refactor status`, `refactor snapshot`, and `refactor scout` should all use the
same scope resolver.

Recommended extracted package:

- `internal/refactor/scope`

Shared input:

```go
type ScopeInput struct {
    Workspace    string
    Path         string
    Language     string // auto|go|typescript|javascript|python|elixir
    IncludeTests bool
}
```

Shared resolved scope:

```go
type Scope struct {
    Workspace string   `json:"workspace"`
    RepoRoot  string   `json:"repo_root"`
    Path      string   `json:"path"`
    Absolute  string   `json:"absolute_path"`
    Mode      string   `json:"mode"`      // explicit | auto_file | auto_directory_single_language
    Language  string   `json:"language"`
    Detected  []string `json:"detected"`
    IsDir     bool     `json:"is_dir"`
}
```

Invariants:

1. `language=auto` is allowed for file targets.
2. Directory targets in `auto` mode must resolve to exactly one supported
   language.
3. Mixed-language directories must fail with the same validation behavior scout
   already uses.
4. Relative `--path` is resolved from the workspace root.
5. The output `Scope` is the canonical scope object reused across commands.

## `agentctl refactor status`

### Command

```bash
agentctl refactor status --workspace .
agentctl refactor status --workspace . --path ./internal --language go
agentctl refactor status --workspace . --path apps/praze-api/lib --language elixir
```

### Flags

| Flag | Default | Notes |
| --- | --- | --- |
| `--workspace` | current working directory | workspace root override |
| `--path` | `.` | file or directory within the workspace |
| `--language` | `auto` | same language rules as scout |
| `--include-tests` | `false` | affects scope discovery counts |

### Purpose

Return the status object that explains whether a refactor run should be treated
as:

- `index_backed`
- `parser_only`

and why.

### Backing Data

- shared `Scope`
- current git head for the workspace
- repoindex metadata via `repoindex.Store.GetMeta`
- repoindex stats via `repoindex.Store.Stats`

Recommended internal package:

- `internal/refactor/status`

### Mode Semantics

`mode = "index_backed"` only when all of the following hold:

1. repoindex store opens successfully
2. repoindex metadata is readable
3. repoindex schema version matches current schema
4. repoindex `head_sha` matches current git HEAD
5. requested scope language is covered by the built index

Otherwise:

- `mode = "parser_only"`

### Status Reason Codes

These are machine-oriented codes, not free-form strings:

- `repoindex_missing`
- `repoindex_open_failed`
- `repoindex_meta_unavailable`
- `repoindex_schema_mismatch`
- `repoindex_head_mismatch`
- `git_head_unavailable`
- `scope_language_not_indexed`
- `scope_resolution_failed`

Only the first blocking reason needs to be primary, but the response may include
multiple reasons.

### Output Shape

Command name:

- `refactor.status`

Envelope `data` shape:

```json
{
  "scope": {
    "workspace": "/repo",
    "repo_root": "/repo",
    "path": "internal",
    "absolute_path": "/repo/internal",
    "mode": "explicit",
    "language": "go",
    "detected": ["go"],
    "is_dir": true
  },
  "mode": "index_backed",
  "reasons": [],
  "git": {
    "head_sha": "abc1234",
    "available": true
  },
  "repo_index": {
    "available": true,
    "store_path": "/Users/me/.agentctl/storage/repoindex/example.db",
    "meta": {
      "repo_root": "/repo",
      "head_sha": "abc1234",
      "schema_version": 3,
      "indexed_at": "2026-04-01T12:00:00Z"
    },
    "stats": {
      "nodes_total": 12000,
      "edges_total": 24000,
      "nodes_by_kind": {
        "package": 42,
        "file": 780,
        "symbol": 11120,
        "concept": 58
      }
    },
    "languages": ["go", "typescript", "elixir"]
  }
}
```

### Notes on `languages`

Current `repoindex.IndexMeta` does not store built languages. Phase 1 should add
that explicitly so status can answer this deterministically.

Recommended extension:

```go
type IndexMeta struct {
    RepoRoot      string    `json:"repo_root"`
    HeadSHA       string    `json:"head_sha,omitempty"`
    SchemaVersion int       `json:"schema_version"`
    IndexedAt     time.Time `json:"indexed_at"`
    Languages     []string  `json:"languages,omitempty"`
}
```

The builder should persist languages from build options, not infer them later.

## `agentctl refactor snapshot`

### Command

```bash
agentctl refactor snapshot --workspace . --path ./internal --language go
agentctl refactor snapshot --workspace . --path src/api/core --language typescript
agentctl refactor snapshot --workspace . --path apps/praze-api/lib --language elixir
```

### Flags

| Flag | Default | Notes |
| --- | --- | --- |
| `--workspace` | current working directory | workspace root override |
| `--path` | `.` | file or directory within the workspace |
| `--language` | `auto` | same resolution semantics as scout |
| `--include-tests` | `false` | include test files in the scope |

### Purpose

Freeze a refactor scope into a deterministic snapshot that can later support:

- `refactor changes --since <snapshot>`
- stable hotspot evidence
- reproducible advisor runs
- artifact-backed audit trails

### Backing Data

- shared `Scope`
- `refactor status`
- deterministic file walk for the resolved scope
- symbol extraction for the resolved language
- CAS for large/full snapshot payloads
- a small local snapshot metadata store for lookup by `snapshot_id`

Recommended internal packages:

- `internal/refactor/status`
- `internal/refactor/snapshot`
- `internal/refactor/snapshotstore`

### Snapshot ID

Use a distinct prefix from session snapshots:

```text
refsnap-<unix_millis>
```

Examples:

- `refsnap-1775039485123`
- `refsnap-1775039490041`

### Snapshot Storage

Phase 1 should persist two things:

1. Full snapshot payload in CAS
2. Small metadata row in a local snapshot store

Recommended store path:

```text
~/.agentctl/storage/refactor_snapshots.db
```

Recommended metadata table:

```sql
CREATE TABLE refactor_snapshot (
    snapshot_id TEXT PRIMARY KEY,
    workspace TEXT NOT NULL,
    repo_root TEXT NOT NULL,
    path TEXT NOT NULL,
    language TEXT NOT NULL,
    include_tests INTEGER NOT NULL DEFAULT 0,
    mode TEXT NOT NULL,
    git_head_sha TEXT,
    index_head_sha TEXT,
    artifact_digest TEXT NOT NULL,
    file_count INTEGER NOT NULL DEFAULT 0,
    symbol_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
```

This is intentionally separate from repoindex so snapshots still work in
`parser_only` mode.

### Snapshot Payload

The full payload should be artifact-backed and deterministic.

Recommended artifact shape:

```json
{
  "snapshot_id": "refsnap-1775039485123",
  "created_at": "2026-04-01T12:31:25Z",
  "mode": "index_backed",
  "scope": {
    "workspace": "/repo",
    "repo_root": "/repo",
    "path": "internal",
    "absolute_path": "/repo/internal",
    "mode": "explicit",
    "language": "go",
    "detected": ["go"],
    "is_dir": true
  },
  "git": {
    "head_sha": "abc1234"
  },
  "repo_index": {
    "head_sha": "abc1234",
    "indexed_at": "2026-04-01T12:00:00Z",
    "schema_version": 3
  },
  "summary": {
    "file_count": 128,
    "symbol_count": 1462,
    "line_count": 28412
  },
  "files": [
    {
      "path": "internal/indexing/repoindex/builder.go",
      "language": "go",
      "line_count": 910,
      "hash": "sha256:...",
      "symbol_count": 21,
      "package": "go:github.com/jkatigb/agentctl/internal/indexing/repoindex"
    }
  ],
  "symbols": [
    {
      "path": "internal/indexing/repoindex/builder.go",
      "symbol_id": "rk::sym:...",
      "name": "Builder.Build",
      "kind": "function",
      "line_start": 52,
      "line_end": 157,
      "signature": "func (b *Builder) Build(ctx context.Context, opts BuildOptions) (BuildResult, error)"
    }
  ]
}
```

Ordering rules:

- files sorted by relative path
- symbols sorted by file path, then line start, then name
- language stored explicitly per file
- hashes computed over the current file content snapshot

### Output Shape

Command name:

- `refactor.snapshot`

Envelope `data` shape:

```json
{
  "snapshot_id": "refsnap-1775039485123",
  "mode": "index_backed",
  "scope": {
    "workspace": "/repo",
    "repo_root": "/repo",
    "path": "internal",
    "absolute_path": "/repo/internal",
    "mode": "explicit",
    "language": "go",
    "detected": ["go"],
    "is_dir": true
  },
  "summary": {
    "file_count": 128,
    "symbol_count": 1462,
    "line_count": 28412
  },
  "artifact": "sha256:...",
  "created_at": "2026-04-01T12:31:25Z"
}
```

The full payload should not be inlined by default. Even for smaller scopes,
prefer artifact-backed output so later commands can treat snapshots uniformly.

## Scout Integration

Phase 1 does not require scout to consume snapshots yet, but it should consume
the shared status logic.

Required Phase 1 scout output addition:

```json
{
  "summary": {
    "...": "...",
    "index_mode": "index_backed"
  }
}
```

or equivalently:

```json
{
  "index_mode": "index_backed"
}
```

Recommendation:

- add top-level `data.index_mode`
- keep `summary` focused on counts

This lets users tell whether a finding came from:

- fresh index-backed context
- parser-only fallback

without waiting for later evidence-pack work.

## CLI Placement

Recommended additions to `agentctl refactor`:

- `newRefactorStatusCommand()`
- `newRefactorSnapshotCommand()`

in [cmd/agentctl/cmd/refactor.go](../../../cmd/agentctl/cmd/refactor.go).

Implementation should reuse common helpers instead of copying scout’s current
scope logic into new command-local branches.

## Non-Goals

- exposing `refactor status` or `refactor snapshot` as standalone skills in
  Phase 1
- implementing `--since snapshot`
- enriching hotspot findings with dependency or churn data
- making snapshots incremental

## Acceptance Criteria

Phase 1 is complete when:

1. `agentctl refactor status` returns a stable machine-readable mode decision
   for the current scope.
2. `agentctl refactor snapshot` persists a deterministic artifact plus metadata
   row keyed by `snapshot_id`.
3. repoindex metadata records built languages explicitly.
4. `refactor scout` emits `index_mode`.
5. all three commands share one scope resolver.

## Recommended Next Step After Phase 1

Once this lands, the next immediate follow-up should be:

- `agentctl refactor deps`

That command is the biggest evidence upgrade per unit of complexity after
freshness and snapshots are in place.
