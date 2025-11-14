# SPEC-004: SkillExecutor Interface

## Status
**Draft** | Priority: High | Complexity: Medium

## Problem Statement

The `jobs.Store` package (persistence layer) is **tightly coupled** to the `runner` package (execution layer), violating the Single Responsibility Principle and making testing difficult.

### Current Dependencies
```
jobs.Store
    ├─> runner.Run()        (direct call to execution logic)
    ├─> skill.Load()        (manifest loading)
    ├─> skill.Manifest      (concrete type dependency)
    └─> policy.Validate()   (policy checking)
```

### Problems
1. **Tight Coupling**: Persistence layer knows about execution details
2. **Hard to Test**: Cannot mock skill execution in job tests
3. **Violation of SRP**: Store handles both persistence AND execution
4. **No Abstraction**: Cannot swap execution strategies
5. **Circular Concerns**: Jobs manages state transitions AND runs skills

### Affected Files
- `internal/jobs/store.go:552-644` (executeSkill method, 92 lines)
- `internal/jobs/store.go:211-219` (RunSkill method)
- `internal/jobs/store.go:17-18` (imports runner and skill)

## Current State Analysis

### executeSkill Method
```go
// internal/jobs/store.go:552-644
func (s *Store) executeSkill(ctx context.Context, jobID string,
    manifest skill.Manifest, artifactPath string, input []byte) ([]byte, error) {

    // Progress file handling (persistence concern)
    progressPath := filepath.Join(s.root, jobID, "progress.ndjson")
    progressFile, err := os.OpenFile(progressPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    defer progressFile.Close()

    // Write start event
    startEvent := ProgressEvent{Kind: "start", ...}
    json.NewEncoder(progressFile).Encode(startEvent)

    // Skill execution (execution concern - MIXED!)
    stdout, stderr, err := runner.Run(ctx, manifest, artifactPath, input)

    // Write result event
    resultEvent := ProgressEvent{Kind: "result", ...}
    json.NewEncoder(progressFile).Encode(resultEvent)

    // Validate and persist (persistence concern)
    resultPath := filepath.Join(s.root, jobID, "result.json")
    os.WriteFile(resultPath, result, 0644)

    return result, nil
}
```

**Issues**:
- Mixes 3 concerns: progress tracking, execution, persistence
- Cannot test execution independently
- Cannot inject test runner
- Jobs package imports execution details

### RunSkill Method
```go
// internal/jobs/store.go:211-219
func (s *Store) RunSkill(ctx context.Context, manifest skill.Manifest,
    artifactPath string, input []byte) (Job, []byte, error) {

    // Job state management
    job, err := s.Create(ctx, manifest.Name, input)

    // Direct execution call
    result, execErr := s.executeSkill(ctx, job.ID, manifest, artifactPath, input)

    // Update state
    s.UpdateState(ctx, job.ID, jobs.StateOK)

    return job, result, nil
}
```

## Proposed Solution

### Create SkillExecutor Interface

```go
// internal/execution/executor.go
package execution

import (
    "context"
    "io"
)

// SkillExecutor executes a skill and returns stdout, stderr, and error
type SkillExecutor interface {
    Execute(ctx context.Context, opts ExecuteOptions) (*Result, error)
}

// ExecuteOptions contains all parameters for skill execution
type ExecuteOptions struct {
    // Skill identification
    ManifestPath string
    ArtifactPath string

    // Input/Output
    Input  []byte
    Stdout io.Writer
    Stderr io.Writer

    // Resource limits
    MaxMemoryBytes uint64
    MaxCPUSeconds  uint64

    // Capabilities
    AllowNetwork    bool
    AllowFilesystem bool
}

// Result contains the execution result
type Result struct {
    Stdout   []byte
    Stderr   []byte
    ExitCode int
    Error    error
}

// ExecutorFunc is a function adapter for SkillExecutor
type ExecutorFunc func(ctx context.Context, opts ExecuteOptions) (*Result, error)

func (f ExecutorFunc) Execute(ctx context.Context, opts ExecuteOptions) (*Result, error) {
    return f(ctx, opts)
}
```

