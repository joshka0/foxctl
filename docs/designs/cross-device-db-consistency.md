# Cross-Device DB Consistency (LibSQL/Turso + Single-Leader Daemon)

Status: Draft (2026-02-08)

## Summary

Make agentctl's DB-backed state consistent across multiple computers for a single user by:

- Using libSQL sync or Turso as the shared source of truth (local-first embedded replicas).
- Standardizing database driver configuration across all stores (not just cache/jobs/memory).
- Enforcing a single active daemon via a DB-backed leader lease.
- Making workspace identity path-independent everywhere.

## Motivation

Today, agentctl persists state across many SQLite databases under `~/.agentctl`. This works well on one machine but breaks down across machines due to:

- Databases being strictly local by default.
- Some identity and indexing logic still depending on absolute workspace paths.
- Background daemons becoming unsafe when two machines share the same state.

## Goals

- Any machine can open the same repo and see the same DB-backed state (sessions, tasks, agents, mailbox, companion memory, knowledge, etc).
- Sync is local-first (fast reads, writes work offline, eventual sync).
- Exactly one machine runs daemon-style background processing at a time (leader).
- Workspace identity is stable across machines even if the repo path differs.
- All persisted timestamps are stored and interpreted in UTC.

## Non-Goals

- Kubernetes or multi-tenant service architecture.
- Postgres migration.
- Cross-device CAS consistency (CAS can remain local and best-effort).
- Perfect conflict resolution for concurrent multi-writer edits (we will prefer simple, debuggable semantics first).

## Current State (Relevant)

- There is a `dbdriver` abstraction (`internal/storage/dbdriver/`) supporting `sqlite`, `libsql`, and `turso` drivers.
- `dbdriver.ConfigLoader` has named loader methods for `CACHE`, `JOBS`, `MEMORY`, plus a generic `LoadConfig(storeName, defaultPath)` that can load config for arbitrary stores via env vars. It also has a generic `ConfigFromPlatformSettings()` that can handle arbitrary store names.
- `sqliteutil.OpenDBWithAutoConfig` was intended to bridge stores to `dbdriver` for those 3 stores, but it had **zero callsites** in production code (dead code) and has been removed.
- Stores now open databases through `dbutil.OpenStoreDB` (or store-specific factory patterns), enabling `dbdriver` configuration across the codebase. Direct `sqliteutil.OpenDBShared` usage is now isolated to `dbutil` internals and tests/tools.
- `AGENTCTL_DB_DRIVER` (global fallback) is supported by `ConfigLoader` and now applies broadly because stores open via `dbutil` (unless a store opts into a dedicated factory).
- `dbutil` (`internal/storage/dbutil/`) is now the generic DB facade: opening DB handles via `dbdriver` + retaining SQLite shared-handle behavior, in addition to scan/timestamp helpers (`ScanTimestamps`, `IsNoRows`, etc.).
- Workspace identity uses `workspace.CanonicalID` which prefers git-remote-based `RepoIdentity` (SHA256 of normalized remote URL, 32 hex chars). When no git remotes exist, it falls back to `PathIdentity` (`ws-` + 16 hex chars of SHA256(absolute path)), which is machine-dependent.
- The sessions store uniquely has a dual-column scheme (`workspace_id` + `workspace_path`) with SQL-level fallback: `WHERE (workspace_id = ? OR (workspace_id = '' AND workspace_path = ?))`. Most other workspace-scoped stores use a single `workspace`/`workspace_id` column; a subset implement `workspace_repair.go` to backfill legacy rows.
- Five stores have `workspace_repair.go` files: sessions, memory, tasks, trajectory, graph.
- Daemon processing is at-least-once; deduplication is single-machine only (`daemon_dedupe.db`). When multiple daemons run on different machines sharing the same DB, duplicate work is likely.

## Proposed Architecture

### 1. Local-First Replicas Everywhere

Each store continues to use a local file under `~/.agentctl` as its primary read path, with optional sync:

- `sqlite`: local-only
- `libsql`: local libSQL file, optionally with `SyncURL` + `SyncToken`
- `turso`: embedded replica that syncs with Turso

