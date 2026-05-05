# Validation Contract — Store & Adapters

Area: `internal/storage/contextengine/` + `internal/context/contextengine/adapters/`
Mission: Unified Context Engine

---

## 1. Store Interface CRUD

### VAL-STORE-001: ContextEvent CRUD

The store must support Create, Read, Update, and List operations for `context_events`. Creating an event assigns a ULID `id`, records `workspace_id`, `session_id`, `task_id`, `kind`, `ref_type`, `ref_value`, `payload` (JSON), and `created_at`. Reading by ID returns the full event. Listing supports filters on `workspace_id`, `session_id`, `task_id`, and `kind`. Update modifies only mutable fields (`payload`, `metadata`). Deleting a context_event succeeds for existing IDs and returns `ENOTFOUND` for unknown IDs.

**Tool:** `go test`
**Evidence:** test output showing create → read → update → list → delete cycle passes with asserted field values and error codes.

### VAL-STORE-002: EvidencePack CRUD

The store must support Create, Read, List, and Delete for `evidence_packs`. Each pack has an `id`, `workspace_id`, `task_id`, `source_kind`, `source_ref`, `summary`, `artifact_digest` (CAS), `status`, and timestamps. Creating with an existing `source_ref` under the same `workspace_id` upserts (idempotent). Reading a non-existent pack returns a not-found error. Listing filters by `workspace_id` and `status`.

**Tool:** `go test`
**Evidence:** test output showing create → read → list filtered by workspace → duplicate create (idempotent) → delete cycle passes.

### VAL-STORE-003: EvidenceNode CRUD

The store must support Create, Read, List, and Delete for `evidence_nodes`. Each node has an `id`, `pack_id` (FK to `evidence_packs`), `kind`, `title`, `content_ref` (CAS digest or inline), `confidence`, `metadata` (JSON), and timestamps. Creating a node with a non-existent `pack_id` returns a foreign-key error. Listing supports filter by `pack_id`.

**Tool:** `go test`
**Evidence:** test output showing create → read → list by pack_id → FK violation on missing pack → delete passes.

### VAL-STORE-004: MemoryClaim CRUD

The store must support Create, Read, Update (status transitions), List, and Delete for `memory_claims`. Each claim has an `id`, `workspace_id`, `claim_type`, `ref_type`, `ref_value`, `status` (draft → active → retired → rejected), `justification`, `evidence_refs` (JSON array), `confidence`, and timestamps. Status transitions must follow the lifecycle: only valid transitions are accepted; invalid transitions return an error. Listing filters by `workspace_id`, `status`, and `claim_type`.

**Tool:** `go test`
**Evidence:** test output showing create → read → valid status transition (draft → active) → invalid transition (retired → active) returns error → list by status passes.

### VAL-STORE-005: ImpactEdge CRUD

The store must support Create, Read, List, and Delete for `impact_edges`. Each edge has an `id`, `workspace_id`, `from_ref_type`, `from_ref_value`, `to_ref_type`, `to_ref_value`, `impact_kind`, `strength`, `evidence` (JSON), and timestamps. Creating a duplicate edge (same from/to pair and kind) upserts. Listing supports filter by `from_ref_value` and `to_ref_value`.

**Tool:** `go test`
**Evidence:** test output showing create → read → list by from_ref → duplicate create (idempotent) → delete passes.

### VAL-STORE-006: StalenessMarker CRUD

The store must support Create, Read, Update, List, and Delete for `staleness_markers`. Each marker has an `id`, `workspace_id`, `target_type`, `target_id`, `staleness_reason`, `staleness_score` (float), `detected_at`, and `resolved_at` (nullable). Listing supports filter by `target_type`, `target_id`, and whether `resolved_at` is NULL (active markers). Updating a marker can set `resolved_at` and `staleness_score`.

**Tool:** `go test`
**Evidence:** test output showing create → read → list active (unresolved) → resolve (set resolved_at) → list no longer returns it as active → delete passes.

### VAL-STORE-007: Projection CRUD

