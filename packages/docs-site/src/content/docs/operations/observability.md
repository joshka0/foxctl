---
title: Observability
description: Events, context updater output, persistence modes, and operational inspection.
---

Status: Current.

Observability docs should explain where foxctl emits operational evidence and
how long it is retained.

## Event surfaces

| Surface | Use for |
|---|---|
| NDJSON event streams | Local inspection and lightweight collectors |
| Jobs store | Skill/job state and execution metadata |
| CAS | Large artifacts and evidence bundles |
| Sessions | Interaction history and continuity |
| Context updater | Workspace context and bridge output |

## Operational checks

```bash
foxctl context --help
```

```bash
foxctl run code/semantic_search --input '{"format":"tree"}'
```

```bash
foxctl cas --help
```

## Production guidance

- Keep logs off stdout for skill execution.
- Persist large evidence in CAS and reference by digest.
- Treat `/tmp` eval artifacts and one-off local paths as snapshot evidence, not
  evergreen product guarantees.
- Re-check runtime-specific traces before diagnosing Jido or classic daemon
  behavior.

## Canonical sources

- [docs/general/context-and-observability.md](https://github.com/joshka0/foxctl/blob/main/docs/general/context-and-observability.md)
- [docs/general/events.md](https://github.com/joshka0/foxctl/blob/main/docs/general/events.md)
- [docs/general/persistence.md](https://github.com/joshka0/foxctl/blob/main/docs/general/persistence.md)
- [docs/general/foxcular-events.md](https://github.com/joshka0/foxctl/blob/main/docs/general/foxcular-events.md)

