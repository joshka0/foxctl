# foxctl Skill-Pack Split Analysis

**Status:** Draft
**Scope:** `skills/` directory and related `internal/` packages
**Date:** 2026-05-28
**Author:** AI Assistant (read-only repo analysis)

---

## Executive Summary

The foxctl repository currently houses approximately 160 skills under `skills/`, totaling ~152 MB of source, manifests, and build artifacts. Every skill—regardless of complexity—transitively imports the full `internal/intelligence/*` stack (turbovec, semantic indexing, repoindex, reranking) because the shared bootstrap library `internal/adapters/skillslib/skillmain` pulls in `storage/memory` → `turbovec` and `circuitbreaker` → `indexing/semantic`. This creates a monolithic dependency graph where even a simple `fs_ls` skill imports ~45 internal packages.

**The runtime already supports external skill packs.** The resolver (`internal/domain/skill/resolver.go`) discovers skills from `FOXCTL_SKILLS_PATH`, `~/.foxctl/skills`, and built-in paths relative to the executable. The `foxctl skills install --manifest ... --binary ...` contract is stable and well-tested. **The blocker is not the install contract; it is the bootstrap dependency graph.**

This report classifies ~48 skills as strong separation candidates, ~110 skills as core retention, and 10+ as ambiguous. The recommended path is to **decouple the skill SDK first (Phase 0)**, then extract domain-specific leaf skills into versioned skill-pack repositories. Attempting to move skills before Phase 0 would either break the build or force external packs to import the entire foxctl module as a dependency, defeating the purpose of separation.

---

## 1. Core Retention Criteria

A skill should remain in the main foxctl repository if it satisfies **any** of the following criteria:

| ID | Criterion | Definition | Examples |
|----|-----------|------------|----------|
| R1 | **Harness-native tool** | The agent daemon, overseer, or RLM runtime invokes it as a default reasoning primitive. | `code_semantic_search`, `rlm_query`, `memory_query` |
| R2 | **Bootstrap-critical** | Required for foxctl to function on a fresh workspace with no external skill packs installed. | `fs_read`, `git_status`, `setup_install`, `skill_inspect` |
| R3 | **Hook runtime** | Enforces agent execution policy; must version-lock with `internal/runtime/agentpolicy` and `internal/runtime/hooks`. | All `hooks_*` skills |
| R4 | **Intelligence substrate** | Improves the harness's ability to reason about code, sessions, or memory via indexing, embeddings, or graph traversal. | `repo_index_build`, `code_dag_grep`, `embedding_worker` |
| R5 | **High co-evolution** | Changes frequently in tandem with internal APIs; externalizing would create daily cross-repo churn. | `code_refactor_scout`, `code_counsel`, `session_summarize` |
| R6 | **Storage/trajectory native** | Writes to or reads from foxctl's canonical stores (memory, sessions, tasks, CAS, vector DB) as a primary function. | `session_save`, `trajectory_export`, `todo_sync_to_provider` |

---

## 2. Externalization Criteria

A skill is a strong separation candidate if it satisfies **all** of the following criteria:

| ID | Criterion | Definition | Counter-example (stays core) |
|----|-----------|------------|------------------------------|
| X1 | **Domain-specific, not foxctl-specific** | Tied to a game engine, mobile framework, external SaaS, or social platform rather than the generic agent harness. | `code_symbols` (generic code reasoning) |
| X2 | **No harness co-dependency** | The agent runtime does not invoke it as a default tool; it is purely user-facing or optional. | `rlm_query` (RLM calls it natively) |
| X3 | **Stable envelope contract** | Uses only envelope I/O and `skillmain` bootstrap; no direct imports of `internal/intelligence/*`, `internal/runtime/*` (outside adapters), or `internal/storage/*` (outside CAS/config). | `code_semantic_search` (imports `intelligence/indexing/*` directly) |
| X4 | **High weight / low core value** | Large LOC, heavy CI time, or large binary artifacts relative to its role in the harness. | `code_refactor_scout` (5K+ LOC, 48MB binary, but core to autonomous review) |
| X5 | **External API wrapper** | Thin client for a third-party service with no deep storage, vector, or session dependency. | `memory_query` (needs `storage/memory`) |

---

## 3. Concrete Keep-Core List

### 3.1 File System & Shell Primitives
`fs_read`, `fs_write`, `fs_ls`, `fs_tree`, `fs_find`, `fs_apply_edit`, `fs_cas_get`, `text_grep`, `text_replace`, `text_ripgrep`, `data_jq`, `json_transform`