The store must support Create, Read, List, and Delete for `projections`. Each projection has an `id`, `workspace_id`, `projection_type`, `target_ref`, `payload` (JSON), `version` (monotonically increasing), and timestamps. Creating a projection with the same `projection_type` + `target_ref` under a `workspace_id` increments `version`. Listing filters by `workspace_id` and `projection_type`.

**Tool:** `go test`
**Evidence:** test output showing create → read → re-create same key (version increments) → list by type → delete passes.

### VAL-STORE-008: RetrievalEpisode CRUD

The store must support Create, Read, List, and Delete for `retrieval_episodes`. Each episode has an `id`, `workspace_id`, `session_id`, `query`, `hit_count`, `latency_ms`, `model_used`, `weights_used` (JSON), `generated_at`, and timestamps. Listing filters by `workspace_id` and `session_id`.

**Tool:** `go test`
**Evidence:** test output showing create → read → list by session_id → delete passes.

### VAL-STORE-009: RetrievalFeedback CRUD

The store must support Create, Read, List, and Delete for `retrieval_feedback`. Each feedback entry has an `id`, `episode_id` (FK to `retrieval_episodes`), `feedback_type` (relevant, irrelevant, partial), `comment`, `user_actor`, and `created_at`. Creating with a non-existent `episode_id` returns a foreign-key error. Listing filters by `episode_id` and `feedback_type`.

**Tool:** `go test`
**Evidence:** test output showing create → read → list by episode → FK violation on missing episode → delete passes.

---

## 2. Store Indexes

### VAL-STORE-010: Workspace-scoped queries use indexes

All queries filtering by `workspace_id` on the 9 entity tables must produce query plans using an index scan (not a full table scan). Verify via `EXPLAIN QUERY PLAN` for representative queries on each table.

**Tool:** `go test`
**Evidence:** test output asserting that `EXPLAIN QUERY PLAN` output for each table's workspace-scoped query contains `USING INDEX` (or equivalent index scan indicator), not `SCAN TABLE`.

### VAL-STORE-011: Task-scoped queries use indexes

Queries filtering by `task_id` on `context_events`, `evidence_packs`, and `retrieval_episodes` must use an index. Verify via `EXPLAIN QUERY PLAN`.

**Tool:** `go test`
**Evidence:** test output asserting index usage for `task_id`-filtered queries on each relevant table.

### VAL-STORE-012: Session-scoped queries use indexes

Queries filtering by `session_id` on `context_events` and `retrieval_episodes` must use an index. Verify via `EXPLAIN QUERY PLAN`.

**Tool:** `go test`
**Evidence:** test output asserting index usage for `session_id`-filtered queries on each relevant table.

### VAL-STORE-013: Ref-type and ref-value composite index

Queries on `memory_claims` and `context_events` filtering by `(ref_type, ref_value)` must use a composite index. Verify via `EXPLAIN QUERY PLAN`.

**Tool:** `go test`
**Evidence:** test output asserting composite index usage for ref-type+ref-value queries.

### VAL-STORE-014: Claim status index

Queries on `memory_claims` filtering by `status` (e.g., listing only `active` claims) must use an index. Verify via `EXPLAIN QUERY PLAN`.

**Tool:** `go test`
**Evidence:** test output asserting `USING INDEX` on `status` column for filtered queries.

### VAL-STORE-015: Staleness target index

Queries on `staleness_markers` filtering by `(target_type, target_id)` must use a composite index. Verify via `EXPLAIN QUERY PLAN`.

**Tool:** `go test`
**Evidence:** test output asserting composite index usage for target-type+target-id queries.

### VAL-STORE-016: Impact edge from/to indexes

Queries on `impact_edges` filtering by `from_ref_value` or `to_ref_value` (independently) must use an index. Verify via `EXPLAIN QUERY PLAN`.

**Tool:** `go test`
**Evidence:** test output asserting index usage for both from-direction and to-direction lookups.

---

## 3. Append-Only Semantics

### VAL-STORE-017: ContextEvents are append-only

