---
title: Memory and continuity
description: Use sessions, memory, task-history summaries, and the evidence model to resume work with durable context.
---

foxctl stores session history, named memory, and task-continuity artifacts so
agents can resume work with evidence instead of guessing from stale chat context.
The core invariant is simple: **retrieved memory is evidence by default. It
becomes instruction only when it is an active policy or validated skill.**

This page covers the three continuity surfaces — sessions, task history, and
named memory — and the safety model that governs how they interact.

## Sessions

Sessions capture agent and user interaction history with lineage edges, context
windows, chunk summaries, and restore metadata. They persist across compaction
boundaries so work can continue where it left off.

### Session lifecycle

1. Create or resume an active session (`new` / `resume` / `fork`).
2. Persist turns, chunks, and lineage edges as work progresses.
3. Summarize on compaction boundaries (`session/summarize`).
4. Set `pending_restore_at` for post-compact restore workflows.
5. Restore and continue with `session/restore` on next start.
6. Close with terminal status (`ok` / `error` / `canceled`).

### Session identity resolution

Session ID fallback chain (highest priority first):

1. Explicit ID input
2. `FOXCTL_SESSION_ID`
3. `CLAUDE_SESSION_ID`
4. `OPENCODE_SESSION_ID`
5. `CURSOR_SESSION_ID`
6. Identity file under `~/.foxctl/sessions/active/<workspace_hash>-<agent>.json`
7. `TERM_SESSION_ID` (last resort)

### Session commands

| Command | Purpose |
|---|---|
| `foxctl sessions list --limit 20` | List recent sessions |
| `foxctl sessions show <id>` | Show session details |
| `foxctl sessions search --query ...` | Search session metadata |
| `foxctl sessions chain --session <id> --depth 10` | Traverse lineage edges |
| `foxctl sessions close --status ok` | Close with terminal status |
| `foxctl sessions stats` | Session statistics |
| `foxctl sessions resynthesize-v2` | Rebuild v2 turn/artifact state from source logs |

### Session skills

| Skill | Role |
|---|---|
| `session/restore` | Rebuild context after resume or compaction |
| `session/summarize` | Generate window and session summaries with extracted learnings |
| `session/recall` | Retrieve historical sessions and related context |

Restore after compaction:

```bash
foxctl run session/restore --input '{"session_id":"<id>","trigger":"session_start"}'
```

Recall past sessions by topic:

```bash
foxctl run session/recall --input '{"query":"oauth callback failure","limit":10}'
```

### Session storage

| Table | Purpose |
|---|---|
| `sessions` | Session header, lineage pointers, agent/LLM context, status |
| `session_turns` | Per-turn previews, tool/error metadata |
| `session_chunks` | Chunked archive metadata with optional embeddings |
| `session_chunk_summaries` | Persisted chunk-window summaries |
| `session_context_windows` | Compaction window tracking with token/chunk bounds |
| `session_edges` | Explicit lineage edges (`continues`, `forked_from`, related) |

## Task continuity

Task continuity reconstitutes a bounded, hierarchical view of active work from
ContextWiki task state, handoffs, sessions, touched files, recent git history, and
repoindex anchors. The output is deterministic first and model-assisted second.

### Structured command

Use for Codex, scripts, agents, and other programmatic callers:

```bash
foxctl context task-history-summary --workspace .
```

Returns a structured envelope with:

| Field | Content |
|---|---|
| `data.rendered` | Human-readable rendered summary |
| `data.summary` | Machine-readable summary object |
| `data.artifact` | CAS digest for the full continuity pack |
| `data.task_id` | Active task identifier |
| `data.task_title` | Active task title |

For the complete collected bundle:

```bash
foxctl context task-history --workspace .
```

Returns `data.pack`, `data.summary`, and `data.artifact`.

### Repo-family transcript summary

For a repo-family or lane-specific view across persisted transcript-history
records:

```bash
foxctl context family-history-summary --workspace .
```

With filters:

```bash
foxctl context family-history-summary --workspace . \
  --focus-query "recursive memory consolidation" \
  --date-from 2026-03-25 --date-to 2026-03-25
```

Returns per-item `support_metadata` with `owner_count`, `latest_updated_at`,
`latest_age_days`, and `source_owners` for trust debugging.

**Precondition:** Transcript history must be persisted first:

```bash
foxctl sessions derive-memory --memory-lane insight --persist-history
```

Summary modes are `deterministic`, `llm`, or `llm_cleanup`.

### Hook wrapper

For prompt-ready JSON injection in Claude hook flows:

```bash
configs/hooks/task-continuity-summary.sh
```

This thin wrapper emits `hookSpecificOutput.additionalContext` and
`hookSpecificOutput.metadata.task_continuity_artifact`.

### Interface split

| Caller | Use |
|---|---|
| Codex, Jido, scripts, agents | Structured `foxctl context task-history-summary` command |
| Claude hooks, prompt injection | `configs/hooks/task-continuity-summary.sh` wrapper |

