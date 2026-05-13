---
title: Overview
description: First commands, safety posture, and main navigation patterns for foxctl.
---

foxctl is a Go-based framework and CLI for skills, repo indexing, retrieval,
context tooling, agent coordination, and room workflows. This page covers the
first commands to run, the project's safety posture, and how to navigate the
documentation.

## First commands

From the repository root, verify the build and explore the tool surface:

```bash
foxctl version
```

Get a semantic tree view of the codebase:

```bash
foxctl run code/semantic_search --input '{"query":"storage memory","format":"tree","limit":25}'
```

Build the repo graph index for call and reference navigation:

```bash
foxctl index repo build --workspace . --go --typescript --elixir
```

Query the graph for an explanation subgraph:

```bash
foxctl run code/dag_grep --input '{"query":"buildEvidencePack","workspace":".","render":"tree","edge_sets":["structural"],"depth":2,"budget":80,"k":5}'
```

Use `./bin/foxctl` from this checkout when `foxctl` on `PATH` is a bundled wrapper without the command you need.

## Safety posture

foxctl is designed to be safe, deterministic, and testable. The following rules govern how the project behaves:

### Envelope contract

All CLI and skill I/O uses a canonical JSON envelope (Protocol v1):

```json
{
  "version": 1,
  "status": "ok|error|progress",
  "command": "skill/name",
  "data": { ... },
  "meta": { "ts": "2026-01-12T12:00:00Z" },
  "error": {}
}
```

- `meta.ts` is always present (RFC3339 UTC)
- `status:"ok"` means `error` fields are empty
- `status:"error"` means `error.code` and `error.message` are required
- stdout is envelopes only; stderr is logs only

Never change `meta.*` fields or envelope shape without a spec update. Downstream tooling (hooks, GUIs, golden tests) depends on stable envelope shape.

### WASI isolation

Core v1 mandates `network:"none"` for WASI skill manifests. Skills run in isolated sandboxes with no network access by default.

### Large outputs

Outputs larger than 64KB or blob-like data are persisted to CAS and returned as `data.summary` + `data.artifact` pointers. This prevents OOM from giant in-memory buffers.

### Keyword heuristics

Do not route, classify, promote, or suppress behavior using ad hoc substring or keyword matching. These heuristics are brittle. Prefer explicit schemas, typed signals, scored features, tests, or learned policies.

### Path policy

Paths must go through `policy.PathValidator`. WASI skills are workspace-confined with rlimits enforced.

## Main navigation paths

| Need | Page |
|---|---|
| Setup and first run | [Install and first run](/start/install-first-run/) |
| Skill execution | [Skills runtime](/skills/runtime-and-install/) |
| Search and snippets | [Search and index](/retrieval/search-and-index/) |
| Graph navigation | [Repoindex and DAG grep](/retrieval/repoindex-and-dag-grep/) |
| Context and memory | [ACA](/context/aca/) and [Memory](/memory/continuity/) |
| Agents and rooms | [Agent lifecycle](/agents/lifecycle/) and [Rooms](/collaboration/rooms/) |
| Integrations | [Providers and MCP](/integrations/providers-and-mcp/) |
| Operations | [Gotchas](/operations/gotchas/) and [Verification](/production/verification/) |

## Quick command reference

```bash
# Skills
foxctl run code/semantic_search --input '{"query":"error handling","limit":10}'
foxctl run code/smart_search --input '{"query":"session restore"}'
foxctl run code/context_grep --input '{"pattern":"Run\\(ctx","path":"internal"}'

# Memory
foxctl memory put --name "gotcha-x" --type "gotcha" --summary "..."
foxctl memory search "gotcha"

# Sessions / continuity
foxctl sessions list
foxctl context task-history-summary --task-id <id>

# Agents
foxctl agent spawn --role researcher --prompt "trace the room runtime"
foxctl agent ask <id> --question "what did you find?" --wait

# Rooms / durable coordination
foxctl room create my-room
foxctl room status my-room
foxctl room inbox my-room

# Web / MCP
foxctl web serve --dev-cors
foxctl mcp serve --skills
```

## Canonical sources

- [`README.md`](https://github.com/joshka0/foxctl/blob/main/README.md)
- [`docs/start/README.md`](https://github.com/joshka0/foxctl/blob/main/docs/start/README.md)
- [`AGENTS.md`](https://github.com/joshka0/foxctl/blob/main/AGENTS.md)