Inserting a `context_event` with an `id` that already exists must fail with a duplicate-key / constraint error, not silently overwrite. The store must never allow an existing event's `kind`, `ref_type`, `ref_value`, or `created_at` fields to be modified after insertion.

**Tool:** `go test`
**Evidence:** test output showing (1) insert succeeds, (2) insert with same ID returns error, (3) re-read confirms original values unchanged.

### VAL-STORE-018: RetrievalEpisodes are append-only

Inserting a `retrieval_episode` with an existing `id` must fail. Episodes record historical retrieval runs and must not be overwritten.

**Tool:** `go test`
**Evidence:** test output showing duplicate insert returns error and original episode data is preserved.

### VAL-STORE-019: RetrievalFeedback is append-only

Inserting `retrieval_feedback` with an existing `id` must fail. Feedback entries are immutable historical records.

**Tool:** `go test`
**Evidence:** test output showing duplicate insert returns error and original feedback data is preserved.

---

## 4. Adapter Conversions — Round-Trip

### VAL-STORE-020: Contextplane TopOfMind round-trip

Converting a `contextplane.TopOfMind` → canonical `ContextEvent` (or `Projection`) → back to `contextplane.TopOfMind` must preserve all non-derived fields: `workspace_id`, `objective`, `phase`, `active_task_ids`, `hard_constraints`, `blockers`, `recent_decisions` (ID + Text + Ref), `open_loops`, `next_actions`, `relevant_refs`.

**Tool:** `go test`
**Evidence:** test output showing source → canonical → source round-trip with `cmp.Equal` or field-by-field assertion; `UpdatedAt` may differ within tolerance.

### VAL-STORE-021: Contextplane Handoff round-trip

Converting a `contextplane.Handoff` → canonical `ContextEvent` → back to `Handoff` must preserve: `task_id`, `phase`, `outcome`, `summary`, `evidence_refs`, `files_touched`, `observations`, `tensions`, `next_actions`, `promotion_candidates`.

**Tool:** `go test`
**Evidence:** test output showing round-trip equality for all fields.

### VAL-STORE-022: Contextplane Observation round-trip

Converting a `contextplane.Observation` → canonical entity → back must preserve: `id`, `statement`, `confidence`, `count`, `project`, `area`, `evidence_refs`, `first_seen`, `last_seen`.

**Tool:** `go test`
**Evidence:** test output showing round-trip equality.

### VAL-STORE-023: Contextplane Tension round-trip

Converting a `contextplane.Tension` → canonical entity → back must preserve: `id`, `kind`, `statement`, `impact`, `related_refs`, `status`, `count`, `created_at`, `last_seen`.

**Tool:** `go test`
**Evidence:** test output showing round-trip equality.

### VAL-STORE-024: Contextplane MemoryProposal round-trip

Converting a `contextplane.MemoryProposal` → canonical entity → back must preserve: `id`, `dedupe_key`, `kind`, `classification`, `status`, `review_required`, `confidence`, `blast_radius`, `summary`, `source_refs`, `proposed_change`, `evaluation_status`, `apply_status`, `count`.

**Tool:** `go test`
**Evidence:** test output showing round-trip equality.

### VAL-STORE-025: Contextplane TaskPacket round-trip

Converting a `contextplane.TaskPacket` → canonical `ContextEvent` → back must preserve: `workspace_id`, `task` (all `TaskCandidate` fields), `objective`, `phase`, `hard_constraints`, `blockers`, `recent_decisions`, `next_actions`, `relevant_refs`.

**Tool:** `go test`
**Evidence:** test output showing round-trip equality.

### VAL-STORE-026: Contextplane RetrievalResult round-trip

Converting a `contextplane.RetrievalResult` → canonical `RetrievalEpisode` + evidence nodes → back must preserve: `workspace_id`, `query`, `top_of_mind` (if present), `latest_handoff` (if present), `observations`, `tensions`, `vault_hits`, `repo_motif_hits`, `weights`, `semantic_model`, `semantic_used`.

