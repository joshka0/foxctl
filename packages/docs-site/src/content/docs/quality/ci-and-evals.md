---
title: CI and evals
description: Local checks, CI support commands, review gates, retrieval evals, and test feedback.
---

Status: Current shell, eval maturity varies by subsystem.

Quality documentation should tell contributors which checks protect which
behavior. It should also separate production CI expectations from experimental
evaluation harnesses.

## Local checks

```bash
make test
```

```bash
make lint
```

```bash
make check
```

```bash
make check-doc-links
```

```bash
git diff --check
```

## Docs-site checks

```bash
bun run --cwd packages/docs-site build
```

```bash
bun run --cwd packages/docs-site check
```

## CI and review support

```bash
foxctl ci --help
```

```bash
foxctl eval --help
```

## Production guidance

- Use current CI traces as source of truth for pipeline failures.
- Keep golden tests deterministic: stable ordering, injected clocks, stable IDs.
- Retrieval eval docs are current references, but new eval harness behavior
  should be labeled experimental until promoted.

## Canonical sources

- [docs/start/testing_and_ci.md](https://github.com/joshka0/foxctl/blob/main/docs/start/testing_and_ci.md)
- [docs/ci/README.md](https://github.com/joshka0/foxctl/blob/main/docs/ci/README.md)
- [docs/ci/checks.md](https://github.com/joshka0/foxctl/blob/main/docs/ci/checks.md)
- [docs/general/retrieval-evals.md](https://github.com/joshka0/foxctl/blob/main/docs/general/retrieval-evals.md)
- [docs/general/code-search-evals.md](https://github.com/joshka0/foxctl/blob/main/docs/general/code-search-evals.md)
- [docs/spec/review_gate.md](https://github.com/joshka0/foxctl/blob/main/docs/spec/review_gate.md)

