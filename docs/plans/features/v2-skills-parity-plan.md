---
vault_refs:
  - notes/repo/agentctl/skills-runtime-wiring.md
  - notes/repo/agentctl/packages/internal-adapters-skillslib-skillerr.md
  - notes/repo/agentctl/packages/internal-adapters-skillslib-skillmain.md
  - notes/repo/agentctl/packages/internal-adapters-skillslib-skillout.md
  - notes/repo/agentctl/packages/cmd-agentctl-cmd.md
---
# V2 Skills Parity Plan

Status: In Progress  
Owner: Solo maintainer  
Last Updated: 2026-03-12

## Goal

Bring the v2 skill/tool model up to date with the recent ACA, Obsidian, retrieval-v2, and runtime changes already present in the repo.

This is not a “convert manifests from v1 to v2” plan.
The real problem is that the **classic runtime tool surface** has moved ahead of the **v2 profile/catalog/executor model**.

## Current State

### Classic runtime already exposes

- ACA read-only tools:
  - `context_show`
  - `context_retrieve`
- Obsidian read-only tools:
  - `obsidian_index_search`
  - `obsidian_read`
  - `obsidian_related`
- Heartwood project-local tools:
  - `heartwood_state`
  - `heartwood_action`
- expanded repo-index retrieval path:
  - `repo_index_search`
  - `repo_index_expand`
  - `repo_index_open`
  - `repo_index_dag_grep`

### V2 currently has

- a generic tool catalog/executor framework under `internal/v2/runtime/tools`
- profile allowlists under `internal/v2/runtime/profiles`
- a canonical non-test `ToolDef` source under `internal/v2/runtime/tools/default_defs.go`
- a real bridge delegate under `internal/v2/adapters/toolbridge`
- a canonical default runtime builder under `internal/v2/services/default_runtime.go`
- Jido-side allowlist derivation from the same v2 catalog/profile model under `internal/v2/adapters/jido/tool_exec.go`

So the gap is:

1. docs/profile parity
2. actual tool-definition + delegate parity
3. wider adoption of the new default builder across live v2 runtime entrypoints
4. live Jido payload adoption of the shared catalog/profile tool model

## Non-goals

- Do not redesign v2 process profiles from scratch in the first slice.
- Do not add write-side Obsidian tools to v2 until dry-run / plan-apply behavior is nailed down.
- Do not try to convert every existing `skills/*` manifest into a new v2 manifest system.

## Phase 1: Governance and Allowlist Parity

### Goal

Make the v2 docs and default profile allowlists acknowledge the read-only ACA/Obsidian surfaces already used in practice.

### Changes

- update `docs/spec/v2_repo_rules_and_skills.md`
- update `internal/v2/runtime/profiles/profiles.go`
- add/extend tests in `internal/v2/runtime/profiles/profiles_test.go`

### Acceptance

- overseer profile includes read-only ACA/Obsidian tools
- companion profile includes read-only ACA/Obsidian tools
- docs explicitly distinguish portable core tools from project-local adjunct tools like Heartwood

## Phase 2: Tool Definition Source for V2

### Goal

Stop relying on test-only `ToolDef` construction in v2.

### Changes

- define one real source of v2 `core/tool.ToolDef` values
- make the source canonical and deterministic
- map slash/dot/underscore aliases only at boundaries

### Likely implementation options

1. derive `core/tool.ToolDef` from a shared registry layer
2. build an explicit v2 catalog in `internal/v2/runtime/tools`
3. bridge selected classic-runtime tool defs into v2 in a controlled way

### Acceptance

- v2 runtime can enumerate real tool defs without tests fabricating them
- profile allowlists have something concrete to filter
- one production builder exists for assembling the default catalog + delegate + executor stack

## Phase 3: Delegate Parity

### Goal

Make the v2 tool executor actually run the newer read-only ACA/Obsidian surfaces, not just allowlist their names.

### Changes

- wire delegates for:
  - `context/show`
  - `context/retrieve`
  - `obsidian/index_search`
  - `obsidian/read`
  - `obsidian/related`
