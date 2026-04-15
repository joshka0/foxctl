# Hook Performance Optimizations

## Problem Statement

Claude Code hooks are experiencing significant latency:

| Hook | Avg Latency | Max Latency | Primary Bottleneck |
|------|-------------|-------------|-------------------|
| `hooks/impact_analysis` | 8.8s | 85s | gopls cold start (30-40s) |
| `hooks/test_feedback` | 1.5s | 11.7s | foxctl CLI overhead |
| `hooks/task_guard` | 1.2s | 11.7s | Multiple SQLite opens |

## Root Cause Analysis

### Execution Flow (Current)

```
Shell Wrapper (bash)
└── foxctl run hooks/<skill>
    ├── config.Load()                      # ~50ms
    ├── skill resolution (findSkill)       # ~20ms
    ├── jobs.Open()                        # ~30ms SQLite
    ├── jobs.FindOrPrepareSkillJob()       # ~50ms transaction + I/O
    │   ├── ULID generation
    │   ├── mkdir job dir
    │   └── write input.json
    └── executor.ExecutePrepared()
        ├── persist.UpdateState() → running  # ~20ms
        ├── exec.Runner.Run()                # ~100ms process spawn
        │   ├── MkdirTemp for workDir
        │   ├── exec.CommandContext()
        │   └── cmd.Run()                    # <-- Skill executes
        ├── persist.UpdateState() → ok       # ~20ms
        └── write result.json                # ~10ms
```

**Total baseline overhead: ~500-700ms before skill logic runs**

### Per-Hook Issues

#### 1. hooks/impact_analysis (max 85s)
- Spawns `foxctl run code/symbols` subprocess
- For Go files: calls `gopls.GetDaemon()` which cold-starts in 30-40s
- Global singleton restarts when workspace changes
- Multiple parallel symbol lookups compound the problem

#### 2. hooks/test_feedback & hooks/task_guard (max 11.7s)
- Simple skills with trivial logic
- 90%+ of time is foxctl overhead
- task_guard opens 3 databases: runner CAS, tasks.db, graph.db
- SQLite contention when multiple hooks run in parallel

---

## Proposed Solutions

### Phase 1: Quick Wins (This PR)

#### 1.1 Conditional gopls for impact_analysis

**Problem**: gopls cold start takes 30-40s, causing 85s max delays.

**Solution**: Only run impact analysis if gopls daemon is already warm.

**Files to modify**:
- `internal/platform/lsp/gopls/daemon.go` - Add `IsDaemonReady()` function
- `skills/hooks_impact_analysis/main.go` - Check before running

**Implementation**:

```go
// internal/platform/lsp/gopls/daemon.go

// IsDaemonReady returns true if a gopls daemon is running for the workspace.
// This does NOT start the daemon - it only checks existing state.
func IsDaemonReady(workspace string) bool {
    daemonMu.Lock()
    defer daemonMu.Unlock()

    if globalDaemon == nil {
        return false
    }
    if globalDaemon.workspace != workspace {
        return false
    }
    return globalDaemon.isAlive()
}
```

```go
// skills/hooks_impact_analysis/main.go (in run function, before LSP calls)

// For Go files, skip if gopls isn't already warm (avoid cold start)
if lang == "go" && !gopls.IsDaemonReady(workspace) {
    return emitOutput(rc, hook.Output{
        Decision: "approve",
        Context:  "Impact analysis skipped: gopls not warm",
    })
}
```

**Impact**: Eliminates 30-85s delays for cold gopls. Users get impact analysis "for free" when gopls is already running from other LSP operations.

---

#### 1.2 Lazy graph.Open() in task_guard

**Problem**: task_guard always opens graph.db even when not needed.

**Current code** (`skills/hooks_task_guard/main.go:224`):
```go
func createModifiedEdge(ctx context.Context, cfg config.Config, ...) {
    graphStore, err := graph.Open(ctx, cfg.Storage.Root)  // Always opens
    // ...
}
```

**Solution**: Only open when scopePath is non-empty and we actually need to create edges.

**Implementation**: Already conditional at call site (line 119, 156), but the function itself always opens. Could add early return:

```go
func createModifiedEdge(ctx context.Context, cfg config.Config, workspaceID, taskID, filePath string) {
    if filePath == "" {
        return  // No file path, nothing to link
    }
    // ... rest of function
}
```

**Impact**: Saves ~30ms when no file path in hook input.

---

### Phase 2: Ephemeral Mode (Future PR)

#### 2.1 Add --ephemeral flag to foxctl run

**Problem**: Job persistence adds ~300-400ms overhead per hook invocation.

**Solution**: Skip job tracking for hooks since their results aren't queried later.

**Files to modify**:
- `cmd/foxctl/cmd/run.go` - Add `--ephemeral` flag
- `internal/runtime/runservice/executor.go` - Skip job store when ephemeral
- `internal/storage/jobs/executor/executor.go` - Direct execution path

