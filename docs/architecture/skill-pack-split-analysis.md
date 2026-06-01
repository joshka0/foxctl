# Skill Pack Split Analysis

| Field | Value |
|-------|-------|
| Status | Draft — proposal, not yet approved |
| Scope | Skill externalization strategy for `skills/` (162 skills, ~147K LoC) |
| Last reviewed | 2026-05-28 |
| Depends on | [package-topology.md](package-topology.md), [skills.md](../general/skills.md) |

---

## Executive Summary

The `skills/` directory contains 162 Go skill binaries that dominate foxctl's
repository size and CI surface (3 dedicated race-test shards). Roughly 60% of
these skills are deeply coupled to foxctl's intelligence, context, storage, or
runtime layers and must remain in the core repo. The remaining ~40% are thin
wrappers over external APIs or domain-agnostic utilities that could be extracted
into independent "skill pack" modules with minimal risk.

The key blocker is that all skills today import `internal/adapters/skillslib/*`,
which is an internal package. Before any extraction can happen, the reusable
skill-building substrate must be promoted to a public (or at least
separately-versioned) module. This document proposes a four-phase migration
that starts with the shallowest extractions and progressively reduces the core
repo's CI blast radius.

**Recommendation**: Execute Phase 0 (skillslib promotion) and Phase 1
(social/Jira/Ardoq extraction, 8 skills) as the first milestone. This proves
the pack interface contract with minimal risk and removes three race-test-shard
skill directories from core CI.

---

## Current State

| Metric | Value |
|--------|-------|
| Total skills | 162 directories under `skills/` |
| Go source lines | ~147K across all skills |
| Test files | 151 `*_test.go` files |
| CI race shards | 3 (`skills-a-g`, `skills-h-o`, `skills-p-x`) |
| Build targets | `make skills-build`, `skills-build-cgo`, `skills-build-all` |
| Install targets | `make skills-install`, `skills-install-cgo`, `skills-install-all` |
| Largest skills | `code_semantic_search` (4385 LoC), `code_refactor_scout` (5155 LoC), `session_summarize` (3575 LoC) |
| Embedded assets | `skills/assets.go` embeds `wasi_echo` manifest + wasm module |

Every skill is a Go `main` package that compiles against `internal/*`. The
`skill.yaml` manifest and the JSON envelope I/O contract (`distribution.type:
exec`, `signature.command`, `io.format: JSON`) form the only stable interface
between the skill binary and the foxctl runtime.

---

## Retention Criteria (Keep-Core)

A skill should stay in the main repository when **any** of the following hold:

1. **Deep intelligence coupling.** Imports packages under
   `internal/intelligence/*` (repoindex, repoquery, retrieval, turbovec,
   codecontext, codemap, semantic indexing, verification, branchimpact,
   refactor, searchindex, searchrank). These packages are the harness's
   retrieval and reasoning layer; their APIs are unstable and co-evolve with
   the skills that consume them.

2. **Deep context/memory coupling.** Imports packages under
   `internal/context/*` (calibration, sessionkit, contextplane, contextengine,
   memorycore). These packages manage the harness's memory and session
   continuity. Skills that write to context buffers, memory stores, or
   calibration databases share mutable state with the runtime.

3. **Hook identity.** Lives under `hooks_*`. Hook skills extend the foxctl
   runtime itself (agent policy enforcement, overseer inbox routing, session
   lifecycle events, knowledge routing). They are structurally part of the
   runtime, not user-facing tooling.

4. **Storage-layer writes.** Directly reads from or writes to foxctl's SQLite
   stores, CAS, graph database, trajectory database, or annotation stores.
   The storage schema is not a stable interface.

5. **Foxctl-meta purpose.** Self-manages the foxctl installation or skill
   runtime (setup, inspection, agent handbooks).

6. **Core pipeline role.** Participates in a foxctl-internal pipeline
   (embedding workers, summary workers, optimization feedback loops, todo
   sync, plan sync).

---

## Externalization Criteria

A skill is a clean extraction candidate when **all** of the following hold:

1. **Shallow coupling.** Only imports `internal/adapters/skillslib/*`
   (skillmain, skillout, skillerr, executil, workspaceutil, etc.) plus at
   most `internal/platform/{errors,config}`. No intelligence, context,
   storage, runtime, or domain imports.

2. **Domain-agnostic.** Provides a capability useful for any project, not
   just foxctl. Improves the *user's workflow* but not the *harness's
   intelligence or memory*.

3. **External-API-backed.** Primarily talks to a third-party service (Jira,
   Ardoq, Reddit, X/Twitter, Facebook, Instagram, YouTube, OpenAPI servers,
   Unity, Godot). The skill's value is the integration, not foxctl-specific
   logic.

