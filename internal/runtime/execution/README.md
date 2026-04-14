# Execution Package

The `execution` package provides abstractions for skill execution, implementing **SPEC-004: SkillExecutor Interface**.

## Purpose

This package decouples the persistence layer (`internal/runtime/jobs`) from execution details (`internal/runner`), enabling:

- **Dependency Injection**: Swap execution strategies easily
- **Testability**: Mock skill execution in tests
- **Flexibility**: Support different execution backends
- **Clean Architecture**: Clear separation of concerns

## Core Types

### SkillExecutor Interface

```go
type SkillExecutor interface {
    Execute(ctx context.Context, opts ExecuteOptions) (*Result, error)
}
```

The main abstraction for skill execution. Implementations load manifests, execute skills, and return results.

### ExecuteOptions

```go
type ExecuteOptions struct {
    ManifestPath string   // Path to skill.yaml
    ArtifactPath string   // Path to skill binary/module
    Input        []byte   // JSON input data

    // Optional output streams
    Stdout io.Writer
    Stderr io.Writer

    // Future: Resource limits and capabilities
    MaxMemoryBytes uint64
    MaxCPUSeconds  uint64
    AllowNetwork    bool
    AllowFilesystem bool
}
```

### Result

```go
type Result struct {
    Stdout   []byte
    Stderr   []byte
    ExitCode int
    Error    error
}
```

## Implementations

### RunnerExecutor (Production)

Adapts the existing `internal/runner.Run` function to the `SkillExecutor` interface.

```go
executor := execution.NewRunnerExecutor()
result, err := executor.Execute(ctx, execution.ExecuteOptions{
    ManifestPath: "/path/to/skill.yaml",
    ArtifactPath: "/path/to/binary",
    Input:        []byte(`{"message": "hello"}`),
})
```

### MockExecutor (Testing)

Test double that records calls and allows custom behavior injection.

```go
mock := execution.NewMockExecutor()
mock.ExecuteFunc = func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
    return &execution.Result{
        Stdout:   []byte(`{"ok": true}`),
        ExitCode: 0,
    }, nil
}

// Use mock in tests
result, err := mock.Execute(ctx, opts)

// Verify calls
assert.Equal(t, 1, mock.CallCount())
assert.Equal(t, "skill.yaml", mock.LastCall().ManifestPath)
```

### ExecutorFunc (Adapter)

Function type adapter enabling simple functions to implement `SkillExecutor`.

```go
customExec := execution.ExecutorFunc(func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
    // Custom execution logic
    return &execution.Result{Stdout: []byte("custom")}, nil
})
```

## Integration with Jobs Package

The `internal/runtime/jobs/executor` package now supports the `SkillExecutor` interface:

```go
// Default: uses RunnerExecutor
exec := executor.New(root, persist)

// Custom executor for testing
mockExec := execution.NewMockExecutor()
exec := executor.New(root, persist, executor.WithSkillExecutor(mockExec))

// Legacy: function-based injection (deprecated)
exec := executor.New(root, persist, executor.WithRunner(customRunnerFunc))
```

## Benefits

### Before SPEC-004

```go
// jobs/executor directly imports runner and skill
import (
    "github.com/joshka0/foxctl/internal/runtime/execution/runner"
    "github.com/joshka0/foxctl/internal/domain/skill"
)

func (e *Executor) executeSkill(...) {
    stdout, stderr, err := runner.Run(ctx, manifest, artifactPath, input)
    // Cannot mock, tightly coupled
}
```

**Problems:**
- Cannot mock execution in tests
- Tight coupling to runner implementation
- Hard to add execution middleware
- Cannot swap execution strategies

### After SPEC-004

```go
// jobs/executor uses the interface
import (
    "github.com/joshka0/foxctl/internal/runtime/execution"
)

type Executor struct {
    skillExecutor execution.SkillExecutor  // Injected!
}

func New(root string, persist Persistence, opts ...Option) *Executor {
    return &Executor{
        skillExecutor: execution.NewRunnerExecutor(),  // Default
    }
}
```

**Benefits:**
- ✅ Easy mocking in tests
- ✅ Decoupled from implementation details
- ✅ Clear interface contract
- ✅ Future extensibility (remote execution, pooling, etc.)

## Testing Example

```go
func TestJobExecutionWithMock(t *testing.T) {
    mock := execution.NewMockExecutor()
    mock.ExecuteFunc = func(ctx context.Context, opts execution.ExecuteOptions) (*execution.Result, error) {
        // Verify execution parameters
        assert.Equal(t, "expected/path.yaml", opts.ManifestPath)

        // Return controlled result
        return &execution.Result{
            Stdout:   []byte(`{"status": "success"}`),
            ExitCode: 0,
        }, nil
    }

    exec := executor.New(root, persist, executor.WithSkillExecutor(mock))

    // Test job logic without actual skill execution
    job, result, err := exec.RunSkill(ctx, manifest, artifact, input)

    require.NoError(t, err)
    assert.Equal(t, 1, mock.CallCount())
}
```

## Future Enhancements

- **Remote Execution**: Implement `SkillExecutor` for distributed execution
- **Execution Pooling**: Reuse execution contexts for performance
- **Middleware**: Add timeout, retry, logging layers
- **Resource Limits**: Enforce memory/CPU constraints
- **Observability**: Structured logging and metrics

## Related Specifications

- **SPEC-001**: Storage Interfaces (similar pattern)
- **SPEC-002**: Refactor Run Command (will use this interface)

## References

- [Dependency Injection in Go](https://blog.golang.org/wire)
- [Interface Segregation Principle](https://en.wikipedia.org/wiki/Interface_segregation_principle)
- [SPEC-004 Full Document](../../docs/refactoring/completed/SPEC-004-skill-executor-interface.md)
