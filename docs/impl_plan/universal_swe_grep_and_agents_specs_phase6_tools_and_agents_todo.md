# Phase 6 – dspy-go Tools & Agents Wiring Todo Spec

This spec breaks down Phase 6 of `universal_swe_grep_and_agents` into concrete
steps focused on **wiring the new retrieval primitives into dspy-go agents** via
kernel-owned tools, without exposing raw DBs or skills directly to LLMs.

- Phases 3–4: provide semantic file + symbol index candidates.
- Phase 5: provides the `code/swe_grep` exec skill over live files.
- Phase 6: exposes `code.symbol_search` and `code.swe_grep` as dspy-go tools and
  aligns `edit.apply_patch` with the `code/diff` skill.

> **Cross-refs**
> - Impl plan: `docs/impl_plan/universal_swe_grep_and_agents.md` (Phase 6)
> - Testing plan: `docs/impl_plan/universal_swe_grep_and_agents_testing.md` (Phase 6)
> - Specs:
>   - `docs/spec/code_symbol_index_and_swe_grep.md` (§6 agent tools)
>   - `docs/spec/dspy_go_agents.md` (tools surface)
>   - `docs/spec/core_profile_v1.md`
> - Codemaps:
>   - Overseer & hierarchy: `Agentctl Overseer & Agent Hierarchy: Spawn Protocol, Depth Limits, and Session Management`.
>   - Planning stack: `agentctl Planning LLM Stack: Auto, Providers, and Integration Tests`.
>   - Knowledge system: `agentctl Knowledge System & Factory Droids`.
>   - Agent runtime & tools: `Dspy-Go Agent Runtime & Tools Integration in agentctl`.

---

## A. Tool Surfaces & Contracts

Goal: define **dspy-go tool contracts** for `code.symbol_search` and
`code.swe_grep` that wrap existing kernel components, plus align
`edit.apply_patch` with the `code/diff` JSON diff format.

### A1. `code.symbol_search` tool contract

- [ ] Add a new dspy-go tool `code.symbol_search` in `internal/agent/tools/`:
  - Implemented as a Go helper over the symbol index per
    `code_symbol_index_and_swe_grep.md` §6.1.
  - Inputs (mirroring spec, adapted to dspy-go JSON args):
    - `workspace_id` (string, required).
    - `question` (string, required).
    - `mode` (string, optional) – `"search" | "callers" | "callees"`.
    - Optional hints: `symbol_hint` (string), `max_results` (int).
  - Outputs (conceptual tool result payload):
    - `candidates[]` with `{file, symbol_id, name, kind, score}`.
- [ ] Decide where this tool lives logically:
  - Either extend `registerCodeTools` or add a dedicated registration helper
    (e.g. `registerSymbolTools`) wired from `NewRegistry`.
- [ ] Ensure tool implementation does **not** expose raw SQL to the agent:
  - Use internal Go APIs over the symbol index (or named-memory wrappers).
  - Treat the symbol index as a kernel-internal service.

### A2. `code.swe_grep` tool contract

- [ ] Add a `code.swe_grep` tool in `internal/agent/tools/`:
  - Inputs (per `code_symbol_index_and_swe_grep.md` §6.2 / §5.2):
    - `workspace_id` (string).
    - `question` (string).
    - `candidate_files[]` with `{path, symbol_id?, priority?}`.
  - Outputs (tool result payload):
    - `snippets[]` with `file`, optional `symbol_id`, `start_line`, `end_line`,
      and `text` or truncated preview.
    - Optional `cas_artifact` reference when snippets are large.
- [ ] Implement the tool as a **thin wrapper** around the `code/swe_grep` exec
  skill:
  - Use the existing CLI/runner path (`agentctl run code_swe_grep`-equivalent)
    or direct skill invocation consistent with other skills.
  - Preserve Protocol v1 envelopes and CAS invariants (no custom transport).
  - Treat **recall and ranking** as upstream concerns (semantic file index +
    symbol index/DAG); this tool delegates only the **LLM-based snippet
    extraction** described in the Phase 5 SWE Grep spec.

### A3. `edit.apply_patch` ↔ `code/diff` JSON alignment

- [ ] Define a canonical JSON patch format based on `code/diff` output
  (`skills/code_diff/main.go`):
  - Prefer a structured diff representation (`diff.hunks[]`, paths, stats) over
    ad-hoc find/replace.
  - Keep this format **internal** to tools; do not introduce new wire-level
    protocol fields.
- [ ] Update `edit.apply_patch` tool contract in `internal/agent/tools/edit_tools.go`:
  - EITHER migrate from `{path, old_text, new_text}` to `{path, diff_json}`
    consuming `code/diff` output, OR
  - Introduce a new tool name (e.g. `edit.apply_structured_diff`) while keeping
    `edit.apply_patch` as a simple helper.
  - Update dspy-go agent instructions accordingly (see §B2).
- [ ] Document the **round-trip path** explicitly:
  - `code/diff` skill produces JSON diff.
  - Agent decides whether to apply, then calls `edit.apply_*` with that JSON.
  - This path becomes the canonical edit pipeline for Phase 6.

---

## B. Agent Roles, Signatures, and Planning Stack

Goal: ensure **Coder/Review agents** know how to use the new tools in their
ReAct loops, without changing overseer depth semantics or planning LLM
contracts.

### B1. Tool registration in Runtime

- [x] Confirm that `internal/agent/runtime/runtime.go` already:
  - Creates a tools registry via `agenttools.NewRegistry`.
  - Registers all V1 tools via `NewRegistry`→`register*Tools`.