Important invariant:

- Remote sync must use a persistent replica file path by default (not a temp dir), otherwise restarts are slow and state is fragile.

### 2. Standardized Per-Store DB Configuration

Extend configuration so every store can be configured via the same env/key patterns.

Implementation status:

- `ConfigLoader` supports `AGENTCTL_DB_DRIVER` as a global fallback.
- `ConfigLoader` supports `LoadConfig(storeName, defaultPath)` for arbitrary stores.
- Most stores still bypass `ConfigLoader` by calling `sqliteutil.OpenDBShared` directly (Phase 1/2 migrates these callsites).

Proposed env conventions:

- `AGENTCTL_DB_DRIVER`: applies to all stores unless overridden (`sqlite`, `libsql`, `turso`). Implemented as a `ConfigLoader` fallback (callsites still need migration).
- `AGENTCTL_<STORE>_DB_DRIVER`: per-store driver override (`sqlite`, `libsql`, `turso`).
- `AGENTCTL_<STORE>_DB_PATH`: local replica file path (SQLite path, libSQL path, or Turso embedded replica path).
- `AGENTCTL_<STORE>_SYNC_URL`: libSQL sync URL (enables embedded replica sync mode).
- `AGENTCTL_<STORE>_SYNC_TOKEN`: libSQL sync auth token.
- `AGENTCTL_LIBSQL_SYNC_URL`: fallback libSQL sync URL for all stores.
- `AGENTCTL_LIBSQL_SYNC_TOKEN`: fallback libSQL sync token for all stores.
- `AGENTCTL_<STORE>_DB_URL`: Turso database URL.
- `AGENTCTL_<STORE>_DB_TOKEN`: Turso auth token.
- `AGENTCTL_TURSO_URL`: fallback Turso URL for all stores.
- `AGENTCTL_TURSO_TOKEN`: fallback Turso token for all stores.

Notes:

- `<STORE>` should be a stable, uppercase logical name matching the DB file. See the full canonical store list in section 2.2 below.
- Default file paths should remain under `~/.agentctl` but use distinct extensions (`.db`, `.libsql`) to avoid accidental reuse.

### 2.1 Multi-DB vs Single DB (Direction)

Near-term direction:

- Prefer multiple databases (one per store) for local SQLite/libSQL.
- Rationale: corruption and migration issues have a smaller blast radius; stores remain independently recoverable.

Long-term direction:

- Move toward a single logical DB once the schema/migration story is stable and we have strong backup/restore primitives.
- This consolidation should be treated as a deliberate refactor (not required to achieve cross-device consistency).

### 2.2 Canonical Store List

Every `.db` file managed by agentctl must appear in this list. Stores are classified as **sync** (should replicate across devices), **local** (safe to remain device-local), or **external** (read-only, not agentctl-owned).

**Sync-critical stores** (Phase 2 MVP):

| Store Name | DB File | Package | Notes |
|---|---|---|---|
| `COORDINATION` | `coordination.db` | `internal/storage/coordination/` | Implemented: daemon leader lease + coordination |
| `SESSIONS` | `sessions.db` | `internal/storage/sessions/` | Session history |
| `TASKS` | `tasks.db` | `internal/storage/tasks/` | Task continuity across devices |
| `MAILBOX` | `mailbox.db` | `internal/storage/mailbox/` | Agent messages |
| `AGENTS` | `agents.db` | `internal/storage/agents/` | Agent registry; also used by the actor system registry (`actor_registry` via `internal/actor/registry_store.go`) |
| `MEMORY` | `memory.db` | `internal/storage/memory/` | Semantic memory + indexer state. Has `factory.go` (done) |
| `COMPANION` | `companion.db` | `internal/companion/` | Companion conversation memory (turns + summaries + distilled history) |
| `CONTEXTVAR` | `contextvar.db` | `internal/storage/contextvar/` | RLM context store |

**Sync-useful stores** (evaluate for Phase 2 or later):