- decide whether these should call:
  - the existing CLI surfaces, or
  - shared internal packages directly

### Acceptance

- v2 tool execution path can run the same read-only ACA/Obsidian retrieval tasks the classic runtime can already run
- at least one non-test service-level path can execute those tools through the real v2 runner

## Phase 4: Search Skill Alignment

### Goal

Align the “v2 skills” story with the newer retrieval stack instead of the older `internal/retrieval` mental model.

### Current Eval Findings

Recent eval work on the `agentctl` clean vault shows two important things:

1. `code/semantic_search` with the explicit ACA-backed `context` scope is materially better than the current default skill path for knowledge-oriented repo questions.
2. That is not enough evidence to make `context` part of the unconditional default yet, because the current default semantic-search path still underperforms badly on implementation-flow queries and needs its own quality work.

Practical decision for now:

- keep `scope:["context"]` additive and explicit
- keep measuring it with retrieval evals
- improve the default code-oriented path before changing the default search story

Reference eval shapes now in use:

- `testdata/evals/retrieval/agentctl.yaml`
- `testdata/evals/retrieval/agentctl-mixed.yaml`
- `testdata/evals/retrieval/praze.yaml`

Most recent mixed-suite comparison (`agentctl-mixed`) after stabilizing the query-time search path:

- `skill_default`: `hit@5 0.86`, `MRR 0.71`
- `skill_context`: `hit@5 0.86`, `MRR 0.79`
- `skill_default_plus_context`: `hit@5 1.00`, `MRR 0.76`

Interpretation:

- the default code-oriented path is now competitive again on implementation-flow queries
- `context` remains valuable
- `default + context` currently gives the strongest overall recall on the mixed suite

That is stronger evidence for eventually blending `context` into the default search story, but it is still worth keeping the rollout repo-aware until more suites beyond `agentctl` are measured.

Current implementation direction:

- `code/semantic_search` reads an optional workspace-local ACA retrieval policy key:
  - `semantic_search_default_scopes`
- `agentctl` can opt into default `context` through that workspace-local policy
- `praze` can stay code-first by leaving that policy unset

### Surfaces to review

- `skills/code_semantic_search`
- `skills/code_smart_search`
- `skills/code_smart_read`
- `skills/code_counsel`
- `skills/code_snippet_extract`

### Checks

- use `internal/searchquery`, `internal/searchindex`, `internal/retrieval/v2`, and `internal/codecontext` consistently
- remove stale assumptions from docs/specs that still describe the old retrieval split
- keep result contracts stable unless versioned explicitly

## Phase 5: Plugin / Extension Tool Families

### Goal

Decide how repo-specific tools like Heartwood should appear in v2 without polluting the portable core tool set.

### Decision points

- keep them out of default portable v2 profiles
- define an explicit plugin / extension registration path
- allow repo-specific profile overlays to opt them in intentionally

### Acceptance

- Heartwood tools are explicitly documented as plugin / extension tools
- default portable v2 profiles do not assume Heartwood exists
- repo-specific overlays or plugin registration can opt them in intentionally

## Test Matrix

Required as parity work lands:

1. profile allowlist tests
2. tool catalog canonical-name tests
3. delegate execution tests for new v2 ACA/Obsidian surfaces
4. end-to-end runner/service tests proving one v2 execution path can use those tools

## Recommended Order

1. Phase 1: docs + allowlists
2. Phase 2: real v2 tool-definition source
3. Phase 3: ACA/Obsidian read delegate parity
4. Phase 4: retrieval/search skill alignment
5. Phase 5: Heartwood/plugin-extension policy

## Immediate Next Slice

After this document lands, the next implementation slice should be:

1. extend Jido child/runtime payloads from shared allowlists to richer shared runtime assembly where practical
2. adopt `internal/v2/services/default_runtime.go` in more live runtime/service constructors as production model wiring becomes available
3. review whether read-only ACA/Obsidian surfaces should be added to more default process profiles or remain companion/overseer-only
