---
title: Rooms and collaboration
description: Use durable rooms, messages, tasks, relays, and viewer backends.
---

Status: Current for core room commands, planned for room-agile extensions.

Rooms are durable collaboration timelines. They separate transport state,
viewer state, participant identity, messages, and tasks.

## Basic room flow

```bash
foxctl room create alpha --title "Agent Alpha"
```

```bash
foxctl room join alpha agent-a --role lead
```

```bash
foxctl room send alpha "Review the retry branch in client.ts"
```

```bash
foxctl room inbox alpha --actor agent-a
```

```bash
foxctl room status alpha
```

## Room tasks

```bash
foxctl room task add alpha --title "Refactor retry path"
```

```bash
foxctl room task claim alpha --id <task-id>
```

```bash
foxctl room task block alpha --id <task-id> --reason "waiting on benchmark data"
```

```bash
foxctl room task complete alpha --id <task-id> --notes "Retry ladder flattened"
```

## Viewer and relay backends

```bash
foxctl room relay alpha --backend tmux
```

```bash
foxctl room loop alpha --backend zellij --session alpha-room
```

## Tmux collaboration

```bash
foxctl tmux create --session foxctl-collab --panes 3 --attach
```

```bash
foxctl tmux send agent-b "Review internal/storage/mailbox/store.go for lease races." --sender agent-a
```

## Current vs planned

- Current: room messages, inbox, status, tasks, tmux/zellij relay surfaces.
- Planned: room-agile milestone enforcement, evidence-lane policy, and
  workpack templates from `docs/plans/features/room-*`.
- Room-agile commands exist as a skill protocol surface, but milestone evidence
  policy must be described as guidance unless exit-policy enforcement is
  explicitly enabled.

## Room-agile examples

```bash
foxctl room epic start <room-id> "Room agile protocol" --goal "..." --owner human-a
```

```bash
foxctl room milestone start <room-id> <epic-id> --proposal <proposal-id>
```

```bash
foxctl room story validate <room-id> <story-id> review pass "..."
```

## Canonical sources

- [docs/general/tmux-collaboration.md](https://github.com/joshka0/foxctl/blob/main/docs/general/tmux-collaboration.md)
- [docs/general/room-runtime-adoption-pass.md](https://github.com/joshka0/foxctl/blob/main/docs/general/room-runtime-adoption-pass.md)
- [docs/general/message-passing.md](https://github.com/joshka0/foxctl/blob/main/docs/general/message-passing.md)
- [docs/general/message-passing-quickstart.md](https://github.com/joshka0/foxctl/blob/main/docs/general/message-passing-quickstart.md)
