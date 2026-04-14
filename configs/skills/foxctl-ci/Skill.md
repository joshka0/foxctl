---
name: foxctl CI
description: GitHub CI integration - check PR status, get check run summaries, review comments (CodeRabbit, Greptile), and failure logs.
---

# GitHub CI Integration

Unified view of CI status, review comments, and check results for pull requests.
Works with open, closed, and merged PRs.

## Quick Commands

```bash
# Unified status - CI + comments + merge status
foxctl ci status --pr 123

# Get review comments (hides resolved by default)
foxctl ci comments --pr 123

# Include resolved/addressed comments
foxctl ci comments --pr 123 --all

# Get CI check results
foxctl ci results --pr 123

# Filter by source
foxctl ci comments --pr 123 --source greptile
foxctl ci comments --pr 123 --source coderabbit

# Show only failed checks
foxctl ci results --pr 123 --failed
```

## Commands

### `ci status` - Unified PR Status

Shows CI failures, review comments, and merge status in one view.

```bash
foxctl ci status --pr 123
foxctl ci status --pr 123 --owner myorg --repo myrepo
```

### `ci comments` - Review Comments

Get review comments with source filtering (supports CodeRabbit, Greptile, human reviewers).
By default, resolved/addressed comments are hidden.

```bash
# Unresolved comments only (default)
foxctl ci comments --pr 123

# Include resolved/addressed comments
foxctl ci comments --pr 123 --all

# Filter by source
foxctl ci comments --pr 123 --source greptile
foxctl ci comments --pr 123 --source coderabbit
foxctl ci comments --pr 123 --source human

# Combine filters
foxctl ci comments --pr 123 --source greptile --all

# JSON output
foxctl ci comments --pr 123 --data-only
```

Each comment includes `resolved` and `outdated` fields when available.

### `ci results` - CI Check Results

Get CI check run results with optional failure filtering.

```bash
# All check results
foxctl ci results --pr 123

# Only failed checks
foxctl ci results --pr 123 --failed

# JSON output
foxctl ci results --pr 123 --data-only
```

## Parameters

| Flag | Description |
|------|-------------|
| `--pr` | PR number or branch name (required) |
| `--owner` | GitHub repo owner (auto-detect from git remote) |
| `--repo` | Repository name (auto-detect from git remote) |
| `--source` | Filter comments: `coderabbit`, `greptile`, `human` |
| `--all` | Include resolved/addressed comments (default: hide resolved) |
| `--failed` | Show only failed checks (for `results`) |
| `--skip-cache` | Bypass cache for fresh data |
| `--data-only` | Output JSON only (for AI/scripting) |

## Supported Review Bots

| Bot | Username | Features |
|-----|----------|----------|
| CodeRabbit | `coderabbitai[bot]` | Severity levels, code suggestions |
| Greptile | `greptile-apps[bot]` | Syntax errors, refactoring suggestions |

## Environment

Set `GITHUB_TOKEN` or use `gh auth login` for private repos.

## Output Example

```markdown
## PR #123: Feature title

**Status:** ❌ Failing | **Merge:** ✅

### CI Failures (2)

1. **lint** [→ logs](url)
   gofmt found diffs
   → internal/auth/service.go:142

### Review Comments (3)

1. **@greptile-apps[bot]** [major] on api/handler.go:55
   **syntax:** missing nil check

2. **@coderabbitai[bot]** [critical] on auth/service.go:142
   Missing nil check before user access
```
