---
title: Storage, CAS, and persistence
description: Understand foxctl's local stores, CAS artifacts, backups, and rebuildable indexes.
---

Status: Current architecture shell.

foxctl uses several stores with different durability expectations. Production
docs should distinguish canonical state from rebuildable local projections.

## Store classes

| Store type | Examples | Production stance |
|---|---|---|
| Canonical metadata | sessions, memory, jobs, events | Preserve and migrate deliberately |
| CAS artifacts | large outputs, evidence bundles | Refer by digest, avoid inline blobs |
| Rebuildable indexes | repoindex, graph projections | Regenerate from canonical inputs |
| External stores | Turso, Postgres, remote services | Document driver and sync boundary |

## CAS

```bash
foxctl cas --help
```

Use CAS for large skill outputs and evidence artifacts. Envelopes should return
a small summary plus an artifact digest instead of embedding large payloads.

## Backup and database inspection

```bash
foxctl backup --help
```

```bash
foxctl db --help
```

## Production boundaries

- Do not treat local repoindex databases as source of truth.
- Preserve `repo_key` and workspace identity boundaries when moving artifacts.
- Keep generated or local-only database files out of git.
- Record whether a store is file, sqlite/libsql/Turso, Postgres, or remote.

## Canonical sources

- [docs/general/storage.md](https://github.com/joshka0/foxctl/blob/main/docs/general/storage.md)
- [docs/general/persistence.md](https://github.com/joshka0/foxctl/blob/main/docs/general/persistence.md)
- [docs/general/events.md](https://github.com/joshka0/foxctl/blob/main/docs/general/events.md)
- [docs/architecture/postgres-storage.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/postgres-storage.md)

