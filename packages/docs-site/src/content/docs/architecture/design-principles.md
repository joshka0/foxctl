---
title: Design principles
description: The engineering rules behind foxctl architecture, storage, skills, retrieval, and agent surfaces.
---

Status: Current guidance.

foxctl is easiest to extend when each feature keeps a clear source of truth,
uses typed boundaries, and exposes one small command path before adding agent or
UI surfaces.

## Core rules

| Rule | Practical effect |
|---|---|
| Functional core, imperative shell | Keep planning, parsing, ranking, and formatting testable |
| Protocol at the boundary | CLI, skills, hooks, and agents exchange stable envelopes |
| CAS for large outputs | Big blobs stay out of inline JSON and logs |
| Derived indexes are rebuildable | Repoindex, semantic indexes, vault indexes, and projections can be recreated |
| Typed evidence over keyword routing | Retrieval and policy use structured signals, not brittle substring rules |
| Feature flags for risky paths | Experimental dependencies and runtime modes stay opt-in |
| Docs state the status | Current, experimental, planned, and archive content are not mixed |

## Source of truth pattern

Separate canonical data from projections:

```text
source files, docs, task state, memory, or database records
  -> derived index or projection
  -> query surface
  -> agent or user-facing explanation
```

Examples:

| Canonical source | Projection |
|---|---|
| Workspace source files | Repoindex graph |
| Memory and session stores | Semantic memory search |
| Vault markdown | Obsidian index |
| ACA runtime files | Top-of-mind and proposal projections |
| Tool output | CAS artifact plus envelope summary |

Do not dual-write first. Prove that a projection can be deleted and rebuilt
before making it part of the default path.

## Runtime boundaries

| Boundary | Rule |
|---|---|
| CLI | Parses user intent, calls services, prints envelopes or human tables |
| Skill | Narrow JSON command with manifest-defined capabilities |
| Storage | Owns persistence, migrations, CAS references, and local DB behavior |
| Context | Owns continuity, memory, evidence, transcript, and retrieval coordination |
| Intelligence | Owns repo graph, semantic search, codemaps, and code retrieval |
| Agents | Consume tools and context; they should not invent storage contracts |
| Interfaces | Web, gateway, chat, OpenAPI, and plugin surfaces adapt existing services |

When in doubt, add a command proof first and delay the generalized interface.

## Dependency posture

Large dependency additions need explicit vetting before install. For production
docs-site work, use Socket/SFW review before introducing or updating frontend
packages, especially after supply-chain incidents affecting broad npm
dependency trees.

Prefer:

- exact dependency versions
- ignored install scripts unless a package is explicitly trusted
- overrides for vulnerable transitive packages
- a docs-site-specific audit gate when the broader workspace already has known
  unrelated findings

## Retrieval design

Use the right retrieval plane:

| Plane | Best for |
|---|---|
| Semantic search | "Find code about this concept" |
| Context grep | "Show the function or file around this exact hit" |
| Repoindex | "Trace relationships between files, packages, symbols, and concepts" |
| DAG grep | "Explain a bounded graph neighborhood" |
| ACA | "What does this workspace already know about the task?" |
| Context engine | "Fuse typed evidence and record retrieval feedback" |

Graph traversal is not a replacement for embedding search. Embedding search is
not a replacement for graph explanations.

## Production release standard

A foxctl feature is production-ready when:

- the command path is documented and verified
- failure and fallback behavior are explicit
- generated artifacts are ignored or intentionally pinned
- tests cover the contract
- docs link to canonical sources
- any new dependency risk has been reviewed

## Canonical sources

- [AGENTS.md](https://github.com/joshka0/foxctl/blob/main/AGENTS.md)
- [docs/architecture/package-topology.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/package-topology.md)
- [docs/spec/v1/protocol_v1.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/v1/protocol_v1.md)
- [docs/DOC_LIFECYCLE.md](https://github.com/joshka0/foxctl/blob/main/docs/DOC_LIFECYCLE.md)

