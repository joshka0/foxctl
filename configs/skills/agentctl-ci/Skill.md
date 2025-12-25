---
name: agentctl CI
description: GitHub CI integration - check PR status, get check run summaries, and retrieve failure logs.
---

# GitHub CI Integration

Summarize GitHub CI check runs for pull requests.

## Check PR Status

```bash
agentctl run ci/github_checks --input '{
  "pr": "123"
}'
```

## Parameters

| Parameter     | Type    | Required | Default     | Description                           |
| ------------- | ------- | -------- | ----------- | ------------------------------------- |
| `pr`          | string  | Yes      | -           | PR number or branch name              |
| `owner`       | string  | No       | auto-detect | GitHub repository owner               |
| `repo`        | string  | No       | auto-detect | Repository name (or `owner/repo`)     |
| `mode`        | string  | No       | `summary`   | Detail level: `summary` or `detailed` |
| `errors_only` | boolean | No       | `false`     | Only show failing/errored checks      |

## Examples

### Summary View

Quick overview of all check statuses:

```bash
agentctl run ci/github_checks --input '{
  "pr": "456",
  "mode": "summary"
}'
```

### Detailed View

Full check information with logs:

```bash
agentctl run ci/github_checks --input '{
  "pr": "456",
  "mode": "detailed"
}'
```

### Failures Only

See only what's broken:

```bash
agentctl run ci/github_checks --input '{
  "pr": "456",
  "errors_only": true
}'
```

### Explicit Repository

Specify repo when not auto-detected:

```bash
agentctl run ci/github_checks --input '{
  "pr": "123",
  "owner": "myorg",
  "repo": "myrepo"
}'
```

Or shorthand:

```bash
agentctl run ci/github_checks --input '{
  "pr": "123",
  "repo": "myorg/myrepo"
}'
```

### Check by Branch

```bash
agentctl run ci/github_checks --input '{
  "pr": "feature/new-api"
}'
```

## Output

Returns check run information:

- **Summary mode**: Check name, status (success/failure/pending), conclusion
- **Detailed mode**: Above plus step logs, timing, annotations

## Use Cases

- **PR triage**: Quickly see what's failing
- **Debug CI**: Get detailed logs for failed checks
- **Automation**: Monitor PR readiness before merge
- **Status reports**: Aggregate CI health for multiple PRs

## Environment

Set `GITHUB_TOKEN` for private repositories or higher rate limits:

```bash
export GITHUB_TOKEN=ghp_xxxxx
```
