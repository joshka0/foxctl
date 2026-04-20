---
name: tui-docs-worker
description: Authors the M1 architecture + audit + IA + integration + ADR docs for the foxctl TUI redesign. Docs-only worker; no code changes.
---

# TUI Docs Worker

NOTE: Startup and cleanup are handled by `worker-base`. This skill defines the WORK PROCEDURE.

## When to Use This Skill

Use this worker for all M1 (milestone `docs`) features. These are pure
docs-authoring features under `docs/plans/tui-redesign/` that together answer
the question "how should we redesign the foxctl TUI operator cockpit?"

Never use this worker for code-bearing features. Those go to
`tui-engineer-worker`.

## Required Skills

- **None.** This is a pure docs worker. If your feature requires code
  inspection you do the inspection via Read/Grep yourself; do not invoke
  other Factory skills.

The scrutiny validator may subsequently spawn subagents that read your docs
to verify coverage and citations (VAL-CROSS-003 specifically tests that the
docs are sufficient for an LLM to extend the system).

## Work Procedure

### 1. Orient yourself

Before writing anything:

1. Read `missionDir/mission.md` and `missionDir/validation-contract.md` so
   you understand the full assertion set.
2. Read `.factory/library/architecture.md` and
   `.factory/library/user-testing.md` for the high-level plan.
3. Read `DESIGN.md` and `AGENTS.md` at the repo root for product and
   engineering principles.
4. Read `docs/plans/go-tui-agent-shell.md` — the canonical prior plan your
   docs must reconcile with (VAL-DOCS-009).
5. Read the feature description carefully. It lists the assertion IDs you are
   responsible for completing (in the `fulfills` field).

### 2. Identify every assertion you must satisfy

Open `missionDir/validation-contract.md`. For each assertion ID in your
feature's `fulfills`:

- Copy the assertion's **Evidence** requirements into your working notes.
- Identify every section, heading, count, or link the assertion demands.
- Note if the assertion requires cross-references to other files (many do).

This is your checklist. You are done only when every item is satisfied.

### 3. Investigate, then write

Most M1 assertions demand **concrete citations** (path:line references) from
the current codebase. You must:

1. Use Grep / Read / Glob to find concrete evidence in
   `internal/interfaces/tui/` before writing prose.
2. Cite specific symbols (not vague file references). If an assertion requires
   "≥20 `path:line` citations", count as you write — do not hand-wave.
3. For go-tui API references (VAL-DOCS-001), cite the source — grindlemire/go-tui
   doc pages OR symbol names like `tui.State[T].Update` that a reader can
   resolve. Do not invent APIs.
4. For reconciliation with `docs/plans/go-tui-agent-shell.md` and `DESIGN.md`
   (VAL-DOCS-009, -013, -014), use direct quotations or named principle
   references.

### 4. Honor the file layout

Your features output files under `docs/plans/tui-redesign/`. Exact filenames
per the validation contract:

- `research-go-tui.md`
- `audit-current-tui.md`
- `architecture.md`
- `information-architecture.md`
- `component-spec.md`
- `integration-map.md`
- `adrs/<NNN>-<slug>.md` (one file per ADR)

Do **not** rename these files. The validation contract and `features.json`
refer to them by path. If your feature description asks you to work on
`architecture.md` and the file already exists (because a prior feature
created it), extend it — do not overwrite.

### 5. Link hygiene

Every internal link must resolve. Before handing off:

1. Run `make check-doc-links` and confirm exit 0.
2. If it fails, fix the broken links before handing off. VAL-DOCS-008 is a
   hard gate; failing it blocks the milestone.

### 6. Handoff

Before writing the handoff JSON, verify every assertion in your `fulfills`
array:

- For each assertion, state specifically which section(s) / table(s) / code
  blocks satisfy it and what evidence a reviewer can grep for.
- Run `make check-doc-links`; report its exit code in `commandsRun`.
- Run `rg -n` commands to confirm counts (e.g., citations, ADRs); include
  those commands in `commandsRun` so the validator can re-run them.

## Example Handoff

```json
{
  "salientSummary": "Authored docs/plans/tui-redesign/architecture.md satisfying VAL-DOCS-003, -009, -010, -012, -014. Each major decision has Decision/Rationale/Alternatives; runtime-bounded decision explicitly names how console_stream_pump/console_ask_runtime/console_cancel_runtime collapse; .gsx toolchain section gives exact commands. Verified via `make check-doc-links` (exit 0) and grep for required section headings.",
  "whatWasImplemented": "Created docs/plans/tui-redesign/architecture.md (1,842 lines). Sections: Decisions (9 subsections each with Decision/Rationale/Alternatives — one per VAL-DOCS-003 sub-requirement a–i); .gsx Toolchain (exact command `go generate ./internal/interfaces/tui/...`, editable glob `*.gsx`, forbidden glob `*_gsx.go`, add-a-view checklist, 4 generated-artifact paths); Traceability table mapping 6 audit findings to decisions (chained NewShell*, 3 runtime goroutines, string-keyed kinds, ambient ShellState, sync boot, undocumented .gsx); Surface Ownership table mapping cockpit surfaces to Runtime/Companion/Rooms/Orchestration/Events; Reconciliation section (312 words) with explicit comparison to docs/plans/go-tui-agent-shell.md four-region shell and named mapping of 5 DESIGN.md principles to decisions.",
  "whatWasLeftUndone": "",
  "verification": {
    "commandsRun": [
      { "command": "make check-doc-links", "exitCode": 0, "observation": "0 broken links across repo." },
      { "command": "rg -c '^## Decision ' docs/plans/tui-redesign/architecture.md", "exitCode": 0, "observation": "9 Decision sections — matches sub-requirements (a)–(i) of VAL-DOCS-003." },
      { "command": "rg -n '(docs/plans/go-tui-agent-shell.md|DESIGN.md)' docs/plans/tui-redesign/architecture.md", "exitCode": 0, "observation": "Both link targets present in Reconciliation section." },
      { "command": "rg -c '^\\| ' docs/plans/tui-redesign/architecture.md", "exitCode": 0, "observation": "Confirms table rows present in Traceability (6+) and Surface Ownership (5+)." }
    ],
    "interactiveChecks": []
  },
  "tests": { "added": [] },
  "discoveredIssues": []
}
```

## When to Return to Orchestrator

Standard return conditions apply. Docs-specific returns:

- **Contract ambiguity** — if an assertion in `validation-contract.md` is
  internally inconsistent (e.g., a required file path conflicts with another
  assertion's path), stop and return.
- **Missing prerequisite docs** — if your feature needs to cross-reference
  another M1 doc that has not been written yet and is not in your feature's
  preconditions, return to the orchestrator rather than inventing content.
- **Broken doc-link check with unclear root cause** — if
  `make check-doc-links` fails and the cause is upstream of your feature (a
  pre-existing broken link elsewhere in the repo), return with the
  observed failure rather than fixing it covertly.