### Implement Adapter for Existing Runner

```go
// internal/execution/runner_adapter.go
package execution

import (
    "context"
    "github.com/jkatigb/agentctl/internal/runner"
    "github.com/jkatigb/agentctl/internal/skill"
)

// RunnerExecutor adapts the existing runner.Run to SkillExecutor interface
type RunnerExecutor struct{}

// NewRunnerExecutor creates a new executor using the default runner
func NewRunnerExecutor() SkillExecutor {
    return &RunnerExecutor{}
}

// Execute implements SkillExecutor using runner.Run
func (e *RunnerExecutor) Execute(ctx context.Context, opts ExecuteOptions) (*Result, error) {
    // Load manifest
    manifest, err := skill.Load(opts.ManifestPath)
    if err != nil {
        return nil, fmt.Errorf("load manifest: %w", err)
    }

    // Call existing runner
    stdout, stderr, err := runner.Run(ctx, manifest, opts.ArtifactPath, opts.Input)

    return &Result{
        Stdout:   stdout,
        Stderr:   stderr,
        ExitCode: determineExitCode(err),
        Error:    err,
    }, nil
}

func determineExitCode(err error) int {
    if err == nil {
        return 0
    }
    // Parse exit code from error if available
    return 1
}
```

### Refactor jobs.Store to Accept Executor

```go
// internal/jobs/store.go (REFACTORED)
package jobs

import (
    "github.com/jkatigb/agentctl/internal/execution"
)

type Store struct {
    db       *sql.DB
    root     string
    executor execution.SkillExecutor  // Injected dependency!
}

// Open creates a job store with default executor
func Open(ctx context.Context, path string) (*Store, error) {
    return OpenWithExecutor(ctx, path, execution.NewRunnerExecutor())
}

// OpenWithExecutor creates a job store with custom executor (for testing!)
func OpenWithExecutor(ctx context.Context, path string, executor execution.SkillExecutor) (*Store, error) {
    db, err := sql.Open("sqlite", filepath.Join(path, "jobs.db"))
    if err != nil {
        return nil, err
    }

    return &Store{
        db:       db,
        root:     path,
        executor: executor,
    }, nil
}

// executeSkill now uses injected executor
func (s *Store) executeSkill(ctx context.Context, jobID, manifestPath, artifactPath string,
    input []byte) ([]byte, error) {

    // Progress tracking setup
    progressPath := filepath.Join(s.root, jobID, "progress.ndjson")
    progressFile, err := os.OpenFile(progressPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
    if err != nil {
        return nil, fmt.Errorf("open progress file: %w", err)
    }
    defer progressFile.Close()

    // Write start event
    if err := writeProgressEvent(progressFile, ProgressEvent{Kind: "start", Timestamp: time.Now()}); err != nil {
        return nil, fmt.Errorf("write start event: %w", err)
    }

    // Execute using injected executor
    result, err := s.executor.Execute(ctx, execution.ExecuteOptions{
        ManifestPath: manifestPath,
        ArtifactPath: artifactPath,
        Input:        input,
    })

    // Write result event
    if err := writeProgressEvent(progressFile, ProgressEvent{
        Kind:      "result",
        Timestamp: time.Now(),
        Stdout:    string(result.Stdout),
        Stderr:    string(result.Stderr),
        Error:     result.Error,
    }); err != nil {
        return nil, fmt.Errorf("write result event: %w", err)
    }

    // Persist result
    resultPath := filepath.Join(s.root, jobID, "result.json")
    if err := os.WriteFile(resultPath, result.Stdout, 0o644); err != nil {
        return nil, fmt.Errorf("write result file: %w", err)
    }

    return result.Stdout, result.Error
}
```

### Mock Executor for Testing

