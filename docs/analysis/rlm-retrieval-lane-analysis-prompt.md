# RLM Retrieval Lane Analysis — Grounded Analysis Prompt

**For:** A strong analysis/design agent  
**Goal:** Produce a structured RLM Retrieval Lane Analysis document  
**Constraint:** Read-only RLM. No write-capable tools. No keyword-heuristic routing.  
**AGENTS.md Rule #16:** _Never use keyword heuristics — do not route, classify, promote, or suppress behavior using ad hoc substring/keyword matching; these heuristics are brittle. Prefer explicit schemas, typed signals, scored features, tests, or learned policies._

---

## Context: What This System Is

`foxctl` is an AI developer assistant runtime. The **RLM (Recursive Language Model)** subsystem is a bounded, read-only query runtime that accepts a task prompt, an environment (pre-loaded handles), and a tool surface, then runs an LLM tool-loop to produce an answer + evidence refs.

The three retrieval lanes are:
1. **Code Lane** — repo symbols, files, repo graph
2. **Memory Lane** — companion episodes, artifact trajectories, scenes
3. **Context Lane** — ContextWiki top-of-mind, handoffs, Obsidian vault notes

---

## Source Code: Complete Key Files

### `internal/rlm/types.go`

```go
package rlm

// Task describes one bounded RLM run request.
type Task struct {
    Prompt          string
    Role            string
    RunID           string
    AgentID         string
    ParentAgentID   string
    OutputRoot      string
    OutputNamespace string
    WorkspaceID     string
    WorkspaceRoot   string
    MaxDepth        int
    MaxIterations   int
    MaxSubcalls     int
}

// Tool is a typed environment tool handle exposed to the RLM runtime.
type Tool struct {
    Name        string
    Description string
    Parameters  json.RawMessage
    ReadOnly    bool
}

// Environment is the typed external state visible to the runtime.
type Environment struct {
    TopOfMind       map[string]any   // ContextWiki top-of-mind JSON blob
    LatestHandoff   map[string]any   // latest ContextWiki handoff JSON blob
    ActiveThreadIDs []string         // companion thread IDs
    SceneHandles    []string         // "conversation:<id>", "episode:<n>"
    ArtifactHandles []string         // "trajectory:<id>", "artifact:<sha256>"
    RepoHandles     []string         // "path:<file>", "repo:<node>"
    VaultHandles    []string         // "note:<path>"
    Tools           []Tool
}

// Result is the final RLM run result.
type Result struct {
    Answer         string
    EvidenceRefs   []string
    RetrievedPaths []string
    Iterations     int
    Subcalls       int
    TrajectoryID   string
    Metadata       map[string]any
}
```

### `internal/rlm/interfaces.go`

```go
package rlm

// Runner executes a bounded RLM task over an external environment.
type Runner interface {
    Run(ctx context.Context, task Task, env Environment) (Result, error)
}

// Sandbox executes model-authored code against external state in a controlled environment.
type Sandbox interface {
    Init(ctx context.Context, state map[string]any) error
    Execute(ctx context.Context, code string) (ExecResult, error)
    Snapshot(ctx context.Context) (map[string]any, error)
    Close(ctx context.Context) error
}

// Bootstrapper prepares a runtime environment from existing foxctl state.
type Bootstrapper interface {
    Build(ctx context.Context, task Task) (Environment, error)
}
```

### `internal/rlm/plan.go` — RouteProfiles, PlanModes, ClassifyRouteProfile

