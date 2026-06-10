# Goal: LongMem-Ready Memory Recall

## Goal

Finish the remaining foxctl work needed for a fair LongMem-style memory recall
evaluation using the existing **Skill**, **Runtime**, **Job**, **Context
engine**, and **Adapter** vocabulary.

The intended end state is one canonical named-memory recall flow: ingestion
creates retrievable atomic memory records, the embedding queue drains through one
shared processor, `memory/query` and `rlm_query` use the same retrieval
semantics, and a LongMemEval command produces reviewable metrics and evidence
artifacts without benchmark leakage.

## Context

- Current repo: `/home/dev/repos/foxctl`.
- Current branch already has early LongMem retrieval slices in progress:
  - `skills/memory_query/main.go`
  - `skills/memory_query/main_test.go`
  - `internal/storage/memory/store.go`
  - `internal/storage/memory/turso_store.go`
  - `internal/storage/memory/search.go`
  - `internal/storage/memory/search_lexical.go`
  - `internal/storage/memory/store_test.go`
- There are unrelated pre-existing docs edits in:
  - `packages/docs-site/src/content/docs/memory/continuity.md`
  - `packages/docs-site/src/content/docs/reference/cli.md`
  - `packages/docs-site/src/content/docs/reference/command-map.md`
  Do not revert, overwrite, or include these in this goal unless directly
  asked.
- Prior findings to preserve:
  - `skills/embedding_worker/main.go` contains the active memory embedding
    processor behavior.
  - `internal/intelligence/indexing/embedding/worker.go` is an older daemon-like
    worker path and is not currently the named-memory queue processor.
  - `foxctl daemon` currently has no clearly wired memory embedding queue worker.
  - `memory/query` has been moved toward hybrid vector + lexical fusion.
  - Atomic fields already exist on named memory: `atomic_text`, `entities`,
    `keywords`.
  - `internal/rlm/plan.go` and `skills/rlm_query/skill.yaml` already support the
    `memory_recall` route profile.
  - Existing eval patterns live under `internal/tooling/evals/` and
    `cmd/foxctl/cmd/eval_*.go`.
  - Existing reranker support lives under
    `internal/intelligence/indexing/rerank/`.
- Existing LongMem pilot artifacts from earlier investigation:
  - dataset: `/tmp/foxctl-hydra-longmem/longmemeval_s_cleaned.json`
  - pilot workspace: `/tmp/foxctl-hydra-longmem/memory-pilot6`
  - current pilot showed vectors are readable but question-to-memory ranking is
    weak.

## Current Integrated Slices

As of 2026-06-02, the branch has integrated the LongMem-ready retrieval slices
needed for the next bounded eval pass:

- Shared named-memory recall now lives under
  `internal/intelligence/retrieval/memoryrecall` so `memory/query` and RLM
  `retrieve_memory` can use the same lexical, vector, fusion, lifecycle, and
  evidence-shaping behavior.
- LongMemEval ingestion now lives under
  `internal/tooling/evals/longmemeval` and is benchmark-integrity focused:
  searchable and embedded memory fields are populated from conversation content,
  while expected answers, evidence labels, case IDs, and eval metadata stay out
  of semantic content.
- LongMem ingestion enqueues named-memory embedding work through the existing
  queue path with provider/model/dimension awareness instead of making a direct
  one-off embedding call.
- Lexical named-memory search filters low-signal stop words and searches the
  atomic memory fields used by retrieval: summary, atomic text, entities, and
  keywords.
- The named-memory embedding processor in
  `internal/intelligence/indexing/embedding/processor.go` is the reusable
  implementation for queue job processing.
- `foxctl agent run` now wires daemon poll ticks to a bounded named-memory
  embedding queue drain for the daemon workspace, using the shared processor
  rather than skill-local embedding logic.

Still incomplete:

- The LongMemEval package should be treated as ingestion/retrieval readiness
  work only. Do not document full LongMemEval command support until the command,
  scoring modes, and artifacts are implemented and tested.

## Constraints

- Use small, composable changes. Each milestone must be a coherent diff with
  behavior-focused tests.
- Do not add a new parallel retrieval framework. Reuse and deepen existing
  modules where possible: memory storage, `memory/query`, `rlm_query`,
  embedding queue, rerank, and eval tooling.
- Apply zero-tech-debt pressure: prefer one canonical flow over compatibility
  branches, aliases, duplicate processors, or fallback paths with no live caller.
- Apply a thermonuclear code-quality bar:
  - Search for code-judo moves that delete duplicated branches or wrappers.
  - Treat files crossing or approaching 1000 lines as decomposition pressure.
  - Do not add feature checks scattered through unrelated shared paths.
  - Do not preserve shallow modules that fail the deletion test.
  - Push stringly payloads and broad `any` shapes toward explicit contracts when
    call sites reveal the real shape.
