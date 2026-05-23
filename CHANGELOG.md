# Changelog

All notable changes merged to `main`, most recent first.

---

## [2026-05-23] !43 — Code-quality cleanup and MR readiness gates

Thermo-nuclear cleanup across frontend, API, runtime, storage, skills, and docs. Deletes unused GUI API wrappers and chat/activity surfaces, consolidates frontend DTOs through `packages/data`, hard-cuts legacy room binding and delivery fallback paths, moves jobs executor ownership into runtime, adds shared observability/path/time helpers, hardens CoVe/RLM local model auth, and adds Pi/Hermes integration checks plus real browser GUI smoke coverage.

---

## [2026-05-22] !42 — Autonomous mechanism memory collisions

Adds autonomous mechanism memory projection and collision planning for repo symbols. Wires Pi-backed blur/collision agents with balanced, far, alien, and far-alien bisociation modes. Adds Obsidian collision cache notes with typed machine records, cache listing/search, and optional cache reuse in future collision-agent runs.

---

## [2026-05-21] !40 — Visual flow editor with React Flow

Visual flow editor for foxctl built on React Flow with xterm.js terminal embeds and room binding. Go HTTP handlers for flow CRUD, TypeScript API client, React Flow canvas with custom node/edge components, room binding through to foxprox spawner.

---

## [2026-05-20] !39 — Scoring parser (open-collider port Phase 1a)

Ports scoring-table parsing, aggregate recalculation, and threshold-with-drift logic from the open-collider Python codebase to Go. First implementation slice of the open-collider room skill suite plan. Also includes pre-commit hook PATH setup, AGENTS.md error inspection guide, and Makefile improvements.

---

## [2026-05-19] !38 — Multi-agent context plane: vault, coordination, pipe protocol

70 Hermes tools across 10 categories. Flow orchestration (13 tools) with lifecycle, topology, and execution commands. Multi-agent coordination (9 tools) for agent discovery and room tasks. Pipe protocol for structured inter-agent messaging. Vault bridge, graph drafts, and knowledge promotion pipeline.

---

## [2026-05-19] !37 — Obsidian vault / knowledge plane integration

7 new vault tools completing the ContextWiki dual-plane model. Search vault index, promote evergreen drafts from learnings, append findings to existing notes, bridge repo docs ↔ vault notes, build repo graph drafts in the vault. Knowledge flow: observation → control plane → learning → vault promote → evergreen note.

---

## [2026-05-18] !35 — Hermes deep foxctl integration (42 tools)

Intelligence layer (12 tools): RepoIndex search/DAG/expand/open, code search, text/filesystem, codemaps. Memory layer (6 tools): memory_search (BM25+vector hybrid), session_recall, memory_put (CLI→Turso), memory_curator, session_extract_learnings. ContextWiki tools, room-agile integration, and more.

---

## [2026-05-18] !34 — Social research skills and Praze launch pipeline

Official-API social research provider support for X, Reddit, YouTube, Facebook Pages, and Instagram Graph. Foxctl social collector skills for each platform with dry-run call planning. Praze launch pipeline skill with Codex room provisioning, directed debate routes, Herdr relay notes, and prototype output. Adds repo gitleaks config.

---

## [2026-05-18] !36 — Document foxctl domain vocabulary

Root CONTEXT.md for short foxctl domain vocabulary. Apache 2.0 LICENSE. Broader/deferred terminology guidance moved to docs/glossary.md. Links from AGENTS.md and docs/README.md.

---

## [2026-05-17] !33 — Pi room-agile epic integration + herdr mux bridge

Integrates Pi as a first-class room-agile participant in foxctl. Adds herdr as a mux backend for room relay delivery.

---

## [2026-05-14] !32 — Revert "delete .dotfiles"

Reverts commit c854f3a5.

---

## [2026-05-14] !31 — Hard-cut ACA to ContextWiki

Hard-cut public ACA terminology to ContextWiki across CLI, API, config, docs, UI, eval fixtures, and generated validation artifacts. Deletes `/context/aca` docs compatibility page, removes old glossary entries. Replaces surfaces with canonical `FOXCTL_CONTEXTWIKI_*`, `contextwiki:`, and `--include-contextwiki` names.

---

## [2026-05-14] !30 — Benchmark budget reporting and docs surfaces

Benchmark manifest coverage, orientation harness, shell output cost/budget reporting, and retrieval eval budget/latency fields. Gather-context/native-agent baseline accounting and embedder health errors for semantic retrieval evals. Docs-site benchmark table refresh.

---

## [2026-05-13] !29 — Harden RLM helper pipeline and refactor scout

Generic LongCoT/RLM helper pipeline hardening. Deepened refactor scout hotspot internals and evidence/cochange helpers. Docs-site/homepage material refresh and Go benchmark runner.

---

## [2026-05-13] !28 — Foxctl docs homepage

Starlight splash homepage for foxctl docs. React component and responsive CSS using HeroUI. Wired Astro React and Tailwind v4 into docs-site package. Supply-chain vetted through Socket CLI and SFW.

---

## [2026-05-12] !27 — Repoindex graph navigation commands

Trace-path, smart-context, and blast-radius graph navigation commands. Projects graph results into anchors for code-oriented navigation. Endpoint validation, query-based node resolution, freshness gating, and transient SQLITE_BUSY retry handling.

---

## [2026-05-11] !25 — Hard cut Voyage embeddings

Removes active Voyage embedding and rerank providers. Defaults to local OpenAI-compatible Qwen models with 4096-dim storage. Adds local Qwen reranker provider. Updates Kubernetes, skill, launchd, docs, and GUI provider surfaces for local embedding/rerank config.

---

## [2026-05-10] !24 — Coordinator control plane and memory decay ranking

Merges coordinator control-plane MVP and memory decay ranking with v2 worker race fix. Binds coordinator semantic anchors to concrete process adapter owner.

---

## [2026-05-08] !21 — Integrate Pi tools and harden embedding lanes

Tracked Pi integration docs and extension tooling for foxctl repoindex, semantic search, memory, filesystem, and shell workflows. Memory/file embedding lanes hardened with queue operations, Turso/Qwen support, file-summary retrieval boundaries, and lane policy tests. Source-aware semantic chunk planning and symbol anchor text handling.

---

## [2026-05-07] !18 — Harden durable execution recovery

Moves v2 SQLite-family storage wiring off libsql names onto Turso adapter packages. Projection-backed orchestration recovery, card-first running-card recovery hooks, startup recovery wiring. Layer 1 runner hardening with durable request identity, idempotent event append, and LLM-chat model/tool effect journaling.

---

## [2026-05-07] !19 — Harden semantic anchor indexing

Wires semantic code anchors through indexing, retrieval, repo graph, and Obsidian bridge surfaces. Paced embedding queue support and incremental repoindex behavior for safer local embedding runs. Repo index build/enrich skill wrappers exposed through agent runtime, profiles, and MCP.
