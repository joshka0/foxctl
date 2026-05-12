---
title: Verification
description: Required checks before foxctl changes and docs are treated as production-ready.
---

Run verification from the repository root unless a command states otherwise.

## Go runtime checks

```bash
# Format, lint, vet, and test
make check

# Run with race detection
make test-race

# Enforce coverage floor (40%)
make check-coverage

# Enforce strict coverage (85%)
make check-coverage-strict

# Short tests for quick feedback
make test-short
```

### Integration tests

```bash
# Full integration tests (may need API keys)
make test-integration

# CLI command integration tests (needs skills-build first)
make skills-build
make test-integration-cmd
```

## Docs site

```bash
# Build the Starlight site
bun run --cwd packages/docs-site build

# Run Astro checks
bun run --cwd packages/docs-site check
```

## Repository docs

```bash
# Check markdown links in repo docs
make check-doc-links

# Check for whitespace errors
git diff --check
```

## Supply-chain gate

For dependency changes, verify that no new advisories are introduced:

```bash
# Install with audit
bun install --ignore-scripts
bun audit
```

When adding new packages, verify they are clean before merging:

```bash
# Audit at moderate level
bun audit
```

A docs-site change should not leave any `workspace:@foxctl/docs-site` audit entry behind.

## Protocol conformance

```bash
# Validate envelope against schema and invariants
foxctl proto validate --input envelope.json

# Validate command output conformance
foxctl run fs/ls --path . | foxctl proto validate

# Strict validation mode
foxctl proto validate --input envelope.json --strict
```

## Storage and CAS integrity

```bash
# Verify CAS artifacts are accessible and valid
foxctl cas gc --older-than=0h --dry-run

# Create and verify backup
foxctl backup create --name pre-deploy
foxctl backup list
```

## Preflight checklist

Before finalizing any change, confirm:

1. **Mutating operations** are protected by feature flags, idempotency, or equivalent rollback-safe controls.
2. **Protocol/envelope invariants** were not broken by changes — `foxctl proto validate` passes.
3. **WASI/network and path policy constraints** still hold for modified skills — manifests still declare `network: "none"`.
4. **Doc links are valid** when markdown changed — `make check-doc-links` passes.
5. **Summaries and references** point to canonical docs under `docs/architecture/*` and `docs/general/*`.

## Golden test verification

Golden fixtures in `testdata/*.json` and `test/golden/` must remain reproducible:

- Sort keys and arrays in output before comparison
- Inject timestamps via clock interface (no `time.Now()` in core)
- Use stable IDs or inject UUID generator
- Run `make check` to verify no golden test drift