**New execution path**:
```
foxctl run hooks/task_guard --ephemeral
└── config.Load()
└── skill resolution
└── exec.Runner.Run() directly  # Skip all job machinery
```

**Impact**: -60% latency (from ~700ms to ~300ms baseline)

---

### Phase 3: Daemon Architecture (Future)

#### 3.1 Persistent foxctl daemon

**Problem**: Each hook spawns a new process with full initialization.

**Solution**: Long-running daemon with Unix socket communication.

```
┌─────────────────────────────────────────────────────────┐
│                    foxctl daemon                       │
│  ┌──────────┐  ┌───────────┐  ┌─────────────────────┐  │
│  │  Config  │  │  SQLite   │  │   gopls daemon      │  │
│  │  (once)  │  │  Pool     │  │   (pre-warmed)      │  │
│  └──────────┘  └───────────┘  └─────────────────────┘  │
│                       │                                  │
│              Unix Socket /tmp/foxctl.sock             │
└─────────────────────────────────────────────────────────┘
```

**Impact**: -90% latency (from 500ms+ to <50ms)

---

## Implementation Plan (Phase 1)

### Step 1: Add IsDaemonReady() to gopls package

**File**: `internal/platform/lsp/gopls/daemon.go`

Add after line 107 (after GetDaemon function):

```go
// IsDaemonReady returns true if a gopls daemon is already running for the workspace.
// Unlike GetDaemon, this does NOT start the daemon if it's not running.
// Use this to avoid cold-start delays in performance-sensitive code paths.
func IsDaemonReady(workspace string) bool {
    daemonMu.Lock()
    defer daemonMu.Unlock()

    if globalDaemon == nil {
        return false
    }
    if globalDaemon.workspace != workspace {
        return false
    }
    return globalDaemon.isAlive()
}
```

### Step 2: Update impact_analysis to check daemon readiness

**File**: `skills/hooks_impact_analysis/main.go`

In the `run()` function, after language detection (~line 180), add:

```go
// For Go files, only proceed if gopls daemon is already warm.
// This avoids 30-40s cold start delays that cause hook timeouts.
// Users get impact analysis "for free" after using any LSP feature.
if lang == "go" {
    if !gopls.IsDaemonReady(workspace) {
        debugLog("gopls daemon not ready for workspace %s, skipping", workspace)
        return emitOutput(rc, hook.Output{
            Decision: "approve",
            Reason:   "impact analysis skipped (gopls not warm)",
        })
    }
}
```

### Step 3: Add import for gopls package

**File**: `skills/hooks_impact_analysis/main.go`

Add to imports:
```go
"github.com/joshka0/foxctl/internal/platform/lsp/gopls"
```

### Step 4: Update shell wrapper timeout (optional)

**File**: `.claude/settings.json`

The impact_analysis hook currently has a 10s timeout which is too short for cold starts but appropriate for warm runs. With this change, we can keep it or even reduce it since cold starts are avoided.

---

## Testing Plan

### Unit Tests

1. `internal/platform/lsp/gopls/daemon_test.go`:
   - Test `IsDaemonReady()` returns false when no daemon
   - Test `IsDaemonReady()` returns false for wrong workspace
   - Test `IsDaemonReady()` returns true for matching warm daemon
   - Test `IsDaemonReady()` returns false after daemon dies

2. `skills/hooks_impact_analysis/main_test.go`:
   - Test skip behavior when gopls not ready
   - Test normal behavior when gopls is ready

### Integration Tests

1. Fresh start (no gopls): Verify impact_analysis completes in <1s
2. After LSP operation: Verify impact_analysis provides full results
3. Workspace switch: Verify graceful handling

### Manual Testing

```bash
# Cold start - should skip quickly
FOXCTL_GOPLS_DEBUG=1 foxctl run hooks/impact_analysis --input '{"file_path":"main.go"}'

# Warm up gopls
foxctl run lsp/gopls --input '{"operation":"references","path":"main.go","line":10,"col":5}'

# Now should provide results
foxctl run hooks/impact_analysis --input '{"file_path":"main.go"}'
```

---

## Rollback Plan

If issues arise:

1. **Revert IsDaemonReady check**: Remove the conditional in impact_analysis
2. **Environment override**: Add `FOXCTL_IMPACT_FORCE=1` to bypass the check

---

## Success Metrics

| Metric | Before | After (Expected) |
|--------|--------|------------------|
| impact_analysis avg latency | 8.8s | <1s (cold), ~2s (warm) |
| impact_analysis max latency | 85s | <10s |
| Hook timeout errors | Frequent | Rare |

---

## Future Considerations

1. **Pre-warm gopls on session start**: Could add SessionStart hook to warm gopls
2. **LSP daemon pool**: Support multiple workspaces without restart penalty
3. **Ephemeral mode**: Skip job persistence for all hooks
4. **Full daemon architecture**: Persistent foxctl with connection pooling
