# Validation Contract — Retrieval Lane Unification & RLM Hard-Cut

Validation assertions for the Unified Context Engine mission covering the
retrieval-lane service layer and the RLM tool-surface hard-cut.

---

## Retrieval Lane Unification

Four lane services each return a canonical `EvidencePack`. A mixed-lane service
fans out to all four and fuses results by typed refs.

---

### VAL-RLM-001: Code lane wraps code_search_ensemble into EvidencePack

`retrieve_code` delegates to the existing `code_search_ensemble` tool and
returns an `EvidencePack` with `lane: "code"`.

When called with a valid workspace and query, the code lane service:
1. Invokes `code_search_ensemble` with the query and workspace parameters.
2. Normalizes the raw tool result into an `EvidencePack` containing typed
   evidence refs (file paths, symbol IDs, snippet anchors).
3. Sets the `Lane` field to `"code"` on every evidence entry.
4. Records a `RetrievalEpisode` with `lane`, `query`, `duration_ms`,
   `result_count`, and `source_tool`.

**Pass:** The returned `EvidencePack.Lane == "code"` and the
`RetrievalEpisode` is persisted with the correct fields.
**Fail:** Lane field is empty, wrong, or `RetrievalEpisode` is not recorded.

Tool: `go test`
Evidence: test output showing `EvidencePack.Lane == "code"` and
`RetrievalEpisode` field assertions pass.

---

### VAL-RLM-002: Memory lane returns EvidencePack from direct retrieval

`retrieve_memory` performs direct memory/session retrieval and returns an
`EvidencePack` with `lane: "memory"`.

When called with a memory query:
1. Invokes the memory/session retrieval backend (facts, timeline, session
   recall).
2. Returns an `EvidencePack` where each entry carries typed refs
   (`memory_ref`, `session_ref`, `fact_id`).
3. Sets `Lane` to `"memory"`.
4. Records a `RetrievalEpisode`.

**Pass:** `EvidencePack.Lane == "memory"`, entries contain memory-typed refs,
and `RetrievalEpisode` is recorded.
**Fail:** Lane is wrong, entries are untyped, or no episode recorded.

Tool: `go test`
Evidence: test output showing lane, ref type, and episode assertions pass.

---

### VAL-RLM-003: Context lane returns EvidencePack from TopOfMind + handoffs + ContextWiki + vault

`retrieve_context` assembles context from TopOfMind, handoffs, ContextWiki bundle, and
vault knowledge and returns an `EvidencePack` with `lane: "context"`.

When called:
1. Loads TopOfMind bundle and latest handoff from the ContextWiki.
2. Queries the vault knowledge plane for related notes.
3. Returns an `EvidencePack` with entries typed as `contextwiki_ref`, `handoff_ref`,
   `vault_ref`, `tom_ref`.
4. Sets `Lane` to `"context"`.
5. Records a `RetrievalEpisode`.

**Pass:** `EvidencePack.Lane == "context"`, contains at least one entry sourced
from ContextWiki/vault/handoff/TopOfMind, and `RetrievalEpisode` is recorded.
**Fail:** Lane is wrong, no ContextWiki-derived entries, or no episode recorded.

Tool: `go test`
Evidence: test output showing lane, entry source types, and episode assertions
pass.

---

### VAL-RLM-004: Task lane returns EvidencePack from TaskStore + taskhistory + TaskContext

`retrieve_task` queries the task store, task history, and task context
subsystems and returns an `EvidencePack` with `lane: "task"`.

When called:
1. Queries `TaskStore` for active/recent tasks matching the query scope.
2. Reads `taskhistory` summaries via the context-plane task-history renderer.
3. Loads `TaskContext` entries for the matching tasks.
4. Returns an `EvidencePack` with entries typed as `task_ref`,
   `taskhistory_ref`, `taskcontext_ref`.
5. Sets `Lane` to `"task"`.
6. Records a `RetrievalEpisode`.