- [ ] After adding `code.symbol_search` / `code.swe_grep` tools:
  - Ensure they are registered by default in `NewRegistry`.
  - Confirm they are visible to all agent roles that should use them
    (primarily `RoleCoder`, possibly `RoleReviewer` later).

### B2. Agent signatures and instructions

- [ ] Update `buildAgentSignature` in `runtime.go` to mention new tools in the
  **Coder** agent instruction:
  - Today: fs.read_file, fs.list_dir, edit.create_file, edit.apply_patch,
    code.search, tests.run.
  - Add: `code.symbol_search` and `code.swe_grep`, plus any
    `edit.apply_structured_diff` variant if introduced.
- [ ] Ensure planner/overseer roles remain focused on task orchestration:
  - No direct access to low-level editing tools unless explicitly desired.
  - Keep retrieval and editing capabilities primarily in Coder/Review agents.

### B3. Planning LLM stack constraints

- [ ] Verify that planning LLM integration (OpenRouter/Groq/OpenAI) used by the
  todo skill (`skills/todo`) remains **unchanged** by Phase 6:
  - `AutoPlanner` stays focused on high-level planning, not retrieval wiring.
  - No new network calls or providers are added for Phase 6.
- [ ] Add or adjust one integration test (if needed) to show that adding new
  tools does not break existing planner behavior or CI gating rules.

---

## C. Overseer, Knowledge, and Multi-Agent Orchestration

Goal: keep the **overseer hierarchy and knowledge system** aligned with the new
retrieval tools, while maintaining depth limits and advisory-only knowledge.

### C1. Overseer & hierarchy considerations

- [x] Confirm existing overseer behavior via codemap:
  - `NewOverseer` sets global `MaxDepth`, concurrency limits, and wires
    `SpawnHandler` into `Runtime`.
  - `HandleSpawnRequest` + `ValidateSpawnDepth` ensure depth limits are
    respected for child agents.
- [ ] Ensure that adding retrieval tools does **not** bypass overseer policy:
  - No new agent roles or spawn behaviors are introduced in Phase 6.
  - Any future retrieval-specialist agents (e.g., dedicated search agent) are
    considered explicitly in later phases/specs.

### C2. Knowledge system & factory droids

- [ ] Add or update knowledge items describing the retrieval funnel and new
  tools:
  - Factory droids or docs for `code.symbol_search` / `code.swe_grep` usage.
  - Keywords to help the knowledge router surface these docs when coding tasks
    mention "search", "grep", or "symbol".
- [ ] Ensure `agentctl knowledge sync` and the knowledge router hook continue to
  operate without modification to their wire contracts:
  - Changes are limited to content and keywords, not schema.

---

## D. Tests, Golden Fixtures, and Observability

Goal: validate that the **Phase 6 tools and agent wiring** behave as expected,
with unit tests, golden tests, and at least one end-to-end agent flow.

### D1. Tool unit tests (`code.symbol_search`, `code.swe_grep`)

- [ ] Add unit tests for `code.symbol_search` tool:
  - Use a small in-memory or fixture-backed symbol index.
  - Assert that queries return expected candidates (file, symbol_id, name,
    kind, score) and respect `max_results`.
- [ ] Add unit tests for `code.swe_grep` tool:
  - Stub or fixture the `code/swe_grep` skill invocation (no real LM).
  - Verify input mapping and that tool outputs match the SWE Grep contract
    (snippets, optional CAS artifact descriptor).

### D2. Patch round-trip tests

- [ ] Implement tests for the canonical round-trip:
  - Run `code/diff` skill on a known change to produce JSON diff.
  - Feed that diff into `edit.apply_*` (whichever tool consumes structured
    diffs) and assert the resulting file content matches the expected target.
  - Cover edge cases: whitespace changes, multiple hunks, large diffs that
    produce CAS artifacts.

### D3. Agent integration tests (stubbed LLM)

- [ ] Add a non-LLM or stub-LLM integration test for a Coding agent that:
  - Uses `code.symbol_search` (or `code.search`) to find locations.
  - Calls `code.swe_grep` to extract snippets.
  - Applies a small edit via `edit.apply_*`.
  - Verifies that the session completes successfully and records tool calls in
    `Session.ToolCalls`.
- [ ] If feasible, add a Planner + Coder integration test:
  - Planner creates a task and spawns a Coder via overseer.
  - Coder uses retrieval tools + edit tools to make a simple change.

### D4. Telemetry, logging, and metrics

- [ ] Ensure existing telemetry hooks (`wrapWithTelemetry` and
  `sessionRecorder.RecordToolCall`) capture Phase 6 tools:
  - Tool names `code.symbol_search`, `code.swe_grep`, and
    any new `edit.apply_*` variant appear in recorded calls.
- [ ] Consider adding simple counters/metrics (if not already present) for
  retrieval-tool usage, consistent with existing metrics infra (no new wire
  fields).

---

## Open Questions / To Discuss

- Should `edit.apply_patch` be migrated entirely to a structured diff format
  (backed by `code/diff`), or should we introduce a new tool name for
  structured diffs and keep the simple text-replace helper for small edits?
- Which agent roles (Coder, Reviewer, others) should have direct access to
  `code.symbol_search` and `code.swe_grep` in v1, and how should their
  instructions differ?
- Do we need any additional knowledge packs or factory droids specifically for
  explaining the retrieval funnel (`semantic_file_index` → symbol index → SWE
  Grep) to humans and/or agents?
