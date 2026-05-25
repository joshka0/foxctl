# Goal: Detached Dream Service Memory Layer

## Goal

Build a first-class detached dream worker that turns agent transcripts into
durable, reviewable memory without requiring a user to manually run
`foxctl sessions derive-memory`.

The delivered system should:

- discover stable transcript files from configured source roots
- dedupe and track processed transcripts in durable state
- derive transcript-history records through the existing insight pipeline
- persist retrieval-ready history records into named memory
- create Obsidian inbox dream notes for long-term review and promotion
- optionally run a real blur agent, starting with Pi SDK support
- expose `run-once` and bounded service/watch modes for testing and operation

This is a source watcher plus dream worker, not a replacement for the curator
maintenance loop.

## Context

Recent DAG grep and source inspection showed these existing anchors:

- Current dream loop is embedded in the curator:
  - `internal/runtime/curator/worker.go`
  - `internal/runtime/daemon/service.go`
- Curator dream mode currently performs memory-store maintenance and optional
  retrieval-feedback draft planning:
  - `internal/context/contextplane/autonomous_memory_drafts.go`
- Transcript-to-history derivation already exists:
  - `internal/context/transcriptpipeline/import.go`
  - `internal/context/transcriptpipeline/run.go`
  - `internal/context/transcriptpipeline/history/records.go`
- Manual CLI persistence exists, but the reusable persistence seam is still too
  CLI-local:
  - `cmd/foxctl/cmd/sessions_derive_memory.go`
  - `cmd/foxctl/cmd/sessions_derive_memory_group.go`
  - `cmd/foxctl/cmd/sessions_history_persistence.go`
- Source providers currently support only `auto`, `claude`, and `codex`:
  - `internal/v2/adapters/sourceimport/types.go`
  - `internal/v2/adapters/sourceimport/importer.go`
- Codex transcript discovery already has reusable helper shape:
  - `internal/context/sessionkit/codexjsonl/locate.go`
- Shared transcript cache exists and is the right durable storage family for
  transcript processing state:
  - `internal/storage/transcriptcache/store.go`
- Generic queue storage already supports dedupe and bounded job processing:
  - `internal/storage/queue/types.go`
  - `internal/storage/queue/store.go`
- Real blur-agent support already exists:
  - `internal/runtime/memoryblur/agent.go`
  - `integrations/pi/memory-blur-agent.ts`
- Context family placement rules say new transcript-derived logic belongs in
  `internal/context/transcriptpipeline` plus `internal/storage/transcriptcache`;
  control-plane presentation belongs in `internal/context/contextplane` or
  `contextplane/taskhistory`.

## Architecture Direction

Use these modules and seams:

- `internal/context/transcriptpipeline`
  - owns source discovery plans, source metadata, transcript parsing, grouped
    source selection, and dream-input planning
- `internal/storage/transcriptcache`
  - owns durable processed-source ledger and dream-run state
- `internal/storage/queue`
  - reused for queued transcript dream jobs when persistent queueing is needed
- `internal/runtime/dreamer`
  - owns worker lifecycle, scan ticks, bounded concurrency, and run-once mode
- `cmd/foxctl/cmd`
  - owns CLI wiring only
- `internal/context/contextplane`
  - owns Obsidian-facing memory proposal/draft types only when needed

Keep the existing curator focused on maintenance over existing stores. Do not
add more transcript ingestion branches to `curator.Worker`.

## Constraints

- Use small composable code. Keep pure planning, durable state, model/agent
  calls, Obsidian writes, and runtime loops in separate modules.
- No compatibility layers or legacy aliases. Hard-cut the new dream source lane
  to one canonical shape.
- Do not add dependencies without explicit approval.
- Do not use keyword or substring heuristics for behavior decisions. Use typed
  providers, source metadata, file fingerprints, explicit source roots, and
  tests.
- Preserve envelope shape: `version`, `status`, `command`, `data`, `meta`,
  `error`.
- Preserve WASI/network policy for skills.
- Keep `internal/*` placement aligned with
  `docs/architecture/package-topology.md`.
- Do not persist raw transcript content into Obsidian notes by default. Obsidian
  dream notes should contain distilled summaries, blurred mechanisms, source
  references, and review metadata.
- Process only stable transcript files: size and mtime must remain unchanged
  across the configured quiet period or an equivalent explicit stability check.
- Use idempotent writes. Re-running the worker must not duplicate named-memory
  records, dream notes, or processed-source rows.
- Make state transitions atomic where possible: source ledger update, queue
  completion, memory persistence, and Obsidian note creation must have clear
  recovery semantics.
- Add semantic comments only at durable behavior owners, such as the source
  ledger, dream worker lifecycle, or dream note contract. Avoid broad mechanical
  anchors.
