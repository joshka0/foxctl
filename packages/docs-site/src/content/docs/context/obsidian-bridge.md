---
title: Obsidian bridge
description: Rebuild, promote, reconcile, and index the Obsidian knowledge layer.
---

Status: Current command shell, detailed behavior remains canonical in architecture docs.

The Obsidian bridge turns repo and documentation context into a knowledge layer
that can be reconciled and queried by agent workflows.

## Command flow

Build the graph projection:

```bash
foxctl obsidian graph build --workspace . --vault-path "/path/to/vault"
```

Promote reviewed graph content:

```bash
foxctl obsidian graph promote --workspace . --vault-path "/path/to/vault"
```

Reconcile bridge metadata:

```bash
foxctl obsidian bridge reconcile --workspace . --vault-path "/path/to/vault"
```

Build the vault index:

```bash
foxctl obsidian index build --vault-path "/path/to/vault"
```

## When to run it

- After canonical docs change.
- After repo graph or semantic anchor changes.
- After bridge metadata changes.
- Before relying on the vault as current retrieval evidence.

## Canonical sources

- [docs/architecture/context-architecture.md](https://github.com/joshka0/foxctl/blob/main/docs/architecture/context-architecture.md)
- [docs/general/memory.md](https://github.com/joshka0/foxctl/blob/main/docs/general/memory.md)
- [AGENTS.md](https://github.com/joshka0/foxctl/blob/main/AGENTS.md)