- Use improve-codebase-architecture vocabulary in final self-review:
  **Module**, **Interface**, **Implementation**, **Depth**, **Seam**,
  **Adapter**, **Leverage**, and **Locality**.
- Do not use secrets or production credentials. Do not run production HydraDB
  operations. Do not echo or persist any throwaway keys that may appear in shell
  history or prior chat context.
- Do not run a full repo ingestion or broad reembedding pass until bounded smoke
  checks pass.
- Do not add dependencies without explicit approval.
- Preserve existing public envelopes and command contracts unless a milestone
  explicitly calls out a hard cut and tests the affected callers.
- Stop after 3 failed attempts at the same failing verification command and
  summarize the blocker.

## Milestones

### Milestone 0: Review And Tighten Current Retrieval Slices

Done when:

- Run a strict code-quality review of the current branch changes touching
  `memory/query` and `internal/storage/memory`.
- Confirm the hybrid lexical/vector implementation is in the right **Module**
  and not just skill-local logic that should live behind a deeper retrieval
  **Interface**.
- Confirm lexical search uses atomic fields consistently for SQLite, Turso, and
  advanced memory search helpers.
- Delete or consolidate any helper, branch, or fallback added by the current
  slice that fails the deletion test.
- Tests still pass:
  - `env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go test -count=1 ./internal/storage/memory ./skills/memory_query`

### Milestone 1: Shared Memory Embedding Job Processor

Status: integrated for the shared processor and daemon workspace drain. The
reusable named-memory embedding processor lives in
`internal/intelligence/indexing/embedding/processor.go`, ingestion uses
model-aware queueing, and daemon poll ticks can drain `kind=memory` jobs for the
daemon workspace without duplicating `skills/embedding_worker` logic.

Done when:

- Extract the named-memory embedding job processing logic from
  `skills/embedding_worker/main.go` into one internal **Module** with a small
  **Interface**.
- The skill worker and any daemon/Runtime worker use that same processor.
- The old `internal/intelligence/indexing/embedding/worker.go` path is either
  deleted, clearly retargeted, or proven to still serve a separate live caller.
- `foxctl daemon` can drain memory embedding **Jobs** without duplicating skill
  logic.
- Tests prove:
  - a memory queue job embeds text, validates dimensions, stores metadata,
    updates named-memory embedding, and marks the job complete;
  - provider/dimension errors leave the job retryable or failed according to
    existing queue semantics;
  - stale/running jobs recover without double-writing embeddings.

### Milestone 2: LongMem Ingestion Adapter

Status: integrated for anti-leakage ingestion and model-aware enqueue. Full
command support and scoring artifacts remain Milestone 4 work.

Done when:

- Add a LongMemEval ingestion **Adapter** that turns dataset conversation content
  into named-memory records without embedding benchmark labels, expected
  answers, evidence IDs, or evaluation metadata as semantic content.
- Ingestion populates `summary`, `atomic_text`, `entities`, and `keywords`
  deliberately. Use the local Qwen embedder only through the existing embedding
  queue/processor path, not by adding a direct one-off embedder call.
- The ingestion path is idempotent for a workspace and records enough provenance
  to connect retrieval evidence back to a dataset case without leaking the
  answer into retrieval.
- Tests prove:
  - answer/evidence fields are not embedded;
  - repeated ingestion does not duplicate records;
  - atomic fields are present and searchable;
  - queue rows are created with the expected kind/workspace/group identity.

### Milestone 3: Canonical Memory Recall Pipeline

Status: integrated for shared `memoryrecall` retrieval semantics across
`memory/query` and RLM `retrieve_memory`, including lexical atomic search,
vector search, fusion, lifecycle policy, and evidence-backed recall shaping.
Keep optional rerank and broader eval artifact coverage under the remaining
milestone checks unless tests prove them in the current slice.

Done when:

- `memory/query` and RLM memory recall share the same retrieval semantics for
  named memory: lexical atomic search, vector search, fusion, lifecycle policy,
  and optional rerank where configured.
- If reranking is wired, use the existing
  `internal/intelligence/indexing/rerank` **Module** and avoid a new reranker
  **Adapter** unless there are two real adapters.
- Session/context expansion is explicit and evidence-backed. Do not silently
  stuff adjacent content into answers without an evidence ref.
- Tests prove:
  - vector-only, lexical-only, and hybrid matches surface correctly;
  - lifecycle suppression still works;
  - optional rerank changes ordering without dropping required metadata;
  - RLM `memory_recall` can retrieve memory and produce evidence refs through
    existing allowed tools.

### Milestone 4: LongMemEval Command And Artifacts