**Tool:** `go test`
**Evidence:** test output showing round-trip equality.

### VAL-STORE-027: Trajectory Event round-trip

Converting a `trajectory.Event` → canonical `ContextEvent` → back must preserve: `id`, `trajectory_id`, `ts`, `kind`, `actor`, `command`, `status`, `data_inline` (key set), `data_artifact`, `meta` (all `EventMeta` fields).

**Tool:** `go test`
**Evidence:** test output showing round-trip equality.

### VAL-STORE-028: Trajectory Trajectory round-trip

Converting a `trajectory.Trajectory` → canonical `ContextEvent` → back must preserve: `id`, `workspace_id`, `root_request_id`, `task_ids`, `epic_id`, `agent_role`, `job_id`, `trace_id`, `status`, `summary`, `artifact_digest`, `outcome` (if present), `session_id`.

**Tool:** `go test`
**Evidence:** test output showing round-trip equality.

### VAL-STORE-029: Observability Event round-trip

Converting an `observability.Event` → canonical `ContextEvent` → back must preserve: `ts`, `trace_id`, `span_id`, `parent_id`, `service`, `version`, `component`, `operation`, `command`, `subtype`, `session_id`, `agent_id`, `workspace_id`, `job_id`, `status`, `duration_ms`, `error_type`, `error_code`, `error_message`, `retriable`, `data` (key set).

**Tool:** `go test`
**Evidence:** test output showing round-trip equality.

### VAL-STORE-030: RLM NodeResult round-trip

Converting an `rlm/runtime.NodeResult` → canonical `EvidencePack` with `EvidenceNode` children → back must preserve: `status`, `summary`, `answer`, `findings` (each Finding's `id`, `summary`, `evidence_refs`, `artifact_refs`), `evidence_refs` (each `EvidenceRef`'s `kind`, `ref`, `title`, `excerpt`), `artifact_refs`, `error_code`, `error_message`, `started_at`, `completed_at`.

**Tool:** `go test`
**Evidence:** test output showing round-trip equality including nested findings and evidence refs.

---

## 5. Adapter Canonical Validation

### VAL-STORE-031: Adapters reject invalid ref types

When converting from any source type, if the adapter produces a canonical `ref_type` value that is not in the known set (e.g., `task`, `session`, `workspace`, `file`, `symbol`, `memory`, `epic`, `trajectory`, `trace`), the conversion must return a validation error, not silently produce an invalid entity.

**Tool:** `go test`
**Evidence:** test output showing adapter returns non-nil error for each invalid ref_type scenario.

### VAL-STORE-032: Adapters reject empty required fields

When converting from any source type, if required fields (e.g., `workspace_id`, `kind`, or `status` where applicable) are empty/zero, the adapter must return a validation error. The canonical store types must never be persisted with missing required fields.

**Tool:** `go test`
**Evidence:** test output showing adapter returns non-nil error for each empty-required-field case.

### VAL-STORE-033: Adapters produce valid status values

When converting to canonical entities that have status fields (e.g., `memory_claims.status`, `evidence_packs.status`), the adapter must only produce values from the known enum. Unknown status strings must cause a validation error.

**Tool:** `go test`
**Evidence:** test output showing adapter accepts known statuses and rejects unknown ones.

### VAL-STORE-034: Adapters normalize timestamps to UTC

All timestamps produced by adapters must be in UTC (`.UTC()` before storage). No local-timezone timestamps must reach the store layer.

**Tool:** `go test`
**Evidence:** test output asserting `result.CreatedAt.Location() == time.UTC` (or equivalent) for all timestamp fields across adapter outputs.

---

## 6. Import Cycle Prevention

### VAL-STORE-035: Contextengine store has no reverse imports

The `internal/storage/contextengine/` package must not import any of these packages (which would create cycles or violate the dependency direction):
- `internal/context/contextplane`
- `internal/context/companion`
- `internal/context/transcriptpipeline`
- `internal/intelligence/`
- `internal/interfaces/`
- `internal/rlm/`
- `internal/runtime/`
- `internal/v2/`