**Pass:** `EvidencePack.Lane == "task"`, entries carry task-typed refs, and
`RetrievalEpisode` is recorded.
**Fail:** Lane is wrong, entries are untyped, or no episode recorded.

Tool: `go test`
Evidence: test output showing lane, ref type, and episode assertions pass.

---

### VAL-RLM-005: Mixed lane fans out to all four lanes and fuses by typed refs

`retrieve_mixed` dispatches to `retrieve_code`, `retrieve_memory`,
`retrieve_context`, and `retrieve_task` concurrently, then fuses the results
by typed evidence refs — never by keyword/substring matching.

When called:
1. Invokes all four lane services in parallel (bounded concurrency).
2. Each lane returns an `EvidencePack` with its own `Lane` value.
3. The mixed lane fuses packs by **typed ref identity** (e.g., matching file
   paths, symbol IDs, memory refs, task refs) — not by keyword/substring
   overlap.
4. The fused `EvidencePack` has `Lane: "mixed"` and preserves per-entry
   provenance showing which original lane contributed each entry.
5. Records one `RetrievalEpisode` for the mixed call with `sub_episodes`
   referencing each child lane episode.

**Pass:** The fused pack has entries from ≥2 lanes, provenance is preserved,
no keyword/substring matching is used during fusion, and all episodes are
recorded.
**Fail:** Fusion uses string matching, provenance is lost, or child episodes
are missing.

Tool: `go test`
Evidence: test output showing fused pack provenance, ref-based fusion, and
episode recording assertions pass.

---

### VAL-RLM-006: Each lane records a RetrievalEpisode on every call

Every lane service (`retrieve_code`, `retrieve_memory`, `retrieve_context`,
`retrieve_task`, `retrieve_mixed`) records exactly one `RetrievalEpisode` per
invocation.

The episode contains:
- `lane`: the lane identifier (`"code"`, `"memory"`, `"context"`, `"task"`,
  `"mixed"`)
- `query`: the input query string
- `duration_ms`: wall-clock duration of the retrieval call
- `result_count`: number of evidence entries in the returned pack
- `source_tool`: the primary tool or service that produced the result
- `timestamp`: RFC3339 UTC timestamp

For `retrieve_mixed`, the episode also carries `sub_episodes` listing child
lane episode IDs.

**Pass:** Every lane call produces exactly one episode with all required
fields populated and non-zero.
**Fail:** Episode is missing, fields are zero-valued, or mixed lane lacks
`sub_episodes`.

Tool: `go test`
Evidence: test output showing episode field assertions pass for all five lanes.

---

### VAL-RLM-007: Lane services reject empty queries with a validation error

All five lane services return a structured error when called with an empty or
whitespace-only query string. The error must be a typed validation error, not
a panic or nil-result.

**Pass:** Each lane returns a non-nil error for `""` and `"  "` inputs; the
error type is a validation error (not a nil `EvidencePack` and nil error).
**Fail:** Any lane returns nil error for empty input, or panics.

Tool: `go test`
Evidence: test output showing error assertions pass for empty/whitespace
queries on all five lanes.

---

### VAL-RLM-008: EvidencePack entries carry typed refs — no untyped map[string]any in domain

`EvidencePack` entries use typed reference structs (`CodeRef`, `MemoryRef`,
`ContextRef`, `TaskRef`) rather than untyped `map[string]any` for ref fields.

The `EvidencePack` struct and its entry types are defined in a domain package
that does not import `os`, `database/sql`, or adapter packages.

**Pass:** EvidencePack and entry types are typed structs in a domain-level
package with no IO imports. Ref fields are typed, not `map[string]any`.
**Fail:** Any entry ref is `map[string]any` or the domain package imports IO.

Tool: `go test`
Evidence: test output showing struct field assertions and import-allowlist
check pass.

---

## RLM Hard-Cut

The default tool surface shrinks from ~18 atomic tools to 6 composite tools.
Old atomic tools are relegated to controller/staged-only profiles. Keyword
routing is replaced with typed `QueryPlan` + structured classifier.

