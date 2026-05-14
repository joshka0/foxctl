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
| ACA | Legacy abbreviation for the ContextWiki/context-system work. Avoid in new prose except when explaining old names. |
| Legacy `aca_` identifiers | Existing eval modes, environment variables, persisted table names, and internal tool names may keep `aca_` for compatibility. Prefer new `contextwiki` aliases where available and explain old names as ContextWiki behavior in prose. |
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
- Avoid **ACA** in new docs except to explain historical names.
