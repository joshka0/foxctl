---
title: Protocol v1 envelopes
description: Canonical envelope shape for foxctl skills, jobs, plugins, agents, and artifacts.
---

Status: Current. Link to `docs/spec/v1/protocol_v1.md` as canonical; the
top-level `docs/spec/protocol_v1.md` file is only a moved stub.

Protocol v1 is the wire contract for foxctl I/O. Changing it without a spec
update breaks hooks, GUI clients, golden tests, and downstream tools.

## Envelope shape

```json
{
  "version": 1,
  "status": "ok",
  "command": "skill/name",
  "data": {},
  "meta": {
    "ts": "2026-05-12T00:00:00Z"
  },
  "error": {}
}
```

## Status rules

| Status | Meaning |
|---|---|
| `ok` | Terminal success; error fields are empty |
| `error` | Terminal failure; `error.code` and `error.message` are required |
| `progress` | Non-terminal progress update |

## Artifactization

Large output should move to CAS and return a small summary plus digest:

```json
{
  "data": {
    "summary": "Evidence bundle written to CAS",
    "artifact": "sha256:abc123..."
  }
}
```

## Review rules

- Do not change `meta.*` fields without a spec update.
- Do not write logs to stdout from skills.
- Preserve deterministic ordering in golden outputs.
- Prefer the stricter Protocol v1 inline-size guidance when docs disagree.

## Canonical sources

- [docs/spec/v1/protocol_v1.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/v1/protocol_v1.md)
- [docs/spec/protocol_v1.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/protocol_v1.md)
- [AGENTS.md](https://github.com/joshka0/foxctl/blob/main/AGENTS.md)

