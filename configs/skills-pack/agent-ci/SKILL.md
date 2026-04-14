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

# Optional: collapse matrix jobs during debugging
npx agent-ci run --quiet --workflow .github/workflows/ci-local.yml --no-matrix

# Upstream-supported, but broader: run all relevant workflows for the branch
npx agent-ci run --quiet --all
```

For this repo, prefer the explicit local workflow over `--all`. It is the narrower and safer default, and it carries repo-local container limits for OrbStack/Docker.

`--all` is valid upstream, but use it only as a final confirmation pass when you specifically want every relevant workflow selection rule to apply.

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
- The `ci-local.yml` workflow in this repo uses the local `foxctl-ci:go1.26.1` container image.
- Local CI container resource caps belong in the workflow `container.options`, not in ad hoc shell wrappers.
- On macOS, OrbStack is the preferred Docker provider, but broad CI should still be treated as host-load-sensitive.

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
- Prefer `--no-matrix` during failure triage unless full matrix coverage is the thing you are validating.
- If CI fails, fix the issue locally and retry before pushing.
- CI was green before you started. Any failure is from your changes — do not assume pre-existing failures.
- The `label-ai` and `release` jobs from `ci.yml` are intentionally omitted from `ci-local.yml` (they need GitHub API).
- Do NOT push to trigger remote CI when agent-ci can run it locally — it's instant and free.
- On a busy host, do not treat `agent-ci` as the first hammer. Start with the narrow failing lane, then use agent-ci as the final workflow-shaped confirmation.
