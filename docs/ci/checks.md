# CI Skill: `ci/checks`

Summarize GitHub check runs for a PR, with a compact JSON view suitable for routing and triage.

CLI wrapper around the `ci/checks` skill:

```bash
foxctl ci checks --pr <number-or-branch> [flags]
```

## Requirements

Same as `ci/prcomments`:

- GitHub token from `GITHUB_TOKEN` or `gh auth token`.
- Repository resolution via flags, env, or `git remote`.

## Flags

- `--pr` (string, required)
  - PR number or branch name.
- `--owner` (string, optional)
  - Repository owner.
- `--repo` (string, optional)
  - Repository name or `owner/repo` shorthand.
- `--mode` (string, optional; default: `summary`)
  - `summary`: only high-level information per check.
  - `detailed`: also resolves job details for GitHub Actions runs and surfaces failed steps.
- `--errors-only` (bool, default: false)
  - When true, only include checks whose conclusion is one of:
    - `failure`, `timed_out`, `action_required`, `stale`, `cancelled`.
- `--skip-cache` (bool, default: false)
  - When true, bypass the result cache and always execute the skill.
- `--data-only` (bool, default: false)
  - When true, post-processes the envelope and prints only `{"status", "data"}` to stdout.

## Examples

### View failing checks only (detailed)

```bash
foxctl ci checks \
  --pr 66 \
  --owner joshka0 \
  --repo foxctl \
  --mode detailed \
  --errors-only
```

Envelope `data` will include:

- `repository`, `pr_number`, `head_sha`.
- `overall_status`: e.g. `failed`, `success`, `mixed`, `cancelled`.
- `totals`: `{ checks, failed, cancelled, neutral, success }`.
- `has_blocking_ci`: `true` when any check has a blocking conclusion (`failure`, `timed_out`, `action_required`, `stale`, or `cancelled`).
- `all_checks_successful`: `true` when all checks succeeded.
- `has_neutral_or_skipped`: `true` when there are neutral/skipped checks.
- `checks`: array of objects with:
  - `id`, `name`, `status`, `conclusion`, `html_url`.
  - `started_at`, `completed_at`, `duration_seconds` (when timestamps are present).
  - `failed_step` (when `--mode=detailed` and a failed step is found for Actions jobs).

### Summary of all checks

```bash
foxctl ci checks \
  --pr 66 \
  --owner joshka0 \
  --repo foxctl
```

- Includes both successful and failed checks.
- Still exposes `overall_status` and `totals` for quick gating.

## Skill contract

Underlying skill: `ci/checks` (exec distribution).

- Input fields:
  - `pr`, `owner`, `repo`, `mode`, `errors_only`.
- Output envelope `data`:
  - `repository`, `pr_number`, `head_sha`.
  - `overall_status` and `totals`.
  - `has_blocking_ci`, `all_checks_successful`, `has_neutral_or_skipped`.
  - `mode`, `errors_only`.
  - `checks`: normalized list of check runs for the PR head SHA (always an array, possibly empty).

This skill is intentionally JSON-only (no markdown); large payloads should be handled by downstream tools via CAS if needed.