4. **Stateless or self-managed state.** Does not write to foxctl's
   SQLite/CAS stores. If it writes anything, it writes to the workspace
   filesystem or returns data in the envelope.

---

## Keep-Core List (98 skills)

### Intelligence/Retrieval Skills (30)

These skills are the primary consumers of `internal/intelligence/*` and
co-evolve with the retrieval engine, repo index, and code evidence pipelines.

| Skill | Key internal deps | Notes |
|-------|-------------------|-------|
| `code_dag_grep` | repoindex, repoquery | DAG traversal over repo index |
| `code_semantic_search` | indexing (7+), retrieval, contextplane, turbovec | Largest skill (4385 LoC). Deepest intelligence coupling. |
| `code_smart_search` | codecontext, retrieval, indexing/semantic | Fused search entrypoint |
| `code_smart_read` | codecontext, indexing/semantic | Smart file reading |
| `code_counsel` | codecontext (3 packages), indexing/semantic | LLM-backed code counseling |
| `code_snippet_extract` | codecontext, indexing/semantic, observability | Evidence extraction |
| `code_context_grep` | tooling/tools/ripgrep | Contextual grep |
| `code_context_ripgrep` | tooling/tools/ripgrep | Ripgrep-backed context extraction |
| `repo_index_build` | protocol | Repo index construction |
| `repo_index_dag_grep` | repoindex, repoquery | Index-backed DAG grep |
| `repo_index_enrich_summaries` | protocol | Summary enrichment |
| `repo_index_expand` | repoindex, repoquery | Index expansion |
| `repo_index_open` | repoindex, repoquery | Index opening/loading |
| `repo_index_search` | repoindex, repoquery | Index search |
| `code_incremental_index` | indexing (6 packages), storage | Incremental indexing pipeline |
| `code_symbols` | indexing/symbol, domain/policy, storage/cas | Symbol extraction |
| `code_complexity` | domain/policy, storage/cas | Complexity metrics |
| `code_branch_impact` | branchimpact, repoindex, turbovec, searchindex | Branch change impact |
| `code_refactor_impact` | repoindex, refactor/impact, turbovec | Refactor impact analysis |
| `code_refactor_scout` | repoindex, refactor (4 packages), indexing/symbol | Largest skill (5155 LoC). CGO-conditional multi-language. |
| `codemap_generate` | codemap, domain/skill, runtime/observability | Codemap generation |
| `codemap_check` | codemap, storage/memory | Codemap validation |
| `codemap_get` | codemap, storage | Codemap retrieval |
| `codemap_list` | codemap, indexing/semantic, storage | Codemap listing |
| `codemap_import` | codemap, storage/memory | Codemap import |
| `code_greenlight` | domain/policy, skillslib/ci, verification | Quality gate decision |
| `cove_verify` | domain/envelope, intelligence/verification, providers/llm | Claim verification |
| `rlm_query` | domain/envelope, protocol | RLM runtime query wrapper |
| `code_refactor_advisor` | domain/skill, verification, tooling/skillrun | Refactor advisory |
| `code_stats` | domain/envelope, platform/config | Code statistics |

### Session/Context/Memory Skills (26)

These skills manage foxctl's session lifecycle, memory, calibration, and
embedding pipelines. They write to foxctl's SQLite stores and share mutable
state with the context engine.

| Skill | Key internal deps | Notes |
|-------|-------------------|-------|
| `session_save` | sessionkit (3 packages), tasksgraph, storage | Session persistence |
| `session_restore` | calibration, memorycore, sessionkit/snapshot, indexing/semantic | Session restoration |
| `session_query` | indexing/semantic, storage/sessions, storage/annotations | Session search |
| `session_recall` | indexing/semantic, storage/sessions, storage/annotations | Session recall |
| `session_anchor` | sessionkit, storage/memory | Session anchoring |
| `session_annotate` | sessionkit/claudejsonl, indexing/semantic, storage/annotations | Session annotation |
| `session_archive` | sessionkit (3 packages), indexing/semantic | Session archival |
| `session_capture` | sessionkit (2 packages), storage/sessions | Session capture |
| `session_expand` | storage | Session context expansion |
| `session_deepdive` | sessionkit/archive, storage/sessions | Deep session analysis |
| `session_export_dspy` | storage/sessions | DSPy export |
| `session_extract_learnings` | indexing/atomic, indexing/semantic, providers/llm | Learning extraction |
| `session_feedback` | sessionkit, storage/memory | Session feedback |
| `session_summarize` | sessionkit/codexjsonl, indexing/semantic, providers/llm | Summarization (3575 LoC) |
| `session_timeline` | storage/memory, storage/sessions | Timeline reconstruction |
| `session_turns` | storage/sessions | Turn extraction |
| `embedding_memories` | indexing (4 packages), storage/memory | Memory embedding |
| `embedding_refresh` | indexing/semantic, storage (3 packages) | Embedding refresh |
| `embedding_queue` | domain/envelope, domain/policy, indexing (3 packages) | Queue management |
| `embedding_tasks` | sessionkit, indexing/semantic, storage (2 packages) | Task embedding |
| `embedding_worker` | indexing (3 packages), turbovec, storage (2 packages) | Embedding worker (1157 LoC) |
| `memory_query` | contextengine, memorycore, indexing/semantic, runtime/observability | Memory search |
| `memory_curator_report` | contextengine, memorycore, runtime/observability | Curator reporting |
| `calibration_get` | calibration, runtime/hooks | Calibration retrieval |
| `calibration_generate` | calibration, providers/llm, storage/sessions | Calibration generation |
| `calibration_feedback` | calibration | Calibration feedback |