| Store Name | DB File | Package | Notes |
|---|---|---|---|
| `KNOWLEDGE` | `knowledge.db` | `internal/storage/knowledge/` | Extracted knowledge |
| `TEAMS` | `teams.db` | `internal/storage/teams/` | Team definitions |
| `TRAJECTORY` | `trajectory.db` | `internal/storage/trajectory/` | Execution traces (useful for cross-device analytics) |

**Local-only stores** (no sync needed):

| Store Name | DB File | Package | Notes |
|---|---|---|---|
| `BLACKBOARD` | `blackboard.db` | `internal/storage/blackboard/` | Local workspace coordination |
| `BOARD` | `board.db` | `internal/storage/blackboard/` | Board state (local coordination) |
| `CACHE` | `cache.db` | `internal/storage/cache/` | Ephemeral speedup |
| `JOBS` | `jobs.db` | `internal/storage/jobs/persist/` | Job queue state (local execution) |
| `QUOTAS` | `quotas.db` | `internal/storage/quotas/` | Rate limiting (device-local) |
| `TESTWATCH` | `test_watch.db` | `internal/storage/testwatch/` | Test watching state |
| `CONTEXTBUFFER` | `contextbuffer.db` | `internal/storage/contextbuffer/` | Context buffer |
| `GRAPH` | `graph.db` | `internal/storage/graph/` | Code relationship graph (rebuilt per-machine) |
| `EMBEDDING_QUEUE` | `embedding_queue.db` | `internal/indexing/embedding/` | Embedding job queue |
| `SUMMARY_QUEUE` | `summary_queue.db` | `internal/sessionkit/summary/` | Session summary job queue |
| `DAEMON_DEDUPE` | `daemon_dedupe.db` | `internal/agent/daemon/` | Message deduplication |
| `PATTERNS` | `patterns.db` | `internal/agent/optimization/` | Agent optimization patterns |
| `POST_REVIEW` | `post_review_events.db` | `internal/indexing/postreview/` | Post-review event tracking |
| `REPOINDEX` | `repoindex/<key>.db` | `internal/indexing/repoindex/` | Per-repo code index (dynamic filename -- see note) |
| `CAS` | `cas.db` | `internal/storage/cas/` | CAS metadata. Has `factory.go` (done) |

**Observability** (different storage root):

| Store Name | DB File | Package | Notes |
|---|---|---|---|
| `EVENTS` | `events.db` | `internal/observability/` | Stored under `$AGENTCTL_OBS_DIR`, not `~/.agentctl/storage/` |

**External** (read-only, not agentctl-owned):

| Store Name | DB File | Package | Notes |
|---|---|---|---|
| — | `opencode.db` | `internal/storage/plans/` | OpenCode session/todo import |

**Note on `REPOINDEX`:** This store creates databases with dynamic names (`repoindex/<workspace-hash>.db`), one per indexed workspace. The `AGENTCTL_<STORE>_DB_PATH` env var model does not apply. Implemented: directory-based config (`AGENTCTL_REPOINDEX_DB_DIR`) to relocate the repoindex directory.

**Note on shared DB files:** Some `.db` files are used by multiple packages and may contain additional tables created by those packages. Examples:

- `agents.db`: `actor_registry` is created by `internal/actor/registry_store.go` (actor system registry).
- `mailbox.db`: `mailbox_notify` is created by `internal/actor/watcher.go` when the watcher is enabled.

> **Cross-reference:** See also `docs/designs/store-migration-plan.md` for the `sqliteutil.OpenDB` -> `dbdriver` migration tiers. Where tier classifications differ, this document takes precedence.

### 3. Workspace Identity Rules (Path-Independent)

Rule: Use canonical workspace IDs for all joins and lookups.

- Store `workspace_id` as the primary workspace key everywhere (canonical git-based identity via `workspace.RepoIdentity`).
- Store absolute `workspace_path` only as metadata for UX and debugging.
- Any "active session" identity file fallback should be keyed by `workspace_id`, not by hashing an absolute path.

Current state detail:

- `workspace.CanonicalID` prefers `RepoIdentity` (git-remote-based, stable across machines) but falls back to `PathIdentity` (absolute-path hash, machine-dependent) when no git remotes exist. The `PathIdentity` fallback produces different IDs for the same repo on different machines, breaking cross-device consistency.
- The sessions store has a unique dual-column fallback (`workspace_id OR workspace_path`) at the SQL level for legacy compatibility. Other stores rely on `workspace_repair.go` at open time.
- Five stores have `workspace_repair.go`: sessions, memory, tasks, trajectory, graph. Other workspace-scoped tables without repair logic should be audited (for example: `teams.workspace_id`, `board_messages.workspace_id`, `test_status.workspace_id`, `context_entries.workspace_id`).

### 4. Single-Leader Daemon (DB Lease)

When multiple machines share DB state, only one should run background loops.

Design:

- Add a small `daemon_leases` table to a dedicated coordination database.
- The daemon attempts to acquire a lease named `agent_daemon` using an atomic `INSERT ... ON CONFLICT DO UPDATE ... WHERE expires_at <= now OR owner_id = self`.
- The leader renews periodically; if renewal fails, it transitions to follower mode and stops processing.

Lease database:

- Prefer a dedicated `coordination.db` (or `daemon.db`) rather than hosting the lease in `MAILBOX`.
  - Rationale: this keeps leader election independent of mailbox schema/churn and allows gating other daemon subsystems that may not need the mailbox store. (Opening the mailbox DB is safe, so this is not strictly a "circular dependency"; it's a separation-of-concerns choice.)

Lease ownership:

- Introduce a stable per-machine `device_id` stored locally (for example `~/.agentctl/device.json`).
- Lease owner uses `device_id` plus a per-process suffix for debugging (`device_id:pid:ulid`).

Daemon behavior:

- Leader: runs background work.
- Follower: does not process background work, but can remain running and periodically retry acquisition.
- CLI should have an escape hatch for debugging (`--leader=force`), but default is safe (`--leader=auto`).

Leader-only subsystems (from `internal/daemon/service.go` and `internal/agent/daemon/`):

- **Agent Runtime + Overseer** — spawned agent management and `actor:system:overseer` polling
- **Mailbox Poll Loop** — per-agent mailbox polling and message processing
- **Companion Compression Daemon** — daily (L0->L1) and weekly (L1->L2) memory compression
- **Summary Worker** — background session summarization
- **Context Updater** — proactive context surfacing
- **File Summary Worker** — LLM-generated file summaries

## Rollout Plan (Phased)

### Phase 0: Document and Normalize

- Define the canonical store list and `<STORE>` names (see section 2.2 above).
- Add a single place in code (e.g., `internal/storage/registry.go`) mapping store name to default filename and sync classification.
- Implement `device_id` creation and retrieval.
- (Done) Remove dead code: `sqliteutil.OpenDBWithAutoConfig` (zero callsites).

Acceptance:

- Running `agentctl doctor` prints device id, workspace id, and DB driver summaries per store.

### Phase 1: Make Every Store Configurable (dbdriver)

- (Done) `dbdriver.ConfigLoader` can load config for arbitrary `<STORE>` names via `LoadConfig(storeName, defaultPath)` and supports the `AGENTCTL_DB_DRIVER` global fallback. Remaining work: migrate callsites to use it via the `dbutil` facade.
- Extend the existing `dbutil` package (`internal/storage/dbutil/`) from its current scan-only role into the generic adapter layer. `dbutil` should own:
  - Opening DB handles using the `dbdriver` config system (env + platform config), and returning `(db, closeFn)` so callers reliably release driver resources.
  - Baseline DB setup and migrations (PRAGMAs/foreign_keys/WAL, schema migration invocation, and "run once" semantics where needed).
  - SQLite pooling/shared-handle behavior (today in `sqliteutil`), but behind a driver-agnostic API.
- Treat `sqliteutil` as a legacy implementation detail and gradually move callsites away from `sqliteutil.*`.
- Update all stores that open DBs to use `dbutil` so they can select sqlite/libsql/turso.
- Ensure every store returns a closer and cleans up driver resources (connector/temp dirs) correctly.
- **While touching each store's `Open()`**, also audit and fix workspace identity:
  - Ensure the store uses `workspace_id` (canonical ID) as the sole join/lookup key.
  - Add `workspace_repair.go` to workspace-scoped stores that have legacy path-based rows or otherwise need backfill (audit first; likely candidates include `teams`, `board`, and any embedded tables that still store raw paths).
  - Demote `workspace_path` to display-only metadata where it exists.

Acceptance:

- A single env var change can route a store from SQLite to libSQL sync without code changes.
- New code uses `dbutil` (not `sqliteutil`) as the opening/migration facade.
- All workspace-scoped queries use `workspace_id` as the primary filter.

### Phase 2: Enable Cross-Device Sync for Persistent Stores

- Pick a default set of persistent stores that should sync across machines.
- Recommended initial synced stores (MVP):
  - `MEMORY` (semantic memory + indexer state)
  - `SESSIONS`
  - `TASKS`
  - `MAILBOX`
  - `AGENTS`
  - `CONTEXTVAR` (RLM context store)
  - `COMPANION` (companion conversation memory)
- Bootstrap flow for first-time remote setup:
  1. Create remote databases.
  2. Migrate existing local SQLite contents to remote in a controlled, explicit way.
  3. Switch local config to use embedded replicas with persistent replica paths.

Sync frequency / latency:

- Default (proposed): auto-sync on write with periodic background sync (configurable interval, e.g., 30s).
- Implementation note: libSQL and Turso drivers can run an optional background sync loop when `SyncInterval > 0` (and for libSQL, when `SyncURL` is configured). `agentctl sync` provides an explicit on-demand sync for configured stores.
- Provide `agentctl sync` as an explicit manual command for on-demand sync.
- Expected cross-device lag: seconds in normal operation, minutes if one device is offline.

Acceptance:

- Machine A writes sessions/tasks/companion turns; machine B sees them after sync.

### Phase 3: Leader Lease and Daemon Safety

- Implement `coordination.db` with `daemon_leases` table and a small helper (`Acquire`, `Renew`, `Release`).
- Gate daemon loops on lease ownership.

Acceptance:

- Two machines can run `agentctl daemon` concurrently and only one processes mailbox messages.

### Phase 4: Cleanup and Hardening

- Remove dual-column `workspace_path` fallback from sessions store (once all legacy rows are repaired).
- Remove other dead bridging code.
- (Done) Consolidate partial store registries (`knownDatabases` in viewer, `dbFiles` in web API) into the single registry from Phase 0.

Acceptance:

- Same repo cloned to different paths on different machines produces one logical workspace in all DB-backed features.
- No store opens a DB via `sqliteutil.OpenDB` or `sqliteutil.OpenDBShared` directly.

## Conflict Semantics (Multi-Writer)

Assumption: single user, occasional concurrent usage.

Baseline policy:

- Prefer append-only rows with ULID/UUID primary keys for new entities.
- For mutable entities, rely on `updated_at` UTC and last-write-wins.
- Add optional `updated_by_device_id` fields for debugging, where helpful.

If needed later:

- Add optimistic concurrency using a `version` integer on the most conflict-prone records (tasks, agent configs).

## Risks and Mitigations

- CGO requirement for libSQL/Turso. Mitigation: keep `sqlite` fallback always available; sync is an opt-in capability.
- Initial data migration to remote. Mitigation: make migration explicit and reversible; do not auto-overwrite local DBs.
- Duplicate work without leader gating. Mitigation: lease gate all daemon loops; keep at-least-once invariants, add dedupe where possible.
- Workspace identity drift during migration. Mitigation: Phase 1 adds repair logic to all workspace-scoped stores before Phase 2 enables sync. Run `agentctl doctor` to verify workspace IDs are consistent.

## Open Questions

- Which stores should move from "sync-useful" to "sync" in Phase 2 MVP? (`KNOWLEDGE`, `TEAMS`, `TRAJECTORY`)
- Should `BLACKBOARD`/`BOARD` sync? They are currently used for local workspace coordination, but cross-device agent collaboration may need them. (See `store-migration-plan.md` which classifies them as local-only.)
- Consolidation: when (if ever) do we merge stores into a single DB, and what is the migration/rollback plan for that step?
- Should `REPOINDEX` dynamic databases get driver config support, or remain SQLite-only?
