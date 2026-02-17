# Implementation Plan: Annotation Recall Sort, Filter & Structured Query

## Problem Statement

- `session/recall` annotation mode returns results sorted only by similarity — no chronological ordering for decision evolution tracking
- No category filtering — can't search "only decisions" or "only debug turns"
- No way to track what happened to a specific file across sessions
- No error→fix chain detection to find how bugs were resolved
- `skill.yaml` missing `annotation_granularity`, `sort_by`, `filter_category` params
- Workspace/project filtering not enforced in annotation mode (inconsistent with other modes)

## Architecture Decision

**Two-skill approach:**
- `session/recall` — semantic search extended with sort/filter primitives (annotation mode only)
- `session/query` (new) — structured, non-semantic queries: file tracking, error chains, category counts

**Why:** Semantic search (recall) and structured queries (file lookup, chain detection) have different input contracts and execution paths. Mixing them into one skill would bloat the mode-dispatch logic.

**Store primitives** live in `internal/storage/annotations/store.go` and are shared by both skills.

```
session/recall (semantic)              session/query (structured)
  ├─ annotation_granularity              ├─ file_path mode
  │   ├─ filter_category                 ├─ error_chains mode
  │   ├─ sort_by (multi-key)             └─ list_categories mode
  │   └─ workspace/project filter
  └─ existing chunk/window modes
         │                                      │
         └──────── store primitives ────────────┘
                   (annotations/store.go)
```

## Design Patterns

1. **Options struct** — `AnnotationSearchOptions` for filtered semantic search. Avoids parameter explosion.
2. **Multi-key sort** — `sort_by` as comma-separated fields (`date,similarity`). Parsed to `[]annotationSortField`, applied via `sort.Slice` with chained comparators.
3. **Exact JSON array match** — `EXISTS (SELECT 1 FROM json_each(file_paths) WHERE value = ?)` for file-path queries. No regex, no partial matching.
4. **Proximity + overlap ranking** — Error chain detection uses turn proximity (lookahead window) combined with file-path overlap scoring.

## File Changes

### `internal/storage/annotations/store.go` (modified)

**New types** near `ScoredAnnotation`:

```go
type AnnotationSearchOptions struct {
    Limit       int
    TOCCategory string   // filter by toc_category (case-insensitive)
    HasErrors   bool     // filter to annotations with non-empty errors
    SessionIDs  []string // restrict to specific sessions
}

type FileTrackingSummary struct {
    SessionID  string    `json:"session_id"`
    TurnCount  int       `json:"turn_count"`
    Categories string    `json:"categories"`
    FirstSeen  time.Time `json:"first_seen"`
    LastSeen   time.Time `json:"last_seen"`
}

type CategoryCount struct {
    Category string `json:"category"`
    Count    int    `json:"count"`
}
```

**New methods:**

1. `SearchSimilarFiltered(ctx, embedding []float32, opts AnnotationSearchOptions) ([]ScoredAnnotation, error)`
   - Replaces internal logic of `SearchSimilar` (which becomes a wrapper)
   - SQL prefilters: `embedding IS NOT NULL AND LENGTH(embedding) > 0`
   - Optional: `LOWER(toc_category) = LOWER(?)` when `TOCCategory` set
   - Optional: `COALESCE(NULLIF(TRIM(errors), ''), '[]') NOT IN ('[]', 'null', '[""]')` when `HasErrors` true
   - Optional: `session_id IN (...)` when `SessionIDs` non-empty
   - Cosine similarity scoring + sort + limit as before

2. `ListBySessionTurnRange(ctx, sessionID string, startTurnExclusive, endTurnInclusive int, category string, limit int) ([]*TurnAnnotation, error)`
   - For error chain lookahead
   - `turn_index > ? AND turn_index <= ?`, optional category filter, ordered by turn_index ASC

3. `ListByFilePath(ctx, filePath string, sessionIDs []string, limit int) ([]*TurnAnnotation, error)`
   - `EXISTS (SELECT 1 FROM json_each(file_paths) WHERE value = ?)`
   - Optional session scope, ordered by timestamp/created_at DESC

4. `SummarizeByFilePath(ctx, filePath string, sessionIDs []string) ([]FileTrackingSummary, error)`
   - Aggregate per session: COUNT, MIN/MAX timestamp, GROUP_CONCAT(DISTINCT toc_category)

5. `CountByCategory(ctx, sessionIDs []string) ([]CategoryCount, error)`
   - `GROUP BY COALESCE(NULLIF(toc_category, ''), 'context')`

**Wrap existing method:**
```go
func (s *Store) SearchSimilar(ctx context.Context, embedding []float32, limit int) ([]ScoredAnnotation, error) {
    return s.SearchSimilarFiltered(ctx, embedding, AnnotationSearchOptions{Limit: limit})
}
```

### `skills/session_recall/main.go` (modified)

**Input additions:**
```go
FilterCategory string `json:"filter_category,omitempty"`
SortBy         string `json:"sort_by,omitempty"` // comma-separated: "date,similarity"
```

