# Knowledge Registry Implementation

**Date:** 2024-11-27\
**Feature:** Knowledge Registry (rule-based matching, no embeddings yet)

## Summary

Implemented the knowledge registry as specified in
`docs/spec/knowledge_registry.md`. This enables Claude to dynamically surface
relevant knowledge packs based on context.

## Changes

### New Files

- `internal/storage/knowledge/store.go` - SQLite-backed store for knowledge
  items, triggers, and documents
- `internal/storage/knowledge/sync.go` - Sync logic to index knowledge from
  filesystem
- `internal/storage/knowledge/store_test.go` - Unit tests for store and sync
- `cmd/agentctl/cmd/knowledge.go` - CLI commands: `sync`, `list`, `search`
- `skills/hooks_knowledge_router/main.go` - Hook skill implementation
- `skills/hooks_knowledge_router/skill.yaml` - Skill manifest
- `.claude/hooks/knowledge-router.sh` - Hook wrapper script

### Modified Files

- `.claude/settings.json` - Added knowledge-router hook to PreToolUse

## CLI Commands

```bash
# Index knowledge packs from filesystem into SQLite
agentctl knowledge sync

# List all knowledge items or filter by kind
agentctl knowledge list
agentctl knowledge list --kind pack

# Search by keyword or file path
agentctl knowledge search --query "react hooks"
agentctl knowledge search --path "src/components/Button.tsx"
```

## Storage Schema

Knowledge items are stored in `~/.agentctl/storage/knowledge.db`:

- **knowledge_items** - Packs, agents, commands with name, kind, description,
  source_path, priority
- **knowledge_triggers** - Keyword, path, intent, content triggers linked to
  items
- **knowledge_documents** - Markdown content with body digests

## Hook Behavior

The `hooks/knowledge_router` skill:

- Runs on PreToolUse events for write operations
- Matches triggers against tool input and file paths
- Returns advisory context hints (never blocks)
- Threshold-gated recommendations (default: 0.5)

## Future Work

- [ ] Embeddings for semantic search (`--embed` flag)
- [ ] UserPromptSubmit hook support when available
- [ ] Intent-based matching
- [ ] Content-based matching with embeddings
