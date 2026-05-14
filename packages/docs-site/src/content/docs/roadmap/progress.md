---
title: Progress
description: Current foxctl capabilities, active in-progress work, and validation paths.
---

This page separates what foxctl currently documents as usable behavior from work
that is still evolving through plans, evals, and benchmark validation.

## Current

| Area | Current state | Docs |
|---|---|---|
| Production docs | Starlight site with Cloudflare Pages deploy path, docs build checks, link checks, and production verification | [Verification](/production/verification/) |
| Repo retrieval | Semantic search, repoindex, DAG grep, codemaps, and evidence packaging | [Repoindex and DAG grep](/retrieval/repoindex-and-dag-grep/) |
| Skills runtime | Job-tracked, ephemeral, and direct execution paths with stable envelopes | [Runtime and install](/skills/runtime-and-install/) |
| Agents and rooms | Agent lifecycle, orchestration, room timelines, mailbox asks, and collaboration flows | [Agent orchestration](/agents/orchestration/) |
| Storage | CAS, persistence, vectors, Turso, and Postgres behavior | [CAS and persistence](/storage/cas-and-persistence/) |
| Integrations | LLM providers, MCP, OpenAPI, plugins, hooks, chat adapters, and Obsidian bridge | [Integration status](/integrations/status/) |
| Benchmarks | Curated Go benchmark runner for hot packages and repeatable local captures | [Benchmarks](/quality/benchmarks/) |

## In progress

| Area | Why it matters | Plan-backed status |
|---|---|---|
| Durable execution recovery | Keeps agent and runtime work recoverable after crashes | Active plan-backed runtime work |
| Runtime side-effect safety | Makes retries and event append behavior idempotent | Active plan-backed safety work |
| Refactor intelligence | Finds hotspots, repeated change patterns, and deterministic refactor targets | Active implementation and validation work |
| Slop function detection | Flags AI-generated sprawl and low-confidence code shape | Active detection backlog |
| RLM helper runtime | Supports recursive helper pipelines, LongCoT evals, and smolvm experiments | Experimental |
| ContextWiki memory evolution | Improves self-corrective retrieval and memory derivation | Active research and plan-backed work |
| Room workpacks and milestones | Makes multi-agent work auditable through evidence lanes and exit policies | Active room workflow backlog |
| OpenSandbox integration | Adds sandboxed workspace execution as an adapter surface | Planned integration work |

## Promotion rule

Do not describe in-progress work as current operator behavior until it has:

1. A stable command or API contract
2. Deterministic tests or evals for the expected behavior
3. Documentation in the matching current docs area
4. A benchmark or operational check when the feature affects hot paths

Plan documents remain useful for contributors, but current docs must stay tied
to behavior that exists in the repository.
