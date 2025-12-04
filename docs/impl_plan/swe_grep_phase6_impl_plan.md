## Phase 6 – dspy-go Tools & Agents Wiring (Summary)

Phase 6 wires the retrieval primitives from Phases 3–5 into **dspy-go agents** via kernel-owned tools, without ever exposing raw DBs or low-level skills directly to LLMs. It introduces `code.symbol_search` and `code.swe_grep` as tools over the symbol index + SWE Grep, and aligns `edit.apply_patch` with the structured JSON diff format from `code/diff`. Coder/Review agents get these tools in their ReAct loops while overseer depth limits, Core Profile v1 envelopes, CAS invariants, and the existing planning LLM stack remain unchanged. The goal is a clean retrieval → reasoning → edit pipeline that’s fully mediated by Go tools, not by arbitrary shelling-out or raw SQL, and is covered by unit, golden, and stub-LLM integration tests.

Below is an implementation-plan-style outline for Phase 6, mirroring the Phase 5 doc.

---

## A/B/C/D Todo Structure – Sanity Check vs Phase 6 Spec

- **Section A – Tool surfaces & contracts**  
  - A1/A2/A3 in the Phase 6 spec map directly to:
    - A new `code.symbol_search` tool in `internal/agent/tools/` over the symbol index.
    - A `code.swe_grep` tool as a thin wrapper over the `code/swe_grep` exec skill.
    - Alignment of `edit.apply_patch` (or a sibling tool) with the `code/diff` structured JSON diff format.
  - Codemaps most relevant:
    - **Skill System: Build → Discovery → Execution → Runtime**  
    - **Skill System: Discovery, Validation, and Execution**  
    - **Execution Runners: WASI & Exec with Network/FS Policy Enforcement**

- **Section B – Agent roles, signatures, and planning stack**  
  - B1/B2/B3 cover:
    - Tool registration in `agenttools.NewRegistry` / runtime wiring.
    - Updating Coder (and optionally Reviewer) agent instructions to mention the new tools.
    - Ensuring the planning LLM stack (AutoPlanner + providers) is unaffected.
  - Codemaps:
    - **Dspy-Go Agent Runtime & Tools Integration in agentctl**  
    - **Planning LLM Stack: Auto, Providers, and Integration Tests**  
    - **Agentctl Overseer & Agent Hierarchy…** (for spawn/depth semantics)

- **Section C – Overseer, knowledge, orchestration**  
  - C1 keeps overseer spawn/depth unchanged; Phase 6 does *not* add new agent types.
  - C2 updates knowledge/factory-droids to describe the retrieval funnel and new tools, but keeps knowledge contracts stable.
  - Codemaps:
    - **Agentctl Overseer & Agent Hierarchy…**  
    - **Agentctl Knowledge System & Factory Droids**  
    - **Core Profile v1: End-to-End Envelope, Jobs & CAS Flow**

- **Section D – Tests, goldens, observability**  
  - D1/D2/D3/D4 define:
    - Tool unit tests for `code.symbol_search` and `code.swe_grep`.
    - Patch round-trip tests via `code/diff` → edit tool → file content.
    - Stub-LLM or non-LLM agent integration tests using the new tools.
    - Telemetry/metrics ensuring new tools show up in tool-call logs and counters.
  - Codemaps:
    - **Core Profile v1: End-to-End Envelope, Jobs & CAS Flow**  
    - **OpenAPI Skill & Plugin Protocol…** (for “tool as wrapper over skill” patterns)

Net: the A/B/C/D spec is already solid; Phase 6 impl work is mostly *where* to put Go glue, how to phase PRs, and how to validate each slice.

---

## Proposed PRs for Phase 6 Tools & Agents

### PR 1 – Tool Surfaces: `code.symbol_search` & `code.swe_grep`

- **Scope**
  - Add `code.symbol_search` tool in `internal/agent/tools/`:
    - Defines dspy-go tool contract (args/result) matching §A1.
    - Uses internal Go APIs over the symbol index (no raw SQL or direct DB access).
  - Add `code.swe_grep` tool:
    - Wraps `code/swe_grep` exec skill via the existing skills runner / `agentctl run` path.
    - Maps Protocol v1 envelope → tool result payload (`snippets[]`, optional CAS ref).
- **Constraints**
  - No new wire fields; reuse envelopes from `code_symbol_index_and_swe_grep.md` and Core Profile v1.
  - Respect PathValidator and workspace anchoring whenever filesystem is touched.
- **Validation**
  - Unit tests for both tools with *stubbed* backends:
    - Stub symbol index responses for `code.symbol_search`.
    - Stub SWE Grep envelopes (no real LM) for `code.swe_grep`, assert mapping.

---

### PR 2 – Structured Diffs: `edit.apply_*` ↔ `code/diff` JSON