```go
package rlm

type RouteProfile string
const (
    RouteProfileAuto          RouteProfile = "auto"
    RouteProfileCodeRetrieval RouteProfile = "code_retrieval"
    RouteProfileMemoryRecall  RouteProfile = "memory_recall"
    RouteProfileMixed         RouteProfile = "mixed"
    RouteProfileEvidenceAudit RouteProfile = "evidence_audit"
)

type PlanMode string
const (
    PlanModeFree   PlanMode = "free"
    PlanModeGuided PlanMode = "guided"
    PlanModeStaged PlanMode = "staged"
    PlanModeHard   PlanMode = "hard"
    PlanModeLambda PlanMode = "lambda-retrieval"
)

// NOTE: This function VIOLATES AGENTS.md rule #16 (no keyword heuristics).
// It is called when route = "auto". Default falls through to code_retrieval
// even for memory-domain queries that don't contain any of the listed keywords.
func ClassifyRouteProfile(prompt string) RouteProfile {
    query := strings.ToLower(strings.TrimSpace(prompt))
    switch {
    case containsAny(query, "thread", "scene", "handoff", "session", "decision", "artifact", "timeline"):
        return RouteProfileMemoryRecall
    case containsAny(query, "code", "repo", "package", "file", "handler", "api", "storage", "auth", "runtime", "function", "symbol", "path"):
        return RouteProfileCodeRetrieval
    default:
        return RouteProfileCodeRetrieval  // ← memory/vault queries silently fall to code lane
    }
}

// Staged plan — only implemented for code_retrieval route.
// memory_recall, mixed, evidence_audit have no staged plan yet.
func buildPlan(route RouteProfile, mode PlanMode) Plan {
    plan := Plan{RouteProfile: route, Mode: mode}
    if mode != PlanModeStaged {
        return plan  // free/guided/hard/lambda: no phases
    }
    switch route {
    case RouteProfileCodeRetrieval:
        plan.Phases = []Phase{
            {
                Name:         "discovery",
                Objective:    "Find likely repository files or canonical notes.",
                AllowedTools: []string{"code_search_ensemble", "semantic_search_code", "smart_search_code", "search_repo", "search_vault"},
                RequireOneOf: []string{"code_search_ensemble"},
                MaxIterations: 3, RequireToolUse: true,
            },
            {
                Name:         "inspection",
                Objective:    "Open and inspect the strongest candidates from discovery.",
                AllowedTools: []string{"load_file", "read_note", "ripgrep_code"},
                RequireOneOf: []string{"load_file", "read_note"},
                MaxIterations: 3, RequireToolUse: true,
            },
            {
                Name:         "verification",
                Objective:    "Cross-check the strongest candidate and confirm the best supporting paths.",
                AllowedTools: []string{"load_file", "read_note", "ripgrep_code", "expand_repo_graph"},
                RequireOneOf: []string{"load_file", "read_note", "ripgrep_code", "expand_repo_graph"},
                MaxIterations: 2, RequireToolUse: true,
            },
        }
    // memory_recall, mixed, evidence_audit: no phases defined yet
    }
    return plan
}
```

### `internal/rlm/run_spec.go` — Tool Profiles

```go
package rlm

type ToolProfile string
const (
    ToolProfileDefault             ToolProfile = "default"
    ToolProfileCodeIntel           ToolProfile = "code-intel"
    ToolProfileLongCoTNoModelTools ToolProfile = "longcot-no-model-tools"
)

func ResolveToolPolicy(available []Tool, profile string) (ToolPolicy, error) {
    switch resolvedProfile {
    case ToolProfileDefault:
        // All 16 tools exposed
        return ToolPolicy{Profile: resolvedProfile, AllowedTools: all, Tools: all}, nil

    case ToolProfileCodeIntel:
        // Narrow: code-only tools
        allow := map[string]struct{}{
            "semantic_search_code": {},
            "smart_search_code":    {},
            "ripgrep_code":         {},
            "code_search_ensemble": {},
            "load_file":            {},
            "search_vault":         {},
            "read_note":            {},
            "subcall":              {},
        }
        // NOTE: memory tools (search_scenes, search_artifacts, etc.) stripped out

    case ToolProfileLongCoTNoModelTools:
        // Empty tool set — model gets no tools at all
        return ToolPolicy{Profile: resolvedProfile, AllowedTools: nil, Tools: []Tool{}}, nil
    }
}
```

### `internal/rlm/env/tools.go` — Full DefaultTools() — 16 tools