### Hook Skills (16)

Hook skills extend the foxctl runtime at lifecycle events. They are
structurally part of the agent runtime, not user-facing tooling.

| Skill | Key internal deps | Notes |
|-------|-------------------|-------|
| `hooks_bash_guard` | runtime/agentpolicy, runtime/hooks | Bash command policy enforcement |
| `hooks_overseer_inbox` | sessionkit, domain/agent, runtime/hooks, storage/blackboard | Overseer inbox routing |
| `hooks_mail_router` | sessionkit, domain/agent, runtime/hooks, storage/blackboard | Mail routing |
| `hooks_stop_guard` | sessionkit, domain/agent, runtime/hooks, storage/blackboard, storage/testwatch | Stop guard |
| `hooks_session_end` | sessionkit, runtime hooks, storage/graph, storage/memory | Session end processing |
| `hooks_subagent_start` | runtime/agentpolicy, runtime/hooks | Subagent start |
| `hooks_subagent_stop` | runtime/hooks | Subagent stop |
| `hooks_context_drain` | storage/contextbuffer | Context buffer drain |
| `hooks_context_enqueue` | storage/contextbuffer | Context buffer enqueue |
| `hooks_dispatch` | domain/envelope, runtime/hooks, storage/contextbuffer | Dispatch routing |
| `hooks_file_guard` | sessionkit, domain/agent, domain/envelope, storage/blackboard | File access guard |
| `hooks_task_guard` | contextengine, contextplane, sessionkit, domain/envelope | Task guard |
| `hooks_impact_analysis` | platform/lsp/gopls, runtime/hooks | Impact analysis at hook time |
| `hooks_knowledge_router` | sessionkit, runtime/hooks (2 packages), storage/knowledge | Knowledge routing |
| `hooks_test_feedback` | sessionkit, runtime/hooks, storage/testwatch | Test feedback |
| `hooks_bash_guard` | runtime/agentpolicy, runtime/hooks | (duplicate entry for completeness) |

### Optimization/Todo/Planning Skills (12)

These skills participate in foxctl's optimization feedback loops and task
planning pipelines.

| Skill | Key internal deps | Notes |
|-------|-------------------|-------|
| `optimize_analyze` | storage/trajectory | Trajectory analysis |
| `optimize_bootstrap` | agent/optimization, storage/trajectory | Optimization bootstrap |
| `optimize_feedback` | agent/optimization, storage/trajectory | Optimization feedback |
| `optimize_from_feedback` | storage/memory | Feedback-driven optimization |
| `optimize_patterns` | agent/optimization, storage/trajectory | Pattern analysis |
| `optimize_reflect` | agent/optimization, storage/trajectory | Optimization reflection |
| `optimize_weights` | agent/optimization, storage/trajectory | Weight tuning |
| `todo` | domain/agent, domain/envelope, overseer, tasksgraph, storage (5 packages) | Todo management (2177 LoC) |
| `todo_continuation` | sessionkit, domain/envelope, tasksgraph, storage (2 packages) | Continuation planning |
| `todo_sync_from_provider` | context/todosync, providers/claude/todos | Provider sync |
| `todo_sync_to_provider` | context/todosync, providers/claude/todos | Provider sync |

### Core Infrastructure Skills (14)

These skills manage foxctl's storage, graph, CAS, and runtime infrastructure.