**AnnotationMatch addition:**
```go
MatchedAt string `json:"matched_at,omitempty"` // RFC3339 timestamp
```

**New types and helpers:**

```go
type annotationSortField string

const (
    sortFieldSimilarity annotationSortField = "similarity"
    sortFieldDate       annotationSortField = "date"
    sortFieldRecent     annotationSortField = "recent"
)

type annotationCandidate struct {
    Match       AnnotationMatch
    SortAt      time.Time
    Similarity  float64
    SessionID   string
    WindowIndex int
    TurnIndex   int
}

func parseSortBy(sortBy, legacySort string) ([]annotationSortField, error)
func sortAnnotationCandidates(cands []annotationCandidate, fields []annotationSortField)
func annotationMatchTime(ann *annotations.TurnAnnotation) time.Time
```

**Sort semantics:**
- `similarity` — descending (highest first)
- `date` — ascending (oldest first, for evolution tracking)
- `recent` — descending (newest first)
- After explicit keys: tiebreak by SessionID, WindowIndex, TurnIndex

**Annotation branch changes:**
- Replace `annStore.SearchSimilar` with `annStore.SearchSimilarFiltered` using `FilterCategory`
- Add session cache + workspace/project filtering (same pattern as chunk/window modes)
- Build candidates with `SortAt` from `annotationMatchTime`
- Sort via `sortAnnotationCandidates`, truncate to `Limit`
- Populate `MatchedAt` field

**Validation:**
- `sort_by` / `filter_category` only valid when `annotation_granularity == true` — else `skillerr.Arg`
- If both `Sort` and `SortBy` set and conflict — `skillerr.Arg`
- Default sort: `[similarity]`

### `skills/session_recall/skill.yaml` (modified)

Add parameters:
- `annotation_granularity` (boolean) — search at annotation level
- `filter_category` (string) — filter by toc_category (e.g., "decision", "debug")
- `sort_by` (string) — comma-separated sort keys: similarity, date, recent

Add to returns:
- `annotation_matches` (array)

Add example:
```yaml
- name: "Decision evolution"
  input:
    query: "database migration strategy"
    annotation_granularity: true
    filter_category: "decision"
    sort_by: "date,similarity"
    limit: 20
  description: "Track how database migration decisions evolved over time"
```

### `skills/session_annotate/main.go` (modified)

Add timestamp capture:
```go
func extractTurnAnnotationTime(rm *claudejsonl.ReaderMessage) time.Time
```
- Parse from message metadata when available
- Zero value when unavailable (recall falls back to `created_at`)
- Set `Timestamp: extractTurnAnnotationTime(rm)` in `turnAnn` construction

### `skills/session_query/main.go` (new)

**Input:**
```go
type Input struct {
    // Mode selectors (exactly one required)
    FilePath       string `json:"file_path,omitempty"`
    ErrorChains    bool   `json:"error_chains,omitempty"`
    ListCategories bool   `json:"list_categories,omitempty"`

    // Shared filters
    SessionID string `json:"session_id,omitempty"`
    Workspace string `json:"workspace,omitempty"`
    Project   string `json:"project,omitempty"`
    Limit     int    `json:"limit,omitempty"`

    // File tracking
    Detail bool `json:"detail,omitempty"`

    // Error chains
    Query        string `json:"query,omitempty"`        // semantic seed for finding errors
    LookaheadMin int    `json:"lookahead_min,omitempty"` // default 3
    LookaheadMax int    `json:"lookahead_max,omitempty"` // default 5
    TopK         int    `json:"top_k,omitempty"`         // fixes per error, default 1
}
```

**Output:**
```go
type Output struct {
    Status  string `json:"status"`
    Message string `json:"message"`

    // File tracking
    FileTrackingSummaries   []FileTrackingSummary   `json:"file_tracking_summaries,omitempty"`
    FileTrackingAnnotations []FileTrackingAnnotation `json:"file_tracking_annotations,omitempty"`

    // Error chains
    ErrorChains []ErrorChain `json:"error_chains,omitempty"`

    // Category counts
    CategoryCounts []CategoryCount `json:"category_counts,omitempty"`
}
```

**Mode implementations:**

1. **File tracking** (`file_path` set):
   - Resolve session scope via workspace/project/session_id
   - `detail=false` → `SummarizeByFilePath` → per-session summaries
   - `detail=true` → `ListByFilePath` → annotation-level results

2. **Error chains** (`error_chains=true`, `query` required):
   - Generate query embedding
   - `SearchSimilarFiltered` with `TOCCategory="debug"` + merge with `HasErrors=true`
   - Dedupe by `(session_id, turn_index)`
   - For each error: `ListBySessionTurnRange(sessionID, turn, turn+lookaheadMax, "code_change", 5)`
   - Rank fixes by file-path overlap (exact match count), tiebreak by proximity
   - Include error turns with no fixes found (empty chain = unresolved error)