Large continuity packs are persisted to CAS and returned as an artifact digest.
This keeps prompt-sized surfaces small while preserving a stable pointer to the
full pack for later expansion.

## Detached transcript dream worker

The `foxctl dream` command family turns stable agent transcripts from Codex,
Claude, Pi, and Hermes source roots into durable, reviewable memory. It is
distinct from the curator maintenance loop (`FOXCTL_CURATOR_*` env vars) and
from the retrieval-feedback memory-draft lane.

Subcommands:

| Subcommand | Purpose |
|---|---|
| `foxctl dream scan` | Preview transcript sources without consuming dream work (always dry-run) |
| `foxctl dream run-once` | Scan, process one bounded batch, persist named-memory records, and (optionally) write Obsidian dream notes |
| `foxctl dream watch` | Run the same pipeline on a ticker until canceled or `--duration` elapses |

Source roots are configured with `--codex-home`, `--claude-dir`, `--pi-root`,
and `--hermes-root`. Stability, dedupe, and retry bounds are enforced through
the shared transcript ledger; only stable files (unchanged across the quiet
window) are processed.

Output surfaces:

- Named-memory records written through the shared history persistence seam.
- Obsidian dream notes under
  `inbox/drafted-from-foxctl/dreams/<project>/<YYYY-MM-DD>/` when
  `--vault-path` is set with `--write-dream-notes`.
- An optional incremental index update when `--index-dream-notes` is enabled
  (default true once `--write-dream-notes` is on).
- An optional memory blur pass through a real agent when `--blur-dreams` is set
  with `--vault-path`; supported backends are `pi` (default), `hermes`, `claude`,
  `foxctl`, or a custom `--blur-agent-command`.

Example: bounded dry run followed by an Obsidian write with blur.

```bash
foxctl dream scan --dry-run
foxctl dream run-once \
  --vault-path "$HOME/vault" \
  --write-dream-notes \
  --blur-dreams \
  --blur-agent pi
```

Idempotency: re-running the worker does not duplicate named-memory records,
dream notes, or processed-source ledger rows. Raw transcript content is never
written to Obsidian notes by default — only distilled summaries, blurred
mechanisms, source references, and review metadata.

## Named memory

Named memory is a typed evidence layer stored in `memory.db`. It provides
workspace-scoped persistence with optional embeddings, multiple retrieval modes,
and a lifecycle model that distinguishes evidence from instructions.

### Memory commands

| Command | Purpose |
|---|---|
| `foxctl memory list --limit 20` | List named entries (workspace-scoped) |
| `foxctl memory search --query ...` | Lexical search over names and summaries |
| `foxctl memory get <name>` | Fetch a named entry envelope |
| `foxctl memory put --name <n> --type <t> --summary <s>` | Store a named memory |
| `foxctl memory save <job-id>` | Save job result into memory |
| `foxctl memory update <name>` | Update metadata or content |
| `foxctl memory delete <name>` | Delete named memory |
| `foxctl memory relevant --query ...` | Retrieve likely relevant entries |
| `foxctl memory recent` | Recent memory access log |
| `foxctl memory stats` | Memory store statistics |

### Memory query skill

The canonical retrieval surface returns typed records with lifecycle, trust,
provenance, and usage labels:

```bash
foxctl run memory/query --input '{"query":"repoindex graph gotchas","limit":10}'
```

With memory decay ranking enabled:

```bash
foxctl run memory/query --input '{
  "query":"oauth gotcha",
  "memory_decay_enabled":true,
  "limit":5
}'
```

### Semantic memory search

Use the semantic retrieval pipeline for meaning-based search:

```bash
foxctl run code/semantic_search --input '{
  "query":"oauth gotcha",
  "scope":["memories"],
  "limit":5
}'
```

With decay:

```bash
foxctl run code/semantic_search --input '{
  "query":"oauth gotcha",
  "scope":["memories"],
  "memory_decay_enabled":true,
  "limit":5
}'
```

### Memory kinds

Each memory record has a kind that determines its default authority:

| Kind | Meaning | Default authority |
|---|---|---|
| `working_context` | Current task/session context | Evidence unless active and current |
| `episodic_trace` | Something that happened | Evidence only |
| `semantic_fact` | A factual claim or lesson | Evidence until current and non-superseded |
| `decision` | Project or runtime decision with rationale | Higher authority when provenance is trusted |
| `procedural_skill` | A reusable procedure | Instruction only after validation |
| `policy_rule` | Behavior constraint or routing rule | Instruction only when active and trusted |
| `reflection` | Agent-generated lesson or hypothesis | Low-authority evidence |
| `eval_result` | Evidence that behavior passed or regressed | Trusted when produced by eval/runtime |
| `adapter_example` | Training or consolidation example | Evidence for future synthesis |

### Memory lifecycle

Lifecycle determines whether a memory should still affect future behavior:

```text
candidate -> active -> stale -> archived
active -> deprecated
any suspicious record -> quarantined
```