```go
// internal/execution/mock_executor.go
package execution

// MockExecutor is a test double for SkillExecutor
type MockExecutor struct {
    ExecuteFunc func(ctx context.Context, opts ExecuteOptions) (*Result, error)
    Calls       []ExecuteOptions
}

func (m *MockExecutor) Execute(ctx context.Context, opts ExecuteOptions) (*Result, error) {
    m.Calls = append(m.Calls, opts)

    if m.ExecuteFunc != nil {
        return m.ExecuteFunc(ctx, opts)
    }

    // Default success
    return &Result{
        Stdout:   []byte(`{"ok": true}`),
        Stderr:   []byte{},
        ExitCode: 0,
    }, nil
}

// NewMockExecutor creates a mock executor
func NewMockExecutor() *MockExecutor {
    return &MockExecutor{
        Calls: make([]ExecuteOptions, 0),
    }
}
```

## Implementation Plan

### Step 1: Create Execution Package (2 hours)
- [ ] Create `internal/execution/` directory
- [ ] Create `executor.go` with interface and types
- [ ] Create `executor_test.go` with interface compliance tests
- [ ] Add comprehensive godoc

### Step 2: Create Runner Adapter (2 hours)
- [ ] Create `runner_adapter.go`
- [ ] Implement RunnerExecutor
- [ ] Add tests verifying it calls runner.Run correctly
- [ ] Test error handling and exit code determination

### Step 3: Create Mock Executor (1 hour)
- [ ] Create `mock_executor.go`
- [ ] Implement MockExecutor with call tracking
- [ ] Add tests for mock behavior
- [ ] Add examples of usage

### Step 4: Refactor jobs.Store (3 hours)
- [ ] Add executor field to Store struct
- [ ] Add OpenWithExecutor constructor
- [ ] Update executeSkill to use executor
- [ ] Update RunSkill method signature if needed
- [ ] Ensure backward compatibility

### Step 5: Update jobs Tests (2 hours)
- [ ] Convert existing tests to use MockExecutor
- [ ] Add tests for execution failures
- [ ] Add tests for timeout scenarios
- [ ] Verify all tests pass

### Step 6: Update Callers (1 hour)
- [ ] Update cmd/ files that call jobs.Open
- [ ] Verify no breaking changes
- [ ] Update integration tests

### Step 7: Documentation (0.5 hours)
- [ ] Add package-level documentation
- [ ] Add usage examples
- [ ] Update architecture docs

**Total Estimated Time**: 11.5 hours

## Testing Strategy

### Unit Tests for Interface
```go
// internal/execution/executor_test.go
func TestRunnerExecutor_Execute(t *testing.T) {
    executor := NewRunnerExecutor()

    result, err := executor.Execute(context.Background(), ExecuteOptions{
        ManifestPath: "testdata/echo/skill.yaml",
        Input:        []byte(`{"message": "hello"}`),
    })

    require.NoError(t, err)
    assert.Equal(t, 0, result.ExitCode)
    assert.Contains(t, string(result.Stdout), "hello")
}

func TestRunnerExecutor_InvalidManifest(t *testing.T) {
    executor := NewRunnerExecutor()

    result, err := executor.Execute(context.Background(), ExecuteOptions{
        ManifestPath: "nonexistent.yaml",
    })

    assert.Error(t, err)
    assert.Nil(t, result)
}
```

### Jobs Store Tests with Mock
```go
// internal/jobs/store_test.go (REFACTORED)
func TestStore_ExecuteSkill_Success(t *testing.T) {
    mockExec := execution.NewMockExecutor()
    mockExec.ExecuteFunc = func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
        return &execution.Result{
            Stdout:   []byte(`{"ok": true, "value": 42}`),
            ExitCode: 0,
        }, nil
    }

    store := setupTestStore(t, mockExec)
    defer store.Close()

    job, result, err := store.RunSkill(ctx, manifestPath, artifactPath, input)

    require.NoError(t, err)
    assert.Equal(t, jobs.StateOK, job.State)
    assert.JSONEq(t, `{"ok": true, "value": 42}`, string(result))

    // Verify mock was called
    assert.Len(t, mockExec.Calls, 1)
    assert.Equal(t, manifestPath, mockExec.Calls[0].ManifestPath)
}

func TestStore_ExecuteSkill_Failure(t *testing.T) {
    mockExec := execution.NewMockExecutor()
    mockExec.ExecuteFunc = func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
        return &execution.Result{
            Stderr:   []byte("execution failed"),
            ExitCode: 1,
            Error:    errors.New("execution failed"),
        }, errors.New("execution failed")
    }

    store := setupTestStore(t, mockExec)
    defer store.Close()

    job, result, err := store.RunSkill(ctx, manifestPath, artifactPath, input)

    assert.Error(t, err)
    assert.Equal(t, jobs.StateError, job.State)
}

func setupTestStore(t *testing.T, executor execution.SkillExecutor) *jobs.Store {
    dir := t.TempDir()
    store, err := jobs.OpenWithExecutor(context.Background(), dir, executor)
    require.NoError(t, err)
    return store
}
```