**Retention rationale (R2):** These are bootstrap-critical. A fresh foxctl installation must provide filesystem introspection and text manipulation without requiring the user to install external packs.

### 3.2 Git Primitives
`git_status`, `git_worktree`, `code_git`

**Retention rationale (R2):** Git operations are foundational to workspace detection, session anchoring, and repo indexing. The core CLI assumes these are always available.

### 3.3 Code Intelligence & Retrieval (The "Harness Brain")
`code_semantic_search`, `code_smart_search`, `code_smart_read`, `code_smart_write`, `code_context_grep`, `code_context_ripgrep`, `code_snippet_extract`, `code_counsel`, `code_llm_search`, `code_diff`, `code_stats`, `code_complexity`, `code_security`, `code_symbols`, `code_imports`

**Retention rationale (R1, R4):** The RLM runtime and agent daemon use these as default reasoning tools. The `code_search_ensemble` provider inside `internal/rlm/env/tool_exec.go` explicitly calls semantic search and code probe logic. Removing these would amputate the agent's code-understanding layer.

### 3.4 Repo Index
`repo_index_build`, `repo_index_search`, `repo_index_expand`, `repo_index_open`, `repo_index_dag_grep`, `repo_index_enrich_summaries`, `code_incremental_index`

**Retention rationale (R4):** The repo graph index is foundational context for agents. It must version-lock with `internal/intelligence/indexing/*` and `internal/intelligence/repoquery`. The DAG grep skill is a direct consumer of the repoindex store format.

### 3.5 Refactor & Review
`code_refactor_scout`, `code_refactor_impact`, `code_refactor_advisor`, `code_greenlight`, `code_branch_impact`

**Retention rationale (R4, R5):** These co-evolve with the repo index and semantic search stack. `code_refactor_scout` is the largest single skill (~5K LOC, 12 test files) and is invoked by the RLM tool profile for autonomous code review. Moving it out would create a high-frequency cross-repo dependency.

### 3.6 Codemap
`codemap_generate`, `codemap_get`, `codemap_check`, `codemap_list`, `codemap_import`

**Retention rationale (R4):** Codemaps are agent traceability primitives. `codemap_generate` and `codemap_get` interact with the indexing backend (`internal/tooling/skillrun`, `internal/intelligence/evidence`) in ways that are still evolving.

### 3.7 Memory, Session, & Embedding Pipeline
`memory_query`, `session_recall`, `session_save`, `session_restore`, `session_summarize`, `session_timeline`, `session_turns`, `session_archive`, `session_capture`, `session_expand`, `session_deepdive`, `session_query`, `session_annotate`, `session_anchor`, `session_feedback`, `session_extract_learnings`, `session_export_dspy`, `embedding_memories`, `embedding_queue`, `embedding_refresh`, `embedding_tasks`, `embedding_worker`

**Retention rationale (R1, R6):** These manage the harness's own persistent state. The agent daemon depends on session and memory stores (`storage/sessions`, `storage/memory`, `storage/vector`). Session skills import `internal/context/sessionkit` and `internal/storage/trajectory`, which are not part of any public SDK surface.

### 3.8 RLM
`rlm_query`

**Retention rationale (R1, R5):** This is the primary skill surface for the RLM runtime (`internal/rlm`). It must stay in lock-step with executor modes (`inspect`, `llm`, `repl`), route profiles, and tool profiles defined in `internal/rlm/env/tool_profiles.go`.

### 3.9 Hooks (Agent Execution Policy)
`hooks_bash_guard`, `hooks_context_drain`, `hooks_context_enqueue`, `hooks_dispatch`, `hooks_file_guard`, `hooks_impact_analysis`, `hooks_knowledge_router`, `hooks_mail_router`, `hooks_overseer_inbox`, `hooks_session_end`, `hooks_stop_guard`, `hooks_subagent_start`, `hooks_subagent_stop`, `hooks_task_guard`, `hooks_test_feedback`

**Retention rationale (R3):** Hooks are the policy layer of the agent runtime. `hooks_bash_guard` and `hooks_subagent_start` directly import `internal/runtime/agentpolicy`. `hooks_dispatch` is the central dispatcher that loads `hooks.yaml`. These must version-lock with the daemon.

