# SPEC-007: Replace Long Parameter Lists with Option Structs

## Status
**Complete – November 14, 2025** | Priority: Medium | Complexity: Low

All targeted call sites have been converted to options structs on `main`:

- `cmd/agentctl/cmd/run.go:154-187` now exposes `RememberOptions` and forwards to the memory store using named fields.
- `internal/memory/store.go:249-281` ships `SaveOptions`/`SaveResult`; the legacy positional helper is just a shim.
- `internal/jobs/executor/executor.go:30-333` routes `RunSkill`/`ExecutePrepared` through a private `executeOptions` struct so the 90‑line executor logic no longer takes positional args.
- `internal/runner/runner.go:15-52` offers `RunWithOptions` with a deprecated thin wrapper around it.

The remaining checklist below is preserved for historical context; all required items are satisfied in the codebase as of the date noted above.

## Problem Statement

Multiple functions across the codebase have **5-7 parameters**, making them:
- Hard to call correctly
- Hard to extend with new options
- Error-prone (easy to swap parameters)
- Hard to read at call sites

### Examples

#### 7 Parameters (Worst Offender)
```go
// cmd/agentctl/cmd/run.go:267
func rememberResult(ctx context.Context, cfg config.Config,
    name, typ, summary, workspacePath string, result []byte) error
```

#### 6 Parameters
```go
// internal/memory/store.go:199
func (s *Store) SaveFromResult(ctx context.Context,
    name, typ, workspace, summary string, result []byte) (NamedEntry, error)
```

#### 5 Parameters (Multiple Locations)
```go
// internal/jobs/store.go:552
func (s *Store) executeSkill(ctx context.Context, jobID string,
    manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error)

// internal/runner/runner.go:14
func Run(ctx context.Context, manifest skill.Manifest,
    artifactPath string, input []byte, opts Options) ([]byte, []byte, error)

// cmd/agentctl/cmd/skill_helpers.go:114
func executeSkill(ctx context.Context, manifest skill.Manifest,
    artifactPath string, input []byte, opts ExecuteOptions) ([]byte, error)
```

## Current State Analysis

### Problem 1: Easy to Swap Parameters
```go
// Which is workspace and which is name?
SaveFromResult(ctx, "workspace1", "artifact", "myname", "summary", data)
// OR
SaveFromResult(ctx, "myname", "artifact", "workspace1", "summary", data)
// Both compile! But one is wrong!
```

### Problem 2: Hard to Extend
```go
// Want to add a new "tags" parameter?
// Must update ALL call sites
func SaveFromResult(ctx, name, typ, workspace, summary string,
    result []byte, tags []string) // ← New parameter breaks all callers
```

### Problem 3: Unclear Call Sites
```go
// What do these strings mean?
rememberResult(ctx, cfg, "mydata", "artifact", "User data", "/home/user/project", []byte("..."))
//                        ^^^^^^^^  ^^^^^^^^^^  ^^^^^^^^^^^  ^^^^^^^^^^^^^^^^^^^
//                        name?     type?       summary?     workspace?
```

## Proposed Solution

### Pattern: Options Struct

Replace long parameter lists with a single options struct that:
1. Names each field clearly
2. Allows optional parameters with zero values
3. Enables future extension without breaking changes
4. Makes call sites self-documenting

### Solution 1: rememberResult Function

#### Before
```go
// cmd/agentctl/cmd/run.go:267
func rememberResult(ctx context.Context, cfg config.Config,
    name, typ, summary, workspacePath string, result []byte) error {

    memStore, _ := memory.Open(ctx, cfg.Paths.Memory)
    defer memStore.Close()

    _, err := memStore.SaveFromResult(ctx, name, typ, workspacePath, summary, result)
    return err
}

// Call site
rememberResult(ctx, cfg, memoryName, memoryType, memorySummary, workspacePath, result)
```

#### After
```go
// cmd/agentctl/cmd/run.go
type RememberOptions struct {
    Name      string
    Type      string
    Summary   string
    Workspace string
    Result    []byte
}

func rememberResult(ctx context.Context, cfg config.Config, opts RememberOptions) error {
    memStore, _ := memory.Open(ctx, cfg.Paths.Memory)
    defer memStore.Close()

    _, err := memStore.SaveFromResult(ctx, memory.SaveOptions{
        Name:      opts.Name,
        Type:      opts.Type,
        Workspace: opts.Workspace,
        Summary:   opts.Summary,
        Result:    opts.Result,
    })
    return err
}

// Call site - self-documenting!
rememberResult(ctx, cfg, RememberOptions{
    Name:      memoryName,
    Type:      memoryType,
    Summary:   memorySummary,
    Workspace: workspacePath,
    Result:    result,
})
```

