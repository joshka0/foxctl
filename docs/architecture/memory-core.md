# Memory Core Architecture

## Metadata

| Field | Value |
|------|-------|
| Status | Current |
| Canonical scope | Agent memory record contract, retrieval lanes, lifecycle, curator trust gates |
| Canonical packages | `internal/context/memorycore`, `internal/storage/memory`, `skills/memory_query`, `skills/memory_curator_report` |
| Last reviewed | 2026-05-05 |

## Core Model

Memory is a typed evidence layer, not an instruction pile.

The core contract is:

```text
raw event or remembered item
  -> typed memory record
  -> retrieval candidate
  -> lane-labeled evidence
  -> optional promoted fact, skill, or policy
  -> later revalidated, superseded, demoted, or archived
```

The original memory content should be treated as mostly immutable. It records
what was observed, when it was observed, and where it came from. Mutable state
belongs in sidecar-style fields: lifecycle, telemetry, supersession, review
status, validation status, and trust adjustments.

Agents should not rewrite old memory to pretend it was always current. They
should derive current assumptions, candidate skills, and state from memory
records, then keep those derived conclusions subject to validity, trust, and
freshness checks.

## Record Contract

Canonical records are defined in `internal/context/memorycore`.

Each record carries:

| Field family | Role |
|-------------|------|
| Kind | Distinguishes `working_context`, `episodic_trace`, `semantic_fact`, `decision`, `procedural_skill`, `policy_rule`, `reflection`, `eval_result`, and `adapter_example` |
| Source lane | Identifies where the record came from, such as named memory, context claim, transcript claim, companion state, or companion evidence |
| Temporal envelope | Separates observed time, ingested time, event time, validity window, last access, last use, last validation, and TTL/revalidation needs |
| Provenance | Records source type, session, agent, tool call, commit, file refs, parent memory IDs, and creator |
| Trust envelope | Carries source trust, confidence, authority, and taint flags |
| Lifecycle envelope | Tracks candidate, active, stale, archived, deprecated, or quarantined state, plus pins and review status |
| Telemetry envelope | Tracks views, selections, explicit uses, success/failure, patches, and restores |
| Usage envelope | States whether the record is instruction-eligible or evidence-only |

The important invariant is:

```text
Retrieved memory is evidence by default.
It becomes instruction only when it is an active policy or validated skill.
```

## Memory Kinds

| Kind | Meaning | Default authority |
|-----|---------|-------------------|
| `working_context` | Current task/session context | Evidence unless active and current |
| `episodic_trace` | Something that happened | Evidence only |
| `semantic_fact` | A factual claim or lesson | Evidence until current and non-superseded |
| `decision` | Project/runtime decision with rationale | Evidence with higher authority when provenance is trusted |
| `procedural_skill` | A reusable procedure | Instruction only after validation or explicit approval |
| `policy_rule` | Behavior constraint or routing rule | Instruction only when active and trusted |
| `reflection` | Agent-generated lesson or hypothesis | Low-authority evidence |
| `eval_result` | Evidence that behavior passed or regressed | Trusted when produced by eval/runtime |
| `adapter_example` | Training or consolidation example | Evidence for future synthesis |

## Lifecycle

Lifecycle is how the system decides whether a memory should still affect future
behavior.

```text
candidate -> active -> stale -> archived
active -> deprecated
any suspicious record -> quarantined
```

| State | Meaning |
|------|---------|
| `candidate` | Structured but not yet trusted |
| `active` | Eligible for ordinary retrieval and context selection |
| `stale` | Still searchable, but suspect unless strongly relevant or explicitly requested |
| `archived` | Retained for history, excluded from normal retrieval |
| `deprecated` | Superseded by another record, retained for audit/history |
| `quarantined` | Never instruction, evidence only with warning |

Pinned is a protection flag, not a lifecycle state. Pinned records are not
auto-archived, silently superseded, or model-patched, but they are still visible
in audit reports.

## Read Path

The agent-facing read path is:

```text
agent/task query
  -> memory/query
  -> named memory + context claims
  -> canonical memorycore records
  -> lifecycle/trust/provenance filtering
  -> lane-labeled context
  -> foxcular event telemetry
```

`memory/query` should be used for prior knowledge, decisions, gotchas, and
candidate procedures. It should not be used as a substitute for current repo
inspection. For code tasks, pair it with `gather_context`:

| Question | Preferred surface |
|---------|-------------------|
| What does the repo currently contain? | `gather_context` / repoindex |
| What have we learned or decided before? | `memory/query` |
| Is an old claim still safe to use? | load/check current repo evidence, then update lifecycle |
| Which records should stop shaping behavior? | `memory/curator_report` |

When memory is injected into an agent prompt, it should keep lane labels and
usage labels visible. A prompt should distinguish:

```text
active policy
validated skill
current semantic fact
decision evidence
episodic trace
stale evidence
quarantined evidence
```

## Write Path

Writes should be append-first and provenance-bearing:

```text
room event, tool result, eval result, or explicit memory put
  -> raw/provenance-bearing storage
  -> typed memory record projection
  -> embeddings or lexical indexes when available
  -> optional later promotion through validation or review
```

The write path should not promote agent reflections into policy by default.
Environment-dependent negative claims should normally require revalidation.
Examples:

```text
tests cannot run locally
API does not support tool calls
repo has no eval harness
CLI command is broken
```

## Curator Layer

The curator answers a different question from retrieval.

Memory core asks:

```text
What do we know, when was it true, where did it come from, and how should it be retrieved?
```

Curator asks:

```text
Which memories and skills still deserve to affect future behavior?
```

`memory/curator_report` plans deterministic lifecycle proposals:

| Proposal | Meaning |
|---------|---------|
| `demote_stale` | Active record should move to stale |
| `archive` | Stale record should move to archived |
| `deprecate` | Record is superseded by another record |
| `revalidate` | Record should be checked before trusted use |
| `skip_pinned` | Pinned record needs manual review |
| `review_quarantined` | Quarantined record remains evidence-only |

Dry-run reports are the default. Apply mode requires explicit confirmation and
only mutates supported lifecycle-backed lanes. Unsupported lanes are reported as
skipped, not silently modified.

## Trust And Authority

Authority is ordered by source and lifecycle, not by retrieval score.

Default authority order:

```text
system/pinned policy
> current human instruction
> current repo truth
> validated eval result
> validated skill
> current semantic fact with trusted provenance
> decision evidence
> episodic trace
> agent reflection
> untrusted external content
```

If memory conflicts with current repo evidence, current repo evidence wins.
If memory conflicts with the current user instruction, the current instruction
wins unless it violates a higher system/runtime policy.

## Observability

Memory reads and curator maintenance emit aggregate foxcular events through
`internal/runtime/observability`.

Events should include:

- operation name, workspace, session, and agent
- counts by kind, lifecycle, and source lane
- selected/returned record counts
- suppressed lifecycle counts
- curator proposals and apply counts
- unavailable source lanes
- status and duration

Events must not include raw memory content, raw terminal output, or unredacted
user queries. Foxcular events are telemetry and audit evidence, not new authority.

## Agent Integration Rules

Agents should follow these rules:

1. Use `memory/query` early for prior decisions, known gotchas, and candidate
   procedures.
2. Treat returned records as evidence unless `usage.instruction_eligible=true`.
3. Prefer `gather_context` or direct repo inspection for current code truth.
4. Keep stale, deprecated, and quarantined records labeled when shown.
5. Record meaningful decisions and eval results after work completes.
6. Promote procedural skills only after validation or explicit human approval.
7. Run curator reports periodically and apply lifecycle changes only through
   explicit report/apply workflows.

## Related Docs

- [General memory reference](../general/memory.md)
- [Companion memory](../general/companion-memory.md)
- [RLM gather context](./rlm-gather-context.md)
- [Context architecture](./context-architecture.md)
- [Memory core + curator implementation plan](../plans/features/memory-core-curator-layer.md)