| Skill | Key internal deps | Notes |
|-------|-------------------|-------|
| `plan_sync` | sessionkit, indexing/atomic, storage/memory, storage/plans | Plan synchronization |
| `epic_complete` | sessionkit, storage, storage/memory, storage/tasks | Epic completion |
| `trajectory_export` | platform/secrets, storage/trajectory | Trajectory export |
| `graph` | storage/graph | Graph operations |
| `graph_cleanup` | storage/graph | Graph cleanup |
| `graph_pagerank` | storage/graph | PageRank computation |
| `setup_install` | skillslib-only | Foxctl setup/install |
| `setup_foxctl_mode` | skillslib-only | Mode configuration |
| `skill_inspect` | skillslib-only | Skill inspection |
| `agent_handbook` | runtime/agentpolicy | Agent profile briefings |
| `mailbox` | domain/agent, domain/envelope, storage/blackboard, storage/teams | Agent mailbox |
| `context_filter` | platform/env | Context filtering |
| `quality_gate` | skillslib-only | Quality gate checks |
| `summary_worker` | sessionkit/summary, providers/llm, storage/queue, storage/sessions | Summary pipeline worker |

---

## Extraction-Candidate List (64 skills)

### Pack: `foxctl-skills-social` (5 skills)

| Skill | Coupling | Notes |
|-------|----------|-------|
| `social_reddit_collect` | `providers/social` | Reddit API collection |
| `social_x_collect` | `providers/social` | X/Twitter collection |
| `social_facebook_collect` | `providers/social` | Facebook collection |
| `social_instagram_collect` | `providers/social` | Instagram collection |
| `social_youtube_collect` | `providers/social` | YouTube collection |

**Extraction package**: Move `internal/providers/social` into the pack module.
All five skills are thin wrappers (~30-50 LoC each) over the shared social
provider client.

### Pack: `foxctl-skills-jira` (2 skills)

| Skill | Coupling | Notes |
|-------|----------|-------|
| `jira_board` | `interfaces/jira` | Jira board operations |
| `jira_issue` | `interfaces/jira` | Jira issue operations |

**Extraction package**: Move `internal/interfaces/jira` into the pack module.

### Pack: `foxctl-skills-ardoq` (1 skill)

| Skill | Coupling | Notes |
|-------|----------|-------|
| `ardoq_resource` | `interfaces/ardoq` | Ardoq resource management (1450 LoC) |

**Extraction package**: Move `internal/interfaces/ardoq` into the pack module.

### Pack: `foxctl-skills-game-engine` (9 skills)

| Skill | Coupling | Notes |
|-------|----------|-------|
| `build_unity` | skillslib-only | Unity CLI build orchestration |
| `build_godot` | `platform/errors` | Godot CLI build |
| `editor_godot` | `platform/errors` | Godot editor integration (3 Go files) |
| `unity_input` | skillslib-only | Unity input management |
| `unity_packages` | skillslib-only | Unity package operations |
| `unity_scenes` | skillslib-only | Unity scene operations |
| `mobile` | skillslib-only | Mobile project scaffolding |
| `mobile_android` | `platform/errors` | Android build/deploy |
| `mobile_ios` | `platform/errors` | iOS build/deploy |

**Extraction package**: Pure skillslib coupling. Extract as-is.

### Pack: `foxctl-skills-presence` (4 skills)

| Skill | Coupling | Notes |
|-------|----------|-------|
| `presence_background` | `platform/config` | Background presence generation |
| `presence_character` | skillslib-only | Character definition |
| `presence_orchestrate` | skillslib-only | Presence bundle coordination |
| `presence_parse` | skillslib-only | Presence parsing |
| `presence_voice` | `platform/config` | Voice configuration |

**Extraction package**: Near-zero coupling. Companion-specific but foxctl-agnostic.

### Pack: `foxctl-skills-heartwood` (2 skills)

| Skill | Coupling | Notes |
|-------|----------|-------|
| `heartwood_action` | skillslib-only | Heartwood action execution |
| `heartwood_state` | skillslib-only | Heartwood state management |

**Extraction package**: Skillslib-only. Domain-specific integration.

### Pack: `foxctl-skills-text` (5 skills)

| Skill | Coupling | Notes |
|-------|----------|-------|
| `text_grep` | `platform/config` | Text grep |
| `text_ripgrep` | `tooling/tools/ripgrep` | Ripgrep text search |
| `text_replace` | `platform/config` | Text replacement |
| `data_jq` | `platform/config` | jq-style JSON transforms |
| `json_transform` | `platform/config`, `platform/errors` | JSON manipulation |

**Extraction package**: Requires either vendoring the ripgrep tooling wrapper
or promoting `internal/tooling/tools/ripgrep` to a public package.

### Pack: `foxctl-skills-web` (3 skills)

| Skill | Coupling | Notes |
|-------|----------|-------|
| `web_extract` | skillslib-only | URL content extraction |
| `web_search` | skillslib-only | Web search |
| `exa` | skillslib-only | Exa search API |

**Extraction package**: Pure skillslib coupling.