### Solution 2: memory.SaveFromResult

#### Before
```go
// internal/memory/store.go:199
func (s *Store) SaveFromResult(ctx context.Context,
    name, typ, workspace, summary string, result []byte) (NamedEntry, error) {

    // Extract digests
    digests := artifacts.ExtractDigests(result)

    entry := NamedEntry{
        Name:      name,
        Type:      typ,
        Workspace: workspace,
        Summary:   summary,
        Data:      result,
        Digests:   digests,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }

    return entry, s.save(ctx, entry)
}
```

#### After
```go
// internal/memory/store.go
type SaveOptions struct {
    Name      string
    Type      string
    Workspace string
    Summary   string
    Result    []byte
    Tags      []string  // ← Easy to add new fields!
}

func (s *Store) Save(ctx context.Context, opts SaveOptions) (NamedEntry, error) {
    // Extract digests
    digests := artifacts.ExtractDigests(opts.Result)

    entry := NamedEntry{
        Name:      opts.Name,
        Type:      opts.Type,
        Workspace: opts.Workspace,
        Summary:   opts.Summary,
        Data:      opts.Result,
        Digests:   digests,
        Tags:      opts.Tags,  // ← New field, no breaking change
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }

    return entry, s.save(ctx, entry)
}

// Call site
entry, err := store.Save(ctx, memory.SaveOptions{
    Name:      "mydata",
    Type:      "artifact",
    Workspace: workspacePath,
    Summary:   "User profile data",
    Result:    data,
})
```

### Solution 3: jobs.executeSkill

#### Before
```go
// internal/jobs/store.go:552
func (s *Store) executeSkill(ctx context.Context, jobID string,
    manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error) {

    // 92 lines of code...
}

// Call site
result, err := s.executeSkill(ctx, job.ID, manifest, handle.ArtifactPath, input)
```

#### After
```go
// internal/jobs/store.go
type executeOptions struct {
    JobID        string
    Manifest     skill.Manifest
    ArtifactPath string
    Input        []byte
}

func (s *Store) executeSkill(ctx context.Context, opts executeOptions) ([]byte, error) {
    // Same 92 lines, but clearer parameter access
    progressPath := filepath.Join(s.root, opts.JobID, "progress.ndjson")
    // ...
}

// Call site - much clearer!
result, err := s.executeSkill(ctx, executeOptions{
    JobID:        job.ID,
    Manifest:     manifest,
    ArtifactPath: handle.ArtifactPath,
    Input:        input,
})
```

### Solution 4: runner.Run (Already Partially Done!)

```go
// internal/runner/runner.go:14
// This one already uses Options for the last param
func Run(ctx context.Context, manifest skill.Manifest,
    artifactPath string, input []byte, opts Options) ([]byte, []byte, error)

// Could be improved further:
type RunOptions struct {
    Manifest     skill.Manifest
    ArtifactPath string
    Input        []byte
    Limits       ResourceLimits  // Nested struct
}

func Run(ctx context.Context, opts RunOptions) (*Result, error) {
    // ...
}
```

## Implementation Plan

### Step 1: Define Options Structs (2 hours)
- [ ] Create RememberOptions in cmd/run.go
- [ ] Create SaveOptions in internal/memory/
- [ ] Create executeOptions in internal/jobs/
- [ ] Create RunOptions in internal/runner/
- [ ] Add godoc to all option structs

### Step 2: Refactor memory Package (1.5 hours)
- [ ] Add SaveOptions struct
- [ ] Create new Save() method
- [ ] Deprecate SaveFromResult (keep for compatibility)
- [ ] Update tests
- [ ] Update callers

### Step 3: Refactor jobs Package (1.5 hours)
- [ ] Add executeOptions struct (private, internal)
- [ ] Update executeSkill signature
- [ ] Update all call sites
- [ ] Update tests

### Step 4: Refactor runner Package (2 hours)
- [ ] Create RunOptions struct
- [ ] Update Run() signature
- [ ] Create Result struct for return values
- [ ] Update all callers
- [ ] Update tests

### Step 5: Refactor cmd Package (2 hours)
- [ ] Add RememberOptions
- [ ] Update rememberResult()
- [ ] Update executeSkill() in skill_helpers.go
- [ ] Update all call sites
- [ ] Update tests

### Step 6: Documentation (0.5 hours)
- [ ] Add examples to godoc
- [ ] Document migration path
- [ ] Add to style guide

**Total Estimated Time**: 9.5 hours

## Testing Strategy

