---
title: Refactor scouts
description: Use foxctl refactor workflows to find cleanup targets, reduce slop, and tighten repository boundaries.
---

Refactor scouts turn cleanup into an evidence workflow. The goal is not to
rewrite broad areas of the repo. The goal is to identify a narrow target, attach
why it matters, and make the cleanup reviewable.

## Cleanup loop

| Step | Command shape | What it answers |
|---|---|---|
| Status | `foxctl refactor status --workspace . --path ./internal --language go` | Is this scope indexed, fresh, and safe to scout? |
| Snapshot | `foxctl refactor snapshot --path ./internal --language go` | What exact file and symbol surface are we about to reason over? |
| Hot paths | `foxctl refactor hot --path ./internal --language go --since HEAD~20` | Which files have recent churn or repeated edit pressure? |
| Dependencies | `foxctl refactor deps --path ./internal --language go --query Run --direction in` | What depends on this package or symbol? |
| Scout | `foxctl refactor scout --path ./internal --language go --focus slop` | Which hotspots, boundary problems, or slop-shaped code should be cleaned first? |
| Advisor | `foxctl refactor advisor --path ./internal --language go --focus slop` | How should a short cleanup sequence be ranked? |

## What to clean

Use scouts to find cleanup that makes future agent work cheaper and safer:

| Target | Signal |
|---|---|
| Overgrown functions | High complexity, repeated guard blocks, or too many responsibilities |
| Repeated remapping | The same conversion, error wrapping, or envelope shaping copied across files |
| Boundary drift | Runtime, retrieval, storage, and adapter code crossing package-family lines |
| Dead or stale paths | Symbols, docs, or command routes that no longer have real call sites |
| Review-hostile files | Files that force broad diffs for narrow behavior changes |

## Review rule

Good cleanup stays small. Each refactor should have:

1. A scout finding or hotspot reason
2. A bounded path or package scope
3. A before/after behavior check
4. No unrelated formatting churn
5. Updated docs when the cleanup changes operator guidance

For the deeper source workflow, see `docs/general/refactor-scout.md`.
