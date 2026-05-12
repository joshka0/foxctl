---
title: CLI reference
description: Production entrypoints for foxctl command families and script wiring.
---

Status: Current shell, command-family coverage still needs a full pass.

The CLI reference should be organized by job, not by binary implementation.

## Command families

| Family | Use for | Current source |
|---|---|---|
| `foxctl run` | Skill execution, job tracking, envelopes, CAS artifacts | `docs/general/`, `skills/` |
| `foxctl skills` | Direct skill invocation and parameter validation | `skills/`, manifests |
| `foxctl index repo` | Repo graph build, search, expand | `docs/general/repoindex.md` |
| `foxctl agent` | Spawn, ask, watch, resume, hierarchy | `docs/general/agent-daemon.md` |
| `foxctl obsidian` | ACA vault graph and bridge refresh | `docs/architecture/context-architecture.md` |
| `foxctl shell` | Structured read-only command-shaped retrieval | `AGENTS.md` |
| `foxctl cas` | CAS artifact storage and lookup | `docs/general/storage.md` |
| `foxctl openapi` | OpenAPI-backed provider integration | `docs/start/openapi_and_plugins.md` |
| `foxctl hooks` | Hook runtime checks | `docs/general/hooks.md` |
| `foxctl ci` | CI support workflows | `docs/ci/` |

For a task-oriented command map, use [Command map](/reference/command-map/).

## Input modes

```bash
foxctl run <skill> --input '{"key":"value"}'
```

```bash
foxctl run <skill> --input-file input.json
```

```bash
foxctl run <skill> --input stdin
```

```bash
foxctl run <skill> --input sha256:<hex>
```

## Canonical sources

- [README.md](https://github.com/joshka0/foxctl/blob/main/README.md)
- [AGENTS.md](https://github.com/joshka0/foxctl/blob/main/AGENTS.md)
- [docs/general/repoindex.md](https://github.com/joshka0/foxctl/blob/main/docs/general/repoindex.md)