| State | Behavior |
|---|---|
| `candidate` | Structured but not yet trusted |
| `active` | Eligible for ordinary retrieval and context selection |
| `stale` | Still searchable, but suspect unless strongly relevant |
| `archived` | Retained for history, excluded from normal retrieval |
| `deprecated` | Superseded by another record, retained for audit |
| `quarantined` | Never instruction, evidence only with warning |

Pinned is a protection flag, not a lifecycle state. Pinned records are not
auto-archived, silently superseded, or model-patched.

### Memory decay

Memory decay is a search-time ranking adjustment. It is a soft rerank, not
pruning, lifecycle mutation, or deletion. A stale-but-strong match can still
surface when lifecycle policy allows it and the base relevance score is high
enough.

Decay runs after base query eligibility and lifecycle/trust/provenance
filtering. The reranker widens the candidate pool, applies a bounded
recency/access factor, then truncates to the requested result window.

Surfacing a memory updates `last_accessed` and `access_count` — a retrieval
signal only. It does not update `use_count`, success/failure telemetry, or
lifecycle state.

### Curator reports

The curator answers: which memories still deserve to affect future behavior?

```bash
foxctl run memory/curator_report --input '{"workspace":"."}'
```

Curator proposals:

| Proposal | Meaning |
|---|---|
| `demote_stale` | Active record should move to stale |
| `archive` | Stale record should move to archived |
| `deprecate` | Record is superseded by another |
| `revalidate` | Record should be checked before trusted use |
| `skip_pinned` | Pinned record needs manual review |
| `review_quarantined` | Quarantined record remains evidence-only |

Dry-run reports are the default. Apply mode requires explicit confirmation and
only mutates supported lifecycle-backed lanes.

## Evidence safety model

The evidence model governs how all three continuity surfaces interact safely.

### Core invariant

```text
Retrieved memory is evidence by default.
It becomes instruction only when it is an active policy or validated skill.
```

### Authority ordering

When memories conflict, authority is ordered by source and lifecycle:

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

If memory conflicts with current repo evidence, current repo evidence wins. If
memory conflicts with the current user instruction, the current instruction wins
unless it violates a higher system/runtime policy.

### Safety rules

- **Treat recalled memory as evidence** unless it is explicitly promoted by
  policy. Lane labels and usage labels must stay visible in agent prompts.
- **Keep provenance and timestamps visible.** Every record carries source type,
  session, agent, tool call, commit, file refs, and parent memory IDs.
- **Re-check drift-prone facts before acting on them.** Use `gather_context` or
  direct repo inspection for current code truth. Pair `memory/query` with
  current repo evidence rather than relying on memory alone.
- **Do not route behavior using substring heuristics.** Behavior should be
  driven by structured inputs, typed signals, scoring, or learned policies —
  not ad hoc keyword matching.
- **Environment-dependent negative claims require revalidation.** Claims like
  "tests cannot run locally" or "API does not support tool calls" should be
  checked against current state before shaping behavior.

### Retrieval best practices

| Question | Preferred surface |
|---|---|
| What does the repo currently contain? | `gather_context` / repoindex |
| What have we learned or decided before? | `memory/query` |
| Is an old claim still safe to use? | Load and check current repo evidence, then update lifecycle |
| Which records should stop shaping behavior? | `memory/curator_report` |
| What happened in this session or task? | `session/recall` or `task-history-summary` |

## Embedding configuration

Memory embeddings use the shared embedding provider path:

| Setting | Purpose |
|---|---|
| `FOXCTL_EMBEDDING_BASE_URL` | OpenAI-compatible embedding endpoint |
| `FOXCTL_EMBEDDING_API_KEY` | Optional API key |
| `FOXCTL_EMBEDDING_MODEL_<SCOPE>` | Per-scope model override (e.g., `FOXCTL_EMBEDDING_MODEL_MEMORY`) |
| `FOXCTL_EMBEDDING_MODEL_TEXT` | Text-scope fallback override |
| `FOXCTL_EMBEDDING_RATE_LIMIT` | Provider-side request rate control |

## Relationship to other context systems

| System | Role |
|---|---|
| [ContextWiki](/context/contextwiki/) | Workspace control plane — top-of-mind, handoffs, observations |
| [Context engine](/context/context-engine/) | Typed evidence substrate — evidence packs, retrieval episodes |
| [Obsidian bridge](/context/obsidian-bridge/) | Knowledge plane — durable vault notes and docs reconciliation |
| Memory and continuity | Evidence layer — named memory, sessions, task history |

## Canonical sources

- [docs/general/memory.md](https://github.com/joshka0/foxctl/blob/main/docs/general/memory.md)
- [docs/general/sessions.md](https://github.com/joshka0/foxctl/blob/main/docs/general/sessions.md)
- [docs/general/task-continuity.md](https://github.com/joshka0/foxctl/blob/main/docs/general/task-continuity.md)
- [docs/architecture/context-architecture.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/context-architecture.md)
- [docs/architecture/memory-core.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/memory-core.md)