- Run `make check-doc-links` whenever markdown docs change.
- Stop after three failed attempts at the same verification failure and report
  the exact blocker.

## Milestones

### Milestone 0: Baseline And Slice Confirmation

Done when:

- Re-run focused DAG grep for:
  - `RunSingleInsight`
  - `PersistHistoryRecords`
  - `RunAutonomousMemoryDrafts`
  - `Worker.dreamLoop`
  - `ResolveAndParseTranscript`
  - `ListSessionFiles`
  - `Store.EnqueueBatch`
- Confirm the new worker belongs outside `curator.Worker`.
- Record any changed architecture decision in PR notes or docs.
- Baseline focused tests pass:

```bash
go test ./internal/context/transcriptpipeline ./internal/context/transcriptpipeline/history ./internal/storage/transcriptcache ./internal/storage/queue -count=1
```

### Milestone 1: Source Discovery And Processed-Source Ledger

Add a typed source discovery layer for transcript candidates.

Done when:

- A pure source candidate model exists with:
  - provider
  - source path
  - session id
  - workspace hint/path
  - size
  - mtime
  - digest or fingerprint
  - stability status
- Codex source discovery uses existing `codexjsonl.ListSessionFiles`.
- Claude source discovery uses existing sessionkit helpers or a narrow new
  helper in the same runtime-helper family.
- Pi and Hermes are represented as configured source roots even if their parser
  support lands in a later milestone.
- `internal/storage/transcriptcache` has a durable processed-source ledger.
- Ledger operations support:
  - upsert discovered source
  - mark queued
  - mark processing
  - mark processed
  - mark failed with bounded retry metadata
  - list candidates needing work
- Tests cover stable-file detection, idempotent rediscovery, status transitions,
  retry bounds, deterministic ordering, and invalid source roots.

### Milestone 2: Shared History Persistence Seam

Move transcript-history persistence out of CLI-local helpers into a reusable
module that the CLI and dream worker both call.

Done when:

- Shared persistence handles single and grouped insight results.
- CLI commands still produce the same persisted history behavior.
- Dream worker can persist history without importing `cmd/foxctl/cmd`.
- Owner id, workspace normalization, prefix reconciliation, and embedding
  behavior stay deterministic.
- Tests cover:
  - owner id selection
  - workspace/family path normalization
  - no-op nil/empty cases
  - prefix reconciliation
  - embedder success and failure behavior

### Milestone 3: Dream Worker Runtime

Add `internal/runtime/dreamer`.

Done when:

- Worker exposes:
  - `Run(ctx) error`
  - `RunOnce(ctx) (Report, error)`
- Runtime shell wires dependencies explicitly:
  - source scanner
  - source ledger
  - optional persistent queue
  - transcript pipeline runner
  - memory store opener
  - Obsidian writer
  - optional blur/dream agent
- Processing is bounded by configured batch size, timeout, and concurrency.
- Run-once mode is deterministic enough for tests.
- Service/watch mode responds cleanly to context cancellation.
- Failures leave enough state to retry without duplicating successful writes.
- Tests cover cancellation, bounded batch size, partial failure recovery, and
  idempotent reruns.

### Milestone 4: Dream Note Contract

Create an Obsidian-facing dream note contract separate from the existing
retrieval-feedback memory draft lane.

Done when:

- Dream notes use a new source lane such as `transcript_dream`.
- Default draft path is:
  - `inbox/drafted-from-foxctl/dreams/<project>/<YYYY-MM-DD>/`
- Suggested canonical target is:
  - `notes/memory/<project>.md`
- Notes include frontmatter for:
  - `type: memory`
  - `status: draft`
  - `trust: raw`
  - `source_lane: transcript_dream`
  - `workspace_id`
  - `provider`
  - `session_id`
  - `source_digest`
  - `dedupe_key`
  - `created_at`
  - `tags`
- Notes include sections for:
  - Summary
  - Durable Learnings
  - Open Loops
  - Blurred Mechanism, when available
  - Source References
  - Review Notes
- Golden tests verify stable markdown/frontmatter output.
- Raw transcript content is not included by default.

### Milestone 5: Real Agent/Model Dreaming And Blurring

Wire optional real model-assisted dream generation without making tests depend
on external services.

Done when:

- Pi SDK is the primary real-agent path using existing `memoryblur` conventions.
- LMStudio/OpenRouter-compatible model configuration is explicit and typed.
- Tests use fakes at the dream-agent seam, not fake production behavior.
- At least one manual smoke command can run a real Pi SDK call when credentials
  and local setup are present.
- Agent output validation rejects literal leakage and malformed output.
- Failures degrade to deterministic local dream notes only when that fallback is
  explicit in config and visible in the report.

