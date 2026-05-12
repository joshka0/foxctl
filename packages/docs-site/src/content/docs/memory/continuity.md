---
title: Memory and continuity
description: Use sessions, memory, and task-history summaries without confusing evidence with instructions.
---

Status: Current.

foxctl stores session and memory artifacts so agents can resume work with
evidence instead of guessing from stale chat context.

## Session recall

```bash
foxctl run session/restore --input '{"session_id":"<id>","trigger":"session_start"}'
```

```bash
foxctl run session/recall --input '{"query":"oauth callback failure","limit":10}'
```

## Task continuity

Use task-history summaries for agents, scripts, and resumable workflows:

```bash
foxctl context task-history-summary --task-id <id>
```

Hook injection uses the prompt-ready wrapper path documented in the canonical
task continuity guide.

## Memory query

```bash
foxctl run memory/query --input '{"query":"repoindex graph gotchas","limit":10}'
```

## Safety model

- Treat recalled memory as evidence unless it is explicitly promoted by policy.
- Keep provenance and timestamps visible.
- Re-check drift-prone facts before acting on them.
- Do not route behavior using substring heuristics.

## Canonical sources

- [docs/general/memory.md](https://github.com/joshka0/foxctl/blob/main/docs/general/memory.md)
- [docs/general/sessions.md](https://github.com/joshka0/foxctl/blob/main/docs/general/sessions.md)
- [docs/general/task-continuity.md](https://github.com/joshka0/foxctl/blob/main/docs/general/task-continuity.md)
- [docs/architecture/context-architecture.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/context-architecture.md)

