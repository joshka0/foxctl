# Implementation Plan: v2 Psychological Memory Primitives — M1 (Salience Scores) + M2 (Turn Anchors)

Branch: `feat/v2-pr18`

## Problem Statement

The v2 runtime stores turns and derives four artifact types (embedding, annotation,
classification, learning), but all artifacts carry equal weight in retrieval. There is
no signal for *which* turns matter most (somatic markers), and no sub-turn granularity
linking retrieved artifacts back to specific decisions, errors, or code references in
the source text (narrative binding).

This plan introduces two additive primitives:

- **M1 — Salience scores**: deterministic per-artifact scoring that encodes
  error severity, repeated failure, todo state, user emphasis, and tool density.
  Enables salience-weighted retrieval so high-signal turns surface before routine ones.
- **M2 — Turn anchors**: deterministic regex-based slicing of turn text into typed
  anchor refs (`decision`, `error`, `gotcha`, `howto`, `code_ref`) at both turn and
  iteration granularity. Anchors are stored in a dedicated table and denormalized into
  the artifact row for zero-join transport.

Both primitives are wired into **both** the `sourceimport` batch path and the
`runtime enrichers` live worker path. All changes are purely additive — zero breaking
changes to existing tables, interfaces, or tests.

## Architecture Decision

**Shared signal engine in `core/run`** — salience computation and anchor slicing live
in `internal/v2/core/run/artifact_signals.go` as pure domain functions. Both the batch
importer and the live enricher import this package. No circular dependencies arise
because `core/run` has no upstream v2 dependencies.

**Dual persistence** — salience and anchor refs are stored as dedicated columns on
`v2_turn_artifacts` (for zero-join artifact transport) and additionally in a separate
`v2_turn_anchors` table keyed to `v2_turns` (for future anchor-centric queries without
artifact joins).

**Signals precomputed in producer** — the enricher `Producer` computes `TurnSignals`
from the loaded `TurnRecord` before enqueueing, so the `Worker`/`Enricher` closure
receives ready-made signals and does not need to re-derive them.

## Consistency Guardrails (Psych Principles -> v2 Contracts)

The psychology framing is useful, but v2 must keep strict engineering invariants.
These guardrails resolve the main conceptual tensions:

1. **Immutable evidence vs reconstructed identity views**
   - `v2_turns`, `v2_turn_iterations`, and `v2_turn_tool_calls` are immutable source-of-truth.
   - Identity/narrative outputs are derived, versioned, and replaceable views.
   - Derived claims must reference evidence anchors; they never mutate source records.

2. **Deterministic online path vs model-assisted offline synthesis**
   - Live `turn.recorded` enrichers use deterministic signal extraction (M1/M2).
   - Higher-order synthesis (episodes/narrative) runs asynchronously and is idempotent by version key.
   - Turn completion remains non-blocking even when synthesis fails.

3. **Narrative coherence without truth decay**
   - Narrative artifacts are allowed to evolve.
   - Every statement must cite one or more anchor refs (`anchor:<turn_id>:...`).
   - Uncited narrative statements are treated as invalid artifacts.

4. **Goal relevance without silent memory erasure**
   - Retrieval gating can narrow eligibility, but never deletes underlying evidence.
   - Drill-down metadata must preserve the ability to descend from summary units to turn spans.
   - If a strict gate yields no results, fallback policy must return a bounded neutral set.

5. **Live/backfill parity**
   - Source resynthesis (`sessions resynthesize-v2`) and live enrichers must produce the same signal schema.
   - Any intentional divergence must be versioned and explicitly documented.

## Design Patterns

| Pattern | Where applied | Rationale |
|---------|--------------|-----------|
| Pure domain function | `ComputeTurnSignals` in `core/run` | Both paths call identical logic; no side effects, fully testable |
| Additive migration | `MigrateSchema` ALTER TABLE | Safe for in-place DB upgrades; no data loss |
| Dual-write denormalization | `salience_score`/`turn_anchors` column + `v2_turn_anchors` table | Transport efficiency + future query flexibility |
| EnricherFunc closure | `artifact_enricher.go` factory | Keeps concrete enricher logic decoupled from worker/queue infrastructure |
| Panic-safe computation | `defer recover()` in `ComputeTurnSignals` | Regex and string scanning must never crash the enricher pipeline |

