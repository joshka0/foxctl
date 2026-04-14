---
name: foxctl Dev Workflow
description: Development workflow with foxctl - CI status, run tests, track tasks, recall sessions, chain skills. Use when asked about PR status, running tests, tracking work, or recalling past sessions.
---

# Development Workflow

Use this skill for CI/CD, testing, task tracking, session memory, and skill pipelines.

**Trigger phrases**: "PR status", "check CI", "run tests", "what did we do", "how did we solve", "remember this", "track this task", "chain these steps"

## CI Status & Comments

```bash
# Unified PR status
foxctl ci status --pr 123

# Review comments (CodeRabbit, Greptile)
foxctl ci comments --pr 123
foxctl ci comments --pr 123 --source greptile --all

# CI check results
foxctl ci results --pr 123 --failed
```

Set `GITHUB_TOKEN` for private repos.

## Test Runner

```bash
# Run tests
foxctl run test/run --input '{"path": "./...", "mode": "test"}'

# With coverage
foxctl run test/run --input '{"path": "./...", "mode": "coverage"}'

# Race detection
foxctl run test/run --input '{"path": "./...", "mode": "race"}'

# Specific pattern
foxctl run test/run --input '{"pattern": "TestAuth.*", "verbose": true}'
```

## Task Management

```bash
# Add task
foxctl todo add --title "Implement auth" --description "JWT flow"

# With dependency
foxctl todo add --title "Deploy" --depends-on 01ABC123

# List/complete
foxctl todo list
foxctl todo active
foxctl todo complete --id <id> --notes "Done"
```

## Session Recall

```bash
# Search past sessions
foxctl run session/recall --input '{"query": "auth JWT refresh", "limit": 5}'

# List recent
foxctl sessions list --limit 10

# Show specific
foxctl sessions show <session-id>
```

## Workflow Pipelines

```bash
# Run workflow
foxctl workflow run pre-impl-analysis --input '{"path": "."}'

# List available
foxctl workflow list
```

Built-in: `pre-impl-analysis`, `code-review`, `lsp-analysis`

Full docs: See individual skill docs in `~/.foxctl/share/configs/skills/`
