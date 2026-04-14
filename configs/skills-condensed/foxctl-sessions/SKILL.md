---
name: foxctl Sessions
description: Search and recall past Claude Code sessions using semantic search. Find how problems were solved before.
---

# Session Memory

Search past sessions via `foxctl run session/recall`.

## Usage

```bash
foxctl run session/recall --input '{"query": "auth JWT refresh", "limit": 5}'
foxctl sessions list --limit 10
foxctl sessions show <session-id>
foxctl sessions search "race condition"
```

## Recall Output

Returns sessions with: summary, decisions, gotchas, files modified.

## Integration

- PreCompact hook saves session state
- SessionStart hook restores context
- Stop hook captures final session

Requires `GEMINI_API_KEY` for embeddings.

Full docs: `~/.foxctl/share/configs/skills/foxctl-sessions/Skill.md`