### Pack: `foxctl-skills-ci` (2 skills)

| Skill | Coupling | Notes |
|-------|----------|-------|
| `ci_checks` | skillslib-only | CI check orchestration |
| `ci_prcomments` | skillslib-only | PR comment management (1317 LoC) |

**Extraction package**: Skillslib-only. CI integration logic.

### Misc Candidates (Case-by-Case, ~15 skills)

These have varying coupling depths and need individual evaluation.

| Skill | Coupling | Recommendation | Notes |
|-------|----------|----------------|-------|
| `arxiv_summarize` | `platform/errors`, `storage/sqliteutil` | Extract | Only needs a small sqlite helper |
| `cloud_localstack_blueprint` | skillslib-only | Extract | LocalStack scaffolding |
| `code_git` | skillslib-only | Extract | Git operations wrapper |
| `code_diff` | `platform/config`, `platform/errors` | Extract | Diff computation |
| `code_imports` | `platform/config`, `platform/errors` | Extract | Import analysis |
| `code_security` | `platform/config`, `platform/errors` | Extract | Security scanning |
| `code_smart_write` | `platform/config`, `platform/errors` | Extract | Smart file writing |
| `code_llm_search` | skillslib-only | Extract | LLM-backed search |
| `expo` | skillslib-only | Extract | Expo/React Native integration (1011 LoC) |
| `http_openapi` | `interfaces/openapi` (5 packages) | **Keep core** | Deep OpenAPI client integration |
| `launch_praze_pipeline` | skillslib-only | Extract | Project-specific scaffolding (2599 LoC) |
| `lsp_gopls` | `platform/lsp/gopls`, `storage/cas` | Borderline | LSP client could be externalized |
| `lsp_pylsp` | `platform/lsp/jsonrpc` | Extract | Python LSP wrapper |
| `lsp_tsserver` | `platform/lsp/jsonrpc` | Extract | TypeScript LSP wrapper |
| `mcp_bridge` | `platform/errors` | Extract | MCP bridge |
| `mcp_install` | `platform/errors` | Extract | MCP installation |
| `obs_logs` | `platform/errors`, `runtime/observability` | Borderline | Uses runtime observability |
| `obs_replay` | `runtime/observability`, `storage/trajectory` | **Keep core** | Writes to trajectory store |
| `providers` | skillslib-only | Extract | Provider listing (1023 LoC) |
| `test_run` | skillslib-only | Extract | Test runner |
| `x402_payment` | skillslib-only | Extract | x402 payment protocol (1050 LoC) |
| `git_status` | skillslib-only | Extract | Git status wrapper |
| `git_worktree` | `domain/policy` | Borderline | Needs policy for workspace rules |
| `html_edit` | skillslib-only | Extract | HTML editing |
| `fs_apply_edit` | `domain/envelope` | Borderline | Needs envelope contract |
| `wasi_echo` | embedded assets only | **Keep core** | Reference WASI skill; embedded in `assets.go` |

---

## Phase 0: Prerequisites

Before any skill is extracted, the following infrastructure must be in place.

### 0.1 Promote `skillslib-core`

The `internal/adapters/skillslib/*` subtree must become a versioned, publicly
importable package. Two options:

**Option A: Public subdirectory** — Create `skillslib/` at the repo root with
its own `go.mod`. Skills in the core repo import it locally; external packs
import it as a normal Go module dependency.

**Option B: Separate module** — Move `skillslib` to its own repository
(`github.com/joshka0/foxctl-skillslib`). Foxctl core and all packs import it
as a standard Go module.

Recommendation: Start with **Option A** (subdirectory with `go.mod` + `go
workspaces`). Migrate to Option B later if the pack ecosystem grows large
enough to warrant it.

### 0.2 Audit skillslib dependency graph

`skillslib/*` currently has 36+ sub-packages. Some of them transitively import
from `internal/storage/*`, `internal/context/*`, and `internal/runtime/*`:

| skillslib package | Transitive internal deps | Classification |
|-------------------|--------------------------|----------------|
| `skillmain`, `skillout`, `skillerr` | `domain/envelope`, `platform/config` | Core (stable) |
| `executil`, `workspaceutil` | `platform/config` | Core (stable) |
| `inlineutil` | `domain/envelope` | Core (stable) |
| `oputil`, `memoryutil` | `storage/*`, `context/*` | **Runtime** — not for external packs |
| `fsutil` | `platform/config` | Core (stable) |
| `gitutil`, `diffutil`, `editutil` | `platform/errors` | Core (stable) |
| `rgutil` | `tooling/tools/ripgrep` | Core (stable) |
| `htmledit`, `textreplace`, `textmatch` | none | Core (stable) |
| `secretutil`, `hashutil` | `platform/secrets` | Core (stable) |
| `hookutil` | `runtime/hooks` | **Runtime** — not for external packs |
| `lsp`, `langutil` | `platform/lsp/*` | Core (stable) |
| `symbolutil` | `platform/symbolutil` | Core (stable) |
| `mathutil`, `sliceutil`, `stringutil`, `textutil` | none | Core (stable) |
| `ci` | domain-level CI types | Core (stable) |
| `codeblocks`, `codeedit` | `platform/errors` | Core (stable) |
| `mcputil` | `platform/errors` | Core (stable) |
| `mobileutil` | `platform/errors` | Core (stable) |
| `obs` | `runtime/observability` | **Runtime** — not for external packs |