3. **Category listing** (`list_categories=true`):
   - Resolve session scope
   - `CountByCategory` → sorted descending by count

**Helpers:**
```go
func overlapScore(a, b []string) int     // count of shared elements
func rankFixes(errorAnn *annotations.TurnAnnotation, fixes []*annotations.TurnAnnotation, topK int) []ErrorFixCandidate
func resolveSessionScope(ctx, sessionStore, in) ([]string, error)
```

### `skills/session_query/skill.yaml` (new)

```yaml
apiVersion: agentctl/v1
kind: Skill
metadata:
  name: session/query
  version: 0.1.0
  description: "Structured queries on session annotations — file tracking, error chains, category counts"
  tags: ["session", "query", "annotations", "structured"]
distribution:
  type: exec
  exec:
    entry: skills/session_query/session_query
io:
  format: JSON
  inline_output_kb: 32
signature:
  command: session/query
  parameters:
    - name: file_path
      type: string
      required: false
      description: "File path to track across sessions"
    - name: detail
      type: boolean
      required: false
      description: "Return annotation-level results instead of per-session summaries"
    - name: error_chains
      type: boolean
      required: false
      description: "Find error→fix chains"
    - name: query
      type: string
      required: false
      description: "Semantic query for error chain discovery"
    - name: list_categories
      type: boolean
      required: false
      description: "List annotation category counts"
    - name: session_id
      type: string
      required: false
    - name: workspace
      type: string
      required: false
    - name: project
      type: string
      required: false
    - name: limit
      type: integer
      required: false
      description: "Max results (default: 20)"
    - name: lookahead_min
      type: integer
      required: false
      description: "Min turns ahead to look for fixes (default: 3)"
    - name: lookahead_max
      type: integer
      required: false
      description: "Max turns ahead to look for fixes (default: 5)"
    - name: top_k
      type: integer
      required: false
      description: "Max fix candidates per error (default: 1)"
```

## Testing Strategy

### Unit Tests
- `SearchSimilarFiltered`: category filter, HasErrors filter, session scope, combined filters
- `ListBySessionTurnRange`: range boundaries, category filter, empty results
- `ListByFilePath` / `SummarizeByFilePath`: exact match, multi-session, empty
- `CountByCategory`: with/without session scope
- `parseSortBy`: single key, multi-key, aliases, conflicts, invalid keys
- `sortAnnotationCandidates`: all sort orders, tiebreaks, stability

### Integration Tests
- End-to-end recall with `filter_category=decision, sort_by=date,similarity`
- File tracking summary vs detail mode
- Error chain with real annotation data — verify fix proximity and overlap ranking
- Category counts match raw SQL verification

### Edge Cases
- Zero annotations match category filter → `status: "no_matches"`
- File path not in any annotation → empty results
- Error turn at end of session → empty fix chain (included in output)
- Legacy annotations without timestamp → fallback to `created_at` for sort
- `sort_by` with all same timestamps → stable tiebreak by session/turn

## Error Handling

- `filter_category` with non-annotation mode → `skillerr.Arg`
- `sort_by` with non-annotation mode → `skillerr.Arg`
- `error_chains=true` without `query` → `skillerr.Arg`
- `session/query` with zero or multiple modes → `skillerr.Arg`
- Annotation store open failure → `skillerr.WrapIO`
- Empty embedding for error chain query → `skillerr.Arg` (requires embeddings)

## Implementation Order

```
Step 1 (store primitives) ──→ Step 2 (recall sort/filter) ──→ Step 3 (recall yaml)
                           ╲──→ Step 4 (annotate timestamp)
                           ╲──→ Step 5 (session/query skill) ──→ Step 6 (query yaml)
```

1. `internal/storage/annotations/store.go` — add types + 5 new methods + wrap SearchSimilar
   - **Checkpoint**: store compiles, existing `SearchSimilar` unchanged
2. `skills/session_recall/main.go` — sort/filter/category + workspace enforcement
   - **Checkpoint**: `annotation_granularity` with `filter_category` and `sort_by` works
3. `skills/session_recall/skill.yaml` — declare new params and returns
4. `skills/session_annotate/main.go` — timestamp capture
   - **Checkpoint**: re-annotated sessions have turn timestamps
5. `skills/session_query/main.go` — file tracking + error chains + category counts
   - **Checkpoint**: all 3 modes return correct results
6. `skills/session_query/skill.yaml` — full manifest

## Resolved Decisions

- `sort_by` direction: shorthand only — `date` = ascending, `recent` = descending. No suffix syntax.
- File-path matching: exact value match in JSON array. No partial/regex.
- Error chain empty entries: included — unresolved errors are useful diagnostic info.
- `has_error` filtering: SQL text check on `errors` field, no schema migration needed.
- `sort_by` scope: annotation_granularity mode only.
- Timestamp source: `TurnAnnotation.Timestamp` preferred, `CreatedAt` as fallback.
- `min_similarity`: hard floor applied before sort.
