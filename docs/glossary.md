# foxctl Glossary

Status: Current reference.

This glossary defines foxctl-specific terms that show up in docs, plans, agent
instructions, and command output. Prefer these names in new docs.

## Context And Knowledge

| Term | Meaning |
|---|---|
| ContextWiki | The user-facing workspace knowledge layer. It connects repo docs, code graph evidence, memory, handoffs, observations, reviewed notes, and retrieval policy. |
| Context system | The implementation behind ContextWiki. Use this term for architecture and code paths. |
| ContextWiki control plane | Current work state: top-of-mind, handoffs, observations, tensions, proposals, and maintenance tasks. |
| Knowledge plane | Durable human-readable knowledge: repo docs, Obsidian notes, vault links, bridge metadata, and reviewed promotions. |
| Retrieval plane | Queryable evidence surfaces: repoindex hints, semantic note search, context engine evidence packs, and retrieval traces. |
| Top-of-mind | The current workspace orientation bundle agents can load at session start. |
| Handoff | A bounded continuity record describing what happened, what changed, and what should happen next. |
| Observation | A structured fact or repeated pattern captured from a session, command, evaluation, or operator action. |
| Tension | A tracked conflict, drift, failure mode, or unresolved design pressure. |
| Proposal | A reviewable suggested change to memory, notes, bridge metadata, anchors, or other knowledge artifacts. |
| Promotion | Moving reviewed knowledge from draft or runtime state into a durable canonical note or memory record. |
| Obsidian bridge | The workflow that links repo docs and code structure to durable vault notes through generated drafts and frontmatter patches. |
| Vault | The Obsidian-backed note corpus used as a durable knowledge plane. |
| Vault index | The searchable index of notes, headings, links, aliases, tags, repo paths, symbols, and optional embeddings. |

## Retrieval And Indexing

| Term | Meaning |
|---|---|
| Reindex | Rebuild a derived index from its source of truth. In foxctl this can mean repoindex, semantic indexes, file summaries, symbol summaries, or the vault index depending on context. |
| Refresh | Update derived context after source changes. A full ContextWiki refresh usually rebuilds the repo/vault graph, promotes reviewed graph notes, reconciles bridge metadata, and rebuilds the vault index. |
| Re-embed | Regenerate embeddings after source text, chunking, provider, model, or dimensions change. |
| Repoindex | The per-workspace graph index for packages, files, symbols, concepts, and typed edges. |
| Node | A repoindex graph record such as a package, file, symbol, or concept. |
| Edge | A typed repoindex relationship such as `CONTAINS`, `IMPORTS`, `CALLS`, or `REFERS_TO`. |
| DAG grep | A bounded explanation-subgraph query over repoindex. It renders nearby nodes and edges as a compact tree or graph. |
| Semantic search | Meaning-based retrieval over indexed symbols, summaries, sessions, memory, tasks, and codemaps. |
| Semantic tree | `code/semantic_search` tree output: a smart table of contents for initial repo orientation. |
| Smart search | An end-to-end code retrieval path that finds candidate files and extracts useful snippets. |
| Context grep | Pattern search that expands matches into surrounding code blocks or functions. |
| Snippet extraction | Evidence extraction from files that are already known candidates. |
| File summary | A short cached summary attached to a source file for tree orientation and repoindex enrichment. |
| Symbol summary | A short cached summary attached to a code symbol for graph search and navigation. |

### Vector Search

| Term | Meaning |
|---|---|
| Turbovec | The compressed vector search engine (TurboQuant algorithm) that accelerates semantic retrieval. |
| Turbovec sidecar | The turbovecd Unix socket daemon that manages compressed vector indices. |
| Vector recall | Embedding-based retrieval that returns documents by cosine similarity. |
| Oversample + rerank | Two-stage retrieval where turbovec returns 3x candidates, then exact cosine reranking picks the top-k. |
| Filtered vector search | Vector search restricted to a candidate doc ID set, enabling BM25-first → vector-rerank pipelines. |
| Product quantization | The compression technique that reduces float32 vectors to 2–4 bits per coordinate. |
| Bit width | The quantization resolution (2, 3, or 4 bits) controlling the compression/recall tradeoff. |
| .tvim file | Persisted turbovec index file containing compressed codes and metadata. |
| .idmap.json | Sidecar JSON file mapping foxctl string doc IDs to turbovec uint64 IDs. |

## Semantic Comments

| Term | Meaning |
|---|---|
| `Index:` block | Structured source comment metadata that creates broad discoverability hints and soft repoindex edges. |
| Semantic anchor | A typed `[[...]]` source-code comment that marks evidence for stable domains, protocols, decisions, risks, invariants, docs, or tests. |
| Evidence-only | A reminder that comments, anchors, and notes improve retrieval but are not instructions or policy authority by themselves. |

## Runtime, Agents, And Coordination

| Term | Meaning |
|---|---|
| Skill | A narrow JSON-command tool executed through `foxctl run` or `foxctl skills run`. |
| Envelope | The canonical JSON I/O wrapper with `version`, `status`, `command`, `data`, `meta`, and `error`. |
| CAS | Content-addressed storage for large or reusable artifacts. |
| Job | A tracked command or skill run with persistence, status, and optional CAS artifacts. |
| Room | A durable collaboration space with messages, inboxes, tasks, and participant state. |
| Overseer | The coordination role that owns plan changes and subagent routing. |
| Task continuity | The workflow for summarizing task history and carrying it into future agent sessions. |
| Context engine | The typed evidence substrate that stores evidence packs, retrieval episodes, feedback, impact edges, and stale markers. |