**Action**: Split skillslib into two import groups:

- `skillslib-core` — Importable by external packs. No transitive deps on
  `internal/storage`, `internal/context`, `internal/runtime`.
- `skillslib-runtime` — Only importable by core skills. May depend on storage,
  context, runtime packages.

External skill packs must only import `skillslib-core`. The Go compiler enforces
this naturally if the module boundary is correct.

Use `make skills-dependency-audit` as the starting point for this work. The
target reports both direct internal imports and transitive imports pulled in by
the current bootstrap. That distinction matters: a skill with only
`skillslib`/platform direct imports is a candidate for the future SDK even if
today's `skillmain` transitively pulls in storage, runtime, or intelligence.

### 0.3 Define skill pack manifest

A skill pack needs a machine-readable manifest so `foxctl skills install
<pack>` can discover, download, and wire skill binaries. Minimum fields:

```yaml
apiVersion: foxctl/v1
kind: SkillPack
metadata:
  name: foxctl-skills-social
  version: 0.1.0
  description: "Social media data collection skills"
skills:
  - command: social/reddit_collect
    binary: social_reddit_collect
  - command: social/x_collect
    binary: social_x_collect
  # ...
dependencies:
  skillslib: ">=0.1.0"
```

### 0.4 Establish versioning convention

- `skillslib-core` follows semver: `v0.x` during initial extraction, `v1.0`
  once the interface is proven stable.
- Skill packs pin `skillslib-core` via `go.mod` `require` directives.
- Foxctl core's `go.mod` uses a `replace` directive pointing to the local
  `skillslib/` subdirectory during development.

---

## Phased Migration Path

### Phase 1: Shallow Extraction — Social, Jira, Ardoq (8 skills)

**Why first**: These are the shallowest skills in the repo. All are thin
wrappers over external API clients. Zero intelligence, context, or storage
coupling.

**Steps**:

1. Create `github.com/joshka0/foxctl-skills-social` repository.
2. Move `skills/social_*` (5 directories) into the new repo.
3. Move `internal/providers/social` into the new repo as a shared internal
   package.
4. Add `go.mod` with `require github.com/joshka0/foxctl-skillslib-core`.
5. Create `github.com/joshka0/foxctl-skills-jira` and move `skills/jira_*`
   plus `internal/interfaces/jira`.
6. Create `github.com/joshka0/foxctl-skills-ardoq` and move
   `skills/ardoq_resource` plus `internal/interfaces/ardoq`.
7. Remove the 8 skill directories and 3 internal packages from foxctl core.
8. Update foxctl's Makefile to remove extracted skills from race shards.
9. Add CI to each pack repo: `go build ./...`, `go test ./...`.
10. Update `foxctl skills install` to support pack-based installation.

**CI impact**: Removes ~8 skill directories from 3 race shards. New pack repos
add 3 independent CI pipelines.

**Verification**: After installing all three packs, run:
```bash
foxctl run social/reddit_collect --input '{"operation":"subreddit_about","subreddit":"golang"}'
foxctl run jira/board --input '{"operation":"list"}'
```

### Phase 2: Game Engine + Presence + Heartwood (15 skills)

**Why second**: Skillslib-only coupling. No intelligence, context, or storage
deps. Straightforward extraction after Phase 0 is in place.

**Steps**:

1. Create `github.com/joshka0/foxctl-skills-game-engine` with 9 Unity/Godot/mobile skills.
2. Create `github.com/joshka0/foxctl-skills-presence` with 4 presence skills.
3. Create `github.com/joshka0/foxctl-skills-heartwood` with 2 heartwood skills.
4. Update Makefile and CI sharding.

**CI impact**: Removes ~15 skill directories. Foxctl's skills-p-x shard becomes
significantly lighter.

### Phase 3: Text/Data + Web + CI Packs (10 skills)

**Why third**: Requires promoting `internal/tooling/tools/ripgrep` to a public
package or vendoring it. The ripgrep wrapper is small and stable enough to
extract.