### 3.10 System & Meta Tooling
`skill_inspect`, `setup_foxctl_mode`, `setup_install`, `test_run`, `context_filter`, `graph`, `graph_cleanup`, `graph_pagerank`, `mailbox`, `mcp_bridge`, `mcp_install`, `providers`, `quality_gate`, `todo`, `todo_continuation`, `todo_sync_from_provider`, `todo_sync_to_provider`, `plan_sync`, `trajectory_export`, `summary_worker`, `cove_verify`

**Retention rationale (R2, R6):** Meta-tooling for the harness itself—setup, skill inspection, MCP wiring, task management, quality gates, and trajectory export. `providers` manages cross-provider config synchronization and is tightly coupled to the CLI's settings layout.

---

## 4. Concrete Extraction-Candidate List

These ~48 skills are strong candidates for external skill-pack repositories.

| Domain | Skills | Rationale |
|--------|--------|-----------|
| **Game Engines** | `build_godot`, `build_unity`, `editor_godot`, `unity_input`, `unity_packages`, `unity_scenes` | Domain-specific to Godot/Unity workflows. The generic harness never invokes these as default tools. |
| **Mobile** | `mobile`, `mobile_android`, `mobile_ios` | Expo/mobile-specific tooling. No agent runtime dependency. |
| **Social Media** | `social_facebook_collect`, `social_instagram_collect`, `social_reddit_collect`, `social_x_collect`, `social_youtube_collect` | External API scrapers with no harness storage coupling. |
| **SaaS Integrations** | `jira_board`, `jira_issue`, `ardoq_resource`, `exa`, `expo`, `cloud_localstack_blueprint`, `x402_payment`, `launch_praze_pipeline`, `heartwood_action`, `heartwood_state` | Thin wrappers around third-party services. |
| **LSP Adapters** | `lsp_gopls`, `lsp_pylsp`, `lsp_tsserver` | Language-server plumbing. Useful but not harness-critical. |
| **Research & Calibration** | `arxiv_summarize`, `calibration_feedback`, `calibration_generate`, `calibration_get`, `epic_complete` | Research, calibration, and experiment workflows. |
| **Optimization Loop** | `optimize_analyze`, `optimize_bootstrap`, `optimize_feedback`, `optimize_from_feedback`, `optimize_patterns`, `optimize_reflect`, `optimize_weights` | Self-improvement experiments. High churn, not in the critical path. |
| **CI-Specific** | `ci_checks`, `ci_prcomments` | Tied to this repository's GitLab setup, not generic harness behavior. |
| **Observability UI** | `obs_logs`, `obs_replay` | Log browsers. Convenient but not required for agent execution. |
| **Web & Content** | `web_search`, `web_extract`, `html_edit`, `http_openapi` | Generic web wrappers. The RLM runtime has its own retrieval path and does not depend on these skills. |

---

## 5. Ambiguous / Case-by-Case List

These skills do not fit cleanly into either bucket. They require further architectural work or a judgment call.

| Skill | Ambiguity | Recommendation |
|-------|-----------|----------------|
| `exa` | Web search provider used by both agents and human users. | **Defer.** If RLM ever hard-codes Exa as a default retrieval provider, it becomes core. Currently it is a thin wrapper. |
| `web_search` / `web_extract` | Used by some agent prompts but not by the RLM tool profile. | **Defer.** If the RLM `retrieve_code` or `retrieve_docs` tools ever delegate to these skills, they become core. |
| `providers` | Config sync across Claude, Codex, OpenCode, Factory, Gemini. Tightly coupled to CLI settings layout. | **Keep core for now.** Extracting it would require stabilizing the cross-provider config schema as a public API. |
| `mcp_bridge` / `mcp_install` | MCP server wiring. The agent daemon may need MCP bridges for tool access. | **Keep core.** If MCP becomes a pure user-config layer, revisit. |
| `http_openapi` | OpenAPI spec parsing. Could be used by the harness to auto-generate tool schemas. | **Defer.** Currently user-facing; if the agent daemon starts auto-registering OpenAPI tools, it becomes core. |
| `quality_gate` | Quality gates for the harness itself vs. quality gates for user projects. | **Keep core.** It evaluates harness trajectories and session quality, which is core telemetry. |
| `graph` / `graph_cleanup` / `graph_pagerank` | Generic graph utilities. The context engine uses graph traversal, but these skills are user-facing. | **Defer.** If the context engine (`internal/context`) starts calling these skills as subprocess tools, keep core. If they remain pure user utilities, extract. |
| `todo` / `todo_continuation` / `todo_sync_*` | Task management. The overseer and agent daemon use task stores, but the todo skill is a higher-level UX layer. | **Keep core.** The task graph (`internal/storage/tasks`) is central to agent planning. The todo skills are the primary CLI surface for it. |
| `plan_sync` | Plan synchronization. Coupled to the session/task stores. | **Keep core.** Plans are part of the agent execution trajectory. |
| `presence_*` (6 skills) | Presence/orchestration system. May be harness infrastructure or experimental feature. | **Evaluate usage.** If the agent daemon's proactive mode depends on these, keep core. If they are standalone experiments, extract to `foxctl-skills-experimental`. |

