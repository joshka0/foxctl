# Smart Todo Sync Design

## Problem

When a task is removed from Claude's TodoWrite list, agentctl doesn't know to remove it. Tasks accumulate indefinitely.

## Current Flow

```
Claude TodoWrite → todo-sync.sh → todo/sync_from_provider
                                        │
                                        ├── Match by task ID tag 〔T:xxx〕
                                        ├── Match by normalized title
                                        ├── Create new if no match
                                        └── Update status if changed
```

**Missing:** Detection of tasks that were in agentctl but NOT in incoming list.

## Solution: Removal Detection

After processing all incoming todos, check which existing session tasks weren't seen:

```go
// Track which tasks were in the incoming list
seenTaskIDs := make(map[string]bool)
for _, task := range processedTasks {
    seenTaskIDs[task.ID] = true
}

// Find tasks in session that weren't in the incoming list
for _, existingTask := range existingTasks {
    if seenTaskIDs[existingTask.ID] {
        continue  // Still in list
    }
    if existingTask.Status == StatusCompleted || existingTask.Status == StatusCanceled {
        continue  // Already done
    }

    // Task was removed from Claude's list - cancel it
    if !in.DryRun {
        existingTask.Status = StatusCanceled
        existingTask.Notes = appendNote(existingTask.Notes,
            "Removed from Claude TodoWrite list")
        s.taskStore.Update(ctx, existingTask)
    }
    result.Removed++
}
```

## Updated SyncResult

```go
type SyncResult struct {
    Created   int      `json:"created"`
    Updated   int      `json:"updated"`
    Completed int      `json:"completed"`
    Removed   int      `json:"removed"`    // NEW: Tasks canceled due to removal
    Mapped    int      `json:"mapped"`
    Unmapped  int      `json:"unmapped"`
    DepsAdded int      `json:"deps_added"`
    Warnings  []string `json:"warnings,omitempty"`
}
```

## Edge Cases

### 1. Empty List `[]`

If Claude sends empty list, should we cancel ALL session tasks?

**Options:**
- A) Yes - empty means "clear all" (aggressive)
- B) No - empty means "no changes" (conservative)
- C) Configurable via flag (flexible)

**Recommendation:** Option B (conservative) by default. Add `clear_on_empty: true` flag for Option A.

### 2. Partial List

Claude may only show top N tasks. Removal detection shouldn't cancel tasks beyond that window.

**Solution:** Only compare against tasks that were originally synced from Claude (track `source: "claude"` on tasks).

### 3. Task ID Stability

If a task has a `〔T:xxx〕` tag, it's been synced before. Use that as the stable identifier.

If no tag, use title hash. But titles can be edited, causing false removals.

**Solution:**
- Prefer task ID tags
- For title-matched tasks, mark them with tags during outbound sync
- Only remove tasks that have been confirmed via bidirectional sync

## Implementation Plan

### Phase 1: Basic Removal Detection
- Add `Removed` to SyncResult
- Detect and cancel tasks not in incoming list
- Skip if incoming list is empty (conservative)

### Phase 2: Source Tracking
- Add `Source` field to tasks (claude, agentctl, etc.)
- Only remove tasks where `Source == "claude"`
- Prevents removing manually created agentctl tasks

### Phase 3: Hash-Based Stability
- Store hash of last synced list per session
- Compare incoming hash vs stored hash
- Only process if hashes differ (optimization)

## File Changes

1. `internal/todosync/sync.go` - Add removal detection to `SyncFromProvider`
2. `internal/todosync/types.go` - Add `Removed` to `SyncResult`
3. `internal/storage/tasks/task.go` - Add `Source` field (Phase 2)
4. `configs/hooks/todo-sync.sh` - Update context message with removal count