**Steps**:

1. Promote `internal/tooling/tools/ripgrep` to `skillslib-core` or a separate
   shared module.
2. Create `github.com/joshka0/foxctl-skills-text` with 5 text/data skills.
3. Create `github.com/joshka0/foxctl-skills-web` with 3 web skills.
4. Create `github.com/joshka0/foxctl-skills-ci` with 2 CI skills.
5. Update Makefile and CI sharding.

**CI impact**: Removes ~10 skill directories.

### Phase 4: Misc Candidates (Case-by-Case)

Evaluate each misc candidate individually. Priority order:

1. **Easy extractions**: `code_git`, `code_diff`, `code_imports`, `code_security`,
   `code_smart_write`, `cloud_localstack_blueprint`, `test_run`, `html_edit`,
   `mcp_bridge`, `mcp_install`, `providers`, `x402_payment`, `code_llm_search`.
2. **Borderline** (need small interface work): `git_worktree`, `fs_apply_edit`,
   `lsp_gopls`, `lsp_pylsp`, `lsp_tsserver`.
3. **Keep core**: `http_openapi`, `obs_replay`, `wasi_echo`, `obs_logs`.

---

## CI and Test Impact

### Current CI Layout

```
foxctl CI
├── race-tests-skills-a-g   (skills/a through skills/g)
├── race-tests-skills-h-o   (skills/h through skills/o)
├── race-tests-skills-p-x   (skills/p through skills/x)
├── skills-build            (build all skills)
├── skills-build-impacted   (build only changed skills)
└── integration tests       (end-to-end skill execution)
```

### Post-Extraction CI Layout

```
foxctl CI (core)
├── race-tests-skills-a-g   (reduced: intelligence + session + context skills)
├── race-tests-skills-h-o   (reduced: hooks + memory + optimization skills)
├── race-tests-skills-p-x   (reduced: remaining core skills)
├── skills-build            (build core skills only)
├── skills-build-impacted   (build only changed core skills)
├── pack-integration-smoke  (verify installed packs are loadable)
└── integration tests

foxctl-skills-social CI
├── go build ./...
├── go test ./...
└── foxctl-pack-lint

foxctl-skills-jira CI
├── go build ./...
├── go test ./...
└── foxctl-pack-lint

(etc. for each pack)
```

### Smoke Test Strategy

Add a `make pack-smoke` target to foxctl core CI that:

1. Installs each published pack via `foxctl skills install <pack>`.
2. Runs `foxctl skills list` and verifies expected skills appear.
3. Runs `foxctl run <skill> --input '{"dry_run":true}'` for each installed
   skill (where supported).
4. Does NOT run full functional tests — those live in the pack repos.

### Race Shard Rebalancing

After Phases 1-3, the 3 skill shards will be unbalanced. Rebalance by
redistributing the remaining ~98 core skills:

| Shard | Post-extraction scope |
|-------|-----------------------|
| `skills-code-a-g` | `code_*` skills (intelligence/retrieval) |
| `skills-hooks-mem` | `hooks_*`, `session_*`, `memory_*`, `embedding_*`, `calibration_*` |
| `skills-infra` | `codemap_*`, `repo_index_*`, `optimize_*`, `todo_*`, `graph_*`, other infra |

---

## API and Versioning Seams

### Seam 1: skillslib-core vs skillslib-runtime

This is the most critical seam. The boundary must be enforced at the Go module
level so external packs cannot accidentally import runtime-coupled packages.

**Contract**: `skillslib-core` exports:

- `skillmain.Main()` and `skillmain.RunContext`
- `skillout.Emit()`, `skillout.EmitWithCAS()`
- `skillerr.*` error types
- `executil.*` process execution helpers
- `workspaceutil.*` workspace resolution
- `inlineutil.*` inline output helpers
- `domain/envelope` types (Envelope, Data, Meta)
- `platform/config` for workspace configuration
- `platform/errors` for error construction

**Not exported** (skillslib-runtime only):

- `memoryutil.*` (writes to foxctl memory stores)
- `oputil.*` (writes to foxctl operation stores)
- `hookutil.*` (accesses runtime hooks)
- `obs.*` (accesses runtime observability)

### Seam 2: CAS Write Path

`skillout.EmitWithCAS()` writes to foxctl's Content-Addressable Storage. If
the CAS schema changes, external packs that use this function break.

**Mitigation**: Treat the CAS write path as a stable interface within
`skillslib-core`. The CAS blob format (SHA-256 digest, JSON envelope wrapping)
is versioned and must not change without a major version bump.

### Seam 3: Envelope Contract