---

### VAL-RLM-009: DefaultTools returns exactly the six composite tools

After the hard-cut, `DefaultTools()` returns exactly these six tools:

1. `retrieve_code`
2. `retrieve_memory`
3. `retrieve_context`
4. `retrieve_task`
5. `retrieve_mixed`
6. `load_evidence_ref`

No other tools appear in the default profile. All six tools have valid JSON
parameter schemas with `"type": "object"` and non-nil `"properties"`.

**Pass:** `len(DefaultTools()) == 6` and the tool names match the list above
(in any order). Each tool has a valid parameter schema.
**Fail:** More or fewer than 6 tools, or any tool name is not in the required
set.

Tool: `go test`
Evidence: test output showing tool count, name set, and schema validation pass.

---

### VAL-RLM-010: Old atomic tools are absent from DefaultTools but present in controller/staged profiles

The following atomic tools must NOT appear in `DefaultTools()`:
- `semantic_search_code`
- `smart_search_code`
- `ripgrep_code`
- `search_repo`
- `expand_repo_graph`
- `load_file`
- `search_vault`
- `read_note`
- `memory_ensemble_retrieve`
- `code_search_ensemble`
- `search_scenes`
- `get_scene`
- `search_artifacts`
- `load_artifact`
- `get_top_of_mind`
- `get_latest_handoff`
- `subcall`

These tools must remain available in the **staged** plan mode's phase
toolsets and in controller-level tool profiles (e.g., `code-intel` still
exposes `code_search_ensemble` for backward compatibility).

**Pass:** None of the listed atomic tools appear in `DefaultTools()`. The
`code-intel` profile still includes `code_search_ensemble`. Staged plan
phases reference the atomic tools.
**Fail:** Any atomic tool appears in `DefaultTools()`, or staged/controller
profiles lose the atomic tools they need.

Tool: `go test`
Evidence: test output showing DefaultTools exclusion and staged/profile
inclusion assertions pass.

---

### VAL-RLM-011: code-intel profile maps to retrieve_code

The `code-intel` tool profile resolves to a tool set containing
`retrieve_code` (plus `load_evidence_ref`).

When `ResolveToolPolicy` is called with `profile: "code-intel"`, the returned
`ToolPolicy.Tools` must include `retrieve_code`.

**Pass:** `code-intel` profile contains `retrieve_code` in its allowed tools.
**Fail:** `code-intel` profile does not contain `retrieve_code`.

Tool: `go test`
Evidence: test output showing profile resolution and tool name assertion pass.

---

### VAL-RLM-012: memory-recall profile maps to retrieve_memory and retrieve_context

The `memory-recall` tool profile resolves to a tool set containing
`retrieve_memory` and `retrieve_context` (plus `load_evidence_ref`).

When `ResolveToolPolicy` is called with `profile: "memory-recall"`, the
returned `ToolPolicy.Tools` must include both `retrieve_memory` and
`retrieve_context`.

**Pass:** `memory-recall` profile contains both `retrieve_memory` and
`retrieve_context`.
**Fail:** Profile is missing either tool.

Tool: `go test`
Evidence: test output showing profile resolution and tool name assertions pass.

---

### VAL-RLM-013: ClassifyRouteProfile keyword routing is removed

`ClassifyRouteProfile` must NOT use keyword/substring matching (e.g.,
`containsAny(query, "thread", "scene", ...)`) to determine the route profile.

After the hard-cut, `ClassifyRouteProfile` either:
- Delegates to a typed `QueryPlan` + structured classifier, OR
- Returns a deterministic default (e.g., always `RouteProfileMixed` or
  `RouteProfileCodeRetrieval`), OR
- Is removed entirely with routing done by the structured classifier.

The existing keyword-based implementation (`containsAny` calls with hardcoded
strings like `"code"`, `"repo"`, `"thread"`, `"session"`) must no longer
exist.