- **Scope**
  - Define canonical structured diff JSON based on `code/diff` output (`skills/code_diff/main.go`):
    - Prefer `diff.hunks[]` with `path`, `old_start`, `old_lines`, `new_lines`, etc., over ad-hoc string replace.
  - Update edit tools in `internal/agent/tools/edit_tools.go`:
    - **Decision point** from spec:
      - Either migrate `edit.apply_patch` to accept structured `diff_json`.
      - Or add `edit.apply_structured_diff` and keep `edit.apply_patch` as simple helper.
    - Implement workspace-safe application of diffs via PathValidator.
- **Validation**
  - Round-trip tests (D2):
    - Run `code/diff` on fixture changes → get JSON diff.
    - Feed that JSON into `edit.apply_*` → assert resulting file content equals target.
    - Cover whitespace-only changes, multiple hunks, and large diffs (with CAS artifacts).

---

### PR 3 – Runtime Wiring & Agent Signatures

- **Scope**
  - `internal/agent/tools/registry.go` (or equivalent):
    - Ensure `code.symbol_search` and `code.swe_grep` are registered by default in `NewRegistry`.
  - `internal/agent/runtime/runtime.go`:
    - Update `buildAgentSignature` for Coder (and maybe Reviewer) to:
      - Explicitly mention `code.symbol_search`, `code.swe_grep`, and any structured-diff edit tool.
      - Keep Overseer/Planner roles focused on orchestration, not low-level editing.
- **Constraints**
  - No changes to overseer depth limits or spawn protocol.
  - No changes to todo/LLM planner contracts (planner stays high-level).
- **Validation**
  - Unit tests asserting:
    - New tools appear in the registry and in the Coder signature.
    - Planner/overseer roles do *not* gain unexpected tools.
  - (Optional) snapshot/golden of agent signatures to detect drift.

---

### PR 4 – Knowledge & Docs for the Retrieval Funnel

- **Scope**
  - Add/update knowledge items / factory droids describing:
    - Retrieval funnel: `semantic_file_index` → symbol index (`code.symbol_search`) → SWE Grep (`code.swe_grep`) → edits via structured diffs.
    - Usage patterns and guardrails for each tool (e.g., when to prefer symbol search over raw grep).
  - Ensure `agentctl knowledge sync` & router hooks stay schema-invariant:
    - Only content/keywords change.
- **Validation**
  - Manual / small automated checks:
    - Knowledge router surfaces the new docs for queries mentioning “symbol search”, “grep”, “diff”, etc.
  - No wire-contract changes; no new tests beyond any existing knowledge sync tests.

---

### PR 5 – Tool/Agent Tests, Goldens, and Telemetry

- **Scope**
  - **D1 – Tool unit tests**
    - `code.symbol_search`: in-memory / fixture index; assert ranking, `max_results`.
    - `code.swe_grep`: stub `code/swe_grep` envelopes; assert mapping and error propagation.
  - **D2 – Patch round-trip tests**
    - Round-trip `code/diff` → edit tool as described in PR 2.
  - **D3 – Agent integration tests (stub LLM)**
    - Non-LLM or stub-LLM test that:
      - Uses `code.symbol_search`/`code.search` to find locations.
      - Calls `code.swe_grep` for snippets.
      - Applies a small edit via `edit.apply_*`.
      - Verifies session completes and records tool calls in `Session.ToolCalls`.
    - (Stretch) Planner + Coder integration: Planner spawns a Coder via overseer; Coder uses retrieval + edit tools to make a small, verifiable change.
  - **D4 – Telemetry & metrics**
    - Ensure `wrapWithTelemetry` / `sessionRecorder.RecordToolCall` include:
      - `code.symbol_search`, `code.swe_grep`, and new edit tool names.
    - Add simple counters if consistent with existing metrics infra (no new wire fields).
- **Validation**
  - All tests run in CI (short mode where appropriate).
  - Tool usage visible in telemetry/logs for integration runs.

---

### PR 6 – Open Questions & Finalization

- **Scope**
  - Resolve spec open questions (Section “Open Questions” in Phase 6 spec):
    - Decide on `edit.apply_patch` migration vs new `edit.apply_structured_diff`.
    - Finalize which roles get direct access to the new tools (Coder vs Reviewer).
    - Add any remaining knowledge packs describing the full retrieval funnel for humans/agents.
- **Validation**
  - Short ADR-ish note (under `docs/impl_plan/` or `docs/spec/` as appropriate) documenting:
    - Final tool naming.
    - Role/tool matrix.
    - Any deviations from the initial Phase 6 todo spec (if necessary).

---

## Validation Overview (How We Know Phase 6 Is “Done”)

- **Tool-level tests**
  - Unit tests for `code.symbol_search`, `code.swe_grep`, and structured-diff edit tool.
  - Error propagation from skills → tools → agents (including Core Profile v1 error codes).

- **Round-trip and integration tests**
  - `code/diff` → edit tool round-trip on fixtures (including CAS-backed large diffs).
  - Stub-LLM Coder (and optionally Planner+Coder) integration using retrieval + edit tools end-to-end.

- **Telemetry & knowledge**
  - New tools visible in telemetry and `Session.ToolCalls`.
  - Knowledge/router surfaces retrieval funnel + tool docs for relevant queries.

---