---

## 6. Phase 0 Prerequisites

**No skill should be moved before these prerequisites are completed.** Phase 0 is the structural leverage point that makes all subsequent extraction cheap and reversible.

### 6.1 Split `skillmain` into Lite and Stores Tiers

**Current state:** `internal/adapters/skillslib/skillmain/main.go` initializes a `StoreProvider` that eagerly opens memory, sessions, and task stores. `StoreProvider.Memory()` → `memory.Open()` → `storage/memory/store.go` → `turbovec` initialization. This pulls the entire intelligence stack into every skill's dependency graph.

**Required change:**
- Create `internal/adapters/skillslib/skillmain/lite/` containing:
  - Envelope parsing and validation
  - Config loading (`config.LoadDotEnv`, `config.Load`)
  - Path validation (`policy.NewPathValidator`)
  - CAS store initialization (CAS is a simple file store; acceptable for lite)
  - Observability span setup (`observability.StartSpan`)
  - `skillout.Emit` helpers
- Keep `internal/adapters/skillslib/skillmain/stores.go` in the existing package as an optional extension.
- Provide a `skillmain.MainLite[I any](command string, run RunFunc[I])` entry point for skills that do not need DB stores.

**Acceptance criteria:** A skill using `MainLite` must have zero transitive dependencies on `internal/intelligence/*` and `internal/storage/memory`, `internal/storage/sessions`, `internal/storage/tasks`.

### 6.2 Make `turbovec` Initialization Lazy

**Current state:** `storage/memory/store.go` declares a `vector.VectorSearcher` field that is initialized via `sync.Once` on first vector search. However, the **import** of `turbovec` still exists at compile time, forcing the linker to pull in the package and its dependencies.

**Required change:**
- Move the `VectorSearcher` interface to `internal/storage/vector/` (already exists) and have `memory.Store` hold it as an interface.
- Use a factory function injected at `Store` construction time, or use build tags to exclude `turbovec` imports when `skillmain/lite` is used.
- Ensure `memory.Open()` without vector search does not import `turbovec`.

### 6.3 Decouple `circuitbreaker` from Intelligence Indexing

**Current state:** `internal/runtime/execution/circuitbreaker` imports `internal/intelligence/indexing/rerank` and `internal/intelligence/indexing/semantic`. This is called from `skillmain` via `circuitbreaker.NewManager()`.

**Required change:**
- Refactor the circuit breaker to accept a generic health-check interface.
- Remove direct indexing dependencies from the circuit breaker package.
- If semantic health checks are needed, inject them at the call site in the full `skillmain`, not in `skillmain/lite`.

### 6.4 Audit Direct `internal/*` Imports in Skills

Run the following analysis for every candidate skill:

```bash
go list -deps ./skills/<name> | grep "^github.com/joshka0/foxctl/internal" | grep -v "adapters/skillslib\|domain/envelope\|domain/policy\|domain/skill\|platform"
```

If a candidate imports `internal/intelligence/*`, `internal/runtime/*`, `internal/context/*`, or `internal/storage/*` outside of the allowed adapter layer, it must either:
1. Be refactored to use `skillmain/lite` (if the dependency is accidental), or
2. Remain in the core repo until the dependency is abstracted.

---

## 7. Phased Migration Path

### Phase 0: SDK Decoupling (Estimated: 1–2 sprints)

- Complete prerequisites in Section 6.
- Verify with a pilot: refactor `fs_ls` and `web_search` to use `skillmain/lite` and confirm the dependency graph shrinks.
- Add CI gate: fail the build if any skill in the `skills/` directory imports `internal/intelligence/*` unless explicitly allow-listed.

### Phase 1: Publish Versioned Skill SDK (Estimated: 1 sprint)

- Extract the now-slim adapter layer into a versioned module:
  - `github.com/joshka0/foxctl/skill-sdk` or a standalone repo `github.com/joshka0/foxctl-skill-sdk`
  - Includes: `domain/envelope`, `domain/policy`, `domain/skill`, `adapters/skillslib/*`, `skillmain/lite`, `skillout`
