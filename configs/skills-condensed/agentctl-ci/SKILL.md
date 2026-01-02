---
name: agentctl CI
description: GitHub CI integration - check PR status, review comments (CodeRabbit, Greptile), and CI results.
---

# GitHub CI Integration

Works with open, closed, and merged PRs.

```bash
# Unified status
agentctl ci status --pr 123

# Review comments (hides resolved by default)
agentctl ci comments --pr 123
agentctl ci comments --pr 123 --all  # include resolved
agentctl ci comments --pr 123 --source greptile

# CI check results
agentctl ci results --pr 123
agentctl ci results --pr 123 --failed
```

## Flags

| Flag | Description |
|------|-------------|
| `--pr` | PR number (required) |
| `--source` | Filter: `coderabbit`, `greptile`, `human` |
| `--all` | Include resolved/addressed comments |
| `--failed` | Only failed checks |
| `--data-only` | JSON output |

## Supported Bots

- CodeRabbit (`coderabbitai[bot]`)
- Greptile (`greptile-apps[bot]`)

Set `GITHUB_TOKEN` for private repos.