```go
// === CONTEXT LANE (4 tools) ===
{Name: "get_top_of_mind",    Description: "Load the ContextWiki top-of-mind bundle for the workspace.", ReadOnly: true},
{Name: "get_latest_handoff", Description: "Load the latest ContextWiki handoff for the workspace.", ReadOnly: true},
{Name: "search_vault",       Description: "Search the Obsidian or vault knowledge plane for relevant notes.", ReadOnly: true,
    Parameters: {query: string, limit: int}},
{Name: "read_note",          Description: "Load one durable note by note handle or vault-relative path.", ReadOnly: true,
    Parameters: {path: string (required)}},

// === MEMORY LANE (5 tools) ===
{Name: "search_scenes",      Description: "Search companion scenes or episodes by summary/topic.", ReadOnly: true,
    Parameters: {query: string, limit: int}},
{Name: "get_scene",          Description: "Load one companion scene or episode by handle.", ReadOnly: true,
    Parameters: {handle: string (required)}},  // e.g. "episode:42" or "conversation:<id>"
{Name: "search_artifacts",   Description: "Search persisted artifacts and trajectory handles by query.", ReadOnly: true,
    Parameters: {query: string, limit: int}},
{Name: "load_artifact",      Description: "Load one bounded artifact, trajectory, event, file, or note by handle.", ReadOnly: true,
    Parameters: {handle: string (required)}},  // polymorphic: trajectory:, event:, artifact:, sha256:, path:, note:
{Name: "memory_ensemble_retrieve", Description: "Run one bounded memory scout ensemble...", ReadOnly: true,
    Parameters: {query: string (required), lanes: []string, max_scouts: int, max_iterations_per_scout: int,
                 max_subcalls_per_scout: int, limit_per_lane: int}},

// === CODE LANE (7 tools) ===
{Name: "semantic_search_code", Description: "Preferred first-pass repo understanding tool.", ReadOnly: true,
    Parameters: {query: string (required), scope: []string, limit: int, repo_index_mode: string}},
{Name: "smart_search_code",    Description: "Preferred follow-up repo understanding tool.", ReadOnly: true,
    Parameters: {question: string (required), repo_index_mode: string, max_candidates: int, max_snippets: int}},
{Name: "ripgrep_code",         Description: "Exact text/symbol/literal pattern searches.", ReadOnly: true,
    Parameters: {pattern: string (required), path: string, glob: []string, glob_not: []string, max_matches: int, max_blocks: int}},
{Name: "search_repo",          Description: "Fallback shallow repo graph FTS search.", ReadOnly: true,
    Parameters: {query: string (required), limit: int}},
{Name: "expand_repo_graph",    Description: "Expand the repo graph around one seed handle.", ReadOnly: true,
    Parameters: {seed: string (required), depth: int, budget: int}},
{Name: "load_file",            Description: "Load a bounded file slice from the workspace.", ReadOnly: true,
    Parameters: {path: string (required), start_line: int, end_line: int}},
{Name: "code_search_ensemble", Description: "Run a staged code-search ensemble... compact evidence pack.", ReadOnly: true,
    Parameters: {query: string (required), task_type: string, candidate_paths: []string, constraints: {...}, budget: {...}}},

// === CROSS-CUTTING (1 tool) ===
{Name: "subcall", Description: "Issue one bounded recursive subcall over selected repo/vault/scene/artifact handles.",
    ReadOnly: true,
    Parameters: {prompt: string (required), role: string, repo_handles: []string, vault_handles: []string,
                 scene_handles: []string, artifact_handles: []string, max_depth: int, max_iterations: int, max_subcalls: int}},
```

### `internal/rlm/env/bootstrap.go` — How Environment is built

```go
// Bootstrapper.Build() sequence:
// 1. workspaceRoot from task.WorkspaceRoot (normalized)
// 2. contextplane.NewWorkspaceStore(workspaceRoot).Layout() → TopOfMindPath, HandoffsDir
// 3. Read TopOfMind JSON (flat blob, no schema)
// 4. latestHandoff() → sorted by filename DESC, first .json wins → env.LatestHandoff
//    → also extracts handoff.EvidenceRefs into env.ArtifactHandles
// 5. topOfMindRefs(top) → raw "relevant_refs" field → appended to env.ArtifactHandles
// 6. loadCompanionHandles() → SQL: GROUP BY conversation_id, ORDER BY latest DESC LIMIT 5
//    → env.ActiveThreadIDs, env.SceneHandles ("conversation:<id>", "episode:<n>")
// 7. loadRepoHandles() → repoquery.SearchWithProjection(query=task.Prompt or top["objective"], limit=5)
//    → env.RepoHandles = "path:<file>" anchors, or "repo:<node>" if no anchors
// 8. loadVaultHandles() → obsidianindex.SearchNotes(query=task.Prompt or top["objective"], limit=5)
//    → env.VaultHandles = "note:<path>"
// 9. loadTrajectoryHandles() → trajectory.ListTrajectories(workspace, limit=5)
//    → appended to env.ArtifactHandles ("trajectory:<id>", "artifact:<digest>")

// Constants:
const (
    defaultRepoHandleLimit  = 5
    defaultVaultHandleLimit = 5
    defaultThreadLimit      = 5  // companion conversations
    defaultArtifactLimit    = 5  // trajectory entries
)

// KEY GAP: vault bootstrap uses env vars for path resolution:
//   FOXCTL_RLM_VAULT_PATH || FOXCTL_ACA_VAULT_PATH || FOXCTL_OBSIDIAN_VAULT_PATH
// If none set → VaultHandles = nil, search_vault silently returns empty
```

