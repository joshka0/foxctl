# General Docs Index

Core subsystem guides, API references, and operational documentation for `foxctl`.

## Architecture & System Overviews

- [architecture.md](architecture.md) - Top-level architecture overview and system context.
- [agent-daemon.md](agent-daemon.md) - Agent daemon architecture, mailbox-driven runner, and lifecycle.
- [runtime-orchestration.md](runtime-orchestration.md) - Runtime orchestration workflows and execution flow.
- [api-server.md](api-server.md) - API server reference, endpoints, and deployment notes.
- [context-and-observability.md](context-and-observability.md) - Context plane, observability hooks, and telemetry.

## Subsystem References

- [skills.md](skills.md) - Skill contracts, execution model, and skill lifecycle.
- [hooks.md](hooks.md) - Hook configuration, event dispatch, and action merging.
- [memory.md](memory.md) - Named memory persistence, retrieval, and companion memory.
- [sessions.md](sessions.md) - Session lifecycle, lineage, and restoration.
- [storage.md](storage.md) - Persisted and ephemeral state, storage backends.
- [events.md](events.md) - Event schema, persistence, and operational audit.
- [persistence.md](persistence.md) - Persistence layer design, CAS, and data durability.
- [companion-memory.md](companion-memory.md) - Companion-specific memory models and retrieval.

## Search, Retrieval & Intelligence

- [search.md](search.md) - Semantic retrieval, reranking, and repo graph search.
- [repoindex.md](repoindex.md) - Repo graph index terminology, build/query commands, and language coverage.
- [code-search-evals.md](code-search-evals.md) - Stable code-search evaluation suites and policies.
- [retrieval-evals.md](retrieval-evals.md) - ACA retrieval evaluation suites and expected bands.
- [refactor-scout.md](refactor-scout.md) - Local refactor scout workflow, seam vocabulary, and advisor integration.
- [rlm-context.md](rlm-context.md) - RLM query-time runtime, context gathering, and reduction.

## Messaging & Collaboration

- [message-passing.md](message-passing.md) - Message-passing architecture and envelope contracts.
- [message-passing-quickstart.md](message-passing-quickstart.md) - Quick start for message-passing patterns.
- [tmux-collaboration.md](tmux-collaboration.md) - tmux-based live collaboration, pane inspection, and ACA promotion.
- [room-runtime-adoption-pass.md](room-runtime-adoption-pass.md) - Room-runtime adoption matrix and remaining gaps.
- [v1-specs.md](v1-specs.md) - Foundational v1 protocol specifications.

## Operations & Maintenance

- [gotchas.md](gotchas.md) - Common pitfalls and their solutions.
- [task-continuity.md](task-continuity.md) - Deterministic task continuity packs and artifact-backed delivery.
- [embedding-rebuilds.md](embedding-rebuilds.md) - Rebuild commands for embedding stores after provider/model changes.
- [core-package-coverage.md](core-package-coverage.md) - Machine-friendly core package coverage matrix.
- [exa_mcp_context_filter.md](exa_mcp_context_filter.md) - Exa MCP context filtering and integration notes.

## Event-Driven & External Systems

- [foxcular-events.md](foxcular-events.md) - Foxcular event system, schema, and consumption patterns.
- [api-server.openapi.yaml](api-server.openapi.yaml) - OpenAPI specification for the foxctl API server.

## Policy & Agent Behavior

- [agent-policy-and-prompts.md](agent-policy-and-prompts.md) - Agent policy rules and system prompt guidelines.
