# Universal SWE Grep & Agents — Preparation Prompt

Use this prompt when you want to **continue researching and planning the later phases**
(semantic index, symbol index, SWE Grep, agents, trajectories, teams) of
`universal_swe_grep_and_agents`.

```text
You are helping design and implement the remaining phases of the
"Universal SWE Grep, Symbol Index, and Agents" plan for the agentctl
repo.

Anchor yourself in these sources first (read them, then cross-check):
- docs/impl_plan/universal_swe_grep_and_agents.md
- docs/impl_plan/universal_swe_grep_and_agents_codemap.md
- docs/impl_plan/universal_swe_grep_and_agents_specs_phase1_review_gate_todo.md
- docs/impl_plan/universal_swe_grep_and_agents_specs_phase2_post_review_harness_todo.md
- docs/spec/review_semantic_trajectory_specs.md
- docs/spec/core_profile_v1.md
- docs/spec/semantic_file_index.md
- docs/spec/code_symbol_index_and_swe_grep.md
- docs/spec/dspy_go_agents.md
- docs/spec/dspy_trajectory_capture.md

Your job in this session:
1. Pick ONE phase beyond Phase 2 (e.g. Phase 3 semantic index, Phase 4 symbol
   index, Phase 5 SWE Grep, Phase 6 agents, Phase 7 trajectories, Phase 8 teams,
   or Phase 9 polish).
2. Summarize the current intent of that phase in 3–5 precise sentences, using
   ONLY the sources above.
3. Propose a *todo-style* Phase N spec file (like the Phase 1 + Phase 2 todo
   specs), broken into A/B/C sections, each with 3–7 concrete checkboxes that
   map cleanly to PRs.
4. For that phase, call out:
   - Cross-refs to specs, impl plan, and testing plan.
   - Expected golden tests / CAS artifacts / envelopes.
   - Any tricky sequencing or dependencies on earlier phases.
5. Stop after you have:
   - A clear Phase N todo spec outline.
   - A short note on how you would validate it (tests + golden fixtures).

Constraints:
- Do NOT invent new wire contracts; keep envelopes and CAS behavior aligned with
  docs/spec/core_profile_v1.md and existing golden tests.
- Prefer small, independent PR-sized chunks; every checkbox should be
  reviewable on its own.
- If sources disagree, prefer docs/spec/* over other docs and surface the
  inconsistency explicitly.

Output format:
- First: a 3–5 sentence summary of the chosen phase.
- Then: a markdown outline for `docs/impl_plan/universal_swe_grep_and_agents_specs_phaseN_<short_name>_todo.md`.
- Finally: a short "Validation" section listing the tests, goldens, and
  observability you expect to add in that phase.
```
