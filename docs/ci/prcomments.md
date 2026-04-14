# CI Skill: `ci/prcomments`

Summarize merge conflicts, CI failures, and PR review comments for a GitHub pull request as a task-focused report, plus a machine-readable JSON summary.

This is a thin wrapper around the `ci/prcomments` skill with an ergonomic CLI front-end:

```bash
foxctl ci prcomments --pr <number-or-branch> [flags]
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
  - Repository owner (e.g. `joshka0`).
- `--repo` (string, optional)
  - Repository name (e.g. `foxctl`), or `owner/repo` shorthand.
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
foxctl ci prcomments \
  --pr 66 \
  --owner joshka0 \
  --repo foxctl \
  --with-context \
  --errors-only \
  --output-path docs/prcomments/pr66.md
```

This will:

- Fetch PR #66 from `joshka0/foxctl`.
- Resolve CI failures and merge conflicts.
- Collect issue + review comments, filtering out bot noise.
- Print a JSON envelope to stdout containing:
  - Task counts and status flags,
  - A truncated markdown preview,
  - A CAS artifact digest when the report is large.
- Write the full markdown report to `docs/prcomments/pr66.md`.

### JSON-centric output for tool integration

```bash
foxctl ci prcomments \
  --pr 66 \
  --owner joshka0 \
  --repo foxctl \
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
  - Optional fields like:
    - `source`: origin of the task, e.g. `github_review`, `coderabbit`.
    - `severity`: optional severity label for bot-originated tasks, e.g. `critical`, `major`, `minor`.
    - `check_name`, `check_url`, `file`, `line`, `comment_author`, `comment_body`.

### Importing PR tasks into `todo/manage`

To turn the current CI/review tasks for a PR into persistent TODOs, use:

```bash
foxctl ci todos \
  --pr 78 \
  --owner joshka0 \
  --repo foxctl \
  --store ~/.foxctl/todo/tasks.json
```

This helper:

- Runs the `ci/prcomments` skill with `format=json`, `errors_only=true`, and `with_context=false`, then reads `data.tasks_list` from the returned envelope and creates a `todo/manage` task for each CI/PR task, including:
  - Title derived from the `summary` and `kind` (e.g. `[review_comment] Fix undocumented conclusion handling`).
  - Description with `source`, `severity`, location (`file:line`), reviewer, and the cleaned comment body.

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
