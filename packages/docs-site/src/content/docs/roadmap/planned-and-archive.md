---
title: Planned and archive docs
description: How the production docs site handles active plans, experiments, and historical material.
---

Status: Current policy shell.

The docs site should make roadmap status explicit. Planned work can be useful to
agents, but it must not read as current operator guidance.

## Planned

Use planned pages for:

- active implementation plans under `docs/plans/`
- roadmap items that need public navigation
- future migration work for this Starlight site
- Go-native runtime and optional Jido work that is still transitional.
- Semantic-anchor behavior that has not been promoted from evidence signal to
  current operator workflow.

## Experimental

Use experimental labels for features that can be tried but are not the default
production path.

Examples include RLM runtime experiments, graph projection spikes, and
refactor-intelligence workflows that still depend on plan-backed docs.

## Archive

Use archive pages for:

- superseded implementation plans
- generated codemaps
- historical design notes
- documents kept for provenance
- legacy TypeScript TUI/viewer material that should not appear in first-run
  guidance.

## Canonical sources

- [docs/plans/README.md](https://github.com/joshka0/foxctl/blob/main/docs/plans/README.md)
- [docs/archive/README.md](https://github.com/joshka0/foxctl/blob/main/docs/archive/README.md)
- [docs/codemaps/README.md](https://github.com/joshka0/foxctl/blob/main/docs/codemaps/README.md)
