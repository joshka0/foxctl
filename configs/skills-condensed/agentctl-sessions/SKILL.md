---
name: agentctl Sessions
description: Search and recall past Claude Code sessions using semantic search. Find how problems were solved before.
---

# Session Memory

Search past sessions via `agentctl run session/recall`.

## Usage

```bash
agentctl run session/recall --input '{"query": "auth JWT refresh", "limit": 5}'
agentctl sessions list --limit 10
agentctl sessions show <session-id>
agentctl sessions search "race condition"
```

## Recall Output

Returns sessions with: summary, decisions, gotchas, files modified.

## Integration

- PreCompact hook saves session state
- SessionStart hook restores context
- Stop hook captures final session

Requires `GEMINI_API_KEY` for embeddings.

Full docs: `~/.agentctl/share/configs/skills/agentctl-sessions/Skill.md`