The JSON envelope (`version`, `status`, `command`, `data`, `meta`, `error`)
is the wire protocol between skill binaries and the foxctl runtime. This
contract is already treated as stable per AGENTS.md.

**Mitigation**: External packs must produce envelopes conforming to the same
schema. Embed a schema validator in `skillslib-core` that catches envelope
shape violations at build time.

### Seam 4: skill.yaml Manifest

The `apiVersion: foxctl/v1`, `kind: Skill`, `signature.command`,
`distribution.type: exec` fields form the install-time contract. Pack-level
manifests must conform to the same schema.

**Mitigation**: Add a `foxctl-pack-lint` tool that validates pack manifests
against the skill JSON schema.

### Seam 5: Go Module Versioning

| Module | Version strategy |
|--------|-----------------|
| `foxctl` (core) | Continue current versioning |
| `foxctl-skillslib-core` | `v0.x` during extraction, `v1.0` when stable |
| `foxctl-skills-*` packs | Independent semver; `require` skillslib-core |

Packs must not import `foxctl` core directly. They may only import
`foxctl-skillslib-core`.

---

## Risks

### R1: skillslib dependency graph is not clean

Some `skillslib` packages transitively import storage, context, or runtime
packages. If external packs pull in the entire foxctl module via `go.mod`, the
extraction is pointless.

**Mitigation**: Audit the full dependency graph of every skillslib sub-package
before Phase 1. Split skillslib into core/runtime as described in Phase 0.2.

### R2: CI fragmentation

Moving skills to separate repos increases the total number of CI pipelines.
A change to `skillslib-core` triggers rebuilds across all pack repos.

**Mitigation**: Start with few packs (3 in Phase 1). Only split further when
the core repo's CI is measurably faster. Use Go module proxy caching to avoid
re-downloading unchanged dependencies.

### R3: Version skew

If `skillslib-core` v0.2 breaks an interface that a pack depends on, the pack
fails at build time. This is better than a runtime failure but still disrupts
development.

**Mitigation**: Pin `skillslib-core` versions in pack `go.mod` files. Use
`go work` during development to test against the latest skillslib. Never
release a breaking skillslib change without updating all packs simultaneously.

### R4: Storage writes from disguised shallow skills

Some "shallow" skills actually write to foxctl stores through skillslib helpers
(e.g., `skillout.EmitWithCAS` writes to CAS). If the CAS schema changes,
external packs break.

**Mitigation**: The CAS write path is part of `skillslib-core`'s stable
interface. Schema changes require a major version bump.

### R5: code_refactor_scout is too large for skills/

At 5155 LoC with CGO-conditional multi-language support, this skill is
arguably too large to live in `skills/`. It should not be extracted — it's
deeply coupled to intelligence internals — but it should be refactored.

**Mitigation**: Move the intelligence-heavy logic into
`internal/intelligence/refactor/scout` packages. Leave the skill binary as a
thin entrypoint that calls the internal package.

### R6: Embedded assets

`skills/assets.go` embeds `wasi_echo/skill.yaml` and `wasi_echo/module.wasm`
via `//go:embed`. If `wasi_echo` moves to an external pack, the build breaks.

**Mitigation**: Keep `wasi_echo` in core as the reference WASI skill.

---

## Recommended Next Issues

| Priority | Issue | Effort | Impact |
|----------|-------|--------|--------|
| 1 | Run `make skills-dependency-audit` and use it to audit `skillslib/*` dependency graph, identifying runtime-coupled packages | 1 day | Prerequisite for all extraction |
| 2 | Create `skillslib-core` / `skillslib-runtime` split (go.work) | 2-3 days | Enables external packs |
| 3 | Define `SkillPack` manifest schema and `foxctl skills install <pack>` | 2 days | Pack installation contract |
| 4 | Extract `foxctl-skills-social` (5 skills + `providers/social`) | 1 day | First pack, proves the model |
| 5 | Extract `foxctl-skills-jira` (2 skills + `interfaces/jira`) | 0.5 day | Second pack |
| 6 | Extract `foxctl-skills-ardoq` (1 skill + `interfaces/ardoq`) | 0.5 day | Third pack |
| 7 | Add `make pack-smoke` CI target to foxctl core | 1 day | CI safety net |
| 8 | Rebalance race-test shards after Phase 1 | 0.5 day | CI optimization |
| 9 | Extract game-engine + presence + heartwood packs (Phase 2) | 2 days | Bulk size reduction |
| 10 | Refactor `code_refactor_scout` to thin entrypoint | 2-3 days | Maintainability |

**Total estimated effort for Phases 0-1**: ~8-10 working days.

**Expected core repo size reduction after Phases 1-3**: ~40 skills removed,
~20K LoC removed, 3 CI jobs lightened by ~25%.
