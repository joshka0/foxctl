---
name: agentctl CI
description: GitHub CI integration - check PR status, get check run summaries, and retrieve failure logs.
---

# GitHub CI Integration

Summarize GitHub CI check runs for pull requests.

## Usage

```bash
agentctl run ci/github_checks --input '{"pr": "123"}'
```

## Parameters

| Param | Type | Description |
|-------|------|-------------|
| `pr` | string | PR number or branch name (required) |
| `owner` | string | GitHub repo owner (auto-detect) |
| `repo` | string | Repository name (auto-detect) |
| `mode` | string | `summary` or `detailed` |
| `errors_only` | bool | Only show failing checks |

## Examples

```bash
# Failures only
agentctl run ci/github_checks --input '{"pr": "456", "errors_only": true}'

# Detailed with logs
agentctl run ci/github_checks --input '{"pr": "456", "mode": "detailed"}'

# Explicit repo
agentctl run ci/github_checks --input '{"pr": "123", "repo": "myorg/myrepo"}'
```

Set `GITHUB_TOKEN` for private repos.

Full docs: `~/repos/personal/agentctl/configs/skills/agentctl-ci/Skill.md`
