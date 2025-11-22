# CI Skill: `ci/prcomments`

Summarize merge conflicts, CI failures, and PR review comments for a GitHub pull request as a task-focused report, plus a machine-readable JSON summary.

This is a thin wrapper around the `ci/prcomments` skill with an ergonomic CLI front-end:

```bash
agentctl ci prcomments --pr <number-or-branch> [flags]
```

## Requirements

- A GitHub token with `repo` scope:
  - Preferred: `gh auth login` then `gh auth token` (used automatically), or
  - `GITHUB_TOKEN` environment variable.
- Repository resolution uses this order:
  1. `--owner` / `--repo` flags (or `--repo owner/repo` shorthand), then
  2. `GITHUB_OWNER` / `GITHUB_REPO` env vars, then
  3. `git remote get-url origin` in the current workspace.

## Flags

- `--pr` (string, required)
  - PR number or branch name.
- `--owner` (string, optional)
  - Repository owner (e.g. `jkatigb`).
- `--repo` (string, optional)
  - Repository name (e.g. `agentctl`), or `owner/repo` shorthand.
- `--with-context` (bool, default: false)
  - Include PR description and timestamps in the markdown output.
- `--format` (string, optional)
  - `markdown` (default) or `json`.
  - `markdown`: emit a task-focused markdown report (with JSON envelope summary).
  - `json`: also include raw comment JSON in the `comments` field.
- `--output-path` (string, optional)
  - Workspace-relative path where the full markdown report is written, e.g.:
  - `docs/prcomments/pr66.md`.
  - The path is validated against the workspace via the skills path policy.
- `--errors-only` (bool, default: false)
  - When true, only include:
    - Merge conflicts,
    - Failing / error / cancelled CI checks, and
    - Actionable review comments (after bot/noise filtering).
  - Suppresses the "all clear" summary to keep the report focused on fixes.
- `--skip-cache` (bool, default: false)
  - When true, bypass the result cache and always execute the skill.
- `--data-only` (bool, default: false)
  - When true, post-processes the envelope and prints only `{"status", "data"}` to stdout.
  - Combine with `--no-comments` to drop the raw `comments` array from `data`.

## Examples

### Task-focused report for a PR in this repo

```bash
agentctl ci prcomments \
  --pr 66 \
  --owner jkatigb \
  --repo agentctl \
  --with-context \
  --errors-only \
  --output-path docs/prcomments/pr66.md
```

This will:

- Fetch PR #66 from `jkatigb/agentctl`.
- Resolve CI failures and merge conflicts.
- Collect issue + review comments, filtering out bot noise.
- Print a JSON envelope to stdout containing:
  - Task counts and status flags,
  - A truncated markdown preview,
  - A CAS artifact digest when the report is large.
- Write the full markdown report to `docs/prcomments/pr66.md`.

### JSON-centric output for tool integration

```bash
agentctl ci prcomments \
  --pr 66 \
  --owner jkatigb \
  --repo agentctl \
  --format json \
  --errors-only
```

Useful when another tool wants to reason over the comments and CI data:

- `data.tasks`: `{ total, merge_conflicts, ci_failures, review_comments }`.
- `data.status`: boolean feature flags for quick filtering.
- `data.comments`: flattened list of issue + review comments, sorted by time.
- `data.has_blocking_issues`: `true` when there is at least one merge conflict, CI failure, or review comment task.
- `data.tasks_list`: structured list of task items, each with:
  - `kind`: `merge_conflict` | `ci_failure` | `review_comment`.
  - `summary`: short natural-language summary.
  - Optional fields like `check_name`, `check_url`, `file`, `line`, `comment_author`, `comment_body`.

## Skill contract

Underlying skill: `ci/prcomments` (exec distribution).

- Input fields (JSON):
  - `pr`, `owner`, `repo`, `with_context`, `format`, `output_path`, `errors_only`.
- Output envelope `data` contains:
  - PR metadata (`repository`, `pr_number`, `title`, `author`, `url`).
  - `tasks` and `status` as described above.
  - `has_blocking_issues` and `tasks_list` for AI-friendly routing.
  - `markdown_preview` and `markdown_truncated`.
  - Optional `markdown_artifact` + size/ kind when CAS is used.
  - Optional `markdown_output_path` when a file is written.
  - Optional `comments` when `format == "json"`.

Large markdown reports are stored in CAS; the envelope exposes their digest so other tools can fetch the full artifact when needed.
