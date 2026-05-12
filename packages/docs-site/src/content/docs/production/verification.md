---
title: Verification
description: Required checks before the foxctl docs site is treated as production-ready.
---

Status: Current for this docs-site slice.

Run verification from the repository root unless a command states otherwise.

## Docs site

```bash
bun run --cwd packages/docs-site build
```

```bash
bun run --cwd packages/docs-site check
```

## Repository docs

```bash
make check-doc-links
```

```bash
git diff --check
```

## Supply-chain gate

For dependency changes:

```bash
sfw npm install --package-lock-only --ignore-scripts --save-exact --omit=optional <packages>
```

```bash
npm audit --package-lock-only --audit-level=moderate
```

Use Socket MCP when exposed by the agent environment. If it is not callable,
record the gap and use Socket CLI plus lockfile review before package edits.
For packages already in the Bun workspace lock, treat transitive security
overrides as dependency changes too.

The actual foxctl workspace install uses Bun:

```bash
bun install --ignore-scripts
```

Run the workspace audit after dependency changes:

```bash
bun audit
```

This first docs-site slice may still show pre-existing advisories from other
workspaces. A docs-site change should not leave any
`workspace:@foxctl/docs-site` audit entry behind.

## Canonical sources

- [docs/plans/features/official-docs-production-release.md](https://github.com/joshka0/foxctl/blob/main/docs/plans/features/official-docs-production-release.md)
- [docs/DOC_LIFECYCLE.md](https://github.com/joshka0/foxctl/blob/main/docs/DOC_LIFECYCLE.md)