### Validation Tests
```go
// internal/memory/save_options_test.go
func TestSaveOptions_Validation(t *testing.T) {
    tests := []struct {
        name    string
        opts    SaveOptions
        wantErr string
    }{
        {
            name: "valid options",
            opts: SaveOptions{
                Name:      "test",
                Type:      "artifact",
                Workspace: "/tmp",
                Result:    []byte("data"),
            },
            wantErr: "",
        },
        {
            name: "missing name",
            opts: SaveOptions{
                Type:      "artifact",
                Workspace: "/tmp",
                Result:    []byte("data"),
            },
            wantErr: "name is required",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            err := tt.opts.Validate()
            if tt.wantErr != "" {
                assert.Contains(t, err.Error(), tt.wantErr)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

### Backward Compatibility Tests
```go
// Ensure deprecated methods still work
func TestSaveFromResult_StillWorks(t *testing.T) {
    store := setupTestStore(t)

    // Old API should still work during transition
    entry, err := store.SaveFromResult(ctx, "name", "type", "ws", "sum", data)

    require.NoError(t, err)
    assert.Equal(t, "name", entry.Name)
}
```

## Benefits

### Before: 7 Parameters
```go
rememberResult(ctx, cfg, memoryName, memoryType, memorySummary, workspacePath, result)
//             ^^^  ^^^  ^^^^^^^^^^  ^^^^^^^^^^  ^^^^^^^^^^^^^  ^^^^^^^^^^^^^  ^^^^^^
//             1    2    3           4           5              6              7
// What is what? Easy to swap parameters!
```

### After: 1 Struct
```go
rememberResult(ctx, cfg, RememberOptions{
    Name:      memoryName,      // ← Clear!
    Type:      memoryType,      // ← Clear!
    Summary:   memorySummary,   // ← Clear!
    Workspace: workspacePath,   // ← Clear!
    Result:    result,          // ← Clear!
})
```

### Improvements
- ✅ **Self-documenting code**
- ✅ **Impossible to swap parameters**
- ✅ **Easy to add new options without breaking changes**
- ✅ **Optional parameters with zero values**
- ✅ **Can add validation methods**
- ✅ **IDE autocomplete helps**

## Optional: Functional Options Pattern

For public APIs, consider the functional options pattern:

```go
// Advanced: Functional options for public APIs
type SaveOption func(*SaveOptions)

func WithTags(tags ...string) SaveOption {
    return func(opts *SaveOptions) {
        opts.Tags = tags
    }
}

func WithSummary(summary string) SaveOption {
    return func(opts *SaveOptions) {
        opts.Summary = summary
    }
}

// Usage
entry, err := store.Save(ctx, "mydata", "artifact", workspace, result,
    WithTags("v1", "prod"),
    WithSummary("Production data"),
)
```

**Note**: This is more complex. Use only for public APIs. Internal code should use plain structs.

## Migration Strategy

### Phase 1: Add New Methods (No Breaking Changes)
- Add new methods with options structs
- Keep old methods working
- Mark old methods as deprecated in comments

### Phase 2: Migrate Callers
- Update all call sites to use new methods
- Can be done incrementally

### Phase 3: Remove Deprecated Methods
- Once all callers migrated, remove old methods
- Or keep for one major version

### Example Migration
```go
// Phase 1: Add new method
func (s *Store) Save(ctx context.Context, opts SaveOptions) (NamedEntry, error) {
    // New implementation
}

// Deprecated: Use Save with SaveOptions instead
func (s *Store) SaveFromResult(ctx context.Context, name, typ, workspace, summary string,
    result []byte) (NamedEntry, error) {
    return s.Save(ctx, SaveOptions{
        Name:      name,
        Type:      typ,
        Workspace: workspace,
        Summary:   summary,
        Result:    result,
    })
}

// Phase 2: Migrate callers
- entry, err := store.SaveFromResult(ctx, name, typ, ws, sum, data)
+ entry, err := store.Save(ctx, memory.SaveOptions{...})

// Phase 3: Remove deprecated method
```

## Success Criteria

- [ ] All functions with 5+ parameters refactored
- [ ] Options structs defined with godoc
- [ ] All call sites updated
- [ ] Tests pass
- [ ] No breaking changes (deprecated methods still work)
- [ ] Documentation updated
- [ ] Style guide updated

## Related Specs
- SPEC-002: Refactor Run Command (RunOptions defined there)
- SPEC-004: SkillExecutor Interface (ExecuteOptions)

## References
- Effective Go: Pass structs, not many parameters
- Go Code Review Comments: Parameter lists
- Functional Options Pattern (Rob Pike)
- API Design Best Practices
