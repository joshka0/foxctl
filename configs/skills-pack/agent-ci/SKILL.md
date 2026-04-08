---
name: agent-ci
description: "Run GitHub Actions CI locally with agent-ci before pushing. Pause on failure, fix, retry."
---

## What I do
- Run the full CI suite locally against your working tree using agent-ci (no commit or push needed).
- Pause on failure so you (or an AI agent) can fix the issue and retry just the failed step.

## When to use me
- Before opening a MR/PR — run CI locally first.
- After making changes and wanting fast confidence without waiting for remote CI.
- When a CI step fails and you want to debug interactively.

## Prerequisites

```bash
# 1. Install agent-ci (one-time)
bun add -d @redwoodjs/agent-ci

# 2. Pull the GitHub Actions runner image (one-time, ~1.5GB)
docker pull ghcr.io/actions/actions-runner:latest
```

## Running CI locally

```bash
# Run the local CI workflow
npx agent-ci run --quiet --workflow .github/workflows/ci-local.yml
```

This runs all jobs in parallel: static-analysis, unit-tests, race-tests, build, cgo-build-and-tests, integration-tests.

## If a step fails

```bash
# The runner pauses on failure. Fix the issue, then retry:
npx agent-ci retry --name <runner-name>

# Retry from a specific step (skips earlier ones)
npx agent-ci retry --name <runner-name> --from-step 3

# Abort and tear down
npx agent-ci abort --name <runner-name>
```

## How it works

- agent-ci emulates the GitHub Actions API locally using the real `actions-runner` binary.
- Your working tree is synced into the container — uncommitted changes are included.
- `actions/checkout`, `actions/setup-go`, and `actions/cache` work natively.
- The `ci-local.yml` workflow uses `runs-on: ubuntu-latest` with inline apt-get for build deps.

## Files

| File | Purpose |
|------|---------|
| `.github/workflows/ci-local.yml` | Local-only CI workflow for agent-ci |
| `.github/workflows/ci.yml` | Production CI (GitHub Actions) |
| `.gitlab-ci.yml` | Production CI (GitLab) — must stay in parity with `ci.yml` |

## Parity rule

The three CI configs (`ci.yml`, `ci-local.yml`, `.gitlab-ci.yml`) must stay in parity.
If you add a check to one, add it to the others. `ci-local.yml` may differ only in
container/image handling (it uses `runs-on` instead of `container:` for local compatibility).

## Operating rules
- Always run `npx agent-ci run --quiet --workflow .github/workflows/ci-local.yml` before opening an MR.
- If CI fails, fix the issue locally and retry before pushing.
- CI was green before you started. Any failure is from your changes — do not assume pre-existing failures.
- The `label-ai` and `release` jobs from `ci.yml` are intentionally omitted from `ci-local.yml` (they need GitHub API).
- Do NOT push to trigger remote CI when agent-ci can run it locally — it's instant and free.
