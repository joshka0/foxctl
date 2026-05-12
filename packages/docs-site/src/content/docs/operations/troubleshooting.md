---
title: Troubleshooting
description: Operational checks for common foxctl docs, runtime, and retrieval failures.
---

Status: Current shell.

Troubleshooting pages should start from symptoms and end with verification.
Avoid broad speculation when a concrete command can show the state.

## Common checks

```bash
git status --short
```

```bash
make check-doc-links
```

```bash
foxctl run code/semantic_search --input '{"query":"your issue","format":"tree","limit":25}'
```

```bash
foxctl agent info <agent-id>
```

## Package manager checks

The repo uses Bun workspaces. Docs-site dependency changes should keep exact
pins, avoid generator installs, and use lifecycle-script controls.

```bash
bun install --ignore-scripts
```

## Canonical sources

- [docs/general/gotchas.md](https://github.com/joshka0/foxctl/blob/main/docs/general/gotchas.md)
- [docs/DOC_LIFECYCLE.md](https://github.com/joshka0/foxctl/blob/main/docs/DOC_LIFECYCLE.md)
- [AGENTS.md](https://github.com/joshka0/foxctl/blob/main/AGENTS.md)
