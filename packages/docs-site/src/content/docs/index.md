---
title: foxctl
description: A local control plane for code understanding, retrieval, agent coordination, skills, context, and operational workflows.
---

**foxctl** is a local control plane for code understanding, retrieval, agent coordination, skills, context, and operational workflows.

It combines a Go CLI, installable skills, semantic and graph-based code retrieval, session continuity, memory storage, MCP serving, and durable multi-agent room orchestration. The repository is primarily Go, with Bun-based packages for the web GUI.

## What foxctl does

| Capability | What it means |
|---|---|
| **Skill execution** | Run local skills via `foxctl run` — job-tracked, ephemeral, or direct |
| **Code retrieval** | Semantic search, smart search, context grep, and repo graph navigation |
| **Repo indexing** | Build a queryable graph of symbols, calls, references, and concepts across Go, TypeScript, and Elixir |
| **Agent coordination** | Spawn persistent agents with roles, execution modes, and tool assignments |
| **Room orchestration** | Durable collaboration timelines with messages, tasks, and relay backends |
| **Context engine** | Typed evidence, memory claims, impact tracking, and retrieval feedback |
| **MCP serving** | Model Context Protocol server for IDE and editor integrations |
| **Storage & CAS** | SQLite/libsql/Turso/Postgres backends with content-addressable artifact storage |

## First steps

1. [Install foxctl](/start/install-first-run/) — get the CLI, skills, and first verification commands
2. [Overview](/start/overview/) — first commands, safety posture, and navigation patterns
3. [Documentation map](/start/docs-map/) — how docs are organized and what status labels mean

## Explore by topic

| Topic | Start here |
|---|---|
| Architecture | [System overview](/architecture/system/), [Design principles](/architecture/design-principles/), [Runtime](/architecture/runtime/) |
| Agents | [Agent lifecycle](/agents/lifecycle/), [Orchestration](/agents/orchestration/) |
| Workflows | [Agents and rooms](/workflows/agents-and-rooms/), [Repo navigation](/workflows/repo-navigation/) |
| Skills | [Runtime and install](/skills/runtime-and-install/) |
| Retrieval | [Search and index](/retrieval/search-and-index/), [Repoindex and DAG grep](/retrieval/repoindex-and-dag-grep/) |
| Context | [ACA](/context/aca/), [Context engine](/context/context-engine/), [Obsidian bridge](/context/obsidian-bridge/) |
| Memory | [Continuity](/memory/continuity/) |
| Integrations | [Providers and MCP](/integrations/providers-and-mcp/), [Hooks](/integrations/hooks/), [Chat platforms](/integrations/chat-platforms/) |
| Operations | [Gotchas](/operations/gotchas/), [Troubleshooting](/operations/troubleshooting/), [Observability](/operations/observability/) |
| Reference | [CLI](/reference/cli/), [Command map](/reference/command-map/), [Protocol v1](/reference/protocol-v1/) |
| Deployment | [Kubernetes](/deployment/kubernetes/) |
| Quality | [CI and evals](/quality/ci-and-evals/) |

## Release labels

Docs and features in this site use status labels:

| Label | Meaning |
|---|---|
| **Current** | Supported by current docs and source. |
| **Planned** | Active plan or roadmap item, not official behavior yet. |
| **Experimental** | Available for evaluation, but not the default production path. |
| **Archive** | Historical or generated material kept for provenance. |

## Canonical sources

This docs site is a guided surface over the repository documentation. The canonical sources remain in the repo:

- [`README.md`](https://github.com/joshka0/foxctl/blob/main/README.md) — product overview and quick start
- [`AGENTS.md`](https://github.com/joshka0/foxctl/blob/main/AGENTS.md) — contributor and AI assistant operating rules
- [`docs/README.md`](https://github.com/joshka0/foxctl/blob/main/docs/README.md) — canonical docs navigation index
- [`docs/DOC_LIFECYCLE.md`](https://github.com/joshka0/foxctl/blob/main/docs/DOC_LIFECYCLE.md) — documentation lifecycle policy
