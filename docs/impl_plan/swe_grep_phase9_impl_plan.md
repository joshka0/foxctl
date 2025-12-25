## Phase 9 – End-to-End Flows & CI Hardening (Summary)

Phase 9 focuses on making the full pipeline **easy to adopt** and **hard to
regress** by adding:

- Deterministic fixtures and `docs/examples/` walkthroughs.
- End-to-end tests that traverse cross-phase flows and assert invariants.
- CI and test-infra hardening (targets, goldens, drift detection, and suite
  gating).

The Phase 9 checklist remains the authoritative breakdown:

- `docs/impl_plan/universal_swe_grep_and_agents_specs_phase9_end_to_end_ci_hardening_todo.md`

This phase should **not change Core Profile v1 envelope shape** or `meta.*`
contracts.

---

## A/B/C/D Todo Structure – Sanity Check vs Phase 9 Spec

- **Section A – Examples & developer workflows**
  - Add small fixture workspaces and walkthrough docs that demonstrate the
    pipeline.

- **Section B – End-to-end flows & invariants**
  - Add E2E tests that cover review gate → post-review fanout → indexing →
    retrieval.
  - Assert cross-phase invariants (idempotence, correlation, artifact
    integrity).

- **Section C – CI hardening & test infrastructure**
  - Ensure Make targets clearly represent suites and that CI runs the right
    subsets.
  - Add explicit golden drift detection and a documented regen path.

- **Section D – Top-level docs & adoption**
  - Ensure the main docs describe the pipeline and link to the phase
    plans/specs.

---

## Proposed PRs for Phase 9 – End-to-End & CI Hardening

### PR 1 – Fixtures + `docs/examples/` Workflows (A1/A2)

- **Scope**
  - Add one or two small, deterministic fixture workspaces (recommended
    locations: `test/fixtures/` or `docs/examples/`).
  - Add a “happy-path” walkthrough under `docs/examples/` that exercises:
    - Task lifecycle + review gate.
    - Post-review → semantic + symbol index.
    - Retrieval tools (`code.symbol_search`, `code.swe_grep`).

- **Constraints**
  - Fixtures must be small, deterministic, and self-contained (no network).
  - Walkthroughs should use standard repo commands.

- **Validation**
  - `make test` stays green.

---

### PR 2 – E2E Pipeline Tests + Cross-Phase Invariants (B1)

- **Scope**
  - Add E2E tests that exercise the full pipeline on a fixture workspace.
  - Assert key invariants:
    - Post-review triggers semantic + symbol index jobs once per review.
    - CAS artifacts are integrity-checked (digest matches content).
    - Retrieval events show up in trajectories with correct
      `meta.correlation_id` and `task_id` (where trajectory capture applies).

- **Constraints**
  - Tests must be deterministic.
  - Heavier suites should be skippable in `-short` and/or gated behind a stable
    env var.

- **Validation**
  - `make test` stays fast.
  - An explicit “heavier suite” command exists and is documented.

---

### PR 3 – Golden Envelopes + Artifact Shape Hardening (B2/C2)

- **Scope**
  - Add/refine goldens where shape stability matters:
    - `test/golden/envelopes/`.
    - CAS-backed artifacts (e.g. NDJSON exports).
  - Extend tests to:
    - Validate envelopes with `protocol.Validate`.
    - If `meta.cas_digest` is set, it matches `data.artifact`.

- **Validation**
  - Golden tests run under `make test`.
  - Golden regeneration is documented (e.g. `go test ./test/golden -update`).

---

### PR 4 – CI Targets, Suites, and Test-Watch Compatibility (C1/C3)

- **Scope**
  - Review/adjust Make targets and CI wiring to make intent explicit:
    - Fast PR gate: `make lint`, `make vet`, `make test`.
    - Optional: `make test-race` where feasible.
    - Optional: dedicated E2E suite target and gating policy.
  - Add explicit golden drift detection in CI.
  - Confirm `test_watch` / `hooks/test_feedback` continue to surface failures
    clearly for indexing/retrieval/trajectory flows.

- **Constraints**
  - Avoid slowing the default PR suite.
  - Prefer Make targets over ad-hoc scripts.

---

### PR 5 – Top-Level Docs Refresh + Cross-Linking (D1/D2)

- **Scope**
  - Update:
    - `README.md` (pipeline summary + entrypoints).
    - `AGENTS.md` (roles/tools/guardrails).
    - `ARCHITECTURE.md` (where subsystems live).
  - Cross-link Phase 1–9 docs and testing plan sections.

---

## Validation Overview – How We Know Phase 9 Is "Done"

- **Examples exist and are usable**
  - A developer can follow `docs/examples/` to run the pipeline end-to-end.

- **E2E invariants are locked in**
  - E2E tests cover the critical path and assert cross-phase invariants.

- **Goldens are stable and reviewable**
  - Golden tests validate envelope/artifact shapes.
  - The regen process is documented and CI catches drift.

- **CI targets match repo conventions**
  - Primary verification uses `make` targets (not raw `go test ./...`).

---

## Open Questions / To Discuss

- What is the minimal, stable set of E2E scenarios we want to lock in as
  blocking tests vs optional/nightly ones?
- How aggressively should CI enforce golden stability (e.g. all goldens vs only
  envelope/trajectory-related ones)?
- Are there additional high-value examples we should include beyond the ones
  described in the Phase 9 TODO spec (e.g. failure-mode walkthroughs, demo
  scripts)?