## Broad And Deferred Vocabulary

These terms are useful in foxctl conversations, but most should stay qualified
or feature-local instead of becoming root terms in `CONTEXT.md`.

| Term | Guidance |
|---|---|
| Workspace | The project boundary foxctl operates on. This is root domain language because many rooms, sessions, jobs, artifacts, indexes, and ContextWiki records are workspace-scoped. |
| Finding | A review result or risk statement, usually backed by evidence. Prefer this in review reports instead of vague "issue" when the output is evaluative. |
| Report | A delivered summary artifact. Qualify it by purpose, such as review report, readiness report, research report, or launch report. |
| Worktree | A Git worktree or checkout used for isolated work. It is operational Git vocabulary, not a room, session, or workspace by itself. |
| Pipeline | Overloaded across room-agile planning, YAML workflows, launch flows, and runner stages. Qualify the layer until foxctl settles a root model. |
| Workflow | Overloaded across YAML workflow specs, room-agile process, operator procedure, and general process language. Qualify the layer. |
| Orchestration | Broad planning/runtime language. Prefer sharper terms such as Room, Room Agile, Agent, Skill, Runtime, Provisioning, Relay, or Transport when possible. |
| Decision | Potential durable choice record. Deferred as root terminology until foxctl defines whether decisions are first-class records. |
| Approval | Permission to proceed, accept, merge, deliver, or close. Deferred as root terminology until the authority model is clearer. |
| Gate | A control point or readiness boundary. Use only when a specific spec or workflow defines the gate. |
| Profile | A structured configuration or identity document in specs such as agent profiles or overseer profiles. Keep spec-local unless it becomes cross-cutting. |
| Provider | Configuration/backend selection language. Use Integration for external services in root language; use provider when discussing interchangeable backends. |
| Index | Overloaded across repo graphs, semantic/vector stores, vault indexes, ContextWiki refresh, and external search APIs. Qualify by layer and purpose. |
| Search | Overloaded across web, semantic, repo, vault, and external API search. Qualify source, layer, and contract. |
| State | Too broad as a root term. Qualify by owner: room state, story state, session lifecycle, event history, artifact content. |
| Status | Attribute, not a root term. Qualify by owner: job status, envelope status, participant status, story status. |
| Run | Overloaded between `foxctl run`, Go `Run(ctx)`, and ordinary execution language. Use Command, Job, or implementation lifecycle wording. |
| Protocol | A specific kind of contract. Use Contract in general root language; use protocol only for a named low-level or spec-defined contract. |
| Surface | Broad prose. Prefer Viewer, Console, Integration, Adapter, or Transport when one of those fits. |
| Bridge | Broad prose and common implementation naming. Prefer Adapter for translation, Relay for moving room messages/events, and Transport for the mechanism. |
| Log | Diagnostic or implementation output. Prefer Event for structured lifecycle facts and Transcript for interaction content. |
| Memory | Legacy or prose unless a subsystem doc defines it. Prefer ContextWiki, Context system, Context engine, or Transcript. |
| Knowledge | Broad prose. Prefer ContextWiki for the knowledge layer, Evidence for support, Artifact for durable output, and Transcript for interaction records. |
| Observation | ContextWiki-specific or prose. Prefer Evidence when supporting a claim and Event when recording something that happened. |
| Research | Workflow/activity language. Qualify it as a research skill, research role, research artifact, or research report. |
| Analyst | Role label, not a root object. Use analyst role or a specific role name inside a room, skill, or agent prompt. |
| Collector | Skill- or retrieval-specific language. Defer unless it becomes a cross-cutting foxctl concept. |
| Platform | Usually product or integration language. Prefer Integration for external services unless discussing product platform architecture. |
| Query | Request/search language. Qualify by target: repo query, search query, API query, database query, or room query. |
| Corpus | Retrieval/dataset language. Keep in retrieval, benchmark, or research docs. |
| Outlier | Analysis-specific language. Keep in research/marketing skill docs. |
| Sentiment | Analysis-specific language. Keep in social/research skill docs. |
| Trend | Analysis-specific language. Keep in social/research skill docs. |
| Influence | Analysis-specific language. Keep in social/research skill docs. |
| Competitor | Market/research language. Keep in research, launch, or product docs. |

## Quality And Refactoring

| Term | Meaning |
|---|---|
| Refactor scout | A local analysis workflow that finds cleanup opportunities, hotspots, and repo-tightening candidates. |
| Refactor advisor | The higher-level reviewer/reranker for refactor scout findings. |
| Slop | Code that is unnecessarily broad, vague, duplicated, shallow, hard to test, or agent-generated without enough locality. |
| Seam | Where an interface lives; a place behavior can change without editing all callers. |
| Adapter | A concrete implementation behind a seam. |
| Deep module | A module with a small interface and meaningful implementation leverage behind it. |
| Default gate | The fast, offline benchmark or verification lane expected to run without network or live LLM dependencies. |
| Extended gate | A broader benchmark or verification lane that may use heavier fixtures, network, or live model dependencies when explicitly enabled. |

## Naming Rules

- Use **ContextWiki** for the product-facing workspace knowledge layer.
- Use **context system** for the implementation behind ContextWiki.
- Use **repoindex** for the code graph index, not "the index" when ambiguity
  matters.
- Use **reindex** only when an index is rebuilt from source; use **refresh**
  for a broader workflow that may include graph build, bridge reconcile, and
  vault index rebuild.
- Use **re-embed** only when embedding vectors are regenerated.