Only upstream imports allowed: `internal/storage/dbutil`, `internal/storage/sqlutil`, `internal/storage/cas`, `internal/platform/*`, stdlib, and third-party libraries.

**Tool:** `go test` (compile-time analysis using `go vet` or a dedicated test that uses `go list -json` to inspect the import graph)
**Evidence:** test output showing no banned imports in the contextengine package's transitive dependency list.

### VAL-STORE-036: Contextengine adapters have unidirectional imports

The `internal/context/contextengine/adapters/` package may import the source type packages (`contextplane`, `rlm/runtime`, `storage/trajectory`, `runtime/observability`, etc.) but those source packages must never import `internal/context/contextengine/` or `internal/storage/contextengine/`.

**Tool:** `go test` (compile-time analysis)
**Evidence:** test output confirming no reverse-import cycles exist in the adapter dependency graph.

---

## 7. CAS Integration

### VAL-STORE-037: Large payloads route through CAS

When an adapter encounters a source payload exceeding the inline threshold (64 KB), the canonical entity's content must be stored via `CASStore.Put` and the canonical entity must record only the `artifact_digest` (e.g., `sha256:<hex>`). The `summary` field must contain a truncated description (≤2 KB). Retrieving the full payload must use `CASStore.Get` with the stored digest.

**Tool:** `go test`
**Evidence:** test output showing (1) large input triggers CAS write, (2) canonical entity contains digest not inline payload, (3) `CASStore.Get` with digest returns original content.

### VAL-STORE-038: EvidenceRef.CASDigest resolves

When a canonical `EvidenceRef` has a non-empty `CASDigest` field, calling the store's resolve method must retrieve the full content from CAS. If the digest does not exist in CAS, the resolve must return a `ECACHE_MISS` or equivalent not-found error (not panic or return nil).

**Tool:** `go test`
**Evidence:** test output showing (1) store resolves valid CAS digest to content, (2) store returns error for non-existent digest.

### VAL-STORE-039: Small payloads stay inline

When an adapter encounters a source payload below the inline threshold (64 KB), the canonical entity must store the content inline (no CAS write). The `artifact_digest` field must remain empty.

**Tool:** `go test`
**Evidence:** test output showing small payload results in inline storage with empty `artifact_digest` and no CAS `Put` calls.

---

## 8. Reverse Impact Lookup

### VAL-STORE-040: Reverse impact lookup by target ref

Given a canonical ref (e.g., `{ref_type: "task", ref_value: "task-42"}`), the store must efficiently return all `ImpactEdge` records where `to_ref_type = "task"` AND `to_ref_value = "task-42"`. This must use the `to_ref_value` index (validated in VAL-STORE-016) and return results ordered by `strength` descending.

**Tool:** `go test`
**Evidence:** test output showing (1) insert N edges targeting the same ref, (2) reverse lookup returns all N edges, (3) results are ordered by `strength` DESC, (4) `EXPLAIN QUERY PLAN` confirms index usage.

### VAL-STORE-041: Forward impact lookup by source ref

Given a canonical ref (e.g., `{ref_type: "memory", ref_value: "mem-gotcha-x"}`), the store must efficiently return all `ImpactEdge` records where `from_ref_type = "memory"` AND `from_ref_value = "mem-gotcha-x"`. This must use the `from_ref_value` index and return results ordered by `strength` descending.

**Tool:** `go test`
**Evidence:** test output showing (1) insert N edges from the same source ref, (2) forward lookup returns all N edges, (3) results ordered by `strength` DESC, (4) `EXPLAIN QUERY PLAN` confirms index usage.

### VAL-STORE-042: Bidirectional impact graph traversal

Given a ref, the store must support returning both incoming edges (where the ref is the target) and outgoing edges (where the ref is the source) in a single call. This enables graph traversal from any node in the impact graph.

**Tool:** `go test`
**Evidence:** test output showing a ref with both incoming and outgoing edges, and the bidirectional query returns both sets correctly partitioned.