## File Changes

Ordered by implementation sequence. Each step is independently testable.

---

### Step 1: Signal engine

#### `internal/v2/core/run/artifact_signals.go` (NEW)

Pure domain computation for salience + anchors.

```go
package run

type TurnIterationSignal struct {
    IterationIndex int
    Text           string
}

type TurnSignalInput struct {
    SessionID      string
    TurnID         string
    TurnIndex      int
    Prompt         string
    FinalOutput    string
    Iterations     []TurnIterationSignal

    ToolCalls      int
    ErrorCount     int
    RepeatedErrors int // count of prior error turns in same parsed session

    TodoTotal  int
    TodoPending int
    TodoActive  int
    TodoDone    int
}

type TurnSignals struct {
    SalienceScore float64
    TurnAnchors   []string
}

// ComputeTurnSignals derives salience score and anchor refs deterministically.
// Panic-safe: defers recover; returns best-effort defaults on panic.
func ComputeTurnSignals(input TurnSignalInput) TurnSignals
```

**Salience formula** (additive, clamp to `[0.0, 1.0]`):
- `ErrorCount > 0` → `+0.3`
- `+0.2 × RepeatedErrors`
- `TodoPending > 0 || TodoActive > 0` → `-0.1`
- prompt contains emphasis phrase (`important`, `we decided`, `ship this`, case-insensitive) → `+0.2`
- `ToolCalls > 5` → `+0.1`

**Anchor ref format** (stable, deterministic):
- Turn-level: `anchor:<turn_id>:<type>:<n>`
- Iteration-level: `anchor:<turn_id>:iter:<iteration_index>:<type>:<n>`

Anchors are deduped and lexicographically sorted before return.