### `internal/rlm/env/adapter.go` — Key dispatch details

```go
// Tool dispatch switch (executeInternal):
// "get_top_of_mind"        → returns env.TopOfMind (pre-loaded blob, no query)
// "get_latest_handoff"     → returns env.LatestHandoff (pre-loaded blob, no query)
// "search_artifacts"       → string Contains on env.ArtifactHandles + trajectory.ListTrajectories re-query
// "load_artifact"          → polymorphic by handle prefix:
//                             trajectory: → store.GetTrajectory + ListEvents(limit=20)
//                             event:      → ListEvents(limit=200) scan by ID
//                             artifact:/sha256: → cas.Get() → io.LimitReader(64*1024) → content as string
//                             path:       → loadFile()
//                             note:       → readNote()
// "search_repo"            → repoquery.SearchWithProjection (FTS)
// "semantic_search_code"   → skill "code/semantic_search" (subprocess, 45s timeout)
// "smart_search_code"      → skill "code/smart_search" (subprocess, 45s timeout)
// "ripgrep_code"           → skill "code/context_ripgrep" (subprocess, 45s timeout)
// "expand_repo_graph"      → repoquery.ExpandWithProjection (EdgeSetStructural, DirOut)
// "load_file"              → os.ReadFile + sliceLines
// "search_vault"           → obsidianindex.SearchNotes
// "read_note"              → os.ReadFile on vault path
// "memory_ensemble_retrieve" → memoryEnsembleRetrieve() (see below)
// "code_search_ensemble"   → codeSearchEnsemble() (see below)
// "search_scenes"          → SQL: lower(summary) LIKE '%query%' on companion_soft_episodes
// "get_scene"              → episode: or conversation: branch (last 5 turns)
// "subcall"                → a.subcall() callback (may be nil → returns {supported:false})

// CAS artifact read limit: 64KB
// Skill subprocess timeout: 45 seconds
// isModelToolAllowed: checks tool.Name against env.Tools list (model-visible allowlist)
// ExecuteInternal: bypasses allowlist (for deterministic/eval paths)
```

### `internal/rlm/env/memory_ensemble.go` — Memory Ensemble

```go
// memoryEnsembleRetrieve() flow:
// 1. Parse input: {query, lanes: [], max_scouts: 3, max_iterations_per_scout: 3, max_subcalls_per_scout: 0}
// 2. selectMemoryScoutRoles(lanes, max_scouts):
//    - "facts"/"fact"    → ScoutRoleMemoryFact = "memory_fact_scout"
//    - "timeline"/"time" → ScoutRoleMemoryTimeline = "memory_timeline_scout"
//    - "aca"/"context"   → ScoutRoleACAContext = "aca_context_scout"
//    - empty lanes → all 3 roles
// 3. For each role, call a.subcall(ctx, Task{prompt: buildMemoryScoutPrompt(role, query), Role: role}, env)
// 4. Each subcall runs a child LLM iteration (max_depth=0, max_iterations=3, max_subcalls=0)
// 5. Parse scout output as JSON: {summary, claims, current_best_view, timeline, context_blocks, gaps}
// 6. Aggregate across scouts → recommendAnswerBasis → buildMemoryEnsembleSummary

// Scout prompts (buildMemoryScoutPrompt):
// memory_fact_scout:     "Find the current explicit facts, preferences, decisions, goals..."
//                         Return: {summary, claims:[{key,value,status,source,evidence_refs,confidence}], gaps}
// memory_timeline_scout: "Reconstruct the update timeline... identify the current best view."
//                         Return: {summary, current_best_view, timeline:[{ts,kind,value,source,...}], gaps}
// aca_context_scout:     "Gather the durable ContextWiki, handoff, and vault-backed context..."
//                         Return: {summary, context_blocks:[{lane,summary,refs}], gaps}

// KEY ISSUE: memory_ensemble_retrieve requires a.subcall != nil.
// If subcall is nil → returns {supported: false, message: "requires subcall support"}
// subcall is only set via adapter.SetSubcall() — callers must wire this up
```

### `internal/rlm/env/scout_roles.go` — Scout Role Tool Filtering

