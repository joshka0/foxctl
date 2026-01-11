---
name: agentctl Dev Workflow
description: Development workflow with agentctl - CI status, run tests, track tasks, recall sessions, chain skills. Use when asked about PR status, running tests, tracking work, or recalling past sessions.
---

# Development Workflow

Use this skill for CI/CD, testing, task tracking, session memory, and skill pipelines.

**Trigger phrases**: "PR status", "check CI", "run tests", "what did we do", "how did we solve", "remember this", "track this task", "chain these steps"

## CI Status & Comments

```bash
# Unified PR status
agentctl ci status --pr 123

# Review comments (CodeRabbit, Greptile)
agentctl ci comments --pr 123
agentctl ci comments --pr 123 --source greptile --all

# CI check results
agentctl ci results --pr 123 --failed
```

Set `GITHUB_TOKEN` for private repos.

## Test Runner

```bash
# Run tests
agentctl run test/run --input '{"path": "./...", "mode": "test"}'

# With coverage
agentctl run test/run --input '{"path": "./...", "mode": "coverage"}'

# Race detection
agentctl run test/run --input '{"path": "./...", "mode": "race"}'

# Specific pattern
agentctl run test/run --input '{"pattern": "TestAuth.*", "verbose": true}'
```

## Task Management

```bash
# Add task
agentctl todo add --title "Implement auth" --description "JWT flow"

# With dependency
agentctl todo add --title "Deploy" --depends-on 01ABC123

# List/complete
agentctl todo list
agentctl todo active
agentctl todo complete --id <id> --notes "Done"
```

## Session Recall

```bash
# Search past sessions
agentctl run session/recall --input '{"query": "auth JWT refresh", "limit": 5}'

# List recent
agentctl sessions list --limit 10

# Show specific
agentctl sessions show <session-id>
```

## Workflow Pipelines

```bash
# Run workflow
agentctl workflow run pre-impl-analysis --input '{"path": "."}'

# List available
agentctl workflow list
```

Built-in: `pre-impl-analysis`, `code-review`, `lsp-analysis`

Full docs: See individual skill docs in `~/.agentctl/share/configs/skills/`
