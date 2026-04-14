# Todo System Ergonomics for AI Agents

> Design doc for improving todo system friction for Claude Code integration

## Status: Draft

## Problem Statement

The current todo system creates friction for AI agents (Claude) in several ways:

1. **Dual System Overhead**: Claude Code has built-in `TodoWrite` tool, foxctl has
   persistent `todo` commands. Maintaining both creates cognitive load and sync issues.

2. **Strict Dependency Enforcement**: Tasks with `depends_on` block completion if
   dependencies aren't marked done first. Real work often happens out of order.

3. **ULID-Based IDs**: Task IDs like `01JFXYZ...` aren't memorable. Completing tasks
   requires copy-pasting or looking up IDs.

4. **Context Loss on Compaction**: TodoWrite state is lost when conversation compacts.
   Agentctl todos persist but may drift from actual progress.

## Proposed Improvements

### 1. Single Source of Truth

**Option A: Hook-Based Sync**
```
TodoWrite call → PostToolUse hook → foxctl todo sync
```
- Intercept TodoWrite operations
- Mirror to foxctl storage
- Bidirectional: session-restore populates TodoWrite from foxctl

**Option B: Replace TodoWrite**
- Custom MCP tool that wraps foxctl todo
- Same interface, persistent backend
- Eliminates dual-system entirely

### 2. Soft Dependencies

```bash
# Current: Error if dependency incomplete
foxctl todo complete --id <id>
# Error: task depends on incomplete task X

# Proposed: Allow with warning
foxctl todo complete --id <id>
# Warning: completing task with incomplete dependency: X
# Completed: <task title>

# Or explicit force flag
foxctl todo complete --id <id> --force
```

**Implementation**: Change `depends_on` from hard constraint to advisory.
Store completion timestamp even if deps incomplete. Surface warnings in `todo list`.

### 3. Fuzzy Matching & Shortcuts

```bash
# By title substring (case-insensitive)
foxctl todo complete "turn persistence"
# Matched: "Implement Turn persistence in sessions.db" - Completed

# By position in list (1-indexed)
foxctl todo done 3
# Completed: <3rd task in list>

# By partial ID
foxctl todo complete 01JFX
# Matched: 01JFXYZ... - Completed

# Interactive picker (if multiple matches)
foxctl todo complete "memory"
# Multiple matches:
#   1. Implement ShortTermMemory
#   2. Add memory hooks
# Select [1-2]:
```

### 4. Batch Operations

```bash
# Complete task and all its incomplete dependencies
foxctl todo complete-chain <id>
# Completing dependency: X
# Completing dependency: Y
# Completing: Z

# Complete all tasks matching pattern
foxctl todo complete-all "Phase 1"

# Mark multiple by position
foxctl todo done 1,3,5
```

### 5. Auto-Detection Hooks

**Test Success → Todo Update**
```yaml
# Hook: PostToolUse on Bash
# Pattern: test command with exit 0
# Action: Check if any todos mention tested file/package, prompt completion
```

**File Write → Progress Detection**
```yaml
# Hook: PostToolUse on Write/Edit
# Pattern: File mentioned in todo description
# Action: Update todo progress, suggest completion if criteria met
```

### 6. Natural Language Interface

```bash
# Instead of structured commands
foxctl todo "mark the turn persistence task as done"
foxctl todo "what's left for phase 1?"
foxctl todo "add: write integration tests for actor system"
```

Uses LLM to parse intent → maps to structured operations.

### 7. Context-Aware Display

```bash
# Show todos relevant to current file/directory
foxctl todo list --context .
# Filters to todos mentioning files in current directory

# Show todos relevant to git diff
foxctl todo list --changed
# Shows todos related to uncommitted changes
```

## Additional Ideas

### Progress Tracking
- Attach "progress indicators" to todos (0-100% or checklist)
- Auto-update based on file changes or test results

### Time Boxing (Optional)
- Soft time estimates that don't enforce but help prioritize
- "Stale" detection for todos not touched in N hours

### Conversation Threading
- Link todos to conversation turns where they were created
- "Why was this created?" → shows original context

### Todo Templates
```bash
foxctl todo add --template feature
# Creates: Design, Implement, Test, Document sub-tasks
```

### Kanban-Style Status
```bash
foxctl todo move <id> --to in-review
# Statuses: backlog → todo → in-progress → in-review → done
```

## Implementation Priority

1. **High**: Soft dependencies (removes immediate friction)
2. **High**: Fuzzy matching by title (most common operation)
3. **Medium**: TodoWrite sync hook (single source of truth)
4. **Medium**: Batch complete-chain
5. **Low**: Natural language interface
6. **Low**: Auto-detection hooks

## Open Questions

1. Should foxctl todos replace TodoWrite entirely, or coexist?
2. How to handle multi-agent scenarios where different agents have different views?
3. Should todos be workspace-scoped or global?

---

*Created from Claude's feedback on todo system friction - Dec 2024*