```go
// Tool sets per scout role:
// memory_fact_scout:    search_artifacts, load_artifact, search_scenes, get_scene, search_vault, read_note
// memory_timeline_scout: search_scenes, get_scene, search_artifacts, load_artifact, get_latest_handoff
// aca_context_scout:    get_top_of_mind, get_latest_handoff, search_vault, read_note

// NOTE: search_scenes uses SQL LIKE '%query%' — no semantic search for memory lane
// NOTE: no session_recall or session_timeline skill exposed in any scout role
// NOTE: no memory_query (named memory retrieval) skill exposed
```

### `internal/rlm/env/code_search_ensemble.go` — Code Ensemble (key parts)

```go
// codeSearchEnsemble() multi-stage pipeline:
// Input: {query, task_type, candidate_paths, constraints, budget}
// task_types: file_locate, execution_trace, symbol_inspect, change_impact, registration_trace

// Budget defaults: max_candidates=8, max_files=4, max_snippets=4
// Semantic stage timeout: 12 seconds

// Stage sequence (internally, no subcalls — direct function calls):
// 1. Derive probes: phraseIdentifierProbes, exactProbes, pathProbes, traceAnchors, symbolProbes
// 2. Semantic search (code/semantic_search skill) — timed at 12s
// 3. Repo graph FTS (repoquery.SearchWithProjection)
// 4. Repo graph DAG expansion (repoquery.DAGGrepWithProjection if available)
// 5. Exact symbol probing via ripgrep
// 6. Candidate deduplication, scoring, ranking
// 7. Optional LLM selector (if llm_selector.enabled) — re-ranks candidates using LLM
// 8. Optional LLM planner (if llm_planner.enabled) — generates seed_queries, path_biases
// 9. Optional LLM replanner (if enable_replan) — adapts after initial pass
// 10. Load file snippets for top candidates
// Returns: {files: [], symbols: [], snippets: [], candidates_trace: [], metadata: {}}

// Route families: code, infra_resource, package_ownership, cochange_history
// These are internal routing labels, not user-visible

// KEY ISSUES:
// - No ContextWiki/memory cross-pollination (include_history, include_aca are reserved/ignored)
// - The LLM planner/selector add latency and LLM cost with currently unclear eval benefit
// - obsidianindex and companion DB are NOT queried inside the ensemble
```

---

## Current Eval Results (from `docs/archive/plans/features/rlm-retrieval-findings.md`)

Benchmark: `foxctl-mixed.yaml`

| Mode | hit@5 | MRR | Notes |
|------|-------|-----|-------|
| `skill_context` (ContextWiki-only) | **0.86** | **0.79** | Strongest overall |
| `skill_default_plus_context` (ContextWiki+code) | 0.86 | 0.71 | Good recall, weaker ranking |
| `repoindex_dag` | 0.29 | 0.10 | Best non-ContextWiki structural lane |
| `repoindex_search` | 0.14 | 0.07 | |
| `rlm_llm` (free mode) | 0.14 | 0.07 | Same as raw FTS — tool loop not helping |
| `rlm_llm_code_staged` | **0.00** | **0.00** | Broken on current evals |

Benchmark: `praze-mixed.yaml` — all modes near zero except ContextWiki (0.12 hit@5).

**Key interpretation:** ContextWiki-only dominates. The RLM runtime is real and tool-using, but the controller is currently no better than a plain FTS call.

---

## Known Rule Violations and TODOs

### AGENTS.md Rule #16 Violation: `ClassifyRouteProfile`

Location: `internal/rlm/plan.go:74-84`

```go
func ClassifyRouteProfile(prompt string) RouteProfile {
    query := strings.ToLower(strings.TrimSpace(prompt))
    switch {
    case containsAny(query, "thread", "scene", "handoff", "session", "decision", "artifact", "timeline"):
        return RouteProfileMemoryRecall
    case containsAny(query, "code", "repo", "package", "file", "handler", "api", ...):
        return RouteProfileCodeRetrieval
    default:
        return RouteProfileCodeRetrieval  // silently misfires for all other queries
    }
}
```

This is **explicitly prohibited** by AGENTS.md rule #16: _"Never use keyword heuristics"_.

### `memory_recall` route has no staged plan
`buildPlan()` only generates phases for `RouteProfileCodeRetrieval`. The memory, mixed, and evidence_audit routes return an empty-phase plan even in staged mode.

### `memory_ensemble_retrieve` requires subcall to be wired
Returns `{supported: false}` if `subcall` not set. Only a few callers wire it.

### `include_history` and `include_aca` are reserved/unimplemented
Both fields exist in `codeSearchEnsembleInput.Constraints` but are explicitly marked "ignored in the first slice."