- Define compatibility contract:
  - `v1.x` tracks stable envelope contracts (`version`, `status`, `command`, `data`, `meta`, `error`)
  - Breaking changes require `v2.x` and a migration window
- Add golden-file tests in the SDK that fail if envelope field requirements change.

### Phase 2: Extract Leaf Skill Packs (Estimated: 2–3 sprints, parallelizable)

Create focused repositories:

| Repository | Skills | Owner / Domain Expert |
|------------|--------|----------------------|
| `foxctl-skills-gamedev` | `build_godot`, `build_unity`, `editor_godot`, `unity_*` | Game engine maintainers |
| `foxctl-skills-mobile` | `mobile`, `mobile_android`, `mobile_ios` | Mobile/expo maintainers |
| `foxctl-skills-social` | `social_*` | Social integrations maintainer |
| `foxctl-skills-integrations` | `jira_*`, `ardoq_resource`, `exa`, `expo`, `cloud_*`, `x402_payment`, `launch_*`, `heartwood_*` | External SaaS maintainers |
| `foxctl-skills-lsp` | `lsp_gopls`, `lsp_pylsp`, `lsp_tsserver` | Language tooling maintainers |
| `foxctl-skills-experimental` | `arxiv_summarize`, `calibration_*`, `optimize_*`, `epic_complete`, `ci_*`, `obs_*`, `html_edit`, `web_*`, `http_openapi` | Research / experimentation team |

Each repository:
- Contains its own `go.mod` importing `foxctl-skill-sdk`.
- Builds `dist/skills/<name>/bin` and `dist/skills/<name>/skill.yaml` in CI.
- Publishes GitHub/GitLab release assets.
- Runs `foxctl skills install --manifest dist/skills/<name>/skill.yaml --binary dist/skills/<name>/bin` in CI against the latest stable foxctl CLI to verify compatibility.

### Phase 3: Core Repo Cleanup & CI Reduction (Estimated: 1 sprint)

- Remove extracted skill sources from the main repo.
- Update `Makefile`:
  - Remove `skills-a-g`, `skills-h-o`, `skills-p-x` race shards.
  - Replace with a single `skills-core` shard or fold skills into existing `core-internal` / `runtime` shards.
  - `make skills-build` only builds in-repo skills.
- Update `.gitlab-ci.yml`:
  - Remove 3 skill-specific race test jobs.
  - Add a single `skills-core-smoke` job.
- Document the new skill-pack ecosystem in `docs/general/skills.md`.

---

## 8. CI / Test Impact

### 8.1 Current State

The core repo CI currently shards skill tests across 3 race-detection jobs:

```yaml
race-skills-a-g:        # skills starting with a-g
race-skills-h-o:        # skills starting with h-o
race-skills-p-x:        # skills starting with p-x
```

Additionally, `make skills-build-impacted` and `make skills-build` compile all ~160 skills on every relevant change.

### 8.2 Post-Extraction State

| Metric | Before | After (Phase 3) |
|--------|--------|-----------------|
| Skill source LOC in core repo | ~84K (Go) | ~55K (Go) |
| Skill race shards | 3 | 0–1 |
| Skill build time (impacted) | O(all skills) | O(core skills only) |
| External skill test blast radius | Touches core CI | Isolated to external repo CI |
| Binary artifact size in `dist/skills` | ~100+ MB | ~60 MB |

### 8.3 Test Fixture Coupling

Several core tests assume specific skills exist:

- `internal/rlm/env/gather_context_realworld_test.go` creates fake files named `eval_code_search_ensemble.go` and references `skills/code_semantic_search`.
- RLM integration tests (`RLM_INTEGRATION_TESTS` in the Makefile) exercise retrieval paths that may depend on semantic search behavior.

**Mitigation:**
- Replace skill-specific fixtures with mock envelopes in `internal/testdata/envelopes/`.
- Keep a minimal `internal/testdata/skills/` directory containing stub `skill.yaml` and `bin` files for tests that need to exercise the resolver/installer.

---

## 9. API & Versioning Seams

### 9.1 Envelope Protocol Contract

The envelope protocol is the most critical seam. Downstream tooling (GUIs, hooks, OpenAPI layer, golden tests) depends on the exact shape of:

```json
{
  "version": 1,
  "status": "ok|error|progress",
  "command": "...",
  "data": {},
  "meta": { "ts": "..." },
  "error": {}
}
```