**Pass:** The `containsAny`-based routing logic in `ClassifyRouteProfile` is
removed. Route resolution uses typed fields or a structured classifier.
**Fail:** `ClassifyRouteProfile` still contains `containsAny` or
`strings.Contains` keyword branching.

Tool: `go test`
Evidence: test output confirming:
1. `ClassifyRouteProfile` does not branch on hardcoded keyword substrings.
2. Route resolution produces deterministic results from structured input.

---

### VAL-RLM-014: Route resolution uses typed QueryPlan with structured classifier

Route profile selection is driven by a typed `QueryPlan` (from
`retrievalv2.QueryPlan` or equivalent) fed into a structured classifier —
not by raw prompt string inspection.

The structured classifier may use:
- Identifier types (`symbol`, `path`, `qualified`)
- Phrase extraction
- Path hints
- Explicit task-type metadata

It must NOT use ad hoc substring matching on the raw prompt.

**Pass:** The classifier accepts a `QueryPlan` (or equivalent typed struct)
and returns a `RouteProfile`. No `strings.Contains` on the raw prompt string
drives routing decisions.
**Fail:** Routing branches on raw prompt substrings.

Tool: `go test`
Evidence: test output showing classifier receives typed input and produces
correct profiles without keyword branching.

---

### VAL-RLM-015: LambdaRunner operates over EvidencePacks

`LambdaRunner` leaf execution calls retrieval lane services that return
`EvidencePack` values — not raw query variants against the old atomic tool
surface.

After the hard-cut, `LambdaRunner`:
1. Selects the lane based on `TaskType` (e.g., `TaskTypeCodeLocate` →
   `retrieve_code`).
2. Each leaf call returns an `EvidencePack`.
3. Composition (`ComposeOp`) merges `EvidencePack` values, not raw strings or
   untyped maps.
4. The `SearchToolForTask` mapping resolves to lane service names
   (`retrieve_code`, `retrieve_memory`, etc.), not `code_search_ensemble`.

**Pass:** `LambdaRunner.leaf` calls a lane service and receives an
`EvidencePack`. `SearchToolForTask` returns a lane service name. Composition
operates on `EvidencePack` values.
**Fail:** LambdaRunner still calls `code_search_ensemble` or operates on raw
strings/maps.

Tool: `go test`
Evidence: test output showing LambdaRunner delegates to lane services and
operates on EvidencePack values.

---

### VAL-RLM-016: LLMRunner records RetrievalEpisode on every retrieval call

`LLMRunner` records a `RetrievalEpisode` for each retrieval tool invocation
made during a run. This covers both single-pass and staged modes.

After the hard-cut:
1. Each tool call to a lane service (`retrieve_code`, `retrieve_memory`,
   `retrieve_context`, `retrieve_task`, `retrieve_mixed`) records a
   `RetrievalEpisode`.
2. The episode is attached to the `Result.Metadata` under a `retrieval_episodes`
   key.
3. Staged mode accumulates episodes across all phases.

**Pass:** `Result.Metadata["retrieval_episodes"]` contains ≥1 episode for
every run that invokes at least one retrieval tool. Each episode has
populated `lane`, `query`, `duration_ms`, and `result_count`.
**Fail:** No episodes in metadata, or episodes have zero-valued fields.

Tool: `go test`
Evidence: test output showing episode recording in both single-pass and staged
LLMRunner modes.

---

### VAL-RLM-017: No new keyword or substring routing heuristics are introduced

The hard-cut must not introduce any new `strings.Contains`, `containsAny`,
or equivalent substring-matching logic for routing, classification, or
behavioral decisions.

All routing and classification must use:
- Typed struct fields
- Enum/match on structured fields
- Scored features or learned policies
- Explicit schemas

This applies to the entire `internal/rlm/` package and the lane service layer.

**Pass:** `git grep -n 'containsAny\|strings\.Contains' internal/rlm/` in the
new/modified files returns no hits in routing/classification functions.
**Fail:** Any new `containsAny` or `strings.Contains` call in routing or
classification logic.