### `search_scenes` uses LIKE substring match
```go
query := "%" + strings.ToLower(strings.TrimSpace(input.Query)) + "%"
// SQL: WHERE lower(summary) LIKE ?
```
No semantic search. Low recall on paraphrased or conceptually similar queries.

### No session_recall, session_timeline, or memory_query skills exposed
These exist as foxctl skills but are not wired into any scout role or tool.

### CAS artifacts hard-limited to 64KB
```go
body, err := io.ReadAll(io.LimitReader(reader, 64*1024))
```
Silently truncates large artifacts (session transcripts, large eval outputs).

---

## Analysis Framework

For each of the three lanes, answer these questions:

### What tools exist today?
- List each tool name, what skill/store it dispatches to, and what it returns
- Note which are composite (call multiple things internally) vs. atomic

### How does the lane bootstrap?
- What handles are pre-loaded into Environment at start?
- What indexes/databases does it rely on? What happens when they're missing?

### What is the retrieval funnel?
- For a typical query, what is the sequence of operations?
- Where does precision increase vs. where does recall broaden?

### What are the failure modes?
- When does this lane return empty or low-quality results?
- What are the timeout/CAS/latency/wiring bottlenecks?

### Cross-lane interactions
- Does this lane reference the others? Are there missed opportunities?

---

## Global Questions to Answer

1. **Tool Surface Bloat**: 16 tools. Are all necessary? Which overlap or could merge?

2. **Routing Heuristics**: `ClassifyRouteProfile()` uses `containsAny()`. This violates AGENTS.md rule #16. What should replace it?

3. **Composite vs. Atomic**: `code_search_ensemble` and `memory_ensemble_retrieve` are large composites. Should they be decomposed or are they the right level of abstraction?

4. **Missing Skills**: Which read-only foxctl skills are NOT exposed to RLM but should be? Candidates: `session/recall`, `session/timeline`, `memory/query`.

5. **CAS & Token Efficiency**: 64KB limit, 45s timeouts, subprocess per skill call. Where does token waste happen?

6. **Lambda vs. LLM Runner Split**: `LambdaRunner` is deterministic/cheap. `LLMRunner` is flexible/expensive. `ToolProfileLongCoTNoModelTools` removes all tools from LLM path. Is this split correct?

7. **The Unified Vision**: Memories ↔ code ↔ tasks should feel unified to the agent. What is the minimal change that would make cross-lane retrieval feel seamless?

8. **Staged plan for memory_recall**: `memory_recall` route has no staged phases. What should the phases look like, analogous to the code_retrieval 3-phase plan?

9. **ContextWiki dominance**: ContextWiki-only wins on evals (hit@5 0.86). The RLM tool loop adds no measurable improvement over raw FTS. What is the hypothesis for why? What would fix it?

---

## Output Format Required

Produce a single markdown document with this structure:

```markdown
# RLM Retrieval Lane Analysis

## Executive Summary
3-5 sentences on current state and biggest opportunity.

## Lane 1: Code Retrieval
### Tools
### Bootstrap
### Retrieval Funnel
### Failure Modes
### Cross-Lane Interactions

## Lane 2: Memory Retrieval
### Tools
### Bootstrap
### Retrieval Funnel
### Failure Modes
### Cross-Lane Interactions

## Lane 3: Context Retrieval (ContextWiki/Vault)
### Tools
### Bootstrap
### Retrieval Funnel
### Failure Modes
### Cross-Lane Interactions

## Global Assessment
### Tool Surface Bloat
### Routing Heuristics (Rule #16 Compliance)
### Composite vs. Atomic
### Missing Skills
### CAS & Token Efficiency
### Lambda vs. LLM Runner Split
### The Unified Vision

## Recommendations (Ranked by impact/effort)
1. Highest impact, lowest effort
2. ...
N. Lowest impact, highest effort

## Appendix: Full Tool Inventory
| Tool Name | Lane | Dispatches To | Composite? | ReadOnly? | Scout Roles |
```

---

## Constraints

- Do NOT propose write-capable tools. RLM is read-only.
- Do NOT suggest adding all 151 skills. Be selective — name specific gaps.
- Do NOT use keyword heuristics in any recommendations (AGENTS.md rule #16).
- Prefer concrete file paths and code snippets over hand-waving.
- If you find a TODO or FIXME, note it explicitly.
- Cite eval numbers when making claims about retrieval quality.