**Rule:** `meta.ts` must remain RFC3339 UTC. `meta.*` fields must not be renamed or removed without a spec update and a migration window. This is already an AUTO-REJECT rule per `AGENTS.md`.

**SDK enforcement:** The `skill-sdk` must contain golden-file tests that serialize/deserialize envelopes and fail on schema drift.

### 9.2 Manifest Contract

The `skill.yaml` schema is defined in `internal/domain/skill/manifest.go`. External packs depend on this schema for:

- `distribution.type` (`exec` vs `wasi`)
- `capabilities.network` (must be `"none"` for WASI)
- `signature.command` and `signature.parameters`
- `openapi.enabled` and `openapi.methods`

**Stability commitment:** The manifest schema should be frozen at `foxctl/v1` for the lifetime of `skill-sdk/v1`. New optional fields are allowed; renaming or removing fields requires a new API version.

### 9.3 Skill SDK Versioning

| Scenario | Policy |
|----------|--------|
| SDK patch release (`v1.0.1`) | Bug fixes in adapters; no breaking changes. |
| SDK minor release (`v1.1.0`) | New adapter utilities, new optional manifest fields. Backward compatible. |
| SDK major release (`v2.0.0`) | Breaking changes in envelope or manifest contracts. Requires foxctl CLI major release and migration guide. |

External skill packs should pin to `skill-sdk ^1.x` and test against the latest stable foxctl CLI in CI.

### 9.4 CLI / Skill Version Skew

**Problem:** A user installs `foxctl v2.5.0` but has skill packs built against `foxctl v2.3.0` (or vice versa). New capabilities or stricter validation may cause runtime failures.

**Mitigation:**
- Add `apiVersion` validation at install time: `foxctl skills install` should warn if the manifest's `apiVersion` does not match the CLI's supported range.
- Add `foxctl skills doctor` command that scans installed skills and reports:
  - Missing or incompatible `apiVersion`
  - Missing required capabilities
  - Deprecated parameter types
  - Skills that have not been updated in N days

---

## 10. Recommended Next Issues

The following issues should be created to track the work derived from this analysis:

1. **Decouple skillmain from intelligence monolith**
   - Split `skillmain` into `lite` and `stores` tiers.
   - Remove `turbovec` compile-time dependency for store-less skills.
   - Remove `indexing/*` from `circuitbreaker` package.

2. **Audit skill dependency graphs for externalization readiness**
   - For each extraction candidate, run `go list -deps` and document direct `internal/*` imports.
   - Create a tracking spreadsheet/matrix of which candidates are blocked on which internal package.

3. **Publish `foxctl-skill-sdk` module**
   - Extract envelope, policy, skill domain types, and `skillmain/lite` into a versioned Go module.
   - Add golden-file tests for envelope serialization.
   - Tag `v1.0.0` and document compatibility contract.

4. **Add `foxctl skills doctor` command**
   - Validate installed manifests against CLI version.
   - Report version skew, missing capabilities, and deprecated fields.

5. **Pilot externalization with `foxctl-skills-web`**
   - Extract `web_search`, `web_extract`, `html_edit` as the first external pack.
   - Validate the CI pipeline, install contract, and SDK ergonomics.
   - Document lessons learned before scaling to larger domains (game engines, mobile).

6. **Refactor core tests to remove skill-specific fixtures**
   - Replace `eval_code_search_ensemble.go` fake files and `Makefile` probe references with mock envelopes.
   - Ensure RLM integration tests do not depend on the physical presence of `skills/code_semantic_search` source code.

7. **Update CI to reduce skill blast radius**
   - Remove `skills-a-g`, `skills-h-o`, `skills-p-x` shards after Phase 0.
   - Add `skills-core-smoke` job that builds and tests only retention-list skills.

---

## Appendix: Data Sources

This analysis is based on the following read-only repo inspection:

- `skills/` directory listing: 160 skill manifests, ~353 Go files, ~151 test files, ~152 MB total.
- `go list -deps` analysis across representative skills (`code_dag_grep`, `fs_ls`, `web_search`, `unity_scenes`, `rlm_query`).
- `internal/adapters/skillslib/skillmain/main.go` and `stores.go` dependency graph.
- `internal/domain/skill/manifest.go`, `resolver.go`, `searchpaths.go` for install/runtime contracts.
- `Makefile` skill build targets and `.gitlab-ci.yml` race shard configuration.
- `AGENTS.md` hard-fail and code-smell rules regarding envelope contracts and WASI policy.

No files were modified. No git mutations were performed.