### Integration Tests
```go
// internal/jobs/integration_test.go
func TestJobExecution_EndToEnd(t *testing.T) {
    // Use real executor for integration test
    store, _ := jobs.Open(context.Background(), t.TempDir())
    defer store.Close()

    manifest := skill.Manifest{
        Name: "test_skill",
        // ... manifest details
    }

    job, result, err := store.RunSkill(ctx, manifest, "", []byte(`{"test": true}`))

    require.NoError(t, err)
    assert.Equal(t, jobs.StateOK, job.State)
    // Verify actual execution occurred
}
```

## Benefits

### Before
```go
// internal/jobs/store.go
import (
    "github.com/jkatigb/agentctl/internal/runner"  // Direct dependency
    "github.com/jkatigb/agentctl/internal/skill"   // Direct dependency
)

func (s *Store) executeSkill(...) {
    stdout, stderr, err := runner.Run(ctx, manifest, ...)  // Tightly coupled
}
```

**Problems**: Cannot mock, cannot test in isolation, mixed concerns

### After
```go
// internal/jobs/store.go
import (
    "github.com/jkatigb/agentctl/internal/execution"  // Interface only
)

func (s *Store) executeSkill(...) {
    result, err := s.executor.Execute(ctx, opts)  // Injected, testable
}
```

**Benefits**:
- ✅ Can inject mock for testing
- ✅ Jobs package doesn't import runner
- ✅ Clear separation of concerns
- ✅ Can add execution middleware (retry, timeout, logging)
- ✅ Can swap execution strategies

### Metrics
- **Coupling Reduced**: jobs package no longer imports runner or skill
- **Testability**: Can mock 100% of execution in tests
- **Flexibility**: Can implement different executors (remote, pooled, etc.)
- **LOC**: Slight increase (~100 lines) but much better architecture

## Migration Strategy

### Phase 1: Create New Components (No Breaking Changes)
- Create execution package with interface
- Create RunnerExecutor adapter
- All existing code continues to work

### Phase 2: Update jobs Package
- Add executor field to Store
- Add OpenWithExecutor for tests
- Keep existing Open() using default executor
- No breaking changes to callers

### Phase 3: Migrate Tests
- Convert job tests to use MockExecutor
- Keep integration tests with real executor
- Verify all tests pass

### Phase 4: Cleanup (Optional)
- Consider moving runner package under execution/
- Update documentation

## Risks and Mitigations

| Risk | Impact | Mitigation |
|------|--------|------------|
| Breaking existing code | High | Keep Open() with default executor |
| Performance overhead of interface | Low | Negligible in Go |
| More complex dependency injection | Medium | Clear examples and documentation |

## Success Criteria

- [ ] SkillExecutor interface defined
- [ ] RunnerExecutor adapter implemented
- [ ] MockExecutor created
- [ ] jobs.Store refactored to use executor
- [ ] All job tests use MockExecutor
- [ ] Integration tests pass with real executor
- [ ] No imports of runner in jobs package
- [ ] 90%+ test coverage

## Related Specs
- SPEC-001: Storage Interfaces (same pattern)
- SPEC-002: Refactor Run Command (will use this interface)

## References
- Dependency Injection in Go
- Interface Segregation Principle
- Repository Pattern (jobs.Store is repository, executor is service)