Tool: `go test` + `grep`
Evidence: test output plus grep results showing no keyword heuristics in
routing/classification code.

---

### VAL-RLM-018: load_evidence_ref resolves typed refs from EvidencePack entries

`load_evidence_ref` accepts a typed evidence reference (e.g., `CodeRef`,
`MemoryRef`, `ContextRef`, `TaskRef`) and returns the underlying content.

The tool:
1. Accepts a `ref` parameter that is a structured object (not a bare string).
2. Dispatches to the appropriate loader based on ref type.
3. Returns the loaded content in a bounded envelope (large content → CAS
   pointer).
4. Records a `RetrievalEpisode` with `lane: "evidence_ref"`.

**Pass:** Tool accepts structured ref, returns content or CAS pointer, and
records an episode. Bounded output (no unbounded inline content).
**Fail:** Tool accepts bare string, returns unbounded content, or skips
episode recording.

Tool: `go test`
Evidence: test output showing ref resolution, output bounding, and episode
recording pass.

---

### VAL-RLM-019: ResolveToolPolicy rejects unknown profiles with a clear error

`ResolveToolPolicy` returns a descriptive error for profiles that are not in
the canonical set (`default`, `code-intel`, `memory-recall`,
`longcot-no-model-tools`).

The error message must contain the unsupported profile name and the string
`"unsupported tool profile"`.

**Pass:** Unknown profile returns error with profile name in message.
**Fail:** Unknown profile panics, returns empty tools without error, or error
message lacks profile name.

Tool: `go test`
Evidence: test output showing error message content assertion passes.

---

### VAL-RLM-020: Tool profile fail-closed — unknown profile yields zero tools

When `FilterTools` receives an unknown profile string, it returns an empty
tool slice (fail-closed) rather than falling through to the default profile.

**Pass:** `FilterTools(availableTools, "nonexistent-profile")` returns
`[]rlm.Tool{}`.
**Fail:** Unknown profile falls through to default and returns all tools.

Tool: `go test`
Evidence: test output showing empty tool slice for unknown profile.

---

### VAL-RLM-021: Staged plan phases reference composite lane tools, not atomic tools

After the hard-cut, `buildPlan` staged phases for `RouteProfileCodeRetrieval`
(and other routes) use composite lane tools in `AllowedTools`:

- Discovery phase: `retrieve_code`, `retrieve_mixed`
- Inspection phase: `load_evidence_ref`
- Verification phase: `load_evidence_ref`

Atomic tool names (`semantic_search_code`, `smart_search_code`, `ripgrep_code`,
`search_repo`, `code_search_ensemble`, `load_file`) must NOT appear in staged
plan phase `AllowedTools`.

**Pass:** No staged plan phase references atomic tool names in `AllowedTools`.
Phases reference only composite lane tools and `load_evidence_ref`.
**Fail:** Any phase references an atomic tool name.

Tool: `go test`
Evidence: test output showing phase AllowedTools sets contain only composite
tools.

---

### VAL-RLM-022: Existing rlm tests continue to pass after hard-cut

All existing test files in `internal/rlm/` and `internal/rlm/env/` pass
without modification to their behavioral assertions (test-only adjustments to
tool names and profile expectations are acceptable).

This guards against accidental breakage of the RLM runtime contract:
- `TestClassifyRouteProfile*` (or its replacement) passes
- `TestBuildPlanStaged*` passes with updated tool names
- `TestFilterTools*` passes with updated profiles
- `TestDefaultToolsExposeSchemas` passes with the new 6-tool set
- `TestResolveToolProfileUnknownFailsClosed` passes
- `TestFilterToolsLongCoTMinimalReturnsNoTools` passes

**Pass:** `go test ./internal/rlm/... ./internal/rlm/env/...` exits 0.
**Fail:** Any test in these packages fails.

Tool: `go test`
Evidence: test output showing all packages pass.