**Regex patterns** (compiled once at package init):
```go
// decision
`(?i)\b(decision|decide|decided|decision[- ]point|we decided|choice)\b`

// error
`(?i)\b(error|err|failed|failure|panic|timeout|timed out|exception|traceback|fatal)\b`

// gotcha
`(?i)\b(gotcha|pitfall|edge case|caveat|warning|note:|danger)\b`

// howto
`(?i)\b(how to|how-to|steps?:|step \d+|playbook|runbook|instruction|guide)\b`

// code_ref — file paths
`(?i)\b(?:[A-Za-z]:\\|/)?(?:[A-Za-z0-9._+-]+/)*[A-Za-z0-9._+-]+\.(go|ts|tsx|js|jsx|py|java|kt|rs|cpp|c|h|hpp|cs|rb|php|md|yaml|yml|json|toml|sh|bash|proto|sql)\b`

// code_ref — function/class signatures
`(?m)\bfunc\s+(?:\([^)]+\)\s*)?[A-Za-z_][A-Za-z0-9_]*\s*\(`
`(?m)\bdef\s+[A-Za-z_][A-Za-z0-9_]*\s*\(`
`(?m)\bclass\s+[A-Za-z_][A-Za-z0-9_]*\b`
`(?m)\b(?:async\s+)?function\s+[A-Za-z_][A-Za-z0-9_]*\s*\(`
```

#### `internal/v2/core/run/artifact_signals_test.go` (NEW)

- `TestComputeTurnSignals_SalienceFormula_AdditiveAndClamped` — known inputs, exact expected scores including clamping
- `TestComputeTurnSignals_AnchorExtraction_Deterministic` — fixture text with all 5 types + file path + func/class patterns; assert exact ordered refs
- `TestComputeTurnSignals_IterationAnchors` — mixed turn + iteration scope refs

---

### Step 2: Search contract

#### `internal/v2/core/run/artifact_search.go` (MODIFIED)

Add to `ArtifactSearchOptions`:
```go
SimilarityWeight float64  // default 1.0 when unset
SalienceWeight   float64  // default 0.0 (backward compatible)
MinSalience      float64  // default 0.0
```

Add to `ScoredArtifact`:
```go
SalienceScore float64  `json:"salience_score,omitempty"`
TurnAnchors   []string `json:"turn_anchors,omitempty"`
CombinedScore float64  `json:"combined_score,omitempty"`
```

Existing callers are unaffected — zero-value defaults preserve current behavior.

---

### Step 3: Schema migration

#### `internal/v2/adapters/libsql/turns/schema.go` (MODIFIED)

1. Extend `v2_turn_artifacts` DDL with two new columns:
```sql
salience_score REAL NOT NULL DEFAULT 0,
turn_anchors   TEXT NOT NULL DEFAULT '[]',
```

2. Add `ALTER TABLE` guards for in-place DB upgrades (using `dbutil.AddColumnIfNotExists`
   or `PRAGMA table_info` check before each ALTER):
```sql
ALTER TABLE v2_turn_artifacts ADD COLUMN salience_score REAL NOT NULL DEFAULT 0;
ALTER TABLE v2_turn_artifacts ADD COLUMN turn_anchors   TEXT NOT NULL DEFAULT '[]';
```

3. Add `v2_turn_anchors` table (FK to turns only, not artifacts):
```sql
CREATE TABLE IF NOT EXISTS v2_turn_anchors (
    turn_id    TEXT NOT NULL,
    anchor_ref TEXT NOT NULL,
    -- anchor_ref format:
    --   turn-level:      anchor:<turn_id>:<type>:<n>
    --   iteration-level: anchor:<turn_id>:iter:<iteration_index>:<type>:<n>
    created_at TEXT NOT NULL,
    PRIMARY KEY (turn_id, anchor_ref),
    FOREIGN KEY(turn_id) REFERENCES v2_turns(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_v2_turn_anchors_turn   ON v2_turn_anchors(turn_id);
CREATE INDEX IF NOT EXISTS idx_v2_turn_anchors_anchor ON v2_turn_anchors(anchor_ref);
```

#### `internal/v2/adapters/libsql/turns/store_test.go` (MODIFIED)

Add `TestMigrateSchema_AddsSignalColumnsAndAnchorTable`:
- run `MigrateSchema` on fresh in-memory DB
- verify `PRAGMA table_info(v2_turn_artifacts)` lists `salience_score` and `turn_anchors`
- verify `v2_turn_anchors` table exists and has FK to `v2_turns`

---

### Step 4: Persistence and search

#### `internal/v2/adapters/libsql/turns/store.go` (MODIFIED)

Locations to touch (in order):

1. **`type Artifact struct`** — add `SalienceScore float64`, `TurnAnchors []string`
2. **`func (a Artifact) Clone()`** — deep copy `TurnAnchors`
3. **`func normalizeArtifact`** — clamp salience to `[0,1]`; replace NaN/Inf with `0`; canonicalize anchors (trim, dedupe, sorted copy)
4. **`insertArtifactWithoutVector` / `insertArtifactWithVector`** — include `salience_score`, `turn_anchors` in INSERT/ON CONFLICT DO UPDATE; after artifact write, sync `v2_turn_anchors` (DELETE existing rows for `turn_id`, insert current refs)
5. **`GetArtifact`** — SELECT and scan `salience_score`, `turn_anchors`; unmarshal JSON anchors, fallback to `[]`
6. **`type artifactSimilarityCandidate`** — add `SalienceScore float64`
7. **`searchArtifactCandidatesVector` / `searchArtifactCandidatesFallback`** — add SQL predicate `COALESCE(a.salience_score, 0) >= $n`; select `a.salience_score` into candidate
8. **`normalizeArtifactSearchOptions`** — normalize `SimilarityWeight` (default 1.0), `SalienceWeight` (default 0.0), `MinSalience` (default 0.0)
9. **`loadScoredArtifacts`** — compute `CombinedScore = SimilarityWeight×similarity + SalienceWeight×salienceScore`; sort by `CombinedScore desc`, then `Similarity desc`, then existing tie-breakers; populate `ScoredArtifact.SalienceScore`, `TurnAnchors`, `CombinedScore`

Add compile-time assertion for new interface:
```go
var _ run.TurnAnchorWriter = (*Store)(nil)
```

#### `internal/v2/core/run/turn_record.go` (MODIFIED)

Add at end of file:
```go
// TurnAnchor is a typed ref pointing to a span within a turn's text.
type TurnAnchor struct {
    TurnID    string
    AnchorRef string
    CreatedAt time.Time
}

// TurnAnchorWriter persists canonical turn anchor refs.
type TurnAnchorWriter interface {
    SaveAnchors(ctx context.Context, anchors []TurnAnchor) error
}
```

#### `internal/v2/adapters/libsql/turns/store_test.go` (MODIFIED, continued)

- `TestTurnStore_SaveArtifact_PersistsSalienceAndAnchors` — roundtrip salience + anchors
- `TestTurnStore_SearchArtifactsByEmbedding_MinSalienceFilter` — assert low-salience artifacts excluded
- `TestTurnStore_SearchArtifactsByEmbedding_CombinedScoring` — verify weighted order differs from similarity-only order

---

### Step 5: Sourceimport batch path

#### `internal/v2/adapters/sourceimport/artifacts.go` (MODIFIED)

In `BuildArtifacts` loop over `parsed.Turns`:

1. Maintain `seenErrors int` counter across turns (counts prior error turns, for `RepeatedErrors`)
2. Build `run.TurnSignalInput` per turn:
   - `SessionID`, `TurnID`, `TurnIndex`, `Prompt`, `FinalOutput.Text`
   - `ToolCalls: countToolCalls(turn)`, `ErrorCount: boolToInt(turnHasError(turn))`, `RepeatedErrors: seenErrors`
   - `TodoTotal/Pending/Active/Done` from `todoStats`
   - `Iterations: []run.TurnIterationSignal` from each `IterationRecord.Message.Text`
3. Call `signals := run.ComputeTurnSignals(signalInput)`
4. After computing `seenErrors`, increment if current turn has error
5. Inject signals into **all four** artifact constructors:
   ```go
   SalienceScore: signals.SalienceScore,
   TurnAnchors:   signals.TurnAnchors,
   ```

All existing content/summary/metadata/embedding logic is unchanged.

#### `internal/v2/adapters/sourceimport/importer_test.go` (MODIFIED)

- Extend `TestBuildArtifacts_DerivesDeterministicTypes` — assert every artifact has `SalienceScore` and `TurnAnchors`
- Add regression case: turn with emphasis phrase + >5 tool calls → `SalienceScore > 0`
- Add determinism case: same `ParsedSession` produces identical signals across two calls

---

### Step 6: Live enricher path

#### `internal/v2/runtime/enrichers/queue.go` (MODIFIED)

Extend `Job` struct:
```go
Signals run.TurnSignals
```

#### `internal/v2/runtime/enrichers/producer.go` (MODIFIED)

In `handleEvent`, after loading `turn`:
1. Build `run.TurnSignalInput` from `turn` fields; set all todo fields to zero (live path)
2. Derive `job.Signals = run.ComputeTurnSignals(input)` before `p.queue.Enqueue(job)`
3. Wrap in recover; on panic attach default `run.TurnSignals{}` and continue

#### `internal/v2/runtime/enrichers/producer_test.go` (MODIFIED)

- `TestProducer_ComputesSignalsAndForwardsToJob` — assert `job.Signals` non-nil, todo fields zero, signals present for a turn with errors

---

### Step 7: Concrete enricher + wiring

#### `internal/v2/services/artifact_enricher.go` (NEW)

Factory function that returns an `enrichers.EnricherFunc` closure:
- Receives `Job` (with precomputed `Signals`)
- Derives artifacts for `job.Turn` using `sourceimport.BuildArtifacts` (or a targeted per-turn helper)
- Filters to artifacts matching `job.ArtifactType` + `job.ArtifactVersion`
- Applies signal overlay:
  ```go
  artifact.SalienceScore = job.Signals.SalienceScore
  artifact.TurnAnchors   = job.Signals.TurnAnchors
  ```
- Calls `store.SaveArtifact` for each; returns wrapped error on failure

#### `internal/v2/services/dependencies.go` (MODIFIED)

Wire the new enricher into `enrichers.NewWorker`:
```go
enricher: services.NewArtifactEnricher(turnsStore),
```

#### `internal/v2/services/artifact_enricher_test.go` (NEW)

- `TestArtifactEnricher_AppliesJobSignalsToSavedArtifacts` — fake store captures saved artifacts; verify salience/anchors match `job.Signals`; verify wrong artifact type is filtered

---

## Testing Strategy

| Test file | New/Modified | Key cases |
|-----------|-------------|-----------|
| `core/run/artifact_signals_test.go` | NEW | Salience formula (additive+clamp), anchor extraction (all 5 types + code_ref), iteration scope, determinism |
| `adapters/libsql/turns/store_test.go` | MODIFIED | Schema migration columns+table, roundtrip salience+anchors, MinSalience SQL filter, combined scoring order |
| `adapters/sourceimport/importer_test.go` | MODIFIED | All artifacts have signals, emphasis+tools → non-zero salience, deterministic re-invocation |
| `runtime/enrichers/producer_test.go` | MODIFIED | Job carries signals, live path todo fields = 0 |
| `services/artifact_enricher_test.go` | NEW | Signals applied to saved artifacts, wrong type filtered |

All existing tests must remain green (additive defaults preserve prior behavior).

## Error Handling

1. **`ComputeTurnSignals` panics** — `defer recover()`; on panic return `TurnSignals` with cheap-field salience only (errors/tools/todos booleans) and empty `TurnAnchors`. Never propagate panic to caller.

2. **Schema migration failures** — check `PRAGMA table_info` before each `ALTER TABLE`; return wrapped error with table/column context on non-existent-column errors so startup fails fast rather than silently accepting partial schema.

3. **Anchor sync in `SaveArtifact`** — if `DELETE`+`INSERT` into `v2_turn_anchors` fails after artifact row write: return error (caller retries job). Keeps failure observability intact via existing worker error event path.

4. **Live path signal derivation** — producer wraps `ComputeTurnSignals` in recover; on failure attaches `TurnSignals{}` defaults and enqueues job anyway. Actual `SaveArtifact` failures in enricher still fail and surface via `EventArtifactFailed` (unchanged).

## Implementation Order

Each step produces a green `go test ./...` checkpoint.

1. Add `run/artifact_signals.go` + `artifact_signals_test.go`. Verify unit tests pass.
2. Extend `run/artifact_search.go` options/result types. Verify compile.
3. Add `run/turn_record.go` `TurnAnchor`/`TurnAnchorWriter`. Verify compile.
4. Update `turns/schema.go` DDL + ALTER guards. Add migration tests.
5. Update `turns/store.go` persistence/search/ranking. Add store tests.
6. Update `sourceimport/artifacts.go` signal injection. Update importer tests.
7. Update `enrichers/queue.go` + `enrichers/producer.go`. Update producer tests.
8. Add `services/artifact_enricher.go`, wire in `dependencies.go`. Add enricher tests.

## Side Effects / Impact Map

- **Search ordering** — only changes when callers set `SalienceWeight > 0`; existing callers are unaffected.
- **DB size** — minor growth per artifact (2 columns + anchor rows in `v2_turn_anchors`).
- **CPU** — regex scanning is bounded per turn; patterns are compiled once at package init (no per-call compilation cost).
- **Wire compatibility** — `ScoredArtifact` new fields are `omitempty`; JSON consumers ignoring unknown fields remain compatible.

## Forward Extension (M3/M4/M5)

M1/M2 establish the base signal layer. The next psych-aligned milestones build on it:

1. **M3 — Episode layer (`v2_episodes`)**
   - Semantic chapters spanning `start_turn_id -> end_turn_id` with `topic`, `summary`, `salience`, and `is_landmark`.
   - Compiler runs asynchronously on topic-shift and completion cues.
   - Retrieval prefers episodes first, then drills into cited turn anchors.

2. **M4 — WorkingContext retrieval gate**
   - Hard filters from current task state (session/workspace/files/labels/salience floor) before vector ranking.
   - Soft rerank combines similarity + salience + label overlap.
   - Encodes Conway's "working self constrains retrieval" without mutating evidence.

3. **M5 — Narrative artifact type**
   - Session-scoped derived narrative with evidence-cited claims.
   - Rebuilt periodically/asynchronously; never blocks turn completion.
   - Supports coherent context injection while preserving auditability.

Cross-reference implementation planning:
- `docs/plans/v2-greenfield-bootstrap.md` (Wave 4 section)
- `docs/plans/v2-implementation-todo.md` (PR-25/PR-26/PR-27)
- `docs/designs/hierarchical-memory-retrieval.md`
- `docs/designs/progressive-memory-system.md`
