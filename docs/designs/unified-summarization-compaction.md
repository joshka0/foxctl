---
title: Unified Summarization & Compaction
status: draft
owners: []
last_updated: 2025-12-25
---

# Unified Summarization & Compaction

## Problem Statement

- Summaries exist ad hoc (sessions) or not at all (tasks, memories); quality
  varies and lacks structure.
- Long-form artifacts (sessions, notes, memories) are stored verbatim;
  search/chat repeatedly pay token and latency costs.
- No consistent authority/context signal (graph/PageRank) applied to summaries.
- No periodic rollups (e.g., daily digests) to bound context.

## Goals

1. Produce high-signal, structured summaries for sessions, tasks, memories, and
   daily digests.
2. Compact large artifacts to short summaries and keep full text in CAS.
3. Make summaries first-class searchable/snippet sources in
   `code/semantic_search`.
4. Integrate graph/PageRank (when available) to highlight important items.
5. Keep summaries fresh via hooks/triggers with minimal overhead.

Non-goals

- Change envelope/wire contracts.
- Introduce new external dependencies.

## Current State

- Sessions: `session_summarize` writes a summary + embedding to `sessions.db`;
  template is lightweight.
- Tasks: title/description/notes/gotchas only; no structured summary or
  embedding derived from a compacted view.
- Memories: stored in `named_memory`; embeddings exist, but no compaction and
  summaries may equal full content.
- Search: `code/semantic_search` can synthesize (`summarize=true`) but largely
  operates on raw summaries/snippets; no stored per-entity structured summary.
- Graph: `graph_edges/graph_nodes` planned, not yet available for importance
  signals.

## Requirements

- Deterministic, sectioned summaries (limited length per section).
- Idempotent writes; UPSERT by entity/workspace.
- Keep full text in CAS; summaries in main tables.
- Embeddings derived from compacted summaries (not raw long text).
- Configurable triggers; hooks must be fast and safe (timeouts, no network
  expansion).

## Proposed Architecture

### Entities and Summary Shapes

- Session summary (JSON + text)
  - Sections: User Requests, Final Goal, Work Completed, Gaps, Next Steps,
    Must-Not-Do, Signals (env/ids).
  - Stored in `sessions.summary_struct` (JSON) and `sessions.summary` (text).
- Task summary
  - Sections: Problem, Attempts, Current Status, Risks, Gotchas, Next Steps.
  - Stored in `tasks.summary_struct` + `tasks.summary` (text); embedding from
    summary.
- Memory compaction
  - If payload exceeds threshold (e.g., >8KB), summarize to ≤2KB; store in
    `named_memory.summary`; full content pinned in CAS.
- Daily digest (workspace-bounded)
  - Aggregate today’s sessions/tasks/memories into `named_memory` entry
    `digest://YYYY-MM-DD` with summary + embedding + CAS of full digest.
- Optional graph context (when graph_nodes ready)
  - Include Top Linked Items (by degree/PageRank) per summary (3–5 entries).

### Data Model Changes (minimal)

- sessions.db: add `summary_struct JSON` (or TEXT JSON-encoded) and
  `generated_at`.
- tasks.db: add `summary TEXT`, `summary_struct JSON`, `summary_generated_at`.
- memory.db (named_memory): ensure `summary` is compact form; keep `result`/CAS
  for full text; `embedding` from summary.
- Optional: daily digest entries (`name` = `digest://YYYY-MM-DD`, `type` =
  `digest`).

### Triggers & Hooks

- Session end: run `session_summarize` with new template; overwrite
  summary/struct; re-embed from summary.
- Task status change (in_progress→review/complete or manual): run task
  summarizer skill; write summary/struct; re-embed.
- Memory put/update: if size > threshold, summarize and embed compact form; keep
  full text in CAS.
- Daily cron/hook: generate daily digest for workspace; write to named_memory
  with embedding.

### Summarization Implementation

- Reuse existing LLM providers (Gemini/Voyage/OpenRouter/Groq/Cerebras per
  availability).
- Deterministic prompts; cap sections (e.g., 3–5 bullets, 1–2 sentences).
- Length caps: summary ≤2KB; struct fields individually capped.
- Fallbacks: if LLM unavailable, pass through truncated text with a hint.

### Search Integration

- `code/semantic_search`:
  - Prefer stored summaries (sessions/tasks/digests) for snippets and
    embeddings.
  - Optionally fetch pagerank/importance when graph_nodes available.
  - Synthesis (`summarize=true`) should consume structured summaries first to
    reduce token use.

### Storage/Performance

- Use UPSERTs; small writes.
- Embedding generation from compacted summaries → lower latency/cost.
- Avoid per-file gopls/large ops in hooks; enforce timeouts.

## Phases

### Phase 1: Session & Task Summaries

- Update `session_summarize` template + storage of `summary_struct`.
- Add task summarizer skill/command: write summary + struct to tasks.db;
  re-embed.
- Wire task status change hook to invoke summarizer.

### Phase 2: Memory Compaction

- Add size threshold compaction in memory flows; store compact summary;
  re-embed.
- Preserve full text in CAS; set digests/pinning.

### Phase 3: Daily Digest

- Add digest skill to aggregate today’s sessions/tasks/memories; store as
  named_memory `digest://YYYY-MM-DD` with embedding.

### Phase 4: Search Integration

- Prefer stored summaries/snippets; ensure embeddings align.
- Add optional graph-aware “Top Linked Items” section when graph_nodes is live.

### Phase 5: Quality & Ops

- Metrics: generation success rate, summary size, token cost, latency, coverage
  (% entities summarized), synthesis success.
- Add doctor checks for stale summaries (age > N days) and missing embeddings.

## Risks & Mitigations

- LLM unavailability/cost: fallback to truncation + hint; keep ops short (<60s)
  with timeouts.
- Dimension mismatches: reuse existing embedding dimension validation; embed
  from compacted text.
- Data drift: add `generated_at` and regenerate on content change/status change.
- Token bloat: strict length caps and section limits.

## Open Questions

- Thresholds: default compaction size (propose 8KB) and summary cap (2KB).
- Provider priority: default to Gemini for consistency with existing embeddings;
  Voyage if only key present.
- Storage of struct: native JSON column vs. TEXT JSON; depends on current
  migrations strategy in sqlite.

## Success Metrics

- Coverage: >90% of sessions/tasks have summaries and embeddings.
- Size: summaries ≤2KB; embeddings from summaries only.
- Latency: summary generation P95 < 5s (per entity) with provider; <0.5s
  fallback.
- Search quality: improved precision@5 using stored summaries vs. raw text.

## Next Steps (implementation)

- Update `session_summarize` prompt and persistence for `summary_struct`.
- Implement task summarizer skill + hook on status change.
- Add memory compaction threshold + re-embed from compacted summary.
- Add daily digest skill and storage in named_memory.
- Adjust `code/semantic_search` to consume stored summaries by default and keep
  synthesis optional.