Status: bounded slice integrated. `foxctl eval longmem` supports ingest,
queue-status, retrieval-only scoring, and RLM answer-mode scoring with JSON
artifacts. Retrieval mode uses BM25-only (no query embedding), so it never
calls an embedder or an LLM. Answer mode uses the configured RLM model with
the `memory_recall` route and `memory-recall` tool profile; tests inject a
fake answer runner and do not require external services.

Done when:

- Add a first-class eval command or subcommand using existing eval package
  patterns under `internal/tooling/evals/` and `cmd/foxctl/cmd/eval_*.go`.
- The eval supports at least:
  - ingest only;
  - drain/check embedding queue status;
  - retrieval-only scoring;
  - answer-mode scoring through RLM `memory_recall`;
  - artifact output directory.
- Metrics include hit@5, hit@10, hit@50, hit@100, MRR, latency, queue
  counts, failure counts, deterministic answer exact/contains match, and
  answer-mode evidence hits. Judge-compatible answer scoring remains deferred.
- Each case artifact includes query, retrieved memory names, scores,
  ranks, expected evidence memory names and session IDs, matched
  evidence, method, duration, anti-leakage finding count, and answer-mode
  answer/evidence fields when answer mode runs.
- Tests use a tiny fixture dataset and do not require external services.

### Milestone 5: Bounded Smoke And Head-To-Head Report

Status: bounded retrieval smoke completed on 2026-06-02. `foxctl eval longmem
--artifact-dir` writes a local `head-to-head.md` generated from `RunResult`,
comparing foxctl retrieval-only, foxctl RLM `memory_recall`, and unavailable
HydraDB/external baselines honestly. The smoke used workspace
`longmem-smoke-20260602151411` and artifacts under
`/tmp/foxctl-longmem-smoke.y9ExDl/artifacts`; it ingested a tiny fixture,
queued two `kind=memory` jobs, drained both through `embedding/worker` with
`parallelism=1`, and produced retrieval hit@5=1.000 and MRR=1.000. Answer mode
was not run because no local RLM answer model was available at the default
LM Studio endpoint, so the report honestly marks foxctl RLM `memory_recall` as
`not run`.

Done when:

- Run a bounded local smoke, not a full repo ingestion:
  - ingest a small LongMem fixture;
  - enqueue embeddings;
  - drain with low parallelism;
  - run retrieval-only eval;
  - run RLM answer-mode eval when the configured local model is available.
- Produce an **Artifact** report that compares:
  - foxctl raw `memory/query`;
  - foxctl RLM `memory_recall`;
  - any available baseline from the LongMemEval/HydraDB comparison plan, without
    requiring production HydraDB access.
- The report includes current limitations and the exact commands needed to run
  the larger evaluation once approved.

## Verification

Run narrow tests after each milestone. Before final completion, run at least:

```bash
env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go test -count=1 ./internal/storage/memory ./skills/memory_query
env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go test -count=1 ./skills/embedding_worker ./skills/embedding_queue
env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go test -count=1 ./internal/intelligence/indexing/embedding/... ./internal/intelligence/indexing/rerank/... ./internal/tooling/evals/...
env -u GOROOT -u GOBIN -u GOTOOLDIR CGO_ENABLED=0 /usr/local/go/bin/go test -count=1 ./cmd/foxctl/cmd
git diff --check
```

If a package does not exist or cannot be tested with the exact command, explain
why and replace it with the nearest package-specific command.

Done when:

- All milestones above are complete.
- The bounded smoke passes or is blocked only by a clearly documented unavailable
  local model/runtime.
- No new duplicated retrieval stack, duplicated embedding processor, or
  benchmark-specific answer leakage exists.
- Final response includes changed files, commands run, artifact paths, residual
  risks, and a confidence score.

## Final Self-Review Required

Before marking complete, perform a strict self-review with these sections:

- **Thermonuclear findings**: what would fail a harsh maintainability review.
- **Zero-tech-debt check**: compatibility/fallback paths deleted or justified by
  live callers.
- **Architecture check**: identify the main **Module**, its **Interface**, the
  **Implementation** hidden behind it, its **Depth**, the **Seam**, any
  **Adapter**, and the resulting **Leverage** and **Locality**.
- **Benchmark integrity check**: evidence that answer labels, expected answer
  text, and benchmark metadata do not leak into embeddings or retrieval content.
- **Residual risk**: what remains uncertain and how to verify it next.

## Stop Conditions

- Pause before schema migrations, dependency additions, public command contract
  changes, or full repo ingestion unless explicitly approved.
- Pause if a milestone requires production credentials or external service access
  beyond local test fixtures.
- Pause after 3 failed attempts at the same failing check and summarize the
  blocker with exact command output.
- Pause if the cleanest implementation requires reverting unrelated user changes.
- Pause if the goal starts expanding into general foxctl architecture cleanup not
  required for LongMem-ready memory recall.