### Milestone 6: CLI And Optional Daemon Wiring

Add a first-class command surface without hiding behavior behind the existing
curator command path.

Done when:

- CLI exposes at least:
  - `foxctl dream scan`
  - `foxctl dream run-once`
  - `foxctl dream watch`
- Commands support:
  - source roots
  - provider selection
  - workspace
  - storage root
  - vault path
  - batch limit
  - quiet period
  - dry run
  - apply drafts
  - blur/dream agent settings
- Outputs are envelope-safe and use CAS for large reports.
- Optional daemon integration starts the dream worker as its own worker, not as
  another branch inside curator.
- Docs clarify the difference between:
  - curator dream maintenance
  - retrieval-feedback memory drafts
  - transcript dream ingestion

### Milestone 7: Semantic Comments And Docs

Add high-signal semantic comments and docs only where they improve retrieval.

Done when:

- Durable owners have concise `Index:` blocks or anchors where justified:
  - source ledger
  - dream worker lifecycle
  - dream note contract
  - idempotent source processing
- Anchors are repo-local, lowercase, and evidence-only.
- Add or update docs under `docs/architecture/` or `docs/general/` describing
  the detached dream worker.
- Run semantic anchor validation when anchors are added.

Suggested checks:

```bash
GOWORK=off go test -count=1 ./internal/intelligence/indexing/semanticanchors ./internal/intelligence/indexing/repoindex
GOWORK=off go test -count=1 ./cmd/foxctl/cmd -run TestIndexRepoSemanticAnchorsE2EIndexCommentsCoexist
```

### Milestone 8: End-To-End Verification

Done when:

- Build succeeds:

```bash
make build
```

- Focused unit and integration tests pass:

```bash
go test ./internal/context/transcriptpipeline ./internal/context/transcriptpipeline/history ./internal/storage/transcriptcache ./internal/storage/queue ./internal/runtime/dreamer -count=1
go test ./cmd/foxctl/cmd -run 'Test.*Dream|Test.*SessionsDeriveMemory' -count=1
```

- Full relevant checks pass:

```bash
make test
make check-doc-links
```

- Manual smoke confirms:
  - `./bin/foxctl dream scan --dry-run ...`
  - `./bin/foxctl dream run-once --dry-run ...`
  - run-once against a small copied transcript fixture
  - optional real Pi SDK call when environment is configured
- Final self-review answers:
  - Did this avoid adding transcript ingestion to `curator.Worker`?
  - Is the source ledger idempotent and retryable?
  - Is Obsidian output reviewable and free of raw transcript dumps?
  - Are provider boundaries explicit and typed?
  - Are tests behavior-focused rather than implementation choreography?
  - What would fail a thermonuclear code review?
  - Confidence score and residual risks.

## Verification

Run focused tests after each milestone. Prefer table-driven unit tests for pure
planning, storage transition tests for ledger behavior, golden tests for
markdown output, and integration tests only where real stores or CLI envelopes
matter.

Minimum final verification:

```bash
make build
go test ./internal/context/transcriptpipeline ./internal/context/transcriptpipeline/history ./internal/storage/transcriptcache ./internal/storage/queue ./internal/runtime/dreamer -count=1
go test ./cmd/foxctl/cmd -run 'Test.*Dream|Test.*SessionsDeriveMemory' -count=1
make check-doc-links
```

Run broader checks before PR/MR if time allows:

```bash
make test
make check
```

## Test Strategy

Use ruthless test filtering. Every new test must name the behavior it protects
and the bug it would catch.

High-value tests:

- source candidate stability prevents processing actively-written transcripts
- ledger rediscovery does not enqueue duplicates
- failed dream jobs retry within bounds and then stop
- rerunning a processed transcript does not duplicate named memories or notes
- CLI persistence and dream-worker persistence use the same shared seam
- dream note rendering is stable, reviewable, and avoids raw transcript content
- cancellation stops watch mode without leaving in-memory workers running
- invalid provider/source-root config fails clearly
- Pi/agent output validation rejects malformed or literal-leaking blur output

Avoid tests that only prove:

- private helper call order
- mock choreography
- pass-through wrappers
- raw line coverage without a meaningful invariant

## Stop Conditions

- Stop before adding new dependencies.
- Stop before broad package moves or package renames.
- Stop before changing public transcript formats or named-memory schema in a
  non-backward-compatible way unless the goal is explicitly updated.
- Stop before making curator own transcript ingestion.
- Stop after three failed attempts at the same test or build failure.
- Stop if Pi/Hermes transcript formats cannot be determined locally; preserve
  their typed provider slots and document the parser follow-up instead of
  guessing from keywords.
- Stop if Obsidian writes cannot be made idempotent; report the state model gap
  before continuing.
