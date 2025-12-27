---
name: agentctl CI
description: GitHub CI integration - check PR status, get check run summaries, and retrieve failure logs.
---

# GitHub CI Integration

Unified view of CI status, review comments, and merge conflicts for pull requests.

## Primary Command

```bash
# Unified status - CI checks + review comments + merge status
agentctl ci status --pr 123
```

This single command shows:
- CI check failures with error excerpts and file:line locations
- Review comments with file positions
- Merge conflict status

## Usage Examples

```bash
# Quick status check
agentctl ci status --pr 123

# Explicit repo (for cross-repo checks)
agentctl ci status --pr 123 --owner myorg --repo myrepo

# JSON output for scripting
agentctl ci status --pr 123 --data-only

# Skip cache for fresh data
agentctl ci status --pr 123 --skip-cache
```

## Output Format

```markdown
## PR #123: Feature title

**Status:** ❌ Failing | **Merge:** ✅

### CI Failures (2)

1. **lint** [→ logs](url)
   gofmt found diffs
   → internal/auth/service.go:142

2. **test** [→ logs](url)
   panic: nil pointer dereference
   → auth/service.go:55

### Review Comments (3)

1. **@reviewer** on api/handler.go:55
   Consider adding rate limiting

2. **@coderabbitai[bot]** [critical] on auth/service.go:142
   Missing nil check before user access
```

## Individual Skills (Advanced)

For granular control, use the underlying skills directly:

```bash
# CI checks only
agentctl run ci/github_checks --input '{"pr": "123", "mode": "detailed", "errors_only": true}'

# PR comments only
agentctl run ci/prcomments --input '{"pr": "123", "errors_only": true}'
```

## Parameters

| Flag | Description |
|------|-------------|
| `--pr` | PR number or branch name (required) |
| `--owner` | GitHub repo owner (auto-detect from git remote) |
| `--repo` | Repository name (auto-detect from git remote) |
| `--skip-cache` | Bypass cache for fresh data |
| `--data-only` | Output JSON only (for AI/scripting) |
| `--format` | Output format: `markdown` or `json` |

Set `GITHUB_TOKEN` or use `gh auth login` for private repos.
