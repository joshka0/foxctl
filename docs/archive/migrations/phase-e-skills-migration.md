# Phase E: Hook Skills Migration to Cross-Platform Utilities

> **Status**: Draft
> **Priority**: P1 (after Phase A/B complete)
> **Effort**: Small per skill

---

## Overview

This document tracks the migration of individual hook skills to use the shared
`pathutil` and `toolutil` packages for cross-platform compatibility.

**Goal**: All hook skills work correctly when invoked via:
- Claude Code (CC) shell hooks
- OpenCode (OC) plugin
- foxctl actor runtime (hooks/dispatch)

---

## Completed

| Skill | Status | Notes |
|-------|--------|-------|
| `hooks/task_guard` | ✅ Done | Uses `toolutil.IsWriteOperation()`, `pathutil.ExtractPath()` |
| `hooks/file_guard` | ✅ Done | Uses `toolutil.IsWriteOperation()`, `pathutil.ExtractPath()`, `pathutil.RelativePath()` |
| `hooks/impact_analysis` | ✅ Done | Uses `pathutil.ExtractPath()`, `pathutil.IsTestFile()` |

---

## Skills to Migrate

### High Priority (P1) - Write Detection

These skills check for write operations and need `toolutil.IsWriteOperation()`:

| Skill | File | Current Check | Migration |
|-------|------|---------------|-----------|
| `hooks/file_guard` | `skills/hooks_file_guard/main.go` | `hook.IsWriteOperation(in.ToolName)` | Use `toolutil.IsWriteOperation(toolName, toolCanonical, toolKind)` |
| `hooks/security_scanner` | (if exists) | Check for write tools | Use `toolutil.IsWriteOperation()` |

### High Priority (P1) - Path Extraction

These skills extract file paths and need `pathutil.ExtractPath()`:

| Skill | File | Current Check | Migration |
|-------|------|---------------|-----------|
| `hooks/file_guard` | `skills/hooks_file_guard/main.go` | Local `extractFilePath()` | Use `pathutil.ExtractPath()` |
| `hooks/impact_analysis` | `skills/hooks_impact_analysis/main.go` | Local path extraction | Use `pathutil.ExtractPath()`, `pathutil.RelativePath()` |
| `hooks/test_feedback` | `skills/hooks_test_feedback/main.go` | Workspace hash derivation | Use consistent workspace ID |

### Medium Priority (P2) - Other Utilities

| Skill | File | Migration Needed |
|-------|------|------------------|
| `hooks/knowledge_router` | `skills/hooks_knowledge_router/main.go` | Add prompt extraction if not present |
| `hooks/overseer_inbox` | `skills/hooks_overseer_inbox/main.go` | No changes needed (mailbox-focused) |
| `hooks/mail_router` | `skills/hooks_mail_router/main.go` | No changes needed (mailbox-focused) |
| `hooks/session_end` | `skills/hooks_session_end/main.go` | Add canonical event handling |

---

## Migration Pattern

### Before (CC-only)

```go
import "github.com/joshka0/foxctl/internal/domain/hook"

func run(ctx context.Context, in hook.Input) error {
    // Only works with CC tool names
    if !hook.IsWriteOperation(in.ToolName) {
        return emitApprove("non-write")
    }

    // Only checks file_path field
    filePath := extractFilePath(in.ToolInput)
}

func extractFilePath(toolInput json.RawMessage) string {
    var input struct {
        FilePath string `json:"file_path"`
    }
    json.Unmarshal(toolInput, &input)
    return input.FilePath
}
```

### After (Cross-Platform)

```go
import (
    "github.com/joshka0/foxctl/internal/domain/hook"
    "github.com/joshka0/foxctl/internal/runtime/hooks/pathutil"
    "github.com/joshka0/foxctl/internal/runtime/hooks/toolutil"
)

func run(ctx context.Context, in hook.Input) error {
    // Works with CC, OC, and foxctl runtime
    // TODO: When hooks.Input adds ToolCanonical/ToolKind, pass them here
    if !toolutil.IsWriteOperation(in.ToolName, "", "") {
        return emitApprove("non-write")
    }

    // Checks file_path, path, file, current_path
    filePath := pathutil.ExtractPath(in.ToolInput)

    // Make relative to workspace
    relPath := pathutil.RelativePath(filePath, in.WorkspaceRoot)
}
```

---

## hook.Input Enhancement (Future)

The `internal/domain/hook/types.go` Input struct should be enhanced to include:

```go
type Input struct {
    // Existing fields...
    ToolName string `json:"tool_name,omitempty"`

    // New fields for cross-platform support
    ToolCanonical string `json:"tool_canonical,omitempty"` // e.g., "edit.apply_patch"
    ToolKind      string `json:"tool_kind,omitempty"`      // read|write|exec|search|any
}
```

This allows adapters to pass the canonical name and kind, enabling skills to
work without re-parsing tool names.

---

## Testing Checklist

For each migrated skill:

- [ ] Builds successfully: `CGO_ENABLED=0 go build -o /tmp/test ./skills/<name>/`
- [ ] Existing tests pass: `go test ./skills/<name>/...`
- [ ] Manual test with CC tool names: `{"tool_name": "Edit", "tool_input": {"file_path": "main.go"}}`
- [ ] Manual test with path field: `{"tool_name": "Edit", "tool_input": {"path": "main.go"}}`
- [ ] Manual test with canonical: `{"tool_name": "edit.apply_patch", "tool_input": {"path": "main.go"}}`

---

## Rollout Plan

1. **Batch 1**: `hooks/file_guard` (high use, simple migration)
2. **Batch 2**: `hooks/impact_analysis` (complex, needs careful testing)
3. **Batch 3**: `hooks/test_feedback`, `hooks/knowledge_router`
4. **Batch 4**: Remaining skills

Each batch should be a separate commit with tests verified.

---

## Dependencies

- [x] Phase A: hooks.yaml cross-platform matchers
- [x] Phase B: pathutil and toolutil packages
- [ ] Update `internal/domain/hook/types.go` to add ToolCanonical/ToolKind (optional enhancement)
